package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
	_ "modernc.org/sqlite"
)

func TestRLMToolExecutor_Put(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	exec := NewRLMToolExecutor(store, "test-conv-1")

	// Test basic put
	args := `{"key": "user_name", "value": "Alice"}`
	result, err := exec.Execute(ctx, "rlm_context_put", json.RawMessage(args))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["success"] != true {
		t.Errorf("Expected success=true, got %v", resp["success"])
	}
	if resp["key"] != "user_name" {
		t.Errorf("Expected key='user_name', got %v", resp["key"])
	}
	if resp["scope"] != "conversation" {
		t.Errorf("Expected scope='conversation', got %v", resp["scope"])
	}
}

func TestRLMToolExecutor_PutWithScope(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	exec := NewRLMToolExecutor(store, "test-conv-2")

	tests := []struct {
		name     string
		scope    string
		expected string
	}{
		{"global scope", "global", "global"},
		{"conversation scope", "conversation", "conversation"},
		{"turn scope", "turn", "turn"},
		{"default scope", "", "conversation"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := map[string]interface{}{
				"key":   "test_key_" + tc.expected,
				"value": "test_value",
			}
			if tc.scope != "" {
				args["scope"] = tc.scope
			}

			argsJSON, _ := json.Marshal(args)
			result, err := exec.Execute(ctx, "rlm_context_put", argsJSON)
			if err != nil {
				t.Fatalf("Put failed: %v", err)
			}

			var resp map[string]interface{}
			if err := json.Unmarshal([]byte(result), &resp); err != nil {
				t.Fatalf("Failed to parse response: %v", err)
			}

			if resp["scope"] != tc.expected {
				t.Errorf("Expected scope=%q, got %v", tc.expected, resp["scope"])
			}
		})
	}
}

func TestRLMToolExecutor_PutWithTTL(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	exec := NewRLMToolExecutor(store, "test-conv-ttl")

	args := `{"key": "ephemeral", "value": "short-lived", "ttl_seconds": 60}`
	result, err := exec.Execute(ctx, "rlm_context_put", json.RawMessage(args))
	if err != nil {
		t.Fatalf("Put with TTL failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["success"] != true {
		t.Errorf("Expected success=true")
	}
}

func TestRLMToolExecutor_Query(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	exec := NewRLMToolExecutor(store, "test-conv-query")

	// First, store some context
	_, _ = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "user_name", "value": "Bob"}`))
	_, _ = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "preferences/theme", "value": "dark"}`))
	_, _ = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "preferences/lang", "value": "en"}`))

	// Query by exact key
	result, err := exec.Execute(ctx, "rlm_context_query", json.RawMessage(`{"key": "user_name"}`))
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["found"] != true {
		t.Errorf("Expected found=true")
	}

	vars := resp["variables"].([]interface{})
	if len(vars) != 1 {
		t.Errorf("Expected 1 variable, got %d", len(vars))
	}
}

func TestRLMToolExecutor_QueryPattern(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	exec := NewRLMToolExecutor(store, "test-conv-pattern")

	// Store some preferences
	_, _ = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "pref/theme", "value": "dark"}`))
	_, _ = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "pref/lang", "value": "en"}`))
	_, _ = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "pref/tz", "value": "UTC"}`))
	_, _ = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "other_key", "value": "ignored"}`))

	// Query by pattern
	result, err := exec.Execute(ctx, "rlm_context_query", json.RawMessage(`{"key_pattern": "pref/*"}`))
	if err != nil {
		t.Fatalf("Query pattern failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	count := int(resp["count"].(float64))
	if count != 3 {
		t.Errorf("Expected 3 results for 'pref/*', got %d", count)
	}
}

func TestRLMToolExecutor_QueryNotFound(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	exec := NewRLMToolExecutor(store, "test-conv-notfound")

	result, err := exec.Execute(ctx, "rlm_context_query", json.RawMessage(`{"key": "nonexistent"}`))
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if resp["found"] != false {
		t.Errorf("Expected found=false for nonexistent key")
	}
}

func TestRLMToolExecutor_List(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	exec := NewRLMToolExecutor(store, "test-conv-list")

	// Store some keys
	_, _ = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "alpha", "value": 1}`))
	_, _ = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "beta", "value": 2}`))
	_, _ = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "gamma", "value": 3}`))

	// List all keys
	result, err := exec.Execute(ctx, "rlm_context_list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(result), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	count := int(resp["total_count"].(float64))
	if count != 3 {
		t.Errorf("Expected 3 keys, got %d", count)
	}
}

