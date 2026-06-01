package workers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
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
			ConversationID: " conv-123 ",
			Date:           " 2026-02-10 ",
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

func TestCompressDailyWorkerContractValidation(t *testing.T) {
	validJob := &river.Job[CompressDailyArgs]{
		Args: CompressDailyArgs{
			ConversationID: "conv-123",
			Date:           "2026-02-10",
		},
	}

	t.Run("nil_worker", func(t *testing.T) {
		var worker *CompressDailyWorker
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: daily compressor dependency is required" {
			t.Fatalf("Work() error = %v, want dependency error", err)
		}
	})

	t.Run("nil_job", func(t *testing.T) {
		worker := &CompressDailyWorker{Compressor: &mockDailyCompressor{}}
		err := worker.Work(context.Background(), nil)
		if err == nil || err.Error() != "jobs: daily compression job is required" {
			t.Fatalf("Work() error = %v, want nil job error", err)
		}
	})

	t.Run("nil_compressor", func(t *testing.T) {
		worker := &CompressDailyWorker{}
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: daily compressor dependency is required" {
			t.Fatalf("Work() error = %v, want dependency error", err)
		}
	})

	t.Run("empty_conversation_id", func(t *testing.T) {
		compressor := &mockDailyCompressor{}
		worker := &CompressDailyWorker{Compressor: compressor}
		err := worker.Work(context.Background(), &river.Job[CompressDailyArgs]{
			Args: CompressDailyArgs{
				ConversationID: "  ",
				Date:           "2026-02-10",
			},
		})
		if err == nil || err.Error() != "jobs: conversation_id is required" {
			t.Fatalf("Work() error = %v, want conversation_id validation error", err)
		}
		if compressor.calls != 0 {
			t.Fatalf("RunDayCompression() calls = %d, want 0", compressor.calls)
		}
	})

	t.Run("empty_date", func(t *testing.T) {
		compressor := &mockDailyCompressor{}
		worker := &CompressDailyWorker{Compressor: compressor}
		err := worker.Work(context.Background(), &river.Job[CompressDailyArgs]{
			Args: CompressDailyArgs{
				ConversationID: "conv-123",
				Date:           "  ",
			},
		})
		if err == nil || err.Error() != "jobs: date is required" {
			t.Fatalf("Work() error = %v, want date validation error", err)
		}
		if compressor.calls != 0 {
			t.Fatalf("RunDayCompression() calls = %d, want 0", compressor.calls)
		}
	})

	t.Run("compressor_error", func(t *testing.T) {
		sentinel := errors.New("storage unavailable")
		compressor := &mockDailyCompressor{err: sentinel}
		worker := &CompressDailyWorker{Compressor: compressor}
		err := worker.Work(context.Background(), validJob)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Work() error = %v, want wrapped sentinel", err)
		}
		if !strings.Contains(err.Error(), "jobs: run day compression:") {
			t.Fatalf("Work() error = %v, want compression context", err)
		}
	})
}

func TestCompressScanWorkerEnqueueBehavior(t *testing.T) {
	clock := skilltest.NewFakeClock(time.Date(2026, time.February, 11, 14, 0, 0, 0, time.UTC))
	lister := &mockConversationLister{conversations: []CompressionConversation{{ID: " conv-a "}, {ID: "  "}, {ID: " conv-b "}}}
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

func TestCompressScanWorkerContractValidation(t *testing.T) {
	validJob := &river.Job[CompressScanArgs]{Args: CompressScanArgs{}}

	t.Run("nil_worker", func(t *testing.T) {
		var worker *CompressScanWorker
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: compression scan worker is nil" {
			t.Fatalf("Work() error = %v, want nil worker error", err)
		}
	})

	t.Run("nil_job", func(t *testing.T) {
		worker := &CompressScanWorker{
			Lister:   &mockConversationLister{},
			Inserter: &mockInserter{},
		}
		err := worker.Work(context.Background(), nil)
		if err == nil || err.Error() != "jobs: compression scan job is required" {
			t.Fatalf("Work() error = %v, want nil job error", err)
		}
	})

	t.Run("nil_lister", func(t *testing.T) {
		worker := &CompressScanWorker{Inserter: &mockInserter{}}
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: compression scan lister is required" {
			t.Fatalf("Work() error = %v, want lister dependency error", err)
		}
	})

	t.Run("nil_inserter", func(t *testing.T) {
		worker := &CompressScanWorker{Lister: &mockConversationLister{}}
		err := worker.Work(context.Background(), validJob)
		if err == nil || err.Error() != "jobs: compression scan inserter is required" {
			t.Fatalf("Work() error = %v, want inserter dependency error", err)
		}
	})

	t.Run("default_limit", func(t *testing.T) {
		lister := &mockConversationLister{}
		worker := &CompressScanWorker{
			Lister:   lister,
			Inserter: &mockInserter{},
		}
		if err := worker.Work(context.Background(), validJob); err != nil {
			t.Fatalf("Work() error = %v", err)
		}
		if lister.lastLimit != 100 {
			t.Fatalf("ListConversations() limit = %d, want 100", lister.lastLimit)
		}
	})

	t.Run("list_error", func(t *testing.T) {
		sentinel := errors.New("list failed")
		worker := &CompressScanWorker{
			Lister:   &mockConversationLister{err: sentinel},
			Inserter: &mockInserter{},
		}
		err := worker.Work(context.Background(), validJob)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Work() error = %v, want wrapped sentinel", err)
		}
		if !strings.Contains(err.Error(), "jobs: list conversations for compression scan:") {
			t.Fatalf("Work() error = %v, want list context", err)
		}
	})

	t.Run("insert_error", func(t *testing.T) {
		sentinel := errors.New("insert failed")
		worker := &CompressScanWorker{
			Lister:   &mockConversationLister{conversations: []CompressionConversation{{ID: "conv-a"}}},
			Inserter: &mockInserter{err: sentinel},
		}
		err := worker.Work(context.Background(), validJob)
		if !errors.Is(err, sentinel) {
			t.Fatalf("Work() error = %v, want wrapped sentinel", err)
		}
		if !strings.Contains(err.Error(), `jobs: enqueue daily compression for "conv-a":`) {
			t.Fatalf("Work() error = %v, want enqueue context", err)
		}
	})
}
