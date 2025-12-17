# Observability Event Stream (NDJSON)

This document specifies the optional **on-disk event stream** that agentctl and
skills MAY emit for observability. The goal is to provide a **backend-agnostic
surface** that can be scraped or tailed by separate processes (e.g. OTEL
Collector, Prometheus node_exporter via textfile collector), without coupling
Core to any specific monitoring stack.

## 1. High-Level Design

- **Default behavior:**
  - Envelopes (Protocol v1) remain the only thing written to **stdout**.
  - Structured logs remain on **stderr** (zerolog JSON), as today.
- **Optional behavior (this doc):**
  - When enabled, agentctl and skills **append NDJSON events** to files under a
    configured observability directory.
  - Each line is a single JSON object (no arrays, no pretty-printing).
  - Files are **append-only**; rotation and shipping are the operators
    responsibility.
- **Backends:**
  - OTEL, Prometheus, Loki, etc. are all expected to run **out of process** and
    consume these NDJSON files.

Nothing in this document changes the Protocol v1 envelope contract.

## 2. Configuration & Directory Layout

### 2.1 Enabling on-disk events

An environment variable (or equivalent config field) controls the root
observability directory:

- `AGENTCTL_OBS_DIR=/path/to/observability`

If `AGENTCTL_OBS_DIR` is **unset or empty**, on-disk events MUST be disabled and
all writers MUST be no-ops.

If `AGENTCTL_OBS_DIR` is set, event writers may create the following layout:

```text
$AGENTCTL_OBS_DIR/
  events/
    core.ndjson             # future: CLI / core events
    code_swe_grep.ndjson    # SWE Grep events (this spec)
    ...                     # future skills: <command>.ndjson
```

Skills and subsystems choose an appropriate `<name>.ndjson` but MUST use only
`[a-z0-9_./-]` in the file name to avoid portability issues.

### 2.2 File semantics

- Files are **append-only** NDJSON streams.
- Writers MUST NOT truncate or rotate files.
- Rotation, retention, and shipping are handled by external tools (logrotate,
  OTEL Collector, etc.).
- Writers SHOULD tolerate missing directories by calling `mkdir -p` on
  `$AGENTCTL_OBS_DIR/events` before first write.

## 3. SWE Grep Events (`code_swe_grep.ndjson`)

This section defines the event schema for the `code/swe_grep` skill. Each
successful run MAY emit exactly one event into:

```text
$AGENTCTL_OBS_DIR/events/code_swe_grep.ndjson
```

### 3.1 Event schema

Each line is a JSON object with the following fields:

```jsonc
{
	"ts": "2025-12-03T17:19:11Z", // RFC3339 UTC timestamp
	"command": "code/swe_grep", // envelope command
	"workspace_id": "test-ws", // logical workspace id from input
	"question_hash": "a1b2c3d4", // SHA-256(question) hex, truncated
	"candidates": 3, // len(input.candidates)
	"files_considered": 3, // files attempted after validation
	"files_relevant": 2, // files that produced >= 1 snippet
	"snippets_emitted": 5, // total snippets across all files
	"has_artifact": true, // true if CAS artifact present
	"artifact_kind": "application/x-swe-grep-snippets+ndjson",
	"duration_ms": 187, // optional wall-clock duration
	"source": "run" // "run" | "cache" | "job" | ...
}
```

Notes:

- **Privacy:**
  - `question_hash` is derived from the full question text but MUST be a
    truncated hash (e.g. first 8 hex chars of SHA-256). Raw question text MUST
    NOT appear in the event.
  - Snippet contents, file contents, and raw paths MUST NOT appear. The event
    only carries aggregate counts and booleans.
- **Consistency:**
  - `command` MUST always be `"code/swe_grep"` for this file.
  - `has_artifact` MUST be `true` when the envelope includes `data.artifact` /
    `meta.cas_digest`, and `false` otherwise.
  - When `has_artifact == false`, `artifact_kind` SHOULD be omitted (`null` or
    missing).

### 3.2 Suggested Go struct (non-normative)

The following struct illustrates the schema in Go:

