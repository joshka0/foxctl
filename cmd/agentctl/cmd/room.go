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
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
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
		newRoomInboxCommand(),
		newRoomSendCommand(),
		newRoomAckCommand(),
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
