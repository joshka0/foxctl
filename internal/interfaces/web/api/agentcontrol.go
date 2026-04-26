package api

import "github.com/joshka0/foxctl/internal/runtime/daemon"

// AgentControl abstracts the daemon client operations used by the web API
// handlers. This decouples the HTTP layer from direct daemon.NewClient()
// calls, enabling future remote daemon or multi-node deployments.
type AgentControl interface {
	// EnsureRunning starts the daemon if not already running.
	EnsureRunning() error

	// IsRunning reports whether the daemon is currently reachable.
	IsRunning() bool

	// Spawn creates a new agent session via the daemon.
	Spawn(params daemon.AgentSpawnParams) (*daemon.AgentSpawnResult, error)

	// List returns active agent sessions.
	List() (*daemon.AgentListResult, error)

	// Kill terminates an agent session by session ID.
	Kill(sessionID string) (*daemon.AgentKillResult, error)
}

// localDaemonControl wraps a daemon.Client to satisfy AgentControl.
type localDaemonControl struct {
	client *daemon.Client
}

// NewLocalDaemonControl returns an AgentControl backed by the local daemon.
func NewLocalDaemonControl() AgentControl {
	return &localDaemonControl{client: daemon.NewClient()}
}

func (c *localDaemonControl) EnsureRunning() error {
	return c.client.EnsureRunning()
}

func (c *localDaemonControl) IsRunning() bool {
	return c.client.IsRunning()
}

func (c *localDaemonControl) Spawn(params daemon.AgentSpawnParams) (*daemon.AgentSpawnResult, error) {
	return c.client.AgentSpawn(params)
}

func (c *localDaemonControl) List() (*daemon.AgentListResult, error) {
	return c.client.AgentList()
}

func (c *localDaemonControl) Kill(sessionID string) (*daemon.AgentKillResult, error) {
	return c.client.AgentKill(sessionID)
}

// defaultAgentControl is the package-level AgentControl used by handler
// functions. It defaults to a local daemon client but can be overridden
// for testing or remote daemon deployments.
var defaultAgentControl AgentControl = NewLocalDaemonControl()

// SetAgentControl overrides the default AgentControl. Call during server
// startup or in tests.
func SetAgentControl(ac AgentControl) {
	if ac != nil {
		defaultAgentControl = ac
	}
}

// agentControl returns the current AgentControl instance.
func agentControl() AgentControl {
	return defaultAgentControl
}
