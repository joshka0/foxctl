# AGENTS.md — AI Assistant Guide for agentctl

**Last Updated:** November 8, 2025  
**Target Audience:** AI coding assistants (Claude, Cursor, GitHub Copilot, etc.) and human contributors

---

## 🤖 Hello, AI Assistant!

This file is for you. It encodes our source‑of‑truth conventions so you can safely help us build **agentctl Core Profile v1**:

- Canonical **JSON envelope** I/O (no binary inline)
- **CAS** for large results with mandatory summaries
- **Jobs** with durable state + **Memory** (auto‑cache + named)
- **WASI‑first** sandbox + **exec** runner when required
- **Generic OpenAPI skill** (`http/openapi`) + **auth/pagination plugins**
- **Go‑first** implementation (no Node/TS)

When in doubt, prefer the sources in **📚 Canonical Sources** and follow the **Do/Ask/Act** guardrails.

---

## 🌿 Branching & PR Norm (critical)

1. **Create branch:** `codex/<feature-name>` (e.g., `codex/openapi-dry-run`)
2. **Open PR** into `main` (never push to `main`)
3. **CI must pass** (lint, vet, unit tests, race)  
4. **Approval required** (at least one human)  
5. **Release** handled via our release workflow (you do **not** publish)

**Why:** keeps AI changes reviewable, secret‑free, and reproducible.

### Mega-PR Workflow (spec batches)

When multiple spec PRs need to land together, follow this script (or run the `/mega-pr` alias once available):

1. **Rebase each source branch**: `git fetch origin <branch>` → `git switch <branch>` → `git rebase origin/main` → run `make fmt`, `make lint`, `CGO_ENABLED=0 go test ./...`, then `git push -f`.
2. **Create aggregation branch**: `git checkout main && git pull --ff-only`, then `git checkout -b codex/<mega-name>`.
3. **Merge or cherry-pick branches sequentially** onto the mega branch, resolving conflicts as you go.
4. **Validate once** on the mega branch: `make fmt`, `make lint`, `CGO_ENABLED=0 go test ./...`.
5. **Push & open** mega PR with `gh pr create --base main --head codex/<mega-name>` (mention the superseded PR numbers).
6. **Close superseded PRs** with a comment like “Superseded by mega PR #XX” and delete their remote branches.

This keeps CodeRabbit focused on a single diff and avoids repeated conflict resolution across overlapping spec branches.

---

## 📋 Quick Reference Card

```yaml
Project: agentctl (single-binary CLI)
Language: Go (>= 1.22), modules on, CGO off by default
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
````

---

## 🛠 Agentctl Usage Guide

1. **Build the CLI**
   ```bash
   CGO_ENABLED=0 make build   # emits ./bin/agentctl
   ```

2. **Compile bundled skills**
   ```bash
   make skills-build   # populates dist/skills/
   ```

3. **Install needed skills** (example: `todo/manage`)
   ```bash
   ./bin/agentctl skills install \
     --manifest dist/skills/todo/skill.yaml \
     --binary   dist/skills/todo/bin
   ```

4. **List & inspect**
   ```bash
   ./bin/agentctl skills list
   ./bin/agentctl skills describe todo/manage
   ```

5. **Run helpers or generic skills**
   ```bash
   ./bin/agentctl todo add --title "Integrate prcomments skill" \
     --description "Package prcomments CLI as exec ci/prcomments skill"

   ./bin/agentctl run todo/manage --input '{"operation":"list"}'
   ```

6. **Check jobs & storage**
   - Job artifacts: `~/.agentctl/jobs/<ulid>/` (progress/result/stderr).
   - CLI helpers: `./bin/agentctl jobs list|result`, `./bin/agentctl memory stats` (shows auto-cache + named memory health).

7. **Config & secrets**
   - Config: `~/.agentctl/config.yaml` (override via `--config`).
   - Secrets/tokens: export env vars before running (`GITHUB_TOKEN`, etc.) or mount under `/run/secrets/<name>` for WASI skills.

Following these steps keeps every run deterministic and fully observable.

---

## 📚 Canonical Sources (Single Source of Truth)

| Topic                              | Source                                                                 |
| ---------------------------------- | ---------------------------------------------------------------------- |
| Envelope fields & rules            | [`docs/spec/core_profile_v1.md`](docs/spec/core_profile_v1.md) §2      |
| Large result CAS rules + summaries | [`docs/spec/core_profile_v1.md`](docs/spec/core_profile_v1.md) §4      |
| Cache key formula (RFC 8785)       | [`docs/spec/core_profile_v1.md`](docs/spec/core_profile_v1.md) §8.1    |
| Job lifecycle & DB schema          | [`docs/spec/core_profile_v1.md`](docs/spec/core_profile_v1.md) §7      |
| WASI vs exec runner rules          | [`docs/spec/core_profile_v1.md`](docs/spec/core_profile_v1.md) §10     |
| OpenAPI generic skill              | [`docs/spec/openapi_skill.md`](docs/spec/openapi_skill.md)             |
| Plugin protocol (auth/pagination)  | [`docs/spec/plugin_protocol.md`](docs/spec/plugin_protocol.md)         |
| Error codes table                  | [`docs/spec/core_profile_v1.md`](docs/spec/core_profile_v1.md) §13     |

> If documents disagree, prefer the spec sections above. If still ambiguous, **ASK for human approval** in the PR.

---

## 🏗️ Repository Layout (expected)

```
agentctl/
├── cmd/agentctl/                  # main CLI
│   └── main.go
├── internal/
│   ├── envelope/                  # envelope types, schema validation, JSON c14n
│   ├── cas/                       # CAS (put/head/get, integrity check)
│   ├── jobs/                      # job queue, SQLite persistence, NDJSON progress
│   ├── memory/                    # auto-cache + named memory
│   ├── cache/                     # memoization, RFC 8785, file/dir digests
│   ├── runner/
│   │   ├── wasi/                  # WASI runner (wazero)
│   │   └── exec/                  # exec runner, resource limits
│   ├── skill/                     # skill discovery/manifest validation
│   └── openapi/                   # http/openapi skill (core), built-in auth/paging
│       ├── builtin_auth/          # bearer, apiKey, basic, oauth2-cc
│       ├── builtin_paging/        # link, cursor, offset
│       └── plugin/                # plugin SPI (stdin/stdout envelopes)
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
 │    ├─ YES → Use CAS wrapper (summary + artifact), set meta.cas_digest
 │    └─ NO  → continue
 ├─ Is this OpenAPI logic?
 │    ├─ Add/modify in internal/openapi/ (built-ins) or plugins/
 │    ├─ Include dry-run, retries, pagination, auth mapping
 │    └─ Never log secrets; redact "***"
 └─ Always:
      - Envelopes to stdout, logs to stderr
      - Include context.Context, handle cancellation
      - Add unit tests + golden outputs
      - Run make check (lint, vet, staticcheck, test -race)
