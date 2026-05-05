package tools

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	models "github.com/XiaoConstantine/mcp-go/pkg/model"
	"github.com/joshka0/foxctl/internal/context/memorycore"
	"github.com/stretchr/testify/require"
)

func parseMemoryToolResult(t *testing.T, result *models.CallToolResult) map[string]any {
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

func TestCanonicalMemoryQueryResultExposesLaneContract(t *testing.T) {
	records := []memorycore.Record{
		{
			ID:         "mem-1",
			Kind:       memorycore.KindSemanticFact,
			SourceLane: memorycore.SourceLaneNamedMemory,
			Lifecycle:  memorycore.LifecycleEnvelope{State: memorycore.LifecycleStateActive},
			Trust: memorycore.TrustEnvelope{
				SourceTrust: "agent_generated",
				Confidence:  0.55,
				Authority:   0.25,
			},
			Usage: memorycore.UsageEnvelope{
				InstructionEligible: false,
				EvidenceOnly:        true,
				Reason:              "named memory records are evidence unless promoted as validated policy or skill",
			},
		},
	}

	result := canonicalMemoryQueryResult(records, 3, true)

	require.Equal(t, records, result["records"])
	require.Equal(t, 1, result["count"])
	require.Equal(t, 3, result["total_found"])
	require.Equal(t, true, result["has_more"])

	contract, ok := result["lane_contract"].(memoryLaneContract)
	require.True(t, ok, "lane_contract type = %T", result["lane_contract"])
	require.Equal(t, "records", contract.RecordSurface)
	require.Equal(t, "source_lane", contract.SourceLaneField)
	require.Equal(t, "lifecycle.state", contract.LifecycleField)
	require.Equal(t, "trust", contract.TrustField)
	require.Equal(t, "usage", contract.UsageField)
	require.Equal(t, "evidence_only", contract.DefaultUsage)

	warnings, ok := result["warnings"].([]memoryRecordWarning)
	require.True(t, ok, "warnings type = %T", result["warnings"])
	require.Len(t, warnings, 1)
	require.Equal(t, "evidence_only", warnings[0].Code)
	require.Equal(t, "mem-1", warnings[0].RecordID)
	require.Equal(t, "named_memory", warnings[0].SourceLane)
}

func TestMemoryRecordWarningsForStaleAndQuarantinedEvidence(t *testing.T) {
	records := []memorycore.Record{
		warningTestRecord("stale-1", memorycore.LifecycleStateStale),
		warningTestRecord("quarantined-1", memorycore.LifecycleStateQuarantined),
	}

	warnings := memoryRecordWarnings(records)

	require.Len(t, warnings, 4)
	requireMemoryWarning(t, warnings, "stale-1", "stale_evidence")
	requireMemoryWarning(t, warnings, "stale-1", "evidence_only")
	requireMemoryWarning(t, warnings, "quarantined-1", "quarantined_evidence")
	requireMemoryWarning(t, warnings, "quarantined-1", "evidence_only")
}

func TestMemoryRecordWarningsAllowExplicitInstructionEligiblePolicy(t *testing.T) {
	records := []memorycore.Record{
		{
			ID:         "policy-1",
			Kind:       memorycore.KindPolicyRule,
			SourceLane: memorycore.SourceLaneNamedMemory,
			Lifecycle:  memorycore.LifecycleEnvelope{State: memorycore.LifecycleStateActive},
			Usage: memorycore.UsageEnvelope{
				InstructionEligible: true,
				EvidenceOnly:        false,
				Reason:              "validated active policy",
			},
		},
		warningTestRecord("fact-1", memorycore.LifecycleStateActive),
	}

	warnings := memoryRecordWarnings(records)

	require.Len(t, warnings, 1)
	requireMemoryWarning(t, warnings, "fact-1", "evidence_only")
	requireNoMemoryWarning(t, warnings, "policy-1", "evidence_only")
}

func warningTestRecord(id string, state memorycore.LifecycleState) memorycore.Record {
	return memorycore.Record{
		ID:         id,
		Kind:       memorycore.KindSemanticFact,
		SourceLane: memorycore.SourceLaneNamedMemory,
		Lifecycle:  memorycore.LifecycleEnvelope{State: state},
		Usage: memorycore.UsageEnvelope{
			InstructionEligible: false,
			EvidenceOnly:        true,
		},
	}
}

func requireMemoryWarning(t *testing.T, warnings []memoryRecordWarning, recordID, code string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.RecordID == recordID && warning.Code == code {
			return
		}
	}
	t.Fatalf("missing warning record_id=%q code=%q in %#v", recordID, code, warnings)
}

func requireNoMemoryWarning(t *testing.T, warnings []memoryRecordWarning, recordID, code string) {
	t.Helper()
	for _, warning := range warnings {
		if warning.RecordID == recordID && warning.Code == code {
			t.Fatalf("unexpected warning record_id=%q code=%q in %#v", recordID, code, warnings)
		}
	}
}

func TestMemoryQuery_Integration(t *testing.T) {
	// Skip in CI - requires VOYAGE_API_KEY and local memory.db
	if os.Getenv("VOYAGE_API_KEY") == "" {
		t.Skip("VOYAGE_API_KEY not set")
	}

	// Use FOXCTL_TEST_WORKSPACE or current working directory
	workspace := os.Getenv("FOXCTL_TEST_WORKSPACE")
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
		"query": "semantic_fact",
		"limit": float64(5),
	})
	if err != nil {
		t.Fatalf("memoryQuery error: %v", err)
	}

	t.Logf("Raw result: %+v", result)

	if result.IsError {
		t.Fatalf("tool returned error: %v", result.Content)
	}

	data := parseMemoryToolResult(t, result)
	t.Logf("Parsed data: %+v", data)

	// Check the response structure
	count, ok := data["count"].(float64)
	require.True(t, ok, "expected count field")
	t.Logf("count: %v", count)

	totalFound, ok := data["total_found"].(float64)
	require.True(t, ok, "expected total_found field")
	t.Logf("total_found: %v", totalFound)

	records, ok := data["records"].([]any)
	require.True(t, ok, "expected records array, got %T", data["records"])
	t.Logf("records length: %d", len(records))

	// If total_found > 0 but records is empty, there's a bug
	if totalFound > 0 && len(records) == 0 {
		t.Errorf("BUG: total_found=%v but records array is empty", totalFound)
	}

	// Log first record if any
	if len(records) > 0 {
		t.Logf("First record: %+v", records[0])
	}
}
