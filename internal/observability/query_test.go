package observability

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
)

func TestQueryEventRecordsFiltersAndSortsNewestFirst(t *testing.T) {
	t.Parallel()

	obsDir := t.TempDir()
	writeWideEventsFile(t, obsDir, []WideEvent{
		{
			Ts:          time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC),
			TraceID:     "trace-1",
			SpanID:      "span-1",
			Service:     "agentctl",
			Component:   ComponentCLI,
			Operation:   "watch.start",
			WorkspaceID: "ws-a",
			Status:      StatusOK,
		},
		{
			Ts:           time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC),
			TraceID:      "trace-2",
			SpanID:       "span-2",
			Service:      "agentctl",
			Component:    ComponentAgent,
			Operation:    "agent.iteration",
			WorkspaceID:  "ws-a",
			Status:       StatusError,
			ErrorCode:    "EAGENT",
			ErrorMessage: "agent failure",
		},
		{
			Ts:           time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
			TraceID:      "trace-3",
			SpanID:       "span-3",
			Service:      "agentctl",
			Component:    ComponentCLI,
			Operation:    "watch.error",
			WorkspaceID:  "ws-b",
			Status:       StatusError,
			ErrorCode:    "EWATCH",
			ErrorMessage: "watch failure",
		},
	})

	entries, err := QueryEventRecords(context.Background(), EventQueryOptions{
		ObsDir:      obsDir,
		Limit:       10,
		ErrorsOnly:  true,
		WorkspaceID: "ws-a",
	})
	if err != nil {
		t.Fatalf("QueryEventRecords() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
	if entries[0].ErrorCode != "EAGENT" {
		t.Fatalf("error_code=%q want EAGENT", entries[0].ErrorCode)
	}

	allEntries, err := QueryEventRecords(context.Background(), EventQueryOptions{
		ObsDir:     obsDir,
		Limit:      10,
		ErrorsOnly: true,
	})
	if err != nil {
		t.Fatalf("QueryEventRecords(all) error = %v", err)
	}
	if len(allEntries) != 2 {
		t.Fatalf("len(allEntries)=%d want 2", len(allEntries))
	}
	if allEntries[0].ErrorCode != "EWATCH" || allEntries[1].ErrorCode != "EAGENT" {
		t.Fatalf("unexpected order: %+v", allEntries)
	}
}

func TestQueryEventRecordsRespectsCancellation(t *testing.T) {
	t.Parallel()

	obsDir := t.TempDir()
	writeWideEventsFile(t, obsDir, []WideEvent{{
		Ts:        time.Now().UTC(),
		TraceID:   "trace-1",
		SpanID:    "span-1",
		Service:   "agentctl",
		Component: ComponentCLI,
		Operation: "watch.error",
		Status:    StatusError,
	}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := QueryEventRecords(ctx, EventQueryOptions{
		ObsDir: obsDir,
		Limit:  10,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v want context.Canceled", err)
	}
}

func TestQueryEventRecordsFallsBackWhenTailScanIsPartial(t *testing.T) {
	t.Parallel()

	obsDir := t.TempDir()
	eventsDir := filepath.Join(obsDir, "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	f, err := os.Create(filepath.Join(eventsDir, WideEventFileName+".ndjson"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	target := WideEvent{
		Ts:           time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC),
		TraceID:      "trace-target",
		SpanID:       "span-target",
		Service:      "agentctl",
		Component:    ComponentAgent,
		Operation:    "agent.iteration",
		Status:       StatusError,
		ErrorCode:    "EAGENT",
		ErrorMessage: "target failure",
	}
	if err := enc.Encode(target); err != nil {
		t.Fatalf("Encode(target) error = %v", err)
	}

	padding := strings.Repeat("x", 4096)
	for i := 0; i < 200; i++ {
		event := WideEvent{
			Ts:        time.Date(2026, 3, 20, 9, 0, 0, i, time.UTC),
			TraceID:   "trace-padding",
			SpanID:    "span-padding-" + time.Date(2026, 3, 20, 9, 0, 0, i, time.UTC).Format("150405.000000000"),
			Service:   "agentctl",
			Component: ComponentCLI,
			Operation: "watch.ok",
			Status:    StatusOK,
			Data: map[string]any{
				"padding": padding,
			},
		}
		if err := enc.Encode(event); err != nil {
			t.Fatalf("Encode(padding) error = %v", err)
		}
	}

	entries, err := QueryEventRecords(context.Background(), EventQueryOptions{
		ObsDir:     obsDir,
		Limit:      1,
		ErrorsOnly: true,
	})
	if err != nil {
		t.Fatalf("QueryEventRecords() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
	if entries[0].ErrorCode != "EAGENT" {
		t.Fatalf("error_code=%q want EAGENT", entries[0].ErrorCode)
	}
}

func TestQueryEventRecordsGolden(t *testing.T) {
	t.Parallel()

	obsDir := t.TempDir()
	writeWideEventsFile(t, obsDir, []WideEvent{
		{
			Ts:           time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC),
			TraceID:      "trace-a",
			SpanID:       "span-a",
			Service:      "agentctl",
			Component:    ComponentCLI,
			Operation:    "watch.error",
			Status:       StatusError,
			ErrorCode:    "EWATCH",
			ErrorMessage: "watch failed",
		},
		{
			Ts:        time.Date(2026, 3, 20, 8, 1, 0, 0, time.UTC),
			TraceID:   "trace-b",
			SpanID:    "span-b",
			Service:   "agentctl",
			Component: ComponentAgent,
			Operation: "agent.iteration",
			Status:    StatusOK,
		},
	})

	entries, err := QueryEventRecords(context.Background(), EventQueryOptions{
		ObsDir: obsDir,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("QueryEventRecords() error = %v", err)
	}

	got, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error = %v", err)
	}
	goldenPath := filepath.Join("testdata", "query_events_golden.json")
	updateGoldenFile(t, goldenPath, append(got, '\n'))
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("ReadFile(golden) error = %v", err)
	}
	if !bytes.Equal(append(got, '\n'), want) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", string(want), string(append(got, '\n')))
	}
}

func writeWideEventsFile(t *testing.T, obsDir string, events []WideEvent) {
	t.Helper()

	eventsDir := filepath.Join(obsDir, "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	f, err := os.Create(filepath.Join(eventsDir, WideEventFileName+".ndjson"))
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

func updateGoldenFile(t *testing.T, path string, body []byte) {
	t.Helper()
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, body, 0o644); err != nil {
			t.Fatalf("WriteFile(golden) error = %v", err)
		}
	}
}
