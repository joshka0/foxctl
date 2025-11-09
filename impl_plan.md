Awesome—here’s a crisp, dependency-ordered implementation plan to ship **agentctl Core v1** with the **generic OpenAPI skill**, **plugin SPI**, and **Go-first** internals.

---

# Progress Snapshot

- ✅ Phases 0–2: bootstrap, envelope/config kernel, and CAS (with CLI + tests) are implemented.
- ✅ Phase 3 foundation: SQLite-backed jobs subsystem, `agentctl run` wired to skills, async worker, CAS pin/unpin per job, and bash-style skills (`fs/ls`, `text/grep`, `http/openapi`) hooked into the CLI.
- ✅ Phase 4 scaffolding: skill parser/installer, exec runner, wazero-based WASI runner, sample `wasi/echo` skill, and `agentctl skills install/list/run`.
- 🚧 Upcoming focus: Phase 5 cache & memory, plus the full OpenAPI skill + plugin SPI.

---

# Implementation Plan (dependency-ordered)

## Phase 0 — Repo & CI bootstrap

**Goal:** Lay the rails so every later change is testable and reproducible.

* Initialize repo scaffolding

  * `cmd/agentctl/` (Cobra skeleton), `internal/` packages, `docs/spec/`, `test/`
  * Add `AGENTS.md`, `docs/spec/core_profile_v1.md` (JSON envelope), `docs/spec/plugin_protocol.md`, `docs/spec/openapi_skill.md`
* Tooling & CI

  * `go.mod` (Go ≥1.22), `.golangci.yml`, `Makefile` (`fmt`, `lint`, `test`, `build`, `snapshot`)
  * CI workflow: lint → test (`-race`) → build (snapshot)
  * Logging baseline (zerolog), config baseline (viper)

**Depends on:** nothing
**Deliverables:** builds pass, CLI `agentctl version` works
**Acceptance checks:** `make lint && make test && make build` all succeed

---

## Phase 1 — Envelope & Config kernel

**Goal:** Canonical JSON envelope and config surface for all commands.

* `internal/envelope/`

  * Types, helpers: `Ok`, `Err`, stream progress writer
  * Canonicalization helpers (RFC8785) for cache keys
  * Validation (UTF-8, required fields)
* `internal/config/`

  * Load/merge: defaults → file → env → flags
  * Expose limits (`inline_output_kb`, `max_capture_kb`), paths (`~/.agentctl`)

**Depends on:** Phase 0
**Deliverables:** unit & golden tests for envelopes
**Acceptance checks:** `agentctl doctor` emits a valid envelope

---

## Phase 2 — CAS (content store)

**Goal:** Store large results; integrity verified on read.

* `internal/cas/`

  * `Put(ctx, r, kind, tags) -> digest, size`
  * `Head`, `Get` (recompute SHA-256; mismatch ⇒ `EIO`)
  * On-disk layout `~/.agentctl/cas/sha256/<digest>`
* CLI: `agentctl cas put|head|get|list|pin|unpin|rm`

**Depends on:** Phase 1
**Deliverables:** CAS package + CLI + tests (concurrency, integrity)
**Acceptance checks:** round-trip puts/gets with integrity; pins survive `gc --dry-run`

---

## Phase 3 — Jobs subsystem

**Goal:** Durable execution with logs/progress/result envelopes.

* `internal/jobs/`

  * SQLite (modernc): tables + indexes (state, workspace, args_hash)
  * Lifecycle: submit/ls/status/tail/wait/result/cancel, `--dedupe`
  * Filesystem layout: `~/.agentctl/jobs/<ulid>/` (result.json, stdout.log, progress.ndjson)
* CLI: `agentctl jobs …`
* Crash recovery: orphan `running` → `error (ERUNTIME_RESTART)`

**Depends on:** Phase 1
**Deliverables:** jobs API, CLI, tests (state transitions, dedupe, recovery)
**Acceptance checks:** submit a dummy “echo” job and observe progress/result envelopes

---

## Phase 4 — Runners & Skill manifest

**Goal:** Execute skills with sandbox & policy enforcement.

* `internal/skill/`

  * `skill.yaml` parser/validator (name regex, capabilities)
  * Discovery: list/describe/search/install (local path only for MVP)
* `internal/runner/exec/`

  * Process exec with resource limits; ephemeral `/work`; policy enforcement (egressAllow)
* `internal/runner/wasi/`

* wazero (pure Go); **Core rule:** `distribution: wasi` ⇒ `network:"none"` validated at install
* CLI: `agentctl skills list|describe|install`, `agentctl run <skill> …` (sync)

**Depends on:** Phases 1–3
**Deliverables:** run a trivial skill producing a small envelope
**Acceptance checks:** WASI skills reject network; exec skills honor egress policy

---

## Phase 5 — Cache & Memory

**Goal:** Deterministic memoization and named memories.

* `internal/cache/`

  * Cache key = `sha256(skill, version, RFC8785(args), sorted input digests)`
  * Modes: `auto|off|only`; hits set `meta.source:"cache"` + `meta.cache_key`
* `internal/memory/`

  * Auto-cache table (24h TTL), named memories table; `UNIQUE(name, workspace)`
  * Workspace autodetect (`.agentctl/`, `.git/`, project files)
* CLI: `agentctl memory recent|cache|put|save|get|search|list|update|delete|relevant`

**Depends on:** Phases 1–4
**Deliverables:** cache hits return identical wrappers; named memory persists
**Acceptance checks:** run same skill twice → 2nd is cache hit; `memory save/get` round-trip

---

## Phase 6 — OpenAPI Tier-1 skill (generic)

**Goal:** Call any OpenAPI 3.x operation; summaries + CAS; dry-run; retries; pagination; auth.

