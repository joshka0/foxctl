// Package main implements the text/replace skill - an advanced search and replace tool more powerful than sed.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
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

type lineRange struct {
	Start        int    `json:"start"`
	End          int    `json:"end"`
	StartPattern string `json:"start_pattern"`
	EndPattern   string `json:"end_pattern"`
}

type operation struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Literal     bool   `json:"literal"`
}

type input struct {
	Pattern             string      `json:"pattern"`
	Replacement         string      `json:"replacement"`
	Paths               []string    `json:"paths"`
	Literal             bool        `json:"literal"`
	MaxFiles            int         `json:"max_files"`
	DryRun              bool        `json:"dry_run"`
	IncludeHidden       bool        `json:"include_hidden"`
	Extensions          []string    `json:"extensions"`
	CaseInsensitive     bool        `json:"case_insensitive"`
	WordBoundary        bool        `json:"word_boundary"`
	Multiline           bool        `json:"multiline"`
	LineRange           *lineRange  `json:"line_range"`
	Operations          []operation `json:"operations"`
	Backup              bool        `json:"backup"`
	BackupSuffix        string      `json:"backup_suffix"`
	ValidateSyntax      bool        `json:"validate_syntax"`
	SkipBinary          bool        `json:"skip_binary"`
	PreserveLineEndings bool        `json:"preserve_line_endings"`
	ShowDiff            bool        `json:"show_diff"`
}

type change struct {
	File             string `json:"file"`
	LineNumber       int    `json:"line_number"`
	OriginalLine     string `json:"original_line"`
	ModifiedLine     string `json:"modified_line"`
	ReplacementsMade int    `json:"replacements_in_line"`
	Diff             string `json:"diff,omitempty"`
}

type fileChange struct {
	File         string   `json:"file"`
	Replacements int      `json:"replacements"`
	Changes      []change `json:"changes,omitempty"`
	BackupPath   string   `json:"backup_path,omitempty"`
	Skipped      bool     `json:"skipped,omitempty"`
	SkipReason   string   `json:"skip_reason,omitempty"`
	Validated    bool     `json:"validated,omitempty"`
	ValidationOK bool     `json:"validation_ok,omitempty"`
}

type replacer interface {
	Match(content string) bool
	Replace(content string) string
}

type literalReplacer struct {
	pattern     string
	replacement string
}

func (r *literalReplacer) Match(content string) bool {
	return strings.Contains(content, r.pattern)
}

func (r *literalReplacer) Replace(content string) string {
	return strings.ReplaceAll(content, r.pattern, r.replacement)
}

type regexReplacer struct {
	pattern     *regexp.Regexp
	replacement string
}

func (r *regexReplacer) Match(content string) bool {
	return r.pattern.MatchString(content)
}

