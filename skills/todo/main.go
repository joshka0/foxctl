// Package main implements the todo/manage skill.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/artifacts"
	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/analysis/overseer"
	"github.com/jkatigb/agentctl/internal/analysis/tasksgraph"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/planning/llm"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

const (
	statusPending    = "pending"
	statusInProgress = "in_progress"
	statusBlocked    = "blocked"
	statusDone       = "completed"
)

// isReviewGateEnabled reports whether the review gate is enabled for this
// process. For v1 this is controlled by an environment variable and applies to
// all workspaces.
func isReviewGateEnabled() bool {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("AGENTCTL_TODO_REVIEW_GATE")))
	switch mode {
	case "1", "true", "on", "enabled":
		return true
	default:
		return false
	}
}

type input struct {
	Operation     string            `json:"operation"`
	WorkspaceID   string            `json:"workspace_id"`
	Add           *addRequest       `json:"add"`
	Update        *updateRequest    `json:"update"`
	Complete      *completeRequest  `json:"complete"`
	SetActive     *setActiveReq     `json:"set_active"`
	EnsureActive  *ensureActiveReq  `json:"ensure_active"`
	GraphInsights *graphInsightsReq `json:"graph_insights"`
	Recommend     *recommendReq     `json:"recommend"`
	Plan          *planReq          `json:"plan"`
	ReviewRequest *reviewRequestReq `json:"review_request"`
	ReviewStatus  *reviewStatusReq  `json:"review_status"`
	Search        *searchReq        `json:"search"`
}

// searchReq defines the input for semantic task search.
type searchReq struct {
	Query         string  `json:"query"`          // Natural language search query
	Limit         int     `json:"limit"`          // Max results to return (default: 10)
	MinSimilarity float64 `json:"min_similarity"` // Minimum similarity threshold (default: 0.3)
}

type addRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ParentID    string   `json:"parent_id"`
	DependsOn   []string `json:"depends_on"`
	ScopePath   string   `json:"scope_path"`
}

type completeRequest struct {
	ID      string `json:"id"`
	Notes   string `json:"notes"`
	Gotchas string `json:"gotchas"`
}

type updateRequest struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Notes       string `json:"notes,omitempty"`
	Gotchas     string `json:"gotchas,omitempty"`
	Status      string `json:"status,omitempty"` // pending, in_progress, blocked
}

// reviewRequestReq defines the input for the review_request operation.
// Per review_gate.md, this initiates a review for a task.
type reviewRequestReq struct {
	TaskID string `json:"task_id"` // Required
	Kind   string `json:"kind"`    // Optional: "auto", "human", or "mixed" (default: "auto")
}

// reviewStatusReq defines the input for the review_status operation.
type reviewStatusReq struct {
	TaskID string `json:"task_id"` // Required
}

type setActiveReq struct {
	TaskID string `json:"task_id"`
}

type ensureActiveReq struct {
	DefaultTitle string `json:"default_title"`
	ScopePath    string `json:"scope_path"`
}

type graphInsightsReq struct {
	IncludeCompleted bool `json:"include_completed"`
	Limit            int  `json:"limit"`
}

type recommendReq struct {
	Limit int `json:"limit"` // Max recommendations to return (default: 10)
}

// planReq defines the input for the plan operation.
type planReq struct {
	Goal           string     `json:"goal"`              // One-sentence description of desired outcome
	Description    string     `json:"description"`       // Longer context for planning
	ScopePaths     []string   `json:"scope_paths"`       // Directories/files likely to be touched
	AttachToTaskID string     `json:"attach_to_task_id"` // Empty=new epic, non-empty=refine within that epic
	Mode           string     `json:"mode"`              // "draft" or "apply"
	MaxTasks       int        `json:"max_tasks"`         // Max tasks to create (default: 20)
	MaxDepth       int        `json:"max_depth"`         // Max nesting depth (default: 3)
	Strategy       string     `json:"strategy"`          // "auto", "epic", or "flat"
	Tasks          []planTask `json:"tasks"`             // Optional: explicit task list to create
}

// planTask defines a task to create as part of a plan.
type planTask struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ScopePath   string   `json:"scope_path"`
	DependsOn   []string `json:"depends_on"` // Titles of other tasks in this plan
}

// planOutput is the JSON representation of a plan result.
type planOutput struct {
	RootTaskID string           `json:"root_task_id"`
	Applied    bool             `json:"applied"`
	Tasks      []*taskOutput    `json:"tasks"`
	Graph      *planGraphOutput `json:"graph,omitempty"`
	Diff       *planDiffOutput  `json:"diff"`
}

type planGraphOutput struct {
	Nodes  []tasksgraph.NodeMetrics `json:"nodes"`
	Edges  []planEdge               `json:"edges"`
	Cycles [][]string               `json:"cycles"`
}

type planEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type planDiffOutput struct {
	AddedTaskIDs   []string `json:"added_task_ids"`
	UpdatedTaskIDs []string `json:"updated_task_ids"`
	RemovedTaskIDs []string `json:"removed_task_ids"`
}

