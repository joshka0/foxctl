package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

// Engine is the main workflow execution engine.
type Engine struct {
	loader     *Loader
	template   *TemplateEngine
	maxWorkers int
	foxctlBin  string
}

// EngineOption configures an Engine.
type EngineOption func(*Engine)

// WithFoxctlBin sets the path to the foxctl binary.
func WithFoxctlBin(path string) EngineOption {
	return func(e *Engine) {
		e.foxctlBin = path
	}
}

// WithEngineMaxWorkers sets the maximum parallel workers.
func WithEngineMaxWorkers(n int) EngineOption {
	return func(e *Engine) {
		if n > 0 {
			e.maxWorkers = n
		}
	}
}

// WithLoaderPaths sets custom workflow search paths.
func WithLoaderPaths(paths ...string) EngineOption {
	return func(e *Engine) {
		e.loader = NewLoader(WithWorkflowPaths(paths...))
	}
}

// NewEngine creates a new workflow engine.
func NewEngine(opts ...EngineOption) *Engine {
	e := &Engine{
		loader:     NewLoader(),
		template:   NewTemplateEngine(),
		maxWorkers: 10,
		foxctlBin:  "foxctl",
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Run executes a workflow by name with the given inputs.
func (e *Engine) Run(ctx context.Context, nameOrPath string, inputs map[string]any) (*WorkflowResult, error) {
	// Load workflow
	handle, err := e.loader.Load(nameOrPath)
	if err != nil {
		return nil, fmt.Errorf("load workflow: %w", err)
	}

	return e.Execute(ctx, handle.Workflow, inputs)
}

// Execute runs a workflow with the given inputs.
func (e *Engine) Execute(ctx context.Context, wf *Workflow, inputs map[string]any) (*WorkflowResult, error) {
	// Validate and apply default inputs
	resolvedInputs, err := e.resolveInputs(wf, inputs)
	if err != nil {
		return nil, fmt.Errorf("resolve inputs: %w", err)
	}

	// Build DAG
	dag, err := NewDAG(wf.Steps)
	if err != nil {
		return nil, fmt.Errorf("build DAG: %w", err)
	}

	// Create execution context
	execCtx := NewExecutionContext(resolvedInputs)

	// Create skill executor
	executor := &skillExecutor{
		foxctlBin: e.foxctlBin,
	}

	// Create scheduler and run
	scheduler := NewScheduler(dag, executor, WithMaxWorkers(e.maxWorkers))
	result, err := scheduler.Run(ctx, execCtx)
	if err != nil {
		return result, err
	}

	// Set workflow name
	result.WorkflowName = wf.Metadata.Name

	// Extract outputs (non-critical for result)
	if len(wf.Outputs) > 0 {
		outputs, err := e.extractOutputs(wf.Outputs, execCtx)
		errs.Ignore(err, "extract workflow outputs")
		result.Outputs = outputs
	}

	return result, nil
}

// Validate validates a workflow without executing it.
func (e *Engine) Validate(nameOrPath string) error {
	handle, err := e.loader.Load(nameOrPath)
	if err != nil {
		return err
	}

	// Try to build DAG to catch dependency issues
	_, err = NewDAG(handle.Workflow.Steps)
	return err
}

// List returns all discoverable workflows.
func (e *Engine) List() ([]Handle, error) {
	return e.loader.List()
}

// resolveInputs validates and applies defaults to workflow inputs.
func (e *Engine) resolveInputs(wf *Workflow, provided map[string]any) (map[string]any, error) {
	result := make(map[string]any)

	// Copy provided inputs
	for k, v := range provided {
		result[k] = v
	}

	// Apply defaults and validate required
	for _, input := range wf.Inputs {
		if _, exists := result[input.Name]; !exists {
			if input.Required {
				return nil, fmt.Errorf("missing required input: %s", input.Name)
			}
			if input.Default != nil {
				result[input.Name] = input.Default
			}
		}
	}

	return result, nil
}

// extractOutputs extracts output values from execution context.
func (e *Engine) extractOutputs(outputs []Output, ctx *ExecutionContext) (map[string]any, error) {
	result := make(map[string]any)

	for _, out := range outputs {
		value, err := e.template.RenderString(out.Value, ctx)
		if err != nil {
			if out.Optional {
				continue
			}
			return nil, fmt.Errorf("output %s: %w", out.Name, err)
		}
		result[out.Name] = value
	}

	return result, nil
}

// skillExecutor executes foxctl skills.
type skillExecutor struct {
	foxctlBin string
}

// Execute runs a skill and returns the result.
func (e *skillExecutor) Execute(ctx context.Context, step *Step, input map[string]any) (*StepResult, error) {
	startTime := time.Now()

	// Marshal input to JSON
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal input: %w", err)
	}

	// Build command
	args := []string{"run", step.Skill, "--input", string(inputJSON)}

	// Add timeout if specified
	var cancel context.CancelFunc
	if step.Timeout != "" {
		if d, err := time.ParseDuration(step.Timeout); err == nil {
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
	}

	cmd := exec.CommandContext(ctx, e.foxctlBin, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run command
	err = cmd.Run()

	result := &StepResult{
		StepID:     step.ID,
		StartTime:  startTime,
		EndTime:    time.Now(),
		DurationMs: time.Since(startTime).Milliseconds(),
	}

	if err != nil {
		result.Status = StepFailed
		result.Error = fmt.Sprintf("skill execution failed: %v\nstderr: %s", err, stderr.String())
		return result, fmt.Errorf("skill %s failed: %w", step.Skill, err)
	}

	// Parse output envelope
	output := stdout.Bytes()
	data, err := parseEnvelopeData(output)
	if err != nil {
		// If we can't parse as envelope, use raw output
		result.Status = StepCompleted
		result.Data = map[string]any{
			"raw": string(output),
		}
		return result, nil
	}

	result.Status = StepCompleted
	result.Data = data

	return result, nil
}

// parseEnvelopeData extracts data from an foxctl envelope.
func parseEnvelopeData(output []byte) (any, error) {
	var env struct {
		Status string `json:"status"`
		Data   any    `json:"data"`
		Error  struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(output, &env); err != nil {
		return nil, err
	}

	if env.Status == "error" {
		return nil, fmt.Errorf("%s: %s", env.Error.Code, env.Error.Message)
	}

	return env.Data, nil
}

// Builder provides a fluent API for building workflows programmatically.
type Builder struct {
	workflow *Workflow
}

// NewBuilder creates a new workflow builder.
func NewBuilder(name string) *Builder {
	return &Builder{
		workflow: &Workflow{
			APIVersion: APIVersion,
			Kind:       Kind,
			Metadata: Metadata{
				Name: name,
			},
		},
	}
}

// Description sets the workflow description.
func (b *Builder) Description(desc string) *Builder {
	b.workflow.Metadata.Description = desc
	return b
}

// Input adds an input parameter.
func (b *Builder) Input(name, typ string, opts ...InputOption) *Builder {
	input := Input{Name: name, Type: typ}
	for _, opt := range opts {
		opt(&input)
	}
	b.workflow.Inputs = append(b.workflow.Inputs, input)
	return b
}

// InputOption configures an input.
type InputOption func(*Input)

// Required marks an input as required.
func Required() InputOption {
	return func(i *Input) { i.Required = true }
}

// Default sets a default value.
func Default(v any) InputOption {
	return func(i *Input) { i.Default = v }
}

// Step adds a step to the workflow.
func (b *Builder) Step(id, skill string, input map[string]any) *StepBuilder {
	step := Step{
		ID:    id,
		Skill: skill,
		Input: input,
	}
	b.workflow.Steps = append(b.workflow.Steps, step)
	return &StepBuilder{
		builder: b,
		step:    &b.workflow.Steps[len(b.workflow.Steps)-1],
	}
}

// Output adds an output definition.
func (b *Builder) Output(name, value string) *Builder {
	b.workflow.Outputs = append(b.workflow.Outputs, Output{Name: name, Value: value})
	return b
}

// Build returns the completed workflow.
func (b *Builder) Build() (*Workflow, error) {
	if err := Validate(b.workflow); err != nil {
		return nil, err
	}
	return b.workflow, nil
}

// StepBuilder provides a fluent API for configuring steps.
type StepBuilder struct {
	builder *Builder
	step    *Step
}

// DependsOn sets step dependencies.
func (sb *StepBuilder) DependsOn(deps ...string) *StepBuilder {
	sb.step.DependsOn = deps
	return sb
}

// If sets a condition for the step.
func (sb *StepBuilder) If(condition string) *StepBuilder {
	sb.step.If = condition
	return sb
}

// Loop configures the step to iterate over an array.
func (sb *StepBuilder) Loop(over, as string) *StepBuilder {
	sb.step.Loop = &LoopConfig{Over: over, As: as}
	return sb
}

// Parallel sets the parallel execution limit for loops.
func (sb *StepBuilder) Parallel(n int) *StepBuilder {
	sb.step.Parallel = &n
	return sb
}

// OnError sets the error handling strategy.
func (sb *StepBuilder) OnError(strategy string) *StepBuilder {
	sb.step.OnError = strategy
	return sb
}

// Timeout sets the step timeout.
func (sb *StepBuilder) Timeout(d string) *StepBuilder {
	sb.step.Timeout = d
	return sb
}

// Retry sets retry configuration.
func (sb *StepBuilder) Retry(maxRetries int, delay string) *StepBuilder {
	sb.step.MaxRetries = maxRetries
	sb.step.RetryDelay = delay
	return sb
}

// Step adds another step (returns to builder).
func (sb *StepBuilder) Step(id, skill string, input map[string]any) *StepBuilder {
	return sb.builder.Step(id, skill, input)
}

// Output adds an output (returns to builder).
func (sb *StepBuilder) Output(name, value string) *Builder {
	return sb.builder.Output(name, value)
}

// Build completes the workflow.
func (sb *StepBuilder) Build() (*Workflow, error) {
	return sb.builder.Build()
}
