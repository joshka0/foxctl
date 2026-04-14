# SPEC-XXX: Test Watcher & Feedback Hooks

**Status:** Draft\
**Last Updated:** 2025-11-27

---

## 1. Overview

This spec defines a **generic test watcher** mechanism and a **PostTool feedback
hook** so that:

- foxctl can run one or more **per-workspace test commands** in the background
  ("watch mode"),
- test results are persisted in a small SQLite-backed store, and
- Claude receives **immediate, advisory feedback** about failing tests via a
  `PostToolUse` hook after code edits, without having to explicitly run tests.

The design is **language-agnostic** and must support at least:

- Go (`go test ./...`),
- JS/TS (`npm test`, `pnpm test`, `vitest run`, etc.), and
- Python (`pytest`, etc.).

---

## 2. Goals & Non-Goals

### 2.1 Goals

- **G1: Immediate Failure Feedback**\
  Claude should learn **quickly** when tests are failing in the current
  workspace, ideally on the next tool call after a failing run.

- **G2: Language-Agnostic Watchers**\
  Test commands are arbitrary shell commands (e.g. `go test`, `npm test`,
  `pytest`), configured per workspace. No hard-coded framework logic is required
  in foxctl.

- **G3: CPU-Friendly Behavior**\
  Watchers must not aggressively re-run tests on every save. Debouncing, minimum
  intervals, and no-overlap semantics are required.

- **G4: Hook-First UX**\
  Feedback is surfaced via a dedicated `hooks/test_feedback` skill wired to
  `PostToolUse`, keeping Claude’s integration surface consistent with other
  hooks (`task_guard`, `knowledge_router`). Unlike `task_guard`/`file_guard`,
  which may lead to guard errors such as `E_GUARD_VIOLATION` when propagated to
  tools (see `archive/specs/dspy_go_agents.md` §11.3, legacy runtime), `hooks/test_feedback` is
  **advisory-only** and MUST NOT block writes or change tool error codes.

- **G5: Simple, Queryable Storage**\
  Test watcher state is persisted in SQLite alongside other foxctl storage so
  hooks and CLI commands can query the latest status per workspace.

### 2.2 Non-Goals

- Running a full CI-like matrix (OS combinations, long-running integration
  tests).
- Enforcing that tests pass before writes occur (this remains advisory in v1).
- Deep, framework-specific parsing of test output (Jest, Pytest, etc.). v1 only
  needs basic extraction of file, line, test name, and short message when
  feasible.

---

## 3. Terminology

| Term              | Definition                                                                                      |
| ----------------- | ----------------------------------------------------------------------------------------------- |
| **Watcher**       | A configured test command (e.g. `go test ./...`) attached to a workspace.                       |
| **Watcher run**   | A single execution of a watcher command (with status, timing, output).                          |
| **Test status**   | The most recent known result for a given `(workspace, watcher_id)`.                             |
| **Feedback hook** | The `hooks/test_feedback` skill, wired on `PostToolUse`, that surfaces test failures to Claude. |

---

## 4. Configuration

### 4.1 Test Watch Config File

Per workspace, test watching is configured via a YAML file under the foxctl
workspace config directory (`.foxctl/`). This file is **owned by foxctl**
and is harness-agnostic; harnesses (Claude, Cursor, etc.) may read it but SHOULD
NOT write other agents' config into harness-specific directories such as
`.claude/`.

```yaml
# .foxctl/test-watch.yaml

# Optional default debounce for all watchers (overridden per-watcher).
debounce: 2s

watchers:
  - id: go
    command: "go test ./..."
    include:
      - "**/*.go"
      - "go.mod"
      - "go.sum"
    debounce: 2s # Optional override
    min_interval: 20s # Minimum time between runs for this watcher

  - id: js
    command: "npm test -- --watch=false"
    include:
      - "**/*.js"
      - "**/*.jsx"
      - "**/*.ts"
      - "**/*.tsx"
      - "package.json"
    debounce: 3s
    min_interval: 30s

  - id: python
    command: "pytest -q"
    include:
      - "**/*.py"
      - "pyproject.toml"
      - "requirements*.txt"
    debounce: 3s
    min_interval: 30s
```

### 4.2 Semantics

- **`watchers[].id`**\
  Logical name for the watcher (e.g. `go`, `js`, `python`). Must be unique per
  workspace.

