package env

import (
	"encoding/json"

	"github.com/joshka0/foxctl/internal/rlm"
)

// DefaultTools returns the composite retrieval tools for the canonical RLM runtime.
func DefaultTools() []rlm.Tool {
	return []rlm.Tool{
		{
			Name:        "gather_context",
			Description: "Gather bounded context across code, memory, context, and task lanes. Returns a reduced ContextBundle. With response_mode=answer_surface, prefer copying answer_seed.paths and answer_seed.facts, using path_set.must/load_ref for verification before inferring from raw evidence.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Context gathering query.",
				},
				"goal": map[string]any{
					"type":        "string",
					"description": "Optional context goal such as answer, plan, debug, recall, or research.",
				},
				"required_evidence": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional terms, symbols, or claims that the returned bundle should try to cover with evidence.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum reduced context facts. Defaults to 10.",
				},
				"task_id": map[string]any{
					"type":        "string",
					"description": "Optional task ID for task-linked context.",
				},
				"task_type": map[string]any{
					"type":        "string",
					"description": "Optional task intent such as file_locate, symbol_inspect, execution_trace, change_impact, registration_trace, architecture_map, subsystem_map, or integration_surface.",
				},
				"source_profiles": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional explicit source profiles to prefer or shape, such as repo_code, repo_docs, codemaps, cochange_history, memory, task, session, or vault_docs.",
				},
				"languages": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional explicit code-language constraints for repo_code searches, such as go, typescript, elixir, python, or markdown.",
				},
				"path_prefixes": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional repo-relative path prefixes to constrain repo_code results, such as apps/api or packages/core.",
				},
				"memory_statuses": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional memory claim statuses to include, such as current, candidate, needs_revalidation, stale, superseded, or rejected. Defaults to current.",
				},
				"lanes": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional lane subset: code, memory, context, task. Defaults to mixed retrieval across all lanes.",
				},
				"max_context_chars": map[string]any{
					"type":        "integer",
					"description": "Optional downstream context character budget for future reducers.",
				},
				"response_mode": map[string]any{
					"type":        "string",
					"description": "Optional response shape: full for full bundle, answer_surface or compact for answer_seed/path_set/facts without raw evidence. Mini/default profiles should use answer_surface.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "load_evidence_ref",
			Description: "Load one evidence ref by typed EvidenceRef identifier. Returns bounded output (default 4096 tokens). Invalid refs return structured error. Use only to verify refs from gather_context, not to re-rank answer_seed paths.",
			Parameters: objectSchema(map[string]any{
				"ref": map[string]any{
					"type":        "string",
					"description": "Typed evidence ref identifier such as path:internal/foo.go, symbol:Bar, task:abc, or memory_claim:xyz.",
				},
				"max_tokens": map[string]any{
					"type":        "integer",
					"description": "Maximum output tokens. Defaults to 4096.",
				},
			}, "ref"),
			ReadOnly: true,
		},
		{
			Name:        "code_search_ensemble",
			Description: "Debug/eval repo retrieval controller for file, symbol, and execution-trace tasks. Prefer gather_context for normal answers; use this only to inspect repo candidate traces and retrieval buckets.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Repo retrieval query.",
				},
				"task_type": map[string]any{
					"type":        "string",
					"description": "Optional task intent: file_locate, execution_trace, symbol_inspect, change_impact, or registration_trace.",
				},
				"candidate_paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional seed paths to include in the candidate set.",
				},
				"constraints": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
					"description":          "Optional debug constraints such as exclude_paths, include_history, include_aca, or require_grounding.",
				},
				"budget": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
					"description":          "Optional debug budget such as max_steps, max_candidates, max_files, max_snippets, or max_tokens_out.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "retrieve_code",
			Description: "Retrieve code evidence from the repository using semantic and structural search. Returns an EvidencePack with code-lane evidence nodes.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Code retrieval query: semantic, structural, or literal search over the repository.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of evidence nodes to return. Defaults to 10.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "retrieve_memory",
			Description: "Retrieve memory evidence from the companion hard state, assumptions, and durable claims. Returns an EvidencePack with memory-lane evidence nodes.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Memory retrieval query: past decisions, facts, timeline, or context.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of evidence nodes to return. Defaults to 10.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "retrieve_context",
			Description: "Retrieve context evidence from the ACA top-of-mind projection, handoffs, and workspace knowledge layer. Returns an EvidencePack with context-lane evidence nodes.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Context retrieval query: current workspace objective, phase, blockers, or decisions.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of evidence nodes to return. Defaults to 10.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "retrieve_task",
			Description: "Retrieve task evidence from the task store, task history, and task context projections. Returns an EvidencePack with task-lane evidence nodes.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Task retrieval query: active tasks, recent task history, or task-linked context.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of evidence nodes to return. Defaults to 10.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "retrieve_mixed",
			Description: "Retrieve mixed evidence by fanning out to all four retrieval lanes (code, memory, context, task) concurrently. Fuses results by typed ref identity. Returns a unified EvidencePack.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Mixed retrieval query: fan out across all lanes and fuse evidence.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of fused evidence nodes to return. Defaults to 10.",
				},
			}, "query"),
			ReadOnly: true,
		},
	}
}

func objectSchema(properties map[string]any, required ...string) json.RawMessage {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	body, _ := json.Marshal(schema)
	return body
}
