package workers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type mockDailyCompressor struct {
	conversationID string
	date           string
	force          bool
	err            error
	calls          int
}

func (m *mockDailyCompressor) RunDayCompression(_ context.Context, conversationID, date string, force bool) (bool, error) {
	m.calls++
	m.conversationID = conversationID
	m.date = date
	m.force = force
	return true, m.err
}

type mockConversationLister struct {
	conversations []CompressionConversation
	err           error
	lastLimit     int
}

func (m *mockConversationLister) ListConversations(_ context.Context, limit int) ([]CompressionConversation, error) {
	m.lastLimit = limit
	if m.err != nil {
		return nil, m.err
	}
	return m.conversations, nil
}

type mockInserter struct {
	inserted []CompressDailyArgs
	err      error
}

func (m *mockInserter) Insert(_ context.Context, args river.JobArgs, _ *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	typedArgs, ok := args.(CompressDailyArgs)
	if !ok {
		return nil, fmt.Errorf("unexpected args type %T", args)
	}
	m.inserted = append(m.inserted, typedArgs)
	if m.err != nil {
		return nil, m.err
	}
	return &rivertype.JobInsertResult{}, nil
}

func TestCompressDailyWorker(t *testing.T) {
	compressor := &mockDailyCompressor{}
	worker := &CompressDailyWorker{Compressor: compressor}

	err := worker.Work(context.Background(), &river.Job[CompressDailyArgs]{
		Args: CompressDailyArgs{
			ConversationID: "conv-123",
			Date:           "2026-02-10",
		},
	})
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	if compressor.calls != 1 {
		t.Fatalf("RunDayCompression() calls = %d, want 1", compressor.calls)
	}
	if compressor.conversationID != "conv-123" {
		t.Fatalf("conversation_id = %q, want conv-123", compressor.conversationID)
	}
	if compressor.date != "2026-02-10" {
		t.Fatalf("date = %q, want 2026-02-10", compressor.date)
	}
	if compressor.force {
		t.Fatalf("force = true, want false")
	}
}

func TestCompressScanWorkerEnqueueBehavior(t *testing.T) {
	clock := skilltest.NewFakeClock(time.Date(2026, time.February, 11, 14, 0, 0, 0, time.UTC))
	lister := &mockConversationLister{conversations: []CompressionConversation{{ID: "conv-a"}, {ID: "  "}, {ID: "conv-b"}}}
	inserter := &mockInserter{}

	worker := &CompressScanWorker{
		Lister:    lister,
		Inserter:  inserter,
		Clock:     clock,
		ScanLimit: 10,
	}

	err := worker.Work(context.Background(), &river.Job[CompressScanArgs]{Args: CompressScanArgs{}})
	if err != nil {
		t.Fatalf("Work() error = %v", err)
	}

	if lister.lastLimit != 10 {
		t.Fatalf("ListConversations() limit = %d, want 10", lister.lastLimit)
	}
	if len(inserter.inserted) != 2 {
		t.Fatalf("inserted jobs = %d, want 2", len(inserter.inserted))
	}

	for i, job := range inserter.inserted {
		if job.Date != "2026-02-10" {
			t.Fatalf("inserted[%d].date = %q, want 2026-02-10", i, job.Date)
		}
	}
	if inserter.inserted[0].ConversationID != "conv-a" {
		t.Fatalf("inserted[0].conversation_id = %q, want conv-a", inserter.inserted[0].ConversationID)
	}
	if inserter.inserted[1].ConversationID != "conv-b" {
		t.Fatalf("inserted[1].conversation_id = %q, want conv-b", inserter.inserted[1].ConversationID)
	}
}
