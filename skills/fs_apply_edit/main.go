// Package main implements the fs/apply_edit skill.
// It applies targeted edits to files using fuzzy matching, with CAS backup for undo.
package main

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/editutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/textutil"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
)

// MatchMode defines how to match the search string.
type MatchMode string

const (
	MatchExact MatchMode = "exact"
	MatchFuzzy MatchMode = "fuzzy"
	MatchRegex MatchMode = "regex"
)

// Edit represents a single edit operation for fs/apply_edit.
type Edit struct {
	Search        string    `json:"search" validate:"required"`
	Replace       string    `json:"replace"`
	LineHint      int       `json:"line_hint,omitempty" validate:"omitempty,min=1"`
	ContextBefore string    `json:"context_before,omitempty"`
	ContextAfter  string    `json:"context_after,omitempty"`
	Global        bool      `json:"global,omitempty"`
	MatchMode     MatchMode `json:"match_mode,omitempty" validate:"omitempty,oneof=exact fuzzy regex"`
}

// Input is the skill input for fs/apply_edit operations.
type Input struct {
	Path         string `json:"path" validate:"required"`
	Edits        []Edit `json:"edits" validate:"required,min=1,dive"`
	DryRun       bool   `json:"dry_run,omitempty"`
	CreateBackup bool   `json:"create_backup,omitempty"`
}

// EditResult captures the outcome of a single edit.
type EditResult struct {
	Search       string `json:"search"`
	Replacements int    `json:"replacements"`
	Lines        []int  `json:"lines,omitempty"`
	Error        string `json:"error,omitempty"`
}

// Output is the skill output for fs/apply_edit operations.
type Output struct {
	Path             string            `json:"path"`
	BackupDigest     string            `json:"backup_digest,omitempty"`
	BackupCASHint    *envelope.CASHint `json:"backup_cas_hint,omitempty"`
	EditsApplied     int               `json:"edits_applied"`
	ReplacementsMade int               `json:"replacements_made"`
	Diff             string            `json:"diff,omitempty"`
	DryRun           bool              `json:"dry_run"`
	EditResults      []EditResult      `json:"edit_results,omitempty"`
}

// main is the skill entry point for fs/apply_edit.
func main() {
	skillmain.Main("fs/apply_edit", run)
}

// run orchestrates file editing with fuzzy matching, backup creation, and diff generation.
//
// Index:
// - Purpose: Apply targeted edits to files using exact, fuzzy, or regex matching with CAS backup
// - Flow: validate input → resolve path → apply edits → create backup → generate diff → emit results
// - SideEffects: file modifications; backup creation; CAS storage; diff generation
// - FailureModes: invalid paths, regex errors, backup failures, file permission errors
// - Observability: emits edit results, replacement counts, backup artifacts, and diff output
// - Related: applyEdit, applyExactMatch, applyFuzzyMatch, applyRegexMatch
// - Keywords: fs/apply_edit, file_editing, fuzzy_matching, backup, diff, cas
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if !in.DryRun {
		in.CreateBackup = true // Default to creating backup unless dry run
	}
	for i := range in.Edits {
		if in.Edits[i].MatchMode == "" {
			in.Edits[i].MatchMode = MatchFuzzy
		}
	}
	for i, edit := range in.Edits {
		if edit.MatchMode != MatchRegex {
			continue
		}
		if _, err := regexp.Compile(edit.Search); err != nil {
			return skillerr.Arg(
				fmt.Sprintf("edits[%d].search is not a valid regex: %v", i, err),
				skillerr.WithHint("Use a valid Go regexp or set match_mode to \"exact\" or \"fuzzy\"."),
			)
		}
	}

	// Resolve and validate path
	targetPath, err := skillmain.ValidatePath(rc, in.Path, skillmain.WithPathMessage("invalid path"))
	if err != nil {
		return err
	}

	// Apply edits
	var editResults []EditResult
	totalReplacements := 0
	editsApplied := 0

	apply := func(original string) (string, error) {
		modified := original
		for _, edit := range in.Edits {
			result, newContent, err := applyEdit(modified, edit)
			if err != nil {
				result.Error = err.Error()
			} else if result.Replacements > 0 {
				modified = newContent
				editsApplied++
				totalReplacements += result.Replacements
			}
			editResults = append(editResults, result)
		}
		return modified, nil
	}

	result, err := editutil.ApplyFile(ctx, rc, targetPath, editutil.FileOptions{
		DryRun:           in.DryRun,
		CreateBackup:     in.CreateBackup,
		BackupTags:       []string{"backup", "fs/apply_edit", fmt.Sprintf("path:%s", targetPath)},
		DiffContext:      0,
		AllowDiffFailure: true,
	}, apply)
	if err != nil {
		return err
	}

	var backupHint *envelope.CASHint
	if result.BackupArtifact != nil {
		hint := skillout.DefaultCASHint(*result.BackupArtifact)
		backupHint = &hint
	}

	out := Output{
		Path:             targetPath,
		BackupDigest:     result.BackupDigest,
		BackupCASHint:    backupHint,
		EditsApplied:     editsApplied,
		ReplacementsMade: totalReplacements,
		Diff:             result.Diff,
		DryRun:           in.DryRun,
		EditResults:      editResults,
	}

	return skillout.Emit(rc, "fs/apply_edit", out)
}

