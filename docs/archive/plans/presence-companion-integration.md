# Implementation Plan: Presence + Companion Agent Integration

> Status: A+ (plan-build-rp, 3 iterations) | Research: Opus scout-research (8 scouts + 2 Opus reviewers)

## Problem Statement

- The presence pipeline (`presence/orchestrate` → `presence/voice` + `presence/background` + `presence/character`) exists in the backend but is never activated
- `CompanionChatHandler` constructs `ServiceConfig` without `PresenceConfig` or `SkillRunner` — presence always disabled
- Frontend has zero presence types, no audio player, no emotion display
- Daemon `handleAsk` discards `resp.Presence`, only forwards text
- No per-conversation presence settings

## Scope and Locked Decisions

- `PresenceEnabled` is `*bool` in conversation settings — `nil` = enabled (default ON)
- `IsPresenceEnabled()` returns `true` when field is `nil` or pointer-to-`true`
- Presence forwarded in both `handleAsk` and `handleConsoleAsk`
- JSON merge-patch semantics remain (field omitted → unchanged; explicit `false` disables)
- Phase 1: emotion-only (no voice/background flags), no API keys required
- Future phases add voice, background, overlay, persistence

## Architecture Decision

**Approach: Adapter pattern + default-on settings + progressive frontend rendering**

- The backend presence engine is already complete in `internal/context/companion/service.go` (`generatePresence()`, `PresenceConfig`, `PresenceBundle`)
- We bridge the interface mismatch (`api.SkillRunner` → `companion.SkillRunner`) with a thin adapter
- Wire `PresenceConfig` into `ServiceConfig` at the existing gap in `companion.go`
- Forward presence through daemon reply envelopes
- Frontend progressively renders: emotion badge (Phase 1) → audio (Phase 2) → background/overlay (Phase 3)

**Why**: No engine changes needed. The entire presence pipeline works — it just isn't connected to the HTTP handler or daemon transport.

**Rejected alternatives**:
- Unifying `SkillRunner` interfaces: Would require cross-package refactor touching many consumers
- SSE streaming for presence: Batch is simpler; orchestrate is <100ms heuristic. Voice (2-5s) can be Phase 2.

## Design Patterns

| Pattern | Where Applied | Why |
|---------|---------------|-----|
| **Adapter** | `companion_presence.go` | Bridge `api.SkillRunner` → `companion.SkillRunner` without cross-package refactor |
| **Null Object** | `*bool` + `IsPresenceEnabled()` | Default-on semantics — nil means enabled |
| **Progressive Enhancement** | Frontend rendering | Emotion badge works without API keys; audio/background added later |

## Phase Structure (logical, deployable rollouts)

- **Phase 1** — Emotion only (no API keys): Backend wiring + frontend emotion badge
- **Phase 2** — Audio playback: Render `<audio>` for `presence.audio_digest`
- **Phase 3** — Background + overlay: Scene layer and character sprite
- **Phase 4** — Daemon transport: Both ask channels include presence
- **Phase 5** — Persistence: Store emotion metadata with turns, replay from history

---

## File Changes

### 1. `internal/storage/conversationsettings/settings.go` (EDIT)

Add `*bool` field with default-on helper.

```go
type Settings struct {
    // ...existing fields...
    PresenceEnabled *bool `json:"presence_enabled,omitempty"`
}

func (s *Settings) IsPresenceEnabled() bool {
    if s == nil {
        return true
    }
    if s.PresenceEnabled == nil {
        return true
    }
    return *s.PresenceEnabled
}
```

### 2. `internal/interfaces/web/api/companion_presence.go` (NEW)

Adapter bridging `api.SkillRunner` → `companion.SkillRunner`.

```go
package api

import (
    "context"
    "fmt"

    "github.com/joshka0/foxctl/internal/context/companion"
)

type companionSkillRunnerAdapter struct {
    inner *SkillRunner
}

func (a *companionSkillRunnerAdapter) Run(ctx context.Context, skillName string, input map[string]any) (*companion.SkillRunResult, error) {
    if a == nil || a.inner == nil {
        return nil, fmt.Errorf("presence skill runner is not configured")
    }

    result, err := a.inner.Run(ctx, skillName, input)
    if err != nil {
        return nil, err
    }
    if result == nil {
        return nil, fmt.Errorf("presence skill runner returned nil result for %s", skillName)
    }

    return &companion.SkillRunResult{
        Success: result.Success,
        Output:  result.Output,
        Error:   result.Error,
    }, nil
}
```

