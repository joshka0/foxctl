package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAgentSets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"empty", "", []string{"core"}},
		{"core", "core", []string{"core"}},
		{"factory", "factory", []string{"factory"}},
		{"all", "all", []string{"all"}},
		{"multiple", "core,factory", []string{"core", "factory"}},
		{"with spaces", " core , factory ", []string{"core", "factory"}},
		{"uppercase", "CORE,FACTORY", []string{"core", "factory"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseAgentSets(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("parseAgentSets(%q) = %v, want %v", tt.input, result, tt.expected)
				return
			}
			for i, v := range result {
				if v != tt.expected[i] {
					t.Errorf("parseAgentSets(%q)[%d] = %q, want %q", tt.input, i, v, tt.expected[i])
				}
			}
		})
	}
}

func TestCollectAgents(t *testing.T) {
	tests := []struct {
		name        string
		sets        []string
		expectError bool
		minCount    int
	}{
		{"core only", []string{"core"}, false, 4},
		{"factory only", []string{"factory"}, false, 3},
		{"all", []string{"all"}, false, 7},
		{"core and factory", []string{"core", "factory"}, false, 7},
		{"invalid set", []string{"invalid"}, true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents, err := collectAgents(tt.sets)
			if tt.expectError {
				if err == nil {
					t.Errorf("collectAgents(%v) expected error, got nil", tt.sets)
				}
				return
			}
			if err != nil {
				t.Errorf("collectAgents(%v) error = %v", tt.sets, err)
				return
			}
			if len(agents) < tt.minCount {
				t.Errorf("collectAgents(%v) returned %d agents, want at least %d", tt.sets, len(agents), tt.minCount)
			}
		})
	}
}

func TestInitWorkspace(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Collect core agents
	agents, err := collectAgents([]string{"core"})
	if err != nil {
		t.Fatalf("collectAgents error: %v", err)
	}

	// Test dry run
	result, err := initWorkspace(tmpDir, agents, false, true)
	if err != nil {
		t.Fatalf("initWorkspace (dry-run) error: %v", err)
	}

	if result["dry_run"] != true {
		t.Error("expected dry_run to be true")
	}

	created := result["created"].([]string)
	if len(created) != len(agents) {
		t.Errorf("dry-run created %d, want %d", len(created), len(agents))
	}

	// Verify no files were actually created
	agentsDir := filepath.Join(tmpDir, ".claude", "agents")
	if _, err := os.Stat(agentsDir); !os.IsNotExist(err) {
		t.Error("dry-run should not create directories")
	}

	// Test actual init
	result, err = initWorkspace(tmpDir, agents, false, false)
	if err != nil {
		t.Fatalf("initWorkspace error: %v", err)
	}

	created = result["created"].([]string)
	if len(created) != len(agents) {
		t.Errorf("created %d, want %d", len(created), len(agents))
	}

	// Verify files exist
	for _, filename := range created {
		path := filepath.Join(agentsDir, filename)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", path)
		}
	}

	// Test re-init without force (should skip)
	result, err = initWorkspace(tmpDir, agents, false, false)
	if err != nil {
		t.Fatalf("initWorkspace (re-init) error: %v", err)
	}

	skipped := result["skipped"].([]string)
	if len(skipped) != len(agents) {
		t.Errorf("skipped %d, want %d", len(skipped), len(agents))
	}

	// Test re-init with force (should overwrite)
	result, err = initWorkspace(tmpDir, agents, true, false)
	if err != nil {
		t.Fatalf("initWorkspace (force) error: %v", err)
	}

	created = result["created"].([]string)
	if len(created) != len(agents) {
		t.Errorf("force created %d, want %d", len(created), len(agents))
	}
}
