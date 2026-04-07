package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
	"github.com/jkatigb/agentctl/internal/zellijbridge"
	"github.com/spf13/cobra"
)

var muxLabelSanitizer = regexp.MustCompile(`[^a-z0-9._-]+`)

type muxCreateError struct {
	code protocol.ErrorCode
	msg  string
	hint string
	data map[string]any
}

func (e *muxCreateError) Error() string {
	return e.msg
}

func newTmuxCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "mux",
		Aliases: []string{"tmux"},
		Short:   "Inspect and message terminal panes for live multi-agent collaboration",
	}
	cmd.AddCommand(
		newTmuxListCommand(),
		newTmuxReadCommand(),
		newTmuxSendCommand(),
		newTmuxRemindCommand(),
		newTmuxSubmitCommand(),
		newTmuxSendParentCommand(),
		newTmuxObserveCommand(),
		newTmuxDoctorCommand(),
		newTmuxCreateCommand(),
	)
	return cmd
}

func newTmuxRemindCommand() *cobra.Command {
	var (
		workspace     string
		sender        string
		recipient     string
		subject       string
		every         time.Duration
		maxIterations int
		replyExpected bool
		ackRequired   bool
		interrupt     bool
	)

	cmd := &cobra.Command{
		Use:   "remind [room-id] <text>",
		Short: "Create a durable room reminder for the current mux participant",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			roomID, body, err := resolveMuxRemindArgs(args)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.remind", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"hint": "Pass agentctl mux remind <room-id> \"...\", or run inside a room-bound pane so AGENTCTL_ROOM_ID is available.",
				}, protocol.WithSource("cli"))
			}
			resolvedRecipient := strings.TrimSpace(recipient)
			if resolvedRecipient == "" {
				identity, err := resolveRoomSender(cmd.Context(), sender)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.remind", protocol.ErrorCodeEARG, err.Error(), map[string]any{
						"hint": "Pass --recipient explicitly or run inside a labeled tmux/zellij pane so agentctl can derive the current participant id.",
					}, protocol.WithSource("cli"))
				}
				resolvedRecipient = identity.Sender
			}
			return runRoomRemindAdd(cmd, workspace, sender, roomID, resolvedRecipient, subject, body, every, maxIterations, ackRequired, replyExpected, interrupt)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&recipient, "recipient", "", "Reminder recipient (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&subject, "subject", "", "Optional root message subject")
	cmd.Flags().DurationVar(&every, "every", 15*time.Minute, "Reminder interval")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 3, "Maximum reminder follow-ups after the initial request")
	cmd.Flags().BoolVar(&replyExpected, "reply-expected", true, "Require a reply to stop reminders")
	cmd.Flags().BoolVar(&ackRequired, "ack-required", false, "Require an ack to stop reminders")
	cmd.Flags().BoolVar(&interrupt, "interrupt", false, "Interrupt the target pane for reminder follow-ups")
	return cmd
}

func resolveMuxRemindArgs(args []string) (string, string, error) {
	switch len(args) {
	case 1:
		roomID := strings.TrimSpace(os.Getenv("AGENTCTL_ROOM_ID"))
		if roomID == "" {
			return "", "", fmt.Errorf("room id is required outside a room-bound pane")
		}
		body := strings.TrimSpace(args[0])
		if body == "" {
			return "", "", fmt.Errorf("reminder text is required")
		}
		return roomID, body, nil
	case 2:
		roomID := strings.TrimSpace(args[0])
		body := strings.TrimSpace(args[1])
		if roomID == "" {
			return "", "", fmt.Errorf("room id is required")
		}
		if body == "" {
			return "", "", fmt.Errorf("reminder text is required")
		}
		return roomID, body, nil
	default:
		return "", "", fmt.Errorf("expected [room-id] <text>")
	}
}