### 3. `internal/interfaces/web/api/companion.go` (EDIT)

Update `CompanionChatHandler` signature and wire `PresenceConfig` into `ServiceConfig`.

**Signature change:**
```go
func CompanionChatHandler(cfg config.Config, log zerolog.Logger, turnLock companion.Locker, skillRunner *SkillRunner) http.HandlerFunc
```

**ServiceConfig construction (~lines 196-208):**
```go
presenceEnabled := settings.IsPresenceEnabled()

svcCfg := companion.ServiceConfig{
    Logger:          log,
    MemoryDB:        memoryDB,
    LLMProvider:     llmProvider,
    LLMAPIKey:       llmAPIKey,
    LLMModel:        llmModel,
    ToolsAllow:      settings.ToolsAllow,
    UseHybridMemory: true,
}

if presenceEnabled {
    svcCfg.PresenceConfig = &companion.PresenceConfig{
        Enabled: true,
    }
    if skillRunner != nil {
        svcCfg.SkillRunner = &companionSkillRunnerAdapter{
            inner: skillRunner,
        }
    }
}
```

### 4. `internal/interfaces/web/server.go` (EDIT)

Update route registration (~line 496) to pass `skillRunner` (already created at ~line 561 for character routes):

```go
// From:
api.CompanionChatHandler(cfg, log, turnLock)
// To:
api.CompanionChatHandler(cfg, log, turnLock, skillRunner)
```

### 5. `internal/agent/daemon/handlers.go` (EDIT)

**5a. `handleAsk` (~lines 77-87):**
```go
answer := map[string]any{"response": result}
if resp != nil && resp.Presence != nil {
    answer["presence"] = resp.Presence
}
if resp != nil && resp.Tone != nil {
    answer["tone"] = resp.Tone
}
replyData := agent.ReplyData{
    AskID:  askData.AskID,
    Answer: answer,
}
```

**5b. `handleConsoleAsk` (~lines 300-319):**
```go
replyData := agent.ConsoleReplyData{
    AskID:    askData.AskID,
    Response: response,
    Status:   status,
    Metrics:  map[string]any{"duration_ms": durationMS},
}
if resp != nil && resp.Presence != nil {
    replyData.Presence = resp.Presence
}
if resp != nil && resp.Tone != nil {
    replyData.Tone = resp.Tone
}
```

### 6. `internal/domain/agent/types.go` (EDIT)

Add optional presence field to `ConsoleReplyData`:
```go
Presence any `json:"presence,omitempty"`
Tone     any `json:"tone,omitempty"`
```

Use `any` to avoid cross-package coupling with `internal/context/companion`.

### 7. `packages/gui-agent/src/api/types.ts` (EDIT)

```typescript
export interface PresenceBundle {
  emotion: string
  intensity: number
  display_text: string
  markers?: string[]
  detected_emoji?: string[]
  background_digest?: string
  overlay_digest?: string
  audio_digest?: string
  audio_duration_ms?: number
  cache_hits: number
  cache_misses: number
  errors?: string[]
}
```

Also add:
- `presence?: PresenceBundle` to `CompanionChatResponse`
- `presence_enabled?: boolean` to `ConversationSettings`
- `presence_enabled?: boolean` to `ConversationSettingsPatch`
- `presence?: PresenceBundle` to `ConsoleMessage`

### 8. `packages/gui-agent/src/api/client.ts` (EDIT)

- Import `PresenceBundle` from types
- Ensure `normalizeConsoleMessage` passes through `presence` field when present

### 9. `packages/gui-agent/src/components/chat/MessageBubble.tsx` (EDIT)

Emotion badge on assistant messages:

```tsx
const emotionColors: Record<string, string> = {
  joy: 'bg-yellow-400',
  sadness: 'bg-blue-400',
  anger: 'bg-red-500',
  fear: 'bg-purple-500',
  surprise: 'bg-orange-400',
  playful: 'bg-pink-400',
  disgust: 'bg-green-500',
}

const presence = message.presence
const emotion = presence?.emotion
const intensity = presence?.intensity ?? 0

{/* In bot avatar area: */}
<div className="relative flex-shrink-0 h-8 w-8 rounded-full flex items-center justify-center bg-muted">
  <Bot className="h-4 w-4" />
  {emotion && emotion !== 'neutral' && (
    <span
      className={`absolute -right-1 -top-1 h-2.5 w-2.5 rounded-full border border-background ${
        emotionColors[emotion] ?? 'bg-muted-foreground'
      }`}
      title={`${emotion} (${intensity.toFixed(2)})`}
      aria-label={`presence emotion ${emotion}`}
    />
  )}
</div>
```

Phase 2 extension: add `<audio>` player below message text when `presence?.audio_digest` is present.

### 10. `packages/gui-agent/src/components/conversations/ConversationsList.tsx` (EDIT)

Add Presence section in conversation inspector panel:
- Toggle: `Presence enabled` bound to `settings.presence_enabled ?? true`
- PATCH sends explicit `{ presence_enabled: false }` to disable, `{ presence_enabled: true }` to re-enable

---

## Testing Strategy

1. **Settings helper**: unit test `IsPresenceEnabled()` with nil settings, nil field, explicit true, explicit false
2. **Adapter**: verify field mapping from `api.RunResult` → `companion.SkillRunResult`, nil runner error
3. **Backend wiring**: `make build` after companion.go + server.go changes. POST `/api/companion/chat` returns `presence` field
4. **Daemon transport**: mock `ChatService` returning `PresenceBundle`, verify both `handleAsk` and `handleConsoleAsk` include `presence` in reply
5. **Frontend types**: `tsc --noEmit` after types.ts + client.ts changes
6. **Emotion badge**: visual verify — bot message with `presence.emotion !== "neutral"` shows colored dot
7. **Settings toggle**: browser test — toggle sends correct PATCH, UI reflects state

## Error Handling

- Missing settings row or nil settings → `IsPresenceEnabled()` returns `true` (default ON)
- Missing/nil `skillRunner` → adapter returns explicit error; `presenceEnabled()` guard in service.go prevents invocation
- Skill execution error → adapter propagates error; `generatePresence()` leaves text response intact
- Presence generation timeout → existing service.go behavior: response text preserved, presence fields omitted
- Daemon reply marshalling → `presence` only added when non-nil, no breaking change for strict clients

## Migration Notes

- No storage schema migration required — `*bool` field with `omitempty` is backward-compatible
- API response change is additive — existing clients ignore unknown `presence` field
- Route path unchanged — only handler signature updated internally
- `CompanionChatHandler` call site in `server.go` must be updated at deploy time (compile enforced)

## Implementation Order

1. `internal/storage/conversationsettings/settings.go` — `*bool` field + helper
   → Verify: `go build ./internal/storage/conversationsettings`

2. `internal/interfaces/web/api/companion_presence.go` — adapter file
   → Verify: `go build ./internal/interfaces/web/api`

3. `internal/interfaces/web/api/companion.go` + `internal/interfaces/web/server.go` — wire PresenceConfig + route registration
   → Verify: `make build`

4. `internal/agent/daemon/handlers.go` + `internal/domain/agent/types.go` — daemon presence forwarding
   → Verify: `go build ./internal/agent/daemon`

5. `packages/gui-agent/src/api/types.ts` + `packages/gui-agent/src/api/client.ts` — frontend types
   → Verify: `cd packages/gui-agent && npx tsc --noEmit`

6. `packages/gui-agent/src/components/chat/MessageBubble.tsx` — emotion badge
   → Verify: `tsc --noEmit` + visual check

7. `packages/gui-agent/src/components/conversations/ConversationsList.tsx` — settings toggle
   → Verify: browser interaction test

## Risks

- **SkillRunner adapter divergence**: If `companion.SkillRunResult` and `api.RunResult` add fields independently, adapter needs updating. Consider unifying long-term.
- **Presence latency**: Voice generation adds 2-5s per turn. Phase 1 (emotion only) is <100ms. Don't enable voice by default.
- **CAS CORS**: If GUI runs on different port than API (Vite dev), CAS asset requests need CORS headers. Check `internal/interfaces/web/server.go` CORS middleware.
- **`heartbeat_at` zero value**: Agents with `0001-01-01T00:00:00Z` heartbeat may cause issues in time comparisons (handled in Plan 1's utility).

## Open Questions

None.
