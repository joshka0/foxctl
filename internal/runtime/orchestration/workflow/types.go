// Package workflow provides a DAG-based workflow engine for chaining foxctl skills.
package workflow

import (
	"time"
)

// APIVersion is the current workflow API version.
const APIVersion = "foxctl/v1"

// Kind identifies workflow manifests.
const Kind = "Workflow"

// Workflow defines a sequence of skill executions with data flow between them.
type Workflow struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Inputs     []Input  `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Steps      []Step   `yaml:"steps" json:"steps"`
	Outputs    []Output `yaml:"outputs,omitempty" json:"outputs,omitempty"`
}

// Metadata contains workflow identification and documentation.
type Metadata struct {
	Name        string            `yaml:"name" json:"name"`
	Version     string            `yaml:"version,omitempty" json:"version,omitempty"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Tags        []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// Input defines a workflow-level input parameter.
type Input struct {
	Name        string `yaml:"name" json:"name"`
	Type        string `yaml:"type,omitempty" json:"type,omitempty"` // string, int, bool, array, object
	Required    bool   `yaml:"required,omitempty" json:"required,omitempty"`
	Default     any    `yaml:"default,omitempty" json:"default,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Output defines a workflow output that can be extracted from step results.
type Output struct {
	Name     string `yaml:"name" json:"name"`
	Value    string `yaml:"value" json:"value"` // Template expression
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

// Step defines a single skill execution within a workflow.
type Step struct {
	ID        string         `yaml:"id" json:"id"`
	Skill     string         `yaml:"skill" json:"skill"`
	Input     map[string]any `yaml:"input,omitempty" json:"input,omitempty"`
	DependsOn []string       `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`

	// Control flow
	If       string      `yaml:"if,omitempty" json:"if,omitempty"`             // Conditional execution
	Loop     *LoopConfig `yaml:"loop,omitempty" json:"loop,omitempty"`         // Iterate over array
	Parallel *int        `yaml:"parallel,omitempty" json:"parallel,omitempty"` // Max parallel for loop

	// Error handling
	OnError    string `yaml:"on_error,omitempty" json:"on_error,omitempty"` // fail, continue, retry
	Timeout    string `yaml:"timeout,omitempty" json:"timeout,omitempty"`   // Duration string
	MaxRetries int    `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`
	RetryDelay string `yaml:"retry_delay,omitempty" json:"retry_delay,omitempty"`

	// Output transformation
	OutputAs string `yaml:"output_as,omitempty" json:"output_as,omitempty"` // Transform output (e.g., "array", "first", "last")
}

// LoopConfig defines iteration over an array value.
type LoopConfig struct {
	Over string `yaml:"over" json:"over"` // Template expression yielding array
	As   string `yaml:"as" json:"as"`     // Variable name for current item
}

// ErrorStrategy defines how to handle step failures.
type ErrorStrategy string

const (
	// ErrorFail stops the workflow on error (default).
	ErrorFail ErrorStrategy = "fail"
	// ErrorContinue marks the step as failed but continues.
	ErrorContinue ErrorStrategy = "continue"
	// ErrorRetry retries the step according to retry settings.
	ErrorRetry ErrorStrategy = "retry"
)

// ParseErrorStrategy converts a string to ErrorStrategy.
func ParseErrorStrategy(s string) ErrorStrategy {
	switch s {
	case "continue":
		return ErrorContinue
	case "retry":
		return ErrorRetry
	default:
		return ErrorFail
	}
}

// StepStatus tracks the execution state of a step.
type StepStatus string

const (
	StepPending   StepStatus = "pending"
	StepRunning   StepStatus = "running"
	StepCompleted StepStatus = "completed"
	StepFailed    StepStatus = "failed"
	StepSkipped   StepStatus = "skipped"
)

// StepResult captures the output of a step execution.
type StepResult struct {
	StepID     string     `json:"step_id"`
	Status     StepStatus `json:"status"`
	Data       any        `json:"data,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartTime  time.Time  `json:"start_time"`
	EndTime    time.Time  `json:"end_time"`
	DurationMs int64      `json:"duration_ms"`
	RetryCount int        `json:"retry_count,omitempty"`

	// For loop steps, contains results for each iteration
	Iterations []StepResult `json:"iterations,omitempty"`
}

// WorkflowResult captures the complete workflow execution result.
type WorkflowResult struct {
	WorkflowName string                `json:"workflow_name"`
	Status       StepStatus            `json:"status"`
	Steps        map[string]StepResult `json:"steps"`
	Outputs      map[string]any        `json:"outputs,omitempty"`
	StartTime    time.Time             `json:"start_time"`
	EndTime      time.Time             `json:"end_time"`
	DurationMs   int64                 `json:"duration_ms"`
	Error        string                `json:"error,omitempty"`
	Metadata     map[string]any        `json:"metadata,omitempty"`
}

// ExecutionContext holds the runtime state during workflow execution.
type ExecutionContext struct {
	Inputs  map[string]any         // Workflow input values
	Steps   map[string]*StepResult // Results keyed by step ID
	Current string                 // Current step being executed
	Vars    map[string]any         // Additional variables (e.g., loop vars)
}

// NewExecutionContext creates a new context with the provided inputs.
func NewExecutionContext(inputs map[string]any) *ExecutionContext {
	return &ExecutionContext{
		Inputs: inputs,
		Steps:  make(map[string]*StepResult),
		Vars:   make(map[string]any),
	}
}

// Clone creates a copy of the context for use in parallel branches.
func (c *ExecutionContext) Clone() *ExecutionContext {
	clone := &ExecutionContext{
		Inputs:  make(map[string]any),
		Steps:   make(map[string]*StepResult),
		Current: c.Current,
		Vars:    make(map[string]any),
	}
	for k, v := range c.Inputs {
		clone.Inputs[k] = v
	}
	for k, v := range c.Steps {
		clone.Steps[k] = v
	}
	for k, v := range c.Vars {
		clone.Vars[k] = v
	}
	return clone
}

// Set stores a step result.
func (c *ExecutionContext) Set(stepID string, result *StepResult) {
	c.Steps[stepID] = result
}

// Get retrieves a step result.
func (c *ExecutionContext) Get(stepID string) *StepResult {
	return c.Steps[stepID]
}

// SetVar sets a variable in the context.
func (c *ExecutionContext) SetVar(name string, value any) {
	c.Vars[name] = value
}

// GetVar retrieves a variable from the context.
func (c *ExecutionContext) GetVar(name string) any {
	return c.Vars[name]
}
