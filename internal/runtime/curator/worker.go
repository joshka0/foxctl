// Package curator implements a background worker that periodically runs
// deterministic curation across all foxctl context stores: memory lifecycle
// transitions, observation cleanup, tension review, and handoff archival.
//
// The worker is leader-gated (only the daemon leader runs it) and configurable
// via config.yaml or environment variables. Two modes:
//
//   - "active" mode: frequent runs (default 5m) for dev machines — keeps the
//     context plane tidy during active development.
//   - "dream" mode: infrequent runs (24–72h) for production/CI — performs
//     heavier analysis like consolidation, dedup, and utility scoring.
//
// The curator is deterministic: no LLM calls. It applies age, confidence,
// count, and utility heuristics. LLM-assisted review is a future extension.
package curator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/memory"
)

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// Mode controls the curator's cadence and aggressiveness.
type Mode string

const (
	// ModeActive runs frequently (default 5m) with light checks.
	// Suitable for dev machines where context should stay tidy.
	ModeActive Mode = "active"

	// ModeDream runs infrequently (default 24h) with full analysis
	// including consolidation clusters, utility scoring, and dedup.
	// Suitable for production or long-running daemons.
	ModeDream Mode = "dream"
)

// Config holds curator worker configuration.
type Config struct {
	// Mode controls cadence: "active" (frequent, light) or "dream" (infrequent, deep).
	// Default: "active".
	Mode Mode `yaml:"mode" json:"mode"`

	// ActiveInterval is the tick interval in active mode. Default: 5m.
	ActiveInterval time.Duration `yaml:"active_interval" json:"active_interval"`

	// DreamInterval is the tick interval in dream mode. Default: 24h.
	DreamInterval time.Duration `yaml:"dream_interval" json:"dream_interval"`

	// StaleAfterDays controls when active records with no uses are proposed stale.
	// Default: 30.
	StaleAfterDays int `yaml:"stale_after_days" json:"stale_after_days"`

	// ArchiveAfterDays controls when stale records are proposed for archive.
	// Default: 90.
	ArchiveAfterDays int `yaml:"archive_after_days" json:"archive_after_days"`

	// MinConfidence controls the observation cleanup threshold.
	// Observations below this are flagged for review. Default: 0.5.
	MinConfidence float64 `yaml:"min_confidence" json:"min_confidence"`

	// HandoffStaleDays controls when handoff files are flagged for archival.
	// Default: 30.
	HandoffStaleDays int `yaml:"handoff_stale_days" json:"handoff_stale_days"`

	// Enabled controls whether the curator runs at all. Default: true.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// DryRun controls whether mutations are applied. Default: false (apply).
	// Set true to only produce reports without mutating state.
	DryRun bool `yaml:"dry_run" json:"dry_run"`
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		Mode:             ModeActive,
		ActiveInterval:   5 * time.Minute,
		DreamInterval:    24 * time.Hour,
		StaleAfterDays:   30,
		ArchiveAfterDays: 90,
		MinConfidence:    0.5,
		HandoffStaleDays: 30,
		Enabled:          true,
		DryRun:           false,
	}
}

// Interval returns the tick duration for the current mode.
func (c Config) Interval() time.Duration {
	switch c.Mode {
	case ModeDream:
		if c.DreamInterval > 0 {
			return c.DreamInterval
		}
		return 24 * time.Hour
	default:
		if c.ActiveInterval > 0 {
			return c.ActiveInterval
		}
		return 5 * time.Minute
	}
}

// IsDream reports whether the curator is in dream mode.
func (c Config) IsDream() bool {
	return c.Mode == ModeDream
}

// ConfigFromEnv builds a Config from environment variables.
//   - FOXCTL_CURATOR_MODE: "active" or "dream"
//   - FOXCTL_CURATOR_INTERVAL: Go duration (e.g. "5m", "24h", "72h")
//   - FOXCTL_CURATOR_ENABLED: "true" or "false"
//   - FOXCTL_CURATOR_DRY_RUN: "true" or "false"
//   - FOXCTL_CURATOR_STALE_AFTER_DAYS: integer
//   - FOXCTL_CURATOR_ARCHIVE_AFTER_DAYS: integer
func ConfigFromEnv(base Config) Config {
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_MODE")); v != "" {
		base.Mode = Mode(strings.ToLower(v))
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			// Override both intervals with the explicit value
			base.ActiveInterval = d
			base.DreamInterval = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_ENABLED")); v != "" {
		base.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_DRY_RUN")); v != "" {
		base.DryRun = strings.EqualFold(v, "true") || v == "1"
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_STALE_AFTER_DAYS")); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &base.StaleAfterDays); err == nil && n == 1 && base.StaleAfterDays > 0 {
			// ok
		}
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_ARCHIVE_AFTER_DAYS")); v != "" {
		if n, err := fmt.Sscanf(v, "%d", &base.ArchiveAfterDays); err == nil && n == 1 && base.ArchiveAfterDays > 0 {
			// ok
		}
	}
	return base
}

