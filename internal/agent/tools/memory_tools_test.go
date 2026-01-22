package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/stretchr/testify/require"
)

func parseMemoryResult(t *testing.T, result *models.CallToolResult) map[string]any {
	t.Helper()
	require.False(t, result.IsError, "tool execution failed: %v", result.Content)
	require.NotEmpty(t, result.Content, "no content returned")

	text, ok := result.Content[0].(models.TextContent)
	require.True(t, ok, "expected TextContent, got %T", result.Content[0])

	var data map[string]any
	err := json.Unmarshal([]byte(text.Text), &data)
	require.NoError(t, err, "unmarshal result: %s", text.Text)

	// Check if response is wrapped in a "data" envelope and unwrap if so
	if envelope, ok := data["data"]; ok {
		unwrapped, ok := envelope.(map[string]any)
		require.True(t, ok, "expected 'data' field to be map[string]any, got %T", envelope)
		return unwrapped
	}
	return data
}

func TestMemoryQuery_Integration(t *testing.T) {
	// Skip in CI - requires VOYAGE_API_KEY and local memory.db
	if os.Getenv("VOYAGE_API_KEY") == "" {
		t.Skip("VOYAGE_API_KEY not set")
	}

	// Use AGENTCTL_TEST_WORKSPACE or current working directory
	workspace := os.Getenv("AGENTCTL_TEST_WORKSPACE")
	if workspace == "" {
		var err error
		workspace, err = os.Getwd()
		if err != nil {
			t.Fatalf("failed to get working directory: %v", err)
		}
	}
	if _, err := os.Stat(workspace); os.IsNotExist(err) {
		t.Skip("test workspace not found")
	}

	cfg := Config{
		WorkspaceRoot: workspace,
	}

	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	ctx := context.Background()

	// Test memory.query tool
	result, err := registry.memoryQuery(ctx, map[string]any{
		"query": "gotcha",
		"limit": float64(5),
	})
	if err != nil {
		t.Fatalf("memoryQuery error: %v", err)
	}

	t.Logf("Raw result: %+v", result)

	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	data := parseMemoryResult(t, result)
	t.Logf("Parsed data: %+v", data)

	// Check the response structure
	count, ok := data["count"].(float64)
	require.True(t, ok, "expected count field")
	t.Logf("count: %v", count)

	totalFound, ok := data["total_found"].(float64)
	require.True(t, ok, "expected total_found field")
	t.Logf("total_found: %v", totalFound)

	memories, ok := data["memories"].([]any)
	require.True(t, ok, "expected memories array, got %T", data["memories"])
	t.Logf("memories length: %d", len(memories))

	// If total_found > 0 but memories is empty, there's a bug
	if totalFound > 0 && len(memories) == 0 {
		t.Errorf("BUG: total_found=%v but memories array is empty", totalFound)
	}

	// Log first memory if any
	if len(memories) > 0 {
		t.Logf("First memory: %+v", memories[0])
	}
}