func newTmuxListCommand() *cobra.Command {
	var (
		backend string
		session string
		limit   int
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List mux panes or agent-owned zellij panes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			switch strings.TrimSpace(backend) {
			case "", "tmux":
				client := tmuxbridge.New()
				panes, err := client.List(cmd.Context())
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.list", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint": "Run `agentctl mux doctor` to inspect connectivity, or set TMUX_BRIDGE_SOCKET if the tmux env is stale.",
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.list", map[string]any{
					"backend": "tmux",
					"panes":   panes,
					"count":   len(panes),
				}, protocol.WithSource("cli"))
			case "zellij":
				cfg, err := loadConfig(cmd.Context())
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.list", protocol.ErrorCodeERuntime, fmt.Sprintf("load config: %v", err), map[string]any{
						"hint": "Ensure agentctl configuration is readable before inspecting zellij-owned panes.",
					}, protocol.WithSource("cli"))
				}
				store, err := openAgentStore(cmd.Context(), cfg.Storage.Root)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.list", protocol.ErrorCodeERuntime, fmt.Sprintf("open agent store: %v", err), map[string]any{
						"hint": "Ensure the storage root is initialized and readable.",
					}, protocol.WithSource("cli"))
				}
				defer func() { _ = store.Close() }()
				resolvedSession := strings.TrimSpace(session)
				if resolvedSession == "" {
					resolvedSession = strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME"))
				}
				agentsList, err := store.List(cmd.Context(), limit)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.list", protocol.ErrorCodeERuntime, fmt.Sprintf("list agents: %v", err), map[string]any{
						"hint": "Verify the agent store is healthy and readable.",
					}, protocol.WithSource("cli"))
				}
				panes := listZellijBoundPanes(agentsList, resolvedSession)
				return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.list", map[string]any{
					"backend": "zellij",
					"mode":    "agent_bindings",
					"session": resolvedSession,
					"panes":   panes,
					"count":   len(panes),
				}, protocol.WithSource("cli"))
			default:
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.list", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported backend %q", backend), map[string]any{
					"hint": "Use --backend tmux or --backend zellij.",
				}, protocol.WithSource("cli"))
			}
		},
	}

	cmd.Flags().StringVar(&backend, "backend", "tmux", "Terminal backend to inspect (tmux|zellij)")
	cmd.Flags().StringVar(&session, "session", "", "Zellij session name when --backend zellij (defaults to ZELLIJ_SESSION_NAME)")
	cmd.Flags().IntVar(&limit, "limit", 500, "Maximum agents to scan when --backend zellij")
	return cmd
}

func newTmuxReadCommand() *cobra.Command {
	var lines int

	cmd := &cobra.Command{
		Use:   "read <target>",
		Short: "Capture the last N lines from a mux pane",
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
		Short: "Diagnose mux connectivity for agentctl and tmux-bridge",
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
		backend           string
		session           string
		panes             int
		paneCommand       string
		agent             string
		agentMode         string
		agentArgs         []string
		agentSessionID    string
		cwd               string
		labelPrefix       string
		parentParticipant string
		parentAgentID     string
		roomID            string
		roomAccess        string
		attach            bool
	)

	cmd := &cobra.Command{
		Use:     "create",
		Aliases: []string{"prepare"},
		Short:   "Create or extend a collaboration session and label its panes",
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
			resolvedBackend := resolveMuxCreateBackend(strings.TrimSpace(backend))
			if resolvedBackend == "" {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.create", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported backend %q", backend), map[string]any{
					"hint": "Use --backend auto, tmux, or zellij.",
				}, protocol.WithSource("cli"))
			}
			resolvedSession := resolveMuxCreateSession(cmd, resolvedBackend, strings.TrimSpace(session))
			if resolvedBackend == "zellij" {
				result, err := runMuxCreateZellij(cmd, resolvedSession, panes, paneCommand, agent, agentMode, agentArgs, agentSessionID, cwd, labelPrefix, parentParticipant, parentAgentID, roomID, roomAccess, attach)
				if err != nil {
					var createErr *muxCreateError
					if errors.As(err, &createErr) {
						return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.create", createErr.code, createErr.msg, mergeHintData(createErr.hint, createErr.data), protocol.WithSource("cli"))
					}
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint": "Ensure zellij is installed, the target session exists, and the current user can run zellij actions against it.",
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.create", map[string]any{
					"result": result,
					"hints": map[string]any{
						"attach": result["attach_command"],
					},
				}, protocol.WithSource("cli"))
			}
			client := tmuxbridge.New()
			result, err := client.PrepareSession(cmd.Context(), tmuxbridge.PrepareOptions{
				Session:           resolvedSession,
				Panes:             panes,
				PaneCommand:       paneCommand,
				Agent:             agent,
				AgentMode:         agentMode,
				AgentArgs:         append([]string(nil), agentArgs...),
				AgentSessionID:    agentSessionID,
				CWD:               cwd,
				LabelPrefix:       labelPrefix,
				ParentParticipant: parentParticipant,
				ParentAgentID:     parentAgentID,
				RoomID:            roomID,
				RoomAccess:        roomAccess,
			})
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint": "Ensure tmux is installed and the target socket is writable. Use --agent plus repeated --agent-arg values, --mode auto for known autonomous mappings, and --agent-session-id for codex/claude resume launches.",
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
					"read_example": "agentctl mux read " + tmuxbridgeLabelExample(result.Panes) + " --lines 80",
					"send_example": "agentctl mux send " + targetExample + " \"review this pane\" --sender " + senderExample,
				},
			}, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().StringVar(&backend, "backend", "auto", "Terminal backend to prepare (auto|tmux|zellij)")
	cmd.Flags().StringVar(&session, "session", "", "Mux session name (defaults to current zellij session when inside zellij, otherwise agentctl-collab)")
	cmd.Flags().IntVar(&panes, "panes", 3, "Number of panes to prepare")
	cmd.Flags().StringVar(&paneCommand, "pane-command", "", "Command to launch in each pane (default: current shell)")
	cmd.Flags().StringVar(&agent, "agent", "", "Agent CLI to launch in each pane (for example: claude, codex, gemini, agent, droid)")
	cmd.Flags().StringVar(&agentMode, "mode", "interactive", "Agent launch mode: interactive or auto")
	cmd.Flags().StringArrayVar(&agentArgs, "agent-arg", nil, "Agent CLI argument (repeatable, preserves order)")
	cmd.Flags().StringVar(&agentSessionID, "agent-session-id", "", "Resume the given agent session id (supported for codex and claude; currently requires --panes 1)")
	cmd.Flags().StringVar(&cwd, "cwd", "", "Working directory for new panes (default: current directory)")
	cmd.Flags().StringVar(&labelPrefix, "label-prefix", "", "Pane label prefix (default: derived from --agent, otherwise agent)")
	cmd.Flags().StringVar(&parentParticipant, "parent-participant", "", "Parent participant id for child panes; implies room access none by default")
	cmd.Flags().StringVar(&parentAgentID, "parent-agent-id", "", "Parent agent id exported into launched panes")
	cmd.Flags().StringVar(&roomID, "room-id", "", "Room id exported into launched panes when room access is direct")
	cmd.Flags().StringVar(&roomAccess, "room-access", "default", "Room access policy for launched panes: default|direct|none")
	cmd.Flags().BoolVar(&attach, "attach", false, "Attach or switch to the prepared session after setup")
	return cmd
}

