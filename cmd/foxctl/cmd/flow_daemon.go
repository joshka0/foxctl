package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/daemon"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Daemon routing for flow lifecycle commands
// ---------------------------------------------------------------------------

// flowDaemonClient abstracts the daemon flow RPC methods for testability.
type flowDaemonClient interface {
	IsRunning() bool
	EnsureRunning() error
	FlowStart(flowID, workspace string) (*daemon.FlowStartResult, error)
	FlowStop(flowID, workspace string) (*daemon.FlowStopResult, error)
	FlowPause(flowID, workspace string) (*daemon.FlowPauseResult, error)
	FlowStatus(flowID, workspace string) (*daemon.FlowStatusResult, error)
	FlowOutput(flowID, runID, nodeID string, data json.RawMessage, workspace string) (*daemon.FlowOutputResult, error)
}

// defaultFlowDaemonClient is the production implementation using the real daemon client.
type defaultFlowDaemonClient struct {
	client *daemon.Client
}

func (d *defaultFlowDaemonClient) IsRunning() bool {
	return d.client.IsRunning()
}

func (d *defaultFlowDaemonClient) EnsureRunning() error {
	return d.client.EnsureRunning()
}

func (d *defaultFlowDaemonClient) FlowStart(flowID, workspace string) (*daemon.FlowStartResult, error) {
	return d.client.FlowStart(flowID, workspace)
}

func (d *defaultFlowDaemonClient) FlowStop(flowID, workspace string) (*daemon.FlowStopResult, error) {
	return d.client.FlowStop(flowID, workspace)
}

func (d *defaultFlowDaemonClient) FlowPause(flowID, workspace string) (*daemon.FlowPauseResult, error) {
	return d.client.FlowPause(flowID, workspace)
}

func (d *defaultFlowDaemonClient) FlowStatus(flowID, workspace string) (*daemon.FlowStatusResult, error) {
	return d.client.FlowStatus(flowID, workspace)
}

func (d *defaultFlowDaemonClient) FlowOutput(flowID, runID, nodeID string, data json.RawMessage, workspace string) (*daemon.FlowOutputResult, error) {
	return d.client.FlowOutput(flowID, runID, nodeID, data, workspace)
}

// newFlowDaemonClient is the factory for creating daemon clients.
// Overridden in tests to inject mock clients.
var newFlowDaemonClient = func() flowDaemonClient {
	return &defaultFlowDaemonClient{client: daemon.NewClient()}
}

// flowDaemonAutoStart controls whether the CLI attempts to auto-start the daemon
// when it's not running. Defaults to true. Set to false in tests.
var flowDaemonAutoStart = true

// routeFlowViaDaemon attempts to route a flow lifecycle command through the daemon.
// Returns (true, nil) if the command was handled via daemon.
// Returns (false, nil) if daemon is not available (caller should fall back to in-process).
// Returns (true, error) if daemon was reached but the RPC failed.
func routeFlowViaDaemon(cmd *cobra.Command, method, flowRef string) (bool, error) {
	stderr := cmd.ErrOrStderr()
	ws := flowResolveWorkspace(flowWorkspaceFlag)

	daemonClient := newFlowDaemonClient()
	wasAutoStarted := false

	// Step 1: Check if daemon is running.
	if !daemonClient.IsRunning() {
		// Step 2: Attempt auto-start if enabled.
		if flowDaemonAutoStart {
			fmt.Fprintf(stderr, "flow: daemon not running, attempting auto-start...\n")
			if err := daemonClient.EnsureRunning(); err != nil {
				fmt.Fprintf(stderr, "flow: auto-start failed (%v), falling back to in-process execution\n", err)
				return false, nil
			}
			fmt.Fprintf(stderr, "flow: daemon started successfully\n")
			wasAutoStarted = true
		} else {
			// Auto-start disabled, fall back silently.
			return false, nil
		}
	}

	// Resolve name to ID before sending to daemon.
	flowID, err := resolveFlowIDForDaemon(cmd, daemonClient, flowRef, ws)
	if err != nil {
		// If we can't resolve via store, fall back to in-process
		// (the in-process path has its own resolution logic).
		return false, nil
	}

	// Step 3: Dispatch to daemon.
	switch method {
	case "start":
		return routeFlowStart(cmd, daemonClient, flowID, ws, stderr, wasAutoStarted)
	case "stop":
		return routeFlowStop(cmd, daemonClient, flowID, ws, stderr, wasAutoStarted)
	case "pause":
		return routeFlowPause(cmd, daemonClient, flowID, ws, stderr, wasAutoStarted)
	case "status":
		return routeFlowStatus(cmd, daemonClient, flowID, ws, stderr, wasAutoStarted)
	default:
		return false, nil
	}
}

