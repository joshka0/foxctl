package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/contextplane"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	ws "github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/coordination"
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
		newRoomRedgreenCommand(),
		newRoomListCommand(),
		newRoomShowCommand(),
		newRoomStatusCommand(),
		newRoomInboxCommand(),
		newRoomSendCommand(),
		newRoomAckCommand(),
		newRoomResolveCommand(),
		newRoomClearCommand(),
		newRoomEpicCommand(),
		newRoomMilestoneCommand(),
		newRoomStoryCommand(),
		newRoomLogCommand(),
		newRoomRetroCommand(),
		newRoomACACommand(),
		newRoomWorkpackCommand(),
		newRoomPlanCommand(),
		newRoomInterviewCommand(),
		newRoomRemindCommand(),
		newRoomJoinCommand(),
		newRoomLeaveCommand(),
		newRoomTaskCommand(),
		newRoomSubscribeCommand(),
		newRoomRelayCommand(),
		newRoomLoopCommand(),
	)
	return cmd
}

func newRoomRemindCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remind",
		Short: "Manage durable scheduled room follow-ups",
	}
	cmd.AddCommand(
		newRoomRemindAddCommand(),
		newRoomRemindListCommand(),
		newRoomRemindCancelCommand(),
	)
	return cmd
}

func newRoomRemindAddCommand() *cobra.Command {
	var (
		workspace     string
		sender        string
		subject       string
		every         time.Duration
		maxIterations int
		replyExpected bool
		ackRequired   bool
		interrupt     bool
		allowPassive  bool
	)
	cmd := &cobra.Command{
		Use:   "add <room-id> <recipient> <text>",
		Short: "Create a durable scheduled follow-up for one participant",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomRemindAdd(cmd, workspace, sender, args[0], args[1], subject, args[2], every, maxIterations, ackRequired, replyExpected, interrupt, allowPassive)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&subject, "subject", "", "Optional root message subject")
	cmd.Flags().DurationVar(&every, "every", 15*time.Minute, "Reminder interval")
	cmd.Flags().IntVar(&maxIterations, "max-iterations", 3, "Maximum reminder follow-ups after the initial request")
	cmd.Flags().BoolVar(&replyExpected, "reply-expected", true, "Require a reply to stop reminders")
	cmd.Flags().BoolVar(&ackRequired, "ack-required", false, "Require an ack to stop reminders")
	cmd.Flags().BoolVar(&interrupt, "interrupt", false, "Interrupt the target pane for reminder follow-ups")
	cmd.Flags().BoolVar(&allowPassive, "allow-passive", false, "Allow scheduling reminders even when the room loop is not currently active")
	return cmd
}

func newRoomRemindListCommand() *cobra.Command {
	var (
		workspace       string
		includeInactive bool
	)
	cmd := &cobra.Command{
		Use:   "list <room-id>",
		Short: "List durable scheduled room follow-ups",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomRemindList(cmd, workspace, args[0], includeInactive)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().BoolVar(&includeInactive, "all", false, "Include completed and cancelled reminders")
	return cmd
}

func newRoomRemindCancelCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
	)
	cmd := &cobra.Command{
		Use:   "cancel <room-id> <reminder-id>",
		Short: "Cancel one durable room follow-up",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomRemindCancel(cmd, workspace, actorID, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

const (
	roomRedgreenMetadataDir       = ".agentctl/room-redgreen"
	roomRedgreenDefaultCheckShell = "go test ./..."
)

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

func newRoomRedgreenCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "redgreen",
		Short: "Run brokered red/green test-driving sessions inside a room",
	}
	cmd.AddCommand(
		newRoomRedgreenInitCommand(),
		newRoomRedgreenHideCommand(),
		newRoomRedgreenShowCommand(),
		newRoomRedgreenCheckCommand(),
	)
	return cmd
}

func newRoomRedgreenInitCommand() *cobra.Command {
	var (
		workspace    string
		title        string
		description  string
		redActor     string
		greenActor   string
		coordinator  string
		worktreeRoot string
		baseRef      string
		checkCommand string
	)
	cmd := &cobra.Command{
		Use:   "init <room-id> <slug>",
		Short: "Create a brokered red/green room with paired worktrees",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomRedgreenInit(cmd, workspace, args[0], args[1], title, description, redActor, greenActor, coordinator, worktreeRoot, baseRef, checkCommand)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&title, "title", "", "Room title override")
	cmd.Flags().StringVar(&description, "description", "", "Room description override")
	cmd.Flags().StringVar(&redActor, "red", "red-a", "Actor id for the hidden-test author")
	cmd.Flags().StringVar(&greenActor, "green", "green-a", "Actor id for the implementation author")
	cmd.Flags().StringVar(&coordinator, "coordinator", "", "Coordinator actor id (defaults to current pane identity or human-a)")
	cmd.Flags().StringVar(&worktreeRoot, "worktree-root", filepath.Join(os.TempDir(), "agentctl-redgreen"), "Parent directory for paired worktrees")
	cmd.Flags().StringVar(&baseRef, "base-ref", "HEAD", "Git ref to branch the paired worktrees from")
	cmd.Flags().StringVar(&checkCommand, "check-command", roomRedgreenDefaultCheckShell, "Shell command used for brokered hidden-suite checks")
	return cmd
}

func newRoomRedgreenHideCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "hide <room-id> <path>",
		Short: "Register a hidden test path that must stay private to the red worktree",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomRedgreenHide(cmd, workspace, sender, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomRedgreenShowCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "show <room-id>",
		Short: "Show brokered red/green room metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomRedgreenShow(cmd, workspace, sender, args[0])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Viewer actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomRedgreenCheckCommand() *cobra.Command {
	var (
		workspace    string
		sender       string
		checkCommand string
	)
	cmd := &cobra.Command{
		Use:   "check <room-id>",
		Short: "Run the hidden suite against the green worktree without exposing the tests",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomRedgreenCheck(cmd, workspace, sender, args[0], checkCommand)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Requester actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&checkCommand, "check-command", "", "Override the stored hidden-suite check shell command for this run")
	return cmd
}

func newRoomCreateCommand() *cobra.Command {
	var (
		workspace    string
		title        string
		description  string
		members      []string
		provision    bool
		muxBackend   string
		muxSession   string
		paneCommand  string
		agentCLI     string
		agentMode    string
		agentArgs    []string
		memberArgs   []string
		attach       bool
		pattern      string
		slug         string
		redActor     string
		greenActor   string
		coordinator  string
		worktreeRoot string
		baseRef      string
		checkCommand string
	)
	cmd := &cobra.Command{
		Use:   "create <room-id>",
		Short: "Create or update a durable room",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomCreateWithProvision(cmd, workspace, args[0], title, description, members, roomCreateProvisionOptions{
				Enabled:          provision,
				MuxBackend:       muxBackend,
				MuxSession:       muxSession,
				PaneCommand:      paneCommand,
				AgentCLI:         agentCLI,
				AgentMode:        agentMode,
				AgentArgs:        append([]string(nil), agentArgs...),
				MemberArgs:       append([]string(nil), memberArgs...),
				Attach:           attach,
				Pattern:          pattern,
				PatternSlug:      slug,
				RedActor:         redActor,
				GreenActor:       greenActor,
				CoordinatorActor: coordinator,
				WorktreeRoot:     worktreeRoot,
				BaseRef:          baseRef,
				CheckCommand:     checkCommand,
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
	cmd.Flags().StringVar(&pattern, "pattern", "", "Optional room bootstrap pattern (currently: redgreen)")
	cmd.Flags().StringVar(&slug, "slug", "", "Pattern slug override (defaults to room id when --pattern redgreen)")
	cmd.Flags().StringVar(&redActor, "red", "red-a", "Red actor id when --pattern redgreen")
	cmd.Flags().StringVar(&greenActor, "green", "green-a", "Green actor id when --pattern redgreen")
	cmd.Flags().StringVar(&coordinator, "coordinator", "", "Coordinator actor id when --pattern redgreen")
	cmd.Flags().StringVar(&worktreeRoot, "worktree-root", filepath.Join(os.TempDir(), "agentctl-redgreen"), "Parent directory for paired worktrees when --pattern redgreen")
	cmd.Flags().StringVar(&baseRef, "base-ref", "HEAD", "Git ref to branch paired worktrees from when --pattern redgreen")
	cmd.Flags().StringVar(&checkCommand, "check-command", roomRedgreenDefaultCheckShell, "Brokered hidden-suite command when --pattern redgreen")
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
		taskFilter string
		verbose    bool
	)
	cmd := &cobra.Command{
		Use:   "status <room-id>",
		Short: "Show a coordinator-facing room summary",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomStatus(cmd, workspace, args[0], limit, staleAfter, only, taskFilter, verbose)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().IntVar(&limit, "limit", 200, "Maximum room messages to inspect for status derivation")
	cmd.Flags().DurationVar(&staleAfter, "stale-after", 5*time.Minute, "Participant idle threshold")
	cmd.Flags().StringSliceVar(&only, "only", nil, "Filter coordinator action summary (ack,reply,assigned,blocked,stale,all)")
	cmd.Flags().StringVar(&taskFilter, "filter", "open", "Tasks to include in status payload: open (excludes completed and canceled), all, or completed")
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
		hint          string
		kind          string
		taskID        string
		priority      int
		ackRequired   bool
		replyExpected bool
		interrupt     bool
		autoCreate    bool
		noMuxSubmit   bool
		muxSubmitMode string
		noLiveRelay   bool
	)
	cmd := &cobra.Command{
		Use:   "send <room-id> <text>",
		Short: "Append a durable message and fan out to mux panes (live relay on by default)",
		Long: "Stores the message in the room timeline, then delivers it to other participants' tmux/zellij panes " +
			"(same path as room relay / room loop) so targets see the line and an implicit submit. " +
			"When this command runs inside tmux or zellij, agentctl also mux-submits the current pane by default " +
			"(Enter-only) so your local shell/agent composer finishes. Use --no-live-relay if room loop already relays; " +
			"use --no-mux-submit to skip the local pane submit.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomSendWithHint(cmd, workspace, args[0], sender, recipient, subject, hint, strings.Join(args[1:], " "), kind, taskID, priority, ackRequired, replyExpected, interrupt, autoCreate, roomSendMuxOpts{
				NoMuxSubmit:   noMuxSubmit,
				MuxSubmitMode: muxSubmitMode,
				NoLiveRelay:   noLiveRelay,
			})
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&recipient, "to", "", "Direct recipient participant id (omit for broadcast). Use --to so live relay targets exactly that pane; broadcast delivers to all other members")
	cmd.Flags().StringVar(&subject, "subject", "", "Optional subject line")
	cmd.Flags().StringVar(&hint, "hint", "", "Optional explicit hint for how the recipient should respond")
	cmd.Flags().StringVar(&kind, "kind", string(agent.BoardMessageKindInfo), "Message kind (info|instruction|alert|review_request)")
	cmd.Flags().StringVar(&taskID, "task-id", "", "Optional task id")
	cmd.Flags().IntVar(&priority, "priority", agent.DefaultPriority, "Priority from 1 (highest) to 5 (lowest)")
	cmd.Flags().BoolVar(&ackRequired, "ack-required", false, "Require explicit acknowledgment")
	cmd.Flags().BoolVar(&replyExpected, "reply-expected", false, "Mark the message as expecting a response (direct messages only)")
	cmd.Flags().BoolVar(&interrupt, "interrupt", false, "Interrupt the target pane before delivering the message (direct messages only)")
	cmd.Flags().BoolVar(&autoCreate, "auto-create", true, "Create the room if it does not exist")
	cmd.Flags().BoolVar(&noMuxSubmit, "no-mux-submit", false, "Do not send mux submit keys to the current pane after a successful send")
	cmd.Flags().StringVar(&muxSubmitMode, "mux-submit-mode", "enter-only", "Submit mode after send when inside tmux/zellij (escape-enter|enter-only)")
	cmd.Flags().BoolVar(&noLiveRelay, "no-live-relay", false, "Do not fan out this message to other participants' mux panes (use when room relay or room loop already delivers)")
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

func newRoomEpicCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "epic",
		Short: "Manage long-running agile epics inside a room",
	}
	cmd.AddCommand(
		newRoomEpicStartCommand(),
		newRoomEpicAskCommand(),
		newRoomEpicAnswerCommand(),
		newRoomEpicFinalizeCommand(),
		newRoomEpicShapeCommand(),
		newRoomEpicShowCommand(),
		newRoomEpicResumeCommand(),
		newRoomEpicHealthCommand(),
		newRoomEpicNextCommand(),
	)
	return cmd
}

func newRoomEpicStartCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		goal      string
		owner     string
		outcome   string
		horizon   string
		scope     []string
		success   []string
	)
	cmd := &cobra.Command{
		Use:   "start <room-id> <title>",
		Short: "Start a durable epic in a room",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomEpicStart(cmd, workspace, sender, args[0], args[1], goal, owner, outcome, horizon, scope, success)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&goal, "goal", "", "Epic goal or desired outcome")
	cmd.Flags().StringVar(&owner, "owner", "", "Epic owner actor id")
	cmd.Flags().StringVar(&outcome, "outcome", "", "Expected business or delivery outcome")
	cmd.Flags().StringVar(&horizon, "horizon", "", "Delivery horizon or target window")
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "Scope item (repeatable)")
	cmd.Flags().StringSliceVar(&success, "success", nil, "Success signal or success measure (repeatable)")
	return cmd
}

func newRoomEpicShowCommand() *cobra.Command {
	var (
		workspace string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "show <room-id> [epic-id]",
		Short: "Show epics or one epic hierarchy for a room",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			epicID := ""
			if len(args) > 1 {
				epicID = args[1]
			}
			return runRoomEpicShow(cmd, workspace, args[0], epicID, limit)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().IntVar(&limit, "limit", 250, "Maximum room messages to inspect")
	return cmd
}

func newRoomEpicResumeCommand() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "resume <room-id> <epic-id>",
		Short: "Return a resumable operational summary for one epic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomEpicResume(cmd, workspace, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	return cmd
}

func newRoomEpicHealthCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "health <room-id> <epic-id>",
		Short: "Return a coordinator-facing health pulse for one epic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomEpicHealth(cmd, workspace, args[0], args[1], actorID, limit)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Optional actor id to tailor actor-specific context")
	cmd.Flags().IntVar(&limit, "limit", roomTaskScanLimit, "Maximum room messages to inspect")
	return cmd
}

func newRoomEpicNextCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
	)
	cmd := &cobra.Command{
		Use:   "next <room-id> <epic-id>",
		Short: "Return the next concrete actions for one epic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomEpicNext(cmd, workspace, args[0], args[1], actorID)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Actor id for actor-specific next actions (defaults to coordinator lane)")
	return cmd
}

func newRoomEpicAskCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		to        string
		kind      string
	)
	cmd := &cobra.Command{
		Use:   "ask <room-id> <epic-id> <question>",
		Short: "Ask one durable intake question for an epic",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomEpicAsk(cmd, workspace, sender, args[0], args[1], to, kind, strings.Join(args[2:], " "))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator or PM actor id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&to, "to", "", "Directed respondent actor id")
	cmd.Flags().StringVar(&kind, "kind", "product", "Epic intake question kind (product|technical|constraint|success)")
	return cmd
}

func newRoomEpicAnswerCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "answer <room-id> <question-id> <answer>",
		Short: "Answer one epic intake question",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomEpicAnswer(cmd, workspace, sender, args[0], args[1], strings.Join(args[2:], " "))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Respondent actor id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomEpicFinalizeCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "finalize <room-id> <epic-id> <summary>",
		Short: "Finalize an epic after intake and milestone-shaping discussion",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomEpicFinalize(cmd, workspace, sender, args[0], args[1], strings.Join(args[2:], " "))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomEpicShapeCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		count     int
	)
	cmd := &cobra.Command{
		Use:   "shape <room-id> <epic-id>",
		Short: "Derive milestone proposals from a finalized epic brief",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomEpicShape(cmd, workspace, sender, args[0], args[1], count)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor id (defaults to current tmux/zellij pane)")
	cmd.Flags().IntVar(&count, "count", 3, "Maximum milestone proposals to derive")
	return cmd
}

func newRoomMilestoneCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "milestone",
		Short: "Manage agile milestones inside a room epic",
	}
	cmd.AddCommand(
		newRoomMilestoneStartCommand(),
		newRoomMilestoneContractCommand(),
		newRoomMilestoneCriteriaCommand(),
		newRoomMilestoneReviewCommand(),
		newRoomMilestoneSummaryCommand(),
		newRoomMilestoneShowCommand(),
	)
	return cmd
}

func newRoomMilestoneStartCommand() *cobra.Command {
	var (
		workspace     string
		sender        string
		goal          string
		objective     string
		owner         string
		scope         []string
		risks         []string
		excludes      []string
		deps          []string
		validators    []string
		requiredLanes []string
		optionalLanes []string
		exits         []string
		proposal      string
	)
	cmd := &cobra.Command{
		Use:   "start <room-id> <epic-id> [title]",
		Short: "Start a milestone under an epic",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := ""
			if len(args) > 2 {
				title = args[2]
			}
			return runRoomMilestoneStartWithPolicy(cmd, workspace, sender, args[0], args[1], title, goal, objective, owner, scope, risks, excludes, deps, validators, requiredLanes, optionalLanes, exits, proposal)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&goal, "goal", "", "Milestone goal")
	cmd.Flags().StringVar(&objective, "objective", "", "Milestone objective narrative")
	cmd.Flags().StringVar(&owner, "owner", "", "Milestone owner actor id")
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "Scope item (repeatable)")
	cmd.Flags().StringSliceVar(&risks, "risk", nil, "Milestone risk (repeatable)")
	cmd.Flags().StringSliceVar(&excludes, "exclude", nil, "Milestone exclusion (repeatable)")
	cmd.Flags().StringSliceVar(&deps, "dependency", nil, "Milestone dependency (repeatable)")
	cmd.Flags().StringSliceVar(&validators, "validator", nil, "Expected validator lane (repeatable)")
	cmd.Flags().StringSliceVar(&requiredLanes, "required-lane", nil, "Required evidence lane for milestone exit (repeatable)")
	cmd.Flags().StringSliceVar(&optionalLanes, "optional-lane", nil, "Optional evidence lane worth tracking but not required for exit (repeatable)")
	cmd.Flags().StringSliceVar(&exits, "exit", nil, "Milestone exit criterion (repeatable)")
	cmd.Flags().StringVar(&proposal, "proposal", "", "Milestone proposal id to promote into a real milestone")
	return cmd
}

func newRoomMilestoneContractCommand() *cobra.Command {
	var (
		workspace     string
		sender        string
		objective     string
		risks         []string
		excludes      []string
		deps          []string
		validators    []string
		requiredLanes []string
		optionalLanes []string
		exits         []string
	)
	cmd := &cobra.Command{
		Use:   "contract <room-id> <milestone-id>",
		Short: "Update the explicit milestone contract",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomMilestoneContractWithPolicy(cmd, workspace, sender, args[0], args[1], objective, risks, excludes, deps, validators, requiredLanes, optionalLanes, exits)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&objective, "objective", "", "Milestone objective narrative")
	cmd.Flags().StringSliceVar(&risks, "risk", nil, "Milestone risk (repeatable)")
	cmd.Flags().StringSliceVar(&excludes, "exclude", nil, "Milestone exclusion (repeatable)")
	cmd.Flags().StringSliceVar(&deps, "dependency", nil, "Milestone dependency (repeatable)")
	cmd.Flags().StringSliceVar(&validators, "validator", nil, "Expected validator lane (repeatable)")
	cmd.Flags().StringSliceVar(&requiredLanes, "required-lane", nil, "Required evidence lane for milestone exit (repeatable)")
	cmd.Flags().StringSliceVar(&optionalLanes, "optional-lane", nil, "Optional evidence lane worth tracking but not required for exit (repeatable)")
	cmd.Flags().StringSliceVar(&exits, "exit", nil, "Milestone exit criterion (repeatable)")
	return cmd
}

func newRoomMilestoneCriteriaCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "criteria <room-id> <milestone-id> <criterion>",
		Short: "Add one acceptance criterion to a milestone",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomMilestoneCriteria(cmd, workspace, sender, args[0], args[1], strings.Join(args[2:], " "))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomMilestoneReviewCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "review <room-id> <milestone-id> <pass|block> <notes>",
		Short: "Record a milestone review verdict",
		Args:  cobra.MinimumNArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomMilestoneReview(cmd, workspace, sender, args[0], args[1], args[2], strings.Join(args[3:], " "))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomMilestoneSummaryCommand() *cobra.Command {
	var (
		workspace           string
		sender              string
		summaryText         string
		passedCriteria      []string
		failedCriteria      []string
		waivedValidations   []string
		blockingValidations []string
		decisions           []string
		findings            []string
		nextItems           []string
		guidanceUpdates     []string
	)
	cmd := &cobra.Command{
		Use:   "summary <room-id> <milestone-id> [notes]",
		Short: "Record a milestone review synthesis summary",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			notes := ""
			if len(args) > 2 {
				notes = strings.Join(args[2:], " ")
			}
			return runRoomMilestoneSummary(cmd, workspace, sender, args[0], args[1], notes, summaryText, passedCriteria, failedCriteria, waivedValidations, blockingValidations, decisions, findings, nextItems, guidanceUpdates)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&summaryText, "summary", "", "Structured synthesis summary (overrides positional notes when provided)")
	cmd.Flags().StringSliceVar(&passedCriteria, "passed-criterion", nil, "Passed synthesis criterion (repeatable)")
	cmd.Flags().StringSliceVar(&failedCriteria, "failed-criterion", nil, "Failed synthesis criterion (repeatable)")
	cmd.Flags().StringSliceVar(&waivedValidations, "waived-validation", nil, "Waived validation id referenced by the synthesis (repeatable)")
	cmd.Flags().StringSliceVar(&blockingValidations, "blocking-validation", nil, "Blocking validation id referenced by the synthesis (repeatable)")
	cmd.Flags().StringSliceVar(&decisions, "decision", nil, "Notable milestone decision (repeatable)")
	cmd.Flags().StringSliceVar(&findings, "finding", nil, "Systemic milestone finding (repeatable)")
	cmd.Flags().StringSliceVar(&nextItems, "next", nil, "Recommended next milestone follow-up (repeatable)")
	cmd.Flags().StringSliceVar(&guidanceUpdates, "guidance", nil, "Guidance update captured from the synthesis (repeatable)")
	return cmd
}

func newRoomMilestoneShowCommand() *cobra.Command {
	var (
		workspace string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "show <room-id> [milestone-id]",
		Short: "Show milestones or one milestone hierarchy for a room",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			milestoneID := ""
			if len(args) > 1 {
				milestoneID = args[1]
			}
			return runRoomMilestoneShow(cmd, workspace, args[0], milestoneID, limit)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().IntVar(&limit, "limit", 250, "Maximum room messages to inspect")
	return cmd
}

func newRoomStoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "story",
		Short: "Manage agile stories under room milestones",
	}
	cmd.AddCommand(
		newRoomStoryProposeCommand(),
		newRoomStoryAcceptCommand(),
		newRoomStoryAddCommand(),
		newRoomStoryStateCommand(),
		newRoomStoryValidateCommand(),
		newRoomStoryShowCommand(),
	)
	return cmd
}

func newRoomStoryProposeCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		owner     string
		rationale string
	)
	cmd := &cobra.Command{
		Use:   "propose <room-id> <milestone-id> <title> <body>",
		Short: "Propose a story under a milestone before accepting it",
		Args:  cobra.MinimumNArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomStoryPropose(cmd, workspace, sender, args[0], args[1], args[2], strings.Join(args[3:], " "), owner, rationale)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&owner, "owner", "", "Proposed story owner actor id")
	cmd.Flags().StringVar(&rationale, "rationale", "", "Why this proposed story belongs in the milestone")
	return cmd
}

func newRoomStoryAcceptCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		owner     string
	)
	cmd := &cobra.Command{
		Use:   "accept <room-id> <milestone-id> <proposal-id>",
		Short: "Accept a proposed story into a real story record",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomStoryAccept(cmd, workspace, sender, args[0], args[1], args[2], owner)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&owner, "owner", "", "Story owner override")
	return cmd
}

func newRoomStoryAddCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		owner     string
	)
	cmd := &cobra.Command{
		Use:   "add <room-id> <milestone-id> <title> <body>",
		Short: "Add a story under a milestone",
		Args:  cobra.MinimumNArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomStoryAdd(cmd, workspace, sender, args[0], args[1], args[2], strings.Join(args[3:], " "), owner)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&owner, "owner", "", "Story owner actor id")
	return cmd
}

func newRoomStoryShowCommand() *cobra.Command {
	var (
		workspace string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "show <room-id> [story-id]",
		Short: "Show stories or one story record for a room",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			storyID := ""
			if len(args) > 1 {
				storyID = args[1]
			}
			return runRoomStoryShow(cmd, workspace, args[0], storyID, limit)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().IntVar(&limit, "limit", 250, "Maximum room messages to inspect")
	return cmd
}

func newRoomStoryStateCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		reason    string
		blockedBy string
		reviewer  string
	)
	cmd := &cobra.Command{
		Use:   "state <room-id> <story-id> <state>",
		Short: "Record an append-only lifecycle state update for an accepted story",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomStoryState(cmd, workspace, sender, args[0], args[1], args[2], reason, blockedBy, reviewer)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&reason, "reason", "", "Lifecycle state reason")
	cmd.Flags().StringVar(&blockedBy, "blocked-by", "", "Optional blocker reference when state=blocked")
	cmd.Flags().StringVar(&reviewer, "reviewer", "", "Optional reviewer when state=in_review")
	return cmd
}

func newRoomStoryValidateCommand() *cobra.Command {
	var (
		workspace      string
		sender         string
		artifactPath   string
		artifactDigest string
		command        string
		notes          string
		relatedStoryID []string
	)
	cmd := &cobra.Command{
		Use:   "validate <room-id> <story-id> <validator-type> <pass|fail|blocked|waived> <summary>",
		Short: "Attach story-owned validation evidence",
		Args:  cobra.MinimumNArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomStoryValidate(cmd, workspace, sender, args[0], args[1], args[2], args[3], strings.Join(args[4:], " "), artifactPath, artifactDigest, command, notes, relatedStoryID)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&artifactPath, "artifact-path", "", "Optional markdown or artifact path")
	cmd.Flags().StringVar(&artifactDigest, "artifact-digest", "", "Optional CAS digest for the validation artifact")
	cmd.Flags().StringVar(&command, "command", "", "Optional command or check that produced this validation")
	cmd.Flags().StringVar(&notes, "notes", "", "Optional validation notes")
	cmd.Flags().StringSliceVar(&relatedStoryID, "related-story", nil, "Related story ids for cross-story validation")
	return cmd
}

func newRoomLogCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Manage the durable delivery log for a room epic",
	}
	cmd.AddCommand(
		newRoomLogAppendCommand(),
		newRoomLogShowCommand(),
	)
	return cmd
}

func newRoomRetroCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "retro",
		Short: "Record and inspect durable agile guidance updates",
	}
	cmd.AddCommand(
		newRoomRetroAddCommand(),
		newRoomRetroShowCommand(),
	)
	return cmd
}

func newRoomRetroAddCommand() *cobra.Command {
	var (
		workspace   string
		sender      string
		milestoneID string
		kind        string
		summaryText string
		impact      string
		change      string
		scope       []string
		followUp    []string
	)
	cmd := &cobra.Command{
		Use:   "add <room-id> <epic-id>",
		Short: "Add one durable retro/guidance update for an epic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomRetroAdd(cmd, workspace, sender, args[0], args[1], milestoneID, kind, summaryText, impact, change, scope, followUp)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&milestoneID, "milestone", "", "Optional milestone id linked to this retro update")
	cmd.Flags().StringVar(&kind, "kind", "", "Guidance kind: process, tooling, coordination, quality, delivery")
	cmd.Flags().StringVar(&summaryText, "summary", "", "Guidance summary")
	cmd.Flags().StringVar(&impact, "impact", "", "Why the guidance matters")
	cmd.Flags().StringVar(&change, "change", "", "Recommended change to carry forward")
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "Scope item (repeatable)")
	cmd.Flags().StringSliceVar(&followUp, "follow-up", nil, "Suggested follow-up action (repeatable)")
	return cmd
}

func newRoomRetroShowCommand() *cobra.Command {
	var (
		workspace   string
		milestoneID string
		limit       int
	)
	cmd := &cobra.Command{
		Use:   "show <room-id> <epic-id>",
		Short: "Show retro/guidance updates for an epic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomRetroShow(cmd, workspace, args[0], args[1], milestoneID, limit)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&milestoneID, "milestone", "", "Optional milestone id filter")
	cmd.Flags().IntVar(&limit, "limit", 250, "Maximum room messages to inspect")
	return cmd
}

func newRoomACACommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "aca",
		Short: "Promote high-signal agile room artifacts into ACA drafts",
	}
	cmd.AddCommand(
		newRoomACAPromoteCommand(),
	)
	return cmd
}

func newRoomACAPromoteCommand() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "promote <epic|milestone|retro|validation> <room-id> <source-id>",
		Short: "Draft one ACA proposal note from a room-agile artifact",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomACAPromote(cmd, workspace, args[1], args[0], args[2])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	return cmd
}

func newRoomWorkpackCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workpack",
		Short: "Show or refresh agile epic work-pack mirrors",
	}
	cmd.AddCommand(
		newRoomWorkpackShowCommand(),
		newRoomWorkpackSyncCommand(),
	)
	return cmd
}

func newRoomWorkpackShowCommand() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "show <room-id> <epic-id>",
		Short: "Show the derived work-pack paths for one epic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomWorkpackShow(cmd, workspace, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	return cmd
}

func newRoomWorkpackSyncCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "sync <room-id> <epic-id>",
		Short: "Force a refresh of the derived work-pack mirror for one epic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomWorkpackSync(cmd, workspace, sender, args[0], args[1])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomLogAppendCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		completed []string
		inFlight  []string
		blockers  []string
		nextFocus []string
		notes     string
	)
	cmd := &cobra.Command{
		Use:   "append <room-id> <epic-id> <label>",
		Short: "Append one durable delivery-log entry to an epic",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomLogAppend(cmd, workspace, sender, args[0], args[1], args[2], completed, inFlight, blockers, nextFocus, notes)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringSliceVar(&completed, "completed", nil, "Completed item (repeatable)")
	cmd.Flags().StringSliceVar(&inFlight, "in-flight", nil, "In-flight item (repeatable)")
	cmd.Flags().StringSliceVar(&blockers, "blocker", nil, "Blocker item (repeatable)")
	cmd.Flags().StringSliceVar(&nextFocus, "next", nil, "Next-focus item (repeatable)")
	cmd.Flags().StringVar(&notes, "notes", "", "Freeform notes for the delivery-log entry")
	return cmd
}

func newRoomLogShowCommand() *cobra.Command {
	var (
		workspace string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "show <room-id> <epic-id>",
		Short: "Show delivery-log entries for one epic",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomLogShow(cmd, workspace, args[0], args[1], limit)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().IntVar(&limit, "limit", 250, "Maximum room messages to inspect")
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

func newRoomInterviewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "interview",
		Short: "Run a durable round-robin interview protocol inside a room",
	}
	cmd.AddCommand(
		newRoomInterviewStartCommand(),
		newRoomInterviewAskCommand(),
		newRoomInterviewAnswerCommand(),
		newRoomInterviewVerifyCommand(),
		newRoomInterviewNextCommand(),
		newRoomInterviewShowCommand(),
	)
	return cmd
}

func newRoomInterviewStartCommand() *cobra.Command {
	var (
		workspace   string
		sender      string
		spec        string
		specRef     string
		submitter   string
		questioner  string
		respondent  string
		verifier    string
		constraints []string
	)
	cmd := &cobra.Command{
		Use:   "start <room-id> <topic>",
		Short: "Start a durable interview session for clarifying a spec or plan",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomInterviewStart(cmd, workspace, sender, args[0], args[1], spec, specRef, submitter, questioner, respondent, verifier, constraints)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&spec, "spec", "", "Inline spec or request summary")
	cmd.Flags().StringVar(&specRef, "spec-ref", "", "Doc path, plan id, or message id that anchors the interview")
	cmd.Flags().StringVar(&submitter, "submitter", "", "Actor who submitted the plan or spec")
	cmd.Flags().StringVar(&questioner, "questioner", "", "Actor responsible for drafting interview questions")
	cmd.Flags().StringVar(&respondent, "respondent", "", "Actor expected to answer the questions")
	cmd.Flags().StringVar(&verifier, "verifier", "", "Actor who decides whether answers match the original intent (defaults to submitter)")
	cmd.Flags().StringSliceVar(&constraints, "constraint", nil, "Constraint or guardrail (repeatable)")
	return cmd
}

func newRoomInterviewAskCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		to        string
	)
	cmd := &cobra.Command{
		Use:   "ask <room-id> <session-id> <question>",
		Short: "Record a directed interview question for another participant",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomInterviewAsk(cmd, workspace, sender, args[0], args[1], to, strings.Join(args[2:], " "))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Questioner actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&to, "to", "", "Respondent actor id (defaults to the session respondent)")
	return cmd
}

func newRoomInterviewAnswerCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "answer <room-id> <question-id> <answer>",
		Short: "Answer a previously recorded interview question",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomInterviewAnswer(cmd, workspace, sender, args[0], args[1], strings.Join(args[2:], " "))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Respondent actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomInterviewVerifyCommand() *cobra.Command {
	var (
		workspace string
		sender    string
	)
	cmd := &cobra.Command{
		Use:   "verify <room-id> <answer-id> <accept|clarify|reject> <notes>",
		Short: "Record whether an interview answer matches the intended meaning",
		Args:  cobra.MinimumNArgs(4),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomInterviewVerify(cmd, workspace, sender, args[0], args[1], args[2], strings.Join(args[3:], " "))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Verifier actor or participant id (defaults to current tmux/zellij pane)")
	return cmd
}

func newRoomInterviewNextCommand() *cobra.Command {
	var (
		workspace string
		actorID   string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "next <room-id>",
		Short: "Show the next pending interview item for one participant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomInterviewNext(cmd, workspace, args[0], actorID, limit)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().IntVar(&limit, "limit", 200, "Maximum room messages to inspect")
	return cmd
}

func newRoomInterviewShowCommand() *cobra.Command {
	var (
		workspace string
		limit     int
	)
	cmd := &cobra.Command{
		Use:   "show <room-id> [session-id]",
		Short: "Show interview sessions or one interview thread",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := ""
			if len(args) > 1 {
				sessionID = args[1]
			}
			return runRoomInterviewShow(cmd, workspace, args[0], sessionID, limit)
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
	Enabled          bool
	MuxBackend       string
	MuxSession       string
	PaneCommand      string
	AgentCLI         string
	AgentMode        string
	AgentArgs        []string
	MemberArgs       []string
	Attach           bool
	Pattern          string
	PatternSlug      string
	RedActor         string
	GreenActor       string
	CoordinatorActor string
	WorktreeRoot     string
	BaseRef          string
	CheckCommand     string
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
	switch strings.TrimSpace(strings.ToLower(provision.Pattern)) {
	case "":
	case "redgreen":
		slug := strings.TrimSpace(provision.PatternSlug)
		if slug == "" {
			slug = roomID
		}
		return runRoomRedgreenInit(
			cmd,
			workspace,
			roomID,
			slug,
			title,
			description,
			provision.RedActor,
			provision.GreenActor,
			provision.CoordinatorActor,
			provision.WorktreeRoot,
			provision.BaseRef,
			provision.CheckCommand,
		)
	default:
		absWorkspace, err := resolveRoomWorkspace(workspace)
		if err != nil {
			return err
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.create", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported room pattern %q", provision.Pattern), map[string]any{
			"hint": "Use --pattern redgreen or omit --pattern.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

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
		members = ensureRoomCoordinatorMember(members, agent.RoomMember{
			ActorID: identity.Sender,
			Role:    "coordinator",
			Backend: identity.Backend,
			Session: identity.Session,
			PaneID:  identity.PaneID,
		})
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
	return ensureRoomCoordinatorMember(existing, agent.RoomMember{ActorID: actorID, Role: "coordinator"})
}

func ensureRoomCoordinatorMember(existing []agent.RoomMember, member agent.RoomMember) []agent.RoomMember {
	member = normalizeRoomMember(member)
	if strings.TrimSpace(member.ActorID) == "" {
		return existing
	}
	out := make([]agent.RoomMember, len(existing))
	copy(out, existing)
	for i := range out {
		out[i] = normalizeRoomMember(out[i])
		if strings.TrimSpace(out[i].ActorID) != member.ActorID {
			continue
		}
		if strings.TrimSpace(out[i].Role) == "" {
			out[i].Role = "coordinator"
		}
		if out[i].Backend == "" {
			out[i].Backend = member.Backend
		}
		if out[i].Session == "" {
			out[i].Session = member.Session
		}
		if out[i].PaneID == "" {
			out[i].PaneID = member.PaneID
		}
		return out
	}
	return append(out, member)
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

func runRoomStatus(cmd *cobra.Command, workspace, roomID string, limit int, staleAfter time.Duration, only []string, taskFilter string, verbose bool) error {
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
	statusEff, omitComp, omitCan, err := parseRoomTaskListSelection("", taskFilter)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.status", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use --filter open (default), all, or completed.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	tasks, err := listRoomTasks(cmd.Context(), taskStore, ws.CanonicalID(absWorkspace), messages, statusEff, omitComp, omitCan)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.status", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	now := time.Now().UTC()
	taskPulse := buildRoomTaskPulseSummary(tasks, now, staleAfter)
	reminderRoots, err := loadRoomReminderRoots(cmd.Context(), absWorkspace, roomID, true)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.status", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	backlog := buildRoomStatusBacklog(summary, messages, reminderRoots)
	filters, err := normalizeRoomStatusFilters(only)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.status", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use comma-separated or repeated --only values from: ack, reply, assigned, blocked, stale, all.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.status", map[string]any{
		"room":            summary,
		"participants":    buildRoomStatusParticipants(summary, messages, tasks, staleAfter, reminderRoots),
		"task_pulse":      taskPulse,
		"backlog":         backlog,
		"action_required": buildRoomStatusActionRequired(summary, messages, tasks, backlog, taskPulse, filters, staleAfter, now, verbose, reminderRoots),
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

func runRoomSend(cmd *cobra.Command, workspace, roomID, sender, recipient, subject, body, kind, taskID string, priority int, ackRequired, replyExpected, interrupt, autoCreate bool, muxOpts ...roomSendMuxOpts) error {
	var mux roomSendMuxOpts
	if len(muxOpts) > 0 {
		mux = muxOpts[0]
	}
	return runRoomSendWithHint(cmd, workspace, roomID, sender, recipient, subject, "", body, kind, taskID, priority, ackRequired, replyExpected, interrupt, autoCreate, mux)
}

func runRoomSendWithHint(cmd *cobra.Command, workspace, roomID, sender, recipient, subject, hint, body, kind, taskID string, priority int, ackRequired, replyExpected, interrupt, autoCreate bool, mux roomSendMuxOpts) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	senderProvided := strings.TrimSpace(sender) != ""
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
	summary, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, "")
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
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
	if recipient != agent.BroadcastRecipient && !roomHasParticipant(summary, recipient) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeEARG, fmt.Sprintf("recipient %q is not a participant in room %q", recipient, roomID), map[string]any{
			"hint": "Add the participant to the room first with `agentctl room join`, or send a broadcast without --to.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if strings.TrimSpace(subject) == "" {
		subject = deriveRoomSubject(body)
	}
	body = annotateRoomSendBody(roomID, identity.Sender, recipient, body, hint, ackRequired, replyExpected)

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
	warnings := make([]string, 0, 4)
	if !senderProvided {
		warnings = append(warnings, fmt.Sprintf("sender was inferred as %s from the current execution context", identity.Sender))
	}
	if msg.Recipient == agent.BroadcastRecipient {
		warnings = append(warnings, "broadcast: live relay (unless --no-live-relay) notifies every other participant; pass --to <participant-id> to deliver only to that target")
	}
	var muxSubmit map[string]any
	if !mux.NoMuxSubmit {
		mode := strings.TrimSpace(mux.MuxSubmitMode)
		if mode == "" {
			mode = "enter-only"
		}
		bridgeMode, err := parseMuxSubmitModeString(mode)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("mux submit skipped: %v", err))
		} else if detail, warn := roomSendMuxSubmitHook(cmd.Context(), bridgeMode); detail != nil {
			muxSubmit = detail
		} else if warn != "" {
			warnings = append(warnings, warn)
		}
	}
	data := map[string]any{
		"room_id":         roomID,
		"stream":          msg.Stream,
		"message_id":      msg.ID,
		"message":         msg,
		"sender_identity": identity,
		"warnings":        warnings,
	}
	if msg.Recipient != agent.BroadcastRecipient {
		data["delivery"] = "direct"
		data["recipient"] = msg.Recipient
	} else {
		data["delivery"] = "broadcast"
	}
	if !mux.NoLiveRelay {
		data["live_relay"] = roomSendRelayHook(cmd.Context(), store, absWorkspace, roomID, []*agent.BoardMessage{msg})
	} else {
		data["live_relay_skipped"] = true
	}
	if muxSubmit != nil {
		data["mux_submit"] = muxSubmit
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.send", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func annotateRoomSendBody(roomID, sender, recipient, body, hint string, ackRequired, replyExpected bool) string {
	body = strings.TrimSpace(body)
	sender = strings.TrimSpace(sender)
	if sender == "" {
		sender = "unknown"
	}
	recipient = normalizeRoomRecipient(recipient)
	if recipient == "" {
		recipient = agent.BroadcastRecipient
	}

	lines := []string{
		"",
		"--",
		"Room send metadata:",
		fmt.Sprintf("Sent by: %s", sender),
	}
	if recipient == agent.BroadcastRecipient {
		lines = append(lines, "Audience: room")
	} else {
		lines = append(lines, fmt.Sprintf("Direct recipient: %s", recipient))
	}
	lines = append(lines,
		fmt.Sprintf("Response requested: %t", replyExpected),
		fmt.Sprintf("Acknowledgment requested: %t", ackRequired),
	)
	if replyExpected && recipient != agent.BroadcastRecipient {
		lines = append(lines, fmt.Sprintf("Reply with: agentctl room send %s --to %s \"<response>\"", roomID, sender))
	}
	if trimmedHint := strings.TrimSpace(hint); trimmedHint != "" {
		lines = append(lines, "Response hint: "+trimmedHint)
	}
	return body + "\n" + strings.Join(lines, "\n")
}

const roomLoopHeartbeatGrace = 15 * time.Second

func runRoomRemindAdd(cmd *cobra.Command, workspace, sender, roomID, recipient, subject, body string, every time.Duration, maxIterations int, ackRequired, replyExpected, interrupt, allowPassive bool) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	if !ackRequired && !replyExpected {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeEARG, "room remind requires --ack-required or --reply-expected", map[string]any{
			"hint": "Use --reply-expected for check-ins that need a response, or --ack-required for simple confirmation reminders.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if every <= 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeEARG, "every must be positive", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if maxIterations <= 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeEARG, "max-iterations must be positive", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()
	recipient, err = resolveRoomRecipient(cmd.Context(), boardStore, absWorkspace, roomID, recipient)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use a direct room participant id or @coordinator. Broadcast reminders are not supported.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if recipient == agent.BroadcastRecipient {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeEARG, "room remind requires a direct recipient", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if strings.TrimSpace(subject) == "" {
		subject = deriveRoomSubject(body)
	}
	root := &agent.BoardMessage{
		WorkspaceID:   absWorkspace,
		Stream:        agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:        identity.Sender,
		Recipient:     recipient,
		Kind:          agent.BoardMessageKindInstruction,
		Priority:      agent.DefaultPriority,
		AckRequired:   ackRequired,
		ReplyExpected: replyExpected,
		Interrupt:     interrupt,
		Subject:       subject,
		Body:          strings.TrimSpace(body),
	}
	if err := boardStore.SendMessage(cmd.Context(), root); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	cfg, err := loadConfig(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	coordStore, err := coordination.Open(cmd.Context(), cfg.Storage.Root)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer coordStore.Close()
	if !allowPassive {
		if err := requireActiveRoomLoop(cmd.Context(), coordStore, absWorkspace, strings.TrimSpace(roomID), time.Now().UTC()); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
				"hint": fmt.Sprintf("Start the room loop first with `agentctl room loop %s --workspace %s ...`, or pass --allow-passive if you intentionally want a stored reminder without an active loop.", strings.TrimSpace(roomID), absWorkspace),
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	reminder, err := coordStore.UpsertRoomReminder(cmd.Context(), coordination.RoomReminder{
		ID:            root.ID,
		WorkspaceID:   absWorkspace,
		RoomID:        strings.TrimSpace(roomID),
		RootMessageID: root.ID,
		Sender:        identity.Sender,
		Recipient:     recipient,
		Subject:       subject,
		Body:          strings.TrimSpace(body),
		AckRequired:   ackRequired,
		ReplyExpected: replyExpected,
		Interrupt:     interrupt,
		Interval:      every,
		MaxIterations: maxIterations,
		LastSentAt:    &root.CreatedAt,
		Active:        true,
	})
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.remind.add", map[string]any{
		"room_id":   roomID,
		"message":   root,
		"reminder":  reminder,
		"actor_id":  identity.Sender,
		"recipient": recipient,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func requireActiveRoomLoop(ctx context.Context, coordStore *coordination.Store, workspaceID, roomID string, now time.Time) error {
	loop, err := coordStore.GetRoomLoop(ctx, workspaceID, roomID)
	if err != nil {
		return err
	}
	if loop == nil || !loop.Enabled {
		return fmt.Errorf("room loop is not active for %q", roomID)
	}
	if loop.LastTickAt == nil || loop.LastTickAt.IsZero() {
		return fmt.Errorf("room loop for %q has no recorded heartbeat", roomID)
	}
	if now.Sub(loop.LastTickAt.UTC()) > roomLoopHeartbeatGrace {
		return fmt.Errorf("room loop heartbeat for %q is stale (last tick %s)", roomID, loop.LastTickAt.UTC().Format(time.RFC3339))
	}
	return nil
}

func runRoomRemindList(cmd *cobra.Command, workspace, roomID string, includeInactive bool) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	coordStore, err := coordination.Open(cmd.Context(), cfg.Storage.Root)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer coordStore.Close()
	reminders, err := coordStore.ListRoomReminders(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), includeInactive)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.remind.list", map[string]any{
		"room_id":   roomID,
		"count":     len(reminders),
		"reminders": reminders,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomRemindCancel(cmd *cobra.Command, workspace, actorID, roomID, reminderID string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), actorID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.cancel", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --actor when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.cancel", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()
	summary, _, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, identity.Sender, 1)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.cancel", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.cancel", protocol.ErrorCodeEARG, "room remind cancel requires coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator or transfer coordinator ownership first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	cfg, err := loadConfig(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.cancel", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	coordStore, err := coordination.Open(cmd.Context(), cfg.Storage.Root)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.cancel", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer coordStore.Close()
	reminder, err := coordStore.GetRoomReminder(cmd.Context(), absWorkspace, reminderID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.cancel", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if reminder == nil || strings.TrimSpace(reminder.RoomID) != strings.TrimSpace(roomID) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.cancel", protocol.ErrorCodeENotFound, fmt.Sprintf("reminder %q not found", reminderID), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	reminder.Active = false
	updated, err := coordStore.UpsertRoomReminder(cmd.Context(), *reminder)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.remind.cancel", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.remind.cancel", map[string]any{
		"room_id":   roomID,
		"actor_id":  identity.Sender,
		"reminder":  updated,
		"cancelled": true,
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

type roomEpicMeta struct {
	Goal    string   `json:"goal"`
	Owner   string   `json:"owner"`
	Outcome string   `json:"outcome"`
	Horizon string   `json:"horizon"`
	Scope   []string `json:"scope"`
	Success []string `json:"success"`
}

type roomEpicQuestionMeta struct {
	Kind     string `json:"kind"`
	Question string `json:"question"`
}

type roomMilestoneMeta struct {
	EpicID                string   `json:"epic_id"`
	Goal                  string   `json:"goal"`
	Objective             string   `json:"objective"`
	Owner                 string   `json:"owner"`
	Scope                 []string `json:"scope"`
	Risks                 []string `json:"risks"`
	Exclusions            []string `json:"exclusions"`
	Dependencies          []string `json:"dependencies"`
	ValidatorsExpected    []string `json:"validators_expected"`
	RequiredEvidenceLanes []string `json:"required_evidence_lanes"`
	OptionalEvidenceLanes []string `json:"optional_evidence_lanes"`
	ExitCriteria          []string `json:"exit_criteria"`
}

type roomMilestoneSummaryMeta struct {
	Summary               string   `json:"summary"`
	PassedCriteria        []string `json:"passed_criteria"`
	FailedCriteria        []string `json:"failed_criteria"`
	WaivedValidationIDs   []string `json:"waived_validation_ids"`
	BlockingValidationIDs []string `json:"blocking_validation_ids"`
	NotableDecisions      []string `json:"notable_decisions"`
	SystemicFindings      []string `json:"systemic_findings"`
	RecommendedNext       []string `json:"recommended_next"`
	GuidanceUpdates       []string `json:"guidance_updates"`
}

type roomStoryMeta struct {
	Owner       string `json:"owner"`
	Description string `json:"description"`
}

type roomStoryStateMeta struct {
	State     string `json:"state"`
	Reason    string `json:"reason"`
	BlockedBy string `json:"blocked_by"`
	Reviewer  string `json:"reviewer"`
}

type roomStoryValidationMeta struct {
	EpicID          string   `json:"epic_id"`
	MilestoneID     string   `json:"milestone_id"`
	StoryID         string   `json:"story_id"`
	ValidatorType   string   `json:"validator_type"`
	Status          string   `json:"status"`
	Summary         string   `json:"summary"`
	Command         string   `json:"command"`
	ArtifactPath    string   `json:"artifact_path"`
	ArtifactDigest  string   `json:"artifact_digest"`
	Notes           string   `json:"notes"`
	WaiverReason    string   `json:"waiver_reason"`
	RelatedStoryIDs []string `json:"related_story_ids"`
}

type roomStoryProposalMeta struct {
	MilestoneID string `json:"milestone_id"`
	Owner       string `json:"owner"`
	Description string `json:"description"`
	Rationale   string `json:"rationale"`
}

type roomDeliveryLogMeta struct {
	Label     string   `json:"label"`
	Completed []string `json:"completed"`
	InFlight  []string `json:"in_flight"`
	Blockers  []string `json:"blockers"`
	NextFocus []string `json:"next_focus"`
	Notes     string   `json:"notes"`
}

type roomGuidanceUpdateMeta struct {
	EpicID            string   `json:"epic_id"`
	MilestoneID       string   `json:"milestone_id"`
	Kind              string   `json:"kind"`
	Summary           string   `json:"summary"`
	Impact            string   `json:"impact"`
	RecommendedChange string   `json:"recommended_change"`
	Scope             []string `json:"scope"`
	FollowUp          []string `json:"follow_up"`
}

type roomMilestoneProposalMeta struct {
	EpicID    string   `json:"epic_id"`
	Goal      string   `json:"goal"`
	Scope     []string `json:"scope"`
	Rationale string   `json:"rationale"`
}

func runRoomEpicStart(cmd *cobra.Command, workspace, sender, roomID, title, goal, owner, outcome, horizon string, scope, success []string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.epic", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.start", protocol.ErrorCodeEARG, "agile scope changes require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	title = strings.TrimSpace(title)
	if title == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.start", protocol.ErrorCodeEARG, "title is required", map[string]any{
			"hint": "Pass a concise epic title such as `room-agile-protocol` or `delivery-ledger-hardening`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		Stream:      agent.RoomStreamName(roomID),
		Sender:      identity.Sender,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindEpic,
		Priority:    agent.DefaultPriority,
		Subject:     "Epic: " + title,
		Body:        buildRoomEpicBody(title, goal, owner, outcome, horizon, scope, success),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.start", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, msg.ID); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.start", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.epic.start", map[string]any{
		"room_id":         roomID,
		"epic_id":         msg.ID,
		"message":         msg,
		"sender_identity": identity,
		"room":            summary,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomEpicAsk(cmd *cobra.Command, workspace, sender, roomID, epicID, recipient, questionKind, question string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.epic", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.ask", protocol.ErrorCodeEARG, "epic intake questions require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epicMsg, epicMeta, err := loadRoomEpic(cmd.Context(), store, absWorkspace, roomID, epicID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.ask", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Create an epic first with `agentctl room epic start` and reuse its epic_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if epicIsFinalized(cmd.Context(), store, absWorkspace, roomID, epicMsg.ID) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.ask", protocol.ErrorCodeEARG, "epic intake is already finalized", map[string]any{
			"hint": "Start a new epic or continue milestone work instead of reopening finalized intake via epic ask.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	recipient = firstNonEmpty(strings.TrimSpace(recipient), strings.TrimSpace(epicMeta.Owner))
	if recipient == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.ask", protocol.ErrorCodeEARG, "recipient is required", map[string]any{
			"hint": "Set --to or start the epic with --owner so intake questions have a default respondent.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.ask", protocol.ErrorCodeEARG, "question is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	questionKind, err = normalizeRoomEpicQuestionKind(questionKind)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.ask", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use one of: product, technical, constraint, success.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: epicMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        recipient,
		Kind:             agent.BoardMessageKindEpicQuestion,
		Priority:         agent.DefaultPriority,
		ReplyExpected:    true,
		Subject:          "Epic Question (" + questionKind + "): " + deriveRoomSubject(question),
		Body:             buildRoomEpicQuestionBody(questionKind, question),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.ask", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.epic.ask", map[string]any{
		"room_id": roomID,
		"epic_id": epicMsg.ID,
		"message": msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomEpicAnswer(cmd *cobra.Command, workspace, sender, roomID, questionID, answer string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.epic", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()

	questionMsg, epicMsg, _, err := loadRoomEpicQuestion(cmd.Context(), store, absWorkspace, roomID, questionID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.answer", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Use a question id returned by `agentctl room epic ask` or listed in `agentctl room epic show`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if questionMsg.Recipient != "" && questionMsg.Recipient != agent.BroadcastRecipient && !sameRoomParticipant(questionMsg.Recipient, identity.Sender) && !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.answer", protocol.ErrorCodeEARG, "only the intended respondent or coordinator can answer this epic question", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.answer", protocol.ErrorCodeEARG, "answer is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: questionMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        epicMsg.Sender,
		Kind:             agent.BoardMessageKindEpicAnswer,
		Priority:         agent.DefaultPriority,
		Subject:          "Epic Answer: " + deriveRoomSubject(answer),
		Body:             answer,
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.answer", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.epic.answer", map[string]any{
		"room_id":     roomID,
		"epic_id":     epicMsg.ID,
		"question_id": questionMsg.ID,
		"message":     msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomEpicFinalize(cmd *cobra.Command, workspace, sender, roomID, epicID, summaryText string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.epic", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.finalize", protocol.ErrorCodeEARG, "epic finalization requires coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epicMsg, _, err := loadRoomEpic(cmd.Context(), store, absWorkspace, roomID, epicID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.finalize", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Create an epic first with `agentctl room epic start` and reuse its epic_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if epicIsFinalized(cmd.Context(), store, absWorkspace, roomID, epicMsg.ID) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.finalize", protocol.ErrorCodeEARG, "epic is already finalized", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if openQuestions, err := countOpenEpicQuestions(cmd.Context(), store, absWorkspace, roomID, epicMsg.ID); err == nil && openQuestions > 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.finalize", protocol.ErrorCodeEARG, "cannot finalize epic while intake questions remain open", map[string]any{
			"open_questions": openQuestions,
			"hint":           "Answer or resolve the remaining epic intake questions before finalizing the epic.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	} else if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.finalize", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	summaryText = strings.TrimSpace(summaryText)
	if summaryText == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.finalize", protocol.ErrorCodeEARG, "summary is required", map[string]any{
			"hint": "Capture the clarified epic brief and major delivery shape before opening milestones.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: epicMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindEpicFinalize,
		Priority:         agent.DefaultPriority,
		Subject:          "Epic Finalized: " + strings.TrimPrefix(strings.TrimSpace(epicMsg.Subject), "Epic: "),
		Body:             summaryText,
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.finalize", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, epicMsg.ID); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.finalize", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.epic.finalize", map[string]any{
		"room_id": roomID,
		"epic_id": epicMsg.ID,
		"message": msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomEpicShape(cmd *cobra.Command, workspace, sender, roomID, epicID string, count int) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.epic", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.shape", protocol.ErrorCodeEARG, "epic shaping requires coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epicMsg, epicMeta, err := loadRoomEpic(cmd.Context(), store, absWorkspace, roomID, epicID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.shape", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Create an epic first with `agentctl room epic start` and reuse its epic_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !epicIsFinalized(cmd.Context(), store, absWorkspace, roomID, epicMsg.ID) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.shape", protocol.ErrorCodeEARG, "epic shaping requires a finalized epic", map[string]any{
			"hint": "Run the intake loop and `agentctl room epic finalize` before shaping milestones.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if count <= 0 {
		count = 3
	}

	messages, err := store.ListRoomMessages(cmd.Context(), absWorkspace, roomID, roomTaskScanLimit)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.shape", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	brief, qa := deriveRoomEpicShapingInputs(messages, epicMsg.ID)
	proposals := deriveRoomMilestoneProposals(epicMsg, epicMeta, brief, qa, count)
	if len(proposals) == 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.shape", protocol.ErrorCodeEARG, "could not derive milestone proposals from epic data", map[string]any{
			"hint": "Add epic scope or success signals before shaping milestones.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	written := make([]*agent.BoardMessage, 0, len(proposals))
	for _, proposal := range proposals {
		msg := &agent.BoardMessage{
			WorkspaceID:      absWorkspace,
			RelatedMessageID: epicMsg.ID,
			Stream:           agent.RoomStreamName(roomID),
			Sender:           identity.Sender,
			Recipient:        agent.BroadcastRecipient,
			Kind:             agent.BoardMessageKindMilestoneProposal,
			Priority:         agent.DefaultPriority,
			Subject:          "Milestone Proposal: " + proposal.Title,
			Body:             buildRoomMilestoneProposalBody(epicMsg.ID, proposal.Goal, proposal.Scope, proposal.Rationale),
		}
		if err := store.SendMessage(cmd.Context(), msg); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.shape", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		written = append(written, msg)
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.epic.shape", map[string]any{
		"room_id":   roomID,
		"epic_id":   epicMsg.ID,
		"count":     len(written),
		"proposals": written,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomEpicShow(cmd *cobra.Command, workspace, roomID, epicID string, limit int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.show", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.show", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epics := buildRoomEpicViews(messages)
	if strings.TrimSpace(epicID) == "" {
		return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.epic.show", map[string]any{
			"room":  summary,
			"count": len(epics),
			"epics": epics,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	for _, epic := range epics {
		if epic["id"] == strings.TrimSpace(epicID) {
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.epic.show", map[string]any{
				"room": summary,
				"epic": epic,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.show", protocol.ErrorCodeENotFound, fmt.Sprintf("epic %q not found", epicID), map[string]any{
		"hint": "Run `agentctl room epic show <room-id>` to list available epics.",
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomEpicResume(cmd *cobra.Command, workspace, roomID, epicID string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.resume", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", roomTaskScanLimit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.resume", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epic := roomEpicViewByID(buildRoomEpicViews(messages), epicID)
	if epic == nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.resume", protocol.ErrorCodeENotFound, fmt.Sprintf("epic %q not found", epicID), map[string]any{
			"hint": "Run `agentctl room epic show <room-id>` to list available epics.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	resume := buildRoomEpicContinuity(summary, messages, epic)
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.epic.resume", map[string]any{
		"room":   summary,
		"epic":   epic,
		"resume": resume,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomEpicHealth(cmd *cobra.Command, workspace, roomID, epicID, actorID string, limit int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	if limit <= 0 {
		limit = roomTaskScanLimit
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.health", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.health", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epic := roomEpicViewByID(buildRoomEpicViews(messages), epicID)
	if epic == nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.health", protocol.ErrorCodeENotFound, fmt.Sprintf("epic %q not found", epicID), map[string]any{
			"hint": "Run `agentctl room epic show <room-id>` to list available epics.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	health := buildRoomEpicHealth(summary, messages, epic, actorID)
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.epic.health", map[string]any{
		"room":   summary,
		"epic":   epic,
		"health": health,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomEpicNext(cmd *cobra.Command, workspace, roomID, epicID, actorID string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.next", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", roomTaskScanLimit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.next", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epic := roomEpicViewByID(buildRoomEpicViews(messages), epicID)
	if epic == nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.epic.next", protocol.ErrorCodeENotFound, fmt.Sprintf("epic %q not found", epicID), map[string]any{
			"hint": "Run `agentctl room epic show <room-id>` to list available epics.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	resume := buildRoomEpicContinuity(summary, messages, epic)
	laneActor := strings.TrimSpace(actorID)
	lane := "coordinator"
	if laneActor == "" {
		laneActor = roomCoordinatorActorID(summary.Members)
		if laneActor == "" {
			laneActor = "coordinator"
		}
	} else {
		lane = laneActor
	}
	items, reason := buildRoomEpicNextItems(summary, messages, epic, resume, laneActor)
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.epic.next", map[string]any{
		"room":    summary,
		"epic_id": epicID,
		"actor":   laneActor,
		"lane":    lane,
		"items":   items,
		"reason":  reason,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomMilestoneStart(cmd *cobra.Command, workspace, sender, roomID, epicID, title, goal, objective, owner string, scope, risks, exclusions, dependencies, validatorsExpected, exitCriteria []string, proposalID string) error {
	return runRoomMilestoneStartWithPolicy(cmd, workspace, sender, roomID, epicID, title, goal, objective, owner, scope, risks, exclusions, dependencies, validatorsExpected, nil, nil, exitCriteria, proposalID)
}

func runRoomMilestoneStartWithPolicy(cmd *cobra.Command, workspace, sender, roomID, epicID, title, goal, objective, owner string, scope, risks, exclusions, dependencies, validatorsExpected, requiredEvidenceLanes, optionalEvidenceLanes, exitCriteria []string, proposalID string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.milestone", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.start", protocol.ErrorCodeEARG, "agile scope changes require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epicMsg, epicMeta, err := loadRoomEpic(cmd.Context(), store, absWorkspace, roomID, epicID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.start", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Create an epic first with `agentctl room epic start` and reuse its epic_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !epicIsFinalized(cmd.Context(), store, absWorkspace, roomID, epicMsg.ID) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.start", protocol.ErrorCodeEARG, "milestones require a finalized epic", map[string]any{
			"hint": "Use `agentctl room epic ask`, `agentctl room epic answer`, and `agentctl room epic finalize` before opening milestones.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	proposalID = strings.TrimSpace(proposalID)
	if proposalID != "" {
		proposalMsg, proposalMeta, err := loadRoomMilestoneProposal(cmd.Context(), store, absWorkspace, roomID, proposalID)
		if err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.start", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
				"hint": "Use a proposal id returned by `agentctl room epic shape` or visible in `agentctl room epic show`.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if strings.TrimSpace(proposalMeta.EpicID) != epicMsg.ID {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.start", protocol.ErrorCodeEARG, "proposal does not belong to this epic", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		title = firstNonEmpty(strings.TrimSpace(title), strings.TrimPrefix(strings.TrimSpace(proposalMsg.Subject), "Milestone Proposal: "))
		goal = firstNonEmpty(strings.TrimSpace(goal), strings.TrimSpace(proposalMeta.Goal))
		owner = firstNonEmpty(strings.TrimSpace(owner), strings.TrimSpace(epicMeta.Owner))
		if len(scope) == 0 {
			scope = append([]string(nil), proposalMeta.Scope...)
		}
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.start", protocol.ErrorCodeEARG, "title is required", map[string]any{
			"hint": "Pass a milestone title directly, or use --proposal <proposal-id> to promote a shaped milestone proposal.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	meta, err := normalizeRoomMilestoneContract(roomMilestoneMeta{
		EpicID:                epicMsg.ID,
		Goal:                  goal,
		Objective:             objective,
		Owner:                 firstNonEmpty(strings.TrimSpace(owner), strings.TrimSpace(epicMeta.Owner)),
		Scope:                 scope,
		Risks:                 risks,
		Exclusions:            exclusions,
		Dependencies:          dependencies,
		ValidatorsExpected:    validatorsExpected,
		RequiredEvidenceLanes: requiredEvidenceLanes,
		OptionalEvidenceLanes: optionalEvidenceLanes,
		ExitCriteria:          exitCriteria,
	})
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.start", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use supported validator values: review, test, integration, user_test, manual_check, audit.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: epicMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindMilestone,
		Priority:         agent.DefaultPriority,
		Subject:          "Milestone: " + title,
		Body:             buildRoomMilestoneBody(epicMsg.ID, title, meta.Goal, meta.Objective, meta.Owner, meta.Scope, meta.Risks, meta.Exclusions, meta.Dependencies, meta.ValidatorsExpected, meta.RequiredEvidenceLanes, meta.OptionalEvidenceLanes, meta.ExitCriteria),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.start", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, epicMsg.ID); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.start", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.milestone.start", map[string]any{
		"room_id":         roomID,
		"epic_id":         epicMsg.ID,
		"milestone_id":    msg.ID,
		"message":         msg,
		"sender_identity": identity,
		"room":            summary,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomMilestoneContract(cmd *cobra.Command, workspace, sender, roomID, milestoneID, objective string, risks, exclusions, dependencies, validatorsExpected, exitCriteria []string) error {
	return runRoomMilestoneContractWithPolicy(cmd, workspace, sender, roomID, milestoneID, objective, risks, exclusions, dependencies, validatorsExpected, nil, nil, exitCriteria)
}

func runRoomMilestoneContractWithPolicy(cmd *cobra.Command, workspace, sender, roomID, milestoneID, objective string, risks, exclusions, dependencies, validatorsExpected, requiredEvidenceLanes, optionalEvidenceLanes, exitCriteria []string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.milestone", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.contract", protocol.ErrorCodeEARG, "agile scope changes require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	milestoneMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, milestoneID, agent.BoardMessageKindMilestone)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.contract", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Use a milestone id returned by `agentctl room milestone start` or listed in `agentctl room milestone show`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	patch, err := normalizeRoomMilestoneContract(roomMilestoneMeta{
		Objective:             objective,
		Risks:                 risks,
		Exclusions:            exclusions,
		Dependencies:          dependencies,
		ValidatorsExpected:    validatorsExpected,
		RequiredEvidenceLanes: requiredEvidenceLanes,
		OptionalEvidenceLanes: optionalEvidenceLanes,
		ExitCriteria:          exitCriteria,
	})
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.contract", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use supported validator values: review, test, integration, user_test, manual_check, audit.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if patch.Objective == "" && len(patch.Risks) == 0 && len(patch.Exclusions) == 0 && len(patch.Dependencies) == 0 && len(patch.ValidatorsExpected) == 0 && len(patch.RequiredEvidenceLanes) == 0 && len(patch.OptionalEvidenceLanes) == 0 && len(patch.ExitCriteria) == 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.contract", protocol.ErrorCodeEARG, "at least one contract field is required", map[string]any{
			"hint": "Pass --objective, --risk, --exclude, --dependency, --validator, --required-lane, --optional-lane, or --exit.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: milestoneMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindMilestoneContract,
		Priority:         agent.DefaultPriority,
		Subject:          "Milestone Contract: " + strings.TrimPrefix(strings.TrimSpace(milestoneMsg.Subject), "Milestone: "),
		Body:             buildRoomMilestoneBody("", "", "", patch.Objective, "", nil, patch.Risks, patch.Exclusions, patch.Dependencies, patch.ValidatorsExpected, patch.RequiredEvidenceLanes, patch.OptionalEvidenceLanes, patch.ExitCriteria),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.contract", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if milestoneMeta := parseRoomMilestoneBody(milestoneMsg.Body); strings.TrimSpace(milestoneMeta.EpicID) != "" {
		if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, milestoneMeta.EpicID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.contract", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.milestone.contract", map[string]any{
		"room_id":      roomID,
		"milestone_id": milestoneMsg.ID,
		"message":      msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomMilestoneCriteria(cmd *cobra.Command, workspace, sender, roomID, milestoneID, criterion string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.milestone", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.criteria", protocol.ErrorCodeEARG, "agile scope changes require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	milestoneMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, milestoneID, agent.BoardMessageKindMilestone)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.criteria", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Start a milestone first with `agentctl room milestone start` and reuse its milestone_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	criterion = strings.TrimSpace(criterion)
	if criterion == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.criteria", protocol.ErrorCodeEARG, "criterion is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: milestoneMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindAcceptanceCriteria,
		Priority:         agent.DefaultPriority,
		Subject:          "Acceptance Criteria: " + deriveRoomSubject(criterion),
		Body:             criterion,
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.criteria", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if milestoneMeta := parseRoomMilestoneBody(milestoneMsg.Body); strings.TrimSpace(milestoneMeta.EpicID) != "" {
		if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, milestoneMeta.EpicID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.criteria", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.milestone.criteria", map[string]any{
		"room_id":      roomID,
		"milestone_id": milestoneMsg.ID,
		"message":      msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomMilestoneReview(cmd *cobra.Command, workspace, sender, roomID, milestoneID, verdict, notes string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.milestone", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.review", protocol.ErrorCodeEARG, "agile scope changes require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	milestoneMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, milestoneID, agent.BoardMessageKindMilestone)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.review", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Use a milestone id returned by `agentctl room milestone start` or listed in `agentctl room milestone show`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	verdict = strings.TrimSpace(strings.ToLower(verdict))
	switch verdict {
	case "pass", "block":
	default:
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.review", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported milestone verdict %q", verdict), map[string]any{
			"hint": "Use pass or block.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.review", protocol.ErrorCodeEARG, "notes are required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: milestoneMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindMilestoneReview,
		Priority:         agent.DefaultPriority,
		Subject:          "Milestone Review: " + verdict,
		Body:             notes,
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.review", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if milestoneMeta := parseRoomMilestoneBody(milestoneMsg.Body); strings.TrimSpace(milestoneMeta.EpicID) != "" {
		if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, milestoneMeta.EpicID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.review", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.milestone.review", map[string]any{
		"room_id":      roomID,
		"milestone_id": milestoneMsg.ID,
		"message":      msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomMilestoneSummary(cmd *cobra.Command, workspace, sender, roomID, milestoneID, notes, summaryText string, passedCriteria, failedCriteria, waivedValidationIDs, blockingValidationIDs, notableDecisions, systemicFindings, recommendedNext, guidanceUpdates []string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.milestone", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.summary", protocol.ErrorCodeEARG, "agile scope changes require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	milestoneMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, milestoneID, agent.BoardMessageKindMilestone)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.summary", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Use a milestone id returned by `agentctl room milestone start` or listed in `agentctl room milestone show`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	effectiveSummary := strings.TrimSpace(summaryText)
	if effectiveSummary == "" {
		effectiveSummary = strings.TrimSpace(notes)
	}
	meta := normalizeRoomMilestoneSummaryMeta(roomMilestoneSummaryMeta{
		Summary:               effectiveSummary,
		PassedCriteria:        passedCriteria,
		FailedCriteria:        failedCriteria,
		WaivedValidationIDs:   waivedValidationIDs,
		BlockingValidationIDs: blockingValidationIDs,
		NotableDecisions:      notableDecisions,
		SystemicFindings:      systemicFindings,
		RecommendedNext:       recommendedNext,
		GuidanceUpdates:       guidanceUpdates,
	})
	if meta.Summary == "" && len(meta.PassedCriteria) == 0 && len(meta.FailedCriteria) == 0 && len(meta.WaivedValidationIDs) == 0 && len(meta.BlockingValidationIDs) == 0 && len(meta.NotableDecisions) == 0 && len(meta.SystemicFindings) == 0 && len(meta.RecommendedNext) == 0 && len(meta.GuidanceUpdates) == 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.summary", protocol.ErrorCodeEARG, "summary details are required", map[string]any{
			"hint": "Pass positional notes or --summary, and optionally add structured synthesis flags like --passed-criterion or --decision.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	messages, err := store.ListRoomMessages(cmd.Context(), absWorkspace, roomID, roomTaskScanLimit)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.summary", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	milestone := roomMilestoneViewByID(buildRoomMilestoneViews(messages), milestoneMsg.ID)
	if milestone == nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.summary", protocol.ErrorCodeENotFound, fmt.Sprintf("milestone %q not found in room view", milestoneMsg.ID), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	allowedWaivedValidationIDs := make(map[string]struct{})
	allowedBlockingValidationIDs := make(map[string]struct{})
	for _, story := range mapSlice(milestone["stories"]) {
		for _, validation := range mapSlice(story["validations"]) {
			id := stringField(validation, "id")
			if id == "" {
				continue
			}
			switch stringField(validation, "status") {
			case "waived":
				allowedWaivedValidationIDs[id] = struct{}{}
			}
		}
	}
	for _, id := range stringSliceValue(milestone["blocking_validation_ids"]) {
		if id != "" {
			allowedBlockingValidationIDs[id] = struct{}{}
		}
	}
	for _, id := range meta.WaivedValidationIDs {
		if _, ok := allowedWaivedValidationIDs[id]; !ok {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.summary", protocol.ErrorCodeEARG, fmt.Sprintf("validation id %q is not attached to this milestone", id), map[string]any{
				"hint": "Reference only waived story validation ids currently attached to accepted stories in this milestone.",
				"id":   id,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	for _, id := range meta.BlockingValidationIDs {
		if _, ok := allowedBlockingValidationIDs[id]; !ok {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.summary", protocol.ErrorCodeEARG, fmt.Sprintf("validation id %q is not a current blocking validation for this milestone", id), map[string]any{
				"hint": "Reference only current blocking validation ids surfaced by the milestone rollup.",
				"id":   id,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: milestoneMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindMilestoneSummary,
		Priority:         agent.DefaultPriority,
		Subject:          "Milestone Summary: " + strings.TrimPrefix(strings.TrimSpace(milestoneMsg.Subject), "Milestone: "),
		Body:             buildRoomMilestoneSummaryBody(meta),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.summary", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if milestoneMeta := parseRoomMilestoneBody(milestoneMsg.Body); strings.TrimSpace(milestoneMeta.EpicID) != "" {
		if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, milestoneMeta.EpicID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.summary", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.milestone.summary", map[string]any{
		"room_id":      roomID,
		"milestone_id": milestoneMsg.ID,
		"message":      msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomMilestoneShow(cmd *cobra.Command, workspace, roomID, milestoneID string, limit int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.show", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.show", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	milestones := buildRoomMilestoneViews(messages)
	if strings.TrimSpace(milestoneID) == "" {
		return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.milestone.show", map[string]any{
			"room":       summary,
			"count":      len(milestones),
			"milestones": milestones,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	for _, milestone := range milestones {
		if milestone["id"] == strings.TrimSpace(milestoneID) {
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.milestone.show", map[string]any{
				"room":      summary,
				"milestone": milestone,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.milestone.show", protocol.ErrorCodeENotFound, fmt.Sprintf("milestone %q not found", milestoneID), map[string]any{
		"hint": "Run `agentctl room milestone show <room-id>` to list available milestones.",
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomStoryAdd(cmd *cobra.Command, workspace, sender, roomID, milestoneID, title, body, owner string) error {
	absWorkspace, identity, store, roomID, _, err := prepareRoomAgileCommand(cmd, "agentctl.room.story", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()

	milestoneMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, milestoneID, agent.BoardMessageKindMilestone)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.add", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Create a milestone first with `agentctl room milestone start` and reuse its milestone_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" || body == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.add", protocol.ErrorCodeEARG, "title and body are required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: milestoneMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStory,
		Priority:         agent.DefaultPriority,
		Subject:          "Story: " + title,
		Body:             buildRoomStoryBody(owner, body),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if milestoneMeta := parseRoomMilestoneBody(milestoneMsg.Body); strings.TrimSpace(milestoneMeta.EpicID) != "" {
		if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, milestoneMeta.EpicID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.story.add", map[string]any{
		"room_id":      roomID,
		"milestone_id": milestoneMsg.ID,
		"story_id":     msg.ID,
		"message":      msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomStoryPropose(cmd *cobra.Command, workspace, sender, roomID, milestoneID, title, body, owner, rationale string) error {
	absWorkspace, identity, store, roomID, _, err := prepareRoomAgileCommand(cmd, "agentctl.room.story", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()

	milestoneMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, milestoneID, agent.BoardMessageKindMilestone)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.propose", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Create a milestone first with `agentctl room milestone start` and reuse its milestone_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" || body == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.propose", protocol.ErrorCodeEARG, "title and body are required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: milestoneMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStoryProposal,
		Priority:         agent.DefaultPriority,
		Subject:          "Story Proposal: " + title,
		Body:             buildRoomStoryProposalBody(milestoneMsg.ID, owner, body, rationale),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.propose", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.story.propose", map[string]any{
		"room_id":      roomID,
		"milestone_id": milestoneMsg.ID,
		"proposal_id":  msg.ID,
		"message":      msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomStoryAccept(cmd *cobra.Command, workspace, sender, roomID, milestoneID, proposalID, owner string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.story", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.accept", protocol.ErrorCodeEARG, "agile scope changes require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	milestoneMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, milestoneID, agent.BoardMessageKindMilestone)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.accept", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Create a milestone first with `agentctl room milestone start` and reuse its milestone_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	proposalMsg, proposalMeta, err := loadRoomStoryProposal(cmd.Context(), store, absWorkspace, roomID, proposalID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.accept", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Use a proposal id returned by `agentctl room story propose` or listed in `agentctl room story show`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if strings.TrimSpace(proposalMeta.MilestoneID) != milestoneMsg.ID {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.accept", protocol.ErrorCodeEARG, "story proposal does not belong to this milestone", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return runRoomStoryAdd(cmd, workspace, identity.Sender, roomID, milestoneID, strings.TrimPrefix(strings.TrimSpace(proposalMsg.Subject), "Story Proposal: "), proposalMeta.Description, firstNonEmpty(strings.TrimSpace(owner), strings.TrimSpace(proposalMeta.Owner)))
}

func runRoomStoryState(cmd *cobra.Command, workspace, sender, roomID, storyID, state, reason, blockedBy, reviewer string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.story", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()

	storyMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, storyID, agent.BoardMessageKindStory)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Accept or add the story first with `agentctl room story accept` or `agentctl room story add`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	storyMeta := parseRoomStoryBody(storyMsg.Body)
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		if owner := strings.TrimSpace(storyMeta.Owner); owner == "" || !sameRoomParticipant(owner, identity.Sender) {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeEARG, "story lifecycle changes require the story owner or coordinator", map[string]any{
				"hint": "Use the story owner account, or run the command as the room coordinator.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	state, err = normalizeRoomStoryState(state)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use one of: proposed, accepted, in_progress, in_review, validated, blocked, waived, done, deferred.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	reason = strings.TrimSpace(reason)
	blockedBy = strings.TrimSpace(blockedBy)
	reviewer = strings.TrimSpace(reviewer)
	switch state {
	case "blocked", "deferred":
		if reason == "" {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeEARG, fmt.Sprintf("%s stories require a reason", state), map[string]any{
				"hint": "Pass --reason to explain why the story is blocked or deferred.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}

	messages, err := store.ListRoomMessages(cmd.Context(), absWorkspace, roomID, roomTaskScanLimit)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	story := roomStoryViewByID(buildRoomStoryViews(messages), storyMsg.ID)
	if story == nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeENotFound, fmt.Sprintf("story %q not found in room view", storyMsg.ID), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	latestValidationStatus := stringField(story, "latest_validation_status")
	switch state {
	case "validated":
		if latestValidationStatus != "pass" {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeEARG, "validated story state requires the latest story validation status to be pass", map[string]any{
				"hint": "Record a passing story validation first, or leave the story in a non-terminal execution state.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	case "waived":
		if latestValidationStatus != "waived" {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeEARG, "waived story state requires the latest story validation status to be waived", map[string]any{
				"hint": "Record a waived story validation first so the lifecycle state and validation ledger do not contradict each other.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	case "done":
		if latestValidationStatus != "pass" && latestValidationStatus != "waived" {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeEARG, "done stories require the latest validation status to be pass or waived", map[string]any{
				"hint": "Validate or explicitly waive the story before marking it done.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: storyMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStoryState,
		Priority:         agent.DefaultPriority,
		Subject:          fmt.Sprintf("Story State (%s): %s", state, strings.TrimPrefix(strings.TrimSpace(storyMsg.Subject), "Story: ")),
		Body:             buildRoomStoryStateBody(state, reason, blockedBy, reviewer),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if milestoneID := strings.TrimSpace(storyMsg.RelatedMessageID); milestoneID != "" {
		if milestoneMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, milestoneID, agent.BoardMessageKindMilestone); err == nil {
			if milestoneMeta := parseRoomMilestoneBody(milestoneMsg.Body); strings.TrimSpace(milestoneMeta.EpicID) != "" {
				if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, milestoneMeta.EpicID); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.state", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
			}
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.story.state", map[string]any{
		"room_id":  roomID,
		"story_id": storyMsg.ID,
		"message":  msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomStoryValidate(cmd *cobra.Command, workspace, sender, roomID, storyID, validatorType, status, summaryText, artifactPath, artifactDigest, commandText, notes string, relatedStoryIDs []string) error {
	absWorkspace, identity, store, roomID, _, err := prepareRoomAgileCommand(cmd, "agentctl.room.story", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()

	storyMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, storyID, agent.BoardMessageKindStory)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Add or accept the story first with `agentctl room story add` or `agentctl room story accept`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	milestoneMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, storyMsg.RelatedMessageID, agent.BoardMessageKindMilestone)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "The story must belong to a valid milestone before validation can be recorded.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	milestoneMeta := parseRoomMilestoneBody(milestoneMsg.Body)
	storyMeta := parseRoomStoryBody(storyMsg.Body)
	validatorType, err = normalizeRoomStoryValidatorType(validatorType)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use one of: review, test, integration, user_test, manual_check, audit.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	status, err = normalizeRoomStoryValidationStatus(status)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use one of: pass, fail, blocked, waived.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	summaryText = strings.TrimSpace(summaryText)
	if summaryText == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeEARG, "summary is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	artifactPath = strings.TrimSpace(artifactPath)
	artifactDigest = strings.TrimSpace(artifactDigest)
	commandText = strings.TrimSpace(commandText)
	notes = strings.TrimSpace(notes)
	if artifactDigest != "" && artifactPath == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeEARG, "artifact-digest requires artifact-path", map[string]any{
			"hint": "Pass --artifact-path alongside --artifact-digest so the validation record can point to the concrete artifact source.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if status == "waived" {
		if !roomMemberHasRoleFromRoomState(cmd.Context(), store, absWorkspace, roomID, identity.Sender, "coordinator") {
			if !sameRoomParticipant(firstNonEmpty(strings.TrimSpace(storyMeta.Owner), identity.Sender), identity.Sender) || strings.TrimSpace(storyMeta.Owner) == "" {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeEARG, "waived validations require the story owner or coordinator", map[string]any{
					"hint": "Use the story owner account or the room coordinator when recording a waiver.",
				}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
		}
		if notes == "" {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeEARG, "waived validations require waiver notes", map[string]any{
				"hint": "Pass --notes with the explicit waiver reason when status is waived.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	normalizedRelatedStoryIDs, err := validateRoomRelatedStoryIDs(cmd.Context(), store, absWorkspace, roomID, milestoneMeta.EpicID, milestoneMsg.ID, storyMsg.ID, relatedStoryIDs)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Related story ids must resolve to stories in the same milestone and epic.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: storyMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindStoryValidation,
		Priority:         agent.DefaultPriority,
		Subject:          fmt.Sprintf("Story Validation (%s/%s): %s", validatorType, status, strings.TrimPrefix(strings.TrimSpace(storyMsg.Subject), "Story: ")),
		Body:             buildRoomStoryValidationBody(milestoneMeta.EpicID, milestoneMsg.ID, storyMsg.ID, validatorType, status, summaryText, artifactPath, artifactDigest, commandText, notes, normalizedRelatedStoryIDs),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if strings.TrimSpace(milestoneMeta.EpicID) != "" {
		if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, milestoneMeta.EpicID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.validate", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.story.validate", map[string]any{
		"room_id":       roomID,
		"epic_id":       milestoneMeta.EpicID,
		"milestone_id":  milestoneMsg.ID,
		"story_id":      storyMsg.ID,
		"validation_id": msg.ID,
		"message":       msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomStoryShow(cmd *cobra.Command, workspace, roomID, storyID string, limit int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.show", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.show", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	stories := buildRoomStoryViews(messages)
	if strings.TrimSpace(storyID) == "" {
		return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.story.show", map[string]any{
			"room":    summary,
			"count":   len(stories),
			"stories": stories,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	for _, story := range stories {
		if story["id"] == strings.TrimSpace(storyID) {
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.story.show", map[string]any{
				"room":  summary,
				"story": story,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.story.show", protocol.ErrorCodeENotFound, fmt.Sprintf("story %q not found", storyID), map[string]any{
		"hint": "Run `agentctl room story show <room-id>` to list available stories.",
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomLogAppend(cmd *cobra.Command, workspace, sender, roomID, epicID, label string, completed, inFlight, blockers, nextFocus []string, notes string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.log", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.log.append", protocol.ErrorCodeEARG, "agile scope changes require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epicMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, epicID, agent.BoardMessageKindEpic)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.log.append", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Create an epic first with `agentctl room epic start` and reuse its epic_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.log.append", protocol.ErrorCodeEARG, "label is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: epicMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindDeliveryLog,
		Priority:         agent.DefaultPriority,
		Subject:          "Delivery Log: " + label,
		Body:             buildRoomDeliveryLogBody(label, completed, inFlight, blockers, nextFocus, notes),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.log.append", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, epicMsg.ID); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.log.append", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.log.append", map[string]any{
		"room_id": roomID,
		"epic_id": epicMsg.ID,
		"log_id":  msg.ID,
		"message": msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomLogShow(cmd *cobra.Command, workspace, roomID, epicID string, limit int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.log.show", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.log.show", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	logs := buildRoomDeliveryLogViews(messages)
	filtered := make([]map[string]any, 0, len(logs))
	for _, entry := range logs {
		if entry["epic_id"] == strings.TrimSpace(epicID) {
			filtered = append(filtered, entry)
		}
	}
	if len(filtered) == 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.log.show", protocol.ErrorCodeENotFound, fmt.Sprintf("delivery log for epic %q not found", epicID), map[string]any{
			"hint": "Append a log entry with `agentctl room log append` after creating the epic.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.log.show", map[string]any{
		"room":    summary,
		"epic_id": strings.TrimSpace(epicID),
		"count":   len(filtered),
		"entries": filtered,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomRetroAdd(cmd *cobra.Command, workspace, sender, roomID, epicID, milestoneID, kind, summaryText, impact, recommendedChange string, scope, followUp []string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomAgileCommand(cmd, "agentctl.room.retro", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.add", protocol.ErrorCodeEARG, "agile scope changes require coordinator role", map[string]any{
			"hint": "Run the command as the room coordinator, or join the room with role=coordinator first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epicMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, epicID, agent.BoardMessageKindEpic)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.add", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Create an epic first with `agentctl room epic start` and reuse its epic_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	kind, err = normalizeRoomGuidanceKind(kind)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.add", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use one of: process, tooling, coordination, quality, delivery.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	meta := normalizeRoomGuidanceUpdateMeta(roomGuidanceUpdateMeta{
		EpicID:            epicMsg.ID,
		MilestoneID:       strings.TrimSpace(milestoneID),
		Kind:              kind,
		Summary:           summaryText,
		Impact:            impact,
		RecommendedChange: recommendedChange,
		Scope:             scope,
		FollowUp:          followUp,
	})
	if meta.Summary == "" || meta.Impact == "" || meta.RecommendedChange == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.add", protocol.ErrorCodeEARG, "summary, impact, and change are required", map[string]any{
			"hint": "Pass --summary, --impact, and --change to capture a usable retro update.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if meta.MilestoneID != "" {
		milestoneMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, meta.MilestoneID, agent.BoardMessageKindMilestone)
		if err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.add", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
				"hint": "Use a milestone id returned by `agentctl room milestone start` or listed in `agentctl room milestone show`.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		milestoneMeta := parseRoomMilestoneBody(milestoneMsg.Body)
		if strings.TrimSpace(milestoneMeta.EpicID) != epicMsg.ID {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.add", protocol.ErrorCodeEARG, "milestone does not belong to this epic", map[string]any{
				"hint": "Use a milestone id from the same epic, or omit --milestone for epic-wide guidance.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}

	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: epicMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        agent.BroadcastRecipient,
		Kind:             agent.BoardMessageKindGuidanceUpdate,
		Priority:         agent.DefaultPriority,
		Subject:          fmt.Sprintf("Retro (%s): %s", meta.Kind, meta.Summary),
		Body:             buildRoomGuidanceUpdateBody(meta),
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, epicMsg.ID); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.retro.add", map[string]any{
		"room_id":      roomID,
		"epic_id":      epicMsg.ID,
		"milestone_id": meta.MilestoneID,
		"update_id":    msg.ID,
		"message":      msg,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomRetroShow(cmd *cobra.Command, workspace, roomID, epicID, milestoneID string, limit int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.show", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.show", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	epic := roomEpicViewByID(buildRoomEpicViews(messages), epicID)
	if epic == nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.retro.show", protocol.ErrorCodeENotFound, fmt.Sprintf("epic %q not found", epicID), map[string]any{
			"hint": "Run `agentctl room epic show <room-id>` to list available epics.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	updates := mapSlice(epic["guidance_updates"])
	filtered := make([]map[string]any, 0, len(updates))
	for _, update := range updates {
		if milestoneID != "" && stringField(anyMap(update["meta"]), "milestone_id") != strings.TrimSpace(milestoneID) {
			continue
		}
		filtered = append(filtered, update)
	}
	groups := make([]map[string]any, 0)
	for _, kind := range []string{"coordination", "delivery", "process", "quality", "tooling"} {
		groupUpdates := make([]map[string]any, 0)
		for _, update := range filtered {
			if stringField(anyMap(update["meta"]), "kind") == kind {
				groupUpdates = append(groupUpdates, update)
			}
		}
		if len(groupUpdates) == 0 {
			continue
		}
		groups = append(groups, map[string]any{
			"kind":    kind,
			"count":   len(groupUpdates),
			"updates": groupUpdates,
		})
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.retro.show", map[string]any{
		"room":         summary,
		"epic":         epic,
		"epic_id":      strings.TrimSpace(epicID),
		"milestone_id": strings.TrimSpace(milestoneID),
		"count":        len(filtered),
		"updates":      filtered,
		"groups":       groups,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

type roomACAPromotionPrepared struct {
	EpicID     string
	TargetKind string
	TargetID   string
	Source     map[string]any
	Input      contextplane.MarkdownProposalInput
}

func runRoomACAPromote(cmd *cobra.Command, workspace, roomID, targetKind, sourceID string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.aca.promote", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", roomTaskScanLimit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.aca.promote", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	prepared, err := buildRoomACAPromotionInput(absWorkspace, roomID, summary, messages, targetKind, sourceID)
	if err != nil {
		code := protocol.ErrorCodeEARG
		if errors.Is(err, os.ErrNotExist) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.aca.promote", code, err.Error(), map[string]any{
			"hint": "Use one of: epic, milestone, retro, validation and an id visible in the matching `agentctl room ... show` command.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if prepared.EpicID != "" {
		if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, prepared.EpicID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.aca.promote", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}

	acaStore := contextplane.NewWorkspaceStore(absWorkspace)
	result, err := acaStore.DraftMarkdownProposal(cmd.Context(), prepared.Input)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.aca.promote", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.aca.promote", map[string]any{
		"room":            summary,
		"target_kind":     prepared.TargetKind,
		"target_id":       prepared.TargetID,
		"epic_id":         prepared.EpicID,
		"source":          prepared.Source,
		"draft_path":      result.DraftPath,
		"promotion_state": result.PromotionState,
		"proposal":        result.Proposal,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func buildRoomACAPromotionInput(absWorkspace, roomID string, room agent.RoomSummary, messages []agent.BoardMessage, targetKind, sourceID string) (roomACAPromotionPrepared, error) {
	targetKind = strings.TrimSpace(strings.ToLower(targetKind))
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return roomACAPromotionPrepared{}, fmt.Errorf("source id is required")
	}
	project := filepath.Base(absWorkspace)
	epics := buildRoomEpicViews(messages)
	switch targetKind {
	case "epic":
		epic := roomEpicViewByID(epics, sourceID)
		if epic == nil {
			return roomACAPromotionPrepared{}, fmt.Errorf("epic %q not found: %w", sourceID, os.ErrNotExist)
		}
		return roomACAPromoteEpic(absWorkspace, roomID, room, messages, project, epic), nil
	case "milestone":
		milestone := roomMilestoneViewByID(buildRoomMilestoneViews(messages), sourceID)
		if milestone == nil {
			return roomACAPromotionPrepared{}, fmt.Errorf("milestone %q not found: %w", sourceID, os.ErrNotExist)
		}
		epic := roomEpicViewByID(epics, stringField(milestone, "epic_id"))
		return roomACAPromoteMilestone(absWorkspace, roomID, project, epic, milestone), nil
	case "retro":
		update := roomGuidanceUpdateViewByID(buildRoomGuidanceUpdateViews(messages), sourceID)
		if update == nil {
			return roomACAPromotionPrepared{}, fmt.Errorf("guidance update %q not found: %w", sourceID, os.ErrNotExist)
		}
		epic := roomEpicViewByID(epics, stringField(update, "epic_id"))
		return roomACAPromoteRetro(absWorkspace, roomID, project, epic, update), nil
	case "validation":
		stories := buildRoomStoryViews(messages)
		story, validation := roomStoryValidationViewByID(stories, sourceID)
		if validation == nil {
			return roomACAPromotionPrepared{}, fmt.Errorf("validation %q not found: %w", sourceID, os.ErrNotExist)
		}
		status := stringField(anyMap(validation["meta"]), "status")
		if status != "fail" && status != "blocked" && status != "waived" {
			return roomACAPromotionPrepared{}, fmt.Errorf("validation %q is not high-signal enough for ACA promotion yet; promote fail, blocked, or waived validations in the first slice", sourceID)
		}
		milestone := roomMilestoneViewByID(buildRoomMilestoneViews(messages), stringField(story, "milestone_id"))
		epic := roomEpicViewByID(epics, stringField(story, "epic_id"))
		return roomACAPromoteValidation(absWorkspace, roomID, project, epic, milestone, story, validation), nil
	default:
		return roomACAPromotionPrepared{}, fmt.Errorf("unsupported room aca promotion kind %q", targetKind)
	}
}

func roomACAPromoteEpic(absWorkspace, roomID string, room agent.RoomSummary, messages []agent.BoardMessage, project string, epic map[string]any) roomACAPromotionPrepared {
	epicID := stringField(epic, "id")
	meta := anyMap(epic["meta"])
	finalBrief := anyMap(epic["final_brief"])
	resume := buildRoomEpicContinuity(room, messages, epic)
	title := firstNonEmpty(stringField(epic, "title"), epicID)
	summaryText := firstNonEmpty(stringField(resume, "summary"), stringField(finalBrief, "body"), stringField(meta, "goal"), title)
	workpackRoot := stringField(epic, "workpack_root")
	workpackPath := filepath.Join(workpackRoot, "epic.md")
	roomMessageIDs := []string{epicID}
	if finalID := stringField(finalBrief, "id"); finalID != "" {
		roomMessageIDs = append(roomMessageIDs, finalID)
	}
	if latestLog := roomEpicLatestLog(epic); latestLog != nil {
		if id := stringField(latestLog, "id"); id != "" {
			roomMessageIDs = append(roomMessageIDs, id)
		}
	}
	relatedLinks := make([]string, 0, len(mapSlice(epic["milestones"])))
	for _, milestone := range mapSlice(epic["milestones"]) {
		milestoneID := stringField(milestone, "id")
		if milestoneID == "" {
			continue
		}
		relatedLinks = append(relatedLinks, fmt.Sprintf("Milestone: [[room-milestones/%s]]", milestoneID))
	}
	body := renderRoomACAPromotionMarkdown(
		title,
		renderRoomEpicMarkdown(epic)+"\n"+renderRoomDeliveryLogMarkdown(epic)+"\n"+renderRoomRetroMarkdown(epic),
		[]string{
			fmt.Sprintf("- Room: `%s`", roomID),
			fmt.Sprintf("- Epic ID: `%s`", epicID),
			fmt.Sprintf("- Work-pack: `%s`", workpackPath),
			fmt.Sprintf("- Meta JSON: `%s`", roomAgileEpicMetaJSONPath(epicID)),
		},
		relatedLinks,
	)
	frontmatter := roomACAFrontmatter(absWorkspace, project, "room_epic", "epic", epicID, roomID, epicID, "", "", "", "", roomMessageIDs, workpackRoot, workpackPath, roomAgileEpicMetaJSONPath(epicID), stringField(epic, "status"), []string{"room/agile", "epic"})
	return roomACAPromotionPrepared{
		EpicID:     epicID,
		TargetKind: "epic",
		TargetID:   epicID,
		Source:     epic,
		Input: contextplane.MarkdownProposalInput{
			NoteType:       "room_epic",
			Project:        project,
			Folder:         filepath.ToSlash(filepath.Join("room-agile", roomACASlug(project, "workspace"), "room_epic")),
			SourceKind:     "epic",
			SourceID:       epicID,
			Title:          title,
			Summary:        fmt.Sprintf("Review ACA epic draft for %s. %s", title, summaryText),
			Body:           body,
			Frontmatter:    frontmatter,
			DedupeKey:      fmt.Sprintf("room_agile_draft|room_epic|%s", epicID),
			Kind:           "room_agile_draft",
			Classification: "room_epic",
			ReviewAction:   "review_room_agile_draft",
			SourceRefs:     uniqueStrings([]string{"room:" + roomID, "epic:" + epicID, "workpack:" + workpackPath}),
			ProposedChange: map[string]any{
				"room_id":          roomID,
				"epic_id":          epicID,
				"room_message_ids": roomMessageIDs,
				"workpack_root":    workpackRoot,
				"workpack_path":    workpackPath,
				"meta_json_path":   roomAgileEpicMetaJSONPath(epicID),
			},
		},
	}
}

func roomACAPromoteMilestone(absWorkspace, roomID, project string, epic, milestone map[string]any) roomACAPromotionPrepared {
	milestoneID := stringField(milestone, "id")
	epicID := stringField(milestone, "epic_id")
	title := firstNonEmpty(stringField(milestone, "title"), milestoneID)
	workpackDir := stringField(milestone, "workpack_dir")
	workpackPath := filepath.Join(workpackDir, "milestone.md")
	summaryMeta := anyMap(milestone["summary_meta"])
	roomMessageIDs := []string{milestoneID}
	if latestSummary := mapField(milestone, "latest_summary"); latestSummary != nil {
		if id := stringField(latestSummary, "id"); id != "" {
			roomMessageIDs = append(roomMessageIDs, id)
		}
	}
	if reviews := boardMessageSliceValue(milestone["reviews"]); len(reviews) > 0 {
		roomMessageIDs = append(roomMessageIDs, reviews[len(reviews)-1].ID)
	}
	body := renderRoomACAPromotionMarkdown(
		title,
		renderRoomMilestoneMarkdown(milestone)+"\n"+renderRoomMilestoneSummaryMarkdown(milestone),
		[]string{
			fmt.Sprintf("- Room: `%s`", roomID),
			fmt.Sprintf("- Epic ID: `%s`", epicID),
			fmt.Sprintf("- Milestone ID: `%s`", milestoneID),
			fmt.Sprintf("- Work-pack: `%s`", workpackPath),
			fmt.Sprintf("- Meta JSON: `%s`", roomAgileMilestoneMetaJSONPath(epicID, milestoneID)),
		},
		roomACARelatedLinks("room_milestone", epicID, milestoneID, ""),
	)
	frontmatter := roomACAFrontmatter(absWorkspace, project, "room_milestone", "milestone", milestoneID, roomID, epicID, milestoneID, "", "", "", roomMessageIDs, roomAgileWorkpackRootPath(epicID), workpackPath, roomAgileMilestoneMetaJSONPath(epicID, milestoneID), stringField(milestone, "status"), []string{"room/agile", "milestone"})
	return roomACAPromotionPrepared{
		EpicID:     epicID,
		TargetKind: "milestone",
		TargetID:   milestoneID,
		Source:     milestone,
		Input: contextplane.MarkdownProposalInput{
			NoteType:       "room_milestone",
			Project:        project,
			Folder:         filepath.ToSlash(filepath.Join("room-agile", roomACASlug(project, "workspace"), "room_milestone")),
			SourceKind:     "milestone",
			SourceID:       milestoneID,
			Title:          title,
			Summary:        fmt.Sprintf("Review ACA milestone draft for %s. %s", title, firstNonEmpty(stringField(summaryMeta, "summary"), stringField(anyMap(milestone["meta"]), "objective"), title)),
			Body:           body,
			Frontmatter:    frontmatter,
			DedupeKey:      fmt.Sprintf("room_agile_draft|room_milestone|%s", milestoneID),
			Kind:           "room_agile_draft",
			Classification: "room_milestone",
			ReviewAction:   "review_room_agile_draft",
			SourceRefs:     uniqueStrings([]string{"room:" + roomID, "epic:" + epicID, "milestone:" + milestoneID, "workpack:" + workpackPath}),
			ProposedChange: map[string]any{
				"room_id":          roomID,
				"epic_id":          epicID,
				"milestone_id":     milestoneID,
				"room_message_ids": roomMessageIDs,
				"workpack_root":    roomAgileWorkpackRootPath(epicID),
				"workpack_path":    workpackPath,
				"meta_json_path":   roomAgileMilestoneMetaJSONPath(epicID, milestoneID),
			},
		},
	}
}

func roomACAPromoteRetro(absWorkspace, roomID, project string, epic, update map[string]any) roomACAPromotionPrepared {
	updateID := stringField(update, "id")
	epicID := stringField(update, "epic_id")
	meta := anyMap(update["meta"])
	title := firstNonEmpty(stringField(update, "summary"), updateID)
	workpackRoot := roomAgileWorkpackRootPath(epicID)
	workpackPath := filepath.Join(workpackRoot, "retro.md")
	roomMessageIDs := []string{updateID}
	body := renderRoomACAPromotionMarkdown(
		title,
		renderRoomRetroMarkdown(map[string]any{"guidance_updates": []map[string]any{update}}),
		[]string{
			fmt.Sprintf("- Room: `%s`", roomID),
			fmt.Sprintf("- Epic ID: `%s`", epicID),
			fmt.Sprintf("- Guidance update ID: `%s`", updateID),
			fmt.Sprintf("- Work-pack: `%s`", workpackPath),
			fmt.Sprintf("- Meta JSON: `%s`", roomAgileEpicMetaJSONPath(epicID)),
		},
		roomACARelatedLinks("room_retro", epicID, stringField(meta, "milestone_id"), ""),
	)
	frontmatter := roomACAFrontmatter(absWorkspace, project, "room_retro", "guidance_update", updateID, roomID, epicID, stringField(meta, "milestone_id"), "", "", updateID, roomMessageIDs, workpackRoot, workpackPath, roomAgileEpicMetaJSONPath(epicID), "completed", []string{"room/agile", "retro", stringField(meta, "kind")})
	return roomACAPromotionPrepared{
		EpicID:     epicID,
		TargetKind: "retro",
		TargetID:   updateID,
		Source:     update,
		Input: contextplane.MarkdownProposalInput{
			NoteType:       "room_retro",
			Project:        project,
			Folder:         filepath.ToSlash(filepath.Join("room-agile", roomACASlug(project, "workspace"), "room_retro")),
			SourceKind:     "guidance_update",
			SourceID:       updateID,
			Title:          title,
			Summary:        fmt.Sprintf("Review ACA retro draft for %s. %s", title, firstNonEmpty(stringField(meta, "impact"), title)),
			Body:           body,
			Frontmatter:    frontmatter,
			DedupeKey:      fmt.Sprintf("room_agile_draft|room_retro|%s", updateID),
			Kind:           "room_agile_draft",
			Classification: "room_retro",
			ReviewAction:   "review_room_agile_draft",
			SourceRefs:     uniqueStrings([]string{"room:" + roomID, "epic:" + epicID, "guidance_update:" + updateID, "workpack:" + workpackPath}),
			ProposedChange: map[string]any{
				"room_id":            roomID,
				"epic_id":            epicID,
				"milestone_id":       stringField(meta, "milestone_id"),
				"guidance_update_id": updateID,
				"room_message_ids":   roomMessageIDs,
				"workpack_root":      workpackRoot,
				"workpack_path":      workpackPath,
				"meta_json_path":     roomAgileEpicMetaJSONPath(epicID),
			},
		},
	}
}

func roomACAPromoteValidation(absWorkspace, roomID, project string, epic, milestone, story, validation map[string]any) roomACAPromotionPrepared {
	validationID := stringField(validation, "id")
	epicID := stringField(story, "epic_id")
	milestoneID := stringField(story, "milestone_id")
	storyID := stringField(story, "id")
	meta := anyMap(validation["meta"])
	title := firstNonEmpty(stringField(story, "title"), storyID)
	workpackDir := stringField(story, "validation_dir")
	workpackPath := filepath.Join(workpackDir, validationID+".md")
	roomMessageIDs := []string{validationID}
	body := renderRoomACAPromotionMarkdown(
		fmt.Sprintf("%s validation", title),
		renderRoomStoryValidationMarkdown(validation),
		[]string{
			fmt.Sprintf("- Room: `%s`", roomID),
			fmt.Sprintf("- Epic ID: `%s`", epicID),
			fmt.Sprintf("- Milestone ID: `%s`", milestoneID),
			fmt.Sprintf("- Story ID: `%s`", storyID),
			fmt.Sprintf("- Validation ID: `%s`", validationID),
			fmt.Sprintf("- Work-pack: `%s`", workpackPath),
			fmt.Sprintf("- Meta JSON: `%s`", roomAgileValidationJSONPath(epicID, milestoneID, storyID, validationID)),
		},
		roomACARelatedLinks("room_validation", epicID, milestoneID, storyID),
	)
	frontmatter := roomACAFrontmatter(absWorkspace, project, "room_validation", "story_validation", validationID, roomID, epicID, milestoneID, storyID, validationID, "", roomMessageIDs, roomAgileWorkpackRootPath(epicID), workpackPath, roomAgileValidationJSONPath(epicID, milestoneID, storyID, validationID), stringField(meta, "status"), []string{"room/agile", "validation", stringField(meta, "status")})
	return roomACAPromotionPrepared{
		EpicID:     epicID,
		TargetKind: "validation",
		TargetID:   validationID,
		Source:     validation,
		Input: contextplane.MarkdownProposalInput{
			NoteType:       "room_validation",
			Project:        project,
			Folder:         filepath.ToSlash(filepath.Join("room-agile", roomACASlug(project, "workspace"), "room_validation")),
			SourceKind:     "story_validation",
			SourceID:       validationID,
			Title:          fmt.Sprintf("%s validation", title),
			Summary:        fmt.Sprintf("Review ACA validation draft for story %s after a %s validation outcome.", title, stringField(meta, "status")),
			Body:           body,
			Frontmatter:    frontmatter,
			DedupeKey:      fmt.Sprintf("room_agile_draft|room_validation|%s", validationID),
			Kind:           "room_agile_draft",
			Classification: "room_validation",
			ReviewAction:   "review_room_agile_draft",
			SourceRefs:     uniqueStrings([]string{"room:" + roomID, "epic:" + epicID, "milestone:" + milestoneID, "story:" + storyID, "validation:" + validationID, "workpack:" + workpackPath}),
			ProposedChange: map[string]any{
				"room_id":          roomID,
				"epic_id":          epicID,
				"milestone_id":     milestoneID,
				"story_id":         storyID,
				"validation_id":    validationID,
				"room_message_ids": roomMessageIDs,
				"workpack_root":    roomAgileWorkpackRootPath(epicID),
				"workpack_path":    workpackPath,
				"meta_json_path":   roomAgileValidationJSONPath(epicID, milestoneID, storyID, validationID),
			},
		},
	}
}

func roomACAFrontmatter(absWorkspace, project, noteType, sourceKind, sourceID, roomID, epicID, milestoneID, storyID, validationID, guidanceUpdateID string, roomMessageIDs []string, workpackRoot, workpackPath, metaJSONPath, status string, tags []string) map[string]any {
	return map[string]any{
		"note_type":              noteType,
		"schema_version":         1,
		"generated_at":           time.Now().UTC().Format(time.RFC3339),
		"workspace":              absWorkspace,
		"workspace_id":           absWorkspace,
		"project":                project,
		"room_id":                roomID,
		"source_kind":            strings.TrimSpace(sourceKind),
		"source_id":              strings.TrimSpace(sourceID),
		"epic_id":                strings.TrimSpace(epicID),
		"milestone_id":           strings.TrimSpace(milestoneID),
		"story_id":               strings.TrimSpace(storyID),
		"validation_id":          strings.TrimSpace(validationID),
		"guidance_update_id":     strings.TrimSpace(guidanceUpdateID),
		"room_message_ids":       uniqueStrings(roomMessageIDs),
		"workpack_root":          strings.TrimSpace(workpackRoot),
		"workpack_path":          strings.TrimSpace(workpackPath),
		"meta_json_path":         strings.TrimSpace(metaJSONPath),
		"status":                 firstNonEmpty(strings.TrimSpace(status), "drafted"),
		"promotion_source":       "room_agile",
		"promotion_review_state": "drafted",
		"tags":                   uniqueStrings(tags),
	}
}

func renderRoomACAPromotionMarkdown(title, core string, provenance, links []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", strings.TrimSpace(title))
	if strings.TrimSpace(core) != "" {
		b.WriteString(strings.TrimSpace(core))
		b.WriteString("\n")
	}
	if len(provenance) > 0 {
		b.WriteString("\n## Provenance\n")
		for _, item := range provenance {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if strings.HasPrefix(item, "- ") {
				b.WriteString(item)
			} else {
				b.WriteString("- " + item)
			}
			b.WriteString("\n")
		}
	}
	if len(links) > 0 {
		b.WriteString("\n## Related\n")
		for _, item := range links {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			b.WriteString("- " + item + "\n")
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func roomACARelatedLinks(noteType, epicID, milestoneID, storyID string) []string {
	out := make([]string, 0, 4)
	if epicID != "" && noteType != "room_epic" {
		out = append(out, fmt.Sprintf("Epic: [[room-epics/%s]]", epicID))
	}
	if milestoneID != "" && noteType != "room_milestone" {
		out = append(out, fmt.Sprintf("Milestone: [[room-milestones/%s]]", milestoneID))
	}
	if storyID != "" && noteType != "room_story" {
		out = append(out, fmt.Sprintf("Story: [[room-stories/%s]]", storyID))
	}
	return out
}

func roomACASlug(value, fallback string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return fallback
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if lastDash || b.Len() == 0 {
				continue
			}
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		return fallback
	}
	return slug
}

func roomGuidanceUpdateViewByID(updates []map[string]any, updateID string) map[string]any {
	updateID = strings.TrimSpace(updateID)
	for _, update := range updates {
		if stringField(update, "id") == updateID {
			return update
		}
	}
	return nil
}

func roomStoryValidationViewByID(stories []map[string]any, validationID string) (map[string]any, map[string]any) {
	validationID = strings.TrimSpace(validationID)
	for _, story := range stories {
		for _, validation := range mapSlice(story["validations"]) {
			if stringField(validation, "id") == validationID {
				return story, validation
			}
		}
	}
	return nil, nil
}

func runRoomWorkpackShow(cmd *cobra.Command, workspace, roomID, epicID string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.workpack.show", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", roomTaskScanLimit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.workpack.show", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	for _, epic := range buildRoomEpicViews(messages) {
		if id, _ := epic["id"].(string); id == strings.TrimSpace(epicID) {
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.workpack.show", map[string]any{
				"room":      summary,
				"epic_id":   epicID,
				"workpack":  buildRoomAgileWorkpackInfo(epic),
				"epic":      epic,
				"workspace": absWorkspace,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.workpack.show", protocol.ErrorCodeENotFound, fmt.Sprintf("epic %q not found", epicID), map[string]any{
		"hint": "Run `agentctl room epic show <room-id>` to list available epics.",
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomWorkpackSync(cmd *cobra.Command, workspace, sender, roomID, epicID string) error {
	absWorkspace, identity, store, roomID, _, err := prepareRoomAgileCommand(cmd, "agentctl.room.workpack", workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()

	epicMsg, err := loadRoomAgileRoot(cmd.Context(), store, absWorkspace, roomID, epicID, agent.BoardMessageKindEpic)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.workpack.sync", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Create the epic first with `agentctl room epic start` and reuse its epic_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := syncRoomAgileWorkpack(cmd.Context(), store, absWorkspace, roomID, epicMsg.ID); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.workpack.sync", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	messages, err := store.ListRoomMessages(cmd.Context(), absWorkspace, roomID, roomTaskScanLimit)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.workpack.sync", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	for _, epic := range buildRoomEpicViews(messages) {
		if id, _ := epic["id"].(string); id == epicMsg.ID {
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.workpack.sync", map[string]any{
				"room_id":   roomID,
				"epic_id":   epicMsg.ID,
				"actor":     identity.Sender,
				"workpack":  buildRoomAgileWorkpackInfo(epic),
				"workspace": absWorkspace,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.workpack.sync", map[string]any{
		"room_id":   roomID,
		"epic_id":   epicMsg.ID,
		"actor":     identity.Sender,
		"workpack":  map[string]any{"root": roomAgileWorkpackRootPath(epicMsg.ID)},
		"workspace": absWorkspace,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func prepareRoomAgileCommand(cmd *cobra.Command, commandName, workspace, sender, roomID string) (string, roomIdentity, blackboard.BoardStore, string, agent.RoomSummary, error) {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, protocol.WriteError(cmd.OutOrStdout(), commandName, protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, protocol.WriteError(cmd.OutOrStdout(), commandName, protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	roomID = strings.TrimSpace(roomID)
	summary, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, identity.Sender)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		store.Close()
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, protocol.WriteError(cmd.OutOrStdout(), commandName, code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return absWorkspace, identity, store, roomID, summary, nil
}

func loadRoomAgileRoot(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, rootID string, kind agent.BoardMessageKind) (agent.BoardMessage, error) {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return agent.BoardMessage{}, err
	}
	rootID = strings.TrimSpace(rootID)
	for _, msg := range messages {
		if msg.ID == rootID && msg.Kind == kind {
			return msg, nil
		}
	}
	return agent.BoardMessage{}, fmt.Errorf("%s %q not found", strings.ReplaceAll(string(kind), "_", " "), rootID)
}

func loadRoomEpic(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, epicID string) (agent.BoardMessage, roomEpicMeta, error) {
	msg, err := loadRoomAgileRoot(ctx, store, workspaceID, roomID, epicID, agent.BoardMessageKindEpic)
	if err != nil {
		return agent.BoardMessage{}, roomEpicMeta{}, err
	}
	return msg, parseRoomEpicBody(msg.Body), nil
}

func loadRoomEpicQuestion(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, questionID string) (agent.BoardMessage, agent.BoardMessage, roomEpicMeta, error) {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return agent.BoardMessage{}, agent.BoardMessage{}, roomEpicMeta{}, err
	}
	questionID = strings.TrimSpace(questionID)
	for _, msg := range messages {
		if msg.ID == questionID && msg.Kind == agent.BoardMessageKindEpicQuestion {
			epicMsg, meta, err := loadRoomEpic(ctx, store, workspaceID, roomID, msg.RelatedMessageID)
			if err != nil {
				return agent.BoardMessage{}, agent.BoardMessage{}, roomEpicMeta{}, err
			}
			return msg, epicMsg, meta, nil
		}
	}
	return agent.BoardMessage{}, agent.BoardMessage{}, roomEpicMeta{}, fmt.Errorf("epic question %q not found", questionID)
}

func loadRoomMilestoneProposal(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, proposalID string) (agent.BoardMessage, roomMilestoneProposalMeta, error) {
	msg, err := loadRoomAgileRoot(ctx, store, workspaceID, roomID, proposalID, agent.BoardMessageKindMilestoneProposal)
	if err != nil {
		return agent.BoardMessage{}, roomMilestoneProposalMeta{}, err
	}
	return msg, parseRoomMilestoneProposalBody(msg.Body), nil
}

func loadRoomStoryProposal(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, proposalID string) (agent.BoardMessage, roomStoryProposalMeta, error) {
	msg, err := loadRoomAgileRoot(ctx, store, workspaceID, roomID, proposalID, agent.BoardMessageKindStoryProposal)
	if err != nil {
		return agent.BoardMessage{}, roomStoryProposalMeta{}, err
	}
	return msg, parseRoomStoryProposalBody(msg.Body), nil
}

func roomMemberHasRoleFromRoomState(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, actorID, role string) bool {
	summary, _, err := loadRoomState(ctx, store, workspaceID, roomID, "", roomTaskScanLimit)
	if err != nil {
		return false
	}
	return roomMemberHasRole(summary.Members, actorID, role)
}

func validateRoomRelatedStoryIDs(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, epicID, milestoneID, storyID string, relatedStoryIDs []string) ([]string, error) {
	if len(relatedStoryIDs) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(relatedStoryIDs))
	out := make([]string, 0, len(relatedStoryIDs))
	for _, candidate := range relatedStoryIDs {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" || candidate == strings.TrimSpace(storyID) {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		relatedStory, err := loadRoomAgileRoot(ctx, store, workspaceID, roomID, candidate, agent.BoardMessageKindStory)
		if err != nil {
			return nil, fmt.Errorf("related story %q not found", candidate)
		}
		if strings.TrimSpace(relatedStory.RelatedMessageID) != strings.TrimSpace(milestoneID) {
			return nil, fmt.Errorf("related story %q is not in milestone %q", candidate, milestoneID)
		}
		relatedMilestone, err := loadRoomAgileRoot(ctx, store, workspaceID, roomID, relatedStory.RelatedMessageID, agent.BoardMessageKindMilestone)
		if err != nil {
			return nil, fmt.Errorf("related story %q milestone not found", candidate)
		}
		relatedMeta := parseRoomMilestoneBody(relatedMilestone.Body)
		if strings.TrimSpace(relatedMeta.EpicID) != strings.TrimSpace(epicID) {
			return nil, fmt.Errorf("related story %q is not in epic %q", candidate, epicID)
		}
		out = append(out, candidate)
	}
	sort.Strings(out)
	return out, nil
}

func epicIsFinalized(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, epicID string) bool {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return false
	}
	for _, msg := range messages {
		if msg.Kind == agent.BoardMessageKindEpicFinalize && strings.TrimSpace(msg.RelatedMessageID) == strings.TrimSpace(epicID) {
			return true
		}
	}
	return false
}

func countOpenEpicQuestions(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, epicID string) (int, error) {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return 0, err
	}
	answersByQuestion := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Kind == agent.BoardMessageKindEpicAnswer {
			answersByQuestion[strings.TrimSpace(msg.RelatedMessageID)] = struct{}{}
		}
	}
	open := 0
	for _, msg := range messages {
		if msg.Kind == agent.BoardMessageKindEpicQuestion && strings.TrimSpace(msg.RelatedMessageID) == strings.TrimSpace(epicID) {
			if _, ok := answersByQuestion[msg.ID]; !ok {
				open++
			}
		}
	}
	return open, nil
}

func buildRoomEpicBody(title, goal, owner, outcome, horizon string, scope, success []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Title: %s\n", strings.TrimSpace(title))
	if strings.TrimSpace(goal) != "" {
		fmt.Fprintf(&b, "Goal: %s\n", strings.TrimSpace(goal))
	}
	if strings.TrimSpace(owner) != "" {
		fmt.Fprintf(&b, "Owner: %s\n", strings.TrimSpace(owner))
	}
	if strings.TrimSpace(outcome) != "" {
		fmt.Fprintf(&b, "Outcome: %s\n", strings.TrimSpace(outcome))
	}
	if strings.TrimSpace(horizon) != "" {
		fmt.Fprintf(&b, "Horizon: %s\n", strings.TrimSpace(horizon))
	}
	appendRoomSection(&b, "Scope", scope)
	appendRoomSection(&b, "Success", success)
	b.WriteString("Protocol:\n- define milestones\n- add stories under milestones\n- review milestones against acceptance criteria\n- append delivery log entries as work progresses\n")
	return strings.TrimSpace(b.String())
}

func parseRoomEpicBody(body string) roomEpicMeta {
	meta := roomEpicMeta{}
	lines := strings.Split(body, "\n")
	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Protocol:") {
			break
		}
		switch trimmed {
		case "Scope:":
			section = "scope"
			continue
		case "Success:":
			section = "success"
			continue
		}
		if section != "" && strings.HasPrefix(trimmed, "- ") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			if value == "" {
				continue
			}
			switch section {
			case "scope":
				meta.Scope = append(meta.Scope, value)
			case "success":
				meta.Success = append(meta.Success, value)
			}
			continue
		}
		section = ""
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Goal":
			meta.Goal = value
		case "Owner":
			meta.Owner = value
		case "Outcome":
			meta.Outcome = value
		case "Horizon":
			meta.Horizon = value
		}
	}
	return meta
}

func normalizeRoomEpicQuestionKind(raw string) (string, error) {
	kind := strings.TrimSpace(strings.ToLower(raw))
	if kind == "" {
		kind = "product"
	}
	switch kind {
	case "product", "technical", "constraint", "success":
		return kind, nil
	default:
		return "", fmt.Errorf("unsupported epic question kind %q", raw)
	}
}

func buildRoomEpicQuestionBody(kind, question string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Kind: %s\n", strings.TrimSpace(kind))
	fmt.Fprintf(&b, "Question: %s\n", strings.TrimSpace(question))
	return strings.TrimSpace(b.String())
}

func parseRoomEpicQuestionBody(body string) roomEpicQuestionMeta {
	meta := roomEpicQuestionMeta{}
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Kind":
			meta.Kind = value
		case "Question":
			meta.Question = value
		}
	}
	return meta
}

func buildRoomMilestoneBody(epicID, title, goal, objective, owner string, scope, risks, exclusions, dependencies, validatorsExpected, requiredEvidenceLanes, optionalEvidenceLanes, exitCriteria []string) string {
	var b strings.Builder
	if strings.TrimSpace(epicID) != "" {
		fmt.Fprintf(&b, "EpicID: %s\n", strings.TrimSpace(epicID))
	}
	if strings.TrimSpace(title) != "" {
		fmt.Fprintf(&b, "Title: %s\n", strings.TrimSpace(title))
	}
	if strings.TrimSpace(goal) != "" {
		fmt.Fprintf(&b, "Goal: %s\n", strings.TrimSpace(goal))
	}
	if strings.TrimSpace(objective) != "" {
		fmt.Fprintf(&b, "Objective: %s\n", strings.TrimSpace(objective))
	}
	if strings.TrimSpace(owner) != "" {
		fmt.Fprintf(&b, "Owner: %s\n", strings.TrimSpace(owner))
	}
	appendRoomSection(&b, "Scope", scope)
	appendRoomSection(&b, "Risks", risks)
	appendRoomSection(&b, "Exclusions", exclusions)
	appendRoomSection(&b, "Dependencies", dependencies)
	appendRoomSection(&b, "ValidatorsExpected", validatorsExpected)
	appendRoomSection(&b, "RequiredEvidenceLanes", requiredEvidenceLanes)
	appendRoomSection(&b, "OptionalEvidenceLanes", optionalEvidenceLanes)
	appendRoomSection(&b, "ExitCriteria", exitCriteria)
	if strings.TrimSpace(epicID) != "" || strings.TrimSpace(title) != "" {
		b.WriteString("Protocol:\n- add acceptance criteria\n- attach stories under the milestone\n- record pass/block review at the milestone boundary\n")
	}
	return strings.TrimSpace(b.String())
}

func parseRoomMilestoneBody(body string) roomMilestoneMeta {
	meta := roomMilestoneMeta{}
	lines := strings.Split(body, "\n")
	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Protocol:") {
			break
		}
		switch trimmed {
		case "Scope:":
			section = "scope"
			continue
		case "Risks:":
			section = "risks"
			continue
		case "Exclusions:":
			section = "exclusions"
			continue
		case "Dependencies:":
			section = "dependencies"
			continue
		case "ValidatorsExpected:":
			section = "validators"
			continue
		case "RequiredEvidenceLanes:":
			section = "required_lanes"
			continue
		case "OptionalEvidenceLanes:":
			section = "optional_lanes"
			continue
		case "ExitCriteria:":
			section = "exit"
			continue
		}
		if section != "" && strings.HasPrefix(trimmed, "- ") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			switch section {
			case "scope":
				meta.Scope = append(meta.Scope, value)
			case "risks":
				meta.Risks = append(meta.Risks, value)
			case "exclusions":
				meta.Exclusions = append(meta.Exclusions, value)
			case "dependencies":
				meta.Dependencies = append(meta.Dependencies, value)
			case "validators":
				meta.ValidatorsExpected = append(meta.ValidatorsExpected, value)
			case "required_lanes":
				meta.RequiredEvidenceLanes = append(meta.RequiredEvidenceLanes, value)
			case "optional_lanes":
				meta.OptionalEvidenceLanes = append(meta.OptionalEvidenceLanes, value)
			case "exit":
				meta.ExitCriteria = append(meta.ExitCriteria, value)
			}
			continue
		}
		section = ""
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "EpicID":
			meta.EpicID = value
		case "Goal":
			meta.Goal = value
		case "Objective":
			meta.Objective = value
		case "Owner":
			meta.Owner = value
		}
	}
	return meta
}

func buildRoomMilestoneSummaryBody(meta roomMilestoneSummaryMeta) string {
	var b strings.Builder
	if strings.TrimSpace(meta.Summary) != "" {
		fmt.Fprintf(&b, "Summary: %s\n", strings.TrimSpace(meta.Summary))
	}
	appendRoomSection(&b, "PassedCriteria", meta.PassedCriteria)
	appendRoomSection(&b, "FailedCriteria", meta.FailedCriteria)
	appendRoomSection(&b, "WaivedValidationIDs", meta.WaivedValidationIDs)
	appendRoomSection(&b, "BlockingValidationIDs", meta.BlockingValidationIDs)
	appendRoomSection(&b, "NotableDecisions", meta.NotableDecisions)
	appendRoomSection(&b, "SystemicFindings", meta.SystemicFindings)
	appendRoomSection(&b, "RecommendedNext", meta.RecommendedNext)
	appendRoomSection(&b, "GuidanceUpdates", meta.GuidanceUpdates)
	return strings.TrimSpace(b.String())
}

func parseRoomMilestoneSummaryBody(body string) roomMilestoneSummaryMeta {
	meta := roomMilestoneSummaryMeta{}
	legacyLines := make([]string, 0)
	lines := strings.Split(body, "\n")
	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch trimmed {
		case "PassedCriteria:":
			section = "passed"
			continue
		case "FailedCriteria:":
			section = "failed"
			continue
		case "WaivedValidationIDs:":
			section = "waived"
			continue
		case "BlockingValidationIDs:":
			section = "blocking"
			continue
		case "NotableDecisions:":
			section = "decisions"
			continue
		case "SystemicFindings:":
			section = "findings"
			continue
		case "RecommendedNext:":
			section = "next"
			continue
		case "GuidanceUpdates:":
			section = "guidance"
			continue
		}
		if section != "" && strings.HasPrefix(trimmed, "- ") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			switch section {
			case "passed":
				meta.PassedCriteria = append(meta.PassedCriteria, value)
			case "failed":
				meta.FailedCriteria = append(meta.FailedCriteria, value)
			case "waived":
				meta.WaivedValidationIDs = append(meta.WaivedValidationIDs, value)
			case "blocking":
				meta.BlockingValidationIDs = append(meta.BlockingValidationIDs, value)
			case "decisions":
				meta.NotableDecisions = append(meta.NotableDecisions, value)
			case "findings":
				meta.SystemicFindings = append(meta.SystemicFindings, value)
			case "next":
				meta.RecommendedNext = append(meta.RecommendedNext, value)
			case "guidance":
				meta.GuidanceUpdates = append(meta.GuidanceUpdates, value)
			}
			continue
		}
		section = ""
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			legacyLines = append(legacyLines, trimmed)
			continue
		}
		if strings.TrimSpace(key) == "Summary" {
			meta.Summary = strings.TrimSpace(value)
			continue
		}
		legacyLines = append(legacyLines, trimmed)
	}
	if meta.Summary == "" && len(legacyLines) > 0 {
		meta.Summary = strings.Join(legacyLines, "\n")
	}
	return meta
}

func buildRoomMilestoneProposalBody(epicID, goal string, scope []string, rationale string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "EpicID: %s\n", strings.TrimSpace(epicID))
	if strings.TrimSpace(goal) != "" {
		fmt.Fprintf(&b, "Goal: %s\n", strings.TrimSpace(goal))
	}
	appendRoomSection(&b, "Scope", scope)
	if strings.TrimSpace(rationale) != "" {
		fmt.Fprintf(&b, "Rationale: %s\n", strings.TrimSpace(rationale))
	}
	return strings.TrimSpace(b.String())
}

func parseRoomMilestoneProposalBody(body string) roomMilestoneProposalMeta {
	meta := roomMilestoneProposalMeta{}
	lines := strings.Split(body, "\n")
	inScope := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if trimmed == "Scope:" {
			inScope = true
			continue
		}
		if inScope && strings.HasPrefix(trimmed, "- ") {
			meta.Scope = append(meta.Scope, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			continue
		}
		inScope = false
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "EpicID":
			meta.EpicID = value
		case "Goal":
			meta.Goal = value
		case "Rationale":
			meta.Rationale = value
		}
	}
	return meta
}

func buildRoomStoryBody(owner, description string) string {
	var b strings.Builder
	if strings.TrimSpace(owner) != "" {
		fmt.Fprintf(&b, "Owner: %s\n", strings.TrimSpace(owner))
	}
	fmt.Fprintf(&b, "Description: %s\n", strings.TrimSpace(description))
	return strings.TrimSpace(b.String())
}

func parseRoomStoryBody(body string) roomStoryMeta {
	meta := roomStoryMeta{}
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Owner":
			meta.Owner = value
		case "Description":
			meta.Description = value
		}
	}
	return meta
}

func normalizeRoomStoryState(raw string) (string, error) {
	state := strings.TrimSpace(strings.ToLower(raw))
	switch state {
	case "proposed", "accepted", "in_progress", "in_review", "validated", "blocked", "waived", "done", "deferred":
		return state, nil
	default:
		return "", fmt.Errorf("unsupported story state %q", raw)
	}
}

func buildRoomStoryStateBody(state, reason, blockedBy, reviewer string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "State: %s\n", strings.TrimSpace(state))
	if strings.TrimSpace(reason) != "" {
		fmt.Fprintf(&b, "Reason: %s\n", strings.TrimSpace(reason))
	}
	if strings.TrimSpace(blockedBy) != "" {
		fmt.Fprintf(&b, "BlockedBy: %s\n", strings.TrimSpace(blockedBy))
	}
	if strings.TrimSpace(reviewer) != "" {
		fmt.Fprintf(&b, "Reviewer: %s\n", strings.TrimSpace(reviewer))
	}
	return strings.TrimSpace(b.String())
}

func parseRoomStoryStateBody(body string) roomStoryStateMeta {
	meta := roomStoryStateMeta{}
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "State":
			meta.State = value
		case "Reason":
			meta.Reason = value
		case "BlockedBy":
			meta.BlockedBy = value
		case "Reviewer":
			meta.Reviewer = value
		}
	}
	return meta
}

func normalizeRoomStoryValidatorType(raw string) (string, error) {
	kind := strings.TrimSpace(strings.ToLower(raw))
	switch kind {
	case "review", "test", "integration", "user_test", "manual_check", "audit":
		return kind, nil
	default:
		return "", fmt.Errorf("unsupported story validator type %q", raw)
	}
}

func normalizeRoomStoryValidationStatus(raw string) (string, error) {
	status := strings.TrimSpace(strings.ToLower(raw))
	switch status {
	case "pass", "fail", "blocked", "waived":
		return status, nil
	default:
		return "", fmt.Errorf("unsupported story validation status %q", raw)
	}
}

func normalizeRoomMilestoneValidatorTypes(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{})
	for _, raw := range values {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		kind, err := normalizeRoomStoryValidatorType(raw)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[kind]; ok {
			continue
		}
		seen[kind] = struct{}{}
		out = append(out, kind)
	}
	sort.Strings(out)
	return out, nil
}

func normalizeRoomList(items []string, sortItems bool) []string {
	clean := cleanRoomItems(items)
	if len(clean) == 0 {
		return nil
	}
	out := make([]string, 0, len(clean))
	seen := make(map[string]struct{}, len(clean))
	for _, item := range clean {
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if sortItems {
		sort.Strings(out)
	}
	return out
}

func normalizeRoomMilestoneContract(meta roomMilestoneMeta) (roomMilestoneMeta, error) {
	meta.EpicID = strings.TrimSpace(meta.EpicID)
	meta.Goal = strings.TrimSpace(meta.Goal)
	meta.Objective = strings.TrimSpace(meta.Objective)
	meta.Owner = strings.TrimSpace(meta.Owner)
	meta.Scope = normalizeRoomList(meta.Scope, false)
	meta.Risks = normalizeRoomList(meta.Risks, true)
	meta.Exclusions = normalizeRoomList(meta.Exclusions, true)
	meta.Dependencies = normalizeRoomList(meta.Dependencies, true)
	validators, err := normalizeRoomMilestoneValidatorTypes(meta.ValidatorsExpected)
	if err != nil {
		return roomMilestoneMeta{}, err
	}
	meta.ValidatorsExpected = validators
	required, err := normalizeRoomMilestoneValidatorTypes(meta.RequiredEvidenceLanes)
	if err != nil {
		return roomMilestoneMeta{}, err
	}
	meta.RequiredEvidenceLanes = required
	optional, err := normalizeRoomMilestoneValidatorTypes(meta.OptionalEvidenceLanes)
	if err != nil {
		return roomMilestoneMeta{}, err
	}
	if len(optional) > 0 && len(required) > 0 {
		filtered := make([]string, 0, len(optional))
		requiredSet := make(map[string]struct{}, len(required))
		for _, lane := range required {
			requiredSet[lane] = struct{}{}
		}
		for _, lane := range optional {
			if _, ok := requiredSet[lane]; ok {
				continue
			}
			filtered = append(filtered, lane)
		}
		optional = filtered
	}
	meta.OptionalEvidenceLanes = optional
	meta.ExitCriteria = normalizeRoomList(meta.ExitCriteria, true)
	return meta, nil
}

func mergeRoomList(base, patch []string, sortItems bool) []string {
	if len(base) == 0 && len(patch) == 0 {
		return nil
	}
	return normalizeRoomList(append(append([]string(nil), base...), patch...), sortItems)
}

func normalizeRoomMilestoneSummaryMeta(meta roomMilestoneSummaryMeta) roomMilestoneSummaryMeta {
	meta.Summary = strings.TrimSpace(meta.Summary)
	meta.PassedCriteria = normalizeRoomList(meta.PassedCriteria, false)
	meta.FailedCriteria = normalizeRoomList(meta.FailedCriteria, false)
	meta.WaivedValidationIDs = normalizeRoomList(meta.WaivedValidationIDs, true)
	meta.BlockingValidationIDs = normalizeRoomList(meta.BlockingValidationIDs, true)
	meta.NotableDecisions = normalizeRoomList(meta.NotableDecisions, false)
	meta.SystemicFindings = normalizeRoomList(meta.SystemicFindings, false)
	meta.RecommendedNext = normalizeRoomList(meta.RecommendedNext, false)
	meta.GuidanceUpdates = normalizeRoomList(meta.GuidanceUpdates, false)
	return meta
}

func mergeRoomMilestoneMeta(base roomMilestoneMeta, patch roomMilestoneMeta) roomMilestoneMeta {
	if patch.EpicID != "" {
		base.EpicID = patch.EpicID
	}
	if patch.Goal != "" {
		base.Goal = patch.Goal
	}
	if patch.Objective != "" {
		base.Objective = patch.Objective
	}
	if patch.Owner != "" {
		base.Owner = patch.Owner
	}
	if len(patch.Scope) > 0 {
		base.Scope = append([]string(nil), patch.Scope...)
	}
	if len(patch.Risks) > 0 {
		base.Risks = mergeRoomList(base.Risks, patch.Risks, true)
	}
	if len(patch.Exclusions) > 0 {
		base.Exclusions = mergeRoomList(base.Exclusions, patch.Exclusions, true)
	}
	if len(patch.Dependencies) > 0 {
		base.Dependencies = mergeRoomList(base.Dependencies, patch.Dependencies, true)
	}
	if len(patch.ValidatorsExpected) > 0 {
		base.ValidatorsExpected = mergeRoomList(base.ValidatorsExpected, patch.ValidatorsExpected, true)
	}
	if len(patch.RequiredEvidenceLanes) > 0 {
		base.RequiredEvidenceLanes = mergeRoomList(base.RequiredEvidenceLanes, patch.RequiredEvidenceLanes, true)
	}
	if len(patch.OptionalEvidenceLanes) > 0 {
		base.OptionalEvidenceLanes = mergeRoomList(base.OptionalEvidenceLanes, patch.OptionalEvidenceLanes, true)
	}
	if len(base.OptionalEvidenceLanes) > 0 && len(base.RequiredEvidenceLanes) > 0 {
		requiredSet := make(map[string]struct{}, len(base.RequiredEvidenceLanes))
		for _, lane := range base.RequiredEvidenceLanes {
			requiredSet[lane] = struct{}{}
		}
		filtered := make([]string, 0, len(base.OptionalEvidenceLanes))
		for _, lane := range base.OptionalEvidenceLanes {
			if _, ok := requiredSet[lane]; ok {
				continue
			}
			filtered = append(filtered, lane)
		}
		base.OptionalEvidenceLanes = filtered
	}
	if len(patch.ExitCriteria) > 0 {
		base.ExitCriteria = mergeRoomList(base.ExitCriteria, patch.ExitCriteria, true)
	}
	return base
}

func buildRoomStoryValidationBody(epicID, milestoneID, storyID, validatorType, status, summaryText, artifactPath, artifactDigest, commandText, notes string, relatedStoryIDs []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "EpicID: %s\n", strings.TrimSpace(epicID))
	fmt.Fprintf(&b, "MilestoneID: %s\n", strings.TrimSpace(milestoneID))
	fmt.Fprintf(&b, "StoryID: %s\n", strings.TrimSpace(storyID))
	fmt.Fprintf(&b, "ValidatorType: %s\n", strings.TrimSpace(validatorType))
	fmt.Fprintf(&b, "Status: %s\n", strings.TrimSpace(status))
	fmt.Fprintf(&b, "Summary: %s\n", strings.TrimSpace(summaryText))
	if strings.TrimSpace(commandText) != "" {
		fmt.Fprintf(&b, "Command: %s\n", strings.TrimSpace(commandText))
	}
	if strings.TrimSpace(artifactPath) != "" {
		fmt.Fprintf(&b, "ArtifactPath: %s\n", strings.TrimSpace(artifactPath))
	}
	if strings.TrimSpace(artifactDigest) != "" {
		fmt.Fprintf(&b, "ArtifactDigest: %s\n", strings.TrimSpace(artifactDigest))
	}
	appendRoomSection(&b, "RelatedStoryIDs", relatedStoryIDs)
	if strings.TrimSpace(status) == "waived" && strings.TrimSpace(notes) != "" {
		fmt.Fprintf(&b, "WaiverReason: %s\n", strings.TrimSpace(notes))
	}
	if strings.TrimSpace(notes) != "" {
		fmt.Fprintf(&b, "Notes: %s\n", strings.TrimSpace(notes))
	}
	return strings.TrimSpace(b.String())
}

func parseRoomStoryValidationBody(body string) roomStoryValidationMeta {
	meta := roomStoryValidationMeta{}
	lines := strings.Split(body, "\n")
	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if trimmed == "RelatedStoryIDs:" {
			section = "related"
			continue
		}
		if section == "related" && strings.HasPrefix(trimmed, "- ") {
			meta.RelatedStoryIDs = append(meta.RelatedStoryIDs, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			continue
		}
		section = ""
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "EpicID":
			meta.EpicID = value
		case "MilestoneID":
			meta.MilestoneID = value
		case "StoryID":
			meta.StoryID = value
		case "ValidatorType":
			meta.ValidatorType = value
		case "Status":
			meta.Status = value
		case "Summary":
			meta.Summary = value
		case "Command":
			meta.Command = value
		case "ArtifactPath":
			meta.ArtifactPath = value
		case "ArtifactDigest":
			meta.ArtifactDigest = value
		case "WaiverReason":
			meta.WaiverReason = value
		case "Notes":
			meta.Notes = value
		}
	}
	return meta
}

func buildRoomStoryProposalBody(milestoneID, owner, description, rationale string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "MilestoneID: %s\n", strings.TrimSpace(milestoneID))
	if strings.TrimSpace(owner) != "" {
		fmt.Fprintf(&b, "Owner: %s\n", strings.TrimSpace(owner))
	}
	fmt.Fprintf(&b, "Description: %s\n", strings.TrimSpace(description))
	if strings.TrimSpace(rationale) != "" {
		fmt.Fprintf(&b, "Rationale: %s\n", strings.TrimSpace(rationale))
	}
	return strings.TrimSpace(b.String())
}

func parseRoomStoryProposalBody(body string) roomStoryProposalMeta {
	meta := roomStoryProposalMeta{}
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "MilestoneID":
			meta.MilestoneID = value
		case "Owner":
			meta.Owner = value
		case "Description":
			meta.Description = value
		case "Rationale":
			meta.Rationale = value
		}
	}
	return meta
}

func buildRoomDeliveryLogBody(label string, completed, inFlight, blockers, nextFocus []string, notes string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Label: %s\n", strings.TrimSpace(label))
	appendRoomSection(&b, "Completed", completed)
	appendRoomSection(&b, "InFlight", inFlight)
	appendRoomSection(&b, "Blockers", blockers)
	appendRoomSection(&b, "NextFocus", nextFocus)
	if strings.TrimSpace(notes) != "" {
		fmt.Fprintf(&b, "Notes: %s\n", strings.TrimSpace(notes))
	}
	return strings.TrimSpace(b.String())
}

func parseRoomDeliveryLogBody(body string) roomDeliveryLogMeta {
	meta := roomDeliveryLogMeta{}
	lines := strings.Split(body, "\n")
	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch trimmed {
		case "Completed:":
			section = "completed"
			continue
		case "InFlight:":
			section = "inflight"
			continue
		case "Blockers:":
			section = "blockers"
			continue
		case "NextFocus:":
			section = "next"
			continue
		}
		if section != "" && strings.HasPrefix(trimmed, "- ") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			switch section {
			case "completed":
				meta.Completed = append(meta.Completed, value)
			case "inflight":
				meta.InFlight = append(meta.InFlight, value)
			case "blockers":
				meta.Blockers = append(meta.Blockers, value)
			case "next":
				meta.NextFocus = append(meta.NextFocus, value)
			}
			continue
		}
		section = ""
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Label":
			meta.Label = value
		case "Notes":
			meta.Notes = value
		}
	}
	return meta
}

func normalizeRoomGuidanceKind(raw string) (string, error) {
	kind := strings.TrimSpace(strings.ToLower(raw))
	switch kind {
	case "process", "tooling", "coordination", "quality", "delivery":
		return kind, nil
	default:
		return "", fmt.Errorf("unsupported retro kind %q", raw)
	}
}

func normalizeRoomGuidanceUpdateMeta(meta roomGuidanceUpdateMeta) roomGuidanceUpdateMeta {
	meta.EpicID = strings.TrimSpace(meta.EpicID)
	meta.MilestoneID = strings.TrimSpace(meta.MilestoneID)
	meta.Kind = strings.TrimSpace(strings.ToLower(meta.Kind))
	meta.Summary = strings.TrimSpace(meta.Summary)
	meta.Impact = strings.TrimSpace(meta.Impact)
	meta.RecommendedChange = strings.TrimSpace(meta.RecommendedChange)
	meta.Scope = normalizeRoomList(meta.Scope, false)
	meta.FollowUp = normalizeRoomList(meta.FollowUp, false)
	return meta
}

func buildRoomGuidanceUpdateBody(meta roomGuidanceUpdateMeta) string {
	var b strings.Builder
	fmt.Fprintf(&b, "EpicID: %s\n", strings.TrimSpace(meta.EpicID))
	if strings.TrimSpace(meta.MilestoneID) != "" {
		fmt.Fprintf(&b, "MilestoneID: %s\n", strings.TrimSpace(meta.MilestoneID))
	}
	fmt.Fprintf(&b, "Kind: %s\n", strings.TrimSpace(meta.Kind))
	fmt.Fprintf(&b, "Summary: %s\n", strings.TrimSpace(meta.Summary))
	fmt.Fprintf(&b, "Impact: %s\n", strings.TrimSpace(meta.Impact))
	fmt.Fprintf(&b, "RecommendedChange: %s\n", strings.TrimSpace(meta.RecommendedChange))
	appendRoomSection(&b, "Scope", meta.Scope)
	appendRoomSection(&b, "FollowUp", meta.FollowUp)
	return strings.TrimSpace(b.String())
}

func parseRoomGuidanceUpdateBody(body string) roomGuidanceUpdateMeta {
	meta := roomGuidanceUpdateMeta{}
	lines := strings.Split(body, "\n")
	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		switch trimmed {
		case "Scope:":
			section = "scope"
			continue
		case "FollowUp:":
			section = "follow_up"
			continue
		}
		if section != "" && strings.HasPrefix(trimmed, "- ") {
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
			switch section {
			case "scope":
				meta.Scope = append(meta.Scope, value)
			case "follow_up":
				meta.FollowUp = append(meta.FollowUp, value)
			}
			continue
		}
		section = ""
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "EpicID":
			meta.EpicID = value
		case "MilestoneID":
			meta.MilestoneID = value
		case "Kind":
			meta.Kind = value
		case "Summary":
			meta.Summary = value
		case "Impact":
			meta.Impact = value
		case "RecommendedChange":
			meta.RecommendedChange = value
		}
	}
	return normalizeRoomGuidanceUpdateMeta(meta)
}

func appendRoomSection(b *strings.Builder, label string, items []string) {
	clean := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			clean = append(clean, item)
		}
	}
	if len(clean) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", label)
	for _, item := range clean {
		fmt.Fprintf(b, "- %s\n", item)
	}
}

type roomEpicShapingQA struct {
	Kind     string
	Question string
	Answer   string
}

type roomMilestoneProposal struct {
	Title     string
	Goal      string
	Scope     []string
	Rationale string
}

func deriveRoomEpicShapingInputs(messages []agent.BoardMessage, epicID string) (string, []roomEpicShapingQA) {
	brief := ""
	answersByQuestion := make(map[string]agent.BoardMessage)
	for _, msg := range messages {
		if msg.Kind == agent.BoardMessageKindEpicFinalize && strings.TrimSpace(msg.RelatedMessageID) == strings.TrimSpace(epicID) {
			brief = strings.TrimSpace(msg.Body)
		}
		if msg.Kind == agent.BoardMessageKindEpicAnswer {
			answersByQuestion[strings.TrimSpace(msg.RelatedMessageID)] = msg
		}
	}
	qa := make([]roomEpicShapingQA, 0)
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindEpicQuestion || strings.TrimSpace(msg.RelatedMessageID) != strings.TrimSpace(epicID) {
			continue
		}
		qMeta := parseRoomEpicQuestionBody(msg.Body)
		qaItem := roomEpicShapingQA{
			Kind:     firstNonEmpty(qMeta.Kind, "product"),
			Question: firstNonEmpty(qMeta.Question, strings.TrimSpace(msg.Body)),
		}
		if answer, ok := answersByQuestion[msg.ID]; ok {
			qaItem.Answer = strings.TrimSpace(answer.Body)
		}
		qa = append(qa, qaItem)
	}
	return brief, qa
}

func deriveRoomMilestoneProposals(epicMsg agent.BoardMessage, epicMeta roomEpicMeta, brief string, qa []roomEpicShapingQA, count int) []roomMilestoneProposal {
	scope := cleanRoomItems(epicMeta.Scope)
	success := cleanRoomItems(epicMeta.Success)
	if len(scope) == 0 && len(success) == 0 {
		return nil
	}
	if count < 1 {
		count = 1
	}

	proposals := make([]roomMilestoneProposal, 0, count)
	scopeChunks := chunkRoomItems(scope, 2)
	scopeProposalCap := count
	if len(success) > 0 && count > 1 {
		scopeProposalCap = count - 1
	}
	for i, chunk := range scopeChunks {
		if len(proposals) >= scopeProposalCap {
			break
		}
		title := fmt.Sprintf("Milestone %d", i+1)
		goal := "Advance the next scoped tranche of the epic."
		if i == 0 {
			goal = firstNonEmpty(epicMeta.Goal, "Turn the clarified epic brief into the first executable tranche.")
		}
		rationale := firstNonEmpty(strings.TrimSpace(brief), "Derived from the clarified epic brief.")
		if i < len(qa) && strings.TrimSpace(qa[i].Answer) != "" {
			rationale = qa[i].Answer
		}
		proposals = append(proposals, roomMilestoneProposal{
			Title:     title,
			Goal:      goal,
			Scope:     chunk,
			Rationale: rationale,
		})
	}
	if len(success) > 0 && len(proposals) < count {
		rationale := "Derived from the explicit success signals attached to the epic."
		for _, item := range qa {
			if item.Kind == "success" && strings.TrimSpace(item.Answer) != "" {
				rationale = item.Answer
				break
			}
		}
		proposals = append(proposals, roomMilestoneProposal{
			Title:     "Verification and adoption",
			Goal:      "Prove the epic meets its acceptance bar and is ready for broad use.",
			Scope:     success,
			Rationale: rationale,
		})
	}
	return proposals
}

func cleanRoomItems(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func chunkRoomItems(items []string, size int) [][]string {
	if size <= 0 {
		size = len(items)
	}
	out := make([][]string, 0)
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		out = append(out, append([]string(nil), items[i:end]...))
	}
	return out
}

func buildRoomEpicViews(messages []agent.BoardMessage) []map[string]any {
	questionsByEpic := make(map[string][]agent.BoardMessage)
	answersByQuestion := make(map[string]agent.BoardMessage)
	finalizeByEpic := make(map[string]agent.BoardMessage)
	proposalsByEpic := make(map[string][]map[string]any)
	for _, msg := range messages {
		switch msg.Kind {
		case agent.BoardMessageKindEpicQuestion:
			questionsByEpic[strings.TrimSpace(msg.RelatedMessageID)] = append(questionsByEpic[strings.TrimSpace(msg.RelatedMessageID)], msg)
		case agent.BoardMessageKindEpicAnswer:
			answersByQuestion[strings.TrimSpace(msg.RelatedMessageID)] = msg
		case agent.BoardMessageKindEpicFinalize:
			finalizeByEpic[strings.TrimSpace(msg.RelatedMessageID)] = msg
		case agent.BoardMessageKindMilestoneProposal:
			proposalsByEpic[strings.TrimSpace(msg.RelatedMessageID)] = append(proposalsByEpic[strings.TrimSpace(msg.RelatedMessageID)], map[string]any{
				"id":    msg.ID,
				"title": strings.TrimPrefix(strings.TrimSpace(msg.Subject), "Milestone Proposal: "),
				"root":  msg,
				"meta":  parseRoomMilestoneProposalBody(msg.Body),
			})
		}
	}
	milestonesByEpic := make(map[string][]map[string]any)
	for _, milestone := range buildRoomMilestoneViews(messages) {
		epicID, _ := milestone["epic_id"].(string)
		milestonesByEpic[epicID] = append(milestonesByEpic[epicID], milestone)
	}
	logsByEpic := make(map[string][]map[string]any)
	for _, entry := range buildRoomDeliveryLogViews(messages) {
		epicID, _ := entry["epic_id"].(string)
		logsByEpic[epicID] = append(logsByEpic[epicID], entry)
	}
	guidanceByEpic := make(map[string][]map[string]any)
	for _, update := range buildRoomGuidanceUpdateViews(messages) {
		epicID, _ := update["epic_id"].(string)
		guidanceByEpic[epicID] = append(guidanceByEpic[epicID], update)
	}
	out := make([]map[string]any, 0)
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindEpic {
			continue
		}
		meta := parseRoomEpicBody(msg.Body)
		questions := questionsByEpic[msg.ID]
		answers := make([]agent.BoardMessage, 0, len(questions))
		questionKinds := make(map[string]int)
		openQuestions := 0
		for _, question := range questions {
			qMeta := parseRoomEpicQuestionBody(question.Body)
			kind := firstNonEmpty(qMeta.Kind, "product")
			questionKinds[kind]++
			if answer, ok := answersByQuestion[question.ID]; ok {
				answers = append(answers, answer)
			} else {
				openQuestions++
			}
		}
		status := "discovery"
		if finalizeByEpic[msg.ID].ID != "" {
			status = "finalized"
		} else if len(questions) > 0 && openQuestions == 0 {
			status = "ready_to_finalize"
		} else if len(questions) > 0 {
			status = "intake_in_progress"
		}
		milestones := milestonesByEpic[msg.ID]
		logs := logsByEpic[msg.ID]
		guidanceUpdates := append([]map[string]any(nil), guidanceByEpic[msg.ID]...)
		sort.Slice(guidanceUpdates, func(i, j int) bool {
			left := anyMap(guidanceUpdates[i]["root"])
			right := anyMap(guidanceUpdates[j]["root"])
			leftAt := parseRFC3339Time(stringField(left, "created_at"))
			rightAt := parseRFC3339Time(stringField(right, "created_at"))
			if leftAt.Equal(rightAt) {
				return stringField(guidanceUpdates[i], "id") > stringField(guidanceUpdates[j], "id")
			}
			return leftAt.After(rightAt)
		})
		storyCount := 0
		for _, milestone := range milestones {
			if count, ok := milestone["story_count"].(int); ok {
				storyCount += count
			}
		}
		epicMessageIDs := uniqueStrings(append(
			append(
				append([]string{msg.ID}, collectRoomMapIDs(milestones)...),
				collectRoomMapIDs(logs)...,
			),
			collectRoomMapIDs(guidanceUpdates)...,
		))
		if finalizeMsg := finalizeByEpic[msg.ID]; strings.TrimSpace(finalizeMsg.ID) != "" {
			epicMessageIDs = uniqueStrings(append(epicMessageIDs, finalizeMsg.ID))
		}
		out = append(out, map[string]any{
			"id":                      msg.ID,
			"title":                   strings.TrimPrefix(strings.TrimSpace(msg.Subject), "Epic: "),
			"status":                  status,
			"source_kind":             "epic",
			"source_id":               msg.ID,
			"room_message_ids":        epicMessageIDs,
			"workpack_root":           roomAgileWorkpackRootPath(msg.ID),
			"epic_markdown":           roomAgileEpicMarkdownPath(msg.ID),
			"meta_json_path":          roomAgileEpicMetaJSONPath(msg.ID),
			"delivery_log_markdown":   roomAgileDeliveryLogMarkdownPath(msg.ID),
			"retro_markdown":          roomAgileRetroMarkdownPath(msg.ID),
			"root":                    msg,
			"meta":                    meta,
			"questions":               questions,
			"question_kinds":          questionKinds,
			"question_count":          len(questions),
			"answers":                 answers,
			"answer_count":            len(answers),
			"open_questions":          openQuestions,
			"finalized":               finalizeByEpic[msg.ID].ID != "",
			"final_brief":             finalizeByEpic[msg.ID],
			"proposals":               proposalsByEpic[msg.ID],
			"proposal_count":          len(proposalsByEpic[msg.ID]),
			"milestones":              milestones,
			"milestone_count":         len(milestones),
			"story_count":             storyCount,
			"logs":                    logs,
			"log_count":               len(logs),
			"guidance_updates":        guidanceUpdates,
			"guidance_update_count":   len(guidanceUpdates),
			"latest_guidance_updates": truncateRoomMapSlice(guidanceUpdates, 3),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]["root"].(agent.BoardMessage)
		right := out[j]["root"].(agent.BoardMessage)
		return left.CreatedAt.After(right.CreatedAt)
	})
	return out
}

func roomEpicViewByID(epics []map[string]any, epicID string) map[string]any {
	epicID = strings.TrimSpace(epicID)
	for _, epic := range epics {
		if stringField(epic, "id") == epicID {
			return epic
		}
	}
	return nil
}

func roomMilestoneViewByID(milestones []map[string]any, milestoneID string) map[string]any {
	milestoneID = strings.TrimSpace(milestoneID)
	for _, milestone := range milestones {
		if stringField(milestone, "id") == milestoneID {
			return milestone
		}
	}
	return nil
}

func roomStoryViewByID(stories []map[string]any, storyID string) map[string]any {
	storyID = strings.TrimSpace(storyID)
	for _, story := range stories {
		if stringField(story, "id") == storyID {
			return story
		}
	}
	return nil
}

func buildRoomEpicContinuity(room agent.RoomSummary, messages []agent.BoardMessage, epic map[string]any) map[string]any {
	milestones := mapSlice(epic["milestones"])
	currentMilestone := findCurrentRoomMilestone(milestones)
	missingValidation := buildRoomStoriesMissingValidation(currentMilestone)
	latestLog := roomEpicLatestLog(epic)
	openInterviewItems := buildRoomOpenInterviewItems(messages)
	phase := deriveRoomEpicPhase(epic, currentMilestone, missingValidation, openInterviewItems)
	summary := summarizeRoomEpicContinuity(epic, currentMilestone, missingValidation, latestLog, openInterviewItems, phase)

	out := map[string]any{
		"epic_id":                    stringField(epic, "id"),
		"title":                      stringField(epic, "title"),
		"status":                     stringField(epic, "status"),
		"phase":                      phase,
		"finalized":                  boolField(epic, "finalized"),
		"current_milestone_id":       stringField(currentMilestone, "id"),
		"current_milestone_title":    stringField(currentMilestone, "title"),
		"milestone_count":            intField(epic, "milestone_count"),
		"story_count":                intField(epic, "story_count"),
		"accepted_story_count":       sumRoomIntField(milestones, "accepted_story_count"),
		"validated_story_count":      sumRoomIntField(milestones, "validated_story_count"),
		"blocked_story_count":        sumRoomIntField(milestones, "blocked_story_count"),
		"open_intake_questions":      intField(epic, "open_questions"),
		"open_interview_items":       len(openInterviewItems),
		"stories_missing_validation": missingValidation,
		"latest_log_label":           stringField(latestLog, "label"),
		"latest_log_notes":           stringField(anyMap(latestLog["meta"]), "notes"),
		"guidance_update_count":      intField(epic, "guidance_update_count"),
		"workpack_root":              stringField(epic, "workpack_root"),
		"summary":                    summary,
	}
	if currentMilestone != nil {
		out["current_milestone"] = currentMilestone
	}
	if latestLog != nil {
		out["latest_log"] = latestLog
	}
	if len(openInterviewItems) > 0 {
		out["interview_items"] = openInterviewItems
	}
	if guidance := mapSlice(epic["latest_guidance_updates"]); len(guidance) > 0 {
		out["recent_guidance_updates"] = guidance
	}
	return out
}

func sumRoomIntField(items []map[string]any, key string) int {
	total := 0
	for _, item := range items {
		total += intField(item, key)
	}
	return total
}

func findCurrentRoomMilestone(milestones []map[string]any) map[string]any {
	if len(milestones) == 0 {
		return nil
	}
	candidates := append([]map[string]any(nil), milestones...)
	sort.Slice(candidates, func(i, j int) bool {
		left := mapField(candidates[i], "root")
		right := mapField(candidates[j], "root")
		leftAt := time.Time{}
		rightAt := time.Time{}
		if left != nil {
			leftAt = parseRFC3339Time(stringField(left, "created_at"))
		}
		if right != nil {
			rightAt = parseRFC3339Time(stringField(right, "created_at"))
		}
		if leftAt.Equal(rightAt) {
			return stringField(candidates[i], "id") < stringField(candidates[j], "id")
		}
		return leftAt.After(rightAt)
	})
	for _, milestone := range candidates {
		if stringField(milestone, "status") != "passed" {
			return milestone
		}
	}
	return candidates[0]
}

func buildRoomStoriesMissingValidation(currentMilestone map[string]any) []map[string]any {
	if currentMilestone == nil {
		return nil
	}
	stories := mapSlice(currentMilestone["stories"])
	missing := make([]map[string]any, 0)
	for _, story := range stories {
		if stringField(story, "status") != "accepted" {
			continue
		}
		if boolField(story, "covered") {
			continue
		}
		missing = append(missing, map[string]any{
			"id":             stringField(story, "id"),
			"title":          stringField(story, "title"),
			"owner":          stringField(anyMap(story["meta"]), "owner"),
			"milestone_id":   stringField(story, "milestone_id"),
			"workpack_dir":   stringField(story, "workpack_dir"),
			"validation_dir": stringField(story, "validation_dir"),
		})
	}
	sort.Slice(missing, func(i, j int) bool {
		return stringField(missing[i], "id") < stringField(missing[j], "id")
	})
	return missing
}

func roomEpicLatestLog(epic map[string]any) map[string]any {
	logs := mapSlice(epic["logs"])
	if len(logs) == 0 {
		return nil
	}
	return logs[0]
}

func buildRoomOpenInterviewItems(messages []agent.BoardMessage) []map[string]any {
	sessions := buildRoomInterviewSessions(messages)
	items := make([]map[string]any, 0)
	for _, session := range sessions {
		status := stringField(session, "status")
		if status == "verified" || status == "rejected" {
			continue
		}
		items = append(items, map[string]any{
			"id":     stringField(session, "id"),
			"topic":  stringField(session, "topic"),
			"status": status,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return stringField(items[i], "id") < stringField(items[j], "id")
	})
	return items
}

func deriveRoomEpicPhase(epic, currentMilestone map[string]any, missingValidation []map[string]any, openInterviewItems []map[string]any) string {
	if !boolField(epic, "finalized") {
		return "discovery"
	}
	if currentMilestone == nil {
		if intField(epic, "milestone_count") > 0 {
			return "completed"
		}
		return "shaping"
	}
	exitPolicy := roomMilestoneExitPolicy(currentMilestone)
	if stringField(exitPolicy, "status") == "blocked" {
		return "blocked"
	}
	if roomMilestoneNeedsReview(currentMilestone) || roomMilestoneNeedsSummary(currentMilestone) {
		return "review"
	}
	if stringField(currentMilestone, "status") == "passed" && intField(epic, "milestone_count") > 0 && len(missingValidation) == 0 && len(openInterviewItems) == 0 {
		return "completed"
	}
	return "execution"
}

func roomMilestoneNeedsReview(milestone map[string]any) bool {
	return stringField(roomMilestoneExitPolicy(milestone), "status") == "ready_for_review"
}

func roomMilestoneNeedsSummary(milestone map[string]any) bool {
	return stringField(roomMilestoneExitPolicy(milestone), "status") == "ready_for_summary"
}

func summarizeRoomEpicContinuity(epic, currentMilestone map[string]any, missingValidation []map[string]any, latestLog map[string]any, openInterviewItems []map[string]any, phase string) string {
	parts := []string{
		fmt.Sprintf("Epic is in %s.", phase),
	}
	if currentMilestone != nil {
		parts = append(parts, fmt.Sprintf("Current milestone is %q.", stringField(currentMilestone, "title")))
	}
	if count := len(missingValidation); count > 0 {
		parts = append(parts, fmt.Sprintf("%d accepted stor%s still need validation.", count, pluralSuffix(count, "y", "ies")))
	}
	if openQuestions := intField(epic, "open_questions"); openQuestions > 0 {
		parts = append(parts, fmt.Sprintf("%d intake question%s remain open.", openQuestions, pluralS(openQuestions)))
	}
	if count := len(openInterviewItems); count > 0 {
		parts = append(parts, fmt.Sprintf("%d interview item%s remain unresolved.", count, pluralS(count)))
	}
	if latestLog != nil {
		label := stringField(latestLog, "label")
		nextFocus := stringSliceField(anyMap(latestLog["meta"]), "next_focus")
		switch {
		case label != "" && len(nextFocus) > 0:
			parts = append(parts, fmt.Sprintf("Latest log is %q and next focus is %q.", label, nextFocus[0]))
		case label != "":
			parts = append(parts, fmt.Sprintf("Latest log is %q.", label))
		}
	}
	if guidanceCount := intField(epic, "guidance_update_count"); guidanceCount > 0 {
		parts = append(parts, fmt.Sprintf("%d guidance update%s captured.", guidanceCount, pluralS(guidanceCount)))
	}
	return strings.Join(parts, " ")
}

func buildRoomEpicHealth(room agent.RoomSummary, messages []agent.BoardMessage, epic map[string]any, actorID string) map[string]any {
	resume := buildRoomEpicContinuity(room, messages, epic)
	milestones := mapSlice(epic["milestones"])
	currentMilestone := mapField(resume, "current_milestone")
	missingValidation := buildRoomEpicStoriesMissingValidation(milestones)
	issues := buildRoomEpicHealthIssues(room, messages, epic, milestones, currentMilestone, missingValidation, actorID)
	healthStatus := deriveRoomEpicHealthStatus(epic, milestones, issues)
	recentLogs := truncateRoomMapSlice(mapSlice(epic["logs"]), 3)
	recentGuidance := truncateRoomMapSlice(mapSlice(epic["guidance_updates"]), 3)

	out := map[string]any{
		"epic_id":                           stringField(epic, "id"),
		"title":                             stringField(epic, "title"),
		"health":                            healthStatus,
		"phase":                             stringField(resume, "phase"),
		"current_milestone_id":              stringField(resume, "current_milestone_id"),
		"current_milestone_title":           stringField(resume, "current_milestone_title"),
		"summary":                           summarizeRoomEpicHealth(healthStatus, issues, epic, currentMilestone, missingValidation),
		"open_intake_questions":             intField(epic, "open_questions"),
		"open_interview_items":              len(mapSlice(resume["interview_items"])),
		"milestone_count":                   intField(epic, "milestone_count"),
		"active_milestone_count":            countRoomActiveMilestones(milestones),
		"stories_missing_validation_count":  len(missingValidation),
		"stories_missing_validation":        missingValidation,
		"blocked_story_count":               sumRoomIntField(milestones, "blocked_story_count"),
		"stale_milestone_summary_count":     countRoomStaleMilestoneSummaries(milestones),
		"milestones_missing_contract_count": countRoomMilestonesMissingContract(milestones),
		"guidance_update_count":             intField(epic, "guidance_update_count"),
		"issues":                            issues,
		"issue_count":                       len(issues),
		"recent_guidance_updates":           recentGuidance,
		"recent_delivery_logs":              recentLogs,
	}
	if actor := strings.TrimSpace(actorID); actor != "" {
		out["actor"] = actor
	}
	if currentMilestone != nil {
		out["current_milestone"] = currentMilestone
	}
	return out
}

func buildRoomEpicStoriesMissingValidation(milestones []map[string]any) []map[string]any {
	missing := make([]map[string]any, 0)
	for _, milestone := range milestones {
		for _, story := range mapSlice(milestone["stories"]) {
			if stringField(story, "status") != "accepted" || boolField(story, "covered") {
				continue
			}
			missing = append(missing, map[string]any{
				"id":              stringField(story, "id"),
				"title":           stringField(story, "title"),
				"owner":           stringField(anyMap(story["meta"]), "owner"),
				"milestone_id":    stringField(story, "milestone_id"),
				"milestone_title": stringField(milestone, "title"),
				"workpack_dir":    stringField(story, "workpack_dir"),
				"validation_dir":  stringField(story, "validation_dir"),
			})
		}
	}
	sort.Slice(missing, func(i, j int) bool {
		leftMilestone := stringField(missing[i], "milestone_id")
		rightMilestone := stringField(missing[j], "milestone_id")
		if leftMilestone != rightMilestone {
			return leftMilestone < rightMilestone
		}
		return stringField(missing[i], "id") < stringField(missing[j], "id")
	})
	return missing
}

func buildRoomEpicHealthIssues(room agent.RoomSummary, messages []agent.BoardMessage, epic map[string]any, milestones []map[string]any, currentMilestone map[string]any, missingValidation []map[string]any, actorID string) []map[string]any {
	issues := make([]map[string]any, 0)
	roomID := room.ID
	epicID := stringField(epic, "id")
	coordinator := roomCoordinatorActorID(room.Members)
	if coordinator == "" {
		coordinator = strings.TrimSpace(actorID)
	}

	if open := intField(epic, "open_questions"); open > 0 {
		issues = append(issues, roomEpicHealthIssue("intake_open", "warn", epicID, "Epic intake remains open", fmt.Sprintf("%d intake question%s still need answers or finalization.", open, pluralS(open)), fmt.Sprintf(`agentctl room epic show %s %s`, roomID, epicID)))
	}

	for _, interview := range buildRoomOpenInterviewItems(messages) {
		targetID := stringField(interview, "id")
		commandHint := fmt.Sprintf(`agentctl room status %s --only interview`, roomID)
		if coordinator != "" {
			commandHint = fmt.Sprintf(`agentctl room interview next %s --actor %s`, roomID, coordinator)
		}
		issues = append(issues, roomEpicHealthIssue("interview_unresolved", "warn", targetID, firstNonEmpty(stringField(interview, "topic"), "Resolve interview item"), "There is at least one unresolved interview thread in the room.", commandHint))
	}

	if intField(epic, "story_count") > 0 && roomEpicLatestLog(epic) == nil {
		issues = append(issues, roomEpicHealthIssue("epic_has_no_log", "info", epicID, "Epic has no delivery log entries", "The epic has active scope but no delivery log checkpoint yet.", fmt.Sprintf(`agentctl room log append %s %s "<label>"`, roomID, epicID)))
	}

	for _, milestone := range milestones {
		milestoneID := stringField(milestone, "id")
		milestoneTitle := firstNonEmpty(stringField(milestone, "title"), milestoneID)
		exitPolicy := anyMap(milestone["exit_policy"])
		exitReasons := stringSliceField(exitPolicy, "reasons")
		if roomMilestoneMissingContract(milestone) {
			issues = append(issues, roomEpicHealthIssue("milestone_missing_contract", "warn", milestoneID, milestoneTitle, "Milestone is missing an explicit contract (objective, validators, or exit criteria).", fmt.Sprintf(`agentctl room milestone contract %s %s --objective "<objective>" --validator review --exit "<exit>"`, roomID, milestoneID)))
		}
		if intField(milestone, "criteria_count") == 0 {
			issues = append(issues, roomEpicHealthIssue("milestone_missing_criteria", "warn", milestoneID, milestoneTitle, "Milestone has no acceptance criteria yet.", fmt.Sprintf(`agentctl room milestone criteria %s %s "<criterion>"`, roomID, milestoneID)))
		}
		if stringField(exitPolicy, "status") == "ready_for_review" {
			issues = append(issues, roomEpicHealthIssue("milestone_needs_review", "warn", milestoneID, milestoneTitle, "Accepted stories are covered and the milestone is ready for review.", fmt.Sprintf(`agentctl room milestone review %s %s pass "<notes>"`, roomID, milestoneID)))
		}
		if stringField(exitPolicy, "status") == "ready_for_summary" {
			issues = append(issues, roomEpicHealthIssue("milestone_needs_summary", "info", milestoneID, milestoneTitle, "Milestone has a review verdict but no summary yet.", fmt.Sprintf(`agentctl room milestone summary %s %s --summary "<summary>"`, roomID, milestoneID)))
		}
		if roomMilestoneSummaryIsStale(milestone) {
			issues = append(issues, roomEpicHealthIssue("stale_summary", "warn", milestoneID, milestoneTitle, "Milestone summary is older than the latest material change in this milestone.", fmt.Sprintf(`agentctl room milestone summary %s %s --summary "<summary>"`, roomID, milestoneID)))
		}
		if stringSliceContains(exitReasons, "missing_required_lane") {
			_, _, requiredLaneMissing := roomMilestoneRequiredLaneStatus(milestone)
			for _, lane := range requiredLaneMissing {
				storyHint := roomMilestoneFirstAcceptedStoryMissingLane(milestone, lane, actorID, coordinator)
				issues = append(issues, roomEpicHealthIssue(
					"milestone_missing_required_lane",
					"warn",
					fmt.Sprintf("%s:%s", milestoneID, lane),
					milestoneTitle,
					fmt.Sprintf("Milestone requires evidence lane %q, but no accepted story covers it yet.", lane),
					roomMilestoneRequiredLaneIssueHint(roomID, milestone, storyHint, lane),
				))
			}
		}
		if stringSliceContains(exitReasons, "has_failed_validation") {
			issues = append(issues, roomEpicHealthIssue("milestone_failed_validation", "block", milestoneID, milestoneTitle, "Milestone has a failing validation and is not ready to exit.", fmt.Sprintf(`agentctl room milestone show %s %s`, roomID, milestoneID)))
		}
		for _, story := range mapSlice(milestone["stories"]) {
			if stringField(story, "status") == "accepted" && !boolField(story, "covered") && stringSliceContains(exitReasons, "accepted_stories_uncovered") {
				issues = append(issues, roomEpicHealthIssue("story_missing_validation", "warn", stringField(story, "id"), firstNonEmpty(stringField(story, "title"), "Validate story"), "Accepted story still lacks validation coverage.", fmt.Sprintf(`agentctl room story validate %s %s review pass "<summary>"`, roomID, stringField(story, "id"))))
			}
			if stringField(story, "state") == "blocked" || stringField(story, "latest_validation_status") == "blocked" {
				issues = append(issues, roomEpicHealthIssue("story_blocked", "block", stringField(story, "id"), firstNonEmpty(stringField(story, "title"), "Story blocked"), "Story is blocked and needs coordinator follow-up.", fmt.Sprintf(`agentctl room story show %s %s`, roomID, stringField(story, "id"))))
			}
		}
	}

	sort.Slice(issues, func(i, j int) bool {
		leftSeverity := roomEpicHealthSeverityRank(stringField(issues[i], "severity"))
		rightSeverity := roomEpicHealthSeverityRank(stringField(issues[j], "severity"))
		if leftSeverity != rightSeverity {
			return leftSeverity < rightSeverity
		}
		leftType := stringField(issues[i], "type")
		rightType := stringField(issues[j], "type")
		if leftType != rightType {
			return leftType < rightType
		}
		return stringField(issues[i], "target_id") < stringField(issues[j], "target_id")
	})
	return issues
}

func roomEpicHealthIssue(issueType, severity, targetID, title, reason, commandHint string) map[string]any {
	return map[string]any{
		"type":         issueType,
		"severity":     severity,
		"target_id":    strings.TrimSpace(targetID),
		"title":        strings.TrimSpace(title),
		"reason":       strings.TrimSpace(reason),
		"command_hint": strings.TrimSpace(commandHint),
	}
}

func roomEpicHealthSeverityRank(severity string) int {
	switch strings.TrimSpace(strings.ToLower(severity)) {
	case "block":
		return 0
	case "warn":
		return 1
	case "info":
		return 2
	default:
		return 3
	}
}

func deriveRoomEpicHealthStatus(epic map[string]any, milestones, issues []map[string]any) string {
	hasBlock := false
	hasWarn := false
	infoOnly := len(issues) > 0
	closingOnly := true
	currentMilestone := findCurrentRoomMilestone(milestones)
	for _, issue := range issues {
		switch stringField(issue, "severity") {
		case "block":
			hasBlock = true
		case "warn":
			hasWarn = true
		}
		switch stringField(issue, "type") {
		case "milestone_needs_summary", "epic_has_no_log":
		default:
			closingOnly = false
		}
	}
	if hasBlock {
		return "blocked"
	}
	if hasWarn {
		return "needs_attention"
	}
	if roomEpicIsComplete(epic, milestones, issues) {
		return "complete"
	}
	if infoOnly && closingOnly && currentMilestone != nil && stringField(currentMilestone, "status") == "passed" {
		return "closing"
	}
	return "healthy"
}

func roomEpicIsComplete(epic map[string]any, milestones, issues []map[string]any) bool {
	if !boolField(epic, "finalized") || intField(epic, "milestone_count") == 0 || len(issues) > 0 {
		return false
	}
	for _, milestone := range milestones {
		if stringField(milestone, "status") != "passed" {
			return false
		}
	}
	return roomEpicLatestLog(epic) != nil
}

func summarizeRoomEpicHealth(health string, issues []map[string]any, epic, currentMilestone map[string]any, missingValidation []map[string]any) string {
	parts := []string{fmt.Sprintf("Epic health is %s.", health)}
	if currentMilestone != nil {
		parts = append(parts, fmt.Sprintf("Current milestone is %q.", stringField(currentMilestone, "title")))
	}
	if len(missingValidation) > 0 {
		parts = append(parts, fmt.Sprintf("%d stor%s still need validation.", len(missingValidation), pluralSuffix(len(missingValidation), "y", "ies")))
	}
	if len(issues) > 0 {
		first := issues[0]
		parts = append(parts, fmt.Sprintf("Top issue: %s.", strings.TrimSpace(stringField(first, "reason"))))
	} else if roomEpicLatestLog(epic) != nil {
		parts = append(parts, fmt.Sprintf("Latest delivery log is %q.", stringField(roomEpicLatestLog(epic), "label")))
	}
	return strings.Join(parts, " ")
}

func countRoomActiveMilestones(milestones []map[string]any) int {
	total := 0
	for _, milestone := range milestones {
		if stringField(milestone, "status") != "passed" {
			total++
		}
	}
	return total
}

func countRoomMilestonesMissingContract(milestones []map[string]any) int {
	total := 0
	for _, milestone := range milestones {
		if roomMilestoneMissingContract(milestone) {
			total++
		}
	}
	return total
}

func roomMilestoneMissingContract(milestone map[string]any) bool {
	if milestone == nil {
		return false
	}
	meta := anyMap(milestone["meta"])
	return stringField(meta, "objective") == "" &&
		intField(milestone, "validator_count") == 0 &&
		intField(milestone, "required_evidence_lane_count") == 0 &&
		intField(milestone, "optional_evidence_lane_count") == 0 &&
		intField(milestone, "exit_criteria_count") == 0
}

func roomMilestoneExitPolicy(milestone map[string]any) map[string]any {
	if milestone == nil {
		return map[string]any{
			"status":  "not_ready",
			"reasons": []string{"milestone_missing"},
			"checks":  map[string]any{},
		}
	}
	requiredLaneStatus, _, _ := roomMilestoneRequiredLaneStatus(milestone)
	acceptedStories := intField(milestone, "accepted_story_count")
	validatedStories := intField(milestone, "validated_story_count")
	checks := map[string]any{
		"accepted_stories_covered": acceptedStories > 0 && validatedStories >= acceptedStories,
		"required_lanes_satisfied": requiredLaneStatus != "missing",
		"has_blocking_story":       intField(milestone, "blocked_story_count") > 0,
		"has_failed_validation":    intField(milestone, "failed_story_count") > 0,
		"has_review":               intField(milestone, "review_count") > 0,
		"has_summary":              intField(milestone, "summary_count") > 0,
	}
	reasons := make([]string, 0, 4)
	if boolField(checks, "has_blocking_story") {
		reasons = append(reasons, "has_blocking_story")
	}
	if boolField(checks, "has_failed_validation") {
		reasons = append(reasons, "has_failed_validation")
	}
	if !boolField(checks, "accepted_stories_covered") {
		reasons = append(reasons, "accepted_stories_uncovered")
	}
	if !boolField(checks, "required_lanes_satisfied") {
		reasons = append(reasons, "missing_required_lane")
	}
	if !boolField(checks, "has_review") {
		reasons = append(reasons, "missing_review")
	}
	if !boolField(checks, "has_summary") {
		reasons = append(reasons, "missing_summary")
	}
	status := "not_ready"
	switch {
	case boolField(checks, "has_blocking_story") || boolField(checks, "has_failed_validation"):
		status = "blocked"
	case !boolField(checks, "accepted_stories_covered") || !boolField(checks, "required_lanes_satisfied"):
		status = "not_ready"
	case !boolField(checks, "has_review"):
		status = "ready_for_review"
	case !boolField(checks, "has_summary"):
		status = "ready_for_summary"
	default:
		status = "ready_to_exit"
	}
	sort.Strings(reasons)
	return map[string]any{
		"status":  status,
		"reasons": reasons,
		"checks":  checks,
	}
}

func roomMilestoneRequiredLaneStatus(milestone map[string]any) (string, []string, []string) {
	required := stringSliceField(anyMap(milestone["meta"]), "required_evidence_lanes")
	if len(required) == 0 {
		return "not_configured", nil, nil
	}
	coverage := anyMap(milestone["lane_coverage"])
	covered := make([]string, 0, len(required))
	missing := make([]string, 0, len(required))
	for _, lane := range required {
		if intField(coverage, lane) > 0 {
			covered = append(covered, lane)
			continue
		}
		missing = append(missing, lane)
	}
	if len(missing) == 0 {
		return "satisfied", covered, nil
	}
	return "missing", covered, missing
}

func roomMilestoneFirstAcceptedStoryMissingLane(milestone map[string]any, lane, actorID, coordinatorID string) map[string]any {
	lane = strings.TrimSpace(lane)
	if lane == "" {
		return nil
	}
	actorID = strings.TrimSpace(actorID)
	coordinatorID = strings.TrimSpace(coordinatorID)
	for _, story := range mapSlice(milestone["stories"]) {
		if stringField(story, "status") != "accepted" {
			continue
		}
		if boolField(anyMap(mapField(story, "evidence_lanes")[lane]), "covered") {
			continue
		}
		if actorID != "" && actorID != coordinatorID {
			owner := stringField(anyMap(story["meta"]), "owner")
			if owner != "" && !sameRoomParticipant(owner, actorID) {
				continue
			}
		}
		return story
	}
	return nil
}

func roomMilestoneRequiredLaneIssueHint(roomID string, milestone, story map[string]any, lane string) string {
	if story != nil {
		return fmt.Sprintf(`agentctl room story validate %s %s %s pass "<summary>"`, roomID, stringField(story, "id"), lane)
	}
	return fmt.Sprintf(`agentctl room milestone show %s %s`, roomID, stringField(milestone, "id"))
}

func countRoomStaleMilestoneSummaries(milestones []map[string]any) int {
	total := 0
	for _, milestone := range milestones {
		if roomMilestoneSummaryIsStale(milestone) {
			total++
		}
	}
	return total
}

func roomMilestoneSummaryIsStale(milestone map[string]any) bool {
	if milestone == nil || intField(milestone, "accepted_story_count") == 0 || stringField(milestone, "status") == "blocked" || intField(milestone, "summary_count") == 0 {
		return false
	}
	summaryMarker := roomMilestoneLatestSummaryMarker(milestone)
	if summaryMarker.At.IsZero() {
		return false
	}
	return roomTimelineMarkerAfter(roomMilestoneLatestMaterialChangeMarker(milestone), summaryMarker)
}

type roomTimelineMarker struct {
	At time.Time
	ID string
}

func roomMilestoneLatestSummaryMarker(milestone map[string]any) roomTimelineMarker {
	latestSummary := mapField(milestone, "latest_summary")
	if latestSummary == nil {
		return roomTimelineMarker{}
	}
	root := mapField(latestSummary, "root")
	return roomTimelineMarker{
		At: parseRFC3339Time(stringField(root, "created_at")),
		ID: stringField(root, "id"),
	}
}

func roomMilestoneLatestMaterialChangeMarker(milestone map[string]any) roomTimelineMarker {
	latest := roomTimelineMarker{}
	for _, story := range mapSlice(milestone["stories"]) {
		if stringField(story, "status") != "accepted" {
			continue
		}
		root := mapField(story, "root")
		latest = roomLaterTimelineMarker(latest, roomTimelineMarker{At: parseRFC3339Time(stringField(root, "created_at")), ID: stringField(root, "id")})
		for _, state := range mapSlice(story["state_history"]) {
			root := mapField(state, "root")
			latest = roomLaterTimelineMarker(latest, roomTimelineMarker{At: parseRFC3339Time(stringField(root, "created_at")), ID: stringField(root, "id")})
		}
		for _, validation := range mapSlice(story["validations"]) {
			root := mapField(validation, "root")
			latest = roomLaterTimelineMarker(latest, roomTimelineMarker{At: parseRFC3339Time(stringField(root, "created_at")), ID: stringField(root, "id")})
		}
	}
	for _, review := range boardMessageSliceValue(milestone["reviews"]) {
		latest = roomLaterTimelineMarker(latest, roomTimelineMarker{At: review.CreatedAt, ID: review.ID})
	}
	return latest
}

func roomLaterTimelineMarker(left, right roomTimelineMarker) roomTimelineMarker {
	if roomTimelineMarkerAfter(right, left) {
		return right
	}
	return left
}

func roomTimelineMarkerAfter(left, right roomTimelineMarker) bool {
	if left.At.After(right.At) {
		return true
	}
	if left.At.Equal(right.At) && strings.TrimSpace(left.ID) != "" && strings.TrimSpace(left.ID) > strings.TrimSpace(right.ID) {
		return true
	}
	return false
}

func pluralS(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func pluralSuffix(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func parseRFC3339Time(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err == nil {
		return parsed
	}
	parsed, _ = time.Parse(time.RFC3339, raw)
	return parsed
}

func buildRoomEpicNextItems(room agent.RoomSummary, messages []agent.BoardMessage, epic, resume map[string]any, actorID string) ([]map[string]any, string) {
	items := make([]map[string]any, 0)
	epicID := stringField(epic, "id")
	roomID := room.ID
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		actorID = roomCoordinatorActorID(room.Members)
	}
	if stringField(resume, "phase") == "completed" {
		return items, "no open work"
	}

	answerByQuestion := make(map[string]agent.BoardMessage)
	for _, msg := range messages {
		if msg.Kind == agent.BoardMessageKindEpicAnswer {
			answerByQuestion[strings.TrimSpace(msg.RelatedMessageID)] = msg
		}
	}
	for _, question := range boardMessageSliceValue(epic["questions"]) {
		questionID := strings.TrimSpace(question.ID)
		if questionID == "" || answerByQuestion[questionID].ID != "" {
			continue
		}
		qMeta := parseRoomEpicQuestionBody(question.Body)
		recipient := normalizeRoomRecipient(question.Recipient)
		if recipient != actorID && actorID != roomCoordinatorActorID(room.Members) {
			continue
		}
		items = append(items, map[string]any{
			"type":         "answer_intake_question",
			"priority":     1,
			"target_id":    questionID,
			"title":        firstNonEmpty(qMeta.Question, strings.TrimSpace(question.Subject)),
			"reason":       "Epic intake is still open and this question does not have an answer yet.",
			"command_hint": fmt.Sprintf(`agentctl room epic answer %s %s "<answer>"`, roomID, questionID),
		})
	}

	if boolField(epic, "finalized") == false && intField(epic, "open_questions") == 0 {
		items = append(items, map[string]any{
			"type":         "finalize_epic",
			"priority":     1,
			"target_id":    epicID,
			"title":        "Finalize epic brief",
			"reason":       "Epic intake is answered and ready to finalize.",
			"command_hint": fmt.Sprintf(`agentctl room epic finalize %s %s "<summary>"`, roomID, epicID),
		})
	}

	currentMilestone := mapField(resume, "current_milestone")
	if currentMilestone == nil {
		if boolField(epic, "finalized") {
			proposals := mapSlice(epic["proposals"])
			if len(proposals) > 0 {
				for _, proposal := range proposals {
					items = append(items, map[string]any{
						"type":         "start_milestone_from_proposal",
						"priority":     2,
						"target_id":    stringField(proposal, "id"),
						"title":        firstNonEmpty(stringField(proposal, "title"), "Start milestone from proposal"),
						"reason":       "Epic is finalized and has a pending milestone proposal.",
						"command_hint": fmt.Sprintf(`agentctl room milestone start %s %s --proposal %s`, roomID, epicID, stringField(proposal, "id")),
					})
				}
			} else {
				items = append(items, map[string]any{
					"type":         "shape_milestones",
					"priority":     2,
					"target_id":    epicID,
					"title":        "Shape milestone proposals",
					"reason":       "Epic is finalized but does not yet have milestone proposals.",
					"command_hint": fmt.Sprintf(`agentctl room epic shape %s %s`, roomID, epicID),
				})
			}
		}
	} else {
		milestoneID := stringField(currentMilestone, "id")
		policyCoveredStoryIDs := make(map[string]struct{})
		exitPolicy := anyMap(currentMilestone["exit_policy"])
		exitReasons := stringSliceField(exitPolicy, "reasons")
		if intField(currentMilestone, "criteria_count") == 0 {
			items = append(items, map[string]any{
				"type":         "add_milestone_criteria",
				"priority":     3,
				"target_id":    milestoneID,
				"title":        "Add milestone acceptance criteria",
				"reason":       "Current milestone has no acceptance criteria yet.",
				"command_hint": fmt.Sprintf(`agentctl room milestone criteria %s %s "<criterion>"`, roomID, milestoneID),
			})
		}
		if stringSliceContains(exitReasons, "missing_required_lane") {
			coordinator := roomCoordinatorActorID(room.Members)
			_, _, requiredLaneMissing := roomMilestoneRequiredLaneStatus(currentMilestone)
			for _, lane := range requiredLaneMissing {
				storyHint := roomMilestoneFirstAcceptedStoryMissingLane(currentMilestone, lane, actorID, coordinator)
				targetID := milestoneID
				title := fmt.Sprintf("Cover required evidence lane %s", lane)
				reason := fmt.Sprintf("Current milestone requires lane %q, but no accepted story covers it yet.", lane)
				commandHint := roomMilestoneRequiredLaneIssueHint(roomID, currentMilestone, storyHint, lane)
				if storyHint != nil {
					targetID = stringField(storyHint, "id")
					policyCoveredStoryIDs[targetID] = struct{}{}
					title = firstNonEmpty(stringField(storyHint, "title"), title)
					reason = fmt.Sprintf("Story needs evidence in required lane %q for the current milestone.", lane)
				}
				items = append(items, map[string]any{
					"type":         "cover_required_lane",
					"priority":     3,
					"target_id":    targetID,
					"title":        title,
					"reason":       reason,
					"command_hint": commandHint,
				})
			}
		}

		for _, story := range mapSlice(currentMilestone["stories"]) {
			if stringField(story, "status") != "accepted" || boolField(story, "covered") || !stringSliceContains(exitReasons, "accepted_stories_uncovered") {
				continue
			}
			if _, ok := policyCoveredStoryIDs[stringField(story, "id")]; ok {
				continue
			}
			owner := stringField(anyMap(story["meta"]), "owner")
			if actorID != roomCoordinatorActorID(room.Members) && owner != "" && !sameRoomParticipant(owner, actorID) {
				continue
			}
			items = append(items, map[string]any{
				"type":         "validate_story",
				"priority":     3,
				"target_id":    stringField(story, "id"),
				"title":        firstNonEmpty(stringField(story, "title"), "Validate story"),
				"reason":       "Accepted story still lacks validation coverage.",
				"command_hint": fmt.Sprintf(`agentctl room story validate %s %s review pass "<summary>"`, roomID, stringField(story, "id")),
			})
		}

		if stringField(exitPolicy, "status") == "blocked" {
			items = append(items, map[string]any{
				"type":         "follow_up_blocker",
				"priority":     2,
				"target_id":    milestoneID,
				"title":        firstNonEmpty(stringField(currentMilestone, "title"), "Follow up blocker"),
				"reason":       "Current milestone is blocked and needs explicit coordinator follow-up.",
				"command_hint": fmt.Sprintf(`agentctl room milestone show %s %s`, roomID, milestoneID),
			})
		}

		if stringField(exitPolicy, "status") == "ready_for_review" {
			items = append(items, map[string]any{
				"type":         "review_milestone",
				"priority":     4,
				"target_id":    milestoneID,
				"title":        "Review milestone",
				"reason":       "Accepted stories are covered and the milestone is ready for review.",
				"command_hint": fmt.Sprintf(`agentctl room milestone review %s %s pass "<notes>"`, roomID, milestoneID),
			})
		}
		if stringField(exitPolicy, "status") == "ready_for_summary" {
			items = append(items, map[string]any{
				"type":         "summarize_milestone",
				"priority":     5,
				"target_id":    milestoneID,
				"title":        "Summarize milestone",
				"reason":       "Current milestone has a review verdict but no summary yet.",
				"command_hint": fmt.Sprintf(`agentctl room milestone summary %s %s "<notes>"`, roomID, milestoneID),
			})
		}
	}

	if next, ok := findNextRoomInterviewItem(actorID, room, messages); ok {
		targetID := ""
		if msg := mapField(next, "message"); msg != nil {
			targetID = stringField(msg, "id")
		}
		items = append(items, map[string]any{
			"type":         "resolve_interview",
			"priority":     2,
			"target_id":    targetID,
			"title":        firstNonEmpty(stringField(next, "topic"), "Resolve interview item"),
			"reason":       "There is an unresolved interview item for this actor.",
			"command_hint": fmt.Sprintf(`agentctl room interview next %s --actor %s`, roomID, actorID),
		})
	}

	if roomEpicLatestLog(epic) == nil && intField(epic, "story_count") > 0 {
		items = append(items, map[string]any{
			"type":         "append_delivery_log",
			"priority":     6,
			"target_id":    epicID,
			"title":        "Append delivery log",
			"reason":       "Epic has active scope but no delivery log entry yet.",
			"command_hint": fmt.Sprintf(`agentctl room log append %s %s "<label>"`, roomID, epicID),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		leftPriority := intField(items[i], "priority")
		rightPriority := intField(items[j], "priority")
		if leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		leftType := stringField(items[i], "type")
		rightType := stringField(items[j], "type")
		if leftType != rightType {
			return leftType < rightType
		}
		return stringField(items[i], "target_id") < stringField(items[j], "target_id")
	})
	if len(items) == 0 {
		return items, "no open work"
	}
	return items, "open epic work remains"
}

func buildRoomMilestoneViews(messages []agent.BoardMessage) []map[string]any {
	storiesByMilestone := make(map[string][]map[string]any)
	for _, story := range buildRoomStoryViews(messages) {
		milestoneID, _ := story["milestone_id"].(string)
		storiesByMilestone[milestoneID] = append(storiesByMilestone[milestoneID], story)
	}
	criteriaByMilestone := make(map[string][]agent.BoardMessage)
	contractsByMilestone := make(map[string][]agent.BoardMessage)
	reviewsByMilestone := make(map[string][]agent.BoardMessage)
	summariesByMilestone := make(map[string][]agent.BoardMessage)
	for _, msg := range messages {
		related := strings.TrimSpace(msg.RelatedMessageID)
		if related == "" {
			continue
		}
		switch msg.Kind {
		case agent.BoardMessageKindAcceptanceCriteria:
			criteriaByMilestone[related] = append(criteriaByMilestone[related], msg)
		case agent.BoardMessageKindMilestoneContract:
			contractsByMilestone[related] = append(contractsByMilestone[related], msg)
		case agent.BoardMessageKindMilestoneReview:
			reviewsByMilestone[related] = append(reviewsByMilestone[related], msg)
		case agent.BoardMessageKindMilestoneSummary:
			summariesByMilestone[related] = append(summariesByMilestone[related], msg)
		}
	}
	out := make([]map[string]any, 0)
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindMilestone {
			continue
		}
		meta := parseRoomMilestoneBody(msg.Body)
		for _, update := range contractsByMilestone[msg.ID] {
			meta = mergeRoomMilestoneMeta(meta, parseRoomMilestoneBody(update.Body))
		}
		reviews := reviewsByMilestone[msg.ID]
		summaryMessages := summariesByMilestone[msg.ID]
		summaryViews := make([]map[string]any, 0, len(summaryMessages))
		var latestSummaryMeta roomMilestoneSummaryMeta
		for _, summaryMsg := range summaryMessages {
			summaryMeta := normalizeRoomMilestoneSummaryMeta(parseRoomMilestoneSummaryBody(summaryMsg.Body))
			summaryViews = append(summaryViews, map[string]any{
				"id":         summaryMsg.ID,
				"root":       summaryMsg,
				"meta":       summaryMeta,
				"summary":    summaryMeta.Summary,
				"created_at": summaryMsg.CreatedAt.Format(time.RFC3339),
				"created_by": strings.TrimSpace(summaryMsg.Sender),
			})
			latestSummaryMeta = summaryMeta
		}
		sort.Slice(summaryViews, func(i, j int) bool {
			leftMsg, ok1 := summaryViews[i]["root"].(agent.BoardMessage)
			rightMsg, ok2 := summaryViews[j]["root"].(agent.BoardMessage)
			if !ok1 || !ok2 {
				return stringField(summaryViews[i], "id") < stringField(summaryViews[j], "id")
			}
			if leftMsg.CreatedAt.Equal(rightMsg.CreatedAt) {
				return leftMsg.ID < rightMsg.ID
			}
			return leftMsg.CreatedAt.Before(rightMsg.CreatedAt)
		})
		var latestSummary map[string]any
		if len(summaryViews) > 0 {
			latestSummary = summaryViews[len(summaryViews)-1]
			if root := mapField(latestSummary, "root"); root != nil {
				latestSummaryMeta = normalizeRoomMilestoneSummaryMeta(parseRoomMilestoneSummaryBody(stringField(root, "body")))
			}
		}
		status := "active"
		if len(reviews) > 0 {
			latest := reviews[len(reviews)-1]
			switch {
			case strings.Contains(strings.ToLower(strings.TrimSpace(latest.Subject)), "block"):
				status = "blocked"
			case strings.Contains(strings.ToLower(strings.TrimSpace(latest.Subject)), "pass"):
				status = "passed"
			}
		}
		stories := storiesByMilestone[msg.ID]
		acceptedStories := 0
		validatedStories := 0
		inProgressStories := 0
		inReviewStories := 0
		doneStories := 0
		deferredStories := 0
		passedStories := 0
		failedStories := 0
		blockedStories := 0
		waivedStories := 0
		blockingValidationIDs := make([]string, 0)
		laneCounts := make(map[string]int)
		laneCoverage := make(map[string]int)
		laneWaivers := make(map[string]int)
		laneBlockers := make(map[string][]string)
		for _, story := range stories {
			if storyStatus, _ := story["status"].(string); storyStatus != "accepted" {
				continue
			}
			acceptedStories++
			if covered, _ := story["covered"].(bool); covered {
				validatedStories++
			}
			switch stringField(story, "state") {
			case "in_progress":
				inProgressStories++
			case "in_review":
				inReviewStories++
			case "blocked":
				blockedStories++
			case "done":
				doneStories++
			case "deferred":
				deferredStories++
			}
			latestStatus, _ := story["latest_validation_status"].(string)
			switch latestStatus {
			case "pass":
				passedStories++
			case "fail":
				failedStories++
			case "blocked":
				if stringField(story, "state") != "blocked" {
					blockedStories++
				}
			case "waived":
				waivedStories++
			}
			if latestStatus == "fail" || latestStatus == "blocked" {
				if id, ok := story["latest_validation_id"].(string); ok && id != "" {
					blockingValidationIDs = append(blockingValidationIDs, id)
				}
			}
			for laneName, laneRaw := range mapField(story, "evidence_lanes") {
				lane := anyMap(laneRaw)
				laneCounts[laneName]++
				if _, ok := laneCoverage[laneName]; !ok {
					laneCoverage[laneName] = 0
				}
				if _, ok := laneWaivers[laneName]; !ok {
					laneWaivers[laneName] = 0
				}
				if _, ok := laneBlockers[laneName]; !ok {
					laneBlockers[laneName] = []string{}
				}
				if boolField(lane, "covered") {
					laneCoverage[laneName]++
				}
				if boolField(lane, "waived") {
					laneWaivers[laneName]++
				}
				if boolField(lane, "blocking") {
					laneBlockers[laneName] = append(laneBlockers[laneName], stringField(lane, "latest_validation_id"))
				}
			}
		}
		sort.Strings(blockingValidationIDs)
		for key, ids := range laneBlockers {
			sort.Strings(ids)
			laneBlockers[key] = ids
		}
		requiredLaneStatus, requiredLaneCovered, requiredLaneMissing := roomMilestoneRequiredLaneStatus(map[string]any{
			"meta":          meta,
			"lane_coverage": laneCoverage,
		})
		milestoneMessageIDs := uniqueStrings(append(
			append(
				append([]string{msg.ID}, collectRoomBoardIDs(criteriaByMilestone[msg.ID])...),
				collectRoomBoardIDs(reviews)...,
			),
			append(collectRoomMapIDs(summaryViews), collectRoomMapIDs(stories)...)...,
		))
		milestoneView := map[string]any{
			"id":                           msg.ID,
			"epic_id":                      meta.EpicID,
			"title":                        strings.TrimPrefix(strings.TrimSpace(msg.Subject), "Milestone: "),
			"status":                       status,
			"source_kind":                  "milestone",
			"source_id":                    msg.ID,
			"workpack_dir":                 roomAgileMilestoneWorkpackDir(meta.EpicID, msg.ID),
			"milestone_markdown":           roomAgileMilestoneMarkdownPath(meta.EpicID, msg.ID),
			"meta_json_path":               roomAgileMilestoneMetaJSONPath(meta.EpicID, msg.ID),
			"criteria_markdown":            roomAgileMilestoneCriteriaMarkdownPath(meta.EpicID, msg.ID),
			"summary_markdown":             roomAgileMilestoneSummaryMarkdownPath(meta.EpicID, msg.ID),
			"root":                         msg,
			"meta":                         meta,
			"contract":                     meta,
			"contract_update_count":        len(contractsByMilestone[msg.ID]),
			"criteria":                     criteriaByMilestone[msg.ID],
			"criteria_count":               len(criteriaByMilestone[msg.ID]),
			"reviews":                      reviews,
			"review_count":                 len(reviews),
			"summaries":                    summaryViews,
			"summary_count":                len(summaryViews),
			"latest_summary":               latestSummary,
			"summary_meta":                 latestSummaryMeta,
			"stories":                      stories,
			"story_count":                  len(stories),
			"accepted_story_count":         acceptedStories,
			"validated_story_count":        validatedStories,
			"in_progress_story_count":      inProgressStories,
			"in_review_story_count":        inReviewStories,
			"done_story_count":             doneStories,
			"deferred_story_count":         deferredStories,
			"passed_story_count":           passedStories,
			"failed_story_count":           failedStories,
			"blocked_story_count":          blockedStories,
			"waived_story_count":           waivedStories,
			"risk_count":                   len(meta.Risks),
			"dependency_count":             len(meta.Dependencies),
			"validator_count":              len(meta.ValidatorsExpected),
			"required_evidence_lane_count": len(meta.RequiredEvidenceLanes),
			"optional_evidence_lane_count": len(meta.OptionalEvidenceLanes),
			"exit_criteria_count":          len(meta.ExitCriteria),
			"passed_criteria_count":        len(latestSummaryMeta.PassedCriteria),
			"failed_criteria_count":        len(latestSummaryMeta.FailedCriteria),
			"waived_validation_count":      len(latestSummaryMeta.WaivedValidationIDs),
			"blocking_validation_count":    len(latestSummaryMeta.BlockingValidationIDs),
			"decision_count":               len(latestSummaryMeta.NotableDecisions),
			"finding_count":                len(latestSummaryMeta.SystemicFindings),
			"recommended_next_count":       len(latestSummaryMeta.RecommendedNext),
			"guidance_update_count":        len(latestSummaryMeta.GuidanceUpdates),
			"blocking_validation_ids":      blockingValidationIDs,
			"lane_counts":                  laneCounts,
			"lane_coverage":                laneCoverage,
			"lane_blockers":                laneBlockers,
			"lane_waivers":                 laneWaivers,
			"required_evidence_lanes":      meta.RequiredEvidenceLanes,
			"optional_evidence_lanes":      meta.OptionalEvidenceLanes,
			"required_lane_status":         requiredLaneStatus,
			"required_lane_covered":        requiredLaneCovered,
			"required_lane_missing":        requiredLaneMissing,
			"coverage": map[string]any{
				"validated": validatedStories,
				"accepted":  acceptedStories,
			},
			"room_message_ids": milestoneMessageIDs,
		}
		milestoneView["exit_policy"] = roomMilestoneExitPolicy(milestoneView)
		out = append(out, milestoneView)
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]["root"].(agent.BoardMessage)
		right := out[j]["root"].(agent.BoardMessage)
		return left.CreatedAt.After(right.CreatedAt)
	})
	return out
}

func buildRoomStoryViews(messages []agent.BoardMessage) []map[string]any {
	milestoneEpicByID := make(map[string]string)
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindMilestone {
			continue
		}
		meta := parseRoomMilestoneBody(msg.Body)
		milestoneEpicByID[msg.ID] = strings.TrimSpace(meta.EpicID)
	}
	validationsByStory := make(map[string][]map[string]any)
	statesByStory := make(map[string][]map[string]any)
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindStoryValidation {
			if msg.Kind == agent.BoardMessageKindStoryState {
				meta := parseRoomStoryStateBody(msg.Body)
				statesByStory[strings.TrimSpace(msg.RelatedMessageID)] = append(statesByStory[strings.TrimSpace(msg.RelatedMessageID)], map[string]any{
					"id":         msg.ID,
					"root":       msg,
					"meta":       meta,
					"state":      meta.State,
					"created_at": msg.CreatedAt.Format(time.RFC3339),
					"created_by": strings.TrimSpace(msg.Sender),
				})
			}
			continue
		}
		meta := parseRoomStoryValidationBody(msg.Body)
		validationsByStory[strings.TrimSpace(msg.RelatedMessageID)] = append(validationsByStory[strings.TrimSpace(msg.RelatedMessageID)], map[string]any{
			"id":               msg.ID,
			"validation_id":    msg.ID,
			"source_kind":      "story_validation",
			"source_id":        msg.ID,
			"root":             msg,
			"meta":             meta,
			"status":           meta.Status,
			"created_at":       msg.CreatedAt.Format(time.RFC3339),
			"created_by":       strings.TrimSpace(msg.Sender),
			"room_message_ids": []string{msg.ID},
		})
	}
	out := make([]map[string]any, 0)
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindStory && msg.Kind != agent.BoardMessageKindStoryProposal {
			continue
		}
		if msg.Kind == agent.BoardMessageKindStoryProposal {
			meta := parseRoomStoryProposalBody(msg.Body)
			out = append(out, map[string]any{
				"id":           msg.ID,
				"milestone_id": strings.TrimSpace(msg.RelatedMessageID),
				"title":        strings.TrimPrefix(strings.TrimSpace(msg.Subject), "Story Proposal: "),
				"status":       "proposed",
				"state":        "proposed",
				"root":         msg,
				"meta":         meta,
			})
			continue
		}
		meta := parseRoomStoryBody(msg.Body)
		milestoneID := strings.TrimSpace(msg.RelatedMessageID)
		epicID := milestoneEpicByID[milestoneID]
		validations := validationsByStory[msg.ID]
		for i := range validations {
			validationID := stringField(validations[i], "id")
			validations[i]["validation_markdown"] = roomAgileValidationMarkdownPath(epicID, milestoneID, msg.ID, validationID)
			validations[i]["validation_json"] = roomAgileValidationJSONPath(epicID, milestoneID, msg.ID, validationID)
		}
		validationSummary := summarizeRoomStoryValidations(validations)
		stateSummary := summarizeRoomStoryStates(statesByStory[msg.ID])
		state := "accepted"
		if stateSummary.State != "" {
			state = stateSummary.State
		} else {
			switch validationSummary.LatestStatus {
			case "pass":
				state = "validated"
			case "waived":
				state = "waived"
			}
		}
		out = append(out, map[string]any{
			"id":                         msg.ID,
			"milestone_id":               milestoneID,
			"title":                      strings.TrimPrefix(strings.TrimSpace(msg.Subject), "Story: "),
			"status":                     "accepted",
			"source_kind":                "story",
			"source_id":                  msg.ID,
			"state":                      state,
			"state_reason":               stateSummary.Reason,
			"blocked_by":                 stateSummary.BlockedBy,
			"reviewer":                   stateSummary.Reviewer,
			"state_update_count":         len(stateSummary.StateHistory),
			"state_history":              stateSummary.StateHistory,
			"latest_state_id":            stateSummary.LatestID,
			"epic_id":                    epicID,
			"workpack_dir":               roomAgileStoryWorkpackDir(epicID, milestoneID, msg.ID),
			"story_markdown":             roomAgileStoryMarkdownPath(epicID, milestoneID, msg.ID),
			"meta_json_path":             roomAgileStoryMetaJSONPath(epicID, milestoneID, msg.ID),
			"validation_dir":             roomAgileStoryValidationDir(epicID, milestoneID, msg.ID),
			"artifacts_dir":              roomAgileStoryArtifactsDir(epicID, milestoneID, msg.ID),
			"root":                       msg,
			"meta":                       meta,
			"validations":                validations,
			"validation_count":           len(validations),
			"latest_validation_status":   validationSummary.LatestStatus,
			"latest_validation_id":       validationSummary.LatestID,
			"latest_validation_markdown": roomAgileValidationMarkdownPath(epicID, milestoneID, msg.ID, validationSummary.LatestID),
			"latest_validation_json":     roomAgileValidationJSONPath(epicID, milestoneID, msg.ID, validationSummary.LatestID),
			"effective_validations":      validationSummary.EffectiveValidations,
			"evidence_lanes":             validationSummary.EvidenceLanes,
			"covered_lanes":              validationSummary.CoveredLanes,
			"blocking_lanes":             validationSummary.BlockingLanes,
			"waived_lanes":               validationSummary.WaivedLanes,
			"covered":                    validationSummary.Covered,
			"has_failures":               validationSummary.HasFailures,
			"has_blockers":               validationSummary.HasBlockers,
			"waived":                     validationSummary.WaivedOnly,
			"room_message_ids":           roomStorySyncMessageIDs(map[string]any{"id": msg.ID, "state_history": stateSummary.StateHistory, "validations": validations}),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]["root"].(agent.BoardMessage)
		right := out[j]["root"].(agent.BoardMessage)
		return left.CreatedAt.After(right.CreatedAt)
	})
	return out
}

type roomStoryValidationSummary struct {
	LatestStatus         string
	LatestID             string
	EffectiveValidations []map[string]any
	Covered              bool
	HasFailures          bool
	HasBlockers          bool
	WaivedOnly           bool
	EvidenceLanes        map[string]map[string]any
	CoveredLanes         []string
	BlockingLanes        []string
	WaivedLanes          []string
}

type roomStoryStateSummary struct {
	State        string
	Reason       string
	BlockedBy    string
	Reviewer     string
	LatestID     string
	StateHistory []map[string]any
}

func summarizeRoomStoryValidations(validations []map[string]any) roomStoryValidationSummary {
	summary := roomStoryValidationSummary{
		EffectiveValidations: make([]map[string]any, 0),
		EvidenceLanes:        make(map[string]map[string]any),
	}
	if len(validations) == 0 {
		return summary
	}
	sort.Slice(validations, func(i, j int) bool {
		left := validations[i]["root"].(agent.BoardMessage)
		right := validations[j]["root"].(agent.BoardMessage)
		if left.CreatedAt.Equal(right.CreatedAt) {
			return stringField(validations[i], "id") < stringField(validations[j], "id")
		}
		return left.CreatedAt.Before(right.CreatedAt)
	})
	lastIndexByType := make(map[string]int)
	for idx, validation := range validations {
		meta := anyMap(validation["meta"])
		validatorType := stringField(meta, "validator_type")
		if validatorType == "" {
			validatorType = "unknown"
		}
		lastIndexByType[validatorType] = idx
	}
	latestByType := make(map[string]map[string]any)
	countByType := make(map[string]int)
	for idx, validation := range validations {
		meta := anyMap(validation["meta"])
		validatorType := stringField(meta, "validator_type")
		if validatorType == "" {
			validatorType = "unknown"
		}
		validation["superseded"] = lastIndexByType[validatorType] != idx
		latestByType[validatorType] = validation
		countByType[validatorType]++
		last := validation
		summary.LatestStatus, _ = last["status"].(string)
		summary.LatestID, _ = last["id"].(string)
	}
	keys := make([]string, 0, len(latestByType))
	for key := range latestByType {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hasPass := false
	hasWaived := false
	for _, key := range keys {
		validation := latestByType[key]
		summary.EffectiveValidations = append(summary.EffectiveValidations, validation)
		status, _ := validation["status"].(string)
		lane := map[string]any{
			"lane":                 key,
			"count":                countByType[key],
			"latest_status":        status,
			"latest_validation_id": stringField(validation, "id"),
			"covered":              status == "pass" || status == "waived",
			"waived":               status == "waived",
			"blocking":             status == "fail" || status == "blocked",
		}
		summary.EvidenceLanes[key] = lane
		switch status {
		case "blocked":
			summary.HasBlockers = true
			summary.BlockingLanes = append(summary.BlockingLanes, key)
		case "fail":
			summary.HasFailures = true
			summary.BlockingLanes = append(summary.BlockingLanes, key)
		case "pass":
			hasPass = true
			summary.CoveredLanes = append(summary.CoveredLanes, key)
		case "waived":
			hasWaived = true
			summary.CoveredLanes = append(summary.CoveredLanes, key)
			summary.WaivedLanes = append(summary.WaivedLanes, key)
		}
	}
	sort.Strings(summary.CoveredLanes)
	sort.Strings(summary.BlockingLanes)
	sort.Strings(summary.WaivedLanes)
	summary.Covered = hasPass || hasWaived
	summary.WaivedOnly = !hasPass && !summary.HasFailures && !summary.HasBlockers && hasWaived
	if summary.HasBlockers {
		summary.LatestStatus = "blocked"
	} else if summary.HasFailures {
		summary.LatestStatus = "fail"
	} else if hasPass {
		summary.LatestStatus = "pass"
	} else if hasWaived {
		summary.LatestStatus = "waived"
	}
	return summary
}

func summarizeRoomStoryStates(states []map[string]any) roomStoryStateSummary {
	summary := roomStoryStateSummary{StateHistory: make([]map[string]any, 0, len(states))}
	if len(states) == 0 {
		return summary
	}
	sort.Slice(states, func(i, j int) bool {
		left := states[i]["root"].(agent.BoardMessage)
		right := states[j]["root"].(agent.BoardMessage)
		if left.CreatedAt.Equal(right.CreatedAt) {
			return stringField(states[i], "id") < stringField(states[j], "id")
		}
		return left.CreatedAt.Before(right.CreatedAt)
	})
	for _, state := range states {
		summary.StateHistory = append(summary.StateHistory, state)
		last := anyMap(state["meta"])
		summary.State = stringField(last, "state")
		summary.Reason = stringField(last, "reason")
		summary.BlockedBy = stringField(last, "blocked_by")
		summary.Reviewer = stringField(last, "reviewer")
		summary.LatestID = stringField(state, "id")
	}
	return summary
}

func buildRoomGuidanceUpdateViews(messages []agent.BoardMessage) []map[string]any {
	out := make([]map[string]any, 0)
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindGuidanceUpdate {
			continue
		}
		meta := parseRoomGuidanceUpdateBody(msg.Body)
		out = append(out, map[string]any{
			"id":               msg.ID,
			"epic_id":          meta.EpicID,
			"milestone_id":     meta.MilestoneID,
			"source_kind":      "guidance_update",
			"source_id":        msg.ID,
			"kind":             meta.Kind,
			"summary":          meta.Summary,
			"workpack_root":    roomAgileWorkpackRootPath(meta.EpicID),
			"workpack_path":    roomAgileRetroMarkdownPath(meta.EpicID),
			"meta_json_path":   roomAgileEpicMetaJSONPath(meta.EpicID),
			"root":             msg,
			"meta":             meta,
			"created_at":       msg.CreatedAt.Format(time.RFC3339),
			"created_by":       strings.TrimSpace(msg.Sender),
			"room_message_ids": []string{msg.ID},
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]["root"].(agent.BoardMessage)
		right := out[j]["root"].(agent.BoardMessage)
		if left.CreatedAt.Equal(right.CreatedAt) {
			return left.ID > right.ID
		}
		return left.CreatedAt.After(right.CreatedAt)
	})
	return out
}

func buildRoomDeliveryLogViews(messages []agent.BoardMessage) []map[string]any {
	out := make([]map[string]any, 0)
	for _, msg := range messages {
		if msg.Kind != agent.BoardMessageKindDeliveryLog {
			continue
		}
		meta := parseRoomDeliveryLogBody(msg.Body)
		out = append(out, map[string]any{
			"id":      msg.ID,
			"epic_id": strings.TrimSpace(msg.RelatedMessageID),
			"label":   firstNonEmpty(meta.Label, strings.TrimPrefix(strings.TrimSpace(msg.Subject), "Delivery Log: ")),
			"root":    msg,
			"meta":    meta,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		left := out[i]["root"].(agent.BoardMessage)
		right := out[j]["root"].(agent.BoardMessage)
		return left.CreatedAt.After(right.CreatedAt)
	})
	return out
}

const roomAgileWorkpackRootDir = ".agentctl/epics"

func roomAgileWorkpackRootPath(epicID string) string {
	epicID = strings.TrimSpace(epicID)
	if epicID == "" {
		return ""
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(homeDir, roomAgileWorkpackRootDir, epicID)
}

func roomAgileMilestoneWorkpackDir(epicID, milestoneID string) string {
	root := roomAgileWorkpackRootPath(epicID)
	milestoneID = strings.TrimSpace(milestoneID)
	if root == "" || milestoneID == "" {
		return ""
	}
	return filepath.Join(root, "milestones", milestoneID)
}

func roomAgileStoryWorkpackDir(epicID, milestoneID, storyID string) string {
	milestoneDir := roomAgileMilestoneWorkpackDir(epicID, milestoneID)
	storyID = strings.TrimSpace(storyID)
	if milestoneDir == "" || storyID == "" {
		return ""
	}
	return filepath.Join(milestoneDir, "stories", storyID)
}

func roomAgileStoryValidationDir(epicID, milestoneID, storyID string) string {
	storyDir := roomAgileStoryWorkpackDir(epicID, milestoneID, storyID)
	if storyDir == "" {
		return ""
	}
	return filepath.Join(storyDir, "validation")
}

func roomAgileStoryArtifactsDir(epicID, milestoneID, storyID string) string {
	storyDir := roomAgileStoryWorkpackDir(epicID, milestoneID, storyID)
	if storyDir == "" {
		return ""
	}
	return filepath.Join(storyDir, "artifacts")
}

func roomAgileEpicMarkdownPath(epicID string) string {
	root := roomAgileWorkpackRootPath(epicID)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "epic.md")
}

func roomAgileEpicMetaJSONPath(epicID string) string {
	root := roomAgileWorkpackRootPath(epicID)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "meta.json")
}

func roomAgileDeliveryLogMarkdownPath(epicID string) string {
	root := roomAgileWorkpackRootPath(epicID)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "delivery-log.md")
}

func roomAgileRetroMarkdownPath(epicID string) string {
	root := roomAgileWorkpackRootPath(epicID)
	if root == "" {
		return ""
	}
	return filepath.Join(root, "retro.md")
}

func roomAgileMilestoneMarkdownPath(epicID, milestoneID string) string {
	dir := roomAgileMilestoneWorkpackDir(epicID, milestoneID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "milestone.md")
}

func roomAgileMilestoneMetaJSONPath(epicID, milestoneID string) string {
	dir := roomAgileMilestoneWorkpackDir(epicID, milestoneID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "meta.json")
}

func roomAgileMilestoneCriteriaMarkdownPath(epicID, milestoneID string) string {
	dir := roomAgileMilestoneWorkpackDir(epicID, milestoneID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "criteria.md")
}

func roomAgileMilestoneSummaryMarkdownPath(epicID, milestoneID string) string {
	dir := roomAgileMilestoneWorkpackDir(epicID, milestoneID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "summary.md")
}

func roomAgileStoryMarkdownPath(epicID, milestoneID, storyID string) string {
	dir := roomAgileStoryWorkpackDir(epicID, milestoneID, storyID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "story.md")
}

func roomAgileStoryMetaJSONPath(epicID, milestoneID, storyID string) string {
	dir := roomAgileStoryWorkpackDir(epicID, milestoneID, storyID)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "meta.json")
}

func roomAgileValidationMarkdownPath(epicID, milestoneID, storyID, validationID string) string {
	dir := roomAgileStoryValidationDir(epicID, milestoneID, storyID)
	if dir == "" || strings.TrimSpace(validationID) == "" {
		return ""
	}
	return filepath.Join(dir, strings.TrimSpace(validationID)+".md")
}

func roomAgileValidationJSONPath(epicID, milestoneID, storyID, validationID string) string {
	dir := roomAgileStoryValidationDir(epicID, milestoneID, storyID)
	if dir == "" || strings.TrimSpace(validationID) == "" {
		return ""
	}
	return filepath.Join(dir, strings.TrimSpace(validationID)+".json")
}

func buildRoomAgileWorkpackInfo(epic map[string]any) map[string]any {
	epicID := stringField(epic, "id")
	root := roomAgileWorkpackRootPath(epicID)
	info := map[string]any{
		"root":                  root,
		"epic_markdown":         roomAgileEpicMarkdownPath(epicID),
		"meta_json":             roomAgileEpicMetaJSONPath(epicID),
		"delivery_log_markdown": roomAgileDeliveryLogMarkdownPath(epicID),
		"retro_markdown":        roomAgileRetroMarkdownPath(epicID),
	}
	milestones := make([]map[string]any, 0)
	for _, milestone := range mapSlice(epic["milestones"]) {
		milestoneID := stringField(milestone, "id")
		milestoneDir := roomAgileMilestoneWorkpackDir(epicID, milestoneID)
		milestoneInfo := map[string]any{
			"id":                 milestoneID,
			"title":              stringField(milestone, "title"),
			"dir":                milestoneDir,
			"milestone_markdown": roomAgileMilestoneMarkdownPath(epicID, milestoneID),
			"meta_json":          roomAgileMilestoneMetaJSONPath(epicID, milestoneID),
			"criteria_markdown":  roomAgileMilestoneCriteriaMarkdownPath(epicID, milestoneID),
			"summary_markdown":   roomAgileMilestoneSummaryMarkdownPath(epicID, milestoneID),
		}
		stories := make([]map[string]any, 0)
		for _, story := range mapSlice(milestone["stories"]) {
			if status := stringField(story, "status"); status != "accepted" {
				continue
			}
			storyID := stringField(story, "id")
			storyDir := roomAgileStoryWorkpackDir(epicID, milestoneID, storyID)
			validations := make([]map[string]any, 0)
			for _, validation := range mapSlice(story["validations"]) {
				validationID := stringField(validation, "id")
				if validationID == "" {
					continue
				}
				validations = append(validations, map[string]any{
					"id":                  validationID,
					"validation_markdown": roomAgileValidationMarkdownPath(epicID, milestoneID, storyID, validationID),
					"validation_json":     roomAgileValidationJSONPath(epicID, milestoneID, storyID, validationID),
				})
			}
			stories = append(stories, map[string]any{
				"id":             storyID,
				"title":          stringField(story, "title"),
				"dir":            storyDir,
				"story_markdown": roomAgileStoryMarkdownPath(epicID, milestoneID, storyID),
				"meta_json":      roomAgileStoryMetaJSONPath(epicID, milestoneID, storyID),
				"validation_dir": roomAgileStoryValidationDir(epicID, milestoneID, storyID),
				"artifacts_dir":  roomAgileStoryArtifactsDir(epicID, milestoneID, storyID),
				"validations":    validations,
			})
		}
		milestoneInfo["stories"] = stories
		milestones = append(milestones, milestoneInfo)
	}
	info["milestones"] = milestones
	return info
}

func buildRoomWorkpackProvenance(workspaceID, roomID, sourceKind, sourceID, epicID, milestoneID, storyID, validationID, guidanceUpdateID, workpackRoot, workpackPath, metaJSONPath string, roomMessageIDs []string) map[string]any {
	return map[string]any{
		"workspace":          strings.TrimSpace(workspaceID),
		"workspace_id":       strings.TrimSpace(workspaceID),
		"room_id":            strings.TrimSpace(roomID),
		"source_kind":        strings.TrimSpace(sourceKind),
		"source_id":          strings.TrimSpace(sourceID),
		"epic_id":            strings.TrimSpace(epicID),
		"milestone_id":       strings.TrimSpace(milestoneID),
		"story_id":           strings.TrimSpace(storyID),
		"validation_id":      strings.TrimSpace(validationID),
		"guidance_update_id": strings.TrimSpace(guidanceUpdateID),
		"room_message_ids":   uniqueStrings(roomMessageIDs),
		"workpack_root":      strings.TrimSpace(workpackRoot),
		"workpack_path":      strings.TrimSpace(workpackPath),
		"meta_json_path":     strings.TrimSpace(metaJSONPath),
	}
}

func prependRoomWorkpackProvenance(content string, provenance map[string]any) string {
	var b strings.Builder
	content = strings.TrimSpace(content)
	if content != "" {
		b.WriteString(content)
		b.WriteString("\n")
	}
	lines := make([]string, 0, 10)
	for _, item := range []struct {
		label string
		value string
	}{
		{"Workspace", stringField(provenance, "workspace")},
		{"Room ID", stringField(provenance, "room_id")},
		{"Source kind", stringField(provenance, "source_kind")},
		{"Source ID", stringField(provenance, "source_id")},
		{"Epic ID", stringField(provenance, "epic_id")},
		{"Milestone ID", stringField(provenance, "milestone_id")},
		{"Story ID", stringField(provenance, "story_id")},
		{"Validation ID", stringField(provenance, "validation_id")},
		{"Guidance update ID", stringField(provenance, "guidance_update_id")},
		{"Meta JSON", stringField(provenance, "meta_json_path")},
	} {
		if strings.TrimSpace(item.value) == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s: `%s`", item.label, item.value))
	}
	if ids := stringSliceField(provenance, "room_message_ids"); len(ids) > 0 {
		lines = append(lines, fmt.Sprintf("- Room message IDs: `%s`", strings.Join(ids, "`, `")))
	}
	if len(lines) > 0 {
		b.WriteString("\n## Provenance\n")
		for _, line := range lines {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func roomEpicSyncMessageIDs(epic map[string]any) []string {
	ids := []string{stringField(epic, "id")}
	switch finalBrief := epic["final_brief"].(type) {
	case map[string]any:
		ids = append(ids, stringField(finalBrief, "id"))
	case agent.BoardMessage:
		if strings.TrimSpace(finalBrief.ID) != "" {
			ids = append(ids, finalBrief.ID)
		}
	}
	for _, milestone := range mapSlice(epic["milestones"]) {
		ids = append(ids, stringField(milestone, "id"))
	}
	for _, log := range mapSlice(epic["logs"]) {
		ids = append(ids, stringField(log, "id"))
	}
	for _, update := range mapSlice(epic["guidance_updates"]) {
		ids = append(ids, stringField(update, "id"))
	}
	return uniqueStrings(ids)
}

func roomMilestoneSyncMessageIDs(milestone map[string]any) []string {
	ids := []string{stringField(milestone, "id")}
	ids = append(ids, collectRoomBoardIDs(boardMessageSliceValue(milestone["criteria"]))...)
	for _, review := range boardMessageSliceValue(milestone["reviews"]) {
		ids = append(ids, review.ID)
	}
	for _, summary := range mapSlice(milestone["summaries"]) {
		ids = append(ids, stringField(summary, "id"))
	}
	for _, story := range mapSlice(milestone["stories"]) {
		ids = append(ids, stringField(story, "id"))
	}
	return uniqueStrings(ids)
}

func roomStorySyncMessageIDs(story map[string]any) []string {
	ids := []string{stringField(story, "id")}
	for _, state := range mapSlice(story["state_history"]) {
		ids = append(ids, stringField(state, "id"))
	}
	for _, validation := range mapSlice(story["validations"]) {
		ids = append(ids, stringField(validation, "id"))
	}
	return uniqueStrings(ids)
}

func collectRoomMapIDs(items []map[string]any) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if id := stringField(item, "id"); id != "" {
			out = append(out, id)
			continue
		}
		if id := stringField(mapField(item, "root"), "id"); id != "" {
			out = append(out, id)
		}
	}
	return uniqueStrings(out)
}

func collectRoomBoardIDs(items []agent.BoardMessage) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.ID) != "" {
			out = append(out, item.ID)
		}
	}
	return uniqueStrings(out)
}

func syncRoomAgileWorkpack(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, epicID string) error {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return err
	}
	var epic map[string]any
	for _, candidate := range buildRoomEpicViews(messages) {
		if id, _ := candidate["id"].(string); id == strings.TrimSpace(epicID) {
			epic = candidate
			break
		}
	}
	if epic == nil {
		return fmt.Errorf("epic %q not found for work-pack sync", epicID)
	}
	epicDir := roomAgileWorkpackRootPath(epicID)
	if epicDir == "" {
		return fmt.Errorf("resolve user home for work-pack sync")
	}
	epicMetaJSON := roomAgileEpicMetaJSONPath(epicID)
	epicProvenance := buildRoomWorkpackProvenance(workspaceID, roomID, "epic", epicID, epicID, "", "", "", "", epicDir, roomAgileEpicMarkdownPath(epicID), epicMetaJSON, roomEpicSyncMessageIDs(epic))
	if err := os.MkdirAll(filepath.Join(epicDir, "milestones"), 0o755); err != nil {
		return fmt.Errorf("create epic work-pack directory: %w", err)
	}
	if err := writeRoomAgileFile(roomAgileEpicMarkdownPath(epicID), prependRoomWorkpackProvenance(renderRoomEpicMarkdown(epic), epicProvenance)); err != nil {
		return err
	}
	if err := writeRoomAgileJSON(epicMetaJSON, map[string]any{
		"schema_version": 1,
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"provenance":     epicProvenance,
		"epic":           epic,
	}); err != nil {
		return err
	}
	deliveryLogProvenance := buildRoomWorkpackProvenance(workspaceID, roomID, "delivery_log", epicID, epicID, "", "", "", "", epicDir, roomAgileDeliveryLogMarkdownPath(epicID), epicMetaJSON, uniqueStrings(append([]string{epicID}, collectRoomMapIDs(mapSlice(epic["logs"]))...)))
	if err := writeRoomAgileFile(roomAgileDeliveryLogMarkdownPath(epicID), prependRoomWorkpackProvenance(renderRoomDeliveryLogMarkdown(epic), deliveryLogProvenance)); err != nil {
		return err
	}
	retroProvenance := buildRoomWorkpackProvenance(workspaceID, roomID, "epic", epicID, epicID, "", "", "", "", epicDir, roomAgileRetroMarkdownPath(epicID), epicMetaJSON, uniqueStrings(append([]string{epicID}, collectRoomMapIDs(mapSlice(epic["guidance_updates"]))...)))
	if err := writeRoomAgileFile(roomAgileRetroMarkdownPath(epicID), prependRoomWorkpackProvenance(renderRoomRetroMarkdown(epic), retroProvenance)); err != nil {
		return err
	}
	for _, milestone := range mapSlice(epic["milestones"]) {
		if err := syncRoomAgileMilestoneWorkpack(workspaceID, roomID, epicID, milestone); err != nil {
			return err
		}
	}
	return nil
}

func syncRoomAgileMilestoneWorkpack(workspaceID, roomID, epicID string, milestone map[string]any) error {
	milestoneID := stringField(milestone, "id")
	if milestoneID == "" {
		return nil
	}
	milestoneDir := roomAgileMilestoneWorkpackDir(epicID, milestoneID)
	milestoneMetaJSON := roomAgileMilestoneMetaJSONPath(epicID, milestoneID)
	milestoneProvenance := buildRoomWorkpackProvenance(workspaceID, roomID, "milestone", milestoneID, epicID, milestoneID, "", "", "", roomAgileWorkpackRootPath(epicID), roomAgileMilestoneMarkdownPath(epicID, milestoneID), milestoneMetaJSON, roomMilestoneSyncMessageIDs(milestone))
	if err := os.MkdirAll(filepath.Join(milestoneDir, "stories"), 0o755); err != nil {
		return fmt.Errorf("create milestone work-pack directory: %w", err)
	}
	if err := writeRoomAgileFile(roomAgileMilestoneMarkdownPath(epicID, milestoneID), prependRoomWorkpackProvenance(renderRoomMilestoneMarkdown(milestone), milestoneProvenance)); err != nil {
		return err
	}
	if err := writeRoomAgileJSON(milestoneMetaJSON, map[string]any{
		"schema_version": 1,
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"provenance":     milestoneProvenance,
		"milestone":      milestone,
	}); err != nil {
		return err
	}
	criteriaProvenance := buildRoomWorkpackProvenance(workspaceID, roomID, "milestone", milestoneID, epicID, milestoneID, "", "", "", roomAgileWorkpackRootPath(epicID), roomAgileMilestoneCriteriaMarkdownPath(epicID, milestoneID), milestoneMetaJSON, uniqueStrings(append([]string{milestoneID}, collectRoomBoardIDs(boardMessageSliceValue(milestone["criteria"]))...)))
	if err := writeRoomAgileFile(roomAgileMilestoneCriteriaMarkdownPath(epicID, milestoneID), prependRoomWorkpackProvenance(renderRoomMilestoneCriteriaMarkdown(milestone), criteriaProvenance)); err != nil {
		return err
	}
	summarySourceID := firstNonEmpty(stringField(mapField(milestone, "latest_summary"), "id"), milestoneID)
	summaryProvenance := buildRoomWorkpackProvenance(workspaceID, roomID, "milestone_summary", summarySourceID, epicID, milestoneID, "", "", "", roomAgileWorkpackRootPath(epicID), roomAgileMilestoneSummaryMarkdownPath(epicID, milestoneID), milestoneMetaJSON, uniqueStrings(append([]string{milestoneID}, collectRoomMapIDs(mapSlice(milestone["summaries"]))...)))
	if err := writeRoomAgileFile(roomAgileMilestoneSummaryMarkdownPath(epicID, milestoneID), prependRoomWorkpackProvenance(renderRoomMilestoneSummaryMarkdown(milestone), summaryProvenance)); err != nil {
		return err
	}
	for _, story := range mapSlice(milestone["stories"]) {
		if err := syncRoomAgileStoryWorkpack(workspaceID, roomID, epicID, milestoneID, story); err != nil {
			return err
		}
	}
	return nil
}

func syncRoomAgileStoryWorkpack(workspaceID, roomID, epicID, milestoneID string, story map[string]any) error {
	storyID := stringField(story, "id")
	if storyID == "" {
		return nil
	}
	validationDir := roomAgileStoryValidationDir(epicID, milestoneID, storyID)
	artifactsDir := roomAgileStoryArtifactsDir(epicID, milestoneID, storyID)
	storyMetaJSON := roomAgileStoryMetaJSONPath(epicID, milestoneID, storyID)
	storyProvenance := buildRoomWorkpackProvenance(workspaceID, roomID, "story", storyID, epicID, milestoneID, storyID, "", "", roomAgileWorkpackRootPath(epicID), roomAgileStoryMarkdownPath(epicID, milestoneID, storyID), storyMetaJSON, roomStorySyncMessageIDs(story))
	if err := os.MkdirAll(validationDir, 0o755); err != nil {
		return fmt.Errorf("create story validation directory: %w", err)
	}
	if err := os.MkdirAll(artifactsDir, 0o755); err != nil {
		return fmt.Errorf("create story artifacts directory: %w", err)
	}
	if err := writeRoomAgileFile(roomAgileStoryMarkdownPath(epicID, milestoneID, storyID), prependRoomWorkpackProvenance(renderRoomStoryMarkdown(story), storyProvenance)); err != nil {
		return err
	}
	if err := writeRoomAgileJSON(storyMetaJSON, map[string]any{
		"schema_version": 1,
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"provenance":     storyProvenance,
		"story":          story,
	}); err != nil {
		return err
	}
	for _, validation := range mapSlice(story["validations"]) {
		validationID := stringField(validation, "id")
		if validationID == "" {
			continue
		}
		validationMetaJSON := roomAgileValidationJSONPath(epicID, milestoneID, storyID, validationID)
		validationProvenance := buildRoomWorkpackProvenance(workspaceID, roomID, "story_validation", validationID, epicID, milestoneID, storyID, validationID, "", roomAgileWorkpackRootPath(epicID), roomAgileValidationMarkdownPath(epicID, milestoneID, storyID, validationID), validationMetaJSON, []string{validationID})
		if err := writeRoomAgileFile(roomAgileValidationMarkdownPath(epicID, milestoneID, storyID, validationID), prependRoomWorkpackProvenance(renderRoomStoryValidationMarkdown(validation), validationProvenance)); err != nil {
			return err
		}
		if err := writeRoomAgileJSON(validationMetaJSON, map[string]any{
			"schema_version": 1,
			"generated_at":   time.Now().UTC().Format(time.RFC3339),
			"provenance":     validationProvenance,
			"validation":     validation,
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeRoomAgileFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create work-pack parent directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o644); err != nil {
		return fmt.Errorf("write work-pack file %s: %w", path, err)
	}
	return nil
}

func writeRoomAgileJSON(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal work-pack json %s: %w", path, err)
	}
	return writeRoomAgileFile(path, string(data))
}

func renderRoomEpicMarkdown(epic map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Epic %s\n\n", stringField(epic, "title"))
	fmt.Fprintf(&b, "- ID: `%s`\n", stringField(epic, "id"))
	fmt.Fprintf(&b, "- Status: `%s`\n", stringField(epic, "status"))
	fmt.Fprintf(&b, "- Milestones: `%d`\n", intField(epic, "milestone_count"))
	fmt.Fprintf(&b, "- Stories: `%d`\n", intField(epic, "story_count"))
	fmt.Fprintf(&b, "- Guidance updates: `%d`\n", intField(epic, "guidance_update_count"))
	if meta := anyMap(epic["meta"]); meta != nil {
		if goal := stringField(meta, "goal"); goal != "" {
			fmt.Fprintf(&b, "- Goal: %s\n", goal)
		}
		if owner := stringField(meta, "owner"); owner != "" {
			fmt.Fprintf(&b, "- Owner: %s\n", owner)
		}
	}
	if finalBrief := anyMap(epic["final_brief"]); finalBrief != nil {
		if body := stringField(finalBrief, "body"); body != "" {
			fmt.Fprintf(&b, "\n## Final Brief\n\n%s\n", body)
		}
	} else {
		appendMarkdownEmptyState(&b, "Final Brief", "No finalized brief recorded yet.")
	}
	if milestones := mapSlice(epic["milestones"]); len(milestones) > 0 {
		fmt.Fprintf(&b, "\n## Milestones\n")
		for _, milestone := range milestones {
			fmt.Fprintf(&b, "- `%s` %s\n", stringField(milestone, "id"), stringField(milestone, "title"))
		}
	} else {
		appendMarkdownEmptyState(&b, "Milestones", "No milestones shaped yet.")
	}
	return b.String()
}

func renderRoomDeliveryLogMarkdown(epic map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Delivery Log\n\n")
	logs := mapSlice(epic["logs"])
	if len(logs) == 0 {
		fmt.Fprintf(&b, "Delivery log entries are listed newest first.\n")
		appendMarkdownEmptyState(&b, "Entries", "No delivery log entries recorded yet.")
		return b.String()
	}
	fmt.Fprintf(&b, "Delivery log entries are listed newest first.\n")
	for _, entry := range logs {
		fmt.Fprintf(&b, "## %s\n\n", stringField(entry, "label"))
		if meta := anyMap(entry["meta"]); meta != nil {
			appendMarkdownList(&b, "Completed", stringSliceField(meta, "completed"))
			appendMarkdownList(&b, "In Flight", stringSliceField(meta, "in_flight"))
			appendMarkdownList(&b, "Blockers", stringSliceField(meta, "blockers"))
			appendMarkdownList(&b, "Next", stringSliceField(meta, "next_focus"))
			if notes := stringField(meta, "notes"); notes != "" {
				fmt.Fprintf(&b, "Notes: %s\n\n", notes)
			}
		}
	}
	return b.String()
}

func renderRoomRetroMarkdown(epic map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Retro Guidance\n\n")
	updates := mapSlice(epic["guidance_updates"])
	if len(updates) == 0 {
		fmt.Fprintf(&b, "No retro guidance updates recorded yet.\n")
		return b.String()
	}
	for _, update := range updates {
		meta := anyMap(update["meta"])
		title := firstNonEmpty(stringField(update, "summary"), stringField(update, "id"))
		fmt.Fprintf(&b, "## %s\n\n", title)
		fmt.Fprintf(&b, "- ID: `%s`\n", stringField(update, "id"))
		fmt.Fprintf(&b, "- Kind: `%s`\n", stringField(meta, "kind"))
		if milestoneID := stringField(meta, "milestone_id"); milestoneID != "" {
			fmt.Fprintf(&b, "- Milestone ID: `%s`\n", milestoneID)
		}
		if impact := stringField(meta, "impact"); impact != "" {
			fmt.Fprintf(&b, "\n### Impact\n\n%s\n", impact)
		}
		if change := stringField(meta, "recommended_change"); change != "" {
			fmt.Fprintf(&b, "\n### Recommended Change\n\n%s\n", change)
		}
		appendMarkdownList(&b, "Scope", stringSliceField(meta, "scope"))
		appendMarkdownList(&b, "Follow-up", stringSliceField(meta, "follow_up"))
		fmt.Fprintf(&b, "\n")
	}
	return b.String()
}

func renderRoomMilestoneMarkdown(milestone map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Milestone %s\n\n", stringField(milestone, "title"))
	fmt.Fprintf(&b, "- ID: `%s`\n", stringField(milestone, "id"))
	fmt.Fprintf(&b, "- Epic ID: `%s`\n", stringField(milestone, "epic_id"))
	fmt.Fprintf(&b, "- Status: `%s`\n", stringField(milestone, "status"))
	fmt.Fprintf(&b, "- Stories: `%d`\n", intField(milestone, "story_count"))
	fmt.Fprintf(&b, "- Validated stories: `%d`\n", intField(milestone, "validated_story_count"))
	if meta := anyMap(milestone["meta"]); meta != nil {
		if goal := stringField(meta, "goal"); goal != "" {
			fmt.Fprintf(&b, "- Goal: %s\n", goal)
		}
		if objective := stringField(meta, "objective"); objective != "" {
			fmt.Fprintf(&b, "- Objective: %s\n", objective)
		}
		if owner := stringField(meta, "owner"); owner != "" {
			fmt.Fprintf(&b, "- Owner: %s\n", owner)
		}
		appendMarkdownList(&b, "Scope", stringSliceField(meta, "scope"))
		appendMarkdownList(&b, "Risks", stringSliceField(meta, "risks"))
		appendMarkdownList(&b, "Exclusions", stringSliceField(meta, "exclusions"))
		appendMarkdownList(&b, "Dependencies", stringSliceField(meta, "dependencies"))
		appendMarkdownList(&b, "Validators Expected", stringSliceField(meta, "validators_expected"))
		appendMarkdownList(&b, "Required Evidence Lanes", stringSliceField(meta, "required_evidence_lanes"))
		appendMarkdownList(&b, "Optional Evidence Lanes", stringSliceField(meta, "optional_evidence_lanes"))
		appendMarkdownList(&b, "Exit Criteria", stringSliceField(meta, "exit_criteria"))
	}
	if status := stringField(milestone, "required_lane_status"); status != "" && status != "not_configured" {
		fmt.Fprintf(&b, "\n## Required Lane Status\n")
		fmt.Fprintf(&b, "- Status: `%s`\n", status)
		appendMarkdownList(&b, "Covered Required Lanes", stringSliceValue(milestone["required_lane_covered"]))
		appendMarkdownList(&b, "Missing Required Lanes", stringSliceValue(milestone["required_lane_missing"]))
	}
	return b.String()
}

func renderRoomMilestoneCriteriaMarkdown(milestone map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Criteria\n\n")
	criteria := mapSlice(milestone["criteria"])
	if len(criteria) == 0 {
		appendMarkdownEmptyState(&b, "Acceptance Criteria", "No acceptance criteria recorded yet.")
		return b.String()
	}
	for _, criterion := range criteria {
		root := mapField(criterion, "root")
		if root == nil {
			root = criterion
		}
		if body := stringField(root, "body"); body != "" {
			fmt.Fprintf(&b, "- %s\n", body)
		}
	}
	return b.String()
}

func renderRoomMilestoneSummaryMarkdown(milestone map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Milestone Summary\n\n")
	fmt.Fprintf(&b, "- Story count: `%d`\n", intField(milestone, "story_count"))
	fmt.Fprintf(&b, "- Validated story count: `%d`\n", intField(milestone, "validated_story_count"))
	fmt.Fprintf(&b, "- Passed story count: `%d`\n", intField(milestone, "passed_story_count"))
	fmt.Fprintf(&b, "- Failed story count: `%d`\n", intField(milestone, "failed_story_count"))
	fmt.Fprintf(&b, "- Blocked story count: `%d`\n", intField(milestone, "blocked_story_count"))
	fmt.Fprintf(&b, "- Waived story count: `%d`\n", intField(milestone, "waived_story_count"))
	if status := stringField(milestone, "required_lane_status"); status != "" && status != "not_configured" {
		fmt.Fprintf(&b, "- Required lane status: `%s`\n", status)
		appendMarkdownList(&b, "Covered Required Lanes", stringSliceValue(milestone["required_lane_covered"]))
		appendMarkdownList(&b, "Missing Required Lanes", stringSliceValue(milestone["required_lane_missing"]))
	}
	if summaryMeta := anyMap(milestone["summary_meta"]); summaryMeta != nil {
		if summary := stringField(summaryMeta, "summary"); summary != "" {
			fmt.Fprintf(&b, "\n## Summary\n\n%s\n", summary)
		}
		appendMarkdownList(&b, "Passed Criteria", stringSliceField(summaryMeta, "passed_criteria"))
		appendMarkdownList(&b, "Failed Criteria", stringSliceField(summaryMeta, "failed_criteria"))
		appendMarkdownList(&b, "Waived Validations", stringSliceField(summaryMeta, "waived_validation_ids"))
		appendMarkdownList(&b, "Blocking Validations", stringSliceField(summaryMeta, "blocking_validation_ids"))
		appendMarkdownList(&b, "Notable Decisions", stringSliceField(summaryMeta, "notable_decisions"))
		appendMarkdownList(&b, "Systemic Findings", stringSliceField(summaryMeta, "systemic_findings"))
		appendMarkdownList(&b, "Recommended Next", stringSliceField(summaryMeta, "recommended_next"))
		appendMarkdownList(&b, "Guidance Updates", stringSliceField(summaryMeta, "guidance_updates"))
	} else if ids := stringSliceValue(milestone["blocking_validation_ids"]); len(ids) > 0 {
		appendMarkdownList(&b, "Blocking validation ids", ids)
	} else {
		appendMarkdownEmptyState(&b, "Summary", "No milestone summary recorded yet.")
	}
	if laneCounts := anyMap(milestone["lane_counts"]); len(laneCounts) > 0 {
		fmt.Fprintf(&b, "\n## Evidence Lanes\n")
		keys := make([]string, 0, len(laneCounts))
		for key := range laneCounts {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		coverage := anyMap(milestone["lane_coverage"])
		waivers := anyMap(milestone["lane_waivers"])
		blockers := anyMap(milestone["lane_blockers"])
		for _, key := range keys {
			line := fmt.Sprintf("- `%s`: seen `%d`, covered `%d`, waived `%d`", key, intField(laneCounts, key), intField(coverage, key), intField(waivers, key))
			blockingIDs := stringSliceValue(blockers[key])
			if len(blockingIDs) > 0 {
				line += fmt.Sprintf(", blocking `%s`", strings.Join(blockingIDs, "`, `"))
			}
			fmt.Fprintln(&b, line)
		}
	} else {
		appendMarkdownEmptyState(&b, "Evidence Lanes", "No evidence lanes recorded yet.")
	}
	return b.String()
}

func renderRoomStoryMarkdown(story map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Story %s\n\n", stringField(story, "title"))
	fmt.Fprintf(&b, "- ID: `%s`\n", stringField(story, "id"))
	fmt.Fprintf(&b, "- Milestone ID: `%s`\n", stringField(story, "milestone_id"))
	fmt.Fprintf(&b, "- State: `%s`\n", stringField(story, "state"))
	fmt.Fprintf(&b, "- Validation count: `%d`\n", intField(story, "validation_count"))
	if latest := stringField(story, "latest_validation_status"); latest != "" {
		fmt.Fprintf(&b, "- Latest validation status: `%s`\n", latest)
	}
	if reason := stringField(story, "state_reason"); reason != "" {
		fmt.Fprintf(&b, "- State reason: %s\n", reason)
	}
	if blockedBy := stringField(story, "blocked_by"); blockedBy != "" {
		fmt.Fprintf(&b, "- Blocked by: `%s`\n", blockedBy)
	}
	if reviewer := stringField(story, "reviewer"); reviewer != "" {
		fmt.Fprintf(&b, "- Reviewer: `%s`\n", reviewer)
	}
	if meta := anyMap(story["meta"]); meta != nil {
		if owner := stringField(meta, "owner"); owner != "" {
			fmt.Fprintf(&b, "- Owner: %s\n", owner)
		}
		if desc := stringField(meta, "description"); desc != "" {
			fmt.Fprintf(&b, "\n## Description\n\n%s\n", desc)
		} else {
			appendMarkdownEmptyState(&b, "Description", "No story description recorded yet.")
		}
	}
	if history := mapSlice(story["state_history"]); len(history) > 0 {
		fmt.Fprintf(&b, "\n## State History\n")
		for _, item := range history {
			meta := anyMap(item["meta"])
			state := stringField(meta, "state")
			reason := stringField(meta, "reason")
			if reason != "" {
				fmt.Fprintf(&b, "- `%s`: %s\n", state, reason)
			} else {
				fmt.Fprintf(&b, "- `%s`\n", state)
			}
		}
	} else {
		appendMarkdownEmptyState(&b, "State History", "No story state transitions recorded yet.")
	}
	if lanes := mapField(story, "evidence_lanes"); len(lanes) > 0 {
		fmt.Fprintf(&b, "\n## Evidence Lanes\n")
		keys := make([]string, 0, len(lanes))
		for key := range lanes {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lane := anyMap(lanes[key])
			line := fmt.Sprintf("- `%s`: %s", key, stringField(lane, "latest_status"))
			if id := stringField(lane, "latest_validation_id"); id != "" {
				line += fmt.Sprintf(" (`%s`)", id)
			}
			if boolField(lane, "waived") {
				line += " [waived]"
			}
			if boolField(lane, "blocking") {
				line += " [blocking]"
			}
			fmt.Fprintln(&b, line)
		}
	} else {
		appendMarkdownEmptyState(&b, "Evidence Lanes", "No evidence lanes recorded yet.")
	}
	if validations := mapSlice(story["validations"]); len(validations) > 0 {
		fmt.Fprintf(&b, "\n## Validation History\n")
		for i := len(validations) - 1; i >= 0; i-- {
			validation := validations[i]
			meta := anyMap(validation["meta"])
			line := fmt.Sprintf("- `%s`: `%s`", stringField(meta, "validator_type"), stringField(meta, "status"))
			if summary := stringField(meta, "summary"); summary != "" {
				line += fmt.Sprintf(" — %s", summary)
			}
			if id := stringField(validation, "id"); id != "" {
				line += fmt.Sprintf(" (`%s`)", id)
			}
			fmt.Fprintln(&b, line)
		}
	} else {
		appendMarkdownEmptyState(&b, "Validation History", "No validation entries recorded yet.")
	}
	return b.String()
}

func renderRoomStoryValidationMarkdown(validation map[string]any) string {
	var b strings.Builder
	meta := anyMap(validation["meta"])
	fmt.Fprintf(&b, "# Story Validation %s\n\n", stringField(validation, "id"))
	if root := mapField(validation, "root"); root != nil {
		if createdAt := stringField(root, "created_at"); createdAt != "" {
			fmt.Fprintf(&b, "- Created at: `%s`\n", createdAt)
		}
		if sender := stringField(root, "sender"); sender != "" {
			fmt.Fprintf(&b, "- Created by: `%s`\n", sender)
		}
	}
	fmt.Fprintf(&b, "- Validator type: `%s`\n", stringField(meta, "validator_type"))
	fmt.Fprintf(&b, "- Status: `%s`\n", stringField(meta, "status"))
	if summary := stringField(meta, "summary"); summary != "" {
		fmt.Fprintf(&b, "- Summary: %s\n", summary)
	}
	if commandText := stringField(meta, "command"); commandText != "" {
		fmt.Fprintf(&b, "- Command: `%s`\n", commandText)
	}
	if artifactPath := stringField(meta, "artifact_path"); artifactPath != "" {
		fmt.Fprintf(&b, "- Artifact path: `%s`\n", artifactPath)
	}
	if digest := stringField(meta, "artifact_digest"); digest != "" {
		fmt.Fprintf(&b, "- Artifact digest: `%s`\n", digest)
	}
	appendMarkdownList(&b, "Related story ids", stringSliceField(meta, "related_story_ids"))
	if waiverReason := stringField(meta, "waiver_reason"); waiverReason != "" {
		fmt.Fprintf(&b, "\n## Waiver Reason\n\n%s\n", waiverReason)
	}
	if notes := stringField(meta, "notes"); notes != "" {
		fmt.Fprintf(&b, "\n## Notes\n\n%s\n", notes)
	} else {
		appendMarkdownEmptyState(&b, "Notes", "No additional notes recorded.")
	}
	return b.String()
}

func appendMarkdownList(b *strings.Builder, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", heading)
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		fmt.Fprintf(b, "- %s\n", item)
	}
}

func appendMarkdownEmptyState(b *strings.Builder, heading, text string) {
	heading = strings.TrimSpace(heading)
	text = strings.TrimSpace(text)
	if heading == "" || text == "" {
		return
	}
	fmt.Fprintf(b, "\n## %s\n\n%s\n", heading, text)
}

func mapSlice(value any) []map[string]any {
	if maps, ok := value.([]map[string]any); ok {
		return maps
	}
	raw, ok := value.([]any)
	if !ok {
		var decoded []map[string]any
		data, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil
		}
		return decoded
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func truncateRoomMapSlice(items []map[string]any, limit int) []map[string]any {
	if limit <= 0 || len(items) <= limit {
		return append([]map[string]any(nil), items...)
	}
	return append([]map[string]any(nil), items[:limit]...)
}

func mapField(value any, key string) map[string]any {
	m := anyMap(value)
	if m == nil {
		return nil
	}
	child, ok := m[key].(map[string]any)
	if !ok {
		child = anyMap(m[key])
	}
	if child == nil {
		return nil
	}
	return child
}

func stringField(value any, key string) string {
	m := anyMap(value)
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func intField(value any, key string) int {
	m := anyMap(value)
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func boolField(value any, key string) bool {
	m := anyMap(value)
	if m == nil {
		return false
	}
	switch v := m[key].(type) {
	case bool:
		return v
	default:
		return false
	}
}

func stringSliceField(value any, key string) []string {
	m := anyMap(value)
	if m == nil {
		return nil
	}
	return stringSliceValue(m[key])
}

func stringSliceValue(value any) []string {
	switch v := value.(type) {
	case []string:
		return append([]string(nil), v...)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		var decoded []string
		data, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return nil
		}
		return decoded
	}
}

func stringSliceContains(values []string, want string) bool {
	want = strings.TrimSpace(want)
	if want == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == want {
			return true
		}
	}
	return false
}

func anyMap(value any) map[string]any {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil
	}
	return decoded
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

type roomInterviewSessionMeta struct {
	Topic       string
	Spec        string
	SpecRef     string
	Submitter   string
	Questioner  string
	Respondent  string
	Verifier    string
	Constraints []string
}

func runRoomInterviewStart(cmd *cobra.Command, workspace, sender, roomID, topic, spec, specRef, submitter, questioner, respondent, verifier string, constraints []string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomInterviewCommand(cmd, workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()

	topic = strings.TrimSpace(topic)
	if topic == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.start", protocol.ErrorCodeEARG, "topic is required", map[string]any{
			"hint": "Pass a concise topic such as `phase-4-api-contract` or `retry-loop-meaning-check`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	submitter = firstNonEmpty(strings.TrimSpace(submitter), identity.Sender)
	verifier = firstNonEmpty(strings.TrimSpace(verifier), submitter)
	body := buildRoomInterviewSessionBody(topic, spec, specRef, submitter, questioner, respondent, verifier, constraints)
	msg := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		Stream:      agent.RoomStreamName(roomID),
		Sender:      identity.Sender,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindInterviewSession,
		Priority:    agent.DefaultPriority,
		Subject:     "Interview Session: " + topic,
		Body:        body,
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.start", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.interview.start", map[string]any{
		"room_id":         roomID,
		"session_id":      msg.ID,
		"topic":           topic,
		"message":         msg,
		"sender_identity": identity,
		"room":            summary,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomInterviewAsk(cmd *cobra.Command, workspace, sender, roomID, sessionID, recipient, question string) error {
	absWorkspace, identity, store, roomID, _, err := prepareRoomInterviewCommand(cmd, workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	sessionMsg, meta, err := loadRoomInterviewSession(cmd.Context(), store, absWorkspace, roomID, sessionID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.ask", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Start a session with `agentctl room interview start` and reuse its session_id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	recipient = firstNonEmpty(strings.TrimSpace(recipient), strings.TrimSpace(meta.Respondent))
	if recipient == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.ask", protocol.ErrorCodeEARG, "recipient is required", map[string]any{
			"hint": "Set --to or define a respondent when starting the interview session.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	question = strings.TrimSpace(question)
	if question == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.ask", protocol.ErrorCodeEARG, "question is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: sessionMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        recipient,
		Kind:             agent.BoardMessageKindInterviewQuestion,
		Priority:         agent.DefaultPriority,
		ReplyExpected:    true,
		Subject:          "Interview Question: " + deriveRoomSubject(question),
		Body:             question,
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.ask", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.interview.ask", map[string]any{
		"room_id":    roomID,
		"session_id": sessionMsg.ID,
		"message":    msg,
		"session":    meta,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomInterviewAnswer(cmd *cobra.Command, workspace, sender, roomID, questionID, answer string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomInterviewCommand(cmd, workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	questionMsg, sessionMsg, meta, err := loadRoomInterviewQuestion(cmd.Context(), store, absWorkspace, roomID, questionID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.answer", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Use a question id returned by `agentctl room interview ask` or listed in `agentctl room interview show`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if questionMsg.Recipient != "" && questionMsg.Recipient != agent.BroadcastRecipient && !sameRoomParticipant(questionMsg.Recipient, identity.Sender) && !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.answer", protocol.ErrorCodeEARG, "only the intended respondent or coordinator can answer this interview question", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.answer", protocol.ErrorCodeEARG, "answer is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	recipient := firstNonEmpty(strings.TrimSpace(meta.Verifier), strings.TrimSpace(meta.Submitter), strings.TrimSpace(questionMsg.Sender))
	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: questionMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        recipient,
		Kind:             agent.BoardMessageKindInterviewAnswer,
		Priority:         agent.DefaultPriority,
		ReplyExpected:    true,
		Subject:          "Interview Answer: " + deriveRoomSubject(answer),
		Body:             answer,
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.answer", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.interview.answer", map[string]any{
		"room_id":     roomID,
		"session_id":  sessionMsg.ID,
		"question_id": questionMsg.ID,
		"message":     msg,
		"session":     meta,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomInterviewVerify(cmd *cobra.Command, workspace, sender, roomID, answerID, verdict, notes string) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomInterviewCommand(cmd, workspace, sender, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	answerMsg, questionMsg, sessionMsg, meta, err := loadRoomInterviewAnswer(cmd.Context(), store, absWorkspace, roomID, answerID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.verify", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Use an answer id returned by `agentctl room interview answer` or listed in `agentctl room interview show`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !sameRoomParticipant(identity.Sender, meta.Verifier) && !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.verify", protocol.ErrorCodeEARG, "only the verifier or coordinator can record an interview verdict", map[string]any{
			"hint": "Set --verifier when starting the session, or run the command as the room coordinator.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	verdict = strings.TrimSpace(strings.ToLower(verdict))
	switch verdict {
	case "accept", "clarify", "reject":
	default:
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.verify", protocol.ErrorCodeEARG, fmt.Sprintf("unsupported interview verdict %q", verdict), map[string]any{
			"hint": "Use accept, clarify, or reject.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	notes = strings.TrimSpace(notes)
	if notes == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.verify", protocol.ErrorCodeEARG, "notes are required", map[string]any{
			"hint": "Capture why the answer matched, needed clarification, or diverged from intent.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	msg := &agent.BoardMessage{
		WorkspaceID:      absWorkspace,
		RelatedMessageID: answerMsg.ID,
		Stream:           agent.RoomStreamName(roomID),
		Sender:           identity.Sender,
		Recipient:        answerMsg.Sender,
		Kind:             agent.BoardMessageKindInterviewVerify,
		Priority:         agent.DefaultPriority,
		Subject:          "Interview Verdict: " + verdict,
		Body:             notes,
	}
	if verdict != "accept" {
		msg.ReplyExpected = true
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.verify", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.interview.verify", map[string]any{
		"room_id":     roomID,
		"session_id":  sessionMsg.ID,
		"question_id": questionMsg.ID,
		"answer_id":   answerMsg.ID,
		"message":     msg,
		"session":     meta,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomInterviewShow(cmd *cobra.Command, workspace, roomID, sessionID string, limit int) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.show", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()
	summary, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, "", limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.show", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	sessions := buildRoomInterviewSessions(messages)
	if strings.TrimSpace(sessionID) == "" {
		return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.interview.show", map[string]any{
			"room":     summary,
			"count":    len(sessions),
			"sessions": sessions,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	for _, session := range sessions {
		if session["id"] == strings.TrimSpace(sessionID) {
			return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.interview.show", map[string]any{
				"room":    summary,
				"session": session,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.show", protocol.ErrorCodeENotFound, fmt.Sprintf("interview session %q not found", sessionID), map[string]any{
		"hint": "Run `agentctl room interview show <room-id>` to list available interview sessions.",
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomInterviewNext(cmd *cobra.Command, workspace, roomID, actorID string, limit int) error {
	absWorkspace, identity, store, roomID, summary, err := prepareRoomInterviewCommand(cmd, workspace, actorID, roomID)
	if err != nil {
		return err
	}
	defer store.Close()
	_, messages, err := loadRoomState(cmd.Context(), store, absWorkspace, roomID, identity.Sender, limit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview.next", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	next, ok := findNextRoomInterviewItem(identity.Sender, summary, messages)
	if !ok {
		return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.interview.next", map[string]any{
			"room":     summary,
			"actor_id": identity.Sender,
			"pending":  false,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.interview.next", map[string]any{
		"room":     summary,
		"actor_id": identity.Sender,
		"pending":  true,
		"item":     next,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func prepareRoomInterviewCommand(cmd *cobra.Command, workspace, sender, roomID string) (string, roomIdentity, blackboard.BoardStore, string, agent.RoomSummary, error) {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	roomID = strings.TrimSpace(roomID)
	summary, err := store.GetRoom(cmd.Context(), absWorkspace, roomID, identity.Sender)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		store.Close()
		return "", roomIdentity{}, nil, "", agent.RoomSummary{}, protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.interview", code, err.Error(), map[string]any{
			"hint": "Create the room first or check the room id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return absWorkspace, identity, store, roomID, summary, nil
}

func loadRoomInterviewSession(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, sessionID string) (agent.BoardMessage, roomInterviewSessionMeta, error) {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return agent.BoardMessage{}, roomInterviewSessionMeta{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	for _, msg := range messages {
		if msg.ID == sessionID && msg.Kind == agent.BoardMessageKindInterviewSession {
			return msg, parseRoomInterviewSessionBody(msg.Body), nil
		}
	}
	return agent.BoardMessage{}, roomInterviewSessionMeta{}, fmt.Errorf("interview session %q not found", sessionID)
}

func loadRoomInterviewQuestion(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, questionID string) (agent.BoardMessage, agent.BoardMessage, roomInterviewSessionMeta, error) {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return agent.BoardMessage{}, agent.BoardMessage{}, roomInterviewSessionMeta{}, err
	}
	questionID = strings.TrimSpace(questionID)
	var question agent.BoardMessage
	for _, msg := range messages {
		if msg.ID == questionID && msg.Kind == agent.BoardMessageKindInterviewQuestion {
			question = msg
			break
		}
	}
	if question.ID == "" {
		return agent.BoardMessage{}, agent.BoardMessage{}, roomInterviewSessionMeta{}, fmt.Errorf("interview question %q not found", questionID)
	}
	sessionID := strings.TrimSpace(question.RelatedMessageID)
	session, meta, err := loadRoomInterviewSession(ctx, store, workspaceID, roomID, sessionID)
	if err != nil {
		return agent.BoardMessage{}, agent.BoardMessage{}, roomInterviewSessionMeta{}, err
	}
	return question, session, meta, nil
}

func loadRoomInterviewAnswer(ctx context.Context, store blackboard.BoardStore, workspaceID, roomID, answerID string) (agent.BoardMessage, agent.BoardMessage, agent.BoardMessage, roomInterviewSessionMeta, error) {
	messages, err := store.ListRoomMessages(ctx, workspaceID, roomID, roomTaskScanLimit)
	if err != nil {
		return agent.BoardMessage{}, agent.BoardMessage{}, agent.BoardMessage{}, roomInterviewSessionMeta{}, err
	}
	answerID = strings.TrimSpace(answerID)
	var answer agent.BoardMessage
	for _, msg := range messages {
		if msg.ID == answerID && msg.Kind == agent.BoardMessageKindInterviewAnswer {
			answer = msg
			break
		}
	}
	if answer.ID == "" {
		return agent.BoardMessage{}, agent.BoardMessage{}, agent.BoardMessage{}, roomInterviewSessionMeta{}, fmt.Errorf("interview answer %q not found", answerID)
	}
	question, session, meta, err := loadRoomInterviewQuestion(ctx, store, workspaceID, roomID, answer.RelatedMessageID)
	if err != nil {
		return agent.BoardMessage{}, agent.BoardMessage{}, agent.BoardMessage{}, roomInterviewSessionMeta{}, err
	}
	return answer, question, session, meta, nil
}

func buildRoomInterviewSessionBody(topic, spec, specRef, submitter, questioner, respondent, verifier string, constraints []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Topic: %s\n", strings.TrimSpace(topic))
	if strings.TrimSpace(spec) != "" {
		fmt.Fprintf(&b, "Spec: %s\n", strings.TrimSpace(spec))
	}
	if strings.TrimSpace(specRef) != "" {
		fmt.Fprintf(&b, "SpecRef: %s\n", strings.TrimSpace(specRef))
	}
	if strings.TrimSpace(submitter) != "" {
		fmt.Fprintf(&b, "Submitter: %s\n", strings.TrimSpace(submitter))
	}
	if strings.TrimSpace(questioner) != "" {
		fmt.Fprintf(&b, "Questioner: %s\n", strings.TrimSpace(questioner))
	}
	if strings.TrimSpace(respondent) != "" {
		fmt.Fprintf(&b, "Respondent: %s\n", strings.TrimSpace(respondent))
	}
	if strings.TrimSpace(verifier) != "" {
		fmt.Fprintf(&b, "Verifier: %s\n", strings.TrimSpace(verifier))
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
	b.WriteString("Protocol:\n- questioner writes directed questions\n- respondent answers one question at a time\n- verifier records accept/clarify/reject\n")
	return strings.TrimSpace(b.String())
}

func parseRoomInterviewSessionBody(body string) roomInterviewSessionMeta {
	meta := roomInterviewSessionMeta{}
	lines := strings.Split(body, "\n")
	inConstraints := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Protocol:") {
			break
		}
		if trimmed == "Constraints:" {
			inConstraints = true
			continue
		}
		if inConstraints {
			if strings.HasPrefix(trimmed, "- ") {
				meta.Constraints = append(meta.Constraints, strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))
			}
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Topic":
			meta.Topic = value
		case "Spec":
			meta.Spec = value
		case "SpecRef":
			meta.SpecRef = value
		case "Submitter":
			meta.Submitter = value
		case "Questioner":
			meta.Questioner = value
		case "Respondent":
			meta.Respondent = value
		case "Verifier":
			meta.Verifier = value
		}
	}
	return meta
}

func buildRoomInterviewSessions(messages []agent.BoardMessage) []map[string]any {
	type sessionState struct {
		Root      agent.BoardMessage
		Meta      roomInterviewSessionMeta
		Entries   []agent.BoardMessage
		Questions int
		Answers   int
		Verified  int
		Status    string
	}
	sessions := make(map[string]*sessionState)
	for _, msg := range messages {
		if msg.Kind == agent.BoardMessageKindInterviewSession {
			sessions[msg.ID] = &sessionState{
				Root:   msg,
				Meta:   parseRoomInterviewSessionBody(msg.Body),
				Status: "questioning",
			}
		}
	}
	questionToSession := make(map[string]string)
	answerToSession := make(map[string]string)
	answerByQuestion := make(map[string]agent.BoardMessage)
	for _, msg := range messages {
		if msg.Kind == agent.BoardMessageKindInterviewQuestion {
			if sessionID := strings.TrimSpace(msg.RelatedMessageID); sessionID != "" {
				questionToSession[msg.ID] = sessionID
			}
		}
	}
	for _, msg := range messages {
		if msg.Kind == agent.BoardMessageKindInterviewAnswer {
			answerByQuestion[strings.TrimSpace(msg.RelatedMessageID)] = msg
			if sessionID := questionToSession[strings.TrimSpace(msg.RelatedMessageID)]; sessionID != "" {
				answerToSession[msg.ID] = sessionID
			}
		}
	}
	for _, msg := range messages {
		related := strings.TrimSpace(msg.RelatedMessageID)
		if related == "" {
			continue
		}
		switch msg.Kind {
		case agent.BoardMessageKindInterviewQuestion:
			session := sessions[related]
			if session == nil {
				continue
			}
			session.Entries = append(session.Entries, msg)
			session.Questions++
			if _, ok := answerByQuestion[msg.ID]; !ok {
				session.Status = "awaiting_answer"
			}
		case agent.BoardMessageKindInterviewAnswer:
			session := sessions[answerToSession[msg.ID]]
			if session == nil {
				continue
			}
			session.Entries = append(session.Entries, msg)
			session.Answers++
			if session.Status == "questioning" || session.Status == "awaiting_answer" {
				session.Status = "awaiting_verification"
			}
		case agent.BoardMessageKindInterviewVerify:
			session := sessions[answerToSession[related]]
			if session == nil {
				continue
			}
			session.Entries = append(session.Entries, msg)
			session.Verified++
			subject := strings.ToLower(strings.TrimSpace(msg.Subject))
			switch {
			case strings.Contains(subject, "accept"):
				session.Status = "verified"
			case strings.Contains(subject, "reject"):
				session.Status = "rejected"
			default:
				session.Status = "needs_clarification"
			}
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
			"topic":       firstNonEmpty(session.Meta.Topic, strings.TrimPrefix(strings.TrimSpace(session.Root.Subject), "Interview Session: ")),
			"status":      session.Status,
			"root":        session.Root,
			"meta":        session.Meta,
			"entries":     session.Entries,
			"questions":   session.Questions,
			"answers":     session.Answers,
			"verified":    session.Verified,
			"entry_count": len(session.Entries),
		})
	}
	return out
}

func findNextRoomInterviewItem(actorID string, room agent.RoomSummary, messages []agent.BoardMessage) (map[string]any, bool) {
	sessions := buildRoomInterviewSessions(messages)
	for _, session := range sessions {
		meta, _ := session["meta"].(roomInterviewSessionMeta)
		entries, _ := session["entries"].([]agent.BoardMessage)
		questionIndex := make(map[string]agent.BoardMessage)
		answerIndex := make(map[string]agent.BoardMessage)
		verifyIndex := make(map[string]agent.BoardMessage)
		for _, entry := range entries {
			switch entry.Kind {
			case agent.BoardMessageKindInterviewQuestion:
				questionIndex[entry.ID] = entry
			case agent.BoardMessageKindInterviewAnswer:
				answerIndex[entry.ID] = entry
			case agent.BoardMessageKindInterviewVerify:
				verifyIndex[entry.RelatedMessageID] = entry
			}
		}
		for _, entry := range entries {
			switch entry.Kind {
			case agent.BoardMessageKindInterviewQuestion:
				if sameRoomParticipant(entry.Recipient, actorID) {
					if answer := findRoomInterviewAnswerForQuestion(entry.ID, answerIndex); answer.ID == "" {
						return map[string]any{
							"type":       "answer_question",
							"session_id": session["id"],
							"topic":      session["topic"],
							"message":    entry,
							"session":    meta,
						}, true
					}
				}
			case agent.BoardMessageKindInterviewAnswer:
				if sameRoomParticipant(meta.Verifier, actorID) || (meta.Verifier == "" && roomMemberHasRole(room.Members, actorID, "coordinator")) {
					if verifyIndex[entry.ID].ID == "" {
						question := questionIndex[strings.TrimSpace(entry.RelatedMessageID)]
						return map[string]any{
							"type":       "verify_answer",
							"session_id": session["id"],
							"topic":      session["topic"],
							"message":    entry,
							"question":   question,
							"session":    meta,
						}, true
					}
				}
			}
		}
	}
	return nil, false
}

func findRoomInterviewAnswerForQuestion(questionID string, answers map[string]agent.BoardMessage) agent.BoardMessage {
	for _, answer := range answers {
		if strings.TrimSpace(answer.RelatedMessageID) == strings.TrimSpace(questionID) {
			return answer
		}
	}
	return agent.BoardMessage{}
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

// roomMemberCanManageRoomTasks reports whether the sender may assign/reassign/reclaim room tasks.
// Eligible: room members with role coordinator or admin; system admin / overseer senders bypass
// room role checks. Other participants (including reviewers and unprivileged agents) cannot assign
// work to others — use a coordinator/admin pane or grant role=admin to a parent agent that should delegate.
func roomMemberCanManageRoomTasks(members []agent.RoomMember, sender string) bool {
	sender = strings.TrimSpace(sender)
	if sender == "" {
		return false
	}
	if agent.IsAdminSender(sender) || agent.IsOverseerSender(sender) {
		return true
	}
	return roomMemberHasRole(members, sender, "coordinator") || roomMemberHasRole(members, sender, "admin")
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
			for _, entry := range buildRoomStatusEntries(participant, messages, nil) {
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

type roomRedgreenState struct {
	Version       int        `json:"version"`
	RoomID        string     `json:"room_id"`
	Workspace     string     `json:"workspace"`
	Slug          string     `json:"slug"`
	RedActor      string     `json:"red_actor"`
	GreenActor    string     `json:"green_actor"`
	Coordinator   string     `json:"coordinator"`
	RedWorktree   string     `json:"red_worktree"`
	GreenWorktree string     `json:"green_worktree"`
	BaseRef       string     `json:"base_ref"`
	CheckCommand  string     `json:"check_command"`
	HiddenPaths   []string   `json:"hidden_paths,omitempty"`
	ChecksRun     int        `json:"checks_run"`
	LastStatus    string     `json:"last_status,omitempty"`
	LastExitCode  int        `json:"last_exit_code,omitempty"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type roomRedgreenCheckResult struct {
	Passed    bool      `json:"passed"`
	ExitCode  int       `json:"exit_code"`
	Command   string    `json:"command"`
	Output    string    `json:"output"`
	CheckedAt time.Time `json:"checked_at"`
	MessageID string    `json:"message_id,omitempty"`
	Recipient string    `json:"recipient"`
	Sender    string    `json:"sender"`
}

func runRoomRedgreenInit(cmd *cobra.Command, workspace, roomID, slug, title, description, redActor, greenActor, coordinator, worktreeRoot, baseRef, checkCommand string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	redActor = strings.TrimSpace(redActor)
	greenActor = strings.TrimSpace(greenActor)
	if redActor == "" || greenActor == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.init", protocol.ErrorCodeEARG, "both --red and --green actor ids are required", map[string]any{
			"hint": "Use distinct actor ids such as red-a and green-a.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if sameRoomParticipant(redActor, greenActor) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.init", protocol.ErrorCodeEARG, "red and green actors must be distinct", map[string]any{
			"hint": "Use different actor ids for the hidden-test author and implementation worker.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if strings.TrimSpace(coordinator) == "" {
		if identity, err := resolveRoomSender(cmd.Context(), ""); err == nil && strings.TrimSpace(identity.Sender) != "" {
			coordinator = identity.Sender
		} else {
			coordinator = "human-a"
		}
	}
	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.init", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()

	slug = sanitizeRoomRedgreenSlug(slug)
	if slug == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.init", protocol.ErrorCodeEARG, "slug must contain at least one alphanumeric character", map[string]any{
			"hint": "Use a short identifier such as retry-logic or parser-phase-1.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	members := mergeRoomMembers(nil,
		agent.RoomMember{ActorID: coordinator, Role: "coordinator"},
		agent.RoomMember{ActorID: redActor, Role: "reviewer"},
		agent.RoomMember{ActorID: greenActor, Role: "reviewer"},
	)
	if strings.TrimSpace(title) == "" {
		title = "Red/Green: " + slug
	}
	if strings.TrimSpace(description) == "" {
		description = "Brokered red/green room with hidden tests in a private worktree"
	}
	room, err := store.UpsertRoom(cmd.Context(), agent.Room{
		ID:          strings.TrimSpace(roomID),
		WorkspaceID: absWorkspace,
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
		Members:     members,
	})
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.init", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	worktreeRoot = strings.TrimSpace(worktreeRoot)
	if worktreeRoot == "" {
		worktreeRoot = filepath.Join(os.TempDir(), "agentctl-redgreen")
	}
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.init", protocol.ErrorCodeERuntime, fmt.Sprintf("mkdir worktree root: %v", err), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	redWorktree := filepath.Join(worktreeRoot, fmt.Sprintf("%s-red-%s", slug, stamp))
	greenWorktree := filepath.Join(worktreeRoot, fmt.Sprintf("%s-green-%s", slug, stamp))
	redBranch := fmt.Sprintf("redgreen-%s-red-%s", slug, stamp)
	greenBranch := fmt.Sprintf("redgreen-%s-green-%s", slug, stamp)
	if err := createRoomRedgreenWorktree(cmd.Context(), absWorkspace, redWorktree, redBranch, baseRef); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.init", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
			"hint": "Ensure the workspace is a git repository and the target worktree path does not already exist.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := createRoomRedgreenWorktree(cmd.Context(), absWorkspace, greenWorktree, greenBranch, baseRef); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.init", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
			"hint":       "The red worktree was created successfully; remove it manually if you want to retry with a clean slate.",
			"red_path":   redWorktree,
			"red_branch": redBranch,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	now := time.Now().UTC()
	state := roomRedgreenState{
		Version:       1,
		RoomID:        room.ID,
		Workspace:     absWorkspace,
		Slug:          slug,
		RedActor:      redActor,
		GreenActor:    greenActor,
		Coordinator:   coordinator,
		RedWorktree:   redWorktree,
		GreenWorktree: greenWorktree,
		BaseRef:       strings.TrimSpace(baseRef),
		CheckCommand:  firstNonEmpty(strings.TrimSpace(checkCommand), roomRedgreenDefaultCheckShell),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := saveRoomRedgreenState(absWorkspace, state); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.init", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
			"hint": "The room and worktrees were created, but agentctl could not persist red/green metadata under .agentctl/room-redgreen.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	systemSender := fmt.Sprintf("actor:system:room:%s", room.ID)
	initMessages := []*agent.BoardMessage{
		{
			WorkspaceID: absWorkspace,
			Stream:      agent.RoomStreamName(room.ID),
			Sender:      systemSender,
			Recipient:   agent.BroadcastRecipient,
			Kind:        agent.BoardMessageKindInfo,
			Priority:    agent.DefaultPriority,
			Subject:     "Red/Green protocol active",
			Body:        fmt.Sprintf("Brokered red/green mode is active for %s.\n\nRoles:\n- red: %s (private hidden tests)\n- green: %s (implementation only)\n- coordinator: %s\n\nGreen must request checks with `agentctl room redgreen check %s --workspace %s` instead of reading hidden tests directly.", slug, redActor, greenActor, coordinator, room.ID, absWorkspace),
		},
		{
			WorkspaceID: absWorkspace,
			Stream:      agent.RoomStreamName(room.ID),
			Sender:      systemSender,
			Recipient:   redActor,
			Kind:        agent.BoardMessageKindInstruction,
			Priority:    agent.DefaultPriority,
			Subject:     "Red ownership: hidden tests",
			Body:        fmt.Sprintf("Your private red worktree is:\n%s\n\nWrite hidden tests there and register any hidden files or directories with:\nagentctl room redgreen hide %s <relative-path> --workspace %s --sender %s\n\nDo not reveal hidden test contents in the room. Only share failure summaries.", redWorktree, room.ID, absWorkspace, redActor),
		},
		{
			WorkspaceID: absWorkspace,
			Stream:      agent.RoomStreamName(room.ID),
			Sender:      systemSender,
			Recipient:   greenActor,
			Kind:        agent.BoardMessageKindInstruction,
			Priority:    agent.DefaultPriority,
			Subject:     "Green ownership: implementation only",
			Body:        fmt.Sprintf("Your green worktree is:\n%s\n\nImplement there without reading hidden tests. Ask the broker to run the hidden suite with:\nagentctl room redgreen check %s --workspace %s --sender %s\n\nOnly the summarized result will be posted back into the room.", greenWorktree, room.ID, absWorkspace, greenActor),
		},
	}
	for _, msg := range initMessages {
		if err := store.SendMessage(cmd.Context(), msg); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.init", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
				"hint": "The room and worktrees are ready, but agentctl failed while posting the initial protocol messages.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}

	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.redgreen.init", map[string]any{
		"room":  room,
		"state": state,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomRedgreenHide(cmd *cobra.Command, workspace, sender, roomID, hiddenPath string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	state, err := loadRoomRedgreenState(absWorkspace, roomID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.hide", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Initialize brokered red/green mode first with `agentctl room redgreen init`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.hide", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside the red pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !sameRoomParticipant(identity.Sender, state.RedActor) && !sameRoomParticipant(identity.Sender, state.Coordinator) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.hide", protocol.ErrorCodeEARG, "only the red actor or coordinator can register hidden paths", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	relPath, err := resolveRoomRedgreenRelativePath(state.RedWorktree, hiddenPath)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.hide", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Use a path inside the red worktree, relative or absolute.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	state.HiddenPaths = appendUniqueRoomPaths(state.HiddenPaths, relPath)
	state.UpdatedAt = time.Now().UTC()
	if err := saveRoomRedgreenState(absWorkspace, state); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.hide", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.redgreen.hide", map[string]any{
		"room_id":      state.RoomID,
		"hidden_paths": state.HiddenPaths,
		"registered":   relPath,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomRedgreenShow(cmd *cobra.Command, workspace, sender, roomID string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	state, err := loadRoomRedgreenState(absWorkspace, roomID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.show", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Initialize brokered red/green mode first with `agentctl room redgreen init`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	viewer := strings.TrimSpace(sender)
	if viewer == "" {
		if identity, err := resolveRoomSender(cmd.Context(), ""); err == nil {
			viewer = identity.Sender
		}
	}
	data := map[string]any{
		"version":         state.Version,
		"room_id":         state.RoomID,
		"workspace":       state.Workspace,
		"slug":            state.Slug,
		"red_actor":       state.RedActor,
		"green_actor":     state.GreenActor,
		"coordinator":     state.Coordinator,
		"green_worktree":  state.GreenWorktree,
		"base_ref":        state.BaseRef,
		"check_command":   state.CheckCommand,
		"checks_run":      state.ChecksRun,
		"last_status":     state.LastStatus,
		"last_exit_code":  state.LastExitCode,
		"last_checked_at": state.LastCheckedAt,
		"created_at":      state.CreatedAt,
		"updated_at":      state.UpdatedAt,
	}
	if sameRoomParticipant(viewer, state.RedActor) || sameRoomParticipant(viewer, state.Coordinator) {
		data["red_worktree"] = state.RedWorktree
		data["hidden_paths"] = append([]string(nil), state.HiddenPaths...)
	} else {
		data["red_worktree"] = "[redacted]"
		data["hidden_paths"] = "[redacted]"
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.redgreen.show", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomRedgreenCheck(cmd *cobra.Command, workspace, sender, roomID, overrideCommand string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	state, err := loadRoomRedgreenState(absWorkspace, roomID)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.check", protocol.ErrorCodeENotFound, err.Error(), map[string]any{
			"hint": "Initialize brokered red/green mode first with `agentctl room redgreen init`.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.check", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside the green pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !sameRoomParticipant(identity.Sender, state.GreenActor) && !sameRoomParticipant(identity.Sender, state.Coordinator) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.check", protocol.ErrorCodeEARG, "only the green actor or coordinator can run brokered checks", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := syncRoomRedgreenWorktree(state.GreenWorktree, state.RedWorktree, state.HiddenPaths); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.check", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
			"hint": "Ensure both paired worktrees exist and the hidden paths were registered correctly.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	checkShell := firstNonEmpty(strings.TrimSpace(overrideCommand), strings.TrimSpace(state.CheckCommand), roomRedgreenDefaultCheckShell)
	result, err := executeRoomRedgreenCheck(cmd.Context(), state.RedWorktree, checkShell)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.check", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	state.ChecksRun++
	state.LastStatus = "failing"
	if result.Passed {
		state.LastStatus = "passing"
	}
	state.LastExitCode = result.ExitCode
	state.LastCheckedAt = &result.CheckedAt
	state.UpdatedAt = result.CheckedAt
	if err := saveRoomRedgreenState(absWorkspace, state); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.check", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
			"hint": "The brokered check finished, but agentctl failed while updating the local red/green metadata.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	store, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.check", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer store.Close()
	systemSender := fmt.Sprintf("actor:system:room:%s", state.RoomID)
	subject := "Red/Green check failed"
	kind := agent.BoardMessageKindAlert
	if result.Passed {
		subject = "Red/Green check passed"
		kind = agent.BoardMessageKindInfo
	}
	body := buildRoomRedgreenCheckBody(state, result)
	msg := &agent.BoardMessage{
		WorkspaceID:   absWorkspace,
		Stream:        agent.RoomStreamName(state.RoomID),
		Sender:        systemSender,
		Recipient:     state.GreenActor,
		Kind:          kind,
		Priority:      agent.DefaultPriority,
		Subject:       subject,
		Body:          body,
		ReplyExpected: !result.Passed,
	}
	if err := store.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.redgreen.check", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
			"hint": "The hidden-suite run completed, but agentctl failed while posting the durable result back into the room.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	result.MessageID = msg.ID
	result.Recipient = state.GreenActor
	result.Sender = systemSender
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.redgreen.check", map[string]any{
		"room_id": state.RoomID,
		"result":  result,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func roomRedgreenStatePath(workspace, roomID string) string {
	return filepath.Join(workspace, roomRedgreenMetadataDir, strings.TrimSpace(roomID)+".json")
}

func loadRoomRedgreenState(workspace, roomID string) (roomRedgreenState, error) {
	path := roomRedgreenStatePath(workspace, roomID)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return roomRedgreenState{}, fmt.Errorf("red/green state for room %q not found", roomID)
		}
		return roomRedgreenState{}, err
	}
	var state roomRedgreenState
	if err := json.Unmarshal(raw, &state); err != nil {
		return roomRedgreenState{}, err
	}
	return state, nil
}

func saveRoomRedgreenState(workspace string, state roomRedgreenState) error {
	path := roomRedgreenStatePath(workspace, state.RoomID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func sanitizeRoomRedgreenSlug(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	var b strings.Builder
	lastDash := false
	for _, r := range input {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func createRoomRedgreenWorktree(ctx context.Context, repoPath, targetPath, branch, baseRef string) error {
	if strings.TrimSpace(targetPath) == "" {
		return fmt.Errorf("worktree path is required")
	}
	if _, err := os.Stat(targetPath); err == nil {
		return fmt.Errorf("worktree path %q already exists", targetPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	args := []string{"-C", repoPath, "worktree", "add"}
	if strings.TrimSpace(branch) != "" {
		args = append(args, "-b", strings.TrimSpace(branch))
	}
	args = append(args, targetPath)
	if strings.TrimSpace(baseRef) != "" {
		args = append(args, strings.TrimSpace(baseRef))
	}
	out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git worktree add failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func boardMessageSliceValue(value any) []agent.BoardMessage {
	switch v := value.(type) {
	case []agent.BoardMessage:
		return append([]agent.BoardMessage(nil), v...)
	case []any:
		out := make([]agent.BoardMessage, 0, len(v))
		for _, item := range v {
			switch msg := item.(type) {
			case agent.BoardMessage:
				out = append(out, msg)
			case map[string]any:
				var decoded agent.BoardMessage
				data, err := json.Marshal(msg)
				if err != nil {
					continue
				}
				if err := json.Unmarshal(data, &decoded); err != nil {
					continue
				}
				out = append(out, decoded)
			}
		}
		return out
	default:
		return nil
	}
}

func resolveRoomRedgreenRelativePath(root, candidate string) (string, error) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return "", fmt.Errorf("hidden path is required")
	}
	var rel string
	if filepath.IsAbs(candidate) {
		var err error
		rel, err = filepath.Rel(root, candidate)
		if err != nil {
			return "", err
		}
	} else {
		rel = candidate
	}
	rel = filepath.Clean(rel)
	if rel == "." || rel == "" {
		return "", fmt.Errorf("hidden path must not be the worktree root")
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("hidden path must stay inside the red worktree")
	}
	return filepath.ToSlash(rel), nil
}

func appendUniqueRoomPaths(existing []string, value string) []string {
	for _, item := range existing {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(value)) {
			return existing
		}
	}
	out := append(append([]string(nil), existing...), value)
	sort.Strings(out)
	return out
}

func syncRoomRedgreenWorktree(srcRoot, dstRoot string, hiddenPaths []string) error {
	hidden := make(map[string]struct{}, len(hiddenPaths))
	for _, path := range hiddenPaths {
		path = filepath.ToSlash(strings.TrimSpace(path))
		if path != "" {
			hidden[path] = struct{}{}
		}
	}
	present := make(map[string]struct{})
	if err := filepath.Walk(srcRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if roomRedgreenPathIsHidden(rel, hidden) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		present[rel] = struct{}{}
		target := filepath.Join(dstRoot, filepath.FromSlash(rel))
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		return copyRoomRedgreenFile(path, target, info.Mode())
	}); err != nil {
		return err
	}
	toRemove := make([]string, 0)
	if err := filepath.Walk(dstRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dstRoot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if roomRedgreenPathIsHidden(rel, hidden) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if _, ok := present[rel]; ok {
			return nil
		}
		toRemove = append(toRemove, path)
		return nil
	}); err != nil {
		return err
	}
	sort.Slice(toRemove, func(i, j int) bool { return len(toRemove[i]) > len(toRemove[j]) })
	for _, path := range toRemove {
		if err := os.RemoveAll(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func roomRedgreenPathIsHidden(rel string, hidden map[string]struct{}) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return true
	}
	for hiddenPath := range hidden {
		if rel == hiddenPath || strings.HasPrefix(rel, hiddenPath+"/") {
			return true
		}
	}
	return false
}

func copyRoomRedgreenFile(src, dst string, mode os.FileMode) error {
	if mode&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		_ = os.Remove(dst)
		return os.Symlink(target, dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode.Perm())
}

func executeRoomRedgreenCheck(ctx context.Context, cwd, shellCommand string) (roomRedgreenCheckResult, error) {
	checkedAt := time.Now().UTC()
	cmd := exec.CommandContext(ctx, "/bin/zsh", "-lc", shellCommand)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	result := roomRedgreenCheckResult{
		Passed:    err == nil,
		ExitCode:  0,
		Command:   shellCommand,
		Output:    trimRoomRedgreenOutput(string(output)),
		CheckedAt: checkedAt,
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return roomRedgreenCheckResult{}, err
}

func trimRoomRedgreenOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "No stdout/stderr output."
	}
	const maxLen = 2400
	if len(output) <= maxLen {
		return output
	}
	return output[len(output)-maxLen:]
}

func buildRoomRedgreenCheckBody(state roomRedgreenState, result roomRedgreenCheckResult) string {
	status := "failed"
	if result.Passed {
		status = "passed"
	}
	return fmt.Sprintf("Brokered hidden-suite check %s for %s.\n\ncommand: %s\nchecks_run: %d\nexit_code: %d\nchecked_at: %s\n\noutput:\n%s", status, state.Slug, result.Command, state.ChecksRun, result.ExitCode, result.CheckedAt.Format(time.RFC3339), result.Output)
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

type roomRelayDeliveryFailure struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
}

type roomRelayResult struct {
	Backend          string                     `json:"backend"`
	DeliveredCount   int                        `json:"delivered_count"`
	FailedCount      int                        `json:"failed_count"`
	DeliveredTo      []string                   `json:"delivered_to,omitempty"`
	FailedMembers    []string                   `json:"failed_members,omitempty"`
	DeliveryFailures []roomRelayDeliveryFailure `json:"delivery_failures,omitempty"`
	SkippedMembers   []string                   `json:"skipped_members,omitempty"`
	Error            string                     `json:"error,omitempty"`
}

func defaultRoomRelayOptions() roomRelayOptions {
	return roomRelayOptions{Backend: "auto"}
}

// relayPersistedRoomMessages fans out already-stored messages to mux panes (tmux/zellij auto),
// using the same path as room loop / room relay. Delivery injects a trailing newline/Enter so the
// agent surface accepts the relayed text without a separate submit step.
func relayPersistedRoomMessages(ctx context.Context, boardStore blackboard.BoardStore, absWorkspace, roomID string, msgs []*agent.BoardMessage) []roomRelayResult {
	summary, err := boardStore.GetRoom(ctx, absWorkspace, strings.TrimSpace(roomID), "")
	if err != nil {
		return []roomRelayResult{{Backend: "auto", Error: err.Error()}}
	}
	client := tmuxbridge.New()
	relay := defaultRoomRelayOptions()
	out := make([]roomRelayResult, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		out = append(out, relayRoomMessage(ctx, client, summary, *m, relay))
	}
	return out
}

// roomSendRelayHook runs after a successful room send to fan out to participant mux panes. Tests may replace it.
var roomSendRelayHook = relayPersistedRoomMessages

// roomTaskNoLiveRelay is true when `room task --no-live-relay` was set (skip mux fan-out because
// a long-running room loop/relay already delivers new messages).
func roomTaskNoLiveRelay(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	v, err := cmd.Flags().GetBool("no-live-relay")
	if err != nil {
		return false
	}
	return v
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
	seenTmux := make(map[string]struct{}, len(tmuxTargets))
	for _, target := range tmuxTargets {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		if _, dup := seenTmux[target]; dup {
			continue
		}
		seenTmux[target] = struct{}{}
		_, err := client.DeliverTextWithOptions(ctx, target, formatRoomRelayContent(room, msg), tmuxbridge.DeliverOptions{Interrupt: msg.Interrupt})
		if err != nil {
			result.FailedCount++
			result.FailedMembers = append(result.FailedMembers, target)
			result.DeliveryFailures = append(result.DeliveryFailures, roomRelayDeliveryFailure{Target: target, Reason: err.Error()})
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
	members, skipped := collectRoomRelayMembers(room, msg)
	result.SkippedMembers = append(result.SkippedMembers, skipped...)
	content := formatRoomRelayContent(room, msg)
	for _, member := range members {
		if roomMemberRelayBackend(member) != "tmux" {
			continue
		}
		target := roomMemberTmuxTarget(member)
		if strings.TrimSpace(target) == "" {
			result.FailedCount++
			aid := strings.TrimSpace(member.ActorID)
			result.FailedMembers = append(result.FailedMembers, aid)
			result.DeliveryFailures = append(result.DeliveryFailures, roomRelayDeliveryFailure{Target: aid, Reason: "no tmux pane target (empty PaneID and actor id)"})
			continue
		}
		_, err := client.DeliverTextWithOptions(ctx, target, content, tmuxbridge.DeliverOptions{Interrupt: msg.Interrupt})
		if err != nil {
			result.FailedCount++
			result.FailedMembers = append(result.FailedMembers, target)
			result.DeliveryFailures = append(result.DeliveryFailures, roomRelayDeliveryFailure{Target: target, Reason: err.Error()})
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
	var main string
	if subject != "" && body != subject {
		main = fmt.Sprintf("%s %s\n%s", prefix, subject, body)
	} else {
		main = fmt.Sprintf("%s %s", prefix, body)
	}
	if msg.AckRequired || msg.ReplyExpected {
		main += "\nAction: open your inbox (`agentctl room inbox <room> --actor <you>`), acknowledge if required, then reply or complete the requested follow-up."
	}
	return main
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

func roomInboxEntryForActor(actorID string, msg agent.BoardMessage, includeBroadcasts bool, latestBySender map[string]roomSenderActivity) (roomInboxEntry, bool) {
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

	interviewPending := roomMessageHasInterviewPendingWork(msg)
	flags := make([]string, 0, 3)
	if msg.AckRequired && msg.Status != agent.BoardMessageStatusAcked {
		flags = append(flags, "ACK-REQUIRED")
	}
	if msg.ReplyExpected && msg.Status != agent.BoardMessageStatusAcked {
		flags = append(flags, "REPLY-EXPECTED")
	}
	if interviewPending {
		flags = append(flags, "INTERVIEW")
	}
	category := "direct"
	switch {
	case interviewPending:
		category = "interview"
	case msg.AckRequired && msg.Status != agent.BoardMessageStatusAcked:
		category = "ack-required"
	case msg.ReplyExpected && msg.Status != agent.BoardMessageStatusAcked:
		category = "reply-expected"
	case isBroadcast:
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

type roomSenderActivity struct {
	CreatedAt time.Time
	MessageID string
}

func latestRoomSenderActivity(messages []agent.BoardMessage) map[string]roomSenderActivity {
	latest := make(map[string]roomSenderActivity, len(messages))
	for _, msg := range messages {
		sender := strings.TrimSpace(msg.Sender)
		if sender == "" {
			continue
		}
		current, ok := latest[sender]
		if !ok || msg.CreatedAt.After(current.CreatedAt) || (msg.CreatedAt.Equal(current.CreatedAt) && strings.TrimSpace(msg.ID) > current.MessageID) {
			latest[sender] = roomSenderActivity{
				CreatedAt: msg.CreatedAt,
				MessageID: strings.TrimSpace(msg.ID),
			}
		}
	}
	return latest
}

func messageStillAwaitsReply(msg agent.BoardMessage, latestBySender map[string]roomSenderActivity) bool {
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
	// A reply counts when the recipient speaks later, or when a different
	// message from that recipient lands at the same timestamp.
	if latestReply.CreatedAt.After(msg.CreatedAt) {
		return false
	}
	if latestReply.CreatedAt.Equal(msg.CreatedAt) && latestReply.MessageID != strings.TrimSpace(msg.ID) {
		return false
	}
	return true
}

func normalizeRoomInboxFilter(filter string) string {
	switch strings.TrimSpace(strings.ToLower(filter)) {
	case "ack-required", "reply-expected", "direct", "broadcast", "interview":
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

func buildRoomStatusParticipants(room agent.RoomSummary, messages []agent.BoardMessage, tasks []taskstore.Task, staleAfter time.Duration, reminderRoots map[string]struct{}) []roomStatusParticipant {
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
		if activity, ok := latestBySender[actorID]; ok {
			tsCopy := activity.CreatedAt
			p.LastActiveAt = &tsCopy
			if staleAfter > 0 && now.Sub(activity.CreatedAt) > staleAfter {
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
		entries := buildRoomStatusEntries(actorID, messages, reminderRoots)
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

func buildRoomStatusBacklog(room agent.RoomSummary, messages []agent.BoardMessage, reminderRoots map[string]struct{}) roomStatusBacklog {
	backlog := roomStatusBacklog{}
	for _, participant := range room.Participants {
		if strings.HasPrefix(strings.TrimSpace(participant), "actor:system:room:") {
			continue
		}
		entries := buildRoomStatusEntries(participant, messages, reminderRoots)
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

func buildRoomStatusActionRequired(room agent.RoomSummary, messages []agent.BoardMessage, tasks []taskstore.Task, backlog roomStatusBacklog, taskPulse roomTaskPulseSummary, filters map[string]struct{}, staleAfter time.Duration, now time.Time, verbose bool, reminderRoots map[string]struct{}) roomStatusActionRequired {
	summary := roomStatusActionRequired{
		Filter:                  sortedRoomStatusFilters(filters),
		ParticipantsWithPending: roomStatusFilteredCount(filters, "ack", "reply", "interview", backlog.ParticipantsWithPending),
		PendingAcks:             roomStatusFilteredCount(filters, "ack", "", "", backlog.PendingAcks),
		PendingReplies:          roomStatusFilteredCount(filters, "reply", "interview", "", backlog.PendingReplies),
		AssignedUnclaimed:       roomStatusFilteredCount(filters, "assigned", "", "", taskPulse.AssignedUnclaimed),
		BlockedTasks:            roomStatusFilteredCount(filters, "blocked", "", "", taskPulse.Blocked),
		StaleTasks:              roomStatusFilteredCount(filters, "stale", "", "", taskPulse.Stale),
		TopEntries:              filterRoomStatusEntries(backlog.LatestByParticipant, filters),
		TopTasks:                buildRoomStatusTaskEntries(tasks, filters, now, staleAfter),
	}
	if !verbose {
		return summary
	}
	summary.VerboseTopEntries = filterRoomStatusVerboseEntries(buildRoomStatusVerboseEntries(room, messages, reminderRoots), filters)
	return summary
}

func buildRoomStatusVerboseEntries(room agent.RoomSummary, messages []agent.BoardMessage, reminderRoots map[string]struct{}) []roomInboxEntry {
	out := make([]roomInboxEntry, 0, len(room.Participants))
	for _, participant := range room.Participants {
		if strings.HasPrefix(strings.TrimSpace(participant), "actor:system:room:") {
			continue
		}
		entries := buildRoomStatusEntries(participant, messages, reminderRoots)
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
		case "INTERVIEW":
			if _, ok := filters["interview"]; ok {
				return true
			}
		}
	}
	return false
}

func normalizeRoomStatusFilters(values []string) (map[string]struct{}, error) {
	allowed := map[string]struct{}{
		"all":       {},
		"ack":       {},
		"reply":     {},
		"interview": {},
		"assigned":  {},
		"blocked":   {},
		"stale":     {},
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

func roomMessageHasInterviewPendingWork(msg agent.BoardMessage) bool {
	switch msg.Kind {
	case agent.BoardMessageKindInterviewQuestion,
		agent.BoardMessageKindInterviewAnswer:
		return true
	case agent.BoardMessageKindInterviewVerify:
		return msg.ReplyExpected && msg.Status != agent.BoardMessageStatusAcked
	default:
		return false
	}
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

func roomStatusFilteredCount(filters map[string]struct{}, primary string, secondary string, tertiary string, value int) int {
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
	if tertiary != "" {
		if _, ok := filters[tertiary]; ok {
			return value
		}
	}
	return 0
}

func buildRoomStatusEntries(actorID string, messages []agent.BoardMessage, reminderRoots map[string]struct{}) []roomInboxEntry {
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
		if _, suppressed := reminderRoots[key]; suppressed {
			continue
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

func loadRoomReminderRoots(ctx context.Context, workspaceID, roomID string, includeInactive bool) (map[string]struct{}, error) {
	cfg, err := loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	coordStore, err := coordination.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, err
	}
	defer coordStore.Close()
	reminders, err := coordStore.ListRoomReminders(ctx, workspaceID, strings.TrimSpace(roomID), includeInactive)
	if err != nil {
		return nil, err
	}
	return roomReminderRootSet(reminders), nil
}

func roomReminderRootSet(reminders []coordination.RoomReminder) map[string]struct{} {
	out := make(map[string]struct{}, len(reminders))
	for _, reminder := range reminders {
		id := strings.TrimSpace(reminder.RootMessageID)
		if id == "" {
			continue
		}
		out[id] = struct{}{}
	}
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
	switch msg.Kind {
	case agent.BoardMessageKindInterviewQuestion:
		return strings.TrimSpace(msg.ID)
	case agent.BoardMessageKindInterviewAnswer:
		if strings.TrimSpace(msg.RelatedMessageID) != "" {
			return strings.TrimSpace(msg.RelatedMessageID)
		}
	case agent.BoardMessageKindInterviewVerify:
		if strings.TrimSpace(msg.RelatedMessageID) != "" {
			return strings.TrimSpace(msg.RelatedMessageID)
		}
	}
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

func findRoomMemberForMuxSubmit(summary agent.RoomSummary, target string) (agent.RoomMember, bool) {
	target = strings.TrimSpace(target)
	if target == "" {
		return agent.RoomMember{}, false
	}
	for _, m := range summary.Members {
		m = normalizeRoomMember(m)
		if sameRoomParticipant(m.ActorID, target) {
			return m, true
		}
	}
	return agent.RoomMember{}, false
}

// muxSubmitForRoomMember runs mux submit against the pane binding stored on a room member.
func muxSubmitForRoomMember(ctx context.Context, member agent.RoomMember, submitMode string) (any, string, error) {
	member = normalizeRoomMember(member)
	switch roomMemberRelayBackend(member) {
	case "zellij":
		session, _, ok := resolveRoomMemberZellijTarget(member)
		if !ok || strings.TrimSpace(session) == "" {
			return nil, "", fmt.Errorf("member %q has no resolvable zellij session", member.ActorID)
		}
		pane := strings.TrimSpace(member.PaneID)
		if pane == "" {
			if _, p, ok := parseZellijParticipantID(member.ActorID); ok {
				pane = p
			}
		}
		pane = normalizeZellijPaneID(pane)
		res, err := zellijbridge.New().Submit(ctx, session, zellijbridge.SubmitOptions{Mode: submitMode, PaneID: pane})
		if err != nil {
			return nil, "", err
		}
		return res, "zellij", nil
	default:
		target := roomMemberTmuxTarget(member)
		if strings.TrimSpace(target) == "" {
			return nil, "", fmt.Errorf("member %q has no tmux pane target", member.ActorID)
		}
		res, err := tmuxbridge.New().Submit(ctx, target, tmuxbridge.SubmitOptions{Mode: submitMode})
		if err != nil {
			return nil, "", err
		}
		return res, "tmux", nil
	}
}

// relayRecipientMatchesMember reports whether a direct message's recipient should be relayed to this
// member's mux pane. It must stay consistent with send-time validation (roomHasParticipant): timeline
// traffic may use stable labels (e.g. human-a) while membership rows store tmux participant ids for
// the same pane, so sameRoomParticipant(actor_id, recipient) alone is not enough.
func relayRecipientMatchesMember(room agent.RoomSummary, member agent.RoomMember, recipient string) bool {
	recipient = strings.TrimSpace(recipient)
	if recipient == "" || recipient == agent.BroadcastRecipient {
		return true
	}
	member = normalizeRoomMember(member)
	actorID := strings.TrimSpace(member.ActorID)
	if actorID == "" {
		return false
	}
	// When both a legacy ActorID "human-a" row and a coordinator row with a real pane exist, only
	// relay to the coordinator pane — otherwise we fan out twice and the label target often fails
	// (failed_count 1) while the timeline still shows status ok.
	if recipient == "human-a" && roomPreferCoordinatorRelayForHumanA(room) {
		return strings.EqualFold(strings.TrimSpace(member.Role), "coordinator") && strings.TrimSpace(member.PaneID) != ""
	}
	if sameRoomParticipant(actorID, recipient) {
		return true
	}
	if pane := strings.TrimSpace(member.PaneID); pane != "" && recipient == pane {
		return true
	}
	if ref, ok := tmuxbridge.ParseParticipantID(actorID); ok && recipient == ref.Target {
		return true
	}
	// Common handoff: direct sends use "human-a" while the coordinator row stores tmux:session:pane.
	if recipient == "human-a" && strings.EqualFold(strings.TrimSpace(member.Role), "coordinator") {
		if roomHasMemberWithExactActorID(room, "human-a") {
			return false
		}
		return true
	}
	return false
}

func roomHasMemberWithExactActorID(room agent.RoomSummary, actorID string) bool {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return false
	}
	for _, m := range room.Members {
		if strings.TrimSpace(m.ActorID) == actorID {
			return true
		}
	}
	return false
}

func roomPreferCoordinatorRelayForHumanA(room agent.RoomSummary) bool {
	var hasHumanA bool
	var coordHasPane bool
	for _, m := range room.Members {
		m = normalizeRoomMember(m)
		if strings.TrimSpace(m.ActorID) == "human-a" {
			hasHumanA = true
		}
		if strings.EqualFold(strings.TrimSpace(m.Role), "coordinator") && strings.TrimSpace(m.PaneID) != "" {
			coordHasPane = true
		}
	}
	return hasHumanA && coordHasPane
}

// collectRoomRelayMembers returns room members that should receive this relay (same routing as targets).
func collectRoomRelayMembers(room agent.RoomSummary, msg agent.BoardMessage) ([]agent.RoomMember, []string) {
	members := make([]agent.RoomMember, 0, len(room.Members))
	skipped := make([]string, 0, len(room.Members))
	recipient := normalizeRoomRecipient(msg.Recipient)
	for _, member := range room.Members {
		member = normalizeRoomMember(member)
		actorID := strings.TrimSpace(member.ActorID)
		if actorID == "" {
			continue
		}
		if sameRoomParticipant(actorID, strings.TrimSpace(msg.Sender)) {
			skipped = append(skipped, actorID)
			continue
		}
		if recipient != agent.BroadcastRecipient && !relayRecipientMatchesMember(room, member, recipient) {
			skipped = append(skipped, actorID)
			continue
		}
		members = append(members, member)
	}
	return members, skipped
}

func collectRoomRelayTargets(room agent.RoomSummary, msg agent.BoardMessage) ([]string, []string) {
	members, skipped := collectRoomRelayMembers(room, msg)
	targets := make([]string, 0, len(members))
	for _, member := range members {
		targets = append(targets, roomMemberTmuxTarget(member))
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
		if recipient != agent.BroadcastRecipient && !relayRecipientMatchesMember(room, member, recipient) {
			skipped = append(skipped, target)
			continue
		}
		if roomMemberRelayBackend(member) != "zellij" {
			tmuxTargets = append(tmuxTargets, roomMemberTmuxTarget(member))
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

func roomMemberTmuxTarget(member agent.RoomMember) string {
	member = normalizeRoomMember(member)
	if paneID := strings.TrimSpace(member.PaneID); paneID != "" {
		return paneID
	}
	return strings.TrimSpace(member.ActorID)
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
