package memoryflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/hooks/lifecycle"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

func TestDetectPromptRecall(t *testing.T) {
	resp := DetectPrompt(DetectorRequest{Prompt: "How did we solve the auth callback bug?"})
	if resp.Decision != "approve" {
		t.Fatalf("decision = %q", resp.Decision)
	}
	if !strings.Contains(resp.Context, "Recall hint") {
		t.Fatalf("context = %q", resp.Context)
	}
}

func TestDetectPromptTodo(t *testing.T) {
	resp := DetectPrompt(DetectorRequest{Prompt: "TODO: make sure we persist the handoff"})
	if !strings.Contains(resp.Context, "Todo hint") {
		t.Fatalf("context = %q", resp.Context)
	}
}

func TestDetectPromptMemory(t *testing.T) {
	resp := DetectPrompt(DetectorRequest{Prompt: "Gotcha: the old installer prints file not found with exit 0"})
	if !strings.Contains(resp.Context, "Memory hint") {
		t.Fatalf("context = %q", resp.Context)
	}
}

func TestRecallFile(t *testing.T) {
	storageRoot := t.TempDir()
	casRoot := filepath.Join(storageRoot, "cas")
	cfg := config.Config{}
	cfg.Storage.Root = storageRoot
	cfg.Paths.CAS = casRoot
	store, err := memory.Open(context.Background(), storageRoot, casRoot)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()
	_, err = store.Save(context.Background(), storage.NamedEntry{
		Name:      "edit:auth",
		Type:      "gotcha",
		Workspace: "/tmp/workspace",
		Summary:   "auth.go: Remember to validate state before token exchange",
		Result:    json.RawMessage(`{"file":"internal/auth/auth.go"}`),
	})
	if err != nil {
		t.Fatalf("save memory: %v", err)
	}

	resp, err := RecallFile(context.Background(), NewDependencies(cfg, lifecycle.Dependencies{}), RecallRequest{
		Workspace: "/tmp/workspace",
		Payload: RecallPayload{
			ToolInput: struct {
				FilePath string `json:"file_path,omitempty"`
			}{
				FilePath: "internal/auth/auth.go",
			},
		},
	})
	if err != nil {
		t.Fatalf("RecallFile: %v", err)
	}
	if !strings.Contains(resp.Context, "`auth.go`") {
		t.Fatalf("context = %q", resp.Context)
	}
	if !strings.Contains(resp.Context, "validate state") {
		t.Fatalf("context = %q", resp.Context)
	}
}

func TestHandleLifecycleTodoWritePrompt(t *testing.T) {
	resp, err := HandleLifecycle(context.Background(), Dependencies{}, LifecycleRequest{
		Workspace: t.TempDir(),
		Payload: LifecyclePayload{
			ToolName: "TodoWrite",
			ToolInput: struct {
				FilePath  string       `json:"file_path,omitempty"`
				Path      string       `json:"path,omitempty"`
				OldString string       `json:"old_string,omitempty"`
				NewString string       `json:"new_string,omitempty"`
				Content   string       `json:"content,omitempty"`
				Operation string       `json:"operation,omitempty"`
				Name      string       `json:"name,omitempty"`
				Todos     []ClaudeTodo `json:"todos,omitempty"`
			}{
				Todos: []ClaudeTodo{{Content: "Ship hook cleanup", Status: "completed"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleLifecycle: %v", err)
	}
	if !strings.Contains(resp.Context, "Memory prompt") {
		t.Fatalf("context = %q", resp.Context)
	}
}

func TestHandleLifecycleEditCapture(t *testing.T) {
	storageRoot := t.TempDir()
	casRoot := filepath.Join(storageRoot, "cas")
	cfg := config.Config{}
	cfg.Storage.Root = storageRoot
	cfg.Paths.CAS = casRoot
	workspaceRoot := t.TempDir()
	resp, err := HandleLifecycle(context.Background(), Dependencies{
		Config: cfg,
	}, LifecycleRequest{
		Workspace: workspaceRoot,
		Payload: LifecyclePayload{
			ToolName: "Edit",
			ToolInput: struct {
				FilePath  string       `json:"file_path,omitempty"`
				Path      string       `json:"path,omitempty"`
				OldString string       `json:"old_string,omitempty"`
				NewString string       `json:"new_string,omitempty"`
				Content   string       `json:"content,omitempty"`
				Operation string       `json:"operation,omitempty"`
				Name      string       `json:"name,omitempty"`
				Todos     []ClaudeTodo `json:"todos,omitempty"`
			}{
				FilePath:  filepath.Join(workspaceRoot, "internal", "auth.go"),
				OldString: "old state logic",
				NewString: "new state logic",
			},
		},
	})
	if err != nil {
		t.Fatalf("HandleLifecycle: %v", err)
	}
	if resp.Decision != "approve" {
		t.Fatalf("decision = %q", resp.Decision)
	}
	store, err := memory.Open(context.Background(), storageRoot, casRoot)
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer store.Close()
	entry, err := store.Get(context.Background(), "edit:internal/auth.go", workspaceRoot)
	if err != nil {
		t.Fatalf("get memory entry: %v", err)
	}
	if !strings.Contains(entry.Summary, "internal/auth.go") {
		t.Fatalf("summary = %q", entry.Summary)
	}
}
