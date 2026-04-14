# Message Passing Examples

This document provides practical examples of using foxctl's message passing system.

## Example 1: Request/Response Between Agents

Two agents communicating: `agent:planner` asks `agent:coder` to implement a feature.

### Agent: Planner (Sender)

```go
package planner

import (
    "context"
    "fmt"
    "time"

    "github.com/joshka0/foxctl/internal/domain/agent"
    "github.com/joshka0/foxctl/internal/domain/envelope"
    "github.com/oklog/ulid/v2"
)

// PlannerAgent sends requests to coder agents
type PlannerAgent struct {
    mailbox   MailboxStore // interface
    namespace string
}

// RequestImplementation asks the coder to implement a feature
func (p *PlannerAgent) RequestImplementation(ctx context.Context, feature string) (*agent.ReplyData, error) {
    askID := ulid.Make().String()
    correlationID := ulid.Make().String()

    // Build the request message
    msg := agent.NewAgentAsk().
        FromNS(p.namespace).
        ToNS("agent:coder").
        AskID(askID).
        Question(fmt.Sprintf("Implement this feature: %s", feature)).
        WithKind("implementation").
        WithContext(map[string]any{
            "feature_id":  ulid.Make().String(),
            "priority":    "high",
            "deadline":    time.Now().Add(2 * time.Hour).Format(time.RFC3339),
        }).
        WithSessionID(getSessionID(ctx)).
        WithWorkspace(getWorkspace(ctx)).
        WithHeader("correlation", correlationID).
        WithTTL(10 * time.Minute).
        MustBuild()

    // Send the message
    if err := p.mailbox.Send(ctx, msg); err != nil {
        return nil, fmt.Errorf("send request: %w", err)
    }

    // Wait for reply (non-blocking example below)
    return p.waitForReply(ctx, askID)
}

// waitForReply blocks until a matching reply arrives
func (p *PlannerAgent) waitForReply(ctx context.Context, askID string) (*agent.ReplyData, error) {
    // In production, use subscriptions or polling
    // This is a simplified polling example
    timeout := time.After(5 * time.Minute)
    pollInterval := 100 * time.Millisecond
    ticker := time.NewTicker(pollInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return nil, ctx.Err()
        case <-timeout:
            return nil, fmt.Errorf("timeout waiting for reply to %s", askID)
        case <-ticker.C:
            // Poll for messages
            messages, err := p.mailbox.Poll(ctx, p.namespace, 30*time.Second, 10)
            if err != nil {
                continue
            }

            for _, msg := range messages {
                if msg.Type == agent.MessageTypeReply {
                    data, err := agent.ParsePayload[agent.ReplyData](msg)
                    if err != nil {
                        continue
                    }
                    if data.AskID == askID {
                        // Ack the message
                        _ = p.mailbox.Ack(ctx, msg.ID)
                        return &data, nil
                    }
                }
                // Nack if not our reply (return to queue)
                _ = p.mailbox.Nack(ctx, msg.ID, 30*time.Second)
            }
        }
    }
}
```

### Agent: Coder (Receiver)

```go
package coder

import (
    "context"
    "fmt"

    "github.com/joshka0/foxctl/internal/runtime/actor"
    "github.com/joshka0/foxctl/internal/domain/agent"
    "github.com/joshka0/foxctl/internal/domain/envelope"
)

// CoderAgent handles implementation requests
type CoderAgent struct {
    *actor.BaseActor
}

// handleAsk processes agent.ask messages
func (a *CoderAgent) handleAsk(ctx context.Context, msg *actor.Message) (*actor.Message, error) {
    // Parse the incoming payload
    var data agent.AskData
    if err := msg.UnmarshalPayload(&data); err != nil {
        return nil, fmt.Errorf("unmarshal payload: %w", err)
    }

    // Process the implementation request
    result, err := a.implementFeature(ctx, data.Question, data.Context)
    if err != nil {
        return nil, fmt.Errorf("implementation failed: %w", err)
    }

    // Build reply
    replyData := agent.ReplyData{
        AskID: data.AskID,
        Answer: map[string]any{
            "status":    " implemented",
            "files":     result.UpdatedFiles,
            "tests_run": result.TestsPassed,
        },
    }

    replyEnv := envelope.OK("agent.reply", replyData)
    replyPayload, _ := json.Marshal(replyEnv)

    return &actor.Message{
        ID:        ulid.Make().String(),
        Subject:   "agent.reply",
        Body:      replyPayload,
        CreatedAt: time.Now(),
    }, nil
}

// Register the handler
func NewCoderActor(cfg actor.Config) *CoderAgent {
    a := &CoderAgent{
        BaseActor: actor.NewBaseActor(cfg),
    }
    a.RegisterHandler("agent.ask", a.handleAsk)
    return a
}
```

