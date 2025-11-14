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

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	errs "github.com/jkatigb/agentctl/internal/errors"
	"github.com/jkatigb/agentctl/internal/skillslib"
)

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
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("text/grep", "ECONFIG", err)
	}

	rc, err := skillslib.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("text/grep", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("text/grep", "EARG", fmt.Errorf("decode input: %w", err))
	}
	if err := run(ctx, rc, in); err != nil {
		fail("text/grep", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *skillslib.RunnerContext, in input) error {
	if in.Pattern == "" {
		return fmt.Errorf("pattern is required")
	}
	if in.MaxMatches <= 0 {
		in.MaxMatches = 100000
	}

	re, err := compileRegex(in.Pattern, in.CI)
	if err != nil {
		return err
	}

	workspace := rc.PathValidator.Workspace()
	basePath := workspace
	if strings.TrimSpace(in.Path) != "" {
		validated, err := rc.PathValidator.ValidatePath(in.Path)
		if err != nil {
			return fmt.Errorf("path validation failed: %w", err)
		}
		basePath = validated
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

	preview := allMatches
	if len(preview) > rc.MaxPreview {
		preview = preview[:rc.MaxPreview]
	}

	var artifactDigest string
	var artifactSize int64
	if len(allMatches) > rc.MaxPreview {
		digest, size, err := storeMatches(ctx, rc, allMatches)
		if err != nil {
			return err
		}
		artifactDigest = digest
		artifactSize = size
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
	if artifactDigest != "" {
		data["artifact"] = artifactDigest
		data["artifact_kind"] = "application/x-ndjson"
		data["artifact_size_bytes"] = artifactSize
	}

	return rc.Emit("text/grep", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func collectEntries(path string, include, exclude []string) ([]skillslib.FileEntry, error) {
	if path != "" {
		info, err := os.Stat(path)
		if err == nil && !info.IsDir() {
			return []skillslib.FileEntry{{Path: path, Info: info}}, nil
		}
	}
	opts := skillslib.ListOptions{
		BasePath: path,
		Include:  include,
		Exclude:  append([]string{".git", "node_modules"}, exclude...),
	}
	return skillslib.WalkFiles(opts)
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

func storeMatches(ctx context.Context, rc *skillslib.RunnerContext, matches []match) (string, int64, error) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	for _, m := range matches {
		if err := enc.Encode(m); err != nil {
			return "", 0, fmt.Errorf("encode match: %w", err)
		}
	}
	obj, err := rc.CASStore.Put(ctx, bytes.NewReader(buf.Bytes()), "application/x-ndjson", []string{"text_grep"})
	if err != nil {
		return "", 0, fmt.Errorf("cas put: %w", err)
	}
	return obj.Digest, obj.Size, nil
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

func trimLine(line string, max int) string {
	if len(line) <= max {
		return line
	}
	return line[:max] + "..."
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit text/grep failure")
	os.Exit(1)
}
