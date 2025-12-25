# AGENTS.md — AI Assistant Guide for agentctl

**Last Updated:** November 30, 2025\
**Target Audience:** AI coding assistants (Claude, Cursor, GitHub Copilot, etc.)
and human contributors

---

## 🚨 TL;DR (agentctl-specific)

This file supplements `.windsurf/rules/global_rules.md` with
**agentctl-specific** conventions. Obey global rules first, then these:

- **Envelope contract is sacred** — never change `meta.*` fields or envelope
  shape without spec + golden updates.
- **WASI = `network:"none"`** — Core v1 mandates isolation; do not relax.
- **Large outputs → CAS** — use `data.summary` + `data.artifact` (and optional
  `meta.cas_digest` matching it).
- **`--dry-run` required** for any state-changing CLI command.
- **Every production bug or near-miss becomes a Gotchas Graveyard row and a new
  rule entry in `docs/start/gotchas.md` within 24 hours — no exceptions.**

## 🤖 Hello, AI Assistant!

This file is for you. It encodes our source‑of‑truth conventions so you can
safely help us build **agentctl Core Profile v1**.

For deeper, frequently updated details, see `docs/start/README.md` and the files
under `docs/start/`.

- Canonical **JSON envelope** I/O (no binary inline)
- **CAS** for large results with mandatory summaries
- **Jobs** with durable state + **Memory** (auto‑cache + named)
- **WASI‑first** sandbox + **exec** runner when required
- **Generic OpenAPI skill** (`http/openapi`) + **auth/pagination plugins**
- **Go‑first** implementation (no Node/TS)

When in doubt, prefer the sources in **📚 Canonical Sources** and follow the
**Do/Ask/Act** guardrails.

---

## 🌿 Branching & PR Norm (critical)

1. **Create branch**
2. **Open PR** into `main` (never push to `main`).
3. **CI must pass** (lint, vet, unit tests, race).
4. **Approval required** (at least one human).
5. **Release** handled via our release workflow (you do **not** publish).

**Why:** keeps AI changes reviewable, secret‑free, and reproducible.

### Mega-PR Workflow (spec batches)

When multiple spec PRs need to land together, follow this script (or run the
`/mega-pr` alias once available):

1. **Rebase each source branch**: `git fetch origin <branch>` →
   `git switch <branch>` → `git rebase origin/main` → run `make fmt`,
   `make lint`, `CGO_ENABLED=0 go test ./...`, then `git push -f`.
2. **Create aggregation branch**: `git checkout main && git pull --ff-only`,
   then `git checkout -b codex/<mega-name>`.
3. **Merge or cherry-pick branches sequentially** onto the mega branch,
   resolving conflicts as you go.
4. **Validate once** on the mega branch: `make fmt`, `make lint`,
   `CGO_ENABLED=0 go test ./...`.
5. **Push & open** mega PR with
   `gh pr create --base main --head codex/<mega-name>` (mention the superseded
   PR numbers).
6. **Close superseded PRs** with a comment like “Superseded by mega PR #XX” and
   delete their remote branches.

This keeps CodeRabbit focused on a single diff and avoids repeated conflict
resolution across overlapping spec branches.

---

## 📋 Quick Reference Card

```yaml
Project: agentctl (single-binary CLI)
Language: Go 1.24.x (modules on, CGO off by default)
CLI: Cobra + Viper
I/O Contract: JSON envelopes (canonical, version: 1)
    Runners: WASI (wazero, pure Go) preferred; exec runner fallback
    Storage: SQLite (modernc.org/sqlite) + filesystem CAS (~/.agentctl/cas)
    Hashing: SHA-256 digests (sha256:<hex>)
    Cache Keys: RFC 8785 canonical JSON + input blob digests
    OpenAPI: Generic skill http/openapi (built-ins + plugins)
    Plugins: Out-of-process (WASI or exec) via JSON envelopes
    Logging: Zerolog (JSON), logs -> stderr; envelopes -> stdout
    Lint/Format: golangci-lint + gofumpt + govet + staticcheck
    Tests: go test ./... (with -race); golden tests in testdata/
    Releases: goreleaser (snapshot in CI; tagged release by humans)
```

