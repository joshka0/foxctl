package updater

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// SessionProvider provides access to session data.
type SessionProvider interface {
	// ActiveSessions returns IDs of currently active sessions.
	ActiveSessions(ctx context.Context) ([]string, error)

	// RecentTurns returns the most recent turns for a session.
	RecentTurns(ctx context.Context, sessionID string, limit int) ([]Turn, error)

	// LastTurnID returns the ID of the most recent turn for a session.
	LastTurnID(ctx context.Context, sessionID string) (string, error)

	// GetSessionWorkspace returns the workspace path for a session.
	GetSessionWorkspace(ctx context.Context, sessionID string) (string, error)
}

// ContextFinder searches for relevant context.
type ContextFinder interface {
	// FindContext searches for relevant context based on analysis results.
	// The workspace parameter scopes memory searches to the session's workspace.
	FindContext(ctx context.Context, analysis *AnalysisResult, sessionID, workspace string) ([]ContextCandidate, error)
}

// ContextInjector delivers context to sessions.
type ContextInjector interface {
	// Inject delivers context to a session.
	// The workspace parameter scopes the injection to the session's workspace.
	Inject(ctx context.Context, sessionID, workspace string, candidate ContextCandidate, reason string) error
}

// Worker is the background context updater service.
type Worker struct {
	config   Config
	analyzer *AnalyzerWithAPIKey
	memory   *ShortTermMemory
	sessions SessionProvider
	finder   ContextFinder
	injector ContextInjector
	logger   *slog.Logger

	// State tracking
	mu           sync.RWMutex
	states       map[string]*SessionState // Per-session state
	metrics      WorkerMetrics
	running      bool
	shutdownOnce sync.Once
	done         chan struct{}
}

// NewWorker creates a new context updater worker.
func NewWorker(
	cfg Config,
	analyzer *AnalyzerWithAPIKey,
	sessions SessionProvider,
	finder ContextFinder,
	injector ContextInjector,
	logger *slog.Logger,
) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		config:   cfg,
		analyzer: analyzer,
		memory:   NewShortTermMemory(cfg.MemorySize, cfg.MemoryTTL),
		sessions: sessions,
		finder:   finder,
		injector: injector,
		logger:   logger.With("component", "context-updater"),
		states:   make(map[string]*SessionState),
		done:     make(chan struct{}),
	}
}

// Start begins the polling loop. Blocks until Stop is called.
func (w *Worker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.mu.Unlock()

	w.logger.Info("starting context updater worker",
		"poll_interval", w.config.PollInterval,
		"turn_window", w.config.TurnWindowSize,
		"provider", w.analyzer.Provider(),
	)

	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()

	// Prune memory periodically
	pruneTicker := time.NewTicker(5 * time.Minute)
	defer pruneTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("context updater stopping due to context cancellation")
			return ctx.Err()
		case <-w.done:
			w.logger.Info("context updater stopped")
			return nil
		case <-ticker.C:
			w.tick(ctx)
		case <-pruneTicker.C:
			pruned := w.memory.Prune()
			if pruned > 0 {
				w.logger.Debug("pruned short-term memory", "count", pruned)
			}
		}
	}
}

// Stop gracefully stops the worker.
func (w *Worker) Stop() {
	w.shutdownOnce.Do(func() {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
		close(w.done)
	})
}

// Running returns true if the worker is running.
func (w *Worker) Running() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.running
}

// Metrics returns the current worker metrics.
func (w *Worker) Metrics() WorkerMetrics {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.metrics
}

// tick processes one polling cycle.
func (w *Worker) tick(ctx context.Context) {
	w.mu.Lock()
	w.metrics.TickCount++
	tickNum := w.metrics.TickCount
	w.metrics.LastTickTime = time.Now()
	w.mu.Unlock()

	w.logger.Debug("tick", "tick_num", tickNum)

	// Get active sessions
	sessions, err := w.sessions.ActiveSessions(ctx)
	if err != nil {
		w.logger.Info("failed to get active sessions", "error", err)
		w.incrementErrorCount()
		return
	}

	w.logger.Debug("active sessions found", "count", len(sessions))

	if len(sessions) == 0 {
		return
	}

	// Process each session
	for _, sessionID := range sessions {
		if err := w.processSession(ctx, sessionID); err != nil {
			w.logger.Debug("failed to process session",
				"session_id", sessionID,
				"error", err,
			)
			w.incrementErrorCount()
		}
	}
}

