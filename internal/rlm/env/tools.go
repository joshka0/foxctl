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
			Description: "Gather bounded context across code, memory, context, and task lanes. Defaults to response_mode=answer_surface for a reduced answer seed/path_set surface. Graph-sensitive tasks include graph recommendation metadata by default; set graph_mode=summary to attach compact graph confidence/gaps/roots/top nodes. Use response_mode=full only for eval/debug bundle inspection. Prefer copying answer_seed.paths and answer_seed.facts, using path_set.must/load_ref for verification before inferring from raw evidence.",
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
				"coverage_requirements": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"id":              map[string]any{"type": "string"},
							"kind":            map[string]any{"type": "string"},
							"label":           map[string]any{"type": "string"},
							"terms":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"required":        map[string]any{"type": "boolean"},
							"min_paths":       map[string]any{"type": "integer"},
							"weight":          map[string]any{"type": "number"},
							"source_profiles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
					},
					"description": "Optional structured reducer coverage slots. Prefer this when the task needs one selected path per role/concept.",
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
				"excluded_paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional repo-relative paths, prefixes, or glob patterns to suppress from repo_code results, such as node_modules, dist, build, generated, or nested worktrees.",
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
					"description": "Optional response shape: defaults to answer_surface. Use full only for eval/debug bundle inspection; answer_surface or compact returns answer_seed/path_set/facts without raw evidence.",
				},
				"graph_mode": map[string]any{
					"type":        "string",
					"enum":        []string{"", "none", "summary"},
					"description": "Optional context graph attachment mode: empty or none only emits graph recommendation metadata; summary attaches compact context_graph confidence, gaps, roots, and top nodes only for answer_surface/compact responses.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "gather_test_context",
			Description: "Gather bounded test context for a repo question. This is the explicit test surface: use it only when tests, specs, fixtures, mocks, or test helpers are requested. Defaults to answer_surface and repo_code with test coverage enabled.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Test-context gathering query.",
				},
				"required_evidence": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional test terms, symbols, fixtures, or behaviors to cover.",
				},
				"task_type": map[string]any{
					"type":        "string",
					"description": "Optional task intent. Defaults to the shared gather_context reducer behavior.",
				},
				"languages": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional language constraints.",
				},
				"path_prefixes": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional repo-relative path prefixes.",
				},
				"excluded_paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional repo-relative paths, prefixes, or globs to suppress.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum reduced context facts. Defaults to 10.",
				},
				"max_context_chars": map[string]any{
					"type":        "integer",
					"description": "Optional downstream context character budget.",
				},
				"response_mode": map[string]any{
					"type":        "string",
					"description": "Optional response shape: defaults to answer_surface.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "gather_docs_context",
			Description: "Gather bounded repo documentation context. This is the explicit docs surface: use it for docs/design/architecture/readme questions. It defaults to repo_docs, documentation_map, and excludes embedded project noise under docs.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Documentation-context gathering query.",
				},
				"required_evidence": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional doc topics or headings to cover.",
				},
				"path_prefixes": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional repo-relative docs path prefixes.",
				},
				"excluded_paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional repo-relative paths, prefixes, or globs to suppress in addition to default docs noise filters.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum reduced context facts. Defaults to 10.",
				},
				"max_context_chars": map[string]any{
					"type":        "integer",
					"description": "Optional downstream context character budget.",
				},
				"response_mode": map[string]any{
					"type":        "string",
					"description": "Optional response shape: defaults to answer_surface.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "expand_context_graph",
			Description: "Expand compact dependency, dependent, test, config, docs, schema, and data context around selected gather_context roots. Returns graph evidence and confidence, not file bodies. Use after gather_context when dependency completeness matters.",
			Parameters: objectSchema(map[string]any{
				"roots": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Required root refs or repo-relative paths, usually path_set.must load_ref values from gather_context.",
				},
				"query": map[string]any{
					"type":        "string",
					"description": "Optional original context query for confidence and diagnostics.",
				},
				"task_type": map[string]any{
					"type":        "string",
					"description": "Optional task intent such as execution_trace, change_impact, subsystem_map, architecture_map, or integration_surface.",
				},
				"depth": map[string]any{
					"type":        "integer",
					"description": "Optional graph depth. Defaults to 1.",
				},
				"direction": map[string]any{
					"type":        "string",
					"description": "Optional direction: both, out, or in. Defaults to both.",
				},
				"source_profiles": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional source profiles for diagnostics and future provider shaping.",
				},
				"coverage_requirements": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"properties": map[string]any{
							"id":              map[string]any{"type": "string"},
							"kind":            map[string]any{"type": "string"},
							"label":           map[string]any{"type": "string"},
							"terms":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
							"required":        map[string]any{"type": "boolean"},
							"min_paths":       map[string]any{"type": "integer"},
							"weight":          map[string]any{"type": "number"},
							"source_profiles": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						},
					},
					"description": "Optional structured coverage requirements copied from gather_context.",
				},
				"include_tests": map[string]any{
					"type":        "boolean",
					"description": "When true, include TESTS edges where indexed.",
				},
				"include_adjacent": map[string]any{
					"type":        "boolean",
					"description": "Reserved for bounded config/docs/schema/data adjacency expansion.",
				},
				"path_prefixes": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional repo-relative prefixes roots must be under.",
				},
				"excluded_paths": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional repo-relative paths, prefixes, or globs to suppress.",
				},
				"budget": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
					"description":          "Optional graph budget: max_roots, max_nodes, max_edges, max_depth, per_node_cap.",
				},
			}, "roots"),
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
