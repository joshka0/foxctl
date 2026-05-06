package companion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// CompressionDaemon runs periodic hybrid memory maintenance for all conversations.
type CompressionDaemon struct {
	memory         *ConversationMemory
	db             *sql.DB
	dailyInterval  time.Duration
	weeklyInterval time.Duration
	now            func() time.Time
	dialect        SQLDialect
	logger         zerolog.Logger
	stopCh         chan struct{}
	doneCh         chan struct{}
	doneOnce       sync.Once // Ensures doneCh is only closed once
	wg             sync.WaitGroup
	mu             sync.Mutex
	lastDailyRun   time.Time
	lastWeeklyRun  time.Time
	lastJanitorRun time.Time
}

// DaemonConfig configures the hybrid maintenance daemon.
type DaemonConfig struct {
	// Memory is the conversation memory to maintain.
	Memory *ConversationMemory

	// DB is the database to query for conversation IDs.
	DB *sql.DB

	// DailyInterval is how often to run daily hybrid context processing checks.
	// Default: 1 hour (checks if daily processing is needed)
	DailyInterval time.Duration

	// WeeklyInterval is how often to run weekly hybrid maintenance checks.
	// Default: 6 hours (checks if weekly maintenance is needed)
	WeeklyInterval time.Duration

	// Dialect is the SQL dialect for query generation.
	// Default: SQLiteDialect{}.
	Dialect SQLDialect

	// Logger for structured logging.
	Logger zerolog.Logger
}

// NewCompressionDaemon creates a hybrid maintenance daemon with default intervals.
//
// Index:
//   Purpose: Configure periodic conversation hybrid maintenance
//   Flow: apply defaults → construct daemon → return
//   Related: CompressionDaemon.Start, CompressionDaemon.Stop
//   Keywords: hybrid_daemon, daily_interval, weekly_interval, maintenance
//
// [[lifecycle:component]]
// [[domain:conversation-memory-maintenance]]
func NewCompressionDaemon(cfg DaemonConfig) *CompressionDaemon {
	if cfg.DailyInterval <= 0 {
		cfg.DailyInterval = 1 * time.Hour
	}
	if cfg.WeeklyInterval <= 0 {
		cfg.WeeklyInterval = 6 * time.Hour
	}

	dialect := cfg.Dialect
	if dialect == nil {
		dialect = SQLiteDialect{}
	}

	return &CompressionDaemon{
		memory:         cfg.Memory,
		db:             cfg.DB,
		dailyInterval:  cfg.DailyInterval,
		weeklyInterval: cfg.WeeklyInterval,
		now:            time.Now,
		dialect:        dialect,
		logger:         cfg.Logger,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}
}

// Start begins daily and weekly hybrid maintenance loops.
//
// Index:
//   Purpose: Run periodic maintenance loops for conversation memory
//   Flow: validate deps → spawn daily/weekly loops → return
//   Related: runDailyLoop, runWeeklyLoop
//   Keywords: maintenance_start, daily_loop, weekly_loop, conversation_memory
//
// [[lifecycle:component]]
// [[domain:hybrid-maintenance-scheduler]]
func (d *CompressionDaemon) Start(ctx context.Context) {
	if d.memory == nil || d.db == nil {
		d.logger.Error().
			Bool("memory_nil", d.memory == nil).
			Bool("db_nil", d.db == nil).
			Msg("hybrid maintenance daemon missing dependencies; not starting")
		d.doneOnce.Do(func() { close(d.doneCh) }) // Ensure doneCh is closed on early failure
		return
	}
	d.wg.Add(3)
	go d.runDailyLoop(ctx)
	go d.runWeeklyLoop(ctx)
	go d.runJanitorLoop(ctx)
}

// Stop signals maintenance loops to stop and waits for completion.
//
// Index:
//   Purpose: Stop maintenance daemon loops
//   Flow: close stop channel → wait for workers → close done channel
//   Related: CompressionDaemon.Start
//   Keywords: maintenance_stop, stop_channel, waitgroup, done
//
// [[lifecycle:component]]
// [[domain:hybrid-maintenance-shutdown]]
func (d *CompressionDaemon) Stop() {
	close(d.stopCh)
	d.wg.Wait()
	d.doneOnce.Do(func() { close(d.doneCh) })
}

