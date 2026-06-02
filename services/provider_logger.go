// services/provider_logger.go
package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	workflowModels "github.com/AI-Template-SDK/senso-workflows/internal/models"
)

// ProviderLogger writes per-provider diagnostic logs to a dedicated file so AI
// provider behaviour (requests, responses, failures) can be investigated apart
// from the regular stdout logs. It stays a no-op until InitProviderLogger
// enables it, which lets providers call LogProvider unconditionally without any
// effect on normal runs.
type ProviderLogger struct {
	mu      sync.Mutex
	logger  *log.Logger
	file    *os.File
	enabled bool
}

var providerLogger = &ProviderLogger{}

// InitProviderLogger turns on provider logging to the given file path. When
// enabled is false it does nothing (logging stays off). An empty path falls
// back to a timestamped default in the working directory. Output is appended,
// so repeated runs accumulate in the same file when a fixed path is given.
func InitProviderLogger(enabled bool, path string) error {
	if !enabled {
		return nil
	}

	if path == "" {
		path = fmt.Sprintf("provider_logs_%s.log", time.Now().Format("20060102_150405"))
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open provider log file %q: %w", path, err)
	}

	providerLogger.mu.Lock()
	providerLogger.file = f
	providerLogger.logger = log.New(f, "", 0)
	providerLogger.enabled = true
	providerLogger.mu.Unlock()

	LogProvider("system", "provider logging started -> %s", path)
	return nil
}

// CloseProviderLogger flushes and closes the underlying log file. Safe to call
// even when logging was never enabled.
func CloseProviderLogger() {
	providerLogger.mu.Lock()
	defer providerLogger.mu.Unlock()
	if providerLogger.file != nil {
		_ = providerLogger.file.Close()
		providerLogger.file = nil
	}
	providerLogger.enabled = false
	providerLogger.logger = nil
}

// ProviderLoggingEnabled reports whether provider logging is currently active.
func ProviderLoggingEnabled() bool {
	providerLogger.mu.Lock()
	defer providerLogger.mu.Unlock()
	return providerLogger.enabled
}

// LogProvider writes one timestamped, provider-tagged line to the provider log
// file. It is a no-op when provider logging is disabled, so it is safe to call
// from any provider hot path.
func LogProvider(provider, format string, args ...interface{}) {
	providerLogger.mu.Lock()
	defer providerLogger.mu.Unlock()
	if !providerLogger.enabled || providerLogger.logger == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	providerLogger.logger.Printf("%s [%s] %s", time.Now().UTC().Format(time.RFC3339Nano), provider, msg)
}

// loggingProvider wraps any AIProvider and records the start, outcome and
// timing of each call to the provider log file. This gives uniform coverage
// across every provider without touching their individual implementations;
// providers that need deeper detail (e.g. AI Overview) also call LogProvider
// directly from inside their request handling.
type loggingProvider struct {
	inner AIProvider
}

// wrapWithLogging returns p decorated with logging when provider logging is
// enabled, otherwise p unchanged (zero overhead on normal runs).
func wrapWithLogging(p AIProvider) AIProvider {
	if !ProviderLoggingEnabled() {
		return p
	}
	return &loggingProvider{inner: p}
}

func formatLocation(location *workflowModels.Location) string {
	if location == nil {
		return "nil"
	}
	parts := location.Country
	if location.Region != nil && *location.Region != "" {
		parts += "/" + *location.Region
	}
	if location.City != nil && *location.City != "" {
		parts += "/" + *location.City
	}
	return parts
}

func (l *loggingProvider) logResult(name, query string, dur time.Duration, resp *AIResponse, err error) {
	if err != nil {
		LogProvider(name, "RunQuestion ERROR after %s query=%q err=%v", dur.Round(time.Millisecond), query, err)
		return
	}
	if resp == nil {
		LogProvider(name, "RunQuestion returned nil response after %s query=%q", dur.Round(time.Millisecond), query)
		return
	}
	LogProvider(name, "RunQuestion OK after %s query=%q respLen=%d citations=%d cost=$%.6f shouldEval=%v",
		dur.Round(time.Millisecond), query, len(resp.Response), len(resp.Citations), resp.Cost, resp.ShouldProcessEvaluation)
}

func (l *loggingProvider) RunQuestion(ctx context.Context, query string, websearch bool, location *workflowModels.Location) (*AIResponse, error) {
	name := l.inner.GetProviderName()
	LogProvider(name, "RunQuestion START query=%q websearch=%v location=%s", query, websearch, formatLocation(location))
	start := time.Now()
	resp, err := l.inner.RunQuestion(ctx, query, websearch, location)
	l.logResult(name, query, time.Since(start), resp, err)
	return resp, err
}

func (l *loggingProvider) RunQuestionWebSearch(ctx context.Context, query string) (*AIResponse, error) {
	name := l.inner.GetProviderName()
	LogProvider(name, "RunQuestionWebSearch START query=%q", query)
	start := time.Now()
	resp, err := l.inner.RunQuestionWebSearch(ctx, query)
	l.logResult(name, query, time.Since(start), resp, err)
	return resp, err
}

func (l *loggingProvider) RunQuestionBatch(ctx context.Context, queries []string, websearch bool, location *workflowModels.Location) ([]*AIResponse, error) {
	name := l.inner.GetProviderName()
	LogProvider(name, "RunQuestionBatch START count=%d websearch=%v location=%s", len(queries), websearch, formatLocation(location))
	start := time.Now()
	resp, err := l.inner.RunQuestionBatch(ctx, queries, websearch, location)
	dur := time.Since(start).Round(time.Millisecond)
	if err != nil {
		LogProvider(name, "RunQuestionBatch ERROR after %s count=%d err=%v", dur, len(queries), err)
	} else {
		LogProvider(name, "RunQuestionBatch OK after %s count=%d responses=%d", dur, len(queries), len(resp))
	}
	return resp, err
}

func (l *loggingProvider) GetProviderName() string { return l.inner.GetProviderName() }
func (l *loggingProvider) SupportsBatching() bool   { return l.inner.SupportsBatching() }
func (l *loggingProvider) GetMaxBatchSize() int     { return l.inner.GetMaxBatchSize() }
