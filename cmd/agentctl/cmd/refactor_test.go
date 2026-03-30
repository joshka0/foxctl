package cmd

import (
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

func TestNewRefactorCommandHasSubcommands(t *testing.T) {
	cmd := newRefactorCommand()
	if cmd.Use != "refactor" {
		t.Fatalf("Use=%q", cmd.Use)
	}
	var hasScout, hasAdvisor bool
	for _, child := range cmd.Commands() {
		switch child.Name() {
		case "scout":
			hasScout = true
		case "advisor":
			hasAdvisor = true
		}
	}
	if !hasScout || !hasAdvisor {
		t.Fatalf("expected scout and advisor subcommands, got scout=%v advisor=%v", hasScout, hasAdvisor)
	}
}

func TestRefactorCommandsRequireLanguage(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "scout", cmd: newRefactorScoutCommand()},
		{name: "advisor", cmd: newRefactorAdvisorCommand()},
	} {
		if flag := tc.cmd.Flags().Lookup("language"); flag == nil {
			t.Fatalf("%s command missing language flag", tc.name)
		}
	}
}

func TestRefactorEntryRootsIncludesRepoRootForBundledSkill(t *testing.T) {
	cwd := "/tmp/workspace"
	manifestPath := filepath.Join("/repo", "skills", "code_refactor_scout", "skill.yaml")
	roots := refactorEntryRoots(manifestPath, cwd)
	if len(roots) < 2 {
		t.Fatalf("expected cwd and repo root, got %#v", roots)
	}
	if roots[0] != filepath.Clean(cwd) {
		t.Fatalf("expected cwd first, got %#v", roots)
	}
	if roots[1] != filepath.Clean("/repo") {
		t.Fatalf("expected repo root second, got %#v", roots)
	}
}
