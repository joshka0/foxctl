// Package main implements the session/restore skill for restoring session state after compaction.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// Input defines the skill input parameters.
type Input struct {
	Trigger   string `json:"trigger"`   // "compact", "resume", "startup"
	Workspace string `json:"workspace"` // Project path
}

// SessionSnapshot represents the captured session state (must match session_save).
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
	Sections    []string `json:"sections,omitempty"`
	LinkedTasks int      `json:"linked_tasks,omitempty"`
	ModTime     string   `json:"mod_time,omitempty"`
}

// TaskInfo represents a simplified task.
type TaskInfo struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	Notes       string `json:"notes,omitempty"`
	Gotchas     string `json:"gotchas,omitempty"`
}

// HookOutput is the Claude Code hook output format.
type HookOutput struct {
	Decision string            `json:"decision"` // "approve", "block", "none"
	Reason   string            `json:"reason,omitempty"`
	Context  string            `json:"context,omitempty"` // Injected context
	Env      map[string]string `json:"env,omitempty"`     // Environment variables
}

// Output defines the skill output.
type Output struct {
	HookOutput    HookOutput `json:"hook_output"`
	SnapshotID    string     `json:"snapshot_id,omitempty"`
	SnapshotAge   string     `json:"snapshot_age,omitempty"`
	ItemsRestored int        `json:"items_restored"`
}

const command = "session/restore"

func main() {
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("DECODE_ERROR", fmt.Errorf("decode input: %w", err))
	}

	// Default workspace to current directory
	if input.Workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			input.Workspace = wd
		}
	}

	// Default trigger
	if input.Trigger == "" {
		input.Trigger = "compact"
	}

	// Get agentctl home
	home := os.Getenv("AGENTCTL_HOME")
	if home == "" {
		homeDir, _ := os.UserHomeDir()
		home = filepath.Join(homeDir, ".agentctl")
	}

	// Open memory store - use cache path (matches CLI)
	cachePath := filepath.Join(home, "cache")
	casPath := filepath.Join(home, "cas")

	memStore, err := memory.Open(ctx, cachePath, casPath)
	if err != nil {
		// No snapshot available - that's ok, just return empty context
		writeEmptyOutput("no memory store")
		return
	}
	defer func() { errs.Ignore(memStore.Close(), "close memory store") }()

	// Search for most recent session snapshot
	snapshots, err := memStore.Search(ctx, input.Workspace, "session-snapshot", 5)
	if err != nil || len(snapshots) == 0 {
		writeEmptyOutput("no snapshots found")
		return
	}

	// Get the most recent one
	latestEntry := snapshots[0].Entry

	// Parse the snapshot
	var snapshot SessionSnapshot
	if err := json.Unmarshal(latestEntry.Result, &snapshot); err != nil {
		writeEmptyOutput("invalid snapshot format")
		return
	}

	// Format context for injection
	contextStr := formatContext(snapshot, input.Trigger)
	snapshotAge := formatAge(snapshot.Timestamp)

	// Build output
	output := Output{
		HookOutput: HookOutput{
			Decision: "approve",
			Reason:   fmt.Sprintf("Restored session snapshot from %s ago", snapshotAge),
			Context:  contextStr,
			Env: map[string]string{
				"AGENTCTL_SESSION_RESTORED": "true",
				"AGENTCTL_SNAPSHOT_ID":      snapshot.SnapshotID,
			},
		},
		SnapshotID:    snapshot.SnapshotID,
		SnapshotAge:   snapshotAge,
		ItemsRestored: countItems(snapshot),
	}

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/restore result")
}

func writeEmptyOutput(reason string) {
	output := Output{
		HookOutput: HookOutput{
			Decision: "approve",
			Reason:   reason,
		},
		ItemsRestored: 0,
	}
	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/restore result")
}

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/restore failure")
	os.Exit(1)
}

