package cmd

import (
	"context"
	"errors"
	"fmt"
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
		newRoomListCommand(),
		newRoomShowCommand(),
		newRoomStatusCommand(),
		newRoomInboxCommand(),
		newRoomSendCommand(),
		newRoomAckCommand(),
		newRoomResolveCommand(),
		newRoomJoinCommand(),
		newRoomLeaveCommand(),
		newRoomTaskCommand(),
		newRoomSubscribeCommand(),
		newRoomRelayCommand(),
		newRoomLoopCommand(),
	)
	return cmd
}

func newRoomCreateCommand() *cobra.Command {
	var (
		workspace   string
		title       string
		description string
		members     []string
	)
	cmd := &cobra.Command{
		Use:   "create <room-id>",
		Short: "Create or update a durable room",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomCreate(cmd, workspace, args[0], title, description, members)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&title, "title", "", "Room title")
	cmd.Flags().StringVar(&description, "description", "", "Room description")
	cmd.Flags().StringArrayVar(&members, "member", nil, "Room member in actor or actor=role form (repeatable)")
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
		autoCreate    bool
	)
	cmd := &cobra.Command{
		Use:   "send <room-id> <text>",
		Short: "Append a durable message to a room timeline",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomSend(cmd, workspace, args[0], sender, recipient, subject, strings.Join(args[1:], " "), kind, taskID, priority, ackRequired, replyExpected, autoCreate)
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
	)
	cmd := &cobra.Command{
		Use:   "resolve <room-id> <message-id>...",
		Short: "Coordinator-only cleanup for stale room messages",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomResolve(cmd, workspace, args[0], actorID, mode, args[1:])
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&actorID, "actor", "", "Coordinator actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&mode, "mode", "acked", "Resolution mode (acked|read)")
	return cmd
}

func newRoomJoinCommand() *cobra.Command {
	var (
		workspace string
		role      string
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
			return runRoomJoin(cmd, workspace, args[0], actorID, role, create, current)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&role, "role", "", "Optional member role")
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
	cmd.Flags().StringVar(&backend, "backend", "tmux", "Terminal backend (tmux|zellij)")
	cmd.Flags().StringVar(&session, "session", "", "Zellij session name (defaults to ZELLIJ_SESSION_NAME when inside zellij)")
	cmd.Flags().StringVar(&plugin, "plugin-path", "", "Path to the zellij room relay plugin wasm")
	cmd.Flags().DurationVar(&poll, "poll", 2*time.Second, "Polling interval")
	cmd.Flags().IntVar(&history, "history", 0, "Number of most recent messages to replay into members before following")
	return cmd
}

func runRoomCreate(cmd *cobra.Command, workspace, roomID, title, description string, rawMembers []string) error {
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

	members, err := parseRoomMembers(rawMembers)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.create", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Members must use actor or actor=role form.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if identity, err := resolveRoomSender(cmd.Context(), ""); err == nil {
		members = ensureRoomCoordinator(members, identity.Sender)
	}

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
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.create", map[string]any{
		"room": room,
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
		if strings.TrimSpace(out[i].ActorID) != actorID {
			continue
		}
		if strings.TrimSpace(out[i].Role) == "" {
			out[i].Role = "coordinator"
		}
		return out
	}
	return append(out, agent.RoomMember{ActorID: actorID, Role: "coordinator"})
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

	rooms, err := store.ListRooms(cmd.Context(), absWorkspace, strings.TrimSpace(actorID), limit)
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

func runRoomSend(cmd *cobra.Command, workspace, roomID, sender, recipient, subject, body, kind, taskID string, priority int, ackRequired, replyExpected, autoCreate bool) error {
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
	recipient = normalizeRoomRecipient(recipient)
	if replyExpected && recipient == agent.BroadcastRecipient {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeEARG, "reply_expected requires a direct recipient", map[string]any{
			"hint": "Pass --to <participant-id> for direct requests. Broadcast room messages should not expect a response.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if autoCreate {
		if _, err := store.EnsureRoom(cmd.Context(), absWorkspace, roomID, roomID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.send", protocol.ErrorCodeERuntime, err.Error(), map[string]any{
				"hint": "Create the room first or leave --auto-create enabled.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
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

func runRoomResolve(cmd *cobra.Command, workspace, roomID, actorID, mode string, messageIDs []string) error {
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
	trimmedIDs := make([]string, 0, len(messageIDs))
	for _, id := range messageIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		trimmedIDs = append(trimmedIDs, id)
	}
	if len(trimmedIDs) == 0 {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.resolve", protocol.ErrorCodeEARG, "at least one non-empty message id is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
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

func runRoomJoin(cmd *cobra.Command, workspace, roomID, actorID, role string, create, current bool) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	if current || strings.TrimSpace(actorID) == "" {
		identity, resolveErr := resolveRoomSender(cmd.Context(), actorID)
		if resolveErr != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.join", protocol.ErrorCodeEARG, resolveErr.Error(), map[string]any{
				"hint": "Pass an explicit actor id, or run inside tmux/zellij with --current so agentctl can derive the participant id.",
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		actorID = identity.Sender
	}
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
	member := agent.RoomMember{ActorID: strings.TrimSpace(actorID), Role: strings.TrimSpace(role)}
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
	out := make([]agent.RoomMember, 0, len(values))
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		member := agent.RoomMember{}
		if idx := strings.LastIndex(raw, "="); idx >= 0 {
			member.ActorID = strings.TrimSpace(raw[:idx])
			member.Role = strings.TrimSpace(raw[idx+1:])
		} else {
			member.ActorID = raw
		}
		if member.ActorID == "" {
			return nil, fmt.Errorf("member actor id is required")
		}
		out = append(out, member)
	}
	return out, nil
}

func mergeRoomMembers(existing []agent.RoomMember, additions ...agent.RoomMember) []agent.RoomMember {
	out := make([]agent.RoomMember, 0, len(existing)+len(additions))
	index := make(map[string]int, len(existing)+len(additions))
	for _, member := range existing {
		member.ActorID = strings.TrimSpace(member.ActorID)
		member.Role = strings.TrimSpace(member.Role)
		if member.ActorID == "" {
			continue
		}
		index[member.ActorID] = len(out)
		out = append(out, member)
	}
	for _, member := range additions {
		member.ActorID = strings.TrimSpace(member.ActorID)
		member.Role = strings.TrimSpace(member.Role)
		if member.ActorID == "" {
			continue
		}
		if pos, ok := index[member.ActorID]; ok {
			if member.Role != "" {
				out[pos].Role = member.Role
			}
			continue
		}
		index[member.ActorID] = len(out)
		out = append(out, member)
	}
	return out
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

func relayRoomMessageTmux(ctx context.Context, client *tmuxbridge.Client, room agent.RoomSummary, msg agent.BoardMessage) roomRelayResult {
	result := roomRelayResult{Backend: "tmux"}
	targets, skipped := collectRoomRelayTargets(room, msg)
	result.SkippedMembers = append(result.SkippedMembers, skipped...)
	for _, target := range targets {
		_, err := client.DeliverText(ctx, target, formatRoomRelayContent(room, msg))
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
	if msg.Status == agent.BoardMessageStatusAcked {
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
