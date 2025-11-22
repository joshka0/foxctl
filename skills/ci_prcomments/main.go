// Package main implements the ci/prcomments skill.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	PR          string `json:"pr"`
	Owner       string `json:"owner"`
	Repo        string `json:"repo"`
	WithContext bool   `json:"with_context"`
	Format      string `json:"format"`
	OutputPath  string `json:"output_path"`
	ErrorsOnly  bool   `json:"errors_only"`
}

type Comment struct {
	ID        int       `json:"id"`
	User      User      `json:"user"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	Path      string    `json:"path,omitempty"`
	Line      *int      `json:"line,omitempty"`
}

type User struct {
	Login string `json:"login"`
}

type PRInfo struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	Number     int    `json:"number"`
	User       User   `json:"user"`
	Mergeable  *bool  `json:"mergeable"`
	MergeState string `json:"mergeable_state"`
}

type CheckRun struct {
	ID         int         `json:"id"`
	Name       string      `json:"name"`
	Status     string      `json:"status"`
	Conclusion string      `json:"conclusion"`
	Output     CheckOutput `json:"output"`
	HTMLURL    string      `json:"html_url"`
}

type CheckOutput struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Text    string `json:"text"`
}

type JobDetails struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"status"`
	Conclusion  string    `json:"conclusion"`
	StartedAt   string    `json:"started_at"`
	CompletedAt string    `json:"completed_at"`
	Steps       []JobStep `json:"steps"`
	HTMLURL     string    `json:"html_url"`
}

