// Package main implements the ci/prcomments skill.
// Generates task-focused PR reports with merge conflicts, CI failures, and review comments.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	cihelpers "github.com/joshka0/foxctl/internal/adapters/skillslib/ci"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/rs/zerolog"
)

// logger is the package-level structured logger that writes JSON to stderr.
var logger = zerolog.New(os.Stderr).With().Str("component", "ci_prcomments").Timestamp().Logger()

// Input defines the skill input parameters for ci/prcomments operations.
type Input struct {
	PR              skillmain.FlexString `json:"pr" validate:"required"`
	Owner           string               `json:"owner,omitempty"`
	Repo            string               `json:"repo,omitempty"`
	WithContext     bool                 `json:"with_context,omitempty"`
	Format          string               `json:"format,omitempty"`
	OutputPath      string               `json:"output_path,omitempty"`
	ErrorsOnly      bool                 `json:"errors_only,omitempty"`
	IncludeResolved bool                 `json:"include_resolved,omitempty"` // Include resolved/addressed comments (default: false)
}

// Comment represents a GitHub comment with metadata and resolution status.
type Comment struct {
	ID        int       `json:"id"`
	User      User      `json:"user"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	Path      string    `json:"path,omitempty"`
	Line      *int      `json:"line,omitempty"`
	Resolved  bool      `json:"resolved,omitempty"` // Thread is resolved/addressed
	Outdated  bool      `json:"outdated,omitempty"` // Comment is on outdated code
}

// User represents a GitHub user with login information.
type User struct {
	Login string `json:"login"`
}

// PRInfo represents basic pull request information from GitHub API.
type PRInfo struct {
	Title      string `json:"title"`
	Body       string `json:"body"`
	Number     int    `json:"number"`
	User       User   `json:"user"`
	Mergeable  *bool  `json:"mergeable"`
	MergeState string `json:"mergeable_state"`
}

// CheckRun represents a GitHub check run from the API response.
type CheckRun struct {
	ID         int         `json:"id"`
	Name       string      `json:"name"`
	Status     string      `json:"status"`
	Conclusion string      `json:"conclusion"`
	Output     CheckOutput `json:"output"`
	HTMLURL    string      `json:"html_url"`
}

// CheckOutput represents the output text of a GitHub check run.
type CheckOutput struct {
	Title   string `json:"title"`
	Summary string `json:"summary"`
	Text    string `json:"text"`
}

// JobDetails represents GitHub Actions job details including steps.
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

// JobStep represents a single step within a GitHub Actions job.
type JobStep struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	Number      int    `json:"number"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

// TaskSummary represents a summary of task counts by category.
type TaskSummary struct {
	Total          int `json:"total"`
	MergeConflicts int `json:"merge_conflicts"`
	CIFailures     int `json:"ci_failures"`
	ReviewComments int `json:"review_comments"`
}

// TaskItem represents an individual task item extracted from PR analysis.
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
	Resolved      bool   `json:"resolved,omitempty"`
	Outdated      bool   `json:"outdated,omitempty"`
}

// PRReview represents a GitHub PR review with body content and state.
type PRReview struct {
	ID        int       `json:"id"`
	User      User      `json:"user"`
	Body      string    `json:"body"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"submitted_at"`
}

// main is the skill entry point for ci/prcomments.
func main() {
	skillmain.Main("ci/prcomments", run)
}