func formatContext(snap SessionSnapshot, trigger string) string {
	var sb strings.Builder

	sb.WriteString("## Session Continuity Context\n\n")
	sb.WriteString(fmt.Sprintf("*Restored after %s (snapshot from %s ago)*\n\n", trigger, formatAge(snap.Timestamp)))

	// Active plan (from ~/.claude/plans/)
	if snap.ActivePlan != nil {
		sb.WriteString("### Active Plan\n")
		sb.WriteString(fmt.Sprintf("**%s** (`%s`)\n", snap.ActivePlan.Title, snap.ActivePlan.FileName))
		if len(snap.ActivePlan.Sections) > 0 {
			sb.WriteString("Sections:\n")
			for _, sec := range snap.ActivePlan.Sections {
				sb.WriteString(fmt.Sprintf("  - %s\n", sec))
			}
		}
		if snap.ActivePlan.LinkedTasks > 0 {
			sb.WriteString(fmt.Sprintf("*%d tasks linked to this plan*\n", snap.ActivePlan.LinkedTasks))
		}
		sb.WriteString("\n")
	}

	// Active task
	if snap.ActiveTask != nil {
		sb.WriteString("### Active Task\n")
		sb.WriteString(fmt.Sprintf("**%s** (ID: %s)\n", snap.ActiveTask.Title, snap.ActiveTask.ID))
		if snap.ActiveTask.Description != "" {
			sb.WriteString(fmt.Sprintf("%s\n", snap.ActiveTask.Description))
		}
		sb.WriteString("\n")
	}

	// Pending todos
	if len(snap.PendingTodos) > 0 {
		sb.WriteString("### Pending Work\n")
		for _, todo := range snap.PendingTodos {
			status := "⏳"
			if todo.Status == "in_progress" {
				status = "🔄"
			}
			sb.WriteString(fmt.Sprintf("- %s %s\n", status, todo.Title))
		}
		sb.WriteString("\n")
	}

	// Collect and display gotchas from all tasks (deduplicated)
	seenGotchas := make(map[string]bool)
	var gotchas []string
	if snap.ActiveTask != nil && snap.ActiveTask.Gotchas != "" {
		entry := fmt.Sprintf("**%s**: %s", snap.ActiveTask.Title, snap.ActiveTask.Gotchas)
		gotchas = append(gotchas, entry)
		seenGotchas[snap.ActiveTask.ID] = true
	}
	for _, todo := range snap.PendingTodos {
		if todo.Gotchas != "" && !seenGotchas[todo.ID] {
			gotchas = append(gotchas, fmt.Sprintf("**%s**: %s", todo.Title, todo.Gotchas))
		}
	}
	if len(gotchas) > 0 {
		sb.WriteString("### Gotchas & Learnings\n")
		for _, g := range gotchas {
			sb.WriteString(fmt.Sprintf("- %s\n", g))
		}
		sb.WriteString("\n")
	}

	// Key decisions
	if len(snap.Decisions) > 0 {
		sb.WriteString("### Key Decisions Made\n")
		for _, d := range snap.Decisions {
			sb.WriteString(fmt.Sprintf("- %s\n", d))
		}
		sb.WriteString("\n")
	}

	// Insights
	if len(snap.Insights) > 0 {
		sb.WriteString("### Insights\n")
		for _, i := range snap.Insights {
			sb.WriteString(fmt.Sprintf("- %s\n", i))
		}
		sb.WriteString("\n")
	}

	// Summary
	if snap.Summary != "" {
		sb.WriteString("### Session Summary\n")
		sb.WriteString(snap.Summary)
		sb.WriteString("\n\n")
	}

	sb.WriteString("---\n")
	sb.WriteString("*Continue where you left off. Use `agentctl todo list` to see full task details.*\n")

	return sb.String()
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func countItems(snap SessionSnapshot) int {
	count := 0
	if snap.ActivePlan != nil {
		count++
	}
	if snap.ActiveTask != nil {
		count++
	}
	count += len(snap.PendingTodos)
	count += len(snap.Decisions)
	count += len(snap.Insights)
	return count
}