---

## 📚 Documentation

**Usage & quick start:** See [`README.md`](README.md) for build instructions,
CLI examples, and development setup.

**Full index:** [`docs/start/README.md`](docs/start/README.md) — directory
tree + quick reference table.

**Key specs:**

- `docs/spec/core_profile_v1.md` — envelope, CAS, jobs, runners (§2, §4, §7,
  §10)
- `docs/spec/openapi_skill.md` — http/openapi input/output
- `docs/spec/plugin_protocol.md` — auth/pagination plugins
- `docs/agent_profile.md` — multi-agent orchestration

> If documents disagree, prefer `docs/spec/`. If still ambiguous, **ASK** in PR.

---

## 🏗️ Repository Layout (expected)

```
agentctl/
├── cmd/agentctl/                  # main CLI
│   ├── cmd/                       # Cobra command tree
│   └── main.go
├── cmd/agentctl_viewer/           # optional TUI viewer
├── internal/
│   ├── domain/                    # pure domain types (envelope, policy, skill, ...)
│   ├── protocol/                  # wire-level helpers (wraps domain/envelope)
│   ├── platform/                  # config/logging/errors/workspace
│   ├── storage/                   # CAS, jobs, cache, memory, mailbox, ...
│   ├── execution/                 # skill runners (wasi/exec), quotas, scheduler
│   ├── openapi/                   # http/openapi internals (loader/builder/client/...)
│   └── agent/                     # agent runtime/daemon/tools
├── skills/
│   └── http_openapi/              # skill.yaml + Go implementation wrapper (exec)
├── plugins/                       # example plugins (Go, WASI/exec)
│   ├── auth-hmac/                 # plugin/auth
│   └── paging-custom/             # plugin/pagination
├── docs/
│   ├── spec/core_profile_v1.md
│   ├── spec/openapi_skill.md
│   └── spec/plugin_protocol.md
├── test/
│   ├── integration/               # live API tests (opt-in)
│   └── golden/                    # envelopes, summaries, CAS wrappers
├── Makefile
├── .golangci.yml
├── go.mod
└── AGENTS.md
```

This layout is a simplified view. See `docs/architecture.md` for an up-to-date
overview of all subsystems.

---

## 🤖 Agent Decision Tree

```
Start
 ├─ Are you changing the envelope schema or wire behavior?
 │    ├─ YES → Update docs/spec/core_profile_v1.md + golden tests + bump minor if compatible
 │    └─ NO  → continue
 ├─ Are you touching network capabilities?
 │    ├─ YES → Enforce WASI: network=none (Core v1), exec only for egress; update egressAllow tests
 │    └─ NO  → continue
 ├─ Is output possibly large?
 │    ├─ YES → Use CAS wrapper (summary + artifact); meta.cas_digest optional (must match artifact if set)
 │    └─ NO  → continue
 ├─ Is this OpenAPI logic?
 │    ├─ Add/modify in internal/openapi/ (built-ins) or plugins/
 │    ├─ Include dry-run, retries, pagination, auth mapping
 │    └─ Never log secrets; redact "***"
 └─ Always:
      - Envelopes to stdout, logs to stderr
      - Include context.Context, handle cancellation
      - Add unit tests + golden outputs
      - Run `make check` (fmt + lint + vet + test + check-coverage + build)
      - Run `make test-race` when changing concurrency/cancelation-sensitive code
```

---

## 🔧 Critical Technologies & Rules

### 1) Envelopes (non-negotiable)

- `version: 1`, `status: "ok"|"error"`, `command`, `data`, `meta`, `error`.
- `meta.ts` **MUST** be present (RFC3339 UTC).
- For `status:"ok"`, `error.code` and `error.message` **MUST** be empty.
- For `status:"error"`, `error.code` and `error.message` **MUST** be non-empty.
- **On large results:** `data.summary` (≤2 KiB) + `data.artifact` digest;
  `meta.cas_digest` is optional (if set MUST match `data.artifact`).
