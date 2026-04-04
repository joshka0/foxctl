package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/agent/runtime"
	"github.com/jkatigb/agentctl/internal/agent/toolnames"
	"github.com/jkatigb/agentctl/internal/agent/types"
	"github.com/jkatigb/agentctl/internal/context/updater"
	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/execution/runner"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/indexing/filesummary"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/lsp/gopls"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/deviceid"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	llmproviders "github.com/jkatigb/agentctl/internal/providers/llm"
	"github.com/jkatigb/agentctl/internal/queue"
	"github.com/jkatigb/agentctl/internal/sessionkit/summary"
	"github.com/jkatigb/agentctl/internal/storage"
	agentstore "github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/cache"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/jkatigb/agentctl/internal/storage/contextbuffer"
	"github.com/jkatigb/agentctl/internal/storage/coordination"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	v2kill "github.com/jkatigb/agentctl/internal/v2/core/kill"
	v2services "github.com/jkatigb/agentctl/internal/v2/services"
	"github.com/oklog/ulid/v2"
)

// ServiceOptions configures the daemon service.
type ServiceOptions struct {
	// Workspace to pre-warm (optional).
	Workspace string
}

// Service is the main daemon service.
type Service struct {
	cfg        config.Config
	opts       ServiceOptions
	listener   net.Listener
	listenerMu sync.RWMutex
	started    time.Time
	socketPath string

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

	// Leader lease (cross-device safety).
	// When enabled, only the leader runs background workers that mutate shared state.
	leaderMu        sync.RWMutex
	leaderEnabled   bool
	isLeader        bool
	deviceID        string
	leaderOwnerID   string
	coordStore      *coordination.Store
	leaseCancel     context.CancelFunc
	leaseWG         sync.WaitGroup
	leaderWorkersMu sync.Mutex // serializes start/stop transitions when leader status flips

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
	summaryQueue  *queue.Store
	summaryStore  *sessions.Store

	// Context updater worker (proactive context surfacing)
	contextUpdater       *updater.Worker
	contextUpdaterCtx    context.Context
	contextUpdaterCancel context.CancelFunc
	contextUpdaterStore  *sessions.Store
	contextUpdaterMemory storage.MemoryStore
	contextUpdaterBuffer contextbuffer.Store

	// File summary worker (background LLM summaries)
	fileSummaryWorker       *filesummary.Worker
	fileSummaryWorkerCtx    context.Context
	fileSummaryWorkerCancel context.CancelFunc
	fileSummaryMemoryStore  storage.MemoryStore // Memory store for file summary worker (close on shutdown)

	// ACA maintenance loop
	acaMaintenanceCtx    context.Context
	acaMaintenanceCancel context.CancelFunc
	acaMaintenanceWG     sync.WaitGroup

	// Agent orchestration
	agentMu             sync.Mutex
	agentRuntime        *runtime.Runtime
	agentOverseer       *runtime.Overseer
	agentCtx            context.Context
	agentCancel         context.CancelFunc
	agentPromptVariants optimization.PromptVariantStore
	agentSessionStore   *sessions.Store       // Session store for agent persistence (close on shutdown)
	agentMailboxStore   mailbox.Store         // Mailbox store for agent messaging (close on shutdown)
	agentBoardStore     blackboard.BoardStore // Blackboard store for agent coordination (close on shutdown)
	agentSessionMap     map[string]string     // agentID (agents.db) → sessionID (runtime); protected by agentMu
}

// NewService creates a new daemon service.
//
// Index:
// - Purpose: Initialize daemon service state and shared resources
// - Flow: load env → init DB pool → build resolver → optionally warm workspace
// - SideEffects: reads env; initializes sqlite pool; starts warm goroutine
// - FailureModes: none (initialization uses defaults)
// - Related: Service.Run, Service.warmWorkspace
// - Keywords: daemon_service, warm_workspace, sqlite_pool, skill_resolver
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
		socketPath:     SocketPath(),
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

const (
	daemonLeaseName     = "agent_daemon"
	daemonLeaseTTL      = 45 * time.Second
	daemonLeaseInterval = 15 * time.Second
	acaMaintenanceTick  = 5 * time.Minute
)

func coordinationIsShared(cfg dbdriver.Config) bool {
	switch cfg.Driver {
	case dbdriver.DriverTurso:
		return true
	case dbdriver.DriverLibSQL:
		return strings.TrimSpace(cfg.LibSQL.SyncURL) != ""
	default:
		return false
	}
}

// startLeaderLease initializes cross-device leader coordination when enabled.
// When coordination is not shared (local-only), leader gating is disabled and background workers start normally.
//
// Index:
// - Purpose: Prevent duplicate daemon background work when multiple machines share DB state
// - Flow: load COORDINATION config → if shared, open coordination store → acquire/renew lease loop → start/stop leader workers on transitions
// - SideEffects: may create/open coordination.db; may start/stop background workers
// - FailureModes: config/open errors, lease acquisition errors (daemon remains follower)
// - Related: Service.stopLeaderLease, coordination.Store.TryAcquireLease, deviceid.LoadOrCreate
// - Keywords: leader_lease, coordination, single_leader, cross_device
func (s *Service) startLeaderLease(ctx context.Context) error {
	loader := dbdriver.NewConfigLoader(s.cfg.Storage.Root)
	coordCfg := loader.LoadConfig("COORDINATION", "coordination.db")

	// Only enforce leader gating when the coordination store is actually shared.
	// For local-only stores, multiple machines won't share state anyway.
	if !coordinationIsShared(coordCfg) {
		deviceID, _ := deviceid.LoadOrCreate(s.cfg.Home)
		s.leaderMu.Lock()
		s.leaderEnabled = false
		s.deviceID = deviceID
		s.isLeader = true
		s.leaderMu.Unlock()
		return s.startLeaderWorkers(ctx)
	}

	deviceID, err := deviceid.LoadOrCreate(s.cfg.Home)
	if err != nil {
		return fmt.Errorf("leader lease: load device id: %w", err)
	}
	ownerID := fmt.Sprintf("%s:%d:%s", deviceID, os.Getpid(), ulid.Make().String())

	store, err := coordination.Open(ctx, s.cfg.Storage.Root)
	if err != nil {
		s.leaderMu.Lock()
		s.leaderEnabled = true
		s.deviceID = deviceID
		s.leaderOwnerID = ownerID
		s.coordStore = nil
		s.isLeader = false
		s.leaderMu.Unlock()
		return fmt.Errorf("leader lease: open coordination store: %w", err)
	}

	s.leaderMu.Lock()
	s.leaderEnabled = true
	s.deviceID = deviceID
	s.leaderOwnerID = ownerID
	s.coordStore = store
	s.isLeader = false
	s.leaderMu.Unlock()

	leaseCtx, cancel := context.WithCancel(ctx)
	s.leaseCancel = cancel

	s.leaseWG.Add(1)
	go s.runLeaseLoop(leaseCtx, store, ownerID)
	return nil
}

