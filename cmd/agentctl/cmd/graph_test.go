package cmd

import "testing"

func TestNewGraphCommand(t *testing.T) {
	cmd := newGraphCommand()
	if cmd.Use != "graph" {
		t.Fatalf("expected use graph, got %s", cmd.Use)
	}
	subs := cmd.Commands()
	expected := []string{"stats", "top", "repair", "edges"}
	if len(subs) != len(expected) {
		t.Fatalf("expected %d subcommands, got %d", len(expected), len(subs))
	}
	got := map[string]bool{}
	for _, sub := range subs {
		got[sub.Use] = true
	}
	for _, name := range expected {
		if !got[name] {
			t.Fatalf("expected subcommand %s to exist", name)
		}
	}
}

func TestGraphStatsFlags(t *testing.T) {
	cmd := newGraphStatsCommand()
	if cmd.Use != "stats" {
		t.Fatalf("expected stats command, got %s", cmd.Use)
	}
	for _, flag := range []string{"workspace", "json"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("expected flag --%s", flag)
		}
	}
}

func TestGraphTopFlags(t *testing.T) {
	cmd := newGraphTopCommand()
	if cmd.Use != "top" {
		t.Fatalf("expected top command, got %s", cmd.Use)
	}
	for _, flag := range []string{"workspace", "type", "limit", "min-rank", "json"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("expected flag --%s", flag)
		}
	}
}

func TestGraphRepairFlags(t *testing.T) {
	cmd := newGraphRepairCommand()
	if cmd.Use != "repair" {
		t.Fatalf("expected repair command, got %s", cmd.Use)
	}
	for _, flag := range []string{"workspace", "dry-run", "json"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("expected flag --%s", flag)
		}
	}
}

func TestGraphEdgesFlags(t *testing.T) {
	cmd := newGraphEdgesCommand()
	if cmd.Use != "edges" {
		t.Fatalf("expected edges command, got %s", cmd.Use)
	}
	for _, flag := range []string{"workspace", "node", "direction", "type", "json"} {
		if cmd.Flags().Lookup(flag) == nil {
			t.Fatalf("expected flag --%s", flag)
		}
	}
}

func TestResolveGraphWorkspace(t *testing.T) {
	tests := []struct {
		name      string
		workspace string
		wantEmpty bool
	}{
		{name: "explicit workspace", workspace: "/some/path", wantEmpty: false},
		{name: "empty uses cwd", workspace: "", wantEmpty: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolveGraphWorkspace(tt.workspace)
			if tt.wantEmpty && result != "" {
				t.Errorf("expected empty result, got %q", result)
			}
			if !tt.wantEmpty && result == "" {
				t.Errorf("expected non-empty result")
			}
			if tt.workspace != "" && result != tt.workspace {
				t.Errorf("expected %q, got %q", tt.workspace, result)
			}
		})
	}
}

func TestTruncateID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{name: "short ID", input: "abc123", expected: "abc123"},
		{name: "exactly 40", input: "1234567890123456789012345678901234567890", expected: "1234567890123456789012345678901234567890"},
		{name: "over 40", input: "12345678901234567890123456789012345678901", expected: "1234567890123456789012345678901234567..."},
		{name: "long ID", input: "this-is-a-very-long-id-that-should-be-truncated-at-some-point", expected: "this-is-a-very-long-id-that-should-be..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateID(tt.input)
			if got != tt.expected {
				t.Errorf("truncateID(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestFormatWeight(t *testing.T) {
	tests := []struct {
		name     string
		weight   float64
		expected string
	}{
		{name: "weight 1.0", weight: 1.0, expected: ""},
		{name: "weight 0.5", weight: 0.5, expected: "(w=0.50)"},
		{name: "weight 2.5", weight: 2.5, expected: "(w=2.50)"},
		{name: "weight 0.0", weight: 0.0, expected: "(w=0.00)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatWeight(tt.weight)
			if got != tt.expected {
				t.Errorf("formatWeight(%v) = %q, want %q", tt.weight, got, tt.expected)
			}
		})
	}
}
