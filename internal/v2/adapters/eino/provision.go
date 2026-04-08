package eino

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"

	"github.com/jkatigb/agentctl/internal/engine"
)

// ProvisionFromLLMConfig constructs a real Eino-backed engine adapter from a
// provider-resolved LLMChatConfig. It:
//   1. Wraps the resolved connection parameters in an oaiModelBridge that implements
//      model.BaseChatModel against the same OpenAI-compatible endpoint.
//   2. Creates an adk.ChatModelAgent with that model (no tools — spike scope).
//   3. Returns the EinoEngineAdapter that satisfies engine.AgentEngine.
//
// The resolved config must have APIKey, BaseURL, and Model already filled in
// (i.e. passed after engine.NewLLMChatEngine has run auto-detection).
// Bedrock configs are rejected: the bridge uses Bearer-token auth only.
func ProvisionFromLLMConfig(cfg engine.LLMChatConfig) (*EinoEngineAdapter, error) {
	if cfg.Provider == "bedrock" {
		return nil, fmt.Errorf("eino gate-on: Bedrock provider is not supported by the spike bridge (use a standard OpenAI-compatible provider)")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("eino gate-on: resolved BaseURL is empty — cannot provision bridge")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("eino gate-on: resolved Model is empty — cannot provision bridge")
	}

	bridge := newOAIModelBridge(cfg.APIKey, cfg.BaseURL, cfg.Model, cfg.Timeout)

	agent, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name:        "agentctl-eino-agent",
		Description: "Eino-backed agentctl engine agent (spike, no tool bridging)",
		Model:       bridge,
	})
	if err != nil {
		return nil, fmt.Errorf("eino gate-on: create ChatModelAgent: %w", err)
	}

	adapter, err := NewEinoEngineAdapter(agent)
	if err != nil {
		return nil, fmt.Errorf("eino gate-on: wrap adapter: %w", err)
	}

	return adapter, nil
}
