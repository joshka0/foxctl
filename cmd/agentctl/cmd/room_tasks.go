package cmd

import (
	"context"
	"errors"
	"fmt"
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

const roomTaskScanLimit = 1000

func newRoomTaskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage room-scoped tasks backed by the task store",
	}
	cmd.AddCommand(
		newRoomTaskAddCommand(),
		newRoomTaskListCommand(),
		newRoomTaskAssignCommand(),
		newRoomTaskReassignCommand(),
		newRoomTaskClaimCommand(),
		newRoomTaskTouchCommand(),
		newRoomTaskBlockCommand(),
		newRoomTaskReclaimCommand(),
		newRoomTaskUnblockCommand(),
		newRoomTaskAbandonCommand(),
		newRoomTaskCompleteCommand(),
	)
	return cmd
}

func newRoomTaskAssignCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		taskID    string
		recipient string
		notes     string
	)
	cmd := &cobra.Command{
		Use:   "assign <room-id>",
		Short: "Assign a room task to a participant without claiming it on their behalf",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskAssign(cmd, workspace, args[0], sender, taskID, recipient, notes)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&taskID, "id", "", "Task id to assign")
	cmd.Flags().StringVar(&recipient, "to", "", "Assigned participant id")
	cmd.Flags().StringVar(&notes, "notes", "", "Optional assignment note")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newRoomTaskReassignCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		taskID    string
		recipient string
		reason    string
	)
	cmd := &cobra.Command{
		Use:   "reassign <room-id>",
		Short: "Coordinator-only reassignment that moves a task back to pending for a new assignee",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskReassign(cmd, workspace, args[0], sender, taskID, recipient, reason)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&taskID, "id", "", "Task id to reassign")
	cmd.Flags().StringVar(&recipient, "to", "", "New assignee participant id")
	cmd.Flags().StringVar(&reason, "reason", "", "Reassignment reason")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("to")
	return cmd
}

func newRoomTaskAddCommand() *cobra.Command {
	var (
		workspace  string
		sender     string
		title      string
		desc       string
		scopePath  string
		parentID   string
		dependsOn  []string
		autoCreate bool
	)
	cmd := &cobra.Command{
		Use:   "add <room-id>",
		Short: "Create a task associated with a room and announce it to participants",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskAdd(cmd, workspace, args[0], sender, title, desc, scopePath, parentID, dependsOn, autoCreate)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&title, "title", "", "Task title")
	cmd.Flags().StringVar(&desc, "description", "", "Task description")
	cmd.Flags().StringVar(&scopePath, "scope", "", "Scope path for the task")
	cmd.Flags().StringVar(&parentID, "parent", "", "Parent task id")
	cmd.Flags().StringSliceVar(&dependsOn, "depends-on", nil, "Dependency task ids")
	cmd.Flags().BoolVar(&autoCreate, "create-room", true, "Create the room if it does not exist")
	_ = cmd.MarkFlagRequired("title")
	return cmd
}

func newRoomTaskListCommand() *cobra.Command {
	var (
		workspace string
		status    string
	)
	cmd := &cobra.Command{
		Use:   "list <room-id>",
		Short: "List tasks associated with a room",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskList(cmd, workspace, args[0], status)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&status, "status", "", "Filter by task status")
	return cmd
}

func newRoomTaskCompleteCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		taskID    string
		notes     string
		gotchas   string
	)
	cmd := &cobra.Command{
		Use:   "complete <room-id>",
		Short: "Complete a room-associated task and broadcast it into the room",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskComplete(cmd, workspace, args[0], sender, taskID, notes, gotchas)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&taskID, "id", "", "Task id to complete")
	cmd.Flags().StringVar(&notes, "notes", "", "Completion notes")
	cmd.Flags().StringVar(&gotchas, "gotchas", "", "Completion gotchas")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newRoomTaskClaimCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		taskID    string
	)
	cmd := &cobra.Command{
		Use:   "claim <room-id>",
		Short: "Claim a room-associated task for one participant",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskClaim(cmd, workspace, args[0], sender, taskID)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&taskID, "id", "", "Task id to claim")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newRoomTaskBlockCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		taskID    string
		reason    string
	)
	cmd := &cobra.Command{
		Use:   "block <room-id>",
		Short: "Mark a claimed room task as blocked",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskBlock(cmd, workspace, args[0], sender, taskID, reason)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&taskID, "id", "", "Task id to block")
	cmd.Flags().StringVar(&reason, "reason", "", "Block reason")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newRoomTaskTouchCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		taskID    string
	)
	cmd := &cobra.Command{
		Use:   "touch <room-id>",
		Short: "Refresh the heartbeat on a claimed or blocked room task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskTouch(cmd, workspace, args[0], sender, taskID)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&taskID, "id", "", "Task id to refresh")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newRoomTaskUnblockCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		taskID    string
	)
	cmd := &cobra.Command{
		Use:   "unblock <room-id>",
		Short: "Move a blocked task back to claimed/in-progress",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskUnblock(cmd, workspace, args[0], sender, taskID)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&taskID, "id", "", "Task id to unblock")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newRoomTaskReclaimCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		taskID    string
		reason    string
	)
	cmd := &cobra.Command{
		Use:   "reclaim <room-id>",
		Short: "Coordinator-only force reclaim that returns a task to pending",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskReclaim(cmd, workspace, args[0], sender, taskID, reason)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&taskID, "id", "", "Task id to reclaim")
	cmd.Flags().StringVar(&reason, "reason", "", "Reclaim reason")
	_ = cmd.MarkFlagRequired("id")
	_ = cmd.MarkFlagRequired("reason")
	return cmd
}

