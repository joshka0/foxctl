/**
 * agentctl OpenCode Plugin
 *
 * Main plugin entry point for OpenCode integration.
 *
 * Architecture (based on OpenCode plugin API research):
 * - Tool hooks (tool.execute.before/after) return Promise<void>
 * - They CANNOT inject context - only block via throwing errors
 * - For context injection, use experimental.chat.system.transform
 * - For rich context, create custom tools the AI calls explicitly
 *
 * See HOOK_ANALYSIS.md for full platform comparison.
 */

import type { Plugin } from "@opencode-ai/plugin";
import { tool } from "@opencode-ai/plugin";

const z = tool.schema;
import { runSkill, getWorkspace } from "./lib/agentctl";
import { mkdir, readdir } from "node:fs/promises";
import { join } from "node:path";
import { createHash } from "node:crypto";

const AGENTCTL_HOME = process.env.AGENTCTL_HOME || join(process.env.HOME || "/tmp", ".agentctl");

/**
 * Session identity from agentctl identity file
 * Written by session-identity.sh hook or agentctl session-id command
 */
interface SessionIdentity {
  session_id: string;
  agent_id: string;
  provider: string;
  workspace: string;
  workspace_hash: string;
  started_at: string;
  last_activity: string;
}

/**
 * Get session identity from identity file
 * Falls back to env vars or generates a workspace-based ID
 */
async function getSessionIdentity(workspace: string): Promise<{ sessionID: string; agentID: string }> {
  // 1. Check env vars first
  const envSessionID = process.env.AGENTCTL_SESSION_ID ||
    process.env.OPENCODE_SESSION_ID ||
    process.env.CLAUDE_SESSION_ID;

  if (envSessionID) {
    return {
      sessionID: envSessionID,
      agentID: process.env.AGENTCTL_AGENT_ID || "opencode"
    };
  }

  // 2. Try to read identity file
  const workspaceHash = createHash("sha256").update(workspace).digest("hex").slice(0, 16);
  const identityDir = join(AGENTCTL_HOME, "sessions", "active");

  try {
    const files = await readdir(identityDir);
    const matchingFile = files.find(f => f.startsWith(workspaceHash));

    if (matchingFile) {
      const identity: SessionIdentity = await Bun.file(join(identityDir, matchingFile)).json();
      return {
        sessionID: identity.session_id,
        agentID: identity.agent_id
      };
    }
  } catch {
    // Identity dir doesn't exist or no matching file
  }

  // 3. Generate fallback based on workspace
  return {
    sessionID: `opencode-${workspaceHash}`,
    agentID: "opencode"
  };
}

/**
 * Pending context for hook → system.transform handoff
 *
 * Tool hooks write context to this file, system.transform reads and injects it.
 * This works around the limitation that tool hooks can't return context directly.
 */
interface PendingContext {
  timestamp: number;
  sessionID: string;
  entries: Array<{
    source: string; // e.g., "file-memory-recall", "semantic-search"
    context: string;
  }>;
}

const CONTEXT_DIR = join(AGENTCTL_HOME, "cache", "pending-context");

async function getPendingContextPath(sessionID: string): Promise<string> {
  await mkdir(CONTEXT_DIR, { recursive: true });
  return join(CONTEXT_DIR, `${sessionID}.json`);
}

async function writePendingContext(
  sessionID: string,
  source: string,
  context: string
): Promise<void> {
  const path = await getPendingContextPath(sessionID);
  let pending: PendingContext;

  try {
    pending = await Bun.file(path).json();
    if (Date.now() - pending.timestamp > 30000) {
      pending = { timestamp: Date.now(), sessionID, entries: [] };
    }
  } catch {
    pending = { timestamp: Date.now(), sessionID, entries: [] };
  }

  pending.entries.push({ source, context });
  pending.timestamp = Date.now();
  await Bun.write(path, JSON.stringify(pending));
}

async function readAndClearPendingContext(
  sessionID: string
): Promise<PendingContext["entries"]> {
  const path = await getPendingContextPath(sessionID);

  try {
    const pending: PendingContext = await Bun.file(path).json();

    // Only return if fresh (< 10 seconds old)
    if (Date.now() - pending.timestamp < 10000) {
      // Clear after reading
      await Bun.write(path, JSON.stringify({ timestamp: 0, sessionID, entries: [] }));
      return pending.entries;
    }
  } catch {
    // File doesn't exist or parse error
  }

  return [];
}

