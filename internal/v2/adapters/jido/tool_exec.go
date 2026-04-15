package jido

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	toolbridge "github.com/joshka0/foxctl/internal/v2/adapters/toolbridge"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/profiles"
	runtimetoolnames "github.com/joshka0/foxctl/internal/v2/runtime/toolnames"
)

const (
	defaultFoxctlBinary    = "foxctl"
	defaultToolCallTimeout = 2 * time.Minute
	foxctlRunInputFileFlag = "--input-file"
)

// ToolCommandSpec configures how Jido agents invoke Go-backed foxctl tools.
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

// ToolCommand is a process-ready command for `foxctl run`.
type ToolCommand struct {
	Path    string
	Args    []string
	Stdin   []byte
	Timeout time.Duration
}

// BuildToolCommand returns a deterministic command line for running one tool
// through the local Go `foxctl` binary.
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
		path = defaultFoxctlBinary
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
		args = append(args, foxctlRunInputFileFlag, "-")
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

// NewDefaultToolCommandSpec derives a Jido-facing allowlist from the real v2
// default tool catalog for one process profile.
func NewDefaultToolCommandSpec(
	profile coretool.ProcessProfile,
	workspace string,
	binary string,
	specs map[coretool.ProcessProfile]profiles.ProfileSpec,
	includeExtensions bool,
) (ToolCommandSpec, error) {
	catalog, err := toolbridge.NewDefaultCatalog(specs, includeExtensions)
	if err != nil {
		return ToolCommandSpec{}, err
	}
	defs := catalog.ForProfile(profile)
	allowed := make([]string, 0, len(defs))
	for _, def := range defs {
		allowed = append(allowed, def.Name)
	}
	return ToolCommandSpec{
		BinaryPath:     strings.TrimSpace(binary),
		Workspace:      strings.TrimSpace(workspace),
		AllowedTools:   allowed,
		DefaultTimeout: defaultToolCallTimeout,
	}, nil
}

func toolAllowed(allowed []string, toolName string) bool {
	if len(allowed) == 0 {
		return true
	}
	want := runtimetoolnames.Canonical(toolName)
	for _, name := range allowed {
		if runtimetoolnames.Canonical(name) == want {
			return true
		}
	}
	return false
}
