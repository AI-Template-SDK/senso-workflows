# Batch API Fixes - BrightData and Perplexity Providers

## Problem Summary

The network workflow pipeline with batched BrightData/Perplexity usage was experiencing two critical issues:

### Issue 1: Race Condition Between Progress and Snapshot APIs
**Error:** `json: cannot unmarshal object into Go value of type []services.PerplexityResult`

**Root Cause:** 
- The progress endpoint (`/progress/{snapshotID}`) would return `status: "ready"`
- Code immediately called snapshot endpoint (`/snapshot/{snapshotID}`) to get results
- Snapshot endpoint was still building and returned `{"status":"building","message":"Snapshot is building, try again in 30s"}`
- Code tried to unmarshal this status object as an array of results → JSON decode error

### Issue 2: Insufficient Timeout
**Error:** `polling timeout after 15 minutes for snapshot s_mgay9ohw1svwisw5f8`

**Root Cause:**
- Hard-coded 15-minute timeout was insufficient for large batches (especially 20 questions)
- Perplexity/BrightData need to scrape websites, run AI models, etc.
- Large batches can legitimately take 20-30+ minutes

## Solutions Implemented

### 1. Removed Polling Timeouts
**Changed:** All `pollUntilComplete` and `pollBatchUntilComplete` functions
- **Before:** 15-minute hard timeout → would fail long-running batches
- **After:** No timeout → polls indefinitely until job completes or fails
- Added poll counter for better observability

**Rationale:** Let the API tell us when it's done or failed, rather than imposing arbitrary limits.

### 2. Added Retry Logic for "Building" Status
**Changed:** All `getBatchResults` functions in both providers
- **New:** Added `isStatusResponse()` helper function to detect status objects
- **New:** Retry loop with up to 20 attempts (10 minutes buffer)
- **New:** 30-second retry interval when snapshot is still "building"

**Behavior:**
1. Call snapshot endpoint
2. Check if response is a status object
3. If status is "building": wait 30s and retry (up to 20 times)
4. If status is "failed": return error immediately
5. If it's actual data: decode and return results

### 3. Improved Logging
- Added attempt counters (`attempt %d/%d`)
- Added poll counters (`poll #%d`)
- Log when waiting/retrying due to "building" status
- Better visibility into long-running operations

## Files Modified

### `/home/tomos/Documents/senso/github/senso-workflows/services/perplexity_provider.go`

1. **`pollUntilComplete()`** (lines 217-249)
   - Removed 15-minute timeout
   - Added poll counter

2. **`pollBatchUntilComplete()`** (lines 574-605)
   - Removed 15-minute timeout
   - Added poll counter

3. **`getBatchResults()`** (lines 607-695)
   - Added retry loop (max 20 attempts)
   - Added status detection and handling
   - Waits 30s between retries when status is "building"

4. **NEW: `isStatusResponse()`** (lines 697-714)
   - Helper function to detect status objects
   - Returns (isStatus, status, message)

### `/home/tomos/Documents/senso/github/senso-workflows/services/brightdata_provider.go`

1. **`pollUntilComplete()`** (lines 213-245)
   - Removed 15-minute timeout
   - Added poll counter

2. **`pollBatchUntilComplete()`** (lines 551-582)
   - Removed 15-minute timeout
   - Added poll counter

3. **`getBatchResults()`** (lines 584-672)
   - Added retry loop (max 20 attempts)
   - Added status detection and handling
   - Waits 30s between retries when status is "building"

4. **NEW: `isStatusResponse()`** (lines 674-691)
   - Helper function to detect status objects
   - Returns (isStatus, status, message)

## Expected Behavior After Fix

### Successful Batch Processing
```
[PerplexityProvider] 📊 Batch job status: running (poll #1)
[PerplexityProvider] 📊 Batch job status: running (poll #2)
...
[PerplexityProvider] 📊 Batch job status: ready (poll #45)
[PerplexityProvider] ✅ Batch job completed after 45 polls, retrieving results
[PerplexityProvider] 📡 API Response Status Code: 202 (attempt 1/20)
[PerplexityProvider] ⏳ Snapshot still building (attempt 1/20): Snapshot is building, try again in 30s
[PerplexityProvider] 💤 Waiting 30s before retry...
[PerplexityProvider] 📡 API Response Status Code: 200 (attempt 2/20)
[PerplexityProvider] ✅ Successfully retrieved 20 results
```

### Handling Long Batches
- No timeout errors
- Continues polling until completion
- Automatically retries snapshot endpoint if still building
- Clear logging shows progress

## Testing Recommendations

1. **Small batches (1-5 questions)**: Should complete quickly, minimal retries
2. **Medium batches (10 questions)**: May see some retries on snapshot endpoint
3. **Large batches (20 questions)**: Will take 20-30 minutes, multiple retries expected
4. **Monitor logs**: Look for poll counts and retry patterns

## Risk Assessment

- **Low Risk**: Changes are well-contained within polling/retry logic
- **Backward Compatible**: Doesn't change API contracts
- **Fail-Safe**: Context cancellation still works for manual interruption
- **Observable**: Enhanced logging provides visibility

## Future Improvements (Optional)

1. Make retry parameters configurable (max retries, interval)
2. Add exponential backoff for retries
3. Add metrics/monitoring for polling duration
4. Consider webhook-based notifications instead of polling