// taskOutput is the JSON representation of a task for envelope output.
type taskOutput struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	ScopePath   string   `json:"scope_path,omitempty"`
	ParentID    string   `json:"parent_id,omitempty"`
	Children    []string `json:"children,omitempty"`
	DependsOn   []string `json:"depends_on,omitempty"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"created_at"`
	CompletedAt string   `json:"completed_at,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	Gotchas     string   `json:"gotchas,omitempty"`

	// Review gate fields (review_gate.md)
	LastReviewStatus string `json:"last_review_status,omitempty"`
	LastReviewAt     string `json:"last_review_at,omitempty"`
	LastReviewID     string `json:"last_review_id,omitempty"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("todo/manage", "ERUNTIME", err, "Check AGENTCTL_HOME is set and accessible")
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("todo/manage", "ERUNTIME", err, "Check storage directory permissions")
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("todo/manage", "EPARSE", fmt.Errorf("decode input: %w", err), "Ensure valid JSON on stdin")
	}
	if err := run(ctx, rc, cfg, in); err != nil {
		fail("todo/manage", "ERUNTIME", err, "")
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, cfg config.Config, in input) error {
	// Open SQLite-backed task store
	store, err := tasks.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open task store: %w", err)
	}
	defer store.Close()

	op := strings.ToLower(strings.TrimSpace(in.Operation))
	if op == "" {
		op = "list"
	}

	workspaceID := in.WorkspaceID
	if workspaceID == "" {
		// Check AGENTCTL_WORKSPACE env var (set by runner when executing skills)
		workspaceID = os.Getenv("AGENTCTL_WORKSPACE")
	}
	if workspaceID == "" {
		// Default to current working directory as workspace ID.
		// Fallback to current directory; error is not actionable.
		workspaceID, _ = os.Getwd() //nolint:errcheck
	}

	var data map[string]any

	switch op {
	case "add":
		task, allTasks, err := handleAdd(ctx, store, cfg, workspaceID, rc.SessionID, in.Add)
		if err != nil {
			return err
		}
		data = map[string]any{
			"task":          task,
			"total_tasks":   len(allTasks),
			"pending_tasks": countPending(allTasks),
			"summary":       fmt.Sprintf("added task %s", task.ID),
		}

	case "update":
		task, allTasks, err := handleUpdate(ctx, store, workspaceID, in.Update)
		if err != nil {
			return err
		}
		data = map[string]any{
			"task":          task,
			"total_tasks":   len(allTasks),
			"pending_tasks": countPending(allTasks),
			"summary":       fmt.Sprintf("updated task %s", task.ID),
		}

	case "complete":
		task, allTasks, err := handleComplete(ctx, store, workspaceID, in.Complete)
		if err != nil {
			return err
		}
		data = map[string]any{
			"task":          task,
			"pending_tasks": countPending(allTasks),
			"summary":       fmt.Sprintf("completed task %s", task.ID),
		}

	case "list":
		allTasks, err := store.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		data = map[string]any{
			"tasks":         toOutputList(allTasks),
			"total_tasks":   len(allTasks),
			"pending_tasks": countPending(allTasks),
		}

	case "get_active":
		task, found, err := store.GetActive(ctx, workspaceID)
		if err != nil {
			return err
		}
		if !found {
			data = map[string]any{
				"active": false,
				"task":   nil,
			}
		} else {
			data = map[string]any{
				"active": true,
				"task":   toOutput(task),
			}
		}

	case "set_active":
		if in.SetActive == nil || in.SetActive.TaskID == "" {
			return fmt.Errorf("set_active.task_id is required")
		}
		task, err := store.SetActive(ctx, workspaceID, in.SetActive.TaskID)
		if err != nil {
			return err
		}
		data = map[string]any{
			"task":    toOutput(task),
			"summary": fmt.Sprintf("set active task to %s", task.ID),
		}

	case "clear_active":
		if err := store.ClearActive(ctx, workspaceID); err != nil {
			return err
		}
		data = map[string]any{
			"summary": "cleared active task",
		}

	case "ensure_active":
		title := "Unnamed task"
		scopePath := ""
		if in.EnsureActive != nil {
			if in.EnsureActive.DefaultTitle != "" {
				title = in.EnsureActive.DefaultTitle
			}
			scopePath = in.EnsureActive.ScopePath
		}
		task, created, err := store.EnsureActive(ctx, workspaceID, title, scopePath)
		if err != nil {
			return err
		}
		data = map[string]any{
			"task":    toOutput(task),
			"created": created,
			"summary": fmt.Sprintf("active task is %s (created=%v)", task.ID, created),
		}

	case "graph_insights":
		allTasks, err := store.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		// Filter completed tasks if not requested
		if in.GraphInsights == nil || !in.GraphInsights.IncludeCompleted {
			allTasks = filterPending(allTasks)
		}
		insights, err := tasksgraph.NewAnalyzer().Analyze(allTasks, workspaceID)
		if err != nil {
			return err
		}
		// Apply limit if specified
		if in.GraphInsights != nil && in.GraphInsights.Limit > 0 && len(insights.Nodes) > in.GraphInsights.Limit {
			// Sort by CriticalPathScore desc, then PageRank desc
			sort.Slice(insights.Nodes, func(i, j int) bool {
				if insights.Nodes[i].CriticalPathScore != insights.Nodes[j].CriticalPathScore {
					return insights.Nodes[i].CriticalPathScore > insights.Nodes[j].CriticalPathScore
				}
				return insights.Nodes[i].PageRank > insights.Nodes[j].PageRank
			})
			insights.Nodes = insights.Nodes[:in.GraphInsights.Limit]
		}
		data = map[string]any{
			"insights": insights,
			"summary":  fmt.Sprintf("analyzed %d tasks, %d cycles", len(allTasks), len(insights.Cycles)),
		}

	case "recommend":
		limit := 10
		if in.Recommend != nil && in.Recommend.Limit > 0 {
			limit = in.Recommend.Limit
		}
		// Open board store for mailbox integration (optional).
		// Best-effort open; error is ignored for optional integration.
		boardStore, _ := blackboard.OpenBoardStore(ctx, cfg.Storage.Root) //nolint:errcheck
		if boardStore != nil {
			defer boardStore.Close()
		}
		scorer := overseer.NewScorer(store, boardStore)
		rec, err := scorer.Recommend(ctx, workspaceID, limit)
		if err != nil {
			return err
		}
		summary := fmt.Sprintf("recommended %d of %d pending tasks", len(rec.Tasks), rec.TotalPending)
		if rec.TopRecommended != nil {
			summary += fmt.Sprintf("; top: %s (score=%.2f)", rec.TopRecommended.Title, rec.TopRecommended.Score)
		}
		data = map[string]any{
			"recommendation": rec,
			"summary":        summary,
		}

	case "plan":
		// Open board store for plan event emission.
		// Best-effort open; error is ignored for optional integration.
		boardStore, _ := blackboard.OpenBoardStore(ctx, cfg.Storage.Root) //nolint:errcheck
		if boardStore != nil {
			defer boardStore.Close()
		}
		planResult, err := handlePlan(ctx, store, boardStore, cfg, workspaceID, rc.SessionID, in.Plan)
		if err != nil {
			return err
		}
		// Safely access Plan.Goal (may be nil if handlePlan validated early)
		goal := ""
		if in.Plan != nil {
			goal = in.Plan.Goal
		}
		summary := fmt.Sprintf("plan for %q: %d tasks", goal, len(planResult.Tasks))
		if planResult.Applied {
			summary += " (applied)"
		} else {
			summary += " (draft)"
		}
		data = map[string]any{
			"plan":    planResult,
			"summary": summary,
		}

	case "review_request":
		task, err := handleReviewRequest(ctx, store, workspaceID, cfg, in.ReviewRequest)
		if err != nil {
			return err
		}
		data = map[string]any{
			"task":    task,
			"summary": fmt.Sprintf("review requested for task %s", task.ID),
		}

	case "review_status":
		task, err := handleReviewStatus(ctx, store, workspaceID, in.ReviewStatus)
		if err != nil {
			return err
		}
		data = map[string]any{
			"task_id":            task.ID,
			"last_review_status": task.LastReviewStatus,
			"last_review_at":     formatTime(task.LastReviewAt),
			"last_review_id":     task.LastReviewID,
			"summary":            fmt.Sprintf("review status for task %s: %s", task.ID, task.LastReviewStatus),
		}

	case "search":
		results, err := handleSearch(ctx, store, cfg, workspaceID, in.Search)
		if err != nil {
			return err
		}
		data = map[string]any{
			"results":     results.Tasks,
			"total_found": results.TotalFound,
			"query":       results.Query,
			"summary":     fmt.Sprintf("found %d tasks matching %q", results.TotalFound, results.Query),
		}

	default:
		return fmt.Errorf("unknown operation %q (expected add|update|complete|list|get_active|set_active|clear_active|ensure_active|graph_insights|recommend|plan|review_request|review_status|search)", op)
	}

	return rc.Emit("todo/manage", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func handleAdd(ctx context.Context, store tasks.Store, cfg config.Config, workspaceID, sessionID string, req *addRequest) (*taskOutput, []tasks.Task, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("add payload is required")
	}
	if err := validateText("title", req.Title); err != nil {
		return nil, nil, err
	}
	if err := validateText("description", req.Description); err != nil {
		return nil, nil, err
	}
	if req.Title == "" {
		return nil, nil, fmt.Errorf("title is required")
	}

	// Validate parent and dependencies exist
	allTasks, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	index := buildIndex(allTasks)

	if req.ParentID != "" {
		parent, ok := index[req.ParentID]
		if !ok {
			return nil, nil, fmt.Errorf("parent task %s not found", req.ParentID)
		}
		if parent.Status == statusDone {
			return nil, nil, fmt.Errorf("cannot add child to completed task %s", req.ParentID)
		}
	}
	if err := validateDependencies(req.DependsOn, index); err != nil {
		return nil, nil, err
	}

	newTask := tasks.Task{
		WorkspaceID: workspaceID,
		Title:       req.Title,
		Description: req.Description,
		ScopePath:   req.ScopePath,
		ParentID:    req.ParentID,
		DependsOn:   dedupe(req.DependsOn),
		Status:      statusPending,
		SessionID:   sessionID,
	}

	added, err := store.Add(ctx, newTask)
	if err != nil {
		return nil, nil, err
	}

	// Create graph edges for task relationships (parent_of, depends_on)
	createTaskDependencyEdges(ctx, cfg, workspaceID, added)

	// Update parent's children if needed
	if req.ParentID != "" {
		parent := index[req.ParentID]
		parent.Children = append(parent.Children, added.ID)
		if _, err := store.Update(ctx, *parent); err != nil {
			return nil, nil, fmt.Errorf("update parent children: %w", err)
		}
	}

	// Refresh task list
	allTasks, err = store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}

	return toOutput(added), allTasks, nil
}

