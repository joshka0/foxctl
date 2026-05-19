// Package curator implements a background worker that periodically runs
// deterministic curation across all foxctl context stores.
//
// Two concurrent loops run inside a single worker:
//
//   - "active" loop: frequent (default 5m) light checks — lifecycle transitions,
//     stale detection, quick sweeps. Keeps the context plane tidy during
//     active development.
//   - "dream" loop: infrequent (default 24h) deep analysis — consolidation
//     clusters, utility scoring, dedup, overlap detection. Like human sleep
//     consolidating memories overnight.
//
// Both loops run simultaneously. Active handles the day-to-day hygiene;
// dream handles the heavier periodic maintenance.
//
// The curator is deterministic: no LLM calls. It applies age, confidence,
// count, and utility heuristics. LLM-assisted review is a future extension
// (dream mode could spawn a cheap aux-model pass for borderline cases).
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

// Config holds curator worker configuration.
type Config struct {
	// ActiveInterval is the tick for the active (light) loop. Default: 5m.
	ActiveInterval time.Duration `yaml:"active_interval" json:"active_interval"`

	// DreamInterval is the tick for the dream (deep) loop. Default: 24h.
	DreamInterval time.Duration `yaml:"dream_interval" json:"dream_interval"`

	// StaleAfterDays: active records with no uses older than this → stale. Default: 30.
	StaleAfterDays int `yaml:"stale_after_days" json:"stale_after_days"`

	// ArchiveAfterDays: stale records older than this → archive. Default: 90.
	ArchiveAfterDays int `yaml:"archive_after_days" json:"archive_after_days"`

	// MinConfidence: observations below this → flagged for review. Default: 0.5.
	MinConfidence float64 `yaml:"min_confidence" json:"min_confidence"`

	// HandoffStaleDays: handoff files older than this → flag for archival. Default: 30.
	HandoffStaleDays int `yaml:"handoff_stale_days" json:"handoff_stale_days"`

	// Enabled controls whether the curator runs. Default: true.
	Enabled bool `yaml:"enabled" json:"enabled"`

	// DryRun: true = report only, no mutations. Default: false.
	DryRun bool `yaml:"dry_run" json:"dry_run"`

	// ActiveEnabled controls whether the active loop runs. Default: true.
	ActiveEnabled bool `yaml:"active_enabled" json:"active_enabled"`

	// DreamEnabled controls whether the dream loop runs. Default: true.
	DreamEnabled bool `yaml:"dream_enabled" json:"dream_enabled"`
}

// DefaultConfig returns sensible defaults — both loops enabled.
func DefaultConfig() Config {
	return Config{
		ActiveInterval:   5 * time.Minute,
		DreamInterval:    24 * time.Hour,
		StaleAfterDays:   30,
		ArchiveAfterDays: 90,
		MinConfidence:    0.5,
		HandoffStaleDays: 30,
		Enabled:          true,
		DryRun:           false,
		ActiveEnabled:    true,
		DreamEnabled:     true,
	}
}

// ConfigFromEnv builds a Config from environment variables.
func ConfigFromEnv(base Config) Config {
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_ACTIVE_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			base.ActiveInterval = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_DREAM_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
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
		if n, _ := fmt.Sscanf(v, "%d", &base.StaleAfterDays); n == 1 && base.StaleAfterDays > 0 {
			// ok
		}
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_ARCHIVE_AFTER_DAYS")); v != "" {
		if n, _ := fmt.Sscanf(v, "%d", &base.ArchiveAfterDays); n == 1 && base.ArchiveAfterDays > 0 {
			// ok
		}
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_ACTIVE_ENABLED")); v != "" {
		base.ActiveEnabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_DREAM_ENABLED")); v != "" {
		base.DreamEnabled = strings.EqualFold(v, "true") || v == "1"
	}
	// Legacy: FOXCTL_CURATOR_INTERVAL overrides both (backward compat)
	if v := strings.TrimSpace(os.Getenv("FOXCTL_CURATOR_INTERVAL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			base.ActiveInterval = d
		}
	}
	return base
}

// ---------------------------------------------------------------------------
// Report types
// ---------------------------------------------------------------------------

// Report is the result of a single curator pass (either active or dream).
type Report struct {
	ID          string    `json:"id"`
	Phase       string    `json:"phase"` // "active" or "dream"
	GeneratedAt time.Time `json:"generated_at"`
	Config      Config    `json:"config"`
	Summary     Summary   `json:"summary"`
	Memory      *MemoryReport `json:"memory,omitempty"`
}

// Summary holds aggregate counts.
type Summary struct {
	MemoryRecords        int `json:"memory_records"`
	MemoryProposals      int `json:"memory_proposals"`
	Observations         int `json:"observations"`
	ObservationProposals int `json:"observation_proposals"`
	Tensions             int `json:"tensions"`
	OpenTensions         int `json:"open_tensions"`
	Handoffs             int `json:"handoffs"`
	StaleHandoffs        int `json:"stale_handoffs"`
	Errors               int `json:"errors"`
}

// MemoryReport wraps the memory curator's output.
type MemoryReport struct {
	TotalRecords      int           `json:"total_records"`
	DemotionProposals int           `json:"demotion_proposals"`
	ArchiveProposals  int           `json:"archive_proposals"`
	DuplicateClusters int           `json:"duplicate_clusters"`
	OverlapClusters   int           `json:"overlap_clusters"`
	Applied           int           `json:"applied,omitempty"`
	AppliedSummary    *ApplySummary `json:"applied_summary,omitempty"`
}

// ApplySummary is set when the curator runs in apply mode.
type ApplySummary struct {
	Attempted int `json:"attempted"`
	Applied   int `json:"applied"`
	Skipped   int `json:"skipped"`
	Failed    int `json:"failed"`
}