- **`watchers[].command`**\
  Shell command to run tests. It must:
  - Exit with status `0` on success, non-zero on failure/error.
  - Emit human-readable output on stderr/stdout.

- **`include` / `exclude`**\
  Glob patterns (workspace-relative) used to determine when a watcher is
  relevant for a file change. If no `include` is specified, the watcher is
  considered for **all** changes.

- **`debounce`**\
  Minimum idle period after the _last relevant change_ before a watcher run is
  scheduled. If more matching changes arrive before the timer expires, the timer
  is reset.

- **`min_interval`**\
  Minimum wall-clock time between the _start_ of consecutive runs for the same
  watcher. This prevents CPU thrashing during rapid edits.

- **Defaults**\
  If unspecified, implementations SHOULD use conservative defaults, e.g.
  `debounce: 2s`, `min_interval: 20s`.

### 4.3 CLI Configuration: `foxctl test-watch`

While `.foxctl/test-watch.yaml` is the source of truth, users and agents
SHOULD normally configure watchers via dedicated CLI commands rather than
editing YAML directly.

Proposed subcommands (v1):

```bash
foxctl test-watch list \
  [--workspace <path>]

foxctl test-watch add \
  --id <id> \
  --command <command> \
  [--include <glob> ...] \
  [--exclude <glob> ...] \
  [--debounce <duration>] \
  [--min-interval <duration>] \
  [--workspace <path>]

foxctl test-watch remove \
  --id <id> \
  [--workspace <path>]
```

Semantics:

- `test-watch list` reads `.foxctl/test-watch.yaml` for the resolved workspace
  and prints the configured watchers.
- `test-watch add`:
  - Resolves the workspace (current dir or `--workspace`).
  - Loads `.foxctl/test-watch.yaml` (creating it if missing).
  - Upserts a watcher with the given `id`, `command`, and fields into the
    `watchers:` list.
  - Writes back a human-editable YAML file.
- `test-watch remove` deletes the watcher with the given `id` from the workspace
  config.

These commands provide a **stable, agent-friendly API** for configuring
workspace-specific test behavior without requiring agents to understand the YAML
structure.

---

## 5. CLI: `foxctl watch tests`

### 5.1 Command

```bash
foxctl watch tests \
  [--workspace <path>] \
  [--once]             \
  [--status-only]
```

**Flags (v1):**

- `--workspace` — Workspace root (default: current directory / detected root).
- `--once` — Run each configured watcher once and exit (no file watching).
  Useful for scripts/CI.
- `--status-only` — Print the latest known status for each watcher and exit (no
  runs).

### 5.2 Behavior

- Load `.foxctl/test-watch.yaml` from the workspace root (or a path provided
  via a future `--config` flag). If the file does not exist, print a helpful
  message and exit non-zero.
- Resolve `workspace_id` using the same mechanism as `todo` / hooks (see
  `task_hooks_memory.md`).
- For each watcher in `watchers`:
  - Initialize internal state: last run time, pending-changes flag, etc.

**Watch mode (default):**

- Monitor the workspace for file changes (implementation detail: fsnotify or
  polling + hashing).
- For each change event:
  - Determine which watchers are relevant (based on `include` / `exclude`).
  - For each relevant watcher:
    - Mark that watcher as having pending changes.
    - Reset or start its debounce timer.
- When a watcher's debounce timer fires:
  - If `min_interval` has not yet elapsed since the last run **start time**,
    delay until it does.
  - If the watcher is already running, set a flag to run it once more after
    completion instead of starting immediately.
  - When safe, execute `command` as a child process with:
    - Working directory = workspace root.
    - Inherited environment (plus any foxctl-specific vars, e.g.
      `AGENTCTL_WORKSPACE`).
  - Capture:
    - Exit code.
    - Start and end timestamps.
    - Tail of stdout/stderr (`raw_tail`, limited to e.g. 8–16 KiB).
    - Optional structured test failures (see §7.3).
  - Persist the resulting status in the **test status store** (see §6).

**`--once` mode:**

- Do not watch for file changes. For each watcher, run the command once and
  persist status.

**`--status-only` mode:**

- Query the test status store and print the latest entry per watcher for the
  workspace. No commands are run.

---

## 6. Storage: Test Status Store

### 6.1 Database

