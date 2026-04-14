package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/protocol"
	"github.com/joshka0/foxctl/internal/runtime/terminal/tmuxbridge"
	"github.com/joshka0/foxctl/internal/runtime/terminal/zellijbridge"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/coordination"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
	taskstore "github.com/joshka0/foxctl/internal/storage/tasks"
	"github.com/spf13/cobra"
)

const (
	roomTaskScanLimit            = 1000
	roomLoopManagedBy            = "foxctl.room.loop"
	roomLoopDefaultPulseInterval = 30 * time.Minute
	roomLoopDefaultReplyStale    = 2 * time.Hour
	roomLoopDefaultTaskStale     = 4 * time.Hour
	roomLoopMinimumPulseFloor    = 24 * time.Hour
	roomLoopLeaseTTL             = 20 * time.Second
	roomPulseInterruptLimit      = 2
	roomPulseBackoffCap          = 8
)

var errRoomTaskMilestoneNotFound = errors.New("room task milestone not found")

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
		workspace     string
		sender        string
		taskID        string
		recipient     string
		notes         string
		provisionPane bool
		paneAgent     string
		paneAgentMode string
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
		workspace   string
		sender      string
		title       string
		desc        string
		scopePath   string
		parentID    string
		milestoneID string
		dependsOn   []string
		autoCreate  bool
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
	cmd.Flags().StringVar(&milestoneID, "milestone-id", "", "Explicit milestone id to link this task to (defaults to the newest open epic's quiet chores milestone when omitted)")
	cmd.Flags().StringSliceVar(&dependsOn, "depends-on", nil, "Dependency task ids")
	cmd.Flags().BoolVar(&autoCreate, "create-room", true, "Create the room if it does not exist")
	_ = cmd.MarkFlagRequired("title")
	_ = milestoneID
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
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.list", protocol.ErrorCodeEARG, err.Error(), map[string]any{
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
		workspace string
		backend   string
		session   string
		plugin    string
		quiet     bool
		verbose   bool
		poll      time.Duration
		taskPoll  time.Duration
		history   int
	)
	cmd := &cobra.Command{
		Use:   "loop <room-id>",
		Short: "Run the room coordination loop using persisted loop policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if quiet && verbose {
				return fmt.Errorf("--quiet and --verbose cannot be used together")
			}
			taskEventMode := "default"
			if quiet {
				taskEventMode = "quiet"
			} else if verbose {
				taskEventMode = "verbose"
			}
			return runRoomLoop(cmd, workspace, args[0], roomRelayOptions{
				Backend:          backend,
				ZellijSession:    session,
				ZellijPluginPath: plugin,
				TaskEventMode:    taskEventMode,
			}, poll, taskPoll, history, roomPulseConfig{})
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&backend, "backend", "auto", "Terminal backend (auto|tmux|zellij)")
	cmd.Flags().StringVar(&session, "session", "", "Zellij session name (defaults to ZELLIJ_SESSION_NAME when inside zellij)")
	cmd.Flags().StringVar(&plugin, "plugin-path", "", "Path to the zellij room relay plugin wasm")
	cmd.Flags().BoolVar(&quiet, "quiet", false, "Suppress task transition relays from the room loop")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "Relay the broader set of task transitions from the room loop")
	cmd.Flags().DurationVar(&poll, "poll", 2*time.Second, "Room message polling interval")
	cmd.Flags().DurationVar(&taskPoll, "task-poll", 3*time.Second, "Room task polling interval")
	cmd.Flags().IntVar(&history, "history", 0, "Number of most recent room messages to replay into participants on startup")
	return cmd
}

