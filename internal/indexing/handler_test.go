package indexing

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
)

// mockIndexer is a test double for the Indexer interface.
type mockIndexer struct {
	id           string
	indexFn      func(ctx context.Context, event PostReviewEvent) (*IndexerResult, error)
	callCount    atomic.Int32
	lastEventMu  sync.Mutex
	lastEvent    PostReviewEvent
	returnErr    error
	returnResult *IndexerResult
}

func newMockIndexer(id string) *mockIndexer {
	return &mockIndexer{
		id: id,
		returnResult: &IndexerResult{
			FilesIndexed: 0,
			FilesSkipped: 0,
			FilesFailed:  0,
		},
	}
}

func (m *mockIndexer) ID() string {
	return m.id
}

func (m *mockIndexer) Index(ctx context.Context, event PostReviewEvent) (*IndexerResult, error) {
	m.callCount.Add(1)
	m.lastEventMu.Lock()
	m.lastEvent = event
	m.lastEventMu.Unlock()

	if m.indexFn != nil {
		return m.indexFn(ctx, event)
	}

	if m.returnErr != nil {
		return nil, m.returnErr
	}

	result := *m.returnResult
	result.FilesIndexed = len(event.Files)
	return &result, nil
}

// LastEvent returns a copy of the last event received, safe for concurrent access.
func (m *mockIndexer) LastEvent() PostReviewEvent {
	m.lastEventMu.Lock()
	defer m.lastEventMu.Unlock()
	return m.lastEvent
}

func TestPostReviewHandler_Handle_Disabled(t *testing.T) {
	cfg := PostReviewConfig{
		Enabled: false,
	}
	handler := NewPostReviewHandler(cfg, zerolog.Nop())

	event := PostReviewEvent{
		WorkspaceID: "ws-1",
		Files: []FileChange{
			{Path: "foo.go", ChangeKind: ChangeKindModified},
		},
	}

	result, err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if !result.Skipped {
		t.Error("expected result to be skipped when disabled")
	}
	if result.Reason != "disabled" {
		t.Errorf("expected reason 'disabled', got %q", result.Reason)
	}
}

func TestPostReviewHandler_Handle_NoFiles(t *testing.T) {
	cfg := PostReviewConfig{
		Enabled: true,
	}
	handler := NewPostReviewHandler(cfg, zerolog.Nop())

	event := PostReviewEvent{
		WorkspaceID: "ws-1",
		Files:       []FileChange{},
	}

	result, err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if !result.Skipped {
		t.Error("expected result to be skipped when no files")
	}
	if result.Reason != "no_files" {
		t.Errorf("expected reason 'no_files', got %q", result.Reason)
	}
}

func TestPostReviewHandler_Handle_NoActiveIndexers(t *testing.T) {
	cfg := PostReviewConfig{
		Enabled:  true,
		Indexers: []IndexerConfig{}, // No indexers configured
	}
	handler := NewPostReviewHandler(cfg, zerolog.Nop())

	event := PostReviewEvent{
		WorkspaceID: "ws-1",
		Files: []FileChange{
			{Path: "foo.go", ChangeKind: ChangeKindModified},
		},
	}

	result, err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if !result.Skipped {
		t.Error("expected result to be skipped when no active indexers")
	}
}

func TestPostReviewHandler_Handle_SingleIndexer(t *testing.T) {
	indexer := newMockIndexer("test_indexer")

	cfg := PostReviewConfig{
		Enabled: true,
		Async:   false, // Synchronous for testing
		Indexers: []IndexerConfig{
			{ID: "test_indexer", Kind: "test", Enabled: true},
		},
	}
	handler := NewPostReviewHandler(cfg, zerolog.Nop())
	if err := handler.RegisterIndexer(indexer); err != nil {
		t.Fatalf("RegisterIndexer failed: %v", err)
	}

	event := PostReviewEvent{
		WorkspaceID: "ws-1",
		TaskID:      "task-123",
		ReviewID:    "review-456",
		Files: []FileChange{
			{Path: "foo.go", ChangeKind: ChangeKindModified},
			{Path: "bar.go", ChangeKind: ChangeKindAdded},
		},
	}

	result, err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if result.Skipped {
		t.Error("expected result not to be skipped")
	}
	if len(result.IndexerResults) != 1 {
		t.Errorf("expected 1 indexer result, got %d", len(result.IndexerResults))
	}
	if result.IndexerResults[0].FilesIndexed != 2 {
		t.Errorf("expected 2 files indexed, got %d", result.IndexerResults[0].FilesIndexed)
	}
	if indexer.callCount.Load() != 1 {
		t.Errorf("expected indexer to be called once, got %d", indexer.callCount.Load())
	}
}