// processSession processes a single session.
func (w *Worker) processSession(ctx context.Context, sessionID string) error {
	w.logger.Debug("processing session", "session_id", sessionID)

	// Get or create session state
	state := w.getOrCreateState(sessionID)

	// Check rate limit
	if !w.canInject(state) {
		w.logger.Debug("rate limited", "session_id", sessionID)
		return nil
	}

	// Get recent turns
	turns, err := w.sessions.RecentTurns(ctx, sessionID, w.config.TurnWindowSize)
	if err != nil {
		w.logger.Info("failed to get turns", "session_id", sessionID, "error", err)
		return err
	}

	w.logger.Debug("got turns", "session_id", sessionID, "count", len(turns))

	if len(turns) == 0 {
		return nil
	}

	// Check if there's new activity since last analysis
	lastTurnID, err := w.sessions.LastTurnID(ctx, sessionID)
	if err != nil {
		w.logger.Info("failed to get last turn ID", "session_id", sessionID, "error", err)
		return err
	}

	w.logger.Debug("checking activity", "session_id", sessionID, "last_turn_id", lastTurnID, "state_last_turn_id", state.LastTurnID)

	if lastTurnID == state.LastTurnID {
		// No new activity
		w.logger.Debug("no new activity", "session_id", sessionID)
		return nil
	}

	// Analyze the conversation
	w.logger.Debug("starting analysis", "session_id", sessionID, "turns", len(turns))
	startTime := time.Now()
	analysis, err := w.analyzer.Analyze(ctx, turns, state.LastAnalysis)
	llmLatency := time.Since(startTime)

	if err != nil {
		w.logger.Warn("analysis failed", "session_id", sessionID, "error", err, "latency_ms", llmLatency.Milliseconds())
		return err
	}
	w.logger.Debug("analysis complete", "session_id", sessionID, "latency_ms", llmLatency.Milliseconds())

	w.mu.Lock()
	w.metrics.AnalysisCount++
	// Update rolling average
	w.metrics.AverageLLMLatencyMs = (w.metrics.AverageLLMLatencyMs*0.9 + float64(llmLatency.Milliseconds())*0.1)
	w.mu.Unlock()

	// Update state
	state.LastTurnID = lastTurnID
	state.LastAnalysis = analysis
	state.LastAnalysisTime = time.Now()

	// Check if we should find and inject context
	shouldFind := w.shouldFindContext(analysis, state)
	w.logger.Debug("should find context", "session_id", sessionID, "should_find", shouldFind, "topics", analysis.Topics)
	if !shouldFind {
		return nil
	}

	// Get session workspace for scoped searches
	workspace, err := w.sessions.GetSessionWorkspace(ctx, sessionID)
	if err != nil {
		w.logger.Info("failed to get session workspace", "session_id", sessionID, "error", err)
		workspace = "" // Fall back to unscoped search
	}

	// Find relevant context
	w.logger.Debug("finding context", "session_id", sessionID, "workspace", workspace)
	candidates, err := w.finder.FindContext(ctx, analysis, sessionID, workspace)
	if err != nil {
		w.logger.Debug("context search failed", "session_id", sessionID, "error", err)
		return nil // Non-fatal
	}
	w.logger.Debug("found candidates", "session_id", sessionID, "count", len(candidates))

	// Filter and inject context
	for _, candidate := range candidates {
		w.logger.Debug("evaluating candidate",
			"session_id", sessionID,
			"candidate_id", candidate.ID,
			"type", candidate.Type,
			"score", candidate.Score,
			"threshold", w.config.ConfidenceMin,
		)
		if candidate.Score < w.config.ConfidenceMin {
			continue
		}

		// Check if recently injected
		if w.memory.WasRecentlyInjected(candidate.ID, candidate.Content, sessionID) {
			w.logger.Debug("candidate recently injected, skipping",
				"session_id", sessionID,
				"candidate_id", candidate.ID,
			)
			continue
		}

		// Inject
		reason := buildInjectionReason(candidate, analysis)
		if err := w.injector.Inject(ctx, sessionID, workspace, candidate, reason); err != nil {
			w.logger.Warn("injection failed",
				"session_id", sessionID,
				"candidate_id", candidate.ID,
				"error", err,
			)
			continue
		}

		// Record injection
		w.memory.Record(candidate.ID, candidate.Content, sessionID, analysis.Topics)
		state.InjectionCount++

		w.mu.Lock()
		w.metrics.InjectionCount++
		w.mu.Unlock()

		w.logger.Info("injected context",
			"session_id", sessionID,
			"candidate_id", candidate.ID,
			"type", candidate.Type,
			"score", candidate.Score,
		)

		// Only inject one per tick to avoid overwhelming the user
		break
	}

	return nil
}

// getOrCreateState returns the state for a session, creating it if needed.
func (w *Worker) getOrCreateState(sessionID string) *SessionState {
	w.mu.Lock()
	defer w.mu.Unlock()

	state, ok := w.states[sessionID]
	if !ok {
		state = &SessionState{
			SessionID:   sessionID,
			WindowStart: time.Now(),
		}
		w.states[sessionID] = state
	}
	return state
}

// canInject checks if we can inject based on rate limiting.
func (w *Worker) canInject(state *SessionState) bool {
	// Reset window if expired
	if time.Since(state.WindowStart) > w.config.RateWindow {
		state.InjectionCount = 0
		state.WindowStart = time.Now()
	}

	return state.InjectionCount < w.config.MaxInjectionRate
}

// shouldFindContext decides if we should search for context.
func (w *Worker) shouldFindContext(analysis *AnalysisResult, state *SessionState) bool {
	// Always search if drift detected
	if analysis.DriftDetected {
		return true
	}

	// Search if confidence is high
	if analysis.Confidence >= w.config.ConfidenceMin {
		return true
	}

	// Periodic search (every 5 analyses)
	return state.InjectionCount == 0 && w.metrics.AnalysisCount%5 == 0
}

func (w *Worker) incrementErrorCount() {
	w.mu.Lock()
	w.metrics.ErrorCount++
	w.mu.Unlock()
}

// buildInjectionReason creates a human-readable reason for the injection.
func buildInjectionReason(candidate ContextCandidate, analysis *AnalysisResult) string {
	switch candidate.Type {
	case "memory":
		return "Related to current work on " + analysis.Intent
	case "session":
		return "From previous session working on similar topic"
	case "codemap":
		return "Code relationship relevant to " + candidate.Query
	default:
		return "Potentially relevant context"
	}
}
