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
