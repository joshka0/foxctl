package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/agent/prompts"
	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/context/contextplane"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/terminal/agentpane"
	"github.com/joshka0/foxctl/internal/runtime/terminal/herdrbridge"
	"github.com/joshka0/foxctl/internal/runtime/terminal/tmuxbridge"
	"github.com/joshka0/foxctl/internal/runtime/terminal/zellijbridge"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/spf13/cobra"
)

var (
	muxLabelSanitizer        = regexp.MustCompile(`[^a-z0-9._-]+`)
	muxGroupDeliverAgentPane = agentpane.Deliver
)

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
		newTmuxSubmitAllCommand(),
		newTmuxInterruptAllCommand(),
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
		passive       bool
	)

	cmd := &cobra.Command{
		Use:   "remind [room-id] <text>",
		Short: "Create a durable room reminder for the current mux participant",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			roomID, body, err := resolveMuxRemindArgs(args)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.remind", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"hint": "Pass foxctl mux remind <room-id> \"...\", or run inside a room-bound pane so FOXCTL_ROOM_ID is available.",
				}, protocol.WithSource("cli"))
			}
			resolvedRecipient := strings.TrimSpace(recipient)
			if resolvedRecipient == "" {
				identity, err := resolveRoomSender(cmd.Context(), sender)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.remind", protocol.ErrorCodeEARG, err.Error(), map[string]any{
						"hint": "Pass --recipient explicitly or run inside a labeled tmux/zellij pane so foxctl can derive the current participant id.",
					}, protocol.WithSource("cli"))
				}
				resolvedRecipient = identity.Sender
			}
			return runRoomRemindAdd(cmd, workspace, sender, roomID, resolvedRecipient, subject, body, "", "", "", every, maxIterations, ackRequired, replyExpected, interrupt, passive, false)
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
	cmd.Flags().BoolVar(&passive, "passive", false, "Relay reminders durably without creating ack/reply inbox debt")
	return cmd
}

func resolveMuxRemindArgs(args []string) (string, string, error) {
	switch len(args) {
	case 1:
		roomID := strings.TrimSpace(os.Getenv("FOXCTL_ROOM_ID"))
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
		socket  string
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
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.list", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint": "Run `foxctl mux doctor` to inspect connectivity, or set TMUX_BRIDGE_SOCKET if the tmux env is stale.",
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.list", map[string]any{
					"backend": "tmux",
					"panes":   panes,
					"count":   len(panes),
				}, protocol.WithSource("cli"))
			case "herdr":
				client := herdrbridge.NewWithOptions(herdrbridge.Options{Session: session, SocketPath: socket})
				panes, err := client.List(cmd.Context(), herdrbridge.ListOptions{})
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.list", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint":        "Start Herdr first, pass --session <name>, or set HERDR_SOCKET_PATH.",
						"socket_path": client.SocketPath(),
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.list", map[string]any{
					"backend":     "herdr",
					"session":     herdrMuxSessionLabel(session),
					"socket_path": client.SocketPath(),
					"panes":       panes,
					"count":       len(panes),
				}, protocol.WithSource("cli"))
			case "zellij":
				cfg, err := loadConfig(cmd.Context())
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.list", protocol.ErrorCodeERuntime, fmt.Sprintf("load config: %v", err), map[string]any{
						"hint": "Ensure foxctl configuration is readable before inspecting zellij-owned panes.",
					}, protocol.WithSource("cli"))
				}
				store, err := openAgentStore(cmd.Context(), cfg.Storage.Root)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.list", protocol.ErrorCodeERuntime, fmt.Sprintf("open agent store: %v", err), map[string]any{
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
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.list", protocol.ErrorCodeERuntime, fmt.Sprintf("list agents: %v", err), map[string]any{
						"hint": "Verify the agent store is healthy and readable.",
					}, protocol.WithSource("cli"))
				}
				panes := listZellijBoundPanes(agentsList, resolvedSession)
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.list", map[string]any{
					"backend": "zellij",
					"mode":    "agent_bindings",
					"session": resolvedSession,
					"panes":   panes,
					"count":   len(panes),
				}, protocol.WithSource("cli"))
			default:
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.list", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported backend %q", backend), map[string]any{
					"hint": "Use --backend tmux, zellij, or herdr.",
				}, protocol.WithSource("cli"))
			}
		},
	}

	cmd.Flags().StringVar(&backend, "backend", "tmux", "Terminal backend to inspect (tmux|zellij|herdr)")
	cmd.Flags().StringVar(&session, "session", "", "Zellij session name or Herdr session namespace")
	cmd.Flags().StringVar(&socket, "socket", "", "Herdr Unix socket path override when --backend herdr")
	cmd.Flags().IntVar(&limit, "limit", 500, "Maximum agents to scan when --backend zellij")
	return cmd
}

