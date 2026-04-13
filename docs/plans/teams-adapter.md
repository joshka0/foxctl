# Microsoft Teams Chat Adapter (Enterprise MVP) + Generic SessionBridge Refactor

> **Status:** Draft
> **Author:** Josh + Codex
> **Date:** 2026-02-09
> **Branch:** `feat/teams-adapter-chat`

## Why This Exists

We already support natural-language chat (console sessions + streaming edits) via Discord and Telegram. Both adapters ship near-identical `SessionBridge` implementations. A Teams adapter without refactoring would introduce a third copy and make future changes (streaming cadence, truncation, cancellation) harder and riskier.

At the same time, Microsoft Teams is the default chat surface for enterprise teams, and enterprise deployments need: strict webhook auth, conservative default routing, and predictable behavior under rate limits.

## Goals (This Branch)

1. Extract a **generic** `SessionBridge` into `internal/interfaces/chatadapter` and refactor Discord/Telegram to use it (no behavior change).
2. Implement a **Teams adapter MVP** that supports:
   - Incoming Bot Framework webhooks (Activity schema)
   - Natural-language chat routed through `consolews` via the shared `SessionBridge`
   - Streaming responses by **editing a single message** on a fixed cadence
   - `/commands` parsing from message text (no “slash command registration” in Teams)
   - Strict inbound auth (JWT validation) and conservative “respond only when mentioned unless allowlisted” gating

## Non-Goals (MVP)

- Adaptive Cards / buttons / rich actions (Phase 3)
- Proactive messaging (agent activity feed into Teams) (Phase 3+)
- Multi-tenant support (single tenant only; enforced by config) (Phase 3+)
- Attachments/file uploads

## Phase 1: Generic SessionBridge Extraction (No Behavior Change)

### 1.1 `internal/interfaces/chatadapter/helpers.go`

Move duplicated helpers from Discord/Telegram into `internal/interfaces/chatadapter`:

```go
func TruncateRunes(s string, maxLen int) string
func TruncateRunesWithSuffix(s, suffix string, maxLen int) string
func IsPartial(meta map[string]any) bool
func GetDataString(data map[string]any, key string) string
func FormatDuration(ms int64) string
```

Notes:
- Keep these **small + deterministic**. This should not become a “misc utils” dump.

### 1.2 `internal/interfaces/chatadapter/session_bridge.go`

Define a platform-agnostic bridge that only depends on:
- `chatadapter.MessageEvent` (`Respond`, `Edit`)
- a typing interface (optional)
- `consolews.Hub` for session creation/subscription

```go
type TypingIndicator interface {
	ShowTyping(channelID string)
}

type SessionBridgeConfig struct {
	PlatformName     string // "discord" | "telegram" | "teams" (observability + metadata)
	MaxMessageLen    int    // Discord: 2000, Telegram: 4096, Teams: 4000 (configurable)
	EditIntervalMS   int    // Default 1500ms. Note: Teams edits are effectively ~1 req/sec per conversation; <1000ms will likely hit 429s.
	ChatProfile      string // Default "explorer"
	ChatSystemPrompt string // Optional override
}
```

Implementation requirements:
- No panics: safe type assertions for `sync.Map` values.
- Honor `EditIntervalMS` and `MaxMessageLen` for partial edits and final replies.
- Per-channel cancellation: cancel previous in-flight request when a new message arrives.
- Emit observability events using `PlatformName` (avoid hard-coding “discord”).

### 1.3 `internal/interfaces/chatadapter/session_bridge_test.go`

Consolidate and keep coverage for:
- “partial then final” edit behavior
- truncation (including suffix within limits)
- cancellation behavior (best-effort final edit)

### 1.4 Refactor Discord + Telegram SessionBridge Files

Make `internal/interfaces/chatadapter/discord/messaging.go` and `internal/interfaces/chatadapter/telegram/messaging.go` thin wrappers that only build `chatadapter.SessionBridgeConfig` and call `chatadapter.NewSessionBridge(...)`.

### 1.5 Refactor Embed/Event Helpers

Replace local helper copies in:
- `internal/interfaces/chatadapter/discord/embeds.go`
- `internal/interfaces/chatadapter/telegram/events.go`

to call `chatadapter.TruncateRunes`, `chatadapter.GetDataString`, `chatadapter.FormatDuration`, etc.

### 1.6 Checkpoint

```bash
make build
go test ./internal/interfaces/chatadapter/...
```

## Phase 2: Microsoft Teams Adapter (Enterprise MVP)

Teams is **webhook-based**. There is no gateway connection; inbound activities arrive over HTTPS and must be validated. Outbound messages go to the Bot Framework Connector API and require OAuth tokens.

### 2.1 Config: `internal/platform/config/config.go`

Add Teams settings to config with env overrides and safe defaults:

```go
type TeamsSettings struct {
	TenantID         string `mapstructure:"tenant_id" json:"tenant_id"`
	ClientID         string `mapstructure:"client_id" json:"client_id"`         // Microsoft App ID
	ClientSecret     string `mapstructure:"client_secret" json:"client_secret"` // redact in MarshalJSON

	// Chats where we respond to all messages. Outside this list: respond only when mentioned.
	ChatConversationIDs []string `mapstructure:"chat_conversation_ids" json:"chat_conversation_ids"`

	ChatProfile      string `mapstructure:"chat_profile" json:"chat_profile"`
	ChatSystemPrompt string `mapstructure:"chat_system_prompt" json:"chat_system_prompt"`

	MaxConcurrentMessages int  `mapstructure:"max_concurrent_messages" json:"max_concurrent_messages"`
	EditIntervalMS        int  `mapstructure:"edit_interval_ms" json:"edit_interval_ms"`
	SkipJWTVerify         bool `mapstructure:"skip_jwt_verify" json:"skip_jwt_verify"` // dev-only guard
}
```

Config wiring requirements:
- Add `Teams TeamsSettings` to the main `config.Config` struct alongside `Discord` and `Telegram`.
- Add `MarshalJSON` redaction for `ClientSecret`.
- Add a `finalizeConfig()` env override block for `TEAMS_*` env vars (mirrors Discord/Telegram’s pattern).

Env vars:
- `TEAMS_TENANT_ID`
- `TEAMS_CLIENT_ID`
- `TEAMS_CLIENT_SECRET`
- `TEAMS_CHAT_CONVERSATION_IDS` (comma-separated)
- `TEAMS_CHAT_PROFILE` (default `explorer`)
- `TEAMS_CHAT_SYSTEM_PROMPT`
- `TEAMS_MAX_CONCURRENT_MESSAGES` (default `10`)
- `TEAMS_EDIT_INTERVAL_MS` (default `1500`)
- `TEAMS_SKIP_JWT_VERIFY` (default `false`; only allow when `--dev-cors` is set; enforced at runtime in `startChatAdapter()`)

### 2.2 Types: `internal/interfaces/chatadapter/teams/types.go`

Define minimal Bot Framework Activity types needed for:
- message routing
- mention detection (entities)
- reply/edit

Minimum fields:
- `type`, `id`, `serviceUrl`, `channelId`, `text`, `replyToId`
- `from`, `recipient`
- `conversation` (id + tenant id)
- `entities` (for mentions)

### 2.3 Inbound Auth: `internal/interfaces/chatadapter/teams/jwt.go`

Enterprise requirement: verify inbound JWT for webhook calls.

Implementation shape:
- Fetch OpenID metadata + keys (with caching + refresh):
  - OpenID config: `https://login.botframework.com/v1/.well-known/openidconfiguration`
  - Use the JWKS URL from that doc; cache keys and refresh on key miss.
- Validate:
  - signature
  - issuer matches the OpenID config issuer
  - `aud` matches `ClientID` (Microsoft App ID)
  - v1 vs v2 app id claim:
    - if `ver == "1.0"`: require `appid == ClientID`
    - if `ver == "2.0"`: require `azp == ClientID` (accept `appid` as fallback if present)
  - issuer is expected for Bot Framework
  - tenant claim matches `TenantID` (single-tenant MVP)
- Provide a **dev escape hatch** behind explicit config (`SkipJWTVerify`) and ensure it cannot be accidentally enabled in prod.

Design note:
- Make JWT verification injectable/testable via a small interface so driver tests can use a fake verifier.

### 2.4 Outbound OAuth: `internal/interfaces/chatadapter/teams/auth.go`

Implement a token manager for Bot Framework Connector calls:
- Client credentials flow:
  - Token endpoint: `https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token`
  - Scope: `https://api.botframework.com/.default`
  - Form fields: `grant_type=client_credentials`, `client_id`, `client_secret`, `scope`
- Cache token, refresh ~5 minutes before expiry
- Thread-safe via mutex

### 2.5 Bot Connector Client: `internal/interfaces/chatadapter/teams/botclient.go`

Teams requires **reply-to-activity** to keep messages in the right thread.

Provide:
- `SendActivity(ctx, conversationID, activity)` (POST)
- `ReplyToActivity(ctx, conversationID, replyToActivityID, activity)` (POST)
- `UpdateActivity(ctx, conversationID, activityID, activity)` (PUT)
- `SendTyping(ctx, conversationID, replyToActivityID)` (optional but useful)

Also:
- Treat `serviceUrl` as untrusted input.
  - Parse as URL, require `https`, and validate host against an allowlist before storing/using it.
  - Reject/ignore activities with untrusted `serviceUrl` to prevent OAuth token exfiltration.
  - Default allowlist should include Teams/Bot Framework hosts (e.g. `smba.trafficmanager.net` and `*.botframework.com`); add config overrides later if we need GCC/DoD clouds.
- Normalize `serviceUrl` + store per conversation (map `conversationID -> serviceUrl`) only after validation.
- Always set outbound `type: "message"` (or `"typing"`)
- Keep `http.Client` timeout at 15s and handle 429/5xx with limited retries/backoff

### 2.6 Adapter Driver: `internal/interfaces/chatadapter/teams/driver.go`

