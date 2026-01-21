package companion

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// CompressionDaemon runs periodic memory compression for all conversations.
// It summarizes yesterday's turns daily and distills old summaries weekly.
type CompressionDaemon struct {
	memory           *ConversationMemory
	db               *sql.DB
	dailyInterval    time.Duration
	weeklyInterval   time.Duration
	logger           zerolog.Logger
	stopCh           chan struct{}
	doneCh           chan struct{}
	doneOnce         sync.Once // Ensures doneCh is only closed once
	wg               sync.WaitGroup
	mu               sync.Mutex
	lastDailyRun     time.Time
	lastWeeklyRun    time.Time
}

// DaemonConfig configures the compression daemon.
type DaemonConfig struct {
	// Memory is the conversation memory to compress.
	Memory *ConversationMemory

	// DB is the database to query for conversation IDs.
	DB *sql.DB

	// DailyInterval is how often to run daily compression.
	// Default: 1 hour (checks if daily compression is needed)
	DailyInterval time.Duration

	// WeeklyInterval is how often to run weekly distillation.
	// Default: 6 hours (checks if weekly distillation is needed)
	WeeklyInterval time.Duration

	// Logger for structured logging.
	Logger zerolog.Logger
}

// NewCompressionDaemon creates a new compression daemon.
func NewCompressionDaemon(cfg DaemonConfig) *CompressionDaemon {
	if cfg.DailyInterval <= 0 {
		cfg.DailyInterval = 1 * time.Hour
	}
	if cfg.WeeklyInterval <= 0 {
		cfg.WeeklyInterval = 6 * time.Hour
	}

	return &CompressionDaemon{
		memory:         cfg.Memory,
		db:             cfg.DB,
		dailyInterval:  cfg.DailyInterval,
		weeklyInterval: cfg.WeeklyInterval,
		logger:         cfg.Logger,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}
}

// Start begins the daemon loops.
func (d *CompressionDaemon) Start(ctx context.Context) {
	if d.memory == nil || d.db == nil {
		d.logger.Error().
			Bool("memory_nil", d.memory == nil).
			Bool("db_nil", d.db == nil).
			Msg("compression daemon missing dependencies; not starting")
		d.doneOnce.Do(func() { close(d.doneCh) }) // Ensure doneCh is closed on early failure
		return
	}
	d.wg.Add(2)
	go d.runDailyLoop(ctx)
	go d.runWeeklyLoop(ctx)
}

// Stop stops the daemon and waits for it to finish.
func (d *CompressionDaemon) Stop() {
	close(d.stopCh)
	d.wg.Wait()
	d.doneOnce.Do(func() { close(d.doneCh) })
}

// runDailyLoop runs daily compression periodically.
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

// runWeeklyLoop runs weekly distillation periodically.
func (d *CompressionDaemon) runWeeklyLoop(ctx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.weeklyInterval)
	defer ticker.Stop()

	// Run immediately on start
	d.runWeeklyDistillation(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.runWeeklyDistillation(ctx)
		}
	}
}

// runDailyCompression compresses yesterday's turns for all conversations.
func (d *CompressionDaemon) runDailyCompression(ctx context.Context) {
	d.mu.Lock()
	// Only run at most every 24 hours.
	if time.Since(d.lastDailyRun) < 24*time.Hour {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	d.logger.Debug().Msg("Starting daily conversation compression")

	// Get all conversations with turns from yesterday
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	rows, err := d.db.QueryContext(ctx, `
		SELECT DISTINCT conversation_id
		FROM companion_turns
		WHERE date(created_at) = ?
	`, yesterday)
	if err != nil {
		d.logger.Error().Err(err).Msg("Failed to query conversations for daily compression")
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
		d.logger.Debug().Msg("No conversations need daily compression")
		d.markDailyRun()
		return
	}

	d.logger.Info().Int("count", len(convIDs)).Msg("Running daily compression")

	for _, convID := range convIDs {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		default:
		}

		if err := d.memory.RunDailyCompression(ctx, convID); err != nil {
			d.logger.Warn().
				Err(err).
				Str("conversation_id", convID).
				Msg("Failed to compress conversation")
		} else {
			d.logger.Debug().
				Str("conversation_id", convID).
				Msg("Compressed conversation")
		}
	}

	d.logger.Info().Int("count", len(convIDs)).Msg("Daily compression complete")
	d.markDailyRun()
}

// runWeeklyDistillation distills old summaries for all conversations.
func (d *CompressionDaemon) runWeeklyDistillation(ctx context.Context) {
	d.mu.Lock()
	// Only run at most every 7 days.
	if time.Since(d.lastWeeklyRun) < 7*24*time.Hour {
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()

	d.logger.Debug().Msg("Starting weekly history distillation")

	// Get conversations with summaries older than the recent window
	recentWindowDays := 7
	if d.memory != nil && d.memory.config.RecentWindowDays > 0 {
		recentWindowDays = d.memory.config.RecentWindowDays
	}
	cutoffDate := time.Now().AddDate(0, 0, -recentWindowDays).Format("2006-01-02")
	rows, err := d.db.QueryContext(ctx, `
		SELECT DISTINCT conversation_id
		FROM companion_day_summaries
		WHERE date < ?
	`, cutoffDate)
	if err != nil {
		d.logger.Error().Err(err).Msg("Failed to query conversations for distillation")
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
		d.logger.Debug().Msg("No conversations need weekly distillation")
		d.markWeeklyRun()
		return
	}

	d.logger.Info().Int("count", len(convIDs)).Msg("Running weekly distillation")

	for _, convID := range convIDs {
		select {
		case <-ctx.Done():
			return
		case <-d.stopCh:
			return
		default:
		}

		if err := d.memory.RunWeeklyDistillation(ctx, convID); err != nil {
			d.logger.Warn().
				Err(err).
				Str("conversation_id", convID).
				Msg("Failed to distill conversation history")
		} else {
			d.logger.Debug().
				Str("conversation_id", convID).
				Msg("Distilled conversation history")
		}
	}

	d.logger.Info().Int("count", len(convIDs)).Msg("Weekly distillation complete")
	d.markWeeklyRun()
}

// RunNow triggers immediate compression for a specific conversation.
// Useful for testing or manual intervention.
func (d *CompressionDaemon) RunNow(ctx context.Context, conversationID string) error {
	if d.memory == nil || d.db == nil {
		d.logger.Error().
			Bool("memory_nil", d.memory == nil).
			Bool("db_nil", d.db == nil).
			Msg("compression daemon missing dependencies; RunNow skipped")
		return errors.New("compression daemon missing dependencies")
	}
	if err := d.memory.RunDailyCompression(ctx, conversationID); err != nil {
		return err
	}
	return d.memory.RunWeeklyDistillation(ctx, conversationID)
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
	d.lastDailyRun = time.Now()
	d.mu.Unlock()
}

func (d *CompressionDaemon) markWeeklyRun() {
	d.mu.Lock()
	d.lastWeeklyRun = time.Now()
	d.mu.Unlock()
}

// DaemonStats contains daemon statistics.
type DaemonStats struct {
	LastDailyRun  time.Time `json:"last_daily_run"`
	LastWeeklyRun time.Time `json:"last_weekly_run"`
}
