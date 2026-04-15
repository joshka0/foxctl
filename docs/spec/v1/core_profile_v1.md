Below is a cohesive **Core Profile v1 (JSON‑Envelope Edition)** rewritten to
incorporate the **generic OpenAPI skill**, **extensibility for auth/pagination
via plugins**, **spec‑as‑memory**, and **Golang‑first implementation guidance**.
I’ve tightened language, resolved previous naming collisions, and added Go
interfaces/stubs where code helps pin down behavior.

---

# foxctl Core Profile v1 — JSON Envelope Edition (with OpenAPI Integration, Go-first)

> **Purpose:** A practical, single‑agent runtime for structured LLM workflows
> with memory and artifact management, plus a first‑class path to invoke
> real-world REST APIs described by OpenAPI. **Audience:** Developers building
> LLM‑powered tools who need deterministic I/O, token efficiency, and persistent
> context. **Scope:** Minimal, production‑ready `foxctl` for daily use. The
> **Agent Profile** (separate spec) adds multi‑agent orchestration,
> mailbox/blackboard, quotas, and OCI skills.

---

## 0. Preamble

### 0.1 Naming note (TOON vs JSON)

Earlier drafts called the envelope **“TOON”**. To avoid collision with the
external **TOON serialization format** (a compact alternative to JSON), this
spec uses **“JSON envelope”** for the canonical wire format. Future adapters MAY
support encoding/decoding to real TOON for prompt efficiency, but JSON remains
authoritative in Core v1.

### 0.2 What is foxctl Core?

`foxctl` is a **single‑binary CLI** that provides:

- **Structured I/O:** predictable **JSON envelopes** for every operation.
- **Skills:** discoverable, typed tools with WASI/exec isolation.
- **Jobs:** async execution with progress and durable state.
- **Artifacts (CAS):** content‑addressable blobs for large outputs.
- **Memory:** auto‑cache plus named, persistent memories.

**Think:** `bash` for LLMs, but with structured output and memory.

### 0.3 Why Core (vs Agent)?

Core targets **single‑agent** workflows: run skills with deterministic I/O,
store large outputs as artifacts, and reuse context across conversations. You
can **upgrade** to the Agent Profile later without breaking Core contracts.

### 0.4 Design principles

- **Token efficiency:** Large outputs go to CAS; envelopes return **summaries +
  digests**.
- **Memory‑first:** Recent work auto‑cached; durable work explicitly named.
- **Deterministic when possible:** Same inputs → same outputs.
- **Zero‑config:** Works out‑of‑the‑box; advanced features opt‑in.
- **Composable:** Unix‑style piping (stdin) and digest chaining.
- **Extensible:** Pluggable auth & pagination for the OpenAPI skill.

### 0.5 Normative keywords

**MUST**, **MUST NOT**, **SHOULD**, **MAY** per RFC 2119/8174.

---

## 1. Core Concepts

### 1.1 Skill

Executable, typed capability with:

- Name `category/verb[-noun]` (e.g., `test/pytest`, `repo/index`).
- Input/Output schema.
- Isolation: **WASI** (preferred) or **exec**.
- Deterministic **JSON envelope** I/O.

**Name regex:** `^[a-z0-9][a-z0-9-]*/[a-z0-9][a-z0-9-]*(?:-[a-z0-9-]+)?$`
(lowercase; hyphens allowed).

### 1.2 Job

One skill execution: `queued → running → ok|error|canceled` (terminal).
**Sync:** `foxctl run <skill>` blocks. **Async:**
`foxctl jobs submit <skill>` returns `job_id`.

### 1.3 Artifact (CAS)

Large outputs stored by digest `sha256:<hex>`.

- **Token‑efficient** (return digest not megabytes).
- **Deduplicated & persistent**.
- Fetch via `cas get`.

### 1.4 Memory

Two tiers:

1. **Auto‑cache (24h)** — by `(skill, version, args, inputs)`.
2. **Named memory (persistent)** — explicitly saved artifacts with metadata.

### 1.5 JSON Envelope

Stable JSON wrapper for all I/O (see §2).

---

## 2. I/O — JSON Envelope (canonical), Streaming, Limits

### 2.1 Envelope

