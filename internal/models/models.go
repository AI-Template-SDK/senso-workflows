// internal/models/models.go
package models

import (
	"time"

	"github.com/google/uuid"
)

// Location represents a geographic location for running queries
type Location struct {
	Country string  `json:"country"`          // Required
	City    *string `json:"city,omitempty"`   // Optional
	Region  *string `json:"region,omitempty"` // Optional (state/region)
}

// ModelConfig represents an AI model configuration
type ModelConfig struct {
	Name      string `json:"name"`       // e.g., "gpt-4.1", "claude-sonnet-4-20250514"
	WebSearch bool   `json:"web_search"` // Whether to enable web search
}

type Question struct {
	ID      string                 `json:"id"`
	Text    string                 `json:"text"`
	Type    string                 `json:"type"`
	Options map[string]interface{} `json:"options"`
}

// QuestionRun represents a single execution of a question with a specific model and location
type QuestionRun struct {
	QuestionID   string    `json:"question_id"`
	Model        string    `json:"model"`
	Location     Location  `json:"location"`
	WebSearch    bool      `json:"web_search"`
	Response     string    `json:"response"`
	Cost         float64   `json:"cost"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	Error        string    `json:"error,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}

type QuestionResult struct {
	QuestionID string                 `json:"question_id"`
	Response   string                 `json:"response"`
	Metadata   map[string]interface{} `json:"metadata"`
	Runs       []*QuestionRun         `json:"runs"` // All individual runs for this question
}

// ExtractResult represents the result of extracting company mentions from a question's response
type ExtractResult struct {
	QuestionID     string           `json:"question_id"`
	TargetCompany  *CompanyMention  `json:"target_company"` // nil if not mentioned
	Competitors    []CompanyMention `json:"competitors"`
	ExtractionCost float64          `json:"extraction_cost"`
	InputTokens    int              `json:"input_tokens"`
	OutputTokens   int              `json:"output_tokens"`
	Model          string           `json:"model"`
	Error          string           `json:"error,omitempty"`
	Timestamp      time.Time        `json:"timestamp"`
}

// CompanyMention represents a company mentioned in the response
type CompanyMention struct {
	Name          string `json:"name"`
	Rank          int    `json:"rank"`           // Order of appearance (1 = first mentioned)
	MentionedText string `json:"mentioned_text"` // All text mentioning this company
	TextSentiment string `json:"text_sentiment"` // positive, negative, neutral, mixed
}

type Analytics struct {
	Metrics   map[string]float64 `json:"metrics"`
	Insights  []string           `json:"insights"`
	Timestamp time.Time          `json:"timestamp"`
}

type PushResult struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type OrgSummary struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	CreatedAt   time.Time  `json:"created_at"`
	IsActive    bool       `json:"is_active"`
	LastRunDate *time.Time `json:"last_run_date,omitempty"`
}
