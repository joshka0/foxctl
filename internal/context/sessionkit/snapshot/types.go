package snapshot

import (
	"time"
)

// Snapshot represents the captured session state.
type Snapshot struct {
	SnapshotID   string            `json:"snapshot_id"`
	SessionID    string            `json:"session_id,omitempty"`
	Trigger      string            `json:"trigger"` // "pre_compact", "manual", "session_end"
	Workspace    string            `json:"workspace"`
	Timestamp    time.Time         `json:"timestamp"`
	ActiveTask   *TaskInfo         `json:"active_task,omitempty"`
	ActivePlan   *PlanInfo         `json:"active_plan,omitempty"`
	PendingTodos []TaskInfo        `json:"pending_todos,omitempty"`
	Decisions    []string          `json:"decisions,omitempty"`
	Insights     []string          `json:"insights,omitempty"`
	Summary      string            `json:"summary,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// PlanInfo represents a simplified plan for the snapshot.
type PlanInfo struct {
	FilePath    string   `json:"file_path"`
	FileName    string   `json:"file_name"`
	Title       string   `json:"title"`
	ContentHash string   `json:"content_hash"`
	Sections    []string `json:"sections,omitempty"`     // Top-level section titles
	LinkedTasks int      `json:"linked_tasks,omitempty"` // Number of tasks linked to this plan
	ModTime     string   `json:"mod_time,omitempty"`     // ISO format
}

// TaskInfo represents a simplified task for the snapshot.
type TaskInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	Notes       string `json:"notes,omitempty"`
	Gotchas     string `json:"gotchas,omitempty"`
}

// CaptureOptions configures snapshot capture.
type CaptureOptions struct {
	Trigger      string // "pre_compact", "manual", "session_end"
	Workspace    string
	SessionID    string
	Summary      string
	IncludePlan  bool // Include active plan info
	IncludeTodos bool // Include pending todos
}

// SaveResult contains information about a saved snapshot.
type SaveResult struct {
	SnapshotID    string         `json:"snapshot_id"`
	ItemsCaptured map[string]int `json:"items_captured"`
}

// RestoreContext contains restored session context for injection.
type RestoreContext struct {
	Snapshot       *Snapshot
	FormattedText  string
	RelatedWindows []WindowMatch
}

// WindowMatch represents a matching context window from past sessions.
type WindowMatch struct {
	SessionID    string  `json:"session_id"`
	WindowIndex  int     `json:"window_index"`
	Summary      string  `json:"summary,omitempty"`
	Similarity   float64 `json:"similarity"`
	MessageCount int     `json:"message_count,omitempty"`
}
