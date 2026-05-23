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
			req("path", JSONSchemaTypeString, "Path to the file"),
			prop("max_bytes", JSONSchemaTypeInteger, "Maximum bytes to read"),
		)),
		readOnlyDef("fs/list_dir", "List directory contents from the workspace.", schemaObject(
			req("path", JSONSchemaTypeString, "Path to the directory"),
			prop("depth", JSONSchemaTypeInteger, "Maximum recursion depth"),
		)),
		def("fs/write_file", "Write a file in the workspace.", schemaObject(
			req("path", JSONSchemaTypeString, "Path to the file"),
			req("content", JSONSchemaTypeString, "File content"),
		)),

		readOnlyDef("code/search", "Search code with ripgrep-backed recall.", schemaObject(
			req("pattern", JSONSchemaTypeString, "Search pattern"),
			prop("path", JSONSchemaTypeString, "Optional subpath"),
			prop("file_pattern", JSONSchemaTypeString, "Optional glob"),
			prop("context_lines", JSONSchemaTypeInteger, "Context lines"),
			prop("max_results", JSONSchemaTypeInteger, "Maximum result count"),
		)),
		readOnlyDef("code/symbols", "Inspect symbols in a file.", schemaObject(
			req("path", JSONSchemaTypeString, "Path to inspect"),
			prop("symbol_type", JSONSchemaTypeString, "Optional symbol kind filter"),
			prop("include_docs", JSONSchemaTypeBoolean, "Include symbol docs"),
		)),
		readOnlyDef("context/search", "Semantic tree/context search over the repo.", schemaObject(
			req("query", JSONSchemaTypeString, "Question or search topic"),
			prop("limit", JSONSchemaTypeInteger, "Maximum result count"),
		)),
		readOnlyDef("smart/search", "Search and extract code evidence in one call.", schemaObject(
			req("question", JSONSchemaTypeString, "Question to investigate"),
			prop("limits", JSONSchemaTypeObject, "Optional search limits"),
		)),
		readOnlyDef("context/grep", "Search with function-body context.", schemaObject(
			req("pattern", JSONSchemaTypeString, "Pattern to search"),
			prop("path", JSONSchemaTypeString, "Optional subpath"),
		)),
		def("context/filter", "Filter chunks for the most relevant context.", schemaObject(
			req("prompt", JSONSchemaTypeString, "Prompt to optimize for"),
			req("source", JSONSchemaTypeObject, "Source text or chunks"),
			prop("budget", JSONSchemaTypeObject, "Optional budget config"),
			prop("llm", JSONSchemaTypeObject, "Optional LLM override"),
		)),

		readOnlyDef("repo_index/search", "Search repo index nodes.", schemaObject(
			req("query", JSONSchemaTypeString, "FTS query string"),
			prop("limit", JSONSchemaTypeInteger, "Maximum result count"),
		)),
		readOnlyDef("repo_index/expand", "Expand the repo index graph.", schemaObject(
			req("seeds", JSONSchemaTypeArray, "Seed node IDs"),
			prop("edge_types", JSONSchemaTypeArray, "Edge types to traverse"),
			prop("direction", JSONSchemaTypeString, "Traversal direction"),
			prop("depth", JSONSchemaTypeInteger, "Traversal depth"),
			prop("budget", JSONSchemaTypeInteger, "Traversal budget"),
			prop("per_node_cap", JSONSchemaTypeInteger, "Per-node edge cap"),
		)),
		readOnlyDef("repo_index/open", "Open a repo index node by ID.", schemaObject(
			req("id", JSONSchemaTypeString, "Node ID"),
		)),
		readOnlyDef("repo_index/dag_grep", "Search and expand a compact explanation subgraph.", schemaObject(
			req("query", JSONSchemaTypeString, "Search query"),
			prop("mode", JSONSchemaTypeString, "fts, semantic, or hybrid"),
			prop("k", JSONSchemaTypeInteger, "Number of seeds"),
			prop("node_kinds", JSONSchemaTypeArray, "Optional node kinds"),
			prop("edge_sets", JSONSchemaTypeArray, "structural, doc, all"),
			prop("edge_types", JSONSchemaTypeArray, "Explicit edge types"),
			prop("direction", JSONSchemaTypeString, "Traversal direction"),
			prop("depth", JSONSchemaTypeInteger, "Traversal depth"),
			prop("budget", JSONSchemaTypeInteger, "Traversal budget"),
			prop("per_node_cap", JSONSchemaTypeInteger, "Per-node edge cap"),
			prop("include_anchors", JSONSchemaTypeBoolean, "Include anchors"),
			prop("render", JSONSchemaTypeString, "none, tree, mermaid"),
		)),

		readOnlyDef("context/show", "Read the ContextWiki top-of-mind bundle.", schemaObject()),
		readOnlyDef("context/retrieve", "Blend ContextWiki state with vault retrieval.", schemaObject(
			req("query", JSONSchemaTypeString, "Question or topic"),
			prop("vault_path", JSONSchemaTypeString, "Optional vault path"),
			prop("limit", JSONSchemaTypeInteger, "Maximum result count"),
		)),

		readOnlyDef("obsidian/index_search", "Search the local Obsidian vault index.", schemaObject(
			req("query", JSONSchemaTypeString, "Vault search query"),
			prop("vault_path", JSONSchemaTypeString, "Optional vault path"),
			prop("limit", JSONSchemaTypeInteger, "Maximum result count"),
			prop("semantic", JSONSchemaTypeBoolean, "Use semantic note search"),
		)),
		readOnlyDef("obsidian/read", "Read one Obsidian note.", schemaObject(
			req("path", JSONSchemaTypeString, "Vault note path"),
			prop("vault_path", JSONSchemaTypeString, "Optional vault path"),
		)),
		readOnlyDef("obsidian/related", "List related Obsidian notes.", schemaObject(
			req("path", JSONSchemaTypeString, "Vault note path"),
			prop("vault_path", JSONSchemaTypeString, "Optional vault path"),
			prop("limit", JSONSchemaTypeInteger, "Maximum result count"),
		)),

		readOnlyDef("memory/query", "Query canonical memory records for relevant context.", schemaObject(
			req("query", JSONSchemaTypeString, "Search query"),
			prop("kinds", JSONSchemaTypeString, "Optional canonical memory kind filter"),
			prop("lifecycle_states", JSONSchemaTypeString, "Optional lifecycle filter; default returns active plus strongly matching candidate/stale evidence"),
			prop("file", JSONSchemaTypeString, "Optional file filter"),
			prop("limit", JSONSchemaTypeInteger, "Maximum result count"),
		)),
		readOnlyDef("session/timeline", "Retrieve a session-oriented semantic timeline.", schemaObject(
			req("query", JSONSchemaTypeString, "Search query"),
			prop("limit", JSONSchemaTypeInteger, "Maximum result count"),
		)),

		readOnlyDef("todo/query", "Query tasks in the task database.", schemaObject(
			prop("status", JSONSchemaTypeString, "Task status"),
			prop("tags", JSONSchemaTypeArray, "Optional tags"),
			prop("parent_id", JSONSchemaTypeString, "Optional parent task id"),
			prop("limit", JSONSchemaTypeInteger, "Maximum result count"),
		)),
		def("todo/add", "Add a new task.", schemaObject(
			req("title", JSONSchemaTypeString, "Task title"),
			prop("description", JSONSchemaTypeString, "Task description"),
			prop("parent_id", JSONSchemaTypeString, "Optional parent task"),
			prop("tags", JSONSchemaTypeArray, "Optional tags"),
			prop("depends_on", JSONSchemaTypeArray, "Optional dependency ids"),
		)),
		def("todo/complete", "Mark a task complete.", schemaObject(
			req("id", JSONSchemaTypeString, "Task id"),
			prop("summary", JSONSchemaTypeString, "Completion summary"),
		)),
		readOnlyDef("todo/graph_insights", "Read task graph insights.", schemaObject(
			prop("root_id", JSONSchemaTypeString, "Optional root task id"),
			prop("insight_type", JSONSchemaTypeString, "critical_path, blockers, priorities, dependencies, all"),
		)),
		def("todo/set_active", "Set the active task.", schemaObject(
			req("task_id", JSONSchemaTypeString, "Task id"),
		)),
		def("todo/ensure_active", "Ensure an active task exists.", schemaObject(
			req("default_title", JSONSchemaTypeString, "Title for a created task"),
			prop("scope_path", JSONSchemaTypeString, "Optional scope path"),
		)),

		def("agent/spawn", "Spawn a subagent.", schemaObject(
			req("role", JSONSchemaTypeString, "Agent role"),
			req("task", JSONSchemaTypeString, "Detailed task"),
			prop("local_max_depth", JSONSchemaTypeInteger, "Optional subtree max depth"),
			prop("llm_provider", JSONSchemaTypeString, "Optional provider override"),
			prop("llm_model", JSONSchemaTypeString, "Optional model override"),
		)),
		readOnlyDef("agent/list", "List active agents.", schemaObject()),
		readOnlyDef("agent/status", "Inspect one agent.", schemaObject(
			req("session_id", JSONSchemaTypeString, "Agent session id"),
		)),
		def("agent/kill", "Terminate an agent.", schemaObject(
			req("session_id", JSONSchemaTypeString, "Agent session id"),
		)),
		readOnlyDef("agent/hierarchy", "Show the agent hierarchy tree.", schemaObject(
			prop("session_id", JSONSchemaTypeString, "Optional root session id"),
		)),
		readOnlyDef("agent/wait", "Wait for child agents.", schemaObject(
			prop("timeout_seconds", JSONSchemaTypeInteger, "Timeout in seconds"),
		)),

		def("think", "Record intermediate reasoning.", schemaObject(
			prop("thought", JSONSchemaTypeString, "Reasoning text"),
		)),
	}
}

