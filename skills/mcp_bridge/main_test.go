package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "mcp/bridge", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		ServerCmd:     "npx",
		ServerArgs:    []string{"-y", "@modelcontextprotocol/server-sqlite", "/path/to/db"},
		ServerEnv:     map[string]string{"NODE_ENV": "production"},
		ServerURL:     "http://localhost:8080/sse",
		ServerHeaders: map[string]string{"Authorization": "Bearer token"},
		ToolName:      "query",
		ToolArgs:      map[string]any{"sql": "SELECT * FROM users"},
	}

	assert.Equal(t, "npx", in.ServerCmd)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-sqlite", "/path/to/db"}, in.ServerArgs)
	assert.Equal(t, "production", in.ServerEnv["NODE_ENV"])
	assert.Equal(t, "http://localhost:8080/sse", in.ServerURL)
	assert.Equal(t, "Bearer token", in.ServerHeaders["Authorization"])
	assert.Equal(t, "query", in.ToolName)
	assert.Equal(t, "SELECT * FROM users", in.ToolArgs["sql"])
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		ServerCmd:  "node",
		ServerArgs: []string{"server.js"},
		ToolName:   "test_tool",
		ToolArgs:   map[string]any{"param": "value"},
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.ServerCmd, decoded.ServerCmd)
	assert.Equal(t, in.ServerArgs, decoded.ServerArgs)
	assert.Equal(t, in.ToolName, decoded.ToolName)
	assert.Equal(t, in.ToolArgs["param"], decoded.ToolArgs["param"])
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.ServerCmd)
	assert.Nil(t, in.ServerArgs)
	assert.Nil(t, in.ServerEnv)
	assert.Empty(t, in.ServerURL)
	assert.Nil(t, in.ServerHeaders)
	assert.Empty(t, in.ToolName)
	assert.Nil(t, in.ToolArgs)
}

func TestInput_JSONFieldNames(t *testing.T) {
	in := input{
		ServerCmd:     "cmd",
		ServerArgs:    []string{"arg"},
		ServerEnv:     map[string]string{"KEY": "val"},
		ServerURL:     "url",
		ServerHeaders: map[string]string{"H": "v"},
		ToolName:      "tool",
		ToolArgs:      map[string]any{"a": "b"},
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	jsonStr := string(data)
	assert.Contains(t, jsonStr, "server_cmd")
	assert.Contains(t, jsonStr, "server_args")
	assert.Contains(t, jsonStr, "server_env")
	assert.Contains(t, jsonStr, "server_url")
	assert.Contains(t, jsonStr, "server_headers")
	assert.Contains(t, jsonStr, "tool_name")
	assert.Contains(t, jsonStr, "tool_args")
}

func TestInput_StdioMode(t *testing.T) {
	// Stdio mode uses ServerCmd
	in := input{
		ServerCmd:  "python",
		ServerArgs: []string{"-m", "mcp_server"},
		ToolName:   "execute",
		ToolArgs:   map[string]any{"code": "print('hello')"},
	}

	assert.NotEmpty(t, in.ServerCmd)
	assert.Empty(t, in.ServerURL)
	assert.Equal(t, "execute", in.ToolName)
}

func TestInput_SSEMode(t *testing.T) {
	// SSE mode uses ServerURL
	in := input{
		ServerURL:     "http://localhost:3000/sse",
		ServerHeaders: map[string]string{"X-API-Key": "secret"},
		ToolName:      "list_files",
		ToolArgs:      map[string]any{"path": "/home"},
	}

	assert.Empty(t, in.ServerCmd)
	assert.NotEmpty(t, in.ServerURL)
	assert.Equal(t, "list_files", in.ToolName)
}

// Tests for arrayFlags type

func TestArrayFlags_String_Empty(t *testing.T) {
	var af arrayFlags
	assert.Equal(t, "", af.String())
}

func TestArrayFlags_String_Single(t *testing.T) {
	af := arrayFlags{"one"}
	assert.Equal(t, "one", af.String())
}

func TestArrayFlags_String_Multiple(t *testing.T) {
	af := arrayFlags{"one", "two", "three"}
	assert.Equal(t, "one two three", af.String())
}

func TestArrayFlags_Set_Single(t *testing.T) {
	var af arrayFlags
	err := af.Set("value1")
	assert.NoError(t, err)
	assert.Equal(t, arrayFlags{"value1"}, af)
}

func TestArrayFlags_Set_Multiple(t *testing.T) {
	var af arrayFlags
	err := af.Set("value1")
	assert.NoError(t, err)
	err = af.Set("value2")
	assert.NoError(t, err)
	err = af.Set("value3")
	assert.NoError(t, err)
	assert.Equal(t, arrayFlags{"value1", "value2", "value3"}, af)
}

func TestArrayFlags_Set_EmptyString(t *testing.T) {
	var af arrayFlags
	err := af.Set("")
	assert.NoError(t, err)
	assert.Equal(t, arrayFlags{""}, af)
}

func TestArrayFlags_Set_WithSpaces(t *testing.T) {
	var af arrayFlags
	err := af.Set("value with spaces")
	assert.NoError(t, err)
	assert.Equal(t, arrayFlags{"value with spaces"}, af)
}

func TestArrayFlags_ImplementsFlagValue(t *testing.T) {
	// Verify arrayFlags implements flag.Value interface
	var af arrayFlags
	// String() method
	_ = af.String()
	// Set() method
	_ = af.Set("test")
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		ServerCmd:     "full-cmd",
		ServerArgs:    []string{"arg1", "arg2", "arg3"},
		ServerEnv:     map[string]string{"VAR1": "val1", "VAR2": "val2"},
		ServerURL:     "http://full.url/sse",
		ServerHeaders: map[string]string{"H1": "v1", "H2": "v2"},
		ToolName:      "full_tool",
		ToolArgs:      map[string]any{"key1": "val1", "key2": 123},
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.ServerCmd, decoded.ServerCmd)
	assert.Equal(t, in.ServerArgs, decoded.ServerArgs)
	assert.Equal(t, in.ServerEnv, decoded.ServerEnv)
	assert.Equal(t, in.ServerURL, decoded.ServerURL)
	assert.Equal(t, in.ServerHeaders, decoded.ServerHeaders)
	assert.Equal(t, in.ToolName, decoded.ToolName)
	assert.Equal(t, in.ToolArgs["key1"], decoded.ToolArgs["key1"])
}

