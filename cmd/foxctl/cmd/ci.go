package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// detectGitRepo attempts to detect owner and repo from git remote origin.
// Returns empty strings if detection fails (non-blocking).
func detectGitRepo(ctx context.Context) (owner, repo string) {
	// Add timeout to prevent hanging on slow git operations
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}
	url := strings.TrimSpace(string(out))
	if url == "" {
		return "", ""
	}

	// Support ssh format: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@") {
		parts := strings.SplitN(url, ":", 2)
		if len(parts) != 2 {
			return "", ""
		}
		path := strings.TrimSuffix(parts[1], ".git")
		sub := strings.SplitN(path, "/", 2)
		if len(sub) != 2 {
			return "", ""
		}
		return sub[0], sub[1]
	}

	// Support https format: https://github.com/owner/repo.git
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		parts := strings.Split(url, "/")
		if len(parts) < 2 {
			return "", ""
		}
		repoName := strings.TrimSuffix(parts[len(parts)-1], ".git")
		ownerName := parts[len(parts)-2]
		return ownerName, repoName
	}

	return "", ""
}

// resolveOwnerRepo returns owner and repo, using git remote detection as fallback.
// If repo is provided as "owner/repo" shorthand, it splits and returns both.
func resolveOwnerRepo(ctx context.Context, owner, repo string) (string, string) {
	// Handle owner/repo shorthand in repo flag
	if repo != "" && strings.Contains(repo, "/") {
		parts := strings.SplitN(repo, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}

	// If both provided, use them
	if owner != "" && repo != "" {
		return owner, repo
	}

	// Auto-detect from git remote
	detectedOwner, detectedRepo := detectGitRepo(ctx)

	if owner == "" {
		owner = detectedOwner
	}
	if repo == "" {
		repo = detectedRepo
	}

	return owner, repo
}

// resolvePRFlag returns the effective PR identifier, preferring prBranch if provided.
func resolvePRFlag(pr, prBranch string) string {
	if prBranch != "" {
		return prBranch
	}
	return pr
}

func newCICommand() *cobra.Command {
	var examples bool
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Inspect CI status and PR review tasks via skills",
		Long: "CI helpers built on ci/* skills. Use these commands to summarize PR review tasks, " +
			"GitHub checks, and import tasks into todo/manage.\n\n" +
			"Common workflows:\n" +
			"  foxctl ci prcomments --pr <number-or-branch> [flags]\n" +
			"  foxctl ci checks --pr <number-or-branch> [flags]\n" +
			"  foxctl ci todos --pr <number-or-branch> [flags]\n\n" +
			"See docs/ci/ for detailed examples and skill contracts.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if examples {
				return writeCIExamples(cmd)
			}
			return cmd.Help()
		},
	}
	cmd.Flags().BoolVar(&examples, "examples", false, "Show example usage for CI commands")
	cmd.AddCommand(
		newCIStatusCommand(),
		newCIPRCommentsCommand(),
		newCIChecksCommand(),
		newCITodosCommand(),
		newCICommentsCommand(),
		newCIResultsCommand(),
	)
	return cmd
}

// CIStatus represents the unified CI status for a PR.
type CIStatus struct {
	PRNumber      int            `json:"pr_number"`
	Title         string         `json:"title"`
	URL           string         `json:"url"`
	OverallStatus string         `json:"overall_status"` // "passing", "failing", "pending"
	CI            CISection      `json:"ci"`
	Comments      CommentSection `json:"comments"`
	MergeStatus   MergeSection   `json:"merge_status"`
}

// CISection contains CI check information.
type CISection struct {
	Status       string      `json:"status"` // "passed", "failed", "pending"
	FailureCount int         `json:"failure_count"`
	Failures     []CIFailure `json:"failures,omitempty"`
}

// CIFailure represents a single CI failure.
type CIFailure struct {
	Name         string   `json:"name"`
	ErrorExcerpt string   `json:"error_excerpt,omitempty"`
	Locations    []string `json:"locations,omitempty"`
	URL          string   `json:"url"`
}

// CommentSection contains review comment information.
type CommentSection struct {
	Count int           `json:"count"`
	Items []CommentItem `json:"items,omitempty"`
}