func newRoomTaskAbandonCommand() *cobra.Command {
	var (
		workspace string
		sender    string
		taskID    string
		reason    string
	)
	cmd := &cobra.Command{
		Use:   "abandon <room-id>",
		Short: "Release a claimed or blocked task back to pending",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskAbandon(cmd, workspace, args[0], sender, taskID, reason)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&taskID, "id", "", "Task id to abandon")
	cmd.Flags().StringVar(&reason, "reason", "", "Optional abandon reason")
	_ = cmd.MarkFlagRequired("id")
	return cmd
}

func newRoomLoopCommand() *cobra.Command {
	var (
		workspace          string
		backend            string
		session            string
		plugin             string
		poll               time.Duration
		taskPoll           time.Duration
		history            int
		pulse              time.Duration
		replyStale         time.Duration
		taskHeartbeatStale time.Duration
	)
	cmd := &cobra.Command{
		Use:   "loop <room-id>",
		Short: "Run the room coordination loop with relay and task completion broadcasts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomLoop(cmd, workspace, args[0], roomRelayOptions{
				Backend:          backend,
				ZellijSession:    session,
				ZellijPluginPath: plugin,
			}, poll, taskPoll, history, roomPulseConfig{
				Interval:        pulse,
				ReplyStaleAfter: replyStale,
				TaskStaleAfter:  taskHeartbeatStale,
			})
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&backend, "backend", "auto", "Terminal backend (auto|tmux|zellij)")
	cmd.Flags().StringVar(&session, "session", "", "Zellij session name (defaults to ZELLIJ_SESSION_NAME when inside zellij)")
	cmd.Flags().StringVar(&plugin, "plugin-path", "", "Path to the zellij room relay plugin wasm")
	cmd.Flags().DurationVar(&poll, "poll", 2*time.Second, "Room message polling interval")
	cmd.Flags().DurationVar(&taskPoll, "task-poll", 3*time.Second, "Room task polling interval")
	cmd.Flags().IntVar(&history, "history", 0, "Number of most recent room messages to replay into participants on startup")
	cmd.Flags().DurationVar(&pulse, "pulse", 30*time.Second, "Reminder pulse interval (0 disables reminders)")
	cmd.Flags().DurationVar(&replyStale, "reply-stale", 2*time.Minute, "Reminder threshold for direct ack/reply requests")
	cmd.Flags().DurationVar(&taskHeartbeatStale, "task-stale", 5*time.Minute, "Reminder threshold for claimed or blocked tasks without task heartbeat movement")
	return cmd
}

