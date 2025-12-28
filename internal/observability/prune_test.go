package observability

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestEvents(t *testing.T, dir string, events []WideEvent) {
	t.Helper()
	eventsDir := filepath.Join(dir, "events")
	if err := os.MkdirAll(eventsDir, 0755); err != nil {
		t.Fatalf("create events dir: %v", err)
	}

	f, err := os.Create(filepath.Join(eventsDir, "wide_events.ndjson"))
	if err != nil {
		t.Fatalf("create events file: %v", err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("encode event: %v", err)
		}
	}
}

func TestPrune_ByAge(t *testing.T) {
	obsDir := t.TempDir()

	now := time.Now()
	events := []WideEvent{
		{Ts: now.Add(-40 * 24 * time.Hour), Operation: "old1", Status: StatusOK},
		{Ts: now.Add(-35 * 24 * time.Hour), Operation: "old2", Status: StatusOK},
		{Ts: now.Add(-10 * 24 * time.Hour), Operation: "recent1", Status: StatusOK},
		{Ts: now.Add(-5 * 24 * time.Hour), Operation: "recent2", Status: StatusOK},
		{Ts: now.Add(-1 * 24 * time.Hour), Operation: "recent3", Status: StatusOK},
	}
	writeTestEvents(t, obsDir, events)

	ctx := context.Background()
	opts := PruneOptions{
		OlderThan: 30 * 24 * time.Hour, // 30 days
		DryRun:    false,
	}

	result, err := Prune(ctx, obsDir, opts)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.EventsPruned != 2 {
		t.Errorf("EventsPruned = %d, want 2", result.EventsPruned)
	}
	if result.EventsKept != 3 {
		t.Errorf("EventsKept = %d, want 3", result.EventsKept)
	}
	if result.BytesFreed <= 0 {
		t.Error("BytesFreed should be positive")
	}

	// Verify the file contents
	eventsFile := filepath.Join(obsDir, "events", "wide_events.ndjson")
	remaining := readEventsFromFile(t, eventsFile)
	if len(remaining) != 3 {
		t.Errorf("remaining events = %d, want 3", len(remaining))
	}

	// Check that recent events are kept
	for _, e := range remaining {
		if e.Operation == "old1" || e.Operation == "old2" {
			t.Errorf("old event %q should have been pruned", e.Operation)
		}
	}
}

func TestPrune_DryRun(t *testing.T) {
	obsDir := t.TempDir()

	now := time.Now()
	events := []WideEvent{
		{Ts: now.Add(-40 * 24 * time.Hour), Operation: "old1", Status: StatusOK},
		{Ts: now.Add(-1 * 24 * time.Hour), Operation: "recent1", Status: StatusOK},
	}
	writeTestEvents(t, obsDir, events)

	ctx := context.Background()
	opts := PruneOptions{
		OlderThan: 30 * 24 * time.Hour,
		DryRun:    true,
	}

	result, err := Prune(ctx, obsDir, opts)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.EventsPruned != 1 {
		t.Errorf("EventsPruned = %d, want 1", result.EventsPruned)
	}

	// File should be unchanged in dry run
	eventsFile := filepath.Join(obsDir, "events", "wide_events.ndjson")
	remaining := readEventsFromFile(t, eventsFile)
	if len(remaining) != 2 {
		t.Errorf("remaining events = %d, want 2 (dry run should not modify)", len(remaining))
	}
}

func TestPrune_EmptyDir(t *testing.T) {
	obsDir := t.TempDir()

	ctx := context.Background()
	opts := DefaultPruneOptions()

	result, err := Prune(ctx, obsDir, opts)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.EventsPruned != 0 {
		t.Errorf("EventsPruned = %d, want 0", result.EventsPruned)
	}
	if result.FilesProcessed != 0 {
		t.Errorf("FilesProcessed = %d, want 0", result.FilesProcessed)
	}
}

