package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
)

func TestCollectFilesRejectsSymlinkEscapes(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	mustWriteFile(t, filepath.Join(workspace, "App.ts"), `const label = "safe";`)
	secretOutside := filepath.Join(outside, "Secret.ts")
	mustWriteFile(t, secretOutside, `const key = "sk_live_123456789012345678901234";`)
	if err := os.Symlink(secretOutside, filepath.Join(workspace, "Leaked.ts")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	files, err := collectFiles(workspace, workspace, validateUnderRoot(t, workspace), input{})
	if err != nil {
		t.Fatalf("collectFiles: %v", err)
	}

	if len(files) != 1 {
		t.Fatalf("file count=%d want 1: %#v", len(files), files)
	}
	if files[0].RelPath != "App.ts" {
		t.Fatalf("RelPath=%q want App.ts", files[0].RelPath)
	}
	if strings.Contains(strings.Join(files[0].Lines, "\n"), "sk_live_") {
		t.Fatal("collected content from symlink escape")
	}
}

func TestFilterBySeverityGeneratedOnlyKeepsAtOrAboveThreshold(t *testing.T) {
	thresholds := []string{"", "info", "low", "medium", "warn", "high", "critical", "unknown"}
	severities := []string{"info", "low", "medium", "warn", "high", "critical", "unknown", ""}

	err := quick.Check(func(thresholdIndex uint8, severityIndexes []uint8) bool {
		threshold := normalizeSeverity(thresholds[int(thresholdIndex)%len(thresholds)])
		items := make([]finding, 0, len(severityIndexes))
		for i, severityIndex := range severityIndexes {
			if i >= 64 {
				break
			}
			items = append(items, finding{Severity: severities[int(severityIndex)%len(severities)]})
		}

		got := filterBySeverity(items, threshold)
		min := severityScore(threshold)
		for _, item := range got {
			if severityScore(item.Severity) < min {
				return false
			}
		}
		return true
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func validateUnderRoot(t *testing.T, root string) func(string) (string, error) {
	t.Helper()
	rootEval, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("eval root symlinks: %v", err)
	}
	rootEval, err = filepath.Abs(rootEval)
	if err != nil {
		t.Fatalf("abs root: %v", err)
	}

	return func(candidate string) (string, error) {
		abs, err := filepath.Abs(candidate)
		if err != nil {
			return "", err
		}
		evaluated, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return "", err
		}
		rel, err := filepath.Rel(rootEval, evaluated)
		if err != nil {
			return "", err
		}
		if rel == ".." || strings.HasPrefix(filepath.ToSlash(rel), "../") {
			return "", fmt.Errorf("%s escapes %s", candidate, rootEval)
		}
		return evaluated, nil
	}
}
