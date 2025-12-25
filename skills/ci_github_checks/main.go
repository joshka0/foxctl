// Package main implements the ci/github_checks skill.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

type input struct {
	PR         string `json:"pr"`
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	Mode       string `json:"mode"`
	ErrorsOnly bool   `json:"errors_only"`
}

type PRInfo struct {
	Number int `json:"number"`
	Head   struct {
		SHA string `json:"sha"`
	} `json:"head"`
}

type CheckRun struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	Conclusion  string `json:"conclusion"`
	HTMLURL     string `json:"html_url"`
	StartedAt   string `json:"started_at"`
	CompletedAt string `json:"completed_at"`
}

type CheckSummary struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	Status          string `json:"status"`
	Conclusion      string `json:"conclusion"`
	HTMLURL         string `json:"html_url"`
	StartedAt       string `json:"started_at,omitempty"`
	CompletedAt     string `json:"completed_at,omitempty"`
	DurationSeconds int64  `json:"duration_seconds,omitempty"`
	FailedStep      string `json:"failed_step,omitempty"`
	ErrorExcerpt    string `json:"error_excerpt,omitempty"`
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

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("ci/github_checks", "ERUNTIME", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("ci/github_checks", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("ci/github_checks", "EARG", fmt.Errorf("decode input: %w", err))
	}
	if err := run(ctx, rc, in); err != nil {
		fail("ci/github_checks", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, in input) error {
	prRef := strings.TrimSpace(in.PR)
	if prRef == "" {
		return errors.New("pr is required")
	}

	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "summary"
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
			_ = err
			// best-effort; keep going with env if present
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

	checkRuns, err := getCheckRuns(client, token, owner, repo, prInfo.Head.SHA)
	if err != nil {
		return fmt.Errorf("get check runs: %w", err)
	}

	total, failed, cancelled, neutral, success := countConclusions(checkRuns)

	filtered := checkRuns
	if in.ErrorsOnly {
		filtered = filterBlockingChecks(checkRuns)
	}

	// sort by conclusion then name for readability
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Conclusion == filtered[j].Conclusion {
			return filtered[i].Name < filtered[j].Name
		}
		return filtered[i].Conclusion < filtered[j].Conclusion
	})

	summaries := make([]CheckSummary, 0, len(filtered))
	for _, c := range filtered {
		cs := CheckSummary{
			ID:          c.ID,
			Name:        c.Name,
			Status:      c.Status,
			Conclusion:  c.Conclusion,
			HTMLURL:     c.HTMLURL,
			StartedAt:   c.StartedAt,
			CompletedAt: c.CompletedAt,
		}

		// compute duration if timestamps are present
		if c.StartedAt != "" && c.CompletedAt != "" {
			if start, err1 := time.Parse(time.RFC3339, c.StartedAt); err1 == nil {
				if end, err2 := time.Parse(time.RFC3339, c.CompletedAt); err2 == nil {
					cs.DurationSeconds = int64(end.Sub(start).Seconds())
				}
			}
		}

		if mode == "detailed" && strings.Contains(c.HTMLURL, "/actions/runs/") {
			job, err := getJobDetails(client, token, owner, repo, c.HTMLURL)
			if err == nil && job != nil {
				failedStep := findFailedStep(job)
				if failedStep != nil {
					cs.FailedStep = failedStep.Name
				}
			}
		}

		summaries = append(summaries, cs)
	}

	overall, hasBlockingCI, allChecksSuccessful, hasNeutralOrSkipped := classifyCIStatus(total, failed, cancelled, neutral, success)

	data := map[string]any{
		"repository":             fmt.Sprintf("%s/%s", owner, repo),
		"pr_number":              prNum,
		"head_sha":               prInfo.Head.SHA,
		"overall_status":         overall,
		"has_blocking_ci":        hasBlockingCI,
		"all_checks_successful":  allChecksSuccessful,
		"has_neutral_or_skipped": hasNeutralOrSkipped,
		"totals": map[string]int{
			"checks":    total,
			"failed":    failed,
			"cancelled": cancelled,
			"neutral":   neutral,
			"success":   success,
		},
		"mode":        mode,
		"errors_only": in.ErrorsOnly,
		"checks":      summaries,
	}

	return rc.Emit("ci/github_checks", data, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func detectRepo(ctx context.Context) (string, string, error) {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("detect repo: %w", err)
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", "", errors.New("empty remote url")
	}

	if strings.HasPrefix(url, "git@") {
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

func getCheckRuns(client *http.Client, token, owner, repo, sha string) ([]CheckRun, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s/check-runs", owner, repo, sha)
	var response struct {
		CheckRuns []CheckRun `json:"check_runs"`
	}
	if err := githubGET(client, token, url, &response); err != nil {
		return nil, err
	}
	return response.CheckRuns, nil
}

func getJobDetails(client *http.Client, token, owner, repo, jobURL string) (*JobDetails, error) {
	parts := strings.Split(jobURL, "/")
	if len(parts) < 8 {
		return nil, errors.New("invalid job URL format")
	}
	jobID := parts[len(parts)-1]

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/jobs/%s", owner, repo, jobID)
	var job JobDetails
	if err := githubGET(client, token, url, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func findFailedStep(job *JobDetails) *JobStep {
	for i := len(job.Steps) - 1; i >= 0; i-- {
		step := &job.Steps[i]
		if step.Conclusion == "failure" {
			return step
		}
	}
	return nil
}

func countConclusions(checkRuns []CheckRun) (total, failed, cancelled, neutral, success int) {
	for _, c := range checkRuns {
		total++
		switch c.Conclusion {
		case "failure", "timed_out", "action_required", "stale":
			failed++
		case "cancelled":
			cancelled++
		case "neutral", "skipped":
			neutral++
		case "success":
			success++
		}
	}
	return total, failed, cancelled, neutral, success
}

func isBlockingConclusion(conclusion string) bool {
	switch conclusion {
	case "failure", "timed_out", "action_required", "stale", "cancelled":
		return true
	default:
		return false
	}
}

func filterBlockingChecks(checkRuns []CheckRun) []CheckRun {
	var out []CheckRun
	for _, c := range checkRuns {
		if isBlockingConclusion(c.Conclusion) {
			out = append(out, c)
		}
	}
	return out
}

func classifyCIStatus(total, failed, cancelled, neutral, success int) (string, bool, bool, bool) {
	overall := "unknown"
	if failed > 0 {
		overall = "failed"
	} else if cancelled > 0 {
		overall = "cancelled"
	} else if success > 0 && success == total {
		overall = "success"
	} else if total > 0 {
		overall = "mixed"
	}

	hasBlockingCI := failed > 0 || cancelled > 0
	allChecksSuccessful := total > 0 && success == total
	hasNeutralOrSkipped := neutral > 0

	return overall, hasBlockingCI, allChecksSuccessful, hasNeutralOrSkipped
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Error body read; error is not actionable in error path.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024)) //nolint:errcheck
		return fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return err
	}
	return nil
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit ci/github_checks failure")
	os.Exit(1)
}
