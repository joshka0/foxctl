// Package foxproxbridge defines the integration surface between foxctl and an
// Foxprox daemon. Foxctl imports ONLY this package; the concrete Foxprox
// implementation lives in the foxprox/ subtree (or an external module) and
// registers itself via RegisterDaemon at program startup.
//
// This decoupling lets foxctl build and run without Foxprox when the bridge is
// not wired, while still supporting full Foxprox functionality when the
// --foxprox flag is passed.
package foxproxbridge

import (
	"context"
	"time"
)

// DaemonLifecycle is the minimal lifecycle surface foxctl needs to manage an
// embedded Foxprox daemon. The concrete implementation is provided by the
// foxprox/ subtree at link time.
type DaemonLifecycle interface {
	// SocketPath returns the Unix-domain socket the daemon listens on.
	SocketPath() string
	// Start bootstraps the broker and listener. Must be called before Wait.
	Start() error
	// Wait blocks until the daemon exits or ctx is cancelled.
	Wait(ctx context.Context) error
	// Shutdown gracefully stops the daemon within the given timeout.
	Shutdown(ctx context.Context) error
}

// DaemonOptions carries the configuration foxctl passes to the concrete
// daemon constructor. Fields mirror foxprox/daemon.Options but without
// importing that package.
type DaemonOptions struct {
	DataDir    string
	SocketPath string
	LogWriter  interface{} // io.Writer — kept as any to avoid io import cycle edge cases
}

// ErrBrokerAlreadyRunning is returned by Start when another daemon is
// already listening on the target socket. The caller should treat this as
// a non-error "reuse" signal.
var ErrBrokerAlreadyRunning error // set by the concrete registration

// DaemonFactory constructs a DaemonLifecycle from options.
// The returned function signature matches the concrete foxprox daemon constructor.
type DaemonFactory func(opts DaemonOptions) (DaemonLifecycle, error)

var factory DaemonFactory

// RegisterDaemon sets the concrete factory. Call once from an init() or
// main() in the package that links the real Foxprox implementation.
func RegisterDaemon(f DaemonFactory, alreadyRunningErr error) {
	factory = f
	ErrBrokerAlreadyRunning = alreadyRunningErr
}

// NewDaemon calls the registered factory. Returns nil, nil if no factory
// has been registered (Foxprox support not linked).
func NewDaemon(opts DaemonOptions) (DaemonLifecycle, error) {
	if factory == nil {
		return nil, ErrNotLinked
	}
	return factory(opts)
}

// Linked reports whether a concrete Foxprox implementation has been registered.
func Linked() bool {
	return factory != nil
}

// ErrNotLinked is returned when Foxprox operations are attempted but no
// concrete implementation has been registered.
var ErrNotLinked = errNotLinked{}

type errNotLinked struct{}

func (errNotLinked) Error() string { return "foxprox: implementation not linked" }

// DefaultSocketPath returns the canonical socket path for Foxprox. When the
// concrete implementation is linked, this delegates to the real function.
// Otherwise returns a sensible default.
func DefaultSocketPath() string {
	if defaultSocketFn != nil {
		return defaultSocketFn()
	}
	return "/tmp/foxprox.sock"
}

var defaultSocketFn func() string

// RegisterDefaultSocketPath sets the socket path resolver from the concrete
// implementation.
func RegisterDefaultSocketPath(fn func() string) {
	defaultSocketFn = fn
}

// --- Client-side types for the HTTP facade ---
// These mirror the foxprox/transport/httpjson wire types but live here so the
// web API handler does not need to import the Foxprox module directly.

// SessionInfo is a portable subset of the Foxprox session response.
type SessionInfo struct {
	ID               string    `json:"id"`
	Status           string    `json:"status"`
	PID              int       `json:"pid"`
	CreatedAt        time.Time `json:"created_at,omitempty"`
	ExitedAt         time.Time `json:"exited_at,omitempty"`
	ExitCode         int       `json:"exit_code,omitempty"`
	ExitError        string    `json:"exit_error,omitempty"`
	LastSeq          uint64    `json:"last_seq"`
	Cmd              []string  `json:"cmd"`
	Cwd              string    `json:"cwd,omitempty"`
	Adapter          string    `json:"adapter,omitempty"`
	SubmitKey        string    `json:"submit_key,omitempty"`
	EnableRawBytes   bool      `json:"enable_raw_bytes,omitempty"`
	OutputBytesTotal int64     `json:"output_bytes_total"`
}

// RoomInfo is a portable subset of the Foxprox room response.
type RoomInfo struct {
	ID          string    `json:"id"`
	Workspace   string    `json:"workspace,omitempty"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	ArchivedAt  time.Time `json:"archived_at,omitempty"`
	CreatedAt   time.Time `json:"created_at,omitempty"`
}

// MemberInfo is a portable subset of the Foxprox room member response.
type MemberInfo struct {
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Role      string `json:"role,omitempty"`
}

// ReadinessInfo is a portable subset of the Foxprox readiness response.
type ReadinessInfo struct {
	Idle       bool    `json:"idle"`
	IdleForMS  int64   `json:"idle_for_ms"`
	ScreenMatch bool   `json:"screen_match,omitempty"`
}

// HTTPClient is the interface the web API handler uses to talk to an Foxprox
// daemon. The concrete implementation wraps the real foxprox/client.
type HTTPClient interface {
	Health(ctx context.Context) error
	ListSessions(ctx context.Context) ([]SessionInfo, error)
	CreateSession(ctx context.Context, cmd []string, cwd string, env []string, rows, cols uint16, adapter, submitKey string, enableRawBytes bool) (SessionInfo, error)
	DeleteSession(ctx context.Context, id string) error
	SessionReadiness(ctx context.Context, id string) (ReadinessInfo, error)
	SessionScreen(ctx context.Context, id string) (map[string]any, error)
	ListRooms(ctx context.Context) ([]RoomInfo, error)
	CreateRoom(ctx context.Context, workspace, title, description string) (RoomInfo, error)
	RoomMembers(ctx context.Context, roomID string) ([]MemberInfo, error)
	JoinRoom(ctx context.Context, roomID, agentID, sessionID, role string, canMutate bool) (MemberInfo, error)
	SendMessage(ctx context.Context, roomID, text, source, submitKey string, skipAgents []string, awaitActivityMS, awaitReadyMS int64) (map[string]any, error)
}

var clientFactory func(socketPath string) HTTPClient

// RegisterClientFactory sets the concrete HTTP client constructor.
func RegisterClientFactory(fn func(socketPath string) HTTPClient) {
	clientFactory = fn
}

// NewClient creates an Foxprox HTTP client. Returns nil if no factory is registered.
func NewClient(socketPath string) HTTPClient {
	if clientFactory == nil {
		return nil
	}
	return clientFactory(socketPath)
}
