# Telegram Chat Adapter Plan

> **Status:** Draft
> **Author:** Josh + Codex
> **Date:** 2026-02-09
> **Branch:** `feat/telegram-adapter`

## Problem Statement

agentctl has a working Discord chat adapter, including natural language chat via
console sessions with streaming edits. We want the same surface area for
Telegram so teams can use agentctl from Telegram DMs and group chats.

**Goal:** Implement a Telegram `ChatAdapter` that supports:
- Text-command routing (Telegram-style `/commands`) to agentctl skills/APIs via `chatadapter.Bridge`
- Inline keyboard interactions (buttons) for Phase 2 agent controls (stop/retry/details)
- Natural language chat in configured chats (or @mentions elsewhere) routed through `consolews` sessions via a `SessionBridge`
- Streaming responses by editing a single Telegram message on an interval

Non-goals for the first cut:
- Full “thread-per-agent” parity with Discord threads (Telegram has “topics” but they are optional)
- Rich formatting beyond basic text + code blocks

## Current Code We Reuse

Telegram should reuse the existing abstractions and helpers:
- `internal/chatadapter/adapter.go` (interface + `CommandEvent`/`MessageEvent`/`InteractionEvent`)
- `internal/chatadapter/bridge.go` (slash-style commands -> skills/APIs)
- `internal/web/consolews/*` (session + streaming events)

Reference implementation patterns:
- `internal/chatadapter/discord/driver.go` (event filtering + concurrency limits)
- `internal/chatadapter/discord/messaging.go` (session bridge + streaming edits)

## Telegram Capabilities (What Changes vs Discord)

| Capability | Telegram Notes |
|---|---|
| Transport | Bot API over HTTPS; choose long polling by default (local dev friendly) and optionally support webhooks later |
| Commands | Bot commands exist, but there is no structured option schema; parse the message text |
| Interactions | Inline keyboards -> callback queries (callback data is limited; keep IDs short) |
| “Threads” | Replies exist everywhere; “topics” exist only in forum supergroups (`message_thread_id`). Note: `go-telegram-bot-api/v5` doesn't expose topic/thread fields, so the MVP keys sessions by chat ID only. |
| Typing | `sendChatAction("typing")` is best-effort |
| Message limits | Text limit is 4096 chars; captions are smaller; plan for truncation and optional file attachments |
| Streaming | Edit message text (`editMessageText`) with backoff on 429s; throttle edits (e.g. 1.5s) |
| Privacy mode | In groups, bots won’t receive all messages unless BotFather “Privacy Mode” is disabled; @mentions + commands still arrive |

## Event Flow

### 1) Command Messages (`/search`, `/todo`, etc.)

Telegram Update (message) ->
1. Ignore messages from bots / non-text messages.
2. If message is a command:
   - Parse `command` and `args` (rest of message).
   - Build `chatadapter.CommandEvent{Command, Options, User, ChannelID, GuildID=""}`.
     - `Options` is best-effort (e.g. `/search <query>` => `{"query": "<query>"}`).
3. Dispatch to `adapter.OnCommand` handler (wired to `chatadapter.Bridge.HandleCommand`).
4. Send a reply message (Telegram has no true ephemeral; reply in chat, or DM for “private” responses).

### 2) Natural Language Chat

Telegram Update (message) ->
1. Determine if we should respond:
   - Private chat: respond to all messages.
   - Group chat: respond to all messages only if chat ID is configured in `TELEGRAM_CHAT_IDS`.
   - Otherwise: respond only if bot is mentioned (`@botusername`) or message is a reply to the bot.
2. Create `chatadapter.MessageEvent` with `Respond` and `Edit` callbacks.
3. Dispatch to `adapter.OnMessage` handler (wired to `telegram.SessionBridge.HandleMessage`).
4. `SessionBridge` maps `chat_id` to a `consolews.Session` (chat-scoped sessions for MVP) and streams:
   - Send initial “Thinking…” message
   - Subscribe to session payloads
   - Periodically edit the message as partials arrive
   - Final edit on reply

### 3) Inline Keyboard Buttons (Phase 2)

Telegram callback query ->
1. Parse `callback_data` in an `action:agentID` convention (e.g. `stop:01J...`) where `agentID` matches the web API `/api/agents/{id}` routes.
2. Build `chatadapter.InteractionEvent{Type:"button", CustomID, User, ChannelID, MessageRef}`
3. Dispatch to `adapter.OnInteraction` handler (reuse the existing Discord logic patterns: stop/retry/details via daemon API).

Additionally (Telegram-only UX improvement):
- In the agent chat (`TELEGRAM_AGENT_CHAT_ID`), users can reply to an agent's root message to send a message to that agent via `POST /api/agents/{id}/ask`.
- This is a lightweight stand-in for Discord threads: Telegram replies become the “thread”.

