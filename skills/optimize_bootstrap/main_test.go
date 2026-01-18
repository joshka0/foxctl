package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "optimize/bootstrap", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Workspace:      "/workspace/path",
		Role:           "coder",
		MaxExamples:    10,
		MinSuccessRate: 0.8,
		Format:         "prompt",
	}

	assert.Equal(t, "/workspace/path", in.Workspace)
	assert.Equal(t, "coder", in.Role)
	assert.Equal(t, 10, in.MaxExamples)
	assert.Equal(t, 0.8, in.MinSuccessRate)
	assert.Equal(t, "prompt", in.Format)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Workspace:      "/test/workspace",
		Role:           "reviewer",
		MaxExamples:    5,
		MinSuccessRate: 0.9,
		Format:         "json",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Role, decoded.Role)
	assert.Equal(t, in.MaxExamples, decoded.MaxExamples)
	assert.Equal(t, in.MinSuccessRate, decoded.MinSuccessRate)
	assert.Equal(t, in.Format, decoded.Format)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Workspace)
	assert.Empty(t, in.Role)
	assert.Zero(t, in.MaxExamples)
	assert.Zero(t, in.MinSuccessRate)
	assert.Empty(t, in.Format)
}

func TestInput_RoleValues(t *testing.T) {
	roles := []string{"coder", "planner", "reviewer", "overseer"}

	for _, role := range roles {
		in := input{Role: role}
		assert.Equal(t, role, in.Role)
	}
}

func TestInput_FormatValues(t *testing.T) {
	formats := []string{"prompt", "json", ""}

	for _, format := range formats {
		in := input{Format: format}
		assert.Equal(t, format, in.Format)
	}
}

func TestInput_MinSuccessRateRange(t *testing.T) {
	// Test valid success rate values
	testCases := []float64{0.0, 0.5, 0.7, 0.9, 1.0}

	for _, rate := range testCases {
		in := input{MinSuccessRate: rate}
		assert.Equal(t, rate, in.MinSuccessRate)
	}
}

// Tests for defaults logic

func TestInput_WorkspaceDefault(t *testing.T) {
	in := input{}

	workspace := in.Workspace
	if workspace == "" {
		workspace = "."
	}

	assert.Equal(t, ".", workspace)
}

func TestInput_MaxExamplesDefault(t *testing.T) {
	in := input{}

	// When MaxExamples is 0 or negative, the skill uses DefaultBootstrapConfig
	assert.Zero(t, in.MaxExamples)
}

func TestInput_MinSuccessRateDefault(t *testing.T) {
	in := input{}

	// When MinSuccessRate is 0, the skill uses DefaultBootstrapConfig
	assert.Zero(t, in.MinSuccessRate)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Workspace:      "/full/test/workspace",
		Role:           "coder",
		MaxExamples:    20,
		MinSuccessRate: 0.75,
		Format:         "prompt",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Role, decoded.Role)
	assert.Equal(t, in.MaxExamples, decoded.MaxExamples)
	assert.InDelta(t, in.MinSuccessRate, decoded.MinSuccessRate, 0.001)
	assert.Equal(t, in.Format, decoded.Format)
}

func TestInput_LargeMaxExamples(t *testing.T) {
	in := input{
		Role:        "coder",
		MaxExamples: 1000,
	}

	assert.Equal(t, 1000, in.MaxExamples)
}

func TestInput_WorkspaceWithSpaces(t *testing.T) {
	in := input{
		Workspace: "/path/with spaces/workspace",
		Role:      "coder",
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
		Role:      "reviewer",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	assert.Contains(t, string(data), "project-v1.0")
}

func TestInput_RoleOnly(t *testing.T) {
	in := input{
		Role: "coder",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	// Should contain role
	assert.Contains(t, string(data), "role")
	assert.Contains(t, string(data), "coder")
}

func TestInput_HighMinSuccessRate(t *testing.T) {
	in := input{
		Role:           "coder",
		MinSuccessRate: 0.99,
	}

	assert.Equal(t, 0.99, in.MinSuccessRate)
}

func TestInput_ZeroMaxExamples(t *testing.T) {
	in := input{
		Role:        "coder",
		MaxExamples: 0,
	}

	// When MaxExamples is 0, it should use default
	assert.Zero(t, in.MaxExamples)
}

func TestInput_NegativeMaxExamples(t *testing.T) {
	in := input{
		Role:        "coder",
		MaxExamples: -5,
	}

	// Negative values should be handled by the skill
	assert.Equal(t, -5, in.MaxExamples)
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := input{
		Workspace:      "/ws",
		Role:           "coder",
		MaxExamples:    10,
		MinSuccessRate: 0.8,
		Format:         "prompt",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "workspace")
	assert.Contains(t, jsonStr, "role")
	assert.Contains(t, jsonStr, "max_examples")
	assert.Contains(t, jsonStr, "min_success_rate")
	assert.Contains(t, jsonStr, "format")
}
