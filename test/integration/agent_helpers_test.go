//go:build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fixedTime = time.Unix(1700000000, 0)
var fixedClock = func() time.Time { return fixedTime }

func newIDGenerator(prefix string) func() string {
	counter := 0
	return func() string {
		counter++
		return fmt.Sprintf("%s-%d", prefix, counter)
	}
}

func TestSender_SendAsk(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := mailbox.Open(ctx, root)
	require.NoError(t, err)
	defer store.Close()

	idGen := newIDGenerator("ask")
	sender := agent.NewSender(store, "agent:a", agent.WithSenderClock(fixedClock), agent.WithSenderIDGenerator(idGen))

	t.Run("simple ask", func(t *testing.T) {
		askID, err := sender.SendAsk(ctx, "agent:b", "What is 2+2?")
		require.NoError(t, err)
		assert.NotEmpty(t, askID)

		// Verify message was sent
		messages, err := store.Poll(ctx, "agent:b", 30*time.Second, 1)
		require.NoError(t, err)
		require.Len(t, messages, 1)

		msg := messages[0]
		assert.Equal(t, "agent:a", msg.FromNS)
		assert.Equal(t, "agent:b", msg.ToNS)
		assert.Equal(t, agent.MessageTypeAsk, msg.Type)

		data, err := agent.ParsePayload[agent.AskData](msg)
		require.NoError(t, err)
		assert.Equal(t, "What is 2+2?", data.Question)
		assert.Equal(t, askID, data.AskID)

		_ = store.Ack(ctx, msg.ID)
	})

	t.Run("ask with options", func(t *testing.T) {
		sessionID := "session-123"
		workspace := "/test/project"
		correlation := "corr-456"

		askID, err := sender.SendAsk(ctx, "agent:b", "Complex question",
			agent.WithAskKind("question"),
			agent.WithAskSession(sessionID),
			agent.WithAskWorkspace(workspace),
			agent.WithAskContext(map[string]any{"priority": "high"}),
			agent.WithAskTTL(10*time.Minute),
			agent.WithAskCorrelation(correlation),
		)
		require.NoError(t, err)
		assert.NotEmpty(t, askID)

		// Verify message
		messages, err := store.Poll(ctx, "agent:b", 30*time.Second, 1)
		require.NoError(t, err)
		require.Len(t, messages, 1)

		msg := messages[0]
		assert.Equal(t, sessionID, msg.SessionID)
		assert.Equal(t, workspace, msg.Workspace)
		assert.Equal(t, correlation, msg.Headers["correlation"])
		assert.EqualValues(t, 10*60*1000, msg.TTLMS)

		data, err := agent.ParsePayload[agent.AskData](msg)
		require.NoError(t, err)
		assert.Equal(t, "question", data.Kind)
		assert.Equal(t, "high", data.Context["priority"])

		_ = store.Ack(ctx, msg.ID)
	})
}

func TestSender_SendReply(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := mailbox.Open(ctx, root)
	require.NoError(t, err)
	defer store.Close()

	idGen := newIDGenerator("reply")
	_ = agent.NewSender(store, "agent:a", agent.WithSenderClock(fixedClock), agent.WithSenderIDGenerator(idGen)) // senderA not used in this test
	senderB := agent.NewSender(store, "agent:b", agent.WithSenderClock(fixedClock), agent.WithSenderIDGenerator(idGen))

	t.Run("reply to ask", func(t *testing.T) {
		// Send original ask
		originalAsk := agent.NewAgentAsk().
			WithID("orig-ask").
			WithTimestamp(fixedTime).
			FromNS("agent:a").
			ToNS("agent:b").
			Question("Question").
			WithSessionID("session-1").
			MustBuild()
		err = store.Send(ctx, originalAsk)
		require.NoError(t, err)

		// Receive the ask
		received, err := store.Poll(ctx, "agent:b", 30*time.Second, 1)
		require.NoError(t, err)
		require.Len(t, received, 1)

		// Send reply
		err = senderB.SendReply(ctx, received[0], map[string]any{
			"result": 42,
		})
		require.NoError(t, err)

		// Receive the reply
		replies, err := store.Poll(ctx, "agent:a", 30*time.Second, 1)
		require.NoError(t, err)
		require.Len(t, replies, 1)

		reply := replies[0]
		assert.Equal(t, "agent:b", reply.FromNS)
		assert.Equal(t, "agent:a", reply.ToNS)
		assert.Equal(t, agent.MessageTypeReply, reply.Type)

		data, err := agent.ParsePayload[agent.ReplyData](reply)
		require.NoError(t, err)
		assert.Equal(t, received[0].AskID(), data.AskID)
		assert.EqualValues(t, 42, data.Answer["result"])

		_ = store.Ack(ctx, received[0].ID)
		_ = store.Ack(ctx, reply.ID)
	})
}

