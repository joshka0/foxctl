package workflow

import (
	"fmt"

	"github.com/jkatigb/agentctl/internal/workflow/frontmatter"
)

// NewOrchestrationConfigReloader builds a WORKFLOW.md frontmatter reloader
// using the default dispatch preflight validator.
func NewOrchestrationConfigReloader(explicitPath, cwd string, opts frontmatter.DecodeOptions) (*frontmatter.Reloader, error) {
	path, err := frontmatter.ResolveWorkflowPath(explicitPath, cwd)
	if err != nil {
		return nil, fmt.Errorf("workflow orchestration config: resolve path: %w", err)
	}
	return frontmatter.NewReloader(path, opts, nil), nil
}

// LoadOrchestrationConfig resolves, decodes, validates, and returns one effective
// orchestration config snapshot from WORKFLOW.md.
//
// This helper is intentionally one-shot. Last-known-good retention semantics
// require a long-lived reloader instance created via NewOrchestrationConfigReloader.
func LoadOrchestrationConfig(explicitPath, cwd string, opts frontmatter.DecodeOptions) (frontmatter.Snapshot, error) {
	loader, err := NewOrchestrationConfigReloader(explicitPath, cwd, opts)
	if err != nil {
		return frontmatter.Snapshot{}, err
	}
	return loader.Reload()
}
