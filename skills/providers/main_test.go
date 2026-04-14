package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skilltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func newTestContext(t *testing.T, buf *bytes.Buffer, homeDir string) func() {
	t.Helper()

	// Save original HOME
	origHome := os.Getenv("HOME")

	// Set HOME to temp directory
	os.Setenv("HOME", homeDir)

	return func() {
		os.Setenv("HOME", origHome)
	}
}

func decodeEnvelope(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var env map[string]any
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nbuffer: %s", err, buf.String())
	}
	return env
}

func assertOK(t *testing.T, env map[string]any) {
	t.Helper()
	if env["status"] != "ok" {
		errField := env["error"]
		t.Fatalf("expected ok status, got %v (error: %v)", env["status"], errField)
	}
}

func getData(t *testing.T, env map[string]any) map[string]any {
	t.Helper()
	data, ok := env["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be map, got %T", env["data"])
	}
	return data
}

func writeClaudeConfig(t *testing.T, homeDir string, cfg map[string]any) string {
	t.Helper()
	path := filepath.Join(homeDir, ".claude.json")
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
	return path
}

func readClaudeConfig(t *testing.T, homeDir string) map[string]any {
	t.Helper()
	path := filepath.Join(homeDir, ".claude.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var cfg map[string]any
	require.NoError(t, json.Unmarshal(data, &cfg))
	return cfg
}

// Tests for validation

func TestProviders_MissingOperation(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	in := input{}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "operation is required")
}