func newTmuxReadCommand() *cobra.Command {
	var (
		backend   string
		session   string
		socket    string
		source    string
		format    string
		stripANSI bool
		lines     int
	)

	cmd := &cobra.Command{
		Use:   "read <target>",
		Short: "Capture the last N lines from a mux pane",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch strings.TrimSpace(backend) {
			case "", "tmux":
				client := tmuxbridge.New()
				result, err := client.Read(cmd.Context(), args[0], lines)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.read", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint": "Use a pane id like %3 or a label set with tmux-bridge name <target> <label>.",
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.read", map[string]any{
					"backend": "tmux",
					"capture": result,
				}, protocol.WithSource("cli"))
			case "herdr":
				client := herdrbridge.NewWithOptions(herdrbridge.Options{Session: session, SocketPath: socket})
				result, err := client.Read(cmd.Context(), args[0], herdrbridge.ReadOptions{
					Source:       source,
					Lines:        lines,
					Format:       format,
					StripANSI:    stripANSI,
					StripANSISet: true,
				})
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.read", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint":        "Use a Herdr pane id like w...-1 or positional shorthand like 1-1.",
						"socket_path": client.SocketPath(),
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.read", map[string]any{
					"backend":     "herdr",
					"socket_path": client.SocketPath(),
					"capture":     result,
				}, protocol.WithSource("cli"))
			default:
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.read", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported backend %q", backend), map[string]any{
					"hint": "Use --backend tmux or --backend herdr.",
				}, protocol.WithSource("cli"))
			}
		},
	}

	cmd.Flags().StringVar(&backend, "backend", "tmux", "Terminal backend to read from (tmux|herdr)")
	cmd.Flags().StringVar(&session, "session", "", "Herdr session namespace when --backend herdr")
	cmd.Flags().StringVar(&socket, "socket", "", "Herdr Unix socket path override when --backend herdr")
	cmd.Flags().StringVar(&source, "source", herdrbridge.ReadSourceRecent, "Herdr read source (visible|recent|recent_unwrapped)")
	cmd.Flags().StringVar(&format, "format", herdrbridge.ReadFormatText, "Herdr read format (text|ansi)")
	cmd.Flags().BoolVar(&stripANSI, "strip-ansi", true, "Strip ANSI when reading Herdr text")
	cmd.Flags().IntVar(&lines, "lines", 50, "Number of scrollback lines to capture")
	return cmd
}

func newTmuxDoctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose mux connectivity for foxctl and tmux-bridge",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client := tmuxbridge.New()
			report, err := client.Doctor(cmd.Context())
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.doctor", protocol.ErrorCodeERuntime, fmt.Sprintf("tmux doctor failed: %v", err), map[string]any{
					"hint": "Install tmux, or run inside a tmux session before using live collaboration tools.",
				}, protocol.WithSource("cli"))
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.doctor", map[string]any{
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
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.create", protocol.ErrorCodeEARG, "panes must be positive", map[string]any{
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
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.create", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported backend %q", backend), map[string]any{
					"hint": "Use --backend auto, tmux, or zellij.",
				}, protocol.WithSource("cli"))
			}
			resolvedSession := resolveMuxCreateSession(cmd, resolvedBackend, strings.TrimSpace(session))
			if resolvedBackend == "zellij" {
				result, err := runMuxCreateZellij(cmd, resolvedSession, panes, paneCommand, agent, agentMode, agentArgs, agentSessionID, cwd, labelPrefix, parentParticipant, parentAgentID, roomID, roomAccess, attach)
				if err != nil {
					var createErr *muxCreateError
					if errors.As(err, &createErr) {
						return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.create", createErr.code, createErr.msg, mergeHintData(createErr.hint, createErr.data), protocol.WithSource("cli"))
					}
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint": "Ensure zellij is installed, the target session exists, and the current user can run zellij actions against it.",
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.create", map[string]any{
					"result": result,
					"hints": map[string]any{
						"attach": result["attach_command"],
					},
				}, protocol.WithSource("cli"))
			}
			client := tmuxbridge.New()
			paneServeExecutable, execErr := os.Executable()
			if execErr != nil || strings.TrimSpace(paneServeExecutable) == "" {
				paneServeExecutable = "foxctl"
			}
			result, err := client.PrepareSession(cmd.Context(), tmuxbridge.PrepareOptions{
				Session:             resolvedSession,
				Panes:               panes,
				PaneCommand:         paneCommand,
				Agent:               agent,
				AgentMode:           agentMode,
				AgentArgs:           append([]string(nil), agentArgs...),
				AgentSessionID:      agentSessionID,
				CWD:                 cwd,
				LabelPrefix:         labelPrefix,
				ParentParticipant:   parentParticipant,
				ParentAgentID:       parentAgentID,
				RoomID:              roomID,
				RoomAccess:          roomAccess,
				PaneServeExecutable: paneServeExecutable,
			})
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint": "Ensure tmux is installed and the target socket is writable. Use --agent plus repeated --agent-arg values, --mode auto for known autonomous mappings, and --agent-session-id for codex/claude resume launches.",
				}, protocol.WithSource("cli"))
			}
			if attach {
				if err := client.AttachOrSwitch(cmd.Context(), result.Session); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint":   "Prepare succeeded, but the attach/switch step failed. Try the returned attach command manually.",
						"result": result,
					}, protocol.WithSource("cli"))
				}
				return nil
			}
			senderExample := tmuxbridgeSenderExample(result.Panes)
			targetExample := tmuxbridgeLabelExample(result.Panes)
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.create", map[string]any{
				"result": result,
				"hints": map[string]any{
					"attach":       result.AttachCommand,
					"read_example": "foxctl mux read " + tmuxbridgeLabelExample(result.Panes) + " --lines 80",
					"send_example": "foxctl mux send " + targetExample + " \"review this pane\" --sender " + senderExample,
				},
			}, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().StringVar(&backend, "backend", "auto", "Terminal backend to prepare (auto|tmux|zellij)")
	cmd.Flags().StringVar(&session, "session", "", "Mux session name (defaults to current zellij session when inside zellij, otherwise foxctl-collab)")
	cmd.Flags().IntVar(&panes, "panes", 3, "Number of panes to prepare")
	cmd.Flags().StringVar(&paneCommand, "pane-command", "", "Command to launch in each pane (default: current shell)")
	cmd.Flags().StringVar(&agent, "agent", "", "Agent CLI to launch in each pane (for example: claude, codex, gemini, agent, droid)")
	cmd.Flags().StringVar(&agentMode, "mode", "interactive", "Agent launch mode: interactive or auto")
	cmd.Flags().StringArrayVar(&agentArgs, "agent-arg", nil, "Agent CLI argument (repeatable, preserves order)")
	cmd.Flags().StringVar(&agentSessionID, "agent-session-id", "", "Resume the given agent session id (supported for codex, claude, gemini, droid, and agent; currently requires --panes 1)")
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

