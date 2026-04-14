package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "setup/agentctl_mode", command)
}

// Tests for Input structure

func TestInput_AllFields(t *testing.T) {
	enabled := true
	in := Input{
		Operation:   "set",
		WorkspaceID: "/home/user/project",
		Enabled:     &enabled,
	}

	assert.Equal(t, "set", in.Operation)
	assert.Equal(t, "/home/user/project", in.WorkspaceID)
	assert.NotNil(t, in.Enabled)
	assert.True(t, *in.Enabled)
}

func TestInput_JSONSerialization(t *testing.T) {
	enabled := false
	in := Input{
		Operation:   "get",
		WorkspaceID: "/path/to/workspace",
		Enabled:     &enabled,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.WorkspaceID, decoded.WorkspaceID)
	assert.NotNil(t, decoded.Enabled)
	assert.Equal(t, *in.Enabled, *decoded.Enabled)
}

func TestInput_EmptyFields(t *testing.T) {
	in := Input{}

	assert.Empty(t, in.Operation)
	assert.Empty(t, in.WorkspaceID)
	assert.Nil(t, in.Enabled)
}

func TestInput_OperationValues(t *testing.T) {
	operations := []string{"get", "set"}

	for _, op := range operations {
		in := Input{Operation: op}
		assert.Equal(t, op, in.Operation)
	}
}

func TestInput_JSONFieldNames(t *testing.T) {
	enabled := true
	in := Input{
		Operation:   "set",
		WorkspaceID: "ws",
		Enabled:     &enabled,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "operation")
	assert.Contains(t, jsonStr, "workspace_id")
	assert.Contains(t, jsonStr, "enabled")
}

func TestInput_EnabledPointer_True(t *testing.T) {
	enabled := true
	in := Input{Enabled: &enabled}

	assert.NotNil(t, in.Enabled)
	assert.True(t, *in.Enabled)
}

func TestInput_EnabledPointer_False(t *testing.T) {
	enabled := false
	in := Input{Enabled: &enabled}

	assert.NotNil(t, in.Enabled)
	assert.False(t, *in.Enabled)
}

func TestInput_EnabledPointer_Nil(t *testing.T) {
	in := Input{Operation: "get"}

	assert.Nil(t, in.Enabled)
}

func TestInput_GetOperation(t *testing.T) {
	in := Input{
		Operation:   "get",
		WorkspaceID: "/some/workspace",
	}

	assert.Equal(t, "get", in.Operation)
	assert.Nil(t, in.Enabled) // get doesn't need enabled
}

func TestInput_SetOperation(t *testing.T) {
	enabled := true
	in := Input{
		Operation:   "set",
		WorkspaceID: "/some/workspace",
		Enabled:     &enabled,
	}

	assert.Equal(t, "set", in.Operation)
	assert.NotNil(t, in.Enabled)
}

// Tests for Output structure

func TestOutput_AllFields(t *testing.T) {
	out := Output{
		Enabled:     true,
		WorkspaceID: "/home/user/project",
	}

	assert.True(t, out.Enabled)
	assert.Equal(t, "/home/user/project", out.WorkspaceID)
}

func TestOutput_JSONSerialization(t *testing.T) {
	out := Output{
		Enabled:     false,
		WorkspaceID: "/path/to/workspace",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Enabled, decoded.Enabled)
	assert.Equal(t, out.WorkspaceID, decoded.WorkspaceID)
}

func TestOutput_EmptyFields(t *testing.T) {
	out := Output{}

	assert.False(t, out.Enabled)
	assert.Empty(t, out.WorkspaceID)
}

func TestOutput_JSONFieldNames(t *testing.T) {
	out := Output{
		Enabled:     true,
		WorkspaceID: "ws",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "enabled")
	assert.Contains(t, jsonStr, "workspace_id")
}

func TestOutput_EnabledTrue(t *testing.T) {
	out := Output{Enabled: true}
	assert.True(t, out.Enabled)
}

func TestOutput_EnabledFalse(t *testing.T) {
	out := Output{Enabled: false}
	assert.False(t, out.Enabled)
}

// Tests for ModeValue structure

func TestModeValue_AllFields(t *testing.T) {
	mv := ModeValue{
		Enabled: true,
	}

	assert.True(t, mv.Enabled)
}

func TestModeValue_JSONSerialization(t *testing.T) {
	mv := ModeValue{Enabled: true}

	data, err := json.Marshal(mv)
	assert.NoError(t, err)

	var decoded ModeValue
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, mv.Enabled, decoded.Enabled)
}

func TestModeValue_EmptyFields(t *testing.T) {
	mv := ModeValue{}
	assert.False(t, mv.Enabled)
}

func TestModeValue_JSONFieldNames(t *testing.T) {
	mv := ModeValue{Enabled: true}

	data, err := json.Marshal(mv)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "enabled")
}

func TestModeValue_EnabledTrue(t *testing.T) {
	mv := ModeValue{Enabled: true}
	assert.True(t, mv.Enabled)
}

func TestModeValue_EnabledFalse(t *testing.T) {
	mv := ModeValue{Enabled: false}
	assert.False(t, mv.Enabled)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	enabled := true
	in := Input{
		Operation:   "set",
		WorkspaceID: "/full/workspace/path",
		Enabled:     &enabled,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded Input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Operation, decoded.Operation)
	assert.Equal(t, in.WorkspaceID, decoded.WorkspaceID)
	assert.NotNil(t, decoded.Enabled)
	assert.Equal(t, *in.Enabled, *decoded.Enabled)
}

func TestOutput_FullJSONRoundTrip(t *testing.T) {
	out := Output{
		Enabled:     true,
		WorkspaceID: "/full/workspace/path",
	}

	data, err := json.Marshal(out)
	assert.NoError(t, err)

	var decoded Output
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, out.Enabled, decoded.Enabled)
	assert.Equal(t, out.WorkspaceID, decoded.WorkspaceID)
}

func TestModeValue_FullJSONRoundTrip(t *testing.T) {
	mv := ModeValue{Enabled: true}

	data, err := json.Marshal(mv)
	assert.NoError(t, err)

	var decoded ModeValue
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, mv.Enabled, decoded.Enabled)
}

func TestInput_WorkspaceIDVariants(t *testing.T) {
	workspaces := []string{
		"/home/user/project",
		"/var/www/app",
		".",
		"./relative/path",
		"/Users/name/repos/myapp",
	}

	for _, ws := range workspaces {
		in := Input{WorkspaceID: ws}
		assert.Equal(t, ws, in.WorkspaceID)
	}
}

func TestInput_JSONDecodeNullEnabled(t *testing.T) {
	jsonData := `{"operation": "get", "workspace_id": "/test"}`

	var decoded Input
	err := json.Unmarshal([]byte(jsonData), &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "get", decoded.Operation)
	assert.Nil(t, decoded.Enabled)
}

func TestInput_JSONDecodeWithEnabled(t *testing.T) {
	jsonData := `{"operation": "set", "workspace_id": "/test", "enabled": true}`

	var decoded Input
	err := json.Unmarshal([]byte(jsonData), &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "set", decoded.Operation)
	assert.NotNil(t, decoded.Enabled)
	assert.True(t, *decoded.Enabled)
}
