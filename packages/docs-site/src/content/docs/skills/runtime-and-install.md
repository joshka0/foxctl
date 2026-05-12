---
title: Skills runtime and install
description: Run, inspect, install, and verify foxctl skills — run paths, input modes, manifests, and authoring.
---

Skills are foxctl's task plugins. Each skill has a manifest (`skill.yaml`), a
binary or WASM entrypoint, and a well-defined I/O contract based on Protocol v1
JSON envelopes.

This page covers the three execution paths, all input modes, the manifest
contract, runtime invariants, and the authoring workflow.

## Run paths

Choose a run path based on what you need:

| Need | Command | Why |
|------|---------|-----|
| Job history, dedupe, async, trajectory capture | `foxctl run <skill> --input '<json>'` | Opens the jobs store, writes CAS and trajectory metadata |
| Sandbox-safe or hook-style execution without persistence | `foxctl run <skill> --ephemeral --input '<json>'` | Skips job persistence; use for hooks, smoke tests, or one-off retrieval when `~/.foxctl` is not writable |
| Direct parameter flags generated from `skill.yaml` | `foxctl skills run <skill> --param value` | Executes the skill directly with manifest-derived parameter flags; clearest when flag form is more readable than raw JSON |

### Job-tracked execution

The default path. `foxctl run` resolves the skill handle, opens the job store,
runs the skill, and persists trajectory metadata:

```bash
foxctl run code/semantic_search --input '{"query":"error handling","limit":10}'
```

Use this when you need job history, deduplication, async execution, or when
downstream tooling depends on job metadata.

### Ephemeral execution

Skips all job persistence. Useful for sandboxed agents, pre-commit hooks,
one-off retrieval, or environments where `~/.foxctl` is not writable:

```bash
foxctl run code/semantic_search --ephemeral --input '{"format":"tree"}'
```

### Direct parameter flags

`foxctl skills run` parses the skill manifest and exposes its parameters as CLI
flags. Use this when a simple flag form is clearer than raw JSON:

```bash
foxctl skills run code/semantic_search --query "session restore"
```

This is also useful when validating skill parameters or testing manifest
definitions.

### Which binary to use

If `foxctl` on PATH says `Command 'run' not available in bundled mode`, you
are using a wrapper from another install. Run the repo-native binary instead:

```bash
./bin/foxctl run code/semantic_search --input '{"query":"test"}'
```

Or rebuild with `make build`.

## Input modes

All run paths accept input through several modes:

| Mode | Syntax | Use when |
|------|--------|----------|
| Raw JSON | `--input '{"key":"value"}'` | Small inline JSON inputs |
| From file | `--input-file input.json` | Repeatable local inputs stored on disk |
| From stdin (raw) | `--input-file -` | Piped raw JSON from another command |
| From stdin (envelope) | `--input stdin` | Reads a Protocol v1 envelope from stdin and passes only its `data` field to the skill |
| From CAS | `--input sha256:<hex>` | Loads previously stored JSON from the content-addressable store |

Direct skill binaries read JSON on stdin through `skillmain.Main`. They do not
parse CLI flags or files themselves — the `foxctl run` and `foxctl skills run`
wrappers handle loading files, extracting envelope data, and merging parameter
flags into JSON.

## Manifest contract

Every skill is defined by a `skill.yaml` manifest. The manifest is the source
of truth for runtime resolution.

### Required fields

| Field | Notes |
|-------|-------|
| `apiVersion` | Must be non-empty |
| `kind` | Must be `Skill` |
| `metadata.name` | Must include namespace, e.g. `code/semantic_search` |
| `metadata.version` | Semantic version string |
| `distribution.type` | `exec` or `wasi` |
| `distribution.exec.entry` | Binary entry path (for `exec`) |
| `distribution.wasi.module` | WASM module path (for `wasi`) |
| `signature.command` | Command ID exposed by the skill |

### Optional fields

