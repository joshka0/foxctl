package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/execution/agentmanager"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/mailbox"
	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
	coretool "github.com/jkatigb/agentctl/internal/v2/core/tool"
	"github.com/oklog/ulid/v2"
)

func resolvedSpawnExecutionLayer(override string) agent.ExecutionLayer {
	switch resolvedAskDispatcherMode(override) {
	case askDispatchModeJido:
		return agent.ExecutionLayerJido
	default:
		return agent.ExecutionLayerClassic
	}
}

func jidoStartAgentForRecord(ctx context.Context, record agent.Agent, storageRoot, workspaceRoot string) error {
	client, err := v2jido.NewEnvJSONRPCClient()
	if err != nil {
		return fmt.Errorf("configure jido client: %w", err)
	}

	initialState, err := json.Marshal(map[string]any{
		"prompt":           strings.TrimSpace(record.Prompt),
		"max_iterations":   record.MaxIterations,
		"max_auto_turns":   record.MaxAutoTurns,
		"think_interval":   record.ThinkInterval,
		"memory_retention": string(record.MemoryRetention),
		"execution_layer":  string(record.ExecutionLayer),
	})
	if err != nil {
		return fmt.Errorf("marshal jido initial state: %w", err)
	}

	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "agentctl"
	}
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot, _ = os.Getwd()
	}
	absWorkspace, absErr := filepath.Abs(workspaceRoot)
	if absErr == nil {
		workspaceRoot = absWorkspace
	}
	pluginConfig, err := buildJidoPluginConfig(record.Role, binaryPath, workspaceRoot)
	if err != nil {
		return fmt.Errorf("build jido plugin config: %w", err)
	}

	req := v2jido.StartAgentRequest{
		RequestID:       ulid.Make().String(),
		IdempotencyKey:  record.ID,
		AgentID:         record.Namespace,
		Profile:         record.Role,
		MemoryRetention: string(record.MemoryRetention),
		ExecMode:        string(record.ExecMode),
		ThinkInterval:   record.ThinkInterval,
		InitialState:    initialState,
		Metadata: map[string]any{
			"plugin_config": pluginConfig,
		},
	}

	resp, err := client.StartAgent(ctx, req)
	if err != nil {
		return fmt.Errorf("jido runtime.start_agent: %w", err)
	}
	if status := strings.ToLower(strings.TrimSpace(resp.Status)); status != "" && status != "started" && status != "existing" {
		return fmt.Errorf("jido runtime.start_agent returned status %q", resp.Status)
	}
	return nil
}

func buildJidoPluginConfig(role, binaryPath, workspaceRoot string) (map[string]any, error) {
	profile := v2ProcessProfileForAgentRole(role)
	spec, err := v2jido.NewDefaultToolCommandSpec(profile, workspaceRoot, binaryPath, nil, false)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"binary":    strings.TrimSpace(binaryPath),
		"workspace": strings.TrimSpace(workspaceRoot),
		"transport": "daemon_rpc",
		"daemon":    true,
		"tool_command": map[string]any{
			"profile":            string(profile),
			"allowed_tools":      append([]string(nil), spec.AllowedTools...),
			"default_timeout_ms": spec.DefaultTimeout.Milliseconds(),
		},
	}, nil
}

func v2ProcessProfileForAgentRole(role string) coretool.ProcessProfile {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case string(coretool.ProfileOverseer):
		return coretool.ProfileOverseer
	case string(coretool.ProfileCompanion):
		return coretool.ProfileCompanion
	default:
		return coretool.ProfileWorker
	}
}

func jidoStopAgentForRecord(ctx context.Context, record agent.Agent) error {
	client, err := v2jido.NewEnvJSONRPCClient()
	if err != nil {
		return fmt.Errorf("configure jido client: %w", err)
	}
	resp, err := client.StopAgent(ctx, v2jido.StopAgentRequest{
		RequestID:      ulid.Make().String(),
		IdempotencyKey: record.ID,
		AgentID:        record.Namespace,
		Reason:         "agentctl agent kill",
	})
	if err != nil {
		return fmt.Errorf("jido runtime.stop_agent: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(resp.Status)) {
	case "", "stopped", "not_found":
		return nil
	default:
		return fmt.Errorf("jido runtime.stop_agent returned status %q", resp.Status)
	}
}

func spawnJidoManagedAgent(ctx context.Context, storageRoot, workspaceRoot string, req agentmanager.SpawnRequest) (agentmanager.SpawnResponse, error) {
	agentStore, err := agents.Open(ctx, storageRoot)
	if err != nil {
		return agentmanager.SpawnResponse{}, fmt.Errorf("open agent store: %w", err)
	}
	defer func() { errs.Ignore(agentStore.Close(), "close agent store") }()

	mailboxStore, err := mailbox.Open(ctx, storageRoot)
	if err != nil {
		return agentmanager.SpawnResponse{}, fmt.Errorf("open mailbox store: %w", err)
	}
	defer func() { errs.Ignore(mailboxStore.Close(), "close mailbox store") }()

	mgr := agentmanager.New(agentStore, mailboxStore)
	resp, err := mgr.Spawn(ctx, req)
	if err != nil {
		return agentmanager.SpawnResponse{}, err
	}

	record, err := agentStore.Get(ctx, resp.AgentID)
	if err != nil {
		return agentmanager.SpawnResponse{}, fmt.Errorf("load created agent: %w", err)
	}
	previousRecord := record
	record.ExecutionLayer = agent.ExecutionLayerJido
	if err := agentStore.Delete(ctx, record.ID); err != nil {
		return agentmanager.SpawnResponse{}, fmt.Errorf("reset created agent for jido layer: %w", err)
	}
	if err := agentStore.Create(ctx, record); err != nil {
		restoreErr := agentStore.Create(ctx, previousRecord)
		if restoreErr != nil {
			return agentmanager.SpawnResponse{}, fmt.Errorf("persist jido execution layer: %w (restore failed: %v)", err, restoreErr)
		}
		return agentmanager.SpawnResponse{}, fmt.Errorf("persist jido execution layer: %w", err)
	}

	if err := jidoStartAgentForRecord(ctx, record, storageRoot, workspaceRoot); err != nil {
		_ = agentStore.Delete(ctx, record.ID)
		if restoreErr := agentStore.Create(ctx, previousRecord); restoreErr != nil {
			return agentmanager.SpawnResponse{}, fmt.Errorf("%w (restore failed: %v)", err, restoreErr)
		}
		return agentmanager.SpawnResponse{}, err
	}
	if err := agentStore.UpdateState(ctx, record.ID, agent.StateRunning); err != nil {
		return agentmanager.SpawnResponse{}, fmt.Errorf("mark jido agent running: %w", err)
	}
	return resp, nil
}
