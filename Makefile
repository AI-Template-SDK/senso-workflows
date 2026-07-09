.PHONY: test eval eval-dataset

# Standard unit tests (fast, no LLM, safe for CI).
test:
	go test ./...

# Behavioral eval of the extraction PROMPTS against the golden set. Hits the live
# LLM (uses OPENAI_API_KEY / Azure config from .env), so it is NOT part of `test`
# or CI. Run manually whenever you change an extraction prompt or model. Fails if
# precision/recall/accuracy drop below the thresholds in mention_eval_test.go.
eval:
	go test -tags=llmeval ./services -run TestMentionGolden -v

# Source real labeled candidate cases from the prod DB (requires a DB tunnel).
# Review the output, then merge good cases into services/testdata/mention_golden.json.
# Example: make eval-dataset ORG_IDS=fa7516a5-...,c31d795e-...
eval-dataset:
	go run ./cmd/build_eval_dataset --org-ids "$(ORG_IDS)" --out eval_candidates.json
