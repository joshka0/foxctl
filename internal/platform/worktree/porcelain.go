package worktree

import (
	"fmt"
	"strings"
)

// ParsePorcelain parses the output of `git worktree list --porcelain` and
// returns structured WorktreeEntry slices.
//
// The porcelain format consists of records separated by blank lines.
// Each record has these fields:
//
//	worktree <path>      — required
//	HEAD <sha>           — required
//	branch <ref>         — optional (present for branches)
//	detached             — optional (present for detached HEAD)
//	bare                 — optional (present for bare repos)
//	locked [reason]      — optional (present when locked)
//	prunable             — optional (present when directory is gone)
//
// This is a pure function — no IO, no side effects.
func ParsePorcelain(input string) ([]WorktreeEntry, error) {
	if strings.TrimSpace(input) == "" {
		return nil, nil
	}

	// Split into records (separated by blank lines)
	records := strings.Split(input, "\n\n")

	var entries []WorktreeEntry
	for _, record := range records {
		record = strings.TrimSpace(record)
		if record == "" {
			continue
		}

		entry, err := parseRecord(record)
		if err != nil {
			return nil, fmt.Errorf("parsing porcelain record: %w", err)
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// parseRecord parses a single worktree record from porcelain output.
func parseRecord(record string) (WorktreeEntry, error) {
	var entry WorktreeEntry
	entry.Status = StatusOK // default status

	lines := strings.Split(record, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "worktree "):
			entry.Path = strings.TrimPrefix(line, "worktree ")
		case strings.HasPrefix(line, "HEAD "):
			entry.Commit = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			entry.Branch = branchFromRef(ref)
		case line == "detached":
			// Detached HEAD: no branch
		case line == "bare":
			entry.Bare = true
		case line == "locked" || strings.HasPrefix(line, "locked "):
			entry.Status = StatusLocked
			if strings.HasPrefix(line, "locked reason: ") {
				entry.Reason = strings.TrimPrefix(line, "locked reason: ")
			}
		case line == "prunable" || strings.HasPrefix(line, "prunable "):
			entry.Status = StatusPrunable
		}
	}

	if entry.Path == "" {
		return entry, fmt.Errorf("missing worktree path")
	}
	if entry.Commit == "" {
		return entry, fmt.Errorf("missing HEAD for worktree %q", entry.Path)
	}

	return entry, nil
}

// branchFromRef converts a full ref like "refs/heads/main" to just "main".
// Returns empty string for non-branch refs (tags, etc.) or invalid input.
func branchFromRef(ref string) string {
	const prefix = "refs/heads/"
	if !strings.HasPrefix(ref, prefix) {
		return ""
	}
	return strings.TrimPrefix(ref, prefix)
}