func resolveMuxCreateBackend(raw string) string {
	switch strings.TrimSpace(raw) {
	case "", "auto":
		if strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME")) != "" {
			return "zellij"
		}
		return "tmux"
	case "tmux", "zellij":
		return strings.TrimSpace(raw)
	default:
		return ""
	}
}

func resolveMuxCreateSession(cmd *cobra.Command, backend, raw string) string {
	value := strings.TrimSpace(raw)
	if value != "" {
		return value
	}
	if backend == "zellij" {
		if current := strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME")); current != "" {
			return current
		}
	}
	return "agentctl-collab"
}

func runMuxCreateZellij(cmd *cobra.Command, session string, panes int, paneCommand, agent, agentMode string, agentArgs []string, agentSessionID, cwd, labelPrefix, parentParticipant, parentAgentID, roomID, roomAccess string, attach bool) (map[string]any, error) {
	if strings.TrimSpace(session) == "" {
		return nil, &muxCreateError{
			code: protocol.ErrorCodeEARG,
			msg:  "zellij session is required",
			hint: "Pass --session or run inside zellij so ZELLIJ_SESSION_NAME can be detected.",
		}
	}
	command, err := resolveMuxCreateCommand(strings.TrimSpace(paneCommand), strings.TrimSpace(agent), strings.TrimSpace(agentMode), append([]string(nil), agentArgs...), strings.TrimSpace(agentSessionID))
	if err != nil {
		return nil, &muxCreateError{
			code: protocol.ErrorCodeEARG,
			msg:  err.Error(),
			hint: "Use --pane-command directly, or pass --agent with optional repeated --agent-arg values.",
		}
	}
	prefix := strings.TrimSpace(labelPrefix)
	if prefix == "" {
		prefix = deriveMuxCreateLabelPrefix(strings.TrimSpace(agent))
	}
	client := zellijbridge.New()
	created := make([]map[string]any, 0, panes)
	for i := 0; i < panes; i++ {
		name := zellijPaneNameForIndex(prefix, i)
		result, createErr := client.CreatePane(cmd.Context(), zellijbridge.CreatePaneOptions{
			Session:           session,
			CWD:               cwd,
			Name:              name,
			Command:           command,
			ParticipantID:     name,
			ParentParticipant: parentParticipant,
			ParentAgentID:     parentAgentID,
			RoomID:            roomID,
			RoomAccess:        roomAccess,
		})
		if createErr != nil {
			return nil, &muxCreateError{
				code: protocol.ErrorCodeERuntime,
				msg:  createErr.Error(),
				hint: "Ensure zellij is installed, the target session exists, and the current user can run zellij actions against it.",
			}
		}
		created = append(created, map[string]any{
			"backend":        "zellij",
			"session":        result.Session,
			"pane_name":      result.PaneName,
			"participant_id": result.ParticipantID,
		})
	}
	attachCommand := "zellij attach " + shellQuoteZshSafe(session)
	if attach && strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME")) == "" {
		attachCmd := exec.CommandContext(cmd.Context(), "zellij", "attach", session)
		attachCmd.Stdin = os.Stdin
		attachCmd.Stdout = os.Stdout
		attachCmd.Stderr = os.Stdout
		if err := attachCmd.Run(); err != nil {
			return nil, &muxCreateError{
				code: protocol.ErrorCodeERuntime,
				msg:  err.Error(),
				hint: "Pane creation succeeded, but attaching to the zellij session failed. Try the returned attach command manually.",
				data: map[string]any{
					"result": map[string]any{
						"session":        session,
						"panes":          created,
						"attach_command": attachCommand,
					},
				},
			}
		}
	}
	return map[string]any{
		"backend":            "zellij",
		"session":            session,
		"created":            true,
		"panes_requested":    panes,
		"pane_command":       command,
		"agent":              agent,
		"agent_mode":         agentMode,
		"agent_args":         append([]string(nil), agentArgs...),
		"agent_session_id":   agentSessionID,
		"cwd":                cwd,
		"label_prefix":       prefix,
		"parent_participant": parentParticipant,
		"parent_agent_id":    parentAgentID,
		"room_id":            roomID,
		"room_access":        roomAccess,
		"attach_command":     attachCommand,
		"panes":              created,
	}, nil
}