## Example 2: Fire-and-Forget Commands

The planner triggers a build process without waiting.

```go
// TriggerBuild initiates a build without waiting for completion
func (p *PlannerAgent) TriggerBuild(ctx context.Context, target string) error {
    msg := agent.NewAgentCmd().
        FromNS(p.namespace).
        ToNS("agent:builder").
        CmdID(ulid.Make().String()).
        Action("build").
        WithSkill("build").
        WithArgs(map[string]any{
            "target":       target,
            "incremental":  true,
            "parallel_jobs": 4,
        }).
        WithWorkspace(getWorkspace(ctx)).
        WithTTL(30 * time.Minute).
        MustBuild()

    return p.mailbox.Send(ctx, msg)
}
```

The builder processes commands:

```go
type BuilderAgent struct {
    *actor.BaseActor
}

func (b *BuilderAgent) handleCmd(ctx context.Context, msg *actor.Message) (*actor.Message, error) {
    var data agent.CmdData
    if err := msg.UnmarshalPayload(&data); err != nil {
        return nil, err
    }

    switch data.Action {
    case "build":
        if err := b.executeBuild(ctx, data.Args); err != nil {
            // Return error to trigger retry
            return nil, err
        }
        // Return nil to ack without reply (fire-and-forget)
        return nil, nil
    default:
        return nil, fmt.Errorf("unknown action: %s", data.Action)
    }
}
```

## Example 3: Event Broadcasting

A monitoring agent publishes events for others to consume.

```go
type MonitorAgent struct {
    *actor.BaseActor
    mailbox MailboxStore
}

// PublishBuildStatus broadcasts build completion
func (m *MonitorAgent) PublishBuildStatus(ctx context.Context, status string, metrics BuildMetrics) error {
    msg := agent.NewAgentEvent().
        FromNS(m.Namespace()).
        ToNS("broadcast"). // Special namespace for events
        EventID(ulid.Make().String()).
        EventKind("build_complete").
        WithCustomData(map[string]any{
            "status":       status,
            "duration_ms":  metrics.Duration.Milliseconds(),
            "tests_passed": metrics.TestsPassed,
            "coverage_pct": metrics.Coverage,
        }).
        WithWorkspace(getWorkspace(ctx)).
        WithHeader("priority", "normal").
        MustBuild()

    return m.mailbox.Send(ctx, msg)
}
```

A listener subscribing to events:

```go
type Agent struct {
    *actor.BaseActor
    eventChan chan Event
}

func (a *MyAgent) startEventLoop(ctx context.Context) {
    go func() {
        ticker := time.NewTicker(500 * time.Millisecond)
        defer ticker.Stop()

        for {
            select {
            case <-ctx.Done():
                return
            case <-ticker.C:
                messages, err := a.mailbox.Poll(ctx, a.Namespace(), 10*time.Second, 10)
                if err != nil {
                    log.Error("poll error", err)
                    continue
                }

                for _, msg := range messages {
                    if msg.Type == agent.MessageTypeEvent {
                        data, _ := agent.ParsePayload[agent.EventData](msg)
                        a.handleEvent(ctx, data)
                        _ = a.mailbox.Ack(ctx, msg.ID)
                    } else {
                        _ = a.mailbox.Nack(ctx, msg.ID, 30*time.Second)
                    }
                }
            }
        }
    }()
}

func (a *MyAgent) handleEvent(ctx context.Context, event agent.EventData) {
    switch event.Kind {
    case "build_complete":
        log.Info("Build completed", "status", event.Custom["status"])
        if status, ok := event.Custom["status"].(string); ok == "success" {
            a.deployAfterBuild(ctx)
        }
    }
}
```

