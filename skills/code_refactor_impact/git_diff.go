package main

import (
	"context"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/intelligence/refactor/impact"
)

type gitDiffProvider struct {
	workspace string
}

func (p gitDiffProvider) ChangedFiles(ctx context.Context, in impact.DiffInput) ([]impact.Change, error) {
	args := diffRangeArgs(in)
	nameStatus, err := executil.GitOutput(ctx, p.workspace, append([]string{"diff", "--name-status", "-z"}, args...)...)
	if err != nil {
		return nil, err
	}
	numstat, err := executil.GitOutput(ctx, p.workspace, append([]string{"diff", "--numstat", "-z"}, args...)...)
	if err != nil {
		return nil, err
	}

	changes := parseNameStatus(nameStatus)
	stats := parseNumstat(numstat)
	for i := range changes {
		if stat, ok := stats[changes[i].Path]; ok {
			changes[i].Additions = stat.additions
			changes[i].Deletions = stat.deletions
		}
	}
	return changes, nil
}

func diffRangeArgs(in impact.DiffInput) []string {
	base := strings.TrimSpace(in.BaseRef)
	if base == "" {
		base = impact.DefaultBaseRef
	}
	head := strings.TrimSpace(in.HeadRef)
	if head == "" {
		return []string{base}
	}
	return []string{base + "..." + head}
}

func parseNameStatus(raw string) []impact.Change {
	fields := splitNUL(raw)
	changes := make([]impact.Change, 0, len(fields)/2)
	for i := 0; i < len(fields); {
		status := strings.TrimSpace(fields[i])
		i++
		if status == "" || i >= len(fields) {
			continue
		}
		change := impact.Change{Status: status}
		if strings.HasPrefix(status, "R") || strings.HasPrefix(status, "C") {
			change.OldPath = fields[i]
			i++
			if i >= len(fields) {
				break
			}
			change.Path = fields[i]
			i++
		} else {
			change.Path = fields[i]
			i++
		}
		changes = append(changes, change)
	}
	return changes
}

type numStat struct {
	additions int
	deletions int
}

func parseNumstat(raw string) map[string]numStat {
	fields := splitNUL(raw)
	stats := make(map[string]numStat)
	for i := 0; i < len(fields); {
		header := strings.TrimSpace(fields[i])
		i++
		if header == "" {
			continue
		}
		parts := strings.Fields(header)
		if len(parts) < 2 {
			continue
		}
		additions := parseCount(parts[0])
		deletions := parseCount(parts[1])
		if len(parts) >= 3 {
			stats[parts[2]] = numStat{additions: additions, deletions: deletions}
			continue
		}
		if i >= len(fields) {
			break
		}
		path := fields[i]
		i++
		stats[path] = numStat{additions: additions, deletions: deletions}
		if i < len(fields) && !looksLikeNumstatHeader(fields[i]) {
			stats[fields[i]] = numStat{additions: additions, deletions: deletions}
			i++
		}
	}
	return stats
}

func looksLikeNumstatHeader(value string) bool {
	parts := strings.Fields(strings.TrimSpace(value))
	return len(parts) >= 2 && isCount(parts[0]) && isCount(parts[1])
}

func isCount(value string) bool {
	if value == "-" {
		return true
	}
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseCount(value string) int {
	if value == "-" {
		return 0
	}
	n := 0
	for _, r := range value {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func splitNUL(raw string) []string {
	parts := strings.Split(raw, "\x00")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}