func handleUpdate(ctx context.Context, store tasks.Store, workspaceID string, req *updateRequest) (*taskOutput, []tasks.Task, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("update payload is required")
	}
	if req.ID == "" {
		return nil, nil, fmt.Errorf("update.id is required")
	}
	if err := validateText("title", req.Title); err != nil {
		return nil, nil, err
	}
	if err := validateText("description", req.Description); err != nil {
		return nil, nil, err
	}
	if err := validateText("notes", req.Notes); err != nil {
		return nil, nil, err
	}
	if err := validateText("gotchas", req.Gotchas); err != nil {
		return nil, nil, err
	}

	task, err := store.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("task %s not found", req.ID)
		}
		return nil, nil, fmt.Errorf("get task %s: %w", req.ID, err)
	}

	// Verify workspace ownership
	if task.WorkspaceID != workspaceID {
		return nil, nil, fmt.Errorf("task %s belongs to a different workspace", req.ID)
	}

	// Don't allow updating completed tasks
	if task.Status == statusDone {
		return nil, nil, fmt.Errorf("cannot update completed task %s", req.ID)
	}

	// Apply updates (only if non-empty)
	if req.Title != "" {
		task.Title = req.Title
	}
	if req.Description != "" {
		task.Description = req.Description
	}
	if req.Notes != "" {
		task.Notes = req.Notes
	}
	if req.Gotchas != "" {
		task.Gotchas = req.Gotchas
	}
	if req.Status != "" {
		// Only allow certain status transitions
		switch req.Status {
		case statusPending, statusInProgress, statusBlocked:
			task.Status = req.Status
		default:
			return nil, nil, fmt.Errorf("invalid status %q (use complete action for completing tasks)", req.Status)
		}
	}

	updated, err := store.Update(ctx, task)
	if err != nil {
		return nil, nil, fmt.Errorf("update task: %w", err)
	}

	allTasks, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}

	return toOutput(updated), allTasks, nil
}

