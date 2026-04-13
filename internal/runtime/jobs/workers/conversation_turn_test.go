package workers

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
)

type mockTurnProcessor struct {
	reply string
	err   error

	calls          int
	conversationID string
	content        string
	principalJSON  json.RawMessage
}

func (m *mockTurnProcessor) ProcessTurn(_ context.Context, conversationID, content string, principalJSON json.RawMessage) (string, error) {
	m.calls++
	m.conversationID = conversationID
	m.content = content
	m.principalJSON = principalJSON
	if m.err != nil {
		return "", m.err
	}
	return m.reply, nil
}

type mockReplyDeliverer struct {
	err error

	calls          int
	conversation   string
	reply          string
	replyTo        string
	ctxWasCanceled bool
}

func (m *mockReplyDeliverer) DeliverReply(ctx context.Context, conversationKey, replyText, replyTo string) error {
	m.calls++
	m.conversation = conversationKey
	m.reply = replyText
	m.replyTo = replyTo
	m.ctxWasCanceled = ctx.Err() != nil
	return m.err
}

type mockLocker struct {
	err error

	lockCalls   int
	unlockCalls int
	lastID      string
}

func (m *mockLocker) Lock(_ context.Context, conversationID string) (func(), error) {
	m.lockCalls++
	m.lastID = conversationID
	if m.err != nil {
		return nil, m.err
	}
	return func() {
		m.unlockCalls++
	}, nil
}

func TestConversationTurnWorkerDependencyValidation(t *testing.T) {
	baseJob := &river.Job[ConversationTurnArgs]{
		Args: ConversationTurnArgs{
			ConversationID: "conv-1",
			Content:        "hello",
		},
	}

	t.Run("nil_worker", func(t *testing.T) {
		var worker *ConversationTurnWorker
		err := worker.Work(context.Background(), baseJob)
		if err == nil || err.Error() != "jobs: conversation turn worker is nil" {
			t.Fatalf("Work() error = %v, want nil worker error", err)
		}
	})

	t.Run("nil_job", func(t *testing.T) {
		worker := &ConversationTurnWorker{
			Processor: &mockTurnProcessor{},
			Deliverer: &mockReplyDeliverer{},
		}
		err := worker.Work(context.Background(), nil)
		if err == nil || err.Error() != "jobs: conversation turn job is required" {
			t.Fatalf("Work() error = %v, want nil job error", err)
		}
	})

	t.Run("nil_processor", func(t *testing.T) {
		worker := &ConversationTurnWorker{
			Deliverer: &mockReplyDeliverer{},
		}
		err := worker.Work(context.Background(), baseJob)
		if err == nil || err.Error() != "jobs: turn processor is required" {
			t.Fatalf("Work() error = %v, want nil processor error", err)
		}
	})

	t.Run("nil_deliverer", func(t *testing.T) {
		worker := &ConversationTurnWorker{
			Processor: &mockTurnProcessor{},
		}
		err := worker.Work(context.Background(), baseJob)
		if err == nil || err.Error() != "jobs: reply deliverer is required" {
			t.Fatalf("Work() error = %v, want nil deliverer error", err)
		}
	})
}

func TestConversationTurnWorkerArgsValidation(t *testing.T) {
	worker := &ConversationTurnWorker{
		Processor: &mockTurnProcessor{},
		Deliverer: &mockReplyDeliverer{},
	}

	t.Run("empty_conversation_id", func(t *testing.T) {
		err := worker.Work(context.Background(), &river.Job[ConversationTurnArgs]{
			Args: ConversationTurnArgs{
				ConversationID: "   ",
				Content:        "hi",
			},
		})
		if err == nil || err.Error() != "jobs: conversation_id is required" {
			t.Fatalf("Work() error = %v, want conversation_id validation error", err)
		}
	})

	t.Run("empty_content", func(t *testing.T) {
		err := worker.Work(context.Background(), &river.Job[ConversationTurnArgs]{
			Args: ConversationTurnArgs{
				ConversationID: "conv-1",
				Content:        " ",
			},
		})
		if err == nil || err.Error() != "jobs: content is required" {
			t.Fatalf("Work() error = %v, want content validation error", err)
		}
	})
}

