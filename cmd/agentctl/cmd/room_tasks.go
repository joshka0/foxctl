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
	"github.com/jkatigb/agentctl/internal/storage/coordination"
	taskstore "github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
	"github.com/jkatigb/agentctl/internal/zellijbridge"
	"github.com/spf13/cobra"
)

const (
	roomTaskScanLimit         = 1000
	roomLoopManagedBy         = "agentctl.room.loop"
	roomLoopMinimumPulseFloor = 24 * time.Hour
	roomPulseInterruptLimit   = 2
	roomPulseBackoffCap       = 8
)

func newRoomTaskCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage room-scoped tasks backed by the task store",
	}
	cmd.PersistentFlags().Bool("no-live-relay", false,
		"Skip fan-out to tmux/zellij panes after persisting messages (use when room loop or room relay already delivers)")
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
		workspace      string
		sender         string
		taskID         string
		recipient      string
		notes          string
		provisionPane  bool
		paneAgent      string
		paneAgentMode  string
	)
	cmd := &cobra.Command{
		Use:   "assign <room-id>",
		Short: "Assign a room task to a participant (coordinator, room admin, or system admin only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRoomTaskAssign(cmd, workspace, args[0], sender, taskID, recipient, notes, roomTaskAssignOptions{
				ProvisionPane: provisionPane,
				PaneAgent:     paneAgent,
				PaneAgentMode: paneAgentMode,
			})
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&sender, "sender", "", "Sender actor or participant id (defaults to current tmux/zellij pane)")
	cmd.Flags().StringVar(&taskID, "id", "", "Task id to assign")
	cmd.Flags().StringVar(&recipient, "to", "", "Assigned participant id")
	cmd.Flags().StringVar(&notes, "notes", "", "Optional assignment note")
	cmd.Flags().BoolVar(&provisionPane, "provision-pane", false, "Auto-create a mux pane for the assignee when they lack pane bindings")
	cmd.Flags().StringVar(&paneAgent, "pane-agent", "", "Agent CLI to launch in the provisioned pane (e.g. codex, claude)")
	cmd.Flags().StringVar(&paneAgentMode, "pane-agent-mode", "auto", "Agent launch mode for the provisioned pane (interactive|auto)")
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
		Short: "Reassign a task to a new participant (coordinator, room admin, or system admin only)",
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
		workspace     string
		status        string
		taskFilter    string
		showCompleted bool
		includeAll    bool
	)
	cmd := &cobra.Command{
		Use:   "list <room-id>",
		Short: "List tasks associated with a room",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			effFilter := strings.ToLower(strings.TrimSpace(taskFilter))
			if showCompleted || includeAll {
				effFilter = "all"
			}
			statusEff, omitComp, omitCan, err := parseRoomTaskListSelection(status, effFilter)
			if err != nil {
				absWs, wsErr := resolveRoomWorkspace(workspace)
				if wsErr != nil {
					return wsErr
				}
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.list", protocol.ErrorCodeEARG, err.Error(), map[string]any{
					"hint": "Use --filter open (default, excludes completed and canceled when not using --status), all, or completed; --show-completed and --all are aliases for --filter all.",
				}, protocol.WithSource("cli"), protocol.WithWorkspace(absWs))
			}
			return runRoomTaskList(cmd, workspace, args[0], statusEff, omitComp, omitCan)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&status, "status", "", "Filter by task status")
	cmd.Flags().StringVar(&taskFilter, "filter", "open", "Task set when not using --status: open (excludes completed and canceled), all, or completed")
	cmd.Flags().BoolVar(&showCompleted, "show-completed", false, "Include completed tasks when not using --status (same as --filter all)")
	cmd.Flags().BoolVar(&includeAll, "all", false, "Include completed tasks when not using --status (same as --filter all)")
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
		Short: "Force-reclaim a task to pending (coordinator, room admin, or system admin only)",
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
				Enabled:                      pulse > 0,
				Interval:                     pulse,
				ReplyStaleAfter:              replyStale,
				TaskStaleAfter:               taskHeartbeatStale,
				MinPulseFloor:                roomLoopMinimumPulseFloor,
				InterruptAttemptLimit:        roomPulseInterruptLimit,
				ReminderBackoffCap:           roomPulseBackoffCap,
				CoordinatorPulseEnabled:      true,
				CoordinatorEscalationEnabled: true,
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
	roomID = strings.TrimSpace(roomID)
	summary, err := boardStore.GetRoom(cmd.Context(), absWorkspace, roomID, "")
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.add", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	addRecipient, rerr := roomTaskEventRecipient(summary.Members)
	if rerr != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.add", protocol.ErrorCodeEARG, rerr.Error(), map[string]any{
			"hint": "Task-added notifications are sent only to the coordinator (or lead); configure the room membership first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
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
		Stream:      agent.RoomStreamName(roomID),
		Sender:      identity.Sender,
		Recipient:   addRecipient,
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

