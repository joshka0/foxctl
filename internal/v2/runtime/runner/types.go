package runner

import (
	"context"
	"encoding/json"
	"time"

	"github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
)

const (
	// DefaultMaxIterations bounds model loops when input does not provide an explicit limit.
	DefaultMaxIterations = 10
)

const (
	StageInitContext         = "InitContext"
	StageResolveDependencies = "ResolveDependencies"
	StageApplyPreHooks       = "ApplyPreHooks"
	StageBuildToolset        = "BuildToolset"
	StageModelCall           = "ModelCall"
	StageApplyPostHooks      = "ApplyPostHooks"
	StagePersistTurn         = "PersistTurn"
	StageEmitEvents          = "EmitEvents"
)

// ModelInput captures the model request for one iteration.
type ModelInput struct {
	Prompt        string             `json:"prompt,omitempty"`
	Iteration     int                `json:"iteration"`
	MaxIterations int                `json:"max_iterations,omitempty"`
	Tools         []coretool.ToolDef `json:"tools,omitempty"`
	Messages      []ModelMessage     `json:"messages,omitempty"`
}

// ModelResponse captures model output for one iteration.
type ModelResponse struct {
	Message   string         `json:"message,omitempty"`
	ToolCalls []run.ToolCall `json:"tool_calls,omitempty"`
	Done      bool           `json:"done"`
}

// ModelMessage is the provider-neutral message history passed to a model.
type ModelMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content,omitempty"`
	ToolCalls  []run.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
}

// ToolResult captures tool execution output.
type ToolResult struct {
	Status string `json:"status,omitempty"`
	Output string `json:"output,omitempty"`
}

// Model abstracts LLM completion for deterministic runner tests.
type Model interface {
	Complete(ctx context.Context, in ModelInput) (ModelResponse, error)
}

// ToolExecutor is the single v2 tool execution path.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args json.RawMessage) (ToolResult, error)
}

// HookRunner applies optional pre/post stage hooks.
type HookRunner interface {
	RunPreHooks(ctx context.Context, in run.TurnInput) error
	RunPostHooks(ctx context.Context, in run.TurnInput, out run.TurnOutput) error
}

// StageObserver receives deterministic stage execution callbacks.
type StageObserver func(stageName string)

// Config wires runner dependencies.
type Config struct {
	EventStore     events.Appender
	EventBus       EventPublisher
	Model          Model
	Tools          []coretool.ToolDef
	RLMREPLFactory RLMREPLRunnerFactory
	ToolExecutor   ToolExecutor
	EffectJournal  run.EffectJournal
	TurnRecorder   run.TurnRecorder
	Hooks          HookRunner
	Now            func() time.Time
	NewID          func() string
	ObserveStage   StageObserver
	OnEventError   func(error)

	// StrictDurableIdentity requires caller-supplied run_id, turn_id, and
	// request_id before any runner side effects execute.
	StrictDurableIdentity bool
}

// EventPublisher fan-outs runtime events to background subscribers.
type EventPublisher interface {
	Publish(ctx context.Context, evt events.Event) error
}
