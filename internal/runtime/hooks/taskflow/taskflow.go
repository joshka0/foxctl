package taskflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/todosync"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
	"github.com/joshka0/foxctl/internal/runtime/hooks/sessionmode"
	"github.com/joshka0/foxctl/internal/storage/graph"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

type Dependencies struct {
	StorageRoot    string
	RunSkill       lifecycle.SkillRunner
	DetectIdentity lifecycle.IdentityDetector
}

type ClaudeTodo = todosync.ClaudeTodo

func NewDependencies(cfg config.Config) Dependencies {
	life := lifecycle.NewDependencies(cfg)
	return Dependencies{
		StorageRoot:    cfg.Storage.Root,
		RunSkill:       life.RunSkill,
		DetectIdentity: life.DetectIdentity,
	}
}

type TodoSyncPayload struct {
	SessionID    string `json:"sessionID,omitempty"`
	AltSessionID string `json:"session_id,omitempty"`
	ToolInput    struct {
		Todos []todosync.ClaudeTodo `json:"todos"`
	} `json:"tool_input"`
}

type TodoSyncRequest struct {
	Workspace string
	Payload   TodoSyncPayload
}

type TodoSyncResponse struct {
	Workspace string   `json:"workspace"`
	SessionID string   `json:"session_id,omitempty"`
	Context   string   `json:"context,omitempty"`
	Created   int      `json:"created,omitempty"`
	Updated   int      `json:"updated,omitempty"`
	Completed int      `json:"completed,omitempty"`
	Removed   int      `json:"removed,omitempty"`
	Mapped    int      `json:"mapped,omitempty"`
	DepsAdded int      `json:"deps_added,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

type TodoContinuationPayload struct {
	SessionID    string `json:"sessionID,omitempty"`
	AltSessionID string `json:"session_id,omitempty"`
	Cwd          string `json:"cwd,omitempty"`
}

type TodoContinuationRequest struct {
	Workspace string
	Payload   TodoContinuationPayload
}

type TodoContinuationResponse struct {
	Decision     string `json:"decision"`
	Reason       string `json:"reason,omitempty"`
	InjectPrompt string `json:"inject_prompt,omitempty"`
	Warning      string `json:"warning,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
}

type TaskFileLinkPayload struct {
	ToolInput struct {
		FilePath string `json:"file_path,omitempty"`
	} `json:"tool_input"`
}

type TaskFileLinkRequest struct {
	Workspace string
	Payload   TaskFileLinkPayload
}

type TaskFileLinkResponse struct {
	Decision string `json:"decision"`
	Context  string `json:"context,omitempty"`
	TaskID   string `json:"task_id,omitempty"`
	FilePath string `json:"file_path,omitempty"`
}

type todoSyncFromProviderEnvelope struct {
	Data struct {
		Created   int      `json:"created"`
		Updated   int      `json:"updated"`
		Completed int      `json:"completed"`
		Removed   int      `json:"removed"`
		Mapped    int      `json:"mapped"`
		DepsAdded int      `json:"deps_added"`
		Warnings  []string `json:"warnings"`
	} `json:"data"`
}

type embeddingTasksEnvelope struct {
	Data struct {
		Embedded int `json:"embedded"`
	} `json:"data"`
}

type todoSyncToProviderEnvelope struct {
	Data struct {
		Written  int      `json:"written"`
		Warnings []string `json:"warnings"`
	} `json:"data"`
}

type sessionAnchorEnvelope struct {
	Data struct {
		Found  bool `json:"found"`
		Anchor struct {
			MainPrompt      string `json:"main_prompt"`
			PendingQuestion string `json:"pending_question"`
		} `json:"anchor"`
	} `json:"data"`
}

type todoContinuationEnvelope struct {
	Data struct {
		ShouldContinue  bool   `json:"should_continue"`
		Prompt          string `json:"prompt"`
		IncompleteCount int    `json:"incomplete_count"`
	} `json:"data"`
}

