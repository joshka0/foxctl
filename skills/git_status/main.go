// Package main implements the git/status skill.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

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
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("git/status", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("git/status", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("git/status", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("git/status", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	repoPath, err := resolveRepoPath(rc, in.RepoPath)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}

	if _, err := exec.LookPath("git"); err != nil {
		return fmt.Errorf("git command not found: %w", err)
	}

	var data map[string]any

	switch in.Operation {
	case "status":
		data, err = getStatus(ctx, rc, repoPath)
	case "diff":
		data, err = getDiff(ctx, rc, repoPath, in)
	case "log":
		data, err = getLog(ctx, rc, repoPath, in)
	default:
		return fmt.Errorf("invalid operation: %s", in.Operation)
	}

	if err != nil {
		return err
	}

	return rc.Emit("git/status", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if in.Operation == "" {
		in.Operation = "status"
	}
	if in.RepoPath == "" {
		in.RepoPath = "."
	}
	if in.Limit <= 0 {
		in.Limit = 10
	}
	return in, nil
}

func resolveRepoPath(rc *runner.RunnerContext, path string) (string, error) {
	if path == "" {
		path = "."
	}
	valid, err := rc.PathValidator.ValidatePath(path)
	if err != nil {
		return "", fmt.Errorf("path validation failed: %w", err)
	}
	return valid, nil
}

func getStatus(ctx context.Context, _ *runner.RunnerContext, repoPath string) (map[string]any, error) {
	// Get porcelain status
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "status", "--porcelain=v1", "-b")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status failed: %w", err)
	}

	files, branch, upstream := parseStatusOutput(string(output))

	// Get HEAD info
	var headHash string
	headCmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "HEAD")
	if headOutput, err := headCmd.Output(); err == nil {
		headHash = strings.TrimSpace(string(headOutput))
	}

	// Get short hash
	var shortHash string
	shortCmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--short", "HEAD")
	if shortOutput, err := shortCmd.Output(); err == nil {
		shortHash = strings.TrimSpace(string(shortOutput))
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

func getDiff(ctx context.Context, rc *runner.RunnerContext, repoPath string, in input) (map[string]any, error) {
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

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}

	diff := string(output)

	data := map[string]any{
		"operation": "diff",
		"staged":    in.Staged,
		"commit":    in.Commit,
		"diff_size": len(diff),
	}

	// Add preview or full diff
	truncated := false
	if len(diff) > rc.MaxPreview {
		data["preview"] = diff[:rc.MaxPreview] + "\n... (truncated)"
		truncated = true
	} else {
		data["preview"] = diff
	}

	// Store full diff as artifact if large
	if truncated {
		buf := bytes.NewBufferString(diff)
		artifact, err := runner.PersistBuffer(ctx, rc, buf, "text/plain", "git_diff")
		if err == nil && artifact.Digest != "" {
			data["artifact"] = artifact.Digest
			data["artifact_kind"] = artifact.Kind
			data["artifact_size_bytes"] = artifact.Size
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

func getLog(ctx context.Context, _ *runner.RunnerContext, repoPath string, in input) (map[string]any, error) {
	format := "--pretty=format:%H%x00%h%x00%an%x00%ai%x00%s"
	args := []string{"-C", repoPath, "log", format, fmt.Sprintf("-%d", in.Limit)}

	if in.Commit != "" {
		args = append(args, in.Commit)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log failed: %w", err)
	}

	commits := parseLogOutput(string(output))

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

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit git/status failure")
	os.Exit(1)
}
