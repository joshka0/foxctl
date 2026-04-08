package eino

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEinoDependencyStrategy guards against machine-local absolute path replace directives
// being committed to go.mod. This ensures the gated Eino path stays CI-safe.
func TestEinoDependencyStrategy(t *testing.T) {
	// Find project root by looking for go.mod
	curr, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}

	root := ""
	temp := curr
	for {
		if _, err := os.Stat(filepath.Join(temp, "go.mod")); err == nil {
			root = temp
			break
		}
		parent := filepath.Dir(temp)
		if parent == temp {
			break
		}
		temp = parent
	}

	if root == "" {
		t.Fatalf("failed to find project root (go.mod) starting from %q", curr)
	}

	goModPath := filepath.Join(root, "go.mod")
	f, err := os.Open(goModPath)
	if err != nil {
		t.Fatalf("failed to open go.mod at %q: %v", goModPath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "replace ") {
			parts := strings.Split(line, "=>")
			if len(parts) == 2 {
				target := strings.TrimSpace(parts[1])
				// Check if the target is an absolute path (starts with / on Unix or C:\ on Windows)
				if filepath.IsAbs(target) {
					t.Errorf("go.mod line %d: absolute path replace directive found: %q. Use go.work for local iteration instead.", lineNum, line)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		t.Errorf("error reading go.mod: %v", err)
	}
}
