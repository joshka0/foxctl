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
	Prompt        string
	Iteration     int
	MaxIterations int
	Tools         []coretool.ToolDef
	Messages      []ModelMessage
}

// ModelResponse captures model output for one iteration.
type ModelResponse struct {
	Message   string
	ToolCalls []run.ToolCall
	Done      bool
}

// ModelMessage is the provider-neutral message history passed to a model.
type ModelMessage struct {
	Role       string
	Content    string
	ToolCalls  []run.ToolCall
	ToolCallID string
	Name       string
}

// ToolResult captures tool execution output.
type ToolResult struct {
	Status string
	Output string
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
	EventStore   events.Appender
	EventBus     EventPublisher
	Model        Model
	Tools        []coretool.ToolDef
	ToolExecutor ToolExecutor
	TurnRecorder run.TurnRecorder
	Hooks        HookRunner
	Now          func() time.Time
	NewID        func() string
	ObserveStage StageObserver
	OnEventError func(error)
}

// EventPublisher fan-outs runtime events to background subscribers.
type EventPublisher interface {
	Publish(ctx context.Context, evt events.Event) error
}
