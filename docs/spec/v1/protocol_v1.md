# Protocol v1 — Canonical Wire Contract

**Version:** 1.0.0 **Status:** Final Draft **Last Updated:** 2025-11-15

> **Purpose:** This document defines the authoritative wire contract (envelope
> shape, commands, errors, artifactization) for foxctl Protocol v1. This is
> the stable foundation that skills, plugins, jobs, agents, and all I/O must
> conform to.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Canonical Envelope (Wire Shape)](#2-canonical-envelope-wire-shape)
3. [Command Namespace](#3-command-namespace)
4. [Artifactization (CAS) Rules](#4-artifactization-cas-rules)
5. [Error Catalog](#5-error-catalog)
6. [Progress Events (Streaming)](#6-progress-events-streaming)
7. [Versioning & Compatibility](#7-versioning--compatibility)
8. [Security Invariants](#8-security-invariants)
9. [Go Reference Types](#9-go-reference-types)

---

## 1. Overview

### 1.1 What is Protocol v1?

Protocol v1 is the **canonical wire contract** for foxctl. It defines:

- **Envelope shape**: One JSON structure for everything (skills, plugins, jobs,
  agents)
- **Command vocabulary**: Reserved top-level commands and their semantics
- **Artifactization**: Rules for when/how to store large outputs in CAS
- **Error codes**: Standardized error taxonomy
- **Streaming**: NDJSON format for progress events
- **Extensibility**: Safe extension points via profiles and plugins

### 1.2 Design Principles

1. **Stable core**: Never break the envelope contract
2. **Token efficiency**: Large outputs → CAS, only summaries inline
3. **Predictability**: Same JSON shape for all operations
4. **Extensibility**: Additive profiles, plugin system
5. **Security-first**: Never emit secrets, enforce workspace confinement

### 1.3 Who Should Read This

- **Skill developers**: To understand input/output contracts
- **Plugin authors**: To implement auth/pagination extensions
- **Agent builders**: To integrate with foxctl as an agent substrate
- **LLM prompt engineers**: To design efficient tool schemas

---

## 2. Canonical Envelope (Wire Shape)

### 2.1 Complete Envelope Structure

Every operation (skill execution, plugin call, job result, agent message) uses
this JSON envelope:

```jsonc
{
  "version": 1, // Protocol version (Core v1)
  "status": "ok|error|progress", // Tri-state status
  "command": "namespace/verb", // e.g., "http/openapi", "plugin/auth"
  "data": { // Command-specific payload
    /* Small responses inline;
       Large responses described + artifactized (see §4) */
  },
  "meta": {
    "ts": "2025-11-15T12:34:56Z", // ISO-8601 UTC timestamp
    "duration_ms": 153, // Total wall time (milliseconds)
    "runner": "wasi|exec|oci|null", // Which runner was used
    "workspace": "/path/to/workspace", // Sanitized absolute path
    "job_id": "01H...", // ULID for async jobs
    "trace_id": "uuid|ulid|''", // Optional distributed tracing hook
    "profiles": ["core/v1", "agent/v1"], // Additive profiles in use
    "source": "run|cache|memory", // Execution source
    "cas_digest": "sha256:...", // Optional; if set MUST match data.artifact
    "skill_version": "1.0.0", // Skill version for reproducibility
    "cache_key": "sha256:..." // Cache key on cache hits
  },
  "error": {
    "code": "EARG|EAUTH|ERUNTIME|...", // Standardized error code (see §5)
    "message": "human summary", // Human-readable error message
    "details": {} // Free-form machine hints
  }
}
```

### 2.2 Normative Invariants

**Status Values** (MUST be exactly one of):

- `ok` — Successful operation
- `error` — Failed operation
- `progress` — Streaming progress update (see §6)

**Required Fields**:

- `version` MUST be `1` for Protocol v1
- `status` MUST be present and valid
- `command` MUST be present and match `^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*$`
- `meta.ts` MUST be RFC3339 UTC timestamp
- `meta.duration_ms` MUST be integer ≥ 0

**Error Handling**:

- Every `error` status MUST set `error.code` + `error.message`
- For `status: "ok"`, `error.code` and `error.message` SHOULD be `null`
- If a result exceeds inline size threshold, MUST artifactize (see §4)

**Security**:

- NEVER emit secrets in `data`, `meta`, logs, or dry-run outputs
- Always redact sensitive values as `"***"`

**Streaming**:

- Use **NDJSON**: zero or more `progress` events, followed by single terminal
  `ok|error` envelope
- See §6 for complete streaming semantics

### 2.3 Field Descriptions

#### `data` (object)

Command-specific payload. Structure varies by command but follows these rules:

- JSON-serializable objects only
- Large outputs (>32KB default) MUST be artifactized with summary
- MAY be null for commands with no output

#### `meta` (object)

Metadata about the execution:

- `ts`: Wall clock timestamp (RFC3339 UTC)
- `duration_ms`: Monotonic-derived execution time
- `runner`: Execution environment (`wasi`, `exec`, `oci`, or `null` for
  built-ins)
- `workspace`: Absolute path to workspace (sanitized, may be omitted)
- `job_id`: Job identifier for async operations (ULID format)
- `trace_id`: Optional distributed tracing correlation ID
- `profiles`: Array of active profiles (e.g., `["core/v1"]`,
  `["core/v1", "agent/v1"]`)
- `source`: How result was obtained (`run`, `cache`, `memory`)
- `cas_digest`: Optional CAS digest; if set MUST match `data.artifact`
- `skill_version`: Version of skill that produced result
- `cache_key`: Cache key for memoization

#### `error` (object)

Error information (present even for `ok` status, but with null values):

- `code`: Standardized error code from catalog (§5)
- `message`: Human-readable description
- `details`: Optional machine-readable error context

---

## 3. Command Namespace

### 3.1 Reserved Commands

Protocol v1 reserves these top-level command namespaces:

#### **Skill Commands**

| Command        | Purpose                        | Status  |
| -------------- | ------------------------------ | ------- |
| `http/openapi` | Execute OpenAPI 3.x operations | Core v1 |
| `fs/ls`        | List directory contents        | Core v1 |
| `fs/read`      | Read file with preview         | Core v1 |
| `text/grep`    | Search for patterns            | Core v1 |
| `todo/manage`  | Task management                | Core v1 |

#### **Plugin Protocol**

| Command             | Purpose                  | Status    |
| ------------------- | ------------------------ | --------- |
| `plugin/auth`       | Auth header/body signing | Plugin v1 |
| `plugin/pagination` | Vendor-specific paging   | Plugin v1 |

#### **Jobs**

| Command       | Purpose             | Status  |
| ------------- | ------------------- | ------- |
| `jobs/submit` | Submit async job    | Core v1 |
| `jobs/tail`   | Stream job progress | Core v1 |
| `jobs/info`   | Get job metadata    | Core v1 |
| `jobs/cancel` | Cancel running job  | Core v1 |

#### **Agent Profile** (Optional in v1.0)

| Command         | Purpose                | Status   |
| --------------- | ---------------------- | -------- |
| `agent/spawn`   | Spawn new agent        | Agent v1 |
| `agent/restart` | Restart agent          | Agent v1 |
| `agent/kill`    | Terminate agent        | Agent v1 |
| `agent/send`    | Send message to agent  | Agent v1 |
| `agent/watch`   | Watch agent events     | Agent v1 |
| `bb/post`       | Post to blackboard     | Agent v1 |
| `bb/watch`      | Watch blackboard topic | Agent v1 |
| `bb/search`     | Search blackboard      | Agent v1 |
| `bb/claim`      | Claim blackboard item  | Agent v1 |
| `bb/release`    | Release claimed item   | Agent v1 |

### 3.2 Command Vocabulary Rules

- Commands MUST follow pattern: `namespace/verb[-noun]`
- Namespace and verb MUST be lowercase alphanumeric with hyphens
- New verbs MAY be added without altering envelope shape
- Custom skills MAY define their own commands following the pattern

---

## 4. Artifactization (CAS) Rules

### 4.1 When to Artifactize

**Rule**: If serialized `data.body` (or equivalent payload) would exceed
`inline_output_kb` (default 32KB), replace inline content with a CAS artifact
and emit a summary.

### 4.2 Artifactized Envelope Shape

When artifactizing, the envelope MUST use this structure:

```jsonc
{
  "status": "ok",
  "command": "http/openapi", // or other command
  "data": {
    "summary": {
      "status_code": 200, // For HTTP-originated responses
      "headers": { // Selected relevant headers
        "etag": "W/\"abc...\"",
        "ratelimit-remaining": "4374"
      },
      "kind": "application/json", // MIME type
      "size_bytes": 1048576, // Total size
      "record_count": 247, // Best-effort for arrays
      "preview": { // Bounded preview
        "first_keys": ["id", "name"], // For objects/arrays of objects
        "sample_record": { // First record sample
          "id": 123,
          "name": "example"
        }
      },
      "pagination": { // If pagination was used
        "has_more": false,
        "total_pages": 3,
        "total_items": 247,
        "strategy_used": "link"
      }
    },
    "artifact": "sha256:abc123..." // CAS digest (64 hex chars)
  },
  "meta": {
    /* ... other meta fields ... */
  }
}
```

### 4.3 Summary Requirements

Summaries MUST include:

- **size_bytes**: Total size of artifactized content
- **kind**: MIME type or content description
- **preview**: Bounded, deterministic sample (first N keys, first record, etc.)

For HTTP responses, summary SHOULD include:

- **status_code**: HTTP status
- **headers**: Curated headers (content-type, etag, rate-limit, caching,
  pagination, tracing)

For arrays, summary SHOULD include:

- **record_count**: Number of items (best-effort)
- **preview.first_keys**: Keys from first object in array
- **preview.sample_record**: First item or representative sample

### 4.4 Preview Bounds

- Keep preview small (< 1KB recommended)
- Use deterministic sampling (first N items, not random)
- Never include sensitive data in previews

---

## 5. Error Catalog

### 5.1 Standardized Error Codes

All client-visible errors MUST use codes from this catalog:

| Code                | Meaning                                           | Caller Remediation                             |
| ------------------- | ------------------------------------------------- | ---------------------------------------------- |
| `EOPENAPI`          | Spec invalid / operationId missing                | Validate spec; pick valid operationId          |
| `EARG`              | Invalid/missing params; request assembly failures | Fix inputs; see `error.details` or `data.hint` |
| `EAUTH`             | Authentication failed / credentials missing       | Provide/refresh credentials                    |
| `ERATELIMIT`        | 429s after retry budget exhausted                 | Wait until reset; lower request rate           |
| `EPAGINATION`       | Couldn't auto-detect; plugin/fields required      | Provide paging config or plugin                |
| `ERUNTIME`          | Transport/server/IO errors                        | Retry later; check network/server              |
| `ENOTFOUND`         | Resource/memory/spec not found                    | Fix resource references                        |
| `ETIMEOUT`          | Operation timed out                               | Increase timeout or reduce payload             |
| `EPOLICY`           | Workspace/path/network policy violation           | Narrow request; fix policy config              |
| `ESKILLDOWN`        | Circuit breaker / disabled skill                  | Backoff; re-enable when healthy                |
| `EPARSE`            | JSON parse error / invalid UTF-8                  | Fix malformed input                            |
| `EOUTPUT_TOO_LARGE` | Output exceeded capture limit                     | Reduce output or increase limit                |
| `EENVELOPE`         | Invalid/malformed envelope                        | Fix envelope structure                         |
| `EIO`               | Filesystem or I/O error                           | Check permissions/disk space                   |
| `ECANCELED`         | Job canceled by user                              | User-initiated cancellation                    |

### 5.2 Error Envelope Requirements

Every error envelope MUST:

1. Set `status: "error"`
2. Set `error.code` to a value from the catalog
3. Set `error.message` to human-readable description
4. Include actionable `error.details` or `data.hint`

Example error envelope:

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
    "ts": "2025-11-15T12:34:56Z",
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

### 5.3 Retry Transparency

All client-visible retry decisions MUST be transparent:

- Include number of attempts in `error.details.attempts`
- Include backoff strategy used in `error.details.backoff`
- Honor and report `Retry-After` headers in `error.details.retry_after`

---

## 6. Progress Events (Streaming)

### 6.1 Purpose

Progress events provide bounded, machine-friendly updates during long-running
operations.

### 6.2 NDJSON Format

Streaming MUST use **NDJSON** (newline-delimited JSON):

- Each line is a complete JSON envelope
- Lines separated by `\n` (normalize `\r\n` to `\n`)
- Zero or more `progress` status envelopes
- Exactly one terminal `ok` or `error` envelope

### 6.3 Progress Envelope Structure

```jsonc
{
  "version": 1,
  "status": "progress",
  "command": "http/openapi",
  "data": {
    "stage": "paginate", // Free-form: "resolve-spec"|"auth"|"request"|"paginate"|"store"
    "pct": 35, // 0-100 best-effort percentage
    "message": "page 2 of 5", // Human-readable status
    "metrics": { // Optional metrics
      "pages": 2,
      "items": 200,
      "bytes": 123456
    }
  },
  "meta": {
    "ts": "2025-11-15T12:34:56Z",
    "job_id": "01H...",
    "seq": 1 // Sequence number (starts at 0)
  },
  "error": {
    "code": null,
    "message": null
  }
}
```

### 6.4 Progress Rules

**MUST**:

- Use `status: "progress"`
- Include `meta.seq` (integer, starting at 0, monotonically increasing)
- Keep progress events small (no large blobs)
- Never include secrets

**SHOULD**:

- Include `data.pct` (0-100) when progress is estimable
- Include `data.stage` for multi-phase operations
- Include `data.message` for human consumption
- Include `data.metrics` for detailed telemetry

**MUST NOT**:

- Decrease `data.pct` once set
- Emit progress after terminal envelope

---

## 7. Versioning & Compatibility

### 7.1 Protocol Version

- **Current**: `version: 1` (Core v1)
- **Breaking changes**: Require major version bump
- **Backward-compatible changes**: MAY add optional fields

### 7.2 Profiles

Profiles are **additive** capability sets:

- `core/v1`: Base protocol (this spec)
- `agent/v1`: Multi-agent extensions (mailbox, blackboard, quotas)
- Future profiles: `plugin/v1`, `oci/v1`, etc.

Profiles declared in `meta.profiles` array.

### 7.3 Command Versioning

- Commands MAY add optional fields anytime
- Removing/renaming fields is a breaking change
- New commands can be added without protocol version bump

### 7.4 Plugin Versioning

Plugins advertise supported protocol via handshake:

```json
{
  "name": "aws-sigv4",
  "commands": ["plugin/auth"],
  "protocols": ["core/v1"],
  "limits": {
    "max_in_kb": 128,
    "max_out_kb": 32
  }
}
```

---

## 8. Security Invariants

### 8.1 Secrets Redaction

**MUST** redact secrets in:

- Envelope `data` fields
- Envelope `meta` fields
- Log outputs
- Dry-run outputs
- Error messages

Redact as `"***"` or remove entirely.

### 8.2 Workspace Confinement

**PathValidator** gates every file access:

- Default: no symlinks
- Canonical, absolute paths only
- Allow-list aware
- Safe prefix checks

See SPEC-011 for complete hardening requirements.

### 8.3 Network Policies

- Skill manifests declare `capabilities.network`
- Runner enforces allow-list
- Default: `network: "none"`
- WASI skills MUST NOT have network access in Core v1

### 8.4 Output Limits

**MUST** enforce:

- Inline output threshold (default 32KB)
- Maximum capture size (default 1024KB)
- Maximum stream line size (default 1024KB)
- Plugin IO size caps

---

## 9. Go Reference Types

### 9.1 Envelope Types

```go
// internal/protocol/envelope.go
package protocol

import "time"

// Envelope is the canonical Protocol v1 wire format
type Envelope struct {
    Version int         `json:"version"`           // MUST be 1
    Status  string      `json:"status"`            // "ok"|"error"|"progress"
    Command string      `json:"command"`           // "namespace/verb"
    Data    interface{} `json:"data,omitempty"`    // Command-specific payload
    Meta    Meta        `json:"meta"`
    Error   *Err        `json:"error"`
}

// Meta contains execution metadata
type Meta struct {
    TS           time.Time `json:"ts"`                        // RFC3339 UTC
    DurationMS   int64     `json:"duration_ms"`               // >= 0
    Runner       string    `json:"runner,omitempty"`          // wasi|exec|oci|null
    Workspace    string    `json:"workspace,omitempty"`       // Absolute path
    JobID        string    `json:"job_id,omitempty"`          // ULID
    TraceID      string    `json:"trace_id,omitempty"`        // UUID/ULID
    Profiles     []string  `json:"profiles,omitempty"`        // ["core/v1", ...]
    Source       string    `json:"source,omitempty"`          // run|cache|memory
    CASDigest    string    `json:"cas_digest,omitempty"`      // sha256:...
    SkillVersion string    `json:"skill_version,omitempty"`   // Semver
    CacheKey     string    `json:"cache_key,omitempty"`       // sha256:...
    Seq          *int      `json:"seq,omitempty"`             // Streaming only
    Final        *bool     `json:"final,omitempty"`           // Streaming only
}

// Err contains error information
type Err struct {
    Code    string                 `json:"code"`              // Error code from catalog
    Message string                 `json:"message"`           // Human-readable
    Details map[string]interface{} `json:"details,omitempty"` // Machine hints
}

// Summary for large responses (artifactized)
type Summary struct {
    StatusCode  int               `json:"status_code,omitempty"`
    Headers     map[string]string `json:"headers,omitempty"`
    Kind        string            `json:"kind,omitempty"`
    SizeBytes   int64             `json:"size_bytes,omitempty"`
    RecordCount int               `json:"record_count,omitempty"`
    Preview     interface{}       `json:"preview,omitempty"`
    Pagination  *PaginationMeta   `json:"pagination,omitempty"`
}

// PaginationMeta describes pagination state
type PaginationMeta struct {
    HasMore     bool    `json:"has_more"`
    TotalPages  int     `json:"total_pages,omitempty"`
    TotalItems  int     `json:"total_items,omitempty"`
    Strategy    string  `json:"strategy_used,omitempty"`
    CursorFinal *string `json:"cursor_final,omitempty"`
}
```

### 9.2 Validation Functions

```go
// ValidateEnvelope checks protocol invariants
func ValidateEnvelope(env *Envelope) error {
    if env.Version != 1 {
        return fmt.Errorf("invalid version: expected 1, got %d", env.Version)
    }

    switch env.Status {
    case "ok", "error", "progress":
        // Valid
    default:
        return fmt.Errorf("invalid status: %q", env.Status)
    }

    if env.Command == "" {
        return fmt.Errorf("command is required")
    }

    if !commandPattern.MatchString(env.Command) {
        return fmt.Errorf("invalid command format: %q", env.Command)
    }

    if env.Status == "error" {
        if env.Error == nil || env.Error.Code == "" || env.Error.Message == "" {
            return fmt.Errorf("error status requires error.code and error.message")
        }
    }

    if env.Meta.DurationMS < 0 {
        return fmt.Errorf("duration_ms must be >= 0")
    }

    return nil
}
```

---

## Appendix A: Conformance Testing

### A.1 Conformance CLI

```bash
# Validate envelope against schema + invariants
foxctl proto validate --input envelope.json

# Validate that command outputs conform
foxctl run fs/ls --path . | foxctl proto validate

# Check for common issues
foxctl proto validate --input envelope.json --strict
```

### A.2 Golden Fixtures

See `tests/golden/` for canonical envelope examples:

- `ok-inline.json` - Successful inline response
- `ok-cas.json` - Successful CAS-artifactized response
- `error-*.json` - Various error scenarios
- `progress-stream.ndjson` - Streaming progress example

---

## Appendix B: Migration from Earlier Versions

If you have code using pre-v1 envelopes:

1. Add `"version": 1` to all envelopes
2. Rename `"toon"` references to `"envelope"` or `"json"`
3. Change tri-state status from custom values to `ok|error|progress`
4. Update error handling to use standardized codes
5. Add `meta.profiles: ["core/v1"]`
6. Implement artifactization for large outputs

---

## Appendix C: References

- **Core Profile v1**: Complete foxctl specification
- **OpenAPI Skill**: Detailed OpenAPI skill contract
- **Plugin Protocol**: Plugin development guide
- **Agent Profile v1**: Multi-agent extensions
- **SPEC-011**: PathValidator security hardening
- **SPEC-018**: Golden test fixtures

---

**Document Status**: Final Draft **Acceptance Criteria**:

- [ ] Wire frozen: envelope + invariants documented
- [ ] Schema validation implemented (`foxctl proto validate`)
- [ ] Golden fixtures created and passing
- [ ] All skills emit conformant envelopes
- [ ] Redaction helpers tested
