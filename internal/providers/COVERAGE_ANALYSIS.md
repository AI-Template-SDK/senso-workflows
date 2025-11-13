# Test Coverage Analysis - Missing Tests

## 📊 Current Coverage Summary

```
Package                                     Coverage    Status
---------------------------------------------------------------
internal/providers                          94.7%      ✅ Excellent
internal/providers/common                   49.5%      ⚠️ Medium
internal/providers/chatgpt                  15.9%      ❌ Low
---------------------------------------------------------------
TOTAL                                       29.2%      ❌ Needs Improvement
```

## 🔍 Detailed Coverage Breakdown

### ✅ **Well Tested (90%+ Coverage)**
- `factory.go` - 94.7% ✅
- `common/location_mapper.go` - 100% ✅
- `common/response_parser.go` - 100% ✅
- `chatgpt/provider.go` (metadata) - 100% ✅

### ⚠️ **Partially Tested (40-70% Coverage)**
- `common/brightdata_client.go` - 49.5% ⚠️
- `chatgpt/async.go` - ~40% ⚠️

### ❌ **Not Tested (0% Coverage)**
- `chatgpt/batch.go` - Core batch processing: **0%** ❌
- `chatgpt/single.go` - Single question runs: **0%** ❌
- `common/brightdata_client.go` - `PollUntilComplete`: **0%** ❌

---

## 🚨 **CRITICAL MISSING TESTS**

### 1. **ChatGPT Batch Processing (`batch.go`) - 0% Coverage**

#### ❌ **`RunQuestionBatch()` - 0%**
**What it does**: Full sync batch flow (submit → poll → retrieve)
**Missing Tests**:
- ✅ Happy path with valid queries
- ✅ Empty queries array
- ✅ Batch size validation (>20)
- ✅ Error handling (submit failure, poll timeout, retrieve failure)
- ✅ Context cancellation during polling
- ✅ Cost calculation verification

#### ❌ **`parseBatchResults()` - 0%**
**What it does**: Parses JSON response into Result structs
**Missing Tests**:
- ✅ Valid JSON array parsing
- ✅ Invalid JSON handling (save error file)
- ✅ Empty results array
- ✅ Malformed JSON
- ✅ Error response with `input` echo

#### ❌ **`matchAndConvertResults()` - 0%**
**What it does**: Matches results to queries (by index or prompt)
**Missing Tests**:
- ✅ Index-based matching (happy path)
- ✅ Prompt-based matching (fallback when indices invalid)
- ✅ Missing result for query
- ✅ Duplicate indices
- ✅ Invalid indices (< 1 or > len(queries))
- ✅ Result count mismatch
- ✅ Matching with error results

#### ❌ **`convertResultToResponse()` - 0%**
**What it does**: Converts Result to AIResponse
**Missing Tests**:
- ✅ Success case with citations (array)
- ✅ Success case with citations (string)
- ✅ Success case with nil citations
- ✅ Error case (sets ShouldProcessEvaluation=false)
- ✅ Empty answer_text_markdown (sets ShouldProcessEvaluation=false)
- ✅ Citation clearing on error
- ✅ Cost calculation (0.0015)

---

### 2. **ChatGPT Single Question (`single.go`) - 0% Coverage**

#### ❌ **`RunQuestion()` - 0%**
**What it does**: Single question sync flow
**Missing Tests**:
- ✅ Happy path
- ✅ Error handling (submit, poll, retrieve failures)
- ✅ Context cancellation
- ✅ Cost calculation

#### ❌ **`RunQuestionWebSearch()` - 0%**
**What it does**: Single question with websearch=true, default US location
**Missing Tests**:
- ✅ Calls RunQuestion with correct params
- ✅ Default location is US
- ✅ Websearch is true

#### ❌ **`submitSingleJob()` - 0%**
**What it does**: Submits single query to BrightData
**Missing Tests**:
- ✅ Payload structure (index=1, correct country)
- ✅ Websearch flag propagation
- ✅ Location mapping

#### ❌ **`parseSingleResult()` - 0%**
**What it does**: Parses single result from response
**Missing Tests**:
- ✅ Valid single result
- ✅ Empty results array
- ✅ Multiple results (should take first)
- ✅ Error result handling

---

### 3. **ChatGPT Async Methods (`async.go`) - Partial Coverage**

#### ⚠️ **`SubmitBatchJob()` - 75%**
**Missing**:
- ✅ Actual HTTP call success (needs mock server)
- ✅ Error handling (API errors, network errors)
- ✅ Payload validation

#### ⚠️ **`PollJobStatus()` - 44.4%**
**Missing**:
- ✅ "ready" status handling
- ✅ "failed" status handling
- ✅ "running" status handling
- ✅ Network errors
- ✅ Context cancellation

#### ⚠️ **`RetrieveBatchResults()` - 30.8%**
**Missing**:
- ✅ Full retrieval flow with mock server
- ✅ Result parsing integration
- ✅ Error handling
- ✅ Empty results

---

### 4. **BrightData Client (`common/brightdata_client.go`) - 49.5% Coverage**

#### ⚠️ **`SubmitBatchJob()` - 63.2%**
**Missing**:
- ✅ Actual HTTP success (needs baseURL injection or mock)
- ✅ Error cases:
  - Non-200 status codes
  - JSON marshal errors
  - Request creation errors
  - Network errors
  - Response decode errors

#### ⚠️ **`CheckProgress()` - 60.0%**
**Missing**:
- ✅ Actual HTTP success (needs mock)
- ✅ Error cases:
  - Non-200 status codes
  - Network errors
  - JSON decode errors

