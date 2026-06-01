//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/mailbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMessagePassingIntegration demonstrates a complete message passing workflow
func TestMessagePassingIntegration(t *testing.T) {
	ctx := context.Background()
	idGen := newIDGenerator("integration")

	// Create a temporary in-memory mailbox for testing
	// (In production, use mailbox.Open with a real directory)
	root := t.TempDir()
	store, err := mailbox.Open(ctx, root)
	require.NoError(t, err)
	defer store.Close()

	t.Run("request-response pattern", func(t *testing.T) {
		// Agent A sends a request to Agent B
		question := "What is the status of the deployment?"

		askMsg := agent.NewAgentAsk().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			FromNS("agent:planner").
			ToNS("agent:deployer").
			Question(question).
			WithKind("question").
			WithSessionID("session-123").
			WithWorkspace("/test/project").
			WithTTL(5 * time.Minute).
			MustBuild()

		// Send the message
		err := store.Send(ctx, askMsg)
		require.NoError(t, err)

		// Agent B polls for messages
		messages, err := store.Poll(ctx, "agent:deployer", 30*time.Second, 10)
		require.NoError(t, err)
		require.Len(t, messages, 1)

		received := messages[0]
		assert.Equal(t, "agent:planner", received.FromNS)
		assert.Equal(t, "agent:deployer", received.ToNS)
		assert.Equal(t, agent.MessageTypeAsk, received.Type)

		// Agent B parses the request
		askData, err := agent.ParsePayload[agent.AskData](received)
		require.NoError(t, err)
		assert.Equal(t, question, askData.Question)

		// Agent B sends a reply
		replyMsg := agent.ReplyTo(received).
			WithID(idGen()).
			WithTimestamp(fixedTime).
			Answer(map[string]any{
				"status":    "deployed",
				"version":   "v1.2.3",
				"timestamp": time.Now().Format(time.RFC3339),
			}).
			MustBuild()

		// Send reply back
		replyMsg.ToNS = "agent:planner"
		replyMsg.FromNS = "agent:deployer"
		err = store.Send(ctx, replyMsg)
		require.NoError(t, err)

		// Agent A polls for reply
		replies, err := store.Poll(ctx, "agent:planner", 30*time.Second, 10)
		require.NoError(t, err)
		require.Len(t, replies, 1)

		// Parse and verify reply
		replyData, err := agent.ParsePayload[agent.ReplyData](replies[0])
		require.NoError(t, err)
		assert.Equal(t, askData.AskID, replyData.AskID)
		assert.Equal(t, "deployed", replyData.Answer["status"])

		// Ack both messages to clean up
		_ = store.Ack(ctx, received.ID)
		_ = store.Ack(ctx, replies[0].ID)
	})

	t.Run("fire-and-forget command", func(t *testing.T) {
		// Agent A triggers a build
		cmdMsg := agent.NewAgentCmd().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			FromNS("agent:planner").
			ToNS("agent:builder").
			CmdID("cmd-456").
			Action("build").
			WithArgs(map[string]any{
				"target":        "all",
				"incremental":   true,
				"parallel_jobs": 4,
			}).
			WithSessionID("session-123").
			MustBuild()

		// Send command
		err := store.Send(ctx, cmdMsg)
		require.NoError(t, err)

		// Agent B receives and processes
		messages, err := store.Poll(ctx, "agent:builder", 30*time.Second, 10)
		require.NoError(t, err)
		require.Len(t, messages, 1)

		cmdData, err := agent.ParsePayload[agent.CmdData](messages[0])
		require.NoError(t, err)
		assert.Equal(t, "build", cmdData.Action)
		assert.Equal(t, "all", cmdData.Args["target"])
		assert.True(t, cmdData.Args["incremental"].(bool))

		// Fire-and-forget: just ack, no reply needed
		err = store.Ack(ctx, messages[0].ID)
		require.NoError(t, err)
	})

	t.Run("event broadcasting", func(t *testing.T) {
		// Agent publishes an event
		eventMsg := agent.NewAgentEvent().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			FromNS("agent:monitor").
			ToNS("broadcast").
			EventID("evt-789").
			EventKind("build_complete").
			WithCustomData(map[string]any{
				"duration_ms": 1500,
				"success":     true,
				"tests":       50,
			}).
			MustBuild()

		// Send event
		err := store.Send(ctx, eventMsg)
		require.NoError(t, err)

		// Multiple agents can poll for events
		messages, err := store.Poll(ctx, "broadcast", 30*time.Second, 10)
		require.NoError(t, err)
		require.Len(t, messages, 1)

		eventData, err := agent.ParsePayload[agent.EventData](messages[0])
		require.NoError(t, err)
		assert.Equal(t, "build_complete", eventData.Kind)
		assert.EqualValues(t, 1500, eventData.Custom["duration_ms"])
		assert.True(t, eventData.Custom["success"].(bool))
		assert.EqualValues(t, 50, eventData.Custom["tests"])
	})

	t.Run("session lineage tracking", func(t *testing.T) {
		sessionID := "session-lineage-test"

		// Send multiple messages in the same session
		msgs := []agent.Message{
			agent.NewAgentAsk().
				WithID(idGen()).
				WithTimestamp(fixedTime).
				FromNS("agent:a").
				ToNS("agent:b").
				Question("Step 1").
				WithSessionID(sessionID).
				MustBuild(),
			agent.NewAgentAsk().
				WithID(idGen()).
				WithTimestamp(fixedTime).
				FromNS("agent:a").
				ToNS("agent:b").
				Question("Step 2").
				WithSessionID(sessionID).
				MustBuild(),
			agent.NewAgentCmd().
				WithID(idGen()).
				WithTimestamp(fixedTime).
				FromNS("agent:a").
				ToNS("agent:c").
				Action("deploy").
				WithSessionID(sessionID).
				MustBuild(),
		}

		// Send all messages
		for _, msg := range msgs {
			err := store.Send(ctx, msg)
			require.NoError(t, err)
		}

		// Query by session
		sessionMsgs, err := store.ListBySession(ctx, sessionID, 10)
		require.NoError(t, err)
		assert.Len(t, sessionMsgs, 3)

		// Verify all messages belong to the session
		for _, msg := range sessionMsgs {
			assert.Equal(t, sessionID, msg.SessionID)
		}

		// Cleanup
		for _, msg := range sessionMsgs {
			_ = store.Ack(ctx, msg.ID)
		}
	})

	t.Run("message retry with Nack", func(t *testing.T) {
		// Send a message
		msg := agent.NewAgentAsk().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			FromNS("agent:a").
			ToNS("agent:b").
			Question("Retry me").
			MustBuild()

		err := store.Send(ctx, msg)
		require.NoError(t, err)

		// Agent B polls but fails to process (Nack with delay)
		messages, err := store.Poll(ctx, "agent:b", 30*time.Second, 10)
		require.NoError(t, err)
		require.Len(t, messages, 1)

		// Mailbox visibility is stored at Unix-second precision, so a positive
		// subsecond delay becomes visible on the next second boundary.
		err = store.Nack(ctx, messages[0].ID, 100*time.Millisecond)
		require.NoError(t, err)

		// Poll again after delay - should see the message for retry.
		require.Eventually(t, func() bool {
			var pollErr error
			messages, pollErr = store.Poll(ctx, "agent:b", 25*time.Millisecond, 10)
			if pollErr != nil {
				return false
			}
			return len(messages) == 1
		}, 1500*time.Millisecond, 25*time.Millisecond)
		assert.Equal(t, msg.ID, messages[0].ID)
		assert.GreaterOrEqual(t, messages[0].Attempt, 2) // Retried delivery

		// Finally ack
		_ = store.Ack(ctx, messages[0].ID)
	})

	t.Run("workspace filtering", func(t *testing.T) {
		workspaceA := "/project/a"
		workspaceB := "/project/b"

		// Send messages for different workspaces
		msgA := agent.NewAgentAsk().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			FromNS("agent:a").
			ToNS("agent:b").
			Question("Project A").
			WithWorkspace(workspaceA).
			MustBuild()

		msgB := agent.NewAgentAsk().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			FromNS("agent:a").
			ToNS("agent:b").
			Question("Project B").
			WithWorkspace(workspaceB).
			MustBuild()

		err := store.Send(ctx, msgA)
		require.NoError(t, err)
		err = store.Send(ctx, msgB)
		require.NoError(t, err)

		// Query by workspace A
		messagesA, err := store.ListByWorkspace(ctx, workspaceA, 10)
		require.NoError(t, err)
		assert.Len(t, messagesA, 1)

		// Query by workspace B
		messagesB, err := store.ListByWorkspace(ctx, workspaceB, 10)
		require.NoError(t, err)
		assert.Len(t, messagesB, 1)

		// Verify workspaces
		assert.Equal(t, workspaceA, messagesA[0].Workspace)
		assert.Equal(t, workspaceB, messagesB[0].Workspace)

		// Cleanup
		for _, msg := range messagesA {
			_ = store.Ack(ctx, msg.ID)
		}
		for _, msg := range messagesB {
			_ = store.Ack(ctx, msg.ID)
		}
	})
}

