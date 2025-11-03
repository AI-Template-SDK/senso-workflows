# Test Name Variations Service

This script tests the `GenerateNameVariations` service function from `org_evaluation_service.go`.

## Purpose

The `GenerateNameVariations` function generates realistic brand name variations that a company might use across different platforms, documents, and contexts. This is useful for:
- Brand monitoring
- Organization mention detection
- Name matching across different formats

## Prerequisites

Create a `.env` file in the project root with your credentials:

### Option 1: Standard OpenAI
```env
OPENAI_API_KEY=your-openai-api-key-here
```

### Option 2: Azure OpenAI
```env
AZURE_OPENAI_ENDPOINT=https://your-resource.openai.azure.com
AZURE_OPENAI_KEY=your-azure-key-here
AZURE_OPENAI_DEPLOYMENT_NAME=your-deployment-name
```

## Usage

```bash
# Run all test cases
go run cmd/test_name_variations/main.go

# Run a specific test case (0-indexed)
go run cmd/test_name_variations/main.go 0  # Senso.ai
go run cmd/test_name_variations/main.go 1  # SunLife
go run cmd/test_name_variations/main.go 2  # TotalExpert
go run cmd/test_name_variations/main.go 3  # Bellweather Community Credit Union
```

## Test Cases

The script includes the following test cases:

1. **Senso.ai** - Testing dot-separated brand names
2. **SunLife** - Testing compound words
3. **TotalExpert** - Testing another compound word variant
4. **Bellweather Community Credit Union** - Testing multi-word organization names with acronym potential

## Customizing Test Cases

You can modify the `testCases` array in `main.go` to add your own test cases:

```go
testCases := []struct {
    OrgName  string
    Websites []string
}{
    {
        OrgName:  "Your Organization",
        Websites: []string{"https://yoursite.com"},
    },
}
```

## Example Output

```
=== Testing GenerateNameVariations Service Function ===

✅ Configuration loaded
   Using Standard OpenAI

📝 Test Case #1
   Organization: Senso.ai
   Websites: [https://senso.ai https://www.senso.ai]

[GenerateNameVariations] 🔍 Generating name variations for org: Senso.ai
[GenerateNameVariations] 🎯 Using Standard OpenAI model: gpt-4.1-mini
[GenerateNameVariations] 🚀 Making AI call for name variations...
[GenerateNameVariations] ✅ AI call completed successfully
[GenerateNameVariations]   - Input tokens: 850
[GenerateNameVariations]   - Output tokens: 120
[GenerateNameVariations] ✅ Generated 20 name variations

✅ Successfully generated 20 name variations:
    1. Senso.ai
    2. senso.ai
    3. SENSO.AI
    4. Senso
    5. senso
    6. SENSO
    7. SensoAI
    8. sensoai
    9. SENSOAI
   10. Senso AI
   ...
```

## Notes

- The function uses structured output from OpenAI to ensure consistent JSON responses
- Temperature is set to 0.3 for consistency (except for gpt-5 models which use reasoning_effort)
- The function generates 15-25 realistic variations per organization
- Database connections are not required for this test