#### ❌ **`PollUntilComplete()` - 0%**
**What it does**: Polls every 10s until job is ready/failed
**Missing Tests**:
- ✅ Polls until "ready" status
- ✅ Returns error on "failed" status
- ✅ Context cancellation stops polling
- ✅ Retries on progress check errors
- ✅ Ticker cleanup
- ✅ Poll count tracking

#### ⚠️ **`GetBatchResults()` - 36.1%**
**Missing**:
- ✅ Full retry logic (20 attempts)
- ✅ "building" status handling (waits 30s)
- ✅ "failed" status handling
- ✅ Success case (returns body bytes)
- ✅ Non-200/202 status codes
- ✅ Context cancellation during retry
- ✅ Max retries exceeded
- ✅ Body read errors

#### ⚠️ **`SaveErrorResponse()` - 75%**
**Missing**:
- ✅ File write error handling
- ✅ File permissions verification

---

### 5. **Factory (`factory.go`) - 94.7% Coverage**

#### ⚠️ **Missing Edge Cases**:
- ✅ Empty OpenAI API key (should return error)
- ✅ Empty model name validation
- ✅ Whitespace-only model name
- ✅ Model name with special characters

---

## 🎯 **Root Cause: HTTP Mocking Limitations**

### **Problem**: 
Tests can't fully test HTTP interactions because:
1. `BrightDataClient` has hardcoded `baseURL = "https://api.brightdata.com/datasets/v3"`
2. No way to inject mock server URL
3. Tests currently expect failures and just log them

### **Solution Options**:

#### **Option 1: Refactor for Dependency Injection** (Recommended)
```go
// Add baseURL parameter to NewBrightDataClient
func NewBrightDataClient(apiKey string, baseURL string) *BrightDataClient {
    if baseURL == "" {
        baseURL = "https://api.brightdata.com/datasets/v3" // Default
    }
    // ...
}
```

#### **Option 2: Interface-Based HTTP Client**
```go
type HTTPDoer interface {
    Do(req *http.Request) (*http.Response, error)
}

type BrightDataClient struct {
    httpClient HTTPDoer  // Inject mock in tests
    // ...
}
```

#### **Option 3: Test Helpers with httptest.Server**
Create a helper that patches the client after creation (reflection/hacks).

---

## 📋 **Priority Test Checklist**

### **🔴 Critical (Must Have)**
1. ✅ `RunQuestionBatch()` - Full flow test with mock server
2. ✅ `matchAndConvertResults()` - Index and prompt matching
3. ✅ `convertResultToResponse()` - All response conversion paths
4. ✅ `parseBatchResults()` - JSON parsing and error handling
5. ✅ `PollUntilComplete()` - Polling logic and cancellation

### **🟡 Important (Should Have)**
6. ✅ `GetBatchResults()` - Retry logic and status handling
7. ✅ `RunQuestion()` - Single question flow
8. ✅ `SubmitBatchJob()` - Full HTTP success path
9. ✅ `PollJobStatus()` - All status types
10. ✅ `RetrieveBatchResults()` - Full integration

### **🟢 Nice to Have**
11. ✅ `RunQuestionWebSearch()` - Wrapper test
12. ✅ `submitSingleJob()` - Payload validation
13. ✅ `parseSingleResult()` - Single result parsing
14. ✅ Error file writing in `SaveErrorResponse()`
15. ✅ Factory edge cases

---

## 🛠️ **Recommended Test Implementation Strategy**

### **Phase 1: Refactor for Testability** (Required)
1. Add `baseURL` parameter to `NewBrightDataClient()` with default
2. Update all provider constructors to pass baseURL
3. Create test helper to create client with mock server URL

### **Phase 2: Core Batch Tests** (High Priority)
1. Mock HTTP server for batch operations
2. Test `RunQuestionBatch()` end-to-end
3. Test `parseBatchResults()` with various JSON inputs
4. Test `matchAndConvertResults()` with index/prompt matching
5. Test `convertResultToResponse()` with all cases

### **Phase 3: Async Method Tests** (Medium Priority)
1. Test `PollUntilComplete()` with different statuses
2. Test `GetBatchResults()` retry logic
3. Test `SubmitBatchJob()` success path
4. Test `PollJobStatus()` all status types

### **Phase 4: Single Question Tests** (Lower Priority)
1. Test `RunQuestion()` flow
2. Test `RunQuestionWebSearch()` wrapper
3. Test `submitSingleJob()` and `parseSingleResult()`

### **Phase 5: Edge Cases** (Polish)
1. Error handling tests
2. Context cancellation tests
3. Network error simulations
4. Malformed data handling

---

## 📈 **Expected Coverage After Fixes**

```
Package                                     Current    Target
---------------------------------------------------------------
internal/providers/common                   49.5%  →  85%+
internal/providers/chatgpt                  15.9%  →  80%+
---------------------------------------------------------------
TOTAL                                       29.2%  →  75%+
```

---

## 🎯 **Key Takeaways**

1. **Main Gap**: HTTP mocking is broken - can't test actual API calls
2. **Core Functions Untested**: Batch processing (0%), single question (0%), polling (0%)
3. **Quick Wins**: Test private methods through public APIs
4. **Required Refactor**: Add baseURL injection to BrightDataClient
5. **Priority**: Focus on batch processing first (most used code path)

---

**Bottom Line**: We need **~40-50 new tests** with proper HTTP mocking to reach 75%+ coverage. The biggest blocker is the hardcoded baseURL preventing mock server usage.

