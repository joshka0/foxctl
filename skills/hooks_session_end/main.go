// Package main implements the hooks/session_end skill.
// This skill captures session metrics at session end and prompts for feedback.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/hookutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/hooks"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/oklog/ulid/v2"
)

const command = "hooks/session_end"

// HookInput extends hooks.Input with transcript path for session end processing.
type HookInput struct {
	hooks.Input
	TranscriptPath string `json:"transcript_path,omitempty"`
}

// SessionMetrics captures metrics about the ended session.
// SessionMetrics captures metrics about the ended session.
type SessionMetrics struct {
	MetricsID         string    `json:"metrics_id"`
	SessionID         string    `json:"session_id"`
	Workspace         string    `json:"workspace"`
	EndedAt           time.Time `json:"ended_at"`
	TasksCompleted    int       `json:"tasks_completed"`
	TasksInProgress   int       `json:"tasks_in_progress"`
	TasksPending      int       `json:"tasks_pending"`
	TrajectoriesCount int       `json:"trajectories_count,omitempty"`
	HasTranscript     bool      `json:"has_transcript"`
	FeedbackPending   bool      `json:"feedback_pending"`
}

// main is the skill entry point for hooks/session_end.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates session end processing with metrics collection and feedback prompting.
//
// Index:
// - Purpose: Capture session metrics at session end and prompt for feedback collection
// - Flow: validate event → resolve workspace → collect task stats → get trajectory count → save metrics → create graph edges → emit feedback prompt
// - SideEffects: metrics storage; graph edge creation; feedback prompting
// - FailureModes: store access failures, workspace resolution errors
// - Observability: emits session statistics, task counts, and feedback prompts
// - Related: buildFeedbackPrompt, createSessionGraphEdges, fileExists
// - Keywords: hooks/session_end, session_metrics, feedback_collection, graph_edges, task_statistics
func run(ctx context.Context, rc *skillmain.RunContext, in HookInput) error {
	// Only process Stop events
	if in.Event != hooks.EventSessionEnd && string(in.Event) != "Stop" {
		return hookutil.EmitOutput(rc, command, hooks.NewNone(), nil)
	}

	// Check if feedback is enabled
	if os.Getenv("AGENTCTL_SESSION_FEEDBACK_ENABLED") == "false" {
		return hookutil.EmitOutput(rc, command, hooks.NewNone(), nil)
	}

	workspaceRoot := hookutil.ResolveWorkspaceRoot(in.Input, "")
	workspaceID := hookutil.ResolveWorkspaceID(in.Input, workspaceRoot)
	if workspaceID == "" {
		return fmt.Errorf("failed to determine workspace directory")
	}

	// Collect session metrics
	metrics := SessionMetrics{
		MetricsID:       ulid.Make().String(),
		SessionID:       in.SessionID,
		Workspace:       workspaceID,
		EndedAt:         timeutil.NowUTC(),
		HasTranscript:   in.TranscriptPath != "" && fileExists(in.TranscriptPath),
		FeedbackPending: true,
	}

	// Get paths from sessionkit
	paths := sessionkit.ResolvePaths(rc.Config)

	// Get task stats for this workspace
	var taskStore tasks.Store
	taskStore, err := rc.Stores.Tasks(ctx)
	if err == nil {

		allTasks, listErr := taskStore.ListByWorkspace(ctx, workspaceID)
		if listErr == nil {
			for _, t := range allTasks {
				switch t.Status {
				case tasks.StatusCompleted:
					metrics.TasksCompleted++
				case tasks.StatusInProgress:
					metrics.TasksInProgress++
				case tasks.StatusPending:
					metrics.TasksPending++
				}
			}
		}
	}

	// Get trajectory count for this session
	if paths.StorageRoot != "" && in.SessionID != "" {
		trajStore, trajErr := trajectory.Open(ctx, paths.StorageRoot)
		if trajErr == nil {
			defer trajStore.Close()
			trajs, listErr := trajStore.ListTrajectories(ctx, trajectory.ListFilter{
				WorkspaceID: workspaceID,
				SessionID:   in.SessionID,
				Limit:       1000,
			})
			if listErr == nil {
				metrics.TrajectoriesCount = len(trajs)
			}
		}
	}

	// Save metrics to memory store (uses Storage.Root for persistent data)
	memStore, err := memory.OpenWithConfig(ctx, rc.Config)
	if err == nil {
		defer memStore.Close()

		metricsJSON, marshalErr := json.Marshal(metrics)
		if marshalErr == nil {
			summaryText := fmt.Sprintf("Session ended: %d completed, %d in-progress, %d pending tasks",
				metrics.TasksCompleted, metrics.TasksInProgress, metrics.TasksPending)

			_, saveErr := memStore.SaveResult(ctx, memory.SaveOptions{
				Name:      fmt.Sprintf("session-end-%s", metrics.MetricsID),
				Type:      "session_end_metrics",
				Workspace: workspaceID,
				Summary:   summaryText,
				Result:    metricsJSON,
				SessionID: in.SessionID,
			})
			_ = saveErr // ignore save errors
		}
	}

	// Create graph edges: session → tasks (worked_on)
	if in.SessionID != "" && taskStore != nil {
		createSessionGraphEdges(ctx, paths.StorageRoot, workspaceID, in.SessionID, taskStore)
	}

	// Build context message prompting for feedback
	contextMsg := buildFeedbackPrompt(metrics)

	output := hooks.Output{
		Decision: hooks.DecisionNone,
		Reason:   "session metrics captured",
		Context:  contextMsg,
		Meta: map[string]any{
			"metrics_id":        metrics.MetricsID,
			"session_id":        metrics.SessionID,
			"tasks_completed":   metrics.TasksCompleted,
			"tasks_in_progress": metrics.TasksInProgress,
			"tasks_pending":     metrics.TasksPending,
			"feedback_pending":  metrics.FeedbackPending,
		},
	}

	return hookutil.EmitOutput(rc, command, output, nil)
}