```

---

## 🔧 Critical Technologies & Rules

### 1) Envelopes (non-negotiable)

* `version: 1`, `status: "ok"|"error"`, `command`, `data`, `meta`, `error`.
* **On large results:** `data.summary` (≤2 KiB) + `data.artifact` digest; set `meta.cas_digest`.
* **Errors:** prefer actionable `error.code` (+ `data.hint`).
* **Stdout:** envelopes only. **Stderr:** logs only.

### 2) CAS

* **Put** returns digest (`sha256:<hex>`).
* **Get** must **verify integrity** by recomputing digest (fail `EIO` on mismatch).
* Store under `~/.agentctl/cas/sha256/<digest>`.

### 3) Jobs & Memory

* States: `queued|running|ok|error|canceled` (terminal).
* Persist job artifacts/outputs under `~/.agentctl/jobs/<ulid>/`.
* Auto‑cache hits must set `meta.source:"cache"` and include `meta.cache_key`.

### 4) OpenAPI Skill (`http/openapi`)

* Input: `spec` (path/CAS/memory), `operationId`, `params` (path/query/header/body), `paging`, `retry`, `auth`, `dry_run`.
* **Built-in auth:** bearer, apiKey, basic, oauth2 client‑credentials.
* **Built-in pagination:** link headers, cursor field, offset/limit.
* **Plugins** for auth/pagination: subprocess over JSON envelopes.
* **Dry-run:** emit `request_plan` (URL, redacted headers), no network call.
* **4xx validation errors:** **do not** artifactize; include error object inline (within `inline_output_kb`).
* Include `status_code` and key headers in summaries (e.g., `etag`, `ratelimit-remaining`).

### 5) Networking & Secrets

* **Core v1 rule:** WASI → `network:"none"` (validate at install).
* Exec runner may allow `network:"egress"` with optional `egressAllow`.
* Secrets mounted at `/run/secrets/<name>`. Never log secret values; redact `"***"`.

### 6) Filesystem Skills

* All `fs/*` and `text/grep` skills must route user paths through `policy.PathValidator`.
* The validator anchors to the current workspace and only allowlists explicit roots (`cfg.Home`, `os.TempDir()`).
* Reject traversal attempts early so we fail with actionable `EPOLICY` guidance instead of touching the host filesystem.
* The executor exports the workspace path via `AGENTCTL_WORKSPACE`; skills should prefer that over `os.Getwd()` when sandboxed.

---

## 🧭 Do / Ask / Act (Guardrails)

**Do**

* Write Go code with tests, docs, and golden outputs.
* Add flags, help text, and update Cobra commands.
* Improve built‑in OpenAPI auth/pagination heuristics.
* Refactor internals without breaking the wire contract.

**Ask** (require human approval in PR)

* Breaking envelope changes (fields, semantics)
* Network policy changes (default egress, wildcards)
* New on‑disk layouts, path migrations, or schema changes
* Adding new built‑in auth schemes beyond the four above

**Act**

* Only after CI is green and a human **approves**.

### ❌ Hard Fail Conditions (LLM must stop)
- Proposing any change that alters the JSON envelope wire contract (fields/semantics) without spec + golden updates.
- Enabling network access for WASI skills (Core v1 mandates `network:"none"`).
- Storing secrets in code, testdata, CAS, or Git history.
- Writing outside the workspace or auto-modifying files when the user has not opted in with `--apply`.
- Adding dependencies that require CGO or platform-specific toolchains.
- Emitting non-JSON stdout from CLI/skills (envelopes-only forever).

### Spec ↔ Code Drift Watchlist
| Topic | Spec (canonical) | Code (actual) | Action |
|-------|------------------|---------------|--------|
| inline_output_kb | `docs/spec/core_profile_v1.md` §2 → 32 KB default | `internal/config.DefaultInlineOutputKB = 32` (tested in `internal/config/config_test.go`) | Keep spec + constant in lockstep |
| WASI network rule | `docs/spec/core_profile_v1.md` §10 → WASI = `network:"none"` | Manifest validation + `scripts/checkmanifests` enforce the restriction | Add regression tests whenever new WASI skill ships |
| Envelope meta fields | `docs/spec/core_profile_v1.md` §2 enumerates `meta.*` | `internal/envelope.Envelope` + CLI smoke tests (`cmd/agentctl/cmd/run_test.go`) assert presence | Extend smoke tests when adding new meta fields |

---

## 🧑‍💻 Go Coding Conventions

* **Style:** gofmt/gofumpt, idiomatic Go, small functions.
* **Errors:** wrap with `%w`, sentinel errors in package, `errors.Is/As`.
* **Context:** first param `ctx context.Context`; honor cancel/timeouts.
* **Logging:** zerolog; fields not strings should be typed.
* **Testing:** table‑driven tests; `-race`; golden files in `testdata/`.
* **IO limits:** stream whenever possible; avoid reading full bodies into RAM if artifactizing.
* **Nil vs empty:** return empty slices/maps, not nil, in JSON (`omitempty` as appropriate).
* **Panics:** never in library code; return errors.

**Example: envelope OK helper (Go)**

```go
func Ok(cmd string, data any, meta Meta) Envelope {
    now := time.Now().UTC().Format(time.RFC3339)
    meta.TS = now
    return Envelope{
        Version: 1,
        Status:  "ok",
        Command: cmd,
        Data:    data,
        Meta:    meta,
        Error:   Error{Code: nil, Message: nil},
    }
}
```

---

## 🧪 Testing Requirements

### Coverage Targets

```json
{
  "lines": 85,
  "functions": 80,
  "branches": 75,
  "statements": 85
}
```

### Must‑have suites

* **Envelope**: valid/invalid cases; large → CAS wrapper; error envelopes with hints.
* **CAS**: integrity check failures; concurrent `put/get`; tags and pinning.
* **Jobs**: lifecycle transitions, crash recovery, `--dedupe`.
* **OpenAPI**: dry‑run, bearer/apiKey/basic/oauth2‑cc, link/cursor/offset pagination, retries on 429/5xx, non‑UTF‑8 bodies rejected (`EPARSE`).
* **Plugins**: auth and pagination example plugins invoked via subprocess (WASI and exec).

**Live integration tests** (opt-in): set `AGENTCTL_TEST_LIVE=1`; otherwise use go-vcr cassettes. Never rely on network in default CI.

### Golden Test Pact
- Every skill ships `testdata/ok.json` (or `.wrapper.json` when CAS is required) for the happy-path envelope.
- Every skill ships `testdata/error.json` capturing actionable `error.code` plus `data.hint`.
- Freeze timestamps, redact host-specific data, and prefer shared helpers so goldens stay deterministic.

---

## 🔌 Plugin Skeletons (Go)

**Auth plugin** (`plugin/auth`, stdin/stdout envelopes):

```go
// cmd/auth-plugin/main.go
package main

func main() {
    env, err := ReadEnvelope(os.Stdin) // plugin/auth request
    if err != nil { writeErr("EENVELOPE", err); return }

    var req AuthRequest
    _ = json.Unmarshal(mustJSON(env.Data), &req)

    headers := req.Headers
    token := os.Getenv("PLUGIN_BEARER_TOKEN")
    if token != "" {
        headers["Authorization"] = "Bearer " + token
    }

    out := Envelope{
        Version: 1, Status: "ok", Command: "plugin/auth",
        Data: map[string]any{"headers": headers},
        Meta: Meta{TS: time.Now().UTC().Format(time.RFC3339), Source: "run"},
        Error: Error{},
    }
    _ = json.NewEncoder(os.Stdout).Encode(out)
}
```

**Pagination plugin** (`plugin/pagination`):

```go
// cmd/paging-plugin/main.go
package main

func main() {
    env, err := ReadEnvelope(os.Stdin)
    if err != nil { writeErr("EENVELOPE", err); return }

    var in PageInput
    _ = json.Unmarshal(mustJSON(env.Data), &in)

    // Example: read "next" field
    var body map[string]any
    _ = json.Unmarshal(in.LastResponse.Body, &body)
    if nxt, ok := body["next"].(string); ok && nxt != "" {
        out := okEnv("plugin/pagination", map[string]any{
            "continue": true, "next_cursor": nxt,
        })
        _ = json.NewEncoder(os.Stdout).Encode(out); return
    }

    _ = json.NewEncoder(os.Stdout).Encode(okEnv("plugin/pagination", map[string]any{
        "continue": false,
    }))
}
```

> Place compiled plugins under `~/.agentctl/plugins` or point to them with `AGENTCTL_OPENAPI_PLUGIN_PATH`.

---

## 🛠️ Make Targets & Quick Commands

```bash
# Dev ergonomics
make fmt                # gofumpt
make lint               # golangci-lint + staticcheck + govet
make test               # unit tests (no network)
make test-race          # unit tests with -race
make test-live          # integration tests (AGENTCTL_TEST_LIVE=1)
make build              # build cmd/agentctl
make snapshot           # goreleaser --snapshot
make check-coverage     # enforce ≥85% coverage locally

# OpenAPI convenience
agentctl openapi import github.yaml --as=github
agentctl run http/openapi \
  --spec=memory:github \
  --operationId=listReposForUser \
  --params='{"path":{"username":"octocat"},"query":{"per_page":100}}' \
  --dry_run

# CAS maintenance
agentctl cas gc --older-than=168h --dry-run   # preview safe cleanup

# All commands output JSON envelopes to stdout.
```

---

## 🧷 Error Codes (remember)

`EARG, ENOTFOUND, ETIMEOUT, ERUNTIME, ERUNTIME_RESTART, EENVELOPE, EPARSE, EOUTPUT_TOO_LARGE, EPOLICY, EIO, EAUTH, EPAGINATION, EOPENAPI, ERATELIMIT, ECANCELED`

**Tip:** include `data.hint` where possible to teach the user how to fix the problem.

---

## 🔐 Security & Privacy

* **Never** log secrets or PII. Redact with `"***"`.
* Secrets live in `/run/secrets/<name>`; use 0600 perms for on‑disk files.
* WASI runner must enforce `network:"none"` (Core v1).
* Loopback & UNIX sockets denied unless allow‑listed in exec runner.
* CAS is integrity‑checked on every `get`.

---

## 🧭 Common Pitfalls

* Returning plain text to stdout (violates “envelopes everywhere”).
* Artifactizing 4xx validation errors (keep inline, bounded by `inline_output_kb`).
* Using wall‑clock for duration; use monotonic clock for `duration_ms`.
* Reading huge responses into RAM when they should be streamed → CAS.
* Forgetting to set `meta.source:"cache"` on cache hits.
* Attempting to read outside the workspace—`policy.PathValidator` now blocks traversal attempts.

---

## 📝 PR Checklist (copy into PR description)

* [ ] Follows branch rule `codex/<feature-name>`
* [ ] No wire contract break, or spec updated accordingly
* [ ] Envelopes on stdout, logs on stderr (checked)
* [ ] Large output → CAS wrapper (summary + artifact + `meta.cas_digest`)
* [ ] Tests: unit + golden; `make test-race` green
* [ ] Lint/vet/staticcheck pass; gofumpt applied
* [ ] Docs updated (`docs/spec/*.md`, CLI help)
* [ ] For OpenAPI: dry-run, retries, pagination, auth, actionable errors

---

## 📖 Helpful References

* Go: Effective Go, Uber Go Style Guide
* Zerolog docs
* wazero (pure Go WASI runtime)
* modernc.org/sqlite (CGO-less SQLite)
* RFC 8785 (JSON Canonicalization Scheme)

---

**You are here to help us ship a safe, deterministic CLI.**
Prefer small, well‑tested changes; when in doubt, ask for a human decision.
