# Message Passing System - Implementation Summary

## What Was Done

foxctl already had a robust message passing system in place! I've made it significantly easier to use through:

### 1. Documentation

**New Guides:**
- `docs/general/message-passing.md` - Comprehensive guide (10+ KB)
  - Architecture overview
  - Message types and structure
  - Usage patterns (request/response, commands, events)
  - Session lineage and workspace tracking
  - Error handling and retries
  - Monitoring and troubleshooting
  - Best practices

- `docs/general/message-passing-quickstart.md` - Quick reference
  - 30-second examples
  - Essential concepts
  - Common patterns
  - Quick lookup table

- `docs/designs/message-passing-examples.md` - Practical examples
  - Request/response between agents
  - Fire-and-forget commands
  - Event broadcasting
  - Multi-agent orchestration
  - Session tracing
  - Testing message handlers

### 2. Enhanced API

**New Builder API** (`internal/domain/agent/builder.go`):
```go
// Fluent, type-safe message construction
msg := agent.NewAgentAsk().
    FromNS("agent:planner").
    ToNS("agent:coder").
    Question("Implement X").
    WithSessionID(sessionID).
    WithWorkspace(workspace).
    WithTTL(10 * time.Minute).
    MustBuild()
```

Supported builders:
- `NewAgentAsk()` - Request messages
- `NewAgentReply()` - Response messages
- `NewAgentCmd()` - Command messages
- `NewAgentEvent()` - Event messages
- `NewConsoleAsk()` - Console input
- `NewConsoleReply()` - Console output
- `NewConsoleEvent()` - Console streaming
- `ReplyTo(original)` - Reply helper (preserves context)

**Convenience Helpers** (`internal/domain/agent/helpers.go`):
```go
// Sender - easy message sending
sender := agent.NewSender(store, "agent:planner")
askID, err := sender.SendAsk(ctx, "agent:coder", "Implement X",
    agent.WithAskSession(sessionID),
    agent.WithAskWorkspace(workspace),
)

// Receiver - easy message receiving
receiver := agent.NewReceiver(store, "agent:coder")
msg, err := receiver.PollOnce(ctx, 30*time.Second)
receiver.Ack(ctx, msg)  // or Nack(ctx, msg, delay)
```

**Message Methods**:
- `msg.AskID()` - Extract ask ID
- `msg.CmdID()` - Extract command ID  
- `msg.EventID()` - Extract event ID
- `msg.UnmarshalPayload(&data)` - Parse envelope

### 3. Tests

**New Test Files**:
- `builder_test.go` - Builder API tests (all message types)
- `helpers_test.go` - Sender/Receiver tests
- `message_integration_test.go` - Integration tests:
  - Request/response pattern
  - Fire-and-forget commands
  - Event broadcasting
  - Session lineage tracking
  - Message retry with Nack
  - Workspace filtering

### 4. Existing Architecture (What Was Already There)

The message passing system was already fully implemented:

**Domain Layer** (`internal/domain/agent/mailbox.go`):
- Message types (Ask, Reply, Cmd, Event, Console*)
- Payload structures
- Envelope format

**Storage Layer** (`internal/storage/mailbox/store.go`):
- SQLite-backed persistence
- Lease-based message delivery
- Transaction safety
- Session lineage tracking
- Workspace filtering

**Actor Layer** (`internal/runtime/actor/`):
- `base_actor.go` - Message handler registration
- `mailbox_adapter.go` - Storage ↔ Actor bridge
- `event_bus.go` - Event pub/sub
- `supervisor.go` - Lifecycle management

## Key Features

1. **Persistent Messages** - Stored in SQLite at `~/.foxctl/mailbox.db`
2. **Lease-Based Delivery** - Prevents duplicate processing
3. **Session Lineage** - Trace conversations across agents
4. **Workspace Context** - Messages tied to workspace paths
5. **Retry Support** - Automatic retries with configurable delays
6. **Correlation Tracking** - Headers for request/response matching
7. **TTL Support** - Messages expire after time-to-live

## Usage Example

**Before** (manual construction):
```go
msg := agent.Message{
    ID: ulid.Make().String(),
    FromNS: "agent:a",
    ToNS: "agent:b",
    Type: agent.MessageTypeAsk,
    TTLMS: int64(5 * time.Minute),
    // ... more fields
}

// Build envelope manually
envelope := map[string]any{
    "version": 1,
    "status": "ok",
    "command": "agent.ask",
    "data": agent.AskData{Message: question},
    // ... more fields
}
payload, _ := json.Marshal(envelope)
msg.Payload = payload

store.Send(ctx, msg)
```

**After** (with builder):
```go
msg := agent.NewAgentAsk().
    FromNS("agent:a").
    ToNS("agent:b").
    Question(question).
    MustBuild()

store.Send(ctx, msg)
```

**Or** (with Sender helper):
```go
sender := agent.NewSender(store, "agent:a")
askID, _ := sender.SendAsk(ctx, "agent:b", question)
```

## Quick Start

1. **Open mailbox store**:
   ```go
   store, _ := mailbox.Open(ctx, root)
   ```

2. **Create sender/receiver**:
   ```go
   sender := agent.NewSender(store, "agent:a")
   receiver := agent.NewReceiver(store, "agent:b")
   ```

3. **Send/Receive**:
   ```go
   // Send
   askID, _ := sender.SendAsk(ctx, "agent:b", "What is X?")
   
   // Receive
   msg, _ := receiver.PollOnce(ctx, 30*time.Second)
   var data agent.AskData
   msg.UnmarshalPayload(&data)
   
   // Reply
   sender.SendReply(ctx, msg, map[string]any{"result": 42})
   
   // Ack
   receiver.Ack(ctx, msg)
   ```

## Running Tests

```bash
# Test builder API
go test ./internal/domain/agent -run TestAgentAskBuilder -v

# Test helpers
go test ./internal/domain/agent -run TestSender_SendAsk -v

# Integration tests
go test ./internal/domain/agent -run TestMessagePassingIntegration -v

# All domain/agent tests
go test ./internal/domain/agent -v
```

## Documentation Links

| Document | Path | Purpose |
|----------|------|---------|
| Quick Start | `docs/general/message-passing-quickstart.md` | Get started in 5 minutes |
| Full Guide | `docs/general/message-passing.md` | Complete reference |
| Examples | `docs/designs/message-passing-examples.md` | Real-world patterns |
| Domain Types | `internal/domain/agent/mailbox.go` | Message structures |
| Builder API | `internal/domain/agent/builder.go` | Fluent interface |
| Helpers | `internal/domain/agent/helpers.go` | Sender/Receiver |

## Files Changed

**New Files:**
- `docs/general/message-passing.md`
- `docs/general/message-passing-quickstart.md`
- `docs/designs/message-passing-examples.md`
- `docs/general/MESSAGE_PASSING_SUMMARY.md` (this file)
- `internal/domain/agent/builder.go`
- `internal/domain/agent/builder_test.go`
- `internal/domain/agent/helpers.go`
- `internal/domain/agent/helpers_test.go`
- `internal/domain/agent/message_integration_test.go`

**No Breaking Changes** - This is purely additive functionality!