```json
{
  "version": 1,
  "status": "ok", // "ok" | "error"
  "command": "repo/index",
  "data": {/* JSON-compatible */},
  "meta": {
    "ts": "2025-11-07T12:34:56Z", // RFC3339 UTC (wall clock)
    "duration_ms": 153, // integer >= 0 (monotonic-derived)
    "source": "run", // "run" | "cache" | "memory"
    "runner": "wasi", // "wasi" | "exec"
    "seq": 0, // streaming only
    "final": false, // streaming only
    "cas_digest": null, // optional; if set MUST match data.artifact
    "memory": null, // {name,type,workspace} when source=="memory"
    "job_id": null, // RECOMMENDED (jobs)
    "skill_version": null, // RECOMMENDED
    "workspace": null, // RECOMMENDED
    "cache_key": null // RECOMMENDED on cache hits
  },
  "error": { "code": null, "message": null }
}
```

**Rules (normative):**

- Envelopes **MUST** be valid UTF‑8 and well‑formed JSON; `version` **MUST** be
  `1`.
- `status` **MUST** be `"ok"` or `"error"`.
- `meta.ts` **MUST** be RFC3339 UTC; `meta.duration_ms` **MUST** be integer ≥ 0.
- `error.code` + `error.message` **MUST** exist; for `status:"ok"` they
  **SHOULD** be `null`.
- If `meta.cas_digest` is set, it **MUST** equal `data.artifact`, and **MUST**
  be omitted when `data.artifact` is absent.
- No binary blobs inline; large outputs **MUST** use CAS (see §4).
- Error envelopes **MAY** include partials and **SHOULD** set
  `meta.partial:true`.

### 2.2 Streaming (NDJSON)

- Streams **MUST** be **NDJSON** (`\n` delimited; normalize `\r\n`).
- Each streamed envelope **MUST** use `command:"progress"` and include
  `meta.seq` (int, starting 0).
- If present, `data.percent` **MUST** be 0–100 and **MUST NOT** decrease.
- Exactly one progress envelope **MUST** set `meta.final:true` (or an error
  envelope terminates the stream).
- `jobs tail` streams progress only; retrieve the final result via
  `jobs result`.

### 2.3 Limits & truncation

Defaults: `inline_output_kb=32`, `max_capture_kb=1024`,
`max_stream_line_kb=1024`. Runners **MUST** stream‑parse envelopes:

- If serialized `data` exceeds `inline_output_kb`, **artifactize** per §4
  (wrapper + digest) without exceeding `max_capture_kb`.
- If raw stdout exceeds `max_capture_kb` before a valid envelope is parsed, emit
  `status:"error"`, `error.code:"EOUTPUT_TOO_LARGE"`.
- Non‑UTF‑8 or malformed JSON → `status:"error"`, `error.code:"EPARSE"`.

### 2.4 Optional encodings (adapters)

JSON is canonical. Adapters (e.g., real TOON) MAY be provided for prompt
efficiency **without changing** the JSON envelope contract.

---

## 3. OpenAPI Integration (Tier‑1 Generic Skill)

### 3.1 Overview

A single **generic** skill, `http/openapi`, enables calling _any_ OpenAPI 3.x
operation using a spec loaded from file, CAS, or memory. This covers ~90% of
real REST use cases with **zero codegen** and bakes in pagination, retries, auth
mapping, and CAS handling.

### 3.2 Skill signature

```yaml
apiVersion: foxctl/v1
kind: Skill
metadata:
  name: http/openapi
  version: 1.0.0
  description: "Invoke OpenAPI operations"
distribution:
  type: exec
io:
  format: JSON
signature:
  command: http/openapi
  parameters:
    - {
        name: spec,
        type: string,
        required: true,
        description: "path|sha256:<hex>|memory:<name>",
      }
    - { name: operationId, type: string, required: true }
    - {
        name: params,
        type: object,
        required: false,
        description: "path/query/header/body map",
      }
    - {
        name: paging,
        type: object,
        required: false,
        description: "strategy & limits",
      }
    - {
        name: retry,
        type: object,
        required: false,
        description: "backoff for 429/5xx",
      }
    - {
        name: auth,
        type: object,
        required: false,
        description: "override or choose auth scheme",
      }
    - {
        name: dry_run,
        type: boolean,
        required: false,
        description: "print request plan only",
      }
  returns:
    - {
        name: result_digest,
        type: string,
        description: "artifact digest when large",
      }
capabilities:
  network: "egress"
```

