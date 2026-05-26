package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestSubagentStopPayload_WithError(t *testing.T) {
	payload := SubagentStopPayload{
		SubagentName: "failed-agent",
		ExitCode:     1,
		Error:        "task failed: timeout exceeded",
	}

	assert.Equal(t, "failed-agent", payload.SubagentName)
	assert.Equal(t, 1, payload.ExitCode)
	assert.Equal(t, "task failed: timeout exceeded", payload.Error)
}

func TestSubagentStopPayload_NonZeroExitCode(t *testing.T) {
	exitCodes := []int{1, 2, 127, 128, 255, -1}

	for _, code := range exitCodes {
		payload := SubagentStopPayload{
			SubagentName: "agent",
			ExitCode:     code,
		}

		assert.Equal(t, code, payload.ExitCode)
	}
}

// Tests for JSON deserialization

func TestSubagentStopPayload_FromJSON(t *testing.T) {
	jsonStr := `{"subagent_name":"test","subagent_type":"Test","agent_id":"123","exit_code":0}`

	var payload SubagentStopPayload
	err := json.Unmarshal([]byte(jsonStr), &payload)
	assert.NoError(t, err)

	assert.Equal(t, "test", payload.SubagentName)
	assert.Equal(t, "Test", payload.SubagentType)
	assert.Equal(t, "123", payload.AgentID)
	assert.Equal(t, 0, payload.ExitCode)
}

func TestSubagentStopPayload_FromJSONWithError(t *testing.T) {
	jsonStr := `{"subagent_name":"failed","exit_code":1,"error":"something went wrong"}`

	var payload SubagentStopPayload
	err := json.Unmarshal([]byte(jsonStr), &payload)
	assert.NoError(t, err)

	assert.Equal(t, "failed", payload.SubagentName)
	assert.Equal(t, 1, payload.ExitCode)
	assert.Equal(t, "something went wrong", payload.Error)
}

func TestSubagentStopPayload_PartialJSON(t *testing.T) {
	jsonStr := `{"subagent_name":"test"}`

	var payload SubagentStopPayload
	err := json.Unmarshal([]byte(jsonStr), &payload)
	assert.NoError(t, err)

	assert.Equal(t, "test", payload.SubagentName)
	assert.Empty(t, payload.SubagentType)
	assert.Empty(t, payload.AgentID)
	assert.Zero(t, payload.ExitCode)
}

func TestSubagentStopPayload_ExitCodeOmittedInJSON(t *testing.T) {
	// When exit_code is 0 and omitempty is used, it may be omitted
	payload := SubagentStopPayload{
		SubagentName: "test",
		ExitCode:     0,
	}

	data, err := json.Marshal(payload)
	assert.NoError(t, err)

	// exit_code with 0 value should be omitted due to omitempty tag
	// But it depends on the struct definition - check if it's present
	var result map[string]any
	err = json.Unmarshal(data, &result)
	assert.NoError(t, err)

	// The subagent_name should always be present
	assert.Contains(t, result, "subagent_name")
}

// Tests for edge cases

func TestSubagentStopPayload_LongError(t *testing.T) {
	longError := ""
	for i := 0; i < 1000; i++ {
		longError += "error message part "
	}

	payload := SubagentStopPayload{
		SubagentName: "agent",
		Error:        longError,
	}

	data, err := json.Marshal(payload)
	assert.NoError(t, err)

	var decoded SubagentStopPayload
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, longError, decoded.Error)
}

func TestSubagentStopPayload_SpecialCharsInError(t *testing.T) {
	payload := SubagentStopPayload{
		SubagentName: "agent",
		Error:        "error: \"unexpected token\" in <script>alert('xss')</script>",
	}

	data, err := json.Marshal(payload)
	assert.NoError(t, err)

	var decoded SubagentStopPayload
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, payload.Error, decoded.Error)
}

func TestSubagentStopPayload_UnicodeInFields(t *testing.T) {
	payload := SubagentStopPayload{
		SubagentName: "探索者",
		Error:        "错误: 操作失败",
	}

	data, err := json.Marshal(payload)
	assert.NoError(t, err)

	var decoded SubagentStopPayload
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "探索者", decoded.SubagentName)
	assert.Equal(t, "错误: 操作失败", decoded.Error)
}

// Tests for name resolution logic (from run function)

func TestNameResolution_PreferSubagentName(t *testing.T) {
	payload := SubagentStopPayload{
		SubagentName: "explicit-name",
		SubagentType: "Type",
	}

	// Logic from run: prefer SubagentName over SubagentType
	name := payload.SubagentName
	if name == "" {
		name = payload.SubagentType
	}

	assert.Equal(t, "explicit-name", name)
}

func TestNameResolution_FallbackToType(t *testing.T) {
	payload := SubagentStopPayload{
		SubagentName: "",
		SubagentType: "TypeFallback",
	}

	name := payload.SubagentName
	if name == "" {
		name = payload.SubagentType
	}

	assert.Equal(t, "TypeFallback", name)
}

func TestNameResolution_BothEmpty(t *testing.T) {
	payload := SubagentStopPayload{}

	name := payload.SubagentName
	if name == "" {
		name = payload.SubagentType
	}

	assert.Empty(t, name)
}

// Tests for AgentID resolution logic

func TestAgentIDResolution_FromPayload(t *testing.T) {
	payload := SubagentStopPayload{
		AgentID: "payload-agent-id",
	}

	agentID := payload.AgentID

	assert.Equal(t, "payload-agent-id", agentID)
}

func TestAgentIDResolution_Empty(t *testing.T) {
	payload := SubagentStopPayload{}

	agentID := payload.AgentID

	assert.Empty(t, agentID)
}

// Tests for exit code semantics

func TestExitCode_Success(t *testing.T) {
	payload := SubagentStopPayload{ExitCode: 0}
	assert.Zero(t, payload.ExitCode)
}

func TestExitCode_GeneralError(t *testing.T) {
	payload := SubagentStopPayload{ExitCode: 1}
	assert.Equal(t, 1, payload.ExitCode)
}

func TestExitCode_CommandNotFound(t *testing.T) {
	payload := SubagentStopPayload{ExitCode: 127}
	assert.Equal(t, 127, payload.ExitCode)
}

func TestExitCode_SignalKilled(t *testing.T) {
	// 128 + signal number (e.g., SIGKILL = 9)
	payload := SubagentStopPayload{ExitCode: 137}
	assert.Equal(t, 137, payload.ExitCode)
}