* `internal/openapi/`

  * Spec loader: path | CAS digest | `memory:<name>`
  * Resolver: `operationId` → method/path/params; **lenient** parse by default; `--strict` option
  * Param validator (pre-flight): types/required; actionable `EARG` with hints
  * Request builder: path/query/header/body; redaction for secrets in logs/dry-run
  * **Auth (built-in):** bearer, apiKey(header/query), basic, oauth2 client-credentials (helper token flow)
  * **Pagination (built-in):** Link headers; cursor field (`next`, `next_page_token`); offset/limit
  * **Retry:** backoff on 429/5xx with `Retry-After`
  * **Dry-run:** `request_plan` envelope (no network)
  * Response handling:

    * success → small JSON inline; large → CAS wrapper with `status_code`, headers summary, `record_count`, `preview`
    * **4xx validation:** keep inline (bounded by `inline_output_kb`), don’t artifactize
    * **5xx after retries:** `ERUNTIME` with upstream request id in meta if present
  * Errors: `EAUTH`, `EPAGINATION`, `EOPENAPI`, `ERATELIMIT` with `data.hint`
* CLI:

  * `agentctl openapi import <file|url> --as=<name> [--strict]` (stores spec in CAS + named memory)
  * `agentctl openapi validate|test` (smoke)
  * `agentctl run http/openapi --spec=memory:<name> --operationId=<id> --params='{}' [--dry_run]`

**Depends on:** Phases 1–5 (CAS, jobs optional for sync MVP)
**Deliverables:** end-to-end call against a public API using dry-run + real run
**Acceptance checks:** GitHub `listReposForUser` works; large responses artifactize; retries & pagination verified with golden tests

---

## Phase 7 — Plugin SPI (auth & pagination)

**Goal:** Extensibility without bloating core—subprocess plugins via envelopes.

* `internal/openapi/plugin/`

  * Protocol: stdin/stdout **JSON envelopes** (`plugin/auth`, `plugin/pagination`)
  * Discovery: `AGENTCTL_OPENAPI_PLUGIN_PATH`, explicit `plugin:<name>` in hints/flags
  * Timeout/cancellation wired to context; result validation
* Example plugins (Go) in `plugins/`

  * `auth-hmac/` (header signature)
  * `paging-custom/` (reads vendor field)
* Spec vendor extensions respected: `x-agentctl.auth`, `x-agentctl.pagination.strategy`, etc.

**Depends on:** Phase 6
**Deliverables:** working plugins + tests (WASI and exec)
**Acceptance checks:** force a call through plugin path; envelope contract upheld

---

## Phase 8 — Jobs integration & UX polish

**Goal:** Async workflow and user ergonomics.

* Wire `http/openapi` into jobs: `jobs submit/tail/result --emit=digest|envelope`
* `--remember` on `run` to persist a result as named memory; `memory relevant` uses workspace ranking
* `gc --dry-run` considers pins and memory references
* CLI help, examples, error messages with hints
* Docs: finalize specs, add how-tos, update `AGENTS.md` checklists

**Depends on:** Phases 3, 5–7
**Deliverables:** smooth async experience with progress NDJSON and final envelope
**Acceptance checks:** submit long-running paginated call, tail progress, fetch final artifact digest

---

## Optional Phase 9 — Tier-2 Codegen (DX)

**Goal:** Nice human CLI, logic stays in Tier-1 skill.

* `agentctl openapi generate <memory:<name>> [--install]`

  * Emit wrappers (one skill per operation or per tag); wrappers call `http/openapi` internally
  * Autocomplete/help text; parameter typing surfaced in `skill.yaml`
* Golden tests on generated skillpacks

**Depends on:** Phase 6
**Deliverables:** generated skills installed & discoverable
**Acceptance checks:** `agentctl skills list` shows per-operation skills; wrappers behave identically to generic

---

# Cross-cutting “Definition of Done”

* Envelopes on **stdout**, logs on **stderr** (verified in tests)
* Large outputs → **CAS wrapper**; `meta.cas_digest` set
* **Secrets redacted** everywhere; never appear in logs/envelopes
* **Context cancellation** respected (Ctrl-C cancels; state → `canceled`)
* **Golden tests** for envelope shapes & summaries
* `make fmt && make lint && make test-race && make build` green in CI
* Docs updated (specs + `AGENTS.md`), CLI help accurate

---

# Risk register & mitigations (brief)

* **Spec drift / messy OpenAPI** → default lenient parse, `--strict` flag; actionable `EOPENAPI` errors; `openapi validate/test` commands.
* **Pagination edge cases** → clear override flags, plugin escape hatch; partial+hint error with `EPAGINATION`.
* **Memory/GC data loss** → pins + memory references excluded from GC; `gc --dry-run` only.
* **WASI networking loopholes** → hard validation at install: WASI ⇒ `network:"none"`; tests enforce.

---

## Minimal tracer bullets (quick vertical slices)

1. **Echo skill** → envelope plumbing + CLI
2. **CAS put/get** → artifact wrapper + integrity check
3. **OpenAPI dry-run** → request plan envelope
4. **OpenAPI success (small)** → inline body + headers + status
5. **OpenAPI success (large)** → CAS wrapper + summary
6. **Pagination (Link)** → multi-page aggregation
7. **Retry (429)** → backoff then success
8. **Auth plugin** → HMAC header injection via plugin
9. **Jobs + tail** → progress NDJSON + final result

Each tracer bullet lands tests & docs.

---

If you want, I can turn this into a set of **GitHub Issues / PR checklist** (one per phase) with concrete subtasks and sample acceptance commands you can paste directly into your tracker.