### 3.3 Spec‑as‑memory

- `foxctl openapi import <file|url> --as=<name>` **MUST** store the original
  spec as a CAS artifact and create a **named memory** entry
  (`type: openapi_spec`) pointing to it.
- Invocations may reference `--spec=memory:<name>`.

### 3.4 Auth mapping (built‑in)

Built‑ins cover common schemes; each may read secrets from `/run/secrets/…` or
`FOXCTL_*` env overrides.

| Scheme                    | Mapping                                                            |
| ------------------------- | ------------------------------------------------------------------ |
| HTTP Bearer               | `Authorization: Bearer $OAS_BEARER_TOKEN`                          |
| API Key (header/query)    | Place `$OAS_API_KEY` in header or query per `securitySchemes`      |
| HTTP Basic                | `Authorization: Basic base64(user:pass)` via `$OAS_BASIC_AUTH`     |
| OAuth2 Client Credentials | Exchange via helper `auth/token` (client_id/secret) → bearer token |

**Extensibility:** Additional schemes (e.g., HMAC, AWS SigV4, mTLS) **MUST** be
provided via plugins (§3.7).

**Secret selection:**

- If multiple APIs are used concurrently, the caller **MAY** pass
  `--auth.secret_name=<name>` to pick a secret bundle; otherwise the skill uses
  the default secret names above.
- Secrets **MUST NOT** appear in logs/envelopes; redact as `"***"`.

### 3.5 Pagination strategies (built‑in)

The skill auto‑detects common patterns and **MAY** be overridden:

- **Link headers** (`rel="next"`) — GitHub style.
- **Cursor** fields (e.g., `next`, `next_page_token`) — Stripe/Google style.
- **Offset/limit** — classic pagination.
- **Total‑count heuristics** — stop when len(items) < requested page size.

Caller overrides:

```
--paging.strategy=link|cursor|offset
--paging.cursor_field=next_page_token
--paging.page_param=page --paging.per_page_param=per_page
--paging.max_pages=10 --paging.max_items=5000
```

### 3.6 Retry & rate limiting

- Default exponential backoff (jitter) on `429, 502, 503, 504` with
  `Retry-After` respected.
- Configurable via
  `--retry.base_ms --retry.factor --retry.max_attempts --retry.max_ms`.

### 3.7 Plugins (auth & pagination)

To support “snowflake” APIs, `http/openapi` loads **out‑of‑process plugins**
(exec or WASI) that communicate via **JSON envelopes** on stdin/stdout. Plugins
are referenced by **name** or **path** and are discoverable via
`FOXCTL_PLUGIN_PATH`.

**Plugin protocol (envelope):**

- **Auth plugin** command: `plugin/auth`. Input `data` includes the request
  (method, url, headers, body) and spec context; output **MUST** return adjusted
  headers and (optional) signed body.
- **Pagination plugin** command: `plugin/pagination`. Input includes the last
  response and requested max items; output indicates the next request or
  termination.

**Vendor extensions (hints):** Specs MAY include `x-foxctl` hints; the skill
**MUST** prefer explicit hints over heuristics.

```yaml
x-foxctl:
  auth: bearer | apiKey | basic | oauth2 | plugin:<name>
  pagination:
    strategy: link|cursor|offset|plugin:<name>
    cursor_field: next_page_token
  retry:
    codes: [429, 503]
    max_attempts: 4
```

### 3.8 Dry run

`--dry_run=true` **MUST** emit an `ok` envelope with `data.summary.request_plan`
(method, URL with query, redacted headers) and **MUST NOT** perform the network
call.

### 3.9 Response wrapping & summaries

- For successful calls:

  - **Always include** `status_code` and response **headers** summary
    (`ratelimit`, `etag`, `link`, pagination hints).
  - If body is large, CAS wrapper per §4; summary **SHOULD** include
    `record_count|first_keys|sample_record`.
- For **4xx** with validation errors: **MUST NOT** artifactize the error details
  by default; include the structured error object inline (bounded to
  `inline_output_kb`).
- For **5xx** after retries: return error envelope with `error.code:"ERUNTIME"`
  and include service `request-id` header when available.