- File: `~/.foxctl/storage/test_watch.db`
- Backend: SQLite via the same storage layer used for `tasks` and `knowledge`.

### 6.2 Tables (v1)

A minimal schema focusing on the **latest status** per watcher:

```sql
-- Latest status per (workspace, watcher_id)
CREATE TABLE test_status (
    workspace_id   TEXT NOT NULL,
    watcher_id     TEXT NOT NULL,

    status         TEXT NOT NULL,     -- 'unknown' | 'pass' | 'fail' | 'error' | 'running'
    command        TEXT NOT NULL,

    started_at     TEXT,              -- RFC3339Nano, last run start
    finished_at    TEXT,              -- RFC3339Nano, last run finish (NULL if running)

    summary        TEXT,              -- Short human summary (e.g. "1 failed, 14 passed")
    failures_json  TEXT,              -- JSON-encoded list of failures (see below)
    raw_tail       TEXT,              -- Last N KiB of test output (stderr/stdout)

    PRIMARY KEY (workspace_id, watcher_id)
);

CREATE INDEX idx_test_status_workspace ON test_status(workspace_id);
```

`failures_json` encodes a list of **loosely structured failures**:

```json
[
  {
    "name": "TestHandler_ValidInput",
    "file": "internal/api/handler_test.go",
    "line": 123,
    "message": "expected status 200, got 500"
  }
]
```

Notes:

- Implementations MAY skip structured parsing for some frameworks and only fill
  `summary` + `raw_tail`.
- History of runs can be added later via a `test_runs` table; v1 only requires
  the latest status.

---

## 7. Test Output Parsing (Guidelines)

The watcher runtime is responsible for filling `summary`, `failures_json`, and
`raw_tail` from the command output.

### 7.1 Minimal Requirements

- Determine `status` based on exit code:
  - `0` → `pass`
  - Non-zero → `fail` or `error` (implementation may distinguish known test
    failures vs. infra errors).
- Populate `raw_tail` with the last N KiB of combined stdout/stderr.
- Populate `summary` with a single line (e.g. `"1 failed, 14 passed"`, or
  `"command exited with status 1"`).

### 7.2 Optional Structured Failures

Implementations MAY parse common frameworks to extract richer failure
information:

- **Go (`go test`)**: parse `--- FAIL: ... (X.YYs)` blocks, file:line, message.
- **Pytest**: parse failure headers with `file:line` and test names.
- **Jest/Vitest**: parse summaries and failing test entries.

When such parsing is implemented, at least the following fields SHOULD be
included in each failure:

- `name` (test name / description),
- `file` (workspace-relative path),
- `line` (best-effort integer),
- `message` (single-line or short multi-line string summarizing the failure).

---

## 8. Hook: `hooks/test_feedback`

### 8.1 Event & Wiring

- **Event:** `PostToolUse`\
  Triggered after tools that can mutate the workspace (e.g.
  edit/write/multi-edit/git apply).

- **Wrapper:** A standard Claude hook wrapper under `.claude/hooks/`, similar to
  `task-guard.sh` and `knowledge-router.sh`, that:
  - Reads hook payload JSON from stdin.
  - Invokes `foxctl run hooks/test_feedback --input-file -`.
  - Emits `data.hook_output` as the hook’s JSON output.

- **Settings:** The harness example SHOULD wire this hook for `PostToolUse`
  events on write-like tools once implemented.

### 8.2 Input

`hooks/test_feedback` receives the standard `hook.Input` structure (see
`task_hooks_memory.md`), including at least:

```json
{
  "event": "PostToolUse",
  "workspace_root": "/path/to/project",
  "session_id": "...",
  "tool_name": "fs/write",
  "tool_input": { "path": "src/components/Button.tsx" },
  "tool_response": {/* tool-dependent */}
}
```

The hook implementation MAY attempt to infer relevant file paths from
`tool_input` / `tool_response` when present.

### 8.3 Behavior (v1)

1. **Resolve workspace:**\
   Derive `workspace_id` from `WorkspaceRoot` using the same logic as other
   hooks.

2. **Load latest test status:**\
   Query `test_status` for all `(workspace_id, watcher_id)` rows.

3. **Filter relevant watchers:**
   - Consider only watchers whose `status` is `fail` or `error`.
   - Optionally, if files touched by the tool are known, prefer failures whose
     `file` is under the same subtree as those paths.

