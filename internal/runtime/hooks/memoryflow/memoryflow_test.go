package memoryflow

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
	"unicode/utf8"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/hooks/lifecycle"
	"github.com/joshka0/foxctl/internal/storage"
	ctxengstore "github.com/joshka0/foxctl/internal/storage/contextengine"
	"github.com/joshka0/foxctl/internal/storage/memory"
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
	workspaceRoot := t.TempDir()
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
		Type:      "semantic_fact",
		Workspace: workspaceRoot,
		Summary:   "auth.go: Remember to validate state before token exchange",
		Result:    json.RawMessage(`{"file":"internal/auth/auth.go"}`),
	})
	if err != nil {
		t.Fatalf("save memory: %v", err)
	}

	resp, err := RecallFile(context.Background(), NewDependencies(cfg, lifecycle.Dependencies{}), RecallRequest{
		Workspace: workspaceRoot,
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

func TestHandleLifecycleEditCaptureRejectsEscapingPath(t *testing.T) {
	storageRoot := t.TempDir()
	casRoot := filepath.Join(storageRoot, "cas")
	cfg := config.Config{}
	cfg.Storage.Root = storageRoot
	cfg.Paths.CAS = casRoot
	workspaceRoot := t.TempDir()
	outsidePath := filepath.Join(filepath.Dir(workspaceRoot), "outside.go")
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
				FilePath:  outsidePath,
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
	if _, err := store.Get(context.Background(), "edit:"+filepath.ToSlash(outsidePath), workspaceRoot); err != memory.ErrNotFound {
		t.Fatalf("escaping edit memory lookup error = %v, want %v", err, memory.ErrNotFound)
	}

	eventStore, err := ctxengstore.Open(context.Background(), storageRoot)
	if err != nil {
		t.Fatalf("open contextengine store: %v", err)
	}
	defer eventStore.Close()
	events, err := eventStore.ListEvents(context.Background(), contextengine.EventFilter{
		WorkspaceID: workspace.ID(workspaceRoot),
		Kind:        contextengine.EventKindCodeChangedDirty,
	})
	if err != nil {
		t.Fatalf("list dirty events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("dirty events for escaping edit = %d, want 0", len(events))
	}
}

func TestMemoryflowRelPathRejectsEscapesAndAllowsDotDotNames(t *testing.T) {
	t.Parallel()

	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	if got := memoryflowRelPath(workspaceRoot, filepath.Join(workspaceRoot, "internal", "auth.go")); got != "internal/auth.go" {
		t.Fatalf("absolute workspace path = %q, want internal/auth.go", got)
	}
	if got := memoryflowRelPath(workspaceRoot, filepath.Join("internal", "auth.go")); got != "internal/auth.go" {
		t.Fatalf("relative workspace path = %q, want internal/auth.go", got)
	}
	if got := memoryflowRelPath(workspaceRoot, filepath.Join("..", "outside.go")); got != "" {
		t.Fatalf("parent-relative escape = %q, want empty", got)
	}
	if got := memoryflowRelPath(workspaceRoot, filepath.Join(filepath.Dir(workspaceRoot), "outside.go")); got != "" {
		t.Fatalf("absolute sibling escape = %q, want empty", got)
	}
	if got := memoryflowRelPath(workspaceRoot, filepath.Join(workspaceRoot, "..cache", "demo.go")); got != "..cache/demo.go" {
		t.Fatalf("dot-dot-prefixed child = %q, want ..cache/demo.go", got)
	}
}

func TestMemoryflowRelPathPropertyRejectsGeneratedEscapes(t *testing.T) {
	t.Parallel()

	workspaceRoot := filepath.Join(t.TempDir(), "workspace")
	parent := filepath.Dir(workspaceRoot)
	property := func(rawName string) bool {
		name := strings.TrimSpace(rawName)
		name = strings.NewReplacer("/", "_", "\\", "_").Replace(name)
		if name == "" {
			name = "file.go"
		}

		if got := memoryflowRelPath(workspaceRoot, filepath.Join("..", name)); got != "" {
			t.Logf("relative escape %q normalized to %q", name, got)
			return false
		}
		if got := memoryflowRelPath(workspaceRoot, filepath.Join(parent, name)); got != "" {
			t.Logf("absolute sibling escape %q normalized to %q", name, got)
			return false
		}

		childName := "..cache-" + name
		want := filepath.ToSlash(childName)
		if got := memoryflowRelPath(workspaceRoot, filepath.Join(workspaceRoot, childName)); got != want {
			t.Logf("dot-dot-prefixed child %q normalized to %q, want %q", childName, got, want)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatalf("memoryflow path property failed: %v", err)
	}
}

func TestRefreshMemoryEmbeddingSkipsEscapingPath(t *testing.T) {
	t.Parallel()

	called := false
	deps := Dependencies{
		RunSkill: func(ctx context.Context, skill string, input any, workspace string, out any) error {
			called = true
			return nil
		},
	}
	workspaceRoot := t.TempDir()
	err := refreshMemoryEmbedding(context.Background(), deps, workspaceRoot, LifecyclePayload{
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
			FilePath: filepath.Join("..", "outside.go"),
		},
	})
	if err != nil {
		t.Fatalf("refreshMemoryEmbedding: %v", err)
	}
	if called {
		t.Fatalf("refreshMemoryEmbedding called embedding refresh for escaping path")
	}
}

func TestSummarizeEditChangePreservesUTF8WhenTruncatingUnicode(t *testing.T) {
	t.Parallel()

	payload := LifecyclePayload{
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
			OldString: strings.Repeat("é", 60),
			NewString: strings.Repeat("界", 60),
		},
	}

	changeType, summary := summarizeEditChange("Edit", payload)
	if changeType != "edit" {
		t.Fatalf("change type = %q, want edit", changeType)
	}
	if !utf8.ValidString(summary) {
		t.Fatalf("summary is not valid UTF-8: %q", summary)
	}
	if strings.Contains(summary, strings.Repeat("é", 51)) || strings.Contains(summary, strings.Repeat("界", 51)) {
		t.Fatalf("summary did not truncate edit snippets to 50 runes: %q", summary)
	}
}

func TestMemoryflowTruncatePropertyPreservesUTF8AndRuneLimit(t *testing.T) {
	t.Parallel()

	property := func(input string, rawLimit uint8) bool {
		if !utf8.ValidString(input) {
			return true
		}
		limit := int(rawLimit)
		got := truncate(input, limit)
		if !utf8.ValidString(got) {
			t.Logf("truncate(%q, %d) produced invalid UTF-8: %q", input, limit, got)
			return false
		}
		if strings.TrimSpace(got) != got {
			t.Logf("truncate(%q, %d) kept leading or trailing whitespace: %q", input, limit, got)
			return false
		}
		if limit <= 0 {
			return got == ""
		}
		if utf8.RuneCountInString(got) > limit {
			t.Logf("truncate(%q, %d) produced %d runes", input, limit, utf8.RuneCountInString(got))
			return false
		}
		trimmed := strings.TrimSpace(input)
		if utf8.RuneCountInString(trimmed) <= limit {
			return got == trimmed
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("truncate property failed: %v", err)
	}
}