## Example 4: Multi-Agent Orchestration

A workflow agent coordinates multiple agents:

```go
type WorkflowAgent struct {
    *actor.BaseActor
    mailbox   MailboxStore
    namespace string
}

// ExecuteFeatureWorkflow orchestrates multiple agents
func (w *WorkflowAgent) ExecuteFeatureWorkflow(ctx context.Context, feature FeatureSpec) error {
    // Step 1: Ask planner for design
    design, err := w.askPlanner(ctx, feature)
    if err != nil {
        return fmt.Errorf("planner failed: %w", err)
    }

    // Step 2: Ask coder to implement
    impl, err := w.askCoder(ctx, design)
    if err != nil {
        return fmt.Errorf("coder failed: %w", err)
    }

    // Step 3: Ask reviewer to review
    review, err := w.askReviewer(ctx, impl.Files)
    if err != nil {
        return fmt.Errorf("reviewer failed: %w", err)
    }

    // Step 4: Trigger deployment if approved
    if review.Approved {
        return w.triggerDeploy(ctx, impl.Files)
    }

    return fmt.Errorf("review not approved: %s", review.Comments)
}

func (w *WorkflowAgent) askPlanner(ctx context.Context, feature FeatureSpec) (*PlannerResult, error) {
    msg := agent.NewAgentAsk().
        FromNS(w.namespace).
        ToNS("agent:planner").
        Question(fmt.Sprintf("Design this feature: %s", feature.Name)).
        WithContext(map[string]any{
            "requirements": feature.Reqs,
            "constraints":  feature.Constraints,
        }).
        MustBuild()

    if err := w.mailbox.Send(ctx, msg); err != nil {
        return nil, err
    }

    reply, err := w.waitForReply(ctx, extractAskID(msg))
    if err != nil {
        return nil, err
    }

    // Parse the design from the reply
    var result PlannerResult
    if err := mapstructure.Decode(reply.Answer, &result); err != nil {
        return nil, err
    }
    return &result, nil
}

func (w *WorkflowAgent) askCoder(ctx context.Context, design *PlannerResult) (*ImplResult, error) {
    // Similar implementation...
    return &ImplResult{}, nil
}

func (w *WorkflowAgent) askReviewer(ctx context.Context, files []string) (*ReviewResult, error) {
    // Similar implementation...
    return &ReviewResult{}, nil
}

func (w *WorkflowAgent) triggerDeploy(ctx context.Context, files []string) error {
    msg := agent.NewAgentCmd().
        FromNS(w.namespace).
        ToNS("agent:deployer").
        Action("deploy").
        WithArgs(map[string]any{
            "environment": "staging",
            "files":       files,
        }).
        MustBuild()

    return w.mailbox.Send(ctx, msg)
}
```

## Example 5: Session Tracing

Track messages across a multi-agent session:

```go
type SessionTracer struct {
    mailbox MailboxStore
}

func (s *SessionTracer) TraceSession(ctx context.Context, sessionID string) ([]SessionMessage, error) {
    // Query all messages for this session
    messages, err := s.mailbox.ListBySession(ctx, sessionID, 100)
    if err != nil {
        return nil, err
    }

    var traced []SessionMessage
    for _, msg := range messages {
        data, _ := agent.ParsePayloadMap(msg)
        traced = append(traced, SessionMessage{
            ID:        msg.ID,
            FromNS:    msg.FromNS,
            ToNS:      msg.ToNS,
            Type:      msg.Type,
            Timestamp: msg.Timestamp,
            Data:      data,
        })
    }

    // Sort by timestamp
    sort.Slice(traced, func(i, j int) bool {
        return traced[i].Timestamp < traced[j].Timestamp
    })

    return traced, nil
}
```

## Example 6: Reply Helper Function

Simplify reply creation:

