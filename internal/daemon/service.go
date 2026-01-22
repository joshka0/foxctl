// Package daemon provides a persistent agentctl service for fast skill execution.
//
// The daemon pre-loads configuration, opens database connections, and maintains
// warm resources to reduce hook latency from ~300ms to <50ms.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/runtime"
	"github.com/oklog/ulid/v2"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/context/updater"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/execution/runner"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/indexing/filesummary"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/lsp/gopls"
	"github.com/jkatigb/agentctl/internal/platform/config"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/queue"
	"github.com/jkatigb/agentctl/internal/sessionkit/summary"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/cache"
	"github.com/jkatigb/agentctl/internal/storage/contextbuffer"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/rs/zerolog"
)

// ServiceOptions configures the daemon service.
type ServiceOptions struct {
	// Workspace to pre-warm (optional).
	Workspace string
}

// Service is the main daemon service.
type Service struct {
	cfg      config.Config
	opts     ServiceOptions
	listener net.Listener
	started  time.Time

	// Skill resolution
	skillResolver *SkillResolver

	// Shared resources
	cacheStore *cache.Store
	cacheMu    sync.RWMutex
	dbPool     *sqliteutil.Pool

	// Warm workspaces (gopls ready)
	warmWorkspaces map[string]bool
	warmMu         sync.RWMutex

	// Metrics
	requestCount atomic.Int64

	// Shutdown coordination
	shutdownCh   chan struct{}
	shutdownOnce sync.Once
	shutdownMu   sync.Mutex // protects shutdown state for acceptLoop
	isShutdown   bool       // set to true when shutdownCh is closed
	wg           sync.WaitGroup

	// Background workers
	summaryWorker *summary.Worker
	summaryCtx    context.Context
	summaryCancel context.CancelFunc

	// Context updater worker (proactive context surfacing)
	contextUpdater       *updater.Worker
	contextUpdaterCtx    context.Context
	contextUpdaterCancel context.CancelFunc

	// File summary worker (background LLM summaries)
	fileSummaryWorker       *filesummary.Worker
	fileSummaryWorkerCtx    context.Context
	fileSummaryWorkerCancel context.CancelFunc
	fileSummaryMemoryStore  storage.MemoryStore // Memory store for file summary worker (close on shutdown)

	// Agent orchestration
	agentRuntime      *runtime.Runtime
	agentOverseer     *runtime.Overseer
	agentCtx          context.Context
	agentCancel       context.CancelFunc
	agentSessionStore *sessions.Store       // Session store for agent persistence (close on shutdown)
	agentMailboxStore mailbox.Store         // Mailbox store for agent messaging (close on shutdown)
	agentBoardStore   blackboard.BoardStore // Blackboard store for agent coordination (close on shutdown)
}

// NewService creates a new daemon service.
func NewService(cfg config.Config, opts ServiceOptions) (*Service, error) {
	// Load environment variables from .env files
	config.LoadDotEnv()

	// Create connection pool and set as global
	pool := sqliteutil.NewPool()
	sqliteutil.SetGlobalPool(pool)

	svc := &Service{
		cfg:            cfg,
		opts:           opts,
		started:        time.Now(),
		skillResolver:  NewSkillResolver(cfg),
		warmWorkspaces: make(map[string]bool),
		shutdownCh:     make(chan struct{}),
		dbPool:         pool,
	}

	// Pre-warm workspace if specified
	if opts.Workspace != "" {
		svc.wg.Add(1)
		go svc.warmWorkspace(opts.Workspace)
	}

	return svc, nil
}

// Run starts the daemon service and blocks until shutdown.
func (s *Service) Run(ctx context.Context) error {
	// Remove stale socket
	socketPath := SocketPath()
	_ = os.Remove(socketPath)

	// Ensure socket directory exists
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}

	// Create listener
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", socketPath, err)
	}
	s.listener = listener

	// Set socket permissions (user-only)
	if err := os.Chmod(socketPath, 0o600); err != nil {
		listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}

	// Write PID file
	if err := s.writePIDFile(); err != nil {
		listener.Close()
		return fmt.Errorf("write pid file: %w", err)
	}
	defer s.removePIDFile()

	// Start background summary worker
	if err := s.startSummaryWorker(ctx); err != nil {
		// Log but don't fail daemon startup - worker is optional
		fmt.Fprintf(os.Stderr, "warning: summary worker failed to start: %v\n", err)
	}

	// Start background context updater worker
	if err := s.startContextUpdater(ctx); err != nil {
		// Log but don't fail daemon startup - worker is optional
		fmt.Fprintf(os.Stderr, "warning: context updater failed to start: %v\n", err)
	}

	// Start background file summary worker
	if err := s.startFileSummaryWorker(ctx); err != nil {
		// Log but don't fail daemon startup - worker is optional
		fmt.Fprintf(os.Stderr, "warning: file summary worker failed to start: %v\n", err)
	}

	// Start agent orchestration (runtime + overseer)
	if err := s.startAgentOrchestration(ctx); err != nil {
		// Log but don't fail daemon startup - orchestration is optional
		fmt.Fprintf(os.Stderr, "warning: agent orchestration failed to start: %v\n", err)
	}

	// Accept connections
	go s.acceptLoop(ctx)

	// Wait for shutdown signal
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.shutdownCh:
		return nil
	}
}

