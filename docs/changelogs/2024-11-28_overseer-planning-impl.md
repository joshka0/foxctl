# Overseer Planning Implementation

**Date:** 2024-11-28\
**Status:** Implemented\
**Related Specs:** `overseer_planning.md`, `overseer_profile.md`,
`mailbox_blackboard.md`

## Summary

Implemented the `todo/manage.plan` operation for overseer-driven task planning
and coordination, with LLM integration for intelligent decomposition and
mail_router hook enhancements for plan event surfacing.

## Changes

### CLI (`cmd/agentctl/cmd/todo.go`)

- Added `agentctl todo plan` command with flags:
  - `--goal` (required): One-sentence goal
  - `--description`: Detailed context
  - `--scope`: Directories/files likely touched (repeatable)
  - `--attach-to`: Attach to existing epic task
  - `--apply`: Apply plan (vs draft mode)
  - `--max-tasks`: Max tasks to create (default: 20)
  - `--strategy`: Planning strategy (auto/epic/flat)

### Skill (`skills/todo/main.go`)

- Added `planReq`, `planTask`, `planOutput`, `planGraphOutput`, `planEdge`,
  `planDiffOutput` structs
- Implemented `handlePlan` function with:
  - **Draft mode**: Preview tasks without persistence
  - **Apply mode**: Create tasks + emit plan events
  - **LLM integration**: Auto-uses Groq/OpenAI when available
  - **Fallback**: Simple epic creation when no LLM
- Plan events emitted: `plan.created`, `plan.updated`

### LLM Planning (`internal/intelligence/planning/llm/`)

- **planner.go**: Interface and prompt/response parsing
- **openai.go**: OpenAI-compatible client (works with Groq, OpenAI)
- **auto.go**: Auto-detect available LLM based on env vars
- **planner_test.go**: Unit tests + opt-in integration test

Supported environment variables:

- `GROQ_API_KEY` → Uses `llama-3.3-70b-versatile`
- `OPENAI_API_KEY` → Uses `gpt-4o-mini`

### Hook (`skills/hooks_mail_router/main.go`)

- Enhanced `buildMailContext` to specially format plan events
- Plan events shown with high visibility:
  - `plan.created` → "New Plan Created"
  - `plan.updated` → "Plan Updated"
  - `plan.review_needed` → "Plan Review Needed [ACTION REQUIRED]"

### Specs

- `docs/spec/overseer_planning.md` - Planning & coordination spec
- `docs/spec/overseer_profile.md` - Canonical overseer profile

## Usage

```bash
# Draft a plan (preview only)
agentctl todo plan --goal "Add OAuth2 authentication"

# Apply a plan with scope paths
agentctl todo plan --goal "Add Google OAuth" \
  --scope internal/auth \
  --scope cmd/auth \
  --apply

# Refine an existing epic
agentctl todo plan --goal "Add GitHub provider" \
  --attach-to <epic-id> \
  --apply

# With LLM (auto-detected from env)
export GROQ_API_KEY=gsk_...
agentctl todo plan --goal "Implement caching layer" --apply
```

## Testing

```bash
# Unit tests (no API key needed)
go test ./internal/intelligence/planning/llm/...

# E2E tests  
go test ./test/e2e/...

# Integration test (requires API key)
GROQ_API_KEY=... go test -v ./internal/intelligence/planning/llm/... -run Integration
```

## Architecture Notes

- Plan events flow: `handlePlan` → `BoardStore.SendMessage` → `mail_router` hook
  → Claude context
- LLM planner is optional; falls back to single epic task
- Overseer actor ID: `actor:system:overseer`
- Broadcast recipient: `actor:agent:*`