func resolveMuxRuntimeBackend(raw string) string {
	switch strings.TrimSpace(raw) {
	case "", "auto":
		if strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME")) != "" {
			return "zellij"
		}
		return "tmux"
	case "tmux", "zellij", "herdr":
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
	return "foxctl-collab"
}

func herdrMuxSessionLabel(session string) string {
	if session = strings.TrimSpace(session); session != "" {
		return session
	}
	if session = strings.TrimSpace(os.Getenv("HERDR_SESSION")); session != "" {
		return session
	}
	return "default"
}

func runMuxCreateZellij(cmd *cobra.Command, session string, panes int, paneCommand, agent, agentMode string, agentArgs []string, agentSessionID, cwd, labelPrefix, parentParticipant, parentAgentID, roomID, roomAccess string, attach bool) (map[string]any, error) {
	if strings.TrimSpace(session) == "" {
		return nil, &muxCreateError{
			code: protocol.ErrorCodeEARG,
			msg:  "zellij session is required",
			hint: "Pass --session or run inside zellij so ZELLIJ_SESSION_NAME can be detected.",
		}
	}
	prefix := strings.TrimSpace(labelPrefix)
	if prefix == "" {
		prefix = deriveMuxCreateLabelPrefix(session, roomID, strings.TrimSpace(agent))
	}
	previewCommand, err := resolveMuxCreateCommandWithPrompt(
		strings.TrimSpace(paneCommand),
		strings.TrimSpace(agent),
		strings.TrimSpace(agentMode),
		append([]string(nil), agentArgs...),
		strings.TrimSpace(agentSessionID),
		buildMuxCreateRoomAgentPrompt(cwd, roomID, roomAccess, "<participant>"),
	)
	if err != nil {
		return nil, &muxCreateError{
			code: protocol.ErrorCodeEARG,
			msg:  err.Error(),
			hint: "Use --pane-command directly, or pass --agent with optional repeated --agent-arg values.",
		}
	}
	client := zellijbridge.New()
	created := make([]map[string]any, 0, panes)
	for i := 0; i < panes; i++ {
		name := zellijPaneNameForIndex(prefix, i)
		command, err := resolveMuxCreateCommandWithPrompt(
			strings.TrimSpace(paneCommand),
			strings.TrimSpace(agent),
			strings.TrimSpace(agentMode),
			append([]string(nil), agentArgs...),
			strings.TrimSpace(agentSessionID),
			buildMuxCreateRoomAgentPrompt(cwd, roomID, roomAccess, name),
		)
		if err != nil {
			return nil, &muxCreateError{
				code: protocol.ErrorCodeEARG,
				msg:  err.Error(),
				hint: "Use --pane-command directly, or pass --agent with optional repeated --agent-arg values.",
			}
		}
		paneCommand := wrapZellijPaneCommand(session, name, roomID, command, zellijPaneStartupProfile(agent, agentMode))
		result, createErr := client.CreatePane(cmd.Context(), zellijbridge.CreatePaneOptions{
			Session:           session,
			CWD:               cwd,
			Name:              name,
			Command:           paneCommand,
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
			"socket_path":    agentpane.DefaultSocketPath(session, name),
			"ready_path":     agentpane.DefaultReadyPath(session, name),
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
		"pane_command":       previewCommand,
		"wrapped_command":    wrapZellijPaneCommand(session, "<participant>", roomID, previewCommand, zellijPaneStartupProfile(agent, agentMode)),
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
	return resolveMuxCreateCommandWithPrompt(paneCommand, agent, agentMode, agentArgs, agentSessionID, "")
}

func resolveMuxCreateCommandWithPrompt(paneCommand, agent, agentMode string, agentArgs []string, agentSessionID, initialPrompt string) (string, error) {
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
		resumeArgs, err := tmuxbridge.AgentResumeArgs(strings.TrimSpace(agent), agentSessionID)
		if err != nil {
			return "", fmt.Errorf("--agent-session-id %w", err)
		}
		args = append(args, resumeArgs...)
	}
	switch strings.TrimSpace(agentMode) {
	case "", "interactive":
	case "auto":
		switch strings.TrimSpace(agent) {
		case "codex":
			args = append(args, "--full-auto")
		case "claude":
			args = append(args, "--permission-mode", "bypassPermissions")
		case "gemini", "agent":
			args = append(args, "--approval-mode", "yolo")
		case "droid":
			// Keep Droid's interactive launch clean. The pane wrapper startup profile
			// upgrades autonomy to High after the UI reaches its stable "Auto (Off)"
			// state, which is more reliable than passing extra root flags here.
		default:
			return "", fmt.Errorf("auto mode is unsupported for agent %q", agent)
		}
	default:
		return "", fmt.Errorf("unsupported mode %q", agentMode)
	}
	args = append(args, agentArgs...)
	args = append(args, muxCreateInteractivePromptArgs(strings.TrimSpace(agent), initialPrompt)...)
	return joinShellCommand(args), nil
}

func buildMuxCreateRoomAgentPrompt(workspaceID, roomID, roomAccess, participantID string) string {
	if strings.TrimSpace(roomID) == "" || strings.TrimSpace(roomAccess) != "direct" {
		return ""
	}
	block := prompts.RoomOnboardingBlock(prompts.RoomOnboardingOptions{
		RoomID:      strings.TrimSpace(roomID),
		WorkspaceID: strings.TrimSpace(workspaceID),
		Role:        "participant",
	})
	if strings.TrimSpace(block) == "" {
		return ""
	}
	return block + "\n- Your participant id is \"" + strings.TrimSpace(participantID) + "\". When replying from this room-bound pane, prefer `foxctl room send " + strings.TrimSpace(roomID) + " --to <recipient> \"<response>\"`; foxctl will derive your sender automatically here. Use `--sender " + strings.TrimSpace(participantID) + "` only when replying from outside this pane."
}

func muxCreateInteractivePromptArgs(agentName, initialPrompt string) []string {
	initialPrompt = strings.TrimSpace(initialPrompt)
	if initialPrompt == "" {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(agentName)) {
	case "claude", "codex", "droid":
		return []string{initialPrompt}
	case "gemini":
		return []string{"--prompt-interactive", initialPrompt}
	default:
		return nil
	}
}

func deriveMuxCreateLabelPrefix(session, roomID, agent string) string {
	base := strings.ToLower(strings.TrimSpace(agent))
	if base == "" {
		return "agent"
	}
	base = muxLabelSanitizer.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		return "agent"
	}
	scope := strings.ToLower(strings.TrimSpace(roomID))
	if scope == "" {
		scope = strings.ToLower(strings.TrimSpace(session))
		if scope == "" || scope == "foxctl-collab" {
			scope = ""
		}
	}
	scope = muxLabelSanitizer.ReplaceAllString(scope, "-")
	scope = strings.Trim(scope, "-")
	if scope != "" {
		return scope + "-" + base
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

func wrapZellijPaneCommand(session, participantID, roomID, childCommand, startupProfile string) string {
	foxctlPath, err := os.Executable()
	if err != nil || strings.TrimSpace(foxctlPath) == "" {
		foxctlPath = "foxctl"
	}
	args := []string{
		foxctlPath,
		"pane",
		"serve",
		"--participant", strings.TrimSpace(participantID),
		"--socket-path", agentpane.DefaultSocketPath(session, participantID),
	}
	if strings.TrimSpace(roomID) != "" {
		args = append(args, "--room-id", strings.TrimSpace(roomID))
	}
	if strings.TrimSpace(startupProfile) != "" {
		args = append(args, "--startup-profile", strings.TrimSpace(startupProfile))
	}
	args = append(
		args,
		"--default-submit-mode", agentpane.SubmitModeNewline,
		"--",
		"sh", "-lc", childCommand,
	)
	return joinShellCommand(args)
}

func zellijPaneStartupProfile(agent, agentMode string) string {
	if strings.EqualFold(strings.TrimSpace(agent), "droid") && strings.EqualFold(strings.TrimSpace(agentMode), "auto") {
		return agentpane.StartupProfileDroidAutoHigh
	}
	return ""
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
	var (
		sender         string
		workspace      string
		confirmRoomID  string
		confirmActor   string
		confirmReplyTo string
		confirmTimeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "send <target> <text>",
		Short: "Send a structured bridge message into a mux pane",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			confirmSpec, err := resolveMuxSendConfirmation(cmd.Context(), workspace, confirmRoomID, confirmActor, confirmReplyTo, confirmTimeout)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.send", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"hint": "Provide --confirm-room-id, --confirm-actor, and --confirm-reply-to together when using send confirmation.",
				}, protocol.WithSource("cli"))
			}
			sendStartedAt := time.Now().UTC()
			client := tmuxbridge.New()
			result, err := client.Send(cmd.Context(), sender, args[0], strings.Join(args[1:], " "))
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.send", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint": "Use a pane id like %3 or a pane label like agent-b. When invoking outside the active mux, pass --sender <pane-label> so replies can route back to your pane.",
				}, protocol.WithSource("cli"))
			}
			if confirmSpec != nil {
				confirmSpec.StartedAt = sendStartedAt
				confirmation, err := waitForMuxRoomConfirmation(cmd.Context(), *confirmSpec)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.send", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"result":       result,
						"confirmation": confirmation,
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.send", map[string]any{
					"result":       result,
					"confirmation": confirmation,
				}, protocol.WithSource("cli"))
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.send", map[string]any{
				"result": result,
			}, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override for room confirmation")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender pane label or pane id when invoking outside tmux or overriding the current pane")
	cmd.Flags().StringVar(&confirmRoomID, "confirm-room-id", "", "Room id to poll for durable confirmation")
	cmd.Flags().StringVar(&confirmActor, "confirm-actor", "", "Room actor expected to post the durable reply")
	cmd.Flags().StringVar(&confirmReplyTo, "confirm-reply-to", "", "Original room message id expected to be answered")
	cmd.Flags().DurationVar(&confirmTimeout, "confirm-timeout", 30*time.Second, "Maximum time to wait for durable room confirmation")
	return cmd
}

