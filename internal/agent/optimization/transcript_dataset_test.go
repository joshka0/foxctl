package optimization_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

func TestExportTranscriptDataset(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	sessionStore, err := sessions.Open(ctx, root)
	if err != nil {
		t.Fatalf("open session store: %v", err)
	}
	defer sessionStore.Close() //nolint:errcheck

	memStore, err := memory.Open(ctx, root, t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer memStore.Close() //nolint:errcheck

	savedSession, err := sessionStore.Save(ctx, storage.Session{
		ID:            "sess-1",
		WorkspacePath: "/tmp/ws",
		ProjectName:   "agentctl",
		AgentType:     "codex",
		RawJSONLPath:  "/tmp/codex-session.jsonl",
		Prompt:        "You are a coding assistant.",
		PromptHash:    "sha256:prompt",
		LLMProvider:   "lmstudio",
		LLMModel:      "model-a",
		StartedAt:     time.Now().UTC(),
		EndedAt:       time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}

	turns := []storage.SessionTurn{
		{
			ID:             "turn-1",
			SessionID:      savedSession.ID,
			TurnIndex:      1,
			Role:           "user",
			ContentPreview: "Fix the broken test",
			Timestamp:      time.Now().UTC(),
			CreatedAt:      time.Now().UTC(),
		},
		{
			ID:             "turn-2",
			SessionID:      savedSession.ID,
			TurnIndex:      2,
			Role:           "assistant",
			ContentPreview: "I updated the failing assertion and reran the suite.",
			ToolCalls: []storage.ToolCall{
				{Name: "read"},
				{Name: "edit"},
			},
			FilesTouched: []string{"internal/foo_test.go"},
			Timestamp:    time.Now().UTC(),
			CreatedAt:    time.Now().UTC(),
		},
	}
	if err := sessionStore.SaveTurns(ctx, turns); err != nil {
		t.Fatalf("save turns: %v", err)
	}

	if _, err := memStore.SaveResult(ctx, storage.MemorySaveOptions{
		Name:      "session-feedback-1",
		Type:      "session_feedback",
		Workspace: "/tmp/ws",
		SessionID: savedSession.ID,
		Summary:   "feedback",
		Result:    []byte(`{"session_id":"sess-1","rating":5,"outcome":"success","notes":"strong execution","timestamp":"2026-03-18T00:00:00Z"}`),
	}); err != nil {
		t.Fatalf("save feedback: %v", err)
	}

	examples, err := optimization.ExportTranscriptDataset(ctx, sessionStore, memStore, optimization.TranscriptDatasetRequest{
		WorkspacePath:   "/tmp/ws",
		Source:          "codex",
		IncludeTools:    true,
		IncludeFiles:    true,
		IncludeFeedback: true,
		Limit:           10,
	})
	if err != nil {
		t.Fatalf("ExportTranscriptDataset: %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("len(examples)=%d want 1", len(examples))
	}

	example := examples[0]
	if example.Metadata.AgentType != "codex" {
		t.Fatalf("agent_type=%q want codex", example.Metadata.AgentType)
	}
	if example.Metadata.Rating != 5 {
		t.Fatalf("rating=%d want 5", example.Metadata.Rating)
	}
	if len(example.Output.ToolsUsed) != 2 {
		t.Fatalf("tools_used=%v want 2 entries", example.Output.ToolsUsed)
	}
	if len(example.Input.Files) != 1 || example.Input.Files[0] != "internal/foo_test.go" {
		t.Fatalf("files=%v want internal/foo_test.go", example.Input.Files)
	}
}

func TestExportTranscriptDataset_CodexRawFallback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	sessionStore, err := sessions.Open(ctx, root)
	if err != nil {
		t.Fatalf("open session store: %v", err)
	}
	defer sessionStore.Close() //nolint:errcheck

	memStore, err := memory.Open(ctx, root, t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer memStore.Close() //nolint:errcheck

	rawPath := root + "/codex-session.jsonl"
	raw := strings.Join([]string{
		`{"type":"response_item","timestamp":"2026-03-19T10:00:00Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Please review the diff"}]}}`,
		`{"type":"response_item","timestamp":"2026-03-19T10:00:01Z","payload":{"type":"function_call","name":"rg"}}`,
		`{"type":"response_item","timestamp":"2026-03-19T10:00:02Z","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I found two concrete issues in the diff."}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(rawPath, []byte(raw), 0o644); err != nil {
		t.Fatalf("write raw codex transcript: %v", err)
	}

	_, err = sessionStore.Save(ctx, storage.Session{
		ID:            "codex-raw-1",
		WorkspacePath: "/tmp/ws",
		ProjectName:   "ws",
		AgentType:     "codex",
		RawJSONLPath:  rawPath,
	})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}

	examples, err := optimization.ExportTranscriptDataset(ctx, sessionStore, memStore, optimization.TranscriptDatasetRequest{
		WorkspacePath: "/tmp/ws",
		Source:        "codex",
		Category:      "coder_impl",
		IncludeTools:  true,
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("ExportTranscriptDataset fallback: %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("len(examples)=%d want 1", len(examples))
	}
	if examples[0].Input.UserRequest != "Please review the diff" {
		t.Fatalf("user_request=%q", examples[0].Input.UserRequest)
	}
	if examples[0].Metadata.AgentType != "codex" {
		t.Fatalf("agent_type=%q want codex", examples[0].Metadata.AgentType)
	}
	if len(examples[0].Output.ToolsUsed) != 1 || examples[0].Output.ToolsUsed[0] != "rg" {
		t.Fatalf("tools_used=%v want [rg]", examples[0].Output.ToolsUsed)
	}
}

func TestExportTranscriptDataset_RespectsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	sessionStore, err := sessions.Open(ctx, root)
	if err != nil {
		t.Fatalf("open session store: %v", err)
	}
	defer sessionStore.Close() //nolint:errcheck

	memStore, err := memory.Open(ctx, root, t.TempDir())
	if err != nil {
		t.Fatalf("open memory store: %v", err)
	}
	defer memStore.Close() //nolint:errcheck

	_, err = sessionStore.Save(ctx, storage.Session{
		ID:            "cancel-1",
		WorkspacePath: "/tmp/ws",
		ProjectName:   "ws",
		AgentType:     "codex",
		RawJSONLPath:  root + "/missing.jsonl",
	})
	if err != nil {
		t.Fatalf("save session: %v", err)
	}

	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = optimization.ExportTranscriptDataset(canceledCtx, sessionStore, memStore, optimization.TranscriptDatasetRequest{
		WorkspacePath: "/tmp/ws",
		Source:        "codex",
		Limit:         10,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v want context.Canceled", err)
	}
}

func TestBuildTranscriptDatasetJSONL(t *testing.T) {
	t.Parallel()

	body, err := optimization.BuildTranscriptDatasetJSONL([]optimization.TranscriptTrainingExample{
		{
			Input:    optimization.TranscriptTrainingInput{UserRequest: "Question"},
			Output:   optimization.TranscriptTrainingOutput{Response: "Answer"},
			Metadata: optimization.TranscriptTrainingMetadata{SessionID: "sess-1"},
		},
	})
	if err != nil {
		t.Fatalf("BuildTranscriptDatasetJSONL: %v", err)
	}
	if !strings.Contains(string(body), `"session_id":"sess-1"`) {
		t.Fatalf("missing session_id in JSONL: %s", string(body))
	}
	if bytes.Count(body, []byte("\n")) != 1 {
		t.Fatalf("expected one JSONL line, got %q", string(body))
	}
}

func TestBuildTranscriptDatasetJSONLGolden(t *testing.T) {
	t.Parallel()

	body, err := optimization.BuildTranscriptDatasetJSONL([]optimization.TranscriptTrainingExample{
		{
			Input: optimization.TranscriptTrainingInput{
				UserRequest: "Please review the diff",
				Context:     "Context block",
				Files:       []string{"internal/foo.go"},
			},
			Output: optimization.TranscriptTrainingOutput{
				Response:    "I found two issues.",
				ToolsUsed:   []string{"read", "edit"},
				FilesEdited: []string{"internal/foo.go"},
			},
			Metadata: optimization.TranscriptTrainingMetadata{
				SessionID:   "sess-golden",
				AgentType:   "codex",
				Category:    "coder_impl",
				ProjectName: "agentctl",
				TurnIndex:   2,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildTranscriptDatasetJSONL() error = %v", err)
	}

	goldenPath := filepath.Join("testdata", "transcript_dataset_golden.jsonl")
	updateTranscriptGoldenFile(t, goldenPath, body)
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("ReadFile(golden) error = %v", err)
	}
	if !bytes.Equal(body, want) {
		var gotPretty bytes.Buffer
		var wantPretty bytes.Buffer
		prettyJSONLines(&gotPretty, body)
		prettyJSONLines(&wantPretty, want)
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", wantPretty.String(), gotPretty.String())
	}
}

func TestParseTranscriptDatasetJSONLLargeLine(t *testing.T) {
	t.Parallel()

	largeResponse := strings.Repeat("y", 70_000)
	body := strings.NewReader(`{"input":{"user_request":"Question"},"output":{"response":"` + largeResponse + `"},"metadata":{"session_id":"sess-1"}}
`)

	examples, err := optimization.ParseTranscriptDatasetJSONL(body)
	if err != nil {
		t.Fatalf("ParseTranscriptDatasetJSONL(large): %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("len(examples)=%d want 1", len(examples))
	}
	if examples[0].Output.Response != largeResponse {
		t.Fatalf("response length=%d want %d", len(examples[0].Output.Response), len(largeResponse))
	}
}

func TestCategorizeTranscriptUserRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		user     string
		response string
		want     string
	}{
		{name: "coder_impl", user: "Please review the v2 implementation", response: "I found two concrete issues in the diff.", want: "coder_impl"},
		{name: "ops_infra", user: "is the embedding worker running?", response: "Yes, but the queue is still full.", want: "ops_infra"},
		{name: "coder_impl_plan", user: "Implement the following plan: GUI Agent UX noise reduction", response: "I'll start by reading the key files to understand the current code before implementing changes.", want: "coder_impl"},
		{name: "coder_impl_schema_design", user: "How would that look like?", response: "Here's the concrete design and schema with SQL for group content encryption.", want: "coder_impl"},
		{name: "ops_infra_port_forward", user: "Interesting i ran dev-port-forward, okay i see it now! So how do I get to index.html", response: "http://localhost:8080 already serves index.html, but the app is failing API calls.", want: "ops_infra"},
		{name: "release_workflow", user: "check the job", response: "Pipeline passed.", want: "release_workflow"},
		{name: "continuation", user: "continue", response: "Picking up where we left off.", want: "continuation"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := optimization.CategorizeTranscriptUserRequest(tt.user, tt.response); got != tt.want {
				t.Fatalf("category=%q want %q", got, tt.want)
			}
		})
	}
}

func TestShouldKeepTranscriptCategoryExample(t *testing.T) {
	t.Parallel()

	example := optimization.TranscriptTrainingExample{
		Metadata: optimization.TranscriptTrainingMetadata{Category: "coder_impl"},
	}
	if !optimization.ShouldKeepTranscriptCategoryExample(example, "coder_impl") {
		t.Fatal("expected coder_impl category to match")
	}
	if optimization.ShouldKeepTranscriptCategoryExample(example, "ops_infra") {
		t.Fatal("expected ops_infra category to be filtered")
	}
	if !optimization.ShouldKeepTranscriptCategoryExample(example, "all") {
		t.Fatal("expected all category to keep example")
	}
}

func updateTranscriptGoldenFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("WriteFile(golden) error = %v", err)
		}
	}
}

func prettyJSONLines(dst *bytes.Buffer, body []byte) {
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	for _, line := range lines {
		var decoded any
		if err := json.Unmarshal([]byte(line), &decoded); err != nil {
			_, _ = dst.WriteString(line + "\n")
			continue
		}
		pretty, err := json.MarshalIndent(decoded, "", "  ")
		if err != nil {
			_, _ = dst.WriteString(line + "\n")
			continue
		}
		_, _ = dst.Write(pretty)
		_, _ = dst.WriteString("\n")
	}
}