// Shutdown gracefully shuts down the daemon.
func (s *Service) Shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() {
		s.shutdownMu.Lock()
		s.isShutdown = true
		s.shutdownMu.Unlock()
		close(s.shutdownCh)
	})

	if s.listener != nil {
		s.listener.Close()
	}

	// Wait for in-flight requests with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Stop summary worker
	if s.summaryWorker != nil {
		if err := s.summaryWorker.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: summary worker stop error: %v\n", err)
		}
	}
	if s.summaryCancel != nil {
		s.summaryCancel()
	}

	// Stop context updater worker
	if s.contextUpdater != nil {
		s.contextUpdater.Stop()
	}
	if s.contextUpdaterCancel != nil {
		s.contextUpdaterCancel()
	}

	// Stop file summary worker
	if s.fileSummaryWorker != nil {
		if err := s.fileSummaryWorker.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: file summary worker stop error: %v\n", err)
		}
	}
	if s.fileSummaryWorkerCancel != nil {
		s.fileSummaryWorkerCancel()
	}

	// Stop agent orchestration
	if s.agentCancel != nil {
		s.agentCancel()
	}
	// Kill all running agent sessions
	if s.agentRuntime != nil {
		for _, session := range s.agentRuntime.List() {
			_ = s.agentRuntime.Kill(session.ID)
		}
	}

	// Close file summary worker memory store
	if s.fileSummaryMemoryStore != nil {
		s.fileSummaryMemoryStore.Close()
		s.fileSummaryMemoryStore = nil
	}

	// Close agent orchestration stores
	if s.agentSessionStore != nil {
		s.agentSessionStore.Close()
		s.agentSessionStore = nil
	}
	if s.agentMailboxStore != nil {
		s.agentMailboxStore.Close()
		s.agentMailboxStore = nil
	}
	if s.agentBoardStore != nil {
		s.agentBoardStore.Close()
		s.agentBoardStore = nil
	}

	// Close shared resources
	s.cacheMu.Lock()
	if s.cacheStore != nil {
		s.cacheStore.Close()
		s.cacheStore = nil
	}
	s.cacheMu.Unlock()

	// Close connection pool
	if s.dbPool != nil {
		s.dbPool.Close()
		sqliteutil.SetGlobalPool(nil)
	}

	// Remove socket
	_ = os.Remove(SocketPath())

	return nil
}

// acceptLoop accepts connections and handles them.
func (s *Service) acceptLoop(ctx context.Context) {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.shutdownCh:
				return
			default:
				// Transient error, continue
				continue
			}
		}

		// Atomically check shutdown and add to wait group to avoid race with Shutdown()
		s.shutdownMu.Lock()
		if s.isShutdown {
			s.shutdownMu.Unlock()
			conn.Close()
			return
		}
		s.wg.Add(1)
		s.shutdownMu.Unlock()

		go func() {
			defer s.wg.Done()
			s.handleConnection(ctx, conn)
		}()
	}
}

// Request is the JSON-RPC-like request format.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	ID     string          `json:"id,omitempty"`
}

