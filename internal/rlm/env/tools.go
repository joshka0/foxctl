package env

import (
	"encoding/json"

	"github.com/joshka0/foxctl/internal/rlm"
)

// DefaultTools returns the 6 composite retrieval tools for the canonical RLM runtime.
func DefaultTools() []rlm.Tool {
	return []rlm.Tool{
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
		{
			Name:        "load_evidence_ref",
			Description: "Load one evidence ref by typed EvidenceRef identifier. Returns bounded output (default 4096 tokens). Invalid refs return structured error.",
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
