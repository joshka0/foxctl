#!/bin/bash
# Run dspy-go agent integration test
#
# Usage: 
#   For Gemini:
#     export AGENTCTL_LLM_API_KEY="your-gemini-api-key"
#     ./run_dspy_test.sh
#
#   For Groq (OpenAI compatible):
#     export AGENTCTL_LLM_API_KEY="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"
#     # Note: dspy-go currently only supports Gemini natively

set -e

cd "$(dirname "$0")/../.."

if [ -z "$AGENTCTL_LLM_API_KEY" ]; then
    echo "Error: AGENTCTL_LLM_API_KEY not set"
    echo ""
    echo "For Gemini, get an API key from: https://ai.google.dev/"
    echo "Then run: export AGENTCTL_LLM_API_KEY='your-key'"
    exit 1
fi

echo "=== Building agentctl with CGO_ENABLED=0 ==="
CGO_ENABLED=0 go build -o ./bin/agentctl ./cmd/agentctl

echo ""
echo "=== Running integration test ==="
CGO_ENABLED=0 go test -tags=integration -v -timeout=10m ./test/integration/...
