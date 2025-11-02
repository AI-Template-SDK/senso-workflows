package common_test

import (
	"testing"

	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
)

func TestIsStatusResponse(t *testing.T) {
	tests := []struct {
		name           string
		jsonBody       string
		expectIsStatus bool
		expectStatus   string
		expectMessage  string
	}{
		{
			name:           "valid status response with building",
			jsonBody:       `{"status": "building", "message": "Snapshot is being built"}`,
			expectIsStatus: true,
			expectStatus:   "building",
			expectMessage:  "Snapshot is being built",
		},
		{
			name:           "valid status response with ready",
			jsonBody:       `{"status": "ready", "message": ""}`,
			expectIsStatus: true,
			expectStatus:   "ready",
			expectMessage:  "",
		},
		{
			name:           "valid status response with failed",
			jsonBody:       `{"status": "failed", "message": "Job failed"}`,
			expectIsStatus: true,
			expectStatus:   "failed",
			expectMessage:  "Job failed",
		},
		{
			name:           "result array is not status response",
			jsonBody:       `[{"url": "test", "prompt": "test"}]`,
			expectIsStatus: false,
			expectStatus:   "",
			expectMessage:  "",
		},
		{
			name:           "empty object is not status response",
			jsonBody:       `{}`,
			expectIsStatus: false,
			expectStatus:   "",
			expectMessage:  "",
		},
		{
			name:           "invalid JSON is not status response",
			jsonBody:       `not valid json`,
			expectIsStatus: false,
			expectStatus:   "",
			expectMessage:  "",
		},
		{
			name:           "object without status field",
			jsonBody:       `{"other_field": "value"}`,
			expectIsStatus: false,
			expectStatus:   "",
			expectMessage:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isStatus, status, message := common.IsStatusResponse([]byte(tt.jsonBody))

			if isStatus != tt.expectIsStatus {
				t.Errorf("IsStatusResponse() isStatus = %v, want %v", isStatus, tt.expectIsStatus)
			}

			if status != tt.expectStatus {
				t.Errorf("IsStatusResponse() status = %s, want %s", status, tt.expectStatus)
			}

			if message != tt.expectMessage {
				t.Errorf("IsStatusResponse() message = %s, want %s", message, tt.expectMessage)
			}
		})
	}
}

func TestMin(t *testing.T) {
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{"a smaller", 5, 10, 5},
		{"b smaller", 10, 5, 5},
		{"equal", 5, 5, 5},
		{"negative a", -5, 10, -5},
		{"negative b", 10, -5, -5},
		{"both negative", -10, -5, -10},
		{"zero and positive", 0, 10, 0},
		{"zero and negative", 0, -10, -10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := common.Min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("Min(%d, %d) = %d, want %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}
