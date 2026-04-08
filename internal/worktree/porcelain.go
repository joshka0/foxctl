package worktree

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ParsePorcelain parses the output of `git worktree list --porcelain`.
//
// The porcelain format consists of records separated by blank lines.
// Each record has these fields:
//
//	worktree <path>           (required)
//	HEAD <sha>                (required)
//	branch <refs/heads/name>  (optional, absent for detached HEAD)
//	bare                      (optional flag)
//	locked [reason]           (optional, may have a reason)
//	prunable [reason]         (optional, may have a reason)
//
// Lines after a blank line start a new record.
func ParsePorcelain(output string) ([]WorktreeEntry, error) {
	var entries []WorktreeEntry

	// Split into records by blank lines
	blocks := splitPorcelainBlocks(output)

	for _, block := range blocks {
		entry, err := parsePorcelainBlock(block)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// splitPorcelainBlocks splits the porcelain output into individual records.
// Records are separated by blank lines.
func splitPorcelainBlocks(output string) []string {
	var blocks []string
	var current []string

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				blocks = append(blocks, strings.Join(current, "\n"))
				current = nil
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		blocks = append(blocks, strings.Join(current, "\n"))
	}

	return blocks
}

// parsePorcelainBlock parses a single porcelain record into a WorktreeEntry.
func parsePorcelainBlock(block string) (WorktreeEntry, error) {
	entry := WorktreeEntry{Status: StatusOK}

	scanner := bufio.NewScanner(strings.NewReader(block))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "worktree "):
			entry.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			entry.Commit = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			branch := strings.TrimPrefix(line, "branch ")
			// Strip refs/heads/ prefix
			entry.Branch = strings.TrimPrefix(branch, "refs/heads/")
		case line == "bare":
			entry.Bare = true
		case strings.HasPrefix(line, "locked"):
			entry.Status = StatusLocked
			reason := strings.TrimSpace(strings.TrimPrefix(line, "locked"))
			if reason != "" {
				entry.Reason = reason
			}
		case strings.HasPrefix(line, "prunable"):
			entry.Status = StatusPrunable
			reason := strings.TrimSpace(strings.TrimPrefix(line, "prunable"))
			if reason != "" {
				entry.Reason = reason
			}
		}
	}

	if entry.Path == "" {
		return WorktreeEntry{}, fmt.Errorf("porcelain entry missing worktree path")
	}
	if entry.Commit == "" {
		return WorktreeEntry{}, fmt.Errorf("porcelain entry missing HEAD for %s", entry.Path)
	}

	return entry, nil
}

// DetectStatus determines the status of a worktree by checking if its
// directory exists and consulting git worktree list porcelain output.
func DetectStatus(entries []WorktreeEntry, path string) WorktreeStatus {
	for _, e := range entries {
		if e.Path == path {
			// If porcelain already reports locked or prunable, use that
			if e.Status == StatusLocked || e.Status == StatusPrunable {
				return e.Status
			}
			// Check if directory actually exists
			if _, err := os.Stat(path); os.IsNotExist(err) {
				return StatusPrunable
			}
			return StatusOK
		}
	}
	return StatusOK
}

// ResolveBaseDir returns the base directory for worktree creation.
// If baseDir is non-empty, it is used directly.
// Otherwise, it defaults to a sibling directory pattern: <repo>-worktrees
// (next to the repo directory, not inside it).
func ResolveBaseDir(repo, baseDir string) (string, error) {
	if baseDir != "" {
		abs, err := filepath.Abs(baseDir)
		if err != nil {
			return "", fmt.Errorf("resolving base dir: %w", err)
		}
		return abs, nil
	}

	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return "", fmt.Errorf("resolving repo path: %w", err)
	}
	return absRepo + "-worktrees", nil
}
