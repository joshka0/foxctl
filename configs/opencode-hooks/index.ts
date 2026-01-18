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
import { mkdir, readdir, rm } from "node:fs/promises";
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
 * Context Buffer Integration
 *
 * Replaces the file-based pending context approach with agentctl's Context Buffer.
 * - writePendingContext -> hooks/context_enqueue skill
 * - readAndClearPendingContext -> hooks/context_drain skill
 *
 * Benefits:
 * - TTL enforcement (auto-expires)
 * - Deduplication (same source+text won't accumulate)
 * - Priority ordering (high priority surfaces first)
 * - Multi-agent safety (scoped by workspace/session/agent)
 */

const MODE_DIR = join(AGENTCTL_HOME, "cache", "session-modes");

const DEFAULT_PENDING_CONTEXT_TTL_SECONDS = 60;

/**
 * Enqueue context to the Context Buffer for later injection.
 * Replaces the file-based writePendingContext approach.
 */
async function writePendingContext(
  sessionID: string,
  source: string,
  context: string,
  ttlMs = DEFAULT_PENDING_CONTEXT_TTL_SECONDS * 1000
): Promise<void> {
  const workspace = getWorkspace(process.cwd());
  const ttlSeconds = Math.ceil(ttlMs / 1000);

  await runSkill(
    "hooks/context_enqueue",
    {
      workspace_id: workspace,
      session_id: sessionID,
      source,
      text: context,
      priority: 2, // Normal priority
      ttl_seconds: ttlSeconds,
      dedupe: true, // Skip if same source+text exists
    },
    { workspace, ephemeral: true, timeout: 3000 }
  ).catch((err) => {
    // Non-critical - log but don't fail
    console.error("[agentctl] Context enqueue error:", err);
  });
}

/**
 * Drain context from the Context Buffer for injection.
 * Replaces the file-based readAndClearPendingContext approach.
 */
async function readAndClearPendingContext(
  sessionID: string
): Promise<Array<{ source: string; context: string }>> {
  const workspace = getWorkspace(process.cwd());

  const result = await runSkill<{
    markdown?: string;
    entries?: Array<{ source: string; text: string }>;
    count: number;
  }>(
    "hooks/context_drain",
    {
      workspace_id: workspace,
      session_id: sessionID,
      format: "json", // Get structured entries
      limit: 50,
    },
    { workspace, ephemeral: true, timeout: 3000 }
  ).catch((err) => {
    console.error("[agentctl] Context drain error:", err);
    return { success: false, data: undefined };
  });

  if (!result.success || !result.data?.entries?.length) {
    return [];
  }

  // Transform to expected format
  return result.data.entries.map((e) => ({
    source: e.source,
    context: e.text,
  }));
}

const ANCHOR_TRIGGER = /(^|\b)(anchor this|anchor it|anchor prompt|@anchor|\/anchor)(\b|$)/i;
const TODO_TRIGGER = /(^|\b)(\/todo)(\b|$)/i;
const COUNSEL_TRIGGER = /(^|\s)\/counsel(\s|$)/i;
const CONTEXT_TRIGGER = /(^|\s)\/context(\s|$)/i;
const STRICT_TRIGGER = /^\s*@(strict|agentctl)\s*(on|off|enable|disable|status|1|0|true|false)?\s*$/i;

// Agentctl mode state per workspace (in-memory, synced with SQLite via skill)
const agentctlModeByWorkspace = new Map<string, boolean>();

/**
 * Check if agentctl mode is enabled for workspace
 */
async function isAgentctlModeEnabled(workspace: string): Promise<boolean> {
  // Check in-memory first
  if (agentctlModeByWorkspace.has(workspace)) {
    return agentctlModeByWorkspace.get(workspace) || false;
  }

  // Load from persistent storage via skill
  const result = await runSkill<{ enabled?: boolean }>(
    "setup/agentctl_mode",
    { operation: "get", workspace_id: workspace },
    { workspace, ephemeral: true, timeout: 2000 }
  ).catch(() => ({ success: false, data: undefined }));

  const enabled = result.success && result.data?.enabled === true;
  agentctlModeByWorkspace.set(workspace, enabled);
  return enabled;
}

/**
 * Set agentctl mode for workspace
 */
async function setAgentctlMode(workspace: string, enabled: boolean): Promise<boolean> {
  const result = await runSkill(
    "setup/agentctl_mode",
    { operation: "set", workspace_id: workspace, enabled },
    { workspace, ephemeral: true, timeout: 2000 }
  ).catch(() => ({ success: false }));

  if (result.success) {
    agentctlModeByWorkspace.set(workspace, enabled);
  }
  return result.success;
}

// Skill advisor patterns - suggest relevant agentctl skills based on prompt
const SKILL_PATTERNS: Array<{ pattern: RegExp; hint: string }> = [
  // CI/PR patterns
  {
    pattern: /(pr\s+comment|review\s+comment|reviewer|what\s+did.*say|feedback\s+on\s+pr)/i,
    hint: "**Skill hint:** Check PR comments:\n```bash\nagentctl run ci/prcomments --input '{\"pr\": <number>}'\n```",
  },
  {
    pattern: /(ci\s+status|build\s+status|check.*fail|pipeline|workflow.*fail|github.*action)/i,
    hint: "**Skill hint:** Check CI status:\n```bash\nagentctl run ci/checks --input '{\"pr\": <number>}'\n```",
  },
  // Search patterns
  {
    pattern: /(semantic.*search|search.*semantic|vector.*search)/i,
    hint: "**Skill hint:** Semantic code search:\n```bash\nagentctl run code/semantic_search --input '{\"query\": \"<your query>\"}'\n```",
  },
  {
    pattern: /(investigate|dig\s+into|explore.*code|understand.*how|figure\s+out)/i,
    hint: "**Skill hint:** Code investigation:\n```bash\nagentctl run code/smart_search --input '{\"query\": \"<pattern>\"}'\n```",
  },
  // Code analysis patterns
  {
    pattern: /(complexity|how\s+complex|cyclomatic|cognitive)/i,
    hint: "**Skill hint:** Code complexity analysis:\n```bash\nagentctl run code/complexity --input '{\"path\": \"<file_or_dir>\"}'\n```",
  },
  {
    pattern: /(security|vulnerabilities|secrets|audit)/i,
    hint: "**Skill hint:** Security scan:\n```bash\nagentctl run code/security --input '{\"path\": \".\"}'\n```",
  },
  // Session patterns
  {
    pattern: /(past\s+session|session\s+history|what\s+did\s+we\s+discuss|previous.*session)/i,
    hint: "**Skill hint:** Session recall:\n```bash\nagentctl run session/recall --input '{\"query\": \"<topic>\"}'\n```",
  },
  // Memory patterns
  {
    pattern: /(gotcha|learned|remember.*that|don't\s+forget)/i,
    hint: "**Skill hint:** Save a memory:\n```bash\nagentctl memory put --name \"gotcha-<topic>\" --type gotcha --summary \"<learning>\"\n```",
  },
];

// Track file reads per session for counsel suggestion
const fileReadsPerSession = new Map<string, Set<string>>();
const COUNSEL_SUGGESTION_THRESHOLD = 3;

function extractAfterCommand(text: string, command: string): string {
  const pattern = new RegExp(`.*\\/${command}\\s+`, "i");
  return text.replace(pattern, "").trim();
}

function stripAnchorTrigger(text: string): string {
  const trimmed = text.trim();
  const cleaned = trimmed
    .replace(ANCHOR_TRIGGER, " ")
    .replace(/^[:\-\s]+/, "")
    .trim();
  return cleaned.length > 0 ? cleaned : trimmed;
}

function stripTodoTrigger(text: string): string {
  const trimmed = text.trim();
  const cleaned = trimmed
    .replace(TODO_TRIGGER, " ")
    .replace(/^[:\-\s]+/, "")
    .trim();
  return cleaned.length > 0 ? cleaned : trimmed;
}

function partsToText(parts: any[]): string {
  if (!Array.isArray(parts)) return "";
  const out: string[] = [];
  for (const p of parts) {
    if (!p) continue;
    if (typeof p === "string") {
      out.push(p);
      continue;
    }
    if (typeof p === "object") {
      if (typeof (p as any).text === "string") {
        out.push((p as any).text);
      }
    }
  }
  return out.join("");
}

function updateTextParts(parts: any[], text: string): void {
  if (!Array.isArray(parts)) return;
  for (const p of parts) {
    if (!p || typeof p !== "object") continue;
    if ((p as any).type === "text" && typeof (p as any).text === "string") {
      (p as any).text = text;
      return;
    }
  }
}

const OPENCODE_CONFIG_DIR = process.env.HOME
  ? join(process.env.HOME, ".config", "opencode")
  : "";
const OPENCODE_DATA_DIR = process.env.HOME ? join(process.env.HOME, ".opencode") : "";

const DEFAULT_IDLE_CAPTURE_INTERVAL_MS = 60_000;
const DEFAULT_IDLE_PLAN_SYNC_INTERVAL_MS = 60_000;
const DEFAULT_IDLE_TODO_INTERVAL_MS = 60_000;
const DEFAULT_IDLE_FLUSH_INTERVAL_MS = 5 * 60_000;

const idleActionLastRunAt = new Map<string, number>();
const idleActionInFlight = new Set<string>();

function normalizePath(value: string): string {
  return value.replace(/\\/g, "/").replace(/\/+$/, "");
}

function isOpenCodeInternalWorkspace(workspace: string): boolean {
  if (!workspace || !process.env.HOME) return false;
  const normalized = normalizePath(workspace);
  const configDir = normalizePath(OPENCODE_CONFIG_DIR);
  const dataDir = normalizePath(OPENCODE_DATA_DIR);
  return (configDir.length > 0 && normalized.startsWith(configDir)) ||
    (dataDir.length > 0 && normalized.startsWith(dataDir));
}

async function runIdleAction(
  key: string,
  intervalMs: number,
  action: () => Promise<void> | void
): Promise<boolean> {
  if (intervalMs <= 0) return false;
  if (idleActionInFlight.has(key)) return false;
  const now = Date.now();
  const lastAt = idleActionLastRunAt.get(key) ?? 0;
  if (now - lastAt < intervalMs) {
    return false;
  }
  idleActionLastRunAt.set(key, now);
  idleActionInFlight.add(key);
  try {
    await action();
  } finally {
    idleActionInFlight.delete(key);
  }
  return true;
}


const TODO_MODE_TTL_MS = 6 * 60 * 60 * 1000;

const lastTodoContinuationBySession = new Map<string, { at: number; digest: string }>();
const lastAnchorQuestionPingBySession = new Map<string, { at: number; digest: string }>();
const lastGotchasInjectedAtCompactionBySession = new Map<string, number>();
const todoModeBySession = new Map<string, number>();
const anchorModeBySession = new Map<string, number>();

function hashText(text: string): string {
  return createHash("sha256").update(text).digest("hex").slice(0, 16);
}

function parseEnvInt(name: string, fallback: number): number {
  const raw = process.env[name];
  if (!raw) return fallback;
  const n = Number.parseInt(raw, 10);
  return Number.isFinite(n) && n > 0 ? n : fallback;
}

function parseEnvInterval(name: string, fallback: number): number {
  const raw = process.env[name];
  if (!raw) return fallback;
  const n = Number.parseInt(raw, 10);
  if (!Number.isFinite(n)) return fallback;
  if (n <= 0) return 0;
  return n;
}

function setMode(map: Map<string, number>, sessionID: string): void {
  map.set(sessionID, Date.now());
}

function hasMode(map: Map<string, number>, sessionID: string): boolean {
  const at = map.get(sessionID);
  if (!at) return false;
  if (Date.now() - at > TODO_MODE_TTL_MS) {
    map.delete(sessionID);
    return false;
  }
  return true;
}

async function getModePath(sessionID: string, mode: string): Promise<string> {
  await mkdir(MODE_DIR, { recursive: true });
  const digest = createHash("sha256").update(`${mode}:${sessionID}`).digest("hex").slice(0, 16);
  return join(MODE_DIR, `${mode}-${digest}.json`);
}

async function persistMode(map: Map<string, number>, sessionID: string, mode: string): Promise<void> {
  setMode(map, sessionID);
  const path = await getModePath(sessionID, mode);
  await Bun.write(path, JSON.stringify({ updated_at: Date.now() }));
}

async function clearMode(map: Map<string, number>, sessionID: string, mode: string): Promise<void> {
  map.delete(sessionID);
  try {
    const path = await getModePath(sessionID, mode);
    await rm(path, { force: true });
  } catch {
    // Ignore cleanup errors.
  }
}

async function loadMode(map: Map<string, number>, sessionID: string, mode: string): Promise<boolean> {
  if (hasMode(map, sessionID)) {
    return true;
  }
  try {
    const path = await getModePath(sessionID, mode);
    const data = await Bun.file(path).json();
    const at = typeof data?.updated_at === "number" ? data.updated_at : 0;
    if (Date.now() - at <= TODO_MODE_TTL_MS) {
      setMode(map, sessionID);
      return true;
    }
  } catch {
    // No persisted mode.
  }
  return false;
}

async function maybePingOverseerForPendingQuestion(
  sessionID: string,
  workspace: string,
  pendingQuestion: string
): Promise<void> {
  const question = pendingQuestion.trim();
  if (!question) return;

  const now = Date.now();
  const digest = hashText(question);
  const prior = lastAnchorQuestionPingBySession.get(sessionID);

  if (prior && prior.digest === digest && now - prior.at < 15 * 60 * 1000) {
    return;
  }

  lastAnchorQuestionPingBySession.set(sessionID, { at: now, digest });

  await runSkill(
    "mailbox/manage",
    {
      operation: "send",
      workspace_id: workspace,
      send: {
        sender: "opencode",
        recipient: "overseer",
        subject: "Pending question",
        body: question,
        kind: "info",
        priority: 2,
        ack_required: true,
      },
    },
    { workspace, ephemeral: true, timeout: 3000 }
  );
}

type TodoContinuationSkillOutput = {
  should_continue: boolean;
  prompt: string;
};

type TodoListItem = {
  id?: string;
  title?: string;
  status?: string;
};

type TodoListSkillOutput = {
  tasks?: TodoListItem[];
};

async function computeTodoContinuation(
  sessionID: string,
  workspace: string,
  anchorGoal: string,
  anchorPending: string
): Promise<{ prompt: string; digest: string } | null> {
  if (process.env.AGENTCTL_TODO_CONTINUATION_DISABLED === "1") {
    return null;
  }

  const cleanedGoal = anchorGoal.trim();
  if (!cleanedGoal) {
    return null;
  }
  const cleanedPending = anchorPending.trim();

  const minPending = parseEnvInt("AGENTCTL_TODO_CONTINUATION_MIN_PENDING", 1);
  const topN = parseEnvInt("AGENTCTL_TODO_CONTINUATION_TOP_N", 5);

  const contResult = await runSkill<TodoContinuationSkillOutput>(
    "todo/continuation",
    {
      workspace_id: workspace,
      session_id: sessionID,
      top_n: topN,
      min_pending: minPending,
      anchor_goal: cleanedGoal,
      anchor_pending: cleanedPending,
    },
    { workspace, ephemeral: true, timeout: 3000 }
  );

  if (!contResult.success) {
    return null;
  }

  const should = contResult.data?.should_continue ?? false;
  const prompt = contResult.data?.prompt ?? "";

  if (!should || prompt.trim().length === 0) {
    return null;
  }

  return { prompt, digest: hashText(prompt) };
}

async function computeTodoLite(
  sessionID: string,
  workspace: string
): Promise<{ prompt: string; digest: string } | null> {
  const topN = parseEnvInt("AGENTCTL_TODO_CONTINUATION_TOP_N", 5);
  const listResult = await runSkill<TodoListSkillOutput>(
    "todo/manage",
    {
      operation: "list",
      workspace_id: workspace,
      list: { session_id: sessionID },
    },
    { workspace, ephemeral: true, timeout: 3000 }
  );

  if (!listResult.success) {
    return null;
  }

  const tasks = Array.isArray(listResult.data?.tasks) ? listResult.data?.tasks ?? [] : [];
  const openTasks = tasks.filter((t) => t?.status !== "completed");
  if (openTasks.length === 0) {
    return null;
  }

  const pending = openTasks.filter((t) => t?.status === "pending");
  const blocked = openTasks.filter((t) => t?.status === "blocked");
  const inProgress = openTasks.filter((t) => t?.status === "in_progress");

  const lines: string[] = [];
  const ordered = [...pending, ...inProgress, ...blocked];
  for (let i = 0; i < ordered.length && i < topN; i += 1) {
    const t = ordered[i];
    const title = (t?.title || t?.id || "").toString().trim();
    if (!title) continue;
    lines.push(`  ${i + 1}. ${title}`);
  }

  let prompt = "[SYSTEM REMINDER - TODO CHECK-IN]";
  prompt += `\n\nIncomplete tasks: ${openTasks.length} (${pending.length} pending, ${blocked.length} blocked, ${inProgress.length} in progress)`;

  if (lines.length > 0) {
    prompt += "\n\n**NEXT TASKS**:\n" + lines.join("\n");
  }

  prompt += "\n\n- Proceed without asking for permission";
  prompt += "\n- Mark each task complete when finished";
  prompt += "\n- Do not stop until all tasks are done";

  return { prompt, digest: hashText(prompt) };
}

export const AgentctlPlugin: Plugin = async ({ client, directory, $ }) => {
  const workspace = getWorkspace(directory);

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
          "Semantic vector search across the codebase. Use for finding related code, implementations, or patterns. Use format='tree' to see related files grouped by directory.",
        args: {
          query: z.string().describe("Natural language search query"),
          scope: z
            .enum(["symbols", "sessions", "memories", "tasks", "codemaps"])
            .optional()
            .describe("Search scope (default: symbols)"),
          limit: z.number().optional().describe("Max results (default: 10)"),
          format: z
            .enum(["json", "tree"])
            .optional()
            .describe("Output format: 'json' (default) or 'tree' (directory tree of related files)"),
        },
        async execute({ query, scope = "symbols", limit = 10, format = "json" }) {
          const result = await runSkill<{ results: unknown[]; tree_text?: string }>(
            "code/semantic_search",
            { query, scope, limit, format },
            { workspace, ephemeral: true, timeout: 10000 }
          );

          if (!result.success || !result.data?.results?.length) {
            return "No matches found.";
          }

          // Return tree view if requested
          if (format === "tree" && result.data.tree_text) {
            return result.data.tree_text;
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
              id?: string;
              subject: string;
              body: string;
              priority: number;
            }>;
          }>(
            "mailbox/manage",
            {
              operation: "inbox",
              workspace_id: workspace,
              inbox: {
                actor_id: recipient,
                only_unread: unreadOnly,
                limit: 25,
              },
            },
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
          }>("todo/manage", { operation: "get_active", workspace_id: workspace }, { workspace, ephemeral: true, timeout: 3000 });

          if (!result.success || !result.data?.task) {
            return "No active task. Use `agentctl todo add` to create one.";
          }

          const t = result.data.task;
          return `**Active Task**: ${t.title}\nID: ${t.id}\nStatus: ${t.status}${t.description ? `\n\n${t.description}` : ""}`;
        },
      }),

      /**
       * Multi-perspective code analysis (mirrors /counsel slash command)
       */
      "agentctl-counsel": tool({
        description:
          "Run multi-perspective code analysis with LLM review. Use for security reviews, correctness checks, and code quality analysis. More thorough than search - actually analyzes the code.",
        args: {
          question: z.string().describe("What to analyze, e.g., 'review auth flow for security issues'"),
          perspectives: z
            .array(z.enum(["security", "correctness", "performance", "maintainability"]))
            .optional()
            .describe("Analysis perspectives (default: security, correctness)"),
          maxFiles: z.number().optional().describe("Max files to analyze (default: 8)"),
        },
        async execute({ question, perspectives = ["security", "correctness"], maxFiles = 8 }) {
          const result = await runSkill<{
            query: string;
            analyses: Array<{
              perspective: string;
              summary: string;
              findings: Array<{
                title: string;
                severity: string;
                description: string;
                location?: string;
              }>;
            }>;
            stats: {
              files_analyzed: number;
              latency_ms: number;
              provider: string;
            };
          }>(
            "code/counsel",
            {
              query: question,
              auto_files: true,
              perspectives,
              max_files: maxFiles,
            },
            { workspace, ephemeral: true, timeout: 60000 }
          );

          if (!result.success) {
            return "Counsel analysis failed. Check API keys (ANTHROPIC_API_KEY, OPENAI_API_KEY, or CEREBRAS_API_KEY).";
          }

          if (!result.data?.analyses?.length) {
            return "No analysis results returned.";
          }

          // Format as markdown
          const lines: string[] = [`## Code Counsel Analysis\n\n**Query:** ${result.data.query}\n`];

          for (const analysis of result.data.analyses) {
            lines.push(`### ${analysis.perspective.toUpperCase()}\n`);
            if (analysis.findings?.length) {
              for (const f of analysis.findings) {
                lines.push(`- **${f.title}** (${f.severity})`);
                lines.push(`  ${f.description}`);
                if (f.location) lines.push(`  Location: \`${f.location}\``);
              }
            } else {
              lines.push("_No issues found_");
            }
            lines.push(`\n**Summary:** ${analysis.summary}\n`);
          }

          lines.push(`---\n**Files analyzed:** ${result.data.stats?.files_analyzed ?? 0} | **Latency:** ${result.data.stats?.latency_ms ?? 0}ms | **Provider:** ${result.data.stats?.provider ?? "unknown"}`);

          return lines.join("\n");
        },
      }),

      /**
       * Quick code context gathering (mirrors /context slash command)
       */
      "agentctl-context": tool({
        description:
          "Quickly gather relevant code snippets for a query. Faster than counsel - no LLM analysis, just retrieves matching code.",
        args: {
          query: z.string().describe("What to find, e.g., 'database connection handling'"),
          mode: z
            .enum(["general", "structure"])
            .optional()
            .describe("Mode: 'general' (snippets) or 'structure' (API shape)"),
          maxFiles: z.number().optional().describe("Max files (default: 5)"),
        },
        async execute({ query, mode = "general", maxFiles = 5 }) {
          const result = await runSkill<{
            stats: {
              query: string;
              files_processed: number;
              snippets_found: number;
              selection_method: string;
            };
            candidates: Array<{ path: string; score: number }>;
            evidence: {
              snippets: Array<{
                file: string;
                start_line: number;
                end_line: number;
                language: string;
                text: string;
              }>;
            };
          }>(
            "code/smart_read",
            {
              query,
              auto_files: true,
              mode,
              max_files: maxFiles,
            },
            { workspace, ephemeral: true, timeout: 15000 }
          );

          if (!result.success) {
            return "Failed to gather code context.";
          }

          const lines: string[] = [`## Code Context: ${result.data?.stats?.query ?? query}\n`];
          lines.push(
            `**Files:** ${result.data?.stats?.files_processed ?? 0} | **Snippets:** ${result.data?.stats?.snippets_found ?? 0} | **Method:** ${result.data?.stats?.selection_method ?? "auto"}\n`
          );

          if (result.data?.candidates?.length) {
            const candidates = result.data.candidates
              .slice(0, 5)
              .map((c) => `\`${c.path}\` (${Math.floor((c.score ?? 0) * 100)}%)`)
              .join(", ");
            lines.push(`**Candidates:** ${candidates}\n`);
          }

          if (result.data?.evidence?.snippets?.length) {
            for (const s of result.data.evidence.snippets.slice(0, 10)) {
              lines.push(`#### ${s.file}:${s.start_line}-${s.end_line}`);
              lines.push(`\`\`\`${s.language || ""}\n${s.text}\n\`\`\`\n`);
            }
          } else {
            lines.push("_No relevant snippets found_");
          }

          lines.push("---\n*Use line numbers to read more context.*");
          return lines.join("\n");
        },
      }),

      /**
       * Smart code retrieval with full function bodies
       */
      "agentctl-ripgrep": tool({
        description:
          "Search code and return full function bodies containing matches. Better than grep for understanding context.",
        args: {
          pattern: z.string().describe("Search pattern"),
          contextLines: z.number().optional().describe("Extra context lines (default: 5)"),
        },
        async execute({ pattern, contextLines = 5 }) {
          const result = await runSkill<{
            total_matches: number;
            results: Array<{
              path: string;
              line: number;
              match_text: string;
              context: string;
            }>;
          }>(
            "code/context_ripgrep",
            {
              pattern,
              context_lines: contextLines,
              max_results: 20,
            },
            { workspace, ephemeral: true, timeout: 10000 }
          );

          if (!result.success || !result.data?.results?.length) {
            return "No matches found.";
          }

          const lines: string[] = [`## Search: "${pattern}" (${result.data.total_matches} matches)\n`];
          for (const r of result.data.results.slice(0, 10)) {
            lines.push(`### ${r.path}:${r.line}`);
            lines.push(`\`\`\`\n${r.context}\n\`\`\`\n`);
          }

          return lines.join("\n");
        },
      }),
    },

    "chat.message": async (
      input: { sessionID: string },
      output: { parts: any[] }
    ): Promise<void> => {
      const text = partsToText(output.parts);

      // Handle @strict/@agentctl commands first
      const strictMatch = text.match(STRICT_TRIGGER);
      if (strictMatch) {
        const cmd = (strictMatch[2] || "status").toLowerCase();
        const isEnable = ["on", "enable", "1", "true"].includes(cmd);
        const isDisable = ["off", "disable", "0", "false"].includes(cmd);

        if (isEnable) {
          const success = await setAgentctlMode(workspace, true);
          if (success) {
            await writePendingContext(
              input.sessionID,
              "Agentctl Mode",
              `**Agentctl Mode: ENABLED**

Tool redirections active:
- Edit/Write/MultiEdit → fs/apply_edit (dry-run preview)
- Grep → code/smart_search (semantic + snippet extraction)
- Glob → code/semantic_search (vector similarity)
- Read (large files) → symbols outline + navigation

Use \`@strict off\` to disable.`
            );
          }
        } else if (isDisable) {
          const success = await setAgentctlMode(workspace, false);
          if (success) {
            await writePendingContext(
              input.sessionID,
              "Agentctl Mode",
              `**Agentctl Mode: DISABLED**

Default tool behavior restored. Use \`@strict on\` to re-enable.`
            );
          }
        } else {
          // Status check
          const enabled = await isAgentctlModeEnabled(workspace);
          await writePendingContext(
            input.sessionID,
            "Agentctl Mode",
            enabled
              ? `**Agentctl Mode: ENABLED**

Tool redirections:
- Edit/Write/MultiEdit → fs/apply_edit
- Grep → code/smart_search
- Glob → code/semantic_search
- Read (large files) → symbols outline`
              : `**Agentctl Mode: DISABLED** (use \`@strict on\` to enable)`
          );
        }
        return; // Don't process further
      }

      const hasAnchor = ANCHOR_TRIGGER.test(text);
      const hasTodo = TODO_TRIGGER.test(text);
      const hasCounsel = COUNSEL_TRIGGER.test(text);
      const hasContext = CONTEXT_TRIGGER.test(text);

      // Skill advisor - check for patterns and suggest skills (runs even without slash commands)
      if (process.env.AGENTCTL_SKILL_ADVISOR_DISABLED !== "1" && text.length >= 10) {
        for (const { pattern, hint } of SKILL_PATTERNS) {
          if (pattern.test(text)) {
            await writePendingContext(input.sessionID, "Skill Advisor", hint, 60_000);
            break; // Only show first matching hint
          }
        }
      }

      // Keyword triggers - execute skills based on natural language patterns
      const textLower = text.toLowerCase();

      // Recall patterns - "how did we", "recall", "remember when"
      const RECALL_PATTERN = /(how did (we|i)|where did (we|i)|what was the|when did (we|i)|recall|remember when|previously|earlier we|last time|didn't (we|i) already|like before|as we discussed)/i;
      if (RECALL_PATTERN.test(textLower)) {
        const query = text.replace(RECALL_PATTERN, "").trim();
        if (query.length > 3) {
          const memResult = await runSkill<{
            results?: Array<{ type?: string; summary?: string }>;
          }>(
            "memory/search",
            { query, limit: 5 },
            { workspace, ephemeral: true, timeout: 5000 }
          ).catch(() => ({ success: false, data: undefined }));

          if (memResult.success && memResult.data?.results?.length) {
            const formatted = memResult.data.results
              .slice(0, 5)
              .map((m) => `- [${m.type || "memory"}] ${(m.summary || "").slice(0, 80)}`)
              .join("\n");
            await writePendingContext(
              input.sessionID,
              "Recall Results",
              `**Recall for:** ${query}\n\n**Memories:**\n${formatted}`
            );
          } else {
            await writePendingContext(
              input.sessionID,
              "Recall",
              `No matching memories found for "${query}". Try \`agentctl run session/recall\` for session history.`
            );
          }
        }
      }

      // Memory save patterns - "remember", "gotcha", "learned"
      const MEMORY_SAVE_PATTERN = /^(remember|note|gotcha|learned|important|decision):?/i;
      const MEMORY_SAVE_INLINE = /(remember this|note this|save this|don't forget|the trick is|the key is|turns out|watch out)/i;
      if (MEMORY_SAVE_PATTERN.test(textLower) || MEMORY_SAVE_INLINE.test(textLower)) {
        // Determine memory type
        let memType = "context";
        if (/^gotcha/i.test(textLower) || /(trick|watch out|careful)/i.test(textLower)) {
          memType = "gotcha";
        } else if (/^learned/i.test(textLower) || /(turns out|realized)/i.test(textLower)) {
          memType = "learning";
        } else if (/^decision/i.test(textLower)) {
          memType = "decision";
        }

        const content = text.replace(MEMORY_SAVE_PATTERN, "").replace(MEMORY_SAVE_INLINE, "").trim();
        if (content.length > 10) {
          const saveResult = await runSkill<{ id?: string }>(
            "memory/put",
            { summary: content, type: memType },
            { workspace, ephemeral: true, timeout: 3000 }
          ).catch(() => ({ success: false, data: undefined }));

          if (saveResult.success) {
            await writePendingContext(
              input.sessionID,
              "Memory Saved",
              `**[${memType}]:** ${content.slice(0, 100)}${content.length > 100 ? "..." : ""}\nID: ${saveResult.data?.id || "saved"}`
            );
          }
        } else {
          await writePendingContext(
            input.sessionID,
            "Memory Hint",
            `Detected ${memType} pattern. Add more detail to auto-save.`
          );
        }
      }

      // Semantic search patterns - "search for", "find the", "where is"
      const SEARCH_PATTERN = /^(search|find|query|where is|look for)/i;
      const SEARCH_INLINE = /(search for|find the|locate)/i;
      if (SEARCH_PATTERN.test(textLower) || SEARCH_INLINE.test(textLower)) {
        const query = text.replace(SEARCH_PATTERN, "").replace(SEARCH_INLINE, "").trim();
        if (query.length > 3) {
          const searchResult = await runSkill<{
            results?: Array<{ path?: string; line?: number; snippet?: string; symbol?: string }>;
          }>(
            "code/semantic_search",
            { query, limit: 8 },
            { workspace, ephemeral: true, timeout: 8000 }
          ).catch(() => ({ success: false, data: undefined }));

          if (searchResult.success && searchResult.data?.results?.length) {
            const formatted = searchResult.data.results
              .slice(0, 8)
              .map((r) => `- ${r.path || "?"}:${r.line || "?"} — ${(r.snippet || r.symbol || "").slice(0, 50)}`)
              .join("\n");
            await writePendingContext(
              input.sessionID,
              "Semantic Search",
              `**Search for:** ${query}\n\n${formatted}\n\n*Use \`code/snippet_extract\` for full context.*`
            );
          }
        }
      }

      // Codemap patterns - "trace", "how does...connect", "architecture of"
      const CODEMAP_PATTERN = /(trace|codemap|how does.*connect|flow of|architecture of)/i;
      if (CODEMAP_PATTERN.test(textLower)) {
        const query = text.replace(/^(trace|codemap|how does|show me the)/i, "").trim();
        if (query.length > 3) {
          const listResult = await runSkill<{
            codemaps?: Array<{ id?: string; query?: string }>;
          }>(
            "codemap/list",
            { limit: 5 },
            { workspace, ephemeral: true, timeout: 3000 }
          ).catch(() => ({ success: false, data: undefined }));

          const maps = listResult.success && listResult.data?.codemaps?.length
            ? listResult.data.codemaps
                .slice(0, 5)
                .map((m) => `- \`${m.id || "?"}\`: ${(m.query || "").slice(0, 60)}`)
                .join("\n")
            : "(no codemaps)";
          await writePendingContext(
            input.sessionID,
            "Codemap Search",
            `**Codemap for:** ${query}\n\n**Existing codemaps:**\n${maps}\n\n*Generate new: \`agentctl run codemap/generate --input '{"query": "..."}'\`*`
          );
        }
      }

      if (!hasAnchor && !hasTodo && !hasCounsel && !hasContext) {
        return;
      }

      let cleaned = text;
      if (hasAnchor) {
        cleaned = stripAnchorTrigger(cleaned);
      }
      if (hasTodo) {
        cleaned = stripTodoTrigger(cleaned);
      }
      const cleanedTrimmed = cleaned.trim();

      if (hasAnchor) {
        if (!cleanedTrimmed) {
          await writePendingContext(input.sessionID, "Anchor", "Usage: `/anchor <goal>`", 60_000);
        } else {
          const setResult = await runSkill(
            "session/anchor",
            {
              operation: "set",
              workspace,
              session_id: input.sessionID,
              main_prompt: cleanedTrimmed,
              trigger: "chat.message",
            },
            { workspace, ephemeral: true, timeout: 3000 }
          );

          if (setResult.success) {
            await persistMode(anchorModeBySession, input.sessionID, "anchor");
            await writePendingContext(
              input.sessionID,
              "Anchor set",
              `**Goal:** ${cleanedTrimmed}`,
              60_000
            );
          }
        }
      }

      if (hasTodo) {
        await persistMode(todoModeBySession, input.sessionID, "todo");
        await writePendingContext(input.sessionID, "TODO mode", "Todo check-in enabled for this session.", 60_000);
      }

      // Handle /counsel <question> - multi-perspective code analysis
      if (hasCounsel) {
        const question = extractAfterCommand(text, "counsel");
        if (!question) {
          await writePendingContext(
            input.sessionID,
            "Counsel",
            "Usage: `/counsel <question>` - e.g., `/counsel review auth flow for security issues`",
            60_000
          );
        } else {
          await writePendingContext(
            input.sessionID,
            "Counsel",
            `Running code analysis for: "${question}"...`,
            120_000
          );

          const result = await runSkill<{
            query: string;
            analyses: Array<{
              perspective: string;
              summary: string;
              findings: Array<{
                title: string;
                severity: string;
                description: string;
                location?: string;
              }>;
            }>;
            stats: { files_analyzed: number; latency_ms: number; provider: string };
          }>(
            "code/counsel",
            {
              query: question,
              auto_files: true,
              perspectives: ["security", "correctness"],
              max_files: 8,
            },
            { workspace, ephemeral: true, timeout: 60000 }
          );

          if (result.success && result.data?.analyses?.length) {
            const lines: string[] = [`## Code Counsel Analysis\n\n**Query:** ${result.data.query}\n`];
            for (const analysis of result.data.analyses) {
              lines.push(`### ${analysis.perspective.toUpperCase()}\n`);
              if (analysis.findings?.length) {
                for (const f of analysis.findings) {
                  lines.push(`- **${f.title}** (${f.severity})`);
                  lines.push(`  ${f.description}`);
                  if (f.location) lines.push(`  Location: \`${f.location}\``);
                }
              } else {
                lines.push("_No issues found_");
              }
              lines.push(`\n**Summary:** ${analysis.summary}\n`);
            }
            lines.push(`---\n**Files:** ${result.data.stats?.files_analyzed ?? 0} | **Latency:** ${result.data.stats?.latency_ms ?? 0}ms`);
            await writePendingContext(input.sessionID, "Counsel Results", lines.join("\n"), 5 * 60_000);
          } else {
            await writePendingContext(
              input.sessionID,
              "Counsel",
              "Analysis failed. Ensure API keys are set (ANTHROPIC_API_KEY, OPENAI_API_KEY, or CEREBRAS_API_KEY).",
              60_000
            );
          }
        }
      }

      // Handle /context <query> - quick code context gathering
      if (hasContext) {
        const query = extractAfterCommand(text, "context");
        if (!query) {
          await writePendingContext(
            input.sessionID,
            "Context",
            "Usage: `/context <query>` - e.g., `/context database connection handling`",
            60_000
          );
        } else {
          const result = await runSkill<{
            stats: { query: string; files_processed: number; snippets_found: number };
            candidates: Array<{ path: string; score: number }>;
            evidence: {
              snippets: Array<{
                file: string;
                start_line: number;
                end_line: number;
                language: string;
                text: string;
              }>;
            };
          }>(
            "code/smart_read",
            {
              query,
              auto_files: true,
              mode: "general",
              max_files: 5,
            },
            { workspace, ephemeral: true, timeout: 15000 }
          );

          if (result.success && result.data?.evidence?.snippets?.length) {
            const lines: string[] = [`## Code Context: ${result.data.stats?.query ?? query}\n`];
            lines.push(`**Files:** ${result.data.stats?.files_processed ?? 0} | **Snippets:** ${result.data.stats?.snippets_found ?? 0}\n`);

            if (result.data.candidates?.length) {
              const candidates = result.data.candidates
                .slice(0, 5)
                .map((c) => `\`${c.path}\` (${Math.floor((c.score ?? 0) * 100)}%)`)
                .join(", ");
              lines.push(`**Candidates:** ${candidates}\n`);
            }

            for (const s of result.data.evidence.snippets.slice(0, 10)) {
              lines.push(`#### ${s.file}:${s.start_line}-${s.end_line}`);
              lines.push(`\`\`\`${s.language || ""}\n${s.text}\n\`\`\`\n`);
            }
            await writePendingContext(input.sessionID, "Context Results", lines.join("\n"), 5 * 60_000);
          } else {
            await writePendingContext(
              input.sessionID,
              "Context",
              `No relevant code found for: "${query}"`,
              60_000
            );
          }
        }
      }

      if (cleaned !== text && cleanedTrimmed) {
        updateTextParts(output.parts, cleaned);
      }
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

      try {
        let sessionID = input.sessionID;
        if (!sessionID) {
          sessionID = (await getSessionIdentity(workspace)).sessionID;
        }
        if (!sessionID) {
          return;
        }

        // 1. Read pending context from tool hooks (file-based handoff)
        const pendingEntries = await readAndClearPendingContext(sessionID);
        for (const entry of pendingEntries) {
          context.push(`**${entry.source}**:\n${entry.context}`);
        }

        // 2. Get active task (lightweight check)
        const taskResult = await runSkill<{
          task?: { title: string; id: string };
        }>("todo/manage", { operation: "get_active", workspace_id: workspace }, { workspace, ephemeral: true, timeout: 2000 });

        if (taskResult.success && taskResult.data?.task) {
          context.push(
            `**Active Task**: ${taskResult.data.task.title} (${taskResult.data.task.id})`
          );
        }

        const anchorResult = await runSkill<{
          found: boolean;
          anchor?: {
            main_prompt?: string;
            pending_question?: string;
            compaction_count?: number;
          };
        }>(
          "session/anchor",
          { operation: "get", workspace, session_id: sessionID },
          { workspace, ephemeral: true, timeout: 2000 }
        );

        if (anchorResult.success && anchorResult.data?.anchor?.main_prompt) {
          context.push(`**Anchor Goal**: ${anchorResult.data.anchor.main_prompt}`);
          if (anchorResult.data.anchor.pending_question) {
            context.push(`**Anchor Pending**: ${anchorResult.data.anchor.pending_question}`);
          }

          const compactionCount = anchorResult.data.anchor.compaction_count ?? 0;
          const lastInjected = lastGotchasInjectedAtCompactionBySession.get(sessionID) ?? 0;
          if (compactionCount > 0 && compactionCount !== lastInjected) {
            const gotchasResult = await runSkill<{
              memories: Array<{ name: string; summary: string }>;
            }>(
              "memory/query",
              { workspace, session_id: sessionID, types: "gotcha", limit: 20 },
              { workspace, ephemeral: true, timeout: 3000 }
            );

            if (gotchasResult.success && gotchasResult.data?.memories?.length) {
              const gotchas = gotchasResult.data.memories
                .map((m) => m.summary)
                .filter((s) => typeof s === "string" && s.trim().length > 0)
                .slice(0, 20)
                .map((s) => `- ${s}`)
                .join("\n");
              if (gotchas) {
                context.push(`**Session Gotchas (latest 20)**:\n${gotchas}`);
              }
            }

            lastGotchasInjectedAtCompactionBySession.set(sessionID, compactionCount);
          }
        }

        // 3. Check for urgent overseer messages (priority >= 2)
        const msgResult = await runSkill<{
          messages: Array<{
            id: string;
            subject: string;
            body: string;
            priority: number;
          }>;
        }>(
          "mailbox/manage",
          {
            operation: "inbox",
            workspace_id: workspace,
            inbox: {
              actor_id: "overseer",
              only_unread: true,
              only_unsurfaced: true,
              limit: 25,
            },
          },
          { workspace, ephemeral: true, timeout: 2000 }
        );

        if (msgResult.success && msgResult.data?.messages?.length) {
          const urgentMessages = msgResult.data.messages.filter(
            (m) => (m.priority ?? 5) <= 2  // P1 and P2 are urgent (lower = higher priority)
          );

          if (urgentMessages.length > 0) {
            const urgent = urgentMessages
              .map((m) => `- **${m.subject}**: ${m.body}`)
              .join("\n");
            context.push(`**Urgent Messages**:\n${urgent}`);

            // Mark messages as surfaced to prevent re-injection
            const messageIDs = urgentMessages.map((m) => m.id);
            await runSkill(
              "mailbox/manage",
              {
                operation: "mark_surfaced",
                workspace_id: workspace,
                actor_id: "overseer",
                message_ids: messageIDs,
              },
              { workspace, ephemeral: true, timeout: 2000 }
            ).catch(() => {}); // Best effort
          }
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

      // =========================================================================
      // AGENTCTL MODE - Block/redirect tools when enabled
      // =========================================================================
      const agentctlModeEnabled = await isAgentctlModeEnabled(workspace);
      if (agentctlModeEnabled) {
        const tool = input.tool;

        // Self-protection: block edits to hook files and settings
        if (tool === "Edit" || tool === "Write" || tool === "MultiEdit") {
          const editPath = (args.file_path || args.path || "") as string;
          if (editPath.includes("/.opencode/") || editPath.includes("/opencode-hooks/")) {
            throw new Error(
              "**[Agentctl Mode] Cannot edit plugin files while enabled.**\n\nUse `@strict off` first to modify plugins."
            );
          }
          if (editPath.includes("/settings.json") || editPath.includes("/config.json")) {
            throw new Error(
              "**[Agentctl Mode] Cannot edit settings while enabled.**\n\nUse `@strict off` first to modify settings."
            );
          }

          // Block Edit/Write - redirect to fs/apply_edit
          const filePath = editPath;
          throw new Error(
            `**[Agentctl Mode] Use search/replace via fs/apply_edit**

**Workflow:**
1. **Get exact text**: \`agentctl run code/context_grep --input '{"mode": "line", "file_path": "${filePath}", "line_start": N, "line_end": M}'\`
2. **Preview change**: \`agentctl run fs/apply_edit --input '{"path": "${filePath}", "edits": [{"search": "exact old text", "replace": "new text"}], "dry_run": true}'\`
3. **Apply**: Set \`dry_run: false\`

**Tips:**
- \`search\` must match exactly (copy from context_grep output)
- Multiple edits: \`"edits": [{...}, {...}]\`
- Use \`--input-file\` for complex edits`
          );
        }

        // Block Grep - redirect to code/smart_search
        if (tool === "Grep") {
          const pattern = (args.pattern || "") as string;
          throw new Error(
            `**[Agentctl Mode] Smart Search via code/smart_search**

> Direct: \`agentctl run code/smart_search --input '{"question": "${pattern.replace(/'/g, "\\'")}"}'\`
> Params: \`limits.max_candidates\`, \`limits.max_snippets\` to control output size.`
          );
        }

        // Block Glob - redirect to code/semantic_search
        if (tool === "Glob") {
          const pattern = (args.pattern || "") as string;
          throw new Error(
            `**[Agentctl Mode] Semantic Search via code/semantic_search**

> Direct: \`agentctl run code/semantic_search --input '{"query": "${pattern.replace(/'/g, "\\'")}","scope": ["symbols"], "limit": 10}'\`
> Scopes: \`symbols\`, \`memory\`, \`codemaps\`. Uses vector embeddings.
> Memory scope supports date-based search (e.g., "January gotchas", "2026 decisions").`
          );
        }

        // Block Read for large files - redirect to symbols/context_grep
        if (tool === "Read") {
          const filePath = (args.file_path || args.path || "") as string;
          const offset = (args.offset || 0) as number;
          const limit = (args.limit || 0) as number;

          // Allow specific line ranges
          if (offset > 0 || limit > 0) {
            // Allow - user specified a range
          } else if (filePath) {
            // Check file size via skill
            const sizeResult = await runSkill<{ line_count?: number }>(
              "fs/stat",
              { path: filePath },
              { workspace, ephemeral: true, timeout: 2000 }
            ).catch(() => ({ success: false, data: { line_count: 0 } }));

            const lineCount = sizeResult.data?.line_count || 0;
            if (lineCount > 300) {
              // Large file - show symbols instead
              const fileName = filePath.split("/").pop() || filePath;

              const symbolsResult = await runSkill<{
                symbols?: Array<{ name: string; type: string; line: number }>;
              }>(
                "code/symbols",
                { path: filePath, max_results: 30 },
                { workspace, ephemeral: true, timeout: 5000 }
              ).catch(() => ({ success: false, data: undefined }));

              const symbolsList = symbolsResult.success && symbolsResult.data?.symbols?.length
                ? symbolsResult.data.symbols
                    .slice(0, 30)
                    .map((s) => `- ${s.type} \`${s.name}\` (line ${s.line})`)
                    .join("\n")
                : "(no symbols found)";

              // Search for relevant gotchas
              const gotchasResult = await runSkill<{
                results?: Array<{ name?: string; summary?: string; snippet?: string }>;
              }>(
                "code/semantic_search",
                { query: `${fileName} gotchas`, scope: "memory", limit: 3 },
                { workspace, ephemeral: true, timeout: 3000 }
              ).catch(() => ({ success: false, data: undefined }));

              let gotchasSection = "";
              if (gotchasResult.success && gotchasResult.data?.results?.length) {
                const gotchasList = gotchasResult.data.results
                  .slice(0, 2)
                  .map((g) => `- **${g.name || "gotcha"}**: ${(g.snippet || g.summary || "").split("\n")[0]}`)
                  .join("\n");
                gotchasSection = `\n\n**Relevant Gotchas:**\n${gotchasList}`;
              }

              throw new Error(
                `**[Agentctl Mode] Large File: ${fileName} (${lineCount} lines)**

**Symbols:**
${symbolsList}${gotchasSection}

**To read specific lines:**
  \`agentctl run code/context_grep --input '{"mode": "line", "file_path": "${filePath}", "line_start": N, "line_end": M}'\`

**To search by concept:**
  \`agentctl run code/semantic_search --input '{"query": "your concept here"}'\`

**To read full file anyway (${lineCount} lines):**
  \`agentctl run fs/read --input '{"path": "${filePath}"}'\``
              );
            }
          }
        }
      }
      // =========================================================================
      // END AGENTCTL MODE
      // =========================================================================

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
            results: Array<{ path: string; name: string; similarity: number; source: string }>;
          }>(
            "code/semantic_search",
            { query: pattern, scope: "symbols", limit: 5 },
            { workspace, ephemeral: true, timeout: 5000 }
          );

          if (searchResult.success && searchResult.data?.results?.length) {
            const formatted = searchResult.data.results
              .map((r) => `- ${r.path || "?"}: ${r.name || "?"} (${((r.similarity ?? 0) * 100).toFixed(0)}%) [${r.source || "symbol"}]`)
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
          messages: Array<{ id: string; subject: string; body: string; priority: number }>;
        }>(
          "mailbox/manage",
          {
            operation: "inbox",
            workspace_id: workspace,
            inbox: {
              actor_id: "overseer",
              only_unread: true,
              only_unsurfaced: true,
              limit: 25,
            },
          },
          { workspace, ephemeral: true, timeout: 2000 }
        );

        if (inboxResult.success && inboxResult.data?.messages?.length) {
          const formatted = inboxResult.data.messages
            .map((m) => `[P${m.priority}] **${m.subject}**: ${m.body}`)
            .join("\n");
          await writePendingContext(sessionID, "Overseer Messages", formatted);

          // Mark messages as surfaced to prevent re-injection
          const messageIDs = inboxResult.data.messages.map((m) => m.id);
          await runSkill(
            "mailbox/manage",
            {
              operation: "mark_surfaced",
              workspace_id: workspace,
              actor_id: "overseer",
              message_ids: messageIDs,
            },
            { workspace, ephemeral: true, timeout: 2000 }
          ).catch(() => {}); // Best effort
        }
      }

      // Task guard - block edits without active task
      if (
        process.env.AGENTCTL_TASK_GUARD_MODE === "strict" &&
        (input.tool === "Edit" || input.tool === "Write")
      ) {
        const taskResult = await runSkill<{ task?: unknown }>(
          "todo/manage",
          { operation: "get_active", workspace_id: workspace },
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
            "code/incremental_index",
            { paths: [filePath], workspace },
            { workspace, ephemeral: true, timeout: 5000 }
          ).catch(() => {});

          // LSP Diagnostics - run language-specific linting after edits
          if (process.env.AGENTCTL_LSP_DIAG_DISABLED !== "1") {
            const lspResult = await runSkill<{
              diagnostics?: Array<{ line: number; col?: number; message: string; severity?: string }>;
              error_count?: number;
            }>(
              "code/lsp_check",
              { path: filePath, max_errors: 10 },
              { workspace, ephemeral: true, timeout: 10000 }
            ).catch(() => ({ success: false, data: undefined }));

            if (lspResult.success && lspResult.data?.diagnostics?.length) {
              const formatted = lspResult.data.diagnostics
                .slice(0, 5)
                .map((d) => `- ${d.severity || "Error"} [L${d.line}${d.col ? `:${d.col}` : ""}]: ${d.message}`)
                .join("\n");
              await writePendingContext(
                input.sessionID,
                `LSP Diagnostics: ${filePath.split("/").pop()}`,
                `**${lspResult.data.error_count ?? lspResult.data.diagnostics.length} issues found:**\n${formatted}`
              );
            }
          }

          // Code complexity warning after edits
          if (process.env.AGENTCTL_COMPLEXITY_DISABLED !== "1") {
            const complexityResult = await runSkill<{
              hotspots?: Array<{ name: string; complexity: number; line: number }>;
            }>(
              "code/complexity",
              { path: filePath, threshold: 15, analysis_mode: "hotspots", max_results: 3 },
              { workspace, ephemeral: true, timeout: 5000 }
            ).catch(() => ({ success: false, data: undefined }));

            if (complexityResult.success && complexityResult.data?.hotspots?.length) {
              const formatted = complexityResult.data.hotspots
                .map((h) => `- \`${h.name}\` (L${h.line}): complexity ${h.complexity}`)
                .join("\n");
              await writePendingContext(
                input.sessionID,
                "Complexity Warning",
                `**High complexity functions detected:**\n${formatted}\n\nConsider refactoring.`
              );
            }
          }
        }
      }

      // TodoWrite sync - sync todos with agentctl and prompt for memories on completion
      if (input.tool === "TodoWrite") {
        const todos = output.metadata?.todos as Array<{ content?: string; status?: string }> | undefined;
        if (todos?.length) {
          // Sync todos with agentctl task system
          runSkill(
            "todo/sync_from_provider",
            {
              workspace_id: workspace,
              session_id: input.sessionID,
              provider: "opencode",
              todos: todos.map((t) => ({
                title: t.content || "",
                status: t.status || "pending",
              })),
            },
            { workspace, ephemeral: true, timeout: 5000 }
          ).catch(() => {});

          // Memory prompt on task completion
          if (process.env.AGENTCTL_MEMORY_PROMPT_DISABLED !== "1") {
            const completedTasks = todos.filter((t) => t.status === "completed");
            if (completedTasks.length > 0) {
              const taskNames = completedTasks.map((t) => t.content).filter(Boolean).join(", ");
              const hint = completedTasks.length === 1
                ? `**Memory prompt:** Task completed: "${taskNames}"\n\nIf you learned something useful or encountered a gotcha, save it:\n\`agentctl memory put --name "gotcha-<topic>" --type gotcha --summary "<learning>"\``
                : `**Memory prompt:** Completed ${completedTasks.length} tasks.\n\nIf you learned something useful or encountered gotchas, save them:\n\`agentctl memory put --name "gotcha-<topic>" --type gotcha --summary "<learning>"\``;
              await writePendingContext(input.sessionID, "Memory Prompt", hint);
            }
          }
        }
      }

      // Track code file reads and suggest /counsel after threshold
      if (input.tool === "Read") {
        const filePath = (output.metadata?.file || output.metadata?.file_path || output.metadata?.path) as string | undefined;
        if (filePath) {
          // Check if it's a code file
          const codeExtensions = [".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".c", ".cpp", ".h", ".java", ".swift", ".kt", ".rb", ".php"];
          const isCodeFile = codeExtensions.some((ext) => filePath.endsWith(ext));

          if (isCodeFile) {
            // Get or create session's file read set
            let readFiles = fileReadsPerSession.get(input.sessionID);
            if (!readFiles) {
              readFiles = new Set<string>();
              fileReadsPerSession.set(input.sessionID, readFiles);
            }
            readFiles.add(filePath);

            // Suggest /counsel after threshold reached
            if (readFiles.size === COUNSEL_SUGGESTION_THRESHOLD) {
              await writePendingContext(
                input.sessionID,
                "Tip",
                `You've read ${COUNSEL_SUGGESTION_THRESHOLD}+ code files. Consider using \`/counsel <question>\` for multi-perspective analysis, or the \`agentctl-counsel\` tool for in-depth code review.`,
                60_000
              );
            }
          }
        }
      }
    },

    /**
     * Session events
     */
    event: async ({ event }: { event: { type: string; properties?: Record<string, unknown> } }): Promise<void> => {
      switch (event.type) {
        case "session.created": {
          // Restore session context from previous compaction/session
          const { sessionID } = await getSessionIdentity(workspace);
          if (sessionID) {
            runSkill<{
              hook_output?: { context?: string; reason?: string };
            }>(
              "session/restore",
              {
                trigger: "session_start",
                workspace,
                session_id: sessionID,
              },
              { workspace, ephemeral: true, timeout: 10000 }
            )
              .then(async (result) => {
                const context = result.data?.hook_output?.context;
                if (result.success && context) {
                  // Write to pending context so it gets injected in the next turn
                  await writePendingContext(
                    sessionID,
                    "Session Restore",
                    context,
                    5 * 60 * 1000 // 5 minutes TTL
                  );
                }
              })
              .catch((err) => console.error("[agentctl] session/restore error:", err));
          }
          break;
        }

        case "session.idle": {
          if (isOpenCodeInternalWorkspace(workspace)) {
            break;
          }

          const eventSessionID =
            (event.properties?.sessionID as string | undefined) ||
            (event.properties?.session_id as string | undefined);

          const { sessionID } = eventSessionID
            ? { sessionID: eventSessionID }
            : await getSessionIdentity(workspace);

          if (!sessionID) {
            break;
          }

          const hasTodoMode = await loadMode(todoModeBySession, sessionID, "todo");
          const hasAnchorMode = await loadMode(anchorModeBySession, sessionID, "anchor");
          if (!hasTodoMode && !hasAnchorMode) {
            break;
          }

          const captureIntervalMs = parseEnvInterval(
            "AGENTCTL_OPENCODE_IDLE_CAPTURE_MS",
            DEFAULT_IDLE_CAPTURE_INTERVAL_MS
          );
          const flushIntervalMs = parseEnvInterval(
            "AGENTCTL_OPENCODE_IDLE_FLUSH_MS",
            DEFAULT_IDLE_FLUSH_INTERVAL_MS
          );
          const planSyncIntervalMs = parseEnvInterval(
            "AGENTCTL_OPENCODE_IDLE_PLAN_SYNC_MS",
            DEFAULT_IDLE_PLAN_SYNC_INTERVAL_MS
          );
          const todoIntervalMs = parseEnvInterval(
            "AGENTCTL_OPENCODE_IDLE_TODO_MS",
            DEFAULT_IDLE_TODO_INTERVAL_MS
          );

          void runIdleAction(`${sessionID}:capture`, captureIntervalMs, async () => {
            await runSkill(
              "session/capture",
              { session_id: sessionID, workspace_root: workspace, status: "idle" },
              { workspace, ephemeral: true, timeout: 15000 }
            );
          }).catch((err) => console.error("[agentctl] Capture error:", err));

          void runIdleAction(`${sessionID}:flush`, flushIntervalMs, async () => {
            await runSkill(
              "embedding/worker",
              { batch_size: 50, max_duration: 60 },
              { workspace, ephemeral: true, timeout: 120000 }
            );
          }).catch((err) => console.error("[agentctl] Flush error:", err));

          void runIdleAction(`${sessionID}:plan_sync`, planSyncIntervalMs, async () => {
            await runSkill(
              "plan/sync",
              { session_id: sessionID, workspace_root: workspace },
              { workspace, ephemeral: true, timeout: 5000 }
            );
          }).catch((err) => console.error("[agentctl] Plan sync error:", err));

          // PageRank recalculation - keeps task scores up-to-date
          const pageRankIntervalMs = parseEnvInterval(
            "AGENTCTL_OPENCODE_IDLE_PAGERANK_MS",
            DEFAULT_IDLE_PLAN_SYNC_INTERVAL_MS // Same interval as plan sync
          );
          void runIdleAction(`${sessionID}:pagerank`, pageRankIntervalMs, async () => {
            await runSkill(
              "graph/pagerank",
              { workspace },
              { workspace, ephemeral: true, timeout: 30000 }
            );
          }).catch((err) => console.error("[agentctl] PageRank error:", err));

          await runIdleAction(`${sessionID}:todo`, todoIntervalMs, async () => {
            try {
              let anchorGoal = "";
              let anchorPending = "";
              if (hasAnchorMode) {
                const anchorResult = await runSkill<{
                  found: boolean;
                  anchor?: { main_prompt?: string; pending_question?: string };
                }>(
                  "session/anchor",
                  { operation: "get", workspace, session_id: sessionID },
                  { workspace, ephemeral: true, timeout: 2000 }
                );

                anchorGoal =
                  anchorResult.success ? anchorResult.data?.anchor?.main_prompt?.trim() ?? "" : "";
                anchorPending =
                  anchorResult.success
                    ? anchorResult.data?.anchor?.pending_question?.trim() ?? ""
                    : "";

                if (!anchorGoal) {
                  await clearMode(anchorModeBySession, sessionID, "anchor");
                }
              }

              if (anchorPending) {
                await maybePingOverseerForPendingQuestion(sessionID, workspace, anchorPending);
              }

              const todo = anchorGoal
                ? await computeTodoContinuation(sessionID, workspace, anchorGoal, anchorPending)
                : hasTodoMode
                  ? await computeTodoLite(sessionID, workspace)
                  : null;

              if (!todo) {
                return;
              }

              const now = Date.now();
              const prior = lastTodoContinuationBySession.get(sessionID);
              const shouldWrite =
                !prior || prior.digest !== todo.digest || now - prior.at > 5 * 60 * 1000;

              if (shouldWrite) {
                lastTodoContinuationBySession.set(sessionID, { at: now, digest: todo.digest });
                await writePendingContext(
                  sessionID,
                  "TODO Continuation",
                  todo.prompt,
                  60 * 60 * 1000
                );
              }
            } catch (err) {
              console.error("[agentctl] Todo continuation error:", err);
            }
          });

          break;
        }

        case "file.edited": {
          const filePath = event.properties?.path as string;
          if (filePath) {
            // Link file to active task via graph
            (async () => {
              try {
                const taskResult = await runSkill<{ task?: { id: string } }>(
                  "todo/manage",
                  { operation: "get_active", workspace_id: workspace },
                  { workspace, ephemeral: true, timeout: 2000 }
                );
                if (taskResult.success && taskResult.data?.task?.id) {
                  const taskId = taskResult.data.task.id;
                  // Add file node (idempotent)
                  await runSkill(
                    "graph/manage",
                    {
                      operation: "add_node",
                      node_id: `file:${filePath}`,
                      node_type: "file",
                      current_path: filePath,
                    },
                    { workspace, ephemeral: true, timeout: 2000 }
                  );
                  // Add edge from task to file
                  await runSkill(
                    "graph/manage",
                    {
                      operation: "add_edge",
                      from_id: `task:${taskId}`,
                      from_type: "task",
                      to_id: `file:${filePath}`,
                      to_type: "file",
                      edge_type: "modifies",
                    },
                    { workspace, ephemeral: true, timeout: 2000 }
                  );
                }
              } catch {
                // Fire-and-forget - ignore errors
              }
            })();
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
      await runSkill(
        "session/save",
        {
          trigger: "pre_compact",
          workspace,
          session_id: input.sessionID,
        },
        { workspace, ephemeral: true, timeout: 5000 }
      );

      await runSkill(
        "session/anchor",
        {
          operation: "bump_compaction",
          workspace,
          session_id: input.sessionID,
          trigger: "pre_compact",
        },
        { workspace, ephemeral: true, timeout: 3000 }
      );

      const anchorResult = await runSkill<{
        found: boolean;
        anchor?: {
          main_prompt?: string;
          pending_question?: string;
          compaction_count?: number;
        };
      }>(
        "session/anchor",
        { operation: "get", workspace, session_id: input.sessionID },
        { workspace, ephemeral: true, timeout: 3000 }
      );

      if (anchorResult.success && anchorResult.data?.anchor?.main_prompt) {
        const lines = [
          "## Session Anchor",
          `**Goal:** ${anchorResult.data.anchor.main_prompt}`,
        ];

        if (anchorResult.data.anchor.compaction_count != null) {
          lines.push(`**Compaction:** ${anchorResult.data.anchor.compaction_count}`);
        }
        if (anchorResult.data.anchor.pending_question) {
          lines.push(`**Pending:** ${anchorResult.data.anchor.pending_question}`);
        }

        output.context.push(lines.join("\n"));

        // Generate a compact seed prompt for the next context window.
        // Avoid CAS digests in this flow; `runSkill` adds --no-cas for session/summarize.
        await runSkill(
          "session/capture",
          { session_id: input.sessionID, workspace_root: workspace, status: "compacting" },
          { workspace, ephemeral: true, timeout: 15000 }
        );

        const seedQuery = [
          anchorResult.data.anchor.main_prompt,
          anchorResult.data.anchor.pending_question,
        ]
          .filter((s) => typeof s === "string" && s.trim().length > 0)
          .join("\n");

        const seedResult = await runSkill<{ seed_prompt?: string }>(
          "session/summarize",
          {
            session_id: input.sessionID,
            mode: "seed",
            query: seedQuery,
            seed_max_chars: 10000,
          },
          { workspace, ephemeral: true, timeout: 8000 }
        );

        if (seedResult.success && seedResult.data?.seed_prompt) {
          await writePendingContext(
            input.sessionID,
            "Session Seed",
            seedResult.data.seed_prompt,
            10 * 60 * 1000
          );
        }

        // Generate full session restore context for next turn
        // This provides rich context (past sessions, memories, todos) after compaction
        const restoreResult = await runSkill<{ hook_output?: { context?: string } }>(
          "session/restore",
          {
            workspace,
            session_id: input.sessionID,
          },
          { workspace, ephemeral: true, timeout: 30000 }
        );

        if (restoreResult.success && restoreResult.data?.hook_output?.context) {
          await writePendingContext(
            input.sessionID,
            "Session Restore",
            restoreResult.data.hook_output.context,
            10 * 60 * 1000
          );
        }
      }
    },
  };
};

export default AgentctlPlugin;
