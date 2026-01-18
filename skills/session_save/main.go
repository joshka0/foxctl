// Package main implements the session/save skill for capturing session state before compaction.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/analysis/tasksgraph"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/sessionkit/claudejsonl"
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
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Default workspace
	in.Workspace = workspaceutil.Resolve(in.Workspace, "", rc.Workspace)

	// Default trigger
	if in.Trigger == "" {
		in.Trigger = "manual"
	}

	// Open stores - memory uses cache path (matches CLI), tasks uses storage
	memStore, memCleanup, err := sessionkit.OpenMemoryInCache(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open memory store", skillerr.WithCause(err))
	}
	defer memCleanup()

	taskStore, taskCleanup, err := sessionkit.OpenTasks(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open task store", skillerr.WithCause(err))
	}
	defer taskCleanup()

	// Build snapshot
	snapshot := SessionSnapshot{
		SnapshotID: fmt.Sprintf("snap-%d", timeutil.NowUTC().UnixMilli()),
		SessionID:  in.SessionID,
		Trigger:    in.Trigger,
		Workspace:  in.Workspace,
		Timestamp:  timeutil.NowUTC(),
		Summary:    in.Summary,
		Metadata:   make(map[string]string),
	}

	itemsCaptured := make(map[string]int)

	// Capture active task
	activeTask, found, err := taskStore.GetActive(ctx, in.Workspace)
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
	paths := sessionkit.ResolvePaths(rc.Config)
	detector := plans.NewDetector(paths.PlansDir)

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

	// Capture pending/in-progress todos with PageRank prioritization
	// First try session-scoped, fall back to workspace-scoped
	sessionID := sessionkit.ResolveSessionID(in.Workspace, in.SessionID)

	// Get pending/in_progress tasks (session-scoped if available)
	pendingTasks, err := taskStore.ListWithOptions(ctx, in.Workspace, tasks.ListOptions{
		SessionID: sessionID,
		Statuses:  []string{tasks.StatusPending, tasks.StatusInProgress},
	})
	if err != nil || len(pendingTasks) == 0 {
		// Fallback: get all pending tasks for workspace (no session filter)
		pendingTasks, err = taskStore.ListWithOptions(ctx, in.Workspace, tasks.ListOptions{
			Statuses: []string{tasks.StatusPending, tasks.StatusInProgress},
		})
	}

	const maxPendingTodos = 10

	if err == nil && len(pendingTasks) > 0 {
		// Use PageRank to prioritize if we have more than maxPendingTodos
		if len(pendingTasks) > maxPendingTodos {
			analyzer := tasksgraph.NewAnalyzer()
			insights, analyzeErr := analyzer.Analyze(pendingTasks, in.Workspace)
			if analyzeErr == nil && len(insights.Nodes) > 0 {
				// Build ID -> PageRank map
				rankMap := make(map[string]float64)
				for _, node := range insights.Nodes {
					rankMap[node.TaskID] = node.PageRank
				}
				// Sort by PageRank descending
				sort.Slice(pendingTasks, func(i, j int) bool {
					return rankMap[pendingTasks[i].ID] > rankMap[pendingTasks[j].ID]
				})
			}
			// Truncate to top N
			pendingTasks = pendingTasks[:maxPendingTodos]
		}

		for _, t := range pendingTasks {
			snapshot.PendingTodos = append(snapshot.PendingTodos, TaskInfo{
				ID:          t.ID,
				Title:       t.Title,
				Description: t.Description,
				Status:      t.Status,
				Notes:       t.Notes,
				Gotchas:     t.Gotchas,
			})
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
	snapshot.Metadata["trigger"] = in.Trigger

	// Serialize snapshot
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return skillerr.Runtime("marshal snapshot", skillerr.WithCause(err))
	}

	// Store in memory with type "session_snapshot"
	snapshotName := fmt.Sprintf("session-snapshot-%s", snapshot.SnapshotID)
	summaryText := fmt.Sprintf("Session snapshot: %s", in.Trigger)
	if snapshot.ActiveTask != nil && snapshot.ActivePlan != nil {
		summaryText = fmt.Sprintf("Session snapshot (%s): %s [Plan: %s]", in.Trigger, snapshot.ActiveTask.Title, snapshot.ActivePlan.Title)
	} else if snapshot.ActiveTask != nil {
		summaryText = fmt.Sprintf("Session snapshot (%s): %s", in.Trigger, snapshot.ActiveTask.Title)
	} else if snapshot.ActivePlan != nil {
		summaryText = fmt.Sprintf("Session snapshot (%s): Plan: %s", in.Trigger, snapshot.ActivePlan.Title)
	}

	_, err = memStore.SaveResult(ctx, memory.SaveOptions{
		Name:      snapshotName,
		Type:      "session_snapshot",
		Workspace: in.Workspace,
		Summary:   summaryText,
		Result:    snapshotJSON,
		SessionID: sessionID,
	})
	if err != nil {
		return skillerr.IO("save snapshot", skillerr.WithCause(err))
	}

	// Output result
	output := Output{
		SnapshotID:    snapshot.SnapshotID,
		ItemsCaptured: itemsCaptured,
		Message:       fmt.Sprintf("Session snapshot saved: %s", snapshotName),
	}

	// Trigger session/archive to create context windows (fire-and-forget)
	// This enables semantic search over past context windows in session/restore
	// Both archive and window summarization run in background to avoid blocking the hook.
	// Note: triggerArchiveAndSummarize uses cmd.Start() which is already non-blocking,
	// so we call it synchronously to avoid a race where main exits before the goroutine runs.
	if in.Trigger == "pre_compact" && sessionID != "" {
		triggerArchiveAndSummarize(sessionID, in.Workspace)
		output.Message += " (archiving in background)"

		// Set pending_restore_at flag for post-compact context injection
		// The UserPromptSubmit hook will check this flag and run session/restore
		sessStore, sessCleanup, sessErr := sessionkit.OpenSessions(ctx, rc.Config)
		if sessErr == nil {
			defer sessCleanup()
			if setErr := sessStore.SetPendingRestore(ctx, sessionID); setErr == nil {
				output.Message += " (pending restore set)"
			}
		}
	}

	return skillout.Emit(rc, command, output)
}

// triggerArchiveAndSummarize runs archive and then window summarization as a background process.
// Uses nohup to ensure the process continues after the parent exits.
func triggerArchiveAndSummarize(sessionID, workspace string) {
	// Find JSONL path using claudejsonl package
	jsonlPath := claudejsonl.LocateSessionJSONL(workspace, sessionID)
	if jsonlPath == "" {
		return // Can't find JSONL, skip
	}

	archiveInput := map[string]any{
		"session_id":    sessionID,
		"jsonl_path":    jsonlPath,
		"workspace":     workspace,
		"embed_windows": true,
		// Archive is idempotent by default - skips already-archived chunks
	}
	archiveJSON, err := json.Marshal(archiveInput)
	if err != nil {
		return
	}

	// Build a shell script that runs archive, then summarize
	// Use nohup to ensure it survives parent exit
	script := fmt.Sprintf(`
nohup sh -c '
agentctl run session/archive --input '\''%s'\'' --ephemeral >/dev/null 2>&1
agentctl run session/summarize --input '\''{"session_id":"%s","mode":"windows"}'\'' --ephemeral >/dev/null 2>&1
' >/dev/null 2>&1 &
`, string(archiveJSON), sessionID)

	_, _ = executil.Start(context.Background(), "", "sh", "-c", script) // Start and don't wait
}
