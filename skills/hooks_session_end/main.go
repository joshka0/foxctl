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

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/hook"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/oklog/ulid/v2"
)

const command = "hooks/session_end"

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

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail(err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail(err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in hook.Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail(fmt.Errorf("decode input: %w", err))
	}

	if err := run(ctx, rc, cfg, in); err != nil {
		fail(err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, cfg config.Config, in hook.Input) error {
	// Only process Stop events
	if in.Event != "Stop" {
		return emitOutput(rc, hook.NewNone())
	}

	// Check if feedback is enabled
	if os.Getenv("AGENTCTL_SESSION_FEEDBACK_ENABLED") == "false" {
		return emitOutput(rc, hook.NewNone())
	}

	workspaceID := in.WorkspaceRoot
	if workspaceID == "" {
		var wdErr error
		workspaceID, wdErr = os.Getwd()
		if wdErr != nil {
			return fmt.Errorf("failed to determine workspace directory: %w", wdErr)
		}
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

	// Get task stats for this workspace
	var taskStore tasks.Store
	var err error
	taskStore, err = tasks.Open(ctx, cfg.Storage.Root)
	if err == nil {
		defer taskStore.Close()

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
	if cfg.Storage.Root != "" && in.SessionID != "" {
		trajStore, trajErr := trajectory.Open(ctx, cfg.Storage.Root)
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

	// Save metrics to memory store
	memStore, err := memory.Open(ctx, cfg.Paths.Cache, cfg.Paths.CAS)
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
			errs.Ignore(saveErr, "save session end metrics")
		}
	}

	// Create graph edges: session → tasks (worked_on)
	if in.SessionID != "" && taskStore != nil {
		createSessionGraphEdges(ctx, cfg, workspaceID, in.SessionID, taskStore)
	}

	// Build context message prompting for feedback
	contextMsg := buildFeedbackPrompt(metrics)

	output := hook.Output{
		Decision: hook.DecisionNone,
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

	return emitOutput(rc, output)
}

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

func emitOutput(rc *runner.RunnerContext, output hook.Output) error {
	env := envelope.OK(command, map[string]any{
		"hook_output": output,
	})
	return envelope.Write(rc.Stdout, env)
}

func fail(err error) {
	env := envelope.Error(command, "ERUNTIME", err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit hook failure")
	os.Exit(1)
}

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
func createSessionGraphEdges(ctx context.Context, cfg config.Config, workspaceID, sessionID string, taskStore tasks.Store) {
	// Open graph store (fail silently - graph is optional)
	graphStore, err := graph.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return
	}
	defer func() { errs.Ignore(graphStore.Close(), "close graph store") }()

	// Ensure session node exists
	sessionNodeID := graph.SessionNodeID(sessionID)
	sessionNode := graph.Node{
		Workspace: workspaceID,
		NodeID:    sessionNodeID,
		NodeType:  graph.NodeTypeSession,
		Title:     sessionID,
		LastSeen:  time.Now().UTC(),
	}
	errs.Ignore(graphStore.UpsertNode(ctx, sessionNode), "upsert session node")

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
			errs.Ignore(graphStore.UpsertNode(ctx, taskNode), "upsert task node")

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
			errs.Ignore(graphStore.UpsertEdge(ctx, edge), "upsert worked_on edge")
		}
	}
}

func intPtr(i int) *int {
	return &i
}
