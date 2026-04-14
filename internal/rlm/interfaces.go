package rlm

import "context"

// Runner executes a bounded RLM task over an external environment.
type Runner interface {
	Run(ctx context.Context, task Task, env Environment) (Result, error)
}

// Sandbox executes model-authored code against external state in a controlled environment.
type Sandbox interface {
	Init(ctx context.Context, state map[string]any) error
	Execute(ctx context.Context, code string) (ExecResult, error)
	Snapshot(ctx context.Context) (map[string]any, error)
	Close(ctx context.Context) error
}

// Bootstrapper prepares a runtime environment from existing foxctl state.
type Bootstrapper interface {
	Build(ctx context.Context, task Task) (Environment, error)
}
