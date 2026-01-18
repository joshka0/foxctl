// Package main implements the todo/manage skill.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/artifacts"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/analysis/overseer"
	"github.com/jkatigb/agentctl/internal/analysis/tasksgraph"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/planning/llm"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/platform/env"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/rs/zerolog"
)

const command = "todo/manage"

// maxTasksForSyncPageRank is the threshold above which PageRank recomputation
// is skipped during mutations. For large workspaces, compute PageRank on-demand
// via list with ranked=true instead of synchronously after every mutation.
const maxTasksForSyncPageRank = 500

var allowedOps = []string{
	"add",
	"update",
	"complete",
	"list",
	"get_active",
	"set_active",
	"clear_active",
	"ensure_active",
	"graph_insights",
	"recommend",
	"plan",
	"review_request",
	"review_status",
	"search",
	"dedupe",
}

// isReviewGateEnabled reports whether the review gate is enabled for this
// process. For v1 this is controlled by an environment variable and applies to
// all workspaces.
func isReviewGateEnabled() bool {
	mode := strings.ToLower(env.GetString("AGENTCTL_TODO_REVIEW_GATE"))
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
	List          *listReq          `json:"list"`
	Dedupe        *dedupeReq        `json:"dedupe"`

	// CLI metadata (added by agentctl todo subcommands)
	CLICommand    string `json:"cli_command,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
}

// listReq defines the input for the list operation.
type listReq struct {
	Ranked         bool   `json:"ranked"`          // Include PageRank scores (default: false)
	Status         string `json:"status"`          // Filter by status: pending, in_progress, completed, blocked
	SortBy         string `json:"sort_by"`         // Sort by: created_at, pagerank, critical_path (default: created_at)
	IncludeMetrics bool   `json:"include_metrics"` // Include full graph metrics (degrees, critical path)
	TitleContains  string `json:"title_contains"`  // Case-insensitive substring match
	SessionID      string `json:"session_id"`
	Limit          int    `json:"limit"` // Limit number of results after sorting
}

type dedupeReq struct {
	Apply bool   `json:"apply"`
	Keep  string `json:"keep"`
	Limit int    `json:"limit"`
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
	SessionID   string   `json:"session_id,omitempty"` // Override runner context session_id
}

type completeRequest struct {
	ID        string `json:"id"`
	Notes     string `json:"notes"`
	Gotchas   string `json:"gotchas"`
	SessionID string `json:"session_id,omitempty"` // Optional: scope to specific session
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
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id,omitempty"` // Optional: scope to specific session
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
	SessionID   string   `json:"session_id,omitempty"`
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

	// Graph metrics (populated when ranked=true or include_metrics=true)
	PageRank          float64 `json:"pagerank,omitempty"`
	CriticalPathScore int     `json:"critical_path_score,omitempty"`
	InDegree          int     `json:"in_degree,omitempty"`
	OutDegree         int     `json:"out_degree,omitempty"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	cfg := rc.Config
	// Open SQLite-backed task store
	store, cleanup, err := sessionkit.OpenTasks(ctx, rc.Config)
	if err != nil {
		return skillerr.WrapIO("open task store", err)
	}
	defer cleanup()

	op := oputil.Op(in.Operation)
	if op == "" {
		op = "list"
	}
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}

	workspaceID := workspaceutil.ResolveID(in.WorkspaceID, rc.Workspace)

	var data map[string]any

	switch op {
	case "add":
		// Prefer session_id from input, fallback to runner context
		addSessionID := rc.SessionID
		if in.Add != nil && strings.TrimSpace(in.Add.SessionID) != "" {
			addSessionID = strings.TrimSpace(in.Add.SessionID)
		}
		task, allTasks, err := handleAdd(ctx, store, cfg, workspaceID, addSessionID, in.Add)
		if err != nil {
			return err
		}
		// Recompute and persist PageRank after adding task
		persistPageRanks(ctx, store, workspaceID, rc.Logger)
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
		// Recompute and persist PageRank after updating task (dependencies may have changed)
		persistPageRanks(ctx, store, workspaceID, rc.Logger)
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
		// Recompute and persist PageRank after completing task (graph structure changed)
		persistPageRanks(ctx, store, workspaceID, rc.Logger)
		data = map[string]any{
			"task":          task,
			"pending_tasks": countPending(allTasks),
			"summary":       fmt.Sprintf("completed task %s", task.ID),
		}

	case "list":
		allTasks, err := store.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return skillerr.WrapIO("list tasks", err)
		}

		if in.List != nil {
			if in.List.Status != "" {
				allTasks = filterByStatus(allTasks, in.List.Status)
			}
			if strings.TrimSpace(in.List.TitleContains) != "" {
				allTasks = filterByTitleContains(allTasks, in.List.TitleContains)
			}
			if strings.TrimSpace(in.List.SessionID) != "" {
				allTasks = filterBySessionID(allTasks, in.List.SessionID)
			}
		}

		// Check if we need graph metrics
		needsMetrics := in.List != nil && (in.List.Ranked || in.List.IncludeMetrics)
		var metricsMap map[string]tasksgraph.NodeMetrics
		var sortBy string

		if needsMetrics {
			// Compute graph metrics using tasksgraph analyzer
			insights, err := tasksgraph.NewAnalyzer().Analyze(allTasks, workspaceID)
			if err == nil {
				metricsMap = make(map[string]tasksgraph.NodeMetrics)
				for _, m := range insights.Nodes {
					metricsMap[m.TaskID] = m
				}
			}
		}

		// Determine sort order
		if in.List != nil && in.List.SortBy != "" {
			sortBy = in.List.SortBy
		}

		// Sort tasks based on sortBy
		if sortBy == "pagerank" && metricsMap != nil {
			sort.Slice(allTasks, func(i, j int) bool {
				return metricsMap[allTasks[i].ID].PageRank > metricsMap[allTasks[j].ID].PageRank
			})
		} else if sortBy == "critical_path" && metricsMap != nil {
			sort.Slice(allTasks, func(i, j int) bool {
				return metricsMap[allTasks[i].ID].CriticalPathScore > metricsMap[allTasks[j].ID].CriticalPathScore
			})
		}
		// Default sort is by creation time (already the case from store)

		if in.List != nil && in.List.Limit > 0 && len(allTasks) > in.List.Limit {
			allTasks = allTasks[:in.List.Limit]
		}

		// Convert to output with optional metrics
		taskOutputs := toOutputListWithMetrics(allTasks, metricsMap)

		data = map[string]any{
			"tasks":         taskOutputs,
			"total_tasks":   len(allTasks),
			"pending_tasks": countPending(allTasks),
			"ranked":        needsMetrics,
		}

	case "get_active":
		task, found, err := store.GetActive(ctx, workspaceID)
		if err != nil {
			return skillerr.WrapIO("get active task", err)
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
			return skillerr.Arg("set_active.task_id is required")
		}
		task, err := store.SetActive(ctx, workspaceID, in.SetActive.TaskID)
		if err != nil {
			return skillerr.WrapIO("set active task", err)
		}
		data = map[string]any{
			"task":    toOutput(task),
			"summary": fmt.Sprintf("set active task to %s", task.ID),
		}

	case "clear_active":
		if err := store.ClearActive(ctx, workspaceID); err != nil {
			return skillerr.WrapIO("clear active task", err)
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
			return skillerr.WrapIO("ensure active task", err)
		}
		data = map[string]any{
			"task":    toOutput(task),
			"created": created,
			"summary": fmt.Sprintf("active task is %s (created=%v)", task.ID, created),
		}

	case "graph_insights":
		allTasks, err := store.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return skillerr.WrapIO("list tasks", err)
		}
		// Filter completed tasks if not requested
		if in.GraphInsights == nil || !in.GraphInsights.IncludeCompleted {
			allTasks = filterPending(allTasks)
		}
		insights, err := tasksgraph.NewAnalyzer().Analyze(allTasks, workspaceID)
		if err != nil {
			return skillerr.WrapRuntime("analyze task graph", err)
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
			return skillerr.WrapRuntime("recommend tasks", err)
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
		planResult, err := handlePlan(ctx, store, boardStore, cfg, workspaceID, rc.SessionID, in.Plan, rc.Logger)
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
			// Recompute and persist PageRank after applying plan (new tasks created)
			persistPageRanks(ctx, store, workspaceID, rc.Logger)
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
		results, err := handleSearch(ctx, store, cfg, workspaceID, in.Search, rc.Logger)
		if err != nil {
			return err
		}
		data = map[string]any{
			"results":     results.Tasks,
			"total_found": results.TotalFound,
			"query":       results.Query,
			"summary":     fmt.Sprintf("found %d tasks matching %q", results.TotalFound, results.Query),
		}

	case "dedupe":
		out, err := handleDedupe(ctx, store, workspaceID, in.Dedupe)
		if err != nil {
			return err
		}
		data = out

	default:
		return skillerr.Argf("unknown operation %q (expected add|update|complete|list|get_active|set_active|clear_active|ensure_active|graph_insights|recommend|plan|review_request|review_status|search|dedupe)", op)
	}

	return skillout.Emit(rc, command, data)
}

func handleAdd(ctx context.Context, store tasks.Store, cfg config.Config, workspaceID, sessionID string, req *addRequest) (*taskOutput, []tasks.Task, error) {
	if req == nil {
		return nil, nil, skillerr.Arg("add payload is required")
	}
	if err := validateText("title", req.Title); err != nil {
		return nil, nil, err
	}
	if err := validateText("description", req.Description); err != nil {
		return nil, nil, err
	}
	if req.Title == "" {
		return nil, nil, skillerr.Arg("title is required")
	}

	// Validate parent and dependencies exist
	allTasks, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, skillerr.WrapIO("list tasks", err)
	}
	index := buildIndex(allTasks)

	if req.ParentID != "" {
		parent, ok := index[req.ParentID]
		if !ok {
			return nil, nil, skillerr.NotFoundf("parent task %s not found", req.ParentID)
		}
		if parent.Status == tasks.StatusCompleted {
			return nil, nil, skillerr.Validationf("cannot add child to completed task %s", req.ParentID)
		}
	}
	if err := validateDependencies(req.DependsOn, index); err != nil {
		return nil, nil, err
	}

	normalizedTitle := normalizeTaskTitle(req.Title)
	existing := findOpenDuplicateTask(allTasks, workspaceID, sessionID, req.ParentID, normalizedTitle)
	if existing != nil && strings.TrimSpace(existing.ScopePath) != "" && strings.TrimSpace(req.ScopePath) != "" && existing.ScopePath != req.ScopePath {
		existing = nil
	}
	if existing != nil {
		changed := false

		if existing.Description == "" && strings.TrimSpace(req.Description) != "" {
			existing.Description = req.Description
			changed = true
		}
		if existing.ScopePath == "" && strings.TrimSpace(req.ScopePath) != "" {
			existing.ScopePath = req.ScopePath
			changed = true
		}

		mergedDeps := mergeStringIDs(existing.DependsOn, dedupe(req.DependsOn))
		if !equalStringSets(existing.DependsOn, mergedDeps) {
			existing.DependsOn = mergedDeps
			changed = true
		}

		updated := *existing
		if changed {
			var err error
			updated, err = store.Update(ctx, updated)
			if err != nil {
				return nil, nil, skillerr.WrapIO("update existing task", err)
			}
		}

		createTaskDependencyEdges(ctx, cfg, workspaceID, updated, nil)

		if req.ParentID != "" {
			parent := index[req.ParentID]
			if !containsString(parent.Children, updated.ID) {
				parent.Children = append(parent.Children, updated.ID)
				if _, err := store.Update(ctx, *parent); err != nil {
					return nil, nil, skillerr.WrapIO("update parent children", err)
				}
			}
		}

		allTasks, err = store.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return nil, nil, skillerr.WrapIO("list tasks", err)
		}

		return toOutput(updated), allTasks, nil
	}

	newTask := tasks.Task{
		WorkspaceID: workspaceID,
		Title:       req.Title,
		Description: req.Description,
		ScopePath:   req.ScopePath,
		ParentID:    req.ParentID,
		DependsOn:   dedupe(req.DependsOn),
		Status:      tasks.StatusPending,
		SessionID:   sessionID,
	}

	added, err := store.Add(ctx, newTask)
	if err != nil {
		return nil, nil, skillerr.WrapIO("add task", err)
	}

	createTaskDependencyEdges(ctx, cfg, workspaceID, added, nil)

	if req.ParentID != "" {
		parent := index[req.ParentID]
		if !containsString(parent.Children, added.ID) {
			parent.Children = append(parent.Children, added.ID)
			if _, err := store.Update(ctx, *parent); err != nil {
				return nil, nil, skillerr.WrapIO("update parent children", err)
			}
		}
	}

	allTasks, err = store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, skillerr.WrapIO("list tasks", err)
	}

	return toOutput(added), allTasks, nil
}

func handleUpdate(ctx context.Context, store tasks.Store, workspaceID string, req *updateRequest) (*taskOutput, []tasks.Task, error) {
	if req == nil {
		return nil, nil, skillerr.Arg("update payload is required")
	}
	if req.ID == "" {
		return nil, nil, skillerr.Arg("update.id is required")
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
			return nil, nil, skillerr.NotFoundf("task %s not found", req.ID)
		}
		return nil, nil, skillerr.WrapIO("get task "+req.ID, err)
	}

	// Verify workspace ownership
	if task.WorkspaceID != workspaceID {
		return nil, nil, skillerr.Validationf("task %s belongs to a different workspace", req.ID)
	}

	// Don't allow updating completed tasks
	if task.Status == tasks.StatusCompleted {
		return nil, nil, skillerr.Validationf("cannot update completed task %s", req.ID)
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
		case tasks.StatusPending, tasks.StatusInProgress, tasks.StatusBlocked:
			task.Status = req.Status
		default:
			return nil, nil, skillerr.Validationf("invalid status %q (use complete action for completing tasks)", req.Status)
		}
	}

	updated, err := store.Update(ctx, task)
	if err != nil {
		return nil, nil, skillerr.WrapIO("update task", err)
	}

	allTasks, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, skillerr.WrapIO("list tasks", err)
	}

	return toOutput(updated), allTasks, nil
}

func handleComplete(ctx context.Context, store tasks.Store, workspaceID string, req *completeRequest) (*taskOutput, []tasks.Task, error) {
	if req == nil {
		return nil, nil, skillerr.Arg("complete payload is required")
	}
	if req.ID == "" {
		return nil, nil, skillerr.Arg("complete.id is required")
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
			return nil, nil, skillerr.NotFoundf("task %s not found", req.ID)
		}
		return nil, nil, skillerr.WrapIO("get task "+req.ID, err)
	}

	// Verify workspace ownership
	if task.WorkspaceID != workspaceID {
		return nil, nil, skillerr.Validationf("task %s belongs to a different workspace", req.ID)
	}

	if task.Status == tasks.StatusCompleted {
		return nil, nil, skillerr.Validationf("task %s already completed", req.ID)
	}

	// Check dependencies
	allTasks, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, skillerr.WrapIO("list tasks", err)
	}
	index := buildIndex(allTasks)

	for _, dep := range task.DependsOn {
		if depTask, ok := index[dep]; ok {
			if depTask.Status != tasks.StatusCompleted {
				return nil, nil, skillerr.Validationf("task %s depends on incomplete task %s", req.ID, dep)
			}
		} else {
			return nil, nil, skillerr.NotFoundf("dependency %s for task %s not found", dep, req.ID)
		}
	}

	// Enforce review gate semantics when enabled.
	if isReviewGateEnabled() {
		if task.Status != tasks.StatusReadyForReview {
			return nil, nil, skillerr.Validationf("task %s must be ready_for_review before completion", req.ID)
		}
		if task.LastReviewStatus != tasks.ReviewStatusOK || task.LastReviewID == "" {
			return nil, nil, skillerr.Validationf("task %s requires an 'ok' review before completion", req.ID)
		}
	}

	now := time.Now().UTC()
	task.Status = tasks.StatusCompleted
	task.CompletedAt = &now
	task.Notes = strings.TrimSpace(req.Notes)
	task.Gotchas = strings.TrimSpace(req.Gotchas)

	updated, err := store.Update(ctx, task)
	if err != nil {
		return nil, nil, skillerr.WrapIO("update task", err)
	}

	// Refresh task list
	allTasks, err = store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, nil, skillerr.WrapIO("list tasks", err)
	}

	return toOutput(updated), allTasks, nil
}

// handleReviewRequest initiates a review for a task per review_gate.md.
// It validates that the task is in an allowed state (in_progress or ready_for_review),
// then sets status to ready_for_review and LastReviewStatus to pending.
func handleReviewRequest(ctx context.Context, store tasks.Store, workspaceID string, cfg config.Config, req *reviewRequestReq) (*taskOutput, error) {
	if req == nil {
		return nil, skillerr.Arg("review_request payload is required")
	}
	if req.TaskID == "" {
		return nil, skillerr.Arg("review_request.task_id is required")
	}

	task, err := store.Get(ctx, req.TaskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, skillerr.NotFoundf("task %s not found", req.TaskID)
		}
		return nil, skillerr.WrapIO("get task "+req.TaskID, err)
	}

	// Verify workspace ownership
	if task.WorkspaceID != workspaceID {
		return nil, skillerr.Validationf("task %s belongs to a different workspace", req.TaskID)
	}

	// Validate task state: only in_progress or ready_for_review can be reviewed
	switch task.Status {
	case tasks.StatusInProgress, tasks.StatusReadyForReview:
		// OK
	case tasks.StatusPending:
		return nil, skillerr.Validationf("task %s is pending; start work before requesting review", req.TaskID)
	case tasks.StatusCompleted:
		return nil, skillerr.Validationf("task %s is already completed", req.TaskID)
	case tasks.StatusBlocked:
		return nil, skillerr.Validationf("task %s is blocked; resolve blockers before requesting review", req.TaskID)
	case tasks.StatusCanceled:
		return nil, skillerr.Validationf("task %s is canceled", req.TaskID)
	default:
		return nil, skillerr.Validationf("task %s has unknown status %q", req.TaskID, task.Status)
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
		return nil, skillerr.WrapIO("store review artifact", err)
	}

	// Transition to ready_for_review and mark review as pending
	task.Status = tasks.StatusReadyForReview
	task.LastReviewStatus = tasks.ReviewStatusPending
	now := time.Now().UTC()
	task.LastReviewAt = &now
	task.LastReviewID = review.ID

	updated, err := store.Update(ctx, task)
	if err != nil {
		return nil, skillerr.WrapIO("update task", err)
	}

	return toOutput(updated), nil
}

// handleReviewStatus returns the review status fields for a task.
// This is a cheap status probe that does not touch CAS or jobs.
func handleReviewStatus(ctx context.Context, store tasks.Store, workspaceID string, req *reviewStatusReq) (tasks.Task, error) {
	if req == nil {
		return tasks.Task{}, skillerr.Arg("review_status payload is required")
	}
	if req.TaskID == "" {
		return tasks.Task{}, skillerr.Arg("review_status.task_id is required")
	}

	task, err := store.Get(ctx, req.TaskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tasks.Task{}, skillerr.NotFoundf("task %s not found", req.TaskID)
		}
		return tasks.Task{}, skillerr.WrapIO("get task "+req.TaskID, err)
	}

	// Verify workspace ownership
	if task.WorkspaceID != workspaceID {
		return tasks.Task{}, skillerr.Validationf("task %s belongs to a different workspace", req.TaskID)
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
func handleSearch(ctx context.Context, store tasks.Store, cfg config.Config, workspaceID string, req *searchReq, logger zerolog.Logger) (*searchOutput, error) {
	if req == nil {
		return nil, skillerr.Arg("search payload is required")
	}
	if req.Query == "" {
		return nil, skillerr.Arg("search.query is required")
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
		return nil, skillerr.Auth("no embedding API key set", skillerr.WithHint("Set GEMINI_API_KEY or VOYAGE_API_KEY."))
	}

	embedder, err := semantic.NewEmbedderFromConfig(
		semantic.ScopeTasks,
		cfg,
		semantic.WithVoyageKey(voyageKey),
		semantic.WithGeminiKey(geminiKey),
	)
	if err != nil {
		return nil, skillerr.WrapRuntime("embedding provider", err)
	}

	// Generate query embedding
	embedResult, err := embedder.Embed(ctx, req.Query)
	if err != nil {
		return nil, skillerr.WrapRuntime("embed query", err)
	}
	queryEmbedding := embedResult.Vec

	// Open memory store for vector search
	memStore, err := memory.OpenWithConfig(ctx, cfg)
	if err != nil {
		return nil, skillerr.WrapIO("open memory store", err)
	}
	defer memStore.Close() //nolint:errcheck

	// Search for similar entries using in-memory cosine similarity
	// This searches all embeddings in the workspace
	entries, err := memStore.SearchSimilar(ctx, workspaceID, queryEmbedding, limit*2) // Fetch more to allow filtering
	if err != nil {
		// Fall back to listing all tasks if similarity search fails
		logger.Warn().Err(err).Msg("similarity search failed, falling back to text match")

		// Simple fallback: text-based search over all tasks
		allTasks, err := store.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return nil, skillerr.WrapIO("list tasks", err)
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

type dedupeGroup struct {
	Key          string   `json:"key"`
	Title        string   `json:"title"`
	ParentID     string   `json:"parent_id"`
	ScopePath    string   `json:"scope_path"`
	SessionID    string   `json:"session_id,omitempty"`
	Count        int      `json:"count"`
	KeptID       string   `json:"kept_id"`
	DuplicateIDs []string `json:"duplicate_ids"`
}

func handleDedupe(ctx context.Context, store tasks.Store, workspaceID string, req *dedupeReq) (map[string]any, error) {
	apply := false
	keep := "newest"
	limit := 50
	if req != nil {
		apply = req.Apply
		if strings.TrimSpace(req.Keep) != "" {
			keep = strings.ToLower(strings.TrimSpace(req.Keep))
		}
		if req.Limit > 0 {
			limit = req.Limit
		}
	}
	if keep != "newest" && keep != "oldest" {
		return nil, skillerr.Validationf("dedupe.keep must be newest or oldest, got %q", keep)
	}

	allTasks, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, skillerr.WrapIO("list tasks", err)
	}

	type groupInfo struct {
		Key       string
		Title     string
		ParentID  string
		ScopePath string
		SessionID string
		Tasks     []*tasks.Task
	}

	groups := make(map[string]*groupInfo)
	for i := range allTasks {
		t := &allTasks[i]
		if t.WorkspaceID != workspaceID {
			continue
		}
		if !isOpenForDedupe(t.Status) {
			continue
		}
		normTitle := normalizeTaskTitle(t.Title)
		if normTitle == "" {
			continue
		}
		sessionKey := strings.TrimSpace(t.SessionID)
		scopeKey := strings.ToLower(strings.TrimSpace(t.ScopePath))
		key := fmt.Sprintf("%s|%s|%s|%s", sessionKey, t.ParentID, normTitle, scopeKey)
		g := groups[key]
		if g == nil {
			g = &groupInfo{Key: key, Title: t.Title, ParentID: t.ParentID, ScopePath: t.ScopePath, SessionID: t.SessionID}
			groups[key] = g
		}
		g.Tasks = append(g.Tasks, t)
	}

	duplicateGroups := make([]*groupInfo, 0)
	for _, g := range groups {
		if len(g.Tasks) < 2 {
			continue
		}
		duplicateGroups = append(duplicateGroups, g)
	}

	sort.Slice(duplicateGroups, func(i, j int) bool {
		return len(duplicateGroups[i].Tasks) > len(duplicateGroups[j].Tasks)
	})
	if limit > 0 && len(duplicateGroups) > limit {
		duplicateGroups = duplicateGroups[:limit]
	}

	dupToKept := make(map[string]string)
	outGroups := make([]dedupeGroup, 0, len(duplicateGroups))
	duplicateTaskCount := 0

	for _, g := range duplicateGroups {
		sort.Slice(g.Tasks, func(i, j int) bool {
			if keep == "oldest" {
				return g.Tasks[i].CreatedAt.Before(g.Tasks[j].CreatedAt)
			}
			return g.Tasks[i].CreatedAt.After(g.Tasks[j].CreatedAt)
		})

		kept := g.Tasks[0]
		dupIDs := make([]string, 0, len(g.Tasks)-1)
		for _, t := range g.Tasks[1:] {
			dupToKept[t.ID] = kept.ID
			dupIDs = append(dupIDs, t.ID)
		}
		duplicateTaskCount += len(dupIDs)

		outGroups = append(outGroups, dedupeGroup{
			Key:          g.Key,
			Title:        kept.Title,
			ParentID:     kept.ParentID,
			ScopePath:    kept.ScopePath,
			SessionID:    kept.SessionID,
			Count:        len(g.Tasks),
			KeptID:       kept.ID,
			DuplicateIDs: dupIDs,
		})
	}

	canceledCount := 0
	updatedCount := 0

	if apply {
		for i := range allTasks {
			t := allTasks[i]
			if !isOpenForDedupe(t.Status) {
				continue
			}
			if len(t.DependsOn) == 0 {
				continue
			}

			changed := false
			newDeps := make([]string, 0, len(t.DependsOn))
			for _, dep := range t.DependsOn {
				if kept, ok := dupToKept[dep]; ok {
					dep = kept
					changed = true
				}
				newDeps = append(newDeps, dep)
			}
			newDeps = mergeStringIDs([]string{}, newDeps)
			if !changed && equalStringSets(t.DependsOn, newDeps) {
				continue
			}
			t.DependsOn = newDeps
			if t.Children == nil {
				t.Children = []string{}
			}
			if t.DependsOn == nil {
				t.DependsOn = []string{}
			}
			if _, err := store.Update(ctx, t); err != nil {
				return nil, skillerr.WrapIO("update dependencies for "+t.ID, err)
			}
			updatedCount++
		}

		for _, g := range outGroups {
			if g.ParentID == "" {
				continue
			}
			parent, err := store.Get(ctx, g.ParentID)
			if err != nil {
				continue
			}
			if parent.Children == nil {
				parent.Children = []string{}
			}

			childSet := make([]string, 0, len(parent.Children))
			for _, child := range parent.Children {
				if _, ok := dupToKept[child]; ok {
					continue
				}
				childSet = append(childSet, child)
			}
			if !containsString(childSet, g.KeptID) {
				childSet = append(childSet, g.KeptID)
			}
			parent.Children = mergeStringIDs([]string{}, childSet)
			if _, err := store.Update(ctx, parent); err != nil {
				return nil, skillerr.WrapIO("update parent children "+parent.ID, err)
			}
		}

		for dupID, keptID := range dupToKept {
			t, err := store.Get(ctx, dupID)
			if err != nil {
				continue
			}
			if t.Status == tasks.StatusCanceled {
				continue
			}
			note := fmt.Sprintf("superseded by %s", keptID)
			if strings.TrimSpace(t.Notes) == "" {
				t.Notes = note
			} else if !strings.Contains(t.Notes, note) {
				t.Notes = t.Notes + "\n" + note
			}
			t.Status = tasks.StatusCanceled
			if t.Children == nil {
				t.Children = []string{}
			}
			if t.DependsOn == nil {
				t.DependsOn = []string{}
			}
			if _, err := store.Update(ctx, t); err != nil {
				return nil, skillerr.WrapIO("cancel duplicate "+dupID, err)
			}
			canceledCount++
		}
	}

	summary := fmt.Sprintf("duplicate_groups=%d duplicate_tasks=%d", len(outGroups), duplicateTaskCount)
	if apply {
		summary = fmt.Sprintf("%s canceled=%d updated=%d", summary, canceledCount, updatedCount)
	}

	return map[string]any{
		"applied":          apply,
		"groups":           outGroups,
		"groups_count":     len(outGroups),
		"duplicate_tasks":  duplicateTaskCount,
		"canceled_tasks":   canceledCount,
		"updated_tasks":    updatedCount,
		"total_open_tasks": countOpenTasks(allTasks),
		"summary":          summary,
	}, nil
}

func countOpenTasks(taskList []tasks.Task) int {
	count := 0
	for _, t := range taskList {
		if isOpenForDedupe(t.Status) {
			count++
		}
	}
	return count
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
func handlePlan(ctx context.Context, store tasks.Store, boardStore blackboard.BoardStore, cfg config.Config, workspaceID, sessionID string, req *planReq, logger zerolog.Logger) (*planOutput, error) {
	if req == nil {
		return nil, skillerr.Arg("plan payload is required")
	}
	if req.Goal == "" {
		return nil, skillerr.Arg("plan.goal is required")
	}

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "draft"
	}
	if mode != "draft" && mode != "apply" {
		return nil, skillerr.Validationf("plan.mode must be 'draft' or 'apply', got %q", mode)
	}

	// Determine the root task (epic)
	var rootTaskID string
	var epicTask *tasks.Task

	if req.AttachToTaskID != "" {
		// Refining an existing epic
		existing, err := store.Get(ctx, req.AttachToTaskID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, skillerr.NotFoundf("attach_to_task_id %s not found", req.AttachToTaskID)
			}
			return nil, skillerr.WrapIO("attach_to_task_id "+req.AttachToTaskID, err)
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
				Status:      tasks.StatusPending,
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
		planner := llm.AutoPlannerFromConfig(llm.ProviderConfig{
			Provider:         cfg.LLM.Provider,
			Model:            cfg.LLM.Model,
			APIKey:           cfg.LLM.APIKey,
			OpenRouterAPIKey: cfg.LLM.OpenRouterAPIKey,
			OpenRouterModel:  cfg.LLM.OpenRouterModel,
			GroqAPIKey:       cfg.LLM.GroqAPIKey,
			OpenAIAPIKey:     cfg.LLM.OpenAIAPIKey,
		})
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
				logger.Warn().Err(err).Msg("llm planning failed, falling back to simple epic")
			} else {
				// Convert LLM tasks to internal tasks
				logger.Info().
					Int("task_count", len(llmResult.Tasks)).
					Str("model", llmResult.ModelUsed).
					Msg("llm planning generated tasks")
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
						Status:      tasks.StatusPending,
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
				Status:      tasks.StatusPending,
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
		allTasks, err := store.ListByWorkspace(ctx, workspaceID)
		if err != nil {
			return nil, skillerr.WrapIO("list tasks", err)
		}

		// Open graph store once for all tasks in this batch (fail silently - graph is optional)
		graphStore, _ := graph.Open(ctx, cfg.Storage.Root)
		if graphStore != nil {
			defer func() { errs.Ignore(graphStore.Close(), "close graph store") }()
		}

		for i := range plannedTasks {
			// Resolve title-based dependencies to actual IDs
			var resolvedDeps []string
			for _, depTitle := range plannedTasks[i].DependsOn {
				if actualID, ok := titleToActualID[depTitle]; ok {
					resolvedDeps = append(resolvedDeps, actualID)
				}
			}
			plannedTasks[i].DependsOn = resolvedDeps

			normalizedTitle := normalizeTaskTitle(plannedTasks[i].Title)
			existing := findOpenDuplicateTask(allTasks, workspaceID, sessionID, plannedTasks[i].ParentID, normalizedTitle)
			if existing != nil && strings.TrimSpace(existing.ScopePath) != "" && strings.TrimSpace(plannedTasks[i].ScopePath) != "" && existing.ScopePath != plannedTasks[i].ScopePath {
				existing = nil
			}
			if existing != nil {
				changed := false

				if existing.Description == "" && strings.TrimSpace(plannedTasks[i].Description) != "" {
					existing.Description = plannedTasks[i].Description
					changed = true
				}
				if existing.ScopePath == "" && strings.TrimSpace(plannedTasks[i].ScopePath) != "" {
					existing.ScopePath = plannedTasks[i].ScopePath
					changed = true
				}

				mergedDeps := mergeStringIDs(existing.DependsOn, plannedTasks[i].DependsOn)
				if !equalStringSets(existing.DependsOn, mergedDeps) {
					existing.DependsOn = mergedDeps
					changed = true
				}

				updated := *existing
				if changed {
					var err error
					updated, err = store.Update(ctx, updated)
					if err != nil {
						return nil, skillerr.WrapIO("update existing task "+plannedTasks[i].Title, err)
					}
				}

				createTaskDependencyEdges(ctx, cfg, workspaceID, updated, graphStore)
				if err := ensureParentHasChild(ctx, store, updated.ParentID, updated.ID); err != nil {
					return nil, err
				}

				titleToActualID[plannedTasks[i].Title] = updated.ID
				createdTasks = append(createdTasks, toOutput(updated))
				if rootTaskID == "" && i == 0 {
					rootTaskID = updated.ID
				}
				continue
			}

			created, err := store.Add(ctx, plannedTasks[i])
			if err != nil {
				return nil, skillerr.WrapIO("create task "+plannedTasks[i].Title, err)
			}
			allTasks = append(allTasks, created)

			createTaskDependencyEdges(ctx, cfg, workspaceID, created, graphStore)
			if err := ensureParentHasChild(ctx, store, created.ParentID, created.ID); err != nil {
				return nil, err
			}

			titleToActualID[plannedTasks[i].Title] = created.ID
			addedIDs = append(addedIDs, created.ID)
			createdTasks = append(createdTasks, toOutput(created))
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
				logger.Warn().Err(err).Msg("failed to send plan event")
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
				Status:      tasks.StatusPending,
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
		return skillerr.Validationf("%s cannot contain backticks (`)", field)
	}
	return nil
}