type muxSendConfirmationSpec struct {
	Workspace string
	RoomID    string
	ActorID   string
	ReplyTo   string
	Timeout   time.Duration
	StartedAt time.Time
}

type muxSendConfirmationResult struct {
	Mode              string    `json:"mode"`
	RoomID            string    `json:"room_id"`
	ActorID           string    `json:"actor_id"`
	ReplyTo           string    `json:"reply_to"`
	Status            string    `json:"status"`
	Signal            string    `json:"signal,omitempty"`
	ConfirmedAt       time.Time `json:"confirmed_at,omitempty"`
	ReplyMessageID    string    `json:"reply_message_id,omitempty"`
	ReplyInboxCleared bool      `json:"reply_inbox_cleared,omitempty"`
}

func resolveMuxSendConfirmation(ctx context.Context, workspace, roomID, actorID, replyTo string, timeout time.Duration) (*muxSendConfirmationSpec, error) {
	roomID = strings.TrimSpace(roomID)
	actorID = strings.TrimSpace(actorID)
	replyTo = strings.TrimSpace(replyTo)
	if roomID == "" && actorID == "" && replyTo == "" {
		return nil, nil
	}
	if roomID == "" || actorID == "" || replyTo == "" {
		return nil, fmt.Errorf("send confirmation requires room id, actor id, and reply-to message id")
	}
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return nil, err
	}
	store, err := openRoomBoardStore(ctx)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	summary, messages, err := loadRoomState(ctx, store, absWorkspace, roomID, actorID, roomTaskScanLimit)
	if err != nil {
		return nil, err
	}
	if !roomSummaryHasParticipant(summary, actorID) {
		return nil, fmt.Errorf("confirmation actor %q is not a participant in room %q", actorID, roomID)
	}
	if roomMessageByID(messages, replyTo) == nil {
		return nil, fmt.Errorf("confirmation reply-to message %q was not found in room %q", replyTo, roomID)
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &muxSendConfirmationSpec{
		Workspace: absWorkspace,
		RoomID:    roomID,
		ActorID:   actorID,
		ReplyTo:   replyTo,
		Timeout:   timeout,
	}, nil
}