### 3.10 Error normalization (additions)

- `EAUTH` — missing/invalid auth.
- `EPAGINATION` — pagination detection failed or inconsistent.
- `EOPENAPI` — spec parse/validation failure.
- `ERATELIMIT` — exceeded rate limit (after retries). All errors **SHOULD**
  include actionable `data.hint`.

---

## 4. Artifacts & Mandatory Summaries (Large Outputs)

**When `data` exceeds `inline_output_kb`**, the runner **MUST**:

1. Write the **full result** to CAS.
2. Return a wrapper with **mandatory `summary`** and `artifact`.
3. Optionally set `meta.cas_digest` to that digest (if set MUST match
   `data.artifact`).
4. Optionally set `data.kind` and `data.size_bytes`.

**HTTP responses:** summary **SHOULD** add `status_code`, selected headers
(`ETag`, rate‑limit), and pagination markers.

---

## 5. CLI Surface

```
# discovery
foxctl skills list [--format=json|compact|toon]
foxctl skills describe <name> [--json]
foxctl skills search <query>
foxctl skills install|uninstall|upgrade <ref>

# openapi
foxctl openapi import <file|url> --as=<name> [--strict]   # store spec as memory+CAS
foxctl openapi validate <memory:<name>|path> [--strict]
foxctl openapi test <memory:<name>> [--op=<operationId>|--tag=<tag>]  # smoke tests
foxctl openapi generate <memory:<name>> [--install] [--group-by=tag|path]  # optional Tier-2 codegen

# execution
foxctl run <skill> [--flags...] [--cache=off] \
  [--input=stdin|sha256:<hex>] [--remember=<name>] [--ttl=<dur>] [--workspace=<path>]
# generic OpenAPI
foxctl run http/openapi --spec=memory:github --operationId=listRepos --params='{"path":{"username":"octocat"},"query":{"per_page":100}}' [--dry_run]

# async jobs
foxctl jobs submit <skill> [...] [--dedupe]
foxctl jobs ls [--state=queued|running|ok|error|canceled] [--since=24h]
foxctl jobs status|wait|tail|result <job_id>
foxctl jobs cancel <job_id>

# artifacts (CAS)
foxctl cas put|head|get|list|pin|unpin|rm ...

# memory
foxctl memory recent|cache|put|save|get|search|list|update|delete|relevant ...

# adapters (optional)
foxctl skills index --format=json|compact|toon
foxctl toon encode|decode           # optional adapter

# admin
foxctl doctor
foxctl gc [--dry-run]
foxctl config show|edit
foxctl version
```

**Envelopes everywhere:** All commands **MUST** emit JSON envelopes on stdout.
`jobs result --emit=digest` returns an `ok` envelope with
`data:{artifact:"sha256:…"}` (or `{note:"no artifact"}`).

---

## 6. Skills

### 6.1 Manifest (`skill.yaml`) — deltas for Core v1

```yaml
io:
  format: JSON                         # canonical
capabilities:
  network: "none" | "egress"
# Core rule: distribution.type: wasi -> network MUST be "none" (validation error otherwise)
```

### 6.2 Parameter typing (recommended set)

`string | integer | number | boolean | enum | array | object | file | dir`.
`file|dir` parameters use **content digests** for caching (dirs recursive;
ignore `.git`, `node_modules` by default; configurable).

---

## 7. Jobs & Execution

- States: `queued`, `running`, `ok`, `error`, `canceled` (terminal).
- `jobs submit --dedupe` returns the existing running job if
  `(skill, args_hash, inputs_hash, workspace)` matches.
- `cancel` target latency SHOULD be < 2s; terminal state `canceled` with
  `error.code:"ECANCELED"`.

SQLite tables and filesystem layout as in the previous Core spec; add indexes on
`(workspace, created_at)` and `(args_hash, inputs_hash)`.

---

## 8. Caching & Memoization

### 8.1 Cache key (canonical)

```
cache_key = sha256(
  skill_name + "\0" +
  skill_version + "\0" +
  json_c14n(args) + "\0" +
  join("\0", sort(input_blob_digests))
)
```

- RFC 8785 JSON canonicalization.
- For OpenAPI calls, **ETag/Last‑Modified** (when present) **MAY** be folded
  into the key or used for conditional requests; cache hits **MUST** include
  `meta.cache_key`.