func TestConversationTurnWorkerHappyPath(t *testing.T) {
	processor := &mockTurnProcessor{reply: "answer"}
	deliverer := &mockReplyDeliverer{}
	worker := &ConversationTurnWorker{
		Processor: processor,
		Deliverer: deliverer,
	}

	principal := json.RawMessage(`{"platform":"teams","tenant_id":"t1","user_id":"u1"}`)
	err := worker.Work(context.Background(), &river.Job[ConversationTurnArgs]{
		Args: ConversationTurnArgs{
			ConversationID: " conv-1 ",
			Content:        " hi there ",
			PrincipalJSON:  principal,
			ChannelID:      " teams:t1:c1 ",
			ReplyTo:        " act-1 ",
		},
	})
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	if processor.calls != 1 {
		t.Fatalf("ProcessTurn() calls = %d, want 1", processor.calls)
	}
	if processor.conversationID != "conv-1" {
		t.Fatalf("conversation_id = %q, want %q", processor.conversationID, "conv-1")
	}
	if processor.content != "hi there" {
		t.Fatalf("content = %q, want %q", processor.content, "hi there")
	}
	if string(processor.principalJSON) != string(principal) {
		t.Fatalf("principal json changed: got %q want %q", string(processor.principalJSON), string(principal))
	}
	if deliverer.calls != 1 {
		t.Fatalf("DeliverReply() calls = %d, want 1", deliverer.calls)
	}
	if deliverer.conversation != "teams:t1:c1" {
		t.Fatalf("deliver conversation = %q, want %q", deliverer.conversation, "teams:t1:c1")
	}
	if deliverer.reply != "answer" {
		t.Fatalf("deliver reply = %q, want %q", deliverer.reply, "answer")
	}
	if deliverer.replyTo != "act-1" {
		t.Fatalf("deliver reply_to = %q, want %q", deliverer.replyTo, "act-1")
	}
}

func TestConversationTurnWorkerWithTurnLock(t *testing.T) {
	processor := &mockTurnProcessor{reply: "ok"}
	deliverer := &mockReplyDeliverer{}
	locker := &mockLocker{}
	worker := &ConversationTurnWorker{
		Processor: processor,
		Deliverer: deliverer,
		TurnLock:  locker,
	}

	err := worker.Work(context.Background(), &river.Job[ConversationTurnArgs]{
		Args: ConversationTurnArgs{
			ConversationID: " conv-lock ",
			Content:        "hello",
			ChannelID:      "channel-1",
		},
	})
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	if locker.lockCalls != 1 {
		t.Fatalf("Lock() calls = %d, want 1", locker.lockCalls)
	}
	if locker.lastID != "conv-lock" {
		t.Fatalf("Lock() conversationID = %q, want %q", locker.lastID, "conv-lock")
	}
	if locker.unlockCalls != 1 {
		t.Fatalf("unlock calls = %d, want 1", locker.unlockCalls)
	}
}

func TestConversationTurnWorkerTimeout(t *testing.T) {
	processor := &mockTurnProcessor{
		err: context.DeadlineExceeded,
	}
	deliverer := &mockReplyDeliverer{}
	worker := &ConversationTurnWorker{
		Processor:   processor,
		Deliverer:   deliverer,
		TurnTimeout: 15 * time.Millisecond,
	}

	processor.err = nil
	processorFnDone := make(chan struct{})
	worker.Processor = TurnProcessorFunc(func(ctx context.Context, conversationID, content string, principalJSON json.RawMessage) (string, error) {
		defer close(processorFnDone)
		<-ctx.Done()
		return "", ctx.Err()
	})

	err := worker.Work(context.Background(), &river.Job[ConversationTurnArgs]{
		Args: ConversationTurnArgs{
			ConversationID: "conv-timeout",
			Content:        "hello",
			ChannelID:      "channel-1",
		},
	})
	if err == nil {
		t.Fatalf("Work() error = nil, want timeout error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Work() error = %v, want context deadline exceeded", err)
	}
	if !strings.Contains(err.Error(), "jobs: process turn:") {
		t.Fatalf("Work() error = %v, want wrapped process turn error", err)
	}
	if deliverer.calls != 0 {
		t.Fatalf("DeliverReply() calls = %d, want 0", deliverer.calls)
	}

	select {
	case <-processorFnDone:
	case <-time.After(1 * time.Second):
		t.Fatalf("processor did not observe timeout")
	}
}

type TurnProcessorFunc func(ctx context.Context, conversationID, content string, principalJSON json.RawMessage) (string, error)

func (f TurnProcessorFunc) ProcessTurn(ctx context.Context, conversationID, content string, principalJSON json.RawMessage) (string, error) {
	return f(ctx, conversationID, content, principalJSON)
}