func waitForMuxRoomConfirmation(ctx context.Context, spec muxSendConfirmationSpec) (muxSendConfirmationResult, error) {
	result := muxSendConfirmationResult{
		Mode:    "room",
		RoomID:  spec.RoomID,
		ActorID: spec.ActorID,
		ReplyTo: spec.ReplyTo,
		Status:  "waiting",
	}
	deadline := time.Now().UTC().Add(spec.Timeout)
	pollEvery := 500 * time.Millisecond
	if spec.Timeout < pollEvery {
		pollEvery = spec.Timeout
	}
	if pollEvery <= 0 {
		pollEvery = 100 * time.Millisecond
	}
	store, err := openRoomBoardStore(ctx)
	if err != nil {
		return result, err
	}
	defer store.Close()
	for {
		summary, messages, err := loadRoomState(ctx, store, spec.Workspace, spec.RoomID, spec.ActorID, roomTaskScanLimit)
		if err != nil {
			return result, err
		}
		replyID, inboxCleared := detectMuxRoomReplyConfirmation(summary, messages, spec.ActorID, spec.ReplyTo)
		if replyID != "" {
			result.Status = "confirmed"
			result.Signal = "room_reply"
			result.ConfirmedAt = time.Now().UTC()
			result.ReplyMessageID = replyID
			result.ReplyInboxCleared = inboxCleared
			return result, nil
		}
		if time.Now().UTC().After(deadline) {
			result.Status = "timed_out_waiting_for_confirmation"
			result.ReplyInboxCleared = inboxCleared
			return result, fmt.Errorf("timed out waiting for durable room confirmation for actor %q in room %q", spec.ActorID, spec.RoomID)
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

func detectMuxRoomReplyConfirmation(summary agent.RoomSummary, messages []agent.BoardMessage, actorID, replyTo string) (string, bool) {
	root := roomMessageByID(messages, replyTo)
	if root == nil {
		return "", false
	}
	entries := buildRoomInboxEntries(actorID, messages, "all", false, nil)
	inboxCleared := true
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == strings.TrimSpace(replyTo) {
			inboxCleared = false
			break
		}
	}
	rootSender := strings.TrimSpace(root.Sender)
	for _, msg := range messages {
		if !sameRoomParticipant(msg.Sender, actorID) {
			continue
		}
		if strings.TrimSpace(msg.ID) == strings.TrimSpace(replyTo) {
			continue
		}
		if msg.CreatedAt.Before(root.CreatedAt) {
			continue
		}
		if msg.CreatedAt.Equal(root.CreatedAt) && strings.TrimSpace(msg.ID) < strings.TrimSpace(replyTo) {
			continue
		}
		if strings.TrimSpace(msg.RelatedMessageID) == strings.TrimSpace(replyTo) {
			return msg.ID, true
		}
		recipient := normalizeRoomRecipient(msg.Recipient)
		if rootSender != "" && (sameRoomParticipant(recipient, rootSender) || recipient == agent.BroadcastRecipient) {
			return msg.ID, true
		}
	}
	return "", inboxCleared
}

func roomMessageByID(messages []agent.BoardMessage, id string) *agent.BoardMessage {
	id = strings.TrimSpace(id)
	for i := range messages {
		if strings.TrimSpace(messages[i].ID) == id {
			return &messages[i]
		}
	}
	return nil
}

func roomSummaryHasParticipant(summary agent.RoomSummary, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	for _, participant := range summary.Participants {
		if sameRoomParticipant(participant, actorID) {
			return true
		}
	}
	for _, member := range summary.Members {
		if sameRoomParticipant(member.ActorID, actorID) {
			return true
		}
	}
	return false
}

func newTmuxSubmitCommand() *cobra.Command {
	var (
		backend   string
		session   string
		socket    string
		modeFlag  string
		paneID    string
		roomID    string
		workspace string
	)

	cmd := &cobra.Command{
		Use:    "submit [target]",
		Hidden: true,
		Short:  "Submit the current mux draft (Escape+Enter by default, or Enter-only)",
		Long: "Deprecated hidden command: relay and room send already deliver text with a trailing submit; " +
			"room task assign/claim/complete fan out to panes by default. Sends keys to submit a drafted prompt in the target pane. " +
			"Default is Escape then Enter for " +
			"multi-line UIs; use --mode enter-only when the line is complete and only Enter should be sent.\n\n" +
			"Use --room <room-id> with [target] as the room participant id to resolve mux pane bindings " +
			"from room storage (avoids mixing up foxctl room ids with zellij --session).\n\n" +
			"Without --room: for tmux pass a pane label or id as [target]; for zellij use --session and optional " +
			"--pane-id (keys go to the focused pane when pane id is omitted); for herdr pass [target], --pane-id, " +
			"or HERDR_PANE_ID.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			submitMode, err := parseMuxSubmitModeString(modeFlag)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"hint": "Use --mode escape-enter (default) or --mode enter-only.",
				}, protocol.WithSource("cli"))
			}
			if rid := strings.TrimSpace(roomID); rid != "" {
				if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeEARG, "participant target is required when using --room", map[string]any{
						"hint": "Example: foxctl mux submit --room my-room cursor-c-a",
					}, protocol.WithSource("cli"))
				}
				absWorkspace, err := resolveRoomWorkspace(workspace)
				if err != nil {
					return err
				}
				store, err := openRoomBoardStore(cmd.Context())
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				defer store.Close()
				summary, err := store.GetRoom(cmd.Context(), absWorkspace, rid, "")
				if err != nil {
					code := protocol.ErrorCodeERuntime
					if errors.Is(err, blackboard.ErrRoomNotFound) {
						code = protocol.ErrorCodeENotFound
					}
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", code, err.Error(), map[string]any{
						"hint": "Create the room or pass the correct --workspace.",
					}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				member, ok := findRoomMemberForMuxSubmit(summary, args[0])
				if !ok {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeENotFound, fmt.Sprintf("participant %q not found in room %q", strings.TrimSpace(args[0]), rid), map[string]any{
						"hint": "Use `foxctl room status <room>` to list participant ids.",
					}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				result, resolvedBackend, err := muxSubmitForRoomMember(cmd.Context(), member, submitMode)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint": "Ensure the participant joined with mux bindings (tmux label or zellij session/pane).",
					}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				data := map[string]any{
					"backend":       resolvedBackend,
					"result":        result,
					"room_id":       rid,
					"participant":   strings.TrimSpace(args[0]),
					"resolved_from": "room",
				}
				if strings.TrimSpace(session) != "" {
					data["note"] = "--session was ignored; submit used pane bindings from --room"
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.submit", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			resolvedBackend := resolveMuxRuntimeBackend(strings.TrimSpace(backend))
			if resolvedBackend == "" {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported backend %q", backend), map[string]any{
					"hint": "Use --backend auto, tmux, zellij, or herdr.",
				}, protocol.WithSource("cli"))
			}
			switch resolvedBackend {
			case "tmux":
				if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeEARG, "target is required for tmux submit", map[string]any{
						"hint": "Pass a pane id like %3 or a pane label like agent-b.",
					}, protocol.WithSource("cli"))
				}
				client := tmuxbridge.New()
				result, err := client.Submit(cmd.Context(), args[0], tmuxbridge.SubmitOptions{Mode: submitMode})
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint": "Use a pane id like %3 or a pane label set with tmux-bridge name <target> <label>.",
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.submit", map[string]any{
					"backend": "tmux",
					"result":  result,
				}, protocol.WithSource("cli"))
			case "zellij":
				resolvedSession := strings.TrimSpace(session)
				if resolvedSession == "" {
					resolvedSession = strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME"))
				}
				if resolvedSession == "" {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeEARG, "session is required for zellij submit", map[string]any{
						"hint": "Pass --session or run inside the target zellij session.",
					}, protocol.WithSource("cli"))
				}
				resolvedPane := strings.TrimSpace(paneID)
				if resolvedPane == "" {
					resolvedPane = strings.TrimSpace(os.Getenv("ZELLIJ_PANE_ID"))
				}
				client := zellijbridge.New()
				result, err := client.Submit(cmd.Context(), resolvedSession, zellijbridge.SubmitOptions{
					Mode:   submitMode,
					PaneID: resolvedPane,
				})
				if err != nil {
					hint := "Zellij submit requires an attached client. Without --pane-id, keys go to the focused pane."
					if strings.TrimSpace(resolvedPane) != "" {
						hint = "Ensure --pane-id matches a terminal pane (e.g. terminal_2) and your zellij version supports action write --pane-id."
					}
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint": hint,
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.submit", map[string]any{
					"backend": "zellij",
					"result":  result,
				}, protocol.WithSource("cli"))
			case "herdr":
				resolvedPane := strings.TrimSpace(paneID)
				if resolvedPane == "" && len(args) > 0 {
					resolvedPane = strings.TrimSpace(args[0])
				}
				if resolvedPane == "" {
					resolvedPane = strings.TrimSpace(os.Getenv("HERDR_PANE_ID"))
				}
				if resolvedPane == "" {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeEARG, "pane id is required for herdr submit", map[string]any{
						"hint": "Pass [target], --pane-id, or run inside Herdr with HERDR_PANE_ID set.",
					}, protocol.WithSource("cli"))
				}
				client := herdrbridge.NewWithOptions(herdrbridge.Options{Session: session, SocketPath: socket})
				result, err := client.Submit(cmd.Context(), resolvedPane, herdrbridge.SubmitOptions{Mode: submitMode})
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
						"hint":        "Ensure the Herdr pane exists and the Herdr socket is reachable.",
						"socket_path": client.SocketPath(),
					}, protocol.WithSource("cli"))
				}
				return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.submit", map[string]any{
					"backend":     "herdr",
					"socket_path": client.SocketPath(),
					"result":      result,
				}, protocol.WithSource("cli"))
			default:
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported backend %q", resolvedBackend), nil, protocol.WithSource("cli"))
			}
		},
	}

	cmd.Flags().StringVar(&backend, "backend", "auto", "Mux backend to submit against (auto|tmux|zellij|herdr)")
	cmd.Flags().StringVar(&session, "session", "", "Zellij session name or Herdr session namespace")
	cmd.Flags().StringVar(&socket, "socket", "", "Herdr Unix socket path override when --backend herdr")
	cmd.Flags().StringVar(&modeFlag, "mode", "escape-enter", "Submit key sequence (escape-enter|enter-only)")
	cmd.Flags().StringVar(&paneID, "pane-id", "", "Zellij terminal pane id or Herdr pane id")
	cmd.Flags().StringVar(&roomID, "room", "", "Foxctl room id: resolve session/pane from stored membership for participant [target]")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root for --room lookup")
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
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.send-parent", protocol.ErrorCodeEARG, "FOXCTL_PARENT_PARTICIPANT_ID is not set", map[string]any{
					"hint": "Launch the pane with --parent-participant or pass the parent explicitly with foxctl mux send --sender ... <target>.",
				}, protocol.WithSource("cli"))
			}
			client := tmuxbridge.New()
			result, err := client.Send(cmd.Context(), sender, parent, strings.Join(args, " "))
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.send-parent", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint":   "Ensure the current pane is inside the active mux and the parent participant pane label is reachable.",
					"parent": parent,
				}, protocol.WithSource("cli"))
			}
			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.send-parent", map[string]any{
				"parent": parent,
				"result": result,
			}, protocol.WithSource("cli"))
		},
	}

	cmd.Flags().StringVar(&sender, "sender", "", "Override the sender pane label or pane id")
	return cmd
}

