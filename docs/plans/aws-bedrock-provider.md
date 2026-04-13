# Plan: AWS Bedrock LLM Provider Support

## Context

The companion/RLM agents currently support 7 LLM providers (anthropic, openai, openrouter, groq, cerebras, gemini, lmstudio). AWS Bedrock is missing, which blocks users deploying on AWS who want to use Bedrock-hosted models (Claude, Llama, Mistral, etc.) without managing separate API keys.

Bedrock uses AWS SDK auth (IAM/STS) + its own Converse API — it is NOT OpenAI-compatible, so it cannot be added as a simple base URL swap.

AWS SDK v2 is already in `go.mod` (used by S3 CAS store).

## Architecture

All companion/RLM agents use `LLMChatEngine` (`internal/runtime/engine/llmchat_engine.go`) which makes OpenAI-compatible HTTP calls via `callLLM`. The dspy-go framework is no longer used by agents.

The integration point is a single function: `callLLM` — add a Bedrock branch that uses the AWS SDK instead of raw HTTP.

## Step 1: Add Bedrock Config Fields

**File:** `internal/platform/config/config.go`

Add to `LLMSettings`:
```go
BedrockRegion string // AWS_REGION or BEDROCK_REGION
```

No API key fields needed — Bedrock uses the standard AWS credential chain (env vars, shared credentials file, IAM role, ECS task role, EC2 instance profile). The existing `aws-sdk-go-v2/config` handles this automatically.

Add env var loading in the config finalizer:
```go
if region := os.Getenv("BEDROCK_REGION"); region != "" && cfg.LLM.BedrockRegion == "" {
    cfg.LLM.BedrockRegion = region
}
```