### 8.2 Modes

Cache is currently disabled in the reference implementation. The CLI only
accepts `--cache=off` (or omitting the flag).

| Mode  | Behavior                                 |
| ----- | ---------------------------------------- |
| `off` | Skip cache entirely (no read, no write). |

### 8.3 Cache hit annotation

On a cache hit, the returned envelope **MUST** include:

- `meta.source = "cache"` — indicates result came from cache
- `meta.cache_key = "<cache_key>"` — the computed cache key

All other fields (data, error, etc.) are preserved from the original cached
result.

### 8.4 Pure skills (opt‑in)

`pure:true` indicates determinism; caches MAY be portable across workspaces.

---

## 9. Composability

### 9.1 Stdin chaining

`--input=stdin` accepts a single envelope; treat `data` as input. Consumers MUST
ignore upstream `command` unless validated.

### 9.2 Digest chaining

`--input=sha256:<hex>` → the runner fetches from CAS and supplies content (stdin
or temp file). Binary chaining MUST use CAS.

---

## 10. Security

### 10.1 Isolation

- **WASI:** sandboxed; **no network** in Core v1.
- **Exec:** process isolation; resource limits; ephemeral `/work`.

### 10.2 Egress control

```yaml
capabilities:
  network: "none" | "egress"
  egressAllow:
    - "api.github.com:443"
    - "*.amazonaws.com:443"
    - "10.0.0.0/8:*"
    - "localhost:5432"
```

- DNS resolved at execution; resolved IPs SHOULD be logged and cached for the
  job (respect TTL).
- Wildcards match domain suffixes; CIDRs and literal IPs supported; `:*` any
  port.
- If `network:"egress"` with no `egressAllow`, **all** outbound egress is
  allowed (subject to policy).
- Loopback and UNIX sockets are **denied** unless explicitly allowed.

### 10.3 Secrets

- Mounted read‑only under `/run/secrets/<name>`; not inherited unless allowed.
- Values MUST NOT appear in logs/envelopes/artifacts.

### 10.4 Verification

- MUST verify checksums at install; SHOULD verify signatures per policy.

---

## 11. Artifacts (CAS)

### 11.1 CLI

`cas put|head|get|list|pin|unpin|rm` (unchanged).

### 11.2 Metadata schema (add tags)

`tags TEXT NOT NULL DEFAULT '[]'` (JSON array). Pinned artifacts excluded from
GC. Integrity MUST be verified on `cas get`.

---

## 12. Memory

Memory provides persistent storage for skill results and contextual data, scoped
by workspace.

### 12.1 Named Memory Entry

| Field           | Type        | Description                                     |
| --------------- | ----------- | ----------------------------------------------- |
| `id`            | `string`    | UUID primary key                                |
| `name`          | `string`    | User key; unique per workspace                  |
| `type`          | `string`    | E.g. `result`, `plan`, `spec`; default `result` |
| `workspace`     | `string`    | Normalized workspace path                       |
| `summary`       | `string`    | Short human-oriented summary                    |
| `result`        | `bytes`     | Full JSON envelope                              |
| `digests`       | `[]string`  | CAS digests referenced by result                |
| `created_at`    | `timestamp` | Immutable                                       |
| `updated_at`    | `timestamp` | Updated on write                                |
| `last_accessed` | `timestamp` | Updated on read                                 |
| `access_count`  | `int`       | Incremented on read                             |

**Invariants:**

- `(name, workspace)` MUST be unique.
- Named memories have **no TTL**; persistent until deleted.
- Digests MUST be pinned on save, unpinned on delete.

### 12.2 Creation

**Via `foxctl run --remember`:**

```bash
foxctl run skill/name --input '{}' \
  --remember my-result \
  --remember-type result \
  --remember-summary "Brief description"
```

- Saves final result envelope regardless of status.
- If `--remember-summary` omitted, auto-generates from envelope.
- Memory failures are best-effort; run still succeeds.

**Via CLI:**

```bash
# From job result
foxctl memory save <job_id> --as=<name>

# From envelope
foxctl memory put --name=<name> --data='{"version":1,...}'
```

### 12.3 Retrieval

