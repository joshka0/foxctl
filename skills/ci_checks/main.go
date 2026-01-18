// Package main implements the ci/checks skill.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	cihelpers "github.com/jkatigb/agentctl/internal/adapters/skillslib/ci"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

// Input defines the skill input parameters.
type Input struct {
	PR         skillmain.FlexString `json:"pr" validate:"required"`
	Owner      string               `json:"owner,omitempty"`
	Repo       string               `json:"repo,omitempty"`
	Mode       string               `json:"mode,omitempty"`
	ErrorsOnly bool                 `json:"errors_only,omitempty"`
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
	ID              int      `json:"id"`
	Name            string   `json:"name"`
	Status          string   `json:"status"`
	Conclusion      string   `json:"conclusion"`
	HTMLURL         string   `json:"html_url"`
	StartedAt       string   `json:"started_at,omitempty"`
	CompletedAt     string   `json:"completed_at,omitempty"`
	DurationSeconds int64    `json:"duration_seconds,omitempty"`
	FailedStep      string   `json:"failed_step,omitempty"`
	ErrorExcerpt    string   `json:"error_excerpt,omitempty"`
	Locations       []string `json:"locations,omitempty"`
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
	skillmain.Main("ci/checks", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	prRef := strings.TrimSpace(in.PR.String())

	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode == "" {
		mode = "summary"
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

	prInfo, err := getPR(client, token, owner, repo, prNum)
	if err != nil {
		return err
	}

	checkRuns, err := getCheckRuns(client, token, owner, repo, prInfo.Head.SHA)
	if err != nil {
		return err
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

		// For blocking checks, fetch detailed error info
		if isBlockingConclusion(c.Conclusion) && strings.Contains(c.HTMLURL, "/actions/runs/") {
			job, err := getJobDetails(client, token, owner, repo, c.HTMLURL)
			if err == nil && job != nil {
				failedStep := findFailedStep(job)
				if failedStep != nil {
					cs.FailedStep = failedStep.Name
				}
				// Fetch logs and extract concise error
				if mode == "detailed" {
					rawLogs, logErr := getJobLogs(client, token, owner, repo, job.ID)
					if logErr == nil && rawLogs != "" {
						stepName := ""
						if failedStep != nil {
							stepName = failedStep.Name
						}
						excerpt, locations := extractConciseError(rawLogs, stepName, c.HTMLURL)
						cs.ErrorExcerpt = excerpt
						cs.Locations = locations
					}
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

	return skillout.Emit(rc, "ci/checks", data)
}

func getPR(client *http.Client, token, owner, repo string, prNum int) (*PRInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d", owner, repo, prNum)
	var pr PRInfo
	if err := cihelpers.GitHubGET(client, token, url, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func getCheckRuns(client *http.Client, token, owner, repo, sha string) ([]CheckRun, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits/%s/check-runs", owner, repo, sha)
	var response struct {
		CheckRuns []CheckRun `json:"check_runs"`
	}
	if err := cihelpers.GitHubGET(client, token, url, &response); err != nil {
		return nil, err
	}
	return response.CheckRuns, nil
}

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

// getJobLogs fetches raw logs for a GitHub Actions job.
func getJobLogs(client *http.Client, token, owner, repo string, jobID int) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/actions/jobs/%d/logs", owner, repo, jobID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", skillerr.WrapRuntime("create log request", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	// Handle redirect manually to get actual log content
	clientNoRedirect := &http.Client{
		Timeout: 30 * time.Second,
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
			data, err := io.ReadAll(redirectResp.Body)
			if err != nil {
				return "", skillerr.WrapIO("read redirect response", err)
			}
			return string(data), nil
		}
	}

	if resp.StatusCode != http.StatusOK {
		return "", skillerr.Runtimef("log fetch returned %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", skillerr.WrapIO("read log response", err)
	}
	return string(data), nil
}

// fileLinePattern matches common file:line patterns in error messages.
var fileLinePattern = regexp.MustCompile(`([a-zA-Z0-9_\-./]+\.(go|ts|tsx|js|jsx|py|rs|java|kt|swift|c|cpp|h|hpp)):(\d+)`)

// extractConciseError extracts a concise error excerpt (max 10 lines) with file:line locations.
func extractConciseError(rawLog, failedStepName, logsURL string) (excerpt string, locations []string) {
	if rawLog == "" {
		return "", nil
	}

	lines := strings.Split(rawLog, "\n")
	seen := make(map[string]bool)
	locationSet := make(map[string]bool)

	var errorLines []string

	// Patterns that indicate actual errors (not just progress)
	errorPatterns := []string{
		"error:", "Error:", "FAILED", "failed", "panic:",
		"exit code 1", "exit code 2", "Process completed with exit code",
		"undefined:", "cannot find", "not found", "fatal:",
		"--- FAIL:", "FAIL:", "compilation failed",
	}

	// Find the failed step section if possible
	inFailedStep := false
	stepStartIdx := -1
	stepEndIdx := len(lines)

	for i, line := range lines {
		if strings.Contains(line, "##[group]") && failedStepName != "" && strings.Contains(line, failedStepName) {
			inFailedStep = true
			stepStartIdx = i
		} else if inFailedStep && strings.Contains(line, "##[error]Process completed with exit code") {
			stepEndIdx = i + 1
			break
		}
	}

	// If we found the failed step, focus on that section
	if stepStartIdx >= 0 {
		lines = lines[stepStartIdx:stepEndIdx]
	}

	// Clean GitHub Actions log formatting
	cleanLine := func(line string) string {
		// Remove timestamps and group markers
		if idx := strings.Index(line, "Z "); idx > 0 && idx < 30 {
			line = line[idx+2:]
		}
		line = strings.TrimPrefix(line, "##[group]")
		line = strings.TrimPrefix(line, "##[endgroup]")
		line = strings.TrimPrefix(line, "##[error]")
		line = strings.TrimPrefix(line, "##[warning]")
		return strings.TrimSpace(line)
	}

	for _, line := range lines {
		cleaned := cleanLine(line)
		if cleaned == "" {
			continue
		}

		// Check if this line contains an error pattern
		isError := false
		for _, pattern := range errorPatterns {
			if strings.Contains(cleaned, pattern) {
				isError = true
				break
			}
		}

		// Extract file:line references
		matches := fileLinePattern.FindAllStringSubmatch(cleaned, -1)
		for _, match := range matches {
			if len(match) >= 4 {
				loc := fmt.Sprintf("%s:%s", match[1], match[3])
				if !locationSet[loc] {
					locationSet[loc] = true
					locations = append(locations, loc)
				}
			}
		}

		// Collect error lines, deduplicate
		if isError && !seen[cleaned] {
			seen[cleaned] = true
			errorLines = append(errorLines, cleaned)
		}
	}

	// Limit to 10 lines
	maxLines := 10
	if len(errorLines) > maxLines {
		errorLines = errorLines[:maxLines]
		errorLines = append(errorLines, fmt.Sprintf("... (truncated, see full logs: %s)", logsURL))
	}

	if len(errorLines) == 0 {
		// No error patterns found, return last few lines of log
		if len(lines) > 5 {
			lines = lines[len(lines)-5:]
		}
		for _, line := range lines {
			cleaned := cleanLine(line)
			if cleaned != "" && !seen[cleaned] {
				seen[cleaned] = true
				errorLines = append(errorLines, cleaned)
			}
		}
	}

	return strings.Join(errorLines, "\n"), locations
}
