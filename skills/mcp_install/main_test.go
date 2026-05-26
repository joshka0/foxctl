package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestInput_StdioMode(t *testing.T) {
	// Stdio mode uses ServerCmd
	in := input{
		ServerCmd:  "python",
		ServerArgs: []string{"-m", "mcp_server"},
		OutputDir:  "./skills",
	}

	assert.NotEmpty(t, in.ServerCmd)
	assert.Empty(t, in.ServerURL)
}

func TestInput_SSEMode(t *testing.T) {
	// SSE mode uses ServerURL
	in := input{
		ServerURL:     "http://localhost:3000/sse",
		ServerHeaders: map[string]string{"X-API-Key": "secret"},
		OutputDir:     "./skills",
	}

	assert.Empty(t, in.ServerCmd)
	assert.NotEmpty(t, in.ServerURL)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		ServerCmd:     "full-cmd",
		ServerArgs:    []string{"arg1", "arg2"},
		ServerEnv:     map[string]string{"VAR1": "val1", "VAR2": "val2"},
		ServerURL:     "http://full.url/sse",
		ServerHeaders: map[string]string{"H1": "v1", "H2": "v2"},
		OutputDir:     "/full/output/dir",
		BridgePath:    "/full/bridge/path",
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
	assert.Equal(t, in.OutputDir, decoded.OutputDir)
	assert.Equal(t, in.BridgePath, decoded.BridgePath)
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
}

func TestInput_MultipleHeaders(t *testing.T) {
	in := input{
		ServerHeaders: map[string]string{
			"Authorization": "Bearer token",
			"Content-Type":  "application/json",
			"X-Custom":      "value",
		},
	}

	assert.Len(t, in.ServerHeaders, 3)
	assert.Equal(t, "Bearer token", in.ServerHeaders["Authorization"])
}

func TestInput_EmptyServerArgs(t *testing.T) {
	in := input{
		ServerCmd:  "simple-server",
		ServerArgs: []string{},
	}

	assert.Empty(t, in.ServerArgs)
	assert.NotNil(t, in.ServerArgs)
}

func TestInput_DefaultBridgePath(t *testing.T) {
	// Default is empty in struct, parseInput sets it to "mcp_bridge"
	in := input{
		ServerCmd: "npx",
	}

	assert.Empty(t, in.BridgePath)
}

func TestInput_OutputDirDefault(t *testing.T) {
	// Default is "." when empty, set in run()
	in := input{
		ServerCmd: "test",
	}

	assert.Empty(t, in.OutputDir)
}

func TestInput_ComplexServerArgs(t *testing.T) {
	in := input{
		ServerCmd: "npx",
		ServerArgs: []string{
			"-y",
			"@modelcontextprotocol/server-sqlite",
			"/path/to/database.db",
			"--verbose",
		},
	}

	assert.Len(t, in.ServerArgs, 4)
	assert.Contains(t, in.ServerArgs, "--verbose")
}
