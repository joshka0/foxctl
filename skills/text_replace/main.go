// Package main implements the text/replace skill - an advanced search and replace tool more powerful than sed with regex and literal support.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	fsutil "github.com/joshka0/foxctl/internal/adapters/skillslib/fs"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/textmatch"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/textreplace"
)

const command = "text/replace"

type (
	lineRange  = textreplace.LineRange
	fileChange = textreplace.FileChange
	replacer   = textreplace.Replacer
)

// operation defines a single search and replace operation with pattern and replacement text.
type operation struct {
	Pattern     string `json:"pattern"`
	Replacement string `json:"replacement"`
	Literal     bool   `json:"literal"`
}

// input defines the skill input parameters for advanced text replacement with multiple options and validation.
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
	CASBackup           bool        `json:"cas_backup"`
	ValidateSyntax      bool        `json:"validate_syntax"`
	SkipBinary          *bool       `json:"skip_binary"`
	PreserveLineEndings *bool       `json:"preserve_line_endings"`
	ShowDiff            bool        `json:"show_diff"`
}

// literalReplacer performs literal string replacement without regex interpretation.
type literalReplacer struct {
	pattern     string
	replacement string
}

// Match checks if the content contains the literal pattern.
func (r *literalReplacer) Match(content string) bool {
	return strings.Contains(content, r.pattern)
}

// Replace performs literal string replacement and returns the modified content with replacement count.
func (r *literalReplacer) Replace(content string) (string, int) {
	count := strings.Count(content, r.pattern)
	return strings.ReplaceAll(content, r.pattern, r.replacement), count
}

// regexReplacer performs regex-based pattern replacement with advanced matching capabilities.
type regexReplacer struct {
	pattern     *regexp.Regexp
	replacement string
}

// Match checks if the content matches the regex pattern.
func (r *regexReplacer) Match(content string) bool {
	return r.pattern.MatchString(content)
}

// Replace performs regex pattern replacement and returns the modified content with replacement count.
func (r *regexReplacer) Replace(content string) (string, int) {
	matches := r.pattern.FindAllStringIndex(content, -1)
	count := len(matches)
	return r.pattern.ReplaceAllString(content, r.replacement), count
}

