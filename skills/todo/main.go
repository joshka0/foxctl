// Package main implements the todo/manage skill.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
)

const (
	statusPending = "pending"
	statusDone    = "completed"
)

type input struct {
	Operation    string           `json:"operation"`
	WorkspaceID  string           `json:"workspace_id"`
	Add          *addRequest      `json:"add"`
	Complete     *completeRequest `json:"complete"`
	SetActive    *setActiveReq    `json:"set_active"`
	EnsureActive *ensureActiveReq `json:"ensure_active"`
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

type setActiveReq struct {
	TaskID string `json:"task_id"`
}

type ensureActiveReq struct {
	DefaultTitle string `json:"default_title"`
	ScopePath    string `json:"scope_path"`
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
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("todo/manage", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("todo/manage", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("todo/manage", "EARG", fmt.Errorf("decode input: %w", err))
	}
	if err := run(ctx, rc, cfg, in); err != nil {
		fail("todo/manage", "ERUNTIME", err)
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
		// Default to current working directory as workspace ID
		workspaceID, _ = os.Getwd()
	}

	var data map[string]any

	switch op {
	case "add":
		task, allTasks, err := handleAdd(ctx, store, workspaceID, in.Add)
		if err != nil {
			return err
		}
		data = map[string]any{
			"task":          task,
			"total_tasks":   len(allTasks),
			"pending_tasks": countPending(allTasks),
			"summary":       fmt.Sprintf("added task %s", task.ID),
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

	default:
		return fmt.Errorf("unknown operation %q (expected add|complete|list|get_active|set_active|clear_active|ensure_active)", op)
	}

	return rc.Emit("todo/manage", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func handleAdd(ctx context.Context, store tasks.Store, workspaceID string, req *addRequest) (*taskOutput, []tasks.Task, error) {
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
	}

	added, err := store.Add(ctx, newTask)
	if err != nil {
		return nil, nil, err
	}

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
		return nil, nil, fmt.Errorf("task %s not found", req.ID)
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

func toOutput(t tasks.Task) *taskOutput {
	out := &taskOutput{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		ScopePath:   t.ScopePath,
		ParentID:    t.ParentID,
		Children:    t.Children,
		DependsOn:   t.DependsOn,
		Status:      t.Status,
		CreatedAt:   t.CreatedAt.Format(time.RFC3339),
		Notes:       t.Notes,
		Gotchas:     t.Gotchas,
	}
	if t.CompletedAt != nil {
		out.CompletedAt = t.CompletedAt.Format(time.RFC3339)
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

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit todo failure")
	os.Exit(1)
}