func TestRLMToolExecutor_QueryCount(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	exec := NewRLMToolExecutor(store, "test-conv-count")

	// Initially 0
	if exec.QueryCount() != 0 {
		t.Errorf("Expected initial query count 0, got %d", exec.QueryCount())
	}

	// Query increments count
	_, _ = exec.Execute(ctx, "rlm_context_query", json.RawMessage(`{"key": "anything"}`))
	if exec.QueryCount() != 1 {
		t.Errorf("Expected query count 1 after query, got %d", exec.QueryCount())
	}

	// Multiple queries
	_, _ = exec.Execute(ctx, "rlm_context_query", json.RawMessage(`{"key": "another"}`))
	_, _ = exec.Execute(ctx, "rlm_context_query", json.RawMessage(`{"key_pattern": "pref/*"}`))
	if exec.QueryCount() != 3 {
		t.Errorf("Expected query count 3, got %d", exec.QueryCount())
	}

	// Reset
	exec.ResetQueryCount()
	if exec.QueryCount() != 0 {
		t.Errorf("Expected query count 0 after reset, got %d", exec.QueryCount())
	}
}

func TestRLMToolExecutor_UnknownTool(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	exec := NewRLMToolExecutor(store, "test-conv")

	_, err := exec.Execute(ctx, "unknown_tool", json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("Expected error for unknown tool")
	}
}

