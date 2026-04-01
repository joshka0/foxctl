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
	var hasStatus, hasSnapshot, hasDeps, hasChanges, hasHot, hasEvidence, hasScout, hasAdvisor bool
	for _, child := range cmd.Commands() {
		switch child.Name() {
		case "status":
			hasStatus = true
		case "snapshot":
			hasSnapshot = true
		case "deps":
			hasDeps = true
		case "changes":
			hasChanges = true
		case "hot":
			hasHot = true
		case "evidence":
			hasEvidence = true
		case "scout":
			hasScout = true
		case "advisor":
			hasAdvisor = true
		}
	}
	if !hasStatus || !hasSnapshot || !hasDeps || !hasChanges || !hasHot || !hasEvidence || !hasScout || !hasAdvisor {
		t.Fatalf("expected status/snapshot/deps/changes/hot/evidence/scout/advisor subcommands, got status=%v snapshot=%v deps=%v changes=%v hot=%v evidence=%v scout=%v advisor=%v", hasStatus, hasSnapshot, hasDeps, hasChanges, hasHot, hasEvidence, hasScout, hasAdvisor)
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

func TestRefactorDepsCommandDefaults(t *testing.T) {
	cmd := newRefactorDepsCommand()
	if flag := cmd.Flags().Lookup("direction"); flag == nil {
		t.Fatal("deps command missing direction flag")
	} else if flag.DefValue != "out" {
		t.Fatalf("deps direction default=%q want out", flag.DefValue)
	}
	if flag := cmd.Flags().Lookup("edge-set"); flag == nil {
		t.Fatal("deps command missing edge-set flag")
	} else if flag.DefValue != "[structural]" {
		t.Fatalf("deps edge-set default=%q want [structural]", flag.DefValue)
	}
}

func TestRefactorChangesCommandDefaults(t *testing.T) {
	cmd := newRefactorChangesCommand()
	if flag := cmd.Flags().Lookup("since"); flag == nil {
		t.Fatal("changes command missing since flag")
	} else if flag.DefValue != "" {
		t.Fatalf("changes since default=%q want empty", flag.DefValue)
	}
	if flag := cmd.Flags().Lookup("max-files"); flag == nil {
		t.Fatal("changes command missing max-files flag")
	} else if flag.DefValue != "200" {
		t.Fatalf("changes max-files default=%q want 200", flag.DefValue)
	}
}

func TestRefactorHotCommandDefaults(t *testing.T) {
	cmd := newRefactorHotCommand()
	if flag := cmd.Flags().Lookup("since"); flag == nil {
		t.Fatal("hot command missing since flag")
	} else if flag.DefValue != "HEAD~20" {
		t.Fatalf("hot since default=%q want HEAD~20", flag.DefValue)
	}
	if flag := cmd.Flags().Lookup("max-results"); flag == nil {
		t.Fatal("hot command missing max-results flag")
	} else if flag.DefValue != "20" {
		t.Fatalf("hot max-results default=%q want 20", flag.DefValue)
	}
}

func TestRefactorEvidenceCommandDefaults(t *testing.T) {
	cmd := newRefactorEvidenceCommand()
	if flag := cmd.Flags().Lookup("artifact"); flag == nil {
		t.Fatal("evidence command missing artifact flag")
	} else if flag.DefValue != "" {
		t.Fatalf("evidence artifact default=%q want empty", flag.DefValue)
	}
	if flag := cmd.Flags().Lookup("snapshot-id"); flag == nil {
		t.Fatal("evidence command missing snapshot-id flag")
	} else if flag.DefValue != "" {
		t.Fatalf("evidence snapshot-id default=%q want empty", flag.DefValue)
	}
	if flag := cmd.Flags().Lookup("full"); flag == nil {
		t.Fatal("evidence command missing full flag")
	} else if flag.DefValue != "false" {
		t.Fatalf("evidence full default=%q want false", flag.DefValue)
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