- **Errors:** prefer actionable `error.code` (+ `data.hint`).
- **Stdout:** envelopes only. **Stderr:** logs only.

### 2) CAS

- **Put** returns digest (`sha256:<hex>`).
- **Get** must **verify integrity** by recomputing digest (fail `EIO` on
  mismatch).
- Store under `~/.agentctl/cas/sha256/<digest>`.

### 3) Jobs & Memory

- States: `queued|running|ok|error|canceled` (terminal).
- Persist job artifacts/outputs under `~/.agentctl/jobs/<ulid>/`.
- Auto‑cache hits must set `meta.source:"cache"` and include `meta.cache_key`.

### 4) OpenAPI Skill (`http/openapi`)

- Input: `spec` (path/CAS/memory), `operationId`, `params`
  (path/query/header/body), `paging`, `retry`, `auth`, `dry_run`.
- **Built-in auth:** bearer, apiKey, basic, oauth2 client‑credentials.
- **Built-in pagination:** link headers, cursor field, offset/limit.
- **Plugins** for auth/pagination: subprocess over JSON envelopes.
- **Dry-run:** emit `request_plan` (URL, redacted headers), no network call.
- **4xx validation errors:** **do not** artifactize; include error object inline
  (within `inline_output_kb`).
- Include `status_code` and key headers in summaries (e.g., `etag`,
  `ratelimit-remaining`).

### 5) Networking & Secrets

- **Core v1 rule:** WASI → `network:"none"` (validate at install).
- Exec runner may allow `network:"egress"` with optional `egressAllow`.
- Secrets mounted at `/run/secrets/<name>`. Never log secret values; redact
  `"***"`.

### 6) Filesystem Skills

- All `fs/*` and `text/grep` skills must route user paths through
  `policy.PathValidator`.
- The validator anchors to the current workspace and only allowlists explicit
  roots (`cfg.Home`, `os.TempDir()`).
- Reject traversal attempts early so we fail with actionable `EPOLICY` guidance
  instead of touching the host filesystem.
- The executor exports the workspace path via `AGENTCTL_WORKSPACE`; skills
  should prefer that over `os.Getwd()` when sandboxed.

---

## 🧭 Do / Ask / Act (agentctl-specific)

> **Global rules from `.windsurf/rules/global_rules.md` always apply.** This
> section covers agentctl-specific conventions only.

**Do** — safe without asking:

- Write Go code with tests, docs, and golden outputs.
- Improve built‑in OpenAPI auth/pagination heuristics.
- Refactor internals without breaking the wire contract.

**Ask** — require human approval:

- Breaking envelope changes (fields, semantics).
- Network policy changes (egress, wildcards).
- New on‑disk layouts, path migrations, or DB schema changes.
- Adding new built‑in auth schemes beyond bearer/apiKey/basic/oauth2-cc.

### ❌ agentctl Hard Fails (beyond global AUTO-REJECT)

| Pattern                                   | Why                    |
| ----------------------------------------- | ---------------------- |
| Changing `meta.*` or envelope fields      | Wire contract break    |
| `network:` not `"none"` in WASI manifest  | Core v1 isolation      |
| Emitting non-JSON stdout from CLI/skills  | Envelopes-only forever |
| Missing `--dry-run` on state-changing cmd | Safety valve           |

### Spec ↔ Code Drift Watchlist

| Topic                | Spec (canonical)                                             | Code (actual)                                                                                                                           | Action                                             |
| -------------------- | ------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------- |
| inline_output_kb     | `docs/spec/core_profile_v1.md` §2 → 32 KB default            | `internal/platform/config.DefaultInlineOutputKB = 32` (tested in `internal/platform/config/config_test.go`)                             | Keep spec + constant in lockstep                   |
| WASI network rule    | `docs/spec/core_profile_v1.md` §10 → WASI = `network:"none"` | Manifest validation + `scripts/checkmanifests` enforce the restriction                                                                  | Add regression tests whenever new WASI skill ships |
| Envelope meta fields | `docs/spec/core_profile_v1.md` §2 enumerates `meta.*`        | `internal/domain/envelope.Envelope` + `internal/protocol.Validate` + CLI smoke tests (`cmd/agentctl/cmd/run_test.go`) assert invariants | Extend smoke tests when adding new meta fields     |