func resolveMuxCreateCommand(paneCommand, agent, agentMode string, agentArgs []string, agentSessionID string) (string, error) {
	if paneCommand != "" {
		return paneCommand, nil
	}
	if agent == "" {
		shell := strings.TrimSpace(os.Getenv("SHELL"))
		if shell == "" {
			shell = "zsh"
		}
		return shell, nil
	}
	args := make([]string, 0, 8)
	args = append(args, agent)
	if strings.TrimSpace(agentSessionID) != "" {
		switch strings.TrimSpace(agent) {
		case "codex":
			args = append(args, "resume", agentSessionID)
		case "claude":
			args = append(args, "--resume", agentSessionID)
		default:
			return "", fmt.Errorf("--agent-session-id is currently supported only for codex and claude")
		}
	}
	switch strings.TrimSpace(agentMode) {
	case "", "interactive":
	case "auto":
		switch strings.TrimSpace(agent) {
		case "codex":
			args = append(args, "--full-auto")
		case "claude":
			args = append(args, "--dangerously-skip-permissions")
		case "gemini", "agent":
			args = append(args, "--yolo")
		default:
			return "", fmt.Errorf("auto mode is unsupported for agent %q", agent)
		}
	default:
		return "", fmt.Errorf("unsupported mode %q", agentMode)
	}
	args = append(args, agentArgs...)
	return joinShellCommand(args), nil
}

func deriveMuxCreateLabelPrefix(agent string) string {
	base := strings.ToLower(strings.TrimSpace(agent))
	if base == "" {
		return "agent"
	}
	base = muxLabelSanitizer.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		return "agent"
	}
	return base
}

func zellijPaneNameForIndex(prefix string, idx int) string {
	if idx < 26 {
		return fmt.Sprintf("%s-%c", prefix, rune('a'+idx))
	}
	return fmt.Sprintf("%s-%d", prefix, idx+1)
}

func joinShellCommand(parts []string) string {
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuoteZshSafe(part))
	}
	return strings.Join(quoted, " ")
}

