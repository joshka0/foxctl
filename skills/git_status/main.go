// Package main implements the git/status skill.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/gitutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const command = "git/status"

type input struct {
	Operation string `json:"operation"`
	RepoPath  string `json:"repo_path"`
	Staged    bool   `json:"staged"`
	Commit    string `json:"commit"`
	Limit     int    `json:"limit"`
	Stat      bool   `json:"stat"`
}

type fileStatus struct {
	Path        string `json:"path"`
	Status      string `json:"status"`
	StagingArea string `json:"staging_area"`
	WorkingTree string `json:"working_tree"`
}

type commitInfo struct {
	Hash      string `json:"hash"`
	ShortHash string `json:"short_hash"`
	Author    string `json:"author"`
	Date      string `json:"date"`
	Message   string `json:"message"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Apply defaults
	in.Operation = oputil.DefaultOp(in.Operation, "status")
	if in.RepoPath == "" {
		in.RepoPath = "."
	}
	if in.Limit <= 0 {
		in.Limit = 10
	}

	repoPath, err := gitutil.ResolveRepoPath(rc, in.RepoPath)
	if err != nil {
		return err
	}

	gitPath, err := gitutil.RequireGit()
	if err != nil {
		return err
	}

	// Dispatch operation
	data, err := oputil.NewSwitch(in.Operation).
		Case("status", func() (map[string]any, error) { return getStatus(ctx, gitPath, repoPath) }).
		Case("diff", func() (map[string]any, error) { return getDiff(ctx, rc, gitPath, repoPath, in) }).
		Case("log", func() (map[string]any, error) { return getLog(ctx, gitPath, repoPath, in) }).
		Run()
	if err != nil {
		var invalid *oputil.InvalidOpError
		if errors.As(err, &invalid) {
			hint := fmt.Sprintf("Use one of: %s.", strings.Join(invalid.Allowed, ", "))
			return skillerr.Arg(err.Error(), skillerr.WithHint(hint))
		}
		return err
	}

	return skillout.Emit(rc, command, data)
}

func getStatus(ctx context.Context, gitPath, repoPath string) (map[string]any, error) {
	// Get porcelain status
	result := executil.Run(ctx, "", gitPath, "-C", repoPath, "status", "--porcelain=v1", "-b")
	if result.Err != nil {
		return nil, skillerr.Runtimef("git status failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	files, branch, upstream := parseStatusOutput(string(result.Stdout))

	// Get HEAD info
	var headHash string
	headResult := executil.Run(ctx, "", gitPath, "-C", repoPath, "rev-parse", "HEAD")
	if headResult.Err == nil {
		headHash = strings.TrimSpace(string(headResult.Stdout))
	}

	// Get short hash
	var shortHash string
	shortResult := executil.Run(ctx, "", gitPath, "-C", repoPath, "rev-parse", "--short", "HEAD")
	if shortResult.Err == nil {
		shortHash = strings.TrimSpace(string(shortResult.Stdout))
	}

	data := map[string]any{
		"operation":       "status",
		"branch":          branch,
		"upstream":        upstream,
		"head":            headHash,
		"short_head":      shortHash,
		"file_count":      len(files),
		"files":           files,
		"modified_count":  countByStatus(files, "M"),
		"added_count":     countByStatus(files, "A"),
		"deleted_count":   countByStatus(files, "D"),
		"untracked_count": countByStatus(files, "?"),
	}

	// Check if working tree is clean
	data["clean"] = len(files) == 0

	return data, nil
}

func getDiff(ctx context.Context, rc *skillmain.RunContext, gitPath, repoPath string, in input) (map[string]any, error) {
	args := []string{"-C", repoPath, "diff"}

	if in.Staged {
		args = append(args, "--staged")
	}
	if in.Commit != "" {
		args = append(args, in.Commit)
	}
	if in.Stat {
		args = append(args, "--stat")
	}

	result := executil.Run(ctx, "", gitPath, args...)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git diff failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	diff := string(result.Stdout)

	data := map[string]any{
		"operation": "diff",
		"staged":    in.Staged,
		"commit":    in.Commit,
		"diff_size": len(diff),
	}

	// Add preview or full diff
	truncated := false
	if len(diff) > rc.MaxPreview {
		data["preview"] = skillout.TruncateStringWithSuffix(diff, rc.MaxPreview, "\n... (truncated)")
		truncated = true
	} else {
		data["preview"] = diff
	}

	// Store full diff as artifact if large
	if truncated {
		buf := bytes.NewBufferString(diff)
		artifact, err := skillmain.PersistBuffer(ctx, rc, buf, "text/plain", "git_diff")
		if err == nil {
			skillout.AddArtifact(data, &artifact)
		}
	}

	// Parse stat if available
	if in.Stat {
		stats := parseDiffStat(diff)
		data["files_changed"] = len(stats)
		data["stats"] = stats
	}

	return data, nil
}

func getLog(ctx context.Context, gitPath, repoPath string, in input) (map[string]any, error) {
	format := "--pretty=format:%H%x00%h%x00%an%x00%ai%x00%s"
	args := []string{"-C", repoPath, "log", format, fmt.Sprintf("-%d", in.Limit)}

	if in.Commit != "" {
		args = append(args, in.Commit)
	}

	result := executil.Run(ctx, "", gitPath, args...)
	if result.Err != nil {
		return nil, skillerr.Runtimef("git log failed: %v\nstderr: %s", result.Err, string(result.Stderr))
	}

	commits := parseLogOutput(string(result.Stdout))

	data := map[string]any{
		"operation":    "log",
		"commit_count": len(commits),
		"commits":      commits,
		"limit":        in.Limit,
	}

	return data, nil
}

func parseStatusOutput(output string) ([]fileStatus, string, string) {
	var files []fileStatus
	var branch, upstream string

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		// Parse branch line
		if strings.HasPrefix(line, "##") {
			parts := strings.Fields(line[3:])
			if len(parts) > 0 {
				branch = parts[0]
				if strings.Contains(parts[0], "...") {
					branchParts := strings.Split(parts[0], "...")
					branch = branchParts[0]
					if len(branchParts) > 1 {
						upstream = branchParts[1]
					}
				}
			}
			continue
		}

		// Parse file status
		if len(line) >= 3 {
			status := fileStatus{
				StagingArea: string(line[0]),
				WorkingTree: string(line[1]),
				Path:        strings.TrimSpace(line[3:]),
			}

			// Determine overall status
			if status.StagingArea != " " {
				status.Status = string(status.StagingArea)
			} else if status.WorkingTree != " " {
				status.Status = string(status.WorkingTree)
			}

			files = append(files, status)
		}
	}

	return files, branch, upstream
}

func parseLogOutput(output string) []commitInfo {
	var commits []commitInfo

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\x00")
		if len(parts) >= 5 {
			commits = append(commits, commitInfo{
				Hash:      parts[0],
				ShortHash: parts[1],
				Author:    parts[2],
				Date:      parts[3],
				Message:   parts[4],
			})
		}
	}

	return commits
}

func parseDiffStat(diff string) map[string]map[string]int {
	stats := make(map[string]map[string]int)
	re := regexp.MustCompile(`^\s*(.+?)\s*\|\s*(\d+)\s*([+\-]+)`)

	scanner := bufio.NewScanner(strings.NewReader(diff))
	for scanner.Scan() {
		line := scanner.Text()
		matches := re.FindStringSubmatch(line)
		if len(matches) >= 4 {
			file := strings.TrimSpace(matches[1])
			additions := strings.Count(matches[3], "+")
			deletions := strings.Count(matches[3], "-")
			stats[file] = map[string]int{
				"additions": additions,
				"deletions": deletions,
			}
		}
	}

	return stats
}

func countByStatus(files []fileStatus, status string) int {
	count := 0
	for _, f := range files {
		if f.Status == status || f.StagingArea == status || f.WorkingTree == status {
			count++
		}
	}
	return count
}
