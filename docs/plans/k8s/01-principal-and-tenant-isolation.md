# Plan 01: Principal Type and Tenant Isolation

**Status**: Proposed
**Depends on**: None (foundation for all subsequent plans)
**Blocks**: 02-casbin-authorization, 03-river-background-jobs, 04-distribution

## Problem

Identity is fragmented across the codebase:

- `chatadapter.UserRef` carries platform user ID/username but never reaches hooks or engines
- `hooks.Input` carries `ActorID`, `SessionID`, `WorkspaceID` but no human user or tenant
- `engine.HookContext` is a bare struct with 4 string fields
- Teams JWT has `TenantID` but it stays in the adapter and is discarded
- Conversation keys are not tenant-scoped: two Teams tenants with colliding conversation IDs would share state

This blocks: multi-tenant deployment, RBAC/ABAC authorization, audit logging, and per-tenant resource limits.

## Design

### 1. Principal Type

```go
// internal/domain/identity/principal.go
package identity

// Principal represents the authenticated identity making a request.
// It unifies platform user identity, agent actor identity, and tenant context
// into a single struct that flows through context.Context.
type Principal struct {
    // TenantID identifies the organizational tenant (e.g., Teams tenant GUID).
    // Empty for single-tenant deployments.
    TenantID string

    // UserID is the platform-specific user identifier.
    // Discord snowflake, Telegram user ID, Teams AAD object ID, etc.
    UserID string

    // Username is the display name (best-effort, not unique).
    Username string

    // ActorID identifies the agent actor processing this request.
    // Format: "actor:agent:<name>" or empty for direct user requests.
    ActorID string

    // Platform identifies the origin: "discord", "telegram", "teams", "web", "cli".
    Platform string

    // WorkspaceID is the stable workspace identifier (hashed).
    WorkspaceID string

    // WorkspaceRoot is the absolute filesystem path of the workspace.
    WorkspaceRoot string

    // SessionID is the durable session identifier.
    SessionID string

    // Roles are the principal's authorization roles (populated by auth middleware).
    Roles []string
}

// IsAnonymous returns true if no user or actor identity is set.
func (p Principal) IsAnonymous() bool {
    return p.UserID == "" && p.ActorID == ""
}

// ConversationKey returns a tenant-scoped conversation key.
// Format: "{platform}:{tenantID}:{rawKey}" or "{platform}::{rawKey}" if no tenant.
func (p Principal) ConversationKey(rawKey string) string {
    if p.TenantID != "" {
        return p.Platform + ":" + p.TenantID + ":" + rawKey
    }
    return p.Platform + "::" + rawKey
}
```

### 2. Context Propagation

```go
// internal/domain/identity/context.go
package identity

import "context"

type contextKey struct{}

// WithPrincipal returns a new context with the given principal.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
    return context.WithValue(ctx, contextKey{}, p)
}

// FromContext extracts the principal from context. Returns zero Principal if absent.
func FromContext(ctx context.Context) Principal {
    p, _ := ctx.Value(contextKey{}).(Principal)
    return p
}
```

### 3. Tenant-Scoped Conversation Keys

Each adapter builds a `Principal` and uses it to generate scoped conversation keys.

## Files to Create

| File | Purpose |
|------|---------|
| `internal/domain/identity/principal.go` | Principal type definition |
| `internal/domain/identity/context.go` | Context helpers (WithPrincipal, FromContext) |
| `internal/domain/identity/principal_test.go` | Unit tests for ConversationKey, IsAnonymous |

## Files to Modify

### `internal/chatadapter/adapter.go`

- Add `Principal identity.Principal` field to `MessageEvent`, `CommandEvent`, `InteractionEvent`
- Keep `UserRef` for backward compat but populate Principal too

### `internal/chatadapter/teams/driver.go`

Construct Principal from Teams activity + JWT:

```go
principal := identity.Principal{
    TenantID:  activity.ChannelData.TenantID, // or from JWT claims
    UserID:    strings.TrimSpace(activity.From.ID),
    Username:  strings.TrimSpace(activity.From.Name),
    Platform:  "teams",
}
convKey := principal.ConversationKey(convID)
```

Use `convKey` instead of raw `convID` for:
- `serviceURLs.Store(convKey, serviceURL)`
- `isChatConversation(convKey)`
- `dispatchWithLimit(a.ctx, "teams.message", convKey, ...)`
- `handleCommand(ctx, serviceURL, activity, cmd, args)` — pass principal via context

### `internal/chatadapter/discord/driver.go`

```go
principal := identity.Principal{
    UserID:   m.Author.ID,
    Username: m.Author.Username,
    Platform: "discord",
    // No TenantID — Discord is single-guild per adapter
}
```

### `internal/chatadapter/telegram/driver.go`

```go
principal := identity.Principal{
    UserID:   strconv.FormatInt(m.From.ID, 10),
    Username: m.From.UserName,
    Platform: "telegram",
}
```

### `internal/chatadapter/session_bridge.go`

- `HandleMessage` extracts Principal from event and stores it in context via `identity.WithPrincipal`
- Uses `principal.ConversationKey(evt.ChannelID)` as the channel sessions map key

### `internal/hooks/types.go`

Add to `Input`:
```go
Principal identity.Principal // Unified identity for policy decisions
```

### `internal/engine/llmchat_engine.go`

Replace `HookContext` with `identity.Principal` or embed it:
```go
type HookContext struct {
    identity.Principal
}
```

Wire Principal into `hooks.Input` when dispatching PreToolUse/PostToolUse.

### `internal/companion/service.go`

- Accept Principal in `Chat()` method (via context or explicit parameter)
- Use tenant-scoped conversation ID for memory lookups

### `internal/web/server.go`

- Teams adapter path: extract TenantID from Teams config/JWT and propagate
- Web/API path: extract from session/JWT (placeholder for Phase 2 HTTP auth)

## Migration Strategy

1. Add `identity` package (no breaking changes)
2. Add `Principal` field to event types (additive, existing code ignores it)
3. Update adapters to populate Principal (one adapter at a time)
4. Update SessionBridge to use `ConversationKey()` for map keys
5. Wire Principal into hooks.Input
6. Update companion.Service to use tenant-scoped IDs

Each step is independently deployable. Existing single-tenant deployments see `TenantID=""` which produces keys like `teams::conv123` — functionally equivalent to current behavior.

## Conversation Key Migration

Existing in-memory maps (sync.Map) are ephemeral and don't need migration. For persistent data:

- `companion_turns.conversation_id` — existing data keeps old format
- New turns get tenant-scoped keys
- Queries should match both formats during transition (OR clause)
- Add `tenant_id` column to companion tables for explicit scoping (future)

## Verification

1. `go build ./internal/domain/identity/...` — compiles
2. `go test ./internal/domain/identity/...` — unit tests pass
3. `go build ./internal/...` — full build passes
4. `go test ./internal/chatadapter/...` — adapter tests pass
5. Manual: Teams messages from two tenants produce distinct conversation keys
6. Manual: hooks.Input carries populated Principal