// main is the skill entry point for text/replace with comprehensive search and replace capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates advanced text replacement with multiple operations, file filtering, and comprehensive validation.
//
// Index:
//
//	Purpose: Perform advanced search and replace operations across multiple files with regex/literal support, backup, and validation
//	Keywords: text/replace, search_replace, regex, literal_replacement, file_processing, syntax_validation
//	Related: buildOperations, buildReplacer, validateFileSyntax, textreplace.ProcessFile
//	Flow: validate input → build operations → create replacers → collect files → process each file → validate syntax → emit results
//	Resources: file system, CAS store
//	Events: none
//	OutputFields: pattern, replacement, literal, case_insensitive, word_boundary, multiline, dry_run, files_modified, files_skipped, replacements_made, preview, files_processed, operations_count, backup_enabled, cas_backup_enabled, validation_enabled, artifact
//
// [[risk:destructive_file_modification]]
// [[invariant:dry_run_safety]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.MaxFiles <= 0 {
		in.MaxFiles = 100
	}
	if len(in.Paths) == 0 {
		in.Paths = []string{"."}
	}
	if in.BackupSuffix == "" {
		in.BackupSuffix = ".bak"
	}
	if in.SkipBinary == nil {
		defaultSkipBinary := true
		in.SkipBinary = &defaultSkipBinary
	}
	if in.PreserveLineEndings == nil {
		defaultPreserveLineEndings := true
		in.PreserveLineEndings = &defaultPreserveLineEndings
	}

	// Validate: either pattern/replacement OR operations, not both
	hasMain := strings.TrimSpace(in.Pattern) != ""
	hasOps := len(in.Operations) > 0
	if !hasMain && !hasOps {
		return skillerr.Arg(
			"either pattern/replacement or operations must be specified",
			skillerr.WithHint("Provide pattern+replacement, or a list of operations."),
		)
	}
	if hasMain && hasOps {
		return skillerr.Arg(
			"cannot specify both pattern/replacement and operations",
			skillerr.WithHint("Use either pattern+replacement or operations, not both."),
		)
	}

	// Build list of operations
	ops := buildOperations(in)
	if len(ops) == 0 {
		return skillerr.Arg("no operations specified", skillerr.WithHint("Provide at least one operation."))
	}

	// Build replacers for all operations
	replacers := make([]replacer, len(ops))
	for i, op := range ops {
		r, err := buildReplacer(op, in.CaseInsensitive, in.WordBoundary, in.Multiline)
		if err != nil {
			return skillerr.Validation(fmt.Sprintf("operation %d failed", i+1), skillerr.WithCause(err))
		}
		replacers[i] = r
	}

	// Compile line range patterns if specified
	var rangeStartRe, rangeEndRe *regexp.Regexp
	if in.LineRange != nil {
		if in.LineRange.StartPattern != "" {
			re, err := regexp.Compile(in.LineRange.StartPattern)
			if err != nil {
				return skillerr.Validationf("invalid start_pattern: %v", err)
			}
			rangeStartRe = re
		}
		if in.LineRange.EndPattern != "" {
			re, err := regexp.Compile(in.LineRange.EndPattern)
			if err != nil {
				return skillerr.Validationf("invalid end_pattern: %v", err)
			}
			rangeEndRe = re
		}
	}

	// Collect files to process
	entries, err := fsutil.CollectEntries(fsutil.CollectOptions{
		Paths:         in.Paths,
		Exclude:       fsutil.CommonExcludeGlobs(),
		IncludeHidden: in.IncludeHidden,
		Extensions:    in.Extensions,
		ValidatePath: func(path string) (string, error) {
			return skillmain.ValidatePath(rc, path)
		},
	})
	if err != nil {
		return skillerr.WrapIO("collect entries", err)
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
				File:       pathutil.RelTo(workspace, entry.Path),
				Skipped:    true,
				SkipReason: "file too large",
			})
			filesSkipped++
			continue
		}

		// Check if binary file
		if *in.SkipBinary {
			isBinary, err := textreplace.IsBinaryFile(entry.Path)
			if err != nil {
				return err
			}
			if isBinary {
				allChanges = append(allChanges, fileChange{
					File:       pathutil.RelTo(workspace, entry.Path),
					Skipped:    true,
					SkipReason: "binary file",
				})
				filesSkipped++
				continue
			}
		}

		fileChanges, err := textreplace.ProcessFile(
			ctx,
			rc,
			entry.Path,
			workspace,
			replacers,
			in.LineRange,
			rangeStartRe,
			rangeEndRe,
			textreplace.Options{
				DryRun:              in.DryRun,
				Backup:              in.Backup,
				BackupSuffix:        in.BackupSuffix,
				CASBackup:           in.CASBackup,
				PreserveLineEndings: *in.PreserveLineEndings,
				ShowDiff:            in.ShowDiff,
			},
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

			rel := pathutil.RelTo(workspace, entry.Path)
			fileChanges.File = rel
			allChanges = append(allChanges, fileChanges)
		}
	}

	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, allChanges, rc.MaxPreview, "text_replace", true)
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
		"preview":            previewResult.Preview,
		"files_processed":    len(entries),
		"operations_count":   len(ops),
		"backup_enabled":     in.Backup,
		"cas_backup_enabled": in.CASBackup,
		"validation_enabled": in.ValidateSyntax,
	}

	if in.LineRange != nil {
		data["line_range"] = in.LineRange
	}

	skillout.AddArtifact(data, previewResult.Artifact)

	return skillout.Emit(rc, command, data)
}

// buildOperations creates a list of operations from input, handling both single and multi-operation modes.
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

// buildReplacer creates a replacer instance based on operation type and regex options.
func buildReplacer(op operation, caseInsensitive, wordBoundary, multiline bool) (replacer, error) {
	if err := textmatch.RequirePattern(op.Pattern); err != nil {
		return nil, err
	}

	if op.Literal {
		return &literalReplacer{
			pattern:     op.Pattern,
			replacement: op.Replacement,
		}, nil
	}

	re, err := textmatch.CompileRegex(op.Pattern, textmatch.RegexOptions{
		CaseInsensitive: caseInsensitive,
		WordBoundary:    wordBoundary,
		Multiline:       multiline,
	})
	if err != nil {
		return nil, err
	}

	return &regexReplacer{
		pattern:     re,
		replacement: op.Replacement,
	}, nil
}

// validateFileSyntax validates file syntax for supported formats (Go, JSON, YAML) after replacement.
func validateFileSyntax(path string) (bool, error) {
	ext := filepath.Ext(path)

	switch ext {
	case ".go":
		result := executil.Run(context.Background(), "", "gofmt", "-e", path)
		if result.Err != nil {
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