// run orchestrates PR comment analysis and task report generation.
//
// Index:
// - Purpose: Generate task-focused PR reports with merge conflicts, CI failures, and review comments
// - Flow: resolve PR → fetch comments/checks → enrich with job details → extract AI prompts → build markdown → emit structured output
// - SideEffects: GitHub API calls; optional file output; artifact persistence for large content
// - FailureModes: invalid PR, missing token, GitHub API errors, network timeouts, file write errors
// - Observability: emits repository/pr_number/title/author/url/tasks/status/has_blocking_issues/tasks_list/markdown_preview/markdown_truncated/format/errors_only/with_context/comments/coderabbit_ai_prompt/markdown_output_path/markdown_artifact
// - Related: getPR, getIssueComments, getReviewComments, getCheckRuns, getJobDetails, buildMarkdownReport, buildTasksList, cihelpers.ResolveOwnerRepo, cihelpers.ResolveToken
// - Keywords: ci/prcomments, pr, comments, check_runs, merge_conflicts, tasks, markdown, coderabbit, greptile, errors_only, with_context
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	prRef := strings.TrimSpace(in.PR.String())

	format := strings.ToLower(strings.TrimSpace(in.Format))
	if format == "" {
		format = "markdown"
	}

	owner, repo, err := cihelpers.ResolveOwnerRepo(ctx, in.Owner, in.Repo)
	if err != nil {
		return err
	}

	token, err := cihelpers.ResolveToken(ctx)
	if err != nil {
		return err
	}

	prNum, err := cihelpers.ResolvePRNumber(owner, repo, prRef, token)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 30 * time.Second}

	var prInfo *PRInfo
	err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var e error
		prInfo, e = getPR(client, token, owner, repo, prNum)
		return e
	})
	if err != nil {
		return err
	}

	var conflictingFiles []string
	if prInfo.Mergeable != nil && !*prInfo.Mergeable {
		conflictingFiles = getChangedFilesForPR(client, token, owner, repo, prNum)
	}

	var issueComments []Comment
	err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var e error
		issueComments, e = getIssueComments(client, token, owner, repo, prNum)
		return e
	})
	if err != nil {
		logger.Warn().Err(err).Str("operation", "get-issue-comments").Int("pr", prNum).Msg("failed to get issue comments")
	}

	var reviewComments []Comment
	err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var e error
		reviewComments, e = getReviewComments(client, token, owner, repo, prNum)
		return e
	})
	if err != nil {
		logger.Warn().Err(err).Str("operation", "get-review-comments").Int("pr", prNum).Msg("failed to get review comments")
	}

	// Fetch PR reviews for CodeRabbit "Fix all issues with AI Agents" content
	var codeRabbitAIPrompt string
	var prReviews []PRReview
	err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var e error
		prReviews, e = getPRReviews(client, token, owner, repo, prNum)
		return e
	})
	if err != nil {
		logger.Warn().Err(err).Str("operation", "get-pr-reviews").Int("pr", prNum).Msg("failed to get PR reviews")
	} else {
		codeRabbitAIPrompt = extractCodeRabbitAIAgentPrompts(prReviews)
		if codeRabbitAIPrompt != "" {
			logger.Info().Str("operation", "extract-ai-prompt").Int("pr", prNum).Int("length", len(codeRabbitAIPrompt)).Msg("found CodeRabbit AI agent prompt")
		}
	}

	// Fetch resolved/outdated status via GraphQL and merge into review comments
	var threadStatuses map[int]ReviewThreadStatus
	err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var e error
		threadStatuses, e = getReviewThreadStatuses(client, token, owner, repo, prNum)
		return e
	})
	if err != nil {
		logger.Warn().Err(err).Str("operation", "get-review-thread-statuses").Int("pr", prNum).Msg("failed to get review thread statuses")
	} else {
		mergeResolvedStatus(reviewComments, threadStatuses)
	}

	var checkRuns []CheckRun
	err = skillmain.GuardCall(rc, skillmain.BreakerHTTP, ctx, func(ctx context.Context) error {
		var e error
		checkRuns, e = getCheckRuns(client, token, owner, repo, prNum)
		return e
	})
	if err != nil {
		logger.Warn().Err(err).Str("operation", "get-check-runs").Int("pr", prNum).Msg("failed to get CI check runs")
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

	tasksList := buildTasksList(conflictingFiles, allComments, checkRuns, in.IncludeResolved)
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

	// Add CodeRabbit "Fix all issues with AI Agents" consolidated prompt if available
	if codeRabbitAIPrompt != "" {
		data["coderabbit_ai_prompt"] = codeRabbitAIPrompt
		data["has_coderabbit_ai_prompt"] = true
	}

	if in.OutputPath != "" {
		validated, err := skillmain.ValidatePath(rc, in.OutputPath, skillmain.WithPathMessage("validate output_path"))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(validated), 0o755); err != nil {
			return skillerr.WrapIO("mkdir output_path", err)
		}
		if err := os.WriteFile(validated, []byte(markdown), 0o644); err != nil {
			return skillerr.WrapIO("write output_path", err)
		}
		data["markdown_output_path"] = in.OutputPath
	}

	if truncated {
		buf := bytes.NewBufferString(markdown)
		artifact, err := skillout.PersistBuffer(ctx, rc, buf, "text/markdown", "ci_prcomments")
		if err == nil && artifact.Digest != "" {
			data["markdown_artifact"] = artifact.Digest
		}
	}

	if format == "json" {
		data["comments"] = allComments
	}

	return skillout.Emit(rc, "ci/prcomments", data)
}

