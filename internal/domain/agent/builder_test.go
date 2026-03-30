package agent

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var fixedTime = time.Unix(1700000000, 0)

func TestAgentAskBuilder(t *testing.T) {
	t.Run("builds valid message", func(t *testing.T) {
		msg, err := NewAgentAsk().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			FromNS("agent:a").
			ToNS("agent:b").
			Question("What is 2+2?").
			WithKind("question").
			WithResponseSchema(json.RawMessage(`{"type":"object","properties":{"summary":{"type":"string"}}}`)).
			WithResponseKeys([]string{"summary", "next_action"}).
			WithSessionID("session-123").
			WithWorkspace("/path/to/project").
			WithTTL(10 * time.Minute).
			Build()

		require.NoError(t, err)

		// Verify message structure
		assert.NotEmpty(t, msg.ID)
		assert.Equal(t, "agent:a", msg.FromNS)
		assert.Equal(t, "agent:b", msg.ToNS)
		assert.Equal(t, MessageTypeAsk, msg.Type)
		assert.EqualValues(t, 10*60*1000, msg.TTLMS)
		assert.Equal(t, "session-123", msg.SessionID)
		assert.Equal(t, "/path/to/project", msg.Workspace)

		// Verify payload envelope
		data, err := ParsePayload[AskData](msg)
		require.NoError(t, err)
		assert.Equal(t, "What is 2+2?", data.Question)
		assert.Equal(t, "question", data.Kind)
		assert.NotEmpty(t, data.AskID)
		assert.JSONEq(t, `{"type":"object","properties":{"summary":{"type":"string"}}}`, string(data.ResponseSchema))
		assert.Equal(t, []string{"summary", "next_action"}, data.ResponseKeys)
	})

	t.Run("envelope format", func(t *testing.T) {
		msg, err := NewAgentAsk().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			Question("Test?").
			Build()
		require.NoError(t, err)

		var envelope struct {
			Version int    `json:"version"`
			Status  string `json:"status"`
			Command string `json:"command"`
			Meta    struct {
				Timestamp string `json:"ts"`
			} `json:"meta"`
			Error map[string]any `json:"error"`
		}

		err = json.Unmarshal(msg.Payload, &envelope)
		require.NoError(t, err)

		assert.Equal(t, 1, envelope.Version)
		assert.Equal(t, "ok", envelope.Status)
		assert.Equal(t, string(MessageTypeAsk), envelope.Command)
		assert.NotEmpty(t, envelope.Meta.Timestamp)
		assert.Empty(t, envelope.Error)
	})
}

func TestAgentReplyBuilder(t *testing.T) {
	t.Run("builds valid reply", func(t *testing.T) {
		msg, err := NewAgentReply().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			AskID("ask-123").
			Answer(map[string]any{"result": 4}).
			Build()

		require.NoError(t, err)

		data, err := ParsePayload[ReplyData](msg)
		require.NoError(t, err)

		assert.Equal(t, "ask-123", data.AskID)
		assert.EqualValues(t, 4, data.Answer["result"])
	})

	t.Run("preserves correlation header", func(t *testing.T) {
		原始消息 := func() Message {
			msg, _ := NewAgentAsk().
				WithID("msg-1").
				WithTimestamp(fixedTime).
				FromNS("agent:a").
				ToNS("agent:b").
				WithHeader("correlation", "corr-123").
				Build()
			return msg
		}()

		replyMsg := ReplyTo(原始消息).
			WithID("msg-1").
			WithTimestamp(fixedTime).
			Answer(map[string]any{"result": "done"}).
			MustBuild()

		assert.Equal(t, "agent:b", replyMsg.FromNS)
		assert.Equal(t, "agent:a", replyMsg.ToNS)
		assert.Equal(t, "corr-123", replyMsg.Headers["correlation"])
	})
}

func TestAgentCmdBuilder(t *testing.T) {
	t.Run("builds valid command", func(t *testing.T) {
		msg, err := NewAgentCmd().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			FromNS("agent:a").
			ToNS("agent:builder").
			CmdID("cmd-123").
			Action("run_build").
			WithArgs(map[string]any{"target": "all"}).
			Build()

		require.NoError(t, err)

		data, err := ParsePayload[CmdData](msg)
		require.NoError(t, err)

		assert.Equal(t, "cmd-123", data.CmdID)
		assert.Equal(t, "run_build", data.Action)
		assert.Equal(t, "all", data.Args["target"])
	})
}

func TestAgentEventBuilder(t *testing.T) {
	t.Run("builds valid event", func(t *testing.T) {
		msg, err := NewAgentEvent().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			FromNS("agent:monitor").
			ToNS("broadcast").
			EventID("evt-123").
			EventKind("build_complete").
			WithJobCount(5).
			WithCustomData(map[string]any{
				"duration_ms": 1500,
				"success":     true,
			}).
			Build()

		require.NoError(t, err)

		data, err := ParsePayload[EventData](msg)
		require.NoError(t, err)

		assert.Equal(t, "evt-123", data.EventID)
		assert.Equal(t, "build_complete", data.Kind)
		assert.Equal(t, 5, data.JobCount)
		assert.EqualValues(t, 1500, data.Custom["duration_ms"])
		assert.True(t, data.Custom["success"].(bool))
	})
}

