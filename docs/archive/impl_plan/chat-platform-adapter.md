# Chat Platform Adapter Layer Design

> **Type:** Implementation plan (historical + execution notes)
> **Current architecture status:** Core adapter layer is implemented for Discord, Telegram, and Teams in `internal/interfaces/chatadapter/*` and `internal/web/server.go` (checkout `93bcbb3b`).
> **Remaining work:** Slack/other platform expansion and additional command coverage as backlog items.
> **Author:** Claude + Josh
> **Date:** 2026-02-09
> **Branch:** `feat/ui-updates`

For current runtime architecture, use [`docs/architecture/chat-platform-adapter.md`](../../architecture/chat-platform-adapter.md).

## Problem Statement

agentctl currently uses a custom GUI (`packages/gui-agent`) and internal primitives (mailbox, blackboard, SSE) for agent coordination. This works well for developers running locally, but doesn't scale to team-wide adoption or enterprise use cases where people already live inside Discord, Telegram, Slack, or Teams.

**Goal:** Build a generic `ChatAdapter` interface that enables agentctl's full agent coordination capabilities through any chat platform, starting with Discord, then Telegram, then Teams, then Slack.

## Architecture

```
Chat Platform (Discord / Telegram / Teams / Slack)
    |
    v
[Platform Driver]  <-- platform-specific Go code
    |                   (discordgo, Telegram Bot API, REST API, slack-go)
    v
[ChatAdapter Interface]  <-- generic abstraction
    |
    v
[agentctl Engine]
    |-- Skill Runner (130 skills, JSON I/O)
    |-- Mailbox (8 message types, priority/deadline)
    |-- Blackboard (shared state, TTL, leases)
    |-- Agent Daemon (spawn/start/kill/trash)
    |-- Companion Chat (personality, compression)
    |-- Console Sessions (WebSocket chat)
    |-- SSE Hub (invalidation events)
    +-- Memory / CAS / Storage
```

The adapter sits between the platform and agentctl's existing internals. Each platform driver translates platform-native events into adapter calls and formats adapter responses back into platform-native rich messages.

## Platform Capability Matrix

| Capability | Discord | Telegram | Teams | Slack |
|---|---|---|---|---|
| **Go Library** | `discordgo` (5.8k stars, mature) | `go-telegram-bot-api` (community) or direct HTTP | No official SDK; `infracloudio/msbotbuilder-go` or REST | `slack-go/slack` (community) |
| **Rich Messages** | Embeds (25 fields, 6KB, 10/msg) | MarkdownV2/HTML formatting + inline keyboards | Adaptive Cards (28KB, open standard) | Block Kit (100 blocks/view) |
| **Slash Commands** | Structured params, autocomplete (25 choices) | Bot commands (`/cmd args`, manual parsing) | Message extensions with structured params | Single text param, manual parsing |
| **Interactive Components** | Buttons (5x5), select (25 opts), modals (5 inputs) | Inline keyboard buttons (callback queries) | Adaptive Card actions (Submit, OpenUrl, ShowCard) | Buttons, selects, date pickers, modals (100 blocks) |
| **Threading** | First-class thread objects (1000 active/guild) | Replies everywhere; topics in forum supergroups (optional) | `replyToId` on activities | `thread_ts` timestamp-based |
| **Proactive Messages** | Direct via bot token | Direct via bot token | Stored conversation reference + OAuth | Bot token + channel ID |
| **Rate Limits** | 50 req/sec global, 5/2s per webhook | Per-bot limits; 429 includes `retry_after` | ~30 msgs/min per conversation | Tier-based; **1 req/min for non-Marketplace apps (2026)** |
| **File Uploads** | 25MB free, 500MB Nitro | Documents/photos supported; size limits apply | 20MB via attachment | Varies by plan |
| **Presence/Status** | Online/Idle/DND + activity types | `sendChatAction` (typing) only | Limited bot presence | Bot status via API |
| **Persistent UI** | Channel pins, thread archives | None | Tabs (full web apps) | App Home (Block Kit, per-user) |
| **Cross-Org** | Multi-server by default | N/A | External/Guest Access | Slack Connect (shared channels) |
| **Enterprise Deployment** | Self-managed | N/A | Microsoft 365 admin + Azure AD | Enterprise Grid (org-wide tokens) |
| **Compliance** | None built-in | None built-in | DLP, eDiscovery, retention, audit | Enterprise Grid compliance |
| **Workflow Automation** | None | None | Power Automate | Workflow Builder + custom steps |
| **Dev Mode** | Gateway WebSocket | Long polling (no public endpoint); webhook optional | Requires public endpoint (or ngrok) | Socket Mode (no public endpoint) |

