// Package main implements the todo/manage skill.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/oklog/ulid/v2"
)

const (
	statusPending = "pending"
	statusDone    = "done"
	storeVersion  = "1"
)

type input struct {
	Operation string           `json:"operation"`
	StorePath string           `json:"store_path"`
	Add       *addRequest      `json:"add"`
	Complete  *completeRequest `json:"complete"`
}

type addRequest struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	ParentID    string   `json:"parent_id"`
	DependsOn   []string `json:"depends_on"`
}

type completeRequest struct {
	ID      string `json:"id"`
	Notes   string `json:"notes"`
	Gotchas string `json:"gotchas"`
}

type store struct {
	Version string  `json:"version"`
	Tasks   []*task `json:"tasks"`
}

type task struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
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
	_ = ctx
	storePath := in.StorePath
	if storePath == "" {
		storePath = filepath.Join(cfg.Home, "todo", "tasks.json")
	}
	op := strings.ToLower(strings.TrimSpace(in.Operation))
	if op == "" {
		op = "list"
	}

	s, err := loadStore(storePath)
	if err != nil {
		return err
	}

	var data map[string]any

	switch op {
	case "add":
		task, err := handleAdd(s, in.Add)
		if err != nil {
			return err
		}
		if err := saveStore(storePath, s); err != nil {
			return err
		}
		data = map[string]any{
			"task":          task,
			"total_tasks":   len(s.Tasks),
			"pending_tasks": countStatus(s, statusPending),
			"summary":       fmt.Sprintf("added task %s", task.ID),
		}
	case "complete":
		task, err := handleComplete(in.Complete, s)
		if err != nil {
			return err
		}
		if err := saveStore(storePath, s); err != nil {
			return err
		}
		data = map[string]any{
			"task":          task,
			"pending_tasks": countStatus(s, statusPending),
			"summary":       fmt.Sprintf("completed task %s", task.ID),
		}
	case "list":
		data = map[string]any{
			"tasks":         s.Tasks,
			"total_tasks":   len(s.Tasks),
			"pending_tasks": countStatus(s, statusPending),
		}
	default:
		return fmt.Errorf("unknown operation %q (expected add|complete|list)", op)
	}

	return rc.Emit("todo/manage", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func handleAdd(s *store, req *addRequest) (*task, error) {
	if req == nil {
		return nil, fmt.Errorf("add payload is required")
	}
	if err := validateText("title", req.Title); err != nil {
		return nil, err
	}
	if err := validateText("description", req.Description); err != nil {
		return nil, err
	}

	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	index := buildIndex(s)
	if req.ParentID != "" {
		parent, ok := index[req.ParentID]
		if !ok {
			return nil, fmt.Errorf("parent task %s not found", req.ParentID)
		}
		if parent.Status == statusDone {
			return nil, fmt.Errorf("cannot add child to completed task %s", req.ParentID)
		}
	}
	if err := validateDependencies(req.DependsOn, index); err != nil {
		return nil, err
	}

	id := ulid.Make().String()
	task := &task{
		ID:          id,
		Title:       req.Title,
		Description: req.Description,
		ParentID:    req.ParentID,
		DependsOn:   dedupe(req.DependsOn),
		Status:      statusPending,
		CreatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if req.ParentID != "" {
		parent := index[req.ParentID]
		parent.Children = append(parent.Children, task.ID)
	}
	s.Tasks = append(s.Tasks, task)
	return task, nil
}

func handleComplete(req *completeRequest, s *store) (*task, error) {
	if req == nil {
		return nil, fmt.Errorf("complete payload is required")
	}
	if req.ID == "" {
		return nil, fmt.Errorf("complete.id is required")
	}
	if err := validateText("notes", req.Notes); err != nil {
		return nil, err
	}
	if err := validateText("gotchas", req.Gotchas); err != nil {
		return nil, err
	}

	index := buildIndex(s)
	task, ok := index[req.ID]
	if !ok {
		return nil, fmt.Errorf("task %s not found", req.ID)
	}
	if task.Status == statusDone {
		return nil, fmt.Errorf("task %s already completed", req.ID)
	}
	for _, dep := range task.DependsOn {
		if depTask, ok := index[dep]; ok {
			if depTask.Status != statusDone {
				return nil, fmt.Errorf("task %s depends on incomplete task %s", req.ID, dep)
			}
		} else {
			return nil, fmt.Errorf("dependency %s for task %s not found", dep, req.ID)
		}
	}

	task.Status = statusDone
	task.CompletedAt = time.Now().UTC().Format(time.RFC3339)
	task.Notes = strings.TrimSpace(req.Notes)
	task.Gotchas = strings.TrimSpace(req.Gotchas)
	return task, nil
}

func validateText(field, value string) error {
	if strings.ContainsRune(value, '`') {
		return fmt.Errorf("%s cannot contain backticks (`)", field)
	}
	return nil
}

func validateDependencies(depends []string, index map[string]*task) error {
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

func loadStore(path string) (*store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &store{
				Version: storeVersion,
				Tasks:   []*task{},
			}, nil
		}
		return nil, fmt.Errorf("read store: %w", err)
	}
	var s store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse store: %w", err)
	}
	if s.Tasks == nil {
		s.Tasks = []*task{}
	}
	return &s, nil
}

func saveStore(path string, s *store) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir store dir: %w", err)
	}
	buf, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return fmt.Errorf("write store: %w", err)
	}
	return nil
}

func buildIndex(s *store) map[string]*task {
	idx := make(map[string]*task, len(s.Tasks))
	for _, t := range s.Tasks {
		idx[t.ID] = t
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

func countStatus(s *store, status string) int {
	count := 0
	for _, t := range s.Tasks {
		if t.Status == status {
			count++
		}
	}
	return count
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit todo failure")
	os.Exit(1)
}
