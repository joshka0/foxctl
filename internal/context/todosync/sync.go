package todosync

import (
	"context"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/tasks"
)

// SyncResult contains the outcome of a sync operation
type SyncResult struct {
	Created   int      `json:"created"`
	Updated   int      `json:"updated"`
	Completed int      `json:"completed"`
	Removed   int      `json:"removed"` // Tasks canceled because they were removed from provider
	Mapped    int      `json:"mapped"`
	Unmapped  int      `json:"unmapped"`
	DepsAdded int      `json:"deps_added"`
	Warnings  []string `json:"warnings,omitempty"`
}

// InboundSyncInput defines parameters for syncing from provider to foxctl
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

// OutboundSyncInput defines parameters for syncing from foxctl to provider
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

type inboundSyncState struct {
	existingTasks []tasks.Task
	taskByID      map[string]tasks.Task
	taskByTitle   map[string]tasks.Task
	createdTasks  map[string]tasks.Task
}

// NewService creates a new sync service
func NewService(taskStore tasks.Store) *Service {
	return &Service{taskStore: taskStore}
}

// SyncFromProvider imports todos from Claude Code into foxctl task store.
// This is the inbound sync direction (Claude → foxctl).
func (s *Service) SyncFromProvider(ctx context.Context, in InboundSyncInput) (*InboundSyncResult, error) {
	result := &InboundSyncResult{}

	// Get existing tasks for mapping
	state, err := s.newInboundSyncState(ctx, in)
	if err != nil {
		return nil, err
	}

	// Process each todo
	for i, todo := range in.Todos {
		task, ok := s.syncInboundTodo(ctx, in, state, result, todo, i)
		if !ok {
			continue
		}
		result.Tasks = append(result.Tasks, task)
	}

	// Removal detection: find tasks that were in foxctl but not in incoming list
	// Skip if incoming list is empty (conservative approach - empty doesn't mean "clear all")
	s.cancelRemovedInboundTasks(ctx, in, state.existingTasks, result)

	return result, nil
}

func (s *Service) newInboundSyncState(ctx context.Context, in InboundSyncInput) (*inboundSyncState, error) {
	existingTasks, err := s.taskStore.ListWithOptions(ctx, in.WorkspaceID, tasks.ListOptions{
		SessionID: in.SessionID,
	})
	if err != nil {
		return nil, err
	}

	taskByID := make(map[string]tasks.Task, len(existingTasks))
	taskByTitle := make(map[string]tasks.Task, len(existingTasks))
	for _, t := range existingTasks {
		taskByID[t.ID] = t
		taskByTitle[normalizeTitle(t.Title)] = t
	}

	return &inboundSyncState{
		existingTasks: existingTasks,
		taskByID:      taskByID,
		taskByTitle:   taskByTitle,
		createdTasks:  make(map[string]tasks.Task),
	}, nil
}

func (s *Service) syncInboundTodo(
	ctx context.Context,
	in InboundSyncInput,
	state *inboundSyncState,
	result *InboundSyncResult,
	todo ClaudeTodo,
	index int,
) (tasks.Task, bool) {
	taskID, title, agentctlStatus := parseInboundTodo(todo)
	task, isNew := resolveInboundTask(taskID, title, state, result)
	if isNew {
		dependsOn, depsAdded := inferInboundDependencies(in.Todos, index, state.createdTasks)
		result.DepsAdded += depsAdded

		task = buildInboundTask(in, title, agentctlStatus, dependsOn)
		if !in.DryRun {
			created, err := s.taskStore.Add(ctx, task)
			if err != nil {
				result.Warnings = append(result.Warnings, "Failed to create task: "+err.Error())
				return tasks.Task{}, false
			}
			task = created
			state.taskByID[task.ID] = task
			state.taskByTitle[normalizeTitle(task.Title)] = task
		}
		state.createdTasks[normalizeTitle(title)] = task
		result.Created++
		return task, true
	}

	if task.Status != agentctlStatus {
		if !s.applyInboundStatusUpdate(ctx, in, result, &task, agentctlStatus) {
			return tasks.Task{}, false
		}
	}

	return task, true
}

func parseInboundTodo(todo ClaudeTodo) (string, string, string) {
	taskID := ParseTaskID(todo.Content)
	title := StripTaskID(todo.Content)
	title = ParseProjectedContent(title) // Remove glyphs too
	agentctlStatus := MapClaudeStatus(todo.Status)
	return taskID, title, agentctlStatus
}