func TestConsoleMessageBuilders(t *testing.T) {
	t.Run("console ask", func(t *testing.T) {
		msg, err := NewConsoleAsk().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			FromNS("agent:a").
			ToNS("console").
			Prompt("Fix this bug").
			ConsoleID("console-1").
			Build()

		require.NoError(t, err)

		data, err := ParsePayload[ConsoleAskData](msg)
		require.NoError(t, err)

		assert.Equal(t, "Fix this bug", data.Prompt)
		assert.Equal(t, "console-1", data.ConsoleID)
		assert.Equal(t, MessageTypeConsoleAsk, msg.Type)
	})

	t.Run("console reply", func(t *testing.T) {
		msg, err := NewConsoleReply().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			AskID("ask-1").
			Response("Bug fixed!").
			Status("ok").
			WithHeader("correlation", "corr-1").
			Build()

		require.NoError(t, err)

		data, err := ParsePayload[ConsoleReplyData](msg)
		require.NoError(t, err)

		assert.Equal(t, "ask-1", data.AskID)
		assert.Equal(t, "Bug fixed!", data.Response)
		assert.Equal(t, "ok", data.Status)
		assert.Equal(t, "corr-1", msg.Headers["correlation"])
	})

	t.Run("console event", func(t *testing.T) {
		msg, err := NewConsoleEvent().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			ToNS("console").
			EventKind("progress").
			Content("Scanning files...").
			Seq(1).
			Iteration(0).
			Build()

		require.NoError(t, err)

		data, err := ParsePayload[ConsoleEventData](msg)
		require.NoError(t, err)

		assert.Equal(t, "progress", data.Kind)
		assert.Equal(t, "Scanning files...", data.Content)
		assert.Equal(t, 1, data.Seq)
		assert.Equal(t, 0, data.Iteration)
	})
}

func TestHeaders(t *testing.T) {
	t.Run("single header", func(t *testing.T) {
		msg := NewAgentAsk().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			WithHeader("priority", "high").
			MustBuild()

		assert.Equal(t, "high", msg.Headers["priority"])
		assert.Len(t, msg.Headers, 1)
	})

	t.Run("multiple headers", func(t *testing.T) {
		headers := map[string]string{
			"priority":  "high",
			"retryable": "false",
			"source":    "test",
		}
		msg := NewAgentAsk().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			WithHeaders(headers).
			MustBuild()

		assert.Equal(t, "high", msg.Headers["priority"])
		assert.Equal(t, "false", msg.Headers["retryable"])
		assert.Equal(t, "test", msg.Headers["source"])
	})
}

func TestParsePayload(t *testing.T) {
	t.Run("parse into struct", func(t *testing.T) {
		msg := NewAgentAsk().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			Question("Test?").
			MustBuild()

		data, err := ParsePayload[AskData](msg)
		require.NoError(t, err)

		assert.Equal(t, "Test?", data.Question)
	})

	t.Run("parse into map", func(t *testing.T) {
		msg := NewAgentAsk().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			Question("Test?").
			WithSessionID("session-1").
			MustBuild()

		data, err := ParsePayloadMap(msg)
		require.NoError(t, err)

		assert.Equal(t, "Test?", data["question"])
		assert.Equal(t, "session-1", msg.SessionID)
	})
}

func TestBuilderValidation(t *testing.T) {
	t.Run("requires id", func(t *testing.T) {
		_, err := NewAgentAsk().
			WithTimestamp(fixedTime).
			Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "message id is required")
	})

	t.Run("requires timestamp", func(t *testing.T) {
		_, err := NewAgentAsk().
			WithID("msg-1").
			Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "message timestamp is required")
	})
}

func TestMustBuild(t *testing.T) {
	t.Run("does not panic on valid build", func(t *testing.T) {
		// This should not panic
		msg := NewAgentAsk().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			Question("Test?").
			MustBuild()
		assert.NotEmpty(t, msg.ID)
	})
}

func TestDefaultTTL(t *testing.T) {
	msg := NewAgentAsk().
		WithID("msg-1").
		WithTimestamp(fixedTime).
		MustBuild()
	expectedTTL := int64(5 * 60 * 1000) // 5 minutes in milliseconds
	assert.Equal(t, expectedTTL, msg.TTLMS)
}

func TestReplyTo(t *testing.T) {
	t.Run("copies context from original", func(t *testing.T) {
		sessionID := "session-123"
		workspace := "/path/to/project"

		original := NewAgentAsk().
			WithID("reply-original-1").
			WithTimestamp(fixedTime).
			FromNS("agent:a").
			ToNS("agent:b").
			WithSessionID(sessionID).
			WithWorkspace(workspace).
			WithHeader("correlation", "corr-456").
			MustBuild()

		reply := ReplyTo(original).
			WithID("reply-msg-1").
			WithTimestamp(fixedTime).
			Answer(map[string]any{
				"result": "done",
			}).
			MustBuild()

		assert.Equal(t, "agent:b", reply.FromNS)
		assert.Equal(t, "agent:a", reply.ToNS)
		assert.Equal(t, sessionID, reply.SessionID)
		assert.Equal(t, workspace, reply.Workspace)
		assert.Equal(t, "corr-456", reply.Headers["correlation"])
	})
}

func BenchmarkMessageBuild(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = NewAgentAsk().
			WithID("msg-1").
			WithTimestamp(fixedTime).
			FromNS("agent:a").
			ToNS("agent:b").
			Question("Benchmark question").
			WithSessionID("session-123").
			WithWorkspace("/path/to/project").
			Build()
	}
}

func BenchmarkMessageParse(b *testing.B) {
	msg := NewAgentAsk().
		Question("Parse this").
		WithSessionID("session-123").
		WithID("bench-msg-id").
		WithTimestamp(time.Now()).
		MustBuild()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ParsePayload[AskData](msg)
	}
}