// getPR fetches basic pull request information from GitHub API.
func getPR(client *http.Client, token, owner, repo string, prNum int) (*PRInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, prNum)
	var pr PRInfo
	if err := cihelpers.GitHubGET(client, token, url, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// getIssueComments fetches issue-level comments for a PR from GitHub API.
func getIssueComments(client *http.Client, token, owner, repo string, prNum int) ([]Comment, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d/comments", owner, repo, prNum)
	var comments []Comment
	if err := cihelpers.GitHubGET(client, token, url, &comments); err != nil {
		return nil, err
	}
	return comments, nil
}

// getReviewComments fetches review comments for a PR from GitHub API.
func getReviewComments(client *http.Client, token, owner, repo string, prNum int) ([]Comment, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/comments", owner, repo, prNum)
	var comments []Comment
	if err := cihelpers.GitHubGET(client, token, url, &comments); err != nil {
		return nil, err
	}
	return comments, nil
}

// getPRReviews fetches all reviews for a PR (includes the review body with summary comments).
func getPRReviews(client *http.Client, token, owner, repo string, prNum int) ([]PRReview, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/reviews", owner, repo, prNum)
	var reviews []PRReview
	if err := cihelpers.GitHubGET(client, token, url, &reviews); err != nil {
		return nil, err
	}
	return reviews, nil
}

// extractCodeRabbitAIAgentPrompts extracts the "🤖 Fix all issues with AI Agents" content from CodeRabbit reviews.
// Returns the consolidated fix instructions if found, or empty string if not present.
func extractCodeRabbitAIAgentPrompts(reviews []PRReview) string {
	for _, review := range reviews {
		if !strings.Contains(review.User.Login, "coderabbit") {
			continue
		}
		prompt := extractAIAgentPromptFromBody(review.Body)
		if prompt != "" {
			return prompt
		}
	}
	return ""
}

// extractAIAgentPromptFromBody extracts the content from the "🤖 Fix all issues with AI Agents" details section.
func extractAIAgentPromptFromBody(body string) string {
	// Look for the details block with the AI Agents summary
	// Format: <details><summary>🤖 Fix all issues with AI Agents</summary>```...```</details>
	markers := []string{
		"🤖 Fix all issues with AI Agents",
		"Fix all issues with AI Agents",
	}

	for _, marker := range markers {
		idx := strings.Index(body, marker)
		if idx == -1 {
			continue
		}

		// Find the content after the summary tag
		afterMarker := body[idx+len(marker):]

		// Look for the code block that contains the instructions
		codeStart := strings.Index(afterMarker, "```")
		if codeStart == -1 {
			continue
		}

		// Skip past the opening ``` and any language identifier
		codeContent := afterMarker[codeStart+3:]
		// Skip the optional language identifier on the first line
		if newlineIdx := strings.Index(codeContent, "\n"); newlineIdx != -1 {
			firstLine := codeContent[:newlineIdx]
			// If first line is short (like "text" or empty), it's a language identifier
			if len(strings.TrimSpace(firstLine)) < 20 && !strings.Contains(firstLine, "@") {
				codeContent = codeContent[newlineIdx+1:]
			}
		}

		// Find the closing ```
		codeEnd := strings.Index(codeContent, "```")
		if codeEnd == -1 {
			// Try to find </details> as fallback
			codeEnd = strings.Index(codeContent, "</details>")
			if codeEnd == -1 {
				continue
			}
		}

		content := strings.TrimSpace(codeContent[:codeEnd])
		if content != "" {
			return content
		}
	}
	return ""
}

// ReviewThreadStatus maps comment IDs to their resolved/outdated status from GraphQL API.
type ReviewThreadStatus struct {
	CommentID  int
	IsResolved bool
	IsOutdated bool
}

// getReviewThreadStatuses fetches resolved/outdated status for all review comments via GraphQL.
// This is needed because the REST API doesn't expose these fields.
func getReviewThreadStatuses(client *http.Client, token, owner, repo string, prNum int) (map[int]ReviewThreadStatus, error) {
	query := `
query($owner: String!, $repo: String!, $prNum: Int!) {
  repository(owner: $owner, name: $repo) {
    pullRequest(number: $prNum) {
      reviewThreads(first: 100) {
        nodes {
          isResolved
          isOutdated
          comments(first: 1) {
            nodes {
              databaseId
            }
          }
        }
      }
    }
  }
}`
	variables := map[string]any{
		"owner": owner,
		"repo":  repo,
		"prNum": prNum,
	}
	payload := map[string]any{
		"query":     query,
		"variables": variables,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, skillerr.WrapRuntime("marshal graphql query", err)
	}

	req, err := http.NewRequest("POST", "https://api.github.com/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, skillerr.WrapRuntime("create graphql request", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, skillerr.WrapRuntime("graphql request", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, skillerr.Runtimef("graphql request failed with status %d", resp.StatusCode)
	}

	var result struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved bool `json:"isResolved"`
							IsOutdated bool `json:"isOutdated"`
							Comments   struct {
								Nodes []struct {
									DatabaseID int `json:"databaseId"`
								} `json:"nodes"`
							} `json:"comments"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, skillerr.WrapParse("decode graphql response", err)
	}

	statuses := make(map[int]ReviewThreadStatus)
	for _, thread := range result.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if len(thread.Comments.Nodes) > 0 {
			commentID := thread.Comments.Nodes[0].DatabaseID
			statuses[commentID] = ReviewThreadStatus{
				CommentID:  commentID,
				IsResolved: thread.IsResolved,
				IsOutdated: thread.IsOutdated,
			}
		}
	}
	return statuses, nil
}

// mergeResolvedStatus updates comments with their resolved/outdated status from GraphQL data.
func mergeResolvedStatus(comments []Comment, statuses map[int]ReviewThreadStatus) {
	for i := range comments {
		if status, ok := statuses[comments[i].ID]; ok {
			comments[i].Resolved = status.IsResolved
			comments[i].Outdated = status.IsOutdated
		}
	}
}

// getCheckRuns fetches check runs for a PR and enriches failed checks with detailed output.
func getCheckRuns(client *http.Client, token, owner, repo string, prNum int) ([]CheckRun, error) {
	prURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, prNum)
	var pr struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := cihelpers.GitHubGET(client, token, prURL, &pr); err != nil {
		return nil, err
	}

	checksURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s/check-runs", owner, repo, pr.Head.SHA)
	var response struct {
		CheckRuns []CheckRun `json:"check_runs"`
	}
	if err := cihelpers.GitHubGET(client, token, checksURL, &response); err != nil {
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

// getCheckRunDetails fetches detailed information for a specific check run.
func getCheckRunDetails(client *http.Client, token, owner, repo string, checkID int) (*CheckRun, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/check-runs/%d", owner, repo, checkID)
	var check CheckRun
	if err := cihelpers.GitHubGET(client, token, url, &check); err != nil {
		return nil, err
	}
	return &check, nil
}

// getJobDetails fetches detailed job information from GitHub Actions API.
func getJobDetails(client *http.Client, token, owner, repo, jobURL string) (*JobDetails, error) {
	parts := strings.Split(jobURL, "/")
	if len(parts) < 8 {
		return nil, skillerr.Validation("invalid job URL format")
	}
	jobID := parts[len(parts)-1]

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/jobs/%s", owner, repo, jobID)
	var job JobDetails
	if err := cihelpers.GitHubGET(client, token, url, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

// getJobErrorOutput fetches and extracts error output from GitHub Actions job logs.
func getJobErrorOutput(client *http.Client, token, owner, repo string, jobID int, failedStepName string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", skillerr.WrapRuntime("create log request", err)
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
		return "", skillerr.WrapRuntime("send log request", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusMovedPermanently {
		location := resp.Header.Get("Location")
		if location != "" {
			redirectReq, err := http.NewRequest("GET", location, nil)
			if err != nil {
				return "", skillerr.WrapRuntime("create redirect request", err)
			}
			redirectResp, err := client.Do(redirectReq)
			if err != nil {
				return "", skillerr.WrapRuntime("send redirect request", err)
			}
			defer redirectResp.Body.Close()
			resp = redirectResp
		}
	}

	if resp.StatusCode != http.StatusOK {
		return "", skillerr.Runtimef("GitHub API returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", skillerr.WrapIO("read log response", err)
	}

	contentType := resp.Header.Get("Content-Type")
	contentDisposition := resp.Header.Get("Content-Disposition")

	if strings.Contains(contentType, "zip") || strings.Contains(contentDisposition, ".zip") || bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		zipReader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", skillerr.WrapIO("parse ZIP", err)
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
		return "", skillerr.Runtime("no relevant log content found in ZIP")
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

// sortCommentsByTime sorts comments by creation time in chronological order.
func sortCommentsByTime(comments []Comment) {
	sort.Slice(comments, func(i, j int) bool {
		return comments[i].CreatedAt.Before(comments[j].CreatedAt)
	})
}

// buildMarkdownReport builds a formatted markdown report of PR tasks and summary.
//
//nolint:revive // bytes.Buffer.Write/fmt.Fprintf never returns an error for in-memory writes.
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

// getChangedFilesForPR fetches the list of changed files for a PR.
func getChangedFilesForPR(client *http.Client, token, owner, repo string, prNum int) []string {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/files", owner, repo, prNum)
	var files []struct {
		Filename string `json:"filename"`
		Status   string `json:"status"`
	}
	if err := cihelpers.GitHubGET(client, token, url, &files); err != nil {
		logger.Warn().Err(err).Str("operation", "get-pr-files").Int("pr", prNum).Msg("failed to get PR files")
		return []string{}
	}
	var conflicting []string
	for _, f := range files {
		conflicting = append(conflicting, f.Filename)
	}
	return conflicting
}

// buildTasksList builds a structured list of task items from PR analysis.
func buildTasksList(conflictingFiles []string, comments []Comment, checkRuns []CheckRun, includeResolved bool) []TaskItem {
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
		// Skip resolved comments unless includeResolved is true
		if comment.Resolved && !includeResolved {
			continue
		}
		cleanBody := cleanCommentBody(comment.Body)
		if cleanBody == "" {
			continue
		}
		item := TaskItem{
			Kind:          "review_comment",
			Summary:       fmt.Sprintf("Address review comment from %s", comment.User.Login),
			CommentAuthor: comment.User.Login,
			CommentBody:   cleanBody,
			Resolved:      comment.Resolved,
			Outdated:      comment.Outdated,
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
		if strings.Contains(comment.User.Login, "greptile") {
			item.Source = "greptile"
			sev, summary := classifyGreptileTask(cleanBody)
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

// displayJobDetailsTaskFormat formats job details for task display in markdown.
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
		// Buffer writes; errors are not actionable for in-memory buffer.
		fmt.Fprintf(buf, "**Failed step:** %s\n", failedStep.Name)
		fmt.Fprintf(buf, "**URL:** %s\n\n", job.HTMLURL)
		output, err := getJobErrorOutput(client, token, owner, repo, job.ID, failedStep.Name)
		if err != nil {
			logger.Warn().Err(err).Str("operation", "get-job-error-output").Int("job_id", job.ID).Str("step", failedStep.Name).Msg("failed to get job error output")
		} else if output != "" {
			fmt.Fprintf(buf, "**Error output:**\n\n```\n%s\n```\n\n", output)
		}
	}
}

// cleanGitHubLogLine cleans GitHub Actions log formatting markers and timestamps.
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

// cleanCommentBody removes metadata and extracts actionable content from bot comments.
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
	if strings.Contains(body, "greptile") || strings.Contains(body, "Greptile") {
		return extractGreptileComment(body)
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

// extractCodexComment extracts actionable content from ChatGPT Codex connector comments.
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

// classifyCodeRabbitTask extracts severity and summary from CodeRabbit comment content.
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

// extractCodeRabbitComment extracts actionable content from CodeRabbit bot comments.
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

// extractGreptileComment extracts actionable content from Greptile bot comments.
// Greptile comments are generally cleaner than CodeRabbit but may contain metadata.
func extractGreptileComment(body string) string {
	// Remove HTML comments
	body = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(body, "")

	lines := strings.Split(body, "\n")
	var result []string
	inCode := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip common Greptile metadata patterns
		if strings.HasPrefix(trimmed, "🤖") && strings.Contains(trimmed, "Greptile") {
			continue
		}
		if strings.Contains(trimmed, "React with 👍 or 👎") {
			continue
		}
		// Track code blocks but include them (they contain actual suggestions)
		if strings.HasPrefix(trimmed, "```") {
			inCode = !inCode
		}
		result = append(result, strings.TrimRight(line, " \t"))
	}

	// Clean up leading/trailing empty lines
	for len(result) > 0 && strings.TrimSpace(result[0]) == "" {
		result = result[1:]
	}
	for len(result) > 0 && strings.TrimSpace(result[len(result)-1]) == "" {
		result = result[:len(result)-1]
	}

	if len(result) > 0 {
		return strings.Join(result, "\n")
	}
	return ""
}