func TestInput_ToolArgsVariousTypes(t *testing.T) {
	in := input{
		ToolArgs: map[string]any{
			"string":  "text",
			"number":  42,
			"float":   3.14,
			"boolean": true,
			"array":   []any{"a", "b"},
			"object":  map[string]any{"nested": "value"},
		},
	}

	assert.Equal(t, "text", in.ToolArgs["string"])
	assert.Equal(t, 42, in.ToolArgs["number"])
	assert.Equal(t, 3.14, in.ToolArgs["float"])
	assert.Equal(t, true, in.ToolArgs["boolean"])
}

func TestInput_MultipleEnvVars(t *testing.T) {
	in := input{
		ServerEnv: map[string]string{
			"PATH":     "/usr/bin",
			"HOME":     "/home/user",
			"NODE_ENV": "development",
			"DEBUG":    "true",
			"API_KEY":  "secret123",
		},
	}

	assert.Len(t, in.ServerEnv, 5)
	assert.Equal(t, "/usr/bin", in.ServerEnv["PATH"])
	assert.Equal(t, "secret123", in.ServerEnv["API_KEY"])
}

func TestInput_MultipleHeaders(t *testing.T) {
	in := input{
		ServerHeaders: map[string]string{
			"Authorization": "Bearer token",
			"Content-Type":  "application/json",
			"X-Request-ID":  "123456",
		},
	}

	assert.Len(t, in.ServerHeaders, 3)
	assert.Equal(t, "Bearer token", in.ServerHeaders["Authorization"])
	assert.Equal(t, "application/json", in.ServerHeaders["Content-Type"])
}

func TestInput_EmptyServerArgs(t *testing.T) {
	in := input{
		ServerCmd:  "simple-server",
		ServerArgs: []string{},
		ToolName:   "ping",
	}

	assert.Empty(t, in.ServerArgs)
	assert.NotNil(t, in.ServerArgs)
}

func TestInput_NullVsEmptyMaps(t *testing.T) {
	// Empty maps
	in1 := input{
		ServerEnv:     map[string]string{},
		ServerHeaders: map[string]string{},
		ToolArgs:      map[string]any{},
	}
	assert.NotNil(t, in1.ServerEnv)
	assert.NotNil(t, in1.ServerHeaders)
	assert.NotNil(t, in1.ToolArgs)

	// Nil maps
	in2 := input{}
	assert.Nil(t, in2.ServerEnv)
	assert.Nil(t, in2.ServerHeaders)
	assert.Nil(t, in2.ToolArgs)
}

func TestArrayFlags_AppendPreservesOrder(t *testing.T) {
	var af arrayFlags
	_ = af.Set("first")
	_ = af.Set("second")
	_ = af.Set("third")

	assert.Equal(t, "first", af[0])
	assert.Equal(t, "second", af[1])
	assert.Equal(t, "third", af[2])
}

func TestArrayFlags_StringJoinsWithSpace(t *testing.T) {
	af := arrayFlags{"a", "b", "c"}
	result := af.String()

	// Verify space-joined
	assert.Equal(t, "a b c", result)
	assert.NotContains(t, result, ",")
}
