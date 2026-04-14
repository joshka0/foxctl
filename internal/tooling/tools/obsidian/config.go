package obsidian

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Transport selects how the adapter talks to Obsidian.
type Transport string

const (
	TransportCLI        Transport = "cli"
	TransportFilesystem Transport = "filesystem"
	TransportMCP        Transport = "mcp"
)

// VaultTarget identifies the target vault by name or path.
type VaultTarget struct {
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// Config configures the Obsidian client shell and vault targeting rules.
type Config struct {
	Target          VaultTarget   `json:"target"`
	Transport       Transport     `json:"transport"`
	CLIPath         string        `json:"cli_path,omitempty"`
	MCPServerName   string        `json:"mcp_server_name,omitempty"`
	WorkspaceRoot   string        `json:"workspace_root,omitempty"`
	PostCreateDelay time.Duration `json:"post_create_delay,omitempty"`
}

// DefaultConfig returns conservative defaults for local vault operations.
func DefaultConfig() Config {
	return Config{
		Transport:       TransportCLI,
		CLIPath:         "obsidian",
		PostCreateDelay: 2 * time.Second,
	}
}

// ResolveVaultTarget builds a vault target from explicit name/path inputs.
func ResolveVaultTarget(name, path string) (VaultTarget, error) {
	target := VaultTarget{
		Name: strings.TrimSpace(name),
		Path: strings.TrimSpace(path),
	}
	if err := target.Validate(); err != nil {
		return VaultTarget{}, err
	}
	if target.Path != "" {
		target.Path = filepath.Clean(target.Path)
	}
	return target, nil
}

// Validate checks that the target is well-formed.
func (t VaultTarget) Validate() error {
	hasName := strings.TrimSpace(t.Name) != ""
	hasPath := strings.TrimSpace(t.Path) != ""
	switch {
	case hasName && hasPath:
		return fmt.Errorf("obsidian: vault target must use either name or path, not both")
	case !hasName && !hasPath:
		return fmt.Errorf("obsidian: vault target requires a name or path")
	default:
		return nil
	}
}

// Display returns a stable human-readable vault selector.
func (t VaultTarget) Display() string {
	if strings.TrimSpace(t.Name) != "" {
		return t.Name
	}
	return filepath.Clean(t.Path)
}

// Validate checks that the client config is coherent.
func (c Config) Validate() error {
	if err := c.Target.Validate(); err != nil {
		return err
	}
	switch c.Transport {
	case "", TransportCLI, TransportFilesystem, TransportMCP:
	default:
		return fmt.Errorf("obsidian: unsupported transport %q", c.Transport)
	}
	if c.Transport == TransportCLI && strings.TrimSpace(c.CLIPath) == "" {
		return fmt.Errorf("obsidian: cli transport requires cli_path")
	}
	if c.Transport == TransportMCP && strings.TrimSpace(c.MCPServerName) == "" {
		return fmt.Errorf("obsidian: mcp transport requires mcp_server_name")
	}
	if c.PostCreateDelay < 0 {
		return fmt.Errorf("obsidian: post_create_delay must be >= 0")
	}
	return nil
}
