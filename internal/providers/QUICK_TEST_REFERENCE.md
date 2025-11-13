# Quick Test Reference

## 🚀 One-Liners

```bash
# Run all provider tests
go test ./internal/providers/...

# With coverage
go test -cover ./internal/providers/...

# Verbose output
go test -v ./internal/providers/...

# Use test runner script
./scripts/run_tests.sh --coverage --verbose

# Run specific test
go test -run TestFactoryCreatesCorrectProvider ./internal/providers/

# Generate HTML coverage report
go test -coverprofile=coverage.out ./internal/providers/... && go tool cover -html=coverage.out
```

## 📊 Current Test Stats

- **Total Tests**: 26
- **Execution Time**: ~1 second
- **Overall Coverage**: 29.2%
- **All Passing**: ✅ Yes

## 📁 Test Files

| File | Tests | Coverage | Status |
|------|-------|----------|--------|
| `factory_test.go` | 4 | 94.7% | ✅ |
| `common/location_mapper_test.go` | 2 | 100% | ✅ |
| `common/response_parser_test.go` | 2 | 100% | ✅ |
| `common/brightdata_client_test.go` | 6 | 50% | ⚠️ |
| `chatgpt/provider_test.go` | 3 | 100% | ✅ |
| `chatgpt/async_test.go` | 4 | - | ✅ |
| `chatgpt/batch_test.go` | 7 | - | ✅ |

## 🧪 What Each Test Does

### Factory Tests
- ✅ Model name pattern matching
- ✅ Provider creation
- ✅ Error handling

### Location Mapper Tests
- ✅ All 15 countries
- ✅ UK→GB mapping
- ✅ Nil/unknown defaults

### Response Parser Tests
- ✅ Status detection
- ✅ Min() function

### BrightData Client Tests
- ✅ Client creation
- ✅ Error file saving
- ⚠️ HTTP calls (needs mock injection)

### ChatGPT Provider Tests
- ✅ Metadata methods
- ✅ Batch size validation
- ✅ JSON parsing
- ✅ Error handling
- ✅ Citation parsing
- ✅ Result matching

## 🔧 Quick Test Scenarios

### Test a Specific Provider
```bash
go test ./internal/providers/chatgpt/ -v
```

### Test with Coverage
```bash
go test -coverprofile=coverage.out ./internal/providers/...
go tool cover -func=coverage.out | grep total
```

### Test Single Function
```bash
go test -run TestMapLocationToCountry ./internal/providers/common/
```

### Run Integration Tests
```bash
export BRIGHTDATA_API_KEY="your-key"
export BRIGHTDATA_DATASET_ID="your-dataset-id"
go test -tags=integration ./internal/providers/chatgpt/ -v
```

## 📝 Adding New Tests

### For ChatGPT Provider
```bash
# Edit existing test file
vim internal/providers/chatgpt/async_test.go

# Run just that package
go test ./internal/providers/chatgpt/ -v
```

### For New Provider (e.g., Perplexity)
```bash
# Create test files
cp -r internal/providers/chatgpt/provider_test.go internal/providers/perplexity/
cp -r internal/providers/chatgpt/testdata internal/providers/perplexity/

# Update provider name references
# Run tests
go test ./internal/providers/perplexity/ -v
```

## 🐛 Debugging

### View Test Output
```bash
go test -v ./internal/providers/chatgpt/
```

### Check Test Data
```bash
cat internal/providers/chatgpt/testdata/sample_response.json | jq
```

### Find Failing Test
```bash
go test ./internal/providers/... 2>&1 | grep FAIL
```

## ✅ Quick Checklist

Before committing:
- [ ] Run `go test ./internal/providers/...` - all pass
- [ ] Check coverage: `go test -cover ./internal/providers/...` 
- [ ] No race conditions: `go test -race ./internal/providers/...`
- [ ] Clean up test artifacts: `rm -f *_error_*.txt`

## 🎯 Coverage Improvement Plan

To get >80% coverage:

1. **Inject baseURL in BrightDataClient** → +30% common coverage
2. **Add mock HTTP server tests for async.go** → +40% chatgpt coverage
3. **Test batch.go private methods via public API** → +20% chatgpt coverage
4. **Add Perplexity tests** → New package coverage
5. **Add Gemini tests** → New package coverage

## 📚 Documentation

- **TEST_GUIDE.md** - Complete testing guide
- **TESTING_SUMMARY.md** - This file
- **ASYNC_ARCHITECTURE.md** - Architecture overview
- **README.md** - Package overview

---

**Tests are ready! Run `./scripts/run_tests.sh --coverage` to see results.** 🎉