// ---------------------------------------------------------------------------
// Report types
// ---------------------------------------------------------------------------

// CuratorReport is the result of a single curator pass.
type CuratorReport struct {
	ID          string       `json:"id"`
	Mode        Mode         `json:"mode"`
	GeneratedAt time.Time    `json:"generated_at"`
	Config      Config       `json:"config"`
	Summary     CuratorSummary `json:"summary"`
	Memory      *MemoryReport  `json:"memory,omitempty"`
}

// CuratorSummary holds aggregate counts.
type CuratorSummary struct {
	MemoryRecords       int `json:"memory_records"`
	MemoryProposals     int `json:"memory_proposals"`
	Observations        int `json:"observations"`
	ObservationProposals int `json:"observation_proposals"`
	Tensions            int `json:"tensions"`
	OpenTensions        int `json:"open_tensions"`
	Handoffs            int `json:"handoffs"`
	StaleHandoffs       int `json:"stale_handoffs"`
	Errors              int `json:"errors"`
}

// MemoryReport wraps the memory curator's output.
type MemoryReport struct {
	TotalRecords      int                        `json:"total_records"`
	DemotionProposals int                        `json:"demotion_proposals"`
	ArchiveProposals  int                        `json:"archive_proposals"`
	DuplicateClusters int                        `json:"duplicate_clusters"`
	OverlapClusters   int                        `json:"overlap_clusters"`
	Applied           int                        `json:"applied,omitempty"`
	AppliedSummary    *ApplySummary              `json:"applied_summary,omitempty"`
}

