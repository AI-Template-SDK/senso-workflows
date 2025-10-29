package common

// BrightData API response structures (shared across all BrightData-based providers)

// TriggerResponse is returned when submitting a job to BrightData
type TriggerResponse struct {
	SnapshotID string `json:"snapshot_id"`
}

// ProgressResponse contains the status of a BrightData job
type ProgressResponse struct {
	Status             string `json:"status"`
	SnapshotID         string `json:"snapshot_id"`
	DatasetID          string `json:"dataset_id"`
	Records            *int   `json:"records,omitempty"`
	Errors             *int   `json:"errors,omitempty"`
	CollectionDuration *int   `json:"collection_duration,omitempty"`
}

// StatusResponse is used to check if response is a status object rather than results
type StatusResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

// AIResponse contains the response from an AI provider
// Defined here to avoid import cycles
type AIResponse struct {
	Response                string
	InputTokens             int
	OutputTokens            int
	Cost                    float64
	Citations               []string
	ShouldProcessEvaluation bool
}
