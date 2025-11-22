package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/spf13/cobra"
)

func newCICommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Inspect CI status and PR review tasks via skills",
	}
	cmd.AddCommand(
		newCIPRCommentsCommand(),
		newCIChecksCommand(),
		newCITodosCommand(),
	)
	return cmd
}

func newCIPRCommentsCommand() *cobra.Command {
	var pr string
	var owner string
	var repo string
	var format string
	var outputPath string
	var withContext bool
	var errorsOnly bool
	var skipCache bool
	var dataOnly bool
	var noComments bool

	cmd := &cobra.Command{
		Use:   "prcomments",
		Short: "Summarize PR merge conflicts, CI failures, and review comments",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(pr) == "" {
				return fmt.Errorf("--pr is required")
			}
			payload := map[string]any{
				"pr":           pr,
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
			return runCISkill(cmd, "ci/prcomments", payload, skipCache, dataOnly, noComments)
		},
	}

	cmd.Flags().StringVar(&pr, "pr", "", "Pull request number or branch name (required)")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub repository owner (optional)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository name or owner/repo shorthand (optional)")
	cmd.Flags().StringVar(&format, "format", "", "Output format emphasis: markdown or json (default markdown)")
	cmd.Flags().StringVar(&outputPath, "output-path", "", "Workspace-relative path to write markdown report (e.g. docs/prcomments/pr66.md)")
	cmd.Flags().BoolVar(&withContext, "with-context", false, "Include PR description and timestamps in the markdown report")
	cmd.Flags().BoolVar(&errorsOnly, "errors-only", false, "Only include failing CI and actionable review comments")
	cmd.Flags().BoolVar(&skipCache, "skip-cache", false, "Bypass result cache and always execute the skill")
	cmd.Flags().BoolVar(&dataOnly, "data-only", false, "Print only {status,data} from the envelope for AI consumption")
	cmd.Flags().BoolVar(&noComments, "no-comments", false, "Omit raw comments array from data when used with --data-only")
	if err := cmd.MarkFlagRequired("pr"); err != nil {
		panic(err)
	}
	return cmd
}

func newCIChecksCommand() *cobra.Command {
	var pr string
	var owner string
	var repo string
	var mode string
	var errorsOnly bool
	var skipCache bool
	var dataOnly bool

	cmd := &cobra.Command{
		Use:   "checks",
		Short: "Summarize GitHub check runs for a PR",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(pr) == "" {
				return fmt.Errorf("--pr is required")
			}
			payload := map[string]any{
				"pr":          pr,
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
			return runCISkill(cmd, "ci/github_checks", payload, skipCache, dataOnly, false)
		},
	}

	cmd.Flags().StringVar(&pr, "pr", "", "Pull request number or branch name (required)")
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub repository owner (optional)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository name or owner/repo shorthand (optional)")
	cmd.Flags().StringVar(&mode, "mode", "", "Detail level: summary or detailed (default summary)")
	cmd.Flags().BoolVar(&errorsOnly, "errors-only", false, "Only include failing/errored/cancelled checks")
	cmd.Flags().BoolVar(&skipCache, "skip-cache", false, "Bypass result cache and always execute the skill")
	cmd.Flags().BoolVar(&dataOnly, "data-only", false, "Print only {status,data} from the envelope for AI consumption")
	if err := cmd.MarkFlagRequired("pr"); err != nil {
		panic(err)
	}
	return cmd
}

func newCITodosCommand() *cobra.Command {
	var pr string
	var owner string
	var repo string
	var storePath string
	var skipCache bool

	cmd := &cobra.Command{
		Use:   "todos",
		Short: "Import CI/PR review tasks into todo/manage",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(pr) == "" {
				return fmt.Errorf("--pr is required")
			}
			payload := map[string]any{
				"pr":           pr,
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
			env, err := runCISkillForEnvelope(cmd, "ci/prcomments", payload, skipCache)
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
	cmd.Flags().StringVar(&owner, "owner", "", "GitHub repository owner (optional)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository name or owner/repo shorthand (optional)")
	cmd.Flags().StringVar(&storePath, "store", "", "Path to todo store (default: ~/.agentctl/todo/tasks.json)")
	cmd.Flags().BoolVar(&skipCache, "skip-cache", false, "Bypass result cache and always execute the ci/prcomments skill")
	if err := cmd.MarkFlagRequired("pr"); err != nil {
		panic(err)
	}
	return cmd
}

func runCISkillForEnvelope(cmd *cobra.Command, skillName string, payload map[string]any, skipCache bool) (envelope.Envelope, error) {
	cfg, err := config.Load(cmd.Context())
	if err != nil {
		return envelope.Envelope{}, err
	}
	if _, err := findSkill(cfg, skillName); err != nil {
		return envelope.Envelope{}, fmt.Errorf("%s skill not found (run make skills-build or agentctl skills install): %w", skillName, err)
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
	var b strings.Builder
	b.WriteString("Imported from ci/prcomments tasks_list.\n")
	if source != "" {
		b.WriteString(fmt.Sprintf("Source: %s\n", source))
	}
	if severity != "" {
		b.WriteString(fmt.Sprintf("Severity: %s\n", severity))
	}
	if file != "" {
		if lineStr != "" {
			b.WriteString(fmt.Sprintf("Location: %s:%s\n", file, lineStr))
		} else {
			b.WriteString(fmt.Sprintf("Location: %s\n", file))
		}
	}
	if commentAuthor != "" {
		b.WriteString(fmt.Sprintf("Reviewer: %s\n", commentAuthor))
	}
	if commentBody != "" {
		b.WriteString("\nOriginal comment:\n")
		b.WriteString(commentBody)
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
	return runTodoSkill(cmd, payload)
}

func runCISkill(cmd *cobra.Command, skillName string, payload map[string]any, skipCache, dataOnly, noComments bool) error {
	cfg, err := config.Load(cmd.Context())
	if err != nil {
		return err
	}
	if _, err := findSkill(cfg, skillName); err != nil {
		return fmt.Errorf("%s skill not found (run make skills-build or agentctl skills install): %w", skillName, err)
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

func init() {
	rootCmd.AddCommand(newCICommand())
}