// TestMessageBuilderFluent demonstrates the fluent builder API
func TestMessageBuilderFluent(t *testing.T) {
	idGen := newIDGenerator("fluent")
	t.Run("chained builder calls", func(t *testing.T) {
		// Build a complex message in one chain
		msg := agent.NewAgentAsk().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			FromNS("agent:sender").
			ToNS("agent:receiver").
			Question("Complex message?").
			WithKind("question").
			WithNeedsBy(time.Now().Add(5*time.Minute)).
			WithContext(map[string]any{
				"priority": "high",
				"source":   "test",
			}).
			WithSessionID("session-abc").
			WithWorkspace("/test/project").
			WithTTL(10*time.Minute).
			WithHeader("correlation", "corr-123").
			WithHeader("priority", "high").
			MustBuild()

		// Verify all fields
		data, err := agent.ParsePayload[agent.AskData](msg)
		require.NoError(t, err)

		assert.Equal(t, "agent:sender", msg.FromNS)
		assert.Equal(t, "agent:receiver", msg.ToNS)
		assert.Equal(t, "Complex message?", data.Question)
		assert.Equal(t, "question", data.Kind)
		assert.Equal(t, "session-abc", msg.SessionID)
		assert.Equal(t, "/test/project", msg.Workspace)
		assert.Equal(t, "corr-123", msg.Headers["correlation"])
		assert.EqualValues(t, 10*60*1000, msg.TTLMS)

		// Test the NeedsBy timestamp was set correctly
		needsBy := time.UnixMilli(data.NeedsByMS)
		assert.True(t, needsBy.After(time.Now()))
		assert.True(t, needsBy.Before(time.Now().Add(6*time.Minute)))
	})

	t.Run("ReplyTo helper preserves context", func(t *testing.T) {
		// Create an original message with context
		original := agent.NewAgentAsk().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			FromNS("agent:a").
			ToNS("agent:b").
			Question("Original").
			WithSessionID("session-xyz").
			WithWorkspace("/workspace").
			WithHeader("correlation", "corr-xyz").
			MustBuild()

		// Use ReplyTo builder
		reply := agent.ReplyTo(original).
			WithID(idGen()).
			WithTimestamp(fixedTime).
			Answer(map[string]any{"result": "success"}).
			MustBuild()

		// Verify context is preserved
		assert.Equal(t, "agent:b", reply.FromNS) // Swapped
		assert.Equal(t, "agent:a", reply.ToNS)   // Swapped
		assert.Equal(t, "session-xyz", reply.SessionID)
		assert.Equal(t, "/workspace", reply.Workspace)
		assert.Equal(t, "corr-xyz", reply.Headers["correlation"])
	})

	t.Run("console message builders", func(t *testing.T) {
		// Console ask from user
		ask := agent.NewConsoleAsk().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			FromNS("user:tui").
			ToNS("agent:coder").
			Prompt("Fix this bug").
			ConsoleID("console-1").
			WithContext(map[string]any{
				"file_line": 123,
			}).
			MustBuild()

		askData, err := agent.ParsePayload[agent.ConsoleAskData](ask)
		require.NoError(t, err)
		assert.Equal(t, "Fix this bug", askData.Prompt)
		assert.Equal(t, "console-1", askData.ConsoleID)

		// Console event during processing
		event := agent.NewConsoleEvent().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			ToNS("console").
			EventKind("progress").
			Content("Reading file...").
			Seq(1).
			MustBuild()

		eventData, err := agent.ParsePayload[agent.ConsoleEventData](event)
		require.NoError(t, err)
		assert.Equal(t, "progress", eventData.Kind)
		assert.Equal(t, "Reading file...", eventData.Content)
		assert.Equal(t, 1, eventData.Seq)

		// Console reply to user
		reply := agent.NewConsoleReply().
			WithID(idGen()).
			WithTimestamp(fixedTime).
			AskID(askData.AskID).
			Response("Bug fixed!").
			Status("ok").
			WithHeader("correlation", "test-corr").
			MustBuild()

		replyData, err := agent.ParsePayload[agent.ConsoleReplyData](reply)
		require.NoError(t, err)
		assert.Equal(t, "Bug fixed!", replyData.Response)
		assert.Equal(t, "ok", replyData.Status)
	})
}
