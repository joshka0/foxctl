// Package main implements the text/grep skill.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	fsutil "github.com/jkatigb/agentctl/internal/adapters/skillslib/fs"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

const command = "text/grep"

type input struct {
	Path       string   `json:"path"`
	Pattern    string   `json:"pattern"`
	CI         bool     `json:"ci"`
	Include    []string `json:"include"`
	Exclude    []string `json:"exclude"`
	MaxMatches int      `json:"max_matches"`
}

type match struct {
	File    string `json:"file"`
	Line    int    `json:"line_no"`
	Text    string `json:"line"`
	Snippet string `json:"snippet"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate pattern
	if strings.TrimSpace(in.Pattern) == "" {
		return fmt.Errorf("pattern is required")
	}
	// Apply defaults
	if in.MaxMatches <= 0 {
		in.MaxMatches = 100000
	}

	re, err := compileRegex(in.Pattern, in.CI)
	if err != nil {
		return err
	}

	workspace, basePath, err := resolveWorkspace(rc, in.Path)
	if err != nil {
		return err
	}

	entries, err := collectEntries(basePath, in.Include, in.Exclude)
	if err != nil {
		return err
	}

	var (
		allMatches []match
		fileHits   = make(map[string]int)
	)

	const maxFileBytes = 2 * 1024 * 1024
	for _, entry := range entries {
		if entry.Info != nil && entry.Info.Size() > maxFileBytes {
			continue
		}
		remaining := in.MaxMatches - len(allMatches)
		if remaining <= 0 {
			break
		}
		fileMatches, err := grepFile(entry.Path, workspace, re, remaining)
		if err != nil {
			return err
		}
		if len(fileMatches) == 0 {
			continue
		}
		rel := relativeTo(workspace, entry.Path)
		fileHits[rel] += len(fileMatches)
		allMatches = append(allMatches, fileMatches...)
	}

	preview, truncated := preparePreview(allMatches, rc.MaxPreview)
	artifact, err := persistMatchesArtifact(ctx, rc, allMatches, truncated)
	if err != nil {
		return err
	}

	data := map[string]any{
		"pattern":          in.Pattern,
		"case_insensitive": in.CI,
		"match_count":      len(allMatches),
		"files_touched":    len(fileHits),
		"preview":          preview,
		"top_files":        summarizeTopFiles(fileHits, 5),
		"max_matches":      in.MaxMatches,
	}
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
	}

	return skillout.Emit(rc, command, data)
}

func resolveWorkspace(rc *skillmain.RunContext, candidate string) (string, string, error) {
	workspace := rc.PathValidator.Workspace()
	if strings.TrimSpace(candidate) == "" {
		return workspace, workspace, nil
	}
	resolved, err := rc.PathValidator.ValidatePath(candidate)
	if err != nil {
		return "", "", fmt.Errorf("path validation failed: %w", err)
	}
	return workspace, resolved, nil
}

func preparePreview(matches []match, limit int) ([]match, bool) {
	preview, truncated := skillout.PreparePreview(matches, limit)
	if truncated {
		dup := make([]match, len(preview))
		copy(dup, preview)
		preview = dup
	}
	return preview, truncated
}

func persistMatchesArtifact(ctx context.Context, rc *skillmain.RunContext, matches []match, truncated bool) (skillmain.Artifact, error) {
	if !truncated {
		return skillmain.Artifact{}, nil
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	for _, m := range matches {
		if err := enc.Encode(m); err != nil {
			return skillmain.Artifact{}, fmt.Errorf("encode match: %w", err)
		}
	}
	return skillmain.PersistBuffer(ctx, rc, buf, "application/x-ndjson", "text_grep")
}

func collectEntries(path string, include, exclude []string) ([]fsutil.FileEntry, error) {
	if path != "" {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return []fsutil.FileEntry{{Path: path, Info: info}}, nil
		}
	}
	opts := fsutil.ListOptions{
		BasePath: path,
		Include:  include,
		Exclude:  append([]string{".git", "node_modules"}, exclude...),
	}
	return fsutil.WalkFiles(opts)
}

func compileRegex(pattern string, ci bool) (*regexp.Regexp, error) {
	if ci && !strings.HasPrefix(pattern, "(?i)") {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}
	return re, nil
}

func grepFile(path, workspace string, re *regexp.Regexp, remaining int) ([]match, error) {
	if remaining <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() {
		errs.Ignore(f.Close(), "close grep file")
	}()

	var matches []match
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024), 1024*64)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if re.MatchString(line) {
			snippet := trimLine(line, 240)
			matches = append(matches, match{
				File:    relativeTo(workspace, path),
				Line:    lineNo,
				Text:    line,
				Snippet: snippet,
			})
			if len(matches) >= remaining {
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return matches, nil
}

func relativeTo(base, target string) string {
	if base == "" {
		if rel, err := filepath.Rel(".", target); err == nil {
			return filepath.ToSlash(rel)
		}
		return filepath.ToSlash(target)
	}
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return filepath.ToSlash(target)
	}
	if strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(target)
	}
	return filepath.ToSlash(rel)
}

func summarizeTopFiles(counts map[string]int, limit int) [][2]any {
	type kv struct {
		File  string
		Count int
	}
	var list []kv
	for file, count := range counts {
		list = append(list, kv{File: file, Count: count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Count == list[j].Count {
			return list[i].File < list[j].File
		}
		return list[i].Count > list[j].Count
	})
	if len(list) > limit {
		list = list[:limit]
	}
	var out [][2]any
	for _, item := range list {
		out = append(out, [2]any{item.File, item.Count})
	}
	return out
}

func trimLine(line string, limit int) string {
	if len(line) <= limit {
		return line
	}
	return line[:limit] + "..."
}