func runRoomTaskAdd(cmd *cobra.Command, workspace, roomID, sender, title, desc, scopePath, parentID string, dependsOn []string, autoCreate bool) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.add", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	taskWorkspaceID := ws.CanonicalID(absWorkspace)
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()
	if autoCreate {
		if _, err := boardStore.EnsureRoom(cmd.Context(), absWorkspace, roomID, roomID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()

	task, err := taskStore.Add(cmd.Context(), taskstore.Task{
		WorkspaceID: taskWorkspaceID,
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(desc),
		ScopePath:   strings.TrimSpace(scopePath),
		ParentID:    strings.TrimSpace(parentID),
		DependsOn:   append([]string(nil), dependsOn...),
		Status:      taskstore.StatusPending,
	})
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	msg := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:      identity.Sender,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Priority:    agent.DefaultPriority,
		Subject:     fmt.Sprintf("Task added: %s", task.Title),
		Body:        formatRoomTaskAddedBody(task),
	}
	if err := boardStore.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.task.add", map[string]any{
		"room_id":         strings.TrimSpace(roomID),
		"task":            task,
		"message_id":      msg.ID,
		"sender_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomTaskList(cmd *cobra.Command, workspace, roomID, status string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	taskWorkspaceID := ws.CanonicalID(absWorkspace)
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()

	summary, messages, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, strings.TrimSpace(roomID), "", roomTaskScanLimit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.list", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()

	roomTasks, err := listRoomTasks(cmd.Context(), taskStore, taskWorkspaceID, messages, strings.TrimSpace(status))
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.task.list", map[string]any{
		"room":  summary,
		"tasks": roomTasks,
		"count": len(roomTasks),
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomTaskComplete(cmd *cobra.Command, workspace, roomID, sender, taskID, notes, gotchas string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	taskWorkspaceID := ws.CanonicalID(absWorkspace)
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()

	task, err := taskStore.Get(cmd.Context(), strings.TrimSpace(taskID))
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeENotFound, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.WorkspaceID != taskWorkspaceID {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeEARG, "task does not belong to this workspace", map[string]any{
			"hint": "Use the same workspace used when the task was created.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusPending && strings.TrimSpace(task.OwnerActorID) == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeEARG, "task must be claimed before completion", map[string]any{
			"hint": "Run 'agentctl room task claim <room-id> --id <task-id>' before completing the task.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if strings.TrimSpace(task.OwnerActorID) != "" && task.OwnerActorID != identity.Sender {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeEARG, "only the current owner can complete this task", map[string]any{
			"owner_actor_id": task.OwnerActorID,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusBlocked {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeEARG, "blocked tasks must be unblocked before completion", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	now := time.Now().UTC()
	task.Status = taskstore.StatusCompleted
	task.CompletedAt = &now
	task.OwnerActorID = ""
	task.HeartbeatAt = &now
	task.BlockedReason = ""
	task.BlockedAt = nil
	task.Notes = strings.TrimSpace(notes)
	task.Gotchas = strings.TrimSpace(gotchas)
	task, err = taskStore.Update(cmd.Context(), task)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()

	msg := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:      identity.Sender,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Priority:    agent.DefaultPriority,
		Subject:     fmt.Sprintf("Task completed: %s", task.Title),
		Body:        formatRoomTaskCompletionBody(task),
	}
	if err := boardStore.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.task.complete", map[string]any{
		"room_id":         strings.TrimSpace(roomID),
		"task":            task,
		"message_id":      msg.ID,
		"sender_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomTaskAssign(cmd *cobra.Command, workspace, roomID, sender, taskID, recipient, notes string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	recipient = normalizeRoomRecipient(recipient)
	if recipient == agent.BroadcastRecipient {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeEARG, "assignment requires a direct participant recipient", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	taskWorkspaceID := ws.CanonicalID(absWorkspace)
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()
	summary, err := boardStore.GetRoom(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), "")
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeEARG, "only room coordinators can assign tasks", map[string]any{
			"sender": identity.Sender,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !roomHasParticipant(summary, recipient) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeEARG, "assignee is not a room participant", map[string]any{
			"recipient": recipient,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()
	task, err := taskStore.Get(cmd.Context(), strings.TrimSpace(taskID))
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeENotFound, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.WorkspaceID != taskWorkspaceID {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeEARG, "task does not belong to this workspace", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusCompleted {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeEARG, "completed tasks cannot be assigned", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	now := time.Now().UTC()
	task.AssignedActorID = recipient
	task.AssignedAt = &now
	if strings.TrimSpace(notes) != "" {
		task.Notes = strings.TrimSpace(notes)
	}
	task, err = taskStore.Update(cmd.Context(), task)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	subject := fmt.Sprintf("Task assigned: %s", task.Title)
	bodyLines := []string{
		fmt.Sprintf("Task ID: %s", task.ID),
		fmt.Sprintf("Assigned to: %s", recipient),
		fmt.Sprintf("Assigned by: %s", identity.Sender),
		"Please claim this task before starting work.",
	}
	if strings.TrimSpace(task.Description) != "" {
		bodyLines = append(bodyLines, task.Description)
	}
	if strings.TrimSpace(notes) != "" {
		bodyLines = append(bodyLines, "Notes: "+strings.TrimSpace(notes))
	}
	broadcast := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:      identity.Sender,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Priority:    agent.DefaultPriority,
		Subject:     subject,
		Body:        strings.Join(bodyLines, "\n"),
	}
	if err := boardStore.SendMessage(cmd.Context(), broadcast); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	direct := &agent.BoardMessage{
		WorkspaceID:   absWorkspace,
		TaskID:        task.ID,
		Stream:        agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:        identity.Sender,
		Recipient:     recipient,
		Kind:          agent.BoardMessageKindInstruction,
		Priority:      agent.DefaultPriority,
		AckRequired:   true,
		ReplyExpected: true,
		Subject:       subject,
		Body:          strings.Join(bodyLines, "\n"),
	}
	if err := boardStore.SendMessage(cmd.Context(), direct); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.task.assign", map[string]any{
		"room_id":         strings.TrimSpace(roomID),
		"task":            task,
		"assignee":        recipient,
		"message_id":      direct.ID,
		"sender_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomTaskReassign(cmd *cobra.Command, workspace, roomID, sender, taskID, recipient, reason string) error {
	absWorkspace, identity, summary, task, boardStore, taskStore, err := loadCoordinatorTaskContext(cmd, workspace, roomID, sender, taskID)
	if err != nil {
		return err
	}
	defer boardStore.Close()
	defer taskStore.Close()

	recipient = normalizeRoomRecipient(recipient)
	if recipient == agent.BroadcastRecipient {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.reassign", protocol.ErrorCodeEARG, "reassign requires a direct participant recipient", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !roomHasParticipant(summary, recipient) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.reassign", protocol.ErrorCodeEARG, "assignee is not a room participant", map[string]any{
			"recipient": recipient,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusCompleted {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.reassign", protocol.ErrorCodeEARG, "completed tasks cannot be reassigned", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	previousOwner := strings.TrimSpace(task.OwnerActorID)
	previousAssignee := strings.TrimSpace(task.AssignedActorID)
	now := time.Now().UTC()
	task.Status = taskstore.StatusPending
	task.AssignedActorID = recipient
	task.AssignedAt = &now
	task.OwnerActorID = ""
	task.ClaimedAt = nil
	task.HeartbeatAt = nil
	task.BlockedReason = ""
	task.BlockedAt = nil
	task, err = taskStore.Update(cmd.Context(), task)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.reassign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	subject := fmt.Sprintf("Task reassigned: %s", task.Title)
	lines := []string{
		fmt.Sprintf("Task ID: %s", task.ID),
		fmt.Sprintf("Reassigned by: %s", identity.Sender),
		fmt.Sprintf("Assigned to: %s", recipient),
		fmt.Sprintf("Previous owner: %s", fallbackRoomValue(previousOwner)),
		fmt.Sprintf("Previous assignee: %s", fallbackRoomValue(previousAssignee)),
		"Status reset to pending. New assignee must claim the task explicitly.",
	}
	if strings.TrimSpace(reason) != "" {
		lines = append(lines, "Reason: "+strings.TrimSpace(reason))
	}
	if err := sendRoomTaskCoordinatorMessages(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), boardStore, identity.Sender, recipient, task, subject, strings.Join(lines, "\n")); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.reassign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.task.reassign", map[string]any{
		"room_id":         strings.TrimSpace(roomID),
		"task":            task,
		"assignee":        recipient,
		"sender_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomTaskReclaim(cmd *cobra.Command, workspace, roomID, sender, taskID, reason string) error {
	absWorkspace, identity, _, task, boardStore, taskStore, err := loadCoordinatorTaskContext(cmd, workspace, roomID, sender, taskID)
	if err != nil {
		return err
	}
	defer boardStore.Close()
	defer taskStore.Close()

	previousOwner := strings.TrimSpace(task.OwnerActorID)
	if previousOwner == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.reclaim", protocol.ErrorCodeEARG, "task has no current owner to reclaim", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusCompleted {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.reclaim", protocol.ErrorCodeEARG, "completed tasks cannot be reclaimed", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	task.Status = taskstore.StatusPending
	task.AssignedActorID = ""
	task.AssignedAt = nil
	task.OwnerActorID = ""
	task.ClaimedAt = nil
	task.HeartbeatAt = nil
	task.BlockedReason = ""
	task.BlockedAt = nil
	task, err = taskStore.Update(cmd.Context(), task)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.reclaim", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	subject := fmt.Sprintf("Task force-reclaimed: %s", task.Title)
	body := strings.Join([]string{
		fmt.Sprintf("Task ID: %s", task.ID),
		fmt.Sprintf("Previous owner: %s", previousOwner),
		fmt.Sprintf("Reclaimed by: %s", identity.Sender),
		"Status reset to pending.",
		"Reason: " + strings.TrimSpace(reason),
	}, "\n")
	msg := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:      identity.Sender,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Priority:    agent.DefaultPriority,
		Subject:     subject,
		Body:        body,
	}
	if err := boardStore.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.reclaim", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.task.reclaim", map[string]any{
		"room_id":         strings.TrimSpace(roomID),
		"task":            task,
		"sender_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomTaskClaim(cmd *cobra.Command, workspace, roomID, sender, taskID string) error {
	return runRoomTaskTransition(cmd, workspace, roomID, sender, taskID, "", "claim")
}

func runRoomTaskTouch(cmd *cobra.Command, workspace, roomID, sender, taskID string) error {
	return runRoomTaskTransition(cmd, workspace, roomID, sender, taskID, "", "touch")
}

func runRoomTaskBlock(cmd *cobra.Command, workspace, roomID, sender, taskID, reason string) error {
	return runRoomTaskTransition(cmd, workspace, roomID, sender, taskID, reason, "block")
}

func runRoomTaskUnblock(cmd *cobra.Command, workspace, roomID, sender, taskID string) error {
	return runRoomTaskTransition(cmd, workspace, roomID, sender, taskID, "", "unblock")
}

func runRoomTaskAbandon(cmd *cobra.Command, workspace, roomID, sender, taskID, reason string) error {
	return runRoomTaskTransition(cmd, workspace, roomID, sender, taskID, reason, "abandon")
}

func runRoomTaskTransition(cmd *cobra.Command, workspace, roomID, sender, taskID, reason, action string) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	taskWorkspaceID := ws.CanonicalID(absWorkspace)
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()

	task, err := taskStore.Get(cmd.Context(), strings.TrimSpace(taskID))
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeENotFound, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.WorkspaceID != taskWorkspaceID {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeEARG, "task does not belong to this workspace", map[string]any{
			"hint": "Use the same workspace used when the task was created.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	now := time.Now().UTC()
	switch action {
	case "claim":
		if task.Status == taskstore.StatusCompleted {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.claim", protocol.ErrorCodeEARG, "completed tasks cannot be claimed", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if assigned := strings.TrimSpace(task.AssignedActorID); assigned != "" && !sameRoomParticipant(assigned, identity.Sender) {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.claim", protocol.ErrorCodeEARG, "task is assigned to another participant", map[string]any{
				"assigned_actor_id": task.AssignedActorID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID != "" && task.OwnerActorID != identity.Sender && task.Status != taskstore.StatusPending && task.Status != taskstore.StatusCanceled {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.claim", protocol.ErrorCodeEARG, "task is already claimed by another participant", map[string]any{
				"owner_actor_id": task.OwnerActorID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		task.Status = taskstore.StatusInProgress
		if strings.TrimSpace(task.AssignedActorID) == "" {
			task.AssignedActorID = identity.Sender
			task.AssignedAt = &now
		}
		task.OwnerActorID = identity.Sender
		task.ClaimedAt = &now
		task.HeartbeatAt = &now
		task.BlockedReason = ""
		task.BlockedAt = nil
	case "touch":
		if task.OwnerActorID == "" {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.touch", protocol.ErrorCodeEARG, "task must be claimed before its heartbeat can be refreshed", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID != identity.Sender {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.touch", protocol.ErrorCodeEARG, "only the current owner can refresh this task heartbeat", map[string]any{
				"owner_actor_id": task.OwnerActorID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.Status != taskstore.StatusInProgress && task.Status != taskstore.StatusBlocked {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.touch", protocol.ErrorCodeEARG, "only in-progress or blocked tasks can be refreshed", map[string]any{
				"status": task.Status,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		task.HeartbeatAt = &now
	case "block":
		if strings.TrimSpace(reason) == "" {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.block", protocol.ErrorCodeEARG, "block reason is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID == "" {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.block", protocol.ErrorCodeEARG, "task must be claimed before it can be blocked", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID != identity.Sender {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.block", protocol.ErrorCodeEARG, "only the current owner can block this task", map[string]any{
				"owner_actor_id": task.OwnerActorID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		task.Status = taskstore.StatusBlocked
		task.BlockedReason = strings.TrimSpace(reason)
		task.BlockedAt = &now
		task.HeartbeatAt = &now
	case "unblock":
		if task.OwnerActorID == "" {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.unblock", protocol.ErrorCodeEARG, "task is not currently claimed", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID != identity.Sender {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.unblock", protocol.ErrorCodeEARG, "only the current owner can unblock this task", map[string]any{
				"owner_actor_id": task.OwnerActorID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		task.Status = taskstore.StatusInProgress
		task.BlockedReason = ""
		task.BlockedAt = nil
		task.HeartbeatAt = &now
	case "abandon":
		if task.OwnerActorID != "" && task.OwnerActorID != identity.Sender {
			return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.abandon", protocol.ErrorCodeEARG, "only the current owner can abandon this task", map[string]any{
				"owner_actor_id": task.OwnerActorID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		task.Status = taskstore.StatusPending
		task.AssignedActorID = ""
		task.AssignedAt = nil
		task.OwnerActorID = ""
		task.ClaimedAt = nil
		task.HeartbeatAt = nil
		task.BlockedReason = ""
		task.BlockedAt = nil
		if strings.TrimSpace(reason) != "" {
			task.Notes = strings.TrimSpace(reason)
		}
	default:
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeEARG, "unsupported room task action", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	task, err = taskStore.Update(cmd.Context(), task)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()

	subject, body := formatRoomTaskTransitionMessage(action, task)
	msg := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:      identity.Sender,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Priority:    agent.DefaultPriority,
		Subject:     subject,
		Body:        body,
	}
	if err := boardStore.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.task."+action, map[string]any{
		"room_id":         strings.TrimSpace(roomID),
		"task":            task,
		"message_id":      msg.ID,
		"sender_identity": identity,
	}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

type roomPulseConfig struct {
	Interval        time.Duration
	ReplyStaleAfter time.Duration
	TaskStaleAfter  time.Duration
}

func runRoomLoop(cmd *cobra.Command, workspace, roomID string, relay roomRelayOptions, poll, taskPoll time.Duration, history int, pulse roomPulseConfig) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()

	client := tmuxbridge.New()
	writer := envelope.NewWriter(cmd.OutOrStdout())
	seq := 0

	summary, messages, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	seenMessages := make(map[string]struct{}, len(messages))
	announcedStates := make(map[string]string)
	for _, msg := range messages {
		seenMessages[msg.ID] = struct{}{}
		if msg.TaskID != "" {
			if task, err := taskStore.Get(cmd.Context(), msg.TaskID); err == nil {
				announcedStates[msg.TaskID] = task.Status
			}
		}
	}
	initial := trimRoomHistory(messages, history)
	for _, msg := range initial {
		seq++
		result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
		if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
			"event":   "room_relay",
			"room_id": roomID,
			"message": msg,
			"relay":   result,
		}, absWorkspace)); err != nil {
			return fmt.Errorf("write room loop relay envelope: %w", err)
		}
	}

	taskStates, err := snapshotRoomTaskStates(cmd.Context(), taskStore, messages)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	messageTicker := time.NewTicker(normalizeRoomPoll(poll))
	defer messageTicker.Stop()
	taskTicker := time.NewTicker(normalizeRoomPoll(taskPoll))
	defer taskTicker.Stop()
	var pulseTicker *time.Ticker
	if pulse.Interval > 0 {
		pulseTicker = time.NewTicker(pulse.Interval)
		defer pulseTicker.Stop()
	}
	remindedMessages := map[string]time.Time{}
	remindedTasks := map[string]time.Time{}
	remindedCoordinators := map[string]time.Time{}

	for {
		select {
		case <-cmd.Context().Done():
			return writer.Write(protocol.OK("agentctl.room.loop", map[string]any{
				"status":  "stopped",
				"room_id": roomID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace), protocol.WithMetaMutator(func(m *envelope.Meta) {
				final := true
				m.Seq = &seq
				m.Final = &final
			})))
		case <-messageTicker.C:
			summary, current, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			for _, msg := range current {
				if _, ok := seenMessages[msg.ID]; ok {
					continue
				}
				seenMessages[msg.ID] = struct{}{}
				if msg.TaskID != "" {
					if task, err := taskStore.Get(cmd.Context(), msg.TaskID); err == nil {
						taskStates[msg.TaskID] = task.Status
						announcedStates[msg.TaskID] = task.Status
					}
				}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
					"event":   "room_relay",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop relay envelope: %w", err)
				}
			}
		case <-taskTicker.C:
			updates, err := detectRoomTaskTransitions(cmd.Context(), taskStore, taskStates, announcedStates)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			for _, update := range updates {
				msg := &agent.BoardMessage{
					WorkspaceID: absWorkspace,
					TaskID:      update.Task.ID,
					Stream:      agent.RoomStreamName(roomID),
					Sender:      roomLoopSender(roomID),
					Recipient:   agent.BroadcastRecipient,
					Kind:        agent.BoardMessageKindTaskUpdate,
					Priority:    agent.DefaultPriority,
					Subject:     update.Subject,
					Body:        update.Body,
				}
				if err := boardStore.SendMessage(cmd.Context(), msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				announcedStates[update.Task.ID] = update.Task.Status
				seq++
				if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
					"event":   "task_broadcast",
					"room_id": roomID,
					"task":    update.Task,
					"subject": update.Subject,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop task envelope: %w", err)
				}
			}
		case <-roomPulseChan(pulseTicker):
			summary, current, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			roomTasks, err := listRoomTasks(cmd.Context(), taskStore, ws.CanonicalID(absWorkspace), current, "")
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			for _, pulseMsg := range detectRoomPulseMessages(roomID, current, time.Now().UTC(), pulse, remindedMessages) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				remindedMessages[pulseMsg.Key] = msg.CreatedAt
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
					"event":   "room_pulse",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop pulse envelope: %w", err)
				}
			}
			for _, pulseMsg := range detectRoomTaskPulseMessages(absWorkspace, roomID, roomTasks, time.Now().UTC(), pulse, remindedTasks) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				remindedTasks[pulseMsg.Key] = msg.CreatedAt
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
					"event":   "task_pulse",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop task pulse envelope: %w", err)
				}
			}
			for _, pulseMsg := range detectRoomCoordinatorPulseMessages(summary, current, roomTasks, time.Now().UTC(), pulse, remindedCoordinators) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				remindedCoordinators[pulseMsg.Key] = msg.CreatedAt
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
					"event":   "coordinator_pulse",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop coordinator pulse envelope: %w", err)
				}
			}
		}
	}
}

func roomPulseChan(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

type roomTaskTransition struct {
	Task    taskstore.Task
	Subject string
	Body    string
}

type roomPulseMessage struct {
	Key     string
	Message agent.BoardMessage
}

func openRoomTaskStore(ctx context.Context) (taskstore.Store, error) {
	cfg, err := loadConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return taskstore.Open(ctx, cfg.Storage.Root)
}

func listRoomTasks(ctx context.Context, store taskstore.Store, workspaceID string, messages []agent.BoardMessage, status string) ([]taskstore.Task, error) {
	taskIDs := collectRoomTaskIDs(messages)
	if len(taskIDs) == 0 {
		return []taskstore.Task{}, nil
	}
	allTasks, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]struct{}, len(taskIDs))
	for _, id := range taskIDs {
		wanted[id] = struct{}{}
	}
	filtered := make([]taskstore.Task, 0, len(taskIDs))
	for _, task := range allTasks {
		if _, ok := wanted[task.ID]; !ok {
			continue
		}
		if status != "" && task.Status != status {
			continue
		}
		filtered = append(filtered, task)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	return filtered, nil
}

func collectRoomTaskIDs(messages []agent.BoardMessage) []string {
	seen := make(map[string]struct{}, len(messages))
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		id := strings.TrimSpace(msg.TaskID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func snapshotRoomTaskStates(ctx context.Context, store taskstore.Store, messages []agent.BoardMessage) (map[string]string, error) {
	states := make(map[string]string)
	for _, taskID := range collectRoomTaskIDs(messages) {
		task, err := store.Get(ctx, taskID)
		if err != nil {
			continue
		}
		states[taskID] = task.Status
	}
	return states, nil
}

func detectRoomTaskTransitions(ctx context.Context, store taskstore.Store, states map[string]string, announced map[string]string) ([]roomTaskTransition, error) {
	out := make([]roomTaskTransition, 0, len(states))
	for taskID, previousStatus := range states {
		task, err := store.Get(ctx, taskID)
		if err != nil {
			continue
		}
		currentStatus := strings.TrimSpace(task.Status)
		if previousStatus == "" {
			states[taskID] = currentStatus
			continue
		}
		if currentStatus == previousStatus {
			continue
		}
		if announced[taskID] == currentStatus {
			states[taskID] = currentStatus
			continue
		}
		states[taskID] = currentStatus
		out = append(out, roomTaskTransition{
			Task:    task,
			Subject: roomTaskStatusSubject(task, previousStatus),
			Body:    roomTaskStatusBody(task, previousStatus),
		})
	}
	return out, nil
}

func detectRoomPulseMessages(roomID string, messages []agent.BoardMessage, now time.Time, cfg roomPulseConfig, reminded map[string]time.Time) []roomPulseMessage {
	if cfg.ReplyStaleAfter <= 0 {
		return nil
	}
	latestBySender := latestRoomSenderActivity(messages)
	latestOutstanding := make(map[string]agent.BoardMessage)
	for _, msg := range messages {
		recipient := normalizeRoomRecipient(msg.Recipient)
		if recipient == agent.BroadcastRecipient || recipient == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(msg.Sender), roomLoopSender(roomID)) {
			continue
		}
		if !msg.AckRequired && !msg.ReplyExpected {
			continue
		}
		if msg.Status == agent.BoardMessageStatusAcked || msg.Status == agent.BoardMessageStatusRead {
			continue
		}
		if msg.ReplyExpected && !messageStillAwaitsReply(msg, latestBySender) {
			continue
		}
		if now.Sub(msg.CreatedAt) < cfg.ReplyStaleAfter {
			continue
		}
		if last, ok := reminded[msg.ID]; ok && now.Sub(last) < cfg.ReplyStaleAfter {
			continue
		}
		if existing, ok := latestOutstanding[recipient]; ok && !msg.CreatedAt.After(existing.CreatedAt) {
			continue
		}
		latestOutstanding[recipient] = msg
	}

	out := make([]roomPulseMessage, 0, len(latestOutstanding))
	for recipient, msg := range latestOutstanding {
		subject := fmt.Sprintf("Reminder: pending response for %s", deriveRoomSubject(msg.Subject))
		body := fmt.Sprintf("No room acknowledgement or reply has been observed yet for message %s.\nOriginal sender: %s\nOriginal request: %s", msg.ID, strings.TrimSpace(msg.Sender), strings.TrimSpace(msg.Subject))
		if strings.TrimSpace(msg.Body) != "" && strings.TrimSpace(msg.Body) != strings.TrimSpace(msg.Subject) {
			body += "\nOriginal body: " + strings.TrimSpace(msg.Body)
		}
		out = append(out, roomPulseMessage{
			Key: msg.ID,
			Message: agent.BoardMessage{
				WorkspaceID:      msg.WorkspaceID,
				TaskID:           msg.TaskID,
				RelatedMessageID: roomMessageChainKey(msg),
				Stream:           msg.Stream,
				Sender:           roomLoopSender(roomID),
				Recipient:        recipient,
				Kind:             agent.BoardMessageKindAlert,
				Priority:         2,
				Subject:          subject,
				Body:             body,
				CreatedAt:        now,
			},
		})
	}
	return out
}

func detectRoomTaskPulseMessages(workspace, roomID string, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]time.Time) []roomPulseMessage {
	if cfg.TaskStaleAfter <= 0 {
		return nil
	}
	out := make([]roomPulseMessage, 0)
	for _, task := range tasks {
		if task.OwnerActorID == "" {
			continue
		}
		if task.Status != taskstore.StatusInProgress && task.Status != taskstore.StatusBlocked {
			continue
		}
		reference := task.CreatedAt
		if task.HeartbeatAt != nil {
			reference = *task.HeartbeatAt
		} else if task.ClaimedAt != nil {
			reference = *task.ClaimedAt
		}
		if now.Sub(reference) < cfg.TaskStaleAfter {
			continue
		}
		if last, ok := reminded[task.ID]; ok && now.Sub(last) < cfg.TaskStaleAfter {
			continue
		}
		subject := fmt.Sprintf("Reminder: task awaiting update: %s", task.Title)
		body := fmt.Sprintf("Task %s is still %s with no recent task heartbeat.\nOwner: %s", task.ID, task.Status, task.OwnerActorID)
		if strings.TrimSpace(task.BlockedReason) != "" {
			body += "\nBlocked reason: " + strings.TrimSpace(task.BlockedReason)
		}
		out = append(out, roomPulseMessage{
			Key: task.ID,
			Message: agent.BoardMessage{
				WorkspaceID: workspace,
				TaskID:      task.ID,
				Stream:      agent.RoomStreamName(strings.TrimSpace(roomID)),
				Sender:      roomLoopSender(roomID),
				Recipient:   task.OwnerActorID,
				Kind:        agent.BoardMessageKindAlert,
				Priority:    2,
				Subject:     subject,
				Body:        body,
				CreatedAt:   now,
			},
		})
	}
	return out
}

func detectRoomCoordinatorPulseMessages(room agent.RoomSummary, messages []agent.BoardMessage, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]time.Time) []roomPulseMessage {
	if cfg.Interval <= 0 {
		return nil
	}
	coordinator := roomCoordinatorActorID(room.Members)
	if coordinator == "" {
		return nil
	}
	backlog := buildRoomStatusBacklog(room, messages)
	taskPulse := buildRoomTaskPulseSummary(tasks, now, cfg.TaskStaleAfter)
	action := buildRoomStatusActionRequired(room, messages, tasks, backlog, taskPulse, map[string]struct{}{"all": {}}, cfg.TaskStaleAfter, now, false)
	if action.ParticipantsWithPending == 0 && action.AssignedUnclaimed == 0 && action.BlockedTasks == 0 && action.StaleTasks == 0 {
		return nil
	}
	key := fmt.Sprintf("%s|%d|%d|%d|%d|%d|%d", coordinator, action.ParticipantsWithPending, action.PendingAcks, action.PendingReplies, action.AssignedUnclaimed, action.BlockedTasks, action.StaleTasks)
	if last, ok := reminded[key]; ok && now.Sub(last) < cfg.Interval {
		return nil
	}
	subject := fmt.Sprintf("Coordinator pulse: %d pending participants, %d blocked, %d stale", action.ParticipantsWithPending, action.BlockedTasks, action.StaleTasks)
	body := fmt.Sprintf("As coordinator, keep the room on track.\nPending participants: %d\nPending acks: %d\nPending replies: %d\nAssigned unclaimed: %d\nBlocked tasks: %d\nStale tasks: %d",
		action.ParticipantsWithPending, action.PendingAcks, action.PendingReplies, action.AssignedUnclaimed, action.BlockedTasks, action.StaleTasks)
	if len(action.TopEntries) > 0 {
		first := action.TopEntries[0]
		body += fmt.Sprintf("\nTop pending: %s -> %s", first.Recipient, strings.TrimSpace(first.Subject))
	}
	return []roomPulseMessage{{
		Key: key,
		Message: agent.BoardMessage{
			WorkspaceID: room.WorkspaceID,
			Stream:      room.Stream,
			Sender:      roomLoopSender(room.ID),
			Recipient:   coordinator,
			Kind:        agent.BoardMessageKindAlert,
			Priority:    2,
			Subject:     subject,
			Body:        body,
			CreatedAt:   now,
		},
	}}
}

func roomLoopSender(roomID string) string {
	return "actor:system:room:" + strings.TrimSpace(roomID)
}

func loadCoordinatorTaskContext(cmd *cobra.Command, workspace, roomID, sender, taskID string) (string, roomIdentity, agent.RoomSummary, taskstore.Task, blackboard.BoardStore, taskstore.Store, error) {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, nil, nil, err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so agentctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, nil, nil, writeErr
	}
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, nil, nil, writeErr
	}
	summary, err := boardStore.GetRoom(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), "")
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, boardStore, nil, writeErr
	}
	if !roomMemberHasRole(summary.Members, identity.Sender, "coordinator") {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task", protocol.ErrorCodeEARG, "only room coordinators can perform this action", map[string]any{
			"sender": identity.Sender,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, boardStore, nil, writeErr
	}
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, boardStore, nil, writeErr
	}
	task, err := taskStore.Get(cmd.Context(), strings.TrimSpace(taskID))
	if err != nil {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task", protocol.ErrorCodeENotFound, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, boardStore, taskStore, writeErr
	}
	if task.WorkspaceID != ws.CanonicalID(absWorkspace) {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task", protocol.ErrorCodeEARG, "task does not belong to this workspace", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, boardStore, taskStore, writeErr
	}
	return absWorkspace, identity, summary, task, boardStore, taskStore, nil
}

func sendRoomTaskCoordinatorMessages(ctx context.Context, workspace, roomID string, boardStore blackboard.BoardStore, sender, recipient string, task taskstore.Task, subject, body string) error {
	broadcast := &agent.BoardMessage{
		WorkspaceID: workspace,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:      sender,
		Recipient:   agent.BroadcastRecipient,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Priority:    agent.DefaultPriority,
		Subject:     subject,
		Body:        body,
	}
	if err := boardStore.SendMessage(ctx, broadcast); err != nil {
		return err
	}
	direct := &agent.BoardMessage{
		WorkspaceID:   workspace,
		TaskID:        task.ID,
		Stream:        agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:        sender,
		Recipient:     recipient,
		Kind:          agent.BoardMessageKindInstruction,
		Priority:      agent.DefaultPriority,
		AckRequired:   true,
		ReplyExpected: true,
		Subject:       subject,
		Body:          body,
	}
	return boardStore.SendMessage(ctx, direct)
}

func fallbackRoomValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "none"
	}
	return value
}

func formatRoomTaskAddedBody(task taskstore.Task) string {
	lines := []string{
		fmt.Sprintf("Task ID: %s", task.ID),
		fmt.Sprintf("Status: %s", task.Status),
	}
	if strings.TrimSpace(task.Description) != "" {
		lines = append(lines, task.Description)
	}
	return strings.Join(lines, "\n")
}

func formatRoomTaskCompletionBody(task taskstore.Task) string {
	lines := []string{
		fmt.Sprintf("Task ID: %s", task.ID),
		fmt.Sprintf("Status: %s", task.Status),
	}
	if strings.TrimSpace(task.Notes) != "" {
		lines = append(lines, "Notes: "+task.Notes)
	}
	if strings.TrimSpace(task.Gotchas) != "" {
		lines = append(lines, "Gotchas: "+task.Gotchas)
	}
	return strings.Join(lines, "\n")
}

func formatRoomTaskTransitionMessage(action string, task taskstore.Task) (string, string) {
	subject := fmt.Sprintf("Task %s: %s", action, task.Title)
	lines := []string{
		fmt.Sprintf("Task ID: %s", task.ID),
		fmt.Sprintf("Status: %s", task.Status),
	}
	if strings.TrimSpace(task.OwnerActorID) != "" {
		lines = append(lines, "Owner: "+task.OwnerActorID)
	}
	if strings.TrimSpace(task.BlockedReason) != "" {
		lines = append(lines, "Blocked reason: "+task.BlockedReason)
	}
	if strings.TrimSpace(task.Notes) != "" {
		lines = append(lines, "Notes: "+task.Notes)
	}
	return subject, strings.Join(lines, "\n")
}

func roomTaskStatusSubject(task taskstore.Task, previousStatus string) string {
	if task.Status == taskstore.StatusCompleted {
		return fmt.Sprintf("Task completed: %s", task.Title)
	}
	return fmt.Sprintf("Task status changed: %s (%s -> %s)", task.Title, previousStatus, task.Status)
}

func roomTaskStatusBody(task taskstore.Task, previousStatus string) string {
	lines := []string{
		fmt.Sprintf("Task ID: %s", task.ID),
		fmt.Sprintf("Previous status: %s", previousStatus),
		fmt.Sprintf("Current status: %s", task.Status),
	}
	if strings.TrimSpace(task.OwnerActorID) != "" {
		lines = append(lines, "Owner: "+task.OwnerActorID)
	}
	if strings.TrimSpace(task.BlockedReason) != "" {
		lines = append(lines, "Blocked reason: "+task.BlockedReason)
	}
	if strings.TrimSpace(task.Notes) != "" {
		lines = append(lines, "Notes: "+task.Notes)
	}
	return strings.Join(lines, "\n")
}
