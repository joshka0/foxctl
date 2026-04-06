package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
	"github.com/jkatigb/agentctl/internal/zellijbridge"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newRoomCommand())
}

func newRoomCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "room",
		Short: "Manage durable coordination rooms and live room relays",
	}
	cmd.AddCommand(
		newRoomCreateCommand(),
		newRoomCoordinatorCommand(),
		newRoomListCommand(),
		newRoomShowCommand(),
		newRoomStatusCommand(),
		newRoomInboxCommand(),
		newRoomSendCommand(),
		newRoomAckCommand(),
		newRoomResolveCommand(),
		newRoomClearCommand(),
		newRoomPlanCommand(),
		newRoomJoinCommand(),
		newRoomLeaveCommand(),
		newRoomTaskCommand(),
		newRoomSubscribeCommand(),
		newRoomRelayCommand(),
		newRoomLoopCommand(),
	)
	return cmd
}

func newRoomCoordinatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coordinator",
		Short: "Show or update room coordinator assignment",
	}
	cmd.AddCommand(
		newRoomCoordinatorSetCommand(),
	)
	return cmd
}

func newRoomCoordinatorSetCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
	)
	cmd := &cobra.Command{
		Use:   "set <room-id> <participant-id>",
		Short: "Transfer coordinator role to another room participant",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomCoordinatorSet(cmd, workspace, args[0], actorID, args[1])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Current coordinator actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomCreateCommand() *cobra.Command {
	var (
		workspace   string
		title       string
		description string
		members     []string
		provision   bool
		muxBackend  string
		muxSession  string
		paneCommand string
		agentCLI    string
		agentMode   string
		agentArgs   []string
		memberArgs  []string
		attach      bool
	)
	cmd := &cobra.Command{
		Use:   "create <room-id>",
		Short: "Create or update a durable room",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomCreateWithProvision(cmd, workspace, args[0], title, description, members, roomCreateProvisionOptions{
				Enabled:     provision,
				MuxBackend:  muxBackend,
				MuxSession:  muxSession,
				PaneCommand: paneCommand,
				AgentCLI:    agentCLI,
				AgentMode:   agentMode,
				AgentArgs:   append([]string(nil), agentArgs...),
				MemberArgs:  append([]string(nil), memberArgs...),
				Attach:      attach,
			})
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&title, "title", "", "Room title")
	cmd.Flags().StringVar(&description, "description", "", "Room description")
	cmd.Flags().StringArrayVar(&members, "member", nil, "Room member in actor, actor=role, actor@agent, actor=role@agent, or actor=role@agent:mode form (repeatable)")
	cmd.Flags().BoolVar(&provision, "provision", false, "Provision one live mux pane per explicit --member and bind it to this room")
	cmd.Flags().StringVar(&muxBackend, "mux-backend", "auto", "Mux backend for provisioning (auto|tmux|zellij)")
	cmd.Flags().StringVar(&muxSession, "mux-session", "", "Mux session name for provisioning (defaults to current zellij session when inside zellij, otherwise agentctl-collab)")
	cmd.Flags().StringVar(&paneCommand, "pane-command", "", "Command to launch in each provisioned pane (default: current shell)")
	cmd.Flags().StringVar(&agentCLI, "agent", "", "Agent CLI to launch in each provisioned pane (for example: claude, codex, gemini, agent, droid)")
	cmd.Flags().StringVar(&agentMode, "mode", "interactive", "Provisioned agent launch mode: interactive or auto")
	cmd.Flags().StringArrayVar(&agentArgs, "agent-arg", nil, "Provisioned agent CLI argument (repeatable, preserves order)")
	cmd.Flags().StringArrayVar(&memberArgs, "member-arg", nil, "Per-member provisioned CLI arg in actor=arg form (repeatable, supports multiple args per actor)")
	cmd.Flags().BoolVar(&attach, "attach", false, "Attach or switch to the provisioned mux session after setup")
	return cmd
}

func newRoomListCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List rooms in the workspace",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runRoomList(cmd, workspace, actorID, limit)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Actor id used for unread counts")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum rooms to return")
	return cmd
}

func newRoomShowCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "show <room-id>",
		Short: "Show room metadata and recent messages",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomShow(cmd, workspace, args[0], actorID, limit)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Actor id used for unread counts")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum messages to return")
	return cmd
}

func newRoomStatusCommand() *cobra.Command {
	var (
		workspace  string
		limit      int
		staleAfter time.Duration
		only       []string
		verbose    bool
	)
	cmd := &cobra.Command{
		Use:   "status <room-id>",
		Short: "Show a coordinator-facing room summary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomStatus(cmd, workspace, args[0], limit, staleAfter, only, verbose)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().IntVar(&limit, "limit", 200, "Maximum room messages to inspect for status derivation")
	cmd.Flags().DurationVar(&staleAfter, "stale-after", 5*time.Minute, "Participant idle threshold")
	cmd.Flags().StringSliceVar(&only, "only", nil, "Filter coordinator action summary (ack,reply,assigned,blocked,stale,all)")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Include verbose actionable entry detail for debugging")
	return cmd
}

func newRoomInboxCommand() *cobra.Command {
	var (
		workspace         string
		actorID           string
		limit             int
		filter            string
		grouped           bool
		idsOnly           bool
		includeBroadcasts bool
	)
	cmd := &cobra.Command{
		Use:   "inbox <room-id>",
		Short: "Show actionable room messages for one participant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomInbox(cmd, workspace, args[0], actorID, limit, filter, grouped, idsOnly, includeBroadcasts)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Actor id used for inbox filtering (defaults to current tmux/zellij pane)")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum room messages to inspect")
	cmd.Flags().StringVar(&filter, "filter", "all", "Filter entries (all|ack-required|reply-expected|direct|broadcast)")
	cmd.Flags().BoolVar(&grouped, "grouped", false, "Group entries by category")
	cmd.Flags().BoolVar(&idsOnly, "ids-only", false, "Return only matching message ids")
	cmd.Flags().BoolVar(&includeBroadcasts, "include-broadcasts", false, "Include plain broadcast messages in the default all filter")
	return cmd
}

func newRoomSendCommand() *cobra.Command {
	var (
		workspace     string
		sender        string
		recipient     string
		subject       string
		kind          string
		taskID        string
		priority      int
		ackRequired   bool
		replyExpected bool
		interrupt     bool
		autoCreate    bool
	)
	cmd := &cobra.Command{
		Use:   "send <room-id> <text>",
		Short: "Append a durable message to a room timeline",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomSend(cmd, workspace, args[0], sender, recipient, subject, strings.Join(args[1:], " "), kind, taskID, priority, ackRequired, replyExpected, interrupt, autoCreate)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&recipient, "to", "", "Target room participant id (defaults to broadcast)")
	cmd.Flags().StringVar(&subject, "subject", "", "Optional subject line")
	cmd.Flags().StringVar(&kind, "kind", string(agent.BoardMessageKindInfo), "Message kind (info|instruction|alert|review_request)")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Optional task id")
	cmd.Flags().IntVar(&priority, "priority", agent.DefaultPriority, "Priority from 1 (highest) to 5 (lowest)")
	cmd.Flags().BoolVar(&ackRequired, "ack-required", false, "Require explicit acknowledgment")
	cmd.Flags().BoolVar(&replyExpected, "reply-expected", false, "Mark the message as expecting a response (direct messages only)")
	cmd.Flags().BoolVar(&interrupt, "interrupt", false, "Interrupt the target pane before delivering the message (direct messages only)")
	cmd.Flags().BoolVar(&autoCreate, "auto-create", true, "Create the room if it does not exist")
	return cmd
}

func newRoomAckCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
	)
	cmd := &cobra.Command{
		Use:   "ack <room-id> <message-id>...",
		Short: "Mark one or more room messages as acknowledged",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomAck(cmd, workspace, args[0], actorID, args[1:])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Actor or participant id acknowledging the messages (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomResolveCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
		mode      string
		all       bool
		only      []string
	)
	cmd := &cobra.Command{
		Use:   "resolve <room-id> [message-id]...",
		Short: "Coordinator-only cleanup for stale room messages",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomResolve(cmd, workspace, args[0], actorID, mode, all, only, args[1:])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&mode, "mode", "acked", "Resolution mode (acked|read)")
	cmd.Flags().BoolVar(&all, "all", false, "Resolve all current room entries matching --only")
	cmd.Flags().StringSliceVar(&only, "only", nil, "Filter current room entries for --all (ack,reply,direct,all)")
	return cmd
}

func newRoomClearCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
		mode      string
		preset    string
	)
	cmd := &cobra.Command{
		Use:   "clear <room-id>",
		Short: "Clear stale room inbox noise by preset",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomClear(cmd, workspace, args[0], actorID, mode, preset)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&mode, "mode", "read", "Clear mode (acked|read)")
	cmd.Flags().StringVar(&preset, "preset", "coordinator-pulses", "Cleanup preset (coordinator-pulses|system-reminders)")
	return cmd
}

func newRoomPlanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Run a durable planning protocol inside a room",
	}
	cmd.AddCommand(
		newRoomPlanStartCommand(),
		newRoomPlanProposeCommand(),
		newRoomPlanAskCommand(),
		newRoomPlanDecideCommand(),
		newRoomPlanReviewCommand(),
		newRoomPlanCloseCommand(),
		newRoomPlanShowCommand(),
	)
	return cmd
}

func newRoomPlanStartCommand() *cobra.Command {
	var (
		workspace   string
		sender      string
		goal        string
		artifact    string
		scope       []string
		constraints []string
	)
	cmd := &cobra.Command{
		Use:   "start <room-id> <topic>",
		Short: "Start a durable planning session in a room",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomPlanStart(cmd, workspace, sender, args[0], args[1], goal, artifact, scope, constraints)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&goal, "goal", "", "Planning goal or desired outcome")
	cmd.Flags().StringVar(&artifact, "artifact", "", "Target plan artifact path, doc path, or external reference")
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "Scope item (repeatable)")
	cmd.Flags().StringSliceVar(&constraints, "constraint", nil, "Constraint or guardrail (repeatable)")
	return cmd
}

func newRoomPlanProposeCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "propose <room-id> <session-id> <title> <body>",
		Short: "Submit a plan proposal within a planning session",
		Args:  cobra.MinimumNArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomPlanEntry(cmd, workspace, args[0], sender, args[1], agent.BoardMessageKindPlanProposal, "Proposal: "+strings.TrimSpace(args[2]), strings.Join(args[3:], " "), false)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomPlanAskCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "ask <room-id> <session-id> <question>",
		Short: "Record an explicit planning question",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			question := strings.Join(args[2:], " ")
			return runRoomPlanEntry(cmd, workspace, args[0], sender, args[1], agent.BoardMessageKindPlanQuestion, "Question: "+deriveRoomSubject(question), question, false)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomPlanDecideCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "decide <room-id> <session-id> <decision>",
		Short: "Record a coordinator decision for a planning session",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			decision := strings.Join(args[2:], " ")
			return runRoomPlanEntry(cmd, workspace, args[0], sender, args[1], agent.BoardMessageKindPlanDecision, "Decision: "+deriveRoomSubject(decision), decision, true)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomPlanReviewCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "review <room-id> <session-id> <approve|block> <notes>",
		Short: "Record a review or approval note for a planning session",
		Args:  cobra.MinimumNArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			verdict := strings.TrimSpace(strings.ToLower(args[2]))
			if verdict != "approve" && verdict != "block" {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan.review", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported review verdict %q", args[2]), map[string]any{
					"hint": "Use approve or block.",
				})
			}
			notes := strings.Join(args[3:], " ")
			return runRoomPlanEntry(cmd, workspace, args[0], sender, args[1], agent.BoardMessageKindPlanReview, "Review: "+verdict, notes, false)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomPlanCloseCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "close <room-id> <session-id> <summary>",
		Short: "Close a planning session with a final durable summary",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			summary := strings.Join(args[2:], " ")
			return runRoomPlanEntry(cmd, workspace, args[0], sender, args[1], agent.BoardMessageKindPlanClose, "Planning session closed", summary, true)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomPlanShowCommand() *cobra.Command {
	var (
		workspace string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "show <room-id> [session-id]",
		Short: "Show planning sessions or one planning session thread",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := ""
			if len(args) > 1 {
				sessionID = args[1]
			}
			return runRoomPlanShow(cmd, workspace, args[0], sessionID, limit)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().IntVar(&limit, "limit", 200, "Maximum room messages to inspect")
	return cmd
}

func newRoomJoinCommand() *cobra.Command {
	var (
		workspace string
		role      string
		backend   string
		session   string
		paneID    string
		unbound   bool
		create    bool
		current   bool
	)
	cmd := &cobra.Command{
		Use:   "join <room-id> [actor-id]",
		Short: "Add or update a room member",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			actorID := ""
			if len(args) > 1 {
				actorID = args[1]
			}
			return runRoomJoin(cmd, workspace, args[0], actorID, role, backend, session, paneID, unbound, create, current)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&role, "role", "", "Optional member role")
	cmd.Flags().StringVar(&backend, "backend", "", "Optional transport backend binding (tmux|zellij)")
	cmd.Flags().StringVar(&session, "session", "", "Optional transport session binding")
	cmd.Flags().StringVar(&paneID, "pane-id", "", "Optional transport pane binding")
	cmd.Flags().BoolVar(&unbound, "unbound", false, "Mark the member transport as unbound/misaligned")
	cmd.Flags().BoolVar(&create, "create", true, "Create the room if it does not exist")
	cmd.Flags().BoolVar(&current, "current", false, "Join the current tmux/zellij participant when actor-id is omitted")
	return cmd
}

func newRoomLeaveCommand() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "leave <room-id> <actor-id>",
		Short: "Remove a room member",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomLeave(cmd, workspace, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	return cmd
}

func newRoomSubscribeCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
		limit     int
		follow    bool
		poll      time.Duration
		history   int
	)
	cmd := &cobra.Command{
		Use:   "subscribe <room-id>",
		Short: "Read a room timeline once or follow it as a stream",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomSubscribe(cmd, workspace, args[0], actorID, limit, follow, poll, history)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Actor id used for unread counts")
	cmd.Flags().IntVar(&limit, "limit", 100, "Maximum messages to read in non-follow mode")
	cmd.Flags().BoolVar(&follow, "follow", false, "Stream new room messages as progress envelopes")
	cmd.Flags().DurationVar(&poll, "poll", 2*time.Second, "Polling interval for follow mode")
	cmd.Flags().IntVar(&history, "history", 20, "Messages to emit immediately before follow mode starts")
	return cmd
}

func newRoomRelayCommand() *cobra.Command {
	var (
		workspace string
		backend   string
		session   string
		plugin    string
		poll      time.Duration
		history   int
	)
	cmd := &cobra.Command{
		Use:   "relay <room-id>",
		Short: "Fan out room messages into live terminal panes for room members",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomRelay(cmd, workspace, args[0], roomRelayOptions{
				Backend:          backend,
				ZellijSession:    session,
				ZellijPluginPath: plugin,
			}, poll, history)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&backend, "backend", "auto", "Terminal backend (auto|tmux|zellij)")
	cmd.Flags().StringVar(&session, "session", "", "Zellij session name (defaults to ZELLIJ_SESSION_NAME when inside zellij)")
	cmd.Flags().StringVar(&plugin, "plugin-path", "", "Path to the zellij room relay plugin wasm")
	cmd.Flags().DurationVar(&poll, "poll", 2*time.Second, "Polling interval")
	cmd.Flags().IntVar(&history, "history", 0, "Number of most recent messages to replay into members before following")
	return cmd
}

type roomCreateProvisionOptions struct {
	Enabled     bool
	MuxBackend  string
	MuxSession  string
	PaneCommand string
	AgentCLI    string
	AgentMode   string
	AgentArgs   []string
	MemberArgs  []string
	Attach      bool
}

type roomProvisionMemberSpec struct {
	Member    agent.RoomMember
	AgentCLI  string
	AgentMode string
}

func runRoomCreate(cmd *cobra.Command, workspace, roomID, title, description string, rawMembers []string) error {
	return runRoomCreateWithProvision(cmd, workspace, roomID, title, description, rawMembers, roomCreateProvisionOptions{})
}

func runRoomCreateWithProvision(cmd *cobra.Command, workspace, roomID, title, description string, rawMembers []string, provision roomCreateProvisionOptions) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
			"hint": "Verify the storage root and workspace path.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	memberSpecs, err := parseRoomMemberSpecs(rawMembers)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.create", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Members must use actor, actor=role, actor@agent, actor=role@agent, or actor=role@agent:mode form.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	members := roomMembersFromSpecs(memberSpecs)
	if identity, err := resolveRoomSender(cmd.Context(), ""); err == nil {
		members = ensureRoomCoordinator(members, identity.Sender)
	}
	memberSpecs = mergeProvisionSpecsWithMembers(memberSpecs, members)

	room, err := store.UpsertRoom(cmd.Context(), agent.Room{
		ID:          strings.TrimSpace(roomID),
		WorkspaceID: absWorkspace,
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
		Members:     members,
	})
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
			"hint": "Provide a room id and ensure the board store is writable.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	var provisioned map[string]any
	if provision.Enabled {
		provisioned, err = provisionRoomMembers(cmd.Context(), absWorkspace, room, memberSpecs, provision)
		if err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
				"hint": "Ensure the mux backend is available, the target session exists or can be created, and each explicit --member has a distinct actor id.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		room, err = store.UpsertRoom(cmd.Context(), room)
		if err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.create", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
				"hint":        "Room was provisioned, but persisting updated pane bindings failed.",
				"provisioned": provisioned,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.create", map[string]any{
		"room":        room,
		"provisioned": provisioned,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func ensureRoomCoordinator(existing []agent.RoomMember, actorID string) []agent.RoomMember {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return existing
	}
	out := make([]agent.RoomMember, len(existing))
	copy(out, existing)
	for i := range out {
		out[i] = normalizeRoomMember(out[i])
		if strings.TrimSpace(out[i].ActorID) != actorID {
			continue
		}
		if strings.TrimSpace(out[i].Role) == "" {
			out[i].Role = "coordinator"
		}
		return out
	}
	return append(out, normalizeRoomMember(agent.RoomMember{ActorID: actorID, Role: "coordinator"}))
}