// applyEdit applies a single edit operation to content.
func applyEdit(content string, edit Edit) (EditResult, string, error) {
	switch edit.MatchMode {
	case MatchExact:
		return applyExactMatch(content, edit)
	case MatchRegex:
		return applyRegexMatch(content, edit)
	case MatchFuzzy:
		fallthrough
	default:
		return applyFuzzyMatch(content, edit)
	}
}

// applyExactMatch performs exact string replacement.
func applyExactMatch(content string, edit Edit) (EditResult, string, error) {
	result := EditResult{Search: edit.Search}

	if edit.Global {
		count := strings.Count(content, edit.Search)
		if count == 0 {
			return result, content, nil
		}
		result.Replacements = count
		result.Lines = findMatchLines(content, edit.Search)
		return result, strings.ReplaceAll(content, edit.Search, edit.Replace), nil
	}

	// Single replacement - use line hint if provided
	if edit.LineHint > 0 {
		lines := strings.Split(content, "\n")
		bestIdx := findBestMatch(lines, edit.Search, edit.LineHint, edit.ContextBefore, edit.ContextAfter)
		if bestIdx >= 0 {
			// Replace in the specific line
			lines[bestIdx] = strings.Replace(lines[bestIdx], edit.Search, edit.Replace, 1)
			result.Replacements = 1
			result.Lines = []int{bestIdx + 1}
			return result, strings.Join(lines, "\n"), nil
		}
	}

	// No line hint or not found at hint - replace first occurrence
	idx := strings.Index(content, edit.Search)
	if idx < 0 {
		return result, content, nil
	}

	result.Replacements = 1
	result.Lines = []int{textutil.CountNewlines(content[:idx]) + 1}
	return result, strings.Replace(content, edit.Search, edit.Replace, 1), nil
}

// applyFuzzyMatch performs fuzzy string replacement with whitespace tolerance.
func applyFuzzyMatch(content string, edit Edit) (EditResult, string, error) {
	result := EditResult{Search: edit.Search}

	// Normalize search pattern for fuzzy matching
	searchNorm := normalizeWhitespace(edit.Search)

	// Try exact match first
	if strings.Contains(content, edit.Search) {
		return applyExactMatch(content, edit)
	}

	// Build lines for line-by-line analysis
	lines := strings.Split(content, "\n")

	// Find best matching location using line hint and context
	candidates := findFuzzyCandidates(lines, searchNorm, edit.LineHint, edit.ContextBefore, edit.ContextAfter)

	if len(candidates) == 0 {
		return result, content, nil
	}

	// Sort by score (higher is better)
	var modified string
	replacements := 0
	var matchedLines []int

	if edit.Global {
		// Replace all candidates
		for _, c := range candidates {
			if strings.Contains(lines[c.lineIdx], edit.Search) {
				lines[c.lineIdx] = strings.ReplaceAll(lines[c.lineIdx], edit.Search, edit.Replace)
			} else {
				// Fuzzy replace - find the similar substring
				lines[c.lineIdx] = fuzzyReplaceLine(lines[c.lineIdx], edit.Search, edit.Replace)
			}
			matchedLines = append(matchedLines, c.lineIdx+1)
			replacements++
		}
		modified = strings.Join(lines, "\n")
	} else {
		// Replace best candidate only
		best := candidates[0]
		for _, c := range candidates {
			if c.score > best.score {
				best = c
			}
		}
		if strings.Contains(lines[best.lineIdx], edit.Search) {
			lines[best.lineIdx] = strings.Replace(lines[best.lineIdx], edit.Search, edit.Replace, 1)
		} else {
			lines[best.lineIdx] = fuzzyReplaceLine(lines[best.lineIdx], edit.Search, edit.Replace)
		}
		modified = strings.Join(lines, "\n")
		matchedLines = []int{best.lineIdx + 1}
		replacements = 1
	}

	result.Replacements = replacements
	result.Lines = matchedLines
	return result, modified, nil
}