## Configuration

Add `TelegramSettings` to `internal/platform/config/config.go` (mirrors the existing `DiscordSettings`):

Required:
- `TELEGRAM_BOT_TOKEN`

Chat behavior:
- `TELEGRAM_CHAT_IDS=<chat-id>[,<chat-id>...]`
  - In these chats: respond to all messages (requires Privacy Mode disabled for group chats).
- `TELEGRAM_CHAT_PROFILE=explorer` (default)
- `TELEGRAM_CHAT_SYSTEM_PROMPT="..."` (optional override)

Optional routing for agent lifecycle/event feed (Phase 2+):
- `TELEGRAM_ACTIVITY_CHAT_ID=<chat-id>`
- `TELEGRAM_AGENT_CHAT_ID=<chat-id>` (forum supergroup recommended if we want topics)

Operational knobs:
- `TELEGRAM_MAX_CONCURRENT_MESSAGES=10` (semaphore cap, matches Discord hardening)
- `TELEGRAM_EDIT_INTERVAL_MS=1500` (stream edit cadence)

## Implementation Plan

### Phase 1: Telegram Adapter (MVP)

New package layout (mirrors Discord):
```
internal/chatadapter/telegram/
  driver.go        # long polling loop + update routing (commands/messages/callbacks)
  commands.go      # optional: setMyCommands, parsing helpers
  interactions.go  # callback queries -> InteractionEvent
  messaging.go     # SessionBridge + streaming edits (Telegram 4096-char limit)
  messaging_test.go
```

Key behaviors:
1. Long polling loop reads updates and hands off to handlers.
2. Commands are parsed from message text and mapped to the existing `chatadapter.Bridge`.
3. Natural language chat reuses the SessionBridge design from Discord, but:
   - uses Telegram message length limit (4096)
   - uses `editMessageText` and handles `retry_after` (429) by backing off
   - keys sessions by `chat_id` (topic/thread IDs are not available via `go-telegram-bot-api/v5`)

### Phase 2: Agent Lifecycle + Buttons

If we want parity with Discord’s agent threads:
- Option A (simple): post all `agent.*` events into `TELEGRAM_ACTIVITY_CHAT_ID` with stop/details buttons
- Option B (better UX): if `TELEGRAM_AGENT_CHAT_ID` is a forum supergroup, create a topic per agent session and post updates into that topic (`message_thread_id`)
  - Note: this likely requires switching to raw HTTP calls (or a library that exposes forum topic fields), since `go-telegram-bot-api/v5` currently does not.

### Phase 3: Rich Output + Large Payload Handling

- Prefer short `data.summary` content in chat.
- If output is large (CAS artifact): post a short summary and include the digest with the retrieval command.
- Optionally attach a `.txt` file for mid-sized results (Telegram documents).

## Risks / Gotchas

| Risk | Severity | Mitigation |
|---|---|---|
| Telegram group Privacy Mode blocks “respond to all messages” | Medium | Document BotFather setting; always support @mentions + commands; treat private chats as always-on |
| Callback data length limit | Medium | Keep callback payloads short; store server-side mapping if needed |
| Rate limits on edits | Medium | Edit interval + backoff on 429 `retry_after` |
| Forum topics / per-topic session mapping | Low | MVP keys sessions by chat ID; reply chains keep messages in-topic; revisit with raw HTTP or a different Telegram library to capture `message_thread_id` |
| No true ephemeral messages | Low | Reply in-thread (via `reply_to_message_id`) or DM for sensitive output |

## Test Plan

Unit:
- mention detection + mention cleaning
- command parsing for MVP commands (`/search`, `/todo`, `/memory`, `/logs`)
- truncation to 4096 (including suffix handling)
- callback parsing for `action:agentID`
- `/chat_id` helper response

Integration (manual):
1. `agentctl web serve --chat telegram` with `TELEGRAM_BOT_TOKEN` set.
2. In any target group/DM, run `/chat_id` to get the numeric chat ID for `TELEGRAM_CHAT_IDS`, `TELEGRAM_ACTIVITY_CHAT_ID`, and `TELEGRAM_AGENT_CHAT_ID`.
3. DM the bot: send a natural language prompt -> gets streamed edits -> final answer.
4. Group chat:
   - with chat ID in `TELEGRAM_CHAT_IDS`: any message triggers response (requires Privacy Mode off)
   - without allowlist: `@botusername` mention triggers response
5. Commands: `/search <query>` returns formatted results.
6. Buttons: stop/retry/details callbacks roundtrip and call daemon endpoints.
7. Agent replies: in the agent chat (`TELEGRAM_AGENT_CHAT_ID`), reply to an agent root message to send an `ask` to that agent and receive the reply.
