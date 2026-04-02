package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
	"github.com/spf13/cobra"
)

func newTmuxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tmux",
		Short: "Inspect tmux panes for live multi-agent collaboration",
	}
	cmd.AddCommand(
		newTmuxListCommand(),
		newTmuxReadCommand(),
		newTmuxSendCommand(),
		newTmuxObserveCommand(),
		newTmuxDoctorCommand(),
		newTmuxCreateCommand(),
	)
	return cmd
}

func newTmuxListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List panes from the reachable tmux server",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := tmuxbridge.New()
			panes, err := client.List(cmd.Context())
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.list", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint": "Run `agentctl tmux doctor` to inspect connectivity, or set TMUX_BRIDGE_SOCKET if the tmux env is stale.",
				}, protocol.WithSource("cli"))
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.list", map[string]any{
				"panes": panes,
				"count": len(panes),
			}, protocol.WithSource("cli"))
		},
	}
}

func newTmuxReadCommand() *cobra.Command {
	var lines int

	cmd := &cobra.Command{
		Use:   "read <target>",
		Short: "Capture the last N lines from a tmux pane",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := tmuxbridge.New()
			result, err := client.Read(cmd.Context(), args[0], lines)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.read", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint": "Use a pane id like %3 or a label set with tmux-bridge name <target> <label>.",
				}, protocol.WithSource("cli"))
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.read", map[string]any{
				"capture": result,
			}, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().IntVar(&lines, "lines", 50, "Number of scrollback lines to capture")
	return cmd
}

func newTmuxDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose tmux connectivity for agentctl and tmux-bridge",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := tmuxbridge.New()
			report, err := client.Doctor(cmd.Context())
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.doctor", protocol.ErrorCodeERuntime, fmt.Sprintf("tmux doctor failed: %v", err), map[string]any{
					"hint": "Install tmux, or run inside a tmux session before using live collaboration tools.",
				}, protocol.WithSource("cli"))
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.doctor", map[string]any{
				"report": report,
			}, protocol.WithSource("cli"))
		},
	}
}

func newTmuxCreateCommand() *cobra.Command {
	var (
		session        string
		panes          int
		paneCommand    string
		agent          string
		agentArgs      []string
		agentSessionID string
		cwd            string
		labelPrefix    string
		attach         bool
	)

	cmd := &cobra.Command{
		Use:     "create",
		Aliases: []string{"prepare"},
		Short:   "Create or extend a tmux collaboration session and label its panes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if panes <= 0 {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.create", protocol.ErrorCodeEARG, "panes must be positive", map[string]any{
					"hint": "Use --panes with a value greater than zero.",
				}, protocol.WithSource("cli"))
			}
			if strings.TrimSpace(cwd) == "" {
				if wd, err := os.Getwd(); err == nil {
					cwd = wd
				}
			}
			client := tmuxbridge.New()
			result, err := client.PrepareSession(cmd.Context(), tmuxbridge.PrepareOptions{
				Session:        session,
				Panes:          panes,
				PaneCommand:    paneCommand,
				Agent:          agent,
				AgentArgs:      append([]string(nil), agentArgs...),
				AgentSessionID: agentSessionID,
				CWD:            cwd,
				LabelPrefix:    labelPrefix,
			})
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint": "Ensure tmux is installed and the target socket is writable. Use --agent plus repeated --agent-arg values for claude/codex/gemini/agent/droid, and use --agent-session-id for codex/claude resume launches.",
				}, protocol.WithSource("cli"))
			}
			if attach {
				if err := client.AttachOrSwitch(cmd.Context(), result.Session); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint":   "Prepare succeeded, but the attach/switch step failed. Try the returned attach command manually.",
						"result": result,
					}, protocol.WithSource("cli"))
				}
				return nil
			}
			senderExample := tmuxbridgeSenderExample(result.Panes)
			targetExample := tmuxbridgeLabelExample(result.Panes)
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.create", map[string]any{
				"result": result,
				"hints": map[string]any{
					"attach":       result.AttachCommand,
					"read_example": "agentctl tmux read " + tmuxbridgeLabelExample(result.Panes) + " --lines 80",
					"send_example": "agentctl tmux send " + targetExample + " \"review this pane\" --sender " + senderExample,
				},
			}, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().StringVar(&session, "session", "agentctl-collab", "tmux session name")
	cmd.Flags().IntVar(&panes, "panes", 3, "Number of panes to prepare")
	cmd.Flags().StringVar(&paneCommand, "pane-command", "", "Command to launch in each pane (default: current shell)")
	cmd.Flags().StringVar(&agent, "agent", "", "Agent CLI to launch in each pane (for example: claude, codex, gemini, agent, droid)")
	cmd.Flags().StringArrayVar(&agentArgs, "agent-arg", nil, "Agent CLI argument (repeatable, preserves order)")
	cmd.Flags().StringVar(&agentSessionID, "agent-session-id", "", "Resume the given agent session id (supported for codex and claude; currently requires --panes 1)")
	cmd.Flags().StringVar(&cwd, "cwd", "", "Working directory for new panes (default: current directory)")
	cmd.Flags().StringVar(&labelPrefix, "label-prefix", "", "Pane label prefix (default: derived from --agent, otherwise agent)")
	cmd.Flags().BoolVar(&attach, "attach", false, "Attach or switch to the prepared session after setup")
	return cmd
}

