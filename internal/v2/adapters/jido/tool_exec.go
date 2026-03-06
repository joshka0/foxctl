package jido

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	defaultAgentctlBinary    = "agentctl"
	defaultToolCallTimeout   = 2 * time.Minute
	agentctlRunInputFileFlag = "--input-file"
)

// ToolCommandSpec configures how Jido agents invoke Go-backed agentctl tools.
type ToolCommandSpec struct {
	BinaryPath     string
	Workspace      string
	AllowedTools   []string
	DefaultTimeout time.Duration
	ExtraArgs      []string
}

// ToolCommandRequest is one tool execution intent from runtime.
type ToolCommandRequest struct {
	ToolName string
	Input    json.RawMessage
	Timeout  time.Duration
}

// ToolCommand is a process-ready command for `agentctl run`.
type ToolCommand struct {
	Path    string
	Args    []string
	Stdin   []byte
	Timeout time.Duration
}

// BuildToolCommand returns a deterministic command line for running one tool
// through the local Go `agentctl` binary.
func BuildToolCommand(spec ToolCommandSpec, req ToolCommandRequest) (ToolCommand, error) {
	toolName := strings.TrimSpace(req.ToolName)
	if toolName == "" {
		return ToolCommand{}, fmt.Errorf("tool_name is required")
	}
	if !toolAllowed(spec.AllowedTools, toolName) {
		return ToolCommand{}, fmt.Errorf("tool %q is not in allowlist", toolName)
	}

	path := strings.TrimSpace(spec.BinaryPath)
	if path == "" {
		path = defaultAgentctlBinary
	}

	args := []string{"run", toolName}

	workspace := strings.TrimSpace(spec.Workspace)
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}

	if len(spec.ExtraArgs) > 0 {
		args = append(args, spec.ExtraArgs...)
	}

	var stdin []byte
	if len(req.Input) > 0 {
		args = append(args, agentctlRunInputFileFlag, "-")
		stdin = append([]byte(nil), req.Input...)
	}

	timeout := req.Timeout
	if timeout <= 0 {
		timeout = spec.DefaultTimeout
	}
	if timeout <= 0 {
		timeout = defaultToolCallTimeout
	}

	return ToolCommand{
		Path:    path,
		Args:    args,
		Stdin:   stdin,
		Timeout: timeout,
	}, nil
}

func toolAllowed(allowed []string, toolName string) bool {
	if len(allowed) == 0 {
		return true
	}
	want := strings.ToLower(strings.TrimSpace(toolName))
	for _, name := range allowed {
		if strings.ToLower(strings.TrimSpace(name)) == want {
			return true
		}
	}
	return false
}