// CommentItem represents a single review comment.
type CommentItem struct {
	Author   string `json:"author"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Body     string `json:"body"`
	Severity string `json:"severity,omitempty"`
}

// MergeSection contains merge status information.
type MergeSection struct {
	Mergeable        bool     `json:"mergeable"`
	HasConflicts     bool     `json:"has_conflicts"`
	ConflictingFiles []string `json:"conflicting_files,omitempty"`
}

func newCIStatusCommand() *cobra.Command {
	var pr string
	var prBranch string
	var owner string
	var repo string
	var format string
	var skipCache bool
	var dataOnly bool
	var noCAS bool
	var helpJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Unified view of CI status, comments, and merge status",
		Long: "Show a unified, concise view of CI failures, review comments, and merge status for a PR. " +
			"Aggregates data from ci/checks and ci/prcomments skills into a single actionable report.",
		Example: "  # View unified status for current branch\n" +
			"  foxctl ci status --pr feat/my-branch\n\n" +
			"  # View unified status for PR number\n" +
			"  foxctl ci status --pr 123\n\n" +
			"  # View unified status by branch name\n" +
			"  foxctl ci status --pr-branch feat/my-feature\n\n" +
			"  # JSON output for AI consumption\n" +
			"  foxctl ci status --pr 123 --data-only\n",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if helpJSON {
				return writeCIHelpJSON(cmd)
			}
			// --pr-branch takes precedence over --pr
			effectivePR := pr
			if prBranch != "" {
				effectivePR = prBranch
			}
			if strings.TrimSpace(effectivePR) == "" {
				return writeCIValidationError(cmd, "--pr or --pr-branch is required", "pr", "Provide --pr with a pull request number or --pr-branch with a branch name, for example: --pr 66 or --pr-branch feat/my-feature.")
			}
			return runCIStatus(cmd, effectivePR, owner, repo, format, skipCache, dataOnly, noCAS)
		},
	}

	cmd.Flags().StringVar(&pr, "pr", "", "Pull request number or branch name")
	cmd.Flags().StringVar(&prBranch, "pr-branch", "", "Branch name to find PR (alternative to --pr)")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub repository owner (optional)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository name or owner/repo shorthand (optional)")
	cmd.Flags().StringVar(&format, "format", "markdown", "Output format: markdown or json")
	cmd.Flags().BoolVar(&skipCache, "skip-cache", false, "Bypass result cache and always execute skills")
	cmd.Flags().BoolVar(&skipCache, "no-cache", false, "Bypass result cache (alias for --skip-cache)")
	cmd.Flags().BoolVar(&dataOnly, "data-only", false, "Print only {status,data} from the envelope for AI consumption")
	cmd.Flags().BoolVar(&noCAS, "no-cas", true, "Disable CAS truncation - return full output inline")
	cmd.Flags().BoolVar(&helpJSON, "help-json", false, "Emit JSON help metadata instead of running the command")
	return cmd
}

func runCIStatus(cmd *cobra.Command, pr, owner, repo, format string, skipCache, dataOnly, noCAS bool) error {
	// Auto-detect owner/repo from git if not provided
	owner, repo = resolveOwnerRepo(cmd.Context(), owner, repo)

	// Build payload for both skills
	checksPayload := map[string]any{
		"pr":          pr,
		"mode":        "detailed",
		"errors_only": true,
	}
	commentsPayload := map[string]any{
		"pr":          pr,
		"errors_only": true,
		"format":      "json",
	}
	if owner != "" {
		checksPayload["owner"] = owner
		commentsPayload["owner"] = owner
	}
	if repo != "" {
		checksPayload["repo"] = repo
		commentsPayload["repo"] = repo
	}

	// Call ci/checks
	checksEnv, checksErr := runCISkillForEnvelopeWithOpts(cmd, "ci/checks", checksPayload, skipCache, noCAS)

	// Call ci/prcomments
	commentsEnv, commentsErr := runCISkillForEnvelopeWithOpts(cmd, "ci/prcomments", commentsPayload, skipCache, noCAS)

	// Build unified status from results
	status := buildCIStatus(checksEnv, checksErr, commentsEnv, commentsErr)

	// Output based on format
	if dataOnly {
		out := map[string]any{
			"status": "ok",
			"data":   status,
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetEscapeHTML(false)
		return enc.Encode(out)
	}

	if format == "json" {
		env := protocol.OK("ci/status", status, protocol.WithSource("cli"))
		return protocol.Write(cmd.OutOrStdout(), env)
	}

	// Markdown format
	markdown := formatCIStatusMarkdown(status)
	env := protocol.OK("ci/status", map[string]any{
		"markdown": markdown,
		"data":     status,
	}, protocol.WithSource("cli"))
	return protocol.Write(cmd.OutOrStdout(), env)
}

func buildCIStatus(checksEnv envelope.Envelope, checksErr error, commentsEnv envelope.Envelope, commentsErr error) CIStatus {
	status := CIStatus{
		OverallStatus: "passing",
		CI: CISection{
			Status: "passed",
		},
	}

	// Extract from ci/checks
	if checksErr == nil && checksEnv.Status == "ok" {
		if data, ok := checksEnv.Data.(map[string]any); ok {
			if prNum, ok := data["pr_number"].(float64); ok {
				status.PRNumber = int(prNum)
			}
			if repo, ok := data["repository"].(string); ok {
				status.URL = fmt.Sprintf("https://github.com/%s/pull/%d", repo, status.PRNumber)
			}
			if hasBlocking, ok := data["has_blocking_ci"].(bool); ok && hasBlocking {
				status.OverallStatus = "failing"
				status.CI.Status = "failed"
			}
			if totals, ok := data["totals"].(map[string]any); ok {
				if failed, ok := totals["failed"].(float64); ok {
					status.CI.FailureCount = int(failed)
				}
			}
			if checks, ok := data["checks"].([]any); ok {
				for _, c := range checks {
					if cm, ok := c.(map[string]any); ok {
						conclusion, _ := cm["conclusion"].(string)
						if conclusion == "failure" || conclusion == "error" || conclusion == "cancelled" {
							failure := CIFailure{
								Name: getString(cm, "name"),
								URL:  getString(cm, "html_url"),
							}
							if excerpt, ok := cm["error_excerpt"].(string); ok {
								failure.ErrorExcerpt = excerpt
							}
							if locs, ok := cm["locations"].([]any); ok {
								for _, loc := range locs {
									if locStr, ok := loc.(string); ok {
										failure.Locations = append(failure.Locations, locStr)
									}
								}
							}
							if failedStep, ok := cm["failed_step"].(string); ok && failure.ErrorExcerpt == "" {
								failure.ErrorExcerpt = fmt.Sprintf("Failed step: %s", failedStep)
							}
							status.CI.Failures = append(status.CI.Failures, failure)
						}
					}
				}
			}
		}
	}

	// Extract from ci/prcomments
	if commentsErr == nil && commentsEnv.Status == "ok" {
		if data, ok := commentsEnv.Data.(map[string]any); ok {
			if title, ok := data["title"].(string); ok {
				status.Title = title
			}
			if prNum, ok := data["pr_number"].(float64); ok && status.PRNumber == 0 {
				status.PRNumber = int(prNum)
			}
			if url, ok := data["url"].(string); ok && status.URL == "" {
				status.URL = url
			}
			// Extract merge status
			if statusMap, ok := data["status"].(map[string]any); ok {
				if hasConflicts, ok := statusMap["has_merge_conflicts"].(bool); ok {
					status.MergeStatus.HasConflicts = hasConflicts
					status.MergeStatus.Mergeable = !hasConflicts
					if hasConflicts {
						status.OverallStatus = "failing"
					}
				}
			}
			// Extract comments
			if tasksList, ok := data["tasks_list"].([]any); ok {
				for _, t := range tasksList {
					if tm, ok := t.(map[string]any); ok {
						kind, _ := tm["kind"].(string)
						if kind == "review_comment" {
							item := CommentItem{
								Author: getString(tm, "comment_author"),
								Body:   getString(tm, "summary"),
								File:   getString(tm, "file"),
							}
							if line, ok := tm["line"].(float64); ok {
								item.Line = int(line)
							}
							if severity, ok := tm["severity"].(string); ok {
								item.Severity = severity
							}
							status.Comments.Items = append(status.Comments.Items, item)
						}
					}
				}
				status.Comments.Count = len(status.Comments.Items)
			}
		}
	}

	return status
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func formatCIStatusMarkdown(status CIStatus) string {
	var buf bytes.Buffer

	// Header
	title := status.Title
	if title == "" {
		title = "PR"
	}
	fmt.Fprintf(&buf, "## PR #%d: %s\n\n", status.PRNumber, title)

	// Status line
	var statusIcon string
	switch status.OverallStatus {
	case "failing":
		statusIcon = "❌"
	case "pending":
		statusIcon = "⏳"
	default:
		statusIcon = "✅"
	}

	mergeIcon := "✅"
	if status.MergeStatus.HasConflicts {
		mergeIcon = "⚠️ Conflicts"
	}

	titleCaser := cases.Title(language.English)
	fmt.Fprintf(&buf, "**Status:** %s %s | **Merge:** %s\n\n", statusIcon, titleCaser.String(status.OverallStatus), mergeIcon)

	// CI Failures
	if status.CI.FailureCount > 0 {
		fmt.Fprintf(&buf, "### CI Failures (%d)\n\n", status.CI.FailureCount)
		for i, f := range status.CI.Failures {
			fmt.Fprintf(&buf, "%d. **%s** [→ logs](%s)\n", i+1, f.Name, f.URL)
			if f.ErrorExcerpt != "" {
				// Indent excerpt
				lines := strings.Split(f.ErrorExcerpt, "\n")
				for _, line := range lines {
					if strings.TrimSpace(line) != "" {
						fmt.Fprintf(&buf, "   %s\n", line)
					}
				}
			}
			for _, loc := range f.Locations {
				fmt.Fprintf(&buf, "   → %s\n", loc)
			}
			fmt.Fprintf(&buf, "\n")
		}
	}

	// Review Comments
	if status.Comments.Count > 0 {
		fmt.Fprintf(&buf, "### Review Comments (%d)\n\n", status.Comments.Count)
		for i, c := range status.Comments.Items {
			loc := ""
			if c.File != "" {
				if c.Line > 0 {
					loc = fmt.Sprintf(" on %s:%d", c.File, c.Line)
				} else {
					loc = fmt.Sprintf(" on %s", c.File)
				}
			}
			severity := ""
			if c.Severity != "" {
				severity = fmt.Sprintf(" [%s]", c.Severity)
			}
			fmt.Fprintf(&buf, "%d. **@%s**%s%s\n", i+1, c.Author, severity, loc)
			fmt.Fprintf(&buf, "   %s\n\n", c.Body)
		}
	}

	// No issues
	if status.CI.FailureCount == 0 && status.Comments.Count == 0 && !status.MergeStatus.HasConflicts {
		fmt.Fprintf(&buf, "✅ **No outstanding issues** - CI passing, no review comments\n")
	}

	return buf.String()
}

func newCIPRCommentsCommand() *cobra.Command {
	var pr string
	var prBranch string
	var owner string
	var repo string
	var format string
	var outputPath string
	var withContext bool
	var errorsOnly bool
	var skipCache bool
	var dataOnly bool
	var noComments bool
	var noCAS bool
	var helpJSON bool

	cmd := &cobra.Command{
		Use:   "prcomments",
		Short: "Summarize PR merge conflicts, CI failures, and review comments",
		Long: "Summarize merge conflicts, CI failures, and PR review comments for a GitHub pull request. " +
			"The underlying ci/prcomments skill returns a task-focused markdown report and a machine-readable JSON summary.",
		Example: "  # Task-focused report with markdown output and errors-only focus\n" +
			"  foxctl ci prcomments \\\n" +
			"    --pr 66 \\\n" +
			"    --owner joshka0 \\\n" +
			"    --repo foxctl \\\n" +
			"    --with-context \\\n" +
			"    --errors-only \\\n" +
			"    --output-path docs/prcomments/pr66.md\n\n" +
			"  # JSON-centric output for AI/tooling\n" +
			"  foxctl ci prcomments \\\n" +
			"    --pr 66 \\\n" +
			"    --owner joshka0 \\\n" +
			"    --repo foxctl \\\n" +
			"    --format json \\\n" +
			"    --errors-only\n",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if helpJSON {
				return writeCIHelpJSON(cmd)
			}
			effectivePR := resolvePRFlag(pr, prBranch)
			if strings.TrimSpace(effectivePR) == "" {
				return writeCIValidationError(cmd, "--pr or --pr-branch is required", "pr", "Provide --pr with a pull request number or --pr-branch with a branch name.")
			}
			// Auto-detect owner/repo from git if not provided
			owner, repo = resolveOwnerRepo(cmd.Context(), owner, repo)

			payload := map[string]any{
				"pr":           effectivePR,
				"with_context": withContext,
				"errors_only":  errorsOnly,
			}
			if owner != "" {
				payload["owner"] = owner
			}
			if repo != "" {
				payload["repo"] = repo
			}
			if format != "" {
				payload["format"] = format
			}
			if outputPath != "" {
				payload["output_path"] = outputPath
			}
			return runCISkill(cmd, "ci/prcomments", payload, skipCache, dataOnly, noComments, noCAS)
		},
	}

	cmd.Flags().StringVar(&pr, "pr", "", "Pull request number or branch name (required)")
	cmd.Flags().StringVar(&prBranch, "pr-branch", "", "Branch name to find PR (alternative to --pr)")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub repository owner (optional)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository name or owner/repo shorthand (optional)")
	cmd.Flags().StringVar(&format, "format", "", "Output format emphasis: markdown or json (default markdown)")
	cmd.Flags().StringVar(&outputPath, "output-path", "", "Workspace-relative path to write markdown report (e.g. docs/prcomments/pr66.md)")
	cmd.Flags().BoolVar(&withContext, "with-context", false, "Include PR description and timestamps in the markdown report")
	cmd.Flags().BoolVar(&errorsOnly, "errors-only", false, "Only include failing CI and actionable review comments")
	cmd.Flags().BoolVar(&skipCache, "skip-cache", false, "Bypass result cache and always execute the skill")
	cmd.Flags().BoolVar(&skipCache, "no-cache", false, "Bypass result cache (alias for --skip-cache)")
	cmd.Flags().BoolVar(&dataOnly, "data-only", false, "Print only {status,data} from the envelope for AI consumption")
	cmd.Flags().BoolVar(&noComments, "no-comments", false, "Omit raw comments array from data when used with --data-only")
	cmd.Flags().BoolVar(&noCAS, "no-cas", true, "Disable CAS truncation - return full output inline")
	cmd.Flags().BoolVar(&helpJSON, "help-json", false, "Emit JSON help metadata instead of running the skill")
	return cmd
}

func newCIChecksCommand() *cobra.Command {
	var pr string
	var prBranch string
	var owner string
	var repo string
	var mode string
	var errorsOnly bool
	var skipCache bool
	var dataOnly bool
	var noCAS bool
	var helpJSON bool

	cmd := &cobra.Command{
		Use:   "checks",
		Short: "Summarize GitHub check runs for a PR",
		Long:  "Summarize GitHub check runs for a pull request, with optional detailed mode for failed steps.",
		Example: "  # View failing checks only (detailed)\n" +
			"  foxctl ci checks \\\n" +
			"    --pr 66 \\\n" +
			"    --owner joshka0 \\\n" +
			"    --repo foxctl \\\n" +
			"    --mode detailed \\\n" +
			"    --errors-only\n",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if helpJSON {
				return writeCIHelpJSON(cmd)
			}
			effectivePR := resolvePRFlag(pr, prBranch)
			if strings.TrimSpace(effectivePR) == "" {
				return writeCIValidationError(cmd, "--pr or --pr-branch is required", "pr", "Provide --pr with a pull request number or --pr-branch with a branch name.")
			}
			// Auto-detect owner/repo from git if not provided
			owner, repo = resolveOwnerRepo(cmd.Context(), owner, repo)

			payload := map[string]any{
				"pr":          effectivePR,
				"errors_only": errorsOnly,
			}
			if owner != "" {
				payload["owner"] = owner
			}
			if repo != "" {
				payload["repo"] = repo
			}
			if mode != "" {
				payload["mode"] = mode
			}
			return runCISkill(cmd, "ci/checks", payload, skipCache, dataOnly, false, noCAS)
		},
	}

	cmd.Flags().StringVar(&pr, "pr", "", "Pull request number or branch name (required)")
	cmd.Flags().StringVar(&prBranch, "pr-branch", "", "Branch name to find PR (alternative to --pr)")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub repository owner (optional)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository name or owner/repo shorthand (optional)")
	cmd.Flags().StringVar(&mode, "mode", "", "Detail level: summary or detailed (default summary)")
	cmd.Flags().BoolVar(&errorsOnly, "errors-only", false, "Only include failing/errored/cancelled checks")
	cmd.Flags().BoolVar(&skipCache, "skip-cache", false, "Bypass result cache and always execute the skill")
	cmd.Flags().BoolVar(&skipCache, "no-cache", false, "Bypass result cache (alias for --skip-cache)")
	cmd.Flags().BoolVar(&dataOnly, "data-only", false, "Print only {status,data} from the envelope for AI consumption")
	cmd.Flags().BoolVar(&noCAS, "no-cas", true, "Disable CAS truncation - return full output inline")
	cmd.Flags().BoolVar(&helpJSON, "help-json", false, "Emit JSON help metadata instead of running the skill")
	return cmd
}

func newCITodosCommand() *cobra.Command {
	var pr string
	var prBranch string
	var owner string
	var repo string
	var storePath string
	var skipCache bool
	var noCAS bool
	var helpJSON bool

	cmd := &cobra.Command{
		Use:   "todos",
		Short: "Import CI/PR review tasks into todo/manage",
		Long:  "Import CI and PR review tasks for a GitHub pull request into the todo/manage skill.",
		Example: "  foxctl ci todos \\\n" +
			"    --pr 78 \\\n" +
			"    --owner joshka0 \\\n" +
			"    --repo foxctl \\\n" +
			"    --store ~/.foxctl/todo/tasks.json\n",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if helpJSON {
				return writeCIHelpJSON(cmd)
			}
			effectivePR := resolvePRFlag(pr, prBranch)
			if strings.TrimSpace(effectivePR) == "" {
				return writeCIValidationError(cmd, "--pr or --pr-branch is required", "pr", "Provide --pr with a pull request number or --pr-branch with a branch name.")
			}
			// Auto-detect owner/repo from git if not provided
			owner, repo = resolveOwnerRepo(cmd.Context(), owner, repo)

			payload := map[string]any{
				"pr":           effectivePR,
				"with_context": false,
				"errors_only":  true,
				"format":       "json",
			}
			if owner != "" {
				payload["owner"] = owner
			}
			if repo != "" {
				payload["repo"] = repo
			}
			env, err := runCISkillForEnvelopeWithOpts(cmd, "ci/prcomments", payload, skipCache, noCAS)
			if err != nil {
				return err
			}
			m, ok := env.Data.(map[string]any)
			if !ok {
				return fmt.Errorf("unexpected data type in envelope: %T", env.Data)
			}
			tasksRaw, ok := m["tasks_list"]
			if !ok {
				// No structured tasks to import; nothing to do.
				return nil
			}
			tasksSlice, ok := tasksRaw.([]any)
			if !ok {
				return fmt.Errorf("unexpected tasks_list type: %T", tasksRaw)
			}
			for _, t := range tasksSlice {
				tm, ok := t.(map[string]any)
				if !ok {
					continue
				}
				if err := createTodoFromCITask(cmd, tm, storePath); err != nil {
					return err
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&pr, "pr", "", "Pull request number or branch name (required)")
	cmd.Flags().StringVar(&prBranch, "pr-branch", "", "Branch name to find PR (alternative to --pr)")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub repository owner (optional)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository name or owner/repo shorthand (optional)")
	cmd.Flags().StringVar(&storePath, "store", "", "Path to todo store (default: ~/.foxctl/todo/tasks.json)")
	cmd.Flags().BoolVar(&skipCache, "skip-cache", false, "Bypass result cache and always execute the ci/prcomments skill")
	cmd.Flags().BoolVar(&skipCache, "no-cache", false, "Bypass result cache (alias for --skip-cache)")
	cmd.Flags().BoolVar(&noCAS, "no-cas", true, "Disable CAS truncation - return full output inline")
	cmd.Flags().BoolVar(&helpJSON, "help-json", false, "Emit JSON help metadata instead of running the skill")
	return cmd
}

func newCICommentsCommand() *cobra.Command {
	var pr string
	var prBranch string
	var owner string
	var repo string
	var source string
	var skipCache bool
	var dataOnly bool
	var showAll bool
	var noCAS bool

	cmd := &cobra.Command{
		Use:   "comments",
		Short: "Get review comments for a PR (supports CodeRabbit, Greptile, and other bots)",
		Long: "Retrieve and filter review comments from a PR. By default, resolved/addressed " +
			"comments are hidden. Use --all to show all comments including resolved ones. " +
			"Supports filtering by source (coderabbit, greptile, human).",
		Example: "  # Get unresolved review comments (default)\n" +
			"  foxctl ci comments --pr 128\n\n" +
			"  # Get all comments including resolved\n" +
			"  foxctl ci comments --pr 128 --all\n\n" +
			"  # Get only Greptile comments\n" +
			"  foxctl ci comments --pr 128 --source greptile\n\n" +
			"  # Get only CodeRabbit comments\n" +
			"  foxctl ci comments --pr 128 --source coderabbit\n",
		RunE: func(cmd *cobra.Command, _ []string) error {
			effectivePR := resolvePRFlag(pr, prBranch)
			if strings.TrimSpace(effectivePR) == "" {
				return writeCIValidationError(cmd, "--pr or --pr-branch is required", "pr", "Provide --pr with a pull request number or --pr-branch with a branch name.")
			}
			owner, repo = resolveOwnerRepo(cmd.Context(), owner, repo)

			payload := map[string]any{
				"pr":               effectivePR,
				"errors_only":      false,
				"format":           "json",
				"include_resolved": showAll, // Include resolved comments when --all is specified
			}
			if owner != "" {
				payload["owner"] = owner
			}
			if repo != "" {
				payload["repo"] = repo
			}

			env, err := runCISkillForEnvelopeWithOpts(cmd, "ci/prcomments", payload, skipCache, noCAS)
			if err != nil {
				return err
			}

			// Extract and filter comments
			data, ok := env.Data.(map[string]any)
			if !ok {
				return fmt.Errorf("unexpected data type: %T", env.Data)
			}

			tasksList, _ := data["tasks_list"].([]any)
			comments := make([]map[string]any, 0) // Initialize as empty slice for stable JSON
			for _, t := range tasksList {
				tm, ok := t.(map[string]any)
				if !ok {
					continue
				}
				kind, _ := tm["kind"].(string)
				if kind != "review_comment" {
					continue
				}
				// Filter by source if specified
				if source != "" {
					itemSource, _ := tm["source"].(string)
					if source == "human" && itemSource != "" {
						continue // Skip bot comments when filtering for human
					}
					if source != "human" && itemSource != source {
						continue
					}
				}
				comments = append(comments, tm)
			}

			result := map[string]any{
				"pr_number":  data["pr_number"],
				"repository": data["repository"],
				"url":        data["url"],
				"comments":   comments,
				"count":      len(comments),
			}
			if source != "" {
				result["filter"] = source
			}

			if dataOnly {
				out := map[string]any{"status": "ok", "data": result}
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetEscapeHTML(false)
				return enc.Encode(out)
			}

			env.Data = result
			return protocol.Write(cmd.OutOrStdout(), env)
		},
	}

	cmd.Flags().StringVar(&pr, "pr", "", "Pull request number or branch name (required)")
	cmd.Flags().StringVar(&prBranch, "pr-branch", "", "Branch name to find PR (alternative to --pr)")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub repository owner (optional)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository name (optional)")
	cmd.Flags().StringVar(&source, "source", "", "Filter by comment source: coderabbit, greptile, human")
	cmd.Flags().BoolVar(&showAll, "all", false, "Show all comments including resolved/addressed (default: hide resolved)")
	cmd.Flags().BoolVar(&skipCache, "skip-cache", false, "Bypass result cache")
	cmd.Flags().BoolVar(&skipCache, "no-cache", false, "Bypass result cache (alias)")
	cmd.Flags().BoolVar(&dataOnly, "data-only", false, "Print only {status,data} for AI consumption")
	cmd.Flags().BoolVar(&noCAS, "no-cas", true, "Disable CAS truncation - return full output inline")
	return cmd
}

func newCIResultsCommand() *cobra.Command {
	var pr string
	var prBranch string
	var owner string
	var repo string
	var failedOnly bool
	var skipCache bool
	var dataOnly bool
	var noCAS bool

	cmd := &cobra.Command{
		Use:   "results",
		Short: "Get CI check results for a PR",
		Long:  "Retrieve CI check run results for a PR. By default shows all checks; use --failed to show only failures.",
		Example: "  # Get all CI results\n" +
			"  foxctl ci results --pr 128\n\n" +
			"  # Get only failed checks\n" +
			"  foxctl ci results --pr 128 --failed\n",
		RunE: func(cmd *cobra.Command, _ []string) error {
			effectivePR := resolvePRFlag(pr, prBranch)
			if strings.TrimSpace(effectivePR) == "" {
				return writeCIValidationError(cmd, "--pr or --pr-branch is required", "pr", "Provide --pr with a pull request number or --pr-branch with a branch name.")
			}
			owner, repo = resolveOwnerRepo(cmd.Context(), owner, repo)

			payload := map[string]any{
				"pr":          effectivePR,
				"mode":        "detailed",
				"errors_only": failedOnly,
			}
			if owner != "" {
				payload["owner"] = owner
			}
			if repo != "" {
				payload["repo"] = repo
			}

			return runCISkill(cmd, "ci/checks", payload, skipCache, dataOnly, false, noCAS)
		},
	}

	cmd.Flags().StringVar(&pr, "pr", "", "Pull request number or branch name (required)")
	cmd.Flags().StringVar(&prBranch, "pr-branch", "", "Branch name to find PR (alternative to --pr)")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub repository owner (optional)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository name (optional)")
	cmd.Flags().BoolVar(&failedOnly, "failed", false, "Show only failed checks")
	cmd.Flags().BoolVar(&skipCache, "skip-cache", false, "Bypass result cache")
	cmd.Flags().BoolVar(&skipCache, "no-cache", false, "Bypass result cache (alias)")
	cmd.Flags().BoolVar(&dataOnly, "data-only", false, "Print only {status,data} for AI consumption")
	cmd.Flags().BoolVar(&noCAS, "no-cas", true, "Disable CAS truncation - return full output inline")
	return cmd
}

type ciFlagHelp struct {
	Name      string `json:"name"`
	Shorthand string `json:"shorthand,omitempty"`
	Type      string `json:"type"`
	Required  bool   `json:"required,omitempty"`
	Default   string `json:"default,omitempty"`
	Usage     string `json:"usage"`
}

type ciHelpMetadata struct {
	Command  string       `json:"command"`
	Use      string       `json:"use"`
	Short    string       `json:"short"`
	Long     string       `json:"long,omitempty"`
	Flags    []ciFlagHelp `json:"flags,omitempty"`
	Examples []string     `json:"examples,omitempty"`
}

type ciExample struct {
	Description string `json:"description,omitempty"`
	Command     string `json:"command"`
}

func runCISkillForEnvelopeWithOpts(cmd *cobra.Command, skillName string, payload map[string]any, skipCache, noCAS bool) (envelope.Envelope, error) {
	cfg, err := loadConfig(cmd.Context())
	if err != nil {
		return envelope.Envelope{}, err
	}
	if _, err := findSkill(cfg, skillName); err != nil {
		return envelope.Envelope{}, writeCISkillMissingError(cmd, skillName)
	}
	input, err := json.Marshal(payload)
	if err != nil {
		return envelope.Envelope{}, err
	}
	runCmd := newRunCommand()
	runCmd.SetContext(cmd.Context())
	var buf bytes.Buffer
	runCmd.SetOut(&buf)
	runCmd.SetErr(cmd.ErrOrStderr())
	args := []string{"--input", string(input)}
	if skipCache {
		args = append(args, "--cache", "off")
	}
	if noCAS {
		args = append(args, "--no-cas")
	}
	args = append(args, skillName)
	runCmd.SetArgs(args)
	if err := runCmd.Execute(); err != nil {
		return envelope.Envelope{}, err
	}
	var env envelope.Envelope
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	if err := dec.Decode(&env); err != nil {
		return envelope.Envelope{}, fmt.Errorf("decode envelope: %w", err)
	}
	return env, nil
}

var runTodoSkillFunc = runTodoSkill

func createTodoFromCITask(cmd *cobra.Command, tm map[string]any, storePath string) error {
	// Extract key fields from the CI task
	rawSummary, _ := tm["summary"].(string)
	summary := strings.ReplaceAll(rawSummary, "`", "'")
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	kind, _ := tm["kind"].(string)
	source, _ := tm["source"].(string)
	severity, _ := tm["severity"].(string)
	file, _ := tm["file"].(string)
	commentAuthor, _ := tm["comment_author"].(string)
	commentBody, _ := tm["comment_body"].(string)

	var lineStr string
	if v, ok := tm["line"].(float64); ok {
		lineStr = fmt.Sprintf("%d", int(v))
	}

	title := summary
	if kind != "" {
		title = fmt.Sprintf("[%s] %s", kind, summary)
	}
	// Build a description with metadata, but strip backticks to satisfy todo validation.
	// strings.Builder.WriteString never returns an error for in-memory writes.
	var b strings.Builder
	b.WriteString("Imported from ci/prcomments tasks_list.\n") //nolint:revive
	if source != "" {
		b.WriteString(fmt.Sprintf("Source: %s\n", source)) //nolint:revive
	}
	if severity != "" {
		b.WriteString(fmt.Sprintf("Severity: %s\n", severity)) //nolint:revive
	}
	if file != "" {
		if lineStr != "" {
			b.WriteString(fmt.Sprintf("Location: %s:%s\n", file, lineStr)) //nolint:revive
		} else {
			b.WriteString(fmt.Sprintf("Location: %s\n", file)) //nolint:revive
		}
	}
	if commentAuthor != "" {
		b.WriteString(fmt.Sprintf("Reviewer: %s\n", commentAuthor)) //nolint:revive
	}
	if commentBody != "" {
		b.WriteString("\nOriginal comment:\n") //nolint:revive
		b.WriteString(commentBody)             //nolint:revive
	}
	desc := b.String()
	desc = strings.ReplaceAll(desc, "`", "'")
	if err := rejectBackticks("title", title); err != nil {
		return err
	}
	if err := rejectBackticks("description", desc); err != nil {
		return err
	}
	payload := map[string]any{
		"operation": "add",
		"add": map[string]any{
			"title":       title,
			"description": desc,
		},
	}
	if storePath != "" {
		payload["store_path"] = storePath
	}
	return runTodoSkillFunc(cmd, payload)
}