func newTmuxSendCommand() *cobra.Command {
	var sender string

	cmd := &cobra.Command{
		Use:   "send <target> <text>",
		Short: "Send a structured bridge message into a tmux pane",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := tmuxbridge.New()
			result, err := client.Send(cmd.Context(), sender, args[0], strings.Join(args[1:], " "))
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.send", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint": "Use a pane id like %3 or a pane label like agent-b. When invoking outside tmux, pass --sender <pane-label> so replies can route back to your pane.",
				}, protocol.WithSource("cli"))
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.send", map[string]any{
				"result": result,
			}, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().StringVar(&sender, "sender", "", "Sender pane label or pane id when invoking outside tmux or overriding the current pane")
	return cmd
}

func tmuxbridgeLabelExample(panes []tmuxbridge.Pane) string {
	if len(panes) == 0 {
		return "agent-b"
	}
	if len(panes) > 1 && strings.TrimSpace(panes[1].Label) != "" {
		return panes[1].Label
	}
	if strings.TrimSpace(panes[0].Label) != "" {
		return panes[0].Label
	}
	return panes[0].ID
}

func tmuxbridgeSenderExample(panes []tmuxbridge.Pane) string {
	if len(panes) == 0 {
		return "agent-a"
	}
	if strings.TrimSpace(panes[0].Label) != "" {
		return panes[0].Label
	}
	return panes[0].ID
}

func newTmuxObserveCommand() *cobra.Command {
	var (
		lines      int
		statement  string
		workspace  string
		confidence float64
		count      int
		project    string
		area       string
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "observe <target>",
		Short: "Promote the latest tmux-bridge message in a pane into an ACA observation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := tmuxbridge.New()
			capture, err := client.Read(cmd.Context(), args[0], lines)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.observe", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint": "Use a pane id like %3 or a label such as agent-b.",
				}, protocol.WithSource("cli"))
			}

			msg, ok := tmuxbridge.LatestBridgeMessage(capture.Lines)
			if !ok {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.observe", protocol.ErrorCodeERuntime, "no tmux-bridge message found in the captured pane lines", map[string]any{
					"hint":    "Send a bridge message first, or increase --lines if the relevant message is further up in scrollback.",
					"capture": capture,
				}, protocol.WithSource("cli"))
			}

			targetWorkspace := resolveContextWorkspace(workspace)
			obs := contextplane.Observation{
				Statement:    defaultTmuxObservationStatement(strings.TrimSpace(statement), capture, msg),
				Confidence:   confidence,
				Count:        count,
				Project:      strings.TrimSpace(project),
				Area:         firstNonEmpty(strings.TrimSpace(area), "tmux-collab"),
				EvidenceRefs: tmuxObservationEvidenceRefs(capture, msg),
			}

			path := ""
			summary := "Recorded tmux-derived observation."
			if dryRun {
				summary = "Dry run: tmux-derived observation not recorded."
			} else {
				store := contextplane.NewWorkspaceStore(targetWorkspace)
				var appendErr error
				path, appendErr = store.AppendObservation(obs)
				if appendErr != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.observe", protocol.ErrorCodeERuntime, appendErr.Error(), map[string]any{
						"hint": "Check workspace permissions and ACA layout under .agentctl/runtime/.",
					}, protocol.WithSource("cli"))
				}
			}

			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.observe", map[string]any{
				"workspace_path": targetWorkspace,
				"capture":        capture,
				"bridge_message": msg,
				"observation":    obs,
				"path":           path,
				"summary":        summary,
				"dry_run":        dryRun,
			}, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().IntVar(&lines, "lines", 80, "Number of scrollback lines to inspect")
	cmd.Flags().StringVar(&statement, "statement", "", "Optional observation statement override")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Workspace path (default: auto-detect from cwd)")
	cmd.Flags().Float64Var(&confidence, "confidence", 0.6, "Observation confidence from 0.0 to 1.0")
	cmd.Flags().IntVar(&count, "count", 1, "Observed count")
	cmd.Flags().StringVar(&project, "project", "", "Project name")
	cmd.Flags().StringVar(&area, "area", "tmux-collab", "Observation area")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without persisting to ACA")
	return cmd
}

func defaultTmuxObservationStatement(override string, capture tmuxbridge.ReadResult, msg tmuxbridge.BridgeMessage) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	target := firstNonEmpty(strings.TrimSpace(capture.Pane.Label), strings.TrimSpace(capture.Target), strings.TrimSpace(capture.ResolvedTarget))
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return fmt.Sprintf("tmux bridge message from %s reached %s", msg.From, target)
	}
	return fmt.Sprintf("tmux bridge message from %s to %s: %s", msg.From, target, content)
}

func tmuxObservationEvidenceRefs(capture tmuxbridge.ReadResult, msg tmuxbridge.BridgeMessage) []string {
	refs := []string{
		"tmux:" + firstNonEmpty(strings.TrimSpace(capture.Pane.Label), strings.TrimSpace(capture.ResolvedTarget), strings.TrimSpace(capture.Target)),
		"tmux-session:" + capture.Pane.Session,
		"tmux-bridge:from:" + msg.From,
	}
	if strings.TrimSpace(msg.ReplyTo) != "" {
		refs = append(refs, "tmux-bridge:reply_to:"+strings.TrimSpace(msg.ReplyTo))
	}
	return refs
}

func init() {
	rootCmd.AddCommand(newTmuxCommand())
}