## Proposed Go Interface

```go
package chatadapter

import "context"

// ChatAdapter is the core abstraction for chat platform integration.
// Each platform (Discord, Teams, Slack) implements this interface.
type ChatAdapter interface {
    // Lifecycle
    Connect(ctx context.Context) error
    Disconnect(ctx context.Context) error
    Name() string // "discord", "teams", "slack"

    // Commands
    RegisterCommands(ctx context.Context, cmds []CommandDef) error

    // Messaging
    SendMessage(ctx context.Context, channel ChannelRef, msg Message) (MessageRef, error)
    EditMessage(ctx context.Context, ref MessageRef, msg Message) error
    DeleteMessage(ctx context.Context, ref MessageRef) error

    // Threading
    CreateThread(ctx context.Context, parent MessageRef, name string) (ChannelRef, error)
    ReplyInThread(ctx context.Context, thread ChannelRef, msg Message) (MessageRef, error)

    // Interactive
    SendCard(ctx context.Context, channel ChannelRef, card Card) (MessageRef, error)
    UpdateCard(ctx context.Context, ref MessageRef, card Card) error

    // Presence
    SetPresence(ctx context.Context, status PresenceStatus, activity string) error

    // Events (the adapter pushes events to handlers)
    OnCommand(handler CommandHandler)
    OnMessage(handler MessageHandler)
    OnInteraction(handler InteractionHandler)
}

// ChannelRef is a platform-agnostic channel/conversation identifier.
type ChannelRef struct {
    Platform  string // "discord", "teams", "slack"
    ID        string // platform-native ID
    ThreadID  string // optional thread within channel
    Workspace string // guild/tenant/workspace
}

// MessageRef identifies a specific message for editing/replying.
type MessageRef struct {
    Platform  string
    ChannelID string
    MessageID string
}

// PresenceStatus maps to platform-native status types.
type PresenceStatus int
const (
    StatusOnline PresenceStatus = iota
    StatusIdle
    StatusBusy
    StatusOffline
)

// CommandDef defines a slash command to register.
type CommandDef struct {
    Name        string
    Description string
    Options     []CommandOption
}

// CommandOption is a typed parameter for a slash command.
type CommandOption struct {
    Name        string
    Description string
    Type        OptionType // String, Integer, Boolean, User, Channel
    Required    bool
    Choices     []Choice   // static choices (max 25)
    Autocomplete bool      // dynamic suggestions
}

type OptionType int
const (
    OptionString OptionType = iota
    OptionInteger
    OptionBoolean
    OptionUser
    OptionChannel
)

type Choice struct {
    Name  string
    Value string
}

// Message is a platform-agnostic message payload.
type Message struct {
    Text       string
    Embeds     []Embed       // rich formatted sections
    Components []Component   // buttons, selects
    Files      []File        // attachments
}

// Embed is a rich message section (maps to Discord embed, Teams card section,
// Slack Block Kit section).
type Embed struct {
    Title       string
    Description string
    Color       int           // hex color
    Fields      []Field
    Footer      string
    Timestamp   string
}

type Field struct {
    Name   string
    Value  string
    Inline bool
}

// Card is a structured interactive form (maps to Discord modal,
// Teams Adaptive Card, Slack Block Kit modal).
type Card struct {
    Title    string
    Sections []CardSection
    Actions  []CardAction
}

type CardSection struct {
    Header string
    Fields []CardField
}

type CardField struct {
    ID          string
    Label       string
    Type        CardFieldType // Text, Select, Toggle, Date
    Placeholder string
    Required    bool
    Options     []Choice      // for Select type
    Default     string
}

type CardFieldType int
const (
    CardFieldText CardFieldType = iota
    CardFieldSelect
    CardFieldToggle
    CardFieldDate
)

type CardAction struct {
    ID    string
    Label string
    Style ActionStyle // Primary, Danger, Default
    Data  map[string]any
}

type ActionStyle int
const (
    ActionDefault ActionStyle = iota
    ActionPrimary
    ActionDanger
)

// File represents an attachment.
type File struct {
    Name     string
    MimeType string
    Reader   io.Reader
    Size     int64
}

// Handler types
type CommandHandler func(ctx context.Context, cmd CommandEvent) error
type MessageHandler func(ctx context.Context, msg MessageEvent) error
type InteractionHandler func(ctx context.Context, interaction InteractionEvent) error

// CommandEvent is received when a user invokes a slash command.
type CommandEvent struct {
    Command   string
    Options   map[string]any
    User      UserRef
    Channel   ChannelRef
    Respond   func(msg Message) error        // ephemeral response
    RespondPublic func(msg Message) error     // visible to channel
}

// MessageEvent is received when a user sends a message.
type MessageEvent struct {
    Content string
    User    UserRef
    Channel ChannelRef
    Ref     MessageRef
}

// InteractionEvent is received when a user interacts with a component.
type InteractionEvent struct {
    Type      string            // "button", "select", "modal_submit"
    CustomID  string
    Values    map[string]string // submitted form values
    User      UserRef
    Channel   ChannelRef
    MessageRef MessageRef       // the message containing the component
    Respond   func(msg Message) error
}

type UserRef struct {
    Platform    string
    ID          string
    DisplayName string
}
```