func runCISkill(cmd *cobra.Command, skillName string, payload map[string]any, skipCache, dataOnly, noComments, noCAS bool) error {
	cfg, err := loadConfig(cmd.Context())
	if err != nil {
		return err
	}
	if _, err := findSkill(cfg, skillName); err != nil {
		return writeCISkillMissingError(cmd, skillName)
	}
	input, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	runCmd := newRunCommand()
	runCmd.SetContext(cmd.Context())
	var buf bytes.Buffer
	if dataOnly {
		runCmd.SetOut(&buf)
	} else {
		runCmd.SetOut(cmd.OutOrStdout())
	}
	runCmd.SetErr(cmd.ErrOrStderr())
	args := []string{"--input", string(input)}
	if skipCache {
		args = append(args, "--cache", "off")
	}
	if noCAS {
		args = append(args, "--no-cas")
	}
	args = append(args, skillName)
	runCmd.SetArgs(args)
	if err := runCmd.Execute(); err != nil {
		return err
	}
	if !dataOnly {
		return nil
	}
	var env envelope.Envelope
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	if err := dec.Decode(&env); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if noComments && skillName == "ci/prcomments" {
		if m, ok := env.Data.(map[string]any); ok {
			delete(m, "comments")
			env.Data = m
		}
	}
	out := map[string]any{
		"status": env.Status,
		"data":   env.Data,
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("encode data-only: %w", err)
	}
	return nil
}