func TestPostReviewHandler_Handle_IndexerError(t *testing.T) {
	indexer := newMockIndexer("failing_indexer")
	indexer.returnErr = errors.New("indexer failed")

	cfg := PostReviewConfig{
		Enabled: true,
		Async:   false,
		Indexers: []IndexerConfig{
			{ID: "failing_indexer", Kind: "test", Enabled: true},
		},
	}
	handler := NewPostReviewHandler(cfg, zerolog.Nop())
	if err := handler.RegisterIndexer(indexer); err != nil {
		t.Fatalf("RegisterIndexer failed: %v", err)
	}

	event := PostReviewEvent{
		WorkspaceID: "ws-1",
		Files: []FileChange{
			{Path: "foo.go", ChangeKind: ChangeKindModified},
		},
	}

	result, err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}
	if len(result.IndexerResults) != 1 {
		t.Errorf("expected 1 indexer result, got %d", len(result.IndexerResults))
	}
	if result.IndexerResults[0].Error != "indexer failed" {
		t.Errorf("expected error message, got %q", result.IndexerResults[0].Error)
	}
	if !result.HasFailures() {
		t.Error("expected HasFailures to return true")
	}
}

func TestPostReviewHandler_Handle_IncludeGlobs(t *testing.T) {
	indexer := newMockIndexer("go_indexer")

	cfg := PostReviewConfig{
		Enabled: true,
		Async:   false,
		Indexers: []IndexerConfig{
			{
				ID:           "go_indexer",
				Kind:         "test",
				Enabled:      true,
				IncludeGlobs: []string{"*.go"},
			},
		},
	}
	handler := NewPostReviewHandler(cfg, zerolog.Nop())
	if err := handler.RegisterIndexer(indexer); err != nil {
		t.Fatalf("RegisterIndexer failed: %v", err)
	}

	event := PostReviewEvent{
		WorkspaceID: "ws-1",
		Files: []FileChange{
			{Path: "foo.go", ChangeKind: ChangeKindModified},
			{Path: "bar.md", ChangeKind: ChangeKindModified},
			{Path: "baz.go", ChangeKind: ChangeKindAdded},
		},
	}

	result, err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Only .go files should be indexed
	if result.IndexerResults[0].FilesIndexed != 2 {
		t.Errorf("expected 2 .go files indexed, got %d", result.IndexerResults[0].FilesIndexed)
	}
}

func TestPostReviewHandler_Handle_ExcludeGlobs(t *testing.T) {
	indexer := newMockIndexer("no_vendor_indexer")

	cfg := PostReviewConfig{
		Enabled: true,
		Async:   false,
		Indexers: []IndexerConfig{
			{
				ID:           "no_vendor_indexer",
				Kind:         "test",
				Enabled:      true,
				ExcludeGlobs: []string{"vendor/*"},
			},
		},
	}
	handler := NewPostReviewHandler(cfg, zerolog.Nop())
	if err := handler.RegisterIndexer(indexer); err != nil {
		t.Fatalf("RegisterIndexer failed: %v", err)
	}

	event := PostReviewEvent{
		WorkspaceID: "ws-1",
		Files: []FileChange{
			{Path: "foo.go", ChangeKind: ChangeKindModified},
			{Path: "vendor/lib.go", ChangeKind: ChangeKindModified},
		},
	}

	result, err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// vendor/lib.go should be excluded
	if result.IndexerResults[0].FilesIndexed != 1 {
		t.Errorf("expected 1 file indexed (vendor excluded), got %d", result.IndexerResults[0].FilesIndexed)
	}
}

