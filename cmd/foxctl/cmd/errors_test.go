package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	configpkg "github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/observability"
)

func TestErrorsCommandUsesObservabilityEvents(t *testing.T) {
	obsDir := t.TempDir()
	workspaceRoot := t.TempDir()
	workspaceID := workspace.ExplicitID(workspaceRoot)
	now := time.Now().UTC()
	observabilityEvents := []observability.Event{
		{
			Timestamp:    now.Add(-2 * time.Hour),
			TraceID:      "trace-a",
			SpanID:       "span-a",
			Operation:    "agent.iteration",
			Status:       observability.StatusError,
			ErrorCode:    "EAGENT",
			ErrorMessage: "agent blew up",
			Data: map[string]any{
				observability.DataKeyService:     "foxctl",
				observability.DataKeyComponent:   observability.ComponentAgent,
				observability.DataKeyWorkspaceID: workspaceID,
			},
		},
		{
			Timestamp: now.Add(-90 * time.Minute),
			TraceID:   "trace-b",
			SpanID:    "span-b",
			Operation: "watch.start",
			Status:    observability.StatusOK,
			Data: map[string]any{
				observability.DataKeyService:     "foxctl",
				observability.DataKeyComponent:   observability.ComponentCLI,
				observability.DataKeyWorkspaceID: workspaceID,
			},
		},
		{
			Timestamp:    now.Add(-30 * time.Minute),
			TraceID:      "trace-c",
			SpanID:       "span-c",
			Operation:    "watch.error",
			Status:       observability.StatusError,
			ErrorCode:    "EWATCH",
			ErrorMessage: "watch failed",
			Data: map[string]any{
				observability.DataKeyService:     "foxctl",
				observability.DataKeyComponent:   observability.ComponentCLI,
				observability.DataKeyWorkspaceID: "other-workspace",
			},
		},
	}
	writeErrorsTestEvents(t, obsDir, observabilityEvents)

	t.Setenv("FOXCTL_OBS_DIR", obsDir)

	cmd := newErrorsCommand()
	var stdout bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stdout)
	cmd.SetContext(configpkg.WithContext(context.Background(), configpkg.Config{}))
	cmd.SetArgs([]string{"--workspace", workspaceRoot, "--limit", "10"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var env envelope.Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("Unmarshal envelope error = %v", err)
	}
	if env.Command != "foxctl.errors" {
		t.Fatalf("command=%q want foxctl.errors", env.Command)
	}

	data, ok := env.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T want map[string]any", env.Data)
	}
	count, ok := data["count"].(float64)
	if !ok {
		t.Fatalf("count type = %T want float64", data["count"])
	}
	if got := int(count); got != 1 {
		t.Fatalf("count=%d want 1", got)
	}

	summary, ok := data["summary"].(map[string]any)
	if !ok {
		t.Fatalf("summary type = %T want map[string]any", data["summary"])
	}
	byErrorCode, ok := summary["by_error_code"].(map[string]any)
	if !ok {
		t.Fatalf("by_error_code type = %T want map[string]any", summary["by_error_code"])
	}
	errorCount, ok := byErrorCode["EAGENT"].(float64)
	if !ok {
		t.Fatalf("by_error_code[EAGENT] type = %T want float64", byErrorCode["EAGENT"])
	}
	if got := int(errorCount); got != 1 {
		t.Fatalf("summary.by_error_code[EAGENT]=%d want 1", got)
	}

	filters, ok := data["filters"].(map[string]any)
	if !ok {
		t.Fatalf("filters type = %T want map[string]any", data["filters"])
	}
	filterWorkspace, ok := filters["workspace"].(string)
	if !ok {
		t.Fatalf("filters.workspace type = %T want string", filters["workspace"])
	}
	if got := filterWorkspace; got != workspaceID {
		t.Fatalf("filters.workspace=%q want %q", got, workspaceID)
	}
}

func writeErrorsTestEvents(t *testing.T, obsDir string, events []observability.Event) {
	t.Helper()

	eventsDir := filepath.Join(obsDir, "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	f, err := os.Create(filepath.Join(eventsDir, observability.EventFileName+".ndjson"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, event := range events {
		if err := enc.Encode(event); err != nil {
			t.Fatalf("Encode() error = %v", err)
		}
	}
}