// resolveFlowIDForDaemon resolves a flow name to an ID for daemon RPC.
// Uses the store directly for resolution since the daemon expects IDs.
func resolveFlowIDForDaemon(cmd *cobra.Command, dc flowDaemonClient, flowRef, workspace string) (string, error) {
	// If it looks like an ID (ULID format), return as-is.
	if isULID(flowRef) {
		return flowRef, nil
	}

	// It's a name — resolve via store.
	ctx := cmd.Context()
	store, err := openFlowStore(ctx, flowWorkspaceFlag)
	if err != nil {
		return "", err
	}
	defer store.Close()

	f, err := resolveFlow(ctx, store, workspace, flowRef)
	if err != nil {
		return "", err
	}
	return f.ID, nil
}

// isULID checks if a string looks like a ULID (26 chars, uppercase alphanumeric).
func isULID(s string) bool {
	if len(s) != 26 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

// routeFlowStart routes flow start through the daemon.
func routeFlowStart(cmd *cobra.Command, dc flowDaemonClient, flowID, workspace string, stderr io.Writer, wasAutoStarted bool) (bool, error) {
	result, err := dc.FlowStart(flowID, workspace)
	if err != nil {
		// Check if the error is a connection error — if so, fall back.
		if isConnectionError(err) {
			fmt.Fprintf(stderr, "flow: daemon connection lost, falling back to in-process execution\n")
			return false, nil
		}
		// If the daemon returns ENOTFOUND, the daemon's flow engine may not
		// have the workspace configured. Fall back to in-process which will
		// properly resolve from the local store.
		if isNotFoundError(err) {
			fmt.Fprintf(stderr, "flow: daemon does not have flow %s, falling back to in-process execution\n", flowID)
			return false, nil
		}
		// Daemon returned a real error — map it to an envelope.
		return true, mapDaemonFlowError(cmd, "flow/start", err)
	}

	return true, protocol.WriteOK(cmd.OutOrStdout(), "flow/start", map[string]any{
		"id":        flowID,
		"state":     result.State,
		"run_id":    result.RunID,
		"workspace": workspace,
	})
}

// routeFlowStop routes flow stop through the daemon.
func routeFlowStop(cmd *cobra.Command, dc flowDaemonClient, flowID, workspace string, stderr io.Writer, wasAutoStarted bool) (bool, error) {
	result, err := dc.FlowStop(flowID, workspace)
	if err != nil {
		if isConnectionError(err) {
			fmt.Fprintf(stderr, "flow: daemon connection lost, falling back to in-process execution\n")
			return false, nil
		}
		if isNotFoundError(err) {
			fmt.Fprintf(stderr, "flow: daemon does not have flow %s, falling back to in-process execution\n", flowID)
			return false, nil
		}
		return true, mapDaemonFlowError(cmd, "flow/stop", err)
	}

	return true, protocol.WriteOK(cmd.OutOrStdout(), "flow/stop", map[string]any{
		"id":      flowID,
		"state":   result.State,
		"stopped": true,
	})
}

// routeFlowPause routes flow pause through the daemon.
func routeFlowPause(cmd *cobra.Command, dc flowDaemonClient, flowID, workspace string, stderr io.Writer, wasAutoStarted bool) (bool, error) {
	result, err := dc.FlowPause(flowID, workspace)
	if err != nil {
		if isConnectionError(err) {
			fmt.Fprintf(stderr, "flow: daemon connection lost, falling back to in-process execution\n")
			return false, nil
		}
		if isNotFoundError(err) {
			fmt.Fprintf(stderr, "flow: daemon does not have flow %s, falling back to in-process execution\n", flowID)
			return false, nil
		}
		return true, mapDaemonFlowError(cmd, "flow/pause", err)
	}

	return true, protocol.WriteOK(cmd.OutOrStdout(), "flow/pause", map[string]any{
		"id":     flowID,
		"state":  result.State,
		"paused": true,
	})
}

// routeFlowStatus routes flow status through the daemon.
func routeFlowStatus(cmd *cobra.Command, dc flowDaemonClient, flowID, workspace string, stderr io.Writer, wasAutoStarted bool) (bool, error) {
	result, err := dc.FlowStatus(flowID, workspace)
	if err != nil {
		if isConnectionError(err) {
			fmt.Fprintf(stderr, "flow: daemon connection lost, falling back to in-process execution\n")
			return false, nil
		}
		if isNotFoundError(err) {
			fmt.Fprintf(stderr, "flow: daemon does not have flow %s, falling back to in-process execution\n", flowID)
			return false, nil
		}
		return true, mapDaemonFlowError(cmd, "flow/status", err)
	}

	return true, protocol.WriteOK(cmd.OutOrStdout(), "flow/status", map[string]any{
		"id":         flowID,
		"flow_state": result.State,
		"run_id":     result.RunID,
		"nodes":      result.Nodes,
		"edges":      result.Edges,
		"workspace":  workspace,
	})
}

// mapDaemonFlowError maps a daemon RPC error to a CLI error envelope.
func mapDaemonFlowError(cmd *cobra.Command, command string, err error) error {
	errMsg := err.Error()
	code := protocol.ErrorCodeERuntime

	// Map daemon error patterns to envelope error codes.
	switch {
	case containsAny(errMsg, "ENOTFOUND", "not found"):
		code = protocol.ErrorCodeENotFound
	case containsAny(errMsg, "EARG", "is required"):
		code = protocol.ErrorCodeEARG
	case containsAny(errMsg, "EALREADY", "already running"):
		code = protocol.ErrorCodeEARG
	case containsAny(errMsg, "ESTATE", "not running", "already stopped"):
		code = protocol.ErrorCodeEARG
	case containsAny(errMsg, "ECYCLE", "cycle"):
		code = protocol.ErrorCodeEARG
	case containsAny(errMsg, "EINVALID", "no nodes", "no source nodes"):
		code = protocol.ErrorCodeEARG
	case containsAny(errMsg, "EFLOW", "not initialized"):
		code = protocol.ErrorCodeESkillDown
	}

	return protocol.WriteError(cmd.OutOrStdout(), command, code, errMsg, nil)
}

// isConnectionError checks if an error is a daemon connection error.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg,
		"connect to daemon",
		"connection refused",
		"no such file or directory",
		"read response",
		"send request",
	)
}