Key behaviors:
- `Connect()` validates config and initializes dependencies (no long-lived connection).
- `RegisterCommands()` is a no-op for Teams (commands are message-text parsed).
- `ShowTyping(channelKey)` sends typing activity via BotClient (best-effort).
- `HTTPHandler()`:
  1. Verify JWT (unless dev override).
  2. Parse Activity JSON.
  3. Fast-ack `200 OK` and dispatch work in a goroutine with a semaphore cap.
  4. Route only `Activity.Type == "message"` in MVP.
  5. Build a `chatadapter.MessageEvent` or `chatadapter.CommandEvent`.

Routing rules (enterprise-safe defaults):
- Determine a stable `channelKey` for sessions: `conversation.id` (optionally prefixed by `tenant_id`).
- Respond to all messages only if `conversation.id` is in `ChatConversationIDs`.
- Otherwise respond only if:
  - the bot is mentioned (via entities), or
  - the conversation is 1:1 (MVP decision: **auto-respond in 1:1**; users explicitly opened a chat with the bot)

Command parsing:
- If `text` starts with `/`, treat as command: `/search foo`, `/todo list`, etc.
- Else treat as natural language and route through `SessionBridge`.

Respond/Edit implementation:
- `Respond("Thinking...")` should `ReplyToActivity(...)` and return a `MessageRef` (conversation + activity ID).
- `Edit(...)` should `UpdateActivity(...)` on the previously sent activity.
- If `UpdateActivity` fails, log and stop editing (MVP). Optional: send a new message and finish (Phase 3).

### 2.7 Wiring: `internal/interfaces/web/server.go` + routes

- Add `--chat teams` option and update help text exactly:
  - `cmd/agentctl/cmd/web.go`: `"Chat adapter to enable (discord|telegram|teams)"`
- In `startChatAdapter()` add `case "teams"`:
  - `adapter := teams.New(s.cfg.Teams, daemonURL)`
  - `adapter.SetSSEHub(s.sseHub)` (even if proactive messaging is Phase 3, keep parity with other adapters)
  - `adapter.OnCommand(bridge.HandleCommand)`
  - `adapter.OnInteraction(...)` (no-op in MVP)
  - `sessionBridge := chatadapter.NewSessionBridge(s.consoleHub, adapter, cfg...)`
  - `adapter.OnMessage(sessionBridge.HandleMessage)`
  - `adapter.Connect(ctx)` (validates config)
  - `s.chatAdapter = adapter`
- Enforce `SkipJWTVerify` at runtime:
  - If `cfg.Teams.SkipJWTVerify == true` and `opts.DevCORS == false`, refuse to start the Teams adapter (and emit a loud observability warning).
- Register webhook handler robustly:
  - In `Server.Handler()`, register `/api/teams/messages` unconditionally. Handler should:
    - If Teams adapter is inactive, return `503 Service Unavailable` (not 404).
    - Otherwise delegate to `teamsAdapter.HTTPHandler()`.

Early validation:
- In `web.NewServer(...)`, for `--chat teams`, fast-fail or skip adapter startup if any of:
  - `TenantID`, `ClientID`, `ClientSecret` are missing
  - `SkipJWTVerify` is set but dev mode is not enabled
  (mirror Discord/Telegram “missing token; skipping adapter” behavior, but keep `/api/teams/messages` route returning 503).

### 2.8 Tests

Unit:
- token caching/refresh logic (auth)
- jwt verifier behavior (claim checks + caching), via an injectable verifier interface

HTTP handler:
- rejects missing/invalid auth
- routes message activities
- mention gating + allowlist gating
- command parsing from `/cmd args`
- ensures fast-ack (does not block on LLM work)

### 2.9 Smoke Test (Manual)

1. Start server:
   ```bash
   TEAMS_TENANT_ID=... TEAMS_CLIENT_ID=... TEAMS_CLIENT_SECRET=... \
   agentctl web serve --chat teams
   ```
2. Expose HTTPS (dev): `ngrok http 8090`
3. Configure Azure Bot “Messaging endpoint” to:
   - `https://<ngrok>/api/teams/messages`
4. In Teams, send:
   - a normal message in an allowlisted conversation -> bot replies + streams edits
   - a message mentioning the bot in a non-allowlisted conversation -> bot responds
   - `/search foo` -> routes through `chatadapter.Bridge`

## Phase 3 (Future)

- Adaptive Cards (buttons) for stop/retry/details and richer agent status
- Proactive activity feed (requires conversation references + storage)
- Better edit fallback (new message when update fails)
- Multi-tenant support (with explicit allowlists and per-tenant creds)

## Disconnect Semantics (Explicit)

Teams has no long-lived socket connection, but we still need clean shutdown:
- `Disconnect()` should:
  - cancel the adapter’s root context (stops in-flight SessionBridge operations)
  - wait for any handler goroutines (bounded by their per-request timeout)
  - clear/stop any token refresh timers (if implemented) and drop in-memory `serviceUrl` cache