## Mapping agentctl Primitives to Chat Features

### 1. Mailbox (8 message types)

| Message Type | Chat Mapping |
|---|---|
| `agent.ask` | Thread reply with approval buttons (Approve/Reject/Modify) |
| `agent.reply` | Thread reply with result embed |
| `agent.cmd` | Slash command `/agent-cmd <action>` or button press |
| `agent.event` | Status embed update in agent's thread |
| `console.ask` | DM to user with question + input form |
| `console.reply` | DM reply with answer |
| `console.event` | Channel notification embed |
| `console.cmd` | Slash command mapped to skill |

### 2. Blackboard (Shared State)

| Blackboard Feature | Chat Mapping |
|---|---|
| Post record | Pin message in shared channel |
| Query records | Slash command `/bb query <topic>` |
| Lease record | Button "Claim" on pinned message (updates embed) |
| Release lease | Button "Release" on claimed message |
| TTL expiry | Auto-archive thread after TTL |

### 3. Agent Lifecycle

| Agent State | Chat Mapping |
|---|---|
| `starting` | Thread created, embed shows "Starting..." with spinner emoji |
| `running` | Embed updated: green color, activity in bot presence |
| `stopped` | Embed updated: gray color, thread auto-archives |
| `error` | Embed updated: red color, error details, "Retry" button |

Control via slash commands:
```
/agent spawn --role researcher --prompt "Find all auth flows"
/agent list
/agent status <id>
/agent stop <id>
/agent kill <id>
```

### 4. Companion Chat

| Feature | Chat Mapping |
|---|---|
| Conversation | Dedicated thread per conversation |
| Personality | `/personality set <dimension> <value>` |
| Compression | Automatic (L1/L2), manual via `/compress` |
| Memory context | `/context set "today's focus"` |
| History | Thread history IS the conversation |

### 5. Console Sessions

| Feature | Chat Mapping |
|---|---|
| Create session | `/console new` creates thread |
| Send message | Type in thread |
| Receive response | Bot replies in thread (streaming via edit) |
| Tool calls | Collapsible embed showing tool name + result |

### 6. SSE Events

| Event | Chat Mapping |
|---|---|
| `invalidate` agents | Update agent status embeds in overview channel |
| `invalidate` jobs | Update job status embed |
| `heartbeat` | Bot presence shows "Online" |
| `agent_state_changed` | Thread notification + embed color change |

## Phase 1: Slash Commands (20 commands)

### Core Commands

| Command | Skill | Rich Output |
|---|---|---|
| `/search <query>` | `code/semantic_search` | Tree embed with file paths + scores |
| `/todo list` | `todo/manage {action:"list"}` | Task table with PageRank |
| `/todo add <title>` | `todo/manage {action:"add"}` | Confirmation embed |
| `/todo complete <id>` | `todo/manage {action:"complete"}` | Updated task embed |
| `/memory <query>` | `memory/query` | Memory list with types + scores |
| `/logs [--errors]` | `obs/logs` | Event table embed |

