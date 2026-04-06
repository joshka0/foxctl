package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestLogCleanupHandlerDeletesMatchingEntries(t *testing.T) {
	t.Parallel()

	obsDir := t.TempDir()
	t.Setenv("AGENTCTL_OBS_DIR", obsDir)
	writeWideEventsFileForAPITest(t, obsDir, []observability.WideEvent{
		{
			Ts:           time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC),
			TraceID:      "trace-smoke",
			SessionID:    "session-smoke",
			Component:    observability.ComponentWeb,
			Operation:    "web.error",
			Status:       observability.StatusError,
			ErrorMessage: "gui smoke failure",
		},
		{
			Ts:           time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC),
			TraceID:      "trace-keep",
			SessionID:    "session-keep",
			Component:    observability.ComponentAgent,
			Operation:    "agent.iteration",
			Status:       observability.StatusError,
			ErrorMessage: "keep me",
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/logs/cleanup", strings.NewReader(`{
		"errors_only": true,
		"text_query": "smoke"
	}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	LogCleanupHandler(config.Config{}, zerolog.Nop())(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	var resp struct {
		Status  string   `json:"status"`
		Deleted int64    `json:"deleted"`
		Errors  []string `json:"errors"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if resp.Status != "ok" {
		t.Fatalf("status=%q want ok", resp.Status)
	}
	if resp.Deleted != 1 {
		t.Fatalf("deleted=%d want 1", resp.Deleted)
	}
	if len(resp.Errors) != 0 {
		t.Fatalf("errors=%v want none", resp.Errors)
	}

	entries, err := observability.QueryEventRecords(req.Context(), observability.EventQueryOptions{
		ObsDir: obsDir,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("QueryEventRecords() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
	if entries[0].TraceID != "trace-keep" {
		t.Fatalf("remaining trace=%q want trace-keep", entries[0].TraceID)
	}
}

func writeWideEventsFileForAPITest(t *testing.T, obsDir string, events []observability.WideEvent) {
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