func newTmuxSubmitAllCommand() *cobra.Command {
	var (
		roomID    string
		workspace string
	)
	cmd := &cobra.Command{
		Use:   "submit-all",
		Short: "Submit drafted input for every resolvable participant in a room",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(roomID) == "" {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.submit-all", protocol.ErrorCodeEARG, "--room is required", map[string]any{
					"hint": "Example: foxctl mux submit-all --room tmux-transport-first-20260410",
				}, protocol.WithSource("cli"))
			}
			return runMuxGroupControl(cmd, workspace, roomID, "submit")
		},
	}
	cmd.Flags().StringVar(&roomID, "room", "", "Foxctl room id to resolve canonical participant panes")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root for --room lookup")
	return cmd
}

func newTmuxInterruptAllCommand() *cobra.Command {
	var (
		roomID    string
		workspace string
	)
	cmd := &cobra.Command{
		Use:   "interrupt-all",
		Short: "Send group interrupt (Escape) to every resolvable participant in a room",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(roomID) == "" {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.interrupt-all", protocol.ErrorCodeEARG, "--room is required", map[string]any{
					"hint": "Example: foxctl mux interrupt-all --room tmux-transport-first-20260410",
				}, protocol.WithSource("cli"))
			}
			return runMuxGroupControl(cmd, workspace, roomID, "interrupt")
		},
	}
	cmd.Flags().StringVar(&roomID, "room", "", "Foxctl room id to resolve canonical participant panes")
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root for --room lookup")
	return cmd
}

