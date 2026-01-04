package sessions_test

import (
	"context"
	"encoding/binary"
	"math"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

func TestOpenAndClose(t *testing.T) {
	store := openTestStore(t)
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestSession_SaveAndGet(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	session := storage.Session{
		ID:            "sess-123",
		WorkspacePath: "/workspace/project",
		ProjectName:   "test-project",
		GitBranch:     "main",
		Summary:       "Test session summary",
		Accomplished:  []string{"Task 1", "Task 2"},
		Decisions:     []string{"Decision 1"},
		Gotchas:       []string{"Gotcha 1"},
		Tags:          []string{"test", "unit"},
		Status:        storage.SessionStatusOK,
	}

	saved, err := store.Save(ctx, session)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.ID != "sess-123" {
		t.Errorf("ID: got %q, want %q", saved.ID, "sess-123")
	}
	if saved.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be set")
	}

	got, err := store.Get(ctx, "sess-123")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Summary != "Test session summary" {
		t.Errorf("summary: got %q, want %q", got.Summary, "Test session summary")
	}
	if len(got.Accomplished) != 2 {
		t.Errorf("accomplished: got %d items, want 2", len(got.Accomplished))
	}
	if got.Status != storage.SessionStatusOK {
		t.Errorf("status: got %q, want %q", got.Status, storage.SessionStatusOK)
	}
}

func TestSession_List(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create sessions in different workspaces
	for i := 0; i < 3; i++ {
		session := storage.Session{
			ID:            "sess-list-" + string(rune('A'+i)),
			WorkspacePath: "/workspace/project1",
			ProjectName:   "project1",
			Summary:       "Session " + string(rune('A'+i)),
			Status:        storage.SessionStatusOK,
		}
		if _, err := store.Save(ctx, session); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	// Create session in different workspace
	otherSession := storage.Session{
		ID:            "sess-other",
		WorkspacePath: "/workspace/other",
		ProjectName:   "other-project",
		Summary:       "Other session",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, otherSession); err != nil {
		t.Fatalf("save other: %v", err)
	}

	// List all sessions
	all, err := store.List(ctx, sessions.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("all: got %d, want 4", len(all))
	}

	// Filter by workspace
	filtered, err := store.List(ctx, sessions.ListOptions{
		WorkspacePath: "/workspace/project1",
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if len(filtered) != 3 {
		t.Errorf("filtered: got %d, want 3", len(filtered))
	}
}

func TestSession_Delete(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	session := storage.Session{
		ID:            "sess-delete",
		WorkspacePath: "/workspace",
		Summary:       "To be deleted",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Verify exists
	got, err := store.Get(ctx, "sess-delete")
	if err != nil {
		t.Fatalf("get before delete: %v", err)
	}
	if got.ID != "sess-delete" {
		t.Fatalf("unexpected session: %v", got)
	}

	// Delete
	if err := store.Delete(ctx, "sess-delete"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify gone
	_, err = store.Get(ctx, "sess-delete")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

// --- Context Window Tests ---

func TestContextWindow_SaveAndGet(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create parent session first
	session := storage.Session{
		ID:            "sess-cw-1",
		WorkspacePath: "/workspace",
		Summary:       "Session with context windows",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	window := storage.ContextWindow{
		ID:               "cw-001",
		SessionID:        "sess-cw-1",
		WindowIndex:      0,
		StartedAt:        time.Now().Add(-1 * time.Hour),
		EndedAt:          time.Now(),
		PreCompactTokens: 50000,
		Trigger:          "auto",
		ChunkStart:       0,
		ChunkEnd:         25,
		MessageCount:     30,
		Summary:          "First context window",
	}

	saved, err := store.SaveContextWindow(ctx, window)
	if err != nil {
		t.Fatalf("save window: %v", err)
	}
	if saved.ID != "cw-001" {
		t.Errorf("ID: got %q, want %q", saved.ID, "cw-001")
	}

	got, err := store.GetContextWindow(ctx, "sess-cw-1", 0)
	if err != nil {
		t.Fatalf("get window: %v", err)
	}
	if got.Summary != "First context window" {
		t.Errorf("summary: got %q, want %q", got.Summary, "First context window")
	}
	if got.PreCompactTokens != 50000 {
		t.Errorf("tokens: got %d, want 50000", got.PreCompactTokens)
	}
	if got.Trigger != "auto" {
		t.Errorf("trigger: got %q, want %q", got.Trigger, "auto")
	}
}

func TestContextWindow_SaveBatch(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create parent session
	session := storage.Session{
		ID:            "sess-cw-batch",
		WorkspacePath: "/workspace",
		Summary:       "Session with multiple windows",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	now := time.Now()
	windows := []storage.ContextWindow{
		{
			ID:               "cw-batch-0",
			SessionID:        "sess-cw-batch",
			WindowIndex:      0,
			StartedAt:        now.Add(-3 * time.Hour),
			EndedAt:          now.Add(-2 * time.Hour),
			PreCompactTokens: 100000,
			Trigger:          "auto",
			MessageCount:     50,
		},
		{
			ID:               "cw-batch-1",
			SessionID:        "sess-cw-batch",
			WindowIndex:      1,
			StartedAt:        now.Add(-2 * time.Hour),
			EndedAt:          now.Add(-1 * time.Hour),
			PreCompactTokens: 120000,
			Trigger:          "manual",
			MessageCount:     40,
		},
		{
			ID:               "cw-batch-2",
			SessionID:        "sess-cw-batch",
			WindowIndex:      2,
			StartedAt:        now.Add(-1 * time.Hour),
			EndedAt:          now,
			PreCompactTokens: 80000,
			Trigger:          "auto",
			MessageCount:     35,
		},
	}

	if err := store.SaveContextWindows(ctx, windows); err != nil {
		t.Fatalf("save batch: %v", err)
	}

	got, err := store.GetContextWindows(ctx, "sess-cw-batch")
	if err != nil {
		t.Fatalf("get windows: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 windows, got %d", len(got))
	}

	// Verify order (by window_index)
	for i, w := range got {
		if w.WindowIndex != i {
			t.Errorf("window %d has index %d", i, w.WindowIndex)
		}
	}
}

func TestContextWindow_GetList(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create session
	session := storage.Session{
		ID:            "sess-cw-list",
		WorkspacePath: "/workspace",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	// Save windows
	for i := 0; i < 5; i++ {
		window := storage.ContextWindow{
			ID:          "cw-list-" + string(rune('0'+i)),
			SessionID:   "sess-cw-list",
			WindowIndex: i,
			Summary:     "Window " + string(rune('A'+i)),
		}
		if _, err := store.SaveContextWindow(ctx, window); err != nil {
			t.Fatalf("save window %d: %v", i, err)
		}
	}

	windows, err := store.GetContextWindows(ctx, "sess-cw-list")
	if err != nil {
		t.Fatalf("get windows: %v", err)
	}
	if len(windows) != 5 {
		t.Errorf("windows: got %d, want 5", len(windows))
	}
}

func TestContextWindow_UpdateSummary(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create session and window
	session := storage.Session{
		ID:            "sess-cw-update",
		WorkspacePath: "/workspace",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	window := storage.ContextWindow{
		ID:          "cw-update",
		SessionID:   "sess-cw-update",
		WindowIndex: 0,
		Summary:     "Initial summary",
	}
	if _, err := store.SaveContextWindow(ctx, window); err != nil {
		t.Fatalf("save window: %v", err)
	}

	// Create test embedding (small for test)
	embedding := serializeEmbedding([]float32{0.1, 0.2, 0.3, 0.4})

	if err := store.UpdateWindowSummary(ctx, "cw-update", "Updated summary", embedding, "voyage-3.5"); err != nil {
		t.Fatalf("update summary: %v", err)
	}

	got, err := store.GetContextWindow(ctx, "sess-cw-update", 0)
	if err != nil {
		t.Fatalf("get window: %v", err)
	}
	if got.Summary != "Updated summary" {
		t.Errorf("summary: got %q, want %q", got.Summary, "Updated summary")
	}
	if got.EmbeddingModel != "voyage-3.5" {
		t.Errorf("embedding_model: got %q, want %q", got.EmbeddingModel, "voyage-3.5")
	}
}

func TestContextWindow_Delete(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create session with windows
	session := storage.Session{
		ID:            "sess-cw-delete",
		WorkspacePath: "/workspace",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	for i := 0; i < 3; i++ {
		window := storage.ContextWindow{
			ID:          "cw-del-" + string(rune('0'+i)),
			SessionID:   "sess-cw-delete",
			WindowIndex: i,
		}
		if _, err := store.SaveContextWindow(ctx, window); err != nil {
			t.Fatalf("save window %d: %v", i, err)
		}
	}

	// Verify windows exist
	before, err := store.GetContextWindows(ctx, "sess-cw-delete")
	if err != nil {
		t.Fatalf("get before: %v", err)
	}
	if len(before) != 3 {
		t.Fatalf("before: got %d, want 3", len(before))
	}

	// Delete all windows for session
	if err := store.DeleteContextWindows(ctx, "sess-cw-delete"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	// Verify deleted
	after, err := store.GetContextWindows(ctx, "sess-cw-delete")
	if err != nil {
		t.Fatalf("get after: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("after: got %d, want 0", len(after))
	}
}

func TestContextWindow_CascadeDeleteWithSession(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create session with windows
	session := storage.Session{
		ID:            "sess-cw-cascade",
		WorkspacePath: "/workspace",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	window := storage.ContextWindow{
		ID:          "cw-cascade",
		SessionID:   "sess-cw-cascade",
		WindowIndex: 0,
	}
	if _, err := store.SaveContextWindow(ctx, window); err != nil {
		t.Fatalf("save window: %v", err)
	}

	// Delete session
	if err := store.Delete(ctx, "sess-cw-cascade"); err != nil {
		t.Fatalf("delete session: %v", err)
	}

	// Verify windows are also deleted (cascade)
	windows, err := store.GetContextWindows(ctx, "sess-cw-cascade")
	if err != nil {
		t.Fatalf("get windows: %v", err)
	}
	if len(windows) != 0 {
		t.Errorf("windows after cascade: got %d, want 0", len(windows))
	}
}

// --- Semantic Search Tests ---

func TestContextWindow_SearchSimilar(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create session
	session := storage.Session{
		ID:            "sess-search",
		WorkspacePath: "/workspace",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	// Create windows with different embeddings
	// Window 0: vector pointing in +x direction
	vec0 := normalizeVector([]float32{1, 0, 0, 0})
	// Window 1: vector pointing in +y direction
	vec1 := normalizeVector([]float32{0, 1, 0, 0})
	// Window 2: vector between +x and +y (should match query best)
	vec2 := normalizeVector([]float32{0.7, 0.7, 0, 0})

	windows := []storage.ContextWindow{
		{
			ID:          "cw-search-0",
			SessionID:   "sess-search",
			WindowIndex: 0,
			Summary:     "Window about topic X",
			Embedding:   serializeEmbedding(vec0),
		},
		{
			ID:          "cw-search-1",
			SessionID:   "sess-search",
			WindowIndex: 1,
			Summary:     "Window about topic Y",
			Embedding:   serializeEmbedding(vec1),
		},
		{
			ID:          "cw-search-2",
			SessionID:   "sess-search",
			WindowIndex: 2,
			Summary:     "Window about topics X and Y",
			Embedding:   serializeEmbedding(vec2),
		},
	}

	if err := store.SaveContextWindows(ctx, windows); err != nil {
		t.Fatalf("save windows: %v", err)
	}

	// Query with vector pointing toward X+Y (similar to window 2)
	queryVec := normalizeVector([]float32{0.6, 0.8, 0, 0})

	results, err := store.SearchContextWindows(ctx, queryVec, 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results: got %d, want 3", len(results))
	}

	// Window 2 should have highest similarity (closest to query)
	if results[0].Window.WindowIndex != 2 {
		t.Errorf("expected window 2 first, got window %d (similarity: %.4f)",
			results[0].Window.WindowIndex, results[0].Similarity)
	}

	// Verify similarities are in descending order
	for i := 1; i < len(results); i++ {
		if results[i].Similarity > results[i-1].Similarity {
			t.Errorf("results not sorted: %d (%.4f) > %d (%.4f)",
				i, results[i].Similarity, i-1, results[i-1].Similarity)
		}
	}

	// All similarities should be positive (vectors in same general direction)
	for i, r := range results {
		if r.Similarity < 0 {
			t.Errorf("result %d has negative similarity: %.4f", i, r.Similarity)
		}
	}
}

func TestSession_SearchSimilar(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create sessions with embeddings
	vec1 := normalizeVector([]float32{1, 0, 0, 0})
	vec2 := normalizeVector([]float32{0, 1, 0, 0})
	vec3 := normalizeVector([]float32{0.5, 0.5, 0, 0})

	sessionsData := []storage.Session{
		{
			ID:             "sess-sim-1",
			WorkspacePath:  "/workspace",
			Summary:        "Session about authentication",
			Embedding:      serializeEmbedding(vec1),
			EmbeddingModel: "voyage-3.5",
			Status:         storage.SessionStatusOK,
		},
		{
			ID:             "sess-sim-2",
			WorkspacePath:  "/workspace",
			Summary:        "Session about database",
			Embedding:      serializeEmbedding(vec2),
			EmbeddingModel: "voyage-3.5",
			Status:         storage.SessionStatusOK,
		},
		{
			ID:             "sess-sim-3",
			WorkspacePath:  "/workspace",
			Summary:        "Session about auth and db",
			Embedding:      serializeEmbedding(vec3),
			EmbeddingModel: "voyage-3.5",
			Status:         storage.SessionStatusOK,
		},
	}

	for _, s := range sessionsData {
		if _, err := store.Save(ctx, s); err != nil {
			t.Fatalf("save session %s: %v", s.ID, err)
		}
	}

	// Search with query similar to session 3
	queryVec := normalizeVector([]float32{0.4, 0.6, 0, 0})

	results, err := store.SearchSimilar(ctx, queryVec, 10)
	if err != nil {
		t.Fatalf("search similar: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("results: got %d, want 3", len(results))
	}

	// Session 3 should be most similar
	if results[0].Session.ID != "sess-sim-3" {
		t.Errorf("expected sess-sim-3 first, got %s (similarity: %.4f)",
			results[0].Session.ID, results[0].Similarity)
	}

	// Verify sorted by similarity
	for i := 1; i < len(results); i++ {
		if results[i].Similarity > results[i-1].Similarity {
			t.Errorf("not sorted: result %d (%.4f) > result %d (%.4f)",
				i, results[i].Similarity, i-1, results[i-1].Similarity)
		}
	}
}

func TestSearchWithNoEmbeddings(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create session without embedding
	session := storage.Session{
		ID:            "sess-no-embed",
		WorkspacePath: "/workspace",
		Summary:       "Session without embedding",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save session: %v", err)
	}

	// Create window without embedding
	window := storage.ContextWindow{
		ID:          "cw-no-embed",
		SessionID:   "sess-no-embed",
		WindowIndex: 0,
		Summary:     "Window without embedding",
	}
	if _, err := store.SaveContextWindow(ctx, window); err != nil {
		t.Fatalf("save window: %v", err)
	}

	// Search should return empty (no matching embeddings)
	queryVec := []float32{0.5, 0.5, 0, 0}

	sessionResults, err := store.SearchSimilar(ctx, queryVec, 10)
	if err != nil {
		t.Fatalf("search sessions: %v", err)
	}
	if len(sessionResults) != 0 {
		t.Errorf("session results: got %d, want 0", len(sessionResults))
	}

	windowResults, err := store.SearchContextWindows(ctx, queryVec, 10)
	if err != nil {
		t.Fatalf("search windows: %v", err)
	}
	if len(windowResults) != 0 {
		t.Errorf("window results: got %d, want 0", len(windowResults))
	}
}

// --- Lineage Tests ---

func TestSession_GetActive(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Create running session
	running := storage.Session{
		ID:            "sess-running",
		WorkspacePath: "/workspace",
		AgentID:       "agentctl",
		Status:        storage.SessionStatusRunning,
	}
	if _, err := store.Save(ctx, running); err != nil {
		t.Fatalf("save running: %v", err)
	}

	// Create completed session
	completed := storage.Session{
		ID:            "sess-completed",
		WorkspacePath: "/workspace",
		AgentID:       "agentctl",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, completed); err != nil {
		t.Fatalf("save completed: %v", err)
	}

	// GetActive should return running session
	active, err := store.GetActive(ctx, "/workspace", "agentctl")
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active == nil {
		t.Fatal("expected active session, got nil")
	}
	if active.ID != "sess-running" {
		t.Errorf("active ID: got %q, want %q", active.ID, "sess-running")
	}
}

func TestSession_SetStatus(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	session := storage.Session{
		ID:            "sess-status",
		WorkspacePath: "/workspace",
		Status:        storage.SessionStatusRunning,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := store.SetStatus(ctx, "sess-status", storage.SessionStatusOK); err != nil {
		t.Fatalf("set status: %v", err)
	}

	got, err := store.Get(ctx, "sess-status")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != storage.SessionStatusOK {
		t.Errorf("status: got %q, want %q", got.Status, storage.SessionStatusOK)
	}
}

func TestSession_Stats(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// Initial stats
	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Count != 0 {
		t.Errorf("initial count: got %d, want 0", stats.Count)
	}

	// Add sessions
	for i := 0; i < 5; i++ {
		session := storage.Session{
			ID:            "sess-stats-" + string(rune('A'+i)),
			WorkspacePath: "/workspace",
			Status:        storage.SessionStatusOK,
		}
		if _, err := store.Save(ctx, session); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}

	// Check updated stats
	stats, err = store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats after: %v", err)
	}
	if stats.Count != 5 {
		t.Errorf("count: got %d, want 5", stats.Count)
	}
}

func TestListReturnsEmptyResults(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	defer store.Close()

	// List should work correctly with no sessions (nil or empty slice is acceptable)
	sessions, err := store.List(ctx, sessions.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}

	// Create session for window test
	session := storage.Session{
		ID:            "sess-empty-test",
		WorkspacePath: "/workspace",
		Status:        storage.SessionStatusOK,
	}
	if _, err := store.Save(ctx, session); err != nil {
		t.Fatalf("save: %v", err)
	}

	// GetContextWindows for session with no windows (nil or empty is acceptable)
	windows, err := store.GetContextWindows(ctx, "sess-empty-test")
	if err != nil {
		t.Fatalf("get windows: %v", err)
	}
	if len(windows) != 0 {
		t.Errorf("expected 0 windows, got %d", len(windows))
	}

	// Search similar should also work with no embeddings
	queryVec := []float32{0.5, 0.5, 0, 0}
	results, err := store.SearchSimilar(ctx, queryVec, 10)
	if err != nil {
		t.Fatalf("search similar: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// --- Helper Functions ---

func openTestStore(t *testing.T) *sessions.Store {
	t.Helper()
	store, err := sessions.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	return store
}

// serializeEmbedding converts a float32 slice to bytes (little endian).
func serializeEmbedding(v []float32) []byte {
	buf := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(f))
	}
	return buf
}

// normalizeVector returns a unit vector.
func normalizeVector(v []float32) []float32 {
	var norm float64
	for _, f := range v {
		norm += float64(f) * float64(f)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return v
	}
	result := make([]float32, len(v))
	for i, f := range v {
		result[i] = float32(float64(f) / norm)
	}
	return result
}