func SyncTodoWrite(ctx context.Context, deps Dependencies, req TodoSyncRequest) (TodoSyncResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return TodoSyncResponse{}, fmt.Errorf("detect workspace")
	}
	response := TodoSyncResponse{Workspace: target}
	todos := req.Payload.ToolInput.Todos
	if len(todos) == 0 {
		return response, nil
	}
	sessionID := firstNonEmpty(strings.TrimSpace(req.Payload.SessionID), strings.TrimSpace(req.Payload.AltSessionID))
	if sessionID == "" && deps.DetectIdentity != nil {
		sessionID, _, _ = deps.DetectIdentity(target)
	}
	response.SessionID = sessionID
	if deps.RunSkill == nil {
		return response, nil
	}

	var syncEnv todoSyncFromProviderEnvelope
	if err := deps.RunSkill(ctx, "todo/sync_from_provider", map[string]any{
		"provider":     "claude",
		"workspace_id": target,
		"session_id":   nullableString(sessionID),
		"todos":        todos,
	}, target, &syncEnv); err != nil {
		return response, err
	}

	response.Created = syncEnv.Data.Created
	response.Updated = syncEnv.Data.Updated
	response.Completed = syncEnv.Data.Completed
	response.Removed = syncEnv.Data.Removed
	response.Mapped = syncEnv.Data.Mapped
	response.DepsAdded = syncEnv.Data.DepsAdded
	response.Warnings = append([]string{}, syncEnv.Data.Warnings...)

	if response.Created > 0 || response.Updated > 0 {
		var embed embeddingTasksEnvelope
		_ = deps.RunSkill(ctx, "embedding/tasks", map[string]any{
			"scope":        "workspace",
			"workspace_id": target,
		}, target, &embed)
	}

	if response.Created > 0 && sessionID != "" {
		_ = linkRecentTasksToActiveEpic(ctx, deps.StorageRoot, target, sessionID, response.Created)
	}

	contextParts := []string{}
	total := response.Created + response.Updated + response.Completed + response.Removed
	if total > 0 || response.Mapped > 0 {
		summary := fmt.Sprintf("**Todo Sync:** Synced %d todos", response.Mapped+response.Created)
		if response.Created > 0 {
			summary += fmt.Sprintf(", created %d", response.Created)
		}
		if response.Updated > 0 {
			summary += fmt.Sprintf(", updated %d", response.Updated)
		}
		if response.Completed > 0 {
			summary += fmt.Sprintf(", completed %d", response.Completed)
		}
		if response.Removed > 0 {
			summary += fmt.Sprintf(", removed %d", response.Removed)
		}
		if response.DepsAdded > 0 {
			summary += fmt.Sprintf(", %d deps inferred", response.DepsAdded)
		}
		contextParts = append(contextParts, summary)
	}
	if len(response.Warnings) > 0 {
		contextParts = append(contextParts, "**Warnings:** "+strings.Join(response.Warnings, "; "))
	}

	if os.Getenv("FOXCTL_TODO_BIDIRECTIONAL") == "1" && sessionID != "" {
		var outbound todoSyncToProviderEnvelope
		if err := deps.RunSkill(ctx, "todo/sync_to_provider", map[string]any{
			"provider":             "claude",
			"workspace_id":         target,
			"session_id":           sessionID,
			"allow_provider_state": true,
		}, target, &outbound); err == nil && outbound.Data.Written > 0 {
			contextParts = append(contextParts, fmt.Sprintf("**Outbound:** Updated Claude todo file with %d tasks", outbound.Data.Written))
			if len(outbound.Data.Warnings) > 0 {
				response.Warnings = append(response.Warnings, outbound.Data.Warnings...)
			}
		}
	}

	response.Context = strings.Join(contextParts, " ")
	return response, nil
}

func ContinueTodoSession(ctx context.Context, deps Dependencies, req TodoContinuationRequest) (TodoContinuationResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return TodoContinuationResponse{}, fmt.Errorf("detect workspace")
	}
	if envEnabled("FOXCTL_TODO_CONTINUATION_DISABLED") {
		return TodoContinuationResponse{Decision: "approve"}, nil
	}
	sessionID := firstNonEmpty(strings.TrimSpace(req.Payload.SessionID), strings.TrimSpace(req.Payload.AltSessionID))
	if sessionID == "" && deps.DetectIdentity != nil {
		sessionID, _, _ = deps.DetectIdentity(target)
	}
	if sessionID == "" {
		return TodoContinuationResponse{
			Decision:  "approve",
			Warning:   "todo continuation: no session_id detected; allowing stop",
			SessionID: sessionID,
		}, nil
	}

	flags, _ := sessionmode.Read(sessionID, time.Now())
	todoMode, anchorGoal := flags.Todo, flags.AnchorGoal
	if !todoMode && strings.TrimSpace(anchorGoal) == "" {
		return TodoContinuationResponse{Decision: "approve", SessionID: sessionID}, nil
	}

	if deps.RunSkill != nil && strings.TrimSpace(anchorGoal) != "" {
		var anchor sessionAnchorEnvelope
		if err := deps.RunSkill(ctx, "session/anchor", map[string]any{
			"operation":  "get",
			"workspace":  target,
			"session_id": sessionID,
		}, target, &anchor); err == nil && anchor.Data.Found && strings.TrimSpace(anchor.Data.Anchor.MainPrompt) != "" {
			anchorGoal = strings.TrimSpace(anchor.Data.Anchor.MainPrompt)
		}
	}

	topN := envInt("FOXCTL_TODO_CONTINUATION_TOP_N", 5)
	minPending := envInt("FOXCTL_TODO_CONTINUATION_MIN_PENDING", 1)
	var continuation todoContinuationEnvelope
	if deps.RunSkill == nil {
		return TodoContinuationResponse{Decision: "approve", SessionID: sessionID}, nil
	}
	if err := deps.RunSkill(ctx, "todo/continuation", map[string]any{
		"workspace_id": target,
		"session_id":   sessionID,
		"top_n":        topN,
		"min_pending":  minPending,
		"anchor_goal":  anchorGoal,
	}, target, &continuation); err != nil {
		return TodoContinuationResponse{Decision: "approve", SessionID: sessionID}, nil
	}
	if !continuation.Data.ShouldContinue {
		return TodoContinuationResponse{Decision: "approve", SessionID: sessionID}, nil
	}
	reason := fmt.Sprintf("Incomplete tasks remain (%d incomplete)", continuation.Data.IncompleteCount)
	return TodoContinuationResponse{
		Decision:     "block",
		Reason:       reason,
		InjectPrompt: continuation.Data.Prompt,
		SessionID:    sessionID,
	}, nil
}

