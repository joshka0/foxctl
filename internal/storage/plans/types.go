package plans

import (
	"time"
)

// PlanInfo represents metadata about a Claude Code plan file.
type PlanInfo struct {
	// FilePath is the absolute path to the plan file.
	FilePath string `json:"file_path"`
	// FileName is just the base name (e.g., "keen-finding-pixel.md").
	FileName string `json:"file_name"`
	// Title is extracted from the first # heading in the file.
	Title string `json:"title"`
	// ContentHash is the SHA256 hash of the file content.
	ContentHash string `json:"content_hash"`
	// ModTime is the file's last modification time.
	ModTime time.Time `json:"mod_time"`
	// Sections are the parsed ## and ### sections from the plan.
	Sections []Section `json:"sections,omitempty"`
	// LinkedTaskIDs are agentctl task IDs associated with this plan.
	LinkedTaskIDs []string `json:"linked_task_ids,omitempty"`
	// Status indicates the plan state: "active", "completed", "archived".
	Status string `json:"status,omitempty"`
}

// Section represents a markdown section (## or ###) in a plan file.
type Section struct {
	// Level is the heading level (2 for ##, 3 for ###, etc.).
	Level int `json:"level"`
	// Title is the heading text without the # prefix.
	Title string `json:"title"`
	// Content is the text content under this section (before next heading).
	Content string `json:"content,omitempty"`
	// TaskID is the linked agentctl task ID, if any.
	TaskID string `json:"task_id,omitempty"`
	// LineNumber is the line number where this section starts (1-indexed).
	LineNumber int `json:"line_number,omitempty"`
	// Children are nested subsections (e.g., ### under ##).
	Children []Section `json:"children,omitempty"`
}

// Plan status constants.
const (
	// StatusActive indicates the plan is currently being worked on.
	StatusActive = "active"
	// StatusCompleted indicates all linked tasks are done.
	StatusCompleted = "completed"
	// StatusArchived indicates the plan has been moved to archived/.
	StatusArchived = "archived"
)

// DetectOptions configures plan detection behavior.
type DetectOptions struct {
	// PlansDir is the directory to scan (default: ~/.claude/plans).
	PlansDir string
	// Since filters to plans modified after this time.
	Since time.Time
	// IncludeArchived includes plans in the archived/ subdirectory.
	IncludeArchived bool
	// Limit is the maximum number of plans to return (0 = unlimited).
	Limit int
}

// ParseOptions configures plan parsing behavior.
type ParseOptions struct {
	// MaxSectionDepth limits how deep to parse nested sections (default: 4).
	MaxSectionDepth int
	// IncludeContent includes the text content of each section.
	IncludeContent bool
	// ExtractSteps attempts to identify actionable steps from sections.
	ExtractSteps bool
}

// DefaultParseOptions returns sensible defaults for parsing.
func DefaultParseOptions() ParseOptions {
	return ParseOptions{
		MaxSectionDepth: 4,
		IncludeContent:  true,
		ExtractSteps:    false,
	}
}

// Step represents an actionable step extracted from a plan.
// This is used when importing plans as tasks.
type Step struct {
	// Title is the step description.
	Title string `json:"title"`
	// Description provides additional context.
	Description string `json:"description,omitempty"`
	// SectionPath is the path of section titles (e.g., ["Phase 1", "Step 1.1"]).
	SectionPath []string `json:"section_path,omitempty"`
	// Order is the step's position in the plan (1-indexed).
	Order int `json:"order"`
	// DependsOn lists step titles this step depends on.
	DependsOn []string `json:"depends_on,omitempty"`
}

// ImportResult describes the result of importing a plan as tasks.
type ImportResult struct {
	// PlanFile is the source plan path.
	PlanFile string `json:"plan_file"`
	// EpicTaskID is the root task ID created for the plan.
	EpicTaskID string `json:"epic_task_id,omitempty"`
	// TasksCreated is the number of tasks created.
	TasksCreated int `json:"tasks_created"`
	// Tasks are the created task references.
	Tasks []TaskRef `json:"tasks,omitempty"`
	// DryRun indicates this was a preview (no tasks created).
	DryRun bool `json:"dry_run"`
}

// TaskRef is a lightweight reference to a created task.
type TaskRef struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	ParentID string `json:"parent_id,omitempty"`
}

// PlanProgress tracks completion status of a plan.
type PlanProgress struct {
	// Total is the number of linked tasks.
	Total int `json:"total"`
	// Completed is the number of tasks with status "completed".
	Completed int `json:"completed"`
	// InProgress is the number of tasks with status "in_progress".
	InProgress int `json:"in_progress"`
	// Pending is the number of tasks with status "pending".
	Pending int `json:"pending"`
	// Blocked is the number of tasks with status "blocked".
	Blocked int `json:"blocked"`
	// PercentComplete is (Completed / Total) * 100.
	PercentComplete float64 `json:"percent_complete"`
}

// CalculateProgress computes progress from task statuses.
func CalculateProgress(statuses map[string]string) PlanProgress {
	p := PlanProgress{Total: len(statuses)}
	for _, status := range statuses {
		switch status {
		case "completed":
			p.Completed++
		case "in_progress":
			p.InProgress++
		case "pending":
			p.Pending++
		case "blocked":
			p.Blocked++
		}
	}
	if p.Total > 0 {
		p.PercentComplete = float64(p.Completed) / float64(p.Total) * 100
	}
	return p
}