func TestPrune_NothingToPrune(t *testing.T) {
	obsDir := t.TempDir()

	now := time.Now()
	events := []WideEvent{
		{Ts: now.Add(-1 * 24 * time.Hour), Operation: "recent1", Status: StatusOK},
		{Ts: now.Add(-2 * 24 * time.Hour), Operation: "recent2", Status: StatusOK},
	}
	writeTestEvents(t, obsDir, events)

	ctx := context.Background()
	opts := PruneOptions{
		OlderThan: 30 * 24 * time.Hour,
		DryRun:    false,
	}

	result, err := Prune(ctx, obsDir, opts)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}

	if result.EventsPruned != 0 {
		t.Errorf("EventsPruned = %d, want 0", result.EventsPruned)
	}
	if result.EventsKept != 2 {
		t.Errorf("EventsKept = %d, want 2", result.EventsKept)
	}
}

func TestPruneBySize(t *testing.T) {
	obsDir := t.TempDir()

	now := time.Now()
	// Create events with known sizes
	events := []WideEvent{
		{Ts: now.Add(-5 * 24 * time.Hour), Operation: "oldest", Status: StatusOK},
		{Ts: now.Add(-4 * 24 * time.Hour), Operation: "old", Status: StatusOK},
		{Ts: now.Add(-3 * 24 * time.Hour), Operation: "middle", Status: StatusOK},
		{Ts: now.Add(-2 * 24 * time.Hour), Operation: "recent", Status: StatusOK},
		{Ts: now.Add(-1 * 24 * time.Hour), Operation: "newest", Status: StatusOK},
	}
	writeTestEvents(t, obsDir, events)

	// Get file size
	eventsFile := filepath.Join(obsDir, "events", "wide_events.ndjson")
	info, err := os.Stat(eventsFile)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	// Prune to keep ~60% of the data (should remove oldest events)
	maxBytes := info.Size() * 60 / 100

	ctx := context.Background()
	result, err := PruneBySize(ctx, obsDir, maxBytes, false)
	if err != nil {
		t.Fatalf("prune by size: %v", err)
	}

	if result.EventsPruned == 0 {
		t.Error("expected some events to be pruned")
	}
	if result.EventsKept == 0 {
		t.Error("expected some events to be kept")
	}

	// Verify oldest events were removed
	remaining := readEventsFromFile(t, eventsFile)
	for _, e := range remaining {
		if e.Operation == "oldest" {
			t.Error("oldest event should have been pruned")
		}
	}
}

func TestPruneBySize_UnderLimit(t *testing.T) {
	obsDir := t.TempDir()

	now := time.Now()
	events := []WideEvent{
		{Ts: now, Operation: "test", Status: StatusOK},
	}
	writeTestEvents(t, obsDir, events)

	ctx := context.Background()
	// Set limit much higher than file size
	result, err := PruneBySize(ctx, obsDir, 1024*1024, false) // 1MB
	if err != nil {
		t.Fatalf("prune by size: %v", err)
	}

	if result.EventsPruned != 0 {
		t.Errorf("EventsPruned = %d, want 0 (under limit)", result.EventsPruned)
	}
	if result.EventsKept != 1 {
		t.Errorf("EventsKept = %d, want 1", result.EventsKept)
	}
}

func readEventsFromFile(t *testing.T, path string) []WideEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	var events []WideEvent
	dec := json.NewDecoder(f)
	for dec.More() {
		var e WideEvent
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("decode: %v", err)
		}
		events = append(events, e)
	}
	return events
}

func TestDefaultPruneOptions(t *testing.T) {
	opts := DefaultPruneOptions()

	if opts.OlderThan != 30*24*time.Hour {
		t.Errorf("OlderThan = %v, want 30 days", opts.OlderThan)
	}
	if opts.MaxSizeBytes != 0 {
		t.Errorf("MaxSizeBytes = %d, want 0", opts.MaxSizeBytes)
	}
	if opts.DryRun {
		t.Error("DryRun should default to false")
	}
}
