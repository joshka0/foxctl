package sourceimport

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/todosync"
	"github.com/jkatigb/agentctl/internal/v2/adapters/libsql/turns"
)

func TestParseClaudeFile_ToCanonicalTurns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "claude-session-1.jsonl")
	lines := []string{
		`{"type":"user","timestamp":"2026-02-20T10:00:00Z","message":{"role":"user","content":"Find TODOs in runtime"}}`,
		`{"type":"assistant","timestamp":"2026-02-20T10:00:01Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"code_search","id":"call-1","input":{"query":"TODO runtime"}}]}}`,
		`{"type":"user","timestamp":"2026-02-20T10:00:02Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","is_error":false,"content":"3 matches"}]}}`,
		`{"type":"assistant","timestamp":"2026-02-20T10:00:03Z","message":{"role":"assistant","content":[{"type":"text","text":"I found three TODOs in runtime."}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	parsed, err := ParseClaudeFile(path, "", "/tmp/workspace", "actor:test")
	if err != nil {
		t.Fatalf("ParseClaudeFile() error = %v", err)
	}
	if parsed.Provider != ProviderClaude {
		t.Fatalf("provider=%q want %q", parsed.Provider, ProviderClaude)
	}
	if parsed.SessionID == "" {
		t.Fatal("session id is empty")
	}
	if len(parsed.Turns) != 1 {
		t.Fatalf("turn count=%d want 1", len(parsed.Turns))
	}
	turn := parsed.Turns[0]
	if !strings.Contains(turn.Prompt, "Find TODOs") {
		t.Fatalf("prompt=%q want contains %q", turn.Prompt, "Find TODOs")
	}
	if turn.FinalOutput.Text == "" {
		t.Fatal("final output is empty")
	}
	if !strings.Contains(strings.ToLower(turn.FinalOutput.Text), "todo") {
		t.Fatalf("final output=%q want TODO summary", turn.FinalOutput.Text)
	}
	toolCalls := 0
	for _, iter := range turn.Iterations {
		toolCalls += len(iter.ToolCalls)
	}
	if toolCalls == 0 {
		t.Fatal("expected at least one tool call")
	}
}

func TestParseCodexFile_ToCanonicalTurns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "rollout-abc-12345678-1234-1234-1234-123456789abc.jsonl")
	lines := []string{
		`{"type":"response_item","timestamp":"2026-02-20T11:00:00Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Summarize storage architecture"}]}}`,
		`{"type":"response_item","timestamp":"2026-02-20T11:00:01Z","payload":{"type":"function_call","name":"code_search","call_id":"codex-call-1","arguments":{"query":"storage adapter"}}}`,
		`{"type":"response_item","timestamp":"2026-02-20T11:00:02Z","payload":{"type":"function_call_output","call_id":"codex-call-1","status":"ok","output":"found adapters"}}`,
		`{"type":"response_item","timestamp":"2026-02-20T11:00:03Z","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Storage adapters are split by backend."}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	parsed, err := ParseCodexFile(path, "", "/tmp/workspace", "actor:test")
	if err != nil {
		t.Fatalf("ParseCodexFile() error = %v", err)
	}
	if parsed.Provider != ProviderCodex {
		t.Fatalf("provider=%q want %q", parsed.Provider, ProviderCodex)
	}
	if parsed.SessionID == "" {
		t.Fatal("session id is empty")
	}
	if len(parsed.Turns) != 1 {
		t.Fatalf("turn count=%d want 1", len(parsed.Turns))
	}
	turn := parsed.Turns[0]
	if !strings.Contains(strings.ToLower(turn.Prompt), "storage") {
		t.Fatalf("prompt=%q want storage topic", turn.Prompt)
	}
	if turn.FinalOutput.Text == "" {
		t.Fatal("final output is empty")
	}
}

func TestBuildArtifacts_DerivesDeterministicTypes(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "claude-seed.jsonl")
	lines := []string{
		`{"type":"user","timestamp":"2026-02-20T12:00:00Z","message":{"role":"user","content":"Review failing tests"}}`,
		`{"type":"assistant","timestamp":"2026-02-20T12:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"I will check the test output and suggest fixes."}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	parsed, err := ParseClaudeFile(path, "sess-artifacts", "/tmp/workspace", "actor:test")
	if err != nil {
		t.Fatalf("ParseClaudeFile() error = %v", err)
	}
	if len(parsed.Turns) != 1 {
		t.Fatalf("turn count=%d want 1", len(parsed.Turns))
	}

	opts := ArtifactBuildOptions{
		IncludeEmbedding: true,
		Embedder:         NewHashEmbedder(16),
		Todos: []todosync.ClaudeTodo{
			{Content: "Fix flaky test", Status: "in_progress"},
			{Content: "Document migration", Status: "pending"},
		},
	}
	first := BuildArtifacts(context.Background(), parsed, opts)
	second := BuildArtifacts(context.Background(), parsed, opts)

	if len(first.Warnings) != 0 {
		t.Fatalf("warnings=%v want none", first.Warnings)
	}
	if len(first.Artifacts) != 4 {
		t.Fatalf("artifact count=%d want 4", len(first.Artifacts))
	}

	typeSet := map[string]bool{}
	var firstEmbedding, secondEmbedding []float32
	for _, artifact := range first.Artifacts {
		typeSet[artifact.ArtifactType] = true
		if artifact.ArtifactType == turns.ArtifactTypeEmbedding {
			firstEmbedding = artifact.Embedding
		}
	}
	for _, artifact := range second.Artifacts {
		if artifact.ArtifactType == turns.ArtifactTypeEmbedding {
			secondEmbedding = artifact.Embedding
		}
	}
	for _, want := range []string{
		turns.ArtifactTypeAnnotation,
		turns.ArtifactTypeClassification,
		turns.ArtifactTypeLearning,
		turns.ArtifactTypeEmbedding,
	} {
		if !typeSet[want] {
			t.Fatalf("missing artifact type %q", want)
		}
	}
	if len(firstEmbedding) != 16 {
		t.Fatalf("embedding dims=%d want 16", len(firstEmbedding))
	}
	if BinaryDigest(firstEmbedding) == "" {
		t.Fatal("embedding digest is empty")
	}
	if BinaryDigest(firstEmbedding) != BinaryDigest(secondEmbedding) {
		t.Fatalf("embedding digest mismatch first=%s second=%s",
			BinaryDigest(firstEmbedding), BinaryDigest(secondEmbedding))
	}
}
