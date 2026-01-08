package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/AI-Template-SDK/senso-workflows/internal/config"
	"github.com/joho/godotenv"
)

type webSearchRequest struct {
	Model string `json:"model"`
	Tools []tool `json:"tools"`
	Input string `json:"input"`
}

type tool struct {
	Type         string        `json:"type"`
	UserLocation *userLocation `json:"user_location,omitempty"`
}

type userLocation struct {
	Type    string  `json:"type"`
	Country string  `json:"country"`
	Region  *string `json:"region,omitempty"`
	City    *string `json:"city,omitempty"`
}

func main() {
	var (
		prompt  = flag.String("prompt", "", "prompt/input text to send")
		model   = flag.String("model", "", "Azure deployment name to use (defaults to AZURE_OPENAI_DEPLOYMENT_NAME)")
		country = flag.String("country", "US", "2-letter country code for user_location (e.g. US)")
		region  = flag.String("region", "", "optional region for user_location")
		city    = flag.String("city", "", "optional city for user_location")
		timeout = flag.Duration("timeout", 2*time.Minute, "request timeout")
	)
	flag.Parse()

	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("dev.env")
	}
	cfg := config.Load()

	if strings.TrimSpace(*prompt) == "" {
		log.Fatalf("--prompt is required")
	}
	if strings.TrimSpace(cfg.AzureOpenAIEndpoint) == "" {
		log.Fatalf("AZURE_OPENAI_ENDPOINT is required")
	}
	if strings.TrimSpace(cfg.AzureOpenAIKey) == "" {
		log.Fatalf("AZURE_OPENAI_KEY is required")
	}

	deployment := strings.TrimSpace(*model)
	if deployment == "" {
		deployment = strings.TrimSpace(cfg.AzureOpenAIDeploymentName)
	}
	if deployment == "" {
		log.Fatalf("Azure deployment is required: set AZURE_OPENAI_DEPLOYMENT_NAME or pass --model")
	}

	loc := &userLocation{
		Type:    "approximate",
		Country: strings.ToUpper(strings.TrimSpace(*country)),
	}
	if strings.TrimSpace(*region) != "" {
		r := strings.TrimSpace(*region)
		loc.Region = &r
	}
	if strings.TrimSpace(*city) != "" {
		c := strings.TrimSpace(*city)
		loc.City = &c
	}

	reqBody := webSearchRequest{
		Model: deployment,
		Tools: []tool{
			{
				Type:         "web_search_preview",
				UserLocation: loc,
			},
		},
		Input: *prompt,
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		log.Fatalf("failed to marshal request: %v", err)
	}

	endpoint := strings.TrimRight(strings.TrimSpace(cfg.AzureOpenAIEndpoint), "/")
	url := endpoint + "/openai/v1/responses"

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		log.Fatalf("failed to create request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("api-key", strings.TrimSpace(cfg.AzureOpenAIKey))

	// Helpful for debugging where the request is going (keep stderr clean of JSON).
	fmt.Fprintf(os.Stderr, "[azure_websearch_test] POST %s\n", url)
	fmt.Fprintf(os.Stderr, "[azure_websearch_test] model=%s country=%s region=%s city=%s\n",
		deployment, loc.Country, safePtr(loc.Region), safePtr(loc.City))

	resp, err := (&http.Client{}).Do(httpReq)
	if err != nil {
		log.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("failed to read response body: %v", err)
	}

	// Print FULL raw JSON response (or error body) to stdout.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(os.Stderr, "[azure_websearch_test] HTTP %d\n", resp.StatusCode)
	}
	os.Stdout.Write(body)
	if len(body) == 0 || body[len(body)-1] != '\n' {
		fmt.Fprintln(os.Stdout)
	}
}

func safePtr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}