// Response is the JSON-RPC-like response format.
type Response struct {
	ID     string `json:"id,omitempty"`
	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

// Error represents an error response.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// handleConnection handles a single client connection.
func (s *Service) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// Set read deadline
	if err := conn.SetReadDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return // Connection already closed or broken
	}

	s.requestCount.Add(1)

	// Read request
	decoder := json.NewDecoder(conn)
	var req Request
	if err := decoder.Decode(&req); err != nil {
		s.writeError(conn, req.ID, "EPARSE", fmt.Sprintf("parse request: %v", err))
		return
	}

	// Route request
	var resp Response
	resp.ID = req.ID

	switch req.Method {
	case "status":
		resp.Result = s.handleStatus()
	case "run":
		result, err := s.handleRun(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "ERUN", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "warm":
		result, err := s.handleWarm(req.Params)
		if err != nil {
			resp.Error = &Error{Code: "EWARM", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "shutdown":
		resp.Result = map[string]any{"status": "shutting_down"}
		s.writeResponse(conn, resp)
		go func() {
			time.Sleep(100 * time.Millisecond)
			s.shutdownOnce.Do(func() {
				close(s.shutdownCh)
			})
		}()
		return
	case "agent.spawn":
		result, err := s.handleAgentSpawn(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "ESPAWN", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.list":
		resp.Result = s.handleAgentList()
	case "agent.status":
		result, err := s.handleAgentStatus(req.Params)
		if err != nil {
			resp.Error = &Error{Code: "ESTATUS", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.kill":
		result, err := s.handleAgentKill(req.Params)
		if err != nil {
			resp.Error = &Error{Code: "EKILL", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.resume":
		result, err := s.handleAgentResume(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "ERESUME", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.hierarchy":
		result, err := s.handleAgentHierarchy(req.Params)
		if err != nil {
			resp.Error = &Error{Code: "EHIERARCHY", Message: err.Error()}
		} else {
			resp.Result = result
		}
	default:
		resp.Error = &Error{Code: "EMETHOD", Message: fmt.Sprintf("unknown method: %s", req.Method)}
	}

	s.writeResponse(conn, resp)
}

// StatusResult contains daemon status information.
type StatusResult struct {
	PID            int      `json:"pid"`
	StartedAt      string   `json:"started_at"`
	UptimeSeconds  float64  `json:"uptime_seconds"`
	RequestCount   int64    `json:"request_count"`
	WarmWorkspaces []string `json:"warm_workspaces"`
}

func (s *Service) handleStatus() StatusResult {
	s.warmMu.RLock()
	workspaces := make([]string, 0, len(s.warmWorkspaces))
	for ws := range s.warmWorkspaces {
		workspaces = append(workspaces, ws)
	}
	s.warmMu.RUnlock()

	return StatusResult{
		PID:            os.Getpid(),
		StartedAt:      s.started.Format(time.RFC3339),
		UptimeSeconds:  time.Since(s.started).Seconds(),
		RequestCount:   s.requestCount.Load(),
		WarmWorkspaces: workspaces,
	}
}

// RunParams are the parameters for the run method.
type RunParams struct {
	Skill     string          `json:"skill"`
	Input     json.RawMessage `json:"input"`
	Workspace string          `json:"workspace,omitempty"`
	Ephemeral bool            `json:"ephemeral,omitempty"`
}

// RunResult is the result of a skill execution.
type RunResult struct {
	Output   json.RawMessage `json:"output"`
	Cached   bool            `json:"cached,omitempty"`
	Duration float64         `json:"duration_ms"`
}

func (s *Service) handleRun(ctx context.Context, params json.RawMessage) (*RunResult, error) {
	start := time.Now()

	var p RunParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.Skill == "" {
		return nil, errors.New("skill is required")
	}

	// Resolve skill
	handle, err := s.skillResolver.Resolve(p.Skill)
	if err != nil {
		return nil, fmt.Errorf("resolve skill %s: %w", p.Skill, err)
	}

	// Build extra env (session propagation)
	extraEnv := s.buildSkillEnv(p.Workspace)

	// Prepare input (default to empty object if nil)
	input := p.Input
	if len(input) == 0 {
		input = []byte("{}")
	}

	// Execute skill
	stdout, stderr, err := runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     handle.Manifest,
		ArtifactPath: handle.ArtifactPath,
		Input:        input,
		ExtraEnv:     extraEnv,
	})
	// Handle execution error
	if err != nil {
		// If there's stderr output, include it in the error envelope
		if len(stderr) > 0 {
			errMsg := fmt.Sprintf("%v: %s", err, string(stderr))
			errEnv := envelope.Error(p.Skill, "EEXEC", errMsg, nil)
			output, _ := json.Marshal(errEnv)
			return &RunResult{Output: output, Duration: ms(start)}, nil
		}
		errEnv := envelope.Error(p.Skill, "EEXEC", err.Error(), nil)
		output, _ := json.Marshal(errEnv)
		return &RunResult{Output: output, Duration: ms(start)}, nil
	}

	// Return skill output
	// If stdout is empty, return a generic success envelope
	if len(stdout) == 0 {
		succEnv := envelope.OK(p.Skill, nil)
		output, _ := json.Marshal(succEnv)
		return &RunResult{Output: output, Duration: ms(start)}, nil
	}

	return &RunResult{
		Output:   stdout,
		Duration: ms(start),
	}, nil
}

// buildSkillEnv constructs environment variables for skill execution.
func (s *Service) buildSkillEnv(workspace string) []string {
	var env []string
	if workspace != "" {
		env = append(env, "AGENTCTL_WORKSPACE="+workspace)
	}
	// Propagate session vars from parent environment
	for _, key := range []string{
		"AGENTCTL_SESSION_ID", "CLAUDE_SESSION_ID",
		"AGENTCTL_AGENT_ID", "AGENTCTL_TRACE_ID",
	} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// ms converts duration since start to milliseconds.
func ms(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000.0
}

// WarmParams are the parameters for the warm method.
type WarmParams struct {
	Workspace string `json:"workspace"`
}

func (s *Service) handleWarm(params json.RawMessage) (map[string]any, error) {
	var p WarmParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.Workspace == "" {
		return nil, errors.New("workspace is required")
	}

	go s.warmWorkspace(p.Workspace)

	return map[string]any{
		"status":    "warming",
		"workspace": p.Workspace,
	}, nil
}

// warmWorkspace pre-warms resources for a workspace.
func (s *Service) warmWorkspace(workspace string) {
	defer s.wg.Done()

	// Check if already warm
	s.warmMu.RLock()
	if s.warmWorkspaces[workspace] {
		s.warmMu.RUnlock()
		return
	}
	s.warmMu.RUnlock()

	// Start gopls daemon for Go projects
	if _, err := os.Stat(filepath.Join(workspace, "go.mod")); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = gopls.GetDaemon(ctx, workspace)
	}

	// Mark as warm
	s.warmMu.Lock()
	s.warmWorkspaces[workspace] = true
	s.warmMu.Unlock()
}

func (s *Service) writeResponse(conn net.Conn, resp Response) {
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	encoder := json.NewEncoder(conn)
	_ = encoder.Encode(resp) // Best effort; connection may already be closed
}

func (s *Service) writeError(conn net.Conn, id, code, message string) {
	s.writeResponse(conn, Response{
		ID:    id,
		Error: &Error{Code: code, Message: message},
	})
}

// PID file management

func (s *Service) writePIDFile() error {
	pidPath := PIDPath()
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0o600)
}

func (s *Service) removePIDFile() {
	_ = os.Remove(PIDPath())
}

// getCacheStore returns the shared cache store, opening it if needed.
// TODO: Used when handleRun implements full skill execution.
func (s *Service) getCacheStore(ctx context.Context) (*cache.Store, error) { //nolint:unused // Will be used when skill execution is implemented
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	if s.cacheStore != nil {
		return s.cacheStore, nil
	}

	store, err := cache.Open(ctx, s.cfg.Paths.Cache, cache.Options{
		AutoTTL: s.cfg.Memory.AutoCacheTTL,
		CASPath: s.cfg.Paths.CAS,
	})
	if err != nil {
		return nil, err
	}

	s.cacheStore = store
	return store, nil
}

// Daemonize forks the current process to run in background.
func Daemonize() error {
	// Check if we're already daemonized
	if os.Getenv("AGENTCTL_DAEMON_CHILD") == "1" {
		return nil
	}

	// Get current executable
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable: %w", err)
	}

	// Build args (replace --background with child marker)
	args := []string{"daemon", "start"}
	for _, arg := range os.Args[2:] {
		if arg != "-b" && arg != "--background" {
			args = append(args, arg)
		}
	}

	// Start child process
	cmd := &exec{
		Path: exe,
		Args: append([]string{exe}, args...),
		Env:  append(os.Environ(), "AGENTCTL_DAEMON_CHILD=1"),
	}

	// Detach from terminal
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open /dev/null: %w", err)
	}
	defer devNull.Close()

	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// Don't wait for child - let it run independently
	return nil
}

// startSummaryWorker initializes and starts the background summary worker.
func (s *Service) startSummaryWorker(ctx context.Context) error {
	// Check if LLM providers are available
	providers := llmproviders.SummarizationProviders()
	if len(providers) == 0 {
		// No providers configured - skip worker silently
		return nil
	}

	// Open queue store
	queueDBPath := filepath.Join(s.cfg.Storage.Root, summary.QueueDBName)
	queueStore, err := queue.Open(ctx, queueDBPath, queue.Options{Table: summary.QueueTable})
	if err != nil {
		return fmt.Errorf("open queue store: %w", err)
	}

	// Open session store
	sessionStore, err := sessions.OpenFromConfig(ctx, s.cfg)
	if err != nil {
		queueStore.Close()
		return fmt.Errorf("open session store: %w", err)
	}

	// Create worker with default config
	workerCfg := summary.DefaultWorkerConfig()
	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

	worker := summary.NewWorker(
		workerCfg,
		queueStore,
		sessionStore,
		providers,
		s.cfg,
		logger,
	)

	// Create cancellable context for worker
	s.summaryCtx, s.summaryCancel = context.WithCancel(ctx)

	// Start the worker
	if err := worker.Start(s.summaryCtx); err != nil {
		s.summaryCancel()
		queueStore.Close()
		sessionStore.Close()
		return fmt.Errorf("start worker: %w", err)
	}

	s.summaryWorker = worker
	return nil
}

// startContextUpdater initializes and starts the background context updater worker.
func (s *Service) startContextUpdater(ctx context.Context) error {
	// Check if cheap LLM providers are available
	if !updater.Available(s.cfg) {
		// No providers configured - skip worker silently
		fmt.Fprintf(os.Stderr, "context updater: skipped (no LLM providers configured)\n")
		return nil
	}
	fmt.Fprintf(os.Stderr, "context updater: LLM providers available, starting...\n")

	// Create slog logger from zerolog-style output
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Open session store for session provider
	sessionStore, err := sessions.OpenFromConfig(ctx, s.cfg)
	if err != nil {
		return fmt.Errorf("open session store: %w", err)
	}

	// Open memory store for context finder
	memoryStore, err := memory.OpenWithConfig(ctx, s.cfg)
	if err != nil {
		sessionStore.Close()
		return fmt.Errorf("open memory store: %w", err)
	}

	// Open contextbuffer store for context injector
	// This is the preferred approach - contextbuffer is designed for hook context injection
	ctxBufferStore, err := contextbuffer.Open(ctx, s.cfg.Storage.Root)
	if err != nil {
		sessionStore.Close()
		memoryStore.Close()
		return fmt.Errorf("open context buffer: %w", err)
	}

	// Get workspace (use opts.Workspace if set, or default to empty for all workspaces)
	workspace := s.opts.Workspace

	// Create embedding provider for semantic memory search
	// Pass API keys from environment since config doesn't store them
	var embedder semantic.EmbeddingProvider
	embedder, err = semantic.NewProviderForScope(
		semantic.ScopeMemory,
		s.cfg,
		semantic.WithVoyageKey(os.Getenv("VOYAGE_API_KEY")),
		semantic.WithGeminiKey(os.Getenv("GEMINI_API_KEY")),
	)
	if err != nil {
		logger.Warn("embedding provider not available, using text search", "error", err)
		embedder = nil // Fall back to text search
	} else {
		logger.Info("semantic search enabled", "provider", embedder.Model())
	}

	// Create adapters
	sessionAdapter := updater.NewSessionStoreAdapter(sessionStore, workspace)
	memoryAdapter := updater.NewMemoryStoreAdapter(memoryStore, embedder, workspace, logger)
	sessionLearningsAdapter := updater.NewSessionLearningsAdapter(sessionStore, workspace)

	// Create combined finder (no codemap searcher for now)
	finder := updater.NewCombinedFinder(memoryAdapter, sessionLearningsAdapter, nil)

	// Create context buffer injector and wrap in Injector
	ctxBufferInjector := updater.NewContextBufferInjector(ctxBufferStore, workspace)
	injector := updater.NewInjector(ctxBufferInjector, updater.DefaultInjectorConfig())

	// Create worker with real providers
	worker, err := updater.NewWorkerFromConfig(updater.DaemonConfig{
		Config:   s.cfg,
		Logger:   logger,
		Sessions: sessionAdapter,
		Finder:   finder,
		Injector: injector,
	})
	if err != nil {
		sessionStore.Close()
		memoryStore.Close()
		ctxBufferStore.Close()
		return fmt.Errorf("create context updater: %w", err)
	}
	if worker == nil {
		// No LLM available
		sessionStore.Close()
		memoryStore.Close()
		ctxBufferStore.Close()
		return nil
	}

	// Create cancellable context for worker
	s.contextUpdaterCtx, s.contextUpdaterCancel = context.WithCancel(ctx)

	// Start the worker in a goroutine
	go func() {
		if err := worker.Start(s.contextUpdaterCtx); err != nil && err != context.Canceled {
			fmt.Fprintf(os.Stderr, "context updater error: %v\n", err)
		}
	}()

	s.contextUpdater = worker
	fmt.Fprintf(os.Stderr, "context updater: started successfully\n")
	return nil
}

// startFileSummaryWorker initializes and starts the background file summary worker.
func (s *Service) startFileSummaryWorker(ctx context.Context) error {
	// Check if we have a workspace
	workspace := s.opts.Workspace
	if workspace == "" {
		fmt.Fprintf(os.Stderr, "file summary worker: skipped (no workspace)\n")
		return nil
	}

	// Check if LLM providers are available for summarization
	// Use FileSummaryProviders which prioritizes Devstral for code summaries
	providers := llmproviders.FileSummaryProviders()
	if len(providers) == 0 {
		fmt.Fprintf(os.Stderr, "file summary worker: skipped (no LLM providers configured)\n")
		return nil
	}

	// Get the cheap/fast LLM for summaries
	llm := llmproviders.NewSummaryLLM(providers[0])
	if llm == nil {
		fmt.Fprintf(os.Stderr, "file summary worker: skipped (no LLM available)\n")
		return nil
	}

	// Open memory store (store in Service for cleanup on shutdown)
	memoryStore, err := memory.Open(ctx, s.cfg.Storage.Root, s.cfg.Paths.CAS)
	if err != nil {
		return fmt.Errorf("open memory store: %w", err)
	}
	s.fileSummaryMemoryStore = memoryStore

	// Get embedding provider (optional) with config-aware model selection
	var embedProvider semantic.EmbeddingProvider
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if voyageKey != "" || geminiKey != "" {
		provider, err := semantic.NewProviderForScope(
			semantic.ScopeFileSummaries,
			s.cfg,
			semantic.WithVoyageKey(voyageKey),
			semantic.WithGeminiKey(geminiKey),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "file summary worker: embedding provider unavailable: %v\n", err)
		} else {
			embedProvider = provider
		}
	}

	// Create worker (uses default logger and observability events)
	workerCfg := filesummary.DefaultWorkerConfig()

	worker := filesummary.NewWorker(
		workerCfg,
		memoryStore,
		llm,
		embedProvider,
		workspace,
		nil, // Use default logger
	)

	// Create cancellable context for worker
	s.fileSummaryWorkerCtx, s.fileSummaryWorkerCancel = context.WithCancel(ctx)

	// Start the worker
	if err := worker.Start(s.fileSummaryWorkerCtx); err != nil {
		s.fileSummaryWorkerCancel()
		s.fileSummaryMemoryStore.Close()
		s.fileSummaryMemoryStore = nil
		return fmt.Errorf("start file summary worker: %w", err)
	}

	s.fileSummaryWorker = worker
	fmt.Fprintf(os.Stderr, "file summary worker: started for %s\n", workspace)
	return nil
}

// startAgentOrchestration initializes the agent runtime and overseer.
func (s *Service) startAgentOrchestration(ctx context.Context) error {
	// Determine LLM configuration from centralized config
	// Priority: configured provider > CEREBRAS > OPENROUTER > GROQ > GEMINI
	llmProvider, llmAPIKey, llmModel := s.resolveLLMConfig()

	if llmAPIKey == "" {
		fmt.Fprintf(os.Stderr, "agent orchestration: skipped (no LLM API key configured)\n")
		return nil
	}

	// Create cancellable context
	s.agentCtx, s.agentCancel = context.WithCancel(ctx)

	// Open sessions store for persistence (store in Service for cleanup on shutdown)
	sessionStore, err := sessions.OpenFromConfig(ctx, s.cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: session store open failed: %v\n", err)
		// Continue without persistence - agents will be in-memory only
		sessionStore = nil
	}
	s.agentSessionStore = sessionStore

	// Open mailbox store for inter-agent messaging (store in Service for cleanup on shutdown)
	var mailboxStore runtime.MailboxStore
	if mb, err := mailbox.Open(ctx, s.cfg.Storage.Root); err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: mailbox store open failed: %v\n", err)
	} else {
		s.agentMailboxStore = mb
		mailboxStore = mb
	}

	// Open blackboard store for workspace coordination (store in Service for cleanup on shutdown)
	var boardStore runtime.BoardStore
	if bb, err := blackboard.OpenBoardStore(ctx, s.cfg.Storage.Root); err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: board store open failed: %v\n", err)
	} else {
		s.agentBoardStore = bb
		boardStore = bb
	}

	// Create memory store opener for agent tools
	openMemoryStore := func(ctx context.Context) (storage.MemoryStore, error) {
		return memory.OpenWithConfig(ctx, s.cfg)
	}

	// Load hook configuration and create dispatcher for agent tools
	var hookDispatcher hooks.Dispatcher
	hookCfg, err := hooks.LoadConfigWithDefaults(s.opts.Workspace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: hook config load failed: %v\n", err)
		hookCfg = hooks.EmptyConfig()
	}
	hookDispatcher = hooks.NewDispatcherWithRegistry(hookCfg, s.cfg.Paths.Skills)

	// Create action executor for processing hook output actions
	// Use concrete store fields (not runtime interface variables) for full interface compliance
	actionExecutor := hooks.NewExecutor(hooks.ExecutorConfig{
		MailboxStore: s.agentMailboxStore,
		BoardStore:   s.agentBoardStore,
		FailOpen:     true, // Don't block on action errors
	})

	// Create runtime config
	runtimeCfg := runtime.Config{
		DefaultMaxIterations: 50,
		DefaultTimeout:       30 * time.Minute,
		LLMProvider:          llmProvider,
		LLMModel:             llmModel,
		LLMAPIKey:            llmAPIKey,
		WorkspaceRoot:        s.opts.Workspace,
		DefaultMaxDepth:      3,
		OpenMemoryStore:      openMemoryStore,
		SessionStore:         sessionStore,
		MailboxStore:         mailboxStore,
		BoardStore:           boardStore,
		HookDispatcher:       hookDispatcher,
		ActionExecutor:       actionExecutor,
	}

	// Create runtime
	s.agentRuntime = runtime.NewRuntime(runtimeCfg)

	// Recover stale sessions from previous daemon run
	if sessionStore != nil {
		if err := s.agentRuntime.RecoverSessions(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "agent orchestration: session recovery failed: %v\n", err)
		}
	}

	// Create overseer config
	overseerCfg := runtime.OverseerConfig{
		MaxDepth:            3,
		MaxConcurrentAgents: 10,
	}

	// Create overseer
	s.agentOverseer = runtime.NewOverseer(s.agentRuntime, overseerCfg)

	fmt.Fprintf(os.Stderr, "agent orchestration: started (provider=%s, model=%s)\n", llmProvider, llmModel)
	return nil
}

