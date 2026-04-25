package llm

import (
	"context"
	"fmt"
	"strings"

	sysconfig "github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/engine"
	corerun "github.com/joshka0/foxctl/internal/v2/core/run"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/runner"
)

// ChatModel adapts the classic chat engine into the v2 runner.Model contract.
//
// Tool calls are passed through to the v2 runner so runtime policy, execution,
// and event recording stay owned by the v2 pipeline.
type ChatModel struct {
	newEngine    func(maxIterations int) (engine.AgentEngine, error)
	workspace    string
	systemPrompt string
}

type ChatModelConfig struct {
	LLM          sysconfig.LLMSettings
	Workspace    string
	SystemPrompt string
}

// NewChatModel builds a v2 model adapter from foxctl LLM settings.
func NewChatModel(cfg ChatModelConfig) (*ChatModel, error) {
	return &ChatModel{
		newEngine: func(maxIterations int) (engine.AgentEngine, error) {
			return engine.NewLLMChatEngine(llmChatConfig(cfg.LLM, maxIterations))
		},
		workspace:    strings.TrimSpace(cfg.Workspace),
		systemPrompt: strings.TrimSpace(cfg.SystemPrompt),
	}, nil
}

// NewChatModelForTest allows tests to inject a deterministic classic engine.
func NewChatModelForTest(run func(context.Context, engine.EngineInput) (engine.EngineOutput, error)) *ChatModel {
	return &ChatModel{
		newEngine: func(int) (engine.AgentEngine, error) {
			return engineRunnerFunc(run), nil
		},
	}
}

func (m *ChatModel) Complete(ctx context.Context, in runner.ModelInput) (runner.ModelResponse, error) {
	if m == nil || m.newEngine == nil {
		return runner.ModelResponse{}, fmt.Errorf("llm model: nil engine factory")
	}
	maxIterations := in.MaxIterations
	if maxIterations <= 0 {
		maxIterations = runner.DefaultMaxIterations
	}
	eng, err := m.newEngine(maxIterations)
	if err != nil {
		return runner.ModelResponse{}, fmt.Errorf("create llm engine: %w", err)
	}

	out, err := eng.Run(ctx, engine.EngineInput{
		Messages:     engineMessages(in),
		Tools:        engineTools(in.Tools),
		SystemPrompt: m.systemPrompt,
		Workspace:    m.workspace,
	})
	if err != nil {
		return runner.ModelResponse{}, err
	}

	switch out.StopReason {
	case engine.StopReasonEndTurn, engine.StopReasonMaxTokens, engine.StopReasonMaxIterations, engine.StopReasonContextBudget:
		if len(out.ToolCalls) > 0 {
			return runner.ModelResponse{
				Message:   strings.TrimSpace(out.AssistantText),
				ToolCalls: runToolCalls(out.ToolCalls),
				Done:      false,
			}, nil
		}
		return runner.ModelResponse{
			Message: strings.TrimSpace(out.AssistantText),
			Done:    true,
		}, nil
	case engine.StopReasonCancelled:
		if err := ctx.Err(); err != nil {
			return runner.ModelResponse{}, err
		}
		return runner.ModelResponse{}, fmt.Errorf("llm turn cancelled")
	case engine.StopReasonError:
		msg := strings.TrimSpace(out.Error)
		if msg == "" {
			msg = "llm turn failed"
		}
		return runner.ModelResponse{}, fmt.Errorf("%s", msg)
	default:
		return runner.ModelResponse{
			Message: strings.TrimSpace(out.AssistantText),
			Done:    true,
		}, nil
	}
}

func llmChatConfig(settings sysconfig.LLMSettings, maxIterations int) engine.LLMChatConfig {
	provider := strings.TrimSpace(settings.Provider)
	cfg := engine.LLMChatConfig{
		Provider:             provider,
		BaseURL:              strings.TrimSpace(settings.BaseURL),
		AuthMode:             strings.TrimSpace(settings.AuthMode),
		AuthHeader:           strings.TrimSpace(settings.AuthHeader),
		AuthPrefix:           settings.AuthPrefix,
		Model:                strings.TrimSpace(settings.Model),
		MaxIterations:        maxIterations,
		Temperature:          0,
		PassthroughToolCalls: true,
	}
	if provider != "" {
		cfg.APIKey = strings.TrimSpace(settings.ResolveAPIKey(provider))
		cfg.Model = strings.TrimSpace(settings.ResolveModel(provider))
		cfg.BaseURL = strings.TrimSpace(settings.ResolveBaseURL(provider))
		cfg.AuthMode = strings.TrimSpace(settings.ResolveAuthMode(provider))
		cfg.AuthHeader = strings.TrimSpace(settings.ResolveAuthHeader(provider))
		cfg.AuthPrefix = settings.ResolveAuthPrefix(provider)
	} else {
		cfg.APIKey = strings.TrimSpace(settings.APIKey)
	}
	return cfg
}

func engineMessages(in runner.ModelInput) []engine.Message {
	if len(in.Messages) == 0 {
		return []engine.Message{engine.NewUserMessage(in.Prompt)}
	}
	out := make([]engine.Message, 0, len(in.Messages))
	for _, msg := range in.Messages {
		out = append(out, engine.Message{
			Role:       strings.TrimSpace(msg.Role),
			Content:    msg.Content,
			ToolCalls:  engineToolCalls(msg.ToolCalls),
			ToolCallID: msg.ToolCallID,
			Name:       msg.Name,
		})
	}
	return out
}

func engineTools(in []coretool.ToolDef) []engine.ToolDef {
	if len(in) == 0 {
		return nil
	}
	out := make([]engine.ToolDef, 0, len(in))
	for _, def := range in {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		out = append(out, engine.ToolDef{
			Name:        name,
			Description: strings.TrimSpace(def.Description),
			Parameters:  append([]byte(nil), def.Parameters...),
		})
	}
	return out
}

func engineToolCalls(in []corerun.ToolCall) []engine.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]engine.ToolCall, 0, len(in))
	for _, call := range in {
		out = append(out, engine.ToolCall{
			ID:        strings.TrimSpace(call.ID),
			Name:      strings.TrimSpace(call.Name),
			Arguments: append([]byte(nil), call.Args...),
		})
	}
	return out
}

func runToolCalls(in []engine.ToolCall) []corerun.ToolCall {
	if len(in) == 0 {
		return nil
	}
	out := make([]corerun.ToolCall, 0, len(in))
	for _, call := range in {
		out = append(out, corerun.ToolCall{
			ID:   strings.TrimSpace(call.ID),
			Name: strings.TrimSpace(call.Name),
			Args: append([]byte(nil), call.Arguments...),
		})
	}
	return out
}

type engineRunnerFunc func(context.Context, engine.EngineInput) (engine.EngineOutput, error)

func (f engineRunnerFunc) Run(ctx context.Context, in engine.EngineInput) (engine.EngineOutput, error) {
	return f(ctx, in)
}
