// Package main implements the text/grep skill for pattern searching across files with regex support.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"

	fsutil "github.com/joshka0/foxctl/internal/adapters/skillslib/fs"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/pathutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/textmatch"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

const command = "text/grep"

// input defines the skill input parameters for text pattern searching with filtering and matching options.
type input struct {
	Path       string   `json:"path"`
	Pattern    string   `json:"pattern"`
	CI         bool     `json:"ci"`
	Include    []string `json:"include"`
	Exclude    []string `json:"exclude"`
	MaxMatches int      `json:"max_matches"`
}

// match represents a text pattern match with file location and content information.
type match = textmatch.Match

// main is the skill entry point for text/grep with pattern searching capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates text pattern searching across files with regex compilation and result aggregation.
//
// Index:
// - Purpose: Search for text patterns across files using regex with filtering, case sensitivity, and match limiting
// - Flow: validate pattern → compile regex → collect files → search files → aggregate results → emit output
// - SideEffects: reads file system; scans file contents; generates match previews; persists search results
// - FailureModes: invalid regex patterns, file access errors, scanner errors, path resolution failures
// - Observability: emits match counts, file statistics, top files, search previews, and comprehensive result metrics
// - Related: grepFile, textmatch.CompileRegex, fsutil.CollectEntries, skillout.PreviewAndPersistNDJSON
// - Keywords: text/grep, pattern_search, regex, file_search, text_matching
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate pattern
	if err := textmatch.RequirePattern(in.Pattern); err != nil {
		return err
	}
	// Apply defaults
	if in.MaxMatches <= 0 {
		in.MaxMatches = 100000
	}

	re, err := textmatch.CompileRegex(in.Pattern, textmatch.RegexOptions{CaseInsensitive: in.CI})
	if err != nil {
		return err
	}

	workspace, basePath, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	entries, err := fsutil.CollectEntries(fsutil.CollectOptions{
		Paths:         []string{basePath},
		Include:       in.Include,
		Exclude:       fsutil.AppendCommonExcludes(in.Exclude),
		IncludeHidden: true,
	})
	if err != nil {
		return skillerr.WrapIO("collect entries", err)
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
		rel := pathutil.RelTo(workspace, entry.Path)
		fileHits[rel] += len(fileMatches)
		allMatches = append(allMatches, fileMatches...)
	}

	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, allMatches, rc.MaxPreview, "text_grep", true)
	if err != nil {
		return err
	}

	data := map[string]any{
		"pattern":          in.Pattern,
		"case_insensitive": in.CI,
		"match_count":      len(allMatches),
		"files_touched":    len(fileHits),
		"preview":          previewResult.Preview,
		"top_files":        skillout.SummarizeTopFiles(fileHits, 5),
		"max_matches":      in.MaxMatches,
	}
	skillout.AddArtifact(data, previewResult.Artifact)

	return skillout.Emit(rc, command, data)
}

// grepFile searches a single file for regex pattern matches with line numbering and snippet generation.
func grepFile(path, workspace string, re *regexp.Regexp, remaining int) ([]match, error) {
	if remaining <= 0 {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, skillerr.WrapIO(fmt.Sprintf("open %s", path), err)
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
			snippet := textmatch.TrimLine(line, 240)
			matches = append(matches, match{
				File:    pathutil.RelTo(workspace, path),
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
		return nil, skillerr.WrapIO(fmt.Sprintf("scan %s", path), err)
	}
	return matches, nil
}
