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
		newRoomTaskCompleteCommand(),
	)
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

func newRoomLoopCommand() *cobra.Command {
	var (
		workspace string
		backend   string
		session   string
		plugin    string
		poll      time.Duration
		taskPoll  time.Duration
		history   int
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
			}, poll, taskPoll, history)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", ".", "Workspace root override")
	cmd.Flags().StringVar(&backend, "backend", "tmux", "Terminal backend (tmux|zellij)")
	cmd.Flags().StringVar(&session, "session", "", "Zellij session name (defaults to ZELLIJ_SESSION_NAME when inside zellij)")
	cmd.Flags().StringVar(&plugin, "plugin-path", "", "Path to the zellij room relay plugin wasm")
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

	now := time.Now().UTC()
	task.Status = taskstore.StatusCompleted
	task.CompletedAt = &now
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

func runRoomLoop(cmd *cobra.Command, workspace, roomID string, relay roomRelayOptions, poll, taskPoll time.Duration, history int) error {
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
		}
	}
}

type roomTaskTransition struct {
	Task    taskstore.Task
	Subject string
	Body    string
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

func roomLoopSender(roomID string) string {
	return "actor:system:room:" + strings.TrimSpace(roomID)
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
	if strings.TrimSpace(task.Notes) != "" {
		lines = append(lines, "Notes: "+task.Notes)
	}
	return strings.Join(lines, "\n")
}