4. **Suppress noise:**
   - Implementations SHOULD avoid repeating identical feedback too frequently. A
     simple strategy is to track the last surfaced
     `(workspace_id, watcher_id, finished_at, status)` in memory and skip if
     unchanged.

5. **Construct feedback:**
   - Build a short `context` string summarizing the most relevant failures,
     e.g.:

     > Tests are currently failing (watcher `js`):\
     >
     > - `Button component › should render primary variant` —
     >   `src/components/Button.test.tsx:42` (Expected primary class, got
     >   secondary).\
     > - 1 more failure. See raw output for details.

   - Include details in `meta`, for example:

     ```json
     {
       "watchers": [
         {
           "watcher_id": "js",
           "status": "fail",
           "summary": "1 failed, 14 passed",
           "failures": [
             {
               "name": "Button component › should render primary variant",
               "file": "src/components/Button.test.tsx",
               "line": 42,
               "message": "Expected primary class, got secondary"
             }
           ]
         }
       ]
     }
     ```

6. **Emit hook output:**

   - `Decision` MUST be `"none"` in v1 (advisory only).
   - `Reason` SHOULD summarize the main point (e.g.
     `"tests failing in watcher js"`).

### 8.4 Output Shape

```json
{
  "decision": "none",
  "reason": "tests failing in watcher js",
  "context": "Tests are currently failing (watcher `js`): ...",
  "meta": {
    "watchers": [
      {
        "watcher_id": "js",
        "status": "fail",
        "summary": "1 failed, 14 passed",
        "failures": [
          {
            "name": "Button component › should render primary variant",
            "file": "src/components/Button.test.tsx",
            "line": 42,
            "message": "Expected primary class, got secondary"
          }
        ]
      }
    ]
  }
}
```

---

## 9. CPU & Resource Considerations

To avoid saturating CPU from frequent test runs, implementations MUST:

- Respect `debounce` and `min_interval` for each watcher.
- Avoid running more than one instance of the same watcher concurrently:
  - If a run is in progress when new changes arrive, set a flag to run **once
    more** after completion (subject to `min_interval`), instead of starting
    immediately.
- Encourage users (via docs) to configure **fast** test commands for watch mode
  (unit tests, or narrower path patterns) and reserve heavy suites for manual or
  CI runs.

---

## 10. Interaction with Task & Memory Specs

- The test watcher and feedback hook are **task-agnostic** in v1; they operate
  at the `(workspace_id, watcher_id)` level.
- A future extension MAY:
  - Tag test runs with `task_id` (when an active task exists),
  - Store test outputs as task-scoped memory entries (see
    `task_hooks_memory.md`), and
  - Enrich task timelines with test pass/fail history.

---

## 11. Rollout Plan (High-Level)

1. **Spec Finalization**
   - Validate watcher config format and test status schema.

2. **Test Status Store Implementation**
   - Add `internal/storage/testwatch` package.
   - Implement migrations for `test_status` table.

3. **`foxctl watch tests` CLI**
   - Implement watch and `--once` / `--status-only` modes.
   - Add basic parsers for Go, Pytest, and Jest/Vitest where cheap; otherwise
     default to `summary` + `raw_tail`.

4. **`hooks/test_feedback` Skill**
   - Implement as an exec skill similar to `hooks/task_guard` and
     `hooks/knowledge_router`.
   - Add bash wrapper under `.claude/hooks/` and wire into
     `.claude/settings.json` for `PostToolUse`.

5. **Harness Integration**
   - Provide an example `.foxctl/test-watch.yaml` in the harness repo.
   - Document the workflow in `AGENTCTL.md`.

6. **Future Enhancements**
   - Optional history of runs (`test_runs` table).
   - Deeper framework-specific parsing for richer failure reporting.
   - Task-aware tagging of test runs.

---

## 12. Open Questions

1. **Framework-Specific Parsing:**\
   How far should v1 go in parsing framework-specific output vs. relying on
   `raw_tail` and a simple `summary`?

2. **Multiple Workspaces / Monorepos:**\
   Should a single `foxctl watch tests` instance support multiple workspace
   roots (e.g. Nx monorepos), or is one instance per workspace sufficient for
   now?

3. **Opt-In vs. Auto-Start:**\
   Should the harness auto-start `foxctl watch tests` when Claude attaches to
   a workspace, or should this always be a manual step for the user?
