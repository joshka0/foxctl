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
	writeEventsFile(t, obsDir, []Event{
		testEvent(time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC), "watch.start", StatusOK, testTrace("trace-1"), testSpan("span-1"), testComponent(ComponentCLI), testWorkspace("ws-a")),
		testEvent(time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC), "agent.iteration", StatusError, testTrace("trace-2"), testSpan("span-2"), testComponent(ComponentAgent), testWorkspace("ws-a"), testError("EAGENT", "agent failure")),
		testEvent(time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC), "watch.error", StatusError, testTrace("trace-3"), testSpan("span-3"), testComponent(ComponentCLI), testWorkspace("ws-b"), testError("EWATCH", "watch failure")),
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
	writeEventsFile(t, obsDir, []Event{{
		Timestamp: time.Now().UTC(),
		TraceID:   "trace-1",
		SpanID:    "span-1",
		Operation: "watch.error",
		Status:    StatusError,
		Data: map[string]any{
			"service":   "foxctl",
			"component": ComponentCLI,
		},
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

	f, err := os.Create(filepath.Join(eventsDir, EventFileName+".ndjson"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	target := testEvent(time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC), "agent.iteration", StatusError, testTrace("trace-target"), testSpan("span-target"), testComponent(ComponentAgent), testError("EAGENT", "target failure"))
	if err := enc.Encode(target); err != nil {
		t.Fatalf("Encode(target) error = %v", err)
	}

	padding := strings.Repeat("x", 4096)
	for i := 0; i < 200; i++ {
		event := testEvent(
			time.Date(2026, 3, 20, 9, 0, 0, i, time.UTC),
			"watch.ok",
			StatusOK,
			testTrace("trace-padding"),
			testSpan("span-padding-"+time.Date(2026, 3, 20, 9, 0, 0, i, time.UTC).Format("150405.000000000")),
			testComponent(ComponentCLI),
			testData("padding", padding),
		)
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
	writeEventsFile(t, obsDir, []Event{
		testEvent(time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC), "watch.error", StatusError, testTrace("trace-a"), testSpan("span-a"), testComponent(ComponentCLI), testError("EWATCH", "watch failed")),
		testEvent(time.Date(2026, 3, 20, 8, 1, 0, 0, time.UTC), "agent.iteration", StatusOK, testTrace("trace-b"), testSpan("span-b"), testComponent(ComponentAgent)),
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

func writeEventsFile(t *testing.T, obsDir string, events []Event) {
	t.Helper()

	eventsDir := filepath.Join(obsDir, "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	f, err := os.Create(filepath.Join(eventsDir, EventFileName+".ndjson"))
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
