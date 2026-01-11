import express from "express";
import cookieParser from "cookie-parser";
import cors from "cors";
import { execSync, spawn } from "child_process";
import { readdirSync, statSync, readFileSync, existsSync, writeFileSync, renameSync, unlinkSync } from "fs";
import { join, basename, dirname, resolve } from "path";
import { homedir } from "os";
import { fileURLToPath } from "url";

// Load .env file from project root (3 levels up from server/index.js)
const __dirname = dirname(fileURLToPath(import.meta.url));
const envPath = join(__dirname, "..", "..", "..", ".env");
if (existsSync(envPath)) {
  const envContent = readFileSync(envPath, "utf-8");
  for (const line of envContent.split("\n")) {
    const trimmed = line.trim();
    // Skip comments and empty lines
    if (!trimmed || trimmed.startsWith("#")) continue;
    // Skip export prefixed lines (just remove the prefix)
    const cleanLine = trimmed.startsWith("export ") ? trimmed.slice(7) : trimmed;
    const eqIndex = cleanLine.indexOf("=");
    if (eqIndex === -1) continue;
    const key = cleanLine.slice(0, eqIndex).trim();
    let value = cleanLine.slice(eqIndex + 1).trim();
    // Remove surrounding quotes if present
    if ((value.startsWith('"') && value.endsWith('"')) ||
        (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1);
    }
    // Only set if not already defined (env takes precedence)
    if (!process.env[key]) {
      process.env[key] = value;
    }
  }
  console.log(`Loaded environment from ${envPath}`);
}

// Default to disabling CAS auto-migrate for the server process to avoid log spam.
// Users can override by explicitly setting AGENTCTL_CAS_AUTO_MIGRATE.
if (!process.env.AGENTCTL_CAS_AUTO_MIGRATE) {
  process.env.AGENTCTL_CAS_AUTO_MIGRATE = "0";
}

// Detect Bun and use native sqlite driver if available
let Database;
if (typeof Bun !== "undefined") {
  const { Database: BunDatabase } = await import("bun:sqlite");
  // Wrapper to make bun:sqlite compatible with better-sqlite3's basic API
  Database = class extends BunDatabase {
    constructor(path, options) {
      super(path, options);
    }
    prepare(sql) {
      const stmt = super.prepare(sql);

      // better-sqlite3 .all() takes arguments directly
      const originalAll = stmt.all;
      stmt.all = function (...args) {
        return originalAll.apply(this, args);
      };

      // better-sqlite3 .get() takes arguments directly
      const originalGet = stmt.get;
      stmt.get = function (...args) {
        return originalGet.apply(this, args);
      };

      // better-sqlite3 .columns() returns an array of column objects
      stmt.columns = function () {
        return (stmt.columnNames || []).map(name => ({ name }));
      };

      return stmt;
    }
  };
} else {
  const { default: BetterSqlite } = await import("better-sqlite3");
  Database = BetterSqlite;
}

const app = express();
const PORT = process.env.PORT || 8090;
const AGENTCTL_HOME = process.env.AGENTCTL_HOME || join(homedir(), ".agentctl");
const WORKSPACE_COOKIE = "agentctl_workspace";

app.use(cors({ origin: true, credentials: true }));
app.use(cookieParser());
app.use(express.json());

function findGitRoot(startDir) {
  let dir = startDir;
  while (true) {
    if (existsSync(join(dir, ".git"))) {
      return dir;
    }
    const parent = dirname(dir);
    if (parent === dir) {
      return "";
    }
    dir = parent;
  }
}

// detectWorkspace returns the workspace root using a detection chain:
// 1. AGENTCTL_WORKSPACE - set by agentctl runner
// 2. CLAUDE_PROJECT_DIR - set by Claude Code
// 3. Git root detection from start dir
// 4. start dir
function detectWorkspace(startDir) {
  if (process.env.AGENTCTL_WORKSPACE) {
    return process.env.AGENTCTL_WORKSPACE;
  }
  if (process.env.CLAUDE_PROJECT_DIR) {
    return process.env.CLAUDE_PROJECT_DIR;
  }
  const gitRoot = findGitRoot(startDir);
  if (gitRoot) {
    return gitRoot;
  }
  return startDir;
}

const DEFAULT_WORKSPACE = detectWorkspace(resolve(process.cwd()));

// Helper: get workspace from cookie
function getWorkspace(req) {
  return req.cookies[WORKSPACE_COOKIE] || DEFAULT_WORKSPACE || "";
}

// Helper: run agentctl skill
// Uses AGENTCTL_BIN env var or falls back to repo-local bin/agentctl when available.
const DEFAULT_AGENTCTL_BIN = (() => {
  const candidate = join(__dirname, "..", "..", "..", "bin", "agentctl");
  if (existsSync(candidate)) {
    return candidate;
  }
  return "agentctl";
})();
const AGENTCTL_BIN = process.env.AGENTCTL_BIN || DEFAULT_AGENTCTL_BIN;
const CAS_DRIVER = (process.env.AGENTCTL_CAS_DRIVER || "sqlite").toLowerCase();

function readCASJSONFromSQLite(digest) {
  const dbPath = process.env.AGENTCTL_CAS_DB_PATH || join(AGENTCTL_HOME, "storage", "cas.db");
  const resolved = resolve(dbPath);
  if (!existsSync(resolved)) {
    return null;
  }
  let db;
  try {
    db = openDB(resolved, { readonly: true });
    const row = db.prepare("SELECT content FROM cas_objects WHERE digest = ?").get(digest);
    if (!row || row.content == null) {
      return null;
    }
    const raw = Buffer.from(row.content).toString("utf-8");
    return JSON.parse(raw);
  } catch (err) {
    console.error("Failed to read CAS SQLite JSON:", err.message);
    return null;
  } finally {
    if (db && typeof db.close === "function") {
      try {
        db.close();
      } catch {
        // Ignore close errors for read-only use.
      }
    }
  }
}

function readCASJSONFromFile(digest) {
  const casRoot = process.env.AGENTCTL_CAS_PATH || join(AGENTCTL_HOME, "cas");

  if (typeof digest !== "string" || !digest.startsWith("sha256:")) {
    return null;
  }

  const hex = digest.slice("sha256:".length);
  if (!/^[0-9a-f]{64}$/i.test(hex)) {
    return null;
  }

  const casPath = join(resolve(casRoot), "sha256", hex);
  if (!existsSync(casPath)) {
    return null;
  }
  try {
    const raw = readFileSync(casPath, "utf-8");
    return JSON.parse(raw);
  } catch (err) {
    console.error("Failed to parse CAS file JSON:", err.message);
    return null;
  }
}

function readCASJSON(digest) {
  if (!digest || typeof digest !== "string" || !digest.startsWith("sha256:")) {
    return null;
  }

  if (CAS_DRIVER !== "file") {
    const fromSQLite = readCASJSONFromSQLite(digest);
    if (fromSQLite) {
      return fromSQLite;
    }
  }

  return readCASJSONFromFile(digest);
}

function inflateTruncatedEnvelope(env) {
  if (!env || typeof env !== "object") {
    return env;
  }
  const meta = env.meta || {};
  const isTruncated = meta.truncated === true || meta.truncate_reason === "inline_output_kb";
  if (!isTruncated) {
    return env;
  }
  const digest = env.data?.artifact || meta.cas_digest;
  const inflated = readCASJSON(digest);
  return inflated || env;
}

function runSkill(skill, input) {
  try {
    const cmd = `${AGENTCTL_BIN} run ${skill} --input '${JSON.stringify(input)}'`;
    const result = execSync(cmd, {
      encoding: "utf-8",
      timeout: 30000,
    });
    const parsed = JSON.parse(result);
    return inflateTruncatedEnvelope(parsed);
  } catch (err) {
    console.error(`Skill ${skill} failed:`, err.message);
    return { error: err.message, data: {} };
  }
}

// Track running agent daemons
const runningDaemons = new Map(); // actor_id -> { pid, startedAt }

// Detect best available LLM provider from environment
// Priority: Anthropic OAuth (future) > OpenRouter > Groq > Gemini > OpenAI
// TODO: Implement Anthropic OAuth via Claude CLI wrapper for Max subscription users
// See: CLAUDE.md "Claude Max OAuth" section for details
// Provider configurations with their env keys and default models
// Models updated Jan 2026
const LLM_PROVIDERS = [
  {
    name: "openrouter",
    envKey: "OPENROUTER_API_KEY",
    modelEnv: "OPENROUTER_MODEL",
    // Free agentic/coding models: devstral, minimax-m2.1, mimo-v2-flash, deepseek-r1
    defaultModel: "minimax/minimax-m2.1", // Fast, efficient for coding/agents
  },
  {
    name: "groq",
    envKey: "GROQ_API_KEY",
    modelEnv: "GROQ_MODEL",
    defaultModel: "qwen/qwen3-32b", // Fast inference on Groq
  },
  {
    name: "gemini",
    envKey: "GEMINI_API_KEY",
    modelEnv: "GEMINI_MODEL",
    defaultModel: "gemini-3-flash-preview", // Latest as of Dec 2025
  },
  {
    name: "anthropic",
    envKey: "ANTHROPIC_API_KEY",
    modelEnv: "ANTHROPIC_MODEL",
    defaultModel: "claude-haiku-4-5", // Direct API (not Max subscription)
  },
  {
    name: "openai",
    envKey: "OPENAI_API_KEY",
    modelEnv: "OPENAI_MODEL",
    defaultModel: "gpt-5.2", // Latest for coding/agentic
  },
];

function normalizeLLMOverrides(meta) {
  if (!meta || typeof meta !== "object") {
    return { provider: undefined, model: undefined };
  }
  const provider =
    typeof meta.llm_provider === "string"
      ? meta.llm_provider.trim()
      : typeof meta.llmProvider === "string"
        ? meta.llmProvider.trim()
        : "";
  const model =
    typeof meta.llm_model === "string"
      ? meta.llm_model.trim()
      : typeof meta.llmModel === "string"
        ? meta.llmModel.trim()
        : "";
  return {
    provider: provider || undefined,
    model: model || undefined,
  };
}

function detectLLMProvider(preferredProvider, preferredModel) {
  // Explicit provider selection (from console meta)
  if (preferredProvider) {
    const match = LLM_PROVIDERS.find(p => p.name === preferredProvider);
    if (!match) {
      return { error: `Unknown LLM provider: ${preferredProvider}` };
    }
    const apiKey = process.env[match.envKey];
    if (!apiKey) {
      return { error: `Missing ${match.envKey} for provider ${preferredProvider}` };
    }
    const model = preferredModel || process.env[match.modelEnv] || match.defaultModel;
    return { provider: match.name, apiKey, model };
  }

  // Check for explicit provider override
  const explicitProvider = process.env.AGENTCTL_LLM_PROVIDER;
  const explicitKey = process.env.AGENTCTL_LLM_API_KEY;
  const explicitModel = process.env.AGENTCTL_LLM_MODEL;

  if (explicitProvider && explicitKey) {
    return {
      provider: explicitProvider,
      apiKey: explicitKey,
      model: preferredModel || explicitModel || LLM_PROVIDERS.find(p => p.name === explicitProvider)?.defaultModel || "default",
    };
  }

  // Auto-detect from available keys
  for (const p of LLM_PROVIDERS) {
    const apiKey = process.env[p.envKey];
    if (apiKey) {
      const model = preferredModel || process.env[p.modelEnv] || p.defaultModel;
      console.log(`Auto-detected LLM provider: ${p.name} (model: ${model})`);
      return {
        provider: p.name,
        apiKey,
        model,
      };
    }
  }

  return null; // No provider found
}

function stopAgentDaemon(actorId) {
  const running = runningDaemons.get(actorId);
  if (!running?.pid) {
    return false;
  }
  try {
    process.kill(running.pid);
  } catch (err) {
    console.warn(`Failed to stop daemon for ${actorId}:`, err?.message || err);
    return false;
  }
  runningDaemons.delete(actorId);
  return true;
}

