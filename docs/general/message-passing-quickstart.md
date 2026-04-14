# Message Passing Quickstart

Get started with agent-to-agent communication in 5 minutes.

## What You Need

```go
import (
    "github.com/joshka0/foxctl/internal/domain/agent"
    "github.com/joshka0/foxctl/internal/storage/mailbox"
)
```

## Essential Concepts

**Three layers:**
1. **Domain** (`internal/domain/agent`) - Message types and builders
2. **Storage** (`internal/storage/mailbox`) - SQLite persistence  
3. **Actor** (`internal/runtime/actor`) - Event-driven processing

**Four message types:**
- `agent.ask` - Request/Response
- `agent.reply` - Response to ask
- `agent.cmd` - Fire-and-forget command
- `agent.event` - Broadcast notification

## 30-Second Examples

### Send a Request (Ask)

```go
// Create sender
sender := agent.NewSender(store, "agent:planner")

askID, err := sender.SendAsk(ctx, "agent:coder", 
    "Implement this feature",
    agent.WithAskSession(sessionID),
    agent.WithAskWorkspace(workspacePath),
)
```

### Send a Reply

```go
sender := agent.NewSender(store, "agent:coder")

err := sender.SendReply(ctx, originalMessage, map[string]any{
    "status": "implemented",
    "files":  []string{"main.go"},
})
```

### Send a Command

```go
sender := agent.NewSender(store, "agent:planner")

cmdID, err := sender.SendCmd(ctx, "agent:builder", "build",
    map[string]any{"target": "all"},
    agent.WithCmdSession(sessionID),
)
```

### Send an Event

```go
sender := agent.NewSender(store, "agent:monitor")

err := sender.SendEvent(ctx, "build_complete",
    map[string]any{"success": true, "duration_ms": 1500},
    agent.WithEventSession(sessionID),
)
```

### Receive Messages

```go
// Create receiver
receiver := agent.NewReceiver(store, "agent:coder")

// Poll for messages
msg, err := receiver.PollOnce(ctx, 30*time.Second)
if err != nil {
    return err
}

// Process message
switch msg.Type {
case agent.MessageTypeAsk:
    var data agent.AskData
    msg.UnmarshalPayload(&data)
    // Handle the ask
    reply := buildReply(data.AskID, processQuestion(data.Question))
    sender.SendReply(ctx, msg, reply)
    
case agent.MessageTypeCmd:
    var data agent.CmdData
    msg.UnmarshalPayload(&data)
    // Handle command
    execute(data.Action, data.Args)
    
case agent.MessageTypeEvent:
    var data agent.EventData
    msg.UnmarshalPayload(&data)
    // Handle event
    handleEvent(data.Kind, data.Custom)
}

// Acknowledge as processed
receiver.Ack(ctx, msg)
```

## Builder API (More Control)

```go
// Build complex messages with fluent API
msg := agent.NewAgentAsk().
    FromNS("agent:planner").
    ToNS("agent:coder").
    Question("What is the status?").
    WithKind("question").
    WithNeedsBy(time.Now().Add(5*time.Minute)).
    WithContext(map[string]any{
        "priority": "high",
        "feature":  "auth",
    }).
    WithSessionID(sessionID).
    WithWorkspace(workspace).
    WithHeader("correlation", ulid.Make().String()).
    WithTTL(10*time.Minute).
    MustBuild()

// Send it
store.Send(ctx, msg)
```

## Common Patterns

### Request/Response Loop

```go
// Agent A: Send request
askID, _ := sender.SendAsk(ctx, "agent:b", "What is X?",
    agent.WithAskCorrelation(correlationID),
)

// Agent B: Receive and reply
msg, _ := receiver.PollOnce(ctx, 30*time.Second)
var data agent.AskData
msg.UnmarshalPayload(&data)
answer := process(data.Question)

sender.SendReply(ctx, msg, map[string]any{"result": answer})

// Agent A: Handle reply
reply, _ := myReceiver.PollOnce(ctx, 30*time.Second)
var replyData agent.ReplyData  
reply.UnmarshalPayload(&replyData)
result := replyData.Answer["result"]
```

### Broadcast Events

```go
// Publisher
sender.SendEvent(ctx, "deployment_complete", 
    map[string]any{"version": "v1.2.3"},
    agent.WithEventDestination("broadcast"),
)

// Any number of subscribers
msg, _ := receiver.PollOnce(ctx, 30*time.Second)
var data agent.EventData
msg.UnmarshalPayload(&data)
// data.Kind == "deployment_complete"
```

### Session Tracing

```go
// All messages in a workflow share sessionID
sessionID := ulid.Make().String()

// Send multiple messages with same session
sender.SendAsk(ctx, "agent:a", "Step 1", 
    agent.WithAskSession(sessionID),
)
sender.SendCmd(ctx, "agent:b", "action", 
    map[string]any{"arg": 1},
    agent.WithCmdSession(sessionID),
)

// Later: trace all messages in session
messages, _ := store.ListBySession(ctx, sessionID, 100)
// messages has the entire conversation
```

## Handle Errors & Retries

```go
msg, _ := receiver.PollOnce(ctx, 30*time.Second)

// Transient error - will retry
if isTransient(err) {
    return err  // Nack with default delay
}

// Permanent error - don't retry  
if isFatal(err) {
    _ = receiver.Ack(ctx, msg)
    return nil // Remove from queue
}

// Explicit retry with delay
if shouldRetry(err) {
    _ = receiver.Nack(ctx, msg, 30*time.Second)
    return nil
}
```

## Testing Messages

```go
// Build test message
msg := agent.NewAgentAsk().
    FromNS("test").
    ToNS("test").
    Question("Test?").
    MustBuild()

// Parse payload
var data agent.AskData
msg.UnmarshalPayload(&data)
assert.Equal(t, "Test?", data.Question)

// Extract IDs
assert.NotEmpty(t, msg.AskID())
```

## Next Steps

- **Full Guide**: See [message-passing.md](./message-passing.md)
- **Examples**: See [message-passing-examples.md](../designs/message-passing-examples.md)
- **API Docs**: See `internal/domain/agent/mailbox.go` (domain types)
- **Storage**: See `internal/storage/mailbox/store.go` (persistence)

## Checklist Before Production

- [ ] Set appropriate TTL (default 5 min, longer for slow ops)
- [ ] Always preserve session lineage
- [ ] Use correlation IDs for tracking
- [ ] Handle errors with retry logic
- [ ] Ack messages when done
- [ ] Test message handling

## Quick Lookup

| Task | Method |
|------|--------|
| Send request | `sender.SendAsk(ctx, toNS, question, opts...)` |
| Send reply | `sender.SendReply(ctx, original, answer)` |
| Send command | `sender.SendCmd(ctx, toNS, action, args, opts...)` |
| Send event | `sender.SendEvent(ctx, kind, data, opts...)` |
| Receive message | `receiver.PollOnce(ctx, lease)` |
| Acknowledge | `receiver.Ack(ctx, msg)` |
| Retry message | `receiver.Nack(ctx, msg, delay)` |
| Build message | `agent.NewAgentAsk()...MustBuild()` |
| Parse payload | `msg.UnmarshalPayload(&target)` |