```go
// SweGrepEvent is the NDJSON observation for a single code/swe_grep run.
type SweGrepEvent struct {
    Ts              time.Time `json:"ts"`
    Command         string    `json:"command"`          // "code/swe_grep"
    WorkspaceID     string    `json:"workspace_id"`
    QuestionHash    string    `json:"question_hash"`    // 8+ hex chars
    Candidates      int       `json:"candidates"`
    FilesConsidered int       `json:"files_considered"`
    FilesRelevant   int       `json:"files_relevant"`
    SnippetsEmitted int       `json:"snippets_emitted"`
    HasArtifact     bool      `json:"has_artifact"`
    ArtifactKind    string    `json:"artifact_kind,omitempty"`
    DurationMS      int64     `json:"duration_ms,omitempty"`
    Source          string    `json:"source"`           // "run" | "cache" | ...
}
```

This struct is illustrative only; the wire contract is the JSON field set above.

## 4. Event Writer Helper (Conceptual)

Implementations are encouraged to use a shared helper to write events, so that
skills do not reimplement append/dir logic.

```go
// writeEvent appends an NDJSON-encoded event to
// $AGENTCTL_OBS_DIR/events/<name>.ndjson if configured.
func writeEvent(ctx context.Context, name string, v any) error
```

### 4.1 Helper behavior (normative)

Given `name` (e.g. `"code_swe_grep"`) and an event value `v`:

1. **Configuration check**
   - Resolve observability directory from configuration, e.g.:
     - `AGENTCTL_OBS_DIR` env var, or
     - an equivalent field in the runtime config.
   - If the directory is unset or empty, the helper MUST return `nil` without
     performing any I/O.

2. **Path resolution**
   - Compute:

     ```text
     eventsDir = $AGENTCTL_OBS_DIR/events
     filePath  = eventsDir + "/" + name + ".ndjson"
     ```

   - The helper MUST NOT allow `name` to escape this directory (no `..`).

3. **Directory creation**
   - Ensure `eventsDir` exists using `os.MkdirAll(eventsDir, 0o755)`.

4. **Append write**
   - Open `filePath` with `O_CREATE | O_WRONLY | O_APPEND` and `0644` perms.
   - Encode the event as a single JSON object followed by `\n`:

     ```go
     enc := json.NewEncoder(f)
     if err := enc.Encode(v); err != nil { /* handle */ }
     ```

5. **Error handling**
   - The helper SHOULD log a short warning to stderr (zerolog) if writing fails.
   - For skill-level observability, errors SHOULD NOT fail the user-visible
     operation; the helper MAY swallow errors after logging.

### 4.2 Question hashing helper (non-normative)

A small helper can produce `question_hash` safely:

```go
func hashQuestion(q string) string {
    sum := sha256.Sum256([]byte(q))
    // First 8 hex chars are usually enough for correlation without being too identifying.
    return hex.EncodeToString(sum[:4])
}
```

## 5. Consumption Patterns (Out of Process)

The NDJSON event stream is designed to be consumed by external tools.

### 5.1 OTEL Collector

- Use a `filelog` receiver pointing at `$AGENTCTL_OBS_DIR/events/*.ndjson`.
- Each line is a log record with attributes matching the JSON fields.
- Standard OTEL processors can derive metrics such as:
  - `swe_grep_requests_total`
  - `swe_grep_snippets_emitted_total`
  - `swe_grep_duration_seconds`

### 5.2 Prometheus via node_exporter

- Run a small sidecar (or future `agentctl obs export` helper) that:
  - Tails the NDJSON files and aggregates counters in memory.
  - Periodically writes a textfile-format `.prom` snapshot into a directory
    watched by node_exporters textfile collector.
- This keeps the agentctl binary free of Prometheus-specific formatting.

## 6. Extension Guidelines

When adding observability events for new skills or subsystems:

- Reuse the same **directory and NDJSON conventions**.
- Prefer **small, aggregate fields** (counts, booleans, hashes) over raw
  payloads.
- Avoid logging secrets, raw questions, or large blobs.
- Keep the event schema stable; if breaking changes are required, prefer
  **additive** fields or a new `<name>.ndjson` stream over in-place reuse.