```bash
# Get specific memory (writes original envelope to stdout)
foxctl memory get <name> --workspace=/path

# List all memories in workspace
foxctl memory list --workspace=/path --limit=20

# Search by name/summary
foxctl memory search --query="term" --workspace=/path
```

**On not found:** Emit `ENOTFOUND` error envelope with hint.

### 12.4 Workspace Detection & Ranking

**Detection priority:**

1. `--workspace` flag if provided
2. Auto-detect from `.foxctl/`, `.git/`, or project files
3. Current working directory

**Relevance ranking:**

```
score = 0.6 * recency + 0.4 * log1p(access_count)
```

```bash
foxctl memory relevant --workspace=/path --limit=10
```

### 12.5 Mutation

```bash
# Update metadata
foxctl memory update <name> --summary="New summary" --type=plan

# Delete
foxctl memory delete <name> --workspace=/path
```

**On not found:** Emit `ENOTFOUND` error envelope.

---

## 13. Error Codes (expanded)

| Code                     | Meaning                               |
| ------------------------ | ------------------------------------- |
| `EARG`                   | Invalid arguments                     |
| `ENOTFOUND`              | Resource not found                    |
| `ETIMEOUT`               | Operation exceeded timeout            |
| `ERUNTIME`               | Skill process error/crash             |
| `ERUNTIME_RESTART`       | Runner recovered after restart        |
| `EENVELOPE`              | Invalid/malformed envelope            |
| `EPARSE`                 | JSON parse error / invalid UTF‑8      |
| `EOUTPUT_TOO_LARGE`      | Stdout exceeded capture limit         |
| `EPOLICY`                | Capability/policy violation           |
| `EIO`                    | Filesystem or I/O error               |
| **`EAUTH`**              | Authentication failed/missing         |
| **`EPAGINATION`**        | Pagination detection/logic failure    |
| **`EOPENAPI`**           | OpenAPI spec parse/validation failure |
| **`ERATELIMIT`**         | Rate limit exceeded after retries     |
| `ECANCELED`              | Job canceled by user                  |
| **`ECACHE_MISS`**        | Cache-only mode and no cached result  |
| **`ECACHE_UNAVAILABLE`** | Cache storage unavailable             |

---

## 14. Configuration

```yaml
version: 1
storage:
  base_path: ~/.foxctl
  artifacts_path: ${base_path}/cas
  jobs_path: ${base_path}/jobs

limits:
  inline_output_kb: 32
  max_capture_kb: 1024
  max_stream_line_kb: 1024
  max_concurrent_jobs: 10

memory:
  auto_cache_ttl: 24h
  default_named_ttl: 30d
  auto_load_workspace: true
  max_auto_load: 5
  max_auto_load_tokens: 10000

cache:
  default_mode: auto

gc:
  jobs_retention: 7d
  artifacts_retention: 30d
  auto_cache_retention: 24h

openapi:
  strict_validate: false
  plugin_path: ~/.foxctl/plugins
  spec_cache_ttl: 24h
  default_retry:
    base_ms: 250
    factor: 2.0
    max_attempts: 5
    max_ms: 8000

trust:
  verify_checksums: true
  verify_signatures: false

adapters:
  toon_enabled: false
```

**Env overrides (examples):** `FOXCTL_BASE_PATH`, `FOXCTL_INLINE_OUTPUT_KB`,
`FOXCTL_MAX_CAPTURE_KB`, `FOXCTL_CACHE_MODE`,
`FOXCTL_OPENAPI_PLUGIN_PATH`, `FOXCTL_OPENAPI_STRICT_VALIDATE`.

---

## 15. Go‑first Reference Interfaces (non‑normative but recommended)

### 15.1 Envelope types

