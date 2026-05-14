---
title: Protocol v1 envelopes
description: Canonical envelope shape, status rules, artifactization, error catalog, and streaming for foxctl I/O.
---

Protocol v1 is the wire contract for foxctl I/O. Every skill execution, plugin call, job result, and agent message uses the same JSON envelope. Changing it without a spec update breaks hooks, GUI clients, golden tests, and downstream tools.

## Envelope shape

```json
{
  "version": 1,
  "status": "ok",
  "command": "namespace/verb",
  "data": {},
  "meta": {
    "ts": "2026-05-12T00:00:00Z",
    "duration_ms": 153,
    "runner": "exec",
    "workspace": "/path/to/workspace",
    "job_id": "01H...",
    "trace_id": "uuid-or-ulid",
    "profiles": ["core/v1"],
    "source": "run",
    "cas_digest": "sha256:...",
    "skill_version": "1.0.0",
    "cache_key": "sha256:..."
  },
  "error": {
    "code": null,
    "message": null,
    "details": {}
  }
}
```

### Field descriptions

#### `version` (integer)

Protocol version. MUST be `1` for Protocol v1.

#### `status` (string)

Tri-state status: `ok`, `error`, or `progress`. See [Status rules](#status-rules) below.

#### `command` (string)

Command identifier following the pattern `namespace/verb` (e.g., `http/openapi`, `fs/ls`, `code/semantic_search`). Must match `^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*$`.

#### `data` (object)

Command-specific payload. Large outputs exceeding the inline threshold (default 32KB) MUST be artifactized. See [Artifactization](#artifactization) below.

#### `meta` (object)

Execution metadata:

| Field | Type | Description |
|---|---|---|
| `ts` | string | RFC3339 UTC timestamp. MUST be present. |
| `duration_ms` | integer | Total wall time in milliseconds. MUST be ≥ 0. |
| `runner` | string | Execution environment: `wasi`, `exec`, `oci`, or `null` |
| `workspace` | string | Sanitized absolute path to workspace |
| `job_id` | string | ULID for async jobs |
| `trace_id` | string | Optional distributed tracing correlation ID |
| `profiles` | array | Active profiles (e.g., `["core/v1"]`, `["core/v1", "agent/v1"]`) |
| `source` | string | How result was obtained: `run`, `cache`, or `memory` |
| `cas_digest` | string | Optional; if set MUST match `data.artifact` |
| `skill_version` | string | Version of the skill that produced the result |
| `cache_key` | string | Cache key on cache hits |
| `seq` | integer | Streaming only: sequence number starting at 0 |
| `final` | boolean | Streaming only: marks last progress event |

#### `error` (object)

Error information. Present even for `ok` status, but with null values:

| Field | Type | Description |
|---|---|---|
| `code` | string | Standardized error code from the catalog |
| `message` | string | Human-readable description |
| `details` | object | Optional machine-readable error context |

## Status rules

| Status | Meaning | Requirements |
|---|---|---|
| `ok` | Terminal success | `error.code` and `error.message` SHOULD be `null` |
| `error` | Terminal failure | `error.code` and `error.message` are required |
| `progress` | Non-terminal progress update | Include `meta.seq` (integer, starting at 0, monotonically increasing) |

### Streaming

Streaming uses **NDJSON** (newline-delimited JSON):

- Each line is a complete JSON envelope
- Zero or more `progress` events, then exactly one terminal `ok` or `error` envelope
- Lines separated by `\n`

Progress events keep payloads small and never include secrets.

## Artifactization

When a skill produces output exceeding the inline threshold (default 32KB), the payload moves to CAS and the envelope returns a summary plus an artifact digest:

```json
{
  "status": "ok",
  "command": "http/openapi",
  "data": {
    "summary": {
      "status_code": 200,
      "kind": "application/json",
      "size_bytes": 1048576,
      "record_count": 247,
      "preview": {
        "first_keys": ["id", "name"],
        "sample_record": { "id": 123, "name": "example" }
      },
      "pagination": {
        "has_more": false,
        "total_pages": 3,
        "total_items": 247,
        "strategy_used": "link"
      }
    },
    "artifact": "sha256:abc123..."
  }
}
```

### Summary requirements

Every artifactized summary MUST include:

- **`size_bytes`** — Total size of artifactized content
- **`kind`** — MIME type or content description
- **`preview`** — Bounded, deterministic sample (first N keys, first record)

For HTTP responses, summaries SHOULD also include `status_code`, `headers`, and pagination metadata. For arrays, include `record_count` and preview samples.

### Preview bounds

- Keep previews under 1KB
- Use deterministic sampling (first N items, not random)
- Never include sensitive data in previews

## Error catalog

All client-visible errors use standardized codes:

| Code | Meaning | Caller remediation |
|---|---|---|
| `EARG` | Invalid or missing parameters | Fix inputs; see `error.details` or `data.hint` |
| `EAUTH` | Authentication failed or credentials missing | Provide or refresh credentials |
| `ERATELIMIT` | 429 after retry budget exhausted | Wait until reset; lower request rate |
| `EPAGINATION` | Auto-detection failed; plugin or fields required | Provide paging config or plugin |
| `ERUNTIME` | Transport, server, or IO errors | Retry later; check network/server |
| `ENOTFOUND` | Resource, memory, or spec not found | Fix resource references |
| `ETIMEOUT` | Operation timed out | Increase timeout or reduce payload |
| `EPOLICY` | Workspace, path, or network policy violation | Narrow request; fix policy config |
| `ESKILLDOWN` | Circuit breaker or disabled skill | Backoff; re-enable when healthy |
| `EPARSE` | JSON parse error or invalid UTF-8 | Fix malformed input |
| `EOUTPUT_TOO_LARGE` | Output exceeded capture limit | Reduce output or increase limit |
| `EENVELOPE` | Invalid or malformed envelope | Fix envelope structure |
| `EIO` | Filesystem or IO error | Check permissions or disk space |
| `ECANCELED` | Job canceled by user | User-initiated cancellation |
| `EOPENAPI` | Spec invalid or operationId missing | Validate spec; pick valid operationId |

### Error envelope example

```json
{
  "version": 1,
  "status": "error",
  "command": "http/openapi",
  "data": {
    "hint": "Missing required parameter 'username'. Expected in path parameters.",
    "issue": "parameter_validation_failed"
  },
  "meta": {
    "ts": "2026-05-12T12:34:56Z",
    "duration_ms": 42,
    "source": "run"
  },
  "error": {
    "code": "EARG",
    "message": "Invalid arguments: missing required path parameter 'username'",
    "details": {
      "missing_params": ["username"],
      "expected_in": "path"
    }
  }
}
```

### Retry transparency

All client-visible retry decisions include:

- Number of attempts in `error.details.attempts`
- Backoff strategy in `error.details.backoff`
- `Retry-After` header value in `error.details.retry_after`

## Command namespace

### Reserved commands

| Namespace | Commands | Profile |
|---|---|---|
| **Skill commands** | `http/openapi`, `fs/ls`, `fs/read`, `text/grep`, `todo/manage` | Core v1 |
| **Plugin protocol** | `plugin/auth`, `plugin/pagination` | Plugin v1 |
| **Jobs** | `jobs/submit`, `jobs/tail`, `jobs/info`, `jobs/cancel` | Core v1 |
| **Agent** | `agent/spawn`, `agent/restart`, `agent/kill`, `agent/send`, `agent/watch` | Agent v1 |
| **Blackboard** | `bb/post`, `bb/watch`, `bb/search`, `bb/claim`, `bb/release` | Agent v1 |

### Vocabulary rules

- Commands follow pattern: `namespace/verb[-noun]`
- Namespace and verb are lowercase alphanumeric with hyphens
- New verbs MAY be added without altering envelope shape
- Custom skills MAY define their own commands following the pattern

## Versioning and compatibility

- **Current version**: `version: 1` (Core v1)
- **Breaking changes** require a major version bump
- **Backward-compatible changes** MAY add optional fields
- **Profiles** are additive capability sets declared in `meta.profiles`

Available profiles:

- `core/v1` — Base protocol
- `agent/v1` — Multi-agent extensions (mailbox, blackboard, quotas)

## Security invariants

| Rule | Enforcement |
|---|---|
| Secrets redaction | MUST redact secrets in `data`, `meta`, logs, dry-run outputs, and error messages as `"***"` |
| Workspace confinement | PathValidator gates every file access; canonical absolute paths only |
| Network policies | Skill manifests declare `capabilities.network`; default is `network: "none"` |
| Output limits | Inline output threshold (default 32KB), max capture size (default 1024KB) |
| WASI isolation | WASI skills MUST NOT have network access in Core v1 |

## Review rules

- Do not change `meta.*` fields without a spec update.
- Do not write logs to stdout from skills — stdout is envelopes only, stderr is logs only.
- Preserve deterministic ordering in golden outputs.
- Prefer the stricter Protocol v1 inline-size guidance when docs disagree.
- Use `data.hint` to help callers fix problems on error envelopes.

## Validation

```bash
# Validate envelope against schema and invariants
foxctl proto validate --input envelope.json

# Validate that command outputs conform
foxctl run fs/ls --path . | foxctl proto validate

# Strict mode
foxctl proto validate --input envelope.json --strict
```

Golden fixtures live in `tests/golden/`:
- `ok-inline.json` — Successful inline response
- `ok-cas.json` — Successful CAS-artifactized response
- `error-*.json` — Various error scenarios
- `progress-stream.ndjson` — Streaming progress example