// Ensure agent exists in agents.db and spawn daemon if needed
function ensureAgentDaemon(actorId, workspace = "", meta = null) {
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");
  let db = null;
  const overrides = normalizeLLMOverrides(meta);
  const hasOverride = Boolean(overrides.provider || overrides.model);

  try {
    // Open with write access
    db = new Database(dbPath);

    // Ensure agents table exists
    db.exec(`
      CREATE TABLE IF NOT EXISTS agents (
        id           TEXT PRIMARY KEY,
        parent_id    TEXT,
        ns           TEXT UNIQUE NOT NULL,
        role         TEXT,
        prompt       TEXT,
        skills_allow TEXT NOT NULL,
        policy       TEXT NOT NULL,
        share_bb     TEXT NOT NULL CHECK (share_bb IN ('all','scoped','none')),
        state        TEXT NOT NULL CHECK (state IN ('starting','running','stopped','error')),
        created_at   TEXT NOT NULL,
        heartbeat_at TEXT,
        llm_provider TEXT,
        llm_model    TEXT
      );
      CREATE INDEX IF NOT EXISTS idx_agents_ns ON agents(ns);
      CREATE INDEX IF NOT EXISTS idx_agents_parent ON agents(parent_id);
      CREATE INDEX IF NOT EXISTS idx_agents_state ON agents(state);
    `);

    // Never persist API keys in the agents DB.
    try {
      db.prepare("UPDATE agents SET llm_api_key = NULL WHERE llm_api_key IS NOT NULL").run();
    } catch {
      // Ignore if column doesn't exist (new schema) or DB is read-only.
    }

    // Check if agent exists for this namespace
    const existing = db.prepare(`
      SELECT id, state, heartbeat_at, llm_provider, llm_model
      FROM agents WHERE ns = ?
    `).get(actorId);

    if (existing) {
      let llmConfig = null;
      if (hasOverride) {
        const providerHint = overrides.provider || (overrides.model && existing.llm_provider ? existing.llm_provider : undefined);
        llmConfig = detectLLMProvider(providerHint, overrides.model);
        if (!llmConfig || llmConfig.error) {
          return { error: llmConfig?.error || "No LLM provider configured. Set an API key in .env or environment." };
        }
      }

      const running = runningDaemons.has(actorId);
      const heartbeatAt = existing.heartbeat_at ? Date.parse(existing.heartbeat_at) : 0;
      const heartbeatFresh = heartbeatAt > 0 && Date.now() - heartbeatAt < 60000;
      const isHealthy = running || (existing.state === "running" && heartbeatFresh);
      const needsUpdate = llmConfig && (existing.llm_provider !== llmConfig.provider || existing.llm_model !== llmConfig.model);

      // Agent exists - check if daemon is running or heartbeat is fresh
      if (!needsUpdate && isHealthy) {
        db.close();
        db = null;
        return { agentId: existing.id, status: "already_running" };
      }

      if (needsUpdate && running) {
        stopAgentDaemon(actorId);
      }

      // Agent exists but daemon not running (or stale heartbeat) - restart
      if (llmConfig) {
        db.prepare(`
          UPDATE agents
          SET state = 'starting',
              heartbeat_at = ?,
               llm_provider = ?,
               llm_model = ?
          WHERE id = ?
        `).run(new Date().toISOString(), llmConfig.provider, llmConfig.model, existing.id);
      } else {
        db.prepare(`UPDATE agents SET state = 'starting', heartbeat_at = ? WHERE id = ?`)
          .run(new Date().toISOString(), existing.id);
      }
      db.close();
      db = null;
      spawnAgentDaemon(existing.id, actorId);
      return { agentId: existing.id, status: "daemon_spawned" };
    }

    // Detect LLM provider
    const llmConfig = detectLLMProvider(overrides.provider, overrides.model);
    if (!llmConfig || llmConfig.error) {
      const message =
        llmConfig?.error ||
        "No LLM provider configured. Set GROQ_API_KEY, OPENROUTER_API_KEY, GEMINI_API_KEY, or AGENTCTL_LLM_API_KEY";
      console.warn(message);
      return { error: llmConfig?.error || "No LLM provider configured. Set an API key in .env or environment." };
    }

    // Create new agent record
    const agentId = generateId();
    const now = new Date().toISOString();
    const defaultPolicy = JSON.stringify({
      max_turns: 50,
      max_output_kb: 1024,
      timeout: "10m",
      filesystem: [{ type: "workspace", path: workspace || "." }],
    });

    db.prepare(`
      INSERT INTO agents (id, parent_id, ns, role, prompt, skills_allow, policy, share_bb, state, created_at, heartbeat_at, llm_provider, llm_model)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).run(
      agentId,
      null, // parent_id
      actorId, // ns = actor_id
      "console", // role
      "You are an interactive console agent. Help the user with their queries.", // prompt
      JSON.stringify(["*"]), // skills_allow - all skills
      defaultPolicy,
      "scoped", // share_bb
      "starting", // state
      now,
      null, // heartbeat_at
      llmConfig.provider,
      llmConfig.model
    );

    db.close();
    db = null;

    console.log(`Created agent ${agentId} with provider: ${llmConfig.provider}, model: ${llmConfig.model}`);

    // Spawn daemon for the new agent
    spawnAgentDaemon(agentId, actorId);

    return { agentId, status: "created_and_spawned" };
  } catch (err) {
    console.error(`ensureAgentDaemon failed:`, err.message);
    return { error: err.message };
  } finally {
    // Ensure database is closed even on error
    if (db) {
      try {
        db.close();
      } catch {
        // Ignore close errors
      }
    }
  }
}

// Spawn agent daemon as background process
function spawnAgentDaemon(agentId, actorId) {
  // Check if already running
  if (runningDaemons.has(actorId)) {
    console.log(`Daemon already running for ${actorId}`);
    return;
  }

  try {
    console.log(`Spawning daemon for agent ${agentId} (${actorId})`);

    // Spawn detached process
    const daemonProc = spawn(AGENTCTL_BIN, ["agent", "run", agentId], {
      detached: true,
      stdio: ["ignore", "pipe", "pipe"],
      env: { ...process.env },
    });

    // Track the process
    runningDaemons.set(actorId, {
      pid: daemonProc.pid,
      agentId,
      startedAt: new Date().toISOString(),
    });

    // Log output for debugging
    daemonProc.stdout?.on("data", (data) => {
      console.log(`[daemon:${actorId}] ${data.toString().trim()}`);
    });
    daemonProc.stderr?.on("data", (data) => {
      console.log(`[daemon:${actorId}:err] ${data.toString().trim()}`);
    });

    // Clean up on exit
    daemonProc.on("exit", (code, signal) => {
      console.log(`Daemon ${actorId} exited with code ${code}, signal ${signal}`);
      runningDaemons.delete(actorId);
    });

    daemonProc.on("error", (err) => {
      console.error(`Daemon ${actorId} error:`, err);
      runningDaemons.delete(actorId);
    });

    // Unref so the parent process can exit independently
    daemonProc.unref();

    console.log(`Daemon spawned for ${actorId} with PID ${daemonProc.pid}`);
  } catch (err) {
    console.error(`Failed to spawn daemon for ${actorId}:`, err);
  }
}

// Helper: open SQLite database
// Note: bun:sqlite has issues with WAL mode databases in readonly mode,
// so we open without readonly when using Bun. The API only reads data.
const isBun = typeof Bun !== "undefined";
function openDB(dbPath, options = {}) {
  const { readonly = !isBun } = options;
  if (isBun) {
    return new Database(dbPath);
  }
  return new Database(dbPath, { readonly });
}

// Known databases
const knownDatabases = {
  "tasks.db": "Tasks",
  "agents.db": "Agents",
  "jobs.db": "Jobs",
  "blackboard.db": "Blackboard",
  "board.db": "Board",
  "mailbox.db": "Mailbox",
  "memory.db": "Memory",
  "knowledge.db": "Knowledge",
  "trajectory.db": "Trajectory",
  "cache.db": "Cache",
  "sessions.db": "Sessions",
  "graph.db": "Graph",
};

// Discover SQLite databases
function discoverDatabases() {
  const searchDirs = [
    AGENTCTL_HOME,
    join(AGENTCTL_HOME, "cache"),
    join(AGENTCTL_HOME, "storage"),
  ];

  const dbByName = new Map();

  for (const dir of searchDirs) {
    try {
      const entries = readdirSync(dir);
      for (const entry of entries) {
        if (!entry.endsWith(".db")) continue;
        const fullPath = join(dir, entry);
        const stat = statSync(fullPath);
        if (!stat.isFile()) continue;

        const dbName = entry.replace(".db", "");
        const friendlyName = knownDatabases[entry] || dbName;

        dbByName.set(dbName, {
          name: dbName,
          friendly_name: friendlyName,
          path: fullPath,
          size: stat.size,
        });
      }
    } catch {
      // Directory doesn't exist
    }
  }

  return Array.from(dbByName.values()).sort((a, b) =>
    a.friendly_name.localeCompare(b.friendly_name)
  );
}

// Validate table name to prevent SQL injection
function validateTableName(tableName) {
  // Only allow alphanumeric, underscore, and hyphen
  if (!/^[a-zA-Z0-9_-]+$/.test(tableName)) {
    throw new Error("Invalid table name");
  }
  return tableName;
}

// Safe regex creation with complexity limit
function createSafeRegex(pattern, flags = "i") {
  // Limit pattern length to prevent ReDoS
  if (pattern.length > 500) {
    throw new Error("Pattern too long (max 500 characters)");
  }
  // Check for known ReDoS patterns (nested quantifiers)
  if (/(\+|\*|\?)\s*\1/.test(pattern) || /\([^)]*(\+|\*)[^)]*\)\+/.test(pattern)) {
    throw new Error("Pattern contains potentially dangerous constructs");
  }
  return new RegExp(pattern, flags);
}

// Atomic file write - write to temp then rename
function atomicWriteFileSync(filePath, content) {
  const tempPath = filePath + ".tmp." + process.pid;
  try {
    writeFileSync(tempPath, content);
    renameSync(tempPath, filePath);
  } catch (err) {
    // Clean up temp file on error
    try {
      unlinkSync(tempPath);
    } catch { }
    throw err;
  }
}

// API Routes

// Jobs
app.get("/api/jobs", (req, res) => {
  const workspace = getWorkspace(req);
  const state = req.query.state || "";
  const limit = parseInt(req.query.limit) || 50;

  const jobsDir = join(AGENTCTL_HOME, "jobs");
  const jobs = [];

  try {
    const entries = readdirSync(jobsDir).sort().reverse().slice(0, limit * 2);

    for (const jobId of entries) {
      const jobPath = join(jobsDir, jobId);
      try {
        const stat = statSync(jobPath);
        if (!stat.isDirectory()) continue;

        // Check workspace filter
        if (workspace) {
          const wsFile = join(jobPath, "workspace");
          if (existsSync(wsFile)) {
            const jobWs = readFileSync(wsFile, "utf-8").trim();
            if (jobWs && jobWs !== workspace) continue;
          }
        }

        const resultFile = join(jobPath, "result.json");
        if (!existsSync(resultFile)) continue;

        const result = JSON.parse(readFileSync(resultFile, "utf-8"));

        // Filter by state
        if (state && result.status !== state) continue;

        jobs.push({
          id: jobId,
          command: result.command || "",
          type: "skill",
          category: "",
          skill: result.command?.split("/")[1] || "",
          state: result.status || "unknown",
          created_at: result.meta?.ts || "",
          error: result.error?.message || "",
        });

        if (jobs.length >= limit) break;
      } catch {
        // Skip malformed jobs
      }
    }
  } catch {
    // Jobs dir doesn't exist
  }

  res.json({ jobs });
});

// Job detail
app.get("/api/jobs/:id", (req, res) => {
  const jobPath = join(AGENTCTL_HOME, "jobs", req.params.id);

  try {
    const resultFile = join(jobPath, "result.json");
    const result = JSON.parse(readFileSync(resultFile, "utf-8"));

    let stderr = "";
    const stderrFile = join(jobPath, "stderr.log");
    if (existsSync(stderrFile)) {
      stderr = readFileSync(stderrFile, "utf-8");
    }

    res.json({
      id: req.params.id,
      command: result.command || "",
      type: "skill",
      category: "",
      skill: result.command?.split("/")[1] || "",
      state: result.status || "unknown",
      created_at: result.meta?.ts || "",
      error: result.error?.message || "",
      result_data: result.data,
      stderr,
    });
  } catch {
    res.status(404).json({ error: "Job not found" });
  }
});

// Tasks - Direct SQLite read (fast)
app.get("/api/tasks", (req, res) => {
  const workspace = getWorkspace(req);
  const limit = parseInt(req.query.limit) || 50;
  const includeMetrics = req.query.metrics !== "false";

  const tasksDB = join(AGENTCTL_HOME, "storage", "tasks.db");
  const graphDB = join(AGENTCTL_HOME, "storage", "graph.db");

  if (!existsSync(tasksDB)) {
    return res.json({ tasks: [] });
  }

  try {
    const db = openDB(tasksDB);

    let query = `
      SELECT id, title, description, status, scope_path, parent_id,
             depends_on, created_at, completed_at, notes, session_id
      FROM tasks`;
    const params = [];

    if (workspace) {
      query += ` WHERE workspace_id = ?`;
      params.push(workspace);
    }
    query += ` ORDER BY created_at DESC LIMIT ?`;
    params.push(limit);

    let tasks = db.prepare(query).all(...params);
    db.close();

    tasks = tasks.map((t) => ({
      ...t,
      depends_on: t.depends_on ? JSON.parse(t.depends_on) : [],
    }));

    if (includeMetrics && existsSync(graphDB)) {
      try {
        const gdb = openDB(graphDB);
        const nodeMap = new Map();

        let gquery = `
          SELECT node_id, pagerank, in_degree, out_degree
          FROM graph_nodes
          WHERE node_type = 'task'`;
        const gparams = [];

        if (workspace) {
          gquery += ` AND workspace = ?`;
          gparams.push(workspace);
        }

        const nodes = gdb.prepare(gquery).all(...gparams);
        for (const n of nodes) {
          const taskId = n.node_id.replace(/^task:/, "");
          nodeMap.set(taskId, n);
        }
        gdb.close();

        tasks = tasks.map((t) => {
          const metrics = nodeMap.get(t.id);
          return {
            ...t,
            pagerank: metrics?.pagerank || 0,
            in_degree: metrics?.in_degree || 0,
            out_degree: metrics?.out_degree || 0,
          };
        });
      } catch (e) {
        console.warn("Failed to load graph metrics:", e.message);
      }
    }

    res.json({ tasks });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Stats - Direct SQLite read (fast)
app.get("/api/stats", (req, res) => {
  const workspace = getWorkspace(req);
  const tasksDB = join(AGENTCTL_HOME, "storage", "tasks.db");

  let taskStats = { total: 0, pending: 0, in_progress: 0, completed: 0 };

  if (existsSync(tasksDB)) {
    try {
      const db = openDB(tasksDB);

      let countQuery = `
        SELECT status, COUNT(*) as cnt
        FROM tasks`;
      const params = [];

      if (workspace) {
        countQuery += ` WHERE workspace_id = ?`;
        params.push(workspace);
      }
      countQuery += ` GROUP BY status`;

      const rows = db.prepare(countQuery).all(...params);
      db.close();

      for (const r of rows) {
        taskStats.total += r.cnt;
        if (r.status === "pending") taskStats.pending = r.cnt;
        else if (r.status === "in_progress") taskStats.in_progress = r.cnt;
        else if (r.status === "completed") taskStats.completed = r.cnt;
      }
    } catch (e) {
      console.warn("Failed to load task stats:", e.message);
    }
  }

  // Count jobs with state breakdown
  const jobsDir = join(AGENTCTL_HOME, "jobs");
  let jobTotal = 0;
  const byState = {};
  const byCommand = {};
  let lastHour = 0;
  let lastDay = 0;
  const oneHourAgo = Date.now() - 60 * 60 * 1000;
  const oneDayAgo = Date.now() - 24 * 60 * 60 * 1000;

  try {
    const entries = readdirSync(jobsDir).sort().reverse().slice(0, 500);
    jobTotal = entries.length;

    for (const jobId of entries.slice(0, 200)) {
      try {
        const resultFile = join(jobsDir, jobId, "result.json");
        if (!existsSync(resultFile)) continue;

        const result = JSON.parse(readFileSync(resultFile, "utf-8"));
        const state = result.status || "unknown";
        byState[state] = (byState[state] || 0) + 1;

        const cmd = result.command?.split("/")[1] || "unknown";
        byCommand[cmd] = (byCommand[cmd] || 0) + 1;

        const ts = result.meta?.ts ? new Date(result.meta.ts).getTime() : 0;
        if (ts > oneHourAgo) lastHour++;
        if (ts > oneDayAgo) lastDay++;
      } catch {
        // Skip malformed
      }
    }
  } catch { }

  res.json({
    total: jobTotal,
    by_state: byState,
    by_command: byCommand,
    recent: { last_hour: lastHour, last_day: lastDay },
    task_stats: taskStats,
  });
});

// Insights - Direct SQLite with JS-computed critical_path_score
app.get("/api/insights", (req, res) => {
  const workspace = getWorkspace(req);

  const graphDB = join(AGENTCTL_HOME, "storage", "graph.db");
  const tasksDB = join(AGENTCTL_HOME, "storage", "tasks.db");

  if (!existsSync(tasksDB)) {
    return res.json({ nodes: [], cycles: [], topological_order: [] });
  }

  try {
    // Get tasks with dependencies
    const tdb = openDB(tasksDB);
    let taskQuery = `SELECT id, title, status, depends_on FROM tasks`;
    const taskParams = [];
    if (workspace) {
      taskQuery += ` WHERE workspace_id = ?`;
      taskParams.push(workspace);
    }
    const tasks = tdb.prepare(taskQuery).all(...taskParams);
    tdb.close();

    // Build adjacency list (task -> its dependencies)
    const adjList = new Map(); // task_id -> [dependency_ids]
    for (const t of tasks) {
      const deps = t.depends_on ? JSON.parse(t.depends_on) : [];
      adjList.set(t.id, deps);
    }

    // Get PageRank from graph.db if available
    const pagerankMap = new Map();
    if (existsSync(graphDB)) {
      try {
        const gdb = openDB(graphDB);
        let prQuery = `SELECT node_id, pagerank, in_degree, out_degree FROM graph_nodes WHERE node_type = 'task'`;
        const prParams = [];
        if (workspace) {
          prQuery += ` AND workspace = ?`;
          prParams.push(workspace);
        }
        const prNodes = gdb.prepare(prQuery).all(...prParams);
        gdb.close();
        for (const n of prNodes) {
          const taskId = n.node_id.replace(/^task:/, "");
          pagerankMap.set(taskId, n);
        }
      } catch { }
    }

    // Compute critical_path_score: longest path to any sink via memoized DFS
    const memo = new Map();
    const visiting = new Set();

    function computeCriticalPath(taskId) {
      if (memo.has(taskId)) return memo.get(taskId);
      if (visiting.has(taskId)) return 0; // Cycle detected

      visiting.add(taskId);
      const deps = adjList.get(taskId) || [];

      if (deps.length === 0) {
        memo.set(taskId, 0);
        visiting.delete(taskId);
        return 0;
      }

      let maxPath = 0;
      for (const depId of deps) {
        if (adjList.has(depId)) {
          const pathLen = computeCriticalPath(depId) + 1;
          if (pathLen > maxPath) maxPath = pathLen;
        }
      }

      memo.set(taskId, maxPath);
      visiting.delete(taskId);
      return maxPath;
    }

    // Build nodes with all metrics
    const nodes = tasks.map((t) => {
      const pr = pagerankMap.get(t.id) || {};
      return {
        task_id: t.id,
        title: t.title,
        status: t.status,
        pagerank: pr.pagerank || 0,
        critical_path_score: computeCriticalPath(t.id),
        in_degree: pr.in_degree || 0,
        out_degree: pr.out_degree || 0,
      };
    });

    // Sort by critical_path_score desc, then pagerank desc
    nodes.sort((a, b) => {
      if (a.critical_path_score !== b.critical_path_score) {
        return b.critical_path_score - a.critical_path_score;
      }
      return b.pagerank - a.pagerank;
    });

    // Topological order via Kahn's algorithm
    const inDegree = new Map();
    const graph = new Map();
    for (const t of tasks) {
      inDegree.set(t.id, 0);
      graph.set(t.id, []);
    }
    for (const t of tasks) {
      const deps = adjList.get(t.id) || [];
      for (const depId of deps) {
        if (graph.has(depId)) {
          graph.get(depId).push(t.id);
          inDegree.set(t.id, (inDegree.get(t.id) || 0) + 1);
        }
      }
    }

    const queue = [];
    for (const [id, deg] of inDegree) {
      if (deg === 0) queue.push(id);
    }

    const topological_order = [];
    const cycles = [];
    while (queue.length > 0) {
      const curr = queue.shift();
      topological_order.push(curr);
      for (const next of graph.get(curr) || []) {
        inDegree.set(next, inDegree.get(next) - 1);
        if (inDegree.get(next) === 0) queue.push(next);
      }
    }

    // If not all nodes processed, there are cycles
    if (topological_order.length < tasks.length) {
      const processed = new Set(topological_order);
      const cycleNodes = tasks.filter((t) => !processed.has(t.id)).map((t) => t.id);
      if (cycleNodes.length > 0) cycles.push(cycleNodes);
    }

    res.json({ nodes, cycles, topological_order });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Mailbox - Direct SQLite read (fast)
app.get("/api/mailbox", (req, res) => {
  const actor = req.query.actor || "admin";
  const workspace = getWorkspace(req);
  const limit = parseInt(req.query.limit) || 50;
  const now = Date.now();

  const mailboxDB = join(AGENTCTL_HOME, "storage", "mailbox.db");
  if (!existsSync(mailboxDB)) {
    return res.json({ messages: [] });
  }

  try {
    const db = openDB(mailboxDB);

    let query = `
      SELECT id, from_ns, to_ns, type, headers, payload, ts, session_id, workspace, agent_id
      FROM mailbox
      WHERE to_ns = ? AND visible_at <= ?`;
    const params = [actor, now];

    if (workspace) {
      query += ` AND (workspace = ? OR workspace IS NULL)`;
      params.push(workspace);
    }
    query += ` ORDER BY ts DESC LIMIT ?`;
    params.push(limit);

    const messages = db.prepare(query).all(...params).map((m) => ({
      id: m.id,
      from: m.from_ns,
      to: m.to_ns,
      type: m.type,
      headers: m.headers ? JSON.parse(m.headers) : {},
      payload: m.payload ? JSON.parse(m.payload) : {},
      ts: m.ts,
      session_id: m.session_id,
      workspace: m.workspace,
      agent_id: m.agent_id,
    }));

    db.close();
    res.json({ messages });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Reservations - Direct SQLite read (fast)
app.get("/api/reservations", (req, res) => {
  const workspace = getWorkspace(req);
  const now = Date.now();

  const boardDB = join(AGENTCTL_HOME, "storage", "board.db");
  if (!existsSync(boardDB)) {
    return res.json({ reservations: [] });
  }

  try {
    const db = openDB(boardDB);

    let query = `
      SELECT id, workspace_id, task_id, path, holder, mode, reason, expires_at, created_at
      FROM file_reservations
      WHERE expires_at > ?`;
    const params = [now];

    if (workspace) {
      query += ` AND workspace_id = ?`;
      params.push(workspace);
    }
    query += ` ORDER BY created_at DESC`;

    const reservations = db.prepare(query).all(...params).map((r) => ({
      id: r.id,
      workspace_id: r.workspace_id,
      task_id: r.task_id,
      path: r.path,
      holder: r.holder,
      mode: r.mode,
      reason: r.reason,
      expires_at: r.expires_at,
      created_at: r.created_at,
    }));

    db.close();
    res.json({ reservations });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Blackboard
app.get("/api/blackboard", (req, res) => {
  const ns = req.query.ns || "";
  const topic = req.query.topic || "";
  const limit = Math.min(parseInt(req.query.limit) || 50, 200);

  const blackboardDB = join(AGENTCTL_HOME, "storage", "blackboard.db");
  if (!existsSync(blackboardDB)) {
    return res.json({ records: [] });
  }

  try {
    const db = openDB(blackboardDB);
    let query = `
      SELECT id, ns, topic, ts, ttl_sec, payload
      FROM blackboard
      WHERE 1=1`;
    const params = [];

    if (ns) {
      query += ` AND ns = ?`;
      params.push(ns);
    }
    if (topic) {
      query += ` AND topic = ?`;
      params.push(topic);
    }

    query += ` ORDER BY ts DESC LIMIT ?`;
    params.push(limit);

    const records = db.prepare(query).all(...params).map((row) => ({
      id: row.id,
      ns: row.ns,
      topic: row.topic,
      ts: row.ts,
      ttl_sec: row.ttl_sec,
      payload: row.payload || "",
    }));

    db.close();
    res.json({ records });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// SQLite - list databases
app.get("/api/sqlite", (req, res) => {
  const databases = discoverDatabases();
  res.json({ databases });
});

// SQLite - list tables
app.get("/api/sqlite/:db", (req, res) => {
  const databases = discoverDatabases();
  const dbInfo = databases.find((d) => d.name === req.params.db);

  if (!dbInfo) {
    return res.status(404).json({ error: "Database not found" });
  }

  try {
    const db = openDB(dbInfo.path);
    const tables = db
      .prepare(
        `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`
      )
      .all();

    const result = tables.map((t) => {
      let rowCount = 0;
      try {
        rowCount = db.prepare(`SELECT COUNT(*) as c FROM "${t.name}"`).get().c;
      } catch { }
      return { name: t.name, row_count: rowCount };
    });

    db.close();
    res.json({ tables: result });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// SQLite - table data
app.get("/api/sqlite/:db/:table", (req, res) => {
  const databases = discoverDatabases();
  const dbInfo = databases.find((d) => d.name === req.params.db);

  if (!dbInfo) {
    return res.status(404).json({ error: "Database not found" });
  }

  const limit = Math.min(parseInt(req.query.limit) || 100, 1000);
  const offset = parseInt(req.query.offset) || 0;

  try {
    const db = openDB(dbInfo.path);
    const tableName = validateTableName(req.params.table);

    // Get columns
    const columns = db.prepare(`PRAGMA table_info("${tableName}")`).all();
    const columnNames = columns.map((c) => c.name);

    // Get rows
    const rows = db
      .prepare(`SELECT * FROM "${tableName}" LIMIT ? OFFSET ?`)
      .all(limit, offset);

    // Get total count
    const total = db
      .prepare(`SELECT COUNT(*) as c FROM "${tableName}"`)
      .get().c;

    db.close();
    res.json({ columns: columnNames, rows, total });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// SQLite - table schema
app.get("/api/sqlite/:db/:table/schema", (req, res) => {
  const databases = discoverDatabases();
  const dbInfo = databases.find((d) => d.name === req.params.db);

  if (!dbInfo) {
    return res.status(404).json({ error: "Database not found" });
  }

  try {
    const db = openDB(dbInfo.path);
    const tableName = validateTableName(req.params.table);
    const columns = db
      .prepare(`PRAGMA table_info("${tableName}")`)
      .all();

    const result = columns.map((c) => ({
      name: c.name,
      type: c.type,
      not_null: c.notnull === 1,
      default_value: c.dflt_value,
      is_pk: c.pk > 0,
    }));

    db.close();
    res.json({ columns: result });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// SQLite - indexes
app.get("/api/sqlite/:db/indexes", (req, res) => {
  const databases = discoverDatabases();
  const dbInfo = databases.find((d) => d.name === req.params.db);

  if (!dbInfo) {
    return res.status(404).json({ error: "Database not found" });
  }

  try {
    const db = openDB(dbInfo.path);
    const indexes = db
      .prepare(
        `SELECT name, tbl_name, sql FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%'`
      )
      .all();

    const result = indexes.map((idx) => ({
      name: idx.name,
      table: idx.tbl_name,
      unique: (idx.sql || "").toUpperCase().includes("UNIQUE"),
      sql: idx.sql,
    }));

    db.close();
    res.json({ indexes: result });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// SQLite - query
app.post("/api/sqlite/:db/query", (req, res) => {
  const databases = discoverDatabases();
  const dbInfo = databases.find((d) => d.name === req.params.db);

  if (!dbInfo) {
    return res.status(404).json({ error: "Database not found" });
  }

  const { query } = req.body;
  if (!query) {
    return res.status(400).json({ error: "Query required" });
  }

  // Block write operations - strip comments and normalize whitespace
  // Remove SQL comments (both -- and /* */ style)
  let normalizedQuery = query
    .replace(/--.*$/gm, "")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .trim()
    .toUpperCase();

  // Remove leading whitespace/newlines
  normalizedQuery = normalizedQuery.replace(/^\s+/, "");

  const dangerous = [
    "INSERT",
    "UPDATE",
    "DELETE",
    "DROP",
    "CREATE",
    "ALTER",
    "ATTACH",
    "DETACH",
    "REINDEX",
    "VACUUM",
    "PRAGMA",  // Block PRAGMA as it can modify DB
  ];

  // Check if query starts with dangerous keywords (including after WITH clause)
  const startsWithDangerous = dangerous.some((kw) => normalizedQuery.startsWith(kw));
  // Also check for CTEs: WITH ... DELETE/INSERT/UPDATE
  const hasDangerousInCTE = normalizedQuery.startsWith("WITH") &&
    dangerous.some((kw) => normalizedQuery.includes(`) ${kw}`) || normalizedQuery.includes(`)${kw}`));

  if (startsWithDangerous || hasDangerousInCTE) {
    return res.status(400).json({ error: "Write operations not allowed" });
  }

  try {
    const db = openDB(dbInfo.path);
    const stmt = db.prepare(query);
    const rows = stmt.all();
    const columns = stmt.columns().map((c) => c.name);

    db.close();
    res.json({ columns, rows });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Search
app.get("/api/search", (req, res) => {
  const q = req.query.q || "";
  const limit = parseInt(req.query.limit) || 50;
  const rerank = req.query.rerank === "true";
  const scope = req.query.scope || "";

  if (!q) {
    return res.json({ results: [], stats: {} });
  }

  const input = { query: q, limit, rerank };
  if (scope) input.scope = scope;

  const result = runSkill("code/semantic_search", input);
  res.json({
    results: result.data?.results || [],
    stats: result.data?.stats || {},
  });
});

// Workspaces
app.get("/api/workspaces", (req, res) => {
  const current = getWorkspace(req);

  // Query sessions database for workspaces
  const sessionsDB = join(AGENTCTL_HOME, "storage", "sessions.db");
  let workspaces = [];

  try {
    const db = openDB(sessionsDB);
    const rows = db
      .prepare(
        `
      SELECT workspace_path, COUNT(*) as session_count, MAX(started_at) as last_used
      FROM sessions
      WHERE workspace_path != '' AND workspace_path NOT LIKE '/tmp/%'
      GROUP BY workspace_path
      ORDER BY last_used DESC
    `
      )
      .all();

    workspaces = rows.map((r) => ({
      path: r.workspace_path,
      name: basename(r.workspace_path),
      session_count: r.session_count,
      last_used: r.last_used,
    }));

    db.close();
  } catch {
    // Sessions DB doesn't exist
  }

  res.json({ workspaces, current });
});

// Switch workspace
app.post("/api/workspaces/switch", (req, res) => {
  const workspace = req.query.workspace || "";

  res.cookie(WORKSPACE_COOKIE, workspace, {
    path: "/",
    maxAge: 365 * 24 * 60 * 60 * 1000, // 1 year
    httpOnly: true,
    sameSite: "lax",
  });

  res.json({ success: true });
});

// Sessions - list all sessions for workspace
app.get("/api/sessions", (req, res) => {
  const workspace = getWorkspace(req);
  const limit = Math.min(parseInt(req.query.limit) || 50, 200);
  const offset = parseInt(req.query.offset) || 0;

  const sessionsDB = join(AGENTCTL_HOME, "storage", "sessions.db");

  try {
    const db = openDB(sessionsDB);

    let query = `
      SELECT id, workspace_path, project_name, git_branch, started_at, ended_at,
             summary, accomplished, message_count, user_turns, tool_invocations,
             raw_jsonl_path, status, agent_id
      FROM sessions
      WHERE 1=1
    `;
    const params = [];

    if (workspace) {
      query += ` AND workspace_path = ?`;
      params.push(workspace);
    }

    query += ` ORDER BY started_at DESC LIMIT ? OFFSET ?`;
    params.push(limit, offset);

    const sessions = db.prepare(query).all(...params);

    // Get total count
    let countQuery = `SELECT COUNT(*) as total FROM sessions WHERE 1=1`;
    const countParams = [];
    if (workspace) {
      countQuery += ` AND workspace_path = ?`;
      countParams.push(workspace);
    }
    const total = db.prepare(countQuery).get(...countParams).total;

    db.close();
    res.json({ sessions, total, limit, offset });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Sessions - regex search across sessions (must be before :id route)
app.get("/api/sessions/search", (req, res) => {
  const workspace = getWorkspace(req);
  const pattern = req.query.pattern || "";
  const limit = Math.min(parseInt(req.query.limit) || 20, 100);

  if (!pattern) {
    return res.json({ results: [], total: 0 });
  }

  const sessionsDB = join(AGENTCTL_HOME, "storage", "sessions.db");

  try {
    const regex = createSafeRegex(pattern, "i");
    const db = openDB(sessionsDB);

    let query = `SELECT id, workspace_path, raw_jsonl_path, summary, started_at FROM sessions WHERE raw_jsonl_path IS NOT NULL`;
    const params = [];

    if (workspace) {
      query += ` AND workspace_path = ?`;
      params.push(workspace);
    }
    query += ` ORDER BY started_at DESC LIMIT 100`;

    const sessions = db.prepare(query).all(...params);
    db.close();

    const results = [];

    for (const session of sessions) {
      if (results.length >= limit) break;
      if (!session.raw_jsonl_path || !existsSync(session.raw_jsonl_path)) continue;

      try {
        const content = readFileSync(session.raw_jsonl_path, "utf-8");
        const lines = content.trim().split("\n");

        for (let i = 0; i < lines.length && results.length < limit; i++) {
          const line = lines[i];
          if (regex.test(line)) {
            try {
              const parsed = JSON.parse(line);
              const preview = parsed.message?.content?.[0]?.text?.slice(0, 200) ||
                parsed.summary?.slice(0, 200) ||
                line.slice(0, 200);
              results.push({
                session_id: session.id,
                session_summary: session.summary,
                session_started_at: session.started_at,
                message_index: i,
                type: parsed.type,
                preview,
                match: line.match(regex)?.[0],
              });
            } catch {
              results.push({
                session_id: session.id,
                message_index: i,
                preview: line.slice(0, 200),
                match: line.match(regex)?.[0],
              });
            }
          }
        }
      } catch {
        // Skip unreadable files
      }
    }

    res.json({ results, total: results.length, pattern });
  } catch (err) {
    if (err.message.includes("Invalid regular expression")) {
      return res.status(400).json({ error: "Invalid regex pattern" });
    }
    res.status(500).json({ error: err.message });
  }
});

// Sessions - get single session with JSONL messages
app.get("/api/sessions/:id", (req, res) => {
  const sessionsDB = join(AGENTCTL_HOME, "storage", "sessions.db");

  try {
    const db = openDB(sessionsDB);

    const session = db
      .prepare(
        `SELECT * FROM sessions WHERE id = ?`
      )
      .get(req.params.id);

    if (!session) {
      db.close();
      return res.status(404).json({ error: "Session not found" });
    }

    db.close();
    res.json({ session });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Sessions - get paginated messages from JSONL
app.get("/api/sessions/:id/messages", (req, res) => {
  const sessionsDB = join(AGENTCTL_HOME, "storage", "sessions.db");
  const limit = Math.min(parseInt(req.query.limit) || 50, 500);
  const offset = parseInt(req.query.offset) || 0;

  try {
    const db = openDB(sessionsDB);

    const session = db
      .prepare(`SELECT raw_jsonl_path FROM sessions WHERE id = ?`)
      .get(req.params.id);

    db.close();

    if (!session || !session.raw_jsonl_path) {
      return res.status(404).json({ error: "Session or JSONL not found" });
    }

    if (!existsSync(session.raw_jsonl_path)) {
      return res.status(404).json({ error: "JSONL file not found", path: session.raw_jsonl_path });
    }

    const content = readFileSync(session.raw_jsonl_path, "utf-8");
    const lines = content.trim().split("\n").filter(Boolean);
    const total = lines.length;

    const messages = lines
      .slice(offset, offset + limit)
      .map((line, idx) => {
        try {
          const parsed = JSON.parse(line);
          return {
            index: offset + idx,
            ...parsed,
          };
        } catch {
          return { index: offset + idx, error: "Parse error", raw: line.slice(0, 200) };
        }
      });

    res.json({ messages, total, limit, offset, path: session.raw_jsonl_path });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Sessions - update a specific message in JSONL
app.put("/api/sessions/:id/messages/:index", (req, res) => {
  const sessionsDB = join(AGENTCTL_HOME, "storage", "sessions.db");
  const messageIndex = parseInt(req.params.index);
  const { message } = req.body;

  if (message === undefined) {
    return res.status(400).json({ error: "message body required" });
  }

  try {
    const db = openDB(sessionsDB);

    const session = db
      .prepare(`SELECT raw_jsonl_path FROM sessions WHERE id = ?`)
      .get(req.params.id);

    db.close();

    if (!session || !session.raw_jsonl_path) {
      return res.status(404).json({ error: "Session or JSONL not found" });
    }

    if (!existsSync(session.raw_jsonl_path)) {
      return res.status(404).json({ error: "JSONL file not found" });
    }

    const content = readFileSync(session.raw_jsonl_path, "utf-8");
    const lines = content.trim().split("\n");

    if (messageIndex < 0 || messageIndex >= lines.length) {
      return res.status(400).json({ error: "Invalid message index" });
    }

    // Replace the line with new message
    lines[messageIndex] = typeof message === "string" ? message : JSON.stringify(message);

    // Write back atomically to prevent corruption
    atomicWriteFileSync(session.raw_jsonl_path, lines.join("\n") + "\n");

    res.json({ success: true, index: messageIndex });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Sessions - get context windows for a session
app.get("/api/sessions/:id/context-windows", (req, res) => {
  const sessionsDB = join(AGENTCTL_HOME, "storage", "sessions.db");

  try {
    const db = openDB(sessionsDB);

    // Check if table exists first
    const tableExists = db
      .prepare(`SELECT name FROM sqlite_master WHERE type='table' AND name='session_context_windows'`)
      .get();

    if (!tableExists) {
      db.close();
      return res.json({ context_windows: [], total: 0 });
    }

    const contextWindows = db
      .prepare(
        `SELECT id, session_id, window_index, started_at, ended_at,
                pre_compact_tokens, trigger, chunk_start, chunk_end,
                message_count, summary, created_at
         FROM session_context_windows
         WHERE session_id = ?
         ORDER BY window_index ASC`
      )
      .all(req.params.id);

    db.close();
    res.json({ context_windows: contextWindows, total: contextWindows.length });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Codemaps - list all codemaps from memory store
app.get("/api/codemaps", (req, res) => {
  const workspace = getWorkspace(req);
  const limit = Math.min(parseInt(req.query.limit) || 50, 200);

  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");

  try {
    const db = openDB(memoryDB, { readonly: false });

    let query = `
      SELECT name, summary, workspace, created_at, result
      FROM named_memory
      WHERE type = 'codemap'
    `;
    const params = [];

    if (workspace) {
      query += ` AND workspace = ?`;
      params.push(workspace);
    }

    query += ` ORDER BY created_at DESC LIMIT ?`;
    params.push(limit);

    const rows = db.prepare(query).all(...params);
    db.close();

    const codemaps = rows.map((row) => {
      // Parse the result JSON to extract codemap details
      let parsed = {};
      try {
        parsed = JSON.parse(row.result || "{}");
      } catch { }

      // Extract ID from name (codemap://xxx -> xxx)
      const id = row.name.replace("codemap://", "");

      return {
        id,
        title: parsed.title || parsed.Title || row.summary?.split(" - ")[0] || id,
        query: parsed.query || parsed.Query || "",
        file_count: parsed.file_count || parsed.FileCount || 0,
        symbol_count: parsed.symbol_count || parsed.SymbolCount || 0,
        created_at: row.created_at,
      };
    });

    res.json({ codemaps });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Codemaps - search via semantic search (must be before :id route)
app.get("/api/codemaps/search", (req, res) => {
  const q = req.query.q || "";
  const limit = parseInt(req.query.limit) || 20;

  if (!q) {
    return res.json({ results: [] });
  }

  const input = { query: q, limit, scope: ["codemaps"] };
  const result = runSkill("code/semantic_search", input);
  res.json({ results: result.data?.results || [] });
});

// Codemaps - get single codemap
app.get("/api/codemaps/:id", (req, res) => {
  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");
  const codemapName = `codemap://${req.params.id}`;

  try {
    const db = openDB(memoryDB);

    const row = db
      .prepare(`SELECT name, summary, workspace, created_at, result FROM named_memory WHERE name = ?`)
      .get(codemapName);

    db.close();

    if (!row) {
      return res.status(404).json({ error: "Codemap not found" });
    }

    // Parse the result JSON
    let codemap = {};
    try {
      codemap = JSON.parse(row.result || "{}");
    } catch { }

    // Normalize field names (Go uses PascalCase, frontend expects snake_case)
    const normalized = {
      id: req.params.id,
      title: codemap.title || codemap.Title || "",
      description: codemap.description || codemap.Description || "",
      query: codemap.query || codemap.Query || "",
      workspace: row.workspace || codemap.workspace || codemap.Workspace || "",
      file_count: codemap.file_count || codemap.FileCount || 0,
      symbol_count: codemap.symbol_count || codemap.SymbolCount || 0,
      traces: (codemap.traces || codemap.Traces || []).map((t) => ({
        number: t.number || t.Number || 0,
        title: t.title || t.Title || "",
        summary: t.summary || t.Summary || "",
        tree: t.tree || t.Tree || "",
        annotations: (t.annotations || t.Annotations || []).map((a) => ({
          label: a.label || a.Label || "",
          title: a.title || a.Title || "",
          description: a.description || a.Description || "",
          path: a.path || a.Path || "",
        })),
      })),
      created_at: row.created_at,
    };

    res.json(normalized);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Codemaps - delete
app.delete("/api/codemaps/:id", (req, res) => {
  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");
  const codemapName = `codemap://${req.params.id}`;

  // Note: We need write access for delete
  // Open a new connection with write access
  try {
    const db = new Database(memoryDB);

    const result = db
      .prepare(`DELETE FROM named_memory WHERE name = ?`)
      .run(codemapName);

    db.close();

    if (result.changes === 0) {
      return res.status(404).json({ error: "Codemap not found" });
    }

    res.json({ success: true });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// ============================================================================
// SSE (Server-Sent Events) for Real-Time Updates
// ============================================================================

// Track connected SSE clients
const sseClients = new Set();

// Broadcast event to all connected clients
function broadcastEvent(type, data = {}) {
  const event = JSON.stringify({ type, data, ts: Date.now() });
  for (const client of sseClients) {
    client.write(`data: ${event}\n\n`);
  }
}

// GET /api/events - SSE stream for real-time updates
app.get("/api/events", (req, res) => {
  // Set SSE headers
  res.setHeader("Content-Type", "text/event-stream");
  res.setHeader("Cache-Control", "no-cache");
  res.setHeader("Connection", "keep-alive");
  res.setHeader("X-Accel-Buffering", "no"); // Disable nginx buffering
  res.flushHeaders();

  // Send initial connection event
  res.write(`data: ${JSON.stringify({ type: "connected", ts: Date.now() })}\n\n`);

  // Add client to set
  sseClients.add(res);

  // Heartbeat to keep connection alive (every 30s)
  const heartbeat = setInterval(() => {
    res.write(`data: ${JSON.stringify({ type: "heartbeat", ts: Date.now() })}\n\n`);
  }, 30000);

  // Clean up on client disconnect
  req.on("close", () => {
    clearInterval(heartbeat);
    sseClients.delete(res);
  });
});

// Poll for changes and broadcast (simple file-based change detection)
// This watches the jobs directory for new job files
let lastJobsCheck = Date.now();
let lastJobCount = 0;

// Track last seen IDs for each resource type to detect new entries
let lastTaskUpdateCheck = Date.now();
let lastMailboxId = "";
let lastBlackboardTS = 0;

// ULID pattern: 26 alphanumeric characters (Crockford base32)
const ULID_PATTERN = /^[0-9A-HJKMNP-TV-Z]{26}$/i;

// Poll jobs directory for changes
setInterval(async () => {
  const jobsDir = join(AGENTCTL_HOME, "jobs");
  try {
    // Filter to only ULID-named directories (job IDs)
    const entries = readdirSync(jobsDir).filter(e => ULID_PATTERN.test(e));
    const currentCount = entries.length;

    // Check for new jobs
    if (currentCount > lastJobCount) {
      const newJobs = entries
        .sort()
        .reverse()
        .slice(0, currentCount - lastJobCount);

      for (const jobId of newJobs) {
        broadcastEvent("job", { id: jobId, state: "created" });
      }
    }
    lastJobCount = currentCount;

    // Check for recently modified jobs (state changes)
    const recentJobs = entries.sort().reverse().slice(0, 10);
    for (const jobId of recentJobs) {
      const resultFile = join(jobsDir, jobId, "result.json");
      try {
        const stat = statSync(resultFile);
        if (stat.mtimeMs > lastJobsCheck) {
          const result = JSON.parse(readFileSync(resultFile, "utf-8"));
          broadcastEvent("job", { id: jobId, state: result.status });
        }
      } catch {
        // Skip jobs without result.json
      }
    }
  } catch {
    // Jobs dir doesn't exist yet
  }

  lastJobsCheck = Date.now();
}, 2000); // Check every 2 seconds

// Poll tasks database for changes
setInterval(() => {
  if (sseClients.size === 0) return; // Skip if no clients

  const tasksDB = join(AGENTCTL_HOME, "storage", "tasks.db");
  if (!existsSync(tasksDB)) return;

  try {
    const db = openDB(tasksDB);
    // Get tasks updated since last check
    const updated = db.prepare(`
      SELECT id, title, status, updated_at
      FROM tasks
      WHERE updated_at > ?
      ORDER BY updated_at DESC
      LIMIT 20
    `).all(new Date(lastTaskUpdateCheck).toISOString());

    db.close();

    for (const task of updated) {
      broadcastEvent("task", { id: task.id, status: task.status, title: task.title });
    }

    if (updated.length > 0) {
      lastTaskUpdateCheck = Date.now();
    }
  } catch {
    // Database doesn't exist or query failed
  }
}, 2000);

// Poll mailbox database for new messages
setInterval(() => {
  if (sseClients.size === 0) return; // Skip if no clients

  const boardDB = join(AGENTCTL_HOME, "storage", "board.db");
  if (!existsSync(boardDB)) return;

  try {
    const db = openDB(boardDB);
    // Get newest message ID
    const latest = db.prepare(`
      SELECT id, recipient, sender, subject, created_at
      FROM board_messages
      ORDER BY created_at DESC
      LIMIT 1
    `).get();

    db.close();

    if (latest && latest.id !== lastMailboxId) {
      // New message detected
      if (lastMailboxId !== "") {
        broadcastEvent("mailbox", {
          id: latest.id,
          actor: latest.recipient,
          from: latest.sender,
          subject: latest.subject,
        });
      }
      lastMailboxId = latest.id;
    }
  } catch {
    // Database doesn't exist or query failed
  }
}, 1000); // Check every 1 second for mailbox (more responsive)

// Poll blackboard database for changes
setInterval(() => {
  if (sseClients.size === 0) return; // Skip if no clients

  const blackboardDB = join(AGENTCTL_HOME, "storage", "blackboard.db");
  if (!existsSync(blackboardDB)) return;

  try {
    const db = openDB(blackboardDB);
    // Get latest timestamp
    const latest = db.prepare(`
      SELECT MAX(ts) as max_ts FROM blackboard
    `).get();

    db.close();

    if (latest && latest.max_ts > lastBlackboardTS) {
      // Blackboard has been updated
      if (lastBlackboardTS > 0) {
        broadcastEvent("blackboard", { updated: true });
      }
      lastBlackboardTS = latest.max_ts;
    }
  } catch {
    // Database doesn't exist or query failed
  }
}, 3000); // Check every 3 seconds for blackboard

// ============================================================================
// Memory Endpoints (named_memory from memory.db)
// ============================================================================

// GET /api/memory - List memory entries
app.get("/api/memory", (req, res) => {
  const workspace = getWorkspace(req);
  const type = req.query.type || "";
  const limit = Math.min(parseInt(req.query.limit) || 50, 200);
  const offset = parseInt(req.query.offset) || 0;

  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");

  try {
    if (!existsSync(memoryDB)) {
      return res.json({ memories: [], total: 0 });
    }

    const db = openDB(memoryDB);
    // Note: pinned_at column migration runs at server startup via runMigrations()

    // Build query with optional filters
    let query = `
      SELECT id, name, type, workspace, summary, created_at, updated_at,
             last_accessed, access_count, session_id, pinned_at
      FROM named_memory
      WHERE 1=1
    `;
    const params = [];

    if (workspace) {
      query += ` AND workspace = ?`;
      params.push(workspace);
    }

    if (type) {
      // Support comma-separated types
      const types = type.split(",").map(t => t.trim());
      query += ` AND type IN (${types.map(() => "?").join(",")})`;
      params.push(...types);
    }

    // Count total: strip ORDER/LIMIT/OFFSET if present, then wrap as subquery
    const cleaned = query
      .replace(/ORDER BY[\s\S]*$/i, "")
      .replace(/LIMIT\s+\?\s*(OFFSET\s+\?)?/i, "")
      .trim();
    const countQuery = `SELECT COUNT(*) as total FROM (${cleaned}) AS _count`;
    const total = db.prepare(countQuery).get(...params).total;

    // Add pagination
    query += ` ORDER BY updated_at DESC LIMIT ? OFFSET ?`;
    params.push(limit, offset);

    const rows = db.prepare(query).all(...params);
    db.close();

    const memories = rows.map((row) => ({
      id: row.id,
      name: row.name,
      type: row.type,
      workspace: row.workspace,
      summary: row.summary,
      created_at: row.created_at,
      updated_at: row.updated_at,
      last_accessed: row.last_accessed,
      access_count: row.access_count,
      session_id: row.session_id,
    }));

    res.json({ memories, total, limit, offset });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/memory/types - List unique memory types with counts
app.get("/api/memory/types", (req, res) => {
  const workspace = getWorkspace(req);
  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");

  try {
    if (!existsSync(memoryDB)) {
      return res.json({ types: [] });
    }

    const db = openDB(memoryDB);

    let query = `
      SELECT type, COUNT(*) as count
      FROM named_memory
      WHERE 1=1
    `;
    const params = [];

    if (workspace) {
      query += ` AND workspace = ?`;
      params.push(workspace);
    }

    query += ` GROUP BY type ORDER BY count DESC`;

    const rows = db.prepare(query).all(...params);
    db.close();

    res.json({ types: rows });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/memory/:id - Get single memory entry with full data
app.get("/api/memory/:id", (req, res) => {
  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");

  try {
    if (!existsSync(memoryDB)) {
      return res.status(404).json({ error: "Memory database not found" });
    }

    const db = openDB(memoryDB);

    // Ensure pinned_at column exists (idempotent migration)
    try {
      db.exec(`ALTER TABLE named_memory ADD COLUMN pinned_at TEXT DEFAULT NULL`);
    } catch {
      // Column already exists, ignore
    }

    const row = db
      .prepare(
        `SELECT id, name, type, workspace, summary, result, digests,
                created_at, updated_at, last_accessed, access_count, session_id, pinned_at
         FROM named_memory WHERE id = ?`
      )
      .get(req.params.id);

    db.close();

    if (!row) {
      return res.status(404).json({ error: "Memory not found" });
    }

    // Parse result (stored as JSON blob)
    let data = null;
    if (row.result) {
      const raw =
        typeof row.result === "string"
          ? row.result
          : Buffer.isBuffer(row.result)
            ? row.result.toString("utf-8")
            : row.result;
      try {
        data = JSON.parse(raw);
      } catch {
        data = raw;
      }
    }

    // Parse digests
    let digests = [];
    if (row.digests) {
      try {
        digests = JSON.parse(row.digests);
      } catch {
        // Single digest or comma-separated
        digests = row.digests.split(",").filter(Boolean);
      }
    }

    res.json({
      id: row.id,
      name: row.name,
      type: row.type,
      workspace: row.workspace,
      summary: row.summary,
      data,
      digests,
      created_at: row.created_at,
      updated_at: row.updated_at,
      last_accessed: row.last_accessed,
      access_count: row.access_count,
      session_id: row.session_id,
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// POST /api/memory - Save a new memory
app.post("/api/memory", (req, res) => {
  const { name, type, summary, data } = req.body;
  const workspace = getWorkspace(req) || process.cwd();

  if (!name || !type) {
    return res.status(400).json({ error: "name and type are required" });
  }

  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");

  try {
    // Use new Database directly for write access (openDB defaults to readonly on non-Bun)
    const db = new Database(memoryDB);
    const now = new Date().toISOString();

    // Generate a simple unique ID (timestamp + random)
    const id = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;

    // Serialize data as JSON
    const resultBlob = JSON.stringify(data || {});
    const digests = "[]";

    db.prepare(`
      INSERT INTO named_memory (id, name, type, workspace, summary, result, digests, created_at, updated_at, last_accessed, access_count)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
      ON CONFLICT(name, workspace) DO UPDATE SET
        summary = excluded.summary,
        result = excluded.result,
        updated_at = excluded.updated_at
    `).run(id, name, type, workspace, summary || "", resultBlob, digests, now, now, now);

    db.close();
    res.json({ success: true, id });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// POST /api/memory/:id/pin - Toggle pin status on a memory entry
app.post("/api/memory/:id/pin", (req, res) => {
  const memoryId = req.params.id;
  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");

  try {
    const db = new Database(memoryDB);
    // Note: pinned_at column migration runs at server startup via runMigrations()

    // Check current pin status
    const entry = db.prepare(`SELECT pinned_at FROM named_memory WHERE id = ?`).get(memoryId);
    if (!entry) {
      db.close();
      return res.status(404).json({ error: "Memory entry not found" });
    }

    // Toggle pin status
    const now = new Date().toISOString();
    const newPinnedAt = entry.pinned_at ? null : now;

    db.prepare(`UPDATE named_memory SET pinned_at = ? WHERE id = ?`).run(newPinnedAt, memoryId);

    db.close();
    res.json({
      success: true,
      pinned: !!newPinnedAt,
      pinned_at: newPinnedAt,
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// DELETE /api/memory/:id - Delete a memory entry
app.delete("/api/memory/:id", (req, res) => {
  const memoryId = req.params.id;
  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");

  try {
    const db = new Database(memoryDB);

    const result = db.prepare(`DELETE FROM named_memory WHERE id = ?`).run(memoryId);

    db.close();

    if (result.changes === 0) {
      return res.status(404).json({ error: "Memory entry not found" });
    }

    res.json({ success: true, deleted: memoryId });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// ============================================================================
// CAS (Content Addressable Storage) Endpoints
// ============================================================================

// GET /api/cas - List CAS objects
app.get("/api/cas", (req, res) => {
  const casDir = join(AGENTCTL_HOME, "cas", "sha256");

  try {
    if (!existsSync(casDir)) {
      return res.json({ objects: [] });
    }

    const files = readdirSync(casDir).filter(f => !f.endsWith(".json") && f.length === 64);
    const objects = files.map(hex => {
      const metaPath = join(casDir, `${hex}.json`);
      let meta = { digest: `sha256:${hex}` };

      if (existsSync(metaPath)) {
        try {
          const metaContent = readFileSync(metaPath, "utf-8");
          meta = { ...meta, ...JSON.parse(metaContent) };
        } catch {
          // Ignore metadata parse errors
        }
      }

      // Get file size
      try {
        const stat = statSync(join(casDir, hex));
        meta.size_bytes = stat.size;
      } catch {
        // Ignore stat errors
      }

      return meta;
    });

    res.json({ objects });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/cas/:digest - Read CAS object with pagination
// Query params: page (default: 1), pageSize (default: 2048)
app.get("/api/cas/:digest", (req, res) => {
  const casDir = join(AGENTCTL_HOME, "cas", "sha256");
  const digest = req.params.digest;

  // Extract hex from digest (handle both "sha256:abc" and "abc" formats)
  const hex = digest.startsWith("sha256:") ? digest.slice(7) : digest;

  if (!/^[0-9a-f]{64}$/i.test(hex)) {
    return res.status(400).json({ error: "Invalid digest format" });
  }

  const objPath = join(casDir, hex);
  const metaPath = join(casDir, `${hex}.json`);

  if (!existsSync(objPath)) {
    return res.status(404).json({ error: "CAS object not found" });
  }

  try {
    // Parse pagination params
    const page = Math.max(1, parseInt(req.query.page) || 1);
    const pageSize = Math.max(1, Math.min(65536, parseInt(req.query.pageSize) || 2048));

    // Read content
    const content = readFileSync(objPath, "utf-8");
    const totalBytes = content.length;
    const totalPages = Math.max(1, Math.ceil(totalBytes / pageSize));

    // Clamp page to valid range
    const actualPage = Math.min(page, totalPages);

    // Extract page content
    const start = (actualPage - 1) * pageSize;
    const end = Math.min(start + pageSize, totalBytes);
    const pageContent = content.slice(start, end);

    // Read metadata if available
    let meta = {};
    if (existsSync(metaPath)) {
      try {
        meta = JSON.parse(readFileSync(metaPath, "utf-8"));
      } catch {
        // Ignore metadata parse errors
      }
    }

    // Build response
    const response = {
      digest: `sha256:${hex}`,
      content: pageContent,
      page: actualPage,
      total_pages: totalPages,
      page_size: pageSize,
      total_bytes: totalBytes,
      content_type: meta.kind || "application/octet-stream",
    };

    // Add navigation hints
    if (actualPage < totalPages) {
      response.next_page = actualPage + 1;
    }
    if (actualPage > 1) {
      response.prev_page = actualPage - 1;
    }

    res.json(response);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// ============================================================================
// Agent Endpoints
// ============================================================================

// GET /api/agents - List all agents
app.get("/api/agents", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  if (!existsSync(dbPath)) {
    return res.json({ agents: [], total: 0 });
  }

  try {
    const db = openDB(dbPath);
    const { state, limit = 50 } = req.query;

    let sql = `
      SELECT id, parent_id, ns, role, skills_allow, policy, share_bb, state,
             llm_provider, llm_model, created_at, heartbeat_at
      FROM agents
    `;
    const params = [];

    if (state) {
      sql += " WHERE state = ?";
      params.push(state);
    }

    sql += " ORDER BY created_at DESC LIMIT ?";
    params.push(parseInt(limit) || 50);

    const agents = db.prepare(sql).all(...params);

    // Get total count
    let countSql = "SELECT COUNT(*) as count FROM agents";
    if (state) {
      countSql += " WHERE state = ?";
    }
    const total = db.prepare(countSql).get(...(state ? [state] : [])).count;

    db.close();
    res.json({ agents, total });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/agents/:id - Get agent details
app.get("/api/agents/:id", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  if (!existsSync(dbPath)) {
    return res.status(404).json({ error: "Agents database not found" });
  }

  try {
    const db = openDB(dbPath);
    const agent = db.prepare(`
      SELECT id, parent_id, ns, role, prompt, skills_allow, policy, share_bb, state,
             llm_provider, llm_model, created_at, heartbeat_at
      FROM agents WHERE id = ?
    `).get(req.params.id);

    db.close();

    if (!agent) {
      return res.status(404).json({ error: "Agent not found" });
    }


    res.json({ agent });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

app.post("/api/agents/:id/daemon/start", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  if (!existsSync(dbPath)) {
    return res.status(404).json({ error: "Agents database not found" });
  }

  try {
    const db = openDB(dbPath);
    const agent = db.prepare(`SELECT id, ns FROM agents WHERE id = ?`).get(req.params.id);
    db.close();

    if (!agent) {
      return res.status(404).json({ error: "Agent not found" });
    }

    const body = req.body || {};
    const workspace = typeof body.workspace === "string" && body.workspace ? body.workspace : getWorkspace(req);
    const meta = body.meta && typeof body.meta === "object" ? body.meta : null;

    const result = ensureAgentDaemon(agent.ns, workspace, meta);
    if (result.error) {
      const message = String(result.error || "failed to start daemon");
      const status = message.toLowerCase().includes("missing") || message.toLowerCase().includes("no llm") ? 400 : 500;
      return res.status(status).json({ error: message, actor_id: agent.ns, ...result });
    }

    res.json({ actor_id: agent.ns, ...result });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// ============================================================================
// Trajectory Endpoints
// ============================================================================

// GET /api/trajectories - List trajectories
app.get("/api/trajectories", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "trajectory.db");

  if (!existsSync(dbPath)) {
    return res.json({ trajectories: [], total: 0 });
  }

  try {
    const db = openDB(dbPath);
    const { status, agent_role, limit = 50 } = req.query;

    let sql = `
      SELECT id, workspace_id, root_request_id, task_ids_json, epic_id,
             agent_role, job_id, trace_id, status, summary, artifact_digest,
             created_at, updated_at
      FROM trajectories
    `;
    const conditions = [];
    const params = [];

    if (status) {
      conditions.push("status = ?");
      params.push(status);
    }
    if (agent_role) {
      conditions.push("agent_role = ?");
      params.push(agent_role);
    }

    if (conditions.length > 0) {
      sql += " WHERE " + conditions.join(" AND ");
    }

    sql += " ORDER BY created_at DESC LIMIT ?";
    params.push(parseInt(limit) || 50);

    const trajectories = db.prepare(sql).all(...params);

    // Get total count
    let countSql = "SELECT COUNT(*) as count FROM trajectories";
    if (conditions.length > 0) {
      countSql += " WHERE " + conditions.join(" AND ");
    }
    const total = db.prepare(countSql).get(...params.slice(0, -1)).count;

    db.close();
    res.json({ trajectories, total });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/trajectories/:id/events - Get trajectory events
app.get("/api/trajectories/:id/events", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "trajectory.db");

  if (!existsSync(dbPath)) {
    return res.json({ events: [] });
  }

  try {
    const db = openDB(dbPath);
    const { kind, limit = 100 } = req.query;

    let sql = `
      SELECT id, trajectory_id, workspace_id, ts, kind, actor, command,
             status, data_inline_json, data_artifact, meta_json
      FROM trajectory_events
      WHERE trajectory_id = ?
    `;
    const params = [req.params.id];

    if (kind) {
      sql += " AND kind = ?";
      params.push(kind);
    }

    sql += " ORDER BY ts DESC LIMIT ?";
    params.push(parseInt(limit) || 100);

    const events = db.prepare(sql).all(...params);

    db.close();
    res.json({ events });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// POST /api/trajectories/:id/feedback - Record feedback for a trajectory
app.post("/api/trajectories/:id/feedback", (req, res) => {
  const trajectoryId = req.params.id;
  const { rating, comment } = req.body;

  if (!rating || rating < 1 || rating > 5) {
    return res.status(400).json({ error: "rating must be between 1 and 5" });
  }

  const dbPath = join(AGENTCTL_HOME, "storage", "trajectory.db");

  if (!existsSync(dbPath)) {
    return res.status(404).json({ error: "Trajectory database not found" });
  }

  try {
    const db = openDB(dbPath);
    const now = new Date().toISOString();

    // Verify trajectory exists
    const trajectory = db.prepare(`SELECT id FROM trajectories WHERE id = ?`).get(trajectoryId);
    if (!trajectory) {
      db.close();
      return res.status(404).json({ error: "Trajectory not found" });
    }

    // Create trajectory_outcomes table if needed (idempotent)
    db.exec(`
      CREATE TABLE IF NOT EXISTS trajectory_outcomes (
        trajectory_id TEXT PRIMARY KEY,
        human_rating INTEGER,
        human_feedback TEXT,
        recorded_at TEXT,
        success INTEGER DEFAULT NULL,
        duration_ms INTEGER DEFAULT NULL,
        tool_call_count INTEGER DEFAULT NULL,
        FOREIGN KEY (trajectory_id) REFERENCES trajectories(id)
      )
    `);

    // Insert or update outcome
    db.prepare(`
      INSERT INTO trajectory_outcomes (trajectory_id, human_rating, human_feedback, recorded_at)
      VALUES (?, ?, ?, ?)
      ON CONFLICT(trajectory_id) DO UPDATE SET
        human_rating = excluded.human_rating,
        human_feedback = excluded.human_feedback,
        recorded_at = excluded.recorded_at
    `).run(trajectoryId, rating, comment || null, now);

    db.close();

    res.json({
      success: true,
      trajectory_id: trajectoryId,
      rating,
      comment: comment || null,
      recorded_at: now,
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/trajectories/:id/feedback - Get feedback for a trajectory
app.get("/api/trajectories/:id/feedback", (req, res) => {
  const trajectoryId = req.params.id;
  const dbPath = join(AGENTCTL_HOME, "storage", "trajectory.db");

  if (!existsSync(dbPath)) {
    return res.json({ feedback: null });
  }

  try {
    const db = openDB(dbPath);

    // Check if outcomes table exists
    const tableExists = db.prepare(`
      SELECT name FROM sqlite_master
      WHERE type='table' AND name='trajectory_outcomes'
    `).get();

    if (!tableExists) {
      db.close();
      return res.json({ feedback: null });
    }

    const outcome = db.prepare(`
      SELECT human_rating as rating, human_feedback as comment, recorded_at
      FROM trajectory_outcomes
      WHERE trajectory_id = ?
    `).get(trajectoryId);

    db.close();
    res.json({ feedback: outcome || null });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/weights - Get learnable scorer weights
app.get("/api/weights", (req, res) => {
  const workspace = getWorkspace(req) || ".";
  const dbPath = join(AGENTCTL_HOME, "storage", "memory.db");

  // Default weights
  const defaultWeights = {
    critical_path: 0.30,
    page_rank: 0.20,
    admin_mail: 0.25,
    overseer_mail: 0.15,
    recency: 0.10,
    version: 1,
    last_updated: null,
  };

  if (!existsSync(dbPath)) {
    return res.json({ weights: defaultWeights, history: [] });
  }

  try {
    const db = openDB(dbPath);

    // Check if scorer_weights table exists
    const tableExists = db.prepare(`
      SELECT name FROM sqlite_master
      WHERE type='table' AND name='scorer_weights'
    `).get();

    if (!tableExists) {
      // Create table for future use
      db.exec(`
        CREATE TABLE IF NOT EXISTS scorer_weights (
          id TEXT PRIMARY KEY,
          workspace TEXT NOT NULL,
          critical_path REAL NOT NULL DEFAULT 0.30,
          page_rank REAL NOT NULL DEFAULT 0.20,
          admin_mail REAL NOT NULL DEFAULT 0.25,
          overseer_mail REAL NOT NULL DEFAULT 0.15,
          recency REAL NOT NULL DEFAULT 0.10,
          version INTEGER NOT NULL DEFAULT 1,
          last_updated TEXT,
          UNIQUE(workspace)
        )
      `);
      db.close();
      return res.json({ weights: defaultWeights, history: [] });
    }

    const weights = db.prepare(`
      SELECT critical_path, page_rank, admin_mail, overseer_mail, recency,
             version, last_updated
      FROM scorer_weights
      WHERE workspace = ?
    `).get(workspace);

    // Get weight history if available
    const historyTableExists = db.prepare(`
      SELECT name FROM sqlite_master
      WHERE type='table' AND name='scorer_weight_history'
    `).get();

    let history = [];
    if (historyTableExists) {
      history = db.prepare(`
        SELECT previous_weights, new_weights, timestamp, reason, sample_size
        FROM scorer_weight_history
        WHERE workspace = ?
        ORDER BY timestamp DESC
        LIMIT 10
      `).all(workspace);
    }

    db.close();

    res.json({
      weights: weights || defaultWeights,
      history: history.map(h => ({
        ...h,
        previous_weights: h.previous_weights ? JSON.parse(h.previous_weights) : null,
        new_weights: h.new_weights ? JSON.parse(h.new_weights) : null,
      })),
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/user-requests - List user requests
app.get("/api/user-requests", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "trajectory.db");

  if (!existsSync(dbPath)) {
    return res.json({ requests: [] });
  }

  try {
    const db = openDB(dbPath);
    const { limit = 50 } = req.query;

    const requests = db.prepare(`
      SELECT id, workspace_id, actor, source, ts, text,
             command_context_json, task_hints_json
      FROM user_requests
      ORDER BY ts DESC
      LIMIT ?
    `).all(parseInt(limit) || 50);

    db.close();
    res.json({ requests });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// ============================================================================
// Console Session Endpoints
// ============================================================================

// Track SSE clients per console for targeted streaming
const consoleSSEClients = new Map(); // consoleId -> Set of response objects
let lastConsoleMessageId = "";

// Generate a ULID-like ID (simplified for JS)
function generateId() {
  const t = Date.now().toString(36).toUpperCase().padStart(10, '0');
  const r = Math.random().toString(36).substring(2, 18).toUpperCase();
  return (t + r).substring(0, 26);
}

// GET /api/consoles - List console sessions
app.get("/api/consoles", (req, res) => {
  const workspace = getWorkspace(req);
  const limit = Math.min(parseInt(req.query.limit) || 50, 200);
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  if (!existsSync(dbPath)) {
    return res.json({ consoles: [], total: 0 });
  }

  try {
    const db = openDB(dbPath);

    // Check if console_sessions table exists
    const tableExists = db.prepare(`
      SELECT name FROM sqlite_master
      WHERE type='table' AND name='console_sessions'
    `).get();

    if (!tableExists) {
      db.close();
      return res.json({ consoles: [], total: 0 });
    }

    let query = `
      SELECT console_id, actor_id, session_id, workspace,
             created_at, last_attached_at, meta
      FROM console_sessions
    `;
    const params = [];

    if (workspace) {
      query += ` WHERE workspace = ?`;
      params.push(workspace);
    }

    query += ` ORDER BY last_attached_at DESC LIMIT ?`;
    params.push(limit);

    const consoles = db.prepare(query).all(...params);

    // Get total count
    let countQuery = `SELECT COUNT(*) as total FROM console_sessions`;
    if (workspace) {
      countQuery += ` WHERE workspace = ?`;
    }
    const total = db.prepare(countQuery).get(...(workspace ? [workspace] : [])).total;

    db.close();

    // Parse meta JSON for each console (tolerate malformed JSON)
    const parsed = consoles.map(c => {
      let metaObj = null;
      if (c.meta) {
        try {
          metaObj = JSON.parse(c.meta);
        } catch (e) {
          console.debug("Failed to parse console meta", e?.message || e);
          metaObj = null;
        }
      }
      return {
        id: c.console_id,
        actor_id: c.actor_id,
        session_id: c.session_id,
        workspace: c.workspace,
        created_at: c.created_at,
        last_attached_at: c.last_attached_at,
        meta: metaObj,
      };
    });

    res.json({ consoles: parsed, total });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// POST /api/consoles - Create or attach to console session
app.post("/api/consoles", (req, res) => {
  const workspace = getWorkspace(req);
  const { actor_id, session_id, meta } = req.body;

  if (!actor_id) {
    return res.status(400).json({ error: "actor_id is required" });
  }

  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  try {
    // Ensure agent daemon is running for this actor_id
    const daemonResult = ensureAgentDaemon(actor_id, workspace, meta);
    if (daemonResult.error) {
      console.warn(`Failed to ensure agent daemon: ${daemonResult.error}`);
      // Continue anyway - the daemon might start later
    } else {
      console.log(`Agent daemon status: ${daemonResult.status} (agent: ${daemonResult.agentId})`);
    }

    // Open with write access
    const db = new Database(dbPath);

    // Ensure table exists
    db.exec(`
      CREATE TABLE IF NOT EXISTS console_sessions (
        console_id       TEXT PRIMARY KEY,
        actor_id         TEXT NOT NULL,
        session_id       TEXT,
        workspace        TEXT NOT NULL,
        created_at       TEXT NOT NULL,
        last_attached_at TEXT NOT NULL,
        meta             TEXT
      );
      CREATE INDEX IF NOT EXISTS idx_console_actor ON console_sessions(actor_id);
      CREATE INDEX IF NOT EXISTS idx_console_workspace ON console_sessions(workspace);
    `);

    const consoleId = generateId();
    const now = new Date().toISOString();
    const metaJSON = meta ? JSON.stringify(meta) : null;

    db.prepare(`
      INSERT INTO console_sessions (console_id, actor_id, session_id, workspace, created_at, last_attached_at, meta)
      VALUES (?, ?, ?, ?, ?, ?, ?)
    `).run(consoleId, actor_id, session_id || null, workspace || "", now, now, metaJSON);

    db.close();

    res.status(201).json({
      id: consoleId,
      actor_id,
      session_id: session_id || null,
      workspace: workspace || "",
      created_at: now,
      last_attached_at: now,
      meta: meta || null,
      daemon_status: daemonResult.status || "unknown",
      daemon_error: daemonResult.error || null,
      agent_id: daemonResult.agentId || null,
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/consoles/:id - Get console session details
app.get("/api/consoles/:id", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  if (!existsSync(dbPath)) {
    return res.status(404).json({ error: "Console not found" });
  }

  try {
    const db = openDB(dbPath);

    const console = db.prepare(`
      SELECT console_id, actor_id, session_id, workspace,
             created_at, last_attached_at, meta
      FROM console_sessions
      WHERE console_id = ?
    `).get(req.params.id);

    db.close();

    if (!console) {
      return res.status(404).json({ error: "Console not found" });
    }

    let metaObj = null;
    if (console.meta) {
      try {
        metaObj = JSON.parse(console.meta);
      } catch (e) {
        console.debug("Failed to parse console meta", e?.message || e);
      }
    }

    res.json({
      id: console.console_id,
      actor_id: console.actor_id,
      session_id: console.session_id,
      workspace: console.workspace,
      created_at: console.created_at,
      last_attached_at: console.last_attached_at,
      meta: metaObj,
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// DELETE /api/consoles/:id - Delete console session
app.delete("/api/consoles/:id", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  try {
    const db = new Database(dbPath);

    const result = db.prepare(`
      DELETE FROM console_sessions WHERE console_id = ?
    `).run(req.params.id);

    db.close();

    if (result.changes === 0) {
      return res.status(404).json({ error: "Console not found" });
    }

    // Clean up SSE clients for this console
    consoleSSEClients.delete(req.params.id);

    res.json({ success: true });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// POST /api/consoles/:id/send - Send console.ask message
// NOTE: This sends directly to mailbox.db (not board_messages) because the agent daemon
// polls from the mailbox table, not the board_messages table used by mailbox/manage skill.
app.post("/api/consoles/:id/send", (req, res) => {
  const { prompt, context } = req.body;
  const consoleId = req.params.id;

  if (!prompt) {
    return res.status(400).json({ error: "prompt is required" });
  }

  const agentsDbPath = join(AGENTCTL_HOME, "storage", "agents.db");
  const mailboxDbPath = join(AGENTCTL_HOME, "storage", "mailbox.db");

  try {
    // Get console session to find actor_id
    const agentsDb = openDB(agentsDbPath);
    const consoleSession = agentsDb.prepare(`
      SELECT actor_id, session_id, workspace, meta FROM console_sessions WHERE console_id = ?
    `).get(consoleId);
    agentsDb.close();

    if (!consoleSession) {
      return res.status(404).json({ error: "Console not found" });
    }

    let metaObj = null;
    if (consoleSession.meta) {
      try {
        metaObj = JSON.parse(consoleSession.meta);
      } catch (err) {
        console.debug("Failed to parse console meta for send", err?.message || err);
      }
    }

    const daemonResult = ensureAgentDaemon(consoleSession.actor_id, consoleSession.workspace || "", metaObj);
    if (daemonResult.error) {
      console.warn(`Failed to ensure agent daemon: ${daemonResult.error}`);
    }

    // Generate IDs
    const askId = generateId();
    const messageId = generateId();
    const now = Math.floor(Date.now() / 1000);

    // Build the envelope payload (matching agent.ConsoleAskData structure)
    const payload = JSON.stringify({
      status: "ok",
      command: "console.ask",
      data: {
        ask_id: askId,
        prompt: prompt,
        context: context || {},
        console_id: consoleId,
      }
    });

    // Build headers
    const headers = JSON.stringify({
      correlation: askId,
      ask_id: askId,
      console_id: consoleId,
    });

    const mailboxDb = new Database(mailboxDbPath);

    // Insert message into mailbox table
    // - from_ns: the console ID (sender)
    // - to_ns: the actor_id (agent namespace that daemon polls)
    // - type: "console.ask" (matches agent.MessageTypeConsoleAsk)
    mailboxDb.prepare(`
      INSERT INTO mailbox (id, from_ns, to_ns, type, ttl_ms, headers, payload, visible_at, attempt, ts, session_id, workspace, agent_id)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).run(
      messageId,
      consoleId,                    // from_ns
      consoleSession.actor_id,      // to_ns (agent namespace)
      "console.ask",                // type (MessageTypeConsoleAsk)
      300000,                       // ttl_ms (5 minutes)
      headers,
      payload,
      now,                          // visible_at
      0,                            // attempt
      now,                          // ts
      consoleSession.session_id || null,
      consoleSession.workspace || "",
      null                          // agent_id
    );

    mailboxDb.close();

    // Update last_attached_at
    try {
      const dbWrite = new Database(agentsDbPath);
      dbWrite.prepare(`
        UPDATE console_sessions SET last_attached_at = ? WHERE console_id = ?
      `).run(new Date().toISOString(), consoleId);
      dbWrite.close();
    } catch {
      // Non-fatal, continue
    }

    res.json({
      message_id: messageId,
      ask_id: askId,
      status: "sent",
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// GET /api/consoles/:id/events - SSE stream for specific console
app.get("/api/consoles/:id/events", (req, res) => {
  const consoleId = req.params.id;

  // Verify console exists
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");
  if (existsSync(dbPath)) {
    try {
      const db = openDB(dbPath);
      const consoleSession = db.prepare(`
        SELECT console_id FROM console_sessions WHERE console_id = ?
      `).get(consoleId);
      db.close();

      if (!consoleSession) {
        return res.status(404).json({ error: "Console not found" });
      }
    } catch {
      // Allow if we can't verify
    }
  }

  // Set SSE headers
  res.setHeader("Content-Type", "text/event-stream");
  res.setHeader("Cache-Control", "no-cache");
  res.setHeader("Connection", "keep-alive");
  res.setHeader("X-Accel-Buffering", "no");
  res.flushHeaders();

  // Send connection event
  res.write(`data: ${JSON.stringify({ type: "connected", console_id: consoleId, ts: Date.now() })}\n\n`);

  // Add to console-specific clients
  if (!consoleSSEClients.has(consoleId)) {
    consoleSSEClients.set(consoleId, new Set());
  }
  consoleSSEClients.get(consoleId).add(res);

  // Also add to global clients for heartbeat
  sseClients.add(res);

  // Clean up on disconnect
  req.on("close", () => {
    sseClients.delete(res);
    const clients = consoleSSEClients.get(consoleId);
    if (clients) {
      clients.delete(res);
      if (clients.size === 0) {
        consoleSSEClients.delete(consoleId);
      }
    }
  });
});

// Helper: Broadcast to console-specific clients
function broadcastConsoleEvent(consoleId, type, data = {}) {
  const clients = consoleSSEClients.get(consoleId);
  if (!clients || clients.size === 0) return;

  const event = JSON.stringify({ type, data: { ...data, console_id: consoleId }, ts: Date.now() });
  for (const client of clients) {
    client.write(`data: ${event}\n\n`);
  }
}

// POST /api/consoles/:id/cancel - Send console.cmd cancel
// NOTE: This sends directly to mailbox.db (not board_messages) because the agent daemon
// polls from the mailbox table.
app.post("/api/consoles/:id/cancel", (req, res) => {
  const consoleId = req.params.id;
  const { ask_id } = req.body;

  const agentsDbPath = join(AGENTCTL_HOME, "storage", "agents.db");
  const mailboxDbPath = join(AGENTCTL_HOME, "storage", "mailbox.db");

  try {
    // Get console session to find actor_id
    const agentsDb = openDB(agentsDbPath);
    const consoleSession = agentsDb.prepare(`
      SELECT actor_id FROM console_sessions WHERE console_id = ?
    `).get(consoleId);
    agentsDb.close();

    if (!consoleSession) {
      return res.status(404).json({ error: "Console not found" });
    }

    // Generate IDs
    const cmdId = generateId();
    const messageId = generateId();
    const now = Math.floor(Date.now() / 1000);

    // Build the envelope payload (matching agent.ConsoleCmdData structure)
    const payload = JSON.stringify({
      status: "ok",
      command: "console.cmd",
      data: {
        cmd_id: cmdId,
        action: "cancel",
        ask_id: ask_id || "",
      }
    });

    // Build headers
    const headers = JSON.stringify({
      ask_id: ask_id || "",
      console_id: consoleId,
    });

    // Open mailbox.db with write access
    const mailboxDb = new Database(mailboxDbPath);

    // Ensure mailbox table exists
    mailboxDb.exec(`
      CREATE TABLE IF NOT EXISTS mailbox (
        id TEXT PRIMARY KEY,
        from_ns TEXT NOT NULL,
        to_ns TEXT NOT NULL,
        type TEXT NOT NULL,
        ttl_ms INTEGER NOT NULL DEFAULT 0,
        headers TEXT,
        payload TEXT,
        visible_at INTEGER NOT NULL,
        attempt INTEGER NOT NULL DEFAULT 0,
        ts INTEGER NOT NULL,
        session_id TEXT,
        workspace TEXT,
        agent_id TEXT
      );
      CREATE INDEX IF NOT EXISTS idx_mailbox_to_ns_visible ON mailbox(to_ns, visible_at);
    `);

    // Insert cancel command into mailbox table
    mailboxDb.prepare(`
      INSERT INTO mailbox (id, from_ns, to_ns, type, ttl_ms, headers, payload, visible_at, attempt, ts, session_id, workspace, agent_id)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).run(
      messageId,
      consoleId,                    // from_ns
      consoleSession.actor_id,      // to_ns (agent namespace)
      "console.cmd",                // type (MessageTypeConsoleCmd)
      60000,                        // ttl_ms (1 minute - cancel commands expire faster)
      headers,
      payload,
      now,                          // visible_at
      0,                            // attempt
      now,                          // ts
      null,                         // session_id
      "",                           // workspace
      null                          // agent_id
    );

    mailboxDb.close();

    res.json({
      cmd_id: cmdId,
      status: "sent",
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// POST /api/consoles/:id/feedback - Record trajectory feedback
app.post("/api/consoles/:id/feedback", (req, res) => {
  const consoleId = req.params.id;
  const { trajectory_id, rating, comment, ask_id } = req.body;

  if (!rating || rating < 1 || rating > 5) {
    return res.status(400).json({ error: "rating must be between 1 and 5" });
  }

  const trajectoryDB = join(AGENTCTL_HOME, "storage", "trajectory.db");

  try {
    if (!existsSync(trajectoryDB)) {
      return res.status(404).json({ error: "Trajectory database not found" });
    }

    // If trajectory_id provided, update the outcome directly
    if (trajectory_id) {
      const db = new Database(trajectoryDB);

      // Check if trajectory_outcomes table exists and update
      db.exec(`
        CREATE TABLE IF NOT EXISTS trajectory_outcomes (
          trajectory_id TEXT PRIMARY KEY,
          success INTEGER,
          duration_ms INTEGER,
          human_rating INTEGER,
          human_feedback TEXT,
          recorded_at TEXT
        )
      `);

      const now = new Date().toISOString();

      // Insert or update outcome
      db.prepare(`
        INSERT INTO trajectory_outcomes (trajectory_id, human_rating, human_feedback, recorded_at)
        VALUES (?, ?, ?, ?)
        ON CONFLICT(trajectory_id) DO UPDATE SET
          human_rating = excluded.human_rating,
          human_feedback = excluded.human_feedback,
          recorded_at = excluded.recorded_at
      `).run(trajectory_id, rating, comment || null, now);

      db.close();

      res.json({
        success: true,
        trajectory_id,
        rating,
      });
    } else {
      // No trajectory_id - store feedback by console/ask_id for later linking
      const db = new Database(trajectoryDB);

      db.exec(`
        CREATE TABLE IF NOT EXISTS pending_feedback (
          id TEXT PRIMARY KEY,
          console_id TEXT NOT NULL,
          ask_id TEXT,
          rating INTEGER NOT NULL,
          comment TEXT,
          created_at TEXT NOT NULL
        )
      `);

      const feedbackId = generateId();
      const now = new Date().toISOString();

      db.prepare(`
        INSERT INTO pending_feedback (id, console_id, ask_id, rating, comment, created_at)
        VALUES (?, ?, ?, ?, ?, ?)
      `).run(feedbackId, consoleId, ask_id || null, rating, comment || null, now);

      db.close();

      res.json({
        success: true,
        feedback_id: feedbackId,
        rating,
      });
    }
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Poll mailbox for console events and broadcast to console-specific SSE clients
setInterval(() => {
  if (consoleSSEClients.size === 0) return;

  const mailboxDB = join(AGENTCTL_HOME, "storage", "mailbox.db");
  if (!existsSync(mailboxDB)) return;

  try {
    const db = openDB(mailboxDB);

    // Get recent console.event and console.reply messages from mailbox table
    // Note: mailbox uses 'type' column (not 'subject') and 'payload' (not 'body')
    // ts is unix timestamp in seconds
    const tenSecondsAgo = Math.floor(Date.now() / 1000) - 10;
    const messages = db.prepare(`
      SELECT id, from_ns, to_ns, type, payload, ts, headers
      FROM mailbox
      WHERE type IN ('console.event', 'console.reply')
        AND ts > ?
      ORDER BY ts DESC
      LIMIT 50
    `).all(tenSecondsAgo);

    db.close();

    for (const msg of messages) {
      // Skip if we've already processed this message
      if (lastConsoleMessageId && msg.id <= lastConsoleMessageId) {
        continue;
      }

      // Extract console_id from headers or payload
      let consoleId = null;
      let headers = {};
      let envelope = {};

      try {
        headers = msg.headers ? JSON.parse(msg.headers) : {};
        consoleId = headers.console_id;
      } catch { }

      if (!consoleId) {
        try {
          envelope = JSON.parse(msg.payload);
          consoleId = envelope.data?.console_id;
        } catch { }
      }

      if (consoleId && consoleSSEClients.has(consoleId)) {
        const eventType = msg.type === "console.reply" ? "console.reply" : "console.event";
        broadcastConsoleEvent(consoleId, eventType, {
          message_id: msg.id,
          from: msg.from_ns,
          ...envelope.data,
        });
      }
    }
    if (messages.length > 0) {
      lastConsoleMessageId = messages[0].id;
    }
  } catch (err) {
    // Database access failed - log only once per minute
    if (Date.now() % 60000 < 500) {
      console.debug("Console mailbox poll error:", err?.message);
    }
  }
}, 500); // Check every 500ms for console messages

// ============================================================================
// Mailbox Messaging Endpoints (Phase 5: Multi-Agent Orchestration)
// ============================================================================

// POST /api/mailbox/send - Send a message to an agent
app.post("/api/mailbox/send", (req, res) => {
  try {
    const { sender, recipient, subject, body, kind, priority, ack_required, headers } = req.body;

    if (!recipient) {
      return res.status(400).json({ error: "recipient is required" });
    }
    if (!subject) {
      return res.status(400).json({ error: "subject is required" });
    }

    const input = {
      operation: "send",
      send: {
        sender: sender || "tui-user",
        recipient,
        subject,
        body: body || "",
        kind: kind || "agent.reply",
        priority: priority || 3,
        ack_required: ack_required || false,
        headers: headers || {},
      },
    };

    const result = runSkill("mailbox/manage", input);

    if (result.error) {
      return res.status(500).json({ error: result.error });
    }

    res.json({
      message_id: result.data?.message_id || result.data?.id,
      status: "sent",
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// POST /api/mailbox/:id/ack - Acknowledge a message (mark as read/delegated)
app.post("/api/mailbox/:id/ack", (req, res) => {
  try {
    const messageId = req.params.id;
    const { actor_id } = req.body;

    const input = {
      operation: "ack",
      ack: {
        actor_id: actor_id || "tui-user",
        message_ids: [messageId],
      },
    };

    const result = runSkill("mailbox/manage", input);

    if (result.error) {
      return res.status(500).json({ error: result.error });
    }

    res.json({
      acknowledged: true,
      message_id: messageId,
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// ============================================================================
// Agent Management Endpoints (Phase 5: Multi-Agent Orchestration)
// ============================================================================

// POST /api/agents - Spawn a new agent
app.post("/api/agents", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  try {
    const {
      ns,
      role,
      parent_id,
      prompt,
      skills_allow,
      policy,
      share_bb,
      llm_provider,
      llm_model,
    } = req.body;

    if (!ns) {
      return res.status(400).json({ error: "ns (namespace) is required" });
    }

    // Generate agent ID
    const id = `agent-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;

    // Ensure database and table exist
    const db = new Database(dbPath);
    db.exec(`
      CREATE TABLE IF NOT EXISTS agents (
        id TEXT PRIMARY KEY,
        parent_id TEXT,
        ns TEXT UNIQUE NOT NULL,
        role TEXT,
        prompt TEXT,
        skills_allow TEXT DEFAULT '*',
        policy TEXT DEFAULT '',
        share_bb TEXT DEFAULT 'scoped',
        state TEXT DEFAULT 'starting',
        llm_provider TEXT,
        llm_model TEXT,
        created_at TEXT NOT NULL,
        heartbeat_at TEXT
      )
    `);

    // Insert the new agent
    db.prepare(`
      INSERT INTO agents (id, parent_id, ns, role, prompt, skills_allow, policy, share_bb, state, llm_provider, llm_model, created_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'starting', ?, ?, ?)
    `).run(
      id,
      parent_id || null,
      ns,
      role || "agent",
      prompt || null,
      skills_allow || "*",
      policy || "",
      share_bb || "scoped",
      llm_provider || null,
      llm_model || null,
      new Date().toISOString()
    );

    db.close();

    // Broadcast SSE event
    broadcastEvent("agent", { id, state: "starting", ns, role });

    res.status(201).json({
      agent: {
        id,
        ns,
        role: role || "agent",
        state: "starting",
        parent_id: parent_id || null,
      },
    });
  } catch (err) {
    if (err.message?.includes("UNIQUE constraint failed")) {
      return res.status(409).json({ error: `Agent with namespace '${req.body.ns}' already exists` });
    }
    res.status(500).json({ error: err.message });
  }
});

// DELETE /api/agents/:id - Stop/kill an agent
app.delete("/api/agents/:id", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  if (!existsSync(dbPath)) {
    return res.status(404).json({ error: "Agents database not found" });
  }

  try {
    const db = new Database(dbPath);
    const agentId = req.params.id;

    // Check if agent exists
    const agent = db.prepare("SELECT id, ns, state FROM agents WHERE id = ?").get(agentId);
    if (!agent) {
      db.close();
      return res.status(404).json({ error: "Agent not found" });
    }

    // Update agent state to 'stopped'
    db.prepare("UPDATE agents SET state = 'stopped', heartbeat_at = ? WHERE id = ?")
      .run(new Date().toISOString(), agentId);

    db.close();

    // Broadcast SSE event
    broadcastEvent("agent", { id: agentId, state: "stopped", ns: agent.ns });

    res.json({
      stopped: true,
      agent_id: agentId,
      previous_state: agent.state,
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// PATCH /api/agents/:id - Update agent state
app.patch("/api/agents/:id", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  if (!existsSync(dbPath)) {
    return res.status(404).json({ error: "Agents database not found" });
  }

  try {
    const db = new Database(dbPath);
    const agentId = req.params.id;
    const { state } = req.body;

    // Check if agent exists
    const agent = db.prepare("SELECT id, ns FROM agents WHERE id = ?").get(agentId);
    if (!agent) {
      db.close();
      return res.status(404).json({ error: "Agent not found" });
    }

    // Update agent state
    if (state) {
      db.prepare("UPDATE agents SET state = ?, heartbeat_at = ? WHERE id = ?")
        .run(state, new Date().toISOString(), agentId);
    }

    db.close();

    // Broadcast SSE event
    broadcastEvent("agent", { id: agentId, state, ns: agent.ns });

    res.json({
      updated: true,
      agent_id: agentId,
      state,
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// POST /api/agents/:id/message - Send a message to a specific agent
app.post("/api/agents/:id/message", (req, res) => {
  const dbPath = join(AGENTCTL_HOME, "storage", "agents.db");

  if (!existsSync(dbPath)) {
    return res.status(404).json({ error: "Agents database not found" });
  }

  try {
    const db = openDB(dbPath);
    const agentId = req.params.id;

    // Get agent namespace
    const agent = db.prepare("SELECT ns FROM agents WHERE id = ?").get(agentId);
    db.close();

    if (!agent) {
      return res.status(404).json({ error: "Agent not found" });
    }

    const { subject, body, kind, priority, sender } = req.body;

    if (!subject) {
      return res.status(400).json({ error: "subject is required" });
    }

    const input = {
      operation: "send",
      send: {
        sender: sender || "tui-user",
        recipient: agent.ns,
        subject,
        body: body || "",
        kind: kind || "agent.cmd",
        priority: priority || 3,
        ack_required: false,
      },
    };

    const result = runSkill("mailbox/manage", input);

    if (result.error) {
      return res.status(500).json({ error: result.error });
    }

    res.json({
      message_id: result.data?.message_id || result.data?.id,
      recipient: agent.ns,
      status: "sent",
    });
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// Run database migrations at startup
function runMigrations() {
  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");
  if (existsSync(memoryDB)) {
    const db = new Database(memoryDB);
    try {
      db.exec(`ALTER TABLE named_memory ADD COLUMN pinned_at TEXT DEFAULT NULL`);
      console.log("Migration: added pinned_at column to named_memory");
    } catch {
      // Column already exists
    }
    db.close();
  }

  const mailboxDbPath = join(AGENTCTL_HOME, "storage", "mailbox.db");
  const mailboxDb = new Database(mailboxDbPath);
  try {
    mailboxDb.exec(`
      CREATE TABLE IF NOT EXISTS mailbox (
        id TEXT PRIMARY KEY,
        from_ns TEXT NOT NULL,
        to_ns TEXT NOT NULL,
        type TEXT NOT NULL,
        ttl_ms INTEGER NOT NULL DEFAULT 0,
        headers TEXT,
        payload TEXT,
        visible_at INTEGER NOT NULL,
        attempt INTEGER NOT NULL DEFAULT 0,
        ts INTEGER NOT NULL,
        session_id TEXT,
        workspace TEXT,
        agent_id TEXT
      );
      CREATE INDEX IF NOT EXISTS idx_mailbox_to_ns_visible ON mailbox(to_ns, visible_at);
    `);
    console.log("Migration: ensured mailbox table exists");
  } catch (err) {
    console.error("Migration: mailbox table error:", err.message);
  }
  mailboxDb.close();
}

// Run migrations before starting server
runMigrations();

app.listen(PORT, () => {
  console.log(`API server running on http://localhost:${PORT}`);
});
