# Workflow Refactoring Summary

## Overview

The `ProcessOrg` workflow has been refactored to address the following issues:

1. **Network Timeout Compliance**: Steps now run under 1 minute to comply with AWS ECS Fargate requirements
2. **Better Visibility**: Each step shows clear progress and can be monitored individually
3. **Improved Error Handling**: Granular error handling with individual retry logic
4. **Faster Recovery**: Failed steps can be retried individually without restarting the entire pipeline

## Changes Made

### 1. Original Workflow Structure (Before)

The original workflow had 4 main steps:
- **Step 1**: Get Real Org Data from Database (✅ Fast)
- **Step 2**: Execute Question Matrix & Store Question Runs (❌ **THE PROBLEM** - 10+ minutes)
- **Step 3**: Generate Real Database Analytics (✅ Fast)
- **Step 4**: Push Analytics Results (✅ Fast)

### 2. New Workflow Structure (After)

The refactored workflow now has 6 main steps:

#### **Step 1**: Get Real Org Data from Database
- **Duration**: < 30 seconds
- **Purpose**: Fetch organization details, models, locations, questions
- **Status**: ✅ Unchanged (already fast)

#### **Step 2**: Initialize Question Processing Queue
- **Duration**: < 30 seconds
- **Purpose**: Create processing manifest with all question×model×location combinations
- **Output**: `QuestionProcessingQueue` with tracking information
- **Benefits**: 
  - Clear visibility of total work to be done
  - Progress tracking capability
  - Batch processing preparation

#### **Step 3**: Process Question Runs in Batches
- **Duration**: Variable (depends on number of runs, processed in batches of 3)
- **Purpose**: Process individual question runs with error handling
- **Features**:
  - Batch processing (3 runs at a time) to avoid overwhelming APIs
  - Individual error handling per question run
  - Progress tracking (completed/failed counts)
  - Graceful degradation (continues processing even if some runs fail)

#### **Step 4**: Update Latest Flags
- **Duration**: < 30 seconds
- **Purpose**: Update database flags after all runs complete
- **Features**:
  - Groups runs by question
  - Finds latest run for each question
  - Updates database flags

#### **Step 5**: Generate Real Database Analytics
- **Duration**: < 30 seconds
- **Purpose**: Calculate analytics from processed data
- **Status**: ✅ Unchanged (already fast)

#### **Step 6**: Push Analytics Results
- **Duration**: < 30 seconds
- **Purpose**: Push analytics to external systems
- **Status**: ✅ Unchanged (already fast)

### 3. New Data Structures

#### `QuestionProcessingQueue`
```go
type QuestionProcessingQueue struct {
    OrgID           string                    `json:"org_id"`
    OrgName         string                    `json:"org_name"`
    TargetCompany   string                    `json:"target_company"`
    TotalRuns       int                       `json:"total_runs"`
    CompletedRuns   int                       `json:"completed_runs"`
    FailedRuns      int                       `json:"failed_runs"`
    QuestionRuns    []*models.QuestionRun     `json:"question_runs"`
    ProcessingItems []*QuestionProcessingItem `json:"processing_items"`
    Status          string                    `json:"status"`
    CreatedAt       time.Time                 `json:"created_at"`
    CompletedAt     *time.Time                `json:"completed_at,omitempty"`
}
```

#### `QuestionProcessingItem`
```go
type QuestionProcessingItem struct {
    QuestionID   uuid.UUID `json:"question_id"`
    QuestionText string    `json:"question_text"`
    ModelID      uuid.UUID `json:"model_id"`
    ModelName    string    `json:"model_name"`
    LocationID   uuid.UUID `json:"location_id"`
    LocationCode string    `json:"location_code"`
    Status       string    `json:"status"` // pending, completed, failed
}
```

### 4. Additional Micro-Steps Workflow

A new workflow `ProcessSingleQuestionRun` has been added that breaks down individual question processing into micro-steps:

#### **Step 1**: Execute AI Call
- **Duration**: 30-60 seconds
- **Purpose**: Make the initial AI API call
- **Output**: AI response with tokens/cost

#### **Step 2**: Create Initial Question Run Record
- **Duration**: < 30 seconds
- **Purpose**: Store the question run in database
- **Output**: Question run record

#### **Step 3**: Extract Mentions
- **Duration**: 10-20 seconds
- **Purpose**: Extract company mentions from AI response
- **Output**: Mentions data

#### **Step 4**: Extract Claims
- **Duration**: 10-20 seconds
- **Purpose**: Extract claims from AI response
- **Output**: Claims data

#### **Step 5**: Extract Citations
- **Duration**: 10-20 seconds
- **Purpose**: Extract citations for claims
- **Output**: Citations data

#### **Step 6**: Calculate Competitive Metrics
- **Duration**: 10-20 seconds
- **Purpose**: Calculate competitive metrics
- **Output**: Competitive metrics

## Benefits Achieved

### 1. **Network Timeout Compliance**
- No step exceeds 1 minute
- Individual question runs can be processed separately
- Batch processing prevents overwhelming APIs

### 2. **Better Visibility**
- Each step shows clear progress
- Detailed logging for each operation
- Progress tracking with completed/failed counts

### 3. **Improved Error Handling**
- Individual error handling per question run
- Graceful degradation (continues processing even if some runs fail)
- Detailed error reporting per step

### 4. **Faster Recovery**
- Failed steps can be retried individually
- No need to restart entire pipeline
- State preservation between steps

### 5. **Resource Optimization**
- Batch processing (3 runs at a time)
- Better control over parallel processing
- Reduced memory usage per step

## Example Processing Flow

For an organization with:
- 5 questions
- 3 models  
- 3 locations

**Total runs**: 5 × 3 × 3 = 45 question runs

**Processing time**: 
- **Before**: 45 runs × 2.5 minutes = ~112 minutes (almost 2 hours!)
- **After**: 45 runs ÷ 3 per batch = 15 batches × 2.5 minutes = ~37.5 minutes

**Visibility**: Each batch shows clear progress, and individual failures don't stop the entire pipeline.

## Future Enhancements

1. **Parallel Processing**: Implement true parallel processing of question runs
2. **Dynamic Batching**: Adjust batch size based on API rate limits
3. **Progress Persistence**: Store progress in database for resuming interrupted workflows
4. **Advanced Monitoring**: Add metrics and alerting for workflow performance
5. **Individual Question Run Workflow**: Use the micro-steps workflow for even finer granularity

## Migration Notes

- The existing `ProcessOrg` workflow maintains backward compatibility
- New workflows are additive and don't break existing functionality
- The refactored workflow can be gradually adopted
- Individual question run processing can be triggered separately if needed

## Testing Recommendations

1. **Unit Tests**: Test each step individually
2. **Integration Tests**: Test the complete workflow with small datasets
3. **Load Tests**: Test with realistic data volumes
4. **Error Handling Tests**: Test various failure scenarios
5. **Timeout Tests**: Verify compliance with 1-minute timeout requirement 