### Agent Commands

| Command | API | Rich Output |
|---|---|---|
| `/agent spawn` | `POST /api/agents/spawn` | Agent card with status + buttons |
| `/agent list` | `GET /api/agents` | Agent table embed |
| `/agent status <id>` | `GET /api/agents/{id}` | Detail embed with sessions |
| `/agent stop <id>` | `POST /api/agents/{id}/daemon/kill` | Confirmation + status update |

### Code Commands

| Command | Skill | Rich Output |
|---|---|---|
| `/counsel <query>` | `code/counsel` | Multi-perspective embed (security, perf, readability) |
| `/verify <claim>` | `verification/cove_verify` | Claims table + verification results |
| `/map <concept>` | `codemap/generate` | Relationship tree embed |
| `/ci-status <pr>` | `ci/checks` | Check status table |

### Utility Commands

| Command | Skill | Rich Output |
|---|---|---|
| `/web <query>` | `web/search` | Search results embed |
| `/skill-help <name>` | `skill/inspect` | Skill manifest embed |
| `/console new` | `POST /api/console/sessions` | New thread for interactive chat |
| `/chat <message>` | Companion chat | Reply in thread |
| `/compress` | `POST .../compress` | Compression stats embed |
| `/context <text>` | Memory context API | Confirmation |

## Implementation Plan

### Phase 1: Discord Adapter (Foundation)

**New packages:**
```
internal/interfaces/chatadapter/
    adapter.go          # ChatAdapter interface + types
    registry.go         # Adapter registry (name -> adapter)
    formatter.go        # Generic embed/card formatting helpers
    discord/
        driver.go       # discordgo-based adapter implementation
        commands.go     # Slash command registration + dispatch
        embeds.go       # Discord-specific embed formatting
        interactions.go # Button/select/modal handlers
    bridge.go           # agentctl skill/API -> adapter message bridge
```

**Dependencies:**
```
github.com/bwmarrin/discordgo  # Discord Go library (5.8k stars)
```

**Work items:**
1. Define `ChatAdapter` interface and types
2. Implement Discord driver (connect, register commands, send/edit messages)
3. Build skill bridge (invoke skill -> format output -> send via adapter)
4. Implement 6 core slash commands (search, todo, memory, logs, agent spawn/list)
5. Implement agent lifecycle embeds (status cards with buttons)
6. Implement threading (one thread per agent task / console session)
7. Wire SSE hub -> adapter presence updates

**Configuration:**
```yaml
# ~/.agentctl/chat-adapters.yaml
discord:
  token: "${DISCORD_BOT_TOKEN}"
  guild_id: "123456789"           # for dev; omit for global commands
  channels:
    agents: "agent-overview"      # agent status channel
    logs: "system-logs"           # log output channel
    general: "general"            # default command channel
```

### Phase 1b: Telegram Adapter (Low Friction)

Telegram is a good early second platform:
- Simple HTTP integration (Bot API) and long polling works locally without a public endpoint
- Inline keyboard buttons cover the core “agent control” interactions (stop/retry/details)
- Streaming can be done via `editMessageText` with a safe edit interval and 429 backoff

**New packages:**
```
internal/interfaces/chatadapter/
    telegram/
        driver.go        # long polling loop + update routing
        messaging.go     # SessionBridge + streaming edits (4096-char limit)
        interactions.go  # callback query handlers
        commands.go      # command parsing + optional Bot API command registration
```

**Configuration (env vars, for first cut):**
```
TELEGRAM_BOT_TOKEN=...
TELEGRAM_CHAT_IDS=<chat-id>,<chat-id>
TELEGRAM_CHAT_PROFILE=explorer
TELEGRAM_CHAT_SYSTEM_PROMPT="..."
```

### Phase 2: Teams Adapter (Enterprise)

**New packages:**
```
internal/interfaces/chatadapter/
    teams/
        driver.go       # Bot Framework REST API client
        auth.go         # Azure AD OAuth2 token management
        cards.go        # Adaptive Card builder
        proactive.go    # Proactive messaging (stored conversation refs)
```

**Dependencies:**
- Direct HTTP to Bot Framework Connector API (no Go SDK dependency)
- Azure AD app registration for authentication
- Optional: `infracloudio/msbotbuilder-go` for faster development

**Additional features:**
- Adaptive Cards for rich interactive forms (agent spawn, config)
- Graph API integration (Planner sync, Calendar awareness)
- Tab embedding for full GUI access
- Compliance-aware message handling (respect DLP, retention)

### Phase 3: Slack Adapter (Selective)

**New packages:**
```
internal/interfaces/chatadapter/
    slack/
        driver.go       # slack-go based adapter
        blocks.go       # Block Kit formatting
        socket.go       # Socket Mode for development
        apphome.go      # App Home dashboard
```

**Dependencies:**
```
github.com/slack-go/slack  # Community Go library
```

**Critical constraint:** Non-Marketplace apps face 1 req/min rate limit on `conversations.history` and `conversations.replies` starting March 2026. Must either:
- Submit to Slack Marketplace (requires review process)
- Design around the rate limit (batch reads, cache aggressively)
- Treat Slack as write-mostly (post results, don't read history)

**Additional features:**
- App Home for persistent agent dashboard
- Workflow Builder custom steps
- Socket Mode for local development
- Block Kit modals for complex forms

### Phase 4: Unified CLI

```bash
# Start chat adapter alongside web server
agentctl web serve --chat discord
agentctl web serve --chat telegram
agentctl web serve --chat teams
agentctl web serve --chat discord,telegram,teams  # multiple adapters

# Standalone adapter mode (no web GUI)
agentctl chat connect discord
agentctl chat connect telegram
agentctl chat connect teams --tenant-id <id>
```

## Critical Findings

### Slack 2026 Rate Limit Change

**Effective March 3, 2026:** All existing non-Marketplace Slack apps will be restricted to **1 request per minute** for `conversations.history` and `conversations.replies`. This makes Slack impractical for real-time agent coordination without Marketplace submission.

**Impact:** Slack should be Phase 3 and designed as write-mostly (push results to channels) rather than read-heavy (poll conversation history).

### Discord is the Best Starting Point

- Mature Go library (`discordgo`, 5.8k stars)
- Generous rate limits (50 req/sec global)
- First-class threads (1000 active/guild, unlimited archived)
- Structured slash commands with autocomplete
- No Marketplace requirement for production use
- Developer-friendly (most agentctl users are developers)

### Teams is the Enterprise Play

- Adaptive Cards are an open cross-platform standard
- Graph API unlocks Calendar, Planner, SharePoint, ADO integration
- Compliance features (DLP, eDiscovery, retention) matter for regulated industries
- No official Go SDK is a friction point but REST API is well-documented
- Proactive messaging requires stored conversation references

## Risk Assessment

| Risk | Severity | Mitigation |
|---|---|---|
| Discord API breaking changes | Medium | Pin discordgo version, monitor changelog |
| Slack rate limits block usage | High | Phase 3, write-mostly design, Marketplace path |
| Teams auth complexity (Azure AD) | Medium | Clear setup guide, reusable token manager |
| Adapter interface too generic | Medium | Start with Discord, refine interface as Teams/Slack added |
| Thread limits (1000 active/guild) | Low | Auto-archive completed tasks, multiple guilds for scale |
| Message size limits | Low | Paginate large outputs, use file attachments for code |

## Open Questions

1. **Should the adapter run in-process with `agentctl web serve` or as a separate binary?**
   - In-process is simpler but couples lifecycle
   - Separate binary allows independent scaling

2. **How should agent-to-agent messages appear?**
   - Option A: Hidden (only show results to humans)
   - Option B: Dedicated "agent-coordination" channel with all inter-agent traffic
   - Option C: Thread-per-workflow showing agent collaboration

3. **Should the adapter replace the GUI or complement it?**
   - Some features (code editing, complex visualization) may always need the GUI
   - Chat adapter could be the primary interface with GUI as "advanced mode"

4. **Multi-workspace support?**
   - One adapter instance per workspace, or one adapter serving multiple workspaces?
   - Discord: one guild = one workspace (natural mapping)
   - Teams: one tenant may have multiple workspaces

5. **Authentication mapping?**
   - How to map Discord/Teams/Slack users to agentctl identities?
   - Simple: config file mapping user IDs
   - Advanced: OAuth flow linking accounts
