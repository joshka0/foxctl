package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Action result messages
type (
	jobCancelledMsg struct {
		jobID string
		err   error
	}
	taskCompletedMsg struct {
		taskID string
		err    error
	}
	taskSetActiveMsg struct {
		taskID string
		err    error
	}
	messageAckedMsg struct {
		messageID string
		err       error
	}
	reservationReleasedMsg struct {
		path string
		err  error
	}
)

// cancelJobCmd cancels a job by ID
func cancelJobCmd(jobID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "agentctl", "jobs", "cancel", jobID)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return jobCancelledMsg{jobID: jobID, err: fmt.Errorf("%w: %s", err, string(output))}
		}
		return jobCancelledMsg{jobID: jobID, err: nil}
	}
}

// completeTaskCmd marks a task as complete
func completeTaskCmd(workspace, taskID string) tea.Cmd {
	return func() tea.Msg {
		workspace = getWorkspace(workspace)
		input, err := json.Marshal(map[string]any{
			"operation":    "complete",
			"workspace_id": workspace,
			"task_id":      taskID,
		})
		if err != nil {
			return taskCompletedMsg{taskID: taskID, err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "agentctl", "run", "todo/manage", "--input", string(input))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return taskCompletedMsg{taskID: taskID, err: fmt.Errorf("%w: %s", err, string(output))}
		}
		return taskCompletedMsg{taskID: taskID, err: nil}
	}
}

// setActiveTaskCmd sets a task as the active task
func setActiveTaskCmd(workspace, taskID string) tea.Cmd {
	return func() tea.Msg {
		workspace = getWorkspace(workspace)
		input, err := json.Marshal(map[string]any{
			"operation":    "set_active",
			"workspace_id": workspace,
			"task_id":      taskID,
		})
		if err != nil {
			return taskSetActiveMsg{taskID: taskID, err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "agentctl", "run", "todo/manage", "--input", string(input))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return taskSetActiveMsg{taskID: taskID, err: fmt.Errorf("%w: %s", err, string(output))}
		}
		return taskSetActiveMsg{taskID: taskID, err: nil}
	}
}

// ackMessageCmd acknowledges a message
func ackMessageCmd(workspace, actorID, messageID string) tea.Cmd {
	return func() tea.Msg {
		workspace = getWorkspace(workspace)
		input, err := json.Marshal(map[string]any{
			"operation":    "ack",
			"workspace_id": workspace,
			"ack": map[string]any{
				"actor_id":    actorID,
				"message_ids": []string{messageID},
			},
		})
		if err != nil {
			return messageAckedMsg{messageID: messageID, err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "agentctl", "run", "mailbox/manage", "--input", string(input))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return messageAckedMsg{messageID: messageID, err: fmt.Errorf("%w: %s", err, string(output))}
		}
		return messageAckedMsg{messageID: messageID, err: nil}
	}
}

// releaseReservationCmd releases a file reservation
func releaseReservationCmd(workspace, holder, path string) tea.Cmd {
	return func() tea.Msg {
		workspace = getWorkspace(workspace)
		input, err := json.Marshal(map[string]any{
			"operation":    "release",
			"workspace_id": workspace,
			"release": map[string]any{
				"holder": holder,
				"path":   path,
			},
		})
		if err != nil {
			return reservationReleasedMsg{path: path, err: err}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		cmd := exec.CommandContext(ctx, "agentctl", "run", "mailbox/manage", "--input", string(input))
		output, err := cmd.CombinedOutput()
		if err != nil {
			return reservationReleasedMsg{path: path, err: fmt.Errorf("%w: %s", err, string(output))}
		}
		return reservationReleasedMsg{path: path, err: nil}
	}
}
