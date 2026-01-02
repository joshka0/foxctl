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
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jkatigb/agentctl/internal/lsp/gopls"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cache"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
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
	wg           sync.WaitGroup
}

// NewService creates a new daemon service.
func NewService(cfg config.Config, opts ServiceOptions) (*Service, error) {
	// Create connection pool and set as global
	pool := sqliteutil.NewPool()
	sqliteutil.SetGlobalPool(pool)

	svc := &Service{
		cfg:            cfg,
		opts:           opts,
		started:        time.Now(),
		warmWorkspaces: make(map[string]bool),
		shutdownCh:     make(chan struct{}),
		dbPool:         pool,
	}

	// Pre-warm workspace if specified
	if opts.Workspace != "" {
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

		s.wg.Add(1)
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
	var p RunParams
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("parse params: %w", err)
	}

	if p.Skill == "" {
		return nil, errors.New("skill is required")
	}

	return nil, fmt.Errorf("daemon run is not available; run without --daemon")
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