func writeCIHelpJSON(cmd *cobra.Command) error {
	commandPath := strings.TrimSpace(cmd.CommandPath())
	sub := strings.TrimPrefix(commandPath, "foxctl ")
	sub = strings.TrimSpace(sub)

	help := ciHelpMetadata{
		Command: sub,
		Use:     cmd.UseLine(),
		Short:   cmd.Short,
		Long:    cmd.Long,
	}

	var flags []ciFlagHelp
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		flag := ciFlagHelp{
			Name:      f.Name,
			Shorthand: f.Shorthand,
			Type:      f.Value.Type(),
			Default:   f.DefValue,
			Usage:     f.Usage,
		}
		if f.Name == "pr" {
			flag.Required = true
		}
		flags = append(flags, flag)
	})
	help.Flags = flags

	if ex := strings.TrimSpace(cmd.Example); ex != "" {
		help.Examples = []string{ex}
	}

	path := strings.ReplaceAll(sub, " ", "/")
	command := "help/" + path
	env := protocol.OK(command, help, protocol.WithSource("cli"))
	if err := protocol.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write help envelope: %w", err)
	}
	return nil
}

func writeCIExamples(cmd *cobra.Command) error {
	examples := []ciExample{
		{
			Description: "Get unified CI status for a PR.",
			Command:     "foxctl ci status --pr 123",
		},
		{
			Description: "Fetch CI checks (data-only JSON for AI consumption).",
			Command:     "foxctl ci checks --pr 123 --data-only",
		},
		{
			Description: "Summarize review comments only (no inline comments).",
			Command:     "foxctl ci prcomments --pr 123 --no-comments",
		},
		{
			Description: "Import CI tasks into todo/manage.",
			Command:     "foxctl ci todos --pr 123 --store ./todo",
		},
	}

	payload := map[string]any{
		"examples": examples,
		"hint":     "Add a subcommand like status, checks, prcomments, todos, or comments for targeted output.",
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.ci.examples", payload, protocol.WithSource("cli"))
}

func writeCIValidationError(cmd *cobra.Command, message, field, hint string) error {
	data := protocol.ValidationErrorData{
		Field:  field,
		Reason: message,
		Hint:   hint,
		Context: map[string]any{
			"command": cmd.CommandPath(),
			"flag":    field,
		},
	}
	env := protocol.ValidationError(cmd.CommandPath(), message, data, protocol.WithSource("cli"))
	if err := protocol.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write validation error envelope: %w", err)
	}
	return fmt.Errorf("%s", message)
}

func writeCISkillMissingError(cmd *cobra.Command, skillName string) error {
	msg := fmt.Sprintf("%s skill not found", skillName)
	data := protocol.ErrorData{
		Hint: "Build embedded skills with 'make skills-build' or install the skill via 'foxctl skills install'.",
		Context: map[string]any{
			"skill":   skillName,
			"command": cmd.CommandPath(),
		},
	}
	env := protocol.ErrorWithData(skillName, protocol.ErrorCodeESkillDown, msg, data, protocol.WithSource("cli"))
	if err := protocol.Write(cmd.OutOrStdout(), env); err != nil {
		return fmt.Errorf("write skill missing error envelope: %w", err)
	}
	return fmt.Errorf("%s", msg)
}

func init() {
	rootCmd.AddCommand(newCICommand())
}
