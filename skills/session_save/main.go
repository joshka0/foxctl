// Package main implements the session/save skill for capturing session state before compaction.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/plans"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

// Input defines the skill input parameters.
type Input struct {
	Trigger   string `json:"trigger"`   // "pre_compact", "manual", "session_end"
	Workspace string `json:"workspace"` // Project path
	SessionID string `json:"session_id,omitempty"`
	Summary   string `json:"summary,omitempty"` // Optional user-provided summary
}

// SessionSnapshot represents the captured session state.
type SessionSnapshot struct {
	SnapshotID   string            `json:"snapshot_id"`
	SessionID    string            `json:"session_id,omitempty"`
	Trigger      string            `json:"trigger"`
	Workspace    string            `json:"workspace"`
	Timestamp    time.Time         `json:"timestamp"`
	ActiveTask   *TaskInfo         `json:"active_task,omitempty"`
	ActivePlan   *PlanInfo         `json:"active_plan,omitempty"`
	PendingTodos []TaskInfo        `json:"pending_todos,omitempty"`
	Decisions    []string          `json:"decisions,omitempty"`
	Insights     []string          `json:"insights,omitempty"`
	Summary      string            `json:"summary,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// PlanInfo represents a simplified plan for the snapshot.
type PlanInfo struct {
	FilePath    string   `json:"file_path"`
	FileName    string   `json:"file_name"`
	Title       string   `json:"title"`
	ContentHash string   `json:"content_hash"`
	Sections    []string `json:"sections,omitempty"`     // Top-level section titles
	LinkedTasks int      `json:"linked_tasks,omitempty"` // Number of tasks linked to this plan
	ModTime     string   `json:"mod_time,omitempty"`     // ISO format
}

// TaskInfo represents a simplified task for the snapshot.
type TaskInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	Notes       string `json:"notes,omitempty"`
	Gotchas     string `json:"gotchas,omitempty"`
}

// Output defines the skill output.
type Output struct {
	SnapshotID    string         `json:"snapshot_id"`
	ItemsCaptured map[string]int `json:"items_captured"`
	Message       string         `json:"message"`
}

const command = "session/save"

func main() {
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("EPARSE", fmt.Errorf("decode input: %w", err))
	}

	// Default workspace to current directory
	if input.Workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			input.Workspace = wd
		}
	}

	// Default trigger
	if input.Trigger == "" {
		input.Trigger = "manual"
	}

	// Get agentctl home
	home := os.Getenv("AGENTCTL_HOME")
	if home == "" {
		homeDir, _ := os.UserHomeDir()
		home = filepath.Join(homeDir, ".agentctl")
	}

	// Open stores - memory uses cache path (matches CLI), tasks uses storage
	cachePath := filepath.Join(home, "cache")
	storageRoot := filepath.Join(home, "storage")
	casPath := filepath.Join(home, "cas")

	memStore, err := memory.Open(ctx, cachePath, casPath)
	if err != nil {
		fail("EIO", fmt.Errorf("open memory store: %w", err))
	}
	defer func() { errs.Ignore(memStore.Close(), "close memory store") }()

	taskStore, err := tasks.Open(ctx, storageRoot)
	if err != nil {
		fail("EIO", fmt.Errorf("open task store: %w", err))
	}
	defer func() { errs.Ignore(taskStore.Close(), "close task store") }()

	// Build snapshot
	snapshot := SessionSnapshot{
		SnapshotID: fmt.Sprintf("snap-%d", timeutil.NowUTC().UnixMilli()),
		SessionID:  input.SessionID,
		Trigger:    input.Trigger,
		Workspace:  input.Workspace,
		Timestamp:  timeutil.NowUTC(),
		Summary:    input.Summary,
		Metadata:   make(map[string]string),
	}

	itemsCaptured := make(map[string]int)

	// Capture active task
	activeTask, found, err := taskStore.GetActive(ctx, input.Workspace)
	if err == nil && found && activeTask.ID != "" {
		snapshot.ActiveTask = &TaskInfo{
			ID:          activeTask.ID,
			Title:       activeTask.Title,
			Description: activeTask.Description,
			Status:      activeTask.Status,
			Notes:       activeTask.Notes,
			Gotchas:     activeTask.Gotchas,
		}
		itemsCaptured["active_task"] = 1
	}

	// Capture active plan from ~/.claude/plans/
	// First check if the active task is linked to a plan
	var activePlanFile string
	if snapshot.ActiveTask != nil && activeTask.PlanFile != "" {
		activePlanFile = activeTask.PlanFile
	}

	// If no plan linked to active task, try to detect the most recently modified plan
	homeDir, _ := os.UserHomeDir()
	plansDir := filepath.Join(homeDir, ".claude", "plans")
	detector := plans.NewDetector(plansDir)

	var activePlan *plans.PlanInfo
	if activePlanFile != "" {
		// Parse the linked plan
		parser := plans.NewParser(plans.DefaultParseOptions())
		parsed, parseErr := parser.ParseFile(activePlanFile)
		if parseErr == nil {
			activePlan = parsed
		}
	} else {
		// Detect most recently modified plan
		mostRecent, detectErr := detector.DetectMostRecent()
		if detectErr == nil && mostRecent != nil {
			activePlan = mostRecent
		}
	}

	// Add plan to snapshot if found
	if activePlan != nil {
		// Extract top-level section titles
		var sectionTitles []string
		for _, sec := range activePlan.Sections {
			sectionTitles = append(sectionTitles, sec.Title)
		}

		// Count tasks linked to this plan
		linkedTasks, _ := taskStore.ListByPlanFile(ctx, activePlan.FilePath)

		snapshot.ActivePlan = &PlanInfo{
			FilePath:    activePlan.FilePath,
			FileName:    activePlan.FileName,
			Title:       activePlan.Title,
			ContentHash: activePlan.ContentHash,
			Sections:    sectionTitles,
			LinkedTasks: len(linkedTasks),
			ModTime:     activePlan.ModTime.Format(time.RFC3339),
		}
		itemsCaptured["active_plan"] = 1
		snapshot.Metadata["plan_file"] = activePlan.FilePath
	}

	// Capture pending/in-progress todos
	allTasks, err := taskStore.ListByWorkspace(ctx, input.Workspace)
	if err == nil {
		for _, t := range allTasks {
			if t.Status == tasks.StatusPending || t.Status == tasks.StatusInProgress {
				snapshot.PendingTodos = append(snapshot.PendingTodos, TaskInfo{
					ID:          t.ID,
					Title:       t.Title,
					Description: t.Description,
					Status:      t.Status,
					Notes:       t.Notes,
					Gotchas:     t.Gotchas,
				})
			}
		}
		itemsCaptured["pending_todos"] = len(snapshot.PendingTodos)
	}

	// Collect all gotchas from tasks for easy reference
	var allGotchas []string
	if snapshot.ActiveTask != nil && snapshot.ActiveTask.Gotchas != "" {
		allGotchas = append(allGotchas, snapshot.ActiveTask.Gotchas)
	}
	for _, t := range snapshot.PendingTodos {
		if t.Gotchas != "" {
			allGotchas = append(allGotchas, t.Gotchas)
		}
	}
	if len(allGotchas) > 0 {
		itemsCaptured["gotchas"] = len(allGotchas)
	}

	// Add metadata
	snapshot.Metadata["captured_at"] = snapshot.Timestamp.Format(time.RFC3339)
	snapshot.Metadata["trigger"] = input.Trigger

	// Serialize snapshot
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		fail("ERUNTIME", fmt.Errorf("marshal snapshot: %w", err))
	}

	// Store in memory with type "session_snapshot"
	snapshotName := fmt.Sprintf("session-snapshot-%s", snapshot.SnapshotID)
	summaryText := fmt.Sprintf("Session snapshot: %s", input.Trigger)
	if snapshot.ActiveTask != nil && snapshot.ActivePlan != nil {
		summaryText = fmt.Sprintf("Session snapshot (%s): %s [Plan: %s]", input.Trigger, snapshot.ActiveTask.Title, snapshot.ActivePlan.Title)
	} else if snapshot.ActiveTask != nil {
		summaryText = fmt.Sprintf("Session snapshot (%s): %s", input.Trigger, snapshot.ActiveTask.Title)
	} else if snapshot.ActivePlan != nil {
		summaryText = fmt.Sprintf("Session snapshot (%s): Plan: %s", input.Trigger, snapshot.ActivePlan.Title)
	}

	_, err = memStore.SaveResult(ctx, memory.SaveOptions{
		Name:      snapshotName,
		Type:      "session_snapshot",
		Workspace: input.Workspace,
		Summary:   summaryText,
		Result:    snapshotJSON,
	})
	if err != nil {
		fail("EIO", fmt.Errorf("save snapshot: %w", err))
	}

	// Output result
	output := Output{
		SnapshotID:    snapshot.SnapshotID,
		ItemsCaptured: itemsCaptured,
		Message:       fmt.Sprintf("Session snapshot saved: %s", snapshotName),
	}

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/save result")
}

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/save failure")
	os.Exit(1)
}
