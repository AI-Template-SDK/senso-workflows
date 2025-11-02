# Provider Testing Guide

Complete guide for running and writing tests for the providers package.

## 📁 Test Structure

```
internal/providers/
├── factory_test.go                  # Factory pattern tests
├── testutil/                        # Shared test utilities
│   ├── mocks.go                    # Mock implementations
│   └── fixtures.go                 # Test data fixtures
├── common/
│   ├── location_mapper_test.go     # Location mapping tests
│   ├── response_parser_test.go     # Response parsing tests
│   └── brightdata_client_test.go   # HTTP client tests
└── chatgpt/
    ├── provider_test.go            # Provider metadata tests
    ├── async_test.go               # Async method tests (renamed to batch_test.go)
    ├── integration_test.go         # Integration tests (real API)
    └── testdata/                   # Test fixtures
        ├── sample_response.json
        ├── error_response.json
        └── invalid_index_response.json
```

## 🚀 Running Tests

### Run All Unit Tests
```bash
# From project root
go test ./internal/providers/...

# With verbose output
go test -v ./internal/providers/...

# With coverage
go test -cover ./internal/providers/...
```

### Run Specific Package
```bash
go test ./internal/providers/chatgpt/
go test ./internal/providers/common/
go test ./internal/providers/
```

### Run Specific Test
```bash
go test -run TestFactoryCreatesCorrectProvider ./internal/providers/
go test -run TestMapLocationToCountry ./internal/providers/common/
go test -run TestProviderMetadata ./internal/providers/chatgpt/
```

### Generate Coverage Report
```bash
# Generate coverage profile
go test -coverprofile=coverage.out ./internal/providers/...

# View coverage in terminal
go tool cover -func=coverage.out

# View coverage in browser
go tool cover -html=coverage.out
```

### Run with Race Detection
```bash
go test -race ./internal/providers/...
```

### Run Integration Tests (Requires Real API Keys)
```bash
# Set environment variables
export BRIGHTDATA_API_KEY="your-key"
export BRIGHTDATA_DATASET_ID="your-dataset-id"

# Run integration tests
go test -tags=integration ./internal/providers/chatgpt/

# Run with verbose output
go test -v -tags=integration ./internal/providers/chatgpt/
```

## 📊 Test Coverage

Current test coverage by package:

| Package | Files Tested | Coverage Goal | Status |
|---------|--------------|---------------|--------|
| `factory.go` | ✅ | 100% | ✅ Complete |
| `common/location_mapper.go` | ✅ | 100% | ✅ Complete |
| `common/response_parser.go` | ✅ | 100% | ✅ Complete |
| `common/brightdata_client.go` | ✅ | 85% | ⚠️ Partial (needs baseURL injection) |
| `chatgpt/provider.go` | ✅ | 100% | ✅ Complete |
| `chatgpt/async.go` | ✅ | 90% | ⚠️ Partial (needs mock server) |
| `chatgpt/batch.go` | ✅ | 85% | ⚠️ Partial (private methods) |

## ✅ What Is Tested

### Factory Tests (`factory_test.go`)
- ✅ Creates correct provider for each model name
- ✅ Handles case-insensitive model names
- ✅ Returns errors for unsupported models
- ✅ Handles nil config
- ✅ Handles nil cost service

### Location Mapper Tests (`common/location_mapper_test.go`)
- ✅ Maps all 15 supported countries
- ✅ Handles nil location (defaults to US)
- ✅ Handles unknown countries (defaults to US)
- ✅ UK→GB special mapping
- ✅ Case-insensitive country codes

### Response Parser Tests (`common/response_parser_test.go`)
- ✅ Detects status responses vs result arrays
- ✅ Parses different status types (building, ready, failed)
- ✅ Handles invalid JSON
- ✅ Min() function correctness

### BrightData Client Tests (`common/brightdata_client_test.go`)
- ✅ Client initialization
- ⚠️ SubmitBatchJob (needs baseURL injection)
- ⚠️ CheckProgress (needs baseURL injection)
- ✅ SaveErrorResponse file creation
- ⚠️ GetBatchResults (needs baseURL injection)

### ChatGPT Provider Tests (`chatgpt/`)
- ✅ Provider metadata (name, async, batching, max size)
- ✅ Batch size validation (max 20)
- ✅ JSON parsing with test fixtures
- ✅ Error response handling
- ✅ Invalid index handling
- ✅ Citation parsing (array, string, null)
- ✅ Result matching by index
- ✅ Result matching by prompt (fallback)

### Integration Tests (`chatgpt/integration_test.go`)
- ✅ Full async flow (submit → poll → retrieve)
- ✅ Sync batch flow (RunQuestionBatch)
- ⚠️ Requires real API credentials

## 🔧 Test Utilities

### Mocks (`testutil/mocks.go`)
- `MockCostService` - Mock cost calculations
- `MockBrightDataServer` - Mock HTTP server for BrightData API
- `MockHTTPDoer` - Mock HTTP client

