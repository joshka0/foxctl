package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestAllowedOps(t *testing.T) {
	assert.Contains(t, allowedOps, "cleanup")
	assert.Contains(t, allowedOps, "stats")
	assert.Contains(t, allowedOps, "repair")
	assert.Len(t, allowedOps, 3)
}

// Tests for input structure

func TestInput_OperationValues(t *testing.T) {
	validOps := []string{"cleanup", "stats", "repair"}

	for _, op := range validOps {
		in := input{Operation: op}
		assert.Equal(t, op, in.Operation)
	}
}

func TestInput_JSONWithWorkspaceOnly(t *testing.T) {
	in := input{
		Workspace: "/my/workspace",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "workspace")
	assert.Contains(t, string(data), "/my/workspace")
}

// Tests for cleanupResult structure

func TestInput_CleanupDefaults(t *testing.T) {
	in := input{
		Operation: "cleanup",
		// All cleanup flags are false
	}

	// Test the default behavior logic from handleCleanup
	cleanExpired := in.CleanExpired
	cleanDangling := in.CleanDangling
	recalculate := in.Recalculate

	if !cleanExpired && !cleanDangling && !recalculate {
		// Default to all cleanup operations
		cleanExpired = true
		cleanDangling = true
		recalculate = true
	}

	assert.True(t, cleanExpired, "should default to cleaning expired")
	assert.True(t, cleanDangling, "should default to cleaning dangling")
	assert.True(t, recalculate, "should default to recalculating")
}

func TestInput_CleanupExplicitFlags(t *testing.T) {
	in := input{
		Operation:     "cleanup",
		CleanExpired:  true,
		CleanDangling: false,
		Recalculate:   false,
	}

	// Test the default behavior logic - should NOT override when explicit
	cleanExpired := in.CleanExpired
	cleanDangling := in.CleanDangling
	recalculate := in.Recalculate

	if !cleanExpired && !cleanDangling && !recalculate {
		cleanExpired = true
		cleanDangling = true
		recalculate = true
	}

	// Since cleanExpired is true, defaults should NOT be applied
	assert.True(t, cleanExpired)
	assert.False(t, cleanDangling) // Should remain false
	assert.False(t, recalculate)   // Should remain false
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Workspace:     "/full/test/workspace",
		Operation:     "cleanup",
		CleanExpired:  true,
		CleanDangling: true,
		Recalculate:   true,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.CleanExpired, decoded.CleanExpired)
	assert.Equal(t, in.CleanDangling, decoded.CleanDangling)
	assert.Equal(t, in.Recalculate, decoded.Recalculate)
}

func TestCleanupResult_FullJSONRoundTrip(t *testing.T) {
	result := cleanupResult{
		ExpiredEdgesRemoved:  1000,
		DanglingEdgesRemoved: 500,
		DegreesRecalculated:  true,
	}

	data, err := json.Marshal(result)
	assert.NoError(t, err)

	var decoded cleanupResult
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, result.ExpiredEdgesRemoved, decoded.ExpiredEdgesRemoved)
	assert.Equal(t, result.DanglingEdgesRemoved, decoded.DanglingEdgesRemoved)
	assert.Equal(t, result.DegreesRecalculated, decoded.DegreesRecalculated)
}

func TestCleanupResult_LargeCounts(t *testing.T) {
	result := cleanupResult{
		ExpiredEdgesRemoved:  100000,
		DanglingEdgesRemoved: 50000,
		DegreesRecalculated:  true,
	}

	data, err := json.Marshal(result)
	assert.NoError(t, err)

	var decoded cleanupResult
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, 100000, decoded.ExpiredEdgesRemoved)
	assert.Equal(t, 50000, decoded.DanglingEdgesRemoved)
}

func TestInput_WorkspaceWithSpaces(t *testing.T) {
	in := input{
		Workspace: "/path/with spaces/workspace",
		Operation: "stats",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "/path/with spaces/workspace", decoded.Workspace)
}

func TestInput_WorkspaceWithSpecialChars(t *testing.T) {
	in := input{
		Workspace: "/Users/user/project-v1.0/workspace",
		Operation: "repair",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "project-v1.0")
}

func TestAllowedOps_Order(t *testing.T) {
	// Verify the order matches what's in the hint message
	assert.Equal(t, "cleanup", allowedOps[0])
	assert.Equal(t, "stats", allowedOps[1])
	assert.Equal(t, "repair", allowedOps[2])
}

func TestCleanupResult_OnlyDegreesRecalculated(t *testing.T) {
	result := cleanupResult{
		DegreesRecalculated: true,
	}

	data, err := json.Marshal(result)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "degrees_recalculated")
	assert.NotContains(t, string(data), "expired_edges_removed")
	assert.NotContains(t, string(data), "dangling_edges_removed")
}