func validateDependencies(depends []string, index map[string]*tasks.Task) error {
	seen := make(map[string]struct{})
	for _, dep := range depends {
		if dep == "" {
			return skillerr.Validation("dependency ids cannot be empty")
		}
		if _, ok := index[dep]; !ok {
			return skillerr.NotFoundf("dependency %s not found", dep)
		}
		if _, ok := seen[dep]; ok {
			return skillerr.Validationf("duplicate dependency %s", dep)
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

func normalizeTaskTitle(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" {
		return ""
	}
	return strings.Join(strings.Fields(t), " ")
}

func isOpenForDedupe(status string) bool {
	s := strings.TrimSpace(status)
	if s == "" {
		return false
	}
	if s == tasks.StatusCompleted || s == tasks.StatusCanceled {
		return false
	}
	return true
}

func findOpenDuplicateTask(taskList []tasks.Task, workspaceID, sessionID, parentID, normalizedTitle string) *tasks.Task {
	if normalizedTitle == "" {
		return nil
	}

	sid := strings.TrimSpace(sessionID)

	var best *tasks.Task
	for i := range taskList {
		t := &taskList[i]
		if t.WorkspaceID != workspaceID {
			continue
		}
		if strings.TrimSpace(t.SessionID) != sid {
			continue
		}
		if t.ParentID != parentID {
			continue
		}
		if !isOpenForDedupe(t.Status) {
			continue
		}
		if normalizeTaskTitle(t.Title) != normalizedTitle {
			continue
		}
		if best == nil || t.CreatedAt.After(best.CreatedAt) {
			best = t
		}
	}
	return best
}

func mergeStringIDs(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, v := range a {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	for _, v := range b {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return []string{}
	}
	return out
}

func equalStringSets(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	seen := make(map[string]int, len(a))
	for _, v := range a {
		seen[v]++
	}
	for _, v := range b {
		if seen[v] == 0 {
			return false
		}
		seen[v]--
		if seen[v] == 0 {
			delete(seen, v)
		}
	}
	return len(seen) == 0
}

func containsString(items []string, value string) bool {
	for _, v := range items {
		if v == value {
			return true
		}
	}
	return false
}

func ensureParentHasChild(ctx context.Context, store tasks.Store, parentID, childID string) error {
	if parentID == "" {
		return nil
	}
	parent, err := store.Get(ctx, parentID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return skillerr.NotFoundf("parent %s not found", parentID)
		}
		return skillerr.WrapIO("get parent "+parentID, err)
	}
	if parent.Children == nil {
		parent.Children = []string{}
	}
	if containsString(parent.Children, childID) {
		return nil
	}
	parent.Children = append(parent.Children, childID)
	if _, err := store.Update(ctx, parent); err != nil {
		return skillerr.WrapIO("update parent children", err)
	}
	return nil
}

func countPending(taskList []tasks.Task) int {
	count := 0
	for _, t := range taskList {
		if t.Status == tasks.StatusPending {
			count++
		}
	}
	return count
}

func filterPending(taskList []tasks.Task) []tasks.Task {
	var out []tasks.Task
	for _, t := range taskList {
		if t.Status == tasks.StatusPending {
			out = append(out, t)
		}
	}
	return out
}

func filterByStatus(taskList []tasks.Task, status string) []tasks.Task {
	if status == "" {
		return taskList
	}
	out := make([]tasks.Task, 0)
	for _, t := range taskList {
		if t.Status == status {
			out = append(out, t)
		}
	}
	return out
}

func filterByTitleContains(taskList []tasks.Task, query string) []tasks.Task {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return taskList
	}
	out := make([]tasks.Task, 0)
	for _, t := range taskList {
		if strings.Contains(strings.ToLower(t.Title), q) {
			out = append(out, t)
		}
	}
	return out
}

func filterBySessionID(taskList []tasks.Task, sessionID string) []tasks.Task {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return taskList
	}
	out := make([]tasks.Task, 0)
	for _, t := range taskList {
		if strings.TrimSpace(t.SessionID) == sid {
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
		SessionID:        t.SessionID,
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

func toOutputListWithMetrics(taskList []tasks.Task, metrics map[string]tasksgraph.NodeMetrics) []*taskOutput {
	out := make([]*taskOutput, len(taskList))
	for i, t := range taskList {
		out[i] = toOutput(t)
		if metrics != nil {
			if m, ok := metrics[t.ID]; ok {
				out[i].PageRank = m.PageRank
				out[i].CriticalPathScore = m.CriticalPathScore
				out[i].InDegree = m.InDegree
				out[i].OutDegree = m.OutDegree
			}
		}
	}
	return out
}

// persistPageRanks recomputes PageRank for all tasks in a workspace and persists the scores.
// This should be called after any mutation that changes the task graph (add, complete, update with deps).
func persistPageRanks(ctx context.Context, store tasks.Store, workspaceID string, logger zerolog.Logger) {
	// List all tasks in the workspace
	allTasks, err := store.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return // Fail silently - PageRank is optional enhancement
	}

	if len(allTasks) == 0 {
		return
	}

	// Skip synchronous PageRank for large workspaces to avoid hangs.
	// PageRank will be computed on-demand when list is called with ranked=true.
	if len(allTasks) > maxTasksForSyncPageRank {
		logger.Debug().
			Int("task_count", len(allTasks)).
			Int("threshold", maxTasksForSyncPageRank).
			Msg("skipping synchronous PageRank recomputation for large workspace")
		return
	}

	// Compute PageRank using tasksgraph analyzer
	insights, err := tasksgraph.NewAnalyzer().Analyze(allTasks, workspaceID)
	if err != nil {
		return // Fail silently
	}

	// Build map of task ID -> PageRank score
	ranks := make(map[string]float64, len(insights.Nodes))
	for _, node := range insights.Nodes {
		ranks[node.TaskID] = node.PageRank
	}

	// Persist to database
	if err := store.SetPageRanks(ctx, ranks); err != nil {
		logger.Warn().Err(err).Msg("failed to persist PageRank scores")
	}
}

// createTaskDependencyEdges creates graph edges for task relationships.
// Edge types:
// - parent_of: parent task → child task
// - depends_on: dependent task → dependency task
//
// If gs is nil, a new graph store is opened and closed internally.
// For batch operations, pass a pre-opened store to avoid repeated open/close overhead.
func createTaskDependencyEdges(ctx context.Context, cfg config.Config, workspaceID string, task tasks.Task, gs graph.Store) {
	var graphStore graph.Store
	var closeStore bool

	if gs != nil {
		graphStore = gs
	} else {
		// Open graph store (fail silently - graph is optional)
		var err error
		graphStore, err = graph.Open(ctx, cfg.Storage.Root)
		if err != nil {
			return
		}
		closeStore = true
	}
	if closeStore {
		defer func() { errs.Ignore(graphStore.Close(), "close graph store") }()
	}

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