// ---------------------------------------------------------------------------
// Worker — runs both active and dream loops concurrently
// ---------------------------------------------------------------------------

// Worker runs two concurrent curator loops: active (light, frequent) and
// dream (deep, infrequent). Both are leader-gated and configurable.
type Worker struct {
	cfg   Config
	memFn MemoryStoreOpener

	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	lastActiveReport *Report
	lastDreamReport  *Report
	lastActiveRunAt  time.Time
	lastDreamRunAt   time.Time
}

// MemoryStoreOpener opens a memory store.
type MemoryStoreOpener func(ctx context.Context, cfg config.Config) (storage.MemoryStore, error)

// NewWorker creates a curator worker with both loops.
func NewWorker(cfg Config, memFn MemoryStoreOpener) *Worker {
	return &Worker{
		cfg:   cfg,
		memFn: memFn,
	}
}

// Start begins both curator loops.
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

	// Active loop: immediate first run, then tick at ActiveInterval
	if w.cfg.ActiveEnabled {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.activeLoop(ctx)
		}()
	}

	// Dream loop: delay first run by 1 minute (let active run first),
	// then tick at DreamInterval
	if w.cfg.DreamEnabled {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			w.dreamLoop(ctx)
		}()
	}

	return nil
}

// Stop terminates both curator loops.
func (w *Worker) Stop() {
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}
	w.mu.Unlock()
	w.wg.Wait()
}

// LastActiveReport returns the most recent active-loop report.
func (w *Worker) LastActiveReport() *Report {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastActiveReport
}

// LastDreamReport returns the most recent dream-loop report.
func (w *Worker) LastDreamReport() *Report {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastDreamReport
}

// LastActiveRunAt returns the time of the most recent active run.
func (w *Worker) LastActiveRunAt() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastActiveRunAt
}

// LastDreamRunAt returns the time of the most recent dream run.
func (w *Worker) LastDreamRunAt() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastDreamRunAt
}

// ---------------------------------------------------------------------------
// Active loop — light, frequent checks
// ---------------------------------------------------------------------------

func (w *Worker) activeLoop(ctx context.Context) {
	// Immediate first run
	w.runActive(ctx)

	ticker := time.NewTicker(w.cfg.ActiveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runActive(ctx)
		}
	}
}

func (w *Worker) runActive(ctx context.Context) {
	report, err := w.execute(ctx, "active", false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curator [active]: run error: %v\n", err)
		return
	}
	w.mu.Lock()
	w.lastActiveReport = report
	w.lastActiveRunAt = time.Now().UTC()
	w.mu.Unlock()

	if report.Summary.MemoryProposals > 0 || report.Summary.StaleHandoffs > 0 {
		fmt.Fprintf(os.Stderr, "curator [active]: memory=%d proposals, handoffs=%d stale\n",
			report.Summary.MemoryProposals, report.Summary.StaleHandoffs)
	}
}

// ---------------------------------------------------------------------------
// Dream loop — deep, infrequent analysis
// ---------------------------------------------------------------------------

func (w *Worker) dreamLoop(ctx context.Context) {
	// Delay first run to let the active loop establish baseline
	select {
	case <-ctx.Done():
		return
	case <-time.After(1 * time.Minute):
	}

	w.runDream(ctx)

	ticker := time.NewTicker(w.cfg.DreamInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.runDream(ctx)
		}
	}
}

func (w *Worker) runDream(ctx context.Context) {
	report, err := w.execute(ctx, "dream", true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curator [dream]: run error: %v\n", err)
		return
	}
	w.mu.Lock()
	w.lastDreamReport = report
	w.lastDreamRunAt = time.Now().UTC()
	w.mu.Unlock()

	fmt.Fprintf(os.Stderr, "curator [dream]: memory=%d records, %d duplicates, %d overlaps, %d proposals\n",
		report.Summary.MemoryRecords,
		report.Memory.DuplicateClusters,
		report.Memory.OverlapClusters,
		report.Summary.MemoryProposals)
}

// ---------------------------------------------------------------------------
// Shared execution
// ---------------------------------------------------------------------------

func (w *Worker) execute(ctx context.Context, phase string, deep bool) (*Report, error) {
	now := time.Now().UTC()
	report := &Report{
		ID:          "curator-" + phase + "-" + now.Format("20060102T150405Z"),
		Phase:       phase,
		GeneratedAt: now,
		Config:      w.cfg,
	}

	if w.memFn != nil {
		memStore, err := w.memFn(ctx, config.Config{})
		if err != nil {
			fmt.Fprintf(os.Stderr, "curator [%s]: open memory store: %v\n", phase, err)
			report.Summary.Errors++
		} else {
			defer memStore.Close()
			memReport := w.curateMemory(ctx, memStore, deep)
			report.Memory = memReport
			report.Summary.MemoryRecords = memReport.TotalRecords
			report.Summary.MemoryProposals = memReport.DemotionProposals + memReport.ArchiveProposals
		}
	}

	return report, nil
}

func (w *Worker) curateMemory(ctx context.Context, memStore storage.MemoryStore, deep bool) *MemoryReport {
	report := &MemoryReport{}

	entries, err := memStore.List(ctx, "", 5000)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curator: list memory: %v\n", err)
		return report
	}

	var records []memorycore.Record
	for _, e := range entries {
		records = append(records, memorycore.RecordFromNamedEntry(e, memorycore.NamedEntryOptions{}))
	}
	report.TotalRecords = len(records)

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

	// Dream mode reports consolidation clusters
	if deep {
		report.DuplicateClusters = plan.Summary.DuplicateClusters
		report.OverlapClusters = plan.Summary.OverlapClusters
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