// --- Agent RPC Handlers ---

// AgentSpawnParams are the parameters for agent.spawn.
type AgentSpawnParams struct {
	Role        string `json:"role"`
	AgentID     string `json:"agent_id,omitempty"` // Agent config ID for session filtering
	WorkspaceID string `json:"workspace_id,omitempty"`
	EpicID      string `json:"epic_id,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	Prompt      string `json:"prompt,omitempty"`

	// Agent metadata
	Name string `json:"name,omitempty"`
	Slug string `json:"slug,omitempty"`

	// Execution config
	MaxIterations    int    `json:"max_iterations,omitempty"`
	MaxContextTokens int    `json:"max_context_tokens,omitempty"` // Context budget (0=no limit)
	ExecMode         string `json:"exec_mode,omitempty"`          // "reactive", "autonomous", "proactive"
	MaxAutoTurns     int    `json:"max_auto_turns,omitempty"`

	// LLM override
	LLMProvider string `json:"llm_provider,omitempty"`
	LLMModel    string `json:"llm_model,omitempty"`
	LLMAPIKey   string `json:"llm_api_key,omitempty"`
}

// AgentSpawnResult is the result of spawning an agent.
type AgentSpawnResult struct {
	SessionID string `json:"session_id"`
	ActorID   string `json:"actor_id"`
	Status    string `json:"status"`
	Role      string `json:"role"`
	NS        string `json:"ns,omitempty"` // Namespace for mailbox routing
}

func (s *Service) handleAgentSpawn(ctx context.Context, params json.RawMessage) (*AgentSpawnResult, error) {
	if s.agentRuntime == nil {
		return nil, errors.New("agent orchestration not initialized")
	}

	var p AgentSpawnParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.Role == "" {
		return nil, errors.New("role is required")
	}

	// Generate actor ID if not provided
	actorID := p.AgentID
	if actorID == "" {
		actorID = "actor:" + p.Role + ":" + ulid.Make().String()
	}

	// Create agent config
	cfg := types.AgentConfig{
		Role:        types.AgentRole(p.Role),
		ActorID:     actorID,
		WorkspaceID: p.WorkspaceID,
		EpicID:      p.EpicID,
		TaskID:      p.TaskID,
		Prompt:      p.Prompt,
	}

	// Apply execution config if provided
	if p.MaxIterations > 0 {
		cfg.MaxIterations = p.MaxIterations
	}
	if p.MaxContextTokens > 0 {
		cfg.MaxContextTokens = p.MaxContextTokens
	}
	if p.ExecMode != "" {
		switch p.ExecMode {
		case string(agent.ModeReactive), string(agent.ModeAutonomous), string(agent.ModeProactive):
			cfg.ExecMode = agent.ExecutionMode(p.ExecMode)
		default:
			return nil, fmt.Errorf("invalid exec_mode: %s", p.ExecMode)
		}
	}
	if p.MaxAutoTurns > 0 {
		cfg.MaxAutoTurns = p.MaxAutoTurns
	}

	// Apply LLM override if provided
	if p.LLMProvider != "" {
		cfg.LLMProvider = p.LLMProvider
	}
	if p.LLMModel != "" {
		cfg.LLMModel = p.LLMModel
	}
	if p.LLMAPIKey != "" {
		cfg.LLMAPIKey = p.LLMAPIKey
	}

	// Spawn the agent
	session, err := s.agentRuntime.Spawn(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("spawn agent: %w", err)
	}

	return &AgentSpawnResult{
		SessionID: session.ID,
		ActorID:   session.Config.ActorID,
		Status:    string(session.Status),
		Role:      string(session.Config.Role),
		NS:        session.Config.ActorID, // Use ActorID as namespace for mailbox
	}, nil
}

// AgentResumeParams are the parameters for agent.resume.
type AgentResumeParams struct {
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
}

// AgentResumeResult is the result of resuming an agent session.
type AgentResumeResult struct {
	SessionID   string `json:"session_id"`
	ActorID     string `json:"actor_id"`
	Status      string `json:"status"`
	FromSession string `json:"from_session"`
}

func (s *Service) handleAgentResume(ctx context.Context, params json.RawMessage) (*AgentResumeResult, error) {
	if s.agentRuntime == nil {
		return nil, errors.New("agent orchestration not initialized")
	}

	var p AgentResumeParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.SessionID == "" {
		return nil, errors.New("session_id is required")
	}
	if p.Prompt == "" {
		return nil, errors.New("prompt is required")
	}

	session, err := s.agentRuntime.Resume(ctx, p.SessionID, p.Prompt)
	if err != nil {
		return nil, fmt.Errorf("resume session: %w", err)
	}

	return &AgentResumeResult{
		SessionID:   session.ID,
		ActorID:     session.Config.ActorID,
		Status:      string(session.Status),
		FromSession: p.SessionID,
	}, nil
}

func (s *Service) handleAgentHierarchy(params json.RawMessage) (*AgentHierarchyResult, error) {
	if s.agentRuntime == nil {
		return nil, errors.New("agent orchestration not initialized")
	}

	var p AgentHierarchyParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("parse params: %w", err)
		}
	}

	// Get the overseer from runtime
	overseer, ok := s.agentRuntime.GetSpawnHandler().(*runtime.Overseer)
	if !ok || overseer == nil {
		// No overseer - return flat list of sessions as root nodes
		sessions := s.agentRuntime.List()
		nodes := make([]HierarchyNode, 0, len(sessions))
		for _, sess := range sessions {
			nodes = append(nodes, HierarchyNode{
				SessionID: sess.ID,
				ActorID:   sess.Config.ActorID,
				Role:      string(sess.Config.Role),
				Depth:     sess.Config.Depth,
				Status:    string(sess.Status),
				Children:  []HierarchyNode{},
			})
		}
		return &AgentHierarchyResult{Nodes: nodes}, nil
	}

	// If session ID specified, return hierarchy for that session
	if p.SessionID != "" {
		node := overseer.GetHierarchy(p.SessionID)
		if node == nil {
			return &AgentHierarchyResult{Nodes: []HierarchyNode{}}, nil
		}
		return &AgentHierarchyResult{Nodes: []HierarchyNode{convertHierarchyNode(node)}}, nil
	}

	// Return all root sessions (depth 0)
	sessions := s.agentRuntime.List()
	nodes := make([]HierarchyNode, 0)
	for _, sess := range sessions {
		if sess.Config.Depth == 0 {
			node := overseer.GetHierarchy(sess.ID)
			if node != nil {
				nodes = append(nodes, convertHierarchyNode(node))
			}
		}
	}

	return &AgentHierarchyResult{Nodes: nodes}, nil
}

func convertHierarchyNode(node *runtime.HierarchyNode) HierarchyNode {
	children := make([]HierarchyNode, 0, len(node.Children))
	for _, child := range node.Children {
		children = append(children, convertHierarchyNode(child))
	}
	return HierarchyNode{
		SessionID: node.SessionID,
		ActorID:   node.ActorID,
		Role:      string(node.Role),
		Depth:     node.Depth,
		Status:    string(node.Status),
		Children:  children,
	}
}

// AgentListResult is the result of listing agents.
type AgentListResult struct {
	Sessions []AgentSessionInfo `json:"sessions"`
	Count    int                `json:"count"`
}

// AgentSessionInfo is summary info about an agent session.
type AgentSessionInfo struct {
	SessionID  string    `json:"session_id"`
	ActorID    string    `json:"actor_id"`
	Role       string    `json:"role"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	Iterations int       `json:"iterations"`
}

