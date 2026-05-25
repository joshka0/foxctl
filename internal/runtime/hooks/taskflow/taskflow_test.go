package taskflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/hooks/sessionmode"
	"github.com/joshka0/foxctl/internal/storage/graph"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

func TestSyncTodoWriteBuildsContext(t *testing.T) {
	deps := Dependencies{
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			switch skill {
			case "todo/sync_from_provider":
				target := out.(*todoSyncFromProviderEnvelope)
				target.Data.Created = 2
				target.Data.Updated = 1
				target.Data.Mapped = 3
				target.Data.DepsAdded = 1
				target.Data.Warnings = []string{"one warning"}
			case "embedding/tasks":
				target := out.(*embeddingTasksEnvelope)
				target.Data.Embedded = 3
			case "todo/sync_to_provider":
				target := out.(*todoSyncToProviderEnvelope)
				target.Data.Written = 2
			default:
				t.Fatalf("unexpected skill %s", skill)
			}
			return nil
		},
		DetectIdentity: func(workspace string) (string, string, string) {
			return "sid-123", "claude", "claude"
		},
	}
	t.Setenv("FOXCTL_TODO_BIDIRECTIONAL", "1")

	response, err := SyncTodoWrite(context.Background(), deps, TodoSyncRequest{
		Workspace: t.TempDir(),
		Payload: TodoSyncPayload{
			ToolInput: struct {
				Todos []ClaudeTodo `json:"todos"`
			}{
				Todos: []ClaudeTodo{{Content: "Do thing", Status: "pending"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("SyncTodoWrite: %v", err)
	}
	if response.Created != 2 || response.Updated != 1 {
		t.Fatalf("unexpected counts: %#v", response)
	}
	if !strings.Contains(response.Context, "Todo Sync") {
		t.Fatalf("expected sync context, got %q", response.Context)
	}
	if !strings.Contains(response.Context, "Outbound") {
		t.Fatalf("expected outbound note, got %q", response.Context)
	}
}

func TestContinueTodoSessionApprovesWithoutSession(t *testing.T) {
	response, err := ContinueTodoSession(context.Background(), Dependencies{}, TodoContinuationRequest{
		Workspace: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("ContinueTodoSession: %v", err)
	}
	if response.Decision != "approve" {
		t.Fatalf("decision = %q", response.Decision)
	}
	if response.Warning == "" {
		t.Fatalf("expected warning when session id is missing")
	}
}

func TestContinueTodoSessionBlocksWhenSkillRequiresContinuation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	sessionID := "sid-123"
	modeDir := filepath.Join(home, ".foxctl", "cache", "session-modes")
	if err := os.MkdirAll(modeDir, 0o755); err != nil {
		t.Fatalf("mkdir mode dir: %v", err)
	}
	if err := sessionmode.EnableTodo(sessionID, time.Now()); err != nil {
		t.Fatalf("write mode flag: %v", err)
	}

	deps := Dependencies{
		DetectIdentity: func(workspace string) (string, string, string) {
			return sessionID, "claude", "claude"
		},
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			switch skill {
			case "todo/continuation":
				target := out.(*todoContinuationEnvelope)
				target.Data.ShouldContinue = true
				target.Data.Prompt = "Keep working on the task list"
				target.Data.IncompleteCount = 3
			default:
				t.Fatalf("unexpected skill %s", skill)
			}
			return nil
		},
	}

	response, err := ContinueTodoSession(context.Background(), deps, TodoContinuationRequest{
		Workspace: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("ContinueTodoSession: %v", err)
	}
	if response.Decision != "block" {
		t.Fatalf("decision = %q", response.Decision)
	}
	if !strings.Contains(response.InjectPrompt, "Keep working") {
		t.Fatalf("inject prompt = %q", response.InjectPrompt)
	}
}

func TestLinkTaskFileSyncModeAddsContext(t *testing.T) {
	t.Setenv("FOXCTL_TASK_FILE_LINK_SYNC", "1")
	storageRoot := t.TempDir()
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	store, err := tasks.Open(context.Background(), storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	defer store.Close()
	task, err := store.Add(context.Background(), tasks.Task{
		WorkspaceID: workspaceRoot,
		Title:       "Ship hook cleanup",
		Status:      tasks.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}
	if _, err := store.SetActive(context.Background(), workspaceRoot, task.ID); err != nil {
		t.Fatalf("set active: %v", err)
	}

	response, err := LinkTaskFile(context.Background(), Dependencies{
		StorageRoot: storageRoot,
	}, TaskFileLinkRequest{
		Workspace: workspaceRoot,
		Payload: TaskFileLinkPayload{
			ToolInput: struct {
				FilePath string `json:"file_path,omitempty"`
			}{
				FilePath: "internal/runtime/hooks/lifecycle/lifecycle.go",
			},
		},
	})
	if err != nil {
		t.Fatalf("LinkTaskFile: %v", err)
	}
	if response.Decision != "approve" {
		t.Fatalf("decision = %q", response.Decision)
	}
	if !strings.Contains(response.Context, "Linked") {
		t.Fatalf("context = %q", response.Context)
	}
}

func TestLinkTaskFileRejectsEscapingPath(t *testing.T) {
	storageRoot := t.TempDir()
	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	store, err := tasks.Open(context.Background(), storageRoot)
	if err != nil {
		t.Fatalf("open tasks: %v", err)
	}
	task, err := store.Add(context.Background(), tasks.Task{
		WorkspaceID: workspaceRoot,
		Title:       "Ship hook cleanup",
		Status:      tasks.StatusInProgress,
	})
	if err != nil {
		t.Fatalf("add task: %v", err)
	}
	if _, err := store.SetActive(context.Background(), workspaceRoot, task.ID); err != nil {
		t.Fatalf("set active: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close tasks: %v", err)
	}

	outsidePath := filepath.Join(filepath.Dir(workspaceRoot), "outside.go")
	response, err := LinkTaskFile(context.Background(), Dependencies{
		StorageRoot: storageRoot,
	}, TaskFileLinkRequest{
		Workspace: workspaceRoot,
		Payload: TaskFileLinkPayload{
			ToolInput: struct {
				FilePath string `json:"file_path,omitempty"`
			}{
				FilePath: outsidePath,
			},
		},
	})
	if err != nil {
		t.Fatalf("LinkTaskFile: %v", err)
	}
	if response.Decision != "approve" {
		t.Fatalf("decision = %q", response.Decision)
	}
	if response.TaskID != "" || response.FilePath != "" {
		t.Fatalf("escaping link response = %#v, want no task or file link", response)
	}

	graphStore, err := graph.Open(context.Background(), storageRoot)
	if err != nil {
		t.Fatalf("open graph: %v", err)
	}
	defer graphStore.Close()
	if _, err := graphStore.GetNode(context.Background(), workspaceRoot, "file:outside.go"); err == nil {
		t.Fatalf("escaping path was linked into the task graph")
	}
}

func TestTaskflowRelPathRejectsEscapesAndAllowsDotDotNames(t *testing.T) {
	t.Parallel()

	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if got := taskflowRelPath(workspaceRoot, filepath.Join(workspaceRoot, "internal", "auth.go")); got != "internal/auth.go" {
		t.Fatalf("absolute workspace path = %q, want internal/auth.go", got)
	}
	if got := taskflowRelPath(workspaceRoot, filepath.Join("internal", "auth.go")); got != "internal/auth.go" {
		t.Fatalf("relative workspace path = %q, want internal/auth.go", got)
	}
	if got := taskflowRelPath(workspaceRoot, filepath.Join("..", "outside.go")); got != "" {
		t.Fatalf("parent-relative escape = %q, want empty", got)
	}
	if got := taskflowRelPath(workspaceRoot, filepath.Join(filepath.Dir(workspaceRoot), "outside.go")); got != "" {
		t.Fatalf("absolute sibling escape = %q, want empty", got)
	}
	if got := taskflowRelPath(workspaceRoot, filepath.Join(workspaceRoot, "..cache", "demo.go")); got != "..cache/demo.go" {
		t.Fatalf("dot-dot-prefixed child = %q, want ..cache/demo.go", got)
	}
}

func TestTaskflowRelPathPropertyRejectsGeneratedEscapes(t *testing.T) {
	t.Parallel()

	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	parent := filepath.Dir(workspaceRoot)
	property := func(rawName string) bool {
		name := strings.TrimSpace(rawName)
		name = strings.NewReplacer("/", "_", "\\", "_").Replace(name)
		if name == "" {
			name = "file.go"
		}

		if got := taskflowRelPath(workspaceRoot, filepath.Join("..", name)); got != "" {
			t.Logf("relative escape %q normalized to %q", name, got)
			return false
		}
		if got := taskflowRelPath(workspaceRoot, filepath.Join(parent, name)); got != "" {
			t.Logf("absolute sibling escape %q normalized to %q", name, got)
			return false
		}

		childName := "..cache-" + name
		want := filepath.ToSlash(childName)
		if got := taskflowRelPath(workspaceRoot, filepath.Join(workspaceRoot, childName)); got != want {
			t.Logf("dot-dot-prefixed child %q normalized to %q, want %q", childName, got, want)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatalf("taskflow path property failed: %v", err)
	}
}