func TestSender_SendCmd(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := mailbox.Open(ctx, root)
	require.NoError(t, err)
	defer store.Close()

	idGen := newIDGenerator("cmd")
	sender := agent.NewSender(store, "agent:a", agent.WithSenderClock(fixedClock), agent.WithSenderIDGenerator(idGen))

	t.Run("send command", func(t *testing.T) {
		cmdID, err := sender.SendCmd(ctx, "agent:builder", "build",
			map[string]any{
				"target": "all",
				"clean":  false,
			},
			agent.WithCmdSession("session-1"),
			agent.WithCmdWorkspace("/project"),
			agent.WithCmdSkill("build"),
			agent.WithCmdTTL(15*time.Minute),
		)
		require.NoError(t, err)
		assert.NotEmpty(t, cmdID)

		// Verify message
		messages, err := store.Poll(ctx, "agent:builder", 30*time.Second, 1)
		require.NoError(t, err)
		require.Len(t, messages, 1)

		msg := messages[0]
		assert.Equal(t, agent.MessageTypeCmd, msg.Type)
		assert.Equal(t, "session-1", msg.SessionID)
		assert.Equal(t, "/project", msg.Workspace)

		data, err := agent.ParsePayload[agent.CmdData](msg)
		require.NoError(t, err)
		assert.Equal(t, "build", data.Action)
		assert.Equal(t, "all", data.Args["target"])
		assert.False(t, data.Args["clean"].(bool))

		_ = store.Ack(ctx, msg.ID)
	})
}

func TestSender_SendEvent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := mailbox.Open(ctx, root)
	require.NoError(t, err)
	defer store.Close()

	idGen := newIDGenerator("event")
	sender := agent.NewSender(store, "agent:monitor", agent.WithSenderClock(fixedClock), agent.WithSenderIDGenerator(idGen))

	t.Run("send event", func(t *testing.T) {
		err := sender.SendEvent(ctx, "build_complete",
			map[string]any{
				"duration_ms": 1500,
				"success":     true,
			},
			agent.WithEventSession("session-1"),
			agent.WithEventWorkspace("/project"),
			agent.WithEventDestination("broadcast"),
			agent.WithEventTTL(5*time.Minute),
		)
		require.NoError(t, err)

		// Verify message
		messages, err := store.Poll(ctx, "broadcast", 30*time.Second, 1)
		require.NoError(t, err)
		require.Len(t, messages, 1)

		msg := messages[0]
		assert.Equal(t, agent.MessageTypeEvent, msg.Type)
		assert.Equal(t, "session-1", msg.SessionID)

		data, err := agent.ParsePayload[agent.EventData](msg)
		require.NoError(t, err)
		assert.Equal(t, "build_complete", data.Kind)
		assert.EqualValues(t, 1500, data.Custom["duration_ms"])
		assert.True(t, data.Custom["success"].(bool))

		_ = store.Ack(ctx, msg.ID)
	})
}

func TestReceiver(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := mailbox.Open(ctx, root)
	require.NoError(t, err)
	defer store.Close()

	idGen := newIDGenerator("receiver")
	sender := agent.NewSender(store, "agent:a", agent.WithSenderClock(fixedClock), agent.WithSenderIDGenerator(idGen))
	receiver := agent.NewReceiver(store, "agent:b")

	t.Run("poll once", func(t *testing.T) {
		// Send a message
		_, err := sender.SendAsk(ctx, "agent:b", "Question")
		require.NoError(t, err)

		// Poll for it
		msg, err := receiver.PollOnce(ctx, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, msg)
		assert.Equal(t, "agent:a", msg.FromNS)
		assert.Equal(t, "agent:b", msg.ToNS)

		// Poll again - should get nil since first message was claimed
		msg, err = receiver.PollOnce(ctx, 30*time.Second)
		require.NoError(t, err)
		assert.Nil(t, msg)
	})

	t.Run("poll all", func(t *testing.T) {
		// Send multiple messages
		for i := 0; i < 3; i++ {
			_, err := sender.SendAsk(ctx, "agent:b", "Question")
			require.NoError(t, err)
		}

		// Poll for all
		messages, err := receiver.PollAll(ctx, 30*time.Second, 10)
		require.NoError(t, err)
		assert.Len(t, messages, 3)

		// Ack all
		for _, msg := range messages {
			_ = receiver.Ack(ctx, &msg)
		}
	})

	t.Run("ack and nack", func(t *testing.T) {
		// Send message
		_, err := sender.SendAsk(ctx, "agent:b", "Question")
		require.NoError(t, err)

		// Poll
		msg, err := receiver.PollOnce(ctx, 30*time.Second)
		require.NoError(t, err)
		require.NotNil(t, msg)

		// Use a short delay here; this test cares about retry semantics, not a literal 5s timeout.
		err = receiver.Nack(ctx, msg, 100*time.Millisecond)
		require.NoError(t, err)

		// Wait and poll again
		require.Eventually(t, func() bool {
			var pollErr error
			msg, pollErr = receiver.PollOnce(ctx, 25*time.Millisecond)
			return pollErr == nil && msg != nil
		}, 750*time.Millisecond, 25*time.Millisecond)
		assert.GreaterOrEqual(t, msg.Attempt, 2) // Retried delivery

		// Now ack
		err = receiver.Ack(ctx, msg)
		require.NoError(t, err)
	})
}