func runRoomTaskAdd(cmd *cobra.Command, workspace, roomID, sender, title, desc, scopePath, parentID string, dependsOn []string, autoCreate bool) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.add", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so foxctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	taskWorkspaceID := ws.CanonicalID(absWorkspace)
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()
	if autoCreate {
		if _, err := boardStore.EnsureRoom(cmd.Context(), absWorkspace, roomID, roomID); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
	}
	roomID = strings.TrimSpace(roomID)
	summary, messages, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.add", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	addRecipient, rerr := roomTaskEventRecipient(summary.Members)
	if rerr != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.add", protocol.ErrorCodeEARG, rerr.Error(), map[string]any{
			"hint": "Task-added notifications are sent only to the coordinator (or lead); configure the room membership first.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()

	// Resolve scope path for sandbox rooms: if the room has a sandbox worktree,
	// make the scope path relative to the worktree root.
	effectiveScopePath := strings.TrimSpace(scopePath)
	if resolved := resolveSandboxScopePath(cmd.Context(), boardStore, absWorkspace, roomID, effectiveScopePath); resolved != effectiveScopePath {
		effectiveScopePath = resolved
	}
	selectedMilestoneID := ""
	if cmd != nil && cmd.Flags() != nil {
		if flag := cmd.Flags().Lookup("milestone-id"); flag != nil {
			selectedMilestoneID = strings.TrimSpace(flag.Value.String())
		}
	}
	selectedEpicID, selectedMilestoneID, err := resolveRoomTaskMilestoneSelection(messages, selectedMilestoneID)
	if err != nil {
		code := protocol.ErrorCodeEARG
		if errors.Is(err, errRoomTaskMilestoneNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.add", code, err.Error(), map[string]any{
			"hint": "Use a milestone id returned by `foxctl room milestone start` or listed in `foxctl room milestone show`, or omit --milestone-id to use the default chores lane.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	task, err := taskStore.Add(cmd.Context(), taskstore.Task{
		WorkspaceID: taskWorkspaceID,
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(desc),
		ScopePath:   effectiveScopePath,
		ParentID:    strings.TrimSpace(parentID),
		DependsOn:   append([]string(nil), dependsOn...),
		Status:      taskstore.StatusPending,
		EpicID:      selectedEpicID,
		MilestoneID: selectedMilestoneID,
	})
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
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
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.add", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.room.task.add", map[string]any{
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
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()

	summary, messages, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, strings.TrimSpace(roomID), "", roomTaskScanLimit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.list", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()

	roomTasks, err := listRoomTasks(cmd.Context(), taskStore, taskWorkspaceID, messages, strings.TrimSpace(status), omitCompleted, omitCanceled)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.list", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.room.task.list", map[string]any{
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
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.complete", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so foxctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	taskWorkspaceID := ws.CanonicalID(absWorkspace)
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.complete", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()

	task, err := taskStore.Get(cmd.Context(), strings.TrimSpace(taskID))
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.complete", protocol.ErrorCodeENotFound, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.WorkspaceID != taskWorkspaceID {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.complete", protocol.ErrorCodeEARG, "task does not belong to this workspace", map[string]any{
			"hint": "Use the same workspace used when the task was created.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusPending && strings.TrimSpace(task.OwnerActorID) == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.complete", protocol.ErrorCodeEARG, "task must be claimed before completion", map[string]any{
			"hint": "Run 'foxctl room task claim <room-id> --id <task-id>' before completing the task.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if strings.TrimSpace(task.OwnerActorID) != "" && task.OwnerActorID != identity.Sender {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.complete", protocol.ErrorCodeEARG, "only the current owner can complete this task", map[string]any{
			"owner_actor_id": task.OwnerActorID,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusBlocked {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.complete", protocol.ErrorCodeEARG, "blocked tasks must be unblocked before completion", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status != taskstore.StatusInProgress {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.complete", protocol.ErrorCodeEARG, "only in-progress tasks can be completed", map[string]any{
			"status": task.Status,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	now := time.Now().UTC()
	task.Status = taskstore.StatusCompleted
	task.CompletedAt = &now
	task.OwnerActorID = ""
	task.ClaimedAt = nil
	task.HeartbeatAt = &now
	task.BlockedReason = ""
	task.BlockedAt = nil
	task.Notes = strings.TrimSpace(notes)
	task.Gotchas = strings.TrimSpace(gotchas)
	if err := validateRoomTaskLifecycle(task); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.complete", protocol.ErrorCodeEARG, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	task, err = taskStore.Update(cmd.Context(), task)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.complete", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	data := map[string]any{
		"room_id":           strings.TrimSpace(roomID),
		"task":              task,
		"sender_identity":   identity,
		"notification_mode": "task_store_only",
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.room.task.complete", data, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func runRoomTaskAssign(cmd *cobra.Command, workspace, roomID, sender, taskID, recipient, notes string, opts roomTaskAssignOptions) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	identity, err := resolveRoomSender(cmd.Context(), sender)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so foxctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	recipient = normalizeRoomRecipient(recipient)
	if recipient == agent.BroadcastRecipient {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeEARG, "assignment requires a direct participant recipient", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	taskWorkspaceID := ws.CanonicalID(absWorkspace)
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()
	summary, err := boardStore.GetRoom(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), "")
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !roomMemberCanManageRoomTasks(summary.Members, identity.Sender) {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeEARG, "only room coordinators, room admins, or system admins may assign tasks to other participants", map[string]any{
			"sender": identity.Sender,
			"hint":   "Grant role=admin on room join for parent agents that should delegate assignments without being the coordinator.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !roomHasParticipant(summary, recipient) {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeEARG, "assignee is not a room participant", map[string]any{
			"recipient": recipient,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()
	task, err := taskStore.Get(cmd.Context(), strings.TrimSpace(taskID))
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeENotFound, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.WorkspaceID != taskWorkspaceID {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeEARG, "task does not belong to this workspace", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusCompleted {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeEARG, "completed tasks cannot be assigned", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusCanceled {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeEARG, "canceled tasks cannot be assigned", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if strings.TrimSpace(task.OwnerActorID) != "" || task.Status == taskstore.StatusInProgress || task.Status == taskstore.StatusBlocked {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeEARG, "claimed tasks must be reassigned instead of assigned", map[string]any{
			"status":         task.Status,
			"owner_actor_id": task.OwnerActorID,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	now := time.Now().UTC()
	if assigned := strings.TrimSpace(task.AssignedActorID); assigned != "" && !sameRoomParticipant(assigned, recipient) {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeEARG, "task is already assigned; use reassign to change assignee", map[string]any{
			"assigned_actor_id": task.AssignedActorID,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	task.AssignedActorID = recipient
	task.AssignedAt = &now
	if strings.TrimSpace(notes) != "" {
		task.Notes = strings.TrimSpace(notes)
	}
	if err := validateRoomTaskLifecycle(task); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeEARG, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	task, err = taskStore.Update(cmd.Context(), task)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
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
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.assign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	out := map[string]any{
		"room_id":          strings.TrimSpace(roomID),
		"task":             task,
		"assignee":         recipient,
		"message_id":       direct.ID,
		"status":           "queued",
		"delivery_owner":   "room_loop",
		"delivery_pending": true,
		"sender_identity":  identity,
	}
	if roomTaskNoLiveRelay(cmd) {
		out["warnings"] = []string{"--no-live-relay is deprecated for task assignment; assignment delivery is room-loop owned"}
	}
	if opts.ProvisionPane {
		provisioned, provisionErr := provisionAssigneePane(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), summary, recipient, opts)
		if provisionErr != nil {
			out["provision_error"] = provisionErr.Error()
		} else if provisioned != nil {
			out["provisioned_pane"] = provisioned
		}
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.room.task.assign", out, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
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
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reassign", protocol.ErrorCodeEARG, "reassign requires a direct participant recipient", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !roomHasParticipant(summary, recipient) {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reassign", protocol.ErrorCodeEARG, "assignee is not a room participant", map[string]any{
			"recipient": recipient,
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusCompleted {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reassign", protocol.ErrorCodeEARG, "completed tasks cannot be reassigned", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusCanceled {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reassign", protocol.ErrorCodeEARG, "canceled tasks cannot be reassigned", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	previousOwner := strings.TrimSpace(task.OwnerActorID)
	previousAssignee := strings.TrimSpace(task.AssignedActorID)
	if previousOwner == "" && previousAssignee == "" {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reassign", protocol.ErrorCodeEARG, "task has no current assignee or owner; use assign instead", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	now := time.Now().UTC()
	task.Status = taskstore.StatusPending
	task.AssignedActorID = recipient
	task.AssignedAt = &now
	task.OwnerActorID = ""
	task.ClaimedAt = nil
	task.HeartbeatAt = nil
	task.BlockedReason = ""
	task.BlockedAt = nil
	if err := validateRoomTaskLifecycle(task); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reassign", protocol.ErrorCodeEARG, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	task, err = taskStore.Update(cmd.Context(), task)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reassign", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.room.task.reassign", map[string]any{
		"room_id":           strings.TrimSpace(roomID),
		"task":              task,
		"assignee":          recipient,
		"previous_owner":    previousOwner,
		"previous_assignee": previousAssignee,
		"reason":            strings.TrimSpace(reason),
		"sender_identity":   identity,
		"notification_mode": "task_store_only",
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
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reclaim", protocol.ErrorCodeEARG, "task has no current owner to reclaim", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusCompleted {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reclaim", protocol.ErrorCodeEARG, "completed tasks cannot be reclaimed", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.Status == taskstore.StatusCanceled {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reclaim", protocol.ErrorCodeEARG, "canceled tasks cannot be reclaimed", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	task.Status = taskstore.StatusPending
	task.AssignedActorID = ""
	task.AssignedAt = nil
	task.OwnerActorID = ""
	task.ClaimedAt = nil
	task.HeartbeatAt = nil
	task.BlockedReason = ""
	task.BlockedAt = nil
	if err := validateRoomTaskLifecycle(task); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reclaim", protocol.ErrorCodeEARG, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	task, err = taskStore.Update(cmd.Context(), task)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.reclaim", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.room.task.reclaim", map[string]any{
		"room_id":           strings.TrimSpace(roomID),
		"task":              task,
		"previous_owner":    previousOwner,
		"reason":            strings.TrimSpace(reason),
		"sender_identity":   identity,
		"notification_mode": "task_store_only",
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
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task."+action, protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so foxctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	taskWorkspaceID := ws.CanonicalID(absWorkspace)
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task."+action, protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()

	task, err := taskStore.Get(cmd.Context(), strings.TrimSpace(taskID))
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task."+action, protocol.ErrorCodeENotFound, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if task.WorkspaceID != taskWorkspaceID {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task."+action, protocol.ErrorCodeEARG, "task does not belong to this workspace", map[string]any{
			"hint": "Use the same workspace used when the task was created.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	now := time.Now().UTC()
	switch action {
	case "claim":
		if task.Status == taskstore.StatusCompleted {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.claim", protocol.ErrorCodeEARG, "completed tasks cannot be claimed", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.Status == taskstore.StatusCanceled {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.claim", protocol.ErrorCodeEARG, "canceled tasks cannot be claimed", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.Status != taskstore.StatusPending {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.claim", protocol.ErrorCodeEARG, "only pending tasks can be claimed", map[string]any{
				"status": task.Status,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if assigned := strings.TrimSpace(task.AssignedActorID); assigned != "" && !sameRoomParticipant(assigned, identity.Sender) {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.claim", protocol.ErrorCodeEARG, "task is assigned to another participant", map[string]any{
				"assigned_actor_id": task.AssignedActorID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID != "" && task.OwnerActorID != identity.Sender && task.Status != taskstore.StatusPending && task.Status != taskstore.StatusCanceled {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.claim", protocol.ErrorCodeEARG, "task is already claimed by another participant", map[string]any{
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
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.touch", protocol.ErrorCodeEARG, "task must be claimed before its heartbeat can be refreshed", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID != identity.Sender {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.touch", protocol.ErrorCodeEARG, "only the current owner can refresh this task heartbeat", map[string]any{
				"owner_actor_id": task.OwnerActorID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.Status != taskstore.StatusInProgress && task.Status != taskstore.StatusBlocked {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.touch", protocol.ErrorCodeEARG, "only in-progress or blocked tasks can be refreshed", map[string]any{
				"status": task.Status,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		task.HeartbeatAt = &now
	case "block":
		if strings.TrimSpace(reason) == "" {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.block", protocol.ErrorCodeEARG, "block reason is required", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID == "" {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.block", protocol.ErrorCodeEARG, "task must be claimed before it can be blocked", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID != identity.Sender {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.block", protocol.ErrorCodeEARG, "only the current owner can block this task", map[string]any{
				"owner_actor_id": task.OwnerActorID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.Status != taskstore.StatusInProgress {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.block", protocol.ErrorCodeEARG, "only in-progress tasks can be blocked", map[string]any{
				"status": task.Status,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		task.Status = taskstore.StatusBlocked
		task.BlockedReason = strings.TrimSpace(reason)
		task.BlockedAt = &now
		task.HeartbeatAt = &now
	case "unblock":
		if task.OwnerActorID == "" {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.unblock", protocol.ErrorCodeEARG, "task is not currently claimed", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID != identity.Sender {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.unblock", protocol.ErrorCodeEARG, "only the current owner can unblock this task", map[string]any{
				"owner_actor_id": task.OwnerActorID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.Status != taskstore.StatusBlocked {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.unblock", protocol.ErrorCodeEARG, "only blocked tasks can be unblocked", map[string]any{
				"status": task.Status,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		task.Status = taskstore.StatusInProgress
		task.BlockedReason = ""
		task.BlockedAt = nil
		task.HeartbeatAt = &now
	case "abandon":
		if task.Status == taskstore.StatusCompleted || task.Status == taskstore.StatusCanceled {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.abandon", protocol.ErrorCodeEARG, "completed or canceled tasks cannot be abandoned", map[string]any{
				"status": task.Status,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		if task.OwnerActorID != "" && task.OwnerActorID != identity.Sender {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task.abandon", protocol.ErrorCodeEARG, "only the current owner can abandon this task", map[string]any{
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
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task."+action, protocol.ErrorCodeEARG, "unsupported room task action", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if err := validateRoomTaskLifecycle(task); err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task."+action, protocol.ErrorCodeEARG, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	task, err = taskStore.Update(cmd.Context(), task)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task."+action, protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	out := map[string]any{
		"room_id":           strings.TrimSpace(roomID),
		"task":              task,
		"sender_identity":   identity,
		"notification_mode": "task_store_only",
	}
	return protocol.WriteOK(cmd.OutOrStdout(), "foxctl.room.task."+action, out, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
}

func validateRoomTaskLifecycle(task taskstore.Task) error {
	assigned := strings.TrimSpace(task.AssignedActorID)
	owner := strings.TrimSpace(task.OwnerActorID)
	blockedReason := strings.TrimSpace(task.BlockedReason)
	switch task.Status {
	case taskstore.StatusPending:
		if owner != "" || task.ClaimedAt != nil {
			return fmt.Errorf("pending task cannot retain an owner or claimed timestamp")
		}
		if blockedReason != "" || task.BlockedAt != nil {
			return fmt.Errorf("pending task cannot retain blocked state")
		}
		if task.CompletedAt != nil {
			return fmt.Errorf("pending task cannot retain completed state")
		}
	case taskstore.StatusInProgress:
		if owner == "" || task.ClaimedAt == nil {
			return fmt.Errorf("in-progress task requires an owner and claimed timestamp")
		}
		if assigned != "" && !sameRoomParticipant(assigned, owner) {
			return fmt.Errorf("in-progress task assignee must match current owner")
		}
		if blockedReason != "" || task.BlockedAt != nil {
			return fmt.Errorf("in-progress task cannot retain blocked state")
		}
		if task.CompletedAt != nil {
			return fmt.Errorf("in-progress task cannot retain completed state")
		}
	case taskstore.StatusBlocked:
		if owner == "" || task.ClaimedAt == nil {
			return fmt.Errorf("blocked task requires an owner and claimed timestamp")
		}
		if assigned != "" && !sameRoomParticipant(assigned, owner) {
			return fmt.Errorf("blocked task assignee must match current owner")
		}
		if blockedReason == "" || task.BlockedAt == nil {
			return fmt.Errorf("blocked task requires blocked reason and blocked timestamp")
		}
		if task.CompletedAt != nil {
			return fmt.Errorf("blocked task cannot retain completed state")
		}
	case taskstore.StatusCompleted:
		if task.CompletedAt == nil {
			return fmt.Errorf("completed task requires completed timestamp")
		}
		if owner != "" || task.ClaimedAt != nil {
			return fmt.Errorf("completed task cannot retain an owner or claimed timestamp")
		}
		if blockedReason != "" || task.BlockedAt != nil {
			return fmt.Errorf("completed task cannot retain blocked state")
		}
	case taskstore.StatusCanceled:
		if owner != "" || task.ClaimedAt != nil {
			return fmt.Errorf("canceled task cannot retain an owner or claimed timestamp")
		}
		if blockedReason != "" || task.BlockedAt != nil {
			return fmt.Errorf("canceled task cannot retain blocked state")
		}
	default:
		return fmt.Errorf("unsupported task status %q", task.Status)
	}
	return nil
}

type roomPulseConfig struct {
	Enabled                      bool
	Interval                     time.Duration
	TaskFollowupInterval         time.Duration
	ReplyStaleAfter              time.Duration
	TaskStaleAfter               time.Duration
	MinPulseFloor                time.Duration
	InterruptAttemptLimit        int
	ReminderBackoffCap           int
	CoordinatorPulseEnabled      bool
	CoordinatorEscalationEnabled bool
}

type roomLoopRuntimeState struct {
	DeliveryLeaseName       string
	DeliveryOwnerID         string
	DeliveryCursorMessageID string
	DeliveryCursorAt        *time.Time
	LastDeliveryTrace       *coordination.RoomLoopDeliveryTrace
	ReplyPulseState         map[string]roomPulseState
	TaskPulseState          map[string]roomPulseState
	TaskFollowupState       map[string]time.Time
	CoordinatorPulseState   map[string]time.Time
}

func runRoomLoop(cmd *cobra.Command, workspace, roomID string, relay roomRelayOptions, poll, taskPoll time.Duration, history int, pulse roomPulseConfig) error {
	absWorkspace, err := resolveRoomWorkspace(workspace)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer boardStore.Close()
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer taskStore.Close()
	coordStore, err := coordination.Open(cmd.Context(), cfg.Storage.Root)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer coordStore.Close()

	client := tmuxbridge.New()
	writer := envelope.NewWriter(cmd.OutOrStdout())
	seq := 0
	startedAt := time.Now().UTC()
	runtime := roomLoopRuntimeState{
		DeliveryLeaseName: roomLoopLeaseName(absWorkspace, roomID),
		DeliveryOwnerID:   roomLoopOwnerID(startedAt),
	}
	acquired, err := coordStore.TryAcquireLease(cmd.Context(), runtime.DeliveryLeaseName, runtime.DeliveryOwnerID, roomLoopLeaseTTL)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	if !acquired {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeEPolicy, fmt.Sprintf("room delivery owner already active for %q", roomID), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	defer func() {
		errs.Ignore(coordStore.ReleaseLease(context.Background(), runtime.DeliveryLeaseName, runtime.DeliveryOwnerID), "release room loop delivery lease")
	}()

	persistedLoop, err := syncRoomLoopState(cmd.Context(), coordStore, absWorkspace, roomID, pulse, startedAt, runtime)
	if err != nil {
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}
	pulse = roomPulseConfigFromStore(persistedLoop)
	runtime = roomLoopRuntimeStateFromStore(persistedLoop)

	summary, messages, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	seenMessages := roomLoopSeedSeenMessages(summary, messages, history, runtime)
	announcedStates := make(map[string]string)
	for _, msg := range messages {
		if msg.TaskID != "" {
			if task, err := taskStore.Get(cmd.Context(), msg.TaskID); err == nil {
				announcedStates[msg.TaskID] = task.Status
			}
		}
	}
	initial := roomLoopInitialMessages(summary, messages, history, runtime)
	for _, msg := range initial {
		seq++
		result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
		if err := persistRoomLoopRelayObservation(cmd.Context(), coordStore, absWorkspace, roomID, pulse, summary, msg, result, time.Now().UTC(), &runtime); err != nil {
			return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		}
		seenMessages[msg.ID] = struct{}{}
		if err := writer.Write(roomProgressEnvelope("foxctl.room.loop", seq, false, map[string]any{
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
		return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
	}

	messageTicker := time.NewTicker(normalizeRoomPoll(poll))
	defer messageTicker.Stop()
	taskTicker := time.NewTicker(normalizeRoomPoll(taskPoll))
	defer taskTicker.Stop()
	configTicker := time.NewTicker(5 * time.Second)
	defer configTicker.Stop()
	pulseTicker := resetRoomPulseTicker(nil, pulse)
	if pulseTicker != nil {
		defer pulseTicker.Stop()
	}
	for {
		select {
		case <-cmd.Context().Done():
			return writer.Write(protocol.OK("foxctl.room.loop", map[string]any{
				"status":  "stopped",
				"room_id": roomID,
			}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace), protocol.WithMetaMutator(func(m *envelope.Meta) {
				final := true
				m.Seq = &seq
				m.Final = &final
			})))
		case <-messageTicker.C:
			updatedPulse, updatedRuntime, pulseChanged, err := refreshRoomLoopPolicy(cmd.Context(), coordStore, absWorkspace, roomID, pulse, time.Now().UTC(), runtime)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			runtime = updatedRuntime
			if pulseChanged {
				pulse = updatedPulse
				pulseTicker = resetRoomPulseTicker(pulseTicker, pulse)
			}
			summary, current, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			for _, msg := range current {
				if _, ok := seenMessages[msg.ID]; ok {
					continue
				}
				if !roomLoopMessageAfterCursor(msg, runtime.DeliveryCursorAt, runtime.DeliveryCursorMessageID) {
					seenMessages[msg.ID] = struct{}{}
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
				if err := persistRoomLoopRelayObservation(cmd.Context(), coordStore, absWorkspace, roomID, pulse, summary, msg, result, time.Now().UTC(), &runtime); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				if err := writer.Write(roomProgressEnvelope("foxctl.room.loop", seq, false, map[string]any{
					"event":   "room_relay",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop relay envelope: %w", err)
				}
			}
		case <-taskTicker.C:
			updatedPulse, updatedRuntime, pulseChanged, err := refreshRoomLoopPolicy(cmd.Context(), coordStore, absWorkspace, roomID, pulse, time.Now().UTC(), runtime)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			runtime = updatedRuntime
			if pulseChanged {
				pulse = updatedPulse
				pulseTicker = resetRoomPulseTicker(pulseTicker, pulse)
			}
			updates, err := detectRoomTaskTransitions(cmd.Context(), taskStore, taskStates, announcedStates)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			loopRoomSummary, loopRoomErr := boardStore.GetRoom(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), "")
			var taskLoopRecipient string
			var taskLoopRecipientErr error
			if loopRoomErr == nil {
				taskLoopRecipient, taskLoopRecipientErr = roomTaskEventRecipient(loopRoomSummary.Members)
			}
			for _, update := range updates {
				announcedStates[update.Task.ID] = update.Task.Status
				if !roomLoopShouldRelayTaskTransition(relay.TaskEventMode, update) {
					continue
				}
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
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				seenMessages[msg.ID] = struct{}{}
				summary, _, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
				if err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				relayResult := relayRoomMessage(cmd.Context(), client, summary, *msg, relay)
				if err := persistRoomLoopRelayObservation(cmd.Context(), coordStore, absWorkspace, roomID, pulse, summary, *msg, relayResult, time.Now().UTC(), &runtime); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				seq++
				if err := writer.Write(roomProgressEnvelope("foxctl.room.loop", seq, false, map[string]any{
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
			updatedPulse, updatedRuntime, pulseChanged, err := refreshRoomLoopPolicy(cmd.Context(), coordStore, absWorkspace, roomID, pulse, time.Now().UTC(), runtime)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			runtime = updatedRuntime
			if pulseChanged {
				pulse = updatedPulse
				pulseTicker = resetRoomPulseTicker(pulseTicker, pulse)
			}
			summary, current, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			reminderMessages, err := processRoomReminderTick(cmd.Context(), coordStore, summary, current, time.Now().UTC())
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			for _, msg := range reminderMessages {
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := persistRoomLoopRelayObservation(cmd.Context(), coordStore, absWorkspace, roomID, pulse, summary, msg, result, time.Now().UTC(), &runtime); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				if err := writer.Write(roomProgressEnvelope("foxctl.room.loop", seq, false, map[string]any{
					"event":   "room_reminder",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop reminder envelope: %w", err)
				}
			}
		case <-roomPulseChan(pulseTicker):
			updatedPulse, updatedRuntime, pulseChanged, err := refreshRoomLoopPolicy(cmd.Context(), coordStore, absWorkspace, roomID, pulse, time.Now().UTC(), runtime)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			runtime = updatedRuntime
			if pulseChanged {
				pulse = updatedPulse
				pulseTicker = resetRoomPulseTicker(pulseTicker, pulse)
			}
			summary, current, err := loadRoomState(cmd.Context(), boardStore, absWorkspace, roomID, "", roomTaskScanLimit)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			reminders, err := loadRoomReminders(cmd.Context(), summary.WorkspaceID, roomID, true)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			roomTasks, err := listRoomTasks(cmd.Context(), taskStore, ws.CanonicalID(absWorkspace), current, "", false, false)
			if err != nil {
				return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
			}
			suppression := buildRoomActionSuppression(current, roomTasks, reminders)
			dirtyRuntime := false
			if cleanupRoomReplyPulseState(current, roomTasks, time.Now().UTC(), pulse, runtime.ReplyPulseState, suppression) {
				dirtyRuntime = true
			}
			if cleanupRoomTaskPulseState(roomTasks, time.Now().UTC(), pulse, runtime.TaskPulseState, suppression) {
				dirtyRuntime = true
			}
			if cleanupRoomTaskFollowupState(roomTasks, runtime.TaskFollowupState, suppression) {
				dirtyRuntime = true
			}
			for _, pulseMsg := range detectRoomPulseMessages(roomID, current, roomTasks, time.Now().UTC(), pulse, runtime.ReplyPulseState, suppression) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				markRoomPulseState(runtime.ReplyPulseState, pulseMsg.Key, msg.CreatedAt, false)
				dirtyRuntime = true
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := persistRoomLoopRelayObservation(cmd.Context(), coordStore, absWorkspace, roomID, pulse, summary, msg, result, time.Now().UTC(), &runtime); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				if err := writer.Write(roomProgressEnvelope("foxctl.room.loop", seq, false, map[string]any{
					"event":   "room_pulse",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop pulse envelope: %w", err)
				}
			}
			for _, pulseMsg := range detectRoomTaskFollowupMessages(summary, roomTasks, time.Now().UTC(), pulse, runtime.TaskFollowupState, suppression) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				runtime.TaskFollowupState[pulseMsg.Key] = msg.CreatedAt
				dirtyRuntime = true
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := persistRoomLoopRelayObservation(cmd.Context(), coordStore, absWorkspace, roomID, pulse, summary, msg, result, time.Now().UTC(), &runtime); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				if err := writer.Write(roomProgressEnvelope("foxctl.room.loop", seq, false, map[string]any{
					"event":   "task_followup",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop task follow-up envelope: %w", err)
				}
			}
			for _, pulseMsg := range detectRoomTaskPulseMessages(absWorkspace, roomID, roomTasks, time.Now().UTC(), pulse, runtime.TaskPulseState, suppression) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				markRoomPulseState(runtime.TaskPulseState, pulseMsg.Key, msg.CreatedAt, false)
				dirtyRuntime = true
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := persistRoomLoopRelayObservation(cmd.Context(), coordStore, absWorkspace, roomID, pulse, summary, msg, result, time.Now().UTC(), &runtime); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				if err := writer.Write(roomProgressEnvelope("foxctl.room.loop", seq, false, map[string]any{
					"event":   "task_pulse",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop task pulse envelope: %w", err)
				}
			}
			for _, pulseMsg := range detectRoomPulseEscalationMessages(summary, current, roomTasks, time.Now().UTC(), pulse, runtime.ReplyPulseState, suppression) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				markRoomPulseState(runtime.ReplyPulseState, pulseMsg.Key, msg.CreatedAt, true)
				dirtyRuntime = true
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := persistRoomLoopRelayObservation(cmd.Context(), coordStore, absWorkspace, roomID, pulse, summary, msg, result, time.Now().UTC(), &runtime); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				if err := writer.Write(roomProgressEnvelope("foxctl.room.loop", seq, false, map[string]any{
					"event":   "room_pulse_escalation",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop pulse escalation envelope: %w", err)
				}
			}
			for _, pulseMsg := range detectRoomTaskEscalationMessages(summary, roomTasks, time.Now().UTC(), pulse, runtime.TaskPulseState, suppression) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				markRoomPulseState(runtime.TaskPulseState, pulseMsg.Key, msg.CreatedAt, true)
				dirtyRuntime = true
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := persistRoomLoopRelayObservation(cmd.Context(), coordStore, absWorkspace, roomID, pulse, summary, msg, result, time.Now().UTC(), &runtime); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				if err := writer.Write(roomProgressEnvelope("foxctl.room.loop", seq, false, map[string]any{
					"event":   "task_pulse_escalation",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop task escalation envelope: %w", err)
				}
			}
			for _, pulseMsg := range detectRoomCoordinatorPulseMessages(summary, current, roomTasks, time.Now().UTC(), pulse, runtime.CoordinatorPulseState, suppression) {
				msg := pulseMsg.Message
				if err := boardStore.SendMessage(cmd.Context(), &msg); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				runtime.CoordinatorPulseState[pulseMsg.Key] = msg.CreatedAt
				dirtyRuntime = true
				seenMessages[msg.ID] = struct{}{}
				seq++
				result := relayRoomMessage(cmd.Context(), client, summary, msg, relay)
				if err := persistRoomLoopRelayObservation(cmd.Context(), coordStore, absWorkspace, roomID, pulse, summary, msg, result, time.Now().UTC(), &runtime); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
				}
				if err := writer.Write(roomProgressEnvelope("foxctl.room.loop", seq, false, map[string]any{
					"event":   "coordinator_pulse",
					"room_id": roomID,
					"message": msg,
					"relay":   result,
				}, absWorkspace)); err != nil {
					return fmt.Errorf("write room loop coordinator pulse envelope: %w", err)
				}
			}
			if dirtyRuntime {
				if err := persistRoomLoopRuntime(cmd.Context(), coordStore, absWorkspace, roomID, pulse, runtime); err != nil {
					return protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.loop", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
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
	if pulse.Interval <= 0 {
		pulse.Interval = roomLoopDefaultPulseInterval
	}
	if pulse.ReplyStaleAfter <= 0 {
		pulse.ReplyStaleAfter = roomLoopDefaultReplyStale
	}
	if pulse.TaskStaleAfter <= 0 {
		pulse.TaskStaleAfter = roomLoopDefaultTaskStale
	}
	if pulse.MinPulseFloor <= 0 {
		pulse.MinPulseFloor = roomLoopMinimumPulseFloor
	}
	if pulse.InterruptAttemptLimit <= 0 {
		pulse.InterruptAttemptLimit = roomPulseInterruptLimit
	}
	if pulse.ReminderBackoffCap <= 0 {
		pulse.ReminderBackoffCap = roomPulseBackoffCap
	}
	if !pulse.Enabled {
		pulse.Enabled = true
	}
	return coordination.RoomLoop{
		WorkspaceID:                  strings.TrimSpace(workspaceID),
		RoomID:                       strings.TrimSpace(roomID),
		Enabled:                      pulse.Enabled,
		ManagedBy:                    roomLoopManagedBy,
		PulseInterval:                pulse.Interval,
		TaskFollowupInterval:         pulse.TaskFollowupInterval,
		ReplyStaleAfter:              pulse.ReplyStaleAfter,
		TaskStaleAfter:               pulse.TaskStaleAfter,
		MinPulseFloor:                pulse.MinPulseFloor,
		InterruptAttemptLimit:        pulse.InterruptAttemptLimit,
		ReminderBackoffCap:           pulse.ReminderBackoffCap,
		CoordinatorPulseEnabled:      pulse.CoordinatorPulseEnabled,
		CoordinatorEscalationEnabled: pulse.CoordinatorEscalationEnabled,
	}
}

func roomLoopLeaseName(workspaceID, roomID string) string {
	return fmt.Sprintf("room-loop:%s:%s:delivery", ws.CanonicalWorkspaceKey(workspaceID), strings.TrimSpace(roomID))
}

func roomLoopOwnerID(now time.Time) string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s:%s:%d:%d", roomLoopManagedBy, strings.TrimSpace(host), os.Getpid(), now.UTC().UnixNano())
}

func roomLoopRuntimeStateFromStore(loop coordination.RoomLoop) roomLoopRuntimeState {
	return roomLoopRuntimeState{
		DeliveryLeaseName:       strings.TrimSpace(loop.DeliveryLeaseName),
		DeliveryOwnerID:         strings.TrimSpace(loop.DeliveryOwnerID),
		DeliveryCursorMessageID: strings.TrimSpace(loop.DeliveryCursorMessageID),
		DeliveryCursorAt:        loop.DeliveryCursorAt,
		LastDeliveryTrace:       loop.LastDeliveryTrace,
		ReplyPulseState:         coordinationPulseStatesToRuntime(loop.ReplyPulseState),
		TaskPulseState:          coordinationPulseStatesToRuntime(loop.TaskPulseState),
		TaskFollowupState:       copyRoomLoopTimeMap(loop.TaskFollowupState),
		CoordinatorPulseState:   copyRoomLoopTimeMap(loop.CoordinatorPulseState),
	}
}

func applyRoomLoopRuntimeState(loop *coordination.RoomLoop, runtime roomLoopRuntimeState) {
	if loop == nil {
		return
	}
	loop.DeliveryLeaseName = strings.TrimSpace(runtime.DeliveryLeaseName)
	loop.DeliveryOwnerID = strings.TrimSpace(runtime.DeliveryOwnerID)
	loop.DeliveryCursorMessageID = strings.TrimSpace(runtime.DeliveryCursorMessageID)
	loop.DeliveryCursorAt = runtime.DeliveryCursorAt
	loop.LastDeliveryTrace = runtime.LastDeliveryTrace
	loop.ReplyPulseState = runtimePulseStatesToCoordination(runtime.ReplyPulseState)
	loop.TaskPulseState = runtimePulseStatesToCoordination(runtime.TaskPulseState)
	loop.TaskFollowupState = copyRoomLoopTimeMap(runtime.TaskFollowupState)
	loop.CoordinatorPulseState = copyRoomLoopTimeMap(runtime.CoordinatorPulseState)
}

func updateRoomLoopLastDeliveryTrace(room agent.RoomSummary, msg agent.BoardMessage, relayResult roomRelayResult, before roomLoopRuntimeState, after roomLoopRuntimeState, observedAt time.Time) *coordination.RoomLoopDeliveryTrace {
	trace := &coordination.RoomLoopDeliveryTrace{
		WorkspaceID:           strings.TrimSpace(room.WorkspaceID),
		RoomID:                strings.TrimSpace(room.ID),
		MessageID:             strings.TrimSpace(msg.ID),
		TaskID:                strings.TrimSpace(msg.TaskID),
		Recipient:             normalizeRoomRecipient(msg.Recipient),
		DeliveryLeaseName:     strings.TrimSpace(after.DeliveryLeaseName),
		DeliveryOwnerID:       strings.TrimSpace(after.DeliveryOwnerID),
		RelayBackend:          strings.TrimSpace(relayResult.Backend),
		FallbackAttempted:     relayResult.FallbackAttempted,
		DeliveredCount:        relayResult.DeliveredCount,
		FailedCount:           relayResult.FailedCount,
		DeliveredTo:           append([]string(nil), relayResult.DeliveredTo...),
		FailedMembers:         append([]string(nil), relayResult.FailedMembers...),
		CursorBeforeMessageID: strings.TrimSpace(before.DeliveryCursorMessageID),
		CursorAfterMessageID:  strings.TrimSpace(after.DeliveryCursorMessageID),
		CursorAdvanced:        strings.TrimSpace(before.DeliveryCursorMessageID) != strings.TrimSpace(after.DeliveryCursorMessageID),
		ObservedAt:            observedAt.UTC(),
	}
	switch {
	case relayResult.DeliveredCount > 0 && relayResult.FailedCount > 0:
		trace.Outcome = "partial"
	case relayResult.DeliveredCount > 0:
		trace.Outcome = "delivered"
	case relayResult.FailedCount > 0 || strings.TrimSpace(relayResult.Error) != "":
		trace.Outcome = "failed"
	case len(relayResult.SkippedMembers) > 0:
		trace.Outcome = "skipped"
	default:
		trace.Outcome = "no_targets"
	}
	if recipient := normalizeRoomRecipient(msg.Recipient); recipient != "" && recipient != agent.BroadcastRecipient {
		for _, member := range room.Members {
			member = normalizeRoomMember(member)
			if !sameRoomParticipant(member.ActorID, recipient) {
				continue
			}
			trace.ChosenActorID = strings.TrimSpace(member.ActorID)
			if binding := member.DeliveryBinding; binding != nil {
				trace.ChosenMuxBackend = strings.TrimSpace(binding.MuxBackend)
				trace.ChosenMuxSession = strings.TrimSpace(binding.MuxSession)
				trace.ChosenMuxPaneID = strings.TrimSpace(binding.MuxPaneID)
				trace.ChosenTransportEndpoint = strings.TrimSpace(binding.TransportEndpoint)
				trace.ChosenTransportKind = strings.TrimSpace(binding.TransportKind)
			}
			trace.ChosenSubmitMode = roomMemberSubmitMode(member)
			break
		}
	}
	return trace
}

func persistRoomLoopRelayObservation(ctx context.Context, store *coordination.Store, workspaceID, roomID string, pulse roomPulseConfig, room agent.RoomSummary, msg agent.BoardMessage, relayResult roomRelayResult, observedAt time.Time, runtime *roomLoopRuntimeState) error {
	if runtime == nil {
		return nil
	}
	before := *runtime
	advanceRoomLoopCursor(runtime, msg)
	runtime.LastDeliveryTrace = updateRoomLoopLastDeliveryTrace(room, msg, relayResult, before, *runtime, observedAt)
	return persistRoomLoopRuntime(ctx, store, workspaceID, roomID, pulse, *runtime)
}

func coordinationPulseStatesToRuntime(states map[string]coordination.RoomLoopPulseState) map[string]roomPulseState {
	out := make(map[string]roomPulseState, len(states))
	for key, state := range states {
		var lastSentAt time.Time
		if state.LastSentAt != nil {
			lastSentAt = state.LastSentAt.UTC()
		}
		out[key] = roomPulseState{
			LastSentAt: lastSentAt,
			Count:      state.Count,
			Escalated:  state.Escalated,
		}
	}
	return out
}

func runtimePulseStatesToCoordination(states map[string]roomPulseState) map[string]coordination.RoomLoopPulseState {
	out := make(map[string]coordination.RoomLoopPulseState, len(states))
	for key, state := range states {
		var lastSentAt *time.Time
		if !state.LastSentAt.IsZero() {
			ts := state.LastSentAt.UTC()
			lastSentAt = &ts
		}
		out[key] = coordination.RoomLoopPulseState{
			LastSentAt: lastSentAt,
			Count:      state.Count,
			Escalated:  state.Escalated,
		}
	}
	return out
}

func copyRoomLoopTimeMap(states map[string]time.Time) map[string]time.Time {
	out := make(map[string]time.Time, len(states))
	for key, value := range states {
		out[key] = value.UTC()
	}
	return out
}

func syncRoomLoopState(ctx context.Context, store *coordination.Store, workspaceID, roomID string, current roomPulseConfig, tickAt time.Time, runtime roomLoopRuntimeState) (coordination.RoomLoop, error) {
	loop, err := store.GetRoomLoop(ctx, workspaceID, roomID)
	if err != nil {
		return coordination.RoomLoop{}, err
	}
	if loop == nil {
		seed := defaultRoomLoopPolicy(workspaceID, roomID, current)
		seed.LastTickAt = &tickAt
		applyRoomLoopRuntimeState(&seed, runtime)
		persisted, err := store.UpsertRoomLoop(ctx, seed)
		if err != nil {
			return coordination.RoomLoop{}, err
		}
		return persisted, nil
	}
	coerced := coerceRoomLoopPolicy(*loop)
	loop = &coerced
	loop.LastTickAt = &tickAt
	mergedRuntime := mergeRoomLoopRuntimeState(roomLoopRuntimeStateFromStore(*loop), runtime)
	applyRoomLoopRuntimeState(loop, mergedRuntime)
	persisted, err := store.UpsertRoomLoop(ctx, *loop)
	if err != nil {
		return coordination.RoomLoop{}, err
	}
	return persisted, nil
}

func mergeRoomLoopRuntimeState(base, override roomLoopRuntimeState) roomLoopRuntimeState {
	merged := base
	if v := strings.TrimSpace(override.DeliveryLeaseName); v != "" {
		merged.DeliveryLeaseName = v
	}
	if v := strings.TrimSpace(override.DeliveryOwnerID); v != "" {
		merged.DeliveryOwnerID = v
	}
	if v := strings.TrimSpace(override.DeliveryCursorMessageID); v != "" {
		merged.DeliveryCursorMessageID = v
	}
	if override.DeliveryCursorAt != nil && !override.DeliveryCursorAt.IsZero() {
		ts := override.DeliveryCursorAt.UTC()
		merged.DeliveryCursorAt = &ts
	}
	if override.LastDeliveryTrace != nil {
		merged.LastDeliveryTrace = override.LastDeliveryTrace
	}
	if len(override.ReplyPulseState) > 0 {
		merged.ReplyPulseState = override.ReplyPulseState
	}
	if len(override.TaskPulseState) > 0 {
		merged.TaskPulseState = override.TaskPulseState
	}
	if len(override.TaskFollowupState) > 0 {
		merged.TaskFollowupState = override.TaskFollowupState
	}
	if len(override.CoordinatorPulseState) > 0 {
		merged.CoordinatorPulseState = override.CoordinatorPulseState
	}
	return merged
}

func refreshRoomLoopPolicy(ctx context.Context, store *coordination.Store, workspaceID, roomID string, current roomPulseConfig, tickAt time.Time, runtime roomLoopRuntimeState) (roomPulseConfig, roomLoopRuntimeState, bool, error) {
	acquired, err := store.TryAcquireLease(ctx, runtime.DeliveryLeaseName, runtime.DeliveryOwnerID, roomLoopLeaseTTL)
	if err != nil {
		return roomPulseConfig{}, roomLoopRuntimeState{}, false, err
	}
	if !acquired {
		return roomPulseConfig{}, roomLoopRuntimeState{}, false, fmt.Errorf("room delivery owner already active for %q", roomID)
	}
	loop, err := syncRoomLoopState(ctx, store, workspaceID, roomID, current, tickAt, runtime)
	if err != nil {
		return roomPulseConfig{}, roomLoopRuntimeState{}, false, err
	}
	next := roomPulseConfigFromStore(loop)
	return next, roomLoopRuntimeStateFromStore(loop), !sameRoomPulseConfig(current, next), nil
}

func sameRoomPulseConfig(a, b roomPulseConfig) bool {
	return a.Enabled == b.Enabled &&
		a.Interval == b.Interval &&
		a.TaskFollowupInterval == b.TaskFollowupInterval &&
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
		TaskFollowupInterval:         loop.TaskFollowupInterval,
		ReplyStaleAfter:              loop.ReplyStaleAfter,
		TaskStaleAfter:               loop.TaskStaleAfter,
		MinPulseFloor:                loop.MinPulseFloor,
		InterruptAttemptLimit:        loop.InterruptAttemptLimit,
		ReminderBackoffCap:           loop.ReminderBackoffCap,
		CoordinatorPulseEnabled:      loop.CoordinatorPulseEnabled,
		CoordinatorEscalationEnabled: loop.CoordinatorEscalationEnabled,
	}
}

func coerceRoomLoopPolicy(loop coordination.RoomLoop) coordination.RoomLoop {
	if strings.TrimSpace(loop.ManagedBy) == "" {
		loop.ManagedBy = roomLoopManagedBy
	}
	if loop.PulseInterval <= 0 {
		loop.PulseInterval = roomLoopDefaultPulseInterval
	}
	if loop.ReplyStaleAfter <= 0 {
		loop.ReplyStaleAfter = roomLoopDefaultReplyStale
	}
	if loop.TaskStaleAfter <= 0 {
		loop.TaskStaleAfter = roomLoopDefaultTaskStale
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
	return loop
}

func roomLoopMessageAfterCursor(msg agent.BoardMessage, cursorAt *time.Time, cursorID string) bool {
	if cursorAt == nil || cursorAt.IsZero() {
		return true
	}
	at := cursorAt.UTC()
	if msg.CreatedAt.After(at) {
		return true
	}
	if msg.CreatedAt.Before(at) {
		return false
	}
	return strings.TrimSpace(msg.ID) > strings.TrimSpace(cursorID)
}

func roomLoopSeedSeenMessages(room agent.RoomSummary, messages []agent.BoardMessage, history int, runtime roomLoopRuntimeState) map[string]struct{} {
	seen := make(map[string]struct{}, len(messages))
	if runtime.DeliveryCursorAt == nil || runtime.DeliveryCursorAt.IsZero() {
		for _, msg := range roomLoopInitialCandidateMessages(messages, history, runtime) {
			if roomLoopShouldReplayInitialMessage(room, msg, messages) {
				continue
			}
			seen[msg.ID] = struct{}{}
		}
		return seen
	}
	for _, msg := range messages {
		if roomLoopMessageAfterCursor(msg, runtime.DeliveryCursorAt, runtime.DeliveryCursorMessageID) {
			continue
		}
		seen[msg.ID] = struct{}{}
	}
	for _, msg := range roomLoopInitialCandidateMessages(messages, history, runtime) {
		if roomLoopShouldReplayInitialMessage(room, msg, messages) {
			continue
		}
		seen[msg.ID] = struct{}{}
	}
	return seen
}

func roomLoopInitialMessages(room agent.RoomSummary, messages []agent.BoardMessage, history int, runtime roomLoopRuntimeState) []agent.BoardMessage {
	initial := roomLoopInitialCandidateMessages(messages, history, runtime)
	out := make([]agent.BoardMessage, 0, len(initial))
	for _, msg := range initial {
		if roomLoopShouldReplayInitialMessage(room, msg, messages) {
			out = append(out, msg)
		}
	}
	return out
}

func roomLoopInitialCandidateMessages(messages []agent.BoardMessage, history int, runtime roomLoopRuntimeState) []agent.BoardMessage {
	if runtime.DeliveryCursorAt == nil || runtime.DeliveryCursorAt.IsZero() {
		return trimRoomHistory(messages, history)
	}
	out := make([]agent.BoardMessage, 0, len(messages))
	for _, msg := range messages {
		if roomLoopMessageAfterCursor(msg, runtime.DeliveryCursorAt, runtime.DeliveryCursorMessageID) {
			out = append(out, msg)
		}
	}
	return out
}

func roomLoopShouldReplayInitialMessage(room agent.RoomSummary, msg agent.BoardMessage, messages []agent.BoardMessage) bool {
	recipient := normalizeRoomRecipient(msg.Recipient)
	if recipient == "" {
		return false
	}
	if recipient == agent.BroadcastRecipient {
		for actorID := range roomLoopCurrentParticipants(room) {
			if _, ok := roomInboxEntryForActor(actorID, msg, false, messages, nil); ok {
				return true
			}
		}
		return false
	}
	if !roomLoopCurrentParticipant(room, recipient) {
		return false
	}
	_, ok := roomInboxEntryForActor(recipient, msg, false, messages, nil)
	return ok
}

func roomLoopCurrentParticipants(room agent.RoomSummary) map[string]struct{} {
	out := make(map[string]struct{}, len(room.Participants)+len(room.Members))
	for _, member := range room.Members {
		if id := strings.TrimSpace(member.ActorID); id != "" && !strings.HasPrefix(id, "actor:system:room:") {
			out[id] = struct{}{}
		}
	}
	for _, participant := range room.Participants {
		if id := strings.TrimSpace(participant); id != "" && !strings.HasPrefix(id, "actor:system:room:") {
			out[id] = struct{}{}
		}
	}
	return out
}

func roomLoopCurrentParticipant(room agent.RoomSummary, actorID string) bool {
	_, ok := roomLoopCurrentParticipants(room)[strings.TrimSpace(actorID)]
	return ok
}

func advanceRoomLoopCursor(runtime *roomLoopRuntimeState, msg agent.BoardMessage) bool {
	if runtime == nil {
		return false
	}
	if !roomLoopMessageAfterCursor(msg, runtime.DeliveryCursorAt, runtime.DeliveryCursorMessageID) {
		return false
	}
	ts := msg.CreatedAt.UTC()
	runtime.DeliveryCursorAt = &ts
	runtime.DeliveryCursorMessageID = strings.TrimSpace(msg.ID)
	return true
}

func persistRoomLoopRuntime(ctx context.Context, store *coordination.Store, workspaceID, roomID string, pulse roomPulseConfig, runtime roomLoopRuntimeState) error {
	_, err := syncRoomLoopState(ctx, store, workspaceID, roomID, pulse, time.Now().UTC(), runtime)
	return err
}

func resetRoomPulseTicker(current *time.Ticker, pulse roomPulseConfig) *time.Ticker {
	if current != nil {
		current.Stop()
	}
	interval := roomPulseTickerInterval(pulse)
	if !roomPulseEnabled(pulse) || interval <= 0 {
		return nil
	}
	return time.NewTicker(interval)
}

func roomPulseEnabled(cfg roomPulseConfig) bool {
	if cfg.Enabled {
		return true
	}
	return cfg.Interval > 0 || cfg.TaskFollowupInterval > 0 || cfg.ReplyStaleAfter > 0 || cfg.TaskStaleAfter > 0 || cfg.CoordinatorPulseEnabled
}

func roomPulseTickerInterval(cfg roomPulseConfig) time.Duration {
	var interval time.Duration
	for _, candidate := range []time.Duration{cfg.Interval, cfg.TaskFollowupInterval} {
		if candidate <= 0 {
			continue
		}
		if interval <= 0 || candidate < interval {
			interval = candidate
		}
	}
	return interval
}

type roomTaskTransition struct {
	Task           taskstore.Task
	PreviousStatus string
	CurrentStatus  string
	Subject        string
	Body           string
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
	storyViews := buildRoomStoryViews(messages)
	milestoneViews := buildRoomMilestoneViews(messages)
	var taskStore taskstore.Store
	for _, reminder := range reminders {
		if strings.TrimSpace(reminder.TaskID) == "" {
			continue
		}
		taskStore, err = openRoomTaskStore(ctx)
		if err != nil {
			return nil, err
		}
		defer taskStore.Close()
		break
	}
	out := make([]agent.BoardMessage, 0, len(reminders))
	for _, reminder := range reminders {
		if !reminder.Active {
			continue
		}
		satisfied, err := roomReminderSatisfied(ctx, taskStore, reminder, messages, storyViews, milestoneViews)
		if err != nil {
			return nil, err
		}
		if satisfied || reminder.SentCount >= reminder.MaxIterations {
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

func roomReminderSatisfied(ctx context.Context, taskStore taskstore.Store, reminder coordination.RoomReminder, messages []agent.BoardMessage, storyViews, milestoneViews []map[string]any) (bool, error) {
	if roomReminderLinkedWorkSatisfied(ctx, taskStore, reminder, messages, storyViews, milestoneViews) {
		return true, nil
	}
	rootMessageID := strings.TrimSpace(reminder.RootMessageID)
	if rootMessageID == "" {
		return true, nil
	}
	var root *agent.BoardMessage
	for i := range messages {
		if strings.TrimSpace(messages[i].ID) == rootMessageID {
			root = &messages[i]
			break
		}
	}
	if root == nil {
		return true, nil
	}
	if root.Status == agent.BoardMessageStatusAcked || root.Status == agent.BoardMessageStatusRead {
		return true, nil
	}
	if root.ReplyExpected && !messageStillAwaitsReply(*root, messages) {
		return true, nil
	}
	return false, nil
}

func roomReminderLinkedWorkSatisfied(ctx context.Context, taskStore taskstore.Store, reminder coordination.RoomReminder, messages []agent.BoardMessage, storyViews, milestoneViews []map[string]any) bool {
	if taskID := strings.TrimSpace(reminder.TaskID); taskID != "" && taskStore != nil {
		task, err := taskStore.Get(ctx, taskID)
		if err == nil {
			return task.Status == taskstore.StatusCompleted || task.Status == taskstore.StatusCanceled
		}
		if !errors.Is(err, sql.ErrNoRows) && !dbutil.IsNoRows(err) {
			return false
		}
	}
	if storyID := strings.TrimSpace(reminder.StoryID); storyID != "" {
		if story := roomStoryViewByID(storyViews, storyID); story != nil {
			switch stringField(story, "state") {
			case "done", "waived", "deferred":
				return true
			}
		}
	}
	if milestoneID := strings.TrimSpace(reminder.MilestoneID); milestoneID != "" {
		if milestone := roomMilestoneViewByID(milestoneViews, milestoneID); milestone != nil {
			return mapField(milestone, "latest_summary") != nil || intField(milestone, "summary_count") > 0
		}
	}
	_ = messages
	return false
}

func buildRoomReminderMessage(room agent.RoomSummary, reminder coordination.RoomReminder, now time.Time) agent.BoardMessage {
	subject := fmt.Sprintf("Reminder (%d/%d): %s", reminder.SentCount+1, reminder.MaxIterations, strings.TrimSpace(reminder.Subject))
	body := fmt.Sprintf("Scheduled follow-up for message %s.\nOriginal sender: %s\nOriginal request: %s", strings.TrimSpace(reminder.RootMessageID), strings.TrimSpace(reminder.Sender), strings.TrimSpace(reminder.Subject))
	if strings.TrimSpace(reminder.Body) != "" && strings.TrimSpace(reminder.Body) != strings.TrimSpace(reminder.Subject) {
		body += "\nOriginal body: " + strings.TrimSpace(reminder.Body)
	}
	body += fmt.Sprintf("\nReminder iteration: %d of %d", reminder.SentCount+1, reminder.MaxIterations)
	interrupt := reminder.Interrupt
	if sameRoomParticipant(strings.TrimSpace(reminder.Sender), strings.TrimSpace(reminder.Recipient)) {
		interrupt = false
	}
	return agent.BoardMessage{
		WorkspaceID:      room.WorkspaceID,
		RelatedMessageID: strings.TrimSpace(reminder.RootMessageID),
		Stream:           room.Stream,
		Sender:           roomLoopSender(room.ID),
		Recipient:        strings.TrimSpace(reminder.Recipient),
		Kind:             agent.BoardMessageKindAlert,
		Priority:         2,
		Interrupt:        interrupt,
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

func cleanupRoomReplyPulseState(messages []agent.BoardMessage, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, states map[string]roomPulseState, suppression *roomActionSuppression) bool {
	_ = tasks
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
		if msg.ReplyExpected && !messageStillAwaitsReply(msg, messages) {
			continue
		}
		if roomActionSuppressesMessage(msg, suppression) {
			continue
		}
		if now.Sub(msg.CreatedAt) < cfg.ReplyStaleAfter {
			continue
		}
		active[msg.ID] = struct{}{}
	}
	dirty := false
	for key := range states {
		if _, ok := active[key]; !ok {
			delete(states, key)
			dirty = true
		}
	}
	return dirty
}

func cleanupRoomTaskPulseState(tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, states map[string]roomPulseState, suppression *roomActionSuppression) bool {
	active := make(map[string]struct{})
	for _, task := range tasks {
		if roomActionSuppressesTask(task, suppression) {
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
		active[task.ID] = struct{}{}
	}
	dirty := false
	for key := range states {
		if _, ok := active[key]; !ok {
			delete(states, key)
			dirty = true
		}
	}
	return dirty
}

func cleanupRoomTaskFollowupState(tasks []taskstore.Task, states map[string]time.Time, suppression *roomActionSuppression) bool {
	active := make(map[string]struct{})
	for _, task := range tasks {
		if roomActionSuppressesTask(task, suppression) {
			continue
		}
		if task.OwnerActorID == "" {
			continue
		}
		if task.Status != taskstore.StatusInProgress && task.Status != taskstore.StatusBlocked {
			continue
		}
		active[task.ID] = struct{}{}
	}
	dirty := false
	for key := range states {
		if _, ok := active[key]; !ok {
			delete(states, key)
			dirty = true
		}
	}
	return dirty
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
			Task:           task,
			PreviousStatus: previousStatus,
			CurrentStatus:  currentStatus,
			Subject:        roomTaskStatusSubject(task, previousStatus),
			Body:           roomTaskStatusBody(task, previousStatus),
		})
	}
	return out, nil
}

func roomLoopShouldRelayTaskTransition(mode string, update roomTaskTransition) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "quiet":
		return false
	case "verbose":
		return update.PreviousStatus != update.CurrentStatus
	default:
		return (update.PreviousStatus == taskstore.StatusPending && update.CurrentStatus == taskstore.StatusInProgress) ||
			update.CurrentStatus == taskstore.StatusCompleted
	}
}

func detectRoomPulseMessages(roomID string, messages []agent.BoardMessage, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]roomPulseState, suppression *roomActionSuppression) []roomPulseMessage {
	_ = tasks
	if !roomPulseEnabled(cfg) || cfg.ReplyStaleAfter <= 0 {
		return nil
	}
	reminderFloor := cfg.ReplyStaleAfter
	if cfg.MinPulseFloor > reminderFloor {
		reminderFloor = cfg.MinPulseFloor
	}
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
		if msg.ReplyExpected && !messageStillAwaitsReply(msg, messages) {
			continue
		}
		if roomActionSuppressesMessage(msg, suppression) {
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
				Interrupt:        !sameRoomParticipant(strings.TrimSpace(msg.Sender), recipient),
				Subject:          subject,
				Body:             body,
				CreatedAt:        now,
			},
		})
	}
	return out
}

func detectRoomPulseEscalationMessages(room agent.RoomSummary, messages []agent.BoardMessage, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]roomPulseState, suppression *roomActionSuppression) []roomPulseMessage {
	_ = tasks
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
	for _, msg := range messages {
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
		if msg.ReplyExpected && !messageStillAwaitsReply(msg, messages) {
			continue
		}
		if roomActionSuppressesMessage(msg, suppression) {
			continue
		}
		subject := fmt.Sprintf("Escalation: repeated unanswered request for %s", recipient)
		body := fmt.Sprintf("Message %s to %s has already triggered %d interrupting reminders with no acknowledgement or reply. Consider updating loop policy, resolving the request, or reclaiming the work.\nOriginal sender: %s\nOriginal subject: %s", msg.ID, recipient, state.Count, strings.TrimSpace(msg.Sender), strings.TrimSpace(msg.Subject))
		out = append(out, roomPulseMessage{Key: msg.ID, Message: agent.BoardMessage{WorkspaceID: room.WorkspaceID, TaskID: msg.TaskID, RelatedMessageID: roomMessageChainKey(msg), Stream: room.Stream, Sender: roomLoopSender(room.ID), Recipient: coordinator, Kind: agent.BoardMessageKindAlert, Priority: 1, Interrupt: true, Subject: subject, Body: body, CreatedAt: now}})
	}
	return out
}

func detectRoomTaskPulseMessages(workspace, roomID string, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]roomPulseState, suppression *roomActionSuppression) []roomPulseMessage {
	if !roomPulseEnabled(cfg) || cfg.TaskStaleAfter <= 0 {
		return nil
	}
	reminderFloor := cfg.TaskStaleAfter
	if cfg.MinPulseFloor > reminderFloor {
		reminderFloor = cfg.MinPulseFloor
	}
	out := make([]roomPulseMessage, 0)
	for _, task := range tasks {
		if roomActionSuppressesTask(task, suppression) {
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

func detectRoomTaskFollowupMessages(room agent.RoomSummary, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]time.Time, suppression *roomActionSuppression) []roomPulseMessage {
	if !roomPulseEnabled(cfg) || cfg.TaskFollowupInterval <= 0 {
		return nil
	}
	coordinator := roomCoordinatorActorID(room.Members)
	if coordinator == "" {
		coordinator = "@coordinator"
	}
	out := make([]roomPulseMessage, 0)
	for _, task := range tasks {
		if roomActionSuppressesTask(task, suppression) {
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
		if now.Sub(reference) < cfg.TaskFollowupInterval {
			continue
		}
		if last, ok := reminded[task.ID]; ok && now.Sub(last) < cfg.TaskFollowupInterval {
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

func detectRoomTaskEscalationMessages(room agent.RoomSummary, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]roomPulseState, suppression *roomActionSuppression) []roomPulseMessage {
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
		if roomActionSuppressesTask(task, suppression) {
			continue
		}
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

func latestRoomDefaultChoresMilestone(messages []agent.BoardMessage) (string, string) {
	for _, epic := range buildRoomEpicViews(messages) {
		if boolField(epic, "closed") {
			continue
		}
		epicID := strings.TrimSpace(stringField(epic, "id"))
		milestoneID := strings.TrimSpace(stringField(epic, "default_chores_milestone_id"))
		if epicID == "" || milestoneID == "" {
			continue
		}
		return epicID, milestoneID
	}
	return "", ""
}

func resolveRoomTaskMilestoneSelection(messages []agent.BoardMessage, selectedMilestoneID string) (string, string, error) {
	selectedMilestoneID = strings.TrimSpace(selectedMilestoneID)
	if selectedMilestoneID == "" {
		epicID, milestoneID := latestRoomDefaultChoresMilestone(messages)
		return epicID, milestoneID, nil
	}
	milestone := roomMilestoneViewByID(buildRoomMilestoneViews(messages), selectedMilestoneID)
	if milestone == nil {
		return "", "", fmt.Errorf("%w: %s", errRoomTaskMilestoneNotFound, selectedMilestoneID)
	}
	if mapField(milestone, "latest_summary") != nil || intField(milestone, "summary_count") > 0 {
		return "", "", fmt.Errorf("milestone %q is already summarized", selectedMilestoneID)
	}
	epicID := strings.TrimSpace(stringField(milestone, "epic_id"))
	if epicID == "" {
		return "", "", fmt.Errorf("milestone %q is missing epic linkage", selectedMilestoneID)
	}
	if epic := roomEpicViewByID(buildRoomEpicViews(messages), epicID); epic != nil && boolField(epic, "closed") {
		return "", "", fmt.Errorf("milestone %q belongs to a closed epic", selectedMilestoneID)
	}
	return epicID, selectedMilestoneID, nil
}

func detectRoomCoordinatorPulseMessages(room agent.RoomSummary, messages []agent.BoardMessage, tasks []taskstore.Task, now time.Time, cfg roomPulseConfig, reminded map[string]time.Time, suppression *roomActionSuppression) []roomPulseMessage {
	if !roomPulseEnabled(cfg) || !cfg.CoordinatorPulseEnabled || cfg.Interval <= 0 {
		return nil
	}
	coordinator := roomCoordinatorActorID(room.Members)
	if coordinator == "" {
		return nil
	}
	backlog := buildRoomStatusBacklog(room, messages, suppression)
	taskPulse := buildRoomTaskPulseSummary(tasks, now, cfg.TaskStaleAfter, suppression)
	action := buildRoomStatusActionRequired(room, messages, tasks, backlog, taskPulse, map[string]struct{}{"all": {}}, cfg.TaskStaleAfter, now, false, suppression)
	if action.ParticipantsWithPending == 0 && action.AssignedUnclaimed == 0 && action.BlockedTasks == 0 && action.StaleTasks == 0 {
		return nil
	}
	key := fmt.Sprintf("%s|%d|%d|%d|%d|%d|%d", coordinator, action.ParticipantsWithPending, action.PendingAcks, action.PendingReplies, action.AssignedUnclaimed, action.BlockedTasks, action.StaleTasks)
	if _, ok := reminded[key]; ok {
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
			Interrupt:   false,
			Subject:     subject,
			Body:        body,
			CreatedAt:   now,
		},
	}}
}

func findEquivalentActiveReminder(reminders []coordination.RoomReminder, candidate coordination.RoomReminder) *coordination.RoomReminder {
	for i := range reminders {
		reminder := reminders[i]
		if !reminder.Active {
			continue
		}
		if !sameRoomReminderContract(reminder, candidate) {
			continue
		}
		dup := reminder
		return &dup
	}
	return nil
}

func sameRoomReminderContract(a, b coordination.RoomReminder) bool {
	return ws.CanonicalID(strings.TrimSpace(a.WorkspaceID)) == ws.CanonicalID(strings.TrimSpace(b.WorkspaceID)) &&
		strings.TrimSpace(a.RoomID) == strings.TrimSpace(b.RoomID) &&
		strings.TrimSpace(a.Recipient) == strings.TrimSpace(b.Recipient) &&
		strings.TrimSpace(a.Subject) == strings.TrimSpace(b.Subject) &&
		strings.TrimSpace(a.Body) == strings.TrimSpace(b.Body) &&
		strings.TrimSpace(a.TaskID) == strings.TrimSpace(b.TaskID) &&
		strings.TrimSpace(a.StoryID) == strings.TrimSpace(b.StoryID) &&
		strings.TrimSpace(a.MilestoneID) == strings.TrimSpace(b.MilestoneID) &&
		a.AckRequired == b.AckRequired &&
		a.ReplyExpected == b.ReplyExpected &&
		a.Passive == b.Passive &&
		a.Interrupt == b.Interrupt
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
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task", protocol.ErrorCodeEARG, err.Error(), map[string]any{
			"hint": "Pass --sender when outside tmux/zellij, or run inside a prepared pane so foxctl can derive the participant id.",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, nil, nil, writeErr
	}
	boardStore, err := openRoomBoardStore(cmd.Context())
	if err != nil {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, nil, nil, writeErr
	}
	summary, err := boardStore.GetRoom(cmd.Context(), absWorkspace, strings.TrimSpace(roomID), "")
	if err != nil {
		code := protocol.ErrorCodeERuntime
		if errors.Is(err, blackboard.ErrRoomNotFound) {
			code = protocol.ErrorCodeENotFound
		}
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task", code, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, boardStore, nil, writeErr
	}
	if !roomMemberCanManageRoomTasks(summary.Members, identity.Sender) {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task", protocol.ErrorCodeEARG, "only room coordinators, room admins, or system admins may perform this action", map[string]any{
			"sender": identity.Sender,
			"hint":   "Reassign/reclaim require the same privilege as assign (coordinator, room admin, or system admin).",
		}, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, boardStore, nil, writeErr
	}
	taskStore, err := openRoomTaskStore(cmd.Context())
	if err != nil {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task", protocol.ErrorCodeERuntime, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, boardStore, nil, writeErr
	}
	task, err := taskStore.Get(cmd.Context(), strings.TrimSpace(taskID))
	if err != nil {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task", protocol.ErrorCodeENotFound, err.Error(), nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, boardStore, taskStore, writeErr
	}
	if task.WorkspaceID != ws.CanonicalID(absWorkspace) {
		writeErr := protocol.WriteError(cmd.OutOrStdout(), "foxctl.room.task", protocol.ErrorCodeEARG, "task does not belong to this workspace", nil, protocol.WithSource("cli"), protocol.WithWorkspace(absWorkspace))
		return "", roomIdentity{}, agent.RoomSummary{}, taskstore.Task{}, boardStore, taskStore, writeErr
	}
	return absWorkspace, identity, summary, task, boardStore, taskStore, nil
}

func formatRoomTaskAddedBody(task taskstore.Task) string {
	lines := []string{
		fmt.Sprintf("Task ID: %s", task.ID),
		fmt.Sprintf("Status: %s", task.Status),
	}
	if strings.TrimSpace(task.EpicID) != "" {
		lines = append(lines, fmt.Sprintf("Epic ID: %s", strings.TrimSpace(task.EpicID)))
	}
	if strings.TrimSpace(task.MilestoneID) != "" {
		lines = append(lines, fmt.Sprintf("Milestone ID: %s", strings.TrimSpace(task.MilestoneID)))
	}
	if strings.TrimSpace(task.Description) != "" {
		lines = append(lines, task.Description)
	}
	return strings.Join(lines, "\n")
}

func appendRoomTaskOperatorTip(body, roomID, taskID, sender string) string {
	lines := []string{strings.TrimSpace(body)}
	tip := []string{
		"Quick tip:",
		"- Read the `foxctl-room-operator` skill if you need the room protocol.",
		fmt.Sprintf("- Claim with: foxctl room task claim %s --id %s", roomID, taskID),
		fmt.Sprintf("- Reply durably to the coordinator with: foxctl room send %s --to %s \"status update\"", roomID, sender),
		fmt.Sprintf("- Send room-wide updates with: foxctl room send %s \"team update\"", roomID),
		fmt.Sprintf("- Complete with: foxctl room task complete %s --id %s --notes \"what changed\"", roomID, taskID),
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
			Provider:      agentCLI,
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