func runRoomTaskList(cmd *cobra.Command, workspace, roomID, status string, omitCompleted, omitCanceled bool) error {
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

	roomTasks, err := listRoomTasks(cmd.Context(), taskStore, taskWorkspaceID, messages, strings.TrimSpace(status), omitCompleted, omitCanceled)
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

	roomID = strings.TrimSpace(roomID)
	summary, err := boardStore.GetRoom(cmd.Context(), absWorkspace, roomID, "")
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	completeRecipient, rerr := roomTaskEventRecipient(summary.Members)
	if rerr != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeEARG, rerr.Error(), map[string]any{
			"hint": "Task completion notifications are sent only to the coordinator (or lead); configure the room membership first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	msg := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(roomID),
		Sender:      identity.Sender,
		Recipient:   completeRecipient,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Priority:    agent.DefaultPriority,
		Subject:     fmt.Sprintf("Task completed: %s", task.Title),
		Body:        formatRoomTaskCompletionBody(task),
	}
	if err := boardStore.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.complete", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	data := map[string]any{
		"room_id":         strings.TrimSpace(roomID),
		"task":            task,
		"message_id":      msg.ID,
		"sender_identity": identity,
	}
	if roomTaskNoLiveRelay(cmd) {
		data["live_relay_skipped"] = true
	} else {
		data["live_relay"] = relayPersistedRoomMessages(cmd.Context(), boardStore, absWorkspace, roomID, []*agent.BoardMessage{msg})
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.task.complete", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomTaskAssign(cmd *cobra.Command, workspace, roomID, sender, taskID, recipient, notes string, opts roomTaskAssignOptions) error {
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
	if !roomMemberCanManageRoomTasks(summary.Members, identity.Sender) {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeEARG, "only room coordinators, room admins, or system admins may assign tasks to other participants", map[string]any{
			"sender": identity.Sender,
			"hint":   "Grant role=admin on room join for parent agents that should delegate assignments without being the coordinator.",
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
		Interrupt:     true,
		Subject:       subject,
		Body:          appendRoomTaskOperatorTip(strings.Join(bodyLines, "\n"), strings.TrimSpace(roomID), task.ID, identity.Sender),
	}
	if err := boardStore.SendMessage(cmd.Context(), direct); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.assign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	out := map[string]any{
		"room_id":         strings.TrimSpace(roomID),
		"task":            task,
		"assignee":        recipient,
		"message_id":      direct.ID,
		"sender_identity": identity,
	}
	if roomTaskNoLiveRelay(cmd) {
		out["live_relay_skipped"] = true
	} else {
		out["live_relay"] = relayPersistedRoomMessages(cmd.Context(), boardStore, absWorkspace, roomID, []*agent.BoardMessage{direct})
	}
	if opts.ProvisionPane {
		provisioned, provisionErr := provisionAssigneePane(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), summary, recipient, opts)
		if provisionErr != nil {
			out["provision_error"] = provisionErr.Error()
		} else if provisioned != nil {
			out["provisioned_pane"] = provisioned
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.task.assign", out, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
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
	absWorkspace, identity, summary, task, boardStore, taskStore, err := loadCoordinatorTaskContext(cmd, workspace, roomID, sender, taskID)
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
	reclaimRecipient, rerr := roomTaskEventRecipient(summary.Members)
	if rerr != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task.reclaim", protocol.ErrorCodeEARG, rerr.Error(), map[string]any{
			"hint": "Task reclaim notifications are sent only to the coordinator (or lead); configure the room membership first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	msg := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(strings.TrimSpace(roomID)),
		Sender:      identity.Sender,
		Recipient:   reclaimRecipient,
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

	roomID = strings.TrimSpace(roomID)
	summary, gerr := boardStore.GetRoom(cmd.Context(), absWorkspace, roomID, "")
	if gerr != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeERuntime, gerr.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	coordOrLead, rerr := roomTaskEventRecipient(summary.Members)
	if rerr != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeEARG, rerr.Error(), map[string]any{
			"hint": "Task notifications are sent only to the coordinator (or lead); configure the room membership first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	subject, body := formatRoomTaskTransitionMessage(action, task)
	recipient := coordOrLead
	msg := &agent.BoardMessage{
		WorkspaceID: absWorkspace,
		TaskID:      task.ID,
		Stream:      agent.RoomStreamName(roomID),
		Sender:      identity.Sender,
		Recipient:   recipient,
		Kind:        agent.BoardMessageKindTaskUpdate,
		Priority:    agent.DefaultPriority,
		Subject:     subject,
		Body:        body,
	}
	if err := boardStore.SendMessage(cmd.Context(), msg); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task."+action, protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	out := map[string]any{
		"room_id":         strings.TrimSpace(roomID),
		"task":            task,
		"message_id":      msg.ID,
		"sender_identity": identity,
	}
	if roomTaskNoLiveRelay(cmd) {
		out["live_relay_skipped"] = true
	} else {
		out["live_relay"] = relayPersistedRoomMessages(cmd.Context(), boardStore, absWorkspace, roomID, []*agent.BoardMessage{msg})
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "agentctl.room.task."+action, out, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

type roomPulseConfig struct {
	Enabled                      bool
	Interval                     time.Duration
	ReplyStaleAfter              time.Duration
	TaskStaleAfter               time.Duration
	MinPulseFloor                time.Duration
	InterruptAttemptLimit        int
	ReminderBackoffCap           int
	CoordinatorPulseEnabled      bool
	CoordinatorEscalationEnabled bool
}

func runRoomLoop(cmd *cobra.Command, workspace, roomID string, relay roomRelayOptions, poll, taskPoll time.Duration, history int, pulse roomPulseConfig) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
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
	coordStore, err := coordination.Open(cmd.Context(), cfg.Storage.Root)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer coordStore.Close()

	client := tmuxbridge.New()
	writer := envelope.NewWriter(cmd.OutOrStdout())
	seq := 0

	persistedLoop, err := syncRoomLoopState(cmd.Context(), coordStore, absWorkspace, roomID, pulse, time.Now().UTC())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	pulse = persistedLoop

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
	configTicker := time.NewTicker(5 * time.Second)
	defer configTicker.Stop()
	var pulseTicker *time.Ticker
	if pulse.Enabled && pulse.Interval > 0 {
		pulseTicker = time.NewTicker(pulse.Interval)
		defer pulseTicker.Stop()
	}
	remindedMessages := map[string]roomPulseState{}
	remindedTasks := map[string]roomPulseState{}
	remindedTaskFollowups := map[string]time.Time{}
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
			updatedPulse, pulseChanged, err := refreshRoomLoopPolicy(cmd.Context(), coordStore, absWorkspace, roomID, pulse, time.Now().UTC())
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			if pulseChanged {
				pulse = updatedPulse
				pulseTicker = resetRoomPulseTicker(pulseTicker, pulse)
			}
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
			updatedPulse, pulseChanged, err := refreshRoomLoopPolicy(cmd.Context(), coordStore, absWorkspace, roomID, pulse, time.Now().UTC())
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			if pulseChanged {
				pulse = updatedPulse
				pulseTicker = resetRoomPulseTicker(pulseTicker, pulse)
			}
			updates, err := detectRoomTaskTransitions(cmd.Context(), taskStore, taskStates, announcedStates)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			loopRoomSummary, loopRoomErr := boardStore.GetRoom(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), "")
			var taskLoopRecipient string
			var taskLoopRecipientErr error
			if loopRoomErr == nil {
				taskLoopRecipient, taskLoopRecipientErr = roomTaskEventRecipient(loopRoomSummary.Members)
			}
			for _, update := range updates {
				announcedStates[update.Task.ID] = update.Task.Status
				if loopRoomErr != nil || taskLoopRecipientErr != nil {
					continue
				}
				msg := &agent.BoardMessage{
					WorkspaceID: absWorkspace,
					TaskID:      update.Task.ID,
					Stream:      agent.RoomStreamName(roomID),
					Sender:      roomLoopSender(roomID),
					Recipient:   taskLoopRecipient,
					Kind:        agent.BoardMessageKindTaskUpdate,
					Priority:    agent.DefaultPriority,
					Subject:     update.Subject,
					Body:        update.Body,
				}
				if err := boardStore.SendMessage(cmd.Context(), msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				seenMessages[msg.ID] = struct{}{}
				summary, _, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				relayResult := relayRoomMessage(cmd.Context(), client, summary, *msg, relay)
				seq++
				if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
					"event":   "task_transition",
					"room_id": roomID,
					"task":    update.Task,
					"subject": update.Subject,
					"message": msg,
					"relay":   relayResult,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop task envelope: %w", err)
				}
			}
		case <-configTicker.C:
			updatedPulse, pulseChanged, err := refreshRoomLoopPolicy(cmd.Context(), coordStore, absWorkspace, roomID, pulse, time.Now().UTC())
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			if pulseChanged {
				pulse = updatedPulse
				pulseTicker = resetRoomPulseTicker(pulseTicker, pulse)
			}
			summary, current, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			reminderMessages, err := processRoomReminderTick(cmd.Context(), coordStore, summary, current, time.Now().UTC())
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			for _, msg := range reminderMessages {
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
					"event":   "room_reminder",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop reminder envelope: %w", err)
				}
			}
		case <-roomPulseChan(pulseTicker):
			summary, current, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			reminderRoots, err := loadRoomReminderRoots(cmd.Context(), summary.WorkspaceID, roomID, true)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			roomTasks, err := listRoomTasks(cmd.Context(), taskStore, ws.CanonicalID(absWorkspace), current, "", false, false)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			cleanupRoomReplyPulseState(current, time.Now().UTC(), pulse, remindedMessages)
			cleanupRoomTaskPulseState(roomTasks, time.Now().UTC(), pulse, remindedTasks)
			cleanupRoomTaskFollowupState(roomTasks, remindedTaskFollowups)
			for _, pulseMsg := range detectRoomPulseMessages(roomID, current, time.Now().UTC(), pulse, remindedMessages, reminderRoots) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				markRoomPulseState(remindedMessages, pulseMsg.Key, msg.CreatedAt, false)
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
			for _, pulseMsg := range detectRoomTaskFollowupMessages(summary, roomTasks, time.Now().UTC(), pulse, remindedTaskFollowups) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				remindedTaskFollowups[pulseMsg.Key] = msg.CreatedAt
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
					"event":   "task_followup",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop task follow-up envelope: %w", err)
				}
			}
			for _, pulseMsg := range detectRoomTaskPulseMessages(absWorkspace, roomID, roomTasks, time.Now().UTC(), pulse, remindedTasks) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				markRoomPulseState(remindedTasks, pulseMsg.Key, msg.CreatedAt, false)
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
			for _, pulseMsg := range detectRoomPulseEscalationMessages(summary, current, time.Now().UTC(), pulse, remindedMessages, reminderRoots) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				markRoomPulseState(remindedMessages, pulseMsg.Key, msg.CreatedAt, true)
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
					"event":   "room_pulse_escalation",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop pulse escalation envelope: %w", err)
				}
			}
			for _, pulseMsg := range detectRoomTaskEscalationMessages(summary, roomTasks, time.Now().UTC(), pulse, remindedTasks) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				markRoomPulseState(remindedTasks, pulseMsg.Key, msg.CreatedAt, true)
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := writer.Write(roomProgressEnvelope("agentctl.room.loop", seq, false, map[string]any{
					"event":   "task_pulse_escalation",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop task escalation envelope: %w", err)
				}
			}
			for _, pulseMsg := range detectRoomCoordinatorPulseMessages(summary, current, roomTasks, time.Now().UTC(), pulse, remindedCoordinators, reminderRoots) {
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

func defaultRoomLoopPolicy(workspaceID, roomID string, pulse roomPulseConfig) coordination.RoomLoop {
	if pulse.MinPulseFloor <= 0 {
		pulse.MinPulseFloor = roomLoopMinimumPulseFloor
	}
	if pulse.InterruptAttemptLimit <= 0 {
		pulse.InterruptAttemptLimit = roomPulseInterruptLimit
	}
	if pulse.ReminderBackoffCap <= 0 {
		pulse.ReminderBackoffCap = roomPulseBackoffCap
	}
	if pulse.Interval <= 0 {
		pulse.Enabled = false
	}
	return coordination.RoomLoop{
		WorkspaceID:                  strings.TrimSpace(workspaceID),
		RoomID:                       strings.TrimSpace(roomID),
		Enabled:                      pulse.Enabled,
		ManagedBy:                    roomLoopManagedBy,
		PulseInterval:                pulse.Interval,
		ReplyStaleAfter:              pulse.ReplyStaleAfter,
		TaskStaleAfter:               pulse.TaskStaleAfter,
		MinPulseFloor:                pulse.MinPulseFloor,
		InterruptAttemptLimit:        pulse.InterruptAttemptLimit,
		ReminderBackoffCap:           pulse.ReminderBackoffCap,
		CoordinatorPulseEnabled:      pulse.CoordinatorPulseEnabled,
		CoordinatorEscalationEnabled: pulse.CoordinatorEscalationEnabled,
	}
}

func syncRoomLoopState(ctx context.Context, store *coordination.Store, workspaceID, roomID string, current roomPulseConfig, tickAt time.Time) (roomPulseConfig, error) {
	loop, err := store.GetRoomLoop(ctx, workspaceID, roomID)
	if err != nil {
		return roomPulseConfig{}, err
	}
	if loop == nil {
		seed := defaultRoomLoopPolicy(workspaceID, roomID, current)
		seed.LastTickAt = &tickAt
		persisted, err := store.UpsertRoomLoop(ctx, seed)
		if err != nil {
			return roomPulseConfig{}, err
		}
		return roomPulseConfigFromStore(persisted), nil
	}
	if strings.TrimSpace(loop.ManagedBy) != roomLoopManagedBy {
		loop.ManagedBy = roomLoopManagedBy
	}
	if loop.MinPulseFloor <= 0 {
		loop.MinPulseFloor = roomLoopMinimumPulseFloor
	}
	if loop.InterruptAttemptLimit <= 0 {
		loop.InterruptAttemptLimit = roomPulseInterruptLimit
	}
	if loop.ReminderBackoffCap <= 0 {
		loop.ReminderBackoffCap = roomPulseBackoffCap
	}
	loop.LastTickAt = &tickAt
	persisted, err := store.UpsertRoomLoop(ctx, *loop)
	if err != nil {
		return roomPulseConfig{}, err
	}
	return roomPulseConfigFromStore(persisted), nil
}

func refreshRoomLoopPolicy(ctx context.Context, store *coordination.Store, workspaceID, roomID string, current roomPulseConfig, tickAt time.Time) (roomPulseConfig, bool, error) {
	next, err := syncRoomLoopState(ctx, store, workspaceID, roomID, current, tickAt)
	if err != nil {
		return roomPulseConfig{}, false, err
	}
	return next, !sameRoomPulseConfig(current, next), nil
}

func sameRoomPulseConfig(a, b roomPulseConfig) bool {
	return a.Enabled == b.Enabled &&
		a.Interval == b.Interval &&
		a.ReplyStaleAfter == b.ReplyStaleAfter &&
		a.TaskStaleAfter == b.TaskStaleAfter &&
		a.MinPulseFloor == b.MinPulseFloor &&
		a.InterruptAttemptLimit == b.InterruptAttemptLimit &&
		a.ReminderBackoffCap == b.ReminderBackoffCap &&
		a.CoordinatorPulseEnabled == b.CoordinatorPulseEnabled &&
		a.CoordinatorEscalationEnabled == b.CoordinatorEscalationEnabled
}

func roomPulseConfigFromStore(loop coordination.RoomLoop) roomPulseConfig {
	return roomPulseConfig{
		Enabled:                      loop.Enabled,
		Interval:                     loop.PulseInterval,
		ReplyStaleAfter:              loop.ReplyStaleAfter,
		TaskStaleAfter:               loop.TaskStaleAfter,
		MinPulseFloor:                loop.MinPulseFloor,
		InterruptAttemptLimit:        loop.InterruptAttemptLimit,
		ReminderBackoffCap:           loop.ReminderBackoffCap,
		CoordinatorPulseEnabled:      loop.CoordinatorPulseEnabled,
		CoordinatorEscalationEnabled: loop.CoordinatorEscalationEnabled,
	}
}

func resetRoomPulseTicker(current *time.Ticker, pulse roomPulseConfig) *time.Ticker {
	if current != nil {
		current.Stop()
	}
	if !roomPulseEnabled(pulse) || pulse.Interval <= 0 {
		return nil
	}
	return time.NewTicker(pulse.Interval)
}

func roomPulseEnabled(cfg roomPulseConfig) bool {
	if cfg.Enabled {
		return true
	}
	return cfg.Interval > 0 || cfg.ReplyStaleAfter > 0 || cfg.TaskStaleAfter > 0 || cfg.CoordinatorPulseEnabled
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

type roomPulseState struct {
	LastSentAt time.Time
	Count      int
	Escalated  bool
}

func processRoomReminderTick(ctx context.Context, coordStore *coordination.Store, room agent.RoomSummary, messages []agent.BoardMessage, now time.Time) ([]agent.BoardMessage, error) {
	if coordStore == nil {
		return nil, nil
	}
	reminders, err := coordStore.ListRoomReminders(ctx, room.WorkspaceID, room.ID, false)
	if err != nil {
		return nil, err
	}
	if len(reminders) == 0 {
		return nil, nil
	}
	latestBySender := latestRoomSenderActivity(messages)
	out := make([]agent.BoardMessage, 0, len(reminders))
	for _, reminder := range reminders {
		if !reminder.Active {
			continue
		}
		if roomReminderSatisfied(reminder.RootMessageID, messages, latestBySender) || reminder.SentCount >= reminder.MaxIterations {
			reminder.Active = false
			if _, err := coordStore.UpsertRoomReminder(ctx, reminder); err != nil {
				return nil, err
			}
			continue
		}
		if reminder.LastSentAt != nil && !reminder.LastSentAt.IsZero() && now.Sub(*reminder.LastSentAt) < reminder.Interval {
			continue
		}
		out = append(out, buildRoomReminderMessage(room, reminder, now))
		reminder.SentCount++
		sentAt := now
		reminder.LastSentAt = &sentAt
		if reminder.SentCount >= reminder.MaxIterations {
			reminder.Active = false
		}
		if _, err := coordStore.UpsertRoomReminder(ctx, reminder); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func roomReminderSatisfied(rootMessageID string, messages []agent.BoardMessage, latestBySender map[string]roomSenderActivity) bool {
	rootMessageID = strings.TrimSpace(rootMessageID)
	if rootMessageID == "" {
		return true
	}
	var root *agent.BoardMessage
	for i := range messages {
		if strings.TrimSpace(messages[i].ID) == rootMessageID {
			root = &messages[i]
			break
		}
	}
	if root == nil {
		return true
	}
	chainKey := rootMessageID
	for _, msg := range messages {
		if roomMessageChainKey(msg) != chainKey && strings.TrimSpace(msg.ID) != chainKey {
			continue
		}
		if msg.Status == agent.BoardMessageStatusAcked || msg.Status == agent.BoardMessageStatusRead {
			return true
		}
	}
	if root.ReplyExpected && !messageStillAwaitsReply(*root, latestBySender) {
		return true
	}
	return false
}

func buildRoomReminderMessage(room agent.RoomSummary, reminder coordination.RoomReminder, now time.Time) agent.BoardMessage {
	subject := fmt.Sprintf("Reminder (%d/%d): %s", reminder.SentCount+1, reminder.MaxIterations, strings.TrimSpace(reminder.Subject))
	body := fmt.Sprintf("Scheduled follow-up for message %s.\nOriginal sender: %s\nOriginal request: %s", strings.TrimSpace(reminder.RootMessageID), strings.TrimSpace(reminder.Sender), strings.TrimSpace(reminder.Subject))
	if strings.TrimSpace(reminder.Body) != "" && strings.TrimSpace(reminder.Body) != strings.TrimSpace(reminder.Subject) {
		body += "\nOriginal body: " + strings.TrimSpace(reminder.Body)
	}
	body += fmt.Sprintf("\nReminder iteration: %d of %d", reminder.SentCount+1, reminder.MaxIterations)
	return agent.BoardMessage{
		WorkspaceID:      room.WorkspaceID,
		RelatedMessageID: strings.TrimSpace(reminder.RootMessageID),
		Stream:           room.Stream,
		Sender:           roomLoopSender(room.ID),
		Recipient:        strings.TrimSpace(reminder.Recipient),
		Kind:             agent.BoardMessageKindAlert,
		Priority:         2,
		Interrupt:        reminder.Interrupt,
		Subject:          subject,
		Body:             body,
		CreatedAt:        now,
	}
}

func markRoomPulseState(states map[string]roomPulseState, key string, sentAt time.Time, escalated bool) {
	state := states[key]
	state.LastSentAt = sentAt
	state.Count++
	if escalated {
		state.Escalated = true
	}
	states[key] = state
}

func roomPulseReminderInterval(base time.Duration, capMultiplier int, state roomPulseState) time.Duration {
	if base <= 0 {
		return 0
	}
	multiplier := 1
	if state.Count > 0 {
		multiplier = 1 << min(state.Count-1, 3)
	}
	if capMultiplier <= 0 {
		capMultiplier = roomPulseBackoffCap
	}
	if multiplier > capMultiplier {
		multiplier = capMultiplier
	}
	return time.Duration(multiplier) * base
}

func roomPulseReady(now time.Time, base time.Duration, capMultiplier int, state roomPulseState) bool {
	if state.LastSentAt.IsZero() {
		return true
	}
	return now.Sub(state.LastSentAt) >= roomPulseReminderInterval(base, capMultiplier, state)
}

func cleanupRoomReplyPulseState(messages []agent.BoardMessage, now time.Time, cfg roomPulseConfig, states map[string]roomPulseState) {
	latestBySender := latestRoomSenderActivity(messages)
	active := make(map[string]struct{})
	for _, msg := range messages {
		recipient := normalizeRoomRecipient(msg.Recipient)
		if recipient == agent.BroadcastRecipient || recipient == "" {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(msg.Sender), roomLoopSender(strings.TrimPrefix(msg.Stream, agent.RoomStreamPrefix))) {
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
		active[msg.ID] = struct{}{}
	}
	for key := range states {
		if _, ok := active[key]; !ok {
			delete(states, key)
		}
	}
}

func cleanupRoomTaskPulseState(tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, states map[string]roomPulseState) {
	active := make(map[string]struct{})
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
		active[task.ID] = struct{}{}
	}
	for key := range states {
		if _, ok := active[key]; !ok {
			delete(states, key)
		}
	}
}

func cleanupRoomTaskFollowupState(tasks []taskstore.Task, states map[string]time.Time) {
	active := make(map[string]struct{})
	for _, task := range tasks {
		if task.OwnerActorID == "" {
			continue
		}
		if task.Status != taskstore.StatusInProgress && task.Status != taskstore.StatusBlocked {
			continue
		}
		active[task.ID] = struct{}{}
	}
	for key := range states {
		if _, ok := active[key]; !ok {
			delete(states, key)
		}
	}
}

func openRoomTaskStore(ctx context.Context) (taskstore.Store, error) {
	cfg, err := loadConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return taskstore.Open(ctx, cfg.Storage.Root)
}

// parseRoomTaskListSelection maps CLI --filter/--status into listRoomTasks arguments.
// When status is non-empty, omission flags are always false (status drives inclusion).
func parseRoomTaskListSelection(status, filter string) (statusOut string, omitCompleted, omitCanceled bool, err error) {
	statusOut = strings.TrimSpace(status)
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter == "" {
		filter = "open"
	}
	switch filter {
	case "open", "active":
	case "all":
	case "completed":
	default:
		return "", false, false, fmt.Errorf("unknown --filter %q, want open, all, or completed", filter)
	}
	if filter == "completed" {
		if statusOut != "" && statusOut != taskstore.StatusCompleted {
			return "", false, false, fmt.Errorf("--filter completed cannot be combined with --status %q", statusOut)
		}
		statusOut = taskstore.StatusCompleted
	}
	if statusOut != "" {
		return statusOut, false, false, nil
	}
	if filter == "open" || filter == "active" {
		return "", true, true, nil
	}
	// filter == "all"
	return "", false, false, nil
}

func listRoomTasks(ctx context.Context, store taskstore.Store, workspaceID string, messages []agent.BoardMessage, status string, omitCompleted, omitCanceled bool) ([]taskstore.Task, error) {
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
		if omitCompleted && status == "" && task.Status == taskstore.StatusCompleted {
			continue
		}
		if omitCanceled && status == "" && task.Status == taskstore.StatusCanceled {
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

func detectRoomPulseMessages(roomID string, messages []agent.BoardMessage, now time.Time, cfg roomPulseConfig, reminded map[string]roomPulseState, reminderRoots map[string]struct{}) []roomPulseMessage {
	if !roomPulseEnabled(cfg) || cfg.ReplyStaleAfter <= 0 {
		return nil
	}
	reminderFloor := cfg.ReplyStaleAfter
	if cfg.MinPulseFloor > reminderFloor {
		reminderFloor = cfg.MinPulseFloor
	}
	latestBySender := latestRoomSenderActivity(messages)
	latestOutstanding := make(map[string]agent.BoardMessage)
	for _, msg := range messages {
		if _, suppressed := reminderRoots[roomMessageChainKey(msg)]; suppressed {
			continue
		}
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
		state := reminded[msg.ID]
		limit := cfg.InterruptAttemptLimit
		if limit <= 0 {
			limit = roomPulseInterruptLimit
		}
		if state.Count >= limit {
			continue
		}
		if !roomPulseReady(now, reminderFloor, cfg.ReminderBackoffCap, state) {
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
				Interrupt:        true,
				Subject:          subject,
				Body:             body,
				CreatedAt:        now,
			},
		})
	}
	return out
}

func detectRoomPulseEscalationMessages(room agent.RoomSummary, messages []agent.BoardMessage, now time.Time, cfg roomPulseConfig, reminded map[string]roomPulseState, reminderRoots map[string]struct{}) []roomPulseMessage {
	if !cfg.CoordinatorEscalationEnabled {
		return nil
	}
	coordinator := roomCoordinatorActorID(room.Members)
	if coordinator == "" {
		return nil
	}
	limit := cfg.InterruptAttemptLimit
	if limit <= 0 {
		limit = roomPulseInterruptLimit
	}
	latestBySender := latestRoomSenderActivity(messages)
	out := make([]roomPulseMessage, 0)
	for _, msg := range messages {
		if _, suppressed := reminderRoots[roomMessageChainKey(msg)]; suppressed {
			continue
		}
		state, ok := reminded[msg.ID]
		if !ok || state.Count < limit || state.Escalated {
			continue
		}
		recipient := normalizeRoomRecipient(msg.Recipient)
		if recipient == agent.BroadcastRecipient || recipient == "" {
			continue
		}
		if msg.Status == agent.BoardMessageStatusAcked || msg.Status == agent.BoardMessageStatusRead {
			continue
		}
		if msg.ReplyExpected && !messageStillAwaitsReply(msg, latestBySender) {
			continue
		}
		subject := fmt.Sprintf("Escalation: repeated unanswered request for %s", recipient)
		body := fmt.Sprintf("Message %s to %s has already triggered %d interrupting reminders with no acknowledgement or reply. Consider updating loop policy, resolving the request, or reclaiming the work.\nOriginal sender: %s\nOriginal subject: %s", msg.ID, recipient, state.Count, strings.TrimSpace(msg.Sender), strings.TrimSpace(msg.Subject))
		out = append(out, roomPulseMessage{Key: msg.ID, Message: agent.BoardMessage{WorkspaceID: room.WorkspaceID, TaskID: msg.TaskID, RelatedMessageID: roomMessageChainKey(msg), Stream: room.Stream, Sender: roomLoopSender(room.ID), Recipient: coordinator, Kind: agent.BoardMessageKindAlert, Priority: 1, Interrupt: true, Subject: subject, Body: body, CreatedAt: now}})
	}
	return out
}

func detectRoomTaskPulseMessages(workspace, roomID string, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]roomPulseState) []roomPulseMessage {
	if !roomPulseEnabled(cfg) || cfg.TaskStaleAfter <= 0 {
		return nil
	}
	reminderFloor := cfg.TaskStaleAfter
	if cfg.MinPulseFloor > reminderFloor {
		reminderFloor = cfg.MinPulseFloor
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
		state := reminded[task.ID]
		limit := cfg.InterruptAttemptLimit
		if limit <= 0 {
			limit = roomPulseInterruptLimit
		}
		if state.Count >= limit {
			continue
		}
		if !roomPulseReady(now, reminderFloor, cfg.ReminderBackoffCap, state) {
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
				Interrupt:   true,
				Subject:     subject,
				Body:        body,
				CreatedAt:   now,
			},
		})
	}
	return out
}

func detectRoomTaskFollowupMessages(room agent.RoomSummary, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]time.Time) []roomPulseMessage {
	if !roomPulseEnabled(cfg) || cfg.Interval <= 0 {
		return nil
	}
	coordinator := roomCoordinatorActorID(room.Members)
	if coordinator == "" {
		coordinator = "@coordinator"
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
		if now.Sub(reference) < cfg.Interval {
			continue
		}
		if last, ok := reminded[task.ID]; ok && now.Sub(last) < cfg.Interval {
			continue
		}
		if cfg.TaskStaleAfter > 0 && now.Sub(reference) >= cfg.TaskStaleAfter {
			continue
		}
		subject := fmt.Sprintf("Task check-in: %s", task.Title)
		bodyLines := []string{
			fmt.Sprintf("Task %s is still %s.", task.ID, task.Status),
			fmt.Sprintf("Owner: %s", task.OwnerActorID),
			"Please post a durable status update or complete the task if the work is done.",
		}
		if strings.TrimSpace(task.BlockedReason) != "" {
			bodyLines = append(bodyLines, "Blocked reason: "+strings.TrimSpace(task.BlockedReason))
		}
		out = append(out, roomPulseMessage{
			Key: task.ID,
			Message: agent.BoardMessage{
				WorkspaceID: room.WorkspaceID,
				TaskID:      task.ID,
				Stream:      room.Stream,
				Sender:      roomLoopSender(room.ID),
				Recipient:   task.OwnerActorID,
				Kind:        agent.BoardMessageKindInfo,
				Priority:    agent.DefaultPriority,
				Subject:     subject,
				Body:        appendRoomTaskOperatorTip(strings.Join(bodyLines, "\n"), room.ID, task.ID, coordinator),
				CreatedAt:   now,
			},
		})
	}
	return out
}

func detectRoomTaskEscalationMessages(room agent.RoomSummary, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]roomPulseState) []roomPulseMessage {
	if !cfg.CoordinatorEscalationEnabled {
		return nil
	}
	coordinator := roomCoordinatorActorID(room.Members)
	if coordinator == "" {
		return nil
	}
	limit := cfg.InterruptAttemptLimit
	if limit <= 0 {
		limit = roomPulseInterruptLimit
	}
	out := make([]roomPulseMessage, 0)
	for _, task := range tasks {
		state, ok := reminded[task.ID]
		if !ok || state.Count < limit || state.Escalated {
			continue
		}
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
		subject := fmt.Sprintf("Escalation: task may be stuck: %s", task.Title)
		body := fmt.Sprintf("Task %s owned by %s has already triggered %d interrupting reminders with no heartbeat change. Consider touching the task, updating loop policy, reclaiming, blocking, or reassigning it.", task.ID, task.OwnerActorID, state.Count)
		out = append(out, roomPulseMessage{Key: task.ID, Message: agent.BoardMessage{WorkspaceID: room.WorkspaceID, TaskID: task.ID, Stream: room.Stream, Sender: roomLoopSender(room.ID), Recipient: coordinator, Kind: agent.BoardMessageKindAlert, Priority: 1, Interrupt: true, Subject: subject, Body: body, CreatedAt: now}})
	}
	return out
}

