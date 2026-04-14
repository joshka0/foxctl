package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextplane/taskhistory"
	"github.com/joshka0/foxctl/internal/domain/agent"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	ws "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/runtime/execution/agentmanager"
	"github.com/joshka0/foxctl/internal/storage/agents"
	"github.com/joshka0/foxctl/internal/storage/mailbox"
	v2jido "github.com/joshka0/foxctl/internal/v2/adapters/jido"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
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

	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "foxctl"
	}
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot, _ = os.Getwd()
	}
	absWorkspace, absErr := filepath.Abs(workspaceRoot)
	if absErr == nil {
		workspaceRoot = absWorkspace
	}
	initialStateMap := buildJidoInitialState(record, workspaceRoot, nil)
	if continuity := taskContinuityState(ctx, storageRoot, workspaceRoot); len(continuity) > 0 {
		initialStateMap["task_continuity"] = continuity
	}
	initialState, err := json.Marshal(initialStateMap)
	if err != nil {
		return fmt.Errorf("marshal jido initial state: %w", err)
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

func buildJidoInitialState(record agent.Agent, workspaceRoot string, continuity map[string]any) map[string]any {
	state := map[string]any{
		"prompt":           strings.TrimSpace(record.Prompt),
		"max_iterations":   record.MaxIterations,
		"max_auto_turns":   record.MaxAutoTurns,
		"think_interval":   record.ThinkInterval,
		"memory_retention": string(record.MemoryRetention),
		"execution_layer":  string(record.ExecutionLayer),
	}
	if root := strings.TrimSpace(workspaceRoot); root != "" {
		state["workspace_root"] = root
	}
	if len(continuity) > 0 {
		state["task_continuity"] = continuity
	}
	return state
}

func taskContinuityState(ctx context.Context, storageRoot, workspaceRoot string) map[string]any {
	collector, cleanup, err := taskhistory.OpenCollector(ctx, storageRoot, workspaceRoot, "")
	if err != nil {
		return nil
	}
	defer cleanup()
	pack, err := collector.Collect(ctx, taskhistory.Options{
		WorkspacePath:          workspaceRoot,
		WorkspaceID:            ws.CanonicalID(workspaceRoot),
		TranscriptHistoryScope: taskhistory.DefaultTranscriptHistoryScope(),
	})
	if err != nil {
		return nil
	}
	artifact := ""
	if cfg, err := loadConfig(ctx); err == nil {
		if digest, persistErr := taskhistory.PersistPack(ctx, cfg.Paths.CAS, pack); persistErr == nil {
			artifact = digest
		}
	}
	return taskhistory.RenderJidoStateWithArtifact(pack, artifact)
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
		Reason:         "foxctl agent kill",
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