func TestProviders_InvalidOperation(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	in := input{
		Operation: "invalid",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid operation")
}

// Tests for list operation

func TestProviders_List(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create a Claude config to make it "installed"
	writeClaudeConfig(t, homeDir, map[string]any{
		"mcpServers": map[string]any{
			"test-mcp": map[string]any{
				"command": "test",
			},
		},
	})

	in := input{
		Operation: "list",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	providers, ok := data["providers"].([]any)
	require.True(t, ok, "providers should be array")
	assert.GreaterOrEqual(t, len(providers), 1)

	// Find claude provider
	var foundClaude bool
	for _, p := range providers {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if pm["name"] == "claude" {
			foundClaude = true
			assert.True(t, pm["config_exists"].(bool))
			assert.Equal(t, float64(1), pm["mcp_count"])
		}
	}
	assert.True(t, foundClaude, "claude provider should be in list")
}

// Tests for get operation

func TestProviders_GetConfig(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create config
	writeClaudeConfig(t, homeDir, map[string]any{
		"key": "value",
		"nested": map[string]any{
			"subkey": "subvalue",
		},
	})

	in := input{
		Operation: "get",
		Provider:  "claude",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	cfg, ok := data["config"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "value", cfg["key"])
}

func TestProviders_GetConfig_NotFound(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	in := input{
		Operation: "get",
		Provider:  "claude",
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	cfg, ok := data["config"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "config not found", cfg["error"])
}

func TestProviders_GetConfig_UnknownProvider(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	in := input{
		Operation: "get",
		Provider:  "unknown",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider")
}

// Tests for set operation

func TestProviders_SetConfig(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create initial config
	writeClaudeConfig(t, homeDir, map[string]any{})

	in := input{
		Operation: "set",
		Provider:  "claude",
		Setting: &settingConfig{
			Key:   "customSetting",
			Value: "customValue",
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	changes, ok := data["changes"].([]any)
	require.True(t, ok)
	require.Len(t, changes, 1)
	change := changes[0].(map[string]any)
	assert.Equal(t, "set", change["type"])
	assert.True(t, change["applied"].(bool))

	// Verify config was updated
	cfg := readClaudeConfig(t, homeDir)
	assert.Equal(t, "customValue", cfg["customSetting"])
}

func TestProviders_SetConfig_Nested(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create initial config
	writeClaudeConfig(t, homeDir, map[string]any{})

	in := input{
		Operation: "set",
		Provider:  "claude",
		Setting: &settingConfig{
			Key:   "nested.key.value",
			Value: 42,
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)

	// Verify nested structure
	cfg := readClaudeConfig(t, homeDir)
	nested := cfg["nested"].(map[string]any)
	key := nested["key"].(map[string]any)
	assert.Equal(t, float64(42), key["value"])
}

func TestProviders_SetConfig_DryRun(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create initial config
	writeClaudeConfig(t, homeDir, map[string]any{})

	in := input{
		Operation: "set",
		Provider:  "claude",
		Setting: &settingConfig{
			Key:   "dryRunKey",
			Value: "dryRunValue",
		},
		DryRun: true,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	changes := data["changes"].([]any)
	require.Len(t, changes, 1)
	change := changes[0].(map[string]any)
	assert.False(t, change["applied"].(bool))

	// Verify config was NOT updated
	cfg := readClaudeConfig(t, homeDir)
	_, exists := cfg["dryRunKey"]
	assert.False(t, exists)
}

func TestProviders_SetConfig_MissingSetting(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	in := input{
		Operation: "set",
		Provider:  "claude",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "setting is required")
}

// Tests for add-mcp operation

func TestProviders_AddMCP(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create initial config
	writeClaudeConfig(t, homeDir, map[string]any{})

	in := input{
		Operation: "add-mcp",
		Provider:  "claude",
		MCP: &mcpConfig{
			Name:    "test-server",
			Command: "/path/to/server",
			Args:    []string{"--port", "3000"},
			Env:     map[string]string{"API_KEY": "secret"},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	changes := data["changes"].([]any)
	require.Len(t, changes, 1)
	change := changes[0].(map[string]any)
	assert.Equal(t, "add_mcp", change["type"])
	assert.Equal(t, "test-server", change["target"])
	assert.True(t, change["applied"].(bool))

	// Verify config
	cfg := readClaudeConfig(t, homeDir)
	mcpServers := cfg["mcpServers"].(map[string]any)
	server := mcpServers["test-server"].(map[string]any)
	assert.Equal(t, "/path/to/server", server["command"])
	args := server["args"].([]any)
	assert.Len(t, args, 2)
}

func TestProviders_AddMCP_DryRun(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create initial config
	writeClaudeConfig(t, homeDir, map[string]any{})

	in := input{
		Operation: "add-mcp",
		Provider:  "claude",
		MCP: &mcpConfig{
			Name:    "dry-run-server",
			Command: "/path/to/server",
		},
		DryRun: true,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	changes := data["changes"].([]any)
	require.Len(t, changes, 1)
	change := changes[0].(map[string]any)
	assert.False(t, change["applied"].(bool))

	// Verify config was NOT updated
	cfg := readClaudeConfig(t, homeDir)
	_, exists := cfg["mcpServers"]
	assert.False(t, exists)
}

func TestProviders_AddMCP_MissingName(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	in := input{
		Operation: "add-mcp",
		Provider:  "claude",
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mcp is required")
}

// Tests for remove-mcp operation

func TestProviders_RemoveMCP(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create config with MCP server
	writeClaudeConfig(t, homeDir, map[string]any{
		"mcpServers": map[string]any{
			"to-remove": map[string]any{
				"command": "test",
			},
			"to-keep": map[string]any{
				"command": "test2",
			},
		},
	})

	in := input{
		Operation: "remove-mcp",
		Provider:  "claude",
		MCP: &mcpConfig{
			Name: "to-remove",
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	changes := data["changes"].([]any)
	require.Len(t, changes, 1)
	change := changes[0].(map[string]any)
	assert.Equal(t, "remove_mcp", change["type"])
	assert.True(t, change["applied"].(bool))

	// Verify config
	cfg := readClaudeConfig(t, homeDir)
	mcpServers := cfg["mcpServers"].(map[string]any)
	_, exists := mcpServers["to-remove"]
	assert.False(t, exists)
	_, exists = mcpServers["to-keep"]
	assert.True(t, exists)
}

// Tests for add-skill operation

func TestProviders_AddSkill(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create source skill directory
	sourceDir := t.TempDir()
	sourceSkill := filepath.Join(sourceDir, "my-skill")
	require.NoError(t, os.MkdirAll(sourceSkill, 0o755))

	in := input{
		Operation: "add-skill",
		Provider:  "claude",
		Skill: &skillConfig{
			Name:   "my-skill",
			Source: sourceSkill,
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	changes := data["changes"].([]any)
	require.Len(t, changes, 1)
	change := changes[0].(map[string]any)
	assert.Equal(t, "add_skill", change["type"])
	assert.True(t, change["applied"].(bool))

	// Verify symlink was created
	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	target, err := os.Readlink(filepath.Join(skillsDir, "my-skill"))
	require.NoError(t, err)
	assert.Equal(t, sourceSkill, target)
}

func TestProviders_AddSkill_MissingSource(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	in := input{
		Operation: "add-skill",
		Provider:  "claude",
		Skill: &skillConfig{
			Name: "my-skill",
			// Missing source
		},
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source are required")
}

// Tests for remove-skill operation

func TestProviders_RemoveSkill(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create skills directory with a skill
	skillsDir := filepath.Join(homeDir, ".claude", "skills")
	require.NoError(t, os.MkdirAll(filepath.Join(skillsDir, "to-remove"), 0o755))

	in := input{
		Operation: "remove-skill",
		Provider:  "claude",
		Skill: &skillConfig{
			Name: "to-remove",
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	changes := data["changes"].([]any)
	require.Len(t, changes, 1)
	change := changes[0].(map[string]any)
	assert.Equal(t, "remove_skill", change["type"])
	assert.True(t, change["applied"].(bool))

	// Verify skill was removed
	_, err = os.Stat(filepath.Join(skillsDir, "to-remove"))
	assert.True(t, os.IsNotExist(err))
}

// Tests for export operation

func TestProviders_Export(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create config
	writeClaudeConfig(t, homeDir, map[string]any{
		"key": "exportedValue",
	})

	exportPath := filepath.Join(t.TempDir(), "export.json")

	in := input{
		Operation: "export",
		Provider:  "claude",
		File:      exportPath,
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	changes := data["changes"].([]any)
	require.Len(t, changes, 1)
	change := changes[0].(map[string]any)
	assert.Equal(t, "export", change["type"])
	assert.True(t, change["applied"].(bool))

	// Verify export file
	exportData, err := os.ReadFile(exportPath)
	require.NoError(t, err)
	var exported map[string]any
	require.NoError(t, json.Unmarshal(exportData, &exported))
	assert.Equal(t, "exportedValue", exported["key"])
}

// Tests for import operation

func TestProviders_Import(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create import file
	importPath := filepath.Join(t.TempDir(), "import.json")
	importData := map[string]any{
		"imported": true,
		"count":    42,
	}
	data, err := json.Marshal(importData)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(importPath, data, 0o644))

	in := input{
		Operation: "import",
		Provider:  "claude",
		File:      importPath,
	}

	err = run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	envData := getData(t, env)

	changes := envData["changes"].([]any)
	require.Len(t, changes, 1)
	change := changes[0].(map[string]any)
	assert.Equal(t, "import", change["type"])
	assert.True(t, change["applied"].(bool))

	// Verify config was imported
	cfg := readClaudeConfig(t, homeDir)
	assert.True(t, cfg["imported"].(bool))
	assert.Equal(t, float64(42), cfg["count"])
}

func TestProviders_Import_DryRun(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create import file
	importPath := filepath.Join(t.TempDir(), "import.json")
	importData := map[string]any{"dryrun": true}
	data, err := json.Marshal(importData)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(importPath, data, 0o644))

	in := input{
		Operation: "import",
		Provider:  "claude",
		File:      importPath,
		DryRun:    true,
	}

	err = run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	envData := getData(t, env)

	changes := envData["changes"].([]any)
	require.Len(t, changes, 1)
	change := changes[0].(map[string]any)
	assert.False(t, change["applied"].(bool))

	// Config should not exist
	_, err = os.Stat(filepath.Join(homeDir, ".claude.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestProviders_Import_MissingFile(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	in := input{
		Operation: "import",
		Provider:  "claude",
		// Missing file
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "file is required")
}

// Tests for sync operation

func TestProviders_Sync_MCP(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create source Claude config with MCP server
	writeClaudeConfig(t, homeDir, map[string]any{
		"mcpServers": map[string]any{
			"shared-server": map[string]any{
				"command": "/path/to/shared",
				"args":    []string{"--shared"},
			},
		},
	})

	// Create target OpenCode config directory
	opencodeDir := filepath.Join(homeDir, ".config", "opencode")
	require.NoError(t, os.MkdirAll(opencodeDir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(opencodeDir, "opencode.json"),
		[]byte("{}"),
		0o644,
	))

	in := input{
		Operation: "sync",
		SyncConfig: &syncConfig{
			From: "claude",
			To:   []string{"opencode"},
			What: []string{"mcp"},
		},
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	changes := data["changes"].([]any)
	require.GreaterOrEqual(t, len(changes), 1)

	// Verify MCP was synced to OpenCode
	opencodeData, err := os.ReadFile(filepath.Join(opencodeDir, "opencode.json"))
	require.NoError(t, err)
	var opencodeCfg map[string]any
	require.NoError(t, json.Unmarshal(opencodeData, &opencodeCfg))

	mcpServers, ok := opencodeCfg["mcpServers"].(map[string]any)
	require.True(t, ok)
	_, exists := mcpServers["shared-server"]
	assert.True(t, exists)
}

func TestProviders_Sync_MissingConfig(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	in := input{
		Operation:  "sync",
		SyncConfig: &syncConfig{},
	}

	err := run(context.Background(), rc, in)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sync_config is required")
}

// Tests for default provider

func TestProviders_DefaultsToClaudeProvider(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	// Create Claude config
	writeClaudeConfig(t, homeDir, map[string]any{
		"default": true,
	})

	in := input{
		Operation: "get",
		// No provider specified - should default to claude
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	cfg := data["config"].(map[string]any)
	assert.True(t, cfg["default"].(bool))
}

// Tests for "all" provider target

func TestProviders_AddMCP_AllProviders(t *testing.T) {
	var buf bytes.Buffer
	homeDir := t.TempDir()
	rc, rcCleanup := skilltest.NewTestRunContext(t, &buf, nil)
	defer rcCleanup()
	homeCleanup := newTestContext(t, &buf, homeDir)
	defer homeCleanup()

	in := input{
		Operation: "add-mcp",
		Provider:  "all",
		MCP: &mcpConfig{
			Name:    "global-server",
			Command: "/path/to/global",
		},
		DryRun: true, // Use dry run to avoid file creation issues
	}

	err := run(context.Background(), rc, in)
	require.NoError(t, err)

	env := decodeEnvelope(t, &buf)
	assertOK(t, env)
	data := getData(t, env)

	changes := data["changes"].([]any)
	// Should have changes for multiple providers
	assert.GreaterOrEqual(t, len(changes), 2)
}
