package rlm

import (
	"encoding/json"
	"time"
)

// Task describes one bounded RLM run request.
type Task struct {
	Prompt          string `json:"prompt"`
	Role            string `json:"role,omitempty"`
	RunID           string `json:"run_id,omitempty"`
	AgentID         string `json:"agent_id,omitempty"`
	ParentAgentID   string `json:"parent_agent_id,omitempty"`
	OutputRoot      string `json:"output_root,omitempty"`
	OutputNamespace string `json:"output_namespace,omitempty"`
	WorkspaceID     string `json:"workspace_id,omitempty"`
	WorkspaceRoot   string `json:"workspace_root,omitempty"`
	MaxDepth        int    `json:"max_depth,omitempty"`
	MaxIterations   int    `json:"max_iterations,omitempty"`
	MaxSubcalls     int    `json:"max_subcalls,omitempty"`
}

// Tool is a typed environment tool handle exposed to the RLM runtime.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	ReadOnly    bool            `json:"read_only"`
}

// Environment is the typed external state visible to the runtime.
type Environment struct {
	TopOfMind       map[string]any `json:"top_of_mind,omitempty"`
	LatestHandoff   map[string]any `json:"latest_handoff,omitempty"`
	ActiveThreadIDs []string       `json:"active_thread_ids,omitempty"`
	SceneHandles    []string       `json:"scene_handles,omitempty"`
	ArtifactHandles []string       `json:"artifact_handles,omitempty"`
	RepoHandles     []string       `json:"repo_handles,omitempty"`
	VaultHandles    []string       `json:"vault_handles,omitempty"`
	Tools           []Tool         `json:"tools,omitempty"`
}

// ExecResult is the result of one sandbox execution step.
type ExecResult struct {
	Output     string         `json:"output,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	DurationMS int64          `json:"duration_ms,omitempty"`
	ExecutedAt time.Time      `json:"executed_at,omitempty"`
}

// Result is the final RLM run result.
type Result struct {
	Answer         string         `json:"answer,omitempty"`
	EvidenceRefs   []string       `json:"evidence_refs,omitempty"`
	RetrievedPaths []string       `json:"retrieved_paths,omitempty"`
	Iterations     int            `json:"iterations,omitempty"`
	Subcalls       int            `json:"subcalls,omitempty"`
	TrajectoryID   string         `json:"trajectory_id,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}