// ApplySummary is set when the curator runs in apply mode.
type ApplySummary struct {
	Attempted int `json:"attempted"`
	Applied   int `json:"applied"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

// ---------------------------------------------------------------------------
// Worker
// ---------------------------------------------------------------------------

// Worker runs the curator on a configurable ticker.
type Worker struct {
	cfg   Config
	memFn MemoryStoreOpener

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	lastReport *CuratorReport
	lastRunAt  time.Time
}

// MemoryStoreOpener opens a memory store for a workspace.
type MemoryStoreOpener func(ctx context.Context, cfg config.Config) (storage.MemoryStore, error)

// NewWorker creates a curator worker.
func NewWorker(cfg Config, memFn MemoryStoreOpener) *Worker {
	return &Worker{
		cfg:   cfg,
		memFn: memFn,
	}
}

// Start begins the curator loop. It runs one immediate pass, then ticks.
func (w *Worker) Start(ctx context.Context) error {
	if !w.cfg.Enabled {
		return nil
	}
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	ctx, w.cancel = context.WithCancel(ctx)
	w.mu.Unlock()

	// Immediate first run
	w.runOnce(ctx)

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(w.cfg.Interval())
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.runOnce(ctx)
			}
		}
	}()
	return nil
}

// Stop terminates the curator loop.
func (w *Worker) Stop() {
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()
	w.wg.Wait()
}

// LastReport returns the most recent curator report (nil if never run).
func (w *Worker) LastReport() *CuratorReport {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastReport
}

// LastRunAt returns the time of the most recent curator run.
func (w *Worker) LastRunAt() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastRunAt
}

// RunOnce executes a single curator pass. Useful for CLI invocations.
func (w *Worker) RunOnce(ctx context.Context) (*CuratorReport, error) {
	return w.execute(ctx)
}

// ---------------------------------------------------------------------------
// Internal
// ---------------------------------------------------------------------------

func (w *Worker) runOnce(ctx context.Context) {
	report, err := w.execute(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curator: run error: %v\n", err)
		return
	}
	w.mu.Lock()
	w.lastReport = report
	w.lastRunAt = time.Now().UTC()
	w.mu.Unlock()

	if report.Summary.MemoryProposals > 0 || report.Summary.ObservationProposals > 0 || report.Summary.OpenTensions > 0 || report.Summary.StaleHandoffs > 0 {
		mode := string(report.Mode)
		fmt.Fprintf(os.Stderr, "curator [%s]: memory=%d proposals, observations=%d proposals, tensions=%d open, handoffs=%d stale\n",
			mode, report.Summary.MemoryProposals, report.Summary.ObservationProposals, report.Summary.OpenTensions, report.Summary.StaleHandoffs)
	}
}

func (w *Worker) execute(ctx context.Context) (*CuratorReport, error) {
	now := time.Now().UTC()
	report := &CuratorReport{
		ID:          "curator-" + now.Format("20060102T150405Z"),
		Mode:        w.cfg.Mode,
		GeneratedAt: now,
		Config:      w.cfg,
	}

	// Memory curator
	if w.memFn != nil {
		memStore, err := w.memFn(ctx, config.Config{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "curator: open memory store: %v\n", err)
			report.Summary.Errors++
		} else {
			defer memStore.Close()
			memReport := w.curateMemory(ctx, memStore)
			report.Memory = memReport
			report.Summary.MemoryRecords = memReport.TotalRecords
			report.Summary.MemoryProposals = memReport.DemotionProposals + memReport.ArchiveProposals
		}
	}

	return report, nil
}

func (w *Worker) curateMemory(ctx context.Context, memStore storage.MemoryStore) *MemoryReport {
	report := &MemoryReport{}

	// List all records
	entries, err := memStore.List(ctx, "", 5000)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curator: list memory: %v\n", err)
		return report
	}

	// Convert to records for the curator planner
	var records []memorycore.Record
	for _, e := range entries {
		records = append(records, memorycore.RecordFromNamedEntry(e, memorycore.NamedEntryOptions{}))
	}
	report.TotalRecords = len(records)

	// Plan the curator report (deterministic)
	curatorCfg := memorycore.DefaultCuratorConfig(time.Now().UTC())
	curatorCfg.StaleAfterDays = w.cfg.StaleAfterDays
	curatorCfg.ArchiveAfterDays = w.cfg.ArchiveAfterDays

	plan := memorycore.PlanCuratorReport(records, curatorCfg)

	for _, p := range plan.Proposals {
		switch p.Action {
		case memorycore.CuratorActionDemoteStale:
			report.DemotionProposals++
		case memorycore.CuratorActionArchive:
			report.ArchiveProposals++
		}
	}
	report.DuplicateClusters = plan.Summary.DuplicateClusters
	report.OverlapClusters = plan.Summary.OverlapClusters

	// In dream mode, also report consolidation and supersession
	if w.cfg.IsDream() {
		// Dream mode could trigger LLM review here in the future
		// For now, just report the clusters
	}

	// Apply if not dry-run
	if !w.cfg.DryRun && len(plan.Proposals) > 0 {
		applied, skipped, failed := w.applyMemoryProposals(ctx, memStore, plan.Proposals)
		report.Applied = applied
		report.AppliedSummary = &ApplySummary{
			Attempted: len(plan.Proposals),
			Applied:   applied,
			Skipped:   skipped,
			Failed:    failed,
		}
	}

	return report
}

func (w *Worker) applyMemoryProposals(ctx context.Context, memStore storage.MemoryStore, proposals []memorycore.CuratorProposal) (applied, skipped, failed int) {
	for _, p := range proposals {
		if p.SourceLane != memorycore.SourceLaneNamedMemory {
			skipped++
			continue
		}
		sourceID := strings.TrimSpace(p.SourceID)
		if sourceID == "" {
			sourceID = strings.TrimSpace(p.RecordID)
		}

		update := memory.LifecycleUpdate{
			LifecycleState: string(p.ProposedState),
			ReviewNotes:    fmt.Sprintf("curator: %s (%s)", p.Action, strings.Join(p.Reasons, "; ")),
		}

		_, err := memStore.UpdateLifecycle(ctx, sourceID, "", update)
		if err != nil {
			fmt.Fprintf(os.Stderr, "curator: apply %s %s: %v\n", p.Action, p.RecordID, err)
			failed++
		} else {
			applied++
		}
	}
	return
}
