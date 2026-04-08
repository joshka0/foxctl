package eino

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"

	"github.com/jkatigb/agentctl/internal/engine"
)

// ProvisionFromLLMConfig constructs a real Eino-backed engine adapter from a
// provider-resolved LLMChatConfig. It:
//  1. Wraps the resolved connection parameters in an oaiModelBridge that implements
//     model.BaseChatModel against the same OpenAI-compatible endpoint.
//  2. Bridges the provided agentctl ToolDefs and ToolExecutor into Eino tools.
//  3. Creates an adk.ChatModelAgent with that model and the bridged tools.
//  4. Returns the EinoEngineAdapter that satisfies engine.AgentEngine.
//
// The resolved config must have APIKey, BaseURL, and Model already filled in
// (i.e. passed after engine.NewLLMChatEngine has run auto-detection).
// Bedrock configs are rejected: the bridge uses Bearer-token auth only.
func ProvisionFromLLMConfig(cfg engine.LLMChatConfig, executor engine.ToolExecutor, defs []engine.ToolDef) (*EinoEngineAdapter, error) {
	if cfg.Provider == "bedrock" {
		return nil, fmt.Errorf("eino gate-on: Bedrock provider is not supported by the spike bridge (use a standard OpenAI-compatible provider)")
	}
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("eino gate-on: resolved BaseURL is empty — cannot provision bridge")
	}
	if cfg.Model == "" {
		return nil, fmt.Errorf("eino gate-on: resolved Model is empty — cannot provision bridge")
	}

	modelBridge := newOAIModelBridge(cfg.APIKey, cfg.BaseURL, cfg.Model, cfg.Timeout)

	// NewEinoToolBridges returns []tool.InvokableTool; ToolsNodeConfig requires
	// []tool.BaseTool. Convert explicitly since Go slices are not covariant.
	invokable := NewEinoToolBridges(defs, executor)
	baseTools := make([]tool.BaseTool, len(invokable))
	for i, t := range invokable {
		baseTools[i] = t
	}

	agent, err := adk.NewChatModelAgent(context.Background(), &adk.ChatModelAgentConfig{
		Name:        "agentctl-eino-agent",
		Description: "Eino-backed agentctl engine agent",
		Model:       modelBridge,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: baseTools,
			},
		},
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