---

## 🧑‍💻 Go Conventions (agentctl-specific)

> General Go style (context propagation, error wrapping, no panics) is enforced
> by `.windsurf/rules/global_rules.md`. Below are agentctl-specific patterns.

- **Logging:** zerolog (JSON) → stderr only; typed fields, not interpolated
  strings.
- **Envelope IO:** stdout = envelopes only; never plain text.
- **Large output:** stream → CAS; include `data.artifact` (and optional
  `meta.cas_digest` matching it).
- **TOCTOU:** validate and operate on the _same_ resolved path (symlinks can
  swap).
- **Nil vs empty:** return `[]T{}` or `map[K]V{}`, not `nil`, for JSON output.

---

## 🧪 Testing Requirements

Agents should assume:

- Local `make` targets (`make test`, `make test-race`, `make lint`,
  `make check-coverage`) are the contract for "done".
- Race tests exclude `internal/storage/vector` by default; see docs for how to
  run vector tests under `-race` when needed.
- New features in envelopes, CAS, jobs, OpenAPI, and plugins come with unit +
  golden tests and, when relevant, integration tests.

**Golden files:**

- Any new or changed envelope shape should update or add a golden JSON file in
  `test/golden/envelopes/` (or the relevant skill `testdata/*.json`) together
  with a table-driven golden test.

For detailed expectations, coverage thresholds, race/CGO notes, watcher /
feedback hooks, and CI jobs, see:

- `docs/start/testing_and_ci.md`
- `docs/impl_plan/universal_swe_grep_and_agents_testing.md`

---

## 🪦 Gotchas Graveyard — Every scar is now a law

These are the issues that have already hurt us (or nearly did). Each row is a
real incident → real root cause → a permanent new rule plus a regression test.

| #  | Gotcha (what hurts)                                                                                                     | Real incident (date + link)                                                                                                | Root cause                                                                  | Permanent new AUTO-REJECT rule (id)                                                                                                  | Added regression test? |
| -- | ----------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ | ---------------------- |
| G1 | Local Go CGO toolchain crash (`runtime/cgo: ... cgo: exit status 2`) when running race tests or building certain skills | 2025-11-30 — local dev on macOS with Go 1.24.10 via mise (`make test-race`)                                                | Misconfigured local Go/CGO toolchain, not a code regression                 | See rule entry `R1` in `docs/start/gotchas.md`; never treat toolchain/CGO crashes as "tests green"                                   | Pending                |
| G2 | JSON helpers returning `nil` slices/maps, producing `null` instead of `[]`/`{}` and surprising downstream tools         | 2025-12-01 — post-review JSON helpers returned `nil` for empty files/metadata, causing `null` in envelopes and viewer bugs | Helpers violated the "nil vs empty" rule and did not normalize empty inputs | See rule entry `R2` in `docs/start/gotchas.md`; JSON-facing code must always return `[]T{}` / `map[K]V{}` so serialization is stable | Pending                |
| G3 | CGO linker `duplicate symbol '_sqlite3_*'` when a build links both `go-libsql` and `go-sqlite3`                         | 2025-12-20 — CGO-enabled builds/tests failed due to SQLite symbol conflicts between embedded SQLite copies                 | `go-libsql` and `go-sqlite3` both embed SQLite by default under CGO         | See rule entry `R3` in `docs/start/gotchas.md`; CGO builds/tests must use `-tags=libsqlite3` (and install system SQLite dev libs)    | Yes (CI)               |

### How the graveyard works (zero tolerance)

1. Every time we hit a production bug or serious near-miss, we add one row to
   this table **within 24 hours**.
