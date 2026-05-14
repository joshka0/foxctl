package tools

import (
	"encoding/json"

	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	"github.com/joshka0/foxctl/internal/v2/runtime/profiles"
)

// DefaultDefs returns the portable default v2 tool definitions used by the
// profile-aware catalog. Names use slash-form contracts and are normalized by
// the catalog/toolname layer on load.
func DefaultDefs() []coretool.ToolDef {
	return []coretool.ToolDef{
		readOnlyDef("fs/read_file", "Read a file from the workspace.", schemaObject(
			req("path", "string", "Path to the file"),
			prop("max_bytes", "integer", "Maximum bytes to read"),
		)),
		readOnlyDef("fs/list_dir", "List directory contents from the workspace.", schemaObject(
			req("path", "string", "Path to the directory"),
			prop("depth", "integer", "Maximum recursion depth"),
		)),
		def("fs/write_file", "Write a file in the workspace.", schemaObject(
			req("path", "string", "Path to the file"),
			req("content", "string", "File content"),
		)),

		readOnlyDef("code/search", "Search code with ripgrep-backed recall.", schemaObject(
			req("pattern", "string", "Search pattern"),
			prop("path", "string", "Optional subpath"),
			prop("file_pattern", "string", "Optional glob"),
			prop("context_lines", "integer", "Context lines"),
			prop("max_results", "integer", "Maximum result count"),
		)),
		readOnlyDef("code/symbols", "Inspect symbols in a file.", schemaObject(
			req("path", "string", "Path to inspect"),
			prop("symbol_type", "string", "Optional symbol kind filter"),
			prop("include_docs", "boolean", "Include symbol docs"),
		)),
		readOnlyDef("context/search", "Semantic tree/context search over the repo.", schemaObject(
			req("query", "string", "Question or search topic"),
			prop("limit", "integer", "Maximum result count"),
		)),
		readOnlyDef("smart/search", "Search and extract code evidence in one call.", schemaObject(
			req("question", "string", "Question to investigate"),
			prop("limits", "object", "Optional search limits"),
		)),
		readOnlyDef("context/grep", "Search with function-body context.", schemaObject(
			req("pattern", "string", "Pattern to search"),
			prop("path", "string", "Optional subpath"),
		)),
		def("context/filter", "Filter chunks for the most relevant context.", schemaObject(
			req("prompt", "string", "Prompt to optimize for"),
			req("source", "object", "Source text or chunks"),
			prop("budget", "object", "Optional budget config"),
			prop("llm", "object", "Optional LLM override"),
		)),

		readOnlyDef("repo_index/search", "Search repo index nodes.", schemaObject(
			req("query", "string", "FTS query string"),
			prop("limit", "integer", "Maximum result count"),
		)),
		readOnlyDef("repo_index/expand", "Expand the repo index graph.", schemaObject(
			req("seeds", "array", "Seed node IDs"),
			prop("edge_types", "array", "Edge types to traverse"),
			prop("direction", "string", "Traversal direction"),
			prop("depth", "integer", "Traversal depth"),
			prop("budget", "integer", "Traversal budget"),
			prop("per_node_cap", "integer", "Per-node edge cap"),
		)),
		readOnlyDef("repo_index/open", "Open a repo index node by ID.", schemaObject(
			req("id", "string", "Node ID"),
		)),
		readOnlyDef("repo_index/dag_grep", "Search and expand a compact explanation subgraph.", schemaObject(
			req("query", "string", "Search query"),
			prop("mode", "string", "fts, semantic, or hybrid"),
			prop("k", "integer", "Number of seeds"),
			prop("node_kinds", "array", "Optional node kinds"),
			prop("edge_sets", "array", "structural, doc, all"),
			prop("edge_types", "array", "Explicit edge types"),
			prop("direction", "string", "Traversal direction"),
			prop("depth", "integer", "Traversal depth"),
			prop("budget", "integer", "Traversal budget"),
			prop("per_node_cap", "integer", "Per-node edge cap"),
			prop("include_anchors", "boolean", "Include anchors"),
			prop("render", "string", "none, tree, mermaid"),
		)),

		readOnlyDef("context/show", "Read the ContextWiki top-of-mind bundle.", schemaObject()),
		readOnlyDef("context/retrieve", "Blend ContextWiki state with vault retrieval.", schemaObject(
			req("query", "string", "Question or topic"),
			prop("vault_path", "string", "Optional vault path"),
			prop("limit", "integer", "Maximum result count"),
		)),

		readOnlyDef("obsidian/index_search", "Search the local Obsidian vault index.", schemaObject(
			req("query", "string", "Vault search query"),
			prop("vault_path", "string", "Optional vault path"),
			prop("limit", "integer", "Maximum result count"),
			prop("semantic", "boolean", "Use semantic note search"),
		)),
		readOnlyDef("obsidian/read", "Read one Obsidian note.", schemaObject(
			req("path", "string", "Vault note path"),
			prop("vault_path", "string", "Optional vault path"),
		)),
		readOnlyDef("obsidian/related", "List related Obsidian notes.", schemaObject(
			req("path", "string", "Vault note path"),
			prop("vault_path", "string", "Optional vault path"),
			prop("limit", "integer", "Maximum result count"),
		)),

		readOnlyDef("memory/query", "Query canonical memory records for relevant context.", schemaObject(
			req("query", "string", "Search query"),
			prop("kinds", "string", "Optional canonical memory kind filter"),
			prop("lifecycle_states", "string", "Optional lifecycle filter; default returns active plus strongly matching candidate/stale evidence"),
			prop("file", "string", "Optional file filter"),
			prop("limit", "integer", "Maximum result count"),
		)),
		readOnlyDef("session/timeline", "Retrieve a session-oriented semantic timeline.", schemaObject(
			req("query", "string", "Search query"),
			prop("limit", "integer", "Maximum result count"),
		)),

		readOnlyDef("todo/query", "Query tasks in the task database.", schemaObject(
			prop("status", "string", "Task status"),
			prop("tags", "array", "Optional tags"),
			prop("parent_id", "string", "Optional parent task id"),
			prop("limit", "integer", "Maximum result count"),
		)),
		def("todo/add", "Add a new task.", schemaObject(
			req("title", "string", "Task title"),
			prop("description", "string", "Task description"),
			prop("parent_id", "string", "Optional parent task"),
			prop("tags", "array", "Optional tags"),
			prop("depends_on", "array", "Optional dependency ids"),
		)),
		def("todo/complete", "Mark a task complete.", schemaObject(
			req("id", "string", "Task id"),
			prop("summary", "string", "Completion summary"),
		)),
		readOnlyDef("todo/graph_insights", "Read task graph insights.", schemaObject(
			prop("root_id", "string", "Optional root task id"),
			prop("insight_type", "string", "critical_path, blockers, priorities, dependencies, all"),
		)),
		def("todo/set_active", "Set the active task.", schemaObject(
			req("task_id", "string", "Task id"),
		)),
		def("todo/ensure_active", "Ensure an active task exists.", schemaObject(
			req("default_title", "string", "Title for a created task"),
			prop("scope_path", "string", "Optional scope path"),
		)),

		def("agent/spawn", "Spawn a subagent.", schemaObject(
			req("role", "string", "Agent role"),
			req("task", "string", "Detailed task"),
			prop("local_max_depth", "integer", "Optional subtree max depth"),
			prop("llm_provider", "string", "Optional provider override"),
			prop("llm_model", "string", "Optional model override"),
		)),
		readOnlyDef("agent/list", "List active agents.", schemaObject()),
		readOnlyDef("agent/status", "Inspect one agent.", schemaObject(
			req("session_id", "string", "Agent session id"),
		)),
		def("agent/kill", "Terminate an agent.", schemaObject(
			req("session_id", "string", "Agent session id"),
		)),
		readOnlyDef("agent/hierarchy", "Show the agent hierarchy tree.", schemaObject(
			prop("session_id", "string", "Optional root session id"),
		)),
		readOnlyDef("agent/wait", "Wait for child agents.", schemaObject(
			prop("timeout_seconds", "integer", "Timeout in seconds"),
		)),

		def("think", "Record intermediate reasoning.", schemaObject(
			prop("thought", "string", "Reasoning text"),
		)),
	}
}