```go
package envelope

type Error struct {
    Code    *string `json:"code"`
    Message *string `json:"message"`
}

type MemoryMeta struct {
    Name      string `json:"name"`
    Type      string `json:"type"`
    Workspace string `json:"workspace"`
}

type Meta struct {
    TS          string      `json:"ts"`
    DurationMs  int64       `json:"duration_ms"`
    Source      string      `json:"source"`             // run|cache|memory
    Runner      string      `json:"runner,omitempty"`   // wasi|exec
    Seq         *int        `json:"seq,omitempty"`
    Final       *bool       `json:"final,omitempty"`
    CasDigest   *string     `json:"cas_digest,omitempty"`
    Memory      *MemoryMeta `json:"memory,omitempty"`
    JobID       *string     `json:"job_id,omitempty"`
    SkillVer    *string     `json:"skill_version,omitempty"`
    Workspace   *string     `json:"workspace,omitempty"`
    CacheKey    *string     `json:"cache_key,omitempty"`
    Partial     *bool       `json:"partial,omitempty"`
}

type Envelope struct {
    Version int         `json:"version"` // const 1
    Status  string      `json:"status"`  // ok|error
    Command string      `json:"command"`
    Data    interface{} `json:"data,omitempty"`
    Meta    Meta        `json:"meta"`
    Error   Error       `json:"error"`
}
```

### 15.2 CAS client

```go
type CAS interface {
    Put(ctx context.Context, r io.Reader, kind string, tags []string) (digest string, size int64, err error)
    Head(ctx context.Context, digest string) (kind string, size int64, tags []string, err error)
    Get(ctx context.Context, digest string) (io.ReadCloser, error)
}
```

### 15.3 OpenAPI skill — plugin SPI (subprocess)

Plugins speak JSON envelopes over stdin/stdout.

```go
// Auth plugin input/output
type AuthRequest struct {
    Method  string            `json:"method"`
    URL     string            `json:"url"`
    Headers map[string]string `json:"headers"`
    Body    []byte            `json:"body,omitempty"`
    Context map[string]any    `json:"context"` // spec fragments, security scheme, etc.
}
type AuthResponse struct {
    Headers map[string]string `json:"headers"`
    Body    []byte            `json:"body,omitempty"`
}

// Pagination plugin
type PageInput struct {
    LastResponse struct {
        Status  int               `json:"status"`
        Headers map[string]string `json:"headers"`
        Body    json.RawMessage   `json:"body"`
    } `json:"last_response"`
    RequestedMaxItems int `json:"requested_max_items"`
}
type PageOutput struct {
    Continue   bool              `json:"continue"`
    NextURL    string            `json:"next_url,omitempty"`
    NextQuery  map[string]string `json:"next_query,omitempty"`
    NextCursor string            `json:"next_cursor,omitempty"`
}
```

### 15.4 OpenAPI request execution skeleton (Go)

```go
func (s *Skill) Execute(ctx context.Context, in Input) (*envelope.Envelope, error) {
    start := time.Now()
    // 1) Load spec (path|CAS|memory)
    spec, err := s.loadSpec(ctx, in.Spec)
    if err != nil { return s.err("EOPENAPI", err, start), nil }

    // 2) Resolve operationId, validate params (lenient unless --strict)
    op, err := s.resolveOperation(spec, in.OperationID)
    if err != nil { return s.err("EOPENAPI", err, start), nil }
    if verr := s.validateParams(op, in.Params, s.Strict); verr != nil {
        return s.err("EARG", verr, start).WithHint("Check parameter names/types per spec"), nil
    }

    // 3) Build request (path/query/header/body)
    req, err := s.buildRequest(op, in.Params)
    if err != nil { return s.err("EARG", err, start), nil }

    // 4) Apply auth (built-in or plugin)
    if err := s.applyAuth(ctx, req, op, in.Auth); err != nil {
        return s.err("EAUTH", err, start), nil
    }

    // 5) Dry-run?
    if in.DryRun {
        return s.ok(start, "http/openapi", map[string]any{
            "summary": map[string]any{
                "request_plan": s.redact(req),
            },
        }), nil
    }

    // 6) Execute with retry/backoff and pagination
    agg, hdrs, status, err := s.fetchPaged(ctx, req, in.Paging, in.Retry)
    if err != nil {
        code := classify(err) // ERATELIMIT|ETIMEOUT|ERUNTIME|EPAGINATION...
        e := s.err(code, err, start)
        e.Data = map[string]any{"hint": hintFor(code, err)}
        return e, nil
    }

    // 7) Wrap result (CAS if large) + summary (status, headers, count, sample)
    bytes := len(agg)
    if bytes > s.InlineKB*1024 {
        digest, size, _ := s.cas.Put(ctx, bytesReader(agg), "application/json", nil)
        return s.okWithArtifact(start, "http/openapi", map[string]any{
            "summary": map[string]any{
                "status_code": status,
                "headers": summarizeHeaders(hdrs),
                "kind": "application/json",
                "size_bytes": size,
                "record_count": countIfArray(agg),
                "preview": preview(agg),
            },
            "artifact": digest,
            "kind": "application/json",
            "size_bytes": size,
        }, &digest), nil
    }
    return s.ok(start, "http/openapi", map[string]any{
        "status_code": status,
        "headers": summarizeHeaders(hdrs),
        "body": json.RawMessage(agg),
    }), nil
}
```

