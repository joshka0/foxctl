package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newCICommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ci",
		Short: "Inspect CI status and PR review tasks via skills",
		Long: "CI helpers built on ci/* skills. Use these commands to summarize PR review tasks, " +
			"GitHub checks, and import tasks into todo/manage.\n\n" +
			"Common workflows:\n" +
			"  agentctl ci prcomments --pr <number-or-branch> [flags]\n" +
			"  agentctl ci checks --pr <number-or-branch> [flags]\n" +
			"  agentctl ci todos --pr <number-or-branch> [flags]\n\n" +
			"See docs/ci/ for detailed examples and skill contracts.",
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
	var helpJSON bool

	cmd := &cobra.Command{
		Use:   "prcomments",
		Short: "Summarize PR merge conflicts, CI failures, and review comments",
		Long: "Summarize merge conflicts, CI failures, and PR review comments for a GitHub pull request. " +
			"The underlying ci/prcomments skill returns a task-focused markdown report and a machine-readable JSON summary.",
		Example: "  # Task-focused report with markdown output and errors-only focus\n" +
			"  agentctl ci prcomments \\\n" +
			"    --pr 66 \\\n" +
			"    --owner jkatigb \\\n" +
			"    --repo agentctl \\\n" +
			"    --with-context \\\n" +
			"    --errors-only \\\n" +
			"    --output-path docs/prcomments/pr66.md\n\n" +
			"  # JSON-centric output for AI/tooling\n" +
			"  agentctl ci prcomments \\\n" +
			"    --pr 66 \\\n" +
			"    --owner jkatigb \\\n" +
			"    --repo agentctl \\\n" +
			"    --format json \\\n" +
			"    --errors-only\n",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if helpJSON {
				return writeCIHelpJSON(cmd)
			}
			if strings.TrimSpace(pr) == "" {
				return writeCIValidationError(cmd, "--pr is required", "pr", "Provide --pr with a pull request number or branch name, for example: --pr 66.")
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
	cmd.Flags().BoolVar(&helpJSON, "help-json", false, "Emit JSON help metadata instead of running the skill")
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
	var helpJSON bool

	cmd := &cobra.Command{
		Use:   "checks",
		Short: "Summarize GitHub check runs for a PR",
		Long:  "Summarize GitHub check runs for a pull request, with optional detailed mode for failed steps.",
		Example: "  # View failing checks only (detailed)\n" +
			"  agentctl ci checks \\\n" +
			"    --pr 66 \\\n" +
			"    --owner jkatigb \\\n" +
			"    --repo agentctl \\\n" +
			"    --mode detailed \\\n" +
			"    --errors-only\n",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(pr) == "" {
				return writeCIValidationError(cmd, "--pr is required", "pr", "Provide --pr with a pull request number or branch name, for example: --pr 66.")
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
	cmd.Flags().BoolVar(&helpJSON, "help-json", false, "Emit JSON help metadata instead of running the skill")
	return cmd
}

func newCITodosCommand() *cobra.Command {
	var pr string
	var owner string
	var repo string
	var storePath string
	var skipCache bool
	var helpJSON bool

	cmd := &cobra.Command{
		Use:   "todos",
		Short: "Import CI/PR review tasks into todo/manage",
		Long:  "Import CI and PR review tasks for a GitHub pull request into the todo/manage skill.",
		Example: "  agentctl ci todos \\\n" +
			"    --pr 78 \\\n" +
			"    --owner jkatigb \\\n" +
			"    --repo agentctl \\\n" +
			"    --store ~/.agentctl/todo/tasks.json\n",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if helpJSON {
				return writeCIHelpJSON(cmd)
			}
			if strings.TrimSpace(pr) == "" {
				return writeCIValidationError(cmd, "--pr is required", "pr", "Provide --pr with a pull request number or branch name, for example: --pr 78.")
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
	cmd.Flags().BoolVar(&helpJSON, "help-json", false, "Emit JSON help metadata instead of running the skill")
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

func runCISkillForEnvelope(cmd *cobra.Command, skillName string, payload map[string]any, skipCache bool) (envelope.Envelope, error) {
	cfg, err := config.Load(cmd.Context())
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
	sub := strings.TrimPrefix(commandPath, "agentctl ")
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
		Hint: "Build embedded skills with 'make skills-build' or install the skill via 'agentctl skills install'.",
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
