package evals

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type gatherContextFixtureCase struct {
	ID            string   `json:"id"`
	ExpectedPaths []string `json:"expected_paths"`
}

func TestGatherContextFoxctlRepoGroundedExpectedPathsExist(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..")
	fixturePath := filepath.Join(repoRoot, "testdata", "evals", "gather-context", "foxctl-repo-grounded.jsonl")

	file, err := os.Open(fixturePath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var evalCase gatherContextFixtureCase
		if err := json.Unmarshal([]byte(line), &evalCase); err != nil {
			t.Fatalf("parse fixture line %d: %v", lineNo, err)
		}
		for _, relPath := range evalCase.ExpectedPaths {
			relPath = strings.TrimSpace(relPath)
			if relPath == "" {
				t.Errorf("case %q line %d has empty expected path", evalCase.ID, lineNo)
				continue
			}
			if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(relPath))); err != nil {
				t.Errorf("case %q line %d expected path %q does not exist: %v", evalCase.ID, lineNo, relPath, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan fixture: %v", err)
	}
}