func TestMessageMethods(t *testing.T) {
	t.Run("AskID extraction", func(t *testing.T) {
		msg := agent.NewAgentAsk().
			Question("Test").
			WithID("ask-msg-1").
			WithTimestamp(fixedTime).
			MustBuild()
		assert.NotEmpty(t, msg.AskID())

		reply := agent.NewAgentReply().
			WithID("reply-msg-1").
			WithTimestamp(fixedTime).
			MustBuild()
		assert.Empty(t, reply.AskID())
	})

	t.Run("CmdID extraction", func(t *testing.T) {
		msg := agent.NewAgentCmd().
			Action("test").
			WithID("cmd-msg-1").
			WithTimestamp(fixedTime).
			MustBuild()
		assert.NotEmpty(t, msg.CmdID())

		reply := agent.NewAgentReply().
			WithID("reply-msg-2").
			WithTimestamp(fixedTime).
			MustBuild()
		assert.Empty(t, reply.CmdID())
	})

	t.Run("EventID extraction", func(t *testing.T) {
		msg := agent.NewAgentEvent().
			EventKind("test").
			WithID("event-msg-1").
			WithTimestamp(fixedTime).
			MustBuild()
		assert.NotEmpty(t, msg.EventID())

		reply := agent.NewAgentReply().
			WithID("reply-msg-3").
			WithTimestamp(fixedTime).
			MustBuild()
		assert.Empty(t, reply.EventID())
	})

	t.Run("UnmarshalPayload", func(t *testing.T) {
		msg := agent.NewAgentAsk().
			Question("Test question").
			WithContext(map[string]any{"key": "value"}).
			WithID("ask-msg-2").
			WithTimestamp(fixedTime).
			MustBuild()

		var data agent.AskData
		err := msg.UnmarshalPayload(&data)
		require.NoError(t, err)
		assert.Equal(t, "Test question", data.Question)
		assert.Equal(t, "value", data.Context["key"])
	})
}

func TestFilterFunctions(t *testing.T) {
	t.Run("WithEvents", func(t *testing.T) {
		messages := []agent.Message{
			agent.NewAgentAsk().
				Question("Ask").
				WithID("filter-ask-1").
				WithTimestamp(fixedTime).
				MustBuild(),
			agent.NewAgentEvent().
				EventKind("build_complete").
				WithID("filter-event-1").
				WithTimestamp(fixedTime).
				MustBuild(),
			agent.NewAgentEvent().
				EventKind("deployment").
				WithID("filter-event-2").
				WithTimestamp(fixedTime).
				MustBuild(),
			agent.NewAgentCmd().
				Action("test").
				WithID("filter-cmd-1").
				WithTimestamp(fixedTime).
				MustBuild(),
		}

		buildEvents := agent.WithEvents(messages, "build_complete")
		assert.Len(t, buildEvents, 1)
		assert.Equal(t, "build_complete", getMessageKind(buildEvents[0]))
	})

	t.Run("WithAskID", func(t *testing.T) {
		askID := "ask-123"
		messages := []agent.Message{
			agent.NewAgentReply().
				AskID(askID).
				Answer(map[string]any{}).
				WithID("filter-reply-1").
				WithTimestamp(fixedTime).
				MustBuild(),
			agent.NewAgentReply().
				AskID("other-ask").
				Answer(map[string]any{}).
				WithID("filter-reply-2").
				WithTimestamp(fixedTime).
				MustBuild(),
			agent.NewAgentAsk().
				Question("Ask").
				WithID("filter-ask-2").
				WithTimestamp(fixedTime).
				MustBuild(),
		}

		matching := agent.WithAskID(messages, askID)
		assert.Len(t, matching, 1)
		assert.Equal(t, askID, matching[0].AskID())
	})
}

func getMessageKind(msg agent.Message) string {
	if data, err := agent.ParsePayload[agent.EventData](msg); err == nil {
		return data.Kind
	}
	return ""
}

func TestReplyToBuilder(t *testing.T) {
	t.Run("preserves context", func(t *testing.T) {
		original := agent.NewAgentAsk().
			FromNS("agent:a").
			ToNS("agent:b").
			Question("Original").
			WithSessionID("session-1").
			WithWorkspace("/project").
			WithHeader("correlation", "corr-1").
			WithID("replyto-ask-1").
			WithTimestamp(fixedTime).
			MustBuild()

		builder := agent.ReplyToBuilder(original)
		reply := builder.
			Answer(map[string]any{"result": "success"}).
			WithID("replyto-reply-1").
			WithTimestamp(fixedTime).
			MustBuild()

		assert.Equal(t, "agent:b", reply.FromNS)
		assert.Equal(t, "agent:a", reply.ToNS)
		assert.Equal(t, "session-1", reply.SessionID)
		assert.Equal(t, "/project", reply.Workspace)
		assert.Equal(t, "corr-1", reply.Headers["correlation"])

		// Verify AskID is preserved
		var data agent.ReplyData
		err := reply.UnmarshalPayload(&data)
		require.NoError(t, err)
		assert.Equal(t, original.AskID(), data.AskID)
	})
}