_(All code above is illustrative and keeps to standard Go `net/http`, `context`,
and JSON. Runners SHOULD use `context.Context` for timeouts/cancellation and a
pooled `http.Client` with sane transport defaults.)_

---

## 16. Examples

**Import spec & call operation (generic):**

```bash
foxctl openapi import github.yaml --as=github

# Dry-run to inspect the request:
foxctl run http/openapi \
  --spec=memory:github \
  --operationId=listReposForUser \
  --params='{"path":{"username":"octocat"},"query":{"per_page":100}}' \
  --dry_run

# Real call with pagination and retry defaults:
foxctl run http/openapi \
  --spec=memory:github \
  --operationId=listReposForUser \
  --params='{"path":{"username":"octocat"},"query":{"per_page":100}}'
```

**Error (pagination detection):**

```json
{
  "version": 1,
  "status": "error",
  "command": "http/openapi",
  "data": {
    "issue": "pagination_detection_failed",
    "hint": "Try --paging.strategy=cursor --paging.cursor_field=next"
  },
  "meta": {
    "ts": "...",
    "duration_ms": 420,
    "source": "run",
    "runner": "exec",
    "partial": true
  },
  "error": {
    "code": "EPAGINATION",
    "message": "Could not auto-detect pagination strategy"
  }
}
```

**Large response → CAS wrapper (summary includes status & headers):**

```json
{
  "version": 1,
  "status": "ok",
  "command": "http/openapi",
  "data": {
    "summary": {
      "status_code": 200,
      "headers": { "etag": "W/\"abc...\"", "ratelimit-remaining": "4374" },
      "kind": "application/json",
      "size_bytes": 1048576,
      "record_count": 247,
      "preview": { "first_keys": ["id", "name", "created_at", "private"] }
    },
    "artifact": "sha256:abc123..."
  },
  "meta": {
    "ts": "...",
    "duration_ms": 1210,
    "source": "run",
    "runner": "exec"
  },
  "error": { "code": null, "message": null }
}
```

---

## 17. Versioning & Compatibility

- **Spec:** `v1`.
- **Envelope:** `version: 1`.
- **Skill API:** `foxctl/v1`. Backward‑compatible changes MAY add optional
  fields. Breaking changes MUST bump versions. The `http/openapi` skill **MUST**
  include `min_spec_version` and `max_spec_version` compatibility metadata.

---

## 18. Testing Guidance (non‑normative)

- **Tier 0:** unit tests with minimal specs (bearer, apiKey, basic;
  link/cursor/offset).
- **Tier 1:** real APIs (GitHub, Stripe, OpenWeatherMap).
- **Tier 2:** tricky specs (multiple auth schemes, non‑standard pagination, huge
  bodies).
- **CLI:** `foxctl openapi test` generates smoke tests for each operation
  (HEAD/GET if safe).

---

### Appendix A — Optional Tier‑2 Codegen (DX)

If desired, `foxctl openapi generate` MAY emit a “skillpack” with **one skill
per operationId** (or grouped by tag) whose wrappers simply call `http/openapi`.
This improves human CLI discovery while keeping logic centralized.

### Appendix B — Digest ABNF

`cas_digest = "sha256:" 64*HEXDIG` (lowercase).

---

**Bottom line:**

- JSON envelopes remain the **canonical wire contract**.
- A **generic OpenAPI skill** brings immediate, broad API coverage with
  **built‑in pagination, retry, auth mapping, CAS, and dry‑run**.
- **Plugins** (subprocesses speaking envelopes) make auth/pagination strategies
  **extensible** without bloating core.
- All examples and interfaces are **Go‑centric**, aligning with your
  implementation preference.