func (s *Service) runLeaseLoop(ctx context.Context, store *coordination.Store, ownerID string) {
	defer s.leaseWG.Done()

	ticker := time.NewTicker(daemonLeaseInterval)
	defer ticker.Stop()

	for {
		acquired, err := store.TryAcquireLease(ctx, daemonLeaseName, ownerID, daemonLeaseTTL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: leader lease acquire failed: %v\n", err)
			s.transitionLeader(ctx, false)
		} else {
			s.transitionLeader(ctx, acquired)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) transitionLeader(ctx context.Context, leader bool) {
	s.leaderWorkersMu.Lock()
	defer s.leaderWorkersMu.Unlock()

	s.leaderMu.Lock()
	if leader == s.isLeader {
		s.leaderMu.Unlock()
		return
	}
	s.isLeader = leader
	s.leaderMu.Unlock()

	if leader {
		fmt.Fprintf(os.Stderr, "daemon: leader lease acquired; starting leader workers\n")
		if err := s.startLeaderWorkers(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "warning: leader workers start error: %v\n", err)
		}
		return
	}

	fmt.Fprintf(os.Stderr, "daemon: leader lease lost; stopping leader workers\n")
	s.stopLeaderWorkers()
}

func (s *Service) startLeaderWorkers(ctx context.Context) error {
	var firstErr error

	if err := s.startSummaryWorker(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: summary worker failed to start: %v\n", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	if err := s.startContextUpdater(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: context updater failed to start: %v\n", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	if err := s.startFileSummaryWorker(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: file summary worker failed to start: %v\n", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	if err := s.startAgentOrchestration(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: agent orchestration failed to start: %v\n", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	if err := s.startACAMaintenanceLoop(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "warning: ACA maintenance loop failed to start: %v\n", err)
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (s *Service) stopLeaderWorkers() {
	s.stopSummaryWorker()
	s.stopContextUpdater()
	s.stopFileSummaryWorker()
	s.stopAgentOrchestration()
	s.stopACAMaintenanceLoop()
}

func (s *Service) stopLeaderLease(ctx context.Context) {
	if s.leaseCancel != nil {
		s.leaseCancel()
		s.leaseCancel = nil
	}
	s.leaseWG.Wait()

	s.leaderMu.RLock()
	store := s.coordStore
	ownerID := s.leaderOwnerID
	s.leaderMu.RUnlock()

	if store != nil {
		releaseCtx := ctx
		if releaseCtx == nil {
			releaseCtx = context.Background()
		}
		releaseCtx, cancel := context.WithTimeout(releaseCtx, 2*time.Second)
		_ = store.ReleaseLease(releaseCtx, daemonLeaseName, ownerID)
		cancel()
		_ = store.Close()
	}

	s.leaderMu.Lock()
	s.coordStore = nil
	s.leaderEnabled = false
	s.leaderOwnerID = ""
	s.isLeader = false
	s.leaderMu.Unlock()
}

// Run starts the daemon service and blocks until shutdown.
//
// Index:
// - Purpose: Start daemon listener and background workers
// - Flow: create socket → start workers → accept connections → wait for shutdown
// - SideEffects: listens on socket; starts goroutines; writes PID file
// - FailureModes: socket errors, PID file errors, worker start errors
// - Related: Service.Shutdown, Service.acceptLoop
// - Keywords: daemon_run, unix_socket, shutdown, summary_worker, context_updater
func (s *Service) Run(ctx context.Context) error {
	// Remove stale socket
	socketPath := s.socketPath
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
	s.setListener(listener)
	fmt.Fprintf(os.Stderr, "daemon: listener bound on %s\n", socketPath)

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

	// Start leader lease coordination (only enabled when COORDINATION is configured for remote sync).
	// This gates background workers so only one machine mutates shared state at a time.
	if err := s.startLeaderLease(ctx); err != nil {
		// Log but don't fail daemon startup; safest behavior is to run as follower (no leader workers).
		fmt.Fprintf(os.Stderr, "warning: leader lease init failed (running without leader workers): %v\n", err)
	}

	// Accept connections
	go s.acceptLoop(ctx, listener)

	// Wait for shutdown signal
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.shutdownCh:
		return nil
	}
}

// Shutdown gracefully shuts down the daemon.
//
// Index:
// - Purpose: Stop daemon workers and release resources
// - Flow: signal shutdown → wait for requests → stop workers → close stores → remove socket
// - SideEffects: stops goroutines; closes databases; removes socket/PID
// - FailureModes: context timeout while waiting for in-flight requests
// - Related: Service.Run, Service.acceptLoop
// - Keywords: daemon_shutdown, stop_workers, close_stores, socket_path
func (s *Service) Shutdown(ctx context.Context) error {
	s.shutdownOnce.Do(func() {
		s.shutdownMu.Lock()
		s.isShutdown = true
		s.shutdownMu.Unlock()
		close(s.shutdownCh)
	})

	if listener := s.clearListener(); listener != nil {
		listener.Close()
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

	// Stop leader lease loop first so it can't restart workers during shutdown.
	s.stopLeaderLease(ctx)
	s.leaderWorkersMu.Lock()
	s.stopLeaderWorkers()
	s.leaderWorkersMu.Unlock()

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
	_ = os.Remove(s.socketPath)

	return nil
}

// acceptLoop accepts connections and handles them.
//
// Index:
// - Purpose: Accept daemon connections and dispatch handlers
// - Flow: accept conn → guard shutdown → spawn handler goroutine
// - SideEffects: spawns goroutines; reads socket
// - FailureModes: accept errors ignored unless shutting down
// - Related: Service.handleConnection
// - Keywords: accept_loop, unix_socket, goroutine, shutdown
func (s *Service) acceptLoop(ctx context.Context, listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-s.shutdownCh:
				return
			default:
				// Transient error, continue
				fmt.Fprintf(os.Stderr, "daemon: accept error: %v\n", err)
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

func (s *Service) setListener(listener net.Listener) {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()
	s.listener = listener
}

func (s *Service) clearListener() net.Listener {
	s.listenerMu.Lock()
	defer s.listenerMu.Unlock()
	listener := s.listener
	s.listener = nil
	return listener
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
//
// Index:
// - Purpose: Decode daemon request and route to handler
// - Flow: decode request → switch method → build response → write response
// - SideEffects: reads/writes socket; updates request count
// - FailureModes: parse errors, handler errors
// - Related: Service.handleRun, Service.handleStatus
// - Keywords: daemon_request, json_rpc, handle_run, handle_status
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
		result, err := s.dispatchAgentSpawn(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "ESPAWN", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.list":
		result, err := s.dispatchAgentList(ctx)
		if err != nil {
			resp.Error = &Error{Code: "ELIST", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.status":
		result, err := s.handleAgentStatus(req.Params)
		if err != nil {
			resp.Error = &Error{Code: "ESTATUS", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.kill":
		result, err := s.dispatchAgentKill(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "EKILL", Message: err.Error()}
		} else {
			resp.Result = result
		}
	case "agent.ask":
		result, err := s.handleAgentAskRPC(ctx, req.Params)
		if err != nil {
			resp.Error = &Error{Code: "EASK", Message: err.Error()}
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
	LeaderEnabled  bool     `json:"leader_enabled,omitempty"`
	IsLeader       bool     `json:"is_leader,omitempty"`
	DeviceID       string   `json:"device_id,omitempty"`
	LeaderOwnerID  string   `json:"leader_owner_id,omitempty"`
}

func (s *Service) handleStatus() StatusResult {
	s.warmMu.RLock()
	workspaces := make([]string, 0, len(s.warmWorkspaces))
	for ws := range s.warmWorkspaces {
		workspaces = append(workspaces, ws)
	}
	s.warmMu.RUnlock()

	s.leaderMu.RLock()
	leaderEnabled := s.leaderEnabled
	isLeader := s.isLeader
	deviceID := s.deviceID
	ownerID := s.leaderOwnerID
	s.leaderMu.RUnlock()

	return StatusResult{
		PID:            os.Getpid(),
		StartedAt:      s.started.Format(time.RFC3339),
		UptimeSeconds:  time.Since(s.started).Seconds(),
		RequestCount:   s.requestCount.Load(),
		WarmWorkspaces: workspaces,
		LeaderEnabled:  leaderEnabled,
		IsLeader:       isLeader,
		DeviceID:       deviceID,
		LeaderOwnerID:  ownerID,
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

// handleRun executes a skill through the runner and returns its output.
//
// Index:
// - Purpose: Execute a skill in the daemon process
// - Flow: parse params → resolve skill → build env → run skill → build result
// - SideEffects: executes skill binary; reads/writes env; uses CAS
// - FailureModes: invalid params, resolve failures, runner errors
// - Related: SkillResolver.Resolve, runner.RunWithOptions
// - Keywords: daemon_run, skill, runner, output, stderr
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
//
// Index:
// - Purpose: Warm LSP and caches for faster future requests
// - Flow: check warm map → start gopls daemon → mark warm
// - SideEffects: starts gopls daemon; updates warm set
// - FailureModes: gopls startup failures ignored
// - Related: Service.Run, Service.handleWarm
// - Keywords: warm_workspace, gopls, lsp, warm_cache
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
//
// Index:
// - Purpose: Spawn daemon child process and detach from terminal
// - Flow: check env → resolve executable → start child → detach IO
// - SideEffects: starts child process
// - FailureModes: executable lookup errors, process start errors
// - Related: exec.Start, Client.EnsureRunningContext
// - Keywords: daemonize, background, socket, child_process
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
//
// Index:
// - Purpose: Start background session summarization worker
// - Flow: check providers → open stores → create worker → start worker
// - SideEffects: opens stores; starts worker goroutine
// - FailureModes: store open errors, worker start errors
// - Related: summary.NewWorker
// - Keywords: summary_worker, sessions, queue, llm_provider
func (s *Service) startSummaryWorker(ctx context.Context) error {
	// Idempotency: allow leader transitions to call start repeatedly.
	if s.summaryWorker != nil {
		return nil
	}

	// Check if LLM providers are available
	providers := llmproviders.SummarizationProviders()
	if len(providers) == 0 {
		// No providers configured - skip worker silently
		return nil
	}

	// Open queue store
	queueStore, err := queue.OpenStore(ctx, s.cfg.Storage.Root, "SUMMARY_QUEUE", summary.QueueDBName, queue.Options{Table: summary.QueueTable})
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

	worker := summary.NewWorker(
		workerCfg,
		queueStore,
		sessionStore,
		providers,
		s.cfg,
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
	s.summaryQueue = queueStore
	s.summaryStore = sessionStore
	return nil
}

func (s *Service) stopSummaryWorker() {
	if s.summaryWorker != nil {
		if err := s.summaryWorker.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: summary worker stop error: %v\n", err)
		}
		s.summaryWorker = nil
	}
	if s.summaryCancel != nil {
		s.summaryCancel()
		s.summaryCancel = nil
	}
	if s.summaryQueue != nil {
		_ = s.summaryQueue.Close()
		s.summaryQueue = nil
	}
	if s.summaryStore != nil {
		_ = s.summaryStore.Close()
		s.summaryStore = nil
	}
	s.summaryCtx = nil
}

// startContextUpdater initializes and starts the background context updater worker.
//
// Index:
// - Purpose: Start context updater for proactive context surfacing
// - Flow: open stores → build finder/injector → start worker
// - SideEffects: opens stores; starts worker goroutine; logs to stderr
// - FailureModes: store open errors, worker creation errors
// - Related: updater.NewWorkerFromConfig
// - Keywords: context_updater, memory_store, session_store, injector
func (s *Service) startContextUpdater(ctx context.Context) error {
	// Idempotency: allow leader transitions to call start repeatedly.
	if s.contextUpdater != nil {
		return nil
	}

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
	worker, err := updater.NewWorkerFromConfig(ctx, updater.DaemonConfig{
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
	s.contextUpdaterStore = sessionStore
	s.contextUpdaterMemory = memoryStore
	s.contextUpdaterBuffer = ctxBufferStore
	fmt.Fprintf(os.Stderr, "context updater: started successfully\n")
	return nil
}

func (s *Service) stopContextUpdater() {
	if s.contextUpdater != nil {
		s.contextUpdater.Stop()
		s.contextUpdater = nil
	}
	if s.contextUpdaterCancel != nil {
		s.contextUpdaterCancel()
		s.contextUpdaterCancel = nil
	}
	if s.contextUpdaterStore != nil {
		_ = s.contextUpdaterStore.Close()
		s.contextUpdaterStore = nil
	}
	if s.contextUpdaterMemory != nil {
		s.contextUpdaterMemory.Close()
		s.contextUpdaterMemory = nil
	}
	if s.contextUpdaterBuffer != nil {
		_ = s.contextUpdaterBuffer.Close()
		s.contextUpdaterBuffer = nil
	}
	s.contextUpdaterCtx = nil
}

func (s *Service) startACAMaintenanceLoop(ctx context.Context) error {
	if s.acaMaintenanceCancel != nil {
		return nil
	}
	if strings.TrimSpace(s.opts.Workspace) == "" {
		return nil
	}
	workspacePath := ws.Normalize(ws.Detect(strings.TrimSpace(s.opts.Workspace)))
	if workspacePath == "" {
		return nil
	}
	s.acaMaintenanceCtx, s.acaMaintenanceCancel = context.WithCancel(ctx)
	worker := contextplane.NewWorker(contextplane.WorkerConfig{
		Config:    s.cfg,
		Workspace: workspacePath,
		VaultPath: acaMaintenanceVaultPath(),
		Interval:  acaMaintenanceInterval(),
	})
	s.acaMaintenanceWG.Add(1)
	go func() {
		defer s.acaMaintenanceWG.Done()
		if err := worker.Run(s.acaMaintenanceCtx); err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "ACA maintenance: worker stopped with error: %v\n", err)
		}
	}()
	return nil
}

func (s *Service) stopACAMaintenanceLoop() {
	if s.acaMaintenanceCancel != nil {
		s.acaMaintenanceCancel()
		s.acaMaintenanceCancel = nil
	}
	s.acaMaintenanceWG.Wait()
	s.acaMaintenanceCtx = nil
}

func acaMaintenanceInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("AGENTCTL_ACA_MAINTENANCE_INTERVAL"))
	if raw == "" {
		return acaMaintenanceTick
	}
	interval, err := time.ParseDuration(raw)
	if err != nil || interval <= 0 {
		return acaMaintenanceTick
	}
	return interval
}

func acaMaintenanceVaultPath() string {
	if value := strings.TrimSpace(os.Getenv("AGENTCTL_ACA_VAULT_PATH")); value != "" {
		return value
	}
	return strings.TrimSpace(os.Getenv("AGENTCTL_OBSIDIAN_VAULT_PATH"))
}

// startFileSummaryWorker initializes and starts the background file summary worker.
//
// Index:
// - Purpose: Start background file summary worker for workspace
// - Flow: resolve workspace → open memory store → configure providers → start worker
// - SideEffects: opens memory store; starts worker goroutine
// - FailureModes: missing workspace, provider errors, worker start errors
// - Related: filesummary.NewWorker
// - Keywords: file_summary, memory_store, workspace, llm_provider
func (s *Service) startFileSummaryWorker(ctx context.Context) error {
	// Idempotency: allow leader transitions to call start repeatedly.
	if s.fileSummaryWorker != nil {
		return nil
	}

	// Check if we have a workspace
	workspace := s.opts.Workspace
	if workspace == "" {
		fmt.Fprintf(os.Stderr, "file summary worker: skipped (no workspace)\n")
		return nil
	}
	if absWorkspace, err := filepath.Abs(workspace); err == nil {
		workspace = filepath.Clean(absWorkspace)
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

func (s *Service) stopFileSummaryWorker() {
	if s.fileSummaryWorker != nil {
		if err := s.fileSummaryWorker.Stop(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: file summary worker stop error: %v\n", err)
		}
		s.fileSummaryWorker = nil
	}
	if s.fileSummaryWorkerCancel != nil {
		s.fileSummaryWorkerCancel()
		s.fileSummaryWorkerCancel = nil
	}
	if s.fileSummaryMemoryStore != nil {
		s.fileSummaryMemoryStore.Close()
		s.fileSummaryMemoryStore = nil
	}
	s.fileSummaryWorkerCtx = nil
}

// startAgentOrchestration initializes the agent runtime and overseer.
//
// Index:
// - Purpose: Start agent runtime, overseer, and hook infrastructure
// - Flow: resolve LLM config → open stores → create runtime/overseer → recover sessions
// - SideEffects: opens stores; starts agent runtime
// - FailureModes: missing LLM keys, store open errors
// - Related: runtime.NewRuntime, runtime.NewOverseer
// - Keywords: agent_orchestration, runtime, overseer, hook_dispatcher
func (s *Service) startAgentOrchestration(ctx context.Context) error {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()

	// Idempotency: allow leader transitions to call start repeatedly.
	if s.agentRuntime != nil {
		return nil
	}

	// Determine LLM configuration from centralized config
	// Priority: configured provider > OPENROUTER > CEREBRAS > GROQ > GEMINI
	llmProvider, llmAPIKey, llmModel := s.resolveLLMConfig()

	if llmAPIKey == "" && !strings.EqualFold(s.cfg.LLM.ResolveAuthMode(llmProvider), "none") {
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

	// Open CAS store for full turn content persistence
	var casStore storage.CASStore
	if cs, err := cas.OpenDefault(ctx, s.cfg.Storage.Root); err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: CAS store open failed: %v\n", err)
	} else {
		casStore = cs
	}

	// Create memory store opener for agent tools
	openMemoryStore := func(ctx context.Context) (storage.MemoryStore, error) {
		return memory.OpenWithConfig(ctx, s.cfg)
	}

	var promptVariantStore optimization.PromptVariantStore
	if pvs, err := optimization.OpenPromptVariantStore(ctx, s.cfg.Storage.Root); err != nil {
		fmt.Fprintf(os.Stderr, "agent orchestration: prompt variant store open failed: %v\n", err)
	} else {
		s.agentPromptVariants = pvs
		promptVariantStore = pvs
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
		LLMBaseURL:           s.cfg.LLM.ResolveBaseURL(llmProvider),
		LLMAuthMode:          s.cfg.LLM.ResolveAuthMode(llmProvider),
		LLMAuthHeader:        s.cfg.LLM.ResolveAuthHeader(llmProvider),
		LLMAuthPrefix:        s.cfg.LLM.ResolveAuthPrefix(llmProvider),
		WorkspaceRoot:        s.opts.Workspace,
		DefaultMaxDepth:      3,
		PromptVariantStore:   promptVariantStore,
		OpenMemoryStore:      openMemoryStore,
		SessionStore:         sessionStore,
		MailboxStore:         mailboxStore,
		BoardStore:           boardStore,
		HookDispatcher:       hookDispatcher,
		ActionExecutor:       actionExecutor,
		CASStore:             casStore,
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

func (s *Service) stopAgentOrchestration() {
	s.agentMu.Lock()
	defer s.agentMu.Unlock()

	if s.agentCancel != nil {
		s.agentCancel()
		s.agentCancel = nil
	}

	// Kill all running agent sessions (best-effort).
	if s.agentRuntime != nil {
		for _, session := range s.agentRuntime.List() {
			_ = s.agentRuntime.Kill(session.ID)
		}
	}

	// Mark all tracked agents as stopped in agents.db (best-effort)
	if len(s.agentSessionMap) > 0 {
		ctx := context.Background()
		store, err := agentstore.Open(ctx, s.cfg.Storage.Root)
		if err != nil {
			slog.Warn("failed to open agents store for shutdown cleanup", "error", err)
		} else {
			for agentID := range s.agentSessionMap {
				if err := store.UpdateState(ctx, agentID, agent.StateStopped); err != nil {
					slog.Warn("failed to update agent state on shutdown", "agent_id", agentID, "error", err)
				}
			}
			store.Close()
		}
		s.agentSessionMap = nil
	}

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
	if s.agentPromptVariants != nil {
		s.agentPromptVariants.Close()
		s.agentPromptVariants = nil
	}

	s.agentRuntime = nil
	s.agentOverseer = nil
	s.agentCtx = nil
}

// --- Agent RPC Handlers ---

func (s *Service) dispatchAgentSpawn(ctx context.Context, params json.RawMessage) (*AgentSpawnResult, error) {
	result, err := s.handleAgentSpawnV2(ctx, params)
	return result, err
}

func (s *Service) dispatchAgentList(ctx context.Context) (*AgentListResult, error) {
	result, err := s.handleAgentListV2(ctx)
	return result, err
}

func (s *Service) dispatchAgentKill(ctx context.Context, params json.RawMessage) (*AgentKillResult, error) {
	result, err := s.handleAgentKillV2(ctx, params)
	return result, err
}

// AgentSpawnParams are the parameters for agent.spawn.
type AgentSpawnParams struct {
	Role            string   `json:"role"`
	AgentID         string   `json:"agent_id,omitempty"` // Agent config ID for session filtering
	WorkspaceID     string   `json:"workspace_id,omitempty"`
	WorkspaceRoot   string   `json:"workspace_root,omitempty"`
	EpicID          string   `json:"epic_id,omitempty"`
	TaskID          string   `json:"task_id,omitempty"`
	Prompt          string   `json:"prompt,omitempty"`
	SkillsAllow     []string `json:"skills_allow,omitempty"`
	MemoryScope     string   `json:"memory_scope,omitempty"`
	MemoryRetention string   `json:"memory_retention,omitempty"`

	// Agent metadata
	Name string `json:"name,omitempty"`
	Slug string `json:"slug,omitempty"`

	// Execution config
	MaxIterations    int    `json:"max_iterations,omitempty"`
	MaxContextTokens int    `json:"max_context_tokens,omitempty"` // Context budget (0=no limit)
	ExecMode         string `json:"exec_mode,omitempty"`          // "reactive", "autonomous", "autonomous_reactive", "proactive", "tick", "story"
	MaxAutoTurns     int    `json:"max_auto_turns,omitempty"`
	ThinkInterval    int    `json:"think_interval,omitempty"`

	// Session timeout
	Timeout string `json:"timeout,omitempty"` // e.g. "10m", "30m"

	// LLM override
	LLMProvider   string `json:"llm_provider,omitempty"`
	LLMModel      string `json:"llm_model,omitempty"`
	LLMAPIKey     string `json:"llm_api_key,omitempty"`
	LLMBaseURL    string `json:"llm_base_url,omitempty"`
	LLMAuthMode   string `json:"llm_auth_mode,omitempty"`
	LLMAuthHeader string `json:"llm_auth_header,omitempty"`
	LLMAuthPrefix string `json:"llm_auth_prefix,omitempty"`

	TerminalBinding agent.TerminalBinding `json:"terminal_binding,omitempty"`
}

// AgentSpawnResult is the result of spawning an agent.
type AgentSpawnResult struct {
	SessionID string `json:"session_id"`
	ActorID   string `json:"actor_id"`
	AgentID   string `json:"agent_id"` // Persistent ID in agents.db
	Name      string `json:"name"`     // Generated or provided agent name
	Status    string `json:"status"`
	Role      string `json:"role"`
	NS        string `json:"ns,omitempty"` // Namespace for mailbox routing
}

// AgentAskParams are the parameters for agent.ask.
type AgentAskParams struct {
	AgentID        string          `json:"agent_id"`
	Message        string          `json:"message"`
	Kind           string          `json:"kind,omitempty"`
	Context        map[string]any  `json:"context,omitempty"`
	ResponseSchema json.RawMessage `json:"response_schema,omitempty"`
	ResponseKeys   []string        `json:"response_keys,omitempty"`
	TimeoutMS      int             `json:"timeout_ms,omitempty"`
}

// AgentAskResult is the result of asking a running agent.
type AgentAskResult struct {
	AgentID   string         `json:"agent_id"`
	AskID     string         `json:"ask_id"`
	Reply     string         `json:"reply"`
	ReplyData map[string]any `json:"reply_data,omitempty"`
	Status    string         `json:"status"`
}

func (s *Service) handleAgentSpawnV2(ctx context.Context, params json.RawMessage) (*AgentSpawnResult, error) {
	return s.handleAgentSpawnWithRoute(ctx, params)
}

func (s *Service) handleAgentSpawnWithRoute(ctx context.Context, params json.RawMessage) (*AgentSpawnResult, error) {
	if s.agentRuntime == nil {
		return nil, errors.New("agent orchestration not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var p AgentSpawnParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.Role == "" {
		return nil, errors.New("role is required")
	}

	// Normalize skills allowlist for runtime tool names
	if len(p.SkillsAllow) > 0 {
		normalized, unknown := toolnames.ValidateAllowlist(toolnames.ToolModeRuntime, p.SkillsAllow)
		if len(unknown) > 0 {
			slog.Warn("unknown skills_allow entries in spawn request", "unknown", unknown)
		}
		p.SkillsAllow = normalized
	}

	// Generate actor ID if not provided
	actorID := p.AgentID
	if actorID == "" {
		actorID = "actor:" + p.Role + ":" + ulid.Make().String()
	}

	// Create agent config
	cfg := types.AgentConfig{
		Role:            types.AgentRole(p.Role),
		ActorID:         actorID,
		WorkspaceID:     p.WorkspaceID,
		WorkspaceRoot:   strings.TrimSpace(p.WorkspaceRoot),
		EpicID:          p.EpicID,
		TaskID:          p.TaskID,
		Prompt:          p.Prompt,
		SkillsAllow:     p.SkillsAllow,
		TerminalBinding: agent.NormalizeTerminalBinding(p.TerminalBinding),
	}
	if cfg.WorkspaceID == "" && cfg.WorkspaceRoot != "" {
		cfg.WorkspaceID = ws.ID(cfg.WorkspaceRoot)
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
		case string(agent.ModeReactive), string(agent.ModeAutonomous), string(agent.ModeAutonomousReactive), string(agent.ModeProactive), string(agent.ModeTick), string(agent.ModeStory):
			cfg.ExecMode = agent.ExecutionMode(p.ExecMode)
		default:
			return nil, fmt.Errorf("invalid exec_mode: %s", p.ExecMode)
		}
	}
	if p.MaxAutoTurns > 0 {
		cfg.MaxAutoTurns = p.MaxAutoTurns
	}
	if p.ThinkInterval > 0 {
		cfg.ThinkInterval = p.ThinkInterval
	}
	if p.Timeout != "" {
		if strings.TrimSpace(p.Timeout) == "0" {
			cfg.Timeout = -1
		} else {
			d, err := time.ParseDuration(p.Timeout)
			if err != nil {
				return nil, fmt.Errorf("invalid timeout %q: %w", p.Timeout, err)
			}
			if d <= 0 {
				return nil, fmt.Errorf("invalid timeout %q: must be positive", p.Timeout)
			}
			cfg.Timeout = d
		}
	}

	if cfg.ExecMode == agent.ModeTick {
		cfg.LLMProvider = "lmstudio"
		if strings.TrimSpace(cfg.LLMModel) == "" {
			cfg.LLMModel = llmproviders.DefaultModelForProvider("lmstudio")
		}
	}

	// Apply LLM override if provided
	if p.LLMProvider != "" {
		cfg.LLMProvider = p.LLMProvider
		cfg.LLMAPIKey = s.cfg.LLM.ResolveAPIKey(cfg.LLMProvider)
		cfg.LLMBaseURL = s.cfg.LLM.ResolveBaseURL(cfg.LLMProvider)
		cfg.LLMAuthMode = s.cfg.LLM.ResolveAuthMode(cfg.LLMProvider)
		cfg.LLMAuthHeader = s.cfg.LLM.ResolveAuthHeader(cfg.LLMProvider)
		cfg.LLMAuthPrefix = s.cfg.LLM.ResolveAuthPrefix(cfg.LLMProvider)
		if strings.TrimSpace(cfg.LLMModel) == "" {
			cfg.LLMModel = llmproviders.DefaultModelForProvider(cfg.LLMProvider)
		}
	}
	if p.LLMModel != "" {
		cfg.LLMModel = p.LLMModel
	}
	if p.LLMAPIKey != "" {
		cfg.LLMAPIKey = p.LLMAPIKey
	}
	if p.LLMBaseURL != "" {
		cfg.LLMBaseURL = p.LLMBaseURL
	}
	if p.LLMAuthMode != "" {
		cfg.LLMAuthMode = p.LLMAuthMode
	}
	if p.LLMAuthHeader != "" {
		cfg.LLMAuthHeader = p.LLMAuthHeader
	}
	if p.LLMAuthPrefix != "" {
		cfg.LLMAuthPrefix = p.LLMAuthPrefix
	}

	// Spawn the agent
	session, err := s.agentRuntime.Spawn(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("spawn agent: %w", err)
	}

	// Persist agent record to agents.db
	agentID := ulid.Make().String()
	agentName := p.Name
	if agentName == "" {
		agentName = agent.GenerateAgentName(rand.New(rand.NewSource(time.Now().UnixNano())))
	}

	execMode := agent.ExecutionMode(p.ExecMode)
	if execMode == "" {
		execMode = agent.ModeReactive
	}

	agentRecord := agent.Agent{
		ID:              agentID,
		Namespace:       actorID,
		Name:            agentName,
		Slug:            p.Slug,
		Role:            p.Role,
		Prompt:          p.Prompt,
		SkillsAllow:     p.SkillsAllow,
		Policy:          agent.Policy{},
		ShareBB:         "scoped",
		State:           agent.StateRunning,
		CreatedAt:       time.Now().UTC(),
		LLMProvider:     cfg.LLMProvider,
		LLMModel:        cfg.LLMModel,
		LLMAPIKey:       cfg.LLMAPIKey,
		LLMBaseURL:      cfg.LLMBaseURL,
		LLMAuthMode:     cfg.LLMAuthMode,
		LLMAuthHeader:   cfg.LLMAuthHeader,
		LLMAuthPrefix:   cfg.LLMAuthPrefix,
		ExecMode:        execMode,
		ExecutionLayer:  agent.ExecutionLayerClassic,
		MaxIterations:   p.MaxIterations,
		MaxAutoTurns:    p.MaxAutoTurns,
		ThinkInterval:   p.ThinkInterval,
		MemoryScope:     agent.NormalizeMemoryScope(agent.MemoryScope(strings.TrimSpace(p.MemoryScope))),
		TerminalBinding: cfg.TerminalBinding,
		MemoryRetention: func() agent.MemoryRetention {
			if strings.TrimSpace(p.MemoryRetention) == "" {
				return agent.DefaultMemoryRetentionForScope(agent.NormalizeMemoryScope(agent.MemoryScope(strings.TrimSpace(p.MemoryScope))))
			}
			return agent.NormalizeMemoryRetention(agent.MemoryRetention(strings.TrimSpace(p.MemoryRetention)))
		}(),
	}
	if execMode == agent.ModeTick {
		agentRecord.LLMProvider = "lmstudio"
		if strings.TrimSpace(agentRecord.LLMModel) == "" {
			agentRecord.LLMModel = llmproviders.DefaultModelForProvider("lmstudio")
		}
	}
	if agentRecord.SkillsAllow == nil {
		agentRecord.SkillsAllow = []string{}
	}

	store, err := agentstore.Open(ctx, s.cfg.Storage.Root)
	if err != nil {
		slog.Warn("failed to open agents store for persistence", "error", err)
	} else {
		defer store.Close()
		if err := store.Create(ctx, agentRecord); err != nil {
			slog.Warn("failed to persist agent record", "agent_id", agentID, "error", err)
		} else {
			// Track mapping for kill/shutdown cleanup
			s.agentMu.Lock()
			if s.agentSessionMap == nil {
				s.agentSessionMap = make(map[string]string)
			}
			s.agentSessionMap[agentID] = session.ID
			s.agentMu.Unlock()

			go func(agentID string, session *runtime.Session) {
				<-session.Done()

				agentState := agent.StateStopped
				sess := session.GetSession()
				if sess.Status == types.StatusError {
					agentState = agent.StateError
				}

				updateCtx := context.Background()
				updateStore, err := agentstore.Open(updateCtx, s.cfg.Storage.Root)
				if err != nil {
					slog.Warn("failed to open agent store for state update", "agent_id", agentID, "error", err)
					return
				}
				defer updateStore.Close()

				if err := updateStore.UpdateState(updateCtx, agentID, agentState); err != nil {
					slog.Warn("failed to update agent state on completion", "agent_id", agentID, "error", err)
				}

				s.agentMu.Lock()
				delete(s.agentSessionMap, agentID)
				s.agentMu.Unlock()
			}(agentID, session)
		}
	}

	return &AgentSpawnResult{
		SessionID: session.ID,
		ActorID:   session.Config.ActorID,
		AgentID:   agentID,
		Name:      agentName,
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

// handleAgentResume resumes a prior agent session with a new prompt.
//
// Index:
// - Purpose: Resume an existing agent session
// - Flow: parse params → validate → resume session → return result
// - SideEffects: triggers agent execution
// - FailureModes: invalid params, runtime resume errors
// - Related: runtime.Runtime.Resume
// - Keywords: agent.resume, session_id, prompt, runtime
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

func (s *Service) handleAgentAskRPC(ctx context.Context, params json.RawMessage) (*AgentAskResult, error) {
	if s.agentMailboxStore == nil {
		return nil, errors.New("agent mailbox is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var p AgentAskParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	agentID := strings.TrimSpace(p.AgentID)
	if agentID == "" {
		return nil, errors.New("agent_id is required")
	}
	message := strings.TrimSpace(p.Message)
	if message == "" {
		return nil, errors.New("message is required")
	}

	store, err := agentstore.Open(ctx, s.cfg.Storage.Root)
	if err != nil {
		return nil, fmt.Errorf("open agents store: %w", err)
	}
	defer store.Close()

	agentRecord, err := store.Get(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("get agent: %w", err)
	}

	timeout := 30 * time.Second
	if p.TimeoutMS > 0 {
		timeout = time.Duration(p.TimeoutMS) * time.Millisecond
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	senderNS := "actor:system:daemon_rpc"
	sender := agent.NewSender(s.agentMailboxStore, senderNS)
	askID, err := sender.SendAsk(
		ctx,
		strings.TrimSpace(agentRecord.Namespace),
		message,
		agent.WithAskTTL(timeout),
		agent.WithAskKind(strings.TrimSpace(p.Kind)),
		agent.WithAskContext(p.Context),
		agent.WithAskResponseSchema(p.ResponseSchema),
		agent.WithAskResponseKeys(p.ResponseKeys),
	)
	if err != nil {
		return nil, fmt.Errorf("send ask: %w", err)
	}

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for reply")
		}

		messages, err := s.agentMailboxStore.List(ctx, senderNS, 100)
		if err != nil {
			return nil, fmt.Errorf("list replies: %w", err)
		}
		for _, msg := range messages {
			if msg.Type != agent.MessageTypeReply {
				continue
			}
			replies := agent.WithAskID([]agent.Message{msg}, askID)
			if len(replies) == 0 {
				continue
			}
			if err := s.agentMailboxStore.Ack(ctx, msg.ID); err != nil {
				slog.Warn("failed to ack daemon ask reply", "message_id", msg.ID, "error", err)
			}

			replyData, err := agent.ParsePayload[agent.ReplyData](msg)
			if err != nil {
				return nil, fmt.Errorf("parse reply payload: %w", err)
			}
			replyText := strings.TrimSpace(fmt.Sprint(replyData.Answer["response"]))
			return &AgentAskResult{
				AgentID:   agentID,
				AskID:     askID,
				Reply:     replyText,
				ReplyData: replyData.Answer,
				Status:    "replied",
			}, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

// handleAgentHierarchy returns the current agent hierarchy.
//
// Index:
// - Purpose: Provide hierarchy tree for agent sessions
// - Flow: parse params → fetch overseer → build hierarchy → return nodes
// - SideEffects: reads runtime state
// - FailureModes: invalid params, orchestration not initialized
// - Related: runtime.Overseer.GetHierarchy
// - Keywords: agent.hierarchy, overseer, sessions, depth
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

func (s *Service) handleAgentListV2(ctx context.Context) (*AgentListResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.agentRuntime == nil {
		return &AgentListResult{Sessions: []AgentSessionInfo{}, Count: 0}, nil
	}
	return buildAgentListResult(s.agentRuntime.List()), nil
}

func buildAgentListResult(sessions []*runtime.Session) *AgentListResult {
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

// handleAgentStatus returns detailed status for a session.
//
// Index:
// - Purpose: Provide detailed agent session status
// - Flow: parse params → lookup session → map status → return result
// - SideEffects: reads runtime state
// - FailureModes: missing session, invalid params
// - Related: runtime.Runtime.Get
// - Keywords: agent.status, session_id, runtime, status
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

type runtimeRunKiller struct {
	runtime *runtime.Runtime
}

func (k runtimeRunKiller) Kill(ctx context.Context, runID string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if k.runtime == nil {
		return errors.New("agent runtime is not initialized")
	}
	return k.runtime.Kill(strings.TrimSpace(runID))
}

func (s *Service) handleAgentKillV2(ctx context.Context, params json.RawMessage) (*AgentKillResult, error) {
	return s.handleAgentKillWithRoute(ctx, params, true)
}

func (s *Service) handleAgentKillWithRoute(ctx context.Context, params json.RawMessage, useV2 bool) (*AgentKillResult, error) {
	if s.agentRuntime == nil {
		return nil, errors.New("agent orchestration not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var p AgentKillParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	sessionID := strings.TrimSpace(p.SessionID)
	if sessionID == "" {
		return nil, errors.New("session_id is required")
	}

	if useV2 {
		svc := v2services.NewKillService(v2services.KillDependencies{
			Killer: runtimeRunKiller{runtime: s.agentRuntime},
		})
		resp, err := svc.Kill(ctx, v2kill.Request{RunID: sessionID})
		if err != nil {
			return nil, err
		}
		if rid := strings.TrimSpace(resp.RunID); rid != "" {
			sessionID = rid
		}
	} else {
		if err := s.agentRuntime.Kill(sessionID); err != nil {
			return nil, err
		}
	}

	s.updateAgentStateAfterKill(ctx, sessionID)

	return &AgentKillResult{
		SessionID: sessionID,
		Status:    "killed",
	}, nil
}

func (s *Service) updateAgentStateAfterKill(ctx context.Context, sessionID string) {
	if ctx == nil {
		ctx = context.Background()
	}
	updateCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	// Update agent state in agents.db (best-effort)
	s.agentMu.Lock()
	var agentID string
	for aID, sID := range s.agentSessionMap {
		if sID == sessionID {
			agentID = aID
			delete(s.agentSessionMap, aID)
			break
		}
	}
	s.agentMu.Unlock()

	if agentID != "" {
		store, err := agentstore.Open(updateCtx, s.cfg.Storage.Root)
		if err != nil {
			slog.Warn("failed to open agents store for kill", "error", err)
		} else {
			if err := store.UpdateState(updateCtx, agentID, agent.StateStopped); err != nil {
				slog.Warn("failed to update agent state on kill", "agent_id", agentID, "error", err)
			}
			store.Close()
		}
	}
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
		Sys: &syscall.SysProcAttr{
			Setsid: true,
		},
	}

	_, err := os.StartProcess(e.Path, e.Args, attr)
	return err
}

// resolveLLMConfig returns the LLM provider, API key, and model from centralized config.
// Priority: configured provider > LM Studio default.
//
// Index:
// - Purpose: Select provider credentials and model for agent orchestration
// - Flow: check configured provider → auto-detect by API key → return defaults
// - SideEffects: reads config
// - Related: llmproviders.DefaultModelForProvider
// - Keywords: llm_provider, api_key, model, config, auto_detect
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

	// Default to LM Studio for local-first behavior when no provider is configured.
	provider = "lmstudio"
	apiKey = llm.ResolveAPIKey(provider)
	model = llm.ResolveModel(provider)
	if model == "" {
		model = llmproviders.DefaultModelForProvider(provider)
	}
	return
}
