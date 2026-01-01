import express from "express";
import cookieParser from "cookie-parser";
import cors from "cors";
import { execSync } from "child_process";
import Database from "better-sqlite3";
import { readdirSync, statSync, readFileSync, existsSync, writeFileSync } from "fs";
import { join, basename, resolve } from "path";
import { homedir } from "os";

const app = express();
const PORT = process.env.PORT || 8090;
const AGENTCTL_HOME = process.env.AGENTCTL_HOME || join(homedir(), ".agentctl");
const WORKSPACE_COOKIE = "agentctl_workspace";

app.use(cors({ origin: true, credentials: true }));
app.use(cookieParser());
app.use(express.json());

// Helper: get workspace from cookie
function getWorkspace(req) {
  return req.cookies[WORKSPACE_COOKIE] || "";
}

// Helper: run agentctl skill
// Uses AGENTCTL_BIN_DIR env var or looks for agentctl in PATH
const AGENTCTL_BIN = process.env.AGENTCTL_BIN || "agentctl";

function runSkill(skill, input) {
  try {
    const cmd = `${AGENTCTL_BIN} run ${skill} --input '${JSON.stringify(input)}'`;
    const result = execSync(cmd, {
      encoding: "utf-8",
      timeout: 30000,
    });
    return JSON.parse(result);
  } catch (err) {
    console.error(`Skill ${skill} failed:`, err.message);
    return { error: err.message, data: {} };
  }
}

// Helper: open SQLite database (read-only)
function openDB(dbPath) {
  return new Database(dbPath, { readonly: true });
}

// Known databases
const knownDatabases = {
  "tasks.db": "Tasks",
  "agents.db": "Agents",
  "jobs.db": "Jobs",
  "blackboard.db": "Blackboard",
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

// Validate database path is under AGENTCTL_HOME
function validateDBPath(dbPath) {
  // Use resolve() to canonicalize the path and prevent path traversal
  const resolved = resolve(dbPath);
  const agentctlHome = resolve(AGENTCTL_HOME);
  if (!resolved.startsWith(agentctlHome + "/") && resolved !== agentctlHome) {
    throw new Error("Database path must be under ~/.agentctl");
  }
  return resolved;
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
    const { renameSync } = require("fs");
    renameSync(tempPath, filePath);
  } catch (err) {
    // Clean up temp file on error
    try {
      const { unlinkSync } = require("fs");
      unlinkSync(tempPath);
    } catch {}
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
  } catch (err) {
    res.status(404).json({ error: "Job not found" });
  }
});

// Tasks
app.get("/api/tasks", (req, res) => {
  const workspace = getWorkspace(req);
  const limit = parseInt(req.query.limit) || 50;

  const input = { operation: "list" };
  if (workspace) input.workspace_id = workspace;

  const result = runSkill("todo/manage", input);
  const tasks = (result.data?.tasks || []).slice(0, limit);

  res.json({ tasks });
});

// Stats
app.get("/api/stats", (req, res) => {
  const workspace = getWorkspace(req);

  // Get task stats
  const taskInput = { operation: "list" };
  if (workspace) taskInput.workspace_id = workspace;
  const taskResult = runSkill("todo/manage", taskInput);
  const tasks = taskResult.data?.tasks || [];

  const taskStats = {
    total: tasks.length,
    pending: tasks.filter((t) => t.status === "pending").length,
    in_progress: tasks.filter((t) => t.status === "in_progress").length,
    completed: tasks.filter((t) => t.status === "completed").length,
  };

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
  } catch {}

  // Frontend expects job stats at top level
  res.json({
    total: jobTotal,
    by_state: byState,
    by_command: byCommand,
    recent: { last_hour: lastHour, last_day: lastDay },
    task_stats: taskStats,
  });
});

// Insights
app.get("/api/insights", (req, res) => {
  const workspace = getWorkspace(req);

  const input = { operation: "graph_insights" };
  if (workspace) input.workspace_id = workspace;

  const result = runSkill("todo/manage", input);
  // Flatten: result.data.insights contains the actual graph data
  const insights = result.data?.insights || {};
  res.json({
    nodes: insights.nodes || [],
    cycles: insights.cycles || [],
    topological_order: insights.topological_order || [],
  });
});

// Mailbox
app.get("/api/mailbox", (req, res) => {
  const actor = req.query.actor || "";
  const limit = parseInt(req.query.limit) || 50;

  const input = { operation: "list", limit };
  if (actor) input.actor_id = actor;

  const result = runSkill("mailbox/manage", input);
  res.json({ messages: result.data?.messages || [] });
});

// Reservations
app.get("/api/reservations", (req, res) => {
  const result = runSkill("reservations/manage", { operation: "list" });
  res.json({ reservations: result.data?.reservations || [] });
});

// Blackboard
app.get("/api/blackboard", (req, res) => {
  const ns = req.query.ns || "";
  const topic = req.query.topic || "";
  const limit = parseInt(req.query.limit) || 50;

  const input = { operation: "list", limit };
  if (ns) input.namespace = ns;
  if (topic) input.topic = topic;

  const result = runSkill("blackboard/manage", input);
  res.json({ records: result.data?.records || [] });
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
      } catch {}
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

// Codemaps - list all codemaps from memory store
app.get("/api/codemaps", (req, res) => {
  const workspace = getWorkspace(req);
  const limit = Math.min(parseInt(req.query.limit) || 50, 200);

  const memoryDB = join(AGENTCTL_HOME, "storage", "memory.db");

  try {
    const db = openDB(memoryDB);

    let query = `
      SELECT name, summary, workspace, created_at, result
      FROM named_entries
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
      } catch {}

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
      .prepare(`SELECT name, summary, workspace, created_at, result FROM named_entries WHERE name = ?`)
      .get(codemapName);

    db.close();

    if (!row) {
      return res.status(404).json({ error: "Codemap not found" });
    }

    // Parse the result JSON
    let codemap = {};
    try {
      codemap = JSON.parse(row.result || "{}");
    } catch {}

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
      .prepare(`DELETE FROM named_entries WHERE name = ?`)
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

app.listen(PORT, () => {
  console.log(`API server running on http://localhost:${PORT}`);
});