func TestPostReviewHandler_Handle_MaxFileKB(t *testing.T) {
	indexer := newMockIndexer("small_files_indexer")

	cfg := PostReviewConfig{
		Enabled: true,
		Async:   false,
		Indexers: []IndexerConfig{
			{
				ID:        "small_files_indexer",
				Kind:      "test",
				Enabled:   true,
				MaxFileKB: 100, // 100KB limit
			},
		},
	}
	handler := NewPostReviewHandler(cfg, zerolog.Nop())
	if err := handler.RegisterIndexer(indexer); err != nil {
		t.Fatalf("RegisterIndexer failed: %v", err)
	}

	event := PostReviewEvent{
		WorkspaceID: "ws-1",
		Files: []FileChange{
			{Path: "small.go", ChangeKind: ChangeKindModified, SizeBytes: 50 * 1024},   // 50KB
			{Path: "large.go", ChangeKind: ChangeKindModified, SizeBytes: 200 * 1024},  // 200KB - excluded
			{Path: "medium.go", ChangeKind: ChangeKindModified, SizeBytes: 100 * 1024}, // 100KB - exactly at limit
		},
	}

	result, err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	// Only small.go and medium.go should be indexed (large.go exceeds 100KB)
	if result.IndexerResults[0].FilesIndexed != 2 {
		t.Errorf("expected 2 files indexed (large excluded), got %d", result.IndexerResults[0].FilesIndexed)
	}
}

func TestPostReviewHandler_Handle_MultipleIndexers(t *testing.T) {
	goIndexer := newMockIndexer("go_indexer")
	mdIndexer := newMockIndexer("md_indexer")

	cfg := PostReviewConfig{
		Enabled: true,
		Async:   false,
		Indexers: []IndexerConfig{
			{ID: "go_indexer", Kind: "test", Enabled: true, IncludeGlobs: []string{"*.go"}},
			{ID: "md_indexer", Kind: "test", Enabled: true, IncludeGlobs: []string{"*.md"}},
		},
	}
	handler := NewPostReviewHandler(cfg, zerolog.Nop())
	if err := handler.RegisterIndexer(goIndexer); err != nil {
		t.Fatalf("RegisterIndexer failed: %v", err)
	}
	if err := handler.RegisterIndexer(mdIndexer); err != nil {
		t.Fatalf("RegisterIndexer failed: %v", err)
	}

	event := PostReviewEvent{
		WorkspaceID: "ws-1",
		Files: []FileChange{
			{Path: "foo.go", ChangeKind: ChangeKindModified},
			{Path: "bar.md", ChangeKind: ChangeKindModified},
			{Path: "baz.go", ChangeKind: ChangeKindAdded},
		},
	}

	result, err := handler.Handle(context.Background(), event)
	if err != nil {
		t.Fatalf("Handle failed: %v", err)
	}

	if len(result.IndexerResults) != 2 {
		t.Errorf("expected 2 indexer results, got %d", len(result.IndexerResults))
	}

	// Find results by indexer ID
	var goResult, mdResult *IndexerResult
	for i := range result.IndexerResults {
		switch result.IndexerResults[i].IndexerID {
		case "go_indexer":
			goResult = &result.IndexerResults[i]
		case "md_indexer":
			mdResult = &result.IndexerResults[i]
		}
	}

	if goResult == nil || goResult.FilesIndexed != 2 {
		t.Errorf("expected go_indexer to index 2 files")
	}
	if mdResult == nil || mdResult.FilesIndexed != 1 {
		t.Errorf("expected md_indexer to index 1 file")
	}
}

func TestPostReviewHandler_RegisterIndexer_Duplicate(t *testing.T) {
	cfg := PostReviewConfig{Enabled: true}
	handler := NewPostReviewHandler(cfg, zerolog.Nop())

	indexer1 := newMockIndexer("same_id")
	indexer2 := newMockIndexer("same_id")

	if err := handler.RegisterIndexer(indexer1); err != nil {
		t.Fatalf("first RegisterIndexer failed: %v", err)
	}
	if err := handler.RegisterIndexer(indexer2); err == nil {
		t.Error("expected error when registering duplicate indexer ID")
	}
}

func TestPostReviewResult_TotalFilesIndexed(t *testing.T) {
	result := &PostReviewResult{
		IndexerResults: []IndexerResult{
			{IndexerID: "a", FilesIndexed: 5},
			{IndexerID: "b", FilesIndexed: 3},
			{IndexerID: "c", FilesIndexed: 0},
		},
	}

	if total := result.TotalFilesIndexed(); total != 8 {
		t.Errorf("expected total 8, got %d", total)
	}
}
