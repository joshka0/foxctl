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
		tools := newAllowlistedToolExecutor(r.Tools, env.Tools)
		if summary, refs, err := inspectMixed(ctx, tools, task.Prompt); err == nil {
			if summary != "" {
				answerParts = append(answerParts, summary)
			}
			evidence = append(evidence, refs...)
		}
	}

	if len(answerParts) == 0 {
		answerParts = append(answerParts, "Bootstrap complete. No contextual signals were available yet.")
	}

	return Result{
		Answer:       strings.Join(answerParts, "\n"),
		EvidenceRefs: uniqueStringsRLM(evidence),
		Iterations:   1,
		Subcalls:     0,
		Metadata:     metadata,
	}, nil
}

func inspectMixed(ctx context.Context, tools ToolExecutor, query string) (string, []string, error) {
	payload, err := tools.Execute(ctx, "retrieve_mixed", mustJSONRLM(map[string]any{
		"query": query,
		"limit": 3,
	}))
	if err != nil {
		return "", nil, err
	}
	refs := collectEvidenceRefsFromPayload(payload)
	if len(refs) == 0 {
		return "", nil, nil
	}
	return "Mixed evidence focus: " + strings.Join(shortenRefs(refs, 3), ", "), refs, nil
}

func collectEvidenceRefsFromPayload(payload map[string]any) []string {
	var refs []string
	collectEvidenceRefsRecursive(payload, &refs)
	if len(refs) == 0 {
		var decoded any
		if body, err := json.Marshal(payload); err == nil && json.Unmarshal(body, &decoded) == nil {
			collectEvidenceRefsRecursive(decoded, &refs)
		}
	}
	return uniqueStringsRLM(refs)
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

func mustJSONRLM(value map[string]any) json.RawMessage {
	body, _ := json.Marshal(value)
	return body
}