func detectRoomCoordinatorPulseMessages(room agent.RoomSummary, messages []agent.BoardMessage, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]time.Time, reminderRoots map[string]struct{}) []roomPulseMessage {
	if !roomPulseEnabled(cfg) || !cfg.CoordinatorPulseEnabled || cfg.Interval <= 0 {
		return nil
	}
	coordinator := roomCoordinatorActorID(room.Members)
	if coordinator == "" {
		return nil
	}
	backlog := buildRoomStatusBacklog(room, messages, reminderRoots)
	taskPulse := buildRoomTaskPulseSummary(tasks, now, cfg.TaskStaleAfter)
	action := buildRoomStatusActionRequired(room, messages, tasks, backlog, taskPulse, map[string]struct{}{"all": {}}, cfg.TaskStaleAfter, now, false, reminderRoots)
	if action.ParticipantsWithPending == 0 && action.AssignedUnclaimed == 0 && action.BlockedTasks == 0 && action.StaleTasks == 0 {
		return nil
	}
	key := fmt.Sprintf("%s|%d|%d|%d|%d|%d|%d", coordinator, action.ParticipantsWithPending, action.PendingAcks, action.PendingReplies, action.AssignedUnclaimed, action.BlockedTasks, action.StaleTasks)
	reminderFloor := cfg.Interval
	if cfg.MinPulseFloor > reminderFloor {
		reminderFloor = cfg.MinPulseFloor
	}
	if last, ok := reminded[key]; ok && now.Sub(last) < reminderFloor {
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
			Kind:        agent.BoardMessageKindCoordinatorPulse,
			Priority:    2,
			Interrupt:   true,
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
	if !roomMemberCanManageRoomTasks(summary.Members, identity.Sender) {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "agentctl.room.task", protocol.ErrorCodeEARG, "only room coordinators, room admins, or system admins may perform this action", map[string]any{
			"sender": identity.Sender,
			"hint":   "Reassign/reclaim require the same privilege as assign (coordinator, room admin, or system admin).",
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

// sendRoomCoordinatorInfoNote delivers a short direct info message to the room coordinator when the
// sender is not the coordinator. It returns (nil, nil) when there is nothing to send.
func sendRoomCoordinatorInfoNote(ctx context.Context, cmd *cobra.Command, boardStore blackboard.BoardStore, workspace, roomID, sender, taskID, subject, body string) (*agent.BoardMessage, error) {
	roomID = strings.TrimSpace(roomID)
	sender = strings.TrimSpace(sender)
	if roomID == "" || sender == "" {
		return nil, nil
	}
	summary, err := boardStore.GetRoom(ctx, workspace, roomID, "")
	if err != nil {
		return nil, err
	}
	coord := strings.TrimSpace(roomCoordinatorActorID(summary.Members))
	if coord == "" || sameRoomParticipant(coord, sender) {
		return nil, nil
	}
	note := &agent.BoardMessage{
		WorkspaceID: workspace,
		TaskID:      strings.TrimSpace(taskID),
		Stream:      agent.RoomStreamName(roomID),
		Sender:      sender,
		Recipient:   coord,
		Kind:        agent.BoardMessageKindInfo,
		Priority:    agent.DefaultPriority,
		Subject:     strings.TrimSpace(subject),
		Body:        strings.TrimSpace(body),
	}
	if err := boardStore.SendMessage(ctx, note); err != nil {
		return nil, err
	}
	if cmd != nil && !roomTaskNoLiveRelay(cmd) {
		_ = relayPersistedRoomMessages(ctx, boardStore, workspace, roomID, []*agent.BoardMessage{note})
	}
	return note, nil
}

func sendRoomTaskCoordinatorMessages(ctx context.Context, workspace, roomID string, boardStore blackboard.BoardStore, sender, recipient string, task taskstore.Task, subject, body string) error {
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
		Interrupt:     true,
		Subject:       subject,
		Body:          appendRoomTaskOperatorTip(body, roomID, task.ID, sender),
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

func appendRoomTaskOperatorTip(body, roomID, taskID, sender string) string {
	lines := []string{strings.TrimSpace(body)}
	tip := []string{
		"Quick tip:",
		"- Read the `agentctl-room-operator` skill if you need the room protocol.",
		fmt.Sprintf("- Claim with: agentctl room task claim %s --id %s", roomID, taskID),
		fmt.Sprintf("- Reply durably to the coordinator with: agentctl room send %s --to %s \"status update\"", roomID, sender),
		fmt.Sprintf("- Send room-wide updates with: agentctl room send %s \"team update\"", roomID),
		fmt.Sprintf("- Complete with: agentctl room task complete %s --id %s --notes \"what changed\"", roomID, taskID),
	}
	lines = append(lines, "", strings.Join(tip, "\n"))
	return strings.Join(lines, "\n")
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

// roomTaskAssignOptions carries optional pane provisioning parameters for task assign.
type roomTaskAssignOptions struct {
	ProvisionPane bool
	PaneAgent     string
	PaneAgentMode string
}

// provisionAssigneePane checks whether the assignee already has mux pane bindings
// in the room membership and, if not, creates a source pane for them in the room session.
func provisionAssigneePane(ctx context.Context, absWorkspace, roomID string, summary agent.RoomSummary, recipient string, opts roomTaskAssignOptions) (map[string]any, error) {
	for _, member := range summary.Members {
		if !sameRoomParticipant(member.ActorID, recipient) {
			continue
		}
		if strings.TrimSpace(member.PaneID) != "" || strings.TrimSpace(member.Session) != "" {
			return nil, nil
		}
	}
	backend := resolveMuxCreateBackend("")
	if backend == "" {
		backend = "tmux"
	}
	session := roomSourceSessionName(roomID, backend)
	agentCLI := strings.TrimSpace(opts.PaneAgent)
	agentMode := strings.TrimSpace(opts.PaneAgentMode)
	command, err := resolveMuxCreateCommand("", agentCLI, agentMode, nil, "")
	if err != nil {
		return nil, fmt.Errorf("resolve pane command for assignee %s: %w", recipient, err)
	}
	switch backend {
	case "tmux":
		client := tmuxbridge.New()
		result, createErr := client.CreatePane(ctx, tmuxbridge.CreatePaneOptions{
			Session:       session,
			CWD:           absWorkspace,
			Label:         recipient,
			Command:       command,
			ParticipantID: recipient,
			RoomID:        roomID,
			RoomAccess:    "direct",
		})
		if createErr != nil {
			return nil, createErr
		}
		return map[string]any{
			"backend":        "tmux",
			"session":        result.Session,
			"pane_id":        result.Pane.ID,
			"agent":          agentCLI,
			"attach_command": result.AttachCommand,
		}, nil
	case "zellij":
		client := zellijbridge.New()
		result, createErr := client.CreatePane(ctx, zellijbridge.CreatePaneOptions{
			Session:       session,
			CWD:           absWorkspace,
			Name:          recipient,
			Command:       command,
			ParticipantID: recipient,
			RoomID:        roomID,
			RoomAccess:    "direct",
		})
		if createErr != nil {
			return nil, createErr
		}
		return map[string]any{
			"backend":        "zellij",
			"session":        result.Session,
			"pane_id":        result.PaneName,
			"agent":          agentCLI,
			"attach_command": "zellij attach " + shellQuoteZshSafe(result.Session),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported mux backend %q for pane provisioning", backend)
	}
}