func resolveParentParticipantID() (string, error) {
	parent := strings.TrimSpace(os.Getenv("FOXCTL_PARENT_PARTICIPANT_ID"))
	if parent == "" {
		return "", fmt.Errorf("FOXCTL_PARENT_PARTICIPANT_ID is not set")
	}
	return parent, nil
}

type muxGroupControlItem struct {
	Participant string `json:"participant"`
	Action      string `json:"action"`
	Backend     string `json:"backend,omitempty"`
	Via         string `json:"via,omitempty"`
	Target      string `json:"target,omitempty"`
	Status      string `json:"status"`
	Error       string `json:"error,omitempty"`
}

func runMuxGroupControl(cmd *cobra.Command, workspace, roomID, action string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux."+action+"-all", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()
	summary, err := store.GetRoom(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), "")
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux."+action+"-all", code, err.Error(), map[string]any{
			"hint": "Create the room first or pass the correct --workspace.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	items := make([]muxGroupControlItem, 0, len(summary.Members))
	for _, member := range summary.Members {
		member = normalizeRoomMember(member)
		if item := muxGroupControlForMember(cmd.Context(), member, action); strings.TrimSpace(item.Participant) != "" {
			items = append(items, item)
		}
	}
	completed := 0
	skipped := 0
	failed := 0
	for _, item := range items {
		switch item.Status {
		case "ok":
			completed++
		case "skipped":
			skipped++
		default:
			failed++
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux."+action+"-all", map[string]any{
		"room_id":      strings.TrimSpace(roomID),
		"action":       action,
		"completed":    completed,
		"skipped":      skipped,
		"failed":       failed,
		"participants": items,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func muxGroupControlForMember(ctx context.Context, member agent.RoomMember, action string) muxGroupControlItem {
	member = normalizeRoomMember(member)
	item := muxGroupControlItem{
		Participant: member.ActorID,
		Action:      action,
	}
	if item.Participant == "" || strings.HasPrefix(item.Participant, "actor:system:") {
		item.Status = "skipped"
		item.Error = "not a controllable participant"
		return item
	}
	if strings.HasPrefix(item.Participant, "tmux:") || strings.HasPrefix(item.Participant, "zellij:") {
		item.Status = "skipped"
		item.Error = "raw mux identity is not a canonical agent participant"
		return item
	}

	binding := agent.NormalizeRoomDeliveryBinding(member.ActorID, member.DeliveryBinding)
	if endpoint := roomDeliveryTransportEndpoint(binding); endpoint != "" && (strings.EqualFold(roomDeliveryTransportKind(binding), agent.PaneSocketTransportKind) || strings.HasPrefix(endpoint, "/")) {
		item.Backend = roomMemberRelayBackend(member)
		item.Via = "participant_transport"
		item.Target = endpoint
		if err := muxGroupControlViaSocket(ctx, endpoint, strings.TrimSpace(member.ActorID), action); err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			return item
		}
		item.Status = "ok"
		return item
	}

	switch roomMemberRelayBackend(member) {
	case "zellij":
		item.Backend = "zellij"
		item.Via = "mux"
		session, _, ok := resolveRoomMemberZellijTarget(member)
		if !ok || strings.TrimSpace(session) == "" {
			item.Status = "skipped"
			item.Error = "member has no resolvable zellij session"
			return item
		}
		pane := roomMemberMuxPaneID(member)
		if pane != "" && !isResolvableZellijPaneID(pane) {
			item.Status = "skipped"
			item.Error = "member stores zellij pane binding by title only"
			return item
		}
		pane = normalizeZellijPaneID(pane)
		item.Target = session
		if pane != "" {
			item.Target = formatZellijParticipantID(session, pane)
		}
		client := zellijbridge.New()
		if action == "interrupt" {
			_, err := client.Interrupt(ctx, session, pane)
			if err != nil {
				item.Status = "failed"
				item.Error = err.Error()
				return item
			}
		} else {
			mode := zellijSubmitModeForParticipant(item.Participant)
			_, err := client.Submit(ctx, session, zellijbridge.SubmitOptions{Mode: mode, PaneID: pane})
			if err != nil {
				item.Status = "failed"
				item.Error = err.Error()
				return item
			}
		}
		item.Status = "ok"
		return item
	case "herdr":
		item.Backend = "herdr"
		item.Via = "mux"
		session, paneID, ok := resolveRoomMemberHerdrTarget(member)
		if !ok || strings.TrimSpace(paneID) == "" {
			item.Status = "skipped"
			item.Error = "member has no resolvable herdr pane"
			return item
		}
		item.Target = paneID
		client := herdrbridge.NewWithOptions(herdrbridge.Options{Session: session})
		var err error
		if action == "interrupt" {
			_, err = client.Interrupt(ctx, paneID)
		} else {
			mode := tmuxSubmitModeForParticipant(item.Participant)
			_, err = client.Submit(ctx, paneID, herdrbridge.SubmitOptions{Mode: mode})
		}
		if err != nil {
			item.Status = "failed"
			item.Error = err.Error()
			return item
		}
		item.Status = "ok"
		return item
	default:
		item.Backend = "tmux"
		item.Via = "mux"
		target := roomMemberTmuxTarget(member)
		if strings.TrimSpace(target) == "" {
			item.Status = "skipped"
			item.Error = "member has no tmux pane target"
			return item
		}
		item.Target = target
		client := tmuxbridge.New()
		if action == "interrupt" {
			_, err := client.Interrupt(ctx, target)
			if err != nil {
				item.Status = "failed"
				item.Error = err.Error()
				return item
			}
		} else {
			mode := tmuxSubmitModeForParticipant(item.Participant)
			_, err := client.Submit(ctx, target, tmuxbridge.SubmitOptions{Mode: mode})
			if err != nil {
				item.Status = "failed"
				item.Error = err.Error()
				return item
			}
		}
		item.Status = "ok"
		return item
	}
}

func muxGroupControlViaSocket(ctx context.Context, socketPath, participantID, action string) error {
	msg := agentpane.ControlMessage{
		Kind:      action,
		Recipient: participantID,
	}
	if action == "submit" {
		msg.SubmitMode = targetSubmitMode(participantID)
	}
	_, err := muxGroupDeliverAgentPane(ctx, socketPath, msg)
	return err
}

func tmuxSubmitModeForParticipant(participantID string) string {
	switch targetSubmitMode(participantID) {
	case agentpane.SubmitModeEnter:
		return tmuxbridge.SubmitModeEnterOnly
	default:
		return tmuxbridge.SubmitModeEscapeEnter
	}
}

func zellijSubmitModeForParticipant(participantID string) string {
	switch targetSubmitMode(participantID) {
	case agentpane.SubmitModeEnter, agentpane.SubmitModeEnterSplit:
		return zellijbridge.SubmitModeEnterOnly
	default:
		return zellijbridge.SubmitModeEscapeEnter
	}
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
		Short: "Promote the latest mux bridge message in a pane into a ContextWiki observation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := tmuxbridge.New()
			capture, err := client.Read(cmd.Context(), args[0], lines)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.observe", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
					"hint": "Use a pane id like %3 or a label such as agent-b.",
				}, protocol.WithSource("cli"))
			}

			msg, ok := tmuxbridge.LatestBridgeMessage(capture.Lines)
			if !ok {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.observe", protocol.ErrorCodeERuntime, "no tmux-bridge message found in the captured pane lines", map[string]any{
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
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.tmux.observe", protocol.ErrorCodeERuntime, appendErr.Error(), map[string]any{
						"hint": "Check workspace permissions and ContextWiki layout under .foxctl/runtime/.",
					}, protocol.WithSource("cli"))
				}
			}

			return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.tmux.observe", map[string]any{
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
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview without persisting to ContextWiki")
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

func tmuxObservationEvidenceRefs(capture tmuxbridge.ReadResult, msg tmuxbridge.BridgeMessage) []contextengine.EvidenceRef {
	refs := []contextengine.EvidenceRef{
		{Type: contextengine.RefTypeRun, Ref: "tmux:" + firstNonEmpty(strings.TrimSpace(capture.Pane.Label), strings.TrimSpace(capture.ResolvedTarget), strings.TrimSpace(capture.Target))},
		{Type: contextengine.RefTypeRun, Ref: "tmux-session:" + capture.Pane.Session},
		{Type: contextengine.RefTypeRun, Ref: "tmux-bridge:from:" + msg.From},
	}
	if strings.TrimSpace(msg.ReplyTo) != "" {
		refs = append(refs, contextengine.EvidenceRef{Type: contextengine.RefTypeRun, Ref: "tmux-bridge:reply_to:" + strings.TrimSpace(msg.ReplyTo)})
	}
	return refs
}

func init() {
	rootCmd.AddCommand(newTmuxCommand())
}
