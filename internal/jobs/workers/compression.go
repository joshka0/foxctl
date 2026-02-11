package workers

import (
	"context"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skilltest"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const (
	compressDailyKind = "companion.compress_daily"
	compressScanKind  = "companion.compress_scan"
)

// CompressDailyArgs contains arguments for a per-conversation daily compression job.
type CompressDailyArgs struct {
	// ConversationID is the conversation to compress.
	ConversationID string `json:"conversation_id"`

	// Date is the UTC date in YYYY-MM-DD format.
	Date string `json:"date"`
}

// Kind returns the River job kind.
func (CompressDailyArgs) Kind() string { return compressDailyKind }

// CompressScanArgs contains arguments for the periodic compression scanner.
type CompressScanArgs struct{}

// Kind returns the River job kind.
func (CompressScanArgs) Kind() string { return compressScanKind }

// DailyCompressor performs day-level conversation compression.
type DailyCompressor interface {
	// RunDayCompression compresses a specific conversation day.
	RunDayCompression(ctx context.Context, conversationID, date string, force bool) (bool, error)
}

// CompressionConversation represents a conversation candidate for compression.
type CompressionConversation struct {
	// ID is the conversation identifier.
	ID string
}

// CompressionConversationLister lists conversations to scan for compression work.
type CompressionConversationLister interface {
	// ListConversations returns conversation candidates up to the provided limit.
	ListConversations(ctx context.Context, limit int) ([]CompressionConversation, error)
}

// CompressionJobInserter enqueues jobs.
type CompressionJobInserter interface {
	// Insert enqueues a job payload.
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// CompressDailyWorker executes daily conversation compression jobs.
type CompressDailyWorker struct {
	river.WorkerDefaults[CompressDailyArgs]

	// Compressor performs day-level compression logic.
	Compressor DailyCompressor
}

// Work executes a single daily compression job.
func (w *CompressDailyWorker) Work(ctx context.Context, job *river.Job[CompressDailyArgs]) error {
	if w == nil || w.Compressor == nil {
		return fmt.Errorf("jobs: daily compressor dependency is required")
	}
	if job == nil {
		return fmt.Errorf("jobs: daily compression job is required")
	}

	conversationID := strings.TrimSpace(job.Args.ConversationID)
	date := strings.TrimSpace(job.Args.Date)
	if conversationID == "" {
		return fmt.Errorf("jobs: conversation_id is required")
	}
	if date == "" {
		return fmt.Errorf("jobs: date is required")
	}

	event := observability.NewEvent("jobs.compress_daily").
		WithComponent(observability.ComponentJob).
		WithData("conversation_id", conversationID).
		WithData("date", date)

	if _, err := w.Compressor.RunDayCompression(ctx, conversationID, date, false); err != nil {
		wrappedErr := fmt.Errorf("jobs: run day compression: %w", err)
		observability.Emit(ctx, event.Error(wrappedErr, 0))
		return wrappedErr
	}

	observability.Emit(ctx, event.Success(0))
	return nil
}

// CompressScanWorker scans conversations and enqueues daily compression jobs.
type CompressScanWorker struct {
	river.WorkerDefaults[CompressScanArgs]

	// Lister provides conversations to scan.
	Lister CompressionConversationLister

	// Inserter enqueues daily compression jobs.
	Inserter CompressionJobInserter

	// Clock provides current time for deterministic date calculations.
	Clock skilltest.Clock

	// ScanLimit is the maximum number of conversations scanned per run.
	ScanLimit int
}

// Work scans for conversation candidates and enqueues daily compression jobs.
func (w *CompressScanWorker) Work(ctx context.Context, job *river.Job[CompressScanArgs]) error {
	if w == nil {
		return fmt.Errorf("jobs: compression scan worker is nil")
	}
	if job == nil {
		return fmt.Errorf("jobs: compression scan job is required")
	}
	if w.Lister == nil {
		return fmt.Errorf("jobs: compression scan lister is required")
	}
	if w.Inserter == nil {
		return fmt.Errorf("jobs: compression scan inserter is required")
	}

	clock := w.Clock
	if clock == nil {
		clock = skilltest.RealClock{}
	}

	limit := w.ScanLimit
	if limit <= 0 {
		limit = 100
	}

	conversations, err := w.Lister.ListConversations(ctx, limit)
	if err != nil {
		return fmt.Errorf("jobs: list conversations for compression scan: %w", err)
	}

	date := clock.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	enqueued := 0
	for _, conversation := range conversations {
		conversationID := strings.TrimSpace(conversation.ID)
		if conversationID == "" {
			continue
		}

		_, err := w.Inserter.Insert(ctx, CompressDailyArgs{
			ConversationID: conversationID,
			Date:           date,
		}, nil)
		if err != nil {
			return fmt.Errorf("jobs: enqueue daily compression for %q: %w", conversationID, err)
		}
		enqueued++
	}

	observability.Emit(ctx, observability.NewEvent("jobs.compress_scan").
		WithComponent(observability.ComponentJob).
		WithData("conversation_count", len(conversations)).
		WithData("enqueued", enqueued).
		WithData("date", date).
		WithData("scan_limit", limit).
		Success(0))

	return nil
}

var _ river.Worker[CompressDailyArgs] = (*CompressDailyWorker)(nil)
var _ river.Worker[CompressScanArgs] = (*CompressScanWorker)(nil)