// applyRegexMatch performs regex-based replacement.
func applyRegexMatch(content string, edit Edit) (EditResult, string, error) {
	result := EditResult{Search: edit.Search}

	re, err := regexp.Compile(edit.Search)
	if err != nil {
		return result, content, fmt.Errorf("invalid regex: %w", err)
	}

	matches := re.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return result, content, nil
	}

	for _, m := range matches {
		result.Lines = append(result.Lines, textutil.CountNewlines(content[:m[0]])+1)
	}

	if edit.Global {
		result.Replacements = len(matches)
		return result, re.ReplaceAllString(content, edit.Replace), nil
	}

	// Single replacement - use local counter to track if we've done the replacement
	replaced := false
	result.Lines = result.Lines[:1]
	newContent := re.ReplaceAllStringFunc(content, func(s string) string {
		if !replaced {
			replaced = true
			return edit.Replace
		}
		return s
	})
	result.Replacements = 1
	return result, newContent, nil
}

// candidate represents a potential match location.
type candidate struct {
	lineIdx int
	score   float64
}

// findFuzzyCandidates finds lines that fuzzily match the search pattern.
func findFuzzyCandidates(lines []string, searchNorm string, lineHint int, ctxBefore, ctxAfter string) []candidate {
	var candidates []candidate
	searchWords := strings.Fields(searchNorm)

	for i, line := range lines {
		lineNorm := normalizeWhitespace(line)

		// Check if line contains similar content
		score := fuzzyMatchScore(lineNorm, searchNorm, searchWords)
		if score < 0.5 {
			continue
		}

		// Boost score based on line hint proximity
		if lineHint > 0 {
			distance := math.Abs(float64(i + 1 - lineHint))
			// Closer to hint = higher boost (max 0.3 boost at distance 0)
			proximityBoost := 0.3 * math.Exp(-distance/10.0)
			score += proximityBoost
		}

		// Boost if context matches
		if ctxBefore != "" && i > 0 {
			prevLines := strings.Join(lines[max(0, i-3):i], "\n")
			if strings.Contains(prevLines, ctxBefore) {
				score += 0.2
			}
		}
		if ctxAfter != "" && i < len(lines)-1 {
			nextLines := strings.Join(lines[i+1:min(len(lines), i+4)], "\n")
			if strings.Contains(nextLines, ctxAfter) {
				score += 0.2
			}
		}

		candidates = append(candidates, candidate{lineIdx: i, score: score})
	}

	return candidates
}

// fuzzyMatchScore calculates how well lineNorm matches searchNorm.
func fuzzyMatchScore(lineNorm, searchNorm string, searchWords []string) float64 {
	// Direct substring check
	if strings.Contains(lineNorm, searchNorm) {
		return 1.0
	}

	// Word-based matching
	matchedWords := 0
	for _, word := range searchWords {
		if strings.Contains(lineNorm, word) {
			matchedWords++
		}
	}

	if len(searchWords) == 0 {
		return 0
	}

	return float64(matchedWords) / float64(len(searchWords))
}

// fuzzyReplaceLine attempts to replace a fuzzy match in a line.
func fuzzyReplaceLine(line, search, replace string) string {
	// Try normalized match
	lineNorm := normalizeWhitespace(line)
	searchNorm := normalizeWhitespace(search)

	if strings.Contains(lineNorm, searchNorm) {
		// Find the actual substring that matches when normalized
		// For simplicity, just do a simple replacement of leading/trailing whitespace normalized
		return strings.Replace(line, strings.TrimSpace(search), strings.TrimSpace(replace), 1)
	}

	// Fallback: just append replace (shouldn't normally hit this)
	return line
}

// findBestMatch finds the best matching line index using hints.
func findBestMatch(lines []string, search string, lineHint int, ctxBefore, ctxAfter string) int {
	candidates := findFuzzyCandidates(lines, normalizeWhitespace(search), lineHint, ctxBefore, ctxAfter)
	if len(candidates) == 0 {
		return -1
	}

	best := candidates[0]
	for _, c := range candidates {
		if c.score > best.score {
			best = c
		}
	}
	return best.lineIdx
}

// findMatchLines returns line numbers where search appears.
func findMatchLines(content, search string) []int {
	var lines []int
	idx := 0
	for {
		pos := strings.Index(content[idx:], search)
		if pos < 0 {
			break
		}
		lineNum := textutil.CountNewlines(content[:idx+pos]) + 1
		lines = append(lines, lineNum)
		idx += pos + len(search)
	}
	return lines
}

// normalizeWhitespace collapses whitespace for fuzzy matching.
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