```go
// Reply creates a reply message from the original message
func ReplyToOriginal(original *actor.Message, answer map[string]any) (*actor.Message, error) {
    // Extract the original AskID from the payload
    var originalData agent.AskData
    if err := json.Unmarshal(original.Body, &originalData); err != nil {
        return nil, fmt.Errorf("unmarshal original: %w", err)
    }

    // Build reply
    replyData := agent.ReplyData{
        AskID:  originalData.AskID,
        Answer: answer,
    }
    replyEnv := envelope.OK("agent.reply", replyData)
    replyPayload, err := json.Marshal(replyEnv)
    if err != nil {
        return nil, fmt.Errorf("marshal reply: %w", err)
    }

    return &actor.Message{
        ID:        ulid.Make().String(),
        FromNS:    original.ToNS,
        ToNS:      original.FromNS,
        Subject:   "agent.reply",
        Body:      replyPayload,
        CreatedAt: time.Now(),
    }, nil
}

// Use in handlers
func (a *MyActor) handleAsk(ctx context.Context, msg *actor.Message) (*actor.Message, error) {
    // Process...
    result := processSomething()

    // Simple reply
    return ReplyToOriginal(msg, map[string]any{"result": result})
}
```

## Best Practices

1. **Always preserve session lineage:**
   ```go
   msg = builder.WithSessionID(original.SessionID)
   ```

2. **Use correlation IDs for request tracking:**
   ```go
   msg = builder.WithHeader("correlation", ulid.Make().String())
   ```

3. **Set appropriate TTLs:**
   - Quick operations: 5-10 minutes
   - Long-running: 30-60 minutes
   - Never default to 0 (messages won't expire)

4. **Handle errors gracefully:**
   ```go
   func (a *MyAgent) handleAsk(ctx context.Context, msg *actor.Message) (*actor.Message, error) {
       result, err := doWork()
       if isTransient(err) {
           return nil, err // Triggers retry
       }
       if isFatal(err) {
           // Ack to prevent retry, but send error response
           reply := buildErrorResponse(err)
           return reply, nil
       }
       return buildSuccessReply(result), nil
   }
   ```

5. **Use builder for consistency:**
   ```go
   // Good: fluent, type-safe
   msg := agent.NewAgentAsk().
       FromNS(a.Namespace()).
       ToNS("agent:b").
       Question("What is X?").
       MustBuild()

   // Avoid: manual construction is error-prone
   msg := agent.Message{
       Type: agent.MessageTypeAsk,
       // Easy to miss required fields...
   }
   ```

## Testing Message Handlers

Test your handlers by constructing messages directly:

```go
func TestMyActorHandleAsk(t *testing.T) {
    actor := NewMyActor(cfg)
    err := actor.Start(context.Background())
    require.NoError(t, err)
    defer actor.Stop(context.Background())

    // Build test message
    testData := agent.AskData{
        AskID:    "test-123",
        Question: "Test question",
    }
    testEnv := envelope.OK("agent.ask", testData)
    testPayload, _ := json.Marshal(testEnv)

    msg := &actor.Message{
        ID:      "test-msg-id",
        FromNS:  "test",
        ToNS:    actor.Namespace(),
        Subject: "agent.ask",
        Body:    testPayload,
    }

    // Call handler
    reply, err := actor.handleAsk(context.Background(), msg)
    require.NoError(t, err)
    require.NotNil(t, reply)

    // Verify reply
    var replyData agent.ReplyData
    err = json.Unmarshal(reply.Body, &replyData)
    require.NoError(t, err)
    assert.Equal(t, "test-123", replyData.AskID)
}
```

## Performance Considerations

1. **Batch polling:** Fetch multiple messages per poll
   ```go
   messages, err := store.Poll(ctx, ns, 10*time.Second, 10) // Get up to 10
   ```

2. **Short TTL for quick ops:** Frequent cleanup of expired messages
   ```go
   builder.WithTTL(5 * time.Minute) // Default
   ```

3. **Large payloads:** Use CAS instead of in-message
   ```go
   // Instead of embedding large data:
   // Bad: builder.WithContext(map[string]any{"log": hugeLog})
   
   // Good: store in CAS, send reference
   casRef := cas.Store(ctx, hugeLog)
   builder.WithContext(map[string]any{"log_artifact": casRef})
   ```