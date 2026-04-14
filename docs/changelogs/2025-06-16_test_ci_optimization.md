# Test & CI Optimization - 2025-06-16

## Summary

This changelog documents improvements to test infrastructure, CI integration,
and code quality fixes for the foxctl project.

## Changes

### 1. Custom Duration Type for JSON Readability

**Files**: `internal/agent/types/types.go`, `internal/agent/tools/tools.go`,
`internal/agent/runtime/runtime.go`, `cmd/foxctl/cmd/dspy_agent.go`

- Added `types.Duration` type wrapping `time.Duration` with human-readable JSON
  marshaling
- Marshals to strings like `"30s"`, `"5m"`, `"1h30m"` instead of nanoseconds
- Unmarshals from both strings and numeric nanoseconds for backward
  compatibility
- Updated `AgentConfig.Timeout` and `ToolCall.Duration` fields to use the new
  type

### 2. TOCTOU Race Fix in Overseer Spawn

**Files**: `internal/agent/runtime/runtime.go`,
`internal/agent/runtime/overseer.go`

- Fixed race condition where concurrent spawn requests could exceed
  `MaxConcurrentAgents` limit
- Added `MaxConcurrentAgents` field to `runtime.Config`
- Moved limit enforcement into `Runtime.Spawn()` under write lock for atomic
  check-and-insert
- Overseer's check is now advisory (early bailout) with actual enforcement in
  Runtime

### 3. OpenAI Planner API Key Detection Refactor

**Files**: `internal/intelligence/planning/llm/openai.go`

- Cached environment variable reads once instead of repeated `os.Getenv()` calls
- Added explicit `Provider` field to `OpenAIConfig` to track detected provider
  source
- Eliminated fragile API key comparison logic
  (`config.APIKey == os.Getenv("GROQ_API_KEY")`)
- `Provider()` method now prefers the tracked provider field with BaseURL
  fallback
- Priority order: OpenRouter > Groq > OpenAI

### 4. CI Workflow Fixes

**Files**: `.github/workflows/ci.yml`

- Fixed `llm-planner-integration` job to use a default model name instead of
  requiring a secret
- `OPENROUTER_MODEL_NAME` now defaults to `"openai/gpt-4o-mini"` (not sensitive)
- Step gating with `env.OPENROUTER_API_KEY` correctly handles missing secrets

### 5. Makefile Test & Coverage Improvements

**Files**: `Makefile`

- Added documentation explaining `RACE_PKGS` exclusions:
  - `cmd/foxctl/cmd`: CLI handlers with minimal concurrency
  - `skills/`: standalone plugin binaries
  - `test/`: integration tests requiring special setup
- Removed `-short` flag from `test-race` for comprehensive race testing
- Updated `check-coverage` threshold from 40% to 60%
- Added coverage summary banner with pass/fail indicator

## Testing

- All lint checks pass
- Unit tests pass including LLM integration tests via OpenRouter
- Race tests run without `-short` flag

## Migration Notes

- No breaking changes to existing APIs
- JSON output for Duration fields will now show human-readable strings
- Backward compatible: numeric nanoseconds still accepted during unmarshal