type JobStep struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	Number      int    `json:"number"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

type TaskSummary struct {
	Total          int `json:"total"`
	MergeConflicts int `json:"merge_conflicts"`
	CIFailures     int `json:"ci_failures"`
	ReviewComments int `json:"review_comments"`
}

type TaskItem struct {
	Kind          string `json:"kind"`
	Summary       string `json:"summary"`
	Source        string `json:"source,omitempty"`
	Severity      string `json:"severity,omitempty"`
	CheckName     string `json:"check_name,omitempty"`
	CheckURL      string `json:"check_url,omitempty"`
	File          string `json:"file,omitempty"`
	Line          *int   `json:"line,omitempty"`
	CommentAuthor string `json:"comment_author,omitempty"`
	CommentBody   string `json:"comment_body,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("ci/prcomments", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("ci/prcomments", "ERUNTIME", err)
	}
	defer func() {
		errors.Ignore(rc.Close(), "runner context close")
	}()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("ci/prcomments", "EARG", fmt.Errorf("decode input: %w", err))
	}
	if err := run(ctx, rc, in); err != nil {
		fail("ci/prcomments", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	prRef := strings.TrimSpace(in.PR)
	if prRef == "" {
		return fmt.Errorf("pr is required")
	}

	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" {
		format = "markdown"
	}

	owner := strings.TrimSpace(in.Owner)
	repo := strings.TrimSpace(in.Repo)

	if repo != "" && strings.Contains(repo, "/") {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) == 2 {
			if owner == "" {
				owner = parts[0]
			}
			repo = parts[1]
		}
	}

	if owner == "" {
		owner = strings.TrimSpace(os.Getenv("GITHUB_OWNER"))
	}
	if repo == "" {
		repo = strings.TrimSpace(os.Getenv("GITHUB_REPO"))
	}

	if owner == "" || repo == "" {
		detectedOwner, detectedRepo, err := detectRepo(ctx)
		if err == nil {
			if owner == "" {
				owner = detectedOwner
			}
			if repo == "" {
				repo = detectedRepo
			}
		} else {
			log.Printf("warning: could not auto-detect repository: %v", err)
		}
	}

	if owner == "" || repo == "" {
		return fmt.Errorf("repository owner and name are required; set owner/repo, GITHUB_OWNER/GITHUB_REPO, or run in a git repository with origin set")
	}

	token, err := resolveToken(ctx)
	if err != nil {
		return err
	}

	prNum, err := resolvePRNumber(owner, repo, prRef, token)
	if err != nil {
		return fmt.Errorf("resolve PR: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}

	prInfo, err := getPR(client, token, owner, repo, prNum)
	if err != nil {
		return fmt.Errorf("get PR info: %w", err)
	}

	var conflictingFiles []string
	if prInfo.Mergeable != nil && !*prInfo.Mergeable {
		conflictingFiles = getChangedFilesForPR(client, token, owner, repo, prNum)
	}

	issueComments, err := getIssueComments(client, token, owner, repo, prNum)
	if err != nil {
		log.Printf("warning: failed to get issue comments: %v", err)
	}

	reviewComments, err := getReviewComments(client, token, owner, repo, prNum)
	if err != nil {
		log.Printf("warning: failed to get review comments: %v", err)
	}

	checkRuns, err := getCheckRuns(client, token, owner, repo, prNum)
	if err != nil {
		log.Printf("warning: failed to get CI check runs: %v", err)
	}

	allComments := append(issueComments, reviewComments...)
	sortCommentsByTime(allComments)

	markdown, summary := buildMarkdownReport(prInfo, owner, repo, prNum, allComments, checkRuns, conflictingFiles, client, token, in.WithContext, in.ErrorsOnly)

	inlineBytes := rc.InlineKB * 1024
	preview := markdown
	truncated := false
	if inlineBytes > 0 && len(preview) > inlineBytes {
		preview = preview[:inlineBytes]
		truncated = true
	}

	tasksList := buildTasksList(conflictingFiles, allComments, checkRuns)
	hasBlockingIssues := len(tasksList) > 0

	data := map[string]any{
		"repository": fmt.Sprintf("%s/%s", owner, repo),
		"pr_number":  prNum,
		"title":      prInfo.Title,
		"author":     prInfo.User.Login,
		"url":        fmt.Sprintf("https://github.com/%s/%s/pull/%d", owner, repo, prNum),
		"tasks":      summary,
		"status": map[string]bool{
			"has_merge_conflicts": summary.MergeConflicts > 0,
			"has_failing_ci":      summary.CIFailures > 0,
			"has_review_tasks":    summary.ReviewComments > 0,
		},
		"has_blocking_issues": hasBlockingIssues,
		"tasks_list":          tasksList,
		"markdown_preview":    preview,
		"markdown_truncated":  truncated,
		"format":              format,
		"errors_only":         in.ErrorsOnly,
		"with_context":        in.WithContext,
	}

	if in.OutputPath != "" {
		validated, err := rc.PathValidator.ValidatePath(in.OutputPath)
		if err != nil {
			return fmt.Errorf("validate output_path: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(validated), 0o755); err != nil {
			return fmt.Errorf("mkdir output_path: %w", err)
		}
		if err := os.WriteFile(validated, []byte(markdown), 0o644); err != nil {
			return fmt.Errorf("write output_path: %w", err)
		}
		data["markdown_output_path"] = in.OutputPath
	}

	if truncated {
		buf := bytes.NewBufferString(markdown)
		artifact, err := runner.PersistBuffer(ctx, rc, buf, "text/markdown", "ci_prcomments")
		if err == nil && artifact.Digest != "" {
			data["markdown_artifact"] = artifact.Digest
			data["artifact_kind"] = artifact.Kind
			data["artifact_size_bytes"] = artifact.Size
		}
	}

	if format == "json" {
		data["comments"] = allComments
	}

	return rc.Emit("ci/prcomments", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func detectRepo(ctx context.Context) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("detect repo: %w", err)
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", "", fmt.Errorf("empty remote url")
	}

	// Support https and ssh formats
	if strings.HasPrefix(url, "git@") {
		// git@github.com:owner/repo.git
		parts := strings.SplitN(url, ":", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("unexpected ssh remote format: %s", url)
		}
		path := strings.TrimSuffix(parts[1], ".git")
		sub := strings.SplitN(path, "/", 2)
		if len(sub) != 2 {
			return "", "", fmt.Errorf("unexpected ssh path format: %s", path)
		}
		return sub[0], sub[1], nil
	}

	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		// https://github.com/owner/repo.git
		parts := strings.Split(url, "/")
		if len(parts) < 2 {
			return "", "", fmt.Errorf("unexpected https remote format: %s", url)
		}
		owner := parts[len(parts)-2]
		repo := strings.TrimSuffix(parts[len(parts)-1], ".git")
		return owner, repo, nil
	}

	return "", "", fmt.Errorf("unsupported remote url format: %s", url)
}

func resolveToken(ctx context.Context) (string, error) {
	token := strings.TrimSpace(os.Getenv("GITHUB_TOKEN"))
	if token != "" {
		return token, nil
	}

	if _, err := exec.LookPath("gh"); err == nil {
		cmd := exec.CommandContext(ctx, "gh", "auth", "token")
		out, err := cmd.Output()
		if err == nil {
			candidate := strings.TrimSpace(string(out))
			if candidate != "" {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("GitHub token is required; set GITHUB_TOKEN or configure gh auth token")
}

func resolvePRNumber(owner, repo, prRef, token string) (int, error) {
	if prNum, err := strconv.Atoi(prRef); err == nil {
		return prNum, nil
	}

	client := &http.Client{Timeout: 30 * time.Second}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls?head=%s:%s&state=all", owner, repo, owner, prRef)

	var prs []PRInfo
	if err := githubGET(client, token, url, &prs); err != nil {
		return 0, err
	}
	if len(prs) == 0 {
		return 0, fmt.Errorf("no PR found for branch %q", prRef)
	}
	return prs[0].Number, nil
}

func getPR(client *http.Client, token, owner, repo string, prNum int) (*PRInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, prNum)
	var pr PRInfo
	if err := githubGET(client, token, url, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func getIssueComments(client *http.Client, token, owner, repo string, prNum int) ([]Comment, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repo, prNum)
	var comments []Comment
	if err := githubGET(client, token, url, &comments); err != nil {
		return nil, err
	}
	return comments, nil
}

func getReviewComments(client *http.Client, token, owner, repo string, prNum int) ([]Comment, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/comments", owner, repo, prNum)
	var comments []Comment
	if err := githubGET(client, token, url, &comments); err != nil {
		return nil, err
	}
	return comments, nil
}

func getCheckRuns(client *http.Client, token, owner, repo string, prNum int) ([]CheckRun, error) {
	prURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, prNum)
	var pr struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := githubGET(client, token, prURL, &pr); err != nil {
		return nil, err
	}

	checksURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s/check-runs", owner, repo, pr.Head.SHA)
	var response struct {
		CheckRuns []CheckRun `json:"check_runs"`
	}
	if err := githubGET(client, token, checksURL, &response); err != nil {
		return nil, err
	}

	var detailed []CheckRun
	for _, check := range response.CheckRuns {
		if check.Conclusion == "failure" || check.Conclusion == "error" {
			full, err := getCheckRunDetails(client, token, owner, repo, check.ID)
			if err != nil {
				detailed = append(detailed, check)
			} else {
				detailed = append(detailed, *full)
			}
		} else {
			detailed = append(detailed, check)
		}
	}
	return detailed, nil
}

func getCheckRunDetails(client *http.Client, token, owner, repo string, checkID int) (*CheckRun, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/check-runs/%d", owner, repo, checkID)
	var check CheckRun
	if err := githubGET(client, token, url, &check); err != nil {
		return nil, err
	}
	return &check, nil
}

func getJobDetails(client *http.Client, token, owner, repo, jobURL string) (*JobDetails, error) {
	parts := strings.Split(jobURL, "/")
	if len(parts) < 8 {
		return nil, fmt.Errorf("invalid job URL format")
	}
	jobID := parts[len(parts)-1]

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/jobs/%s", owner, repo, jobID)
	var job JobDetails
	if err := githubGET(client, token, url, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func getJobErrorOutput(client *http.Client, token, owner, repo string, jobID int, failedStepName string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	clientNoRedirect := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	resp, err := clientNoRedirect.Do(req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		location := resp.Header.Get("Location")
		if location != "" {
			redirectReq, err := http.NewRequest("GET", location, nil)
			if err != nil {
				return "", err
			}
			redirectResp, err := client.Do(redirectReq)
			if err != nil {
				return "", err
			}
			defer func() {
				_ = redirectResp.Body.Close()
			}()
			resp = redirectResp
		}
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	contentType := resp.Header.Get("Content-Type")
	contentDisposition := resp.Header.Get("Content-Disposition")

	if strings.Contains(contentType, "zip") || strings.Contains(contentDisposition, ".zip") || bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", fmt.Errorf("failed to parse ZIP: %v", err)
		}

		var bestMatch string
		var bestScore int

		for _, file := range zipReader.File {
			if !strings.HasSuffix(file.Name, ".txt") {
				continue
			}
			rc, err := file.Open()
			if err != nil {
				continue
			}
			content, err := io.ReadAll(rc)
			if errClose := rc.Close(); errClose != nil {
				continue
			}
			if err != nil {
				continue
			}
			logContent := string(content)
			score := 0
			if strings.Contains(strings.ToLower(file.Name), strings.ToLower(failedStepName)) {
				score += 10
			}
			if strings.Contains(logContent, "Error:") || strings.Contains(logContent, "error") {
				score += 5
			}
			if strings.Contains(logContent, "exit code") || strings.Contains(logContent, "failed") {
				score += 3
			}
			if strings.Contains(logContent, "diff --git") || strings.Contains(logContent, "Formatting changed files") {
				score += 15
			}

			lines := strings.Split(logContent, "\n")
			var errorLines []string
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if strings.Contains(strings.ToLower(line), "error:") ||
					strings.Contains(strings.ToLower(line), "failed") ||
					strings.Contains(line, "exit code") ||
					strings.Contains(line, "command not found") ||
					strings.Contains(line, "no such file") ||
					strings.Contains(line, "diff --git") ||
					strings.Contains(line, "Formatting changed files") ||
					strings.Contains(line, "Process completed with exit code") {
					errorLines = append(errorLines, line)
				}
			}

			if len(errorLines) > 0 {
				score += 20
				extracted := strings.Join(errorLines, "\n")
				if len(errorLines) > 40 {
					extracted = strings.Join(errorLines[len(errorLines)-40:], "\n")
				}
				if score > bestScore {
					bestMatch = extracted
					bestScore = score
				}
			} else if score > bestScore {
				if len(lines) > 20 {
					bestMatch = strings.Join(lines[len(lines)-20:], "\n")
				} else {
					bestMatch = logContent
				}
				bestScore = score
			}
		}

		if bestMatch != "" {
			return bestMatch, nil
		}
		return "", fmt.Errorf("no relevant log content found in ZIP")
	}

	logContent := string(data)
	lines := strings.Split(logContent, "\n")
	var stepOutput []string
	inFailedStep := false
	foundError := false

	for _, line := range lines {
		if strings.Contains(line, "##[group]Run make fmt") || (strings.Contains(line, "##[group]") && strings.Contains(line, failedStepName)) {
			inFailedStep = true
			stepOutput = append(stepOutput, cleanGitHubLogLine(line))
			continue
		}
		if inFailedStep {
			cleaned := cleanGitHubLogLine(line)
			if cleaned != "" {
				stepOutput = append(stepOutput, cleaned)
			}
			if strings.Contains(line, "##[error]Process completed with exit code") {
				foundError = true
				break
			}
		}
	}

	if len(stepOutput) > 0 && foundError {
		return strings.Join(stepOutput, "\n"), nil
	}

	var errorLines []string
	for _, line := range lines {
		cleaned := cleanGitHubLogLine(line)
		if cleaned != "" && (strings.Contains(cleaned, "error:") ||
			strings.Contains(cleaned, "Error:") ||
			strings.Contains(cleaned, "failed") ||
			strings.Contains(cleaned, "exit code") ||
			strings.Contains(cleaned, "diff --git") ||
			strings.Contains(cleaned, "Formatting changed files")) {
			errorLines = append(errorLines, cleaned)
		}
	}

	if len(errorLines) > 0 {
		if len(errorLines) > 40 {
			return strings.Join(errorLines[len(errorLines)-40:], "\n"), nil
		}
		return strings.Join(errorLines, "\n"), nil
	}

	if len(lines) > 20 {
		return strings.Join(lines[len(lines)-20:], "\n"), nil
	}
	return logContent, nil
}

func githubGET(client *http.Client, token, url string, v any) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return err
	}
	return nil
}

func sortCommentsByTime(comments []Comment) {
	sort.Slice(comments, func(i, j int) bool {
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})
}

func buildMarkdownReport(prInfo *PRInfo, owner, repo string, prNum int, comments []Comment, checkRuns []CheckRun, conflictingFiles []string, client *http.Client, token string, withContext, errorsOnly bool) (string, TaskSummary) {
	var buf bytes.Buffer

	fmt.Fprintf(&buf, "# Tasks for PR #%d: %s\n\n", prInfo.Number, prInfo.Title)
	fmt.Fprintf(&buf, "**Repository:** %s/%s\n", owner, repo)
	fmt.Fprintf(&buf, "**URL:** https://github.com/%s/%s/pull/%d\n\n", owner, repo, prNum)

	if withContext && !errorsOnly && strings.TrimSpace(prInfo.Body) != "" {
		fmt.Fprintf(&buf, "## Context\n\n%s\n\n", prInfo.Body)
	}

	taskNum := 1
	var summary TaskSummary

	if prInfo.Mergeable != nil && !*prInfo.Mergeable && len(conflictingFiles) > 0 {
		summary.MergeConflicts = 1
		fmt.Fprintf(&buf, "## ⚠️ PR has merge conflicts\n\n")
		fmt.Fprintf(&buf, "### Task %d: Resolve merge conflicts\n\n", taskNum)
		fmt.Fprintf(&buf, "**⚠️ IMPORTANT:** When resolving conflicts, make sure to understand fully how to properly synthesize any changes coming in and the current ones when comparing. Don't just accept one or the other without getting context!\n\n")
		fmt.Fprintf(&buf, "**Files changed in this PR:**\n")
		for _, file := range conflictingFiles {
			fmt.Fprintf(&buf, "- `%s`\n", file)
		}
		fmt.Fprintf(&buf, "\n---\n\n")
		taskNum++
	}

	var failedChecks []CheckRun
	for _, check := range checkRuns {
		if check.Conclusion == "failure" || check.Conclusion == "error" || check.Conclusion == "cancelled" {
			failedChecks = append(failedChecks, check)
		}
	}
	summary.CIFailures = len(failedChecks)

	if len(failedChecks) > 0 {
		fmt.Fprintf(&buf, "## 🔴 CI Failures to Fix (%d)\n\n", len(failedChecks))
		for _, check := range failedChecks {
			fmt.Fprintf(&buf, "### Task %d: Fix %s\n\n", taskNum, check.Name)
			taskNum++
			if strings.Contains(check.HTMLURL, "/actions/runs/") {
				jobDetails, err := getJobDetails(client, token, owner, repo, check.HTMLURL)
				if err == nil && jobDetails != nil {
					displayJobDetailsTaskFormat(&buf, jobDetails, client, token, owner, repo)
				} else {
					fmt.Fprintf(&buf, "**URL:** %s\n\n", check.HTMLURL)
				}
			} else {
				fmt.Fprintf(&buf, "**URL:** %s\n\n", check.HTMLURL)
			}
			fmt.Fprintf(&buf, "---\n\n")
		}
	}

	commentCount := 0
	if len(comments) > 0 {
		fmt.Fprintf(&buf, "## 📝 Review Comments to Address (%d)\n\n", len(comments))
		for _, comment := range comments {
			cleanBody := cleanCommentBody(comment.Body)
			if cleanBody == "" {
				continue
			}
			commentCount++
			fmt.Fprintf(&buf, "### Task %d: %s's feedback\n\n", taskNum, comment.User.Login)
			if comment.Path != "" {
				location := comment.Path
				if comment.Line != nil {
					location += fmt.Sprintf(":%d", *comment.Line)
				}
				fmt.Fprintf(&buf, "**File:** `%s`\n\n", location)
			}
			fmt.Fprintf(&buf, "%s\n\n", cleanBody)
			if withContext {
				fmt.Fprintf(&buf, "_Posted: %s_\n\n", comment.CreatedAt.Format("2006-01-02 15:04:05"))
			}
			fmt.Fprintf(&buf, "---\n\n")
			taskNum++
		}
	}
	summary.ReviewComments = commentCount
	summary.Total = summary.MergeConflicts + summary.CIFailures + summary.ReviewComments

	if summary.Total == 0 {
		if !errorsOnly {
			fmt.Fprintf(&buf, "## ✅ No outstanding tasks\n\nAll CI checks passed, no merge conflicts, and no review comments to address.")
		}
	} else if !errorsOnly {
		fmt.Fprintf(&buf, "## Summary\n\n")
		fmt.Fprintf(&buf, "**Total tasks:** %d\n", summary.Total)
		if summary.MergeConflicts > 0 {
			fmt.Fprintf(&buf, "- ⚠️ Merge conflicts: PR is not mergeable (changed files: %d)\n", len(conflictingFiles))
		}
		if summary.CIFailures > 0 {
			fmt.Fprintf(&buf, "- 🔴 CI failures: %d\n", summary.CIFailures)
		}
		if summary.ReviewComments > 0 {
			fmt.Fprintf(&buf, "- 📝 Review comments: %d\n", summary.ReviewComments)
		}
	}

	return buf.String(), summary
}

func getChangedFilesForPR(client *http.Client, token, owner, repo string, prNum int) []string {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/files", owner, repo, prNum)
	var files []struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
	}
	if err := githubGET(client, token, url, &files); err != nil {
		log.Printf("warning: failed to get PR files: %v", err)
		return nil
	}
	var conflicting []string
	for _, f := range files {
		conflicting = append(conflicting, f.Filename)
	}
	return conflicting
}