func handleComplete(ctx context.Context, store tasks.Store, workspaceID string, req *completeRequest) (*taskOutput, []tasks.Task, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("complete payload is required")
	}
	if req.ID == "" {
		return nil, nil, fmt.Errorf("complete.id is required")
	}
	if err := validateText("notes", req.Notes); err != nil {
		return nil, nil, err
	}
	if err := validateText("gotchas", req.Gotchas); err != nil {
		return nil, nil, err
	}

	task, err := store.Get(ctx, req.ID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, fmt.Errorf("task %s not found", req.ID)
		}
		return nil, nil, fmt.Errorf("get task %s: %w", req.ID, err)
	}

	// Verify workspace ownership
	if task.WorkspaceID != workspaceID {
		return nil, nil, fmt.Errorf("task %s belongs to a different workspace", req.ID)
	}

	if task.Status == statusDone {
		return nil, nil, fmt.Errorf("task %s already completed", req.ID)
	}

	// Check dependencies
	allTasks, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	index := buildIndex(allTasks)

	for _, dep := range task.DependsOn {
		if depTask, ok := index[dep]; ok {
			if depTask.Status != statusDone {
				return nil, nil, fmt.Errorf("task %s depends on incomplete task %s", req.ID, dep)
			}
		} else {
			return nil, nil, fmt.Errorf("dependency %s for task %s not found", dep, req.ID)
		}
	}

	// Enforce review gate semantics when enabled.
	if isReviewGateEnabled() {
		if task.Status != tasks.StatusReadyForReview {
			return nil, nil, fmt.Errorf("task %s must be ready_for_review before completion", req.ID)
		}
		if task.LastReviewStatus != tasks.ReviewStatusOK || task.LastReviewID == "" {
			return nil, nil, fmt.Errorf("task %s requires an 'ok' review before completion", req.ID)
		}
	}

	now := time.Now().UTC()
	task.Status = statusDone
	task.CompletedAt = &now
	task.Notes = strings.TrimSpace(req.Notes)
	task.Gotchas = strings.TrimSpace(req.Gotchas)

	updated, err := store.Update(ctx, task)
	if err != nil {
		return nil, nil, err
	}

	// Refresh task list
	allTasks, err = store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}

	return toOutput(updated), allTasks, nil
}

// handleReviewRequest initiates a review for a task per review_gate.md.
// It validates that the task is in an allowed state (in_progress or ready_for_review),
// then sets status to ready_for_review and LastReviewStatus to pending.
func handleReviewRequest(ctx context.Context, store tasks.Store, workspaceID string, cfg config.Config, req *reviewRequestReq) (*taskOutput, error) {
	if req == nil {
		return nil, fmt.Errorf("review_request payload is required")
	}
	if req.TaskID == "" {
		return nil, fmt.Errorf("review_request.task_id is required")
	}

	task, err := store.Get(ctx, req.TaskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("task %s not found", req.TaskID)
		}
		return nil, fmt.Errorf("get task %s: %w", req.TaskID, err)
	}

	// Verify workspace ownership
	if task.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("task %s belongs to a different workspace", req.TaskID)
	}

	// Validate task state: only in_progress or ready_for_review can be reviewed
	switch task.Status {
	case tasks.StatusInProgress, tasks.StatusReadyForReview:
		// OK
	case tasks.StatusPending:
		return nil, fmt.Errorf("task %s is pending; start work before requesting review", req.TaskID)
	case tasks.StatusCompleted:
		return nil, fmt.Errorf("task %s is already completed", req.TaskID)
	case tasks.StatusBlocked:
		return nil, fmt.Errorf("task %s is blocked; resolve blockers before requesting review", req.TaskID)
	case tasks.StatusCanceled:
		return nil, fmt.Errorf("task %s is canceled", req.TaskID)
	default:
		return nil, fmt.Errorf("task %s has unknown status %q", req.TaskID, task.Status)
	}

	// Persist a minimal review artifact via CAS so downstream components have a
	// stable anchor even in Phase 1.
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind == "" {
		kind = "auto"
	}
	review := agent.ReviewArtifact{
		WorkspaceID: workspaceID,
		TaskID:      task.ID,
		Kind:        kind,
		Status:      tasks.ReviewStatusPending,
		Summary:     fmt.Sprintf("pending review for task %s: %s", task.ID, task.Title),
		CreatedBy:   os.Getenv("AGENTCTL_AGENT_NAME"),
	}
	review, err = artifacts.StoreReviewArtifact(ctx, cfg, review, nil)
	if err != nil {
		return nil, fmt.Errorf("store review artifact: %w", err)
	}

	// Transition to ready_for_review and mark review as pending
	task.Status = tasks.StatusReadyForReview
	task.LastReviewStatus = tasks.ReviewStatusPending
	now := time.Now().UTC()
	task.LastReviewAt = &now
	task.LastReviewID = review.ID

	updated, err := store.Update(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("update task: %w", err)
	}

	return toOutput(updated), nil
}

