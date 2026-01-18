// Package main implements the epic/complete skill.
// Completes an epic with learnings extraction, gotcha capture, and closure flow.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

const commandName = "epic/complete"

// Input defines the skill input.
type Input struct {
	EpicID        string `json:"epic_id,omitempty"`        // Specific epic to complete (default: active epic)
	Force         bool   `json:"force,omitempty"`          // Complete even with pending tasks
	SkipLearnings bool   `json:"skip_learnings,omitempty"` // Skip learnings extraction
	DryRun        bool   `json:"dry_run,omitempty"`        // Skip writes and LLM calls
}

// Output defines the skill output.
type Output struct {
	EpicID         string          `json:"epic_id"`
	EpicTitle      string          `json:"epic_title"`
	EpicGoal       string          `json:"epic_goal,omitempty"`
	TasksTotal     int             `json:"tasks_total"`
	TasksCompleted int             `json:"tasks_completed"`
	TasksPending   int             `json:"tasks_pending"`
	TasksInProg    int             `json:"tasks_in_progress"`
	TasksBlocked   int             `json:"tasks_blocked"`
	FilesModified  []string        `json:"files_modified,omitempty"`
	Gotchas        []GotchaSummary `json:"gotchas,omitempty"`
	Decisions      []string        `json:"decisions,omitempty"`
	Learnings      []string        `json:"learnings,omitempty"`
	Status         string          `json:"status"` // "completed", "blocked", "error"
	Message        string          `json:"message,omitempty"`
}

// GotchaSummary is a summary of a gotcha from task completion.
type GotchaSummary struct {
	TaskTitle string `json:"task_title"`
	Gotcha    string `json:"gotcha"`
}

func main() {
	config.LoadDotEnv()
	skillmain.Main(commandName, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	cfg := rc.Config

	workspaceID := workspaceutil.Resolve(rc.Workspace, "", rc.Workspace)

	sessionID := sessionkit.ResolveSessionID(rc.Workspace, rc.SessionID)

	// Open task store
	store, cleanup, err := sessionkit.OpenTasks(ctx, cfg)
	if err != nil {
		return skillerr.WrapIO("open task store", err)
	}
	defer cleanup()

	// Get the epic to complete
	var epic tasks.Epic
	if in.EpicID != "" {
		// Use provided epic ID
		epic, err = store.GetEpic(ctx, in.EpicID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return skillerr.NotFoundf("epic %s not found", in.EpicID)
			}
			return skillerr.WrapIO("get epic", err)
		}
	} else {
		// Get active epic for this workspace/session
		var found bool
		epic, found, err = store.GetActiveEpic(ctx, workspaceID, sessionID)
		if err != nil {
			return skillerr.WrapIO("get active epic", err)
		}
		if !found {
			return skillerr.Validationf("no active epic found for this session. Use epic_id parameter or set an active epic first")
		}
	}

	// Check if already completed
	if epic.Status == tasks.EpicStatusCompleted {
		return skillerr.Validationf("epic %s is already completed", epic.ID)
	}

	// Get all tasks linked to this epic
	epicTasks, err := store.ListTasksByEpic(ctx, epic.ID)
	if err != nil {
		return skillerr.WrapIO("list tasks by epic", err)
	}

	// Calculate task statistics
	var completed, pending, inProgress, blocked int
	var gotchas []GotchaSummary
	filesModified := make(map[string]bool)

	for _, t := range epicTasks {
		switch t.Status {
		case tasks.StatusCompleted:
			completed++
		case tasks.StatusPending:
			pending++
		case tasks.StatusInProgress:
			inProgress++
		case tasks.StatusBlocked:
			blocked++
		}

		// Collect gotchas from completed tasks
		if t.Gotchas != "" {
			gotchas = append(gotchas, GotchaSummary{
				TaskTitle: t.Title,
				Gotcha:    t.Gotchas,
			})
		}

		// Collect scope paths as files modified
		if t.ScopePath != "" {
			filesModified[t.ScopePath] = true
		}
	}

	total := len(epicTasks)
	pendingTotal := pending + inProgress + blocked

	// Check if we can complete
	if pendingTotal > 0 && !in.Force {
		output := Output{
			EpicID:         epic.ID,
			EpicTitle:      epic.Title,
			EpicGoal:       epic.Goal,
			TasksTotal:     total,
			TasksCompleted: completed,
			TasksPending:   pending,
			TasksInProg:    inProgress,
			TasksBlocked:   blocked,
			Status:         "blocked",
			Message:        fmt.Sprintf("Cannot complete epic: %d tasks remaining (%d pending, %d in progress, %d blocked). Use force=true to complete anyway.", pendingTotal, pending, inProgress, blocked),
		}
		return skillout.Emit(rc, commandName, output)
	}

	// Convert files map to slice
	var files []string
	for f := range filesModified {
		files = append(files, f)
	}

	// Extract learnings if not skipped and we have a session
	var learnings, decisions []string
	if !in.SkipLearnings && sessionID != "" {
		extracted, err := extractLearnings(ctx, cfg, epic.ID, workspaceID)
		if err != nil {
			log.Printf("[WARN] failed to extract learnings: %v", err)
		} else {
			learnings = extracted.Learnings
			decisions = extracted.Decisions
		}
	}

	if in.DryRun {
		output := Output{
			EpicID:         epic.ID,
			EpicTitle:      epic.Title,
			EpicGoal:       epic.Goal,
			TasksTotal:     total,
			TasksCompleted: completed,
			TasksPending:   pending,
			TasksInProg:    inProgress,
			TasksBlocked:   blocked,
			FilesModified:  files,
			Gotchas:        gotchas,
			Decisions:      decisions,
			Learnings:      learnings,
			Status:         "dry_run",
			Message:        "dry run: no changes applied",
		}
		return skillout.Emit(rc, commandName, output)
	}

	// Persist gotchas from tasks to memory store
	if len(gotchas) > 0 {
		persistGotchas(ctx, cfg, epic.ID, workspaceID, gotchas)
	}

	// Mark epic as completed
	now := time.Now().UTC()
	epic.Status = tasks.EpicStatusCompleted
	epic.CompletedAt = &now

	_, err = store.UpdateEpic(ctx, epic)
	if err != nil {
		return skillerr.WrapIO("update epic status", err)
	}

	// Clear active epic
	if err := store.ClearActiveEpic(ctx, workspaceID, sessionID); err != nil {
		log.Printf("[WARN] failed to clear active epic: %v", err)
	}

	output := Output{
		EpicID:         epic.ID,
		EpicTitle:      epic.Title,
		EpicGoal:       epic.Goal,
		TasksTotal:     total,
		TasksCompleted: completed,
		TasksPending:   pending,
		TasksInProg:    inProgress,
		TasksBlocked:   blocked,
		FilesModified:  files,
		Gotchas:        gotchas,
		Decisions:      decisions,
		Learnings:      learnings,
		Status:         "completed",
		Message:        fmt.Sprintf("Epic '%s' completed successfully with %d tasks", epic.Title, total),
	}

	return skillout.Emit(rc, commandName, output)
}