func buildTasksList(conflictingFiles []string, comments []Comment, checkRuns []CheckRun) []TaskItem {
	tasks := make([]TaskItem, 0)
	if len(conflictingFiles) > 0 {
		tasks = append(tasks, TaskItem{
			Kind:    "merge_conflict",
			Summary: fmt.Sprintf("Resolve merge conflicts in this PR (changed files: %d)", len(conflictingFiles)),
		})
	}
	for _, check := range checkRuns {
		if check.Conclusion == "failure" || check.Conclusion == "error" || check.Conclusion == "cancelled" {
			item := TaskItem{
				Kind:      "ci_failure",
				Summary:   fmt.Sprintf("Fix CI check %s", check.Name),
				CheckName: check.Name,
				CheckURL:  check.HTMLURL,
			}
			tasks = append(tasks, item)
		}
	}
	for _, comment := range comments {
		cleanBody := cleanCommentBody(comment.Body)
		if cleanBody == "" {
			continue
		}
		item := TaskItem{
			Kind:          "review_comment",
			Summary:       fmt.Sprintf("Address review comment from %s", comment.User.Login),
			CommentAuthor: comment.User.Login,
			CommentBody:   cleanBody,
		}
		if comment.User.Login == "coderabbitai[bot]" {
			item.Source = "coderabbit"
			sev, summary := classifyCodeRabbitTask(cleanBody)
			if sev != "" {
				item.Severity = sev
			}
			if summary != "" {
				item.Summary = summary
			}
		}
		if comment.Path != "" {
			item.File = comment.Path
			if comment.Line != nil {
				item.Line = comment.Line
			}
		}
		tasks = append(tasks, item)
	}
	return tasks
}

