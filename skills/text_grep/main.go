// Package main implements the text/grep skill.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"regexp"

	fsutil "github.com/jkatigb/agentctl/internal/adapters/skillslib/fs"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/pathutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/textmatch"
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

type match = textmatch.Match

func main() {
	skillmain.Main(command, run)
}

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