// ExtractedLearnings holds learnings from session analysis.
type ExtractedLearnings struct {
	Learnings []string
	Decisions []string
	Gotchas   []string
}

// extractLearnings retrieves learnings from memory store linked to this epic.
func extractLearnings(ctx context.Context, cfg config.Config, epicID, workspace string) (*ExtractedLearnings, error) {
	result := &ExtractedLearnings{
		Learnings: []string{},
		Decisions: []string{},
		Gotchas:   []string{},
	}

	// Try to read any recent learnings from memory store
	memStore, cleanup, err := sessionkit.OpenMemory(ctx, cfg)
	if err != nil {
		return result, nil // Non-fatal
	}
	defer cleanup()

	filter := memory.ListFilter{
		Types: []string{"learning", "decision", "gotcha"},
	}
	entries, _, err := memStore.ListFiltered(ctx, workspace, filter, 200, 0)
	if err != nil {
		return result, nil
	}

	for _, entry := range entries {
		if !matchesEpic(entry, epicID) {
			continue
		}
		switch entry.Type {
		case "learning":
			result.Learnings = append(result.Learnings, entry.Summary)
		case "decision":
			result.Decisions = append(result.Decisions, entry.Summary)
		case "gotcha":
			result.Gotchas = append(result.Gotchas, entry.Summary)
		}
	}

	return result, nil
}

func matchesEpic(entry storage.NamedEntry, epicID string) bool {
	if epicID == "" {
		return false
	}

	if len(entry.Result) > 0 {
		var payload struct {
			EpicID string `json:"epic_id"`
		}
		if err := json.Unmarshal(entry.Result, &payload); err == nil && payload.EpicID == epicID {
			return true
		}
	}

	tag := "epic:" + epicID
	return strings.Contains(entry.Name, tag) || strings.Contains(entry.Summary, tag)
}

// persistGotchas saves task gotchas to the memory store.
func persistGotchas(ctx context.Context, cfg config.Config, epicID, workspace string, gotchas []GotchaSummary) {
	if len(gotchas) == 0 {
		return
	}

	memStore, cleanup, err := sessionkit.OpenMemory(ctx, cfg)
	if err != nil {
		log.Printf("[WARN] failed to open memory store: %v", err)
		return
	}
	defer cleanup()

	for _, g := range gotchas {
		summary := fmt.Sprintf("[%s] %s", g.TaskTitle, g.Gotcha)
		name := fmt.Sprintf("task-gotcha-%s", sanitizeForName(g.TaskTitle))

		entry := storage.NamedEntry{
			Name:      name,
			Type:      "gotcha",
			Summary:   summary,
			Workspace: workspace,
		}

		if _, err := memStore.Save(ctx, entry); err != nil {
			log.Printf("[WARN] failed to persist gotcha: %v", err)
		}
	}
}

// sanitizeForName creates a safe name from a string.
func sanitizeForName(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		if r == ' ' {
			return '-'
		}
		return -1
	}, s)
	// Truncate to reasonable length
	if len(s) > 40 {
		s = s[:40]
	}
	return strings.Trim(s, "-")
}
