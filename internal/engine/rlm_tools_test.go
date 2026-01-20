package engine

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/storage/contextvar"
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
