# Phase 6 — OpenAPI Tier‑1 Skill (`http/openapi`)

**Status:** In Progress\
**Last Updated:** 2025-11-30

This document slices the existing
[`docs/spec/openapi_skill.md`](./openapi_skill.md) into a concrete
implementation plan for Phase 6 of the Core Profile v1 roadmap. The OpenAPI spec
doc remains the **canonical wire and behavior specification**; this document
defines scope, milestones, and acceptance criteria for the initial
implementation.

---

## 1. Scope & Dependencies

**Goal:** Implement the `http/openapi` Tier‑1 skill and CLI so that agentctl can
invoke arbitrary OpenAPI 3.x operations with:

- Spec‑as‑memory (`spec` from path / CAS / named memory)
- Built‑in auth (bearer, apiKey, basic, oauth2 client‑credentials)
- Built‑in pagination (link headers, cursor, offset/limit)
- Retries & rate‑limit handling
- CAS integration & summaries
- Dry‑run mode

**Depends on:**

- Phase 3 (CAS + artifacts)
- Phase 4 (jobs + sync/async execution)
- Phase 5 (cache + named memory)

**Out of scope for Phase 6:**

- Plugin SPI for custom auth/pagination (Phase 7)
- Tier‑2 generated wrappers (`agentctl openapi generate`, Phase 9)
- Non‑HTTP protocols

---

## 2. Architecture & Components

### 2.1 Packages

- `internal/openapi/`
  - `loader.go` — load spec from path / CAS / memory
  - `resolver.go` — map `operationId` → method, path, parameter schema
  - `params.go` — coerce & validate `params` payload
  - `auth/` — built‑in auth implementations
  - `paging/` — built‑in pagination strategies
  - `client.go` — request execution, retries, response capture
  - `summarize.go` — CAS wrapping + envelope summaries
- `skills/http_openapi/` (or similar)
  - Small `main.go` wiring stdin/stdout envelopes to `internal/openapi`
- `cmd/agentctl/cmd/openapi.go`
  - CLI helpers: `openapi import|describe|validate|test`
  - `agentctl run http/openapi` uses this skill via normal `run` path

### 2.2 Execution Model

- Skill is an **exec** skill (`network: egress`), not WASI.
- Wire contract:
  - Input JSON matches `Input Envelope Schema` in `openapi_skill.md` (§3).
  - Output is a standard Core v1 envelope; large results use CAS (§4 of Core
    Profile + `openapi_skill.md` §10).
- All network I/O happens inside the skill process; Core runtime stays
  WASI‑strict.

---

## 3. Inputs & Outputs (Summary)

Full details stay in `openapi_skill.md`; this section summarizes what we must
support in Phase 6.

### 3.1 Input

Top‑level fields (see `openapi_skill.md` §3):

- `spec: string` (required)
  - `"/path/to/openapi.yaml"` or `"./spec.json"`
  - `"sha256:<hex>"` (CAS)
  - `"memory:<name>"` (named memory)
- `operationId: string` (required)
- `params: object` (optional)
  - `path`, `query`, `header`, `body`
  - Strings MAY be coerced into typed values per schema (lenient by default).
- `auth: object` (optional)
- `paging: object` (optional)
- `retry: object` (optional)
- `dry_run: bool` (optional)

### 3.2 Output

On **success**, `status="ok"`, `command="http/openapi"`, and:

- `data.summary` (object, small) — MUST include at least:
  - `status_code: int`
  - `headers: map[string]string` (subset; redacted as needed)
  - `paging` metadata (e.g. `has_more`, `next_cursor`, `page_count`)
- `data.body` (optional) — inline JSON body when "small enough".
- `data.artifact` (optional) — CAS digest for large responses.
- `meta.cas_digest` is optional; if set it MUST match `data.artifact` and MUST
  be omitted when `data.artifact` is absent.

On **validation or runtime errors**, follow `openapi_skill.md` §12 plus Core
Profile §13:

- `EAUTH` for auth failures
- `EPAGINATION` for pagination strategy failures
- `EOPENAPI` for spec parse/validation errors
- `ERATELIMIT` for rate‑limit exhaustion after retries
- `EARG` for input schema/param errors
- `EIO` for filesystem/network I/O failures

---

## 4. Spec Management (`spec` field)

### 4.1 Sources

Implement a `Loader` that can resolve:

1. **Filesystem path**: YAML or JSON files
   - Resolve relative paths against the workspace or current working dir.
2. **CAS digest**: `sha256:<hex>`
   - Use CAS store to load raw bytes, then parse as YAML/JSON.
3. **Named memory**: `memory:<name>`
   - Use named memory store to fetch the envelope.
   - Interpret `data` or `result` as the spec content (YAML or JSON).

### 4.2 Caching

- In‑process cache (map from `spec_key` → parsed spec) with TTL (e.g. 24h) to
  avoid repeated parse cost.
- Cache key SHOULD incorporate digest when loading from CAS or memory so that
  updates invalidate correctly.
- This is _not_ the Phase 5 CAS/memory cache; it is an internal, per‑process
  optimization.

### 4.3 Validation

- Parse using a tolerant OpenAPI parser (3.0.x/3.1.x).
- Validation must catch at least:
  - Missing `paths`/`components`.
  - Duplicate or missing `operationId`.
- On fatal issues, emit `EOPENAPI` with `data.hint` and examples.

---

## 5. Operation Resolution & Params

### 5.1 Operation Resolution

- Given `operationId`, locate the operation in the parsed spec.
- Extract:
  - HTTP method (GET/POST/…)
  - Path template (e.g. `/users/{username}`)
  - Parameter list & locations
  - RequestBody schema (if present)
  - Expected response schemas (for summarization heuristics)

If `operationId` is not found, emit `EOPENAPI` with clear hint and list of
closest matches.

### 5.2 Params Handling

- Accept `params.path`, `params.query`, `params.header`, `params.body`.
- Responsibilities:
  - **Coercion**: Convert string/number/bool values into the types required by
    schema (lenient by default).
  - **Validation**: Check required parameters, enum domains, basic format checks
    (e.g. integer vs string).
  - **Location routing**: Place values in path, query string, headers, or body
    as needed.

On validation failure:

- Emit `EARG` with `data.hint` and a list of failing parameters.
- Do **not** make any network request.

---

## 6. Auth (Built‑in)

Implement built‑in auth schemes as described in `openapi_skill.md` §6.

### 6.1 Supported Schemes

- `bearer`
  - Token sourced from `/run/secrets/<name>` or environment.
- `apiKey`
  - Location (header/query) derived from spec where possible.
- `basic`
  - Username/password from secret material.
- `oauth2` (client‑credentials)
  - Token endpoint, client id/secret from spec and/or hints.

### 6.2 Selection Rules

- If `auth.scheme` explicitly set: use it.
- Else: auto‑detect from `security` + `components.securitySchemes`.
- Never log raw credentials; log `"***"` redactions only.

On failures, emit `EAUTH` with high‑level hint; do not leak secrets.

---

## 7. Pagination

Implement the built‑in strategies in `openapi_skill.md` §7.

### 7.1 Strategies

- `link` header based (GitHub style)
- `cursor` field in body (e.g. `next`, `next_page_token`)
- `offset`/`limit` query parameters

### 7.2 Controls

- `paging.strategy`: `auto`, `link`, `cursor`, `offset`, `none`.
- `paging.max_pages` and `paging.max_items`:
  - Hard caps; MUST stop requesting once any cap is hit.

On pagination misconfiguration or irreconcilable responses, emit `EPAGINATION`
with `data.hint` and partial results summary if safe.

---

## 8. Retries & Rate Limiting

### 8.1 Retry Policy

- Default:
  - `base_ms = 250`, `factor = 2.0`, `max_attempts = 5`, `max_ms = 8000`.
