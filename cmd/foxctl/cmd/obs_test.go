package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	configpkg "github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

func TestObsEventsCommandQueriesNonErrorEvents(t *testing.T) {
	obsDir := t.TempDir()
	workspaceRoot := t.TempDir()
	workspaceID := workspace.ID(workspaceRoot)
	now := time.Now().UTC()
	writeErrorsTestEvents(t, obsDir, []observability.Event{
		{
			Timestamp: now.Add(-2 * time.Minute),
			Operation: "watch.start",
			Status:    observability.StatusOK,
			Data: map[string]any{
				observability.DataKeyService:     "foxctl",
				observability.DataKeyComponent:   observability.ComponentCLI,
				observability.DataKeyWorkspaceID: workspaceID,
			},
		},
		{
			Timestamp:    now.Add(-1 * time.Minute),
			Operation:    "watch.error",
			Status:       observability.StatusError,
			ErrorCode:    "EWATCH",
			ErrorMessage: "watch failed",
			Data: map[string]any{
				observability.DataKeyService:     "foxctl",
				observability.DataKeyComponent:   observability.ComponentCLI,
				observability.DataKeyWorkspaceID: workspaceID,
			},
		},
	})

	t.Setenv("FOXCTL_OBS_DIR", obsDir)
	cmd := newObsEventsCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(configpkg.WithContext(context.Background(), configpkg.Config{}))
	cmd.SetArgs([]string{"--workspace", workspaceRoot, "--limit", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	data := decodeObsTestData(t, stdout.Bytes(), "foxctl.obs.events")
	if got := int(data["count"].(float64)); got != 2 {
		t.Fatalf("count=%d want 2", got)
	}
	summary := data["summary"].(map[string]any)
	byStatus := summary["by_status"].(map[string]any)
	if got := int(byStatus["ok"].(float64)); got != 1 {
		t.Fatalf("summary.by_status.ok=%d want 1", got)
	}
	if got := int(byStatus["error"].(float64)); got != 1 {
		t.Fatalf("summary.by_status.error=%d want 1", got)
	}
}

func TestObsSymbolMetricsCommandSummarizesFunctionSizes(t *testing.T) {
	obsDir := t.TempDir()
	workspaceRoot := t.TempDir()
	workspaceID := workspace.ID(workspaceRoot)
	now := time.Now().UTC()
	writeErrorsTestEvents(t, obsDir, []observability.Event{
		symbolMetricTestEvent(now.Add(-3*time.Minute), workspaceID, "a.go", "A", "function", 10, 18),
		symbolMetricTestEvent(now.Add(-2*time.Minute), workspaceID, "b.go", "B", "function", 40, 50),
		symbolMetricTestEvent(now.Add(-1*time.Minute), workspaceID, "c.go", "C", "class", 20, 30),
		symbolMetricTestEvent(now.Add(-1*time.Minute), "other-workspace", "huge.go", "Huge", "function", 100, 120),
	})

	t.Setenv("FOXCTL_OBS_DIR", obsDir)
	cmd := newObsSymbolMetricsCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(configpkg.WithContext(context.Background(), configpkg.Config{}))
	cmd.SetArgs([]string{"--workspace", workspaceRoot, "--limit", "10", "--top", "2", "--sort-by", "source_lines"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	data := decodeObsTestData(t, stdout.Bytes(), "foxctl.obs.symbol_metrics")
	if got := int(data["count"].(float64)); got != 3 {
		t.Fatalf("count=%d want 3", got)
	}
	summary := data["summary"].(map[string]any)
	sourceLines := summary["source_lines"].(map[string]any)
	if got := int(sourceLines["max"].(float64)); got != 40 {
		t.Fatalf("source_lines.max=%d want 40", got)
	}
	if got := int(sourceLines["p50"].(float64)); got != 20 {
		t.Fatalf("source_lines.p50=%d want 20", got)
	}
	byKind := summary["by_kind"].(map[string]any)
	functionStats := byKind["function"].(map[string]any)
	if got := int(functionStats["count"].(float64)); got != 2 {
		t.Fatalf("by_kind.function.count=%d want 2", got)
	}
	largest := data["largest"].([]any)
	if len(largest) != 2 {
		t.Fatalf("largest len=%d want 2", len(largest))
	}
	first := largest[0].(map[string]any)
	if got := first["symbol_id"].(string); got != "B" {
		t.Fatalf("largest[0].symbol_id=%q want B", got)
	}
}

func symbolMetricTestEvent(ts time.Time, workspaceID, path, symbolID, kind string, sourceLines, embeddingLines int) observability.Event {
	return observability.Event{
		Timestamp: ts,
		Operation: symbolEmbeddingTextOperation,
		Status:    observability.StatusOK,
		Data: map[string]any{
			observability.DataKeyService:     "foxctl",
			observability.DataKeyComponent:   observability.ComponentJob,
			observability.DataKeyWorkspaceID: workspaceID,
			"indexer_id":                     "code_symbol_dag",
			"file_path":                      path,
			"symbol_id":                      symbolID,
			"symbol_kind":                    kind,
			"source_chars":                   sourceLines * 10,
			"source_lines":                   sourceLines,
			"stripped_source_chars":          sourceLines * 8,
			"stripped_source_lines":          sourceLines - 1,
			"embedding_text_chars":           embeddingLines * 10,
			"embedding_text_lines":           embeddingLines,
			"field_count":                    1,
			"relationship_hint_count":        2,
			"semantic_anchor_count":          1,
		},
	}
}

func decodeObsTestData(t *testing.T, raw []byte, command string) map[string]any {
	t.Helper()
	var env envelope.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("Unmarshal envelope error = %v", err)
	}
	if env.Command != command {
		t.Fatalf("command=%q want %s", env.Command, command)
	}
	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T want map[string]any", env.Data)
	}
	return data
}