// classifyGreptileTask extracts severity and summary from Greptile comment content.
// Greptile uses labels like "logic", "style", "security", "performance" as comment types.
func classifyGreptileTask(cleanBody string) (severity, summary string) {
	lines := strings.Split(cleanBody, "\n")

	// Look for severity indicators
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)

		// Security and performance issues are typically critical/major
		if strings.Contains(lower, "security") || strings.Contains(lower, "vulnerability") {
			severity = "critical"
			break
		}
		if strings.Contains(lower, "performance") || strings.Contains(lower, "memory leak") {
			severity = "major"
			break
		}
		if strings.Contains(lower, "bug") || strings.Contains(lower, "error") {
			severity = "major"
			break
		}
		if strings.Contains(lower, "style") || strings.Contains(lower, "naming") {
			severity = "minor"
			break
		}
	}

	// Extract first meaningful line as summary
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		// Skip headers and metadata
		if strings.HasPrefix(trimmed, "#") {
			trimmed = strings.TrimLeft(trimmed, "# ")
		}
		if strings.HasPrefix(trimmed, "```") {
			continue
		}
		if len(trimmed) > 10 {
			summary = trimmed
			if len(summary) > 100 {
				summary = summary[:100] + "..."
			}
			break
		}
	}

	return severity, summary
}