2. In the same PR, we add or update the corresponding rule entry in
   `docs/start/gotchas.md` that references the Gotcha ID (e.g. `G1`) and
   documents how the mistake is blocked going forward.
3. We add or update a regression test that would have caught the incident.
4. The mistake becomes structurally impossible to repeat for both humans and
   agents without tripping the new rule and test.

---

## 🔌 Plugin Skeletons (Go)

Plugins are out-of-process helpers used by the OpenAPI skill for custom auth and
pagination. They speak JSON envelopes over stdin/stdout and are discovered via
plugin search paths.

For concrete Go skeletons and protocol details, see:

- `docs/start/openapi_and_plugins.md`
- `docs/spec/plugin_protocol.md`

---

## 🛠️ Make Targets & Quick Commands

```bash
# Dev ergonomics
make fmt                # gofumpt
make lint               # golangci-lint + staticcheck + govet
make test               # unit tests (no network)
make test-race          # unit tests with -race
make test-integration   # integration tests (build tag: integration)
make build              # build cmd/agentctl
make snapshot           # goreleaser --snapshot
make check-coverage     # enforce ≥85% coverage locally

# OpenAPI convenience
agentctl openapi import github.yaml --as=github
agentctl run http/openapi \
  --input '{"spec":"memory:github","operationId":"listReposForUser","params":{"path":{"username":"octocat"},"query":{"per_page":100}},"dry_run":true}'

# CAS maintenance
agentctl cas gc --older-than=168h --dry-run   # preview safe cleanup

# All commands output JSON envelopes to stdout.
```

---

## 🧷 Error Codes (remember)

`EARG, EOPENAPI, EAUTH, EPAGINATION, ERATELIMIT, ERUNTIME, ERUNTIME_RESTART, EOUTPUT_TOO_LARGE, EPOLICY, ENOTFOUND, ETIMEOUT, ESKILLDOWN, EPARSE, EENVELOPE, EIO, ECANCELED, ECACHE_MISS, ECACHE_UNAVAILABLE`

**Tip:** include `data.hint` where possible to teach the user how to fix the
problem.

---

## 🔐 Security (agentctl-specific)

> Secrets handling is governed by `.windsurf/rules/global_rules.md`. Below are
> agentctl-specific security rules.

- WASI runner must enforce `network:"none"` (Core v1).
- Exec runner: loopback & UNIX sockets denied unless explicitly allow‑listed.
- CAS is integrity‑checked on every `get` (fail `EIO` on mismatch).
- Secrets mount path: `/run/secrets/<name>`.

---

## 🧭 Common Pitfalls

- Returning plain text to stdout (violates “envelopes everywhere”).
- Artifactizing 4xx validation errors (keep inline, bounded by
  `inline_output_kb`).
- Using wall‑clock for duration; use monotonic clock for `duration_ms`.
- Reading huge responses into RAM when they should be streamed → CAS.
- Forgetting to set `meta.source:"cache"` on cache hits.
- Attempting to read outside the workspace—`policy.PathValidator` now blocks
  traversal attempts.

---

## 📝 PR Checklist (agentctl-specific)

> Complete the Gold Standard Checklist in `.github/pull_request_template.md`
> first, then these:

- [ ] Envelope on stdout, logs on stderr
- [ ] Large output → CAS wrapper (`data.summary` + `data.artifact`; optional
      `meta.cas_digest` matching it)
- [ ] `--dry-run` implemented for state-changing commands
- [ ] Wire contract unchanged, or spec + golden tests updated
- [ ] OpenAPI: dry-run, retries, pagination, auth, actionable errors

---

## 📖 Helpful References

- Go: Effective Go, Uber Go Style Guide
- Zerolog docs
- wazero (pure Go WASI runtime)
- modernc.org/sqlite (CGO-less SQLite)
- RFC 8785 (JSON Canonicalization Scheme)

---

**You are here to help us ship a safe, deterministic CLI.** Prefer small,
well‑tested changes; when in doubt, ask for a human decision.
