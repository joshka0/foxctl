package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const defaultAgentLimit = 25

// LoadInitialShellState loads the base shell snapshot and optionally enriches it with live agents.
func LoadInitialShellState(ctx context.Context, opts Options) (ShellState, error) {
	state, err := LoadShellState(opts)
	if err != nil {
		return ShellState{}, err
	}

	baseURL := strings.TrimSpace(opts.APIBaseURL)
	if baseURL == "" {
		return state, nil
	}

	if ctx == nil {
		ctx = context.Background()
	}

	client, err := NewAPIClient(baseURL, nil)
	if err != nil {
		return ShellState{}, fmt.Errorf("configure --api-base-url %q: %w", baseURL, err)
	}
	adapter, err := NewAgentAdapter(client)
	if err != nil {
		return ShellState{}, fmt.Errorf("configure agent adapter: %w", err)
	}

	agents, err := adapter.ListAgents(ctx, normalizeAgentLimit(opts.AgentLimit))
	if err != nil {
		return ShellState{}, fmt.Errorf(
			"load agents from --api-base-url %q: %w; verify foxctl API is reachable and /api/agents is available",
			client.BaseURL(),
			err,
		)
	}

	state.Workers = mapAgentsToWorkers(agents.Agents)
	return state, nil
}

func normalizeAgentLimit(limit int) int {
	if limit <= 0 {
		return defaultAgentLimit
	}
	return limit
}

func mapAgentsToWorkers(agents []AgentRecord) []WorkerSummary {
	type sortableWorker struct {
		worker WorkerSummary
		name   string
		id     string
	}

	mapped := make([]sortableWorker, 0, len(agents))
	for _, agent := range agents {
		displayName := workerDisplayName(agent)
		mapped = append(mapped, sortableWorker{
			worker: WorkerSummary{
				Name:   displayName,
				Status: workerStatus(agent),
				Task:   workerTask(agent),
			},
			name: strings.ToLower(displayName),
			id:   strings.TrimSpace(agent.ID),
		})
	}

	sort.SliceStable(mapped, func(i, j int) bool {
		if mapped[i].name != mapped[j].name {
			return mapped[i].name < mapped[j].name
		}
		if mapped[i].id != mapped[j].id {
			return mapped[i].id < mapped[j].id
		}
		return mapped[i].worker.Task < mapped[j].worker.Task
	})

	workers := make([]WorkerSummary, 0, len(mapped))
	for _, item := range mapped {
		workers = append(workers, item.worker)
	}
	return workers
}

func workerDisplayName(agent AgentRecord) string {
	if name := firstNonEmpty(agent.Name, agent.Slug, agent.ID); name != "" {
		return name
	}
	return "unknown-agent"
}

func workerStatus(agent AgentRecord) string {
	if status := strings.TrimSpace(agent.State); status != "" {
		return status
	}
	return "unknown"
}

func workerTask(agent AgentRecord) string {
	parts := make([]string, 0, 4)

	if role := strings.TrimSpace(agent.Role); role != "" {
		parts = append(parts, "role="+role)
	}
	if mode := strings.TrimSpace(agent.ExecMode); mode != "" {
		parts = append(parts, "mode="+mode)
	}

	model := strings.TrimSpace(agent.LLMModel)
	provider := strings.TrimSpace(agent.LLMProvider)
	switch {
	case provider != "" && model != "":
		parts = append(parts, "model="+provider+"/"+model)
	case model != "":
		parts = append(parts, "model="+model)
	case provider != "":
		parts = append(parts, "model="+provider)
	}

	workspace := firstNonEmpty(agent.WorkspaceRoot, agent.Namespace)
	if workspace != "" {
		parts = append(parts, "workspace="+workspace)
	}

	if len(parts) == 0 {
		return "no runtime metadata"
	}
	return strings.Join(parts, " | ")
}
