// Package main implements the text/ripgrep skill for fast text searching using ripgrep with comprehensive pattern matching.
package main

import (
	"context"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/rgutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/textmatch"
	"github.com/jkatigb/agentctl/internal/tools/ripgrep"
)

const command = "text/ripgrep"

// input defines the skill input parameters for ripgrep-based text search with advanced filtering options.
type input struct {
	Path            string   `json:"path"`
	Pattern         string   `json:"pattern"`
	CaseInsensitive bool     `json:"case_insensitive"`
	Glob            []string `json:"glob"`
	GlobNot         []string `json:"glob_not"`
	MaxMatches      int      `json:"max_matches"`
	ContextLines    int      `json:"context_lines"`
	Hidden          bool     `json:"hidden"`
}

type match = textmatch.Match

// main is the skill entry point for text/ripgrep with comprehensive pattern matching capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates ripgrep-based text search with pattern validation, path resolution, and comprehensive result formatting.
//
// Index:
// - Purpose: Perform fast text searches using ripgrep with pattern matching, file filtering, and result preview
// - Flow: validate pattern → normalize input → check ripgrep availability → resolve path → build options → execute search → format results → emit with preview
// - SideEffects: executes ripgrep process; reads file contents; generates match previews; manages result limits
// - FailureModes: invalid patterns, ripgrep not available, path resolution errors, search execution failures
// - Observability: emits match counts, file statistics, preview data, top files summary, and comprehensive search metrics
// - Related: emitEmptyResult, convertMatches, rgutil.Normalize, ripgrep.SearchJSON
// - Keywords: text/ripgrep, pattern_search, ripgrep, file_search, text_matching, result_preview
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate pattern
	if err := textmatch.RequirePattern(in.Pattern); err != nil {
		return err
	}
	searchInput := rgutil.Normalize(rgutil.SearchInput{
		Pattern:         in.Pattern,
		CaseInsensitive: in.CaseInsensitive,
		Glob:            in.Glob,
		GlobNot:         in.GlobNot,
		MaxMatches:      in.MaxMatches,
		ContextLines:    in.ContextLines,
		Hidden:          in.Hidden,
	})
	in.MaxMatches = searchInput.MaxMatches
	// Check if ripgrep is available
	if err := rgutil.RequireRipgrep(); err != nil {
		return err
	}
	// Resolve workspace and path
	workspace, searchPath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	opts := rgutil.BuildSearchOpts(searchInput, workspace, searchPath, nil)
	result, err := ripgrep.SearchJSON(ctx, opts)
	if err != nil {
		return skillerr.WrapRuntime("ripgrep execution failed", err)
	}

	if len(result.Matches) == 0 {
		return emitEmptyResult(rc, in)
	}

	matches, fileHits := convertMatches(result.Matches)

	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, matches, rc.MaxPreview, "text_ripgrep", true)
	if err != nil {
		return err
	}

	// Build response data
	data := map[string]any{
		"pattern":          in.Pattern,
		"case_insensitive": in.CaseInsensitive,
		"match_count":      len(matches),
		"files_touched":    len(fileHits),
		"preview":          previewResult.Preview,
		"top_files":        skillout.SummarizeTopFiles(fileHits, 5),
		"max_matches":      in.MaxMatches,
	}
	skillout.AddArtifact(data, previewResult.Artifact)

	return skillout.Emit(rc, command, data)
}

// emitEmptyResult emits a standardized empty search result when no matches are found.
func emitEmptyResult(rc *skillmain.RunContext, in input) error {
	data := textmatch.EmptySearchResult(in.Pattern, in.CaseInsensitive, []match{})
	data["max_matches"] = in.MaxMatches
	return skillout.Emit(rc, command, data)
}

// convertMatches converts ripgrep matches to standardized format with snippet generation and file hit counting.
func convertMatches(rgMatches []ripgrep.Match) ([]match, map[string]int) {
	matches := make([]match, 0, len(rgMatches))
	fileHits := make(map[string]int)
	for _, m := range rgMatches {
		snippet := textmatch.TrimLine(m.Text, 240)
		matches = append(matches, match{
			File:    m.Path,
			Line:    m.Line,
			Text:    m.Text,
			Snippet: snippet,
		})
		fileHits[m.Path]++
	}
	return matches, fileHits
}