// buildFeedbackPrompt builds a markdown prompt for session feedback.
func buildFeedbackPrompt(metrics SessionMetrics) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("## Session Summary\n")
	sb.WriteString(fmt.Sprintf("- Tasks completed: %d\n", metrics.TasksCompleted))
	sb.WriteString(fmt.Sprintf("- Tasks in progress: %d\n", metrics.TasksInProgress))
	sb.WriteString(fmt.Sprintf("- Tasks pending: %d\n", metrics.TasksPending))
	if metrics.TrajectoriesCount > 0 {
		sb.WriteString(fmt.Sprintf("- Trajectories recorded: %d\n", metrics.TrajectoriesCount))
	}

	// Only show feedback command when SessionID is available
	if metrics.SessionID != "" {
		sb.WriteString("\n")
		sb.WriteString("To provide feedback on this session, run:\n")
		sb.WriteString("```bash\n")
		sb.WriteString(fmt.Sprintf("agentctl run session/feedback --input '{\"session_id\": \"%s\", \"rating\": 4, \"outcome\": \"success\", \"what_worked\": [...], \"what_didnt_work\": [...]}'\n", metrics.SessionID))
		sb.WriteString("```\n")
	}

	sb.WriteString("---\n")
	return sb.String()
}

// fileExists checks if a file exists, handling tilde expansion.
func fileExists(path string) bool {
	if path == "" {
		return false
	}
	// Expand ~ if present
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			path = filepath.Join(home, path[2:])
		}
	}
	_, err := os.Stat(path)
	return err == nil
}

// createSessionGraphEdges creates graph edges from the session to tasks it worked on.
// Edge types: session → task (worked_on)
func createSessionGraphEdges(ctx context.Context, storagePath, workspaceID, sessionID string, taskStore tasks.Store) {
	// Open graph store (fail silently - graph is optional)
	graphStore, err := graph.Open(ctx, storagePath)
	if err != nil {
		return
	}
	defer func() { _ = graphStore.Close() }()

	// Ensure session node exists
	sessionNodeID := graph.SessionNodeID(sessionID)
	sessionNode := graph.Node{
		Workspace: workspaceID,
		NodeID:    sessionNodeID,
		NodeType:  graph.NodeTypeSession,
		Title:     sessionID,
		LastSeen:  time.Now().UTC(),
	}
	_ = graphStore.UpsertNode(ctx, sessionNode)

	// Get all tasks for this workspace
	allTasks, err := taskStore.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return
	}

	// Create edges to in-progress and completed tasks
	for _, t := range allTasks {
		if t.Status == tasks.StatusInProgress || t.Status == tasks.StatusCompleted {
			taskNodeID := graph.TaskNodeID(t.ID)

			// Ensure task node exists with proper title
			taskNode := graph.Node{
				Workspace: workspaceID,
				NodeID:    taskNodeID,
				NodeType:  graph.NodeTypeTask,
				Title:     t.Title,
				LastSeen:  time.Now().UTC(),
			}
			_ = graphStore.UpsertNode(ctx, taskNode)

			// Create edge: session → task (worked_on)
			edge := graph.Edge{
				Workspace: workspaceID,
				FromID:    sessionNodeID,
				FromType:  graph.NodeTypeSession,
				ToID:      taskNodeID,
				ToType:    graph.NodeTypeTask,
				EdgeType:  graph.EdgeTypeWorkedOn,
				Weight:    1.0,
				TTLDays:   intPtr(90), // 90 day TTL for session edges
				CreatedAt: time.Now().UTC(),
			}
			_ = graphStore.UpsertEdge(ctx, edge)
		}
	}
}

// intPtr returns a pointer to an integer.
func intPtr(i int) *int {
	return &i
}