// isNotFoundError checks if an error indicates the flow was not found in the daemon.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg, "ENOTFOUND", "not found")
}

// containsAny checks if s contains any of the substrings.
func containsAny(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}

// routeFlowOutputViaDaemon routes flow output push through the daemon.
func routeFlowOutputViaDaemon(cmd *cobra.Command, runID, nodeID string, data json.RawMessage, workspace string) (bool, error) {
	stderr := cmd.ErrOrStderr()

	daemonClient := newFlowDaemonClient()

	// Check if daemon is running
	if !daemonClient.IsRunning() {
		if flowDaemonAutoStart {
			fmt.Fprintf(stderr, "flow: daemon not running, attempting auto-start...\n")
			if err := daemonClient.EnsureRunning(); err != nil {
				fmt.Fprintf(stderr, "flow: auto-start failed (%v), falling back to in-process\n", err)
				return false, nil
			}
		} else {
			return false, nil
		}
	}

	// Pass both run_id and node_id to the daemon. The daemon will
	// resolve the flow_id from the run_id.
	result, err := daemonClient.FlowOutput("", runID, nodeID, data, workspace)
	if err != nil {
		if isConnectionError(err) {
			fmt.Fprintf(stderr, "flow: daemon connection lost, falling back to in-process\n")
			return false, nil
		}
		return true, mapDaemonFlowError(cmd, "flow/output", err)
	}

	return true, protocol.WriteOK(cmd.OutOrStdout(), "flow/output", map[string]any{
		"run_id":    runID,
		"flow_id":   result.FlowID,
		"node_id":   result.NodeID,
		"workspace": workspace,
		"ok":        result.OK,
	})
}

// Compile-time interface checks.
var (
	_ flowDaemonClient = (*defaultFlowDaemonClient)(nil)
)
