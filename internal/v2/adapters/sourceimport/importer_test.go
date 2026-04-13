package sourceimport

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/context/todosync"
	"github.com/jkatigb/agentctl/internal/v2/adapters/libsql/turns"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
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

func TestParseClaudeFile_UsesInjectedClockForMissingTimestamps(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "claude-missing-timestamp.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":"Review pipeline"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Pipeline reviewed."}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	fixedNow := time.Date(2026, time.February, 24, 8, 30, 0, 0, time.UTC)
	parsed, err := parseClaudeFileWithClock(path, "sess-fixed-time", "/tmp/workspace", "actor:test", func() time.Time {
		return fixedNow
	})
	if err != nil {
		t.Fatalf("parseClaudeFileWithClock() error = %v", err)
	}
	if len(parsed.Turns) != 1 {
		t.Fatalf("turn count=%d want 1", len(parsed.Turns))
	}
	turn := parsed.Turns[0]
	if !turn.CreatedAt.Equal(fixedNow) {
		t.Fatalf("created_at=%s want %s", turn.CreatedAt, fixedNow)
	}
	if !turn.UpdatedAt.Equal(fixedNow) {
		t.Fatalf("updated_at=%s want %s", turn.UpdatedAt, fixedNow)
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
	foundSourceTag := false
	for _, artifact := range first.Artifacts {
		typeSet[artifact.ArtifactType] = true
		if artifact.ArtifactType == turns.ArtifactTypeEmbedding {
			firstEmbedding = artifact.Embedding
		}
		if artifact.ArtifactType == turns.ArtifactTypeAnnotation {
			var metadata map[string]any
			if err := json.Unmarshal(artifact.MetadataJSON, &metadata); err != nil {
				t.Fatalf("unmarshal annotation metadata: %v", err)
			}
			if strings.TrimSpace(toString(metadata["artifact_from"])) != "sourceimport" {
				t.Fatalf("annotation metadata artifact_from=%q want sourceimport", toString(metadata["artifact_from"]))
			}
			foundSourceTag = true
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
	if !foundSourceTag {
		t.Fatal("expected annotation artifact with sourceimport provenance tag")
	}
	if BinaryDigest(firstEmbedding) == "" {
		t.Fatal("embedding digest is empty")
	}
	if BinaryDigest(firstEmbedding) != BinaryDigest(secondEmbedding) {
		t.Fatalf("embedding digest mismatch first=%s second=%s",
			BinaryDigest(firstEmbedding), BinaryDigest(secondEmbedding))
	}
}

func TestBuildEpisodes_DeterministicChunkedOutput(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 22, 12, 0, 0, 0, time.UTC)
	parsed := ParsedSession{
		Provider:  ProviderClaude,
		SessionID: "sess-episodes",
		Turns: []run.TurnRecord{
			{
				ID:        "turn-e-1",
				SessionID: "source:claude:sess-episodes",
				TurnIndex: 1,
				Prompt:    "Investigate failing migration tests",
				CreatedAt: now.Add(-3 * time.Hour),
				UpdatedAt: now.Add(-3 * time.Hour),
			},
			{
				ID:        "turn-e-2",
				SessionID: "source:claude:sess-episodes",
				TurnIndex: 2,
				Prompt:    "we decided to keep libsql fallback",
				FinalOutput: run.MessageRef{
					ID:   "msg-e-2",
					Role: "assistant",
					Text: "decision recorded",
				},
				Iterations: []run.IterationRecord{
					{
						IterationIndex: 1,
						ToolCalls: []run.ToolCallRecord{
							{Name: "code_search", Status: "error"},
						},
					},
				},
				CreatedAt: now.Add(-2 * time.Hour),
				UpdatedAt: now.Add(-2 * time.Hour),
			},
			{
				ID:        "turn-e-3",
				SessionID: "source:claude:sess-episodes",
				TurnIndex: 3,
				Prompt:    "ship it",
				CreatedAt: now.Add(-1 * time.Hour),
				UpdatedAt: now.Add(-1 * time.Hour),
			},
		},
	}

	artifacts := []turns.Artifact{
		{
			TurnID:          "turn-e-1",
			ArtifactType:    turns.ArtifactTypeAnnotation,
			ArtifactVersion: "v1",
			Ref:             turns.BuildArtifactRef("turn-e-1", turns.ArtifactTypeAnnotation, "v1"),
		},
		{
			TurnID:          "turn-e-2",
			ArtifactType:    turns.ArtifactTypeClassification,
			ArtifactVersion: "v1",
			Ref:             turns.BuildArtifactRef("turn-e-2", turns.ArtifactTypeClassification, "v1"),
		},
		{
			TurnID:          "turn-e-3",
			ArtifactType:    turns.ArtifactTypeLearning,
			ArtifactVersion: "v1",
			Ref:             turns.BuildArtifactRef("turn-e-3", turns.ArtifactTypeLearning, "v1"),
		},
	}

	opts := EpisodeBuildOptions{
		EpisodeVersion:     "v1",
		MaxTurnsPerEpisode: 2,
		Now:                func() time.Time { return now },
	}

	first := BuildEpisodes(parsed, artifacts, opts)
	second := BuildEpisodes(parsed, artifacts, opts)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("BuildEpisodes() not deterministic:\nfirst=%+v\nsecond=%+v", first, second)
	}

	if len(first.Episodes) != 2 {
		t.Fatalf("episodes len=%d want 2", len(first.Episodes))
	}
	ep1 := first.Episodes[0]
	if ep1.BoundaryKey != "chunk:0001-0002:turn-e-1-turn-e-2" {
		t.Fatalf("episode boundary_key=%q want chunk:0001-0002:turn-e-1-turn-e-2", ep1.BoundaryKey)
	}
	if ep1.StartTurnID != "turn-e-1" || ep1.EndTurnID != "turn-e-2" {
		t.Fatalf("episode turn span unexpected: start=%q end=%q", ep1.StartTurnID, ep1.EndTurnID)
	}
	if !ep1.IsLandmark {
		t.Fatal("episode 1 should be landmark due to decision/error cues")
	}
	if len(ep1.AnchorRefs) == 0 {
		t.Fatal("episode anchor refs should not be empty")
	}
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}
