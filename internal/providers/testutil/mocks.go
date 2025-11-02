package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/AI-Template-SDK/senso-workflows/internal/providers/common"
)

// MockCostService is a mock implementation of CostService for testing
type MockCostService struct {
	CalculateCostFunc func(provider, model string, inputTokens, outputTokens int, websearch bool) float64
}

func (m *MockCostService) CalculateCost(provider, model string, inputTokens, outputTokens int, websearch bool) float64 {
	if m.CalculateCostFunc != nil {
		return m.CalculateCostFunc(provider, model, inputTokens, outputTokens, websearch)
	}
	return 0.0015 // Default mock cost
}

func (m *MockCostService) GetCostByModel(provider, model string) (float64, float64, error) {
	return 0.0, 0.0, nil
}

func (m *MockCostService) GetCostByProvider(provider string) map[string]interface{} {
	return map[string]interface{}{}
}

// NewMockCostService creates a new mock cost service
func NewMockCostService() *MockCostService {
	return &MockCostService{}
}

// MockBrightDataServer creates a mock HTTP server for BrightData API
type MockBrightDataServer struct {
	Server     *httptest.Server
	SnapshotID string
	Status     string
	Results    []byte
}

// NewMockBrightDataServer creates a new mock BrightData server
func NewMockBrightDataServer() *MockBrightDataServer {
	mock := &MockBrightDataServer{
		SnapshotID: "test-snapshot-123",
		Status:     "ready",
	}

	mux := http.NewServeMux()

	// POST /trigger - Submit job
	mux.HandleFunc("/datasets/v3/trigger", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		response := common.TriggerResponse{
			SnapshotID: mock.SnapshotID,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// GET /progress/:snapshot_id - Check progress
	mux.HandleFunc("/datasets/v3/progress/", func(w http.ResponseWriter, r *http.Request) {
		response := common.ProgressResponse{
			Status:     mock.Status,
			SnapshotID: mock.SnapshotID,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// GET /snapshot/:snapshot_id - Get results
	mux.HandleFunc("/datasets/v3/snapshot/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if mock.Results != nil {
			w.Write(mock.Results)
		} else {
			w.Write([]byte("[]"))
		}
	})

	mock.Server = httptest.NewServer(mux)
	return mock
}

// Close closes the mock server
func (m *MockBrightDataServer) Close() {
	m.Server.Close()
}

// SetStatus sets the mock job status
func (m *MockBrightDataServer) SetStatus(status string) {
	m.Status = status
}

// SetResults sets the mock results response
func (m *MockBrightDataServer) SetResults(results []byte) {
	m.Results = results
}

// MockHTTPDoer is a mock HTTP client for testing
type MockHTTPDoer struct {
	DoFunc func(req *http.Request) (*http.Response, error)
}

func (m *MockHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	if m.DoFunc != nil {
		return m.DoFunc(req)
	}
	return nil, nil
}
