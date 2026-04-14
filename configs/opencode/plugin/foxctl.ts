import { type Plugin, tool } from "@opencode-ai/plugin";

// Streamlined foxctl plugin - matches MCP skill groups
// Groups: code-intel, code-write, project

export const AgentctlPlugin: Plugin = async ({ $, directory }) => {
  const runSkill = async (skill: string, input: object): Promise<string> => {
    const inputJson = JSON.stringify(input);
    try {
      const result = await $`foxctl run ${skill} --input ${inputJson}`.text();
      return result;
    } catch (error: any) {
      return JSON.stringify({
        error: error.message || "foxctl skill failed",
      });
    }
  };

  return {
    tool: {
      // === code-intel group ===

      agentctl_semantic_search: tool({
        description: "Vector-based semantic code search across symbols, memories, sessions, codemaps",
        args: {
          query: tool.schema.string().describe("Natural language query"),
          scopes: tool.schema.array(tool.schema.string()).optional().describe("Scopes: symbols, memories, tasks, sessions, codemaps"),
          limit: tool.schema.number().optional().describe("Max results (default: 10)"),
        },
        async execute(args) {
          return runSkill("code/semantic_search", {
            query: args.query,
            scopes: args.scopes,
            limit: args.limit || 10,
          });
        },
      }),

      agentctl_smart_search: tool({
        description: "Smart code search: auto-generates candidates from indexes and extracts relevant snippets",
        args: {
          query: tool.schema.string().describe("Natural language question about the code"),
          glob: tool.schema.string().optional().describe("File pattern filter"),
          limits: tool.schema.object({
            max_candidates: tool.schema.number().optional(),
            max_snippets: tool.schema.number().optional(),
          }).optional(),
        },
        async execute(args) {
          return runSkill("code/smart_search", {
            query: args.query,
            glob: args.glob,
            limits: args.limits,
          });
        },
      }),

      agentctl_symbols: tool({
        description: "Extract code symbols: functions, types, interfaces, structs, methods",
        args: {
          path: tool.schema.string().describe("Path to analyze"),
          symbol_types: tool.schema.array(tool.schema.string()).optional(),
        },
        async execute(args) {
          return runSkill("code/symbols", {
            path: args.path,
            symbol_types: args.symbol_types,
          });
        },
      }),

      agentctl_snippet_extract: tool({
        description: "Extract code snippets from known candidate files based on a question",
        args: {
          query: tool.schema.string().describe("Natural language question"),
          candidates: tool.schema.array(tool.schema.object({
            path: tool.schema.string(),
            line: tool.schema.number().optional(),
            priority: tool.schema.number().optional(),
          })).describe("Candidate files to extract from"),
          mode: tool.schema.enum(["snippets", "masked", "flow"]).optional(),
        },
        async execute(args) {
          return runSkill("code/snippet_extract", {
            query: args.query,
            candidates: args.candidates,
            mode: args.mode || "snippets",
          });
        },
      }),

      agentctl_context_grep: tool({
        description: "Pattern search returning full function/method bodies containing matches",
        args: {
          pattern: tool.schema.string().describe("Regex pattern to search"),
          path: tool.schema.string().optional().describe("Path to search (default: .)"),
          glob: tool.schema.string().optional().describe("File pattern filter"),
          max_blocks: tool.schema.number().optional(),
        },
        async execute(args) {
          return runSkill("code/context_grep", {
            pattern: args.pattern,
            path: args.path || ".",
            glob: args.glob,
            max_blocks: args.max_blocks,
          });
        },
      }),

      agentctl_codemap_get: tool({
        description: "Retrieve a stored codemap by ID",
        args: {
          id: tool.schema.string().describe("Codemap ID"),
        },
        async execute(args) {
          return runSkill("codemap/get", { id: args.id });
        },
      }),

      agentctl_codemap_generate: tool({
        description: "Generate AI-powered code trace map for a query",
        args: {
          query: tool.schema.string().describe("What to trace (e.g., 'authentication flow')"),
          workspace: tool.schema.string().optional(),
        },
        async execute(args) {
          return runSkill("codemap/generate", {
            query: args.query,
            workspace: args.workspace,
          });
        },
      }),

      // === code-write group ===

      agentctl_apply_edit: tool({
        description: "Apply search/replace edits to files with dry-run preview",
        args: {
          path: tool.schema.string().describe("File path"),
          edits: tool.schema.array(tool.schema.object({
            search: tool.schema.string().describe("Text to find (exact match)"),
            replace: tool.schema.string().describe("Replacement text"),
          })).describe("List of search/replace pairs"),
          dry_run: tool.schema.boolean().optional().describe("Preview changes without applying"),
        },
        async execute(args) {
          return runSkill("fs/apply_edit", {
            path: args.path,
            edits: args.edits,
            dry_run: args.dry_run,
          });
        },
      }),

      agentctl_smart_write: tool({
        description: "Symbol-based code editing with diff preview",
        args: {
          path: tool.schema.string().describe("File path"),
          symbol: tool.schema.string().optional().describe("Symbol to edit"),
          content: tool.schema.string().describe("New content"),
          dry_run: tool.schema.boolean().optional().describe("Preview only"),
        },
        async execute(args) {
          return runSkill("code/smart_write", {
            path: args.path,
            symbol: args.symbol,
            content: args.content,
            dry_run: args.dry_run,
          });
        },
      }),

      // === fs group ===

      agentctl_read: tool({
        description: "Read file contents with optional line range",
        args: {
          path: tool.schema.string().describe("File path"),
          offset: tool.schema.number().optional().describe("Start line (0-indexed)"),
          limit: tool.schema.number().optional().describe("Max lines to read"),
        },
        async execute(args) {
          return runSkill("fs/read", {
            path: args.path,
            offset: args.offset,
            limit: args.limit,
          });
        },
      }),

      // === project group ===

      agentctl_todo: tool({
        description: "Manage tasks with dependencies and active task tracking",
        args: {
          operation: tool.schema.enum(["add", "list", "complete", "delete", "update", "set_active"]),
          id: tool.schema.string().optional(),
          title: tool.schema.string().optional(),
          description: tool.schema.string().optional(),
          priority: tool.schema.enum(["low", "medium", "high"]).optional(),
          depends_on: tool.schema.array(tool.schema.string()).optional(),
        },
        async execute(args) {
          return runSkill("todo/manage", args);
        },
      }),

      agentctl_memory_query: tool({
        description: "Query memories: gotchas, decisions, patterns, insights",
        args: {
          query: tool.schema.string().describe("Search query"),
          types: tool.schema.array(tool.schema.string()).optional().describe("Memory types to search"),
          limit: tool.schema.number().optional(),
        },
        async execute(args) {
          return runSkill("memory/query", {
            query: args.query,
            types: args.types,
            limit: args.limit || 10,
          });
        },
      }),

      agentctl_session_recall: tool({
        description: "Recall context from previous sessions",
        args: {
          query: tool.schema.string().optional().describe("Search query"),
          session_id: tool.schema.string().optional().describe("Specific session ID"),
        },
        async execute(args) {
          return runSkill("session/recall", {
            query: args.query,
            session_id: args.session_id,
          });
        },
      }),
    },
  };
};

export default AgentctlPlugin;