func displayJobDetailsTaskFormat(buf *bytes.Buffer, job *JobDetails, client *http.Client, token, owner, repo string) {
	var failedStep *JobStep
	for i := len(job.Steps) - 1; i >= 0; i-- {
		step := &job.Steps[i]
		if step.Conclusion == "failure" || step.Conclusion == "error" {
			failedStep = step
			break
		}
	}
	if failedStep != nil {
		fmt.Fprintf(buf, "**Failed step:** %s\n", failedStep.Name)
		fmt.Fprintf(buf, "**URL:** %s\n\n", job.HTMLURL)
		output, err := getJobErrorOutput(client, token, owner, repo, job.ID, failedStep.Name)
		if err != nil {
			log.Printf("warning: failed to get job error output: %v", err)
		} else if output != "" {
			fmt.Fprintf(buf, "**Error output:**\n\n```\n%s\n```\n\n", output)
		}
	}
}

func cleanGitHubLogLine(line string) string {
	pattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{7}Z\s+`)
	line = pattern.ReplaceAllString(line, "")
	line = strings.Replace(line, "##[group]", "", 1)
	line = strings.Replace(line, "##[endgroup]", "", 1)
	line = strings.Replace(line, "##[error]", "Error: ", 1)
	line = strings.Replace(line, "##[warning]", "Warning: ", 1)
	line = strings.Replace(line, "##[command]", "", 1)
	return strings.TrimSpace(line)
}

func cleanCommentBody(body string) string {
	if body == "" {
		return ""
	}

	body = regexp.MustCompile(`(?s)<!-- internal state.*?<!-- internal state end -->`).ReplaceAllString(body, "")

	if strings.Contains(body, "chatgpt-codex-connector[bot]") {
		return extractCodexComment(body)
	}
	if strings.Contains(body, "coderabbitai[bot]") {
		return extractCodeRabbitComment(body)
	}

	htmlCommentRegex := regexp.MustCompile(`(?s)<!--.*?-->`)
	body = htmlCommentRegex.ReplaceAllString(body, "")

	lines := strings.Split(body, "\n")
	var cleaned []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "_⚠️") &&
			!strings.HasPrefix(trimmed, "_🔴") &&
			!strings.HasPrefix(trimmed, "_📜") &&
			!strings.HasPrefix(trimmed, "_🧰") &&
			!strings.HasPrefix(trimmed, "_🔇") &&
			!strings.HasPrefix(trimmed, "📥") &&
			!strings.HasPrefix(trimmed, "📒") &&
			!strings.HasPrefix(trimmed, "💤") &&
			!strings.Contains(trimmed, "Comment @") &&
			!strings.Contains(trimmed, "This is an auto-generated") &&
			trimmed != "" {
			cleaned = append(cleaned, strings.TrimRight(line, " \t"))
		}
	}

	for len(cleaned) > 0 && strings.TrimSpace(cleaned[0]) == "" {
		cleaned = cleaned[1:]
	}
	for len(cleaned) > 0 && strings.TrimSpace(cleaned[len(cleaned)-1]) == "" {
		cleaned = cleaned[:len(cleaned)-1]
	}

	if len(cleaned) > 0 {
		return strings.Join(cleaned, "\n")
	}
	return ""
}

func extractCodexComment(body string) string {
	lines := strings.Split(body, "\n")
	var result []string
	inContent := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "chatgpt-codex-connector[bot]") {
			inContent = true
			continue
		}
		if strings.Contains(trimmed, "Useful? React with 👍") {
			break
		}
		if inContent && trimmed != "" {
			result = append(result, strings.TrimRight(line, " \t"))
		}
	}
	if len(result) > 0 {
		return strings.Join(result, "\n")
	}
	return ""
}

func classifyCodeRabbitTask(cleanBody string) (severity, summary string) {
	lines := strings.Split(cleanBody, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "critical") || strings.Contains(trimmed, "🔴") {
			severity = "critical"
			break
		}
		if strings.Contains(lower, "major") || strings.Contains(trimmed, "🟠") {
			severity = "major"
			break
		}
		if strings.Contains(lower, "minor") || strings.Contains(trimmed, "🟡") {
			severity = "minor"
			break
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "potential issue") {
			continue
		}
		if strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") {
			continue
		}
		clean := strings.Trim(trimmed, "*` ")
		clean = strings.TrimLeft(clean, "# ")
		if clean == "" {
			continue
		}
		summary = clean
		break
	}

	return severity, summary
}