func shellQuoteZshSafe(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$`()[]{}*?!&;|<>") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func mergeHintData(hint string, data map[string]any) map[string]any {
	if strings.TrimSpace(hint) == "" && len(data) == 0 {
		return nil
	}
	out := make(map[string]any, len(data)+1)
	for k, v := range data {
		out[k] = v
	}
	if strings.TrimSpace(hint) != "" {
		out["hint"] = hint
	}
	return out
}

func newTmuxSendCommand() *cobra.Command {
	var sender string

	cmd := &cobra.Command{
		Use:   "send <target> <text>",
		Short: "Send a structured bridge message into a mux pane",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := tmuxbridge.New()
			result, err := client.Send(cmd.Context(), sender, args[0], strings.Join(args[1:], " "))
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.send", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint": "Use a pane id like %3 or a pane label like agent-b. When invoking outside the active mux, pass --sender <pane-label> so replies can route back to your pane.",
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

func newTmuxSubmitCommand() *cobra.Command {
	var (
		backend string
		session string
	)

	cmd := &cobra.Command{
		Use:   "submit [target]",
		Short: "Submit the current mux draft with Escape then Enter",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolvedBackend := resolveMuxCreateBackend(strings.TrimSpace(backend))
			if resolvedBackend == "" {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.submit", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported backend %q", backend), map[string]any{
					"hint": "Use --backend auto, tmux, or zellij.",
				}, protocol.WithSource("cli"))
			}
			switch resolvedBackend {
			case "tmux":
				if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.submit", protocol.ErrorCodeEARG, "target is required for tmux submit", map[string]any{
						"hint": "Pass a pane id like %3 or a pane label like agent-b.",
					}, protocol.WithSource("cli"))
				}
				client := tmuxbridge.New()
				result, err := client.Submit(cmd.Context(), args[0])
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.submit", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint": "Use a pane id like %3 or a pane label set with tmux-bridge name <target> <label>.",
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.submit", map[string]any{
					"backend": "tmux",
					"result":  result,
				}, protocol.WithSource("cli"))
			case "zellij":
				resolvedSession := strings.TrimSpace(session)
				if resolvedSession == "" {
					resolvedSession = strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME"))
				}
				if resolvedSession == "" {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.submit", protocol.ErrorCodeEARG, "session is required for zellij submit", map[string]any{
						"hint": "Pass --session or run inside the target zellij session.",
					}, protocol.WithSource("cli"))
				}
				client := zellijbridge.New()
				result, err := client.Submit(cmd.Context(), resolvedSession)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.submit", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint": "Zellij submit acts on the focused pane in the named session and requires an attached client.",
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.submit", map[string]any{
					"backend": "zellij",
					"result":  result,
				}, protocol.WithSource("cli"))
			default:
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.submit", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported backend %q", resolvedBackend), nil, protocol.WithSource("cli"))
			}
		},
	}

	cmd.Flags().StringVar(&backend, "backend", "auto", "Mux backend to submit against (auto|tmux|zellij)")
	cmd.Flags().StringVar(&session, "session", "", "Zellij session name when --backend zellij (defaults to ZELLIJ_SESSION_NAME)")
	return cmd
}

func newTmuxSendParentCommand() *cobra.Command {
	var sender string

	cmd := &cobra.Command{
		Use:   "send-parent <text>",
		Short: "Send a private message to the parent participant from the current pane",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			parent, err := resolveParentParticipantID()
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.send-parent", protocol.ErrorCodeEARG, "AGENTCTL_PARENT_PARTICIPANT_ID is not set", map[string]any{
					"hint": "Launch the pane with --parent-participant or pass the parent explicitly with agentctl mux send --sender ... <target>.",
				}, protocol.WithSource("cli"))
			}
			client := tmuxbridge.New()
			result, err := client.Send(cmd.Context(), sender, parent, strings.Join(args, " "))
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.tmux.send-parent", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint":   "Ensure the current pane is inside the active mux and the parent participant pane label is reachable.",
					"parent": parent,
				}, protocol.WithSource("cli"))
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.tmux.send-parent", map[string]any{
				"parent": parent,
				"result": result,
			}, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().StringVar(&sender, "sender", "", "Override the sender pane label or pane id")
	return cmd
}

func resolveParentParticipantID() (string, error) {
	parent := strings.TrimSpace(os.Getenv("AGENTCTL_PARENT_PARTICIPANT_ID"))
	if parent == "" {
		return "", fmt.Errorf("AGENTCTL_PARENT_PARTICIPANT_ID is not set")
	}
	return parent, nil
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
		Short: "Promote the latest mux bridge message in a pane into an ACA observation",
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