func (s *Service) handleAgentList() *AgentListResult {
	if s.agentRuntime == nil {
		return &AgentListResult{Sessions: []AgentSessionInfo{}, Count: 0}
	}

	sessions := s.agentRuntime.List()
	result := &AgentListResult{
		Sessions: make([]AgentSessionInfo, 0, len(sessions)),
		Count:    len(sessions),
	}

	for _, session := range sessions {
		info := AgentSessionInfo{
			SessionID:  session.ID,
			ActorID:    session.Config.ActorID,
			Role:       string(session.Config.Role),
			Status:     string(session.Status),
			StartedAt:  session.StartedAt,
			Iterations: session.Iterations,
		}
		result.Sessions = append(result.Sessions, info)
	}

	return result
}

// AgentStatusParams are the parameters for agent.status.
type AgentStatusParams struct {
	SessionID string `json:"session_id"`
}

// AgentStatusResult is the detailed status of an agent session.
type AgentStatusResult struct {
	SessionID  string     `json:"session_id"`
	ActorID    string     `json:"actor_id"`
	Role       string     `json:"role"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	Iterations int        `json:"iterations"`
	Summary    string     `json:"summary,omitempty"`
	Error      string     `json:"error,omitempty"`
	Children   []string   `json:"children,omitempty"`
}

func (s *Service) handleAgentStatus(params json.RawMessage) (*AgentStatusResult, error) {
	if s.agentRuntime == nil {
		return nil, errors.New("agent orchestration not initialized")
	}

	var p AgentStatusParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.SessionID == "" {
		return nil, errors.New("session_id is required")
	}

	session, ok := s.agentRuntime.Get(p.SessionID)
	if !ok {
		return nil, fmt.Errorf("session not found: %s", p.SessionID)
	}

	return &AgentStatusResult{
		SessionID:  session.ID,
		ActorID:    session.Config.ActorID,
		Role:       string(session.Config.Role),
		Status:     string(session.Status),
		StartedAt:  session.StartedAt,
		EndedAt:    session.EndedAt,
		Iterations: session.Iterations,
		Summary:    session.Summary,
		Error:      session.Error,
		Children:   session.Children,
	}, nil
}

// AgentKillParams are the parameters for agent.kill.
type AgentKillParams struct {
	SessionID string `json:"session_id"`
}

// AgentKillResult is the result of killing an agent.
type AgentKillResult struct {
	SessionID string `json:"session_id"`
	Status    string `json:"status"`
}

func (s *Service) handleAgentKill(params json.RawMessage) (*AgentKillResult, error) {
	if s.agentRuntime == nil {
		return nil, errors.New("agent orchestration not initialized")
	}

	var p AgentKillParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.SessionID == "" {
		return nil, errors.New("session_id is required")
	}

	if err := s.agentRuntime.Kill(p.SessionID); err != nil {
		return nil, err
	}

	return &AgentKillResult{
		SessionID: p.SessionID,
		Status:    "killed",
	}, nil
}

// exec is a minimal process starter (avoiding os/exec import cycle issues).
type exec struct {
	Path   string
	Args   []string
	Env    []string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func (e *exec) Start() error {
	// Use syscall.ForkExec for proper daemonization
	// This is simplified - in production use os/exec
	stdin, ok := e.Stdin.(*os.File)
	if !ok {
		return errors.New("stdin must be *os.File")
	}
	stdout, ok := e.Stdout.(*os.File)
	if !ok {
		return errors.New("stdout must be *os.File")
	}
	stderr, ok := e.Stderr.(*os.File)
	if !ok {
		return errors.New("stderr must be *os.File")
	}

	attr := &os.ProcAttr{
		Dir:   "/",
		Env:   e.Env,
		Files: []*os.File{stdin, stdout, stderr},
	}

	_, err := os.StartProcess(e.Path, e.Args, attr)
	return err
}

// resolveLLMConfig returns the LLM provider, API key, and model from centralized config.
// Priority: configured provider > cerebras > openrouter > groq > gemini
func (s *Service) resolveLLMConfig() (provider, apiKey, model string) {
	llm := s.cfg.LLM

	// If a provider is explicitly configured, use it
	if llm.Provider != "" {
		provider = llm.Provider
		apiKey = llm.ResolveAPIKey(provider)
		model = llm.ResolveModel(provider)
		if model == "" {
			model = llmproviders.DefaultModelForProvider(provider)
		}
		return
	}

	// Auto-detect from available API keys (priority order)
	providers := []string{"cerebras", "openrouter", "groq", "gemini"}
	for _, p := range providers {
		if key := llm.ResolveAPIKey(p); key != "" {
			provider = p
			apiKey = key
			model = llm.ResolveModel(p)
			if model == "" {
				model = llmproviders.DefaultModelForProvider(p)
			}
			return
		}
	}

	return "", "", ""
}