export const AgentctlPlugin: Plugin = async ({ client, directory, $ }) => {
  const workspace = directory || getWorkspace();

  return {
    /**
     * Custom tools the AI can call explicitly
     */
    tool: {
      /**
       * Query agentctl memories (gotchas, decisions, patterns)
       */
      "agentctl-memory": tool({
        description:
          "Search agentctl memories for gotchas, decisions, and patterns relevant to a file or topic",
        args: {
          query: z.string().describe("Search query or file path"),
          types: z
            .array(z.enum(["gotcha", "decision", "pattern", "codemap"]))
            .optional()
            .describe("Filter by memory types"),
        },
        async execute({ query, types }) {
          const result = await runSkill<{ memories: unknown[] }>(
            "memory/query",
            {
              query,
              types: types?.join(","),
              limit: 5,
            },
            { workspace, ephemeral: true, timeout: 5000 }
          );

          if (!result.success || !result.data?.memories?.length) {
            return "No relevant memories found.";
          }

          return JSON.stringify(result.data.memories, null, 2);
        },
      }),

      /**
       * Semantic code search across the codebase
       */
      "agentctl-search": tool({
        description:
          "Semantic vector search across the codebase. Use for finding related code, implementations, or patterns.",
        args: {
          query: z.string().describe("Natural language search query"),
          scope: z
            .enum(["symbols", "sessions", "memories", "tasks", "codemaps"])
            .optional()
            .describe("Search scope (default: symbols)"),
          limit: z.number().optional().describe("Max results (default: 10)"),
        },
        async execute({ query, scope = "symbols", limit = 10 }) {
          const result = await runSkill<{ results: unknown[] }>(
            "code/semantic_search",
            { query, scope, limit },
            { workspace, ephemeral: true, timeout: 10000 }
          );

          if (!result.success || !result.data?.results?.length) {
            return "No matches found.";
          }

          return JSON.stringify(result.data.results, null, 2);
        },
      }),

      /**
       * Check overseer inbox for human messages
       */
      "agentctl-inbox": tool({
        description:
          "Check the overseer inbox for messages from humans or other agents",
        args: {
          recipient: z
            .string()
            .optional()
            .describe("Filter by recipient (default: overseer)"),
          unreadOnly: z
            .boolean()
            .optional()
            .describe("Only show unread (default: true)"),
        },
        async execute({ recipient = "overseer", unreadOnly = true }) {
          const result = await runSkill<{
            messages: Array<{
              subject: string;
              body: string;
              priority: number;
            }>;
          }>(
            "mailbox/list",
            { recipient, unread_only: unreadOnly },
            { workspace, ephemeral: true, timeout: 3000 }
          );

          if (!result.success || !result.data?.messages?.length) {
            return "No messages in inbox.";
          }

          return result.data.messages
            .map((m) => `[P${m.priority}] ${m.subject}\n${m.body}`)
            .join("\n\n---\n\n");
        },
      }),

      /**
       * Get file structure and symbols
       */
      "agentctl-symbols": tool({
        description:
          "Get symbols (functions, types, variables) from a file. Useful before reading to understand structure.",
        args: {
          path: z.string().describe("File path to analyze"),
          includePrivate: z
            .boolean()
            .optional()
            .describe("Include private symbols (default: false)"),
        },
        async execute({ path, includePrivate = false }) {
          const result = await runSkill<{
            preview: Array<{ name: string; type: string; line: number }>;
          }>(
            "code/symbols",
            { path, include_private: includePrivate, max_results: 50 },
            { workspace, ephemeral: true, timeout: 5000 }
          );

          if (!result.success || !result.data?.preview?.length) {
            return "No symbols found.";
          }

          const byType: Record<string, string[]> = {};
          for (const sym of result.data.preview) {
            const key = sym.type || "other";
            if (!byType[key]) byType[key] = [];
            byType[key].push(`L${sym.line}: ${sym.name}`);
          }

          return Object.entries(byType)
            .map(([type, symbols]) => `## ${type}\n${symbols.join("\n")}`)
            .join("\n\n");
        },
      }),

      /**
       * Get active task and its context
       */
      "agentctl-task": tool({
        description: "Get the currently active task and its context",
        args: {},
        async execute() {
          const result = await runSkill<{
            task?: {
              id: string;
              title: string;
              description?: string;
              status: string;
            };
          }>("task/active", {}, { workspace, ephemeral: true, timeout: 3000 });

          if (!result.success || !result.data?.task) {
            return "No active task. Use `agentctl todo add` to create one.";
          }

          const t = result.data.task;
          return `**Active Task**: ${t.title}\nID: ${t.id}\nStatus: ${t.status}${t.description ? `\n\n${t.description}` : ""}`;
        },
      }),
    },

    /**
     * Experimental: Inject context into system prompt
     *
     * This is the ONLY way to inject context that the AI sees.
     * Runs before every AI turn.
     *
     * Reads pending context written by tool.execute.before hooks.
     */
    "experimental.chat.system.transform": async (
      input: { sessionID?: string },
      output: { system: string[] }
    ): Promise<void> => {
      const context: string[] = [];

      const { sessionID } = input.sessionID
        ? { sessionID: input.sessionID }
        : await getSessionIdentity(workspace);

      try {
        // 1. Read pending context from tool hooks (file-based handoff)
        const pendingEntries = await readAndClearPendingContext(sessionID);
        for (const entry of pendingEntries) {
          context.push(`**${entry.source}**:\n${entry.context}`);
        }

        // 2. Get active task (lightweight check)
        const taskResult = await runSkill<{
          task?: { title: string; id: string };
        }>("task/active", {}, { workspace, ephemeral: true, timeout: 2000 });

        if (taskResult.success && taskResult.data?.task) {
          context.push(
            `**Active Task**: ${taskResult.data.task.title} (${taskResult.data.task.id})`
          );
        }

        // 3. Check for urgent overseer messages (priority >= 2)
        const msgResult = await runSkill<{
          messages: Array<{
            subject: string;
            body: string;
            priority: number;
          }>;
        }>(
          "mailbox/list",
          { recipient: "overseer", unread_only: true, min_priority: 2 },
          { workspace, ephemeral: true, timeout: 2000 }
        );

        if (msgResult.success && msgResult.data?.messages?.length) {
          const urgent = msgResult.data.messages
            .map((m) => `- **${m.subject}**: ${m.body}`)
            .join("\n");
          context.push(`**Urgent Messages**:\n${urgent}`);
        }
      } catch (err) {
        // Silently fail - don't break the AI interaction
        console.error("[agentctl] System transform error:", err);
      }

      // Inject context into system prompt
      if (context.length > 0) {
        output.system.push(`\n\n---\n## agentctl Context\n${context.join("\n\n")}`);
      }
    },

    /**
     * Tool execution hooks - writes context to temp file for system.transform
     *
     * Pattern: hook writes to file → system.transform reads and injects
     * This works around the limitation that tool hooks return void.
     */
    "tool.execute.before": async (
      input: { tool: string; sessionID: string; callID: string },
      output: { args: Record<string, unknown> }
    ): Promise<void> => {
      const sessionID = input.sessionID;
      const args = output.args;

      if (input.tool === "Edit" || input.tool === "Write") {
        const filePath = (args.file_path || args.path) as string;
        if (filePath) {
          const memResult = await runSkill<{
            memories: Array<{ name: string; summary: string; type: string }>;
          }>(
            "memory/query",
            { file: filePath, types: "gotcha,decision", limit: 5 },
            { workspace, ephemeral: true, timeout: 3000 }
          );

          if (memResult.success && memResult.data?.memories?.length) {
            const formatted = memResult.data.memories
              .map((m) => `- **${m.name}** (${m.type}): ${m.summary}`)
              .join("\n");
            await writePendingContext(
              sessionID,
              `File Memories: ${filePath.split("/").pop()}`,
              formatted
            );
          }
        }
      }

      // Semantic search augmentation for Grep/Glob
      if (input.tool === "Grep" || input.tool === "Glob") {
        const pattern = (args.pattern || args.query || "") as string;
        if (pattern.length > 3) {
          const searchResult = await runSkill<{
            results: Array<{ file: string; symbol: string; score: number }>;
          }>(
            "code/semantic_search",
            { query: pattern, scope: "symbols", limit: 5 },
            { workspace, ephemeral: true, timeout: 5000 }
          );

          if (searchResult.success && searchResult.data?.results?.length) {
            const formatted = searchResult.data.results
              .map((r) => `- ${r.file}: ${r.symbol} (${(r.score * 100).toFixed(0)}%)`)
              .join("\n");
            await writePendingContext(
              sessionID,
              "Semantic Search Results",
              formatted
            );
          }
        }
      }

      // Bash execution context - inject pwd@date for every bash command
      if (input.tool === "Bash") {
        const command = (args.command || "") as string;
        const workdir = (args.workdir || workspace) as string;
        const now = new Date().toISOString();
        await writePendingContext(
          sessionID,
          "Bash Execution",
          `\`\`\`\n...running from ${workdir}@${now}\n$ ${command.slice(0, 200)}${command.length > 200 ? "..." : ""}\n\`\`\``
        );
      }

      // Overseer inbox check for Read/Bash/Task
      if (["Read", "Bash", "Task", "Grep", "Glob"].includes(input.tool)) {
        const inboxResult = await runSkill<{
          messages: Array<{ subject: string; body: string; priority: number }>;
        }>(
          "mailbox/list",
          { recipient: "overseer", unread_only: true },
          { workspace, ephemeral: true, timeout: 2000 }
        );

        if (inboxResult.success && inboxResult.data?.messages?.length) {
          const formatted = inboxResult.data.messages
            .map((m) => `[P${m.priority}] **${m.subject}**: ${m.body}`)
            .join("\n");
          await writePendingContext(sessionID, "Overseer Messages", formatted);
        }
      }

      // Task guard - block edits without active task
      if (
        process.env.AGENTCTL_TASK_GUARD_MODE === "strict" &&
        (input.tool === "Edit" || input.tool === "Write")
      ) {
        const taskResult = await runSkill<{ task?: unknown }>(
          "task/active",
          {},
          { workspace, ephemeral: true, timeout: 2000 }
        );

        if (!taskResult.success || !taskResult.data?.task) {
          throw new Error(
            "No active task. Create one with `agentctl todo add --title '...'` before editing files."
          );
        }
      }

      // Security scanner - block secrets in writes
      if (input.tool === "Write" || input.tool === "Edit") {
        const content = (args.content ||
          args.new_string ||
          "") as string;
        const secretPatterns = [
          /AKIA[0-9A-Z]{16}/i, // AWS Access Key
          /-----BEGIN (RSA |EC |DSA )?PRIVATE KEY-----/i,
          /ghp_[a-zA-Z0-9]{36}/, // GitHub PAT
          /sk-[a-zA-Z0-9]{48}/, // OpenAI API key
        ];

        for (const pattern of secretPatterns) {
          if (pattern.test(content)) {
            throw new Error(
              `Potential secret detected in content. Please redact before writing.`
            );
          }
        }
      }
    },

    /**
     * After tool execution - side effects only
     * Note: New API no longer provides args in .after hook - use metadata if available
     */
    "tool.execute.after": async (
      input: { tool: string; sessionID: string; callID: string },
      output: { title: string; output: string; metadata: Record<string, unknown> }
    ): Promise<void> => {
      // Queue file for indexing after edit
      // Extract file path from metadata (if available) or parse from output
      if (input.tool === "Edit" || input.tool === "Write") {
        const filePath = (output.metadata?.file || output.metadata?.file_path || output.metadata?.path) as string | undefined;
        if (filePath) {
          runSkill(
            "index/file",
            { path: filePath },
            { workspace, ephemeral: true, timeout: 5000 }
          ).catch(() => {});
        }
      }
    },

    /**
     * Session events
     */
    event: async ({ event }: { event: { type: string; properties?: Record<string, unknown> } }): Promise<void> => {
      switch (event.type) {
        case "session.created": {
          // Warm up daemon
          runSkill(
            "daemon/warmup",
            { workspace_root: workspace },
            { workspace, ephemeral: true, timeout: 3000 }
          ).catch(() => {});
          break;
        }

        case "session.idle": {
          const sessionID = (event.properties?.sessionID as string) || "unknown";

          // Capture session state
          runSkill(
            "session/capture",
            { session_id: sessionID, workspace_root: workspace, status: "idle" },
            { workspace, ephemeral: true, timeout: 15000 }
          ).catch((err) => console.error("[agentctl] Capture error:", err));

          // Flush embedding queue
          runSkill(
            "embedding/flush",
            { workspace_root: workspace },
            { workspace, ephemeral: true, timeout: 120000 }
          ).catch((err) => console.error("[agentctl] Flush error:", err));

          // Sync plans to tasks
          runSkill(
            "plan/sync",
            { session_id: sessionID, workspace_root: workspace },
            { workspace, ephemeral: true, timeout: 5000 }
          ).catch((err) => console.error("[agentctl] Plan sync error:", err));
          break;
        }

        case "file.edited": {
          const filePath = event.properties?.path as string;
          if (filePath) {
            // Link to active task
            runSkill(
              "task/link_file",
              { path: filePath },
              { workspace, ephemeral: true, timeout: 3000 }
            ).catch(() => {});
          }
          break;
        }
      }
    },

    /**
     * Session compacting - save state before context is summarized
     */
    "experimental.session.compacting": async (
      input: { sessionID: string },
      output: { context: string[]; prompt?: string }
    ): Promise<void> => {
      const result = await runSkill<{ summary?: string }>(
        "session/save",
        {
          session_id: input.sessionID,
          workspace_root: workspace,
        },
        { workspace, ephemeral: true, timeout: 5000 }
      );

      if (result.success && result.data?.summary) {
        output.context.push(result.data.summary);
      }
    },
  };
};

export default AgentctlPlugin;