// handleReviewStatus returns the review status fields for a task.
// This is a cheap status probe that does not touch CAS or jobs.
func handleReviewStatus(ctx context.Context, store tasks.Store, workspaceID string, req *reviewStatusReq) (tasks.Task, error) {
	if req == nil {
		return tasks.Task{}, fmt.Errorf("review_status payload is required")
	}
	if req.TaskID == "" {
		return tasks.Task{}, fmt.Errorf("review_status.task_id is required")
	}

	task, err := store.Get(ctx, req.TaskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tasks.Task{}, fmt.Errorf("task %s not found", req.TaskID)
		}
		return tasks.Task{}, fmt.Errorf("get task %s: %w", req.TaskID, err)
	}

	// Verify workspace ownership
	if task.WorkspaceID != workspaceID {
		return tasks.Task{}, fmt.Errorf("task %s belongs to a different workspace", req.TaskID)
	}

	return task, nil
}

// searchOutput is the result of a semantic task search.
type searchOutput struct {
	Query      string              `json:"query"`
	TotalFound int                 `json:"total_found"`
	Tasks      []*searchTaskResult `json:"tasks"`
}

// searchTaskResult pairs a task with its similarity score.
type searchTaskResult struct {
	Task       *taskOutput `json:"task"`
	Similarity float64     `json:"similarity"`
}

