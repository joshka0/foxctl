// Package main implements the code/context_ripgrep skill.
// It performs ripgrep searches and expands matches to their surrounding
// code blocks (functions, methods, classes) using language-specific heuristics.
package main

import (
	"context"
	"encoding/json"
	"io"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/codeblocks"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/rgutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/textmatch"
	"github.com/jkatigb/agentctl/internal/tools/ripgrep"
)

type input struct {
	Path            string   `json:"path"`
	Pattern         string   `json:"pattern" validate:"required"`
	CaseInsensitive bool     `json:"case_insensitive"`
	Glob            []string `json:"glob"`
	GlobNot         []string `json:"glob_not"`
	MaxMatches      int      `json:"max_matches"`
	MaxBlocks       int      `json:"max_blocks"`
	MaxBlockLines   int      `json:"max_block_lines"`
	Hidden          bool     `json:"hidden"`
}

type (
	Block        = codeblocks.Block
	BlockPreview = codeblocks.BlockPreview
	rawMatch     = codeblocks.RawMatch
)

func main() {
	skillmain.Main("code/context_ripgrep", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	if in.MaxBlocks <= 0 {
		in.MaxBlocks = 50
	}
	if in.MaxBlockLines <= 0 {
		in.MaxBlockLines = 400
	}
	searchInput := rgutil.Normalize(rgutil.SearchInput{
		Pattern:         in.Pattern,
		CaseInsensitive: in.CaseInsensitive,
		Glob:            in.Glob,
		GlobNot:         in.GlobNot,
		MaxMatches:      in.MaxMatches,
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

	rawMatches := make([]rawMatch, 0, len(result.Matches))
	for _, match := range result.Matches {
		rawMatches = append(rawMatches, rawMatch{
			File: match.Path,
			Line: match.Line,
			Text: match.Text,
		})
	}

	blocks := codeblocks.ExpandMatches(workspace, rawMatches, in.MaxBlocks, in.MaxBlockLines)
	fileHits := make(map[string]int)
	for _, block := range blocks {
		fileHits[block.File] += block.MatchCount
	}

	// Prepare preview and artifact
	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, blocks, rc.MaxPreview, "code_context_ripgrep", true)
	if err != nil {
		return err
	}
	preview := codeblocks.PrepareBlockPreview(previewResult.Preview)

	// Build response data
	totalMatches := 0
	for _, b := range blocks {
		totalMatches += b.MatchCount
	}

	data := map[string]any{
		"pattern":          in.Pattern,
		"case_insensitive": in.CaseInsensitive,
		"match_count":      totalMatches,
		"block_count":      len(blocks),
		"files_touched":    len(fileHits),
		"preview":          preview,
		"top_files":        skillout.SummarizeTopFiles(fileHits, 5),
		"max_blocks":       in.MaxBlocks,
	}
	skillout.AddArtifact(data, previewResult.Artifact)

	return skillout.Emit(rc, "code/context_ripgrep", data)
}

func emitEmptyResult(rc *skillmain.RunContext, in input) error {
	data := textmatch.EmptySearchResult(in.Pattern, in.CaseInsensitive, []BlockPreview{})
	data["block_count"] = 0
	data["max_blocks"] = in.MaxBlocks
	return skillout.Emit(rc, "code/context_ripgrep", data)
}

// parseInput parses and validates input from a reader.
// Exported for testing.
func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, skillerr.WrapParse("decode input", err)
	}

	// Apply defaults
	if in.MaxMatches <= 0 {
		in.MaxMatches = 10000
	}
	if in.MaxBlocks <= 0 {
		in.MaxBlocks = 50
	}
	if in.MaxBlockLines <= 0 {
		in.MaxBlockLines = 400
	}

	// Validate required fields
	if err := textmatch.RequirePattern(in.Pattern); err != nil {
		return input{}, err
	}

	return in, nil
}