// ExtensionDefs returns repo-local extension tool definitions that are not part
// of the portable core v2 tool set.
func ExtensionDefs() []coretool.ToolDef {
	return []coretool.ToolDef{
		readOnlyDef("heartwood/state", "Fetch compact Heartwood participant state.", schemaObject(
			prop("heartwood_root", "string", "Path to the Heartwood repo"),
			req("host", "string", "WebSocket host"),
			req("db_name", "string", "Heartwood database name"),
			prop("token", "string", "Optional token"),
			prop("token_path", "string", "Optional token path"),
			prop("wait_timeout_ms", "integer", "Timeout in milliseconds"),
			prop("message_limit", "integer", "Recent message limit"),
		)),
		def("heartwood/action", "Execute a whitelisted Heartwood action.", schemaObject(
			prop("heartwood_root", "string", "Path to the Heartwood repo"),
			req("host", "string", "WebSocket host"),
			req("db_name", "string", "Heartwood database name"),
			prop("token", "string", "Optional token"),
			prop("token_path", "string", "Optional token path"),
			prop("wait_timeout_ms", "integer", "Timeout in milliseconds"),
			req("operation", "string", "Action name"),
			prop("args", "object", "Action arguments"),
		)),
	}
}

// NewDefaultCatalog builds a real non-test v2 catalog from the default tool
// definitions and profile specs. Set includeExtensions to include repo-local
// extension defs such as Heartwood.
func NewDefaultCatalog(specs map[coretool.ProcessProfile]profiles.ProfileSpec, includeExtensions bool) (*Catalog, error) {
	defs := append([]coretool.ToolDef{}, DefaultDefs()...)
	if includeExtensions {
		defs = append(defs, ExtensionDefs()...)
	}
	return NewCatalog(defs, specs)
}

func def(name, description string, params json.RawMessage) coretool.ToolDef {
	return coretool.ToolDef{
		Name:        name,
		Description: description,
		Parameters:  params,
	}
}

func readOnlyDef(name, description string, params json.RawMessage) coretool.ToolDef {
	tool := def(name, description, params)
	tool.Policy.EffectReplay = coretool.EffectReplayReadOnly
	return tool
}

func schemaObject(props ...schemaProp) json.RawMessage {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	properties := schema["properties"].(map[string]any)
	required := make([]string, 0, len(props))
	for _, prop := range props {
		properties[prop.name] = prop.schema
		if prop.required {
			required = append(required, prop.name)
		}
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	body, _ := json.Marshal(schema)
	return body
}

type schemaProp struct {
	name     string
	schema   map[string]any
	required bool
}

func prop(name, typ, description string) schemaProp {
	return schemaProp{
		name: name,
		schema: map[string]any{
			"type":        typ,
			"description": description,
		},
	}
}

func req(name, typ, description string) schemaProp {
	p := prop(name, typ, description)
	p.required = true
	return p
}
