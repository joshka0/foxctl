package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/observability"
	configpkg "github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
)

func TestErrorsCommandUsesObservabilityEvents(t *testing.T) {
	obsDir := t.TempDir()
	workspaceRoot := t.TempDir()
	workspaceID := workspace.ID(workspaceRoot)
	observabilityEvents := []observability.WideEvent{
		{
			Ts:           time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC),
			TraceID:      "trace-a",
			SpanID:       "span-a",
			Service:      "agentctl",
			Component:    observability.ComponentAgent,
			Operation:    "agent.iteration",
			WorkspaceID:  workspaceID,
			Status:       observability.StatusError,
			ErrorCode:    "EAGENT",
			ErrorMessage: "agent blew up",
		},
		{
			Ts:          time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
			TraceID:     "trace-b",
			SpanID:      "span-b",
			Service:     "agentctl",
			Component:   observability.ComponentCLI,
			Operation:   "watch.start",
			WorkspaceID: workspaceID,
			Status:      observability.StatusOK,
		},
		{
			Ts:           time.Date(2026, 3, 20, 11, 0, 0, 0, time.UTC),
			TraceID:      "trace-c",
			SpanID:       "span-c",
			Service:      "agentctl",
			Component:    observability.ComponentCLI,
			Operation:    "watch.error",
			WorkspaceID:  "other-workspace",
			Status:       observability.StatusError,
			ErrorCode:    "EWATCH",
			ErrorMessage: "watch failed",
		},
	}
	writeErrorsTestEvents(t, obsDir, observabilityEvents)

	t.Setenv("AGENTCTL_OBS_DIR", obsDir)

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
	if env.Command != "agentctl.errors" {
		t.Fatalf("command=%q want agentctl.errors", env.Command)
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

func writeErrorsTestEvents(t *testing.T, obsDir string, events []observability.WideEvent) {
	t.Helper()

	eventsDir := filepath.Join(obsDir, "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	f, err := os.Create(filepath.Join(eventsDir, observability.WideEventFileName+".ndjson"))
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