| Field | Purpose |
|-------|---------|
| `io` | Input/output schema metadata |
| `capabilities` | Runtime policy: `network`, `filesystem`, `pure` |
| `memory` | Memory limits and constraints |
| `openapi` | OpenAPI specification reference |

### Distribution semantics

| Distribution | Artifact resolution | Runtime rules |
|-------------|---------------------|---------------|
| `exec` | Prefer `bin-cgo` (when requested), then `bin`, then `exec.entry` | Network capability may be `none` or `egress` |
| `wasi` | `module.wasm` or manifest `wasi.module` | `capabilities.network` must be `none` |

## Runtime contract

Skills follow strict output and isolation rules:

- **Stdout is envelopes only.** Skill stdout must be Protocol v1 JSON
  envelopes (`version/status/command/data/meta`). No free-form text on stdout.
- **Logs go to stderr.** All diagnostic output goes to stderr, never stdout.
- **Large outputs go to CAS.** When output exceeds the inline threshold
  (approximately 64KB), persist to the content-addressable store and return
  `data.summary` plus `data.artifact` pointer instead of an oversized inline
  payload.
- **WASI network isolation.** WASI skills keep `network: "none"` — this is a
  Core v1 isolation guarantee with no exceptions.
- **Workspace/path policy checked.** All file access goes through
  `policy.PathValidator` before execution, preventing path escapes.
- **`meta.ts` required.** Every terminal envelope must include an RFC3339 UTC
  timestamp in `meta.ts`.

The repo-level agent guide uses a 64KB large-output threshold, while the
Protocol v1 spec describes a lower default inline threshold for `data.body`.
When in doubt, follow the stricter Protocol v1 artifactization rule.

## Install and inspect

List all installed skills:

```bash
foxctl skills list
```

Show detailed information about a specific skill, including its manifest,
parameters, and runtime configuration:

```bash
foxctl skills info <skill-name>
```

Install a skill from a source directory or archive:

```bash
foxctl skills install <source>
```

Build and install all skills from the repository:

```bash
make skills-install
```

Or build a single skill explicitly:

```bash
CGO_ENABLED=0 go build -o ~/.foxctl/skills/my/skill/bin ./skills/my_skill
```

## Runtime flow

When a skill is executed, the runtime follows this sequence:

1. **Resolve** the skill handle from installed `skill.yaml` manifests.
2. **Parse and validate** manifest fields.
3. **Resolve artifact** — prefer `bin-cgo` when requested, then `bin`, then
   the manifest `entry` path.
4. **Run** via the exec runner (for `exec` distribution) or the WASI runner
   (for `wasi` distribution).
5. **Parse stdout** as a Protocol v1 envelope.

## Authoring a skill

The short version for creating a new skill:

1. Create `skills/<name>/main.go` and `skills/<name>/skill.yaml`.
2. Keep stdout output as Protocol v1-compliant JSON envelopes.
3. Declare accurate `capabilities` (`network`, `filesystem`, `pure`).
4. Build and install with `make skills-install` or the explicit `go build`
   command above.
5. Verify with `foxctl skills list`, then test through both run paths:

```bash
foxctl run <command> --input '{"query":"test"}'
foxctl skills run <command> --param value
```

See the [add a skill](/guides/add-a-skill/) guide for the full authoring
workflow.

## Execution invariants

These invariants hold for all skill executions:

| Invariant | Why it matters |
|-----------|----------------|
| stdout must be protocol envelope JSON | Keeps CLI, hooks, and automation stable |
| `meta.ts` present on terminal envelopes | Required by envelope contract |
| WASI network remains disabled | Core v1 isolation guarantee |
| Workspace/path policy checked before execution | Prevents path escapes |
| Large outputs go to CAS | Avoids oversized inline payloads |

## Cross-references

- [Search and index](/retrieval/search-and-index/) — using skills for code retrieval
- [Repoindex and DAG grep](/retrieval/repoindex-and-dag-grep/) — graph query skills
- [Protocol v1](/reference/protocol-v1/) — envelope specification
- [Add a skill](/guides/add-a-skill/) — full authoring guide
