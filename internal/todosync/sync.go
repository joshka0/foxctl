package todosync

import (
	"context"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

// SyncResult contains the outcome of a sync operation
type SyncResult struct {
	Created   int      `json:"created"`
	Updated   int      `json:"updated"`
	Completed int      `json:"completed"`
	Mapped    int      `json:"mapped"`
	Unmapped  int      `json:"unmapped"`
	DepsAdded int      `json:"deps_added"`
	Warnings  []string `json:"warnings,omitempty"`
}

// InboundSyncInput defines parameters for syncing from provider to agentctl
type InboundSyncInput struct {
	WorkspaceID string       `json:"workspace_id" validate:"required"`
	SessionID   string       `json:"session_id"`
	Todos       []ClaudeTodo `json:"todos" validate:"required"`
	DryRun      bool         `json:"dry_run"`
}

// InboundSyncResult contains the outcome of inbound sync
type InboundSyncResult struct {
	SyncResult
	Tasks []tasks.Task `json:"tasks,omitempty"` // Tasks that were created/updated (dry run shows what would happen)
}

// OutboundSyncInput defines parameters for syncing from agentctl to provider
type OutboundSyncInput struct {
	WorkspaceID string           `json:"workspace_id" validate:"required"`
	SessionID   string           `json:"session_id"`
	Order       string           `json:"order"` // "agentctl_rank", "stable", "off"
	MaxItems    int              `json:"max_items"`
	Config      ProjectionConfig `json:"config"`
	DryRun      bool             `json:"dry_run"`
}

// OutboundSyncResult contains the outcome of outbound sync
type OutboundSyncResult struct {
	Written   int          `json:"written"`
	Updated   int          `json:"updated"`
	Unchanged int          `json:"unchanged"`
	FilePath  string       `json:"file_path"`
	Todos     []ClaudeTodo `json:"todos,omitempty"` // Projected todos (dry run shows what would be written)
	Warnings  []string     `json:"warnings,omitempty"`
}

// Service provides bidirectional todo synchronization
type Service struct {
	taskStore tasks.Store
}

// NewService creates a new sync service
func NewService(taskStore tasks.Store) *Service {
	return &Service{taskStore: taskStore}
}

// SyncFromProvider imports todos from Claude Code into agentctl task store.
// This is the inbound sync direction (Claude → agentctl).
func (s *Service) SyncFromProvider(ctx context.Context, in InboundSyncInput) (*InboundSyncResult, error) {
	result := &InboundSyncResult{}

	// Get existing tasks for mapping
	existingTasks, err := s.taskStore.ListWithOptions(ctx, in.WorkspaceID, tasks.ListOptions{
		SessionID: in.SessionID,
	})
	if err != nil {
		return nil, err
	}

	// Build maps for existing tasks
	taskByID := make(map[string]tasks.Task)
	taskByTitle := make(map[string]tasks.Task)
	for _, t := range existingTasks {
		taskByID[t.ID] = t
		taskByTitle[normalizeTitle(t.Title)] = t
	}

	// Track created tasks for dependency resolution
	createdTasks := make(map[string]tasks.Task)

	// Process each todo
	for i, todo := range in.Todos {
		taskID := ParseTaskID(todo.Content)
		title := StripTaskID(todo.Content)
		title = ParseProjectedContent(title) // Remove glyphs too
		agentctlStatus := MapClaudeStatus(todo.Status)

		var task tasks.Task
		var isNew bool

		if taskID != "" {
			// Has tag - use ID as primary key
			if existing, ok := taskByID[taskID]; ok {
				task = existing
				result.Mapped++
			} else {
				// Tag references non-existent task - create with that ID
				isNew = true
				result.Warnings = append(result.Warnings, "Tag references unknown task ID: "+taskID)
			}
		} else {
			// No tag - try to match by title
			if existing, ok := taskByTitle[normalizeTitle(title)]; ok {
				task = existing
				result.Mapped++
			} else {
				// No match - create new
				isNew = true
				result.Unmapped++
			}
		}

		if isNew {
			// Infer dependencies from earlier pending todos in the list
			var dependsOn []string
			for j := 0; j < i; j++ {
				prevID := ParseTaskID(in.Todos[j].Content)
				if prevID == "" {
					// Check if we just created it
					prevTitle := ParseProjectedContent(StripTaskID(in.Todos[j].Content))
					if created, ok := createdTasks[normalizeTitle(prevTitle)]; ok {
						prevID = created.ID
					}
				}
				if prevID != "" && in.Todos[j].Status != "completed" {
					dependsOn = append(dependsOn, prevID)
					result.DepsAdded++
				}
			}

			task = tasks.Task{
				WorkspaceID: in.WorkspaceID,
				Title:       title,
				Description: "Synced from Claude Code TodoWrite",
				Status:      agentctlStatus,
				DependsOn:   dependsOn,
				SessionID:   in.SessionID,
				CreatedAt:   time.Now(),
			}

			if !in.DryRun {
				created, err := s.taskStore.Add(ctx, task)
				if err != nil {
					result.Warnings = append(result.Warnings, "Failed to create task: "+err.Error())
					continue
				}
				task = created
				taskByID[task.ID] = task
				taskByTitle[normalizeTitle(task.Title)] = task
			}
			createdTasks[normalizeTitle(title)] = task
			result.Created++
		} else {
			// Update existing task status if changed
			if task.Status != agentctlStatus {
				if agentctlStatus == StatusCompleted {
					if !in.DryRun {
						now := time.Now()
						task.CompletedAt = &now
						task.Status = agentctlStatus
						_, err := s.taskStore.Update(ctx, task)
						if err != nil {
							result.Warnings = append(result.Warnings, "Failed to complete task: "+err.Error())
							continue
						}
					}
					result.Completed++
				} else if agentctlStatus == StatusInProgress {
					if !in.DryRun {
						_, err := s.taskStore.SetActive(ctx, in.WorkspaceID, task.ID)
						if err != nil {
							result.Warnings = append(result.Warnings, "Failed to set active: "+err.Error())
							continue
						}
					}
					result.Updated++
				}
			}
		}

		result.Tasks = append(result.Tasks, task)
	}

	return result, nil
}

// SyncToProvider exports agentctl tasks to Claude Code todo file.
// This is the outbound sync direction (agentctl → Claude).
func (s *Service) SyncToProvider(ctx context.Context, in OutboundSyncInput) (*OutboundSyncResult, error) {
	result := &OutboundSyncResult{}

	// Get tasks to project
	opts := tasks.ListOptions{
		SessionID: in.SessionID,
	}
	if in.MaxItems > 0 {
		opts.Limit = in.MaxItems
	}

	taskList, err := s.taskStore.ListWithOptions(ctx, in.WorkspaceID, opts)
	if err != nil {
		return nil, err
	}

	// Build dependency map for dep counting
	depMap := make(map[string]int) // taskID -> unresolved dep count
	taskStatusMap := make(map[string]string)
	for _, t := range taskList {
		taskStatusMap[t.ID] = t.Status
	}
	for _, t := range taskList {
		unresolvedCount := 0
		for _, depID := range t.DependsOn {
			if status, ok := taskStatusMap[depID]; ok && status != StatusCompleted {
				unresolvedCount++
			}
		}
		depMap[t.ID] = unresolvedCount
	}

	// Sort tasks for projection
	sortedTasks := sortForProjection(taskList, in.Order)

	// Generate projection config
	cfg := in.Config
	if cfg.MaxContentLength == 0 {
		cfg = DefaultProjectionConfig()
	}

	// Build projected todos
	for _, task := range sortedTasks {
		content := FormatContent(task.Title, task.Status, task.ID, depMap[task.ID], cfg)
		claudeStatus := MapAgentctlStatus(task.Status)
		activeForm := GenerateActiveForm(task.Title)

		todo := ClaudeTodo{
			Content:    content,
			Status:     claudeStatus,
			ActiveForm: activeForm,
		}
		result.Todos = append(result.Todos, todo)
	}

	result.Written = len(result.Todos)
	return result, nil
}

// normalizeTitle normalizes a title for comparison (lowercase, trimmed)
func normalizeTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(title))
}

// sortForProjection sorts tasks according to the specified order
func sortForProjection(taskList []tasks.Task, order string) []tasks.Task {
	// Make a copy to avoid mutating the original
	sorted := make([]tasks.Task, len(taskList))
	copy(sorted, taskList)

	switch order {
	case "stable", "off":
		// Keep original order
		return sorted
	case "agentctl_rank":
		// Sort by: in_progress first, then pending (ready before blocked), then completed
		// Within groups, maintain original order (could add PageRank later)
		var inProgress, pending, blocked, completed []tasks.Task
		for _, t := range sorted {
			switch t.Status {
			case StatusInProgress:
				inProgress = append(inProgress, t)
			case StatusBlocked:
				blocked = append(blocked, t)
			case StatusCompleted:
				completed = append(completed, t)
			default:
				pending = append(pending, t)
			}
		}
		result := make([]tasks.Task, 0, len(sorted))
		result = append(result, inProgress...)
		result = append(result, pending...)
		result = append(result, blocked...)
		result = append(result, completed...)
		return result
	default:
		return sorted
	}
}