func TestRLMToolExecutor_InvalidInput(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	exec := NewRLMToolExecutor(store, "test-conv")

	// Invalid JSON
	_, err := exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{invalid}`))
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}

	// Missing required field
	_, err = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"value": "no key"}`))
	if err == nil {
		t.Error("Expected error for missing key")
	}

	// Invalid scope
	_, err = exec.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "test", "value": "v", "scope": "invalid"}`))
	if err == nil {
		t.Error("Expected error for invalid scope")
	}
}

func TestRLMToolDefs(t *testing.T) {
	defs := RLMToolDefs()

	if len(defs) != 4 {
		t.Errorf("Expected 4 tool definitions, got %d", len(defs))
	}

	expectedNames := map[string]bool{
		"rlm_context_put":        true,
		"rlm_context_query":      true,
		"rlm_context_list":       true,
		"rlm_personality_adjust": true,
	}

	for _, def := range defs {
		if !expectedNames[def.Name] {
			t.Errorf("Unexpected tool: %s", def.Name)
		}
		if def.Description == "" {
			t.Errorf("Tool %s missing description", def.Name)
		}
		if len(def.Parameters) == 0 {
			t.Errorf("Tool %s missing parameters", def.Name)
		}
	}
}

func TestCompositeToolExecutor(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	rlmExec := NewRLMToolExecutor(store, "test-conv")

	// Create a mock executor for testing composite
	mockExec := &mockToolExecutor{
		tools: []ToolDef{
			{Name: "mock_tool", Description: "A mock tool"},
		},
		executeResult: `{"mock": "result"}`,
	}

	composite := NewCompositeToolExecutor(rlmExec, mockExec)

	// Should list all tools
	tools := composite.List()
	if len(tools) != 5 { // 4 RLM + 1 mock
		t.Errorf("Expected 5 tools, got %d", len(tools))
	}

	// Execute RLM tool through composite
	_, err := composite.Execute(ctx, "rlm_context_put", json.RawMessage(`{"key": "test", "value": "v"}`))
	if err != nil {
		t.Errorf("Execute RLM tool through composite failed: %v", err)
	}

	// Execute mock tool through composite
	result, err := composite.Execute(ctx, "mock_tool", json.RawMessage(`{}`))
	if err != nil {
		t.Errorf("Execute mock tool through composite failed: %v", err)
	}
	if result != `{"mock": "result"}` {
		t.Errorf("Unexpected mock result: %s", result)
	}

	// Unknown tool should error
	_, err = composite.Execute(ctx, "unknown", json.RawMessage(`{}`))
	if err == nil {
		t.Error("Expected error for unknown tool in composite")
	}
}

// mockToolExecutor is a test helper
type mockToolExecutor struct {
	tools         []ToolDef
	executeResult string
}

func (m *mockToolExecutor) Execute(ctx context.Context, name string, args json.RawMessage) (string, error) {
	return m.executeResult, nil
}

func (m *mockToolExecutor) List() []ToolDef {
	return m.tools
}

// setupTestContextStore creates a temporary store for testing
func setupTestContextStore(t *testing.T) contextvar.Store {
	t.Helper()

	dir := t.TempDir()
	store, err := contextvar.Open(context.Background(), dir)
	if err != nil {
		t.Fatalf("Failed to open test store: %v", err)
	}

	return store
}

type fakeEmbedProvider struct {
	vec []float32
}

func (f fakeEmbedProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return append([]float32(nil), f.vec...), nil
}

func (f fakeEmbedProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for range texts {
		out = append(out, append([]float32(nil), f.vec...))
	}
	return out, nil
}

func (f fakeEmbedProvider) Model() string { return "fake-embedder" }

func (f fakeEmbedProvider) Dimensions() int { return len(f.vec) }

var _ semantic.EmbeddingProvider = (*fakeEmbedProvider)(nil)

func setupTestCompanionDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open companion sqlite: %v", err)
	}

	schema := `
		CREATE TABLE companion_memory_mode_state (
			conversation_id TEXT PRIMARY KEY,
			mode TEXT NOT NULL,
			schema_version INTEGER NOT NULL DEFAULT 1,
			last_processed_event INTEGER NOT NULL DEFAULT 0,
			last_soft_event INTEGER NOT NULL DEFAULT 0,
			last_evidence_event INTEGER NOT NULL DEFAULT 0,
			updated_at TEXT NOT NULL
		);
		CREATE TABLE companion_hard_state_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			entry_type TEXT NOT NULL,
			key TEXT NOT NULL,
			value_json TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			source_event_id INTEGER NOT NULL,
			confidence REAL NOT NULL DEFAULT 0.8,
			metadata_json TEXT,
			supersedes INTEGER,
			created_at TEXT NOT NULL
		);
		CREATE TABLE companion_soft_episodes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			episode_type TEXT NOT NULL,
			start_event_id INTEGER NOT NULL,
			end_event_id INTEGER NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			needs_summary INTEGER NOT NULL DEFAULT 0,
			assumption_ids TEXT NOT NULL DEFAULT '[]',
			token_count INTEGER DEFAULT 0,
			boundary_hash TEXT NOT NULL,
			created_at TEXT NOT NULL,
			deleted_at TEXT
		);
		CREATE TABLE companion_evidence_snippets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			source_event_id INTEGER NOT NULL,
			event_type TEXT NOT NULL,
			fact_text TEXT NOT NULL,
			content_hash TEXT NOT NULL,
			confidence REAL NOT NULL DEFAULT 0.5,
			bucket TEXT NOT NULL DEFAULT 'default',
			ttl_days INTEGER,
			created_at TEXT NOT NULL,
			expires_at TEXT
		);
		CREATE TABLE companion_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			conversation_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			payload_json TEXT
		);
	`

	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatalf("create companion schema: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestRLMToolExecutor_SemanticQuery_VectorByTypeFiltersSessionAndAddsLayer(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	companionDB := setupTestCompanionDB(t)
	convID := "conv-a"

	exec := NewRLMToolExecutor(store, convID)
	exec.SetCompanionDB(companionDB)

	if _, err := companionDB.ExecContext(ctx, `
		INSERT INTO companion_memory_mode_state (conversation_id, mode, schema_version, last_processed_event, last_soft_event, last_evidence_event, updated_at)
		VALUES (?, 'hybrid', 1, 10, 10, 10, '2026-02-01 10:00:00')
	`, convID); err != nil {
		t.Fatalf("insert mode state: %v", err)
	}
	if _, err := companionDB.ExecContext(ctx, `
		INSERT INTO companion_events (id, conversation_id, event_type, payload_json)
		VALUES (1, ?, 'user_message', '{"text":"I prefer concise responses"}')
	`, convID); err != nil {
		t.Fatalf("insert source event: %v", err)
	}
	if _, err := companionDB.ExecContext(ctx, `
		INSERT INTO companion_hard_state_entries
			(conversation_id, entry_type, key, value_json, status, source_event_id, confidence, created_at)
		VALUES
			(?, 'preference', 'response_style', '{"value":"concise"}', 'active', 1, 0.95, '2026-02-01 10:00:00')
	`, convID); err != nil {
		t.Fatalf("insert hard state: %v", err)
	}
	if _, err := companionDB.ExecContext(ctx, `
		INSERT INTO companion_soft_episodes
			(conversation_id, episode_type, start_event_id, end_event_id, summary, needs_summary, assumption_ids, token_count, boundary_hash, created_at)
		VALUES
			(?, 'planning', 1, 1, 'Discussed response format and constraints.', 0, '[]', 12, 'episode-a', '2026-02-01 10:01:00')
	`, convID); err != nil {
		t.Fatalf("insert soft episode: %v", err)
	}
	if _, err := companionDB.ExecContext(ctx, `
		INSERT INTO companion_evidence_snippets
			(conversation_id, source_event_id, event_type, fact_text, content_hash, confidence, bucket, created_at)
		VALUES
			(?, 1, 'user_message', 'User asked for concise responses.', 'evidence-a', 0.91, 'preference', '2026-02-01 10:01:00')
	`, convID); err != nil {
		t.Fatalf("insert evidence snippet: %v", err)
	}

	// Cross-session rows should be ignored.
	if _, err := companionDB.ExecContext(ctx, `
		INSERT INTO companion_hard_state_entries
			(conversation_id, entry_type, key, value_json, status, source_event_id, confidence, created_at)
		VALUES
			('conv-b', 'preference', 'response_style', '{"value":"verbose"}', 'active', 1, 0.50, '2026-02-01 10:02:00')
	`); err != nil {
		t.Fatalf("insert cross-session hard state: %v", err)
	}

	out, err := exec.Execute(ctx, "rlm_context_query", json.RawMessage(`{"semantic_query":"concise responses","limit":5}`))
	if err != nil {
		t.Fatalf("Execute semantic query failed: %v", err)
	}

	var resp struct {
		Memories []map[string]any `json:"memories"`
		Found    bool             `json:"found"`
		Count    int              `json:"count"`
		Stats    map[string]any   `json:"stats"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("Failed to parse semantic query response: %v", err)
	}
	if !resp.Found {
		t.Fatalf("Expected found=true, got false")
	}
	if resp.Count == 0 || len(resp.Memories) == 0 {
		t.Fatalf("Expected memories, got none")
	}

	hasHard := false
	hasEpisode := false
	hasEvidence := false
	for _, m := range resp.Memories {
		name, _ := m["name"].(string)
		if name == "" {
			t.Fatalf("Expected memory name, got empty")
		}
		// Must not include other conversations.
		if strings.Contains(name, "conv-b-") {
			t.Fatalf("Unexpected cross-session memory: %s", name)
		}

		typ, _ := m["type"].(string)
		layer, _ := m["layer"].(string)
		switch typ {
		case "companion_hard_state":
			hasHard = true
			if layer != "H1" {
				t.Fatalf("Expected H1 for companion_hard_state, got %q", layer)
			}
		case "companion_soft_episode":
			hasEpisode = true
			if layer != "E1" {
				t.Fatalf("Expected E1 for companion_soft_episode, got %q", layer)
			}
		case "companion_evidence_snippet":
			hasEvidence = true
			if layer != "EVD" {
				t.Fatalf("Expected EVD for companion_evidence_snippet, got %q", layer)
			}
		}
	}

	if !hasHard || !hasEpisode || !hasEvidence {
		t.Fatalf("Expected hard/episode/evidence results, got hard=%v episode=%v evidence=%v", hasHard, hasEpisode, hasEvidence)
	}

	if got, _ := resp.Stats["method"].(string); got != "hybrid" {
		t.Fatalf("Expected stats.method=hybrid, got %q", got)
	}
}

