package rlm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ToolExecutor runs one read-only tool call for the experimental RLM runtime.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args json.RawMessage) (map[string]any, error)
}

// InspectRunner is the first experimental executor.
// It does not recurse yet; it uses the read-only tool surface to produce a deterministic inspection summary.
type InspectRunner struct {
	Tools ToolExecutor
}

// Run executes one bounded inspection pass over the environment.
func (r InspectRunner) Run(ctx context.Context, task Task, env Environment) (Result, error) {
	if err := ValidateTask(task); err != nil {
		return Result{}, err
	}
	if err := ValidateEnvironment(env); err != nil {
		return Result{}, err
	}

	answerParts := make([]string, 0, 8)
	evidence := make([]string, 0, 12)
	metadata := map[string]any{
		"repo_handle_count":   len(env.RepoHandles),
		"vault_handle_count":  len(env.VaultHandles),
		"scene_handle_count":  len(env.SceneHandles),
		"thread_handle_count": len(env.ActiveThreadIDs),
	}

	if objective := stringField(env.TopOfMind, "objective"); objective != "" {
		answerParts = append(answerParts, "Objective: "+objective)
	}
	if phase := stringField(env.TopOfMind, "phase"); phase != "" {
		answerParts = append(answerParts, "Phase: "+phase)
	}
	if summary := stringField(env.LatestHandoff, "summary"); summary != "" {
		answerParts = append(answerParts, "Latest handoff: "+summary)
	}

	if r.Tools != nil {
		if repoSummary, repoRefs, err := inspectRepo(ctx, r.Tools, task.Prompt); err == nil {
			if repoSummary != "" {
				answerParts = append(answerParts, repoSummary)
			}
			evidence = append(evidence, repoRefs...)
		}
		if len(env.VaultHandles) > 0 {
			if vaultSummary, vaultRefs, err := inspectVault(ctx, r.Tools, task.Prompt); err == nil {
				if vaultSummary != "" {
					answerParts = append(answerParts, vaultSummary)
				}
				evidence = append(evidence, vaultRefs...)
			}
		}
		if len(env.SceneHandles) > 0 || len(env.ActiveThreadIDs) > 0 {
			if sceneSummary, sceneRefs, err := inspectScenes(ctx, r.Tools, task.Prompt); err == nil {
				if sceneSummary != "" {
					answerParts = append(answerParts, sceneSummary)
				}
				evidence = append(evidence, sceneRefs...)
			}
		}
		if shouldSubcall(task, env) {
			if subSummary, subRefs, err := inspectSubcall(ctx, r.Tools, task, env); err == nil {
				if subSummary != "" {
					answerParts = append(answerParts, subSummary)
				}
				evidence = append(evidence, subRefs...)
				metadata["subcall_used"] = true
			}
		}
	}

	if len(answerParts) == 0 {
		answerParts = append(answerParts, "Bootstrap complete. No contextual signals were available yet.")
	}

	return Result{
		Answer:       strings.Join(answerParts, "\n"),
		EvidenceRefs: uniqueStringsRLM(evidence),
		Iterations:   1,
		Subcalls:     boolToInt(metadata["subcall_used"] == true),
		Metadata:     metadata,
	}, nil
}

func shouldSubcall(task Task, env Environment) bool {
	if task.MaxDepth <= 0 || task.MaxSubcalls <= 0 {
		return false
	}
	handleCount := len(env.RepoHandles) + len(env.VaultHandles) + len(env.SceneHandles) + len(env.ArtifactHandles)
	return handleCount > 3
}

func inspectRepo(ctx context.Context, tools ToolExecutor, query string) (string, []string, error) {
	payload, err := tools.Execute(ctx, "search_repo", mustJSONRLM(map[string]any{
		"query": query,
		"limit": 3,
	}))
	if err != nil {
		return "", nil, err
	}
	results := decodeResults(payload["results"])
	if len(results) == 0 {
		return "", nil, nil
	}
	paths := make([]string, 0, len(results))
	for _, item := range results {
		if path := strings.TrimSpace(fmt.Sprint(item["path"])); path != "" {
			paths = append(paths, "path:"+path)
		}
	}
	return "Repo focus: " + strings.Join(shortenRefs(paths, 3), ", "), paths, nil
}

func inspectVault(ctx context.Context, tools ToolExecutor, query string) (string, []string, error) {
	payload, err := tools.Execute(ctx, "search_vault", mustJSONRLM(map[string]any{
		"query": query,
		"limit": 3,
	}))
	if err != nil {
		return "", nil, err
	}
	results := decodeResults(payload["results"])
	if len(results) == 0 {
		return "", nil, nil
	}
	refs := make([]string, 0, len(results))
	for _, item := range results {
		if path := strings.TrimSpace(fmt.Sprint(item["path"])); path != "" {
			refs = append(refs, "note:"+path)
		}
	}
	return "Vault focus: " + strings.Join(shortenRefs(refs, 3), ", "), refs, nil
}

func inspectScenes(ctx context.Context, tools ToolExecutor, query string) (string, []string, error) {
	payload, err := tools.Execute(ctx, "search_scenes", mustJSONRLM(map[string]any{
		"query": query,
		"limit": 3,
	}))
	if err != nil {
		return "", nil, err
	}
	results := decodeResults(payload["results"])
	if len(results) == 0 {
		return "", nil, nil
	}
	refs := make([]string, 0, len(results))
	for _, item := range results {
		if handle := strings.TrimSpace(fmt.Sprint(item["handle"])); handle != "" {
			refs = append(refs, handle)
		}
	}
	return "Scene focus: " + strings.Join(shortenRefs(refs, 3), ", "), refs, nil
}

func inspectSubcall(ctx context.Context, tools ToolExecutor, task Task, env Environment) (string, []string, error) {
	payload, err := tools.Execute(ctx, "subcall", mustJSONRLM(map[string]any{
		"prompt":           "Inspect the most relevant subset for: " + task.Prompt,
		"repo_handles":     shortenRefs(env.RepoHandles, 1),
		"vault_handles":    shortenRefs(env.VaultHandles, 1),
		"scene_handles":    shortenRefs(env.SceneHandles, 1),
		"artifact_handles": shortenRefs(env.ArtifactHandles, 1),
		"max_depth":        task.MaxDepth - 1,
		"max_iterations":   maxInt(task.MaxIterations-1, 1),
		"max_subcalls":     maxInt(task.MaxSubcalls-1, 0),
	}))
	if err != nil {
		return "", nil, err
	}
	raw, err := json.Marshal(payload["result"])
	if err != nil {
		return "", nil, err
	}
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", nil, err
	}
	if strings.TrimSpace(result.Answer) == "" {
		return "", nil, nil
	}
	return "Subcall: " + strings.TrimSpace(result.Answer), result.EvidenceRefs, nil
}

func decodeResults(value any) []map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func stringField(value map[string]any, key string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value[key]))
}

func uniqueStringsRLM(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func shortenRefs(values []string, max int) []string {
	if len(values) <= max {
		return values
	}
	return values[:max]
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func mustJSONRLM(value map[string]any) json.RawMessage {
	body, _ := json.Marshal(value)
	return body
}
