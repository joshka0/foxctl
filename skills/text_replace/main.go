// Package main implements the text/replace skill.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib"
	fsutil "github.com/jkatigb/agentctl/internal/adapters/skillslib/fs"
	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	Pattern       string   `json:"pattern"`
	Replacement   string   `json:"replacement"`
	Paths         []string `json:"paths"`
	Literal       bool     `json:"literal"`
	MaxFiles      int      `json:"max_files"`
	DryRun        bool     `json:"dry_run"`
	IncludeHidden bool     `json:"include_hidden"`
	Extensions    []string `json:"extensions"`
}

type change struct {
	File            string `json:"file"`
	LineNumber      int    `json:"line_number"`
	OriginalLine    string `json:"original_line"`
	ModifiedLine    string `json:"modified_line"`
	ReplacementsMade int   `json:"replacements_in_line"`
}

type fileChange struct {
	File         string   `json:"file"`
	Replacements int      `json:"replacements"`
	Changes      []change `json:"changes,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("text/replace", "ECONFIG", err)
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("text/replace", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("text/replace", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("text/replace", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	// Compile pattern
	var replacer func(string) string
	var matcher func(string) bool

	if in.Literal {
		// Literal string replacement
		needle := in.Pattern
		replacer = func(line string) string {
			return strings.ReplaceAll(line, needle, in.Replacement)
		}
		matcher = func(line string) bool {
			return strings.Contains(line, needle)
		}
	} else {
		// Regex replacement
		re, err := regexp.Compile(in.Pattern)
		if err != nil {
			return fmt.Errorf("invalid regex pattern: %w", err)
		}
		replacer = func(line string) string {
			return re.ReplaceAllString(line, in.Replacement)
		}
		matcher = func(line string) bool {
			return re.MatchString(line)
		}
	}

	// Collect files to process
	entries, err := collectEntries(rc, in.Paths, in.IncludeHidden, in.Extensions)
	if err != nil {
		return err
	}

	if len(entries) > in.MaxFiles {
		entries = entries[:in.MaxFiles]
	}

	workspace := rc.PathValidator.Workspace()
	var (
		allChanges       []fileChange
		totalReplacements int
		filesModified    int
	)

	const maxFileBytes = 10 * 1024 * 1024 // 10MB limit
	for _, entry := range entries {
		if entry.Info != nil && entry.Info.Size() > maxFileBytes {
			continue
		}

		fileChanges, err := processFile(entry.Path, workspace, matcher, replacer, in.DryRun)
		if err != nil {
			return err
		}

		if len(fileChanges) > 0 {
			filesModified++
			totalReplacements += sumReplacements(fileChanges)

			rel := relativeTo(workspace, entry.Path)
			allChanges = append(allChanges, fileChange{
				File:         rel,
				Replacements: len(fileChanges),
				Changes:      fileChanges,
			})
		}
	}

	// Prepare preview
	preview, truncated := preparePreview(allChanges, rc.MaxPreview)

	// Persist full results if truncated
	artifact, err := persistChangesArtifact(ctx, rc, allChanges, truncated)
	if err != nil {
		return err
	}

	data := map[string]any{
		"pattern":            in.Pattern,
		"replacement":        in.Replacement,
		"literal":            in.Literal,
		"dry_run":            in.DryRun,
		"files_modified":     filesModified,
		"replacements_made":  totalReplacements,
		"preview":            preview,
		"files_processed":    len(entries),
	}

	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
		data["artifact_kind"] = artifact.Kind
		data["artifact_size_bytes"] = artifact.Size
	}

	return rc.Emit("text/replace", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if strings.TrimSpace(in.Pattern) == "" {
		return input{}, fmt.Errorf("pattern is required")
	}
	if in.MaxFiles <= 0 {
		in.MaxFiles = 100
	}
	if len(in.Paths) == 0 {
		in.Paths = []string{"."}
	}
	return in, nil
}

func collectEntries(rc *runner.RunnerContext, paths []string, includeHidden bool, extensions []string) ([]fsutil.FileEntry, error) {
	var allEntries []fsutil.FileEntry
	seen := make(map[string]bool)

	for _, p := range paths {
		// Validate path
		resolved, err := rc.PathValidator.ValidatePath(p)
		if err != nil {
			return nil, fmt.Errorf("path validation failed for %s: %w", p, err)
		}

		// Check if single file or directory
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}

		if !info.IsDir() {
			// Single file
			if !seen[resolved] && matchesExtension(resolved, extensions) {
				allEntries = append(allEntries, fsutil.FileEntry{Path: resolved, Info: info})
				seen[resolved] = true
			}
			continue
		}

		// Directory - walk it
		exclude := []string{".git", "node_modules"}
		if !includeHidden {
			exclude = append(exclude, ".*")
		}

		opts := fsutil.ListOptions{
			BasePath: resolved,
			Include:  []string{},
			Exclude:  exclude,
		}

		entries, err := fsutil.WalkFiles(opts)
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", p, err)
		}

		for _, entry := range entries {
			if !seen[entry.Path] && matchesExtension(entry.Path, extensions) {
				allEntries = append(allEntries, entry)
				seen[entry.Path] = true
			}
		}
	}

	return allEntries, nil
}

func matchesExtension(path string, extensions []string) bool {
	if len(extensions) == 0 {
		return true
	}
	ext := filepath.Ext(path)
	for _, e := range extensions {
		if ext == e || ext == "."+e {
			return true
		}
	}
	return false
}

func processFile(path, workspace string, matcher func(string) bool, replacer func(string) string, dryRun bool) ([]change, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() {
		errs.Ignore(f.Close(), "close file")
	}()

	var (
		changes     []change
		modifiedLines []string
		lineNo      int
	)

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024), 1024*64)

	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		if matcher(line) {
			newLine := replacer(line)
			replacementsInLine := countReplacements(line, newLine)

			changes = append(changes, change{
				File:             relativeTo(workspace, path),
				LineNumber:       lineNo,
				OriginalLine:     truncateLine(line, 200),
				ModifiedLine:     truncateLine(newLine, 200),
				ReplacementsMade: replacementsInLine,
			})
			modifiedLines = append(modifiedLines, newLine)
		} else {
			modifiedLines = append(modifiedLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}

	// Write back if not dry run and changes were made
	if !dryRun && len(changes) > 0 {
		if err := writeFile(path, modifiedLines); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
	}

	return changes, nil
}

func writeFile(path string, lines []string) error {
	// Read file info to preserve permissions
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	// Create temp file in same directory
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".text_replace_tmp_*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Write lines
	writer := bufio.NewWriter(tmpFile)
	for _, line := range lines {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("write line: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("flush: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	// Preserve permissions
	if err := os.Chmod(tmpPath, info.Mode()); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

func countReplacements(original, modified string) int {
	// This is a simple heuristic - count character differences
	// For more accuracy, we could use edit distance
	if original == modified {
		return 0
	}
	return 1
}

func sumReplacements(changes []change) int {
	sum := 0
	for _, c := range changes {
		sum += c.ReplacementsMade
	}
	return sum
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

func truncateLine(line string, max int) string {
	if len(line) <= max {
		return line
	}
	return line[:max] + "..."
}

func preparePreview(changes []fileChange, max int) ([]fileChange, bool) {
	preview, truncated := skillslib.PreparePreview(changes, max)
	if truncated {
		dup := make([]fileChange, len(preview))
		copy(dup, preview)
		preview = dup
	}
	return preview, truncated
}

func persistChangesArtifact(ctx context.Context, rc *runner.RunnerContext, changes []fileChange, truncated bool) (runner.Artifact, error) {
	if !truncated {
		return runner.Artifact{}, nil
	}
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	for _, c := range changes {
		if err := enc.Encode(c); err != nil {
			return runner.Artifact{}, fmt.Errorf("encode change: %w", err)
		}
	}
	return runner.PersistBuffer(ctx, rc, buf, "application/x-ndjson", "text_replace")
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit text/replace failure")
	os.Exit(1)
}
