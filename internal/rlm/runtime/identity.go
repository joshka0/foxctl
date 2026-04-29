package runtime

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/rlm"
)

const (
	defaultRunID    = "run-unknown"
	defaultAgentID  = "agent-root"
	outputRootDir   = "out"
	childAgentLabel = "rlm"
)

// IdentityPlan is the deterministic identity and namespace plan for one RLM run.
type IdentityPlan struct {
	RunID           string
	AgentID         string
	ParentAgentID   string
	OutputRoot      string
	OutputNamespace string
}

// PlanIdentity returns a filesystem-safe, readable identity plan for one run.
func PlanIdentity(task rlm.Task) IdentityPlan {
	runID := sanitizeSegment(task.RunID, defaultRunID)
	agentID := sanitizeAgentPath(task.AgentID, defaultAgentID)

	return IdentityPlan{
		RunID:           runID,
		AgentID:         agentID,
		ParentAgentID:   sanitizeOptionalAgentPath(task.ParentAgentID),
		OutputRoot:      ResolveOutputRoot(task.OutputRoot, task.WorkspaceRoot),
		OutputNamespace: buildOutputNamespace(runID, agentID),
	}
}

// WithTaskIdentity applies identity fields onto a task.
func WithTaskIdentity(task rlm.Task, identity IdentityPlan) rlm.Task {
	task.RunID = identity.RunID
	task.AgentID = identity.AgentID
	task.ParentAgentID = identity.ParentAgentID
	task.OutputRoot = identity.OutputRoot
	task.OutputNamespace = identity.OutputNamespace
	return task
}

// ChildIdentity allocates a deterministic readable child identity.
func ChildIdentity(parent IdentityPlan, ordinal int) IdentityPlan {
	if ordinal < 1 {
		ordinal = 1
	}
	childLeaf := fmt.Sprintf("%s-%04d", childAgentLabel, ordinal)
	childAgentID := sanitizeAgentPath(path.Join(parent.AgentID, childLeaf), defaultAgentID)
	return IdentityPlan{
		RunID:           parent.RunID,
		AgentID:         childAgentID,
		ParentAgentID:   parent.AgentID,
		OutputRoot:      parent.OutputRoot,
		OutputNamespace: buildOutputNamespace(parent.RunID, childAgentID),
	}
}

// ResolveOutputRoot returns the shared output root path for the run.
func ResolveOutputRoot(outputRoot, workspaceRoot string) string {
	if root := strings.TrimSpace(outputRoot); root != "" {
		return filepath.Clean(root)
	}
	return OutputRootPath(workspaceRoot)
}

// OutputRootPath returns the legacy deterministic output root path.
func OutputRootPath(workspaceRoot string) string {
	root := strings.TrimSpace(workspaceRoot)
	if root == "" {
		return outputRootDir
	}
	return filepath.Clean(filepath.Join(root, outputRootDir))
}

func buildOutputNamespace(runID, agentID string) string {
	return path.Join("runs", runID, "agents", sanitizeAgentPath(agentID, defaultAgentID))
}

func sanitizeOptionalAgentPath(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return sanitizeAgentPath(trimmed, defaultAgentID)
}

func sanitizeAgentPath(raw, fallback string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	parts := strings.Split(normalized, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		cleaned = append(cleaned, sanitizeSegment(part, "agent"))
	}
	if len(cleaned) == 0 {
		return sanitizeSegment(fallback, defaultAgentID)
	}
	return strings.Join(cleaned, "/")
}

func sanitizeSegment(raw, fallback string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return fallback
	}

	var b strings.Builder
	b.Grow(len(raw))
	lastDash := false
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return fallback
	}
	return out
}
