package rlm

import (
	"context"
	"errors"
	"strings"
)

// ErrNotImplemented indicates the runtime has been scaffolded but not yet connected
// to a real recursive model execution backend.
var ErrNotImplemented = errors.New("rlm: runner not implemented")

// RunFunc adapts a function to the Runner interface.
type RunFunc func(ctx context.Context, task Task, env Environment) (Result, error)

// Run calls f(ctx, task, env).
func (f RunFunc) Run(ctx context.Context, task Task, env Environment) (Result, error) {
	return f(ctx, task, env)
}

// ReadOnlyRunner is the first experimental runtime shell.
// It enforces basic bounded-task validation but does not yet execute a real recursive model.
type ReadOnlyRunner struct {
	Execute RunFunc
}

// Run validates the task/environment and delegates to Execute when configured.
func (r ReadOnlyRunner) Run(ctx context.Context, task Task, env Environment) (Result, error) {
	if err := ValidateTask(task); err != nil {
		return Result{}, err
	}
	if err := ValidateEnvironment(env); err != nil {
		return Result{}, err
	}
	if r.Execute == nil {
		return Result{}, ErrNotImplemented
	}
	return r.Execute(ctx, task, env)
}

// ValidateTask enforces the minimal read-only runtime contract.
func ValidateTask(task Task) error {
	if strings.TrimSpace(task.Prompt) == "" {
		return errors.New("rlm: prompt is required")
	}
	if task.MaxDepth < 0 {
		return errors.New("rlm: max_depth must be >= 0")
	}
	if task.MaxIterations < 0 {
		return errors.New("rlm: max_iterations must be >= 0")
	}
	if task.MaxSubcalls < 0 {
		return errors.New("rlm: max_subcalls must be >= 0")
	}
	return nil
}

// ValidateEnvironment enforces the first-version tool safety contract.
func ValidateEnvironment(env Environment) error {
	for _, tool := range env.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return errors.New("rlm: tool name is required")
		}
		if !tool.ReadOnly {
			return errors.New("rlm: first-version runtime only allows read-only tools")
		}
	}
	return nil
}
