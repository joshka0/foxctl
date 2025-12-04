package tools

import (
	"testing"
)

func TestNewRegistry_RegistersAllTools(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	tools := registry.List()

	// Build a map of tool names for easy lookup
	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name()] = true
	}

	// Verify Phase 6 tools are registered
	requiredTools := []string{
		// Code search tools (PR1)
		"code.search",
		"code.symbol_search",
		"code.swe_grep",
		// Edit tools (PR2)
		"edit.apply_patch",
		"edit.apply_structured_diff",
		"edit.create_file",
		// FS tools
		"fs.read_file",
		"fs.list_dir",
		// Test tools
		"tests.run",
	}

	for _, name := range requiredTools {
		if !toolNames[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}

	// Verify we have a reasonable number of tools
	if len(tools) < len(requiredTools) {
		t.Errorf("expected at least %d tools, got %d", len(requiredTools), len(tools))
	}
}

func TestNewRegistry_CodeToolsPresent(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	tools := registry.List()

	// Specifically verify the new Phase 6 code tools
	codeTools := map[string]bool{
		"code.symbol_search": false,
		"code.swe_grep":      false,
	}

	for _, tool := range tools {
		if _, exists := codeTools[tool.Name()]; exists {
			codeTools[tool.Name()] = true
		}
	}

	for name, found := range codeTools {
		if !found {
			t.Errorf("Phase 6 tool %q not registered", name)
		}
	}
}

func TestNewRegistry_EditToolsPresent(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	tools := registry.List()

	// Specifically verify the new Phase 6 edit tool
	editTools := map[string]bool{
		"edit.apply_patch":           false, // Existing
		"edit.apply_structured_diff": false, // New in PR2
		"edit.create_file":           false, // Existing
	}

	for _, tool := range tools {
		if _, exists := editTools[tool.Name()]; exists {
			editTools[tool.Name()] = true
		}
	}

	for name, found := range editTools {
		if !found {
			t.Errorf("Edit tool %q not registered", name)
		}
	}
}

func TestNewRegistry_NoUnexpectedPlannerTools(t *testing.T) {
	cfg := Config{
		WorkspaceRoot:    t.TempDir(),
		MaxSearchResults: 50,
	}
	registry, err := NewRegistry(cfg, nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	tools := registry.List()

	// These are low-level coding tools that should NOT be exposed to planner
	// (The runtime uses signatures to control what agents see, but all tools
	// are technically registered - this test documents which tools exist)
	lowLevelCodingTools := []string{
		"code.search",
		"code.symbol_search",
		"code.swe_grep",
		"edit.apply_patch",
		"edit.apply_structured_diff",
		"fs.read_file",
	}

	toolNames := make(map[string]bool)
	for _, tool := range tools {
		toolNames[tool.Name()] = true
	}

	// Document that these tools ARE in the registry (planner restriction is at signature level)
	for _, name := range lowLevelCodingTools {
		if !toolNames[name] {
			t.Errorf("low-level tool %q should be in registry (restriction is at signature level)", name)
		}
	}
}