// handleSearch performs semantic search over task embeddings.
func handleSearch(ctx context.Context, store tasks.Store, cfg config.Config, workspaceID string, req *searchReq) (*searchOutput, error) {
	if req == nil {
		return nil, fmt.Errorf("search payload is required")
	}
	if req.Query == "" {
		return nil, fmt.Errorf("search.query is required")
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 10
	}
	minSimilarity := req.MinSimilarity
	if minSimilarity <= 0 {
		minSimilarity = 0.3
	}

	output := &searchOutput{
		Query: req.Query,
	}

	// Check for API key
	geminiKey := os.Getenv("GEMINI_API_KEY")
	voyageKey := os.Getenv("VOYAGE_API_KEY")
	if geminiKey == "" && voyageKey == "" {
		return nil, fmt.Errorf("no embedding API key set (GEMINI_API_KEY or VOYAGE_API_KEY)")
	}

	// Initialize embedding provider (prefer Gemini for consistency with task embeddings)
	// Task embeddings are created with Gemini (3072-dim), so query must use same provider
	var provider interface {
		Embed(ctx context.Context, text string) ([]float32, error)
	}

	if geminiKey != "" {
		gp, err := semantic.NewGeminiProvider(semantic.GeminiConfig{
			APIKey:        geminiKey,
			RateLimitWait: boolPtr(true),
		})
		if err != nil {
			return nil, fmt.Errorf("gemini provider: %w", err)
		}
		provider = gp
	} else {
		vp, err := semantic.NewVoyageProvider(semantic.VoyageConfig{
			APIKey:        voyageKey,
			RateLimitWait: boolPtr(true),
		})
		if err != nil {
			return nil, fmt.Errorf("voyage provider: %w", err)
		}
		provider = vp
	}

	// Generate query embedding
	queryEmbedding, err := provider.Embed(ctx, req.Query)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// Open memory store for vector search
	memStore, err := memory.Open(ctx, cfg.Storage.Root, filepath.Join(cfg.Storage.Root, "cas"))
	if err != nil {
		return nil, fmt.Errorf("open memory store: %w", err)
	}
	defer memStore.Close() //nolint:errcheck

	// Search for similar entries using in-memory cosine similarity
	// This searches all embeddings in the workspace
	entries, err := memStore.SearchSimilar(ctx, workspaceID, queryEmbedding, limit*2) // Fetch more to allow filtering
	if err != nil {
		// Fall back to listing all tasks if similarity search fails
		// Warning output to stderr; error is not actionable.
		fmt.Fprintf(os.Stderr, "warning: similarity search failed, falling back to text match: %v\n", err)

		// Simple fallback: text-based search over all tasks
		allTasks, err := store.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return nil, fmt.Errorf("list tasks: %w", err)
		}

		queryLower := strings.ToLower(req.Query)
		for _, t := range allTasks {
			// Simple text matching on title/description
			titleMatch := strings.Contains(strings.ToLower(t.Title), queryLower)
			descMatch := strings.Contains(strings.ToLower(t.Description), queryLower)
			notesMatch := strings.Contains(strings.ToLower(t.Notes), queryLower)

			if titleMatch || descMatch || notesMatch {
				output.Tasks = append(output.Tasks, &searchTaskResult{
					Task:       toOutput(t),
					Similarity: 0.5, // Placeholder similarity for text match
				})
				if len(output.Tasks) >= limit {
					break
				}
			}
		}
		output.TotalFound = len(output.Tasks)
		return output, nil
	}

	// Filter entries to only task embeddings (type='task_embedding')
	var taskIDs []string
	scoreByID := make(map[string]float64)

	for _, entry := range entries {
		if entry.Entry.Type != "task_embedding" {
			continue
		}
		// Extract task ID from name: "task://<task_id>"
		if !strings.HasPrefix(entry.Entry.Name, "task://") {
			continue
		}
		taskID := strings.TrimPrefix(entry.Entry.Name, "task://")

		// Check similarity threshold
		if entry.Score < minSimilarity {
			continue
		}

		taskIDs = append(taskIDs, taskID)
		scoreByID[taskID] = entry.Score

		if len(taskIDs) >= limit {
			break
		}
	}

	// Fetch task details for matching IDs
	for _, taskID := range taskIDs {
		task, err := store.Get(ctx, taskID)
		if err != nil {
			// Task may have been deleted after embedding was created
			continue
		}

		// Verify workspace ownership
		if task.WorkspaceID != workspaceID {
			continue
		}

		output.Tasks = append(output.Tasks, &searchTaskResult{
			Task:       toOutput(task),
			Similarity: scoreByID[taskID],
		})
	}

	output.TotalFound = len(output.Tasks)
	return output, nil
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// formatTime safely formats a *time.Time for JSON output.
func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// handlePlan creates or refines a task graph based on the plan request.
// If mode="draft", it returns a proposed plan without persisting.
// If mode="apply", it creates the tasks and emits plan events via mailbox.
func handlePlan(ctx context.Context, store tasks.Store, boardStore blackboard.BoardStore, cfg config.Config, workspaceID, sessionID string, req *planReq) (*planOutput, error) {
	if req == nil {
		return nil, fmt.Errorf("plan payload is required")
	}
	if req.Goal == "" {
		return nil, fmt.Errorf("plan.goal is required")
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "draft"
	}
	if mode != "draft" && mode != "apply" {
		return nil, fmt.Errorf("plan.mode must be 'draft' or 'apply', got %q", mode)
	}

	// Determine the root task (epic)
	var rootTaskID string
	var epicTask *tasks.Task

	if req.AttachToTaskID != "" {
		// Refining an existing epic
		existing, err := store.Get(ctx, req.AttachToTaskID)
		if err != nil {
			return nil, fmt.Errorf("attach_to_task_id %s not found: %w", req.AttachToTaskID, err)
		}
		rootTaskID = existing.ID
		epicTask = &existing
	}

	// Build the list of tasks to create
	var plannedTasks []tasks.Task
	var addedIDs []string

	// If explicit tasks are provided, use them; otherwise create from goal
	if len(req.Tasks) > 0 {
		// Use explicitly provided task list
		titleToID := make(map[string]string)

		// First pass: create all tasks (without dependencies)
		for i, pt := range req.Tasks {
			scopePath := pt.ScopePath
			if scopePath == "" && len(req.ScopePaths) > 0 {
				// Use first scope path as default
				scopePath = req.ScopePaths[0]
			}

			t := tasks.Task{
				WorkspaceID: workspaceID,
				Title:       pt.Title,
				Description: pt.Description,
				ScopePath:   scopePath,
				Status:      statusPending,
				SessionID:   sessionID,
			}

			// If we have an epic, make tasks children of it
			if epicTask != nil {
				t.ParentID = epicTask.ID
			} else if i == 0 {
				// First task becomes the epic if no attach_to_task_id
				t.Title = "Epic: " + req.Goal
				t.Description = req.Description
			}

			plannedTasks = append(plannedTasks, t)
			// Use index as temporary ID for dependency resolution
			titleToID[pt.Title] = fmt.Sprintf("temp-%d", i)
		}

		// If no epic was attached and we have tasks, first task is the epic
		if epicTask == nil && len(plannedTasks) > 0 {
			plannedTasks[0].Title = "Epic: " + req.Goal
			plannedTasks[0].Description = req.Description
		}

		// Second pass: resolve dependencies by title
		for i, pt := range req.Tasks {
			var deps []string
			for _, depTitle := range pt.DependsOn {
				if _, ok := titleToID[depTitle]; ok {
					deps = append(deps, depTitle) // Will resolve after creation
				}
			}
			if len(deps) > 0 {
				plannedTasks[i].DependsOn = deps // Store titles temporarily
			}
		}
	} else {
		// No explicit tasks: try LLM planning or fall back to single epic
		planner := llm.AutoPlanner()
		if planner != nil && planner.Available() {
			// Use LLM to generate task decomposition
			maxTasks := req.MaxTasks
			if maxTasks == 0 {
				maxTasks = 20
			}
			maxDepth := req.MaxDepth
			if maxDepth == 0 {
				maxDepth = 3
			}

			llmResult, err := planner.Plan(ctx, llm.PlanRequest{
				Goal:        req.Goal,
				Description: req.Description,
				ScopePaths:  req.ScopePaths,
				MaxTasks:    maxTasks,
				MaxDepth:    maxDepth,
				Strategy:    req.Strategy,
			})
			if err != nil {
				// Warning output to stderr; error is not actionable.
				fmt.Fprintf(os.Stderr, "warning: LLM planning failed, falling back to simple epic: %v\n", err)
			} else {
				// Convert LLM tasks to internal tasks
				// Info output to stderr; error is not actionable.
				fmt.Fprintf(os.Stderr, "info: LLM planning generated %d tasks using %s\n", len(llmResult.Tasks), llmResult.ModelUsed)
				titleToID := make(map[string]string)
				for i, pt := range llmResult.Tasks {
					scopePath := pt.ScopePath
					if scopePath == "" && len(req.ScopePaths) > 0 {
						scopePath = req.ScopePaths[0]
					}
					t := tasks.Task{
						WorkspaceID: workspaceID,
						Title:       pt.Title,
						Description: pt.Description,
						ScopePath:   scopePath,
						Status:      statusPending,
						DependsOn:   pt.DependsOn, // Titles for now
						SessionID:   sessionID,
					}
					if epicTask != nil {
						t.ParentID = epicTask.ID
					}
					plannedTasks = append(plannedTasks, t)
					titleToID[pt.Title] = fmt.Sprintf("temp-%d", i)
				}
			}
		}

		// Fallback: create a single epic task from the goal
		if len(plannedTasks) == 0 {
			epicTitle := "Epic: " + req.Goal
			if epicTask != nil {
				epicTitle = req.Goal // Refining existing, don't prefix
			}

			t := tasks.Task{
				WorkspaceID: workspaceID,
				Title:       epicTitle,
				Description: req.Description,
				Status:      statusPending,
				SessionID:   sessionID,
			}
			if len(req.ScopePaths) > 0 {
				t.ScopePath = req.ScopePaths[0]
			}
			if epicTask != nil {
				t.ParentID = epicTask.ID
			}
			plannedTasks = append(plannedTasks, t)
		}
	}

	// Apply mode: actually create the tasks
	var createdTasks []*taskOutput
	titleToActualID := make(map[string]string)

	if mode == "apply" {
		for i := range plannedTasks {
			// Resolve title-based dependencies to actual IDs
			var resolvedDeps []string
			for _, depTitle := range plannedTasks[i].DependsOn {
				if actualID, ok := titleToActualID[depTitle]; ok {
					resolvedDeps = append(resolvedDeps, actualID)
				}
			}
			plannedTasks[i].DependsOn = resolvedDeps

			created, err := store.Add(ctx, plannedTasks[i])
			if err != nil {
				return nil, fmt.Errorf("create task %q: %w", plannedTasks[i].Title, err)
			}

			// Create graph edges for task relationships (parent_of, depends_on)
			createTaskDependencyEdges(ctx, cfg, workspaceID, created)

			titleToActualID[plannedTasks[i].Title] = created.ID
			addedIDs = append(addedIDs, created.ID)
			createdTasks = append(createdTasks, toOutput(created))

			// Set root task ID from first created task if not attached
			if rootTaskID == "" && i == 0 {
				rootTaskID = created.ID
			}
		}

		// Emit plan event via mailbox if boardStore is available
		if boardStore != nil && len(createdTasks) > 0 {
			eventType := "plan.created"
			if req.AttachToTaskID != "" {
				eventType = "plan.updated"
			}

			msg := agent.BoardMessage{
				WorkspaceID: workspaceID,
				TaskID:      rootTaskID,
				Sender:      "actor:system:overseer",
				Recipient:   "actor:agent:*", // Broadcast
				Kind:        agent.BoardMessageKindInfo,
				Priority:    3,
				Subject:     fmt.Sprintf("%s:%s", eventType, rootTaskID),
				Body:        fmt.Sprintf("Plan for %q: created %d tasks", req.Goal, len(createdTasks)),
			}
			if err := boardStore.SendMessage(ctx, &msg); err != nil {
				// Log but don't fail the operation.
				// Warning output to stderr; error is not actionable.
				fmt.Fprintf(os.Stderr, "warning: failed to send plan event: %v\n", err)
			}
		}
	} else {
		// Draft mode: just return the proposed tasks without IDs
		for _, t := range plannedTasks {
			out := &taskOutput{
				ID:          "(draft)",
				Title:       t.Title,
				Description: t.Description,
				ScopePath:   t.ScopePath,
				ParentID:    t.ParentID,
				DependsOn:   t.DependsOn,
				Status:      statusPending,
				CreatedAt:   time.Now().Format(time.RFC3339),
			}
			createdTasks = append(createdTasks, out)
		}
		if rootTaskID == "" {
			rootTaskID = "(draft)"
		}
	}

	// Build graph output if we have created tasks
	var graphOutput *planGraphOutput
	if mode == "apply" && len(addedIDs) > 0 {
		allTasks, err := store.ListByWorkspace(ctx, workspaceID)
		if err == nil {
			insights, err := tasksgraph.NewAnalyzer().Analyze(allTasks, workspaceID)
			if err == nil {
				var edges []planEdge
				for _, t := range allTasks {
					for _, dep := range t.DependsOn {
						edges = append(edges, planEdge{From: t.ID, To: dep})
					}
				}
				graphOutput = &planGraphOutput{
					Nodes:  insights.Nodes,
					Edges:  edges,
					Cycles: insights.Cycles,
				}
			}
		}
	}

	return &planOutput{
		RootTaskID: rootTaskID,
		Applied:    mode == "apply",
		Tasks:      createdTasks,
		Graph:      graphOutput,
		Diff: &planDiffOutput{
			AddedTaskIDs:   addedIDs,
			UpdatedTaskIDs: nil,
			RemovedTaskIDs: nil,
		},
	}, nil
}