// runDailyLoop runs daily hybrid maintenance periodically.
func (d *CompressionDaemon) runDailyLoop(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.dailyInterval)
	defer ticker.Stop()

	// Run immediately on start
	d.runDailyCompression(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.runDailyCompression(ctx)
		}
	}
}

// runWeeklyLoop runs weekly maintenance periodically.
func (d *CompressionDaemon) runWeeklyLoop(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.weeklyInterval)
	defer ticker.Stop()

	// Run immediately on start
	d.runWeeklyMaintenance(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.runWeeklyMaintenance(ctx)
		}
	}
}

// runJanitorLoop runs hybrid cleanup jobs periodically.
func (d *CompressionDaemon) runJanitorLoop(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	d.runJanitors(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.runJanitors(ctx)
		}
	}
}

// runDailyCompression runs daily hybrid context processing for all conversations.
func (d *CompressionDaemon) runDailyCompression(ctx context.Context) {
	d.mu.Lock()
	// Only run at most every 24 hours.
	if d.now().Sub(d.lastDailyRun) < 24*time.Hour {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	d.logger.Debug().Msg("Starting daily hybrid conversation maintenance")

	// Get all conversations with turns from yesterday
	yesterday := d.now().AddDate(0, 0, -1).Format("2006-01-02")
	rows, err := d.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT DISTINCT conversation_id
		FROM companion_turns
		WHERE date(created_at) = %s
	`, d.dialect.Placeholder(1)), yesterday)
	if err != nil {
		d.logger.Error().Err(err).Msg("Failed to query conversations for daily hybrid maintenance")
		return
	}
	defer rows.Close()

	var convIDs []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			convIDs = append(convIDs, id)
		}
	}

	if len(convIDs) == 0 {
		d.logger.Debug().Msg("No conversations need daily hybrid maintenance")
		d.markDailyRun()
		return
	}

	d.logger.Info().Int("count", len(convIDs)).Msg("Running daily hybrid maintenance")

	for _, convID := range convIDs {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		default:
		}

		if err := d.memory.EnsureHybridMode(ctx, convID); err != nil {
			d.logger.Warn().
				Err(err).
				Str("conversation_id", convID).
				Msg("Failed to ensure hybrid mode")
			continue
		}
		if err := d.memory.BuildHybridContextLayers(ctx, convID); err != nil {
			d.logger.Warn().
				Err(err).
				Str("conversation_id", convID).
				Msg("Failed to build hybrid context layers")
		}
	}

	d.logger.Info().Int("count", len(convIDs)).Msg("Daily hybrid maintenance complete")
	d.markDailyRun()
}

// runWeeklyMaintenance runs weekly hybrid maintenance for all conversations.
func (d *CompressionDaemon) runWeeklyMaintenance(ctx context.Context) {
	d.mu.Lock()
	// Only run at most every 7 days.
	if d.now().Sub(d.lastWeeklyRun) < 7*24*time.Hour {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	d.logger.Debug().Msg("Starting weekly hybrid maintenance")
	if err := d.runEpisodeSummaryJanitor(ctx); err != nil {
		d.logger.Warn().Err(err).Msg("Episode summary janitor failed during weekly maintenance")
	}
	d.markWeeklyRun()
}

// RunNow triggers immediate hybrid maintenance for a specific conversation.
// Useful for testing or manual intervention.
func (d *CompressionDaemon) RunNow(ctx context.Context, conversationID string) error {
	if d.memory == nil || d.db == nil {
		d.logger.Error().
			Bool("memory_nil", d.memory == nil).
			Bool("db_nil", d.db == nil).
			Msg("hybrid maintenance daemon missing dependencies; RunNow skipped")
		return errors.New("hybrid maintenance daemon missing dependencies")
	}
	if err := d.memory.EnsureHybridMode(ctx, conversationID); err != nil {
		return err
	}
	if err := d.memory.BuildHybridContextLayers(ctx, conversationID); err != nil {
		return err
	}
	return d.runEpisodeSummaryJanitor(ctx)
}

// Stats returns daemon statistics.
func (d *CompressionDaemon) Stats() DaemonStats {
	d.mu.Lock()
	defer d.mu.Unlock()

	return DaemonStats{
		LastDailyRun:  d.lastDailyRun,
		LastWeeklyRun: d.lastWeeklyRun,
	}
}

func (d *CompressionDaemon) markDailyRun() {
	d.mu.Lock()
	d.lastDailyRun = d.now()
	d.mu.Unlock()
}

func (d *CompressionDaemon) markWeeklyRun() {
	d.mu.Lock()
	d.lastWeeklyRun = d.now()
	d.mu.Unlock()
}

func (d *CompressionDaemon) markJanitorRun() {
	d.mu.Lock()
	d.lastJanitorRun = d.now()
	d.mu.Unlock()
}

// runJanitors executes periodic hybrid janitor tasks.
func (d *CompressionDaemon) runJanitors(ctx context.Context) {
	d.mu.Lock()
	if d.now().Sub(d.lastJanitorRun) < 30*time.Minute {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	if err := d.runEvidenceJanitor(ctx); err != nil {
		d.logger.Warn().Err(err).Msg("Evidence janitor failed")
	}
	if err := d.runEpisodeSummaryJanitor(ctx); err != nil {
		d.logger.Warn().Err(err).Msg("Episode summary janitor failed")
	}
	if err := d.runStagingJanitor(ctx); err != nil {
		d.logger.Warn().Err(err).Msg("Extraction staging janitor failed")
	}

	d.markJanitorRun()
}

func (d *CompressionDaemon) runEvidenceJanitor(ctx context.Context) error {
	if d.db == nil {
		return nil
	}
	type evidenceJanitor interface {
		DeleteExpiredEvidence(context.Context) (int64, error)
	}

	if janitor, ok := any(d.memory).(evidenceJanitor); ok {
		deleted, err := janitor.DeleteExpiredEvidence(ctx)
		if err != nil {
			return err
		}
		if deleted > 0 {
			d.logger.Info().Int64("deleted", deleted).Msg("Evidence TTL janitor cleaned expired snippets")
		}
		return nil
	}

	res, err := d.db.ExecContext(ctx, `
		DELETE FROM companion_evidence_snippets
		WHERE expires_at IS NOT NULL AND expires_at < CURRENT_TIMESTAMP
	`)
	if err != nil {
		return err
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if deleted > 0 {
		d.logger.Info().Int64("deleted", deleted).Msg("Evidence TTL janitor cleaned expired snippets")
	}
	return nil
}

func (d *CompressionDaemon) runEpisodeSummaryJanitor(ctx context.Context) error {
	if d.db == nil {
		return nil
	}
	if !d.canSummarizeEpisode() {
		d.logger.Debug().Msg("Episode summarizer unavailable; skipping episode summary janitor")
		return nil
	}

	rows, err := d.db.QueryContext(ctx, `
		SELECT id, conversation_id, start_event_id, end_event_id
		FROM companion_soft_episodes
		WHERE needs_summary = 1
		ORDER BY id ASC
		LIMIT 50
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type episodeRow struct {
		id           int64
		convID       string
		startEventID int64
		endEventID   int64
	}
	var episodes []episodeRow
	for rows.Next() {
		var e episodeRow
		if err := rows.Scan(&e.id, &e.convID, &e.startEventID, &e.endEventID); err != nil {
			return err
		}
		episodes = append(episodes, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(episodes) == 0 {
		return nil
	}

	updated := 0
	for _, ep := range episodes {
		summary, tokenCount, err := d.summarizeEpisode(ctx, ep.convID, ep.id, ep.startEventID, ep.endEventID)
		if err != nil {
			d.logger.Warn().Err(err).
				Str("conversation_id", ep.convID).
				Int64("episode_id", ep.id).
				Msg("Episode summary generation failed, retry later")
			continue
		}

		res, err := d.db.ExecContext(ctx, fmt.Sprintf(`
			UPDATE companion_soft_episodes
			SET summary = %s, needs_summary = 0, token_count = %s
			WHERE id = %s
		`, d.dialect.Placeholder(1), d.dialect.Placeholder(2), d.dialect.Placeholder(3)), summary, tokenCount, ep.id)
		if err != nil {
			d.logger.Warn().Err(err).
				Str("conversation_id", ep.convID).
				Int64("episode_id", ep.id).
				Msg("Failed to update generated episode summary")
			continue
		}
		affected, err := res.RowsAffected()
		if err == nil {
			updated += int(affected)
		}
	}

	if updated > 0 {
		d.logger.Info().Int("updated", updated).Msg("Episode summary janitor generated summaries")
	}
	return nil
}

func (d *CompressionDaemon) summarizeEpisode(ctx context.Context, conversationID string, episodeID, startEventID, endEventID int64) (string, int, error) {
	type summarizerA interface {
		SummarizeEpisode(context.Context, string, int64, int64, int64) (string, int, error)
	}
	type summarizerB interface {
		SummarizeEpisode(context.Context, int64, int64, int64) (string, int, error)
	}
	type summarizerC interface {
		SummarizeEpisode(context.Context, int64) (string, int, error)
	}

	if s, ok := any(d.memory).(summarizerA); ok {
		return s.SummarizeEpisode(ctx, conversationID, episodeID, startEventID, endEventID)
	}
	if s, ok := any(d.memory).(summarizerB); ok {
		return s.SummarizeEpisode(ctx, episodeID, startEventID, endEventID)
	}
	if s, ok := any(d.memory).(summarizerC); ok {
		return s.SummarizeEpisode(ctx, episodeID)
	}

	return "", 0, errors.New("episode summarizer helper unavailable")
}

func (d *CompressionDaemon) canSummarizeEpisode() bool {
	type summarizerA interface {
		SummarizeEpisode(context.Context, string, int64, int64, int64) (string, int, error)
	}
	type summarizerB interface {
		SummarizeEpisode(context.Context, int64, int64, int64) (string, int, error)
	}
	type summarizerC interface {
		SummarizeEpisode(context.Context, int64) (string, int, error)
	}

	_, hasA := any(d.memory).(summarizerA)
	_, hasB := any(d.memory).(summarizerB)
	_, hasC := any(d.memory).(summarizerC)
	return hasA || hasB || hasC
}

func (d *CompressionDaemon) runStagingJanitor(ctx context.Context) error {
	if d.db == nil {
		return nil
	}
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, conversation_id, source_event_id, proposed_entry_type, raw_text, reason, attempt_count
		FROM companion_extraction_staging
		WHERE resolved_at IS NULL AND discarded_at IS NULL AND attempt_count < 3
		ORDER BY id ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	type stagingRow struct {
		id           int64
		conversation string
		sourceEvent  int64
		entryType    string
		rawText      string
		reason       string
		attempts     int
	}
	var entries []stagingRow
	for rows.Next() {
		var e stagingRow
		if err := rows.Scan(&e.id, &e.conversation, &e.sourceEvent, &e.entryType, &e.rawText, &e.reason, &e.attempts); err != nil {
			return err
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, entry := range entries {
		nextAttempts := entry.attempts + 1

		// Attempt normalization via daemon pass.
		// In this implementation, staging entries are considered attempted every run.
		// Max attempts is 3 (0, 1, 2), then the entry is discarded.
		if nextAttempts >= 3 {
			if _, err := d.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE companion_extraction_staging
				SET attempt_count = attempt_count + 1,
					discarded_at = CURRENT_TIMESTAMP,
					discard_reason = COALESCE(discard_reason, 'max_attempts_exceeded')
				WHERE id = %s AND resolved_at IS NULL AND discarded_at IS NULL
			`, d.dialect.Placeholder(1)), entry.id); err != nil {
				return err
			}
			continue
		}

		if _, err := d.db.ExecContext(ctx, fmt.Sprintf(`
			UPDATE companion_extraction_staging
			SET attempt_count = attempt_count + 1
			WHERE id = %s
		`, d.dialect.Placeholder(1)), entry.id); err != nil {
			return err
		}
	}

	return nil
}

// DaemonStats contains daemon statistics.
type DaemonStats struct {
	LastDailyRun   time.Time `json:"last_daily_run"`
	LastWeeklyRun  time.Time `json:"last_weekly_run"`
	LastJanitorRun time.Time `json:"last_janitor_run"`
}