func resolveInboundTask(
	taskID string,
	title string,
	state *inboundSyncState,
	result *InboundSyncResult,
) (tasks.Task, bool) {
	if taskID != "" {
		// Has tag - use ID as primary key
		if existing, ok := state.taskByID[taskID]; ok {
			result.Mapped++
			return existing, false
		}
		// Tag references non-existent task - create with that ID
		result.Warnings = append(result.Warnings, "Tag references unknown task ID: "+taskID)
		return tasks.Task{}, true
	}

	// No tag - try to match by title
	if existing, ok := state.taskByTitle[normalizeTitle(title)]; ok {
		result.Mapped++
		return existing, false
	}

	// No match - create new
	result.Unmapped++
	return tasks.Task{}, true
}

func inferInboundDependencies(
	todos []ClaudeTodo,
	index int,
	createdTasks map[string]tasks.Task,
) ([]string, int) {
	var dependsOn []string
	depsAdded := 0
	for j := 0; j < index; j++ {
		prevID := ParseTaskID(todos[j].Content)
		if prevID == "" {
			// Check if we just created it
			prevTitle := ParseProjectedContent(StripTaskID(todos[j].Content))
			if created, ok := createdTasks[normalizeTitle(prevTitle)]; ok {
				prevID = created.ID
			}
		}
		if prevID != "" && todos[j].Status != "completed" {
			dependsOn = append(dependsOn, prevID)
			depsAdded++
		}
	}
	return dependsOn, depsAdded
}

func buildInboundTask(
	in InboundSyncInput,
	title string,
	status string,
	dependsOn []string,
) tasks.Task {
	return tasks.Task{
		WorkspaceID: in.WorkspaceID,
		Title:       title,
		Description: "Synced from Claude Code TodoWrite",
		Status:      status,
		DependsOn:   dependsOn,
		SessionID:   in.SessionID,
		CreatedAt:   time.Now(),
	}
}

func (s *Service) applyInboundStatusUpdate(
	ctx context.Context,
	in InboundSyncInput,
	result *InboundSyncResult,
	task *tasks.Task,
	agentctlStatus string,
) bool {
	switch agentctlStatus {
	case StatusCompleted:
		if !in.DryRun {
			now := time.Now()
			task.CompletedAt = &now
			task.Status = agentctlStatus
			_, err := s.taskStore.Update(ctx, *task)
			if err != nil {
				result.Warnings = append(result.Warnings, "Failed to complete task: "+err.Error())
				return false
			}
		}
		result.Completed++
	case StatusInProgress:
		if !in.DryRun {
			_, err := s.taskStore.SetActive(ctx, in.WorkspaceID, task.ID)
			if err != nil {
				result.Warnings = append(result.Warnings, "Failed to set active: "+err.Error())
				return false
			}
		}
		result.Updated++
	}
	return true
}

func (s *Service) cancelRemovedInboundTasks(
	ctx context.Context,
	in InboundSyncInput,
	existingTasks []tasks.Task,
	result *InboundSyncResult,
) {
	if len(in.Todos) == 0 {
		return
	}

	seenTaskIDs := collectSeenTaskIDs(result.Tasks)
	for _, existingTask := range existingTasks {
		if seenTaskIDs[existingTask.ID] {
			continue // Still in list
		}
		// Skip already completed/canceled tasks
		if existingTask.Status == StatusCompleted || existingTask.Status == StatusCanceled {
			continue
		}
		// Task was removed from Claude's list - cancel it
		if !in.DryRun {
			existingTask.Status = StatusCanceled
			note := "Removed from Claude TodoWrite list"
			if existingTask.Notes == "" {
				existingTask.Notes = note
			} else {
				existingTask.Notes = existingTask.Notes + "\n" + note
			}
			_, err := s.taskStore.Update(ctx, existingTask)
			if err != nil {
				result.Warnings = append(result.Warnings, "Failed to cancel removed task: "+err.Error())
				continue
			}
		}
		result.Removed++
	}
}

func collectSeenTaskIDs(tasks []tasks.Task) map[string]bool {
	seenTaskIDs := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		if task.ID != "" {
			seenTaskIDs[task.ID] = true
		}
	}
	return seenTaskIDs
}

// SyncToProvider exports foxctl tasks to Claude Code todo file.
// This is the outbound sync direction (foxctl → Claude).
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
