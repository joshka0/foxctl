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
	var hasStatus, hasSnapshot, hasScout, hasAdvisor bool
	for _, child := range cmd.Commands() {
		switch child.Name() {
		case "status":
			hasStatus = true
		case "snapshot":
			hasSnapshot = true
		case "scout":
			hasScout = true
		case "advisor":
			hasAdvisor = true
		}
	}
	if !hasStatus || !hasSnapshot || !hasScout || !hasAdvisor {
		t.Fatalf("expected status/snapshot/scout/advisor subcommands, got status=%v snapshot=%v scout=%v advisor=%v", hasStatus, hasSnapshot, hasScout, hasAdvisor)
	}
}

func TestRefactorStatusCommandDefaults(t *testing.T) {
	cmd := newRefactorStatusCommand()
	if flag := cmd.Flags().Lookup("language"); flag == nil {
		t.Fatal("status command missing language flag")
	} else if flag.DefValue != "auto" {
		t.Fatalf("status language default=%q want auto", flag.DefValue)
	}
	if flag := cmd.Flags().Lookup("workspace"); flag == nil {
		t.Fatal("status command missing workspace flag")
	} else if flag.DefValue != "." {
		t.Fatalf("status workspace default=%q want .", flag.DefValue)
	}
}

func TestRefactorSnapshotCommandDefaults(t *testing.T) {
	cmd := newRefactorSnapshotCommand()
	if flag := cmd.Flags().Lookup("language"); flag == nil {
		t.Fatal("snapshot command missing language flag")
	} else if flag.DefValue != "auto" {
		t.Fatalf("snapshot language default=%q want auto", flag.DefValue)
	}
	if flag := cmd.Flags().Lookup("workspace"); flag == nil {
		t.Fatal("snapshot command missing workspace flag")
	} else if flag.DefValue != "." {
		t.Fatalf("snapshot workspace default=%q want .", flag.DefValue)
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

func TestRefactorCommandsExposeFocusFlag(t *testing.T) {
	for _, tc := range []struct {
		name string
		cmd  *cobra.Command
	}{
		{name: "scout", cmd: newRefactorScoutCommand()},
		{name: "advisor", cmd: newRefactorAdvisorCommand()},
	} {
		flag := tc.cmd.Flags().Lookup("focus")
		if flag == nil {
			t.Fatalf("%s command missing focus flag", tc.name)
		}
		if flag.DefValue != "all" {
			t.Fatalf("%s focus default=%q want all", tc.name, flag.DefValue)
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