### Fixtures (`testutil/fixtures.go`)
- `SampleConfig()` - Test configuration
- `SampleLocation()` - Test location (California, US)
- `SampleQueries()` - Test queries
- `SampleBrightDataResponse()` - Mock API response JSON
- `SampleErrorResponse()` - Mock error response JSON
- `SampleStatusResponse()` - Mock status response JSON

### Test Data (`chatgpt/testdata/`)
- `sample_response.json` - Valid 3-result response
- `error_response.json` - Mixed error/success response
- `invalid_index_response.json` - Results with invalid indices

## ⚠️ Known Limitations

### BrightData Client Tests
The `BrightDataClient` tests are partial because the client uses a hardcoded `baseURL`. To achieve full coverage, we would need to:

1. Inject `baseURL` as a parameter (refactor)
2. Use interface-based HTTP client (dependency injection)
3. Use httptest.Server with custom baseURL

**Workaround for now:**
- Integration tests cover real API calls
- Unit tests verify the structure and logic

### Private Methods
Go doesn't allow testing private methods directly. Methods like `convertResultToResponse()` are tested indirectly through:
- Public method calls
- Test data validation
- Integration tests

## 📝 Adding New Tests

### For a New Provider (e.g., Perplexity)

1. Create test files:
```bash
mkdir -p internal/providers/perplexity/testdata
touch internal/providers/perplexity/provider_test.go
touch internal/providers/perplexity/batch_test.go
touch internal/providers/perplexity/integration_test.go
```

2. Copy test structure from ChatGPT provider
3. Update expected values (provider name, etc.)
4. Add provider-specific test data

### For Common Utilities

Add tests to `internal/providers/common/*_test.go`:
```go
func TestNewUtility(t *testing.T) {
    // Test setup
    // Execute
    // Assert
}
```

## 🎯 Running Tests in CI/CD

### GitHub Actions Example
```yaml
- name: Run Unit Tests
  run: go test -v ./internal/providers/...

- name: Generate Coverage
  run: go test -coverprofile=coverage.out ./internal/providers/...

- name: Upload Coverage
  run: go tool cover -html=coverage.out -o coverage.html
```

### Docker Example
```bash
docker run --rm -v $(pwd):/app -w /app golang:1.21 \
  go test -v ./internal/providers/...
```

## 🐛 Debugging Failed Tests

### View Verbose Output
```bash
go test -v ./internal/providers/chatgpt/
```

### Run Single Test with Logging
```bash
go test -v -run TestProviderMetadata ./internal/providers/chatgpt/
```

### Check Test Data Files
```bash
cat internal/providers/chatgpt/testdata/sample_response.json
```

### View Error Files Created During Tests
```bash
ls -la *_error_*.txt
```

## 📚 Best Practices

1. **Table-Driven Tests**: Use for multiple similar test cases
2. **Test Data Files**: Store complex JSON in `testdata/` directory
3. **Integration Tests**: Use build tags (`// +build integration`)
4. **Mock External Dependencies**: Don't call real APIs in unit tests
5. **Clear Test Names**: Describe what is being tested
6. **Cleanup**: Remove temporary files in test teardown

## 🔍 Example Test Execution

```bash
$ go test -v ./internal/providers/common/

=== RUN   TestMapLocationToCountry
=== RUN   TestMapLocationToCountry/nil_location_defaults_to_US
=== RUN   TestMapLocationToCountry/US_maps_to_US
=== RUN   TestMapLocationToCountry/UK_maps_to_GB
--- PASS: TestMapLocationToCountry (0.00s)
    --- PASS: TestMapLocationToCountry/nil_location_defaults_to_US (0.00s)
    --- PASS: TestMapLocationToCountry/US_maps_to_US (0.00s)
    --- PASS: TestMapLocationToCountry/UK_maps_to_GB (0.00s)
=== RUN   TestIsStatusResponse
=== RUN   TestIsStatusResponse/valid_status_response_with_building
=== RUN   TestIsStatusResponse/result_array_is_not_status_response
--- PASS: TestIsStatusResponse (0.00s)
    --- PASS: TestIsStatusResponse/valid_status_response_with_building (0.00s)
    --- PASS: TestIsStatusResponse/result_array_is_not_status_response (0.00s)
PASS
ok      github.com/AI-Template-SDK/senso-workflows/internal/providers/common    0.003s
```

## 🚀 Quick Start

```bash
# 1. Run all unit tests (no API keys needed)
go test ./internal/providers/...

# 2. Check coverage
go test -cover ./internal/providers/...

# 3. Run integration tests (requires API keys)
export BRIGHTDATA_API_KEY="your-key"
export BRIGHTDATA_DATASET_ID="your-dataset-id"
go test -tags=integration ./internal/providers/chatgpt/
```

That's it! The test suite is ready to use. 🎉