func (r *regexReplacer) Replace(content string) string {
	return r.pattern.ReplaceAllString(content, r.replacement)
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
	// Build list of operations
	ops := buildOperations(in)
	if len(ops) == 0 {
		return fmt.Errorf("no operations specified")
	}

	// Build replacers for all operations
	replacers := make([]replacer, len(ops))
	for i, op := range ops {
		r, err := buildReplacer(op, in.CaseInsensitive, in.WordBoundary, in.Multiline)
		if err != nil {
			return fmt.Errorf("operation %d: %w", i+1, err)
		}
		replacers[i] = r
	}

	// Compile line range patterns if specified
	var rangeStartRe, rangeEndRe *regexp.Regexp
	if in.LineRange != nil {
		if in.LineRange.StartPattern != "" {
			re, err := regexp.Compile(in.LineRange.StartPattern)
			if err != nil {
				return fmt.Errorf("invalid start_pattern: %w", err)
			}
			rangeStartRe = re
		}
		if in.LineRange.EndPattern != "" {
			re, err := regexp.Compile(in.LineRange.EndPattern)
			if err != nil {
				return fmt.Errorf("invalid end_pattern: %w", err)
			}
			rangeEndRe = re
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
		allChanges        []fileChange
		totalReplacements int
		filesModified     int
		filesSkipped      int
	)

	const maxFileBytes = 10 * 1024 * 1024 // 10MB limit
	for _, entry := range entries {
		if entry.Info != nil && entry.Info.Size() > maxFileBytes {
			allChanges = append(allChanges, fileChange{
				File:       relativeTo(workspace, entry.Path),
				Skipped:    true,
				SkipReason: "file too large",
			})
			filesSkipped++
			continue
		}

		// Check if binary file
		if in.SkipBinary {
			isBinary, err := isBinaryFile(entry.Path)
			if err != nil {
				return err
			}
			if isBinary {
				allChanges = append(allChanges, fileChange{
					File:       relativeTo(workspace, entry.Path),
					Skipped:    true,
					SkipReason: "binary file",
				})
				filesSkipped++
				continue
			}
		}

		fileChanges, err := processFile(
			entry.Path,
			workspace,
			replacers,
			in.LineRange,
			rangeStartRe,
			rangeEndRe,
			in.DryRun,
			in.Backup,
			in.BackupSuffix,
			in.PreserveLineEndings,
			in.ShowDiff,
		)
		if err != nil {
			return err
		}

		if len(fileChanges.Changes) > 0 {
			filesModified++
			totalReplacements += fileChanges.Replacements

			// Validate syntax if requested
			if in.ValidateSyntax && !in.DryRun {
				validationOK, err := validateFileSyntax(entry.Path)
				if err == nil {
					fileChanges.Validated = true
					fileChanges.ValidationOK = validationOK
				}
			}

			rel := relativeTo(workspace, entry.Path)
			fileChanges.File = rel
			allChanges = append(allChanges, fileChanges)
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
		"case_insensitive":   in.CaseInsensitive,
		"word_boundary":      in.WordBoundary,
		"multiline":          in.Multiline,
		"dry_run":            in.DryRun,
		"files_modified":     filesModified,
		"files_skipped":      filesSkipped,
		"replacements_made":  totalReplacements,
		"preview":            preview,
		"files_processed":    len(entries),
		"operations_count":   len(ops),
		"backup_enabled":     in.Backup,
		"validation_enabled": in.ValidateSyntax,
	}

	if in.LineRange != nil {
		data["line_range"] = in.LineRange
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

	// Set defaults
	if in.MaxFiles <= 0 {
		in.MaxFiles = 100
	}
	if len(in.Paths) == 0 {
		in.Paths = []string{"."}
	}
	if in.BackupSuffix == "" {
		in.BackupSuffix = ".bak"
	}
	// Default to true for skip_binary and preserve_line_endings per manifest
	if !in.SkipBinary {
		in.SkipBinary = true
	}
	if !in.PreserveLineEndings {
		in.PreserveLineEndings = true
	}

	// Validate: either pattern/replacement OR operations, not both
	hasMain := strings.TrimSpace(in.Pattern) != ""
	hasOps := len(in.Operations) > 0

	if !hasMain && !hasOps {
		return input{}, fmt.Errorf("either pattern/replacement or operations must be specified")
	}
	if hasMain && hasOps {
		return input{}, fmt.Errorf("cannot specify both pattern/replacement and operations")
	}

	return in, nil
}

func buildOperations(in input) []operation {
	if len(in.Operations) > 0 {
		return in.Operations
	}
	return []operation{{
		Pattern:     in.Pattern,
		Replacement: in.Replacement,
		Literal:     in.Literal,
	}}
}

func buildReplacer(op operation, caseInsensitive, wordBoundary, multiline bool) (replacer, error) {
	if op.Literal {
		return &literalReplacer{
			pattern:     op.Pattern,
			replacement: op.Replacement,
		}, nil
	}

	// Build regex pattern with flags
	pattern := op.Pattern

	// Add word boundaries if requested
	if wordBoundary && !strings.HasPrefix(pattern, `\b`) {
		pattern = `\b` + pattern
	}
	if wordBoundary && !strings.HasSuffix(pattern, `\b`) {
		pattern = pattern + `\b`
	}

	// Add case-insensitive flag
	if caseInsensitive && !strings.HasPrefix(pattern, "(?i)") {
		pattern = "(?i)" + pattern
	}

	// Add multiline/dotall flags
	if multiline {
		if !strings.HasPrefix(pattern, "(?s)") && !strings.HasPrefix(pattern, "(?i)") {
			pattern = "(?s)" + pattern
		} else if strings.HasPrefix(pattern, "(?i)") && !strings.Contains(pattern, "(?s)") {
			pattern = strings.Replace(pattern, "(?i)", "(?is)", 1)
		}
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern: %w", err)
	}

	return &regexReplacer{
		pattern:     re,
		replacement: op.Replacement,
	}, nil
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

func isBinaryFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = f.Close()
	}()

	// Read first 512 bytes
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	buf = buf[:n]

	// Check for null bytes (common in binary files)
	for _, b := range buf {
		if b == 0 {
			return true, nil
		}
	}

	return false, nil
}

func detectLineEnding(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func processFile(
	path, workspace string,
	replacers []replacer,
	lineRange *lineRange,
	rangeStartRe, rangeEndRe *regexp.Regexp,
	dryRun, backup bool,
	backupSuffix string,
	preserveLineEndings, showDiff bool,
) (fileChange, error) {
	// Read file
	content, err := os.ReadFile(path)
	if err != nil {
		return fileChange{}, fmt.Errorf("read %s: %w", path, err)
	}

	// Detect line ending
	lineEnding := "\n"
	if preserveLineEndings {
		lineEnding = detectLineEnding(string(content))
	}

	// Split into lines (normalize to \n for processing)
	normalizedContent := strings.ReplaceAll(string(content), "\r\n", "\n")
	lines := strings.Split(normalizedContent, "\n")

	// Determine which lines to process
	inRange := determineLineRange(lines, lineRange, rangeStartRe, rangeEndRe)

	var (
		changes       []change
		modifiedLines []string
		modified      bool
	)

	for lineNo, line := range lines {
		lineNum := lineNo + 1
		processLine := inRange == nil || inRange[lineNo]

		if !processLine {
			modifiedLines = append(modifiedLines, line)
			continue
		}

		// Apply all replacers to this line
		newLine := line
		madeChanges := false

		for _, r := range replacers {
			if r.Match(newLine) {
				beforeReplace := newLine
				newLine = r.Replace(newLine)
				if newLine != beforeReplace {
					madeChanges = true
				}
			}
		}

		if madeChanges {
			modified = true
			diff := ""
			if showDiff {
				diff = fmt.Sprintf("- %s\n+ %s", truncateLine(line, 200), truncateLine(newLine, 200))
			}

			changes = append(changes, change{
				LineNumber:       lineNum,
				OriginalLine:     truncateLine(line, 200),
				ModifiedLine:     truncateLine(newLine, 200),
				ReplacementsMade: 1,
				Diff:             diff,
			})
		}

		modifiedLines = append(modifiedLines, newLine)
	}

	result := fileChange{
		Replacements: len(changes),
		Changes:      changes,
	}

	// Write back if not dry run and changes were made
	if !dryRun && modified {
		// Create backup if requested
		if backup {
			backupPath := path + backupSuffix
			if err := os.WriteFile(backupPath, content, 0o644); err != nil {
				return fileChange{}, fmt.Errorf("create backup %s: %w", backupPath, err)
			}
			result.BackupPath = relativeTo(workspace, backupPath)
		}

		// Write modified content with original line endings
		newContent := strings.Join(modifiedLines, lineEnding)
		if err := writeFileAtomic(path, []byte(newContent)); err != nil {
			return fileChange{}, fmt.Errorf("write %s: %w", path, err)
		}
	}

	return result, nil
}

func determineLineRange(lines []string, lr *lineRange, startRe, endRe *regexp.Regexp) map[int]bool {
	if lr == nil {
		return nil // Process all lines
	}

	inRange := make(map[int]bool)

	// Line number based range
	if lr.Start > 0 || lr.End > 0 {
		start := lr.Start
		if start <= 0 {
			start = 1
		}
		end := lr.End
		if end <= 0 {
			end = len(lines)
		}

		for i := start - 1; i < end && i < len(lines); i++ {
			inRange[i] = true
		}
		return inRange
	}

	// Pattern-based range
	if startRe != nil || endRe != nil {
		insideRange := startRe == nil // If no start pattern, start from beginning

		for i, line := range lines {
			if startRe != nil && startRe.MatchString(line) {
				insideRange = true
			}

			if insideRange {
				inRange[i] = true
			}

			if endRe != nil && endRe.MatchString(line) {
				insideRange = false
			}
		}
		return inRange
	}

	return nil
}

func writeFileAtomic(path string, data []byte) error {
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

	// Write data
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	// Preserve permissions
	if err := os.Chmod(tmpPath, info.Mode()); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("chmod: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

func validateFileSyntax(path string) (bool, error) {
	ext := filepath.Ext(path)

	switch ext {
	case ".go":
		cmd := exec.Command("gofmt", "-e", path)
		if err := cmd.Run(); err != nil {
			return false, nil
		}
		return true, nil

	case ".json":
		data, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		var js json.RawMessage
		if err := json.Unmarshal(data, &js); err != nil {
			return false, nil
		}
		return true, nil

	case ".yaml", ".yml":
		// Could add YAML validation if we import a YAML library
		return true, nil

	default:
		// Unknown file type, skip validation
		return true, nil
	}
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

func truncateLine(line string, maxLen int) string {
	if len(line) <= maxLen {
		return line
	}
	return line[:maxLen] + "..."
}

func preparePreview(changes []fileChange, maxItems int) ([]fileChange, bool) {
	preview, truncated := skillslib.PreparePreview(changes, maxItems)
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