- Retry on:
  - HTTP 429 (Too Many Requests)
  - 5xx (server errors), _excluding_ 501/505 unless configured.
- Respect `Retry-After` when present.

### 8.2 Error Surfacing

- If retries exhausted due to rate limiting → `ERATELIMIT`.
- If still failing with 5xx → `ERUNTIME` with upstream context in `data.hint`
  and selected headers (e.g. correlation id).

---

## 9. Dry‑Run Mode

When `dry_run=true`:

- Do **not** perform any network requests.
- Instead, emit an envelope with:
  - `status="ok"`
  - `data.request_plan` including:
    - Final URL
    - HTTP method
    - Redacted headers
    - Body schema / example payload (if available)
- This mode SHOULD reuse the same resolver and params logic as a real call, to
  catch errors early.

---

## 10. Response Processing & CAS

### 10.1 Inline vs CAS

- Use Core Profile limits (e.g. `inline_output_kb`) to decide whether to inline
  the body or store as CAS artifact.
- For large responses:
  - Write full body to CAS.
  - Return wrapper envelope with `data.summary` and `data.artifact` (and
    optionally `meta.cas_digest`).

### 10.2 Summaries

Summaries should capture:

- `status_code`
- Selected headers (e.g. `etag`, `content-type`, rate‑limit info)
- Approximate `record_count` when the response is a JSON array
- Optional `preview` (first N items or fields)

### 10.3 Error Responses

- 4xx validation/usage errors **MUST NOT** be artifactized; keep within
  `inline_output_kb`.
- 5xx errors after retries SHOULD include enough context in `data` to debug, but
  still respect size limits.

---

## 11. CLI Surface

### 11.1 Spec Management Commands

- `agentctl openapi import <file|url> --as=<name> [--strict]`
  - Store spec in CAS and named memory (type `openapi_spec`).
- `agentctl openapi describe memory:<name>`
  - List available operations, params, and auth hints.
- `agentctl openapi validate <spec-ref>`
  - Run parser and basic checks; emit `ok` or `EOPENAPI`.

### 11.2 Execution

- `agentctl run http/openapi --spec=memory:<name> --operationId=<id> --params='{}' [--dry_run]`
  - CLI wrapper is thin: it only helps construct the `data` payload for the
    `http/openapi` skill and defers to the normal `run` machinery.

---

## 12. Testing & Golden Fixtures

### 12.1 Unit Tests

- Loader tests: path / CAS / memory inputs, failure modes.
- Resolver tests: valid and missing `operationId`.
- Params tests: type coercion, required fields, enums, nested objects.
- Auth tests: bearer/apiKey/basic/oauth2 flows; redaction in logs.
- Pagination tests: link/cursor/offset, `max_pages`/`max_items` caps.
- Retry tests: 429 and 5xx handling with backoff.
- Dry‑run tests: request_plan structure correctness.

### 12.2 Golden Fixtures

Under `test/golden/openapi/`:

- `github_list_repos_ok.json` — success envelope with CAS wrapper.
- `github_list_repos_dry_run.json` — request_plan example.
- `bad_spec_eopenapi.json` — invalid spec error.
- `auth_failure_eauth.json` — masked auth error.
- `pagination_epagination.json` — pagination strategy failure.

Tests should compare entire envelopes (minus timestamps) to golden fixtures.

---

## 13. Acceptance Criteria

- [ ] `http/openapi` can call a real API (e.g. GitHub) end‑to‑end using an
      imported spec in named memory.
- [ ] Large responses are stored in CAS with summaries and digests.
- [ ] Built‑in auth schemes work and never log secrets.
- [ ] Pagination strategies handle link/cursor/offset APIs with caps.
- [ ] Retries respect rate limits and surface `ERATELIMIT` when exhausted.
- [ ] Dry‑run mode produces accurate request plans with no network calls.
- [ ] All golden tests in `test/golden/openapi/` pass.
- [ ] Core Profile v1 contracts (envelope, CAS, error codes) are upheld.
