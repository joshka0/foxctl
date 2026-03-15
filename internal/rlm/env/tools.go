package env

import (
	"encoding/json"

	"github.com/jkatigb/agentctl/internal/rlm"
)

// DefaultTools returns the initial read-only tool surface for the experimental RLM runtime.
func DefaultTools() []rlm.Tool {
	return []rlm.Tool{
		{
			Name:        "get_top_of_mind",
			Description: "Load the ACA top-of-mind bundle for the workspace.",
			Parameters:  emptyObjectSchema(),
			ReadOnly:    true,
		},
		{
			Name:        "get_latest_handoff",
			Description: "Load the latest ACA handoff for the workspace.",
			Parameters:  emptyObjectSchema(),
			ReadOnly:    true,
		},
		{
			Name:        "search_scenes",
			Description: "Search companion scenes or episodes by summary/topic.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Text query for matching scene or episode summaries.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return. Defaults to 10.",
				},
			}),
			ReadOnly: true,
		},
		{
			Name:        "get_scene",
			Description: "Load one companion scene or episode by handle.",
			Parameters: objectSchema(map[string]any{
				"handle": map[string]any{
					"type":        "string",
					"description": "Scene handle such as episode:42 or conversation:<id>.",
				},
			}, "handle"),
			ReadOnly: true,
		},
		{
			Name:        "search_artifacts",
			Description: "Search persisted artifacts and trajectory handles by query.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Text query to match artifact or trajectory handles.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of handles to return. Defaults to 10.",
				},
			}),
			ReadOnly: true,
		},
		{
			Name:        "load_artifact",
			Description: "Load one bounded artifact, trajectory, event, file, or note by handle.",
			Parameters: objectSchema(map[string]any{
				"handle": map[string]any{
					"type":        "string",
					"description": "Artifact handle such as trajectory:<id>, artifact:<digest>, event:<trajectory>:<event>, path:<file>, or note:<path>.",
				},
			}, "handle"),
			ReadOnly: true,
		},
		{
			Name:        "semantic_search_code",
			Description: "Preferred first-pass repo understanding tool. Run the repo-aware code semantic search skill over the current workspace.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Semantic code query for files, symbols, packages, or architectural concepts.",
				},
				"scope": map[string]any{
					"type":        "array",
					"description": "Optional semantic_search scopes such as symbols, codemaps, sessions, memories, tasks, or context.",
					"items":       map[string]any{"type": "string"},
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of semantic search results to return.",
				},
				"repo_index_mode": map[string]any{
					"type":        "string",
					"description": "Optional repo index mode override, such as off, search, or dag.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "smart_search_code",
			Description: "Preferred follow-up repo understanding tool. Run the smart code search pipeline to produce candidate files and snippets.",
			Parameters: objectSchema(map[string]any{
				"question": map[string]any{
					"type":        "string",
					"description": "Natural-language coding question to answer with code candidates and snippets.",
				},
				"repo_index_mode": map[string]any{
					"type":        "string",
					"description": "Optional repo index mode override, such as off, search, or dag.",
				},
				"max_candidates": map[string]any{
					"type":        "integer",
					"description": "Optional maximum number of candidate files to consider.",
				},
				"max_snippets": map[string]any{
					"type":        "integer",
					"description": "Optional maximum number of snippets to emit.",
				},
			}, "question"),
			ReadOnly: true,
		},
		{
			Name:        "ripgrep_code",
			Description: "Use for exact text, symbol, or literal pattern searches after semantic search narrows the area.",
			Parameters: objectSchema(map[string]any{
				"pattern": map[string]any{
					"type":        "string",
					"description": "Regex or literal search pattern for code/context_ripgrep.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional workspace-relative path to limit the search.",
				},
				"glob": map[string]any{
					"type":        "array",
					"description": "Optional file glob filters.",
					"items":       map[string]any{"type": "string"},
				},
				"glob_not": map[string]any{
					"type":        "array",
					"description": "Optional negative glob filters.",
					"items":       map[string]any{"type": "string"},
				},
				"max_matches": map[string]any{
					"type":        "integer",
					"description": "Optional maximum raw matches.",
				},
				"max_blocks": map[string]any{
					"type":        "integer",
					"description": "Optional maximum expanded code blocks.",
				},
			}, "pattern"),
			ReadOnly: true,
		},
		{
			Name:        "search_repo",
			Description: "Fallback shallow repo graph search over projected file anchors. Prefer semantic_search_code or smart_search_code first for repo understanding queries.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Repo query text, such as a subsystem, symbol, path fragment, or concept.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of projected repo results to return.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "expand_repo_graph",
			Description: "Expand the repo graph around one seed handle.",
			Parameters: objectSchema(map[string]any{
				"seed": map[string]any{
					"type":        "string",
					"description": "Repo seed handle to expand from, usually a repo:<node> or path:<file> style handle.",
				},
				"depth": map[string]any{
					"type":        "integer",
					"description": "Graph expansion depth. Defaults to the adapter defaults when omitted.",
				},
				"budget": map[string]any{
					"type":        "integer",
					"description": "Maximum expansion budget for returned anchors.",
				},
			}, "seed"),
			ReadOnly: true,
		},
		{
			Name:        "load_file",
			Description: "Load a bounded file slice from the workspace.",
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute or workspace-relative file path.",
				},
				"start_line": map[string]any{
					"type":        "integer",
					"description": "Optional 1-based start line for slicing.",
				},
				"end_line": map[string]any{
					"type":        "integer",
					"description": "Optional 1-based end line for slicing.",
				},
			}, "path"),
			ReadOnly: true,
		},
		{
			Name:        "search_vault",
			Description: "Search the Obsidian or vault knowledge plane for relevant notes.",
			Parameters: objectSchema(map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Vault search query text.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"description": "Maximum number of note hits to return.",
				},
			}, "query"),
			ReadOnly: true,
		},
		{
			Name:        "read_note",
			Description: "Load one durable note by note handle or vault-relative path.",
			Parameters: objectSchema(map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Vault-relative note path or note:<path> handle.",
				},
			}, "path"),
			ReadOnly: true,
		},
		{
			Name:        "subcall",
			Description: "Issue one bounded recursive subcall over selected repo, vault, scene, or artifact handles.",
			Parameters: objectSchema(map[string]any{
				"prompt": map[string]any{
					"type":        "string",
					"description": "Focused child prompt for the recursive subcall.",
				},
				"repo_handles": map[string]any{
					"type":        "array",
					"description": "Optional narrowed repo handles for the child call.",
					"items":       map[string]any{"type": "string"},
				},
				"vault_handles": map[string]any{
					"type":        "array",
					"description": "Optional narrowed vault handles for the child call.",
					"items":       map[string]any{"type": "string"},
				},
				"scene_handles": map[string]any{
					"type":        "array",
					"description": "Optional narrowed scene handles for the child call.",
					"items":       map[string]any{"type": "string"},
				},
				"artifact_handles": map[string]any{
					"type":        "array",
					"description": "Optional narrowed artifact handles for the child call.",
					"items":       map[string]any{"type": "string"},
				},
				"max_depth": map[string]any{
					"type":        "integer",
					"description": "Child max recursive depth budget.",
				},
				"max_iterations": map[string]any{
					"type":        "integer",
					"description": "Child max iteration budget.",
				},
				"max_subcalls": map[string]any{
					"type":        "integer",
					"description": "Child max subcall budget.",
				},
			}, "prompt"),
			ReadOnly: true,
		},
	}
}

func emptyObjectSchema() json.RawMessage {
	return objectSchema(nil)
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