func extractCodeRabbitComment(body string) string {
	body = regexp.MustCompile(`(?s)<!-- internal state.*?<!-- internal state end -->`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?s)<!-- tips_.*?-->`).ReplaceAllString(body, "")
	body = regexp.MustCompile(`(?s)<!-- DwQgtGAEAqAWCWBnSTIEMB26CuAXA9mAOYCmGJATmriQCaQDG\+Ats2bgFyQAOFk\+.*?-->`).ReplaceAllString(body, "")

	endMarker := "<!-- This is an auto-generated comment by CodeRabbit -->"
	if endIndex := strings.Index(body, endMarker); endIndex != -1 {
		body = body[:endIndex]
	}

	lines := strings.Split(body, "\n")
	var result []string
	inContent := false
	inReminders := false
	inCode := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "coderabbitai[bot]") {
			inContent = true
			continue
		}
		if !inContent {
			continue
		}
		if trimmed == "" {
			continue
		}
		// Drop top-level Reminders block until the first horizontal rule
		if strings.HasPrefix(trimmed, "Reminders") || strings.HasPrefix(trimmed, "# Reminders") {
			inReminders = true
			continue
		}
		if inReminders {
			if trimmed == "---" {
				inReminders = false
			}
			continue
		}
		// Drop obvious meta/prompt scaffolding that is not actionable code review content
		if strings.HasPrefix(trimmed, "<details>") || strings.HasPrefix(trimmed, "</details>") ||
			strings.HasPrefix(trimmed, "<summary>") || strings.HasPrefix(trimmed, "</summary>") ||
			strings.HasPrefix(trimmed, "<sub>") || strings.HasPrefix(trimmed, "</sub>") {
			continue
		}
		if strings.Contains(trimmed, "Prompt for AI Agents") || strings.HasPrefix(trimmed, "✅ Addressed in commit") {
			continue
		}
		// Drop code fences and everything inside; these are usually long AI prompts or suggestions
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		result = append(result, strings.TrimRight(line, " \t"))
	}
	if len(result) > 0 {
		return strings.Join(result, "\n")
	}
	return ""
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errors.Ignore(envelope.Write(os.Stdout, env), "emit ci/prcomments failure")
	os.Exit(1)
}