func LinkTaskFile(ctx context.Context, deps Dependencies, req TaskFileLinkRequest) (TaskFileLinkResponse, error) {
	target := workspace.Normalize(workspace.Detect(strings.TrimSpace(req.Workspace)))
	if target == "" {
		return TaskFileLinkResponse{}, fmt.Errorf("detect workspace")
	}
	if envEnabled("FOXCTL_TASK_FILE_LINK_DISABLED") {
		return TaskFileLinkResponse{Decision: "approve"}, nil
	}
	filePath := strings.TrimSpace(req.Payload.ToolInput.FilePath)
	if filePath == "" {
		return TaskFileLinkResponse{Decision: "approve"}, nil
	}
	if !filepath.IsAbs(filePath) {
		filePath = filepath.Join(target, filePath)
	}
	filePath = filepath.Clean(filePath)
	relPath := filepath.ToSlash(strings.TrimPrefix(filePath, target+string(filepath.Separator)))
	if relPath == "" || relPath == filePath {
		relPath = filepath.ToSlash(filepath.Base(filePath))
	}

	taskStore, err := tasks.Open(ctx, deps.StorageRoot)
	if err != nil {
		return TaskFileLinkResponse{}, err
	}
	defer func() { _ = taskStore.Close() }()
	task, found, err := taskStore.GetActive(ctx, target)
	if err != nil || !found {
		return TaskFileLinkResponse{Decision: "approve"}, err
	}

	graphStore, err := graph.Open(ctx, deps.StorageRoot)
	if err != nil {
		return TaskFileLinkResponse{}, err
	}
	defer func() { _ = graphStore.Close() }()

	now := time.Now().UTC()
	fileNodeID := "file:" + relPath
	taskNodeID := "task:" + task.ID
	_ = graphStore.UpsertNode(ctx, graph.Node{
		Workspace:   target,
		NodeID:      fileNodeID,
		NodeType:    graph.NodeTypeFile,
		Title:       relPath,
		CurrentPath: relPath,
		LastSeen:    now,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	_ = graphStore.UpsertNode(ctx, graph.Node{
		Workspace: target,
		NodeID:    taskNodeID,
		NodeType:  graph.NodeTypeTask,
		Title:     task.Title,
		LastSeen:  now,
		CreatedAt: now,
		UpdatedAt: now,
	})
	ttlDays := 90
	_ = graphStore.UpsertEdge(ctx, graph.Edge{
		Workspace: target,
		FromID:    taskNodeID,
		FromType:  graph.NodeTypeTask,
		ToID:      fileNodeID,
		ToType:    graph.NodeTypeFile,
		EdgeType:  graph.EdgeTypeModified,
		Weight:    1.0,
		TTLDays:   &ttlDays,
		CreatedAt: now,
	})

	if envEnabled("FOXCTL_TASK_FILE_LINK_SYNC") {
		return TaskFileLinkResponse{
			Decision: "approve",
			Context:  fmt.Sprintf("**Graph:** Linked `%s` → task \"%s\"", relPath, task.Title),
			TaskID:   task.ID,
			FilePath: relPath,
		}, nil
	}
	return TaskFileLinkResponse{
		Decision: "approve",
		TaskID:   task.ID,
		FilePath: relPath,
	}, nil
}

func linkRecentTasksToActiveEpic(ctx context.Context, storageRoot, workspacePath, sessionID string, createdCount int) error {
	if createdCount <= 0 {
		return nil
	}
	store, err := tasks.Open(ctx, storageRoot)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	epic, found, err := store.GetActiveEpic(ctx, workspacePath, sessionID)
	if err != nil || !found {
		return err
	}
	taskList, err := store.ListByWorkspace(ctx, workspacePath)
	if err != nil {
		return err
	}
	sort.SliceStable(taskList, func(i, j int) bool {
		return taskList[i].CreatedAt.After(taskList[j].CreatedAt)
	})
	linked := 0
	for _, task := range taskList {
		if task.SessionID != sessionID || strings.TrimSpace(task.EpicID) != "" {
			continue
		}
		if err := store.LinkTaskToEpic(ctx, task.ID, epic.ID); err == nil {
			linked++
		}
		if linked >= createdCount {
			break
		}
	}
	return nil
}

func envEnabled(name string) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