Add to `ResolveAPIKey`: return `"bedrock-iam"` sentinel (Bedrock doesn't use API keys; the sentinel signals "credentials available via IAM").

Add to `ResolveModel`: return env override or default `"anthropic.claude-3-5-sonnet-20241022-v2:0"`.

## Step 2: Add Default Model + Provider Detection

**File:** `internal/providers/llm/defaults.go`

Add case:
```go
case "bedrock":
    if model := os.Getenv("BEDROCK_MODEL"); model != "" {
        return model
    }
    return "anthropic.claude-3-5-sonnet-20241022-v2:0"
```

**File:** `internal/runtime/engine/llmchat_engine.go`

Add to `detectProvider()`:
```go
if os.Getenv("BEDROCK_REGION") != "" || os.Getenv("AWS_DEFAULT_REGION") != "" {
    return "bedrock-iam", "bedrock"
}
```

Add to `baseURLForProvider()`:
```go
case "bedrock":
    return "" // Uses AWS SDK, not HTTP base URL
```

## Step 3: Create Bedrock Adapter

**File:** `internal/runtime/engine/bedrock.go` (NEW)

This file translates between the engine's OpenAI message types and Bedrock's Converse API:

```go
package engine

// BedrockClient wraps the AWS Bedrock Runtime Converse API.
type BedrockClient struct {
    client *bedrockruntime.Client
    region string
}

// NewBedrockClient creates a client using the default AWS credential chain.
func NewBedrockClient(ctx context.Context, region string) (*BedrockClient, error)

// Converse sends messages using the Bedrock Converse API,
// translating from oaiMessage/oaiTool to Bedrock types and back.
func (b *BedrockClient) Converse(ctx context.Context, model string, messages []oaiMessage, tools []oaiTool, temp float64, maxTokens int) (*oaiResponse, error)
```

Translation mapping:
| OpenAI | Bedrock Converse |
|--------|-----------------|
| `oaiMessage{role:"user"}` | `types.ConversationRole("user")` + `types.ContentBlockMemberText` |
| `oaiMessage{role:"assistant"}` | `types.ConversationRole("assistant")` + text/toolUse blocks |
| `oaiMessage{role:"system"}` | `ConverseInput.System` (extracted, not in messages) |
| `oaiToolCall` | `types.ContentBlockMemberToolUse` |
| `oaiMessage{role:"tool"}` | `types.ContentBlockMemberToolResult` |
| `oaiTool` | `types.ToolMemberToolSpec` with JSON schema |
| `oaiResponse.Usage` | `ConverseOutput.Usage` (input/output tokens) |

## Step 4: Wire Bedrock into callLLM

**File:** `internal/runtime/engine/llmchat_engine.go`

Add a `bedrockClient *BedrockClient` field to `LLMChatEngine`.

In `NewLLMChatEngine`, if `config.Provider == "bedrock"`, initialize the Bedrock client:
```go
if config.Provider == "bedrock" {
    region := config.BaseURL // reuse BaseURL field for region, or use env
    if region == "" {
        region = os.Getenv("BEDROCK_REGION")
        if region == "" {
            region = os.Getenv("AWS_DEFAULT_REGION")
        }
    }
    bc, err := NewBedrockClient(ctx, region)
    if err != nil {
        return nil, fmt.Errorf("bedrock client: %w", err)
    }
    e.bedrockClient = bc
}
```

In `callLLM`, add a branch at the top:
```go
if e.bedrockClient != nil {
    return e.bedrockClient.Converse(ctx, e.config.Model, messages, tools, e.config.Temperature, e.config.MaxTokens)
}
```

## Step 5: Register in Provider Availability

**File:** `internal/interfaces/web/api/companion.go`

Add to `CompanionProvidersHandler` providers list:
```go
{ID: "bedrock", Available: cfg.LLM.BedrockRegion != ""},
```

## Step 6: Add go.mod Dependency

```bash
go get github.com/aws/aws-sdk-go-v2/service/bedrockruntime
```

This is the only new dependency — the core AWS SDK and credential chain modules are already present from the S3 CAS store.

## Key Files

| File | Action |
|------|--------|
| `internal/platform/config/config.go` | MODIFY — add BedrockRegion field, env loading, resolve methods |
| `internal/providers/llm/defaults.go` | MODIFY — add bedrock default model |
| `internal/runtime/engine/bedrock.go` | CREATE — Bedrock Converse adapter |
| `internal/runtime/engine/llmchat_engine.go` | MODIFY — add bedrockClient field, detection, callLLM branch |
| `internal/interfaces/web/api/companion.go` | MODIFY — add bedrock to providers list |
| `go.mod` / `go.sum` | MODIFY — add bedrockruntime dependency |

## Environment Variables

| Variable | Required | Purpose |
|----------|----------|---------|
| `BEDROCK_REGION` | Yes (or `AWS_DEFAULT_REGION`) | AWS region for Bedrock endpoint |
| `BEDROCK_MODEL` | No | Override default model (default: `anthropic.claude-3-5-sonnet-20241022-v2:0`) |
| `AWS_ACCESS_KEY_ID` | Conditional | Static credentials (not needed with IAM roles) |
| `AWS_SECRET_ACCESS_KEY` | Conditional | Static credentials (not needed with IAM roles) |
| `AWS_SESSION_TOKEN` | No | For temporary credentials |
| `AWS_PROFILE` | No | Named profile from `~/.aws/credentials` |

The AWS SDK credential chain handles all these automatically — no custom credential loading needed.

## Verification

1. `go build ./internal/runtime/engine/...` — compiles
2. `go test ./internal/runtime/engine/...` — existing tests pass
3. Unit test for `bedrock.go`: mock Bedrock client, verify OAI→Bedrock message translation
4. Integration test (manual):
   ```bash
   export BEDROCK_REGION=us-east-1
   export AWS_PROFILE=your-profile  # or IAM role
   agentctl web serve --port=8080
   # In GUI: select "bedrock" provider → chat works
   ```
5. `curl localhost:8080/api/companion/providers` — shows `bedrock: true`

## Out of Scope (Future)

- **Streaming** — Bedrock supports `ConverseStream`, but companion doesn't use streaming yet
- **Cross-region inference** — Bedrock supports inference profiles for multi-region; add if needed
- **Bedrock Guardrails** — content filtering integration; separate feature