// ExtensionDefs returns repo-local extension tool definitions that are not part
// of the portable core v2 tool set.
func ExtensionDefs() []coretool.ToolDef {
	return []coretool.ToolDef{
		readOnlyDef("heartwood/state", "Fetch compact Heartwood participant state.", schemaObject(
			prop("heartwood_root", JSONSchemaTypeString, "Path to the Heartwood repo"),
			req("host", JSONSchemaTypeString, "WebSocket host"),
			req("db_name", JSONSchemaTypeString, "Heartwood database name"),
			prop("token", JSONSchemaTypeString, "Optional token"),
			prop("token_path", JSONSchemaTypeString, "Optional token path"),
			prop("wait_timeout_ms", JSONSchemaTypeInteger, "Timeout in milliseconds"),
			prop("message_limit", JSONSchemaTypeInteger, "Recent message limit"),
		)),
		def("heartwood/action", "Execute a whitelisted Heartwood action.", schemaObject(
			prop("heartwood_root", JSONSchemaTypeString, "Path to the Heartwood repo"),
			req("host", JSONSchemaTypeString, "WebSocket host"),
			req("db_name", JSONSchemaTypeString, "Heartwood database name"),
			prop("token", JSONSchemaTypeString, "Optional token"),
			prop("token_path", JSONSchemaTypeString, "Optional token path"),
			prop("wait_timeout_ms", JSONSchemaTypeInteger, "Timeout in milliseconds"),
			req("operation", JSONSchemaTypeString, "Action name"),
			prop("args", JSONSchemaTypeObject, "Action arguments"),
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
	schema := JSONSchema{
		Type:       JSONSchemaTypeObject,
		Properties: map[string]JSONSchemaField{},
	}
	required := make([]string, 0, len(props))
	for _, prop := range props {
		schema.Properties[prop.name] = prop.schema
		if prop.required {
			required = append(required, prop.name)
		}
	}
	if len(required) > 0 {
		schema.Required = required
	}
	body, _ := json.Marshal(schema)
	return body
}

type schemaProp struct {
	name     string
	schema   JSONSchemaField
	required bool
}

func prop(name string, typ JSONSchemaType, description string) schemaProp {
	return schemaProp{
		name: name,
		schema: JSONSchemaField{
			Type:        typ,
			Description: description,
		},
	}
}

func req(name string, typ JSONSchemaType, description string) schemaProp {
	p := prop(name, typ, description)
	p.required = true
	return p
}