func TestRLMToolExecutor_SemanticQuery_TruncatesLongSummaries(t *testing.T) {
	ctx := context.Background()
	store := setupTestContextStore(t)
	defer store.Close()

	companionDB := setupTestCompanionDB(t)
	convID := "conv-long"

	exec := NewRLMToolExecutor(store, convID)
	exec.SetCompanionDB(companionDB)

	long := strings.Repeat("x", 2000)
	if _, err := companionDB.ExecContext(ctx, `
		INSERT INTO companion_memory_mode_state (conversation_id, mode, schema_version, last_processed_event, last_soft_event, last_evidence_event, updated_at)
		VALUES (?, 'hybrid', 1, 1, 1, 1, '2026-02-01 10:00:00')
	`, convID); err != nil {
		t.Fatalf("insert mode state: %v", err)
	}
	if _, err := companionDB.ExecContext(ctx, `
		INSERT INTO companion_hard_state_entries
			(conversation_id, entry_type, key, value_json, status, source_event_id, confidence, created_at)
		VALUES (?, 'preference', 'verbosity', ?, 'active', 1, 0.99, '2026-02-01 10:00:00')
	`, convID, long); err != nil {
		t.Fatalf("insert long hard state value: %v", err)
	}

	out, err := exec.Execute(ctx, "rlm_context_query", json.RawMessage(`{"semantic_query":"anything","limit":1}`))
	if err != nil {
		t.Fatalf("Execute semantic query failed: %v", err)
	}

	var resp struct {
		Memories []map[string]any `json:"memories"`
		Stats    struct {
			Truncated bool `json:"truncated"`
		} `json:"stats"`
	}
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("Failed to parse semantic query response: %v", err)
	}
	if len(resp.Memories) != 1 {
		t.Fatalf("Expected 1 memory, got %d", len(resp.Memories))
	}
	summary, _ := resp.Memories[0]["summary"].(string)
	if len(summary) == 0 || len(summary) > 1300 {
		t.Fatalf("Expected truncated summary, got len=%d", len(summary))
	}
	if !resp.Stats.Truncated {
		t.Fatalf("Expected stats.truncated=true for long summaries")
	}
}