func validateText(field, value string) error {
	if strings.ContainsRune(value, '`') {
		return fmt.Errorf("%s cannot contain backticks (`)", field)
	}
	return nil
}

func validateDependencies(depends []string, index map[string]*tasks.Task) error {
	seen := make(map[string]struct{})
	for _, dep := range depends {
		if dep == "" {
			return errors.New("dependency ids cannot be empty")
		}
		if _, ok := index[dep]; !ok {
			return fmt.Errorf("dependency %s not found", dep)
		}
		if _, ok := seen[dep]; ok {
			return fmt.Errorf("duplicate dependency %s", dep)
		}
		seen[dep] = struct{}{}
	}
	return nil
}

func buildIndex(taskList []tasks.Task) map[string]*tasks.Task {
	idx := make(map[string]*tasks.Task, len(taskList))
	for i := range taskList {
		idx[taskList[i].ID] = &taskList[i]
	}
	return idx
}

func dedupe(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func countPending(taskList []tasks.Task) int {
	count := 0
	for _, t := range taskList {
		if t.Status == statusPending {
			count++
		}
	}
	return count
}

func filterPending(taskList []tasks.Task) []tasks.Task {
	var out []tasks.Task
	for _, t := range taskList {
		if t.Status == statusPending {
			out = append(out, t)
		}
	}
	return out
}

func toOutput(t tasks.Task) *taskOutput {
	out := &taskOutput{
		ID:               t.ID,
		Title:            t.Title,
		Description:      t.Description,
		ScopePath:        t.ScopePath,
		ParentID:         t.ParentID,
		Children:         t.Children,
		DependsOn:        t.DependsOn,
		Status:           t.Status,
		CreatedAt:        t.CreatedAt.Format(time.RFC3339),
		Notes:            t.Notes,
		Gotchas:          t.Gotchas,
		LastReviewStatus: t.LastReviewStatus,
		LastReviewID:     t.LastReviewID,
	}
	if t.CompletedAt != nil {
		out.CompletedAt = t.CompletedAt.Format(time.RFC3339)
	}
	if t.LastReviewAt != nil {
		out.LastReviewAt = t.LastReviewAt.Format(time.RFC3339)
	}
	return out
}

func toOutputList(taskList []tasks.Task) []*taskOutput {
	out := make([]*taskOutput, len(taskList))
	for i, t := range taskList {
		out[i] = toOutput(t)
	}
	return out
}

func fail(command, code string, err error, hint string) {
	var data map[string]any
	if hint != "" {
		data = map[string]any{"hint": hint}
	}
	env := envelope.Error(command, code, err.Error(), data)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit todo failure")
	os.Exit(1)
}

// createTaskDependencyEdges creates graph edges for task relationships.
// Edge types:
// - parent_of: parent task → child task
// - depends_on: dependent task → dependency task
func createTaskDependencyEdges(ctx context.Context, cfg config.Config, workspaceID string, task tasks.Task) {
	// Open graph store (fail silently - graph is optional)
	graphStore, err := graph.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return
	}
	defer func() { errs.Ignore(graphStore.Close(), "close graph store") }()

	now := time.Now().UTC()

	// Ensure the task node exists
	taskNodeID := graph.TaskNodeID(task.ID)
	taskNode := graph.Node{
		Workspace: workspaceID,
		NodeID:    taskNodeID,
		NodeType:  graph.NodeTypeTask,
		Title:     task.Title,
		LastSeen:  now,
	}
	errs.Ignore(graphStore.UpsertNode(ctx, taskNode), "upsert task node")

	// Create parent_of edge: parent → child
	if task.ParentID != "" {
		parentNodeID := graph.TaskNodeID(task.ParentID)

		// Ensure parent node exists
		parentNode := graph.Node{
			Workspace: workspaceID,
			NodeID:    parentNodeID,
			NodeType:  graph.NodeTypeTask,
			Title:     task.ParentID, // Title will be updated by other operations
			LastSeen:  now,
		}
		errs.Ignore(graphStore.UpsertNode(ctx, parentNode), "upsert parent task node")

		// Edge: parent → child (parent_of)
		// Structural edges (parent_of, depends_on) should not expire - they represent
		// permanent task relationships, not temporal activity
		edge := graph.Edge{
			Workspace: workspaceID,
			FromID:    parentNodeID,
			FromType:  graph.NodeTypeTask,
			ToID:      taskNodeID,
			ToType:    graph.NodeTypeTask,
			EdgeType:  graph.EdgeTypeParentOf,
			Weight:    1.0,
			TTLDays:   nil, // No TTL for structural edges
			CreatedAt: now,
		}
		errs.Ignore(graphStore.UpsertEdge(ctx, edge), "upsert parent_of edge")
	}

	// Create depends_on edges: this task → dependency
	for _, depID := range task.DependsOn {
		depNodeID := graph.TaskNodeID(depID)

		// Ensure dependency node exists
		depNode := graph.Node{
			Workspace: workspaceID,
			NodeID:    depNodeID,
			NodeType:  graph.NodeTypeTask,
			Title:     depID, // Title will be updated by other operations
			LastSeen:  now,
		}
		errs.Ignore(graphStore.UpsertNode(ctx, depNode), "upsert dependency task node")

		// Edge: this task → dependency (depends_on)
		// Structural edges should not expire
		edge := graph.Edge{
			Workspace: workspaceID,
			FromID:    taskNodeID,
			FromType:  graph.NodeTypeTask,
			ToID:      depNodeID,
			ToType:    graph.NodeTypeTask,
			EdgeType:  graph.EdgeTypeDependsOn,
			Weight:    1.0,
			TTLDays:   nil, // No TTL for structural edges
			CreatedAt: now,
		}
		errs.Ignore(graphStore.UpsertEdge(ctx, edge), "upsert depends_on edge")
	}
}