func runRoomList(cmd *cobra.Command, workspace, actorID string, limit int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	rooms, err := store.ListRooms(cmd.Context(), absWorkspace, strings.TrimSpace(actorID), limit, false)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.list", map[string]any{
		"rooms": rooms,
		"count": len(rooms),
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomShow(cmd *cobra.Command, workspace, roomID, actorID string, limit int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.show", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, actorID, limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.show", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.show", map[string]any{
		"room":     summary,
		"messages": messages,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

type roomStatusParticipant struct {
	ActorID              string           `json:"actor_id"`
	Role                 string           `json:"role,omitempty"`
	LastActiveAt         *time.Time       `json:"last_active_at,omitempty"`
	Status               string           `json:"status"`
	AssignedTaskCount    int              `json:"assigned_task_count"`
	OwnedTaskCount       int              `json:"owned_task_count"`
	ActionableInboxCount int              `json:"actionable_inbox_count"`
	LatestActionable     *roomStatusEntry `json:"latest_actionable,omitempty"`
}

type roomTaskPulseSummary struct {
	Pending           int `json:"pending"`
	AssignedUnclaimed int `json:"assigned_unclaimed"`
	InProgress        int `json:"in_progress"`
	Blocked           int `json:"blocked"`
	Stale             int `json:"stale"`
	Completed         int `json:"completed"`
}

type roomStatusBacklog struct {
	ParticipantsWithPending int               `json:"participants_with_pending"`
	PendingAcks             int               `json:"pending_acks"`
	PendingReplies          int               `json:"pending_replies"`
	LatestByParticipant     []roomStatusEntry `json:"latest_by_participant,omitempty"`
}

type roomStatusEntry struct {
	ID        string                   `json:"id"`
	Sender    string                   `json:"sender"`
	Recipient string                   `json:"recipient"`
	Subject   string                   `json:"subject"`
	Priority  int                      `json:"priority"`
	Status    agent.BoardMessageStatus `json:"status"`
	CreatedAt time.Time                `json:"created_at"`
	Category  string                   `json:"category"`
	Flags     []string                 `json:"flags,omitempty"`
	Preview   string                   `json:"preview,omitempty"`
}

type roomStatusActionRequired struct {
	ParticipantsWithPending int               `json:"participants_with_pending"`
	PendingAcks             int               `json:"pending_acks"`
	PendingReplies          int               `json:"pending_replies"`
	AssignedUnclaimed       int               `json:"assigned_unclaimed"`
	BlockedTasks            int               `json:"blocked_tasks"`
	StaleTasks              int               `json:"stale_tasks"`
	Filter                  []string          `json:"filter,omitempty"`
	TopEntries              []roomStatusEntry `json:"top_entries,omitempty"`
	TopTasks                []roomStatusTask  `json:"top_tasks,omitempty"`
	VerboseTopEntries       []roomInboxEntry  `json:"verbose_top_entries,omitempty"`
}

type roomStatusTask struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Status          string     `json:"status"`
	AssignedActorID string     `json:"assigned_actor_id,omitempty"`
	OwnerActorID    string     `json:"owner_actor_id,omitempty"`
	BlockedReason   string     `json:"blocked_reason,omitempty"`
	HeartbeatAt     *time.Time `json:"heartbeat_at,omitempty"`
	Signals         []string   `json:"signals,omitempty"`
}

func runRoomStatus(cmd *cobra.Command, workspace, roomID string, limit int, staleAfter time.Duration, only []string, verbose bool) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.status", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()
	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.status", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.status", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()
	tasks, err := listRoomTasks(cmd.Context(), taskStore, ws.CanonicalID(absWorkspace), messages, "")
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.status", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	now := time.Now().UTC()
	taskPulse := buildRoomTaskPulseSummary(tasks, now, staleAfter)
	backlog := buildRoomStatusBacklog(summary, messages)
	filters, err := normalizeRoomStatusFilters(only)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.status", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use comma-separated or repeated --only values from: ack, reply, assigned, blocked, stale, all.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.status", map[string]any{
		"room":            summary,
		"participants":    buildRoomStatusParticipants(summary, messages, tasks, staleAfter),
		"task_pulse":      taskPulse,
		"backlog":         backlog,
		"action_required": buildRoomStatusActionRequired(summary, messages, tasks, backlog, taskPulse, filters, staleAfter, now, verbose),
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

type roomInboxEntry struct {
	ID        string                   `json:"id"`
	Sender    string                   `json:"sender"`
	Recipient string                   `json:"recipient"`
	Subject   string                   `json:"subject"`
	Priority  int                      `json:"priority"`
	Status    agent.BoardMessageStatus `json:"status"`
	CreatedAt time.Time                `json:"created_at"`
	Category  string                   `json:"category"`
	Flags     []string                 `json:"flags,omitempty"`
	Preview   string                   `json:"preview,omitempty"`
	Message   agent.BoardMessage       `json:"message"`
}

func runRoomInbox(cmd *cobra.Command, workspace, roomID, actorID string, limit int, filter string, grouped, idsOnly, includeBroadcasts bool) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), actorID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.inbox", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --actor when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.inbox", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, identity.Sender, limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.inbox", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	entries := buildRoomInboxEntries(identity.Sender, messages, strings.TrimSpace(filter), includeBroadcasts)
	if idsOnly {
		ids := make([]string, 0, len(entries))
		for _, entry := range entries {
			ids = append(ids, entry.ID)
		}
		return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.inbox", map[string]any{
			"room":   summary,
			"actor":  identity.Sender,
			"filter": normalizeRoomInboxFilter(filter),
			"ids":    ids,
			"count":  len(ids),
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	data := map[string]any{
		"room":    summary,
		"actor":   identity.Sender,
		"filter":  normalizeRoomInboxFilter(filter),
		"count":   len(entries),
		"entries": entries,
	}
	if grouped {
		data["groups"] = groupRoomInboxEntries(entries)
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.inbox", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomSend(cmd *cobra.Command, workspace, roomID, sender, recipient, subject, body, kind, taskID string, priority int, ackRequired, replyExpected, interrupt, autoCreate bool) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	roomID = strings.TrimSpace(roomID)
	if autoCreate {
		if _, err := store.EnsureRoom(cmd.Context(), absWorkspace, roomID, roomID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
				"hint": "Create the room first or leave --auto-create enabled.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	recipient, err = resolveRoomRecipient(cmd.Context(), store, absWorkspace, roomID, recipient)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use a room participant id, or @coordinator once the room has an assigned coordinator.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if replyExpected && recipient == agent.BroadcastRecipient {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeEARG, "reply_expected requires a direct recipient", map[string]any{
			"hint": "Pass --to <participant-id> for direct requests. Broadcast room messages should not expect a response.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if interrupt && recipient == agent.BroadcastRecipient {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeEARG, "interrupt requires a direct recipient", map[string]any{
			"hint": "Pass --to <participant-id> when you need to interrupt a specific pane before sending the message.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if strings.TrimSpace(subject) == "" {
		subject = deriveRoomSubject(body)
	}

	msg := &agent.BoardMessage{
		WorkspaceID:   absWorkspace,
		TaskID:        strings.TrimSpace(taskID),
		Stream:        agent.RoomStreamName(roomID),
		Sender:        identity.Sender,
		Recipient:     recipient,
		Kind:          agent.BoardMessageKind(strings.TrimSpace(kind)),
		Priority:      priority,
		AckRequired:   ackRequired,
		ReplyExpected: replyExpected,
		Interrupt:     interrupt,
		Subject:       subject,
		Body:          strings.TrimSpace(body),
	}
	if msg.Kind == "" {
		msg.Kind = agent.BoardMessageKindInfo
	}
	if msg.Priority <= 0 {
		msg.Priority = agent.DefaultPriority
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.send", map[string]any{
		"room_id":         roomID,
		"stream":          msg.Stream,
		"message_id":      msg.ID,
		"message":         msg,
		"sender_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomAck(cmd *cobra.Command, workspace, roomID, actorID string, messageIDs []string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), actorID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.ack", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --actor when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.ack", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	roomID = strings.TrimSpace(roomID)
	if _, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, identity.Sender); err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.ack", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	trimmedIDs := make([]string, 0, len(messageIDs))
	for _, id := range messageIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		trimmedIDs = append(trimmedIDs, id)
	}
	if len(trimmedIDs) == 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.ack", protocol.ErrorCodeEARG, "at least one non-empty message id is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	updated, err := store.AckMessages(cmd.Context(), absWorkspace, identity.Sender, trimmedIDs)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.ack", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if updated == 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.ack", protocol.ErrorCodeENotFound, "no room messages were acknowledged", map[string]any{
			"hint": "Check the message ids and ensure they belong to this workspace.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.ack", map[string]any{
		"room_id":        roomID,
		"message_ids":    trimmedIDs,
		"updated":        updated,
		"acker_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomResolve(cmd *cobra.Command, workspace, roomID, actorID, mode string, resolveAll bool, only []string, messageIDs []string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), actorID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.resolve", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --actor when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.resolve", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	roomID = strings.TrimSpace(roomID)
	summary, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, identity.Sender)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.resolve", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.resolve", protocol.ErrorCodeEARG, "room resolve requires coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	trimmedIDs, err := resolveRoomMessageIDsForResolve(cmd.Context(), store, absWorkspace, summary, resolveAll, only, messageIDs)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.resolve", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass one or more message ids, or use --all with --only ack,reply,direct to bulk-resolve current room entries.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	trimmedIDs, err = expandRoomResolveMessageIDs(cmd.Context(), store, absWorkspace, roomID, trimmedIDs)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.resolve", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	var (
		updated        int
		resolvedStatus agent.BoardMessageStatus
	)
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "", "acked", "ack":
		updated, err = store.AckMessages(cmd.Context(), absWorkspace, identity.Sender, trimmedIDs)
		resolvedStatus = agent.BoardMessageStatusAcked
	case "read":
		updated, err = store.MarkRead(cmd.Context(), absWorkspace, identity.Sender, trimmedIDs)
		resolvedStatus = agent.BoardMessageStatusRead
	default:
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.resolve", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported resolve mode %q", mode), map[string]any{
			"hint": "Use --mode acked or --mode read.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.resolve", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if updated == 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.resolve", protocol.ErrorCodeENotFound, "no room messages were resolved", map[string]any{
			"hint": "Check the message ids and ensure they belong to this workspace.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.resolve", map[string]any{
		"room_id":           roomID,
		"message_ids":       trimmedIDs,
		"updated":           updated,
		"resolved_status":   resolvedStatus,
		"resolver_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomClear(cmd *cobra.Command, workspace, roomID, actorID, mode, preset string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), actorID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.clear", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --actor when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.clear", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	roomID = strings.TrimSpace(roomID)
	summary, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, identity.Sender)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.clear", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.clear", protocol.ErrorCodeEARG, "room clear requires coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	messageIDs, err := resolveRoomClearPresetMessageIDs(cmd.Context(), store, absWorkspace, summary, identity.Sender, preset)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.clear", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use --preset coordinator-pulses or --preset system-reminders.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if len(messageIDs) == 0 {
		return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.clear", map[string]any{
			"room_id":           roomID,
			"preset":            strings.TrimSpace(strings.ToLower(preset)),
			"message_ids":       []string{},
			"updated":           0,
			"resolved_status":   strings.TrimSpace(strings.ToLower(mode)),
			"resolver_identity": identity,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	expandedIDs, err := expandRoomResolveMessageIDs(cmd.Context(), store, absWorkspace, roomID, messageIDs)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.clear", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	var (
		updated        int
		resolvedStatus agent.BoardMessageStatus
	)
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "", "read":
		updated, err = store.MarkRead(cmd.Context(), absWorkspace, identity.Sender, expandedIDs)
		resolvedStatus = agent.BoardMessageStatusRead
	case "acked", "ack":
		updated, err = store.AckMessages(cmd.Context(), absWorkspace, identity.Sender, expandedIDs)
		resolvedStatus = agent.BoardMessageStatusAcked
	default:
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.clear", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported clear mode %q", mode), map[string]any{
			"hint": "Use --mode read or --mode acked.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.clear", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.clear", map[string]any{
		"room_id":           roomID,
		"preset":            strings.TrimSpace(strings.ToLower(preset)),
		"message_ids":       expandedIDs,
		"updated":           updated,
		"resolved_status":   resolvedStatus,
		"resolver_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomPlanStart(cmd *cobra.Command, workspace, sender, roomID, topic, goal, artifact string, scope, constraints []string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomPlanCommand(cmd, workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()

	topic = strings.TrimSpace(topic)
	if topic == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan.start", protocol.ErrorCodeEARG, "topic is required", map[string]any{
			"hint": "Pass a concise planning topic such as `phase-3-ui-polish` or `api-rate-limit-refactor`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	body := buildRoomPlanSessionBody(topic, goal, artifact, scope, constraints)
	msg := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		Stream:      agent.RoomStreamName(roomID),
		Sender:      identity.Sender,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindPlanSession,
		Priority:    agent.DefaultPriority,
		Subject:     "Plan Session: " + topic,
		Body:        body,
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan.start", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.plan.start", map[string]any{
		"room_id":         roomID,
		"session_id":      msg.ID,
		"topic":           topic,
		"message":         msg,
		"sender_identity": identity,
		"room":            summary,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomPlanEntry(cmd *cobra.Command, workspace, roomID, sender, sessionID string, kind agent.BoardMessageKind, subject, body string, requireCoordinator bool) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomPlanCommand(cmd, workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()

	if requireCoordinator && !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan", protocol.ErrorCodeEARG, "room plan phase changes require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	sessionMsg, err := loadRoomPlanSession(cmd.Context(), store, absWorkspace, roomID, sessionID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Start a session with `agentctl room plan start` and reuse its session_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: sessionMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             kind,
		Priority:         agent.DefaultPriority,
		Subject:          strings.TrimSpace(subject),
		Body:             strings.TrimSpace(body),
	}
	if msg.Subject == "" {
		msg.Subject = deriveRoomSubject(msg.Body)
	}
	if msg.Body == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan", protocol.ErrorCodeEARG, "body is required", map[string]any{
			"hint": "Include the actual proposal, question, decision, review note, or closure summary.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.plan", map[string]any{
		"room_id":         roomID,
		"session_id":      sessionMsg.ID,
		"message":         msg,
		"sender_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomPlanShow(cmd *cobra.Command, workspace, roomID, sessionID string, limit int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan.show", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan.show", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	sessions := buildRoomPlanSessions(messages)
	if strings.TrimSpace(sessionID) == "" {
		return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.plan.show", map[string]any{
			"room":     summary,
			"count":    len(sessions),
			"sessions": sessions,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	for _, session := range sessions {
		if session["id"] == strings.TrimSpace(sessionID) {
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.plan.show", map[string]any{
				"room":    summary,
				"session": session,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}

	return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan.show", protocol.ErrorCodeENotFound, fmt.Sprintf("planning session %q not found", sessionID), map[string]any{
		"hint": "Run `agentctl room plan show <room-id>` to list available planning sessions.",
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func prepareRoomPlanCommand(cmd *cobra.Command, workspace, sender, roomID string) (string, roomIdentity, blackboard.BoardStore, string, agent.RoomSummary, error) {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	roomID = strings.TrimSpace(roomID)
	summary, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, identity.Sender)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		store.Close()
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.plan", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return absWorkspace, identity, store, roomID, summary, nil
}

func loadRoomPlanSession(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, sessionID string) (agent.BoardMessage, error) {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return agent.BoardMessage{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	for _, msg := range messages {
		if msg.ID == sessionID && msg.Kind == agent.BoardMessageKindPlanSession {
			return msg, nil
		}
	}
	return agent.BoardMessage{}, fmt.Errorf("planning session %q not found", sessionID)
}

func buildRoomPlanSessionBody(topic, goal, artifact string, scope, constraints []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %s\n", strings.TrimSpace(topic))
	if strings.TrimSpace(goal) != "" {
		fmt.Fprintf(&b, "Goal: %s\n", strings.TrimSpace(goal))
	}
	if strings.TrimSpace(artifact) != "" {
		fmt.Fprintf(&b, "Artifact: %s\n", strings.TrimSpace(artifact))
	}
	if len(scope) > 0 {
		b.WriteString("Scope:\n")
		for _, item := range scope {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	if len(constraints) > 0 {
		b.WriteString("Constraints:\n")
		for _, item := range constraints {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			fmt.Fprintf(&b, "- %s\n", item)
		}
	}
	b.WriteString("Protocol:\n- submit proposals\n- record open questions\n- coordinator records decisions\n- reviewers approve or block\n- close with a final summary\n")
	return strings.TrimSpace(b.String())
}

func buildRoomPlanSessions(messages []agent.BoardMessage) []map[string]any {
	type sessionState struct {
		Root      agent.BoardMessage
		Entries   []agent.BoardMessage
		Decisions int
		Questions int
		Proposals int
		Reviews   int
		Status    string
	}
	sessions := make(map[string]*sessionState)
	for _, msg := range messages {
		if msg.Kind == agent.BoardMessageKindPlanSession {
			sessions[msg.ID] = &sessionState{Root: msg, Status: "drafting"}
		}
	}
	for _, msg := range messages {
		related := strings.TrimSpace(msg.RelatedMessageID)
		if related == "" {
			continue
		}
		session := sessions[related]
		if session == nil {
			continue
		}
		session.Entries = append(session.Entries, msg)
		switch msg.Kind {
		case agent.BoardMessageKindPlanProposal:
			session.Proposals++
		case agent.BoardMessageKindPlanQuestion:
			session.Questions++
		case agent.BoardMessageKindPlanDecision:
			session.Decisions++
		case agent.BoardMessageKindPlanReview:
			session.Reviews++
			subject := strings.ToLower(strings.TrimSpace(msg.Subject))
			if strings.Contains(subject, "block") {
				session.Status = "blocked"
			} else if session.Status != "blocked" {
				session.Status = "in_review"
			}
		case agent.BoardMessageKindPlanClose:
			session.Status = "closed"
		}
	}

	ordered := make([]*sessionState, 0, len(sessions))
	for _, session := range sessions {
		ordered = append(ordered, session)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].Root.CreatedAt.After(ordered[j].Root.CreatedAt)
	})

	out := make([]map[string]any, 0, len(ordered))
	for _, session := range ordered {
		out = append(out, map[string]any{
			"id":          session.Root.ID,
			"topic":       strings.TrimPrefix(strings.TrimSpace(session.Root.Subject), "Plan Session: "),
			"status":      session.Status,
			"root":        session.Root,
			"entries":     session.Entries,
			"proposals":   session.Proposals,
			"questions":   session.Questions,
			"decisions":   session.Decisions,
			"reviews":     session.Reviews,
			"closed":      session.Status == "closed",
			"entry_count": len(session.Entries),
		})
	}
	return out
}

func runRoomCoordinatorSet(cmd *cobra.Command, workspace, roomID, actorID, targetID string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), actorID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.coordinator.set", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --actor when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	roomID = strings.TrimSpace(roomID)
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.coordinator.set", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()
	summary, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, identity.Sender)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.coordinator.set", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	currentCoordinator := roomCoordinatorActorID(summary.Members)
	if currentCoordinator != "" && !sameRoomParticipant(currentCoordinator, identity.Sender) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.coordinator.set", protocol.ErrorCodeEARG, "only the current coordinator can reassign coordinator role", map[string]any{
			"hint": "Run the command as the existing room coordinator.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	targetID = strings.TrimSpace(targetID)
	if !roomHasParticipant(summary, targetID) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.coordinator.set", protocol.ErrorCodeENotFound, "target participant is not a room member", map[string]any{
			"hint": "Join the target participant to the room before assigning coordinator role.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	updatedMembers := make([]agent.RoomMember, 0, len(summary.Members))
	foundTarget := false
	for _, member := range summary.Members {
		if sameRoomParticipant(member.ActorID, targetID) {
			member.Role = "coordinator"
			foundTarget = true
		} else if strings.EqualFold(strings.TrimSpace(member.Role), "coordinator") {
			member.Role = ""
		}
		updatedMembers = append(updatedMembers, member)
	}
	if !foundTarget {
		updatedMembers = append(updatedMembers, agent.RoomMember{ActorID: targetID, Role: "coordinator"})
	}
	if _, err := store.ReplaceRoomMembers(cmd.Context(), absWorkspace, roomID, updatedMembers); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.coordinator.set", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	updated, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, "")
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.coordinator.set", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.coordinator.set", map[string]any{
		"room":                 updated,
		"previous_coordinator": currentCoordinator,
		"coordinator":          targetID,
		"actor":                identity.Sender,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomJoin(cmd *cobra.Command, workspace, roomID, actorID, role, backend, session, paneID string, unbound, create, current bool) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	member := agent.RoomMember{
		ActorID: strings.TrimSpace(actorID),
		Role:    strings.TrimSpace(role),
	}
	if current || member.ActorID == "" {
		identity, resolveErr := resolveRoomSender(cmd.Context(), actorID)
		if resolveErr != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.join", protocol.ErrorCodeEARG, resolveErr.Error(), map[string]any{
				"hint": "Pass an explicit actor id, or run inside tmux/zellij with --current so agentctl can derive the participant id.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if member.ActorID == "" {
			member.ActorID = identity.Sender
		}
		if current {
			member.Backend = identity.Backend
			member.Session = identity.Session
			member.PaneID = identity.PaneID
		}
	}
	if value := strings.TrimSpace(backend); value != "" {
		member.Backend = strings.ToLower(value)
	}
	if value := strings.TrimSpace(session); value != "" {
		member.Session = value
	}
	if value := strings.TrimSpace(paneID); value != "" {
		member.PaneID = value
	}
	member.Unbound = unbound
	member = normalizeRoomMember(member)
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.join", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	roomID = strings.TrimSpace(roomID)
	if create {
		if _, err := store.EnsureRoom(cmd.Context(), absWorkspace, roomID, roomID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.join", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	summary, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, "")
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.join", code, err.Error(), map[string]any{
			"hint": "Create the room first or use --create.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	updatedMembers := mergeRoomMembers(summary.Members, member)
	if _, err := store.ReplaceRoomMembers(cmd.Context(), absWorkspace, roomID, updatedMembers); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.join", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	updated, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, "")
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.join", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.join", map[string]any{
		"room": updated,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomLeave(cmd *cobra.Command, workspace, roomID, actorID string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.leave", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, err := store.GetRoom(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), "")
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.leave", code, err.Error(), map[string]any{
			"hint": "Check the room id before removing a member.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	updatedMembers := removeRoomMember(summary.Members, strings.TrimSpace(actorID))
	if _, err := store.ReplaceRoomMembers(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), updatedMembers); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.leave", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	updated, err := store.GetRoom(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), "")
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.leave", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.leave", map[string]any{
		"room": updated,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomSubscribe(cmd *cobra.Command, workspace, roomID, actorID string, limit int, follow bool, poll time.Duration, history int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.subscribe", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, actorID, limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.subscribe", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	if !follow {
		return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.subscribe", map[string]any{
			"room":     summary,
			"messages": messages,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	writer := envelope.NewWriter(cmd.OutOrStdout())
	seen := make(map[string]struct{}, len(messages))
	seq := 0
	initial := trimRoomHistory(messages, history)
	for _, msg := range initial {
		seq++
		seen[msg.ID] = struct{}{}
		if err := writer.Write(roomProgressEnvelope("agentctl.room.subscribe", seq, false, map[string]any{
			"event":   "room_message",
			"room_id": roomID,
			"message": msg,
		}, absWorkspace)); err != nil {
			return fmt.Errorf("write room subscribe progress envelope: %w", err)
		}
	}
	for _, msg := range messages {
		seen[msg.ID] = struct{}{}
	}

	ticker := time.NewTicker(normalizeRoomPoll(poll))
	defer ticker.Stop()

	for {
		select {
		case <-cmd.Context().Done():
			return writer.Write(protocol.OK("agentctl.room.subscribe", map[string]any{
				"status":  "stopped",
				"room_id": roomID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace), protocol.WithMetaMutator(func(m *envelope.Meta) {
				final := true
				m.Seq = &seq
				m.Final = &final
			})))
		case <-ticker.C:
			_, current, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, actorID, roomMaxInt(limit, 200))
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.subscribe", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			for _, msg := range current {
				if _, ok := seen[msg.ID]; ok {
					continue
				}
				seen[msg.ID] = struct{}{}
				seq++
				if err := writer.Write(roomProgressEnvelope("agentctl.room.subscribe", seq, false, map[string]any{
					"event":   "room_message",
					"room_id": roomID,
					"message": msg,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room subscribe progress envelope: %w", err)
				}
			}
		}
	}
}

func runRoomRelay(cmd *cobra.Command, workspace, roomID string, relay roomRelayOptions, poll time.Duration, history int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	client := tmuxbridge.New()
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.relay", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	writer := envelope.NewWriter(cmd.OutOrStdout())
	seq := 0

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", roomMaxInt(history, 200))
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.relay", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	seen := make(map[string]struct{}, len(messages))
	for _, msg := range messages {
		seen[msg.ID] = struct{}{}
	}
	initial := trimRoomHistory(messages, history)
	for _, msg := range initial {
		seq++
		result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
		if err := writer.Write(roomProgressEnvelope("agentctl.room.relay", seq, false, map[string]any{
			"event":   "room_relay",
			"room_id": roomID,
			"message": msg,
			"relay":   result,
		}, absWorkspace)); err != nil {
			return fmt.Errorf("write room relay progress envelope: %w", err)
		}
	}

	ticker := time.NewTicker(normalizeRoomPoll(poll))
	defer ticker.Stop()
	for {
		select {
		case <-cmd.Context().Done():
			return writer.Write(protocol.OK("agentctl.room.relay", map[string]any{
				"status":  "stopped",
				"room_id": roomID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace), protocol.WithMetaMutator(func(m *envelope.Meta) {
				final := true
				m.Seq = &seq
				m.Final = &final
			})))
		case <-ticker.C:
			summary, current, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", roomMaxInt(history, 200))
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.relay", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			for _, msg := range current {
				if _, ok := seen[msg.ID]; ok {
					continue
				}
				seen[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := writer.Write(roomProgressEnvelope("agentctl.room.relay", seq, false, map[string]any{
					"event":   "room_relay",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room relay progress envelope: %w", err)
				}
			}
		}
	}
}

func resolveRoomWorkspace(workspace string) (string, error) {
	if strings.TrimSpace(workspace) == "" {
		workspace = "."
	}
	return filepath.Abs(workspace)
}

func openRoomBoardStore(ctx context.Context) (blackboard.BoardStore, error) {
	cfg, err := loadConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
}

func parseRoomMembers(values []string) ([]agent.RoomMember, error) {
	specs, err := parseRoomMemberSpecs(values)
	if err != nil {
		return nil, err
	}
	return roomMembersFromSpecs(specs), nil
}

func parseRoomMemberSpecs(values []string) ([]roomProvisionMemberSpec, error) {
	out := make([]roomProvisionMemberSpec, 0, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		spec := roomProvisionMemberSpec{}
		body := raw
		if idx := strings.LastIndex(body, "@"); idx >= 0 {
			agentSpec := strings.TrimSpace(body[idx+1:])
			body = strings.TrimSpace(body[:idx])
			if agentSpec == "" {
				return nil, fmt.Errorf("member agent is required after @ in %q", raw)
			}
			if modeIdx := strings.Index(agentSpec, ":"); modeIdx >= 0 {
				spec.AgentCLI = strings.TrimSpace(agentSpec[:modeIdx])
				spec.AgentMode = strings.TrimSpace(agentSpec[modeIdx+1:])
				if spec.AgentMode == "" {
					return nil, fmt.Errorf("member mode is required after : in %q", raw)
				}
			} else {
				spec.AgentCLI = agentSpec
			}
			if spec.AgentCLI == "" {
				return nil, fmt.Errorf("member agent is required after @ in %q", raw)
			}
		}
		if idx := strings.LastIndex(body, "="); idx >= 0 {
			spec.Member.ActorID = strings.TrimSpace(body[:idx])
			spec.Member.Role = strings.TrimSpace(body[idx+1:])
		} else {
			spec.Member.ActorID = strings.TrimSpace(body)
		}
		if spec.Member.ActorID == "" {
			return nil, fmt.Errorf("member actor id is required")
		}
		spec.Member = normalizeRoomMember(spec.Member)
		out = append(out, spec)
	}
	return out, nil
}

func parseRoomMemberArgMap(values []string) (map[string][]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make(map[string][]string, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		idx := strings.Index(raw, "=")
		if idx <= 0 || idx == len(raw)-1 {
			return nil, fmt.Errorf("member arg must use actor=arg form")
		}
		actorID := strings.TrimSpace(raw[:idx])
		arg := strings.TrimSpace(raw[idx+1:])
		if actorID == "" || arg == "" {
			return nil, fmt.Errorf("member arg must use actor=arg form")
		}
		out[actorID] = append(out[actorID], arg)
	}
	return out, nil
}

func roomMembersFromSpecs(specs []roomProvisionMemberSpec) []agent.RoomMember {
	out := make([]agent.RoomMember, 0, len(specs))
	for _, spec := range specs {
		if spec.Member.ActorID == "" {
			continue
		}
		out = append(out, normalizeRoomMember(spec.Member))
	}
	return out
}

func mergeProvisionSpecsWithMembers(specs []roomProvisionMemberSpec, members []agent.RoomMember) []roomProvisionMemberSpec {
	if len(specs) == 0 {
		return nil
	}
	index := make(map[string]agent.RoomMember, len(members))
	for _, member := range members {
		member = normalizeRoomMember(member)
		if member.ActorID != "" {
			index[member.ActorID] = member
		}
	}
	merged := make([]roomProvisionMemberSpec, 0, len(specs))
	for _, spec := range specs {
		spec.Member = normalizeRoomMember(spec.Member)
		if updated, ok := index[spec.Member.ActorID]; ok {
			spec.Member = updated
		}
		merged = append(merged, spec)
	}
	return merged
}

func mergeRoomMembers(existing []agent.RoomMember, additions ...agent.RoomMember) []agent.RoomMember {
	out := make([]agent.RoomMember, 0, len(existing)+len(additions))
	index := make(map[string]int, len(existing)+len(additions))
	for _, member := range existing {
		member = normalizeRoomMember(member)
		if member.ActorID == "" {
			continue
		}
		index[member.ActorID] = len(out)
		out = append(out, member)
	}
	for _, member := range additions {
		member = normalizeRoomMember(member)
		if member.ActorID == "" {
			continue
		}
		if pos, ok := index[member.ActorID]; ok {
			if member.Role != "" {
				out[pos].Role = member.Role
			}
			if member.Backend != "" {
				out[pos].Backend = member.Backend
			}
			if member.Session != "" {
				out[pos].Session = member.Session
			}
			if member.PaneID != "" {
				out[pos].PaneID = member.PaneID
			}
			out[pos].Unbound = member.Unbound
			continue
		}
		index[member.ActorID] = len(out)
		out = append(out, member)
	}
	return out
}

func normalizeRoomMember(member agent.RoomMember) agent.RoomMember {
	member.ActorID = strings.TrimSpace(member.ActorID)
	member.Role = strings.TrimSpace(member.Role)
	member.Backend = strings.ToLower(strings.TrimSpace(member.Backend))
	member.Session = strings.TrimSpace(member.Session)
	member.PaneID = strings.TrimSpace(member.PaneID)
	return member
}

func provisionRoomMembers(ctx context.Context, workspace string, room agent.Room, specs []roomProvisionMemberSpec, opts roomCreateProvisionOptions) (map[string]any, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("room provisioning requires at least one explicit --member")
	}
	backend := resolveMuxCreateBackend(opts.MuxBackend)
	if backend == "" {
		return nil, fmt.Errorf("unsupported mux backend %q", opts.MuxBackend)
	}
	session := resolveMuxCreateSession(nil, backend, opts.MuxSession)
	memberArgMap, err := parseRoomMemberArgMap(opts.MemberArgs)
	if err != nil {
		return nil, err
	}
	baseCommand, err := resolveMuxCreateCommand(strings.TrimSpace(opts.PaneCommand), strings.TrimSpace(opts.AgentCLI), strings.TrimSpace(opts.AgentMode), append([]string(nil), opts.AgentArgs...), "")
	if err != nil {
		return nil, err
	}
	provisioned := make([]map[string]any, 0, len(specs))
	updatedMembers := make([]agent.RoomMember, 0, len(specs))
	for _, spec := range specs {
		member := normalizeRoomMember(spec.Member)
		if member.ActorID == "" {
			continue
		}
		memberAgent := strings.TrimSpace(spec.AgentCLI)
		memberMode := firstNonEmpty(strings.TrimSpace(spec.AgentMode), strings.TrimSpace(opts.AgentMode))
		command := baseCommand
		memberArgs := append([]string(nil), opts.AgentArgs...)
		if extra := memberArgMap[member.ActorID]; len(extra) > 0 {
			memberArgs = append(memberArgs, extra...)
		}
		if memberAgent != "" || strings.TrimSpace(spec.AgentMode) != "" || len(memberArgs) != len(opts.AgentArgs) {
			resolvedAgent := firstNonEmpty(memberAgent, strings.TrimSpace(opts.AgentCLI))
			command, err = resolveMuxCreateCommand("", resolvedAgent, memberMode, memberArgs, "")
			if err != nil {
				return nil, fmt.Errorf("member %s: %w", member.ActorID, err)
			}
		}
		switch backend {
		case "tmux":
			client := tmuxbridge.New()
			result, createErr := client.CreatePane(ctx, tmuxbridge.CreatePaneOptions{
				Session:       session,
				CWD:           workspace,
				Label:         member.ActorID,
				Command:       command,
				ParticipantID: member.ActorID,
				RoomID:        room.ID,
				RoomRole:      member.Role,
				RoomAccess:    "direct",
			})
			if createErr != nil {
				return nil, createErr
			}
			member.Backend = "tmux"
			member.Session = result.Session
			member.PaneID = result.Pane.ID
			updatedMembers = append(updatedMembers, member)
			provisioned = append(provisioned, map[string]any{
				"actor_id":       member.ActorID,
				"role":           member.Role,
				"agent":          firstNonEmpty(memberAgent, strings.TrimSpace(opts.AgentCLI)),
				"mode":           memberMode,
				"agent_args":     memberArgs,
				"backend":        "tmux",
				"session":        result.Session,
				"pane_id":        result.Pane.ID,
				"attach_command": result.AttachCommand,
			})
		case "zellij":
			client := zellijbridge.New()
			result, createErr := client.CreatePane(ctx, zellijbridge.CreatePaneOptions{
				Session:       session,
				CWD:           workspace,
				Name:          member.ActorID,
				Command:       command,
				ParticipantID: member.ActorID,
				RoomID:        room.ID,
				RoomRole:      member.Role,
				RoomAccess:    "direct",
			})
			if createErr != nil {
				return nil, createErr
			}
			member.Backend = "zellij"
			member.Session = result.Session
			member.PaneID = result.PaneName
			updatedMembers = append(updatedMembers, member)
			provisioned = append(provisioned, map[string]any{
				"actor_id":       member.ActorID,
				"role":           member.Role,
				"agent":          firstNonEmpty(memberAgent, strings.TrimSpace(opts.AgentCLI)),
				"mode":           memberMode,
				"agent_args":     memberArgs,
				"backend":        "zellij",
				"session":        result.Session,
				"pane_id":        result.PaneName,
				"attach_command": "zellij attach " + shellQuoteZshSafe(result.Session),
			})
		default:
			return nil, fmt.Errorf("unsupported mux backend %q", backend)
		}
	}
	room.Members = mergeRoomMembers(room.Members, updatedMembers...)
	if opts.Attach {
		switch backend {
		case "tmux":
			if err := tmuxbridge.New().AttachOrSwitch(ctx, session); err != nil {
				return nil, err
			}
		case "zellij":
			if strings.TrimSpace(os.Getenv("ZELLIJ_SESSION_NAME")) == "" {
				attachCmd := exec.CommandContext(ctx, "zellij", "attach", session)
				attachCmd.Stdin = os.Stdin
				attachCmd.Stdout = os.Stdout
				attachCmd.Stderr = os.Stdout
				if err := attachCmd.Run(); err != nil {
					return nil, err
				}
			}
		}
	}
	return map[string]any{
		"backend":        backend,
		"session":        session,
		"pane_command":   baseCommand,
		"attach_command": provisionAttachCommand(backend, session),
		"panes":          provisioned,
	}, nil
}

func provisionAttachCommand(backend, session string) string {
	switch strings.TrimSpace(backend) {
	case "tmux":
		return "tmux attach-session -t " + shellQuoteZshSafe(session)
	case "zellij":
		return "zellij attach " + shellQuoteZshSafe(session)
	default:
		return ""
	}
}

func removeRoomMember(existing []agent.RoomMember, actorID string) []agent.RoomMember {
	actorID = strings.TrimSpace(actorID)
	out := make([]agent.RoomMember, 0, len(existing))
	for _, member := range existing {
		if strings.TrimSpace(member.ActorID) == actorID {
			continue
		}
		out = append(out, member)
	}
	return out
}

func roomMemberHasRole(members []agent.RoomMember, actorID, role string) bool {
	actorID = strings.TrimSpace(actorID)
	role = strings.TrimSpace(role)
	if actorID == "" || role == "" {
		return false
	}
	for _, member := range members {
		if sameRoomParticipant(member.ActorID, actorID) && strings.EqualFold(strings.TrimSpace(member.Role), role) {
			return true
		}
	}
	return false
}

func roomHasParticipant(room agent.RoomSummary, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	for _, participant := range room.Participants {
		if sameRoomParticipant(participant, actorID) {
			return true
		}
	}
	for _, member := range room.Members {
		if sameRoomParticipant(member.ActorID, actorID) {
			return true
		}
	}
	return false
}

func roomCoordinatorActorID(members []agent.RoomMember) string {
	for _, member := range members {
		if strings.EqualFold(strings.TrimSpace(member.Role), "coordinator") {
			return strings.TrimSpace(member.ActorID)
		}
	}
	return ""
}

func resolveRoomRecipient(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, recipient string) (string, error) {
	normalized := normalizeRoomRecipient(recipient)
	switch normalized {
	case agent.BroadcastRecipient:
		return normalized, nil
	case "@coordinator", "coordinator":
		summary, err := store.GetRoom(ctx, workspaceID, roomID, "")
		if err != nil {
			return "", err
		}
		coordinator := roomCoordinatorActorID(summary.Members)
		if coordinator == "" {
			return "", fmt.Errorf("room has no assigned coordinator")
		}
		return coordinator, nil
	default:
		return normalized, nil
	}
}

func resolveRoomMessageIDsForResolve(ctx context.Context, store blackboard.BoardStore, workspaceID string, summary agent.RoomSummary, resolveAll bool, only []string, messageIDs []string) ([]string, error) {
	trimmedIDs := make([]string, 0, len(messageIDs))
	for _, id := range messageIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		trimmedIDs = append(trimmedIDs, id)
	}
	if resolveAll {
		messages, err := store.ListRoomMessages(ctx, workspaceID, summary.ID, roomTaskScanLimit)
		if err != nil {
			return nil, err
		}
		filters, err := normalizeRoomResolveFilters(only)
		if err != nil {
			return nil, err
		}
		for _, participant := range summary.Participants {
			if strings.HasPrefix(strings.TrimSpace(participant), "actor:system:room:") {
				continue
			}
			for _, entry := range buildRoomStatusEntries(participant, messages) {
				if roomResolveEntryMatches(entry, filters) {
					trimmedIDs = append(trimmedIDs, entry.ID)
				}
			}
		}
	}
	if len(trimmedIDs) == 0 {
		return nil, fmt.Errorf("at least one matching room message is required")
	}
	return trimmedIDs, nil
}

func resolveRoomClearPresetMessageIDs(ctx context.Context, store blackboard.BoardStore, workspaceID string, summary agent.RoomSummary, actorID, preset string) ([]string, error) {
	messages, err := store.ListRoomMessages(ctx, workspaceID, summary.ID, roomTaskScanLimit)
	if err != nil {
		return nil, err
	}

	switch strings.TrimSpace(strings.ToLower(preset)) {
	case "", "coordinator-pulses":
		ids := make([]string, 0)
		for _, msg := range messages {
			if roomMessageMatchesCoordinatorPulse(msg, summary.ID, actorID) {
				ids = append(ids, msg.ID)
			}
		}
		return ids, nil
	case "system-reminders":
		ids := make([]string, 0)
		for _, msg := range messages {
			if roomMessageMatchesSystemReminder(msg, summary.ID) {
				ids = append(ids, msg.ID)
			}
		}
		return ids, nil
	default:
		return nil, fmt.Errorf("unsupported room clear preset %q", preset)
	}
}

func roomMessageMatchesCoordinatorPulse(msg agent.BoardMessage, roomID, actorID string) bool {
	if strings.TrimSpace(msg.Sender) != roomLoopSender(roomID) {
		return false
	}
	if strings.TrimSpace(msg.Recipient) != strings.TrimSpace(actorID) {
		return false
	}
	if msg.TaskID != "" {
		return false
	}
	if msg.Kind == agent.BoardMessageKindCoordinatorPulse {
		return true
	}
	return msg.Kind == agent.BoardMessageKindAlert &&
		!msg.AckRequired &&
		!msg.ReplyExpected &&
		strings.HasPrefix(strings.TrimSpace(msg.Subject), "Coordinator pulse:")
}

func roomMessageMatchesSystemReminder(msg agent.BoardMessage, roomID string) bool {
	if strings.TrimSpace(msg.Sender) != roomLoopSender(roomID) {
		return false
	}
	if msg.Kind != agent.BoardMessageKindAlert {
		return false
	}
	subject := strings.TrimSpace(msg.Subject)
	if strings.HasPrefix(subject, "Coordinator pulse:") {
		return false
	}
	return strings.HasPrefix(subject, "Reminder:")
}

func normalizeRoomResolveFilters(values []string) (map[string]struct{}, error) {
	allowed := map[string]struct{}{
		"all":    {},
		"ack":    {},
		"reply":  {},
		"direct": {},
	}
	filters := make(map[string]struct{})
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			value := strings.TrimSpace(strings.ToLower(part))
			if value == "" {
				continue
			}
			if _, ok := allowed[value]; !ok {
				return nil, fmt.Errorf("unsupported room resolve filter %q", value)
			}
			if value == "all" {
				return map[string]struct{}{"all": {}}, nil
			}
			filters[value] = struct{}{}
		}
	}
	if len(filters) == 0 {
		return map[string]struct{}{"all": {}}, nil
	}
	return filters, nil
}

func roomResolveEntryMatches(entry roomInboxEntry, filters map[string]struct{}) bool {
	if roomStatusIncludesAll(filters) {
		return true
	}
	if _, ok := filters["direct"]; ok {
		return true
	}
	for _, flag := range entry.Flags {
		switch flag {
		case "ACK-REQUIRED":
			if _, ok := filters["ack"]; ok {
				return true
			}
		case "REPLY-EXPECTED":
			if _, ok := filters["reply"]; ok {
				return true
			}
		}
	}
	return false
}

func deriveRoomSubject(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "room message"
	}
	first := body
	if idx := strings.IndexByte(first, '\n'); idx >= 0 {
		first = first[:idx]
	}
	first = strings.Join(strings.Fields(first), " ")
	if len(first) > 80 {
		first = first[:77] + "..."
	}
	return first
}

func loadRoomState(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, actorID string, limit int) (agent.RoomSummary, []agent.BoardMessage, error) {
	summary, err := store.GetRoom(ctx, workspaceID, strings.TrimSpace(roomID), strings.TrimSpace(actorID))
	if err != nil {
		return agent.RoomSummary{}, nil, err
	}
	messages, err := store.ListRoomMessages(ctx, workspaceID, strings.TrimSpace(roomID), limit)
	if err != nil {
		return agent.RoomSummary{}, nil, err
	}
	return summary, messages, nil
}

func trimRoomHistory(messages []agent.BoardMessage, history int) []agent.BoardMessage {
	if history <= 0 {
		return []agent.BoardMessage{}
	}
	if len(messages) <= history {
		return messages
	}
	return append([]agent.BoardMessage(nil), messages[len(messages)-history:]...)
}

func normalizeRoomPoll(value time.Duration) time.Duration {
	if value <= 0 {
		return 2 * time.Second
	}
	return value
}

type roomRelayResult struct {
	Backend        string   `json:"backend"`
	DeliveredCount int      `json:"delivered_count"`
	FailedCount    int      `json:"failed_count"`
	DeliveredTo    []string `json:"delivered_to,omitempty"`
	FailedMembers  []string `json:"failed_members,omitempty"`
	SkippedMembers []string `json:"skipped_members,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type roomRelayOptions struct {
	Backend          string
	ZellijSession    string
	ZellijPluginPath string
}

func relayRoomMessage(ctx context.Context, client *tmuxbridge.Client, room agent.RoomSummary, msg agent.BoardMessage, relay roomRelayOptions) roomRelayResult {
	switch strings.TrimSpace(strings.ToLower(relay.Backend)) {
	case "auto", "mixed":
		return relayRoomMessageAuto(ctx, client, room, msg, relay)
	case "", "tmux":
		return relayRoomMessageTmux(ctx, client, room, msg)
	case "zellij":
		return relayRoomMessageZellij(ctx, room, msg, relay)
	default:
		return roomRelayResult{
			Backend: "unknown",
			Error:   fmt.Sprintf("unsupported relay backend %q", relay.Backend),
		}
	}
}

func relayRoomMessageAuto(ctx context.Context, client *tmuxbridge.Client, room agent.RoomSummary, msg agent.BoardMessage, relay roomRelayOptions) roomRelayResult {
	result := roomRelayResult{Backend: "auto"}
	tmuxTargets, zellijTargets, failed, skipped := collectRoomRelayTargetsByBackend(room, msg)
	result.SkippedMembers = append(result.SkippedMembers, skipped...)
	if len(failed) > 0 {
		result.FailedMembers = append(result.FailedMembers, failed...)
		result.FailedCount += len(failed)
	}
	for _, target := range tmuxTargets {
		_, err := client.DeliverTextWithOptions(ctx, target, formatRoomRelayContent(room, msg), tmuxbridge.DeliverOptions{Interrupt: msg.Interrupt})
		if err != nil {
			result.FailedCount++
			result.FailedMembers = append(result.FailedMembers, target)
			continue
		}
		result.DeliveredCount++
		result.DeliveredTo = append(result.DeliveredTo, target)
	}
	for session, targets := range zellijTargets {
		zellijResult := relayRoomMessageZellijTargets(ctx, room, msg, session, targets, relay)
		result.DeliveredCount += zellijResult.DeliveredCount
		result.FailedCount += zellijResult.FailedCount
		result.DeliveredTo = append(result.DeliveredTo, zellijResult.DeliveredTo...)
		result.FailedMembers = append(result.FailedMembers, zellijResult.FailedMembers...)
		result.SkippedMembers = append(result.SkippedMembers, zellijResult.SkippedMembers...)
		if result.Error == "" && zellijResult.Error != "" {
			result.Error = zellijResult.Error
		}
	}
	return result
}

func relayRoomMessageTmux(ctx context.Context, client *tmuxbridge.Client, room agent.RoomSummary, msg agent.BoardMessage) roomRelayResult {
	result := roomRelayResult{Backend: "tmux"}
	targets, skipped := collectRoomRelayTargets(room, msg)
	result.SkippedMembers = append(result.SkippedMembers, skipped...)
	for _, target := range targets {
		_, err := client.DeliverTextWithOptions(ctx, target, formatRoomRelayContent(room, msg), tmuxbridge.DeliverOptions{Interrupt: msg.Interrupt})
		if err != nil {
			result.FailedCount++
			result.FailedMembers = append(result.FailedMembers, target)
			continue
		}
		result.DeliveredCount++
		result.DeliveredTo = append(result.DeliveredTo, target)
	}
	return result
}

func formatRoomRelayContent(room agent.RoomSummary, msg agent.BoardMessage) string {
	body := strings.TrimSpace(msg.Body)
	subject := strings.TrimSpace(msg.Subject)
	sender := strings.TrimSpace(msg.Sender)
	recipient := normalizeRoomRecipient(msg.Recipient)
	if sender == "" {
		sender = "unknown"
	}
	if body == "" {
		body = subject
	}
	prefix := fmt.Sprintf("[room %s from=%s to=%s", room.ID, sender, recipient)
	if msg.AckRequired {
		prefix += " ack"
	}
	if msg.ReplyExpected {
		prefix += " reply"
	}
	if msg.Interrupt {
		prefix += " interrupt"
	}
	prefix += "]"
	if subject != "" && body != subject {
		return fmt.Sprintf("%s %s\n%s", prefix, subject, body)
	}
	return fmt.Sprintf("%s %s", prefix, body)
}

func buildRoomInboxEntries(actorID string, messages []agent.BoardMessage, filter string, includeBroadcasts bool) []roomInboxEntry {
	normalized := normalizeRoomInboxFilter(filter)
	latestBySender := latestRoomSenderActivity(messages)
	entries := make([]roomInboxEntry, 0, len(messages))
	for _, msg := range messages {
		entry, ok := roomInboxEntryForActor(actorID, msg, includeBroadcasts, latestBySender)
		if !ok {
			continue
		}
		if normalized != "all" && entry.Category != normalized {
			continue
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Priority != entries[j].Priority {
			return entries[i].Priority < entries[j].Priority
		}
		if !entries[i].CreatedAt.Equal(entries[j].CreatedAt) {
			return entries[i].CreatedAt.Before(entries[j].CreatedAt)
		}
		return entries[i].ID < entries[j].ID
	})
	return entries
}

func roomInboxEntryForActor(actorID string, msg agent.BoardMessage, includeBroadcasts bool, latestBySender map[string]time.Time) (roomInboxEntry, bool) {
	recipient := normalizeRoomRecipient(msg.Recipient)
	isDirect := sameRoomParticipant(recipient, actorID)
	isBroadcast := recipient == agent.BroadcastRecipient
	if !isDirect && !isBroadcast {
		return roomInboxEntry{}, false
	}
	if msg.Status == agent.BoardMessageStatusAcked || msg.Status == agent.BoardMessageStatusRead {
		return roomInboxEntry{}, false
	}
	if msg.ReplyExpected && !messageStillAwaitsReply(msg, latestBySender) {
		return roomInboxEntry{}, false
	}

	flags := make([]string, 0, 2)
	if msg.AckRequired && msg.Status != agent.BoardMessageStatusAcked {
		flags = append(flags, "ACK-REQUIRED")
	}
	if msg.ReplyExpected && msg.Status != agent.BoardMessageStatusAcked {
		flags = append(flags, "REPLY-EXPECTED")
	}
	category := "direct"
	if msg.AckRequired && msg.Status != agent.BoardMessageStatusAcked {
		category = "ack-required"
	} else if msg.ReplyExpected && msg.Status != agent.BoardMessageStatusAcked {
		category = "reply-expected"
	} else if isBroadcast {
		category = "broadcast"
	}
	if isBroadcast && !includeBroadcasts && category == "broadcast" {
		return roomInboxEntry{}, false
	}
	return roomInboxEntry{
		ID:        msg.ID,
		Sender:    msg.Sender,
		Recipient: recipient,
		Subject:   msg.Subject,
		Priority:  msg.Priority,
		Status:    msg.Status,
		CreatedAt: msg.CreatedAt,
		Category:  category,
		Flags:     flags,
		Preview:   summarizeRoomPreview(msg.Body),
		Message:   msg,
	}, true
}

func latestRoomSenderActivity(messages []agent.BoardMessage) map[string]time.Time {
	latest := make(map[string]time.Time, len(messages))
	for _, msg := range messages {
		sender := strings.TrimSpace(msg.Sender)
		if sender == "" {
			continue
		}
		if ts, ok := latest[sender]; !ok || msg.CreatedAt.After(ts) {
			latest[sender] = msg.CreatedAt
		}
	}
	return latest
}

func messageStillAwaitsReply(msg agent.BoardMessage, latestBySender map[string]time.Time) bool {
	if !msg.ReplyExpected {
		return false
	}
	recipient := normalizeRoomRecipient(msg.Recipient)
	if recipient == agent.BroadcastRecipient {
		return false
	}
	latestReply, ok := latestBySender[recipient]
	if !ok {
		return true
	}
	return latestReply.Before(msg.CreatedAt)
}

func normalizeRoomInboxFilter(filter string) string {
	switch strings.TrimSpace(strings.ToLower(filter)) {
	case "ack-required", "reply-expected", "direct", "broadcast":
		return strings.TrimSpace(strings.ToLower(filter))
	default:
		return "all"
	}
}

func groupRoomInboxEntries(entries []roomInboxEntry) map[string][]roomInboxEntry {
	grouped := make(map[string][]roomInboxEntry)
	for _, entry := range entries {
		grouped[entry.Category] = append(grouped[entry.Category], entry)
	}
	return grouped
}

func summarizeRoomPreview(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= 140 {
		return body
	}
	return body[:140] + "..."
}

func buildRoomStatusParticipants(room agent.RoomSummary, messages []agent.BoardMessage, tasks []taskstore.Task, staleAfter time.Duration) []roomStatusParticipant {
	latestBySender := latestRoomSenderActivity(messages)
	participantSet := map[string]struct{}{}
	for _, member := range room.Members {
		if id := strings.TrimSpace(member.ActorID); id != "" {
			participantSet[id] = struct{}{}
		}
	}
	for _, participant := range room.Participants {
		if id := strings.TrimSpace(participant); id != "" && !strings.HasPrefix(id, "actor:system:room:") {
			participantSet[id] = struct{}{}
		}
	}
	participants := make([]roomStatusParticipant, 0, len(participantSet))
	now := time.Now().UTC()
	for actorID := range participantSet {
		p := roomStatusParticipant{
			ActorID: actorID,
			Role:    roomMemberRole(room.Members, actorID),
			Status:  "idle",
		}
		if ts, ok := latestBySender[actorID]; ok {
			tsCopy := ts
			p.LastActiveAt = &tsCopy
			if staleAfter > 0 && now.Sub(ts) > staleAfter {
				p.Status = "stale"
			} else {
				p.Status = "active"
			}
		}
		for _, task := range tasks {
			if sameRoomParticipant(task.AssignedActorID, actorID) {
				p.AssignedTaskCount++
			}
			if sameRoomParticipant(task.OwnerActorID, actorID) {
				p.OwnedTaskCount++
			}
		}
		entries := buildRoomStatusEntries(actorID, messages)
		p.ActionableInboxCount = len(entries)
		if len(entries) > 0 {
			entry := entries[0]
			actionable := roomStatusEntryFromInbox(entry)
			p.LatestActionable = &actionable
		}
		participants = append(participants, p)
	}
	sort.SliceStable(participants, func(i, j int) bool {
		return participants[i].ActorID < participants[j].ActorID
	})
	return participants
}

func buildRoomTaskPulseSummary(tasks []taskstore.Task, now time.Time, staleAfter time.Duration) roomTaskPulseSummary {
	var pulse roomTaskPulseSummary
	for _, task := range tasks {
		switch task.Status {
		case taskstore.StatusPending:
			pulse.Pending++
			if strings.TrimSpace(task.AssignedActorID) != "" {
				pulse.AssignedUnclaimed++
			}
		case taskstore.StatusInProgress:
			pulse.InProgress++
		case taskstore.StatusBlocked:
			pulse.Blocked++
		case taskstore.StatusCompleted:
			pulse.Completed++
		}
		if taskIsStale(task, now, staleAfter) {
			pulse.Stale++
		}
	}
	return pulse
}

func buildRoomStatusBacklog(room agent.RoomSummary, messages []agent.BoardMessage) roomStatusBacklog {
	backlog := roomStatusBacklog{}
	for _, participant := range room.Participants {
		if strings.HasPrefix(strings.TrimSpace(participant), "actor:system:room:") {
			continue
		}
		entries := buildRoomStatusEntries(participant, messages)
		if len(entries) == 0 {
			continue
		}
		backlog.ParticipantsWithPending++
		backlog.LatestByParticipant = append(backlog.LatestByParticipant, roomStatusEntryFromInbox(entries[0]))
		for _, entry := range entries {
			for _, flag := range entry.Flags {
				switch flag {
				case "ACK-REQUIRED":
					backlog.PendingAcks++
				case "REPLY-EXPECTED":
					backlog.PendingReplies++
				}
			}
		}
	}
	sort.SliceStable(backlog.LatestByParticipant, func(i, j int) bool {
		if !backlog.LatestByParticipant[i].CreatedAt.Equal(backlog.LatestByParticipant[j].CreatedAt) {
			return backlog.LatestByParticipant[i].CreatedAt.After(backlog.LatestByParticipant[j].CreatedAt)
		}
		return backlog.LatestByParticipant[i].Recipient < backlog.LatestByParticipant[j].Recipient
	})
	return backlog
}

func buildRoomStatusActionRequired(room agent.RoomSummary, messages []agent.BoardMessage, tasks []taskstore.Task, backlog roomStatusBacklog, taskPulse roomTaskPulseSummary, filters map[string]struct{}, staleAfter time.Duration, now time.Time, verbose bool) roomStatusActionRequired {
	summary := roomStatusActionRequired{
		Filter:                  sortedRoomStatusFilters(filters),
		ParticipantsWithPending: roomStatusFilteredCount(filters, "ack", "reply", backlog.ParticipantsWithPending),
		PendingAcks:             roomStatusFilteredCount(filters, "ack", "", backlog.PendingAcks),
		PendingReplies:          roomStatusFilteredCount(filters, "reply", "", backlog.PendingReplies),
		AssignedUnclaimed:       roomStatusFilteredCount(filters, "assigned", "", taskPulse.AssignedUnclaimed),
		BlockedTasks:            roomStatusFilteredCount(filters, "blocked", "", taskPulse.Blocked),
		StaleTasks:              roomStatusFilteredCount(filters, "stale", "", taskPulse.Stale),
		TopEntries:              filterRoomStatusEntries(backlog.LatestByParticipant, filters),
		TopTasks:                buildRoomStatusTaskEntries(tasks, filters, now, staleAfter),
	}
	if !verbose {
		return summary
	}
	summary.VerboseTopEntries = filterRoomStatusVerboseEntries(buildRoomStatusVerboseEntries(room, messages), filters)
	return summary
}

func buildRoomStatusVerboseEntries(room agent.RoomSummary, messages []agent.BoardMessage) []roomInboxEntry {
	out := make([]roomInboxEntry, 0, len(room.Participants))
	for _, participant := range room.Participants {
		if strings.HasPrefix(strings.TrimSpace(participant), "actor:system:room:") {
			continue
		}
		entries := buildRoomStatusEntries(participant, messages)
		if len(entries) == 0 {
			continue
		}
		out = append(out, entries[0])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].Recipient < out[j].Recipient
	})
	return out
}

func buildRoomStatusTaskEntries(tasks []taskstore.Task, filters map[string]struct{}, now time.Time, staleAfter time.Duration) []roomStatusTask {
	out := make([]roomStatusTask, 0, len(tasks))
	for _, task := range tasks {
		signals := roomStatusTaskSignals(task, now, staleAfter)
		if len(signals) == 0 {
			continue
		}
		filteredSignals := filterRoomStatusTaskSignals(signals, filters)
		if len(filteredSignals) == 0 {
			continue
		}
		out = append(out, roomStatusTask{
			ID:              task.ID,
			Title:           task.Title,
			Status:          task.Status,
			AssignedActorID: task.AssignedActorID,
			OwnerActorID:    task.OwnerActorID,
			BlockedReason:   task.BlockedReason,
			HeartbeatAt:     task.HeartbeatAt,
			Signals:         filteredSignals,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		leftRank := roomStatusTaskPriority(out[i].Signals)
		rightRank := roomStatusTaskPriority(out[j].Signals)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func roomStatusTaskSignals(task taskstore.Task, now time.Time, staleAfter time.Duration) []string {
	signals := make([]string, 0, 3)
	if task.Status == taskstore.StatusPending && strings.TrimSpace(task.AssignedActorID) != "" {
		signals = append(signals, "assigned")
	}
	if task.Status == taskstore.StatusBlocked {
		signals = append(signals, "blocked")
	}
	if taskIsStale(task, now, staleAfter) {
		signals = append(signals, "stale")
	}
	return signals
}

func filterRoomStatusTaskSignals(signals []string, filters map[string]struct{}) []string {
	if roomStatusIncludesAll(filters) {
		return append([]string(nil), signals...)
	}
	out := make([]string, 0, len(signals))
	for _, signal := range signals {
		if _, ok := filters[signal]; ok {
			out = append(out, signal)
		}
	}
	return out
}

func roomStatusTaskPriority(signals []string) int {
	for _, signal := range signals {
		if signal == "stale" {
			return 0
		}
	}
	for _, signal := range signals {
		if signal == "blocked" {
			return 1
		}
	}
	return 2
}

func filterRoomStatusEntries(entries []roomStatusEntry, filters map[string]struct{}) []roomStatusEntry {
	if roomStatusIncludesAll(filters) {
		return append([]roomStatusEntry(nil), entries...)
	}
	out := make([]roomStatusEntry, 0, len(entries))
	for _, entry := range entries {
		if roomStatusEntryMatchesFilters(entry, filters) {
			out = append(out, entry)
		}
	}
	return out
}

func filterRoomStatusVerboseEntries(entries []roomInboxEntry, filters map[string]struct{}) []roomInboxEntry {
	if roomStatusIncludesAll(filters) {
		return append([]roomInboxEntry(nil), entries...)
	}
	out := make([]roomInboxEntry, 0, len(entries))
	for _, entry := range entries {
		if roomStatusEntryMatchesFilters(roomStatusEntryFromInbox(entry), filters) {
			out = append(out, entry)
		}
	}
	return out
}

func roomStatusEntryMatchesFilters(entry roomStatusEntry, filters map[string]struct{}) bool {
	if roomStatusIncludesAll(filters) {
		return true
	}
	for _, flag := range entry.Flags {
		switch flag {
		case "ACK-REQUIRED":
			if _, ok := filters["ack"]; ok {
				return true
			}
		case "REPLY-EXPECTED":
			if _, ok := filters["reply"]; ok {
				return true
			}
		}
	}
	return false
}

func normalizeRoomStatusFilters(values []string) (map[string]struct{}, error) {
	allowed := map[string]struct{}{
		"all":      {},
		"ack":      {},
		"reply":    {},
		"assigned": {},
		"blocked":  {},
		"stale":    {},
	}
	filters := make(map[string]struct{})
	for _, raw := range values {
		for _, part := range strings.Split(raw, ",") {
			value := strings.TrimSpace(strings.ToLower(part))
			if value == "" {
				continue
			}
			if _, ok := allowed[value]; !ok {
				return nil, fmt.Errorf("unsupported room status filter %q", value)
			}
			if value == "all" {
				return map[string]struct{}{"all": {}}, nil
			}
			filters[value] = struct{}{}
		}
	}
	if len(filters) == 0 {
		return map[string]struct{}{"all": {}}, nil
	}
	return filters, nil
}

func sortedRoomStatusFilters(filters map[string]struct{}) []string {
	out := make([]string, 0, len(filters))
	for key := range filters {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func roomStatusIncludesAll(filters map[string]struct{}) bool {
	_, ok := filters["all"]
	return ok
}

func roomStatusFilteredCount(filters map[string]struct{}, primary string, secondary string, value int) int {
	if roomStatusIncludesAll(filters) {
		return value
	}
	if primary != "" {
		if _, ok := filters[primary]; ok {
			return value
		}
	}
	if secondary != "" {
		if _, ok := filters[secondary]; ok {
			return value
		}
	}
	return 0
}

func buildRoomStatusEntries(actorID string, messages []agent.BoardMessage) []roomInboxEntry {
	entries := buildRoomInboxEntries(actorID, messages, "all", false)
	if len(entries) == 0 {
		return nil
	}
	latestByChain := make(map[string]roomInboxEntry, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(strings.TrimSpace(entry.Sender), "actor:system:room:") {
			continue
		}
		if len(entry.Flags) == 0 {
			continue
		}
		key := roomMessageChainKey(entry.Message)
		if key == "" {
			key = entry.ID
		}
		current, ok := latestByChain[key]
		if !ok || roomStatusEntryMoreRecent(entry, current) {
			latestByChain[key] = entry
		}
	}
	out := make([]roomInboxEntry, 0, len(latestByChain))
	for _, entry := range latestByChain {
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func roomStatusEntryMoreRecent(left, right roomInboxEntry) bool {
	if !left.CreatedAt.Equal(right.CreatedAt) {
		return left.CreatedAt.After(right.CreatedAt)
	}
	if left.Priority != right.Priority {
		return left.Priority < right.Priority
	}
	return left.ID < right.ID
}

func roomStatusEntryFromInbox(entry roomInboxEntry) roomStatusEntry {
	return roomStatusEntry{
		ID:        entry.ID,
		Sender:    entry.Sender,
		Recipient: entry.Recipient,
		Subject:   entry.Subject,
		Priority:  entry.Priority,
		Status:    entry.Status,
		CreatedAt: entry.CreatedAt,
		Category:  entry.Category,
		Flags:     append([]string(nil), entry.Flags...),
		Preview:   entry.Preview,
	}
}

func expandRoomResolveMessageIDs(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID string, messageIDs []string) ([]string, error) {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return nil, fmt.Errorf("list room messages: %w", err)
	}
	byID := make(map[string]agent.BoardMessage, len(messages))
	for _, msg := range messages {
		byID[msg.ID] = msg
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(messageIDs))
	for _, id := range messageIDs {
		msg, ok := byID[id]
		if !ok {
			if _, exists := seen[id]; !exists {
				seen[id] = struct{}{}
				out = append(out, id)
			}
			continue
		}
		chain := roomMessageChainKey(msg)
		if chain == "" {
			chain = msg.ID
		}
		for _, candidate := range messages {
			if roomMessageChainKey(candidate) != chain {
				continue
			}
			if _, exists := seen[candidate.ID]; exists {
				continue
			}
			seen[candidate.ID] = struct{}{}
			out = append(out, candidate.ID)
		}
	}
	return out, nil
}

func roomMessageChainKey(msg agent.BoardMessage) string {
	if strings.TrimSpace(msg.RelatedMessageID) != "" {
		return strings.TrimSpace(msg.RelatedMessageID)
	}
	return strings.TrimSpace(msg.ID)
}

func roomMemberRole(members []agent.RoomMember, actorID string) string {
	for _, member := range members {
		if sameRoomParticipant(member.ActorID, actorID) {
			return strings.TrimSpace(member.Role)
		}
	}
	return ""
}

func taskIsStale(task taskstore.Task, now time.Time, staleAfter time.Duration) bool {
	if staleAfter <= 0 || strings.TrimSpace(task.OwnerActorID) == "" {
		return false
	}
	if task.Status != taskstore.StatusInProgress && task.Status != taskstore.StatusBlocked {
		return false
	}
	reference := task.CreatedAt
	if task.HeartbeatAt != nil {
		reference = *task.HeartbeatAt
	} else if task.ClaimedAt != nil {
		reference = *task.ClaimedAt
	}
	return now.Sub(reference) > staleAfter
}

func collectRoomRelayTargets(room agent.RoomSummary, msg agent.BoardMessage) ([]string, []string) {
	targets := make([]string, 0, len(room.Members))
	skipped := make([]string, 0, len(room.Members))
	recipient := normalizeRoomRecipient(msg.Recipient)
	for _, member := range room.Members {
		target := strings.TrimSpace(member.ActorID)
		if target == "" {
			continue
		}
		if sameRoomParticipant(target, strings.TrimSpace(msg.Sender)) {
			skipped = append(skipped, target)
			continue
		}
		if recipient != agent.BroadcastRecipient && !sameRoomParticipant(target, recipient) {
			skipped = append(skipped, target)
			continue
		}
		targets = append(targets, target)
	}
	return targets, skipped
}

func collectRoomRelayTargetsByBackend(room agent.RoomSummary, msg agent.BoardMessage) ([]string, map[string][]string, []string, []string) {
	tmuxTargets := make([]string, 0, len(room.Members))
	zellijTargets := make(map[string][]string)
	failed := make([]string, 0, len(room.Members))
	skipped := make([]string, 0, len(room.Members))
	recipient := normalizeRoomRecipient(msg.Recipient)
	for _, member := range room.Members {
		member = normalizeRoomMember(member)
		target := member.ActorID
		if target == "" {
			continue
		}
		if sameRoomParticipant(target, strings.TrimSpace(msg.Sender)) {
			skipped = append(skipped, target)
			continue
		}
		if recipient != agent.BroadcastRecipient && !sameRoomParticipant(target, recipient) {
			skipped = append(skipped, target)
			continue
		}
		if roomMemberRelayBackend(member) != "zellij" {
			tmuxTargets = append(tmuxTargets, target)
			continue
		}
		session, zellijTarget, ok := resolveRoomMemberZellijTarget(member)
		if !ok {
			failed = append(failed, target)
			continue
		}
		zellijTargets[session] = append(zellijTargets[session], zellijTarget)
	}
	return tmuxTargets, zellijTargets, failed, skipped
}

func roomMemberRelayBackend(member agent.RoomMember) string {
	if member.Backend != "" {
		return member.Backend
	}
	if strings.HasPrefix(member.ActorID, "zellij:") {
		return "zellij"
	}
	return "tmux"
}

func resolveRoomMemberZellijTarget(member agent.RoomMember) (string, string, bool) {
	if session, paneID, ok := parseZellijParticipantID(member.ActorID); ok {
		return session, formatZellijParticipantID(session, paneID), true
	}
	session := strings.TrimSpace(member.Session)
	if session == "" || member.Unbound {
		return "", "", false
	}
	if paneID := normalizeZellijPaneID(member.PaneID); paneID != "" {
		return session, formatZellijParticipantID(session, paneID), true
	}
	if actorID := strings.TrimSpace(member.ActorID); actorID != "" {
		return session, actorID, true
	}
	return "", "", false
}

func normalizeRoomRecipient(recipient string) string {
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return agent.BroadcastRecipient
	}
	return recipient
}

func roomProgressEnvelope(command string, seq int, final bool, data map[string]any, workspace string) envelope.Envelope {
	finalCopy := final
	env := protocol.OK(command, data,
		protocol.WithSource("cli"),
		protocol.WithWorkspace(workspace),
		protocol.WithMetaMutator(func(m *envelope.Meta) {
			m.Seq = &seq
			m.Final = &finalCopy
		}),
	)
	env.Status = "progress"
	return env
}

func roomMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func sameRoomParticipant(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if aRef, ok := tmuxbridge.ParseParticipantID(a); ok {
		if bRef, ok := tmuxbridge.ParseParticipantID(b); ok {
			return aRef.Session == bRef.Session && aRef.Target == bRef.Target
		}
	}
	if aSession, aPaneID, ok := parseZellijParticipantID(a); ok {
		if bSession, bPaneID, ok := parseZellijParticipantID(b); ok {
			return aSession == bSession && aPaneID == bPaneID
		}
	}
	return false
}
