package observability

import (
	"context"
	"testing"
	"time"
)

func TestDeleteEventRecordsDeletesMatchingErrors(t *testing.T) {
	t.Parallel()

	obsDir := t.TempDir()
	writeEventsFile(t, obsDir, []Event{
		testEvent(time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC), "web.smoke.failed", StatusError, testTrace("trace-smoke"), testSession("session-smoke"), testComponent(ComponentWeb), testError("", "gui smoke failure"), testData("label", "smoke")),
		testEvent(time.Date(2026, 3, 20, 9, 0, 0, 0, time.UTC), "agent.iteration", StatusError, testTrace("trace-keep"), testSession("session-keep"), testComponent(ComponentAgent), testError("", "keep me")),
		testEvent(time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC), "agent.iteration", StatusOK, testTrace("trace-ok"), testSession("session-ok"), testComponent(ComponentAgent)),
	})

	result, err := DeleteEventRecords(context.Background(), DeleteEventOptions{
		ObsDir:     obsDir,
		ErrorsOnly: true,
		TextQuery:  "smoke",
	})
	if err != nil {
		t.Fatalf("DeleteEventRecords() error = %v", err)
	}
	if result.EventsDeleted != 1 {
		t.Fatalf("deleted=%d want 1", result.EventsDeleted)
	}

	entries, err := QueryEventRecords(context.Background(), EventQueryOptions{
		ObsDir: obsDir,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("QueryEventRecords() error = %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("len(entries)=%d want 2", len(entries))
	}
	for _, entry := range entries {
		if entry.TraceID == "trace-smoke" {
			t.Fatal("smoke trace should have been deleted")
		}
	}
}

func TestDeleteEventRecordsMatchesFocusedTraceAndSession(t *testing.T) {
	t.Parallel()

	obsDir := t.TempDir()
	writeEventsFile(t, obsDir, []Event{
		testEvent(time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC), "agent.iteration", StatusError, testTrace("trace-focus"), testSession("session-a")),
		testEvent(time.Date(2026, 3, 20, 8, 1, 0, 0, time.UTC), "agent.tool", StatusError, testTrace("trace-focus"), testSession("session-a")),
		testEvent(time.Date(2026, 3, 20, 8, 2, 0, 0, time.UTC), "agent.iteration", StatusError, testTrace("trace-other"), testSession("session-b")),
	})

	result, err := DeleteEventRecords(context.Background(), DeleteEventOptions{
		ObsDir:     obsDir,
		ErrorsOnly: true,
		SessionID:  "session-a",
		TraceIDs:   []string{"trace-focus"},
	})
	if err != nil {
		t.Fatalf("DeleteEventRecords() error = %v", err)
	}
	if result.EventsDeleted != 2 {
		t.Fatalf("deleted=%d want 2", result.EventsDeleted)
	}

	entries, err := QueryEventRecords(context.Background(), EventQueryOptions{
		ObsDir:     obsDir,
		Limit:      10,
		ErrorsOnly: true,
	})
	if err != nil {
		t.Fatalf("QueryEventRecords() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
	if entries[0].TraceID != "trace-other" {
		t.Fatalf("remaining trace=%q want trace-other", entries[0].TraceID)
	}
}

func TestDeleteEventRecordsDryRunLeavesFileUntouched(t *testing.T) {
	t.Parallel()

	obsDir := t.TempDir()
	writeEventsFile(t, obsDir, []Event{
		testEvent(time.Date(2026, 3, 20, 8, 0, 0, 0, time.UTC), "web.error", StatusError, testTrace("trace-dry-run"), testError("", "dry run target")),
	})

	result, err := DeleteEventRecords(context.Background(), DeleteEventOptions{
		ObsDir:     obsDir,
		ErrorsOnly: true,
		TextQuery:  "dry run",
		DryRun:     true,
	})
	if err != nil {
		t.Fatalf("DeleteEventRecords() error = %v", err)
	}
	if result.EventsDeleted != 1 {
		t.Fatalf("deleted=%d want 1", result.EventsDeleted)
	}

	entries, err := QueryEventRecords(context.Background(), EventQueryOptions{
		ObsDir: obsDir,
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("QueryEventRecords() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries)=%d want 1", len(entries))
	}
}
