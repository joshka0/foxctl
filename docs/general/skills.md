# Skills

Machine-friendly reference for skill contracts and execution.

## Metadata

| Field | Value |
|------|-------|
| Status | Current |
| Canonical packages | `internal/domain/skill`, `internal/runtime/execution/*`, `internal/runtime/runservice` |
| Last reviewed | 2026-02-17 |

## Runtime Flow

1. Resolve a skill handle from installed `skill.yaml` manifests.
2. Parse and validate manifest fields.
3. Resolve artifact (`bin-cgo` when preferred, then `bin`, then manifest `entry`).
4. Run via exec or WASI runner.
5. Parse stdout as protocol envelope (`version/status/command/data/meta.ts`).

## Manifest Contract (`skill.yaml`)

Source of truth: `internal/domain/skill/manifest.go`.

| Field | Required | Notes |
|------|----------|-------|
| `apiVersion` | Yes | Must be non-empty |
| `kind` | Yes | Must be `Skill` |
| `metadata.name` | Yes | Must include namespace, e.g. `code/semantic_search` |
| `metadata.version` | Yes | Semantic version string |
| `distribution.type` | Yes | `exec` or `wasi` |
| `distribution.exec.entry` | For `exec` | Binary entry path |
| `distribution.wasi.module` | For `wasi` | WASM module path |
| `signature.command` | Yes | Command id exposed by the skill |
| `io`, `capabilities`, `memory`, `openapi` | Optional | Runtime policy and UX metadata |

## Distribution Semantics

| Distribution | Artifact resolution | Runtime rules |
|-------------|---------------------|---------------|
| `exec` | Prefer `bin-cgo` (when requested), then `bin`, then `exec.entry` | Network capability may be `none` or `egress` |
| `wasi` | `module.wasm` or manifest `wasi.module` | `capabilities.network` must be `none` |

## Execution Invariants

| Invariant | Why it matters |
|----------|----------------|
| stdout must be protocol envelope JSON | Keeps CLI, hooks, and automation stable |
| `meta.ts` present on terminal envelopes | Required by envelope contract |
| WASI network remains disabled | Core v1 isolation guarantee |
| Workspace/path policy checked before execution | Prevents path escapes |
| Large outputs go to CAS (`data.summary` + `data.artifact`) | Avoids oversized inline payloads |

## Authoring Checklist

1. Add `skills/<name>/main.go` and `skills/<name>/skill.yaml`.
2. Keep envelope output protocol-compliant.
3. Declare accurate `capabilities` (`network`, `filesystem`, `pure`).
4. Build/install with `make skills-install` (or explicit `go build .../bin`).
5. Verify with `foxctl skills list` and either `foxctl run <command> --input ...`
   for job-tracked installed execution or `foxctl skills run <command> ...` for
   direct manifest-derived parameter flags.

## Running Skills

| Need | Command |
|------|---------|
| Discover available agent guides and skill workflows | `foxctl skills` or `foxctl skills list` |
| Fit installed skills into a small context window | `foxctl skills --compact` |
| Find likely skills for a task with next-step commands | `foxctl skills search "<task>"` or `foxctl skills search "<task>" --compact` |
| Read the core foxctl onboarding guide | `foxctl skills get foxctl` |
| Read a specific skill guide | `foxctl skills get <skill>` |
| Print the local skills root or a skill directory | `foxctl skills path [skill]` |
| Check guide/manifest drift before review or CI | `foxctl skills doctor [skill] --strict` |
| Run the advisory global drift report | `make skills-doctor` |
| Gate only changed skills against strict drift rules | `make skills-doctor-changed BASE_REF=origin/main HEAD_REF=HEAD` |
| Job history, dedupe, async, or trajectory capture | `foxctl run <skill> --input '<json>'` |
| Sandbox-safe or hook-style execution without job persistence | `foxctl run <skill> --ephemeral --input '<json>'` |
| Direct parameter flags generated from `skill.yaml` | `foxctl skills run <skill> --param value` |
| Raw JSON from a file or pipeline | `foxctl run <skill> --input-file input.json` or `--input-file -` |
| Envelope chaining | `foxctl run <skill> --input stdin` |

Direct skill binaries read JSON on stdin through `skillmain.Main`; they do not
parse CLI flags or files themselves. The `foxctl run` and `foxctl skills run`
wrappers are responsible for loading files, extracting envelope data, and
merging parameter flags into JSON.

## Skill Drift Guardrails

Treat `skill.yaml` as the feature contract and `SKILL.md` as the agent-facing
workflow contract. When a skill gains parameters, changes command behavior, or
moves directories, update both in the same change.

Run this before review for a specific skill:

```bash
foxctl skills doctor <skill> --strict
```

The doctor checks manifest validity, guide presence, guide freshness relative to
`skill.yaml`, copy-pasteable command examples, and required parameter coverage.

CI enforces the same strict contract only for skills with files changed directly
under `skills/<dir>/...`:

```bash
make skills-doctor-changed BASE_REF=origin/main HEAD_REF=HEAD
```

The global report remains advisory while existing installed skills are brought
up to the strict guide contract:

```bash
make skills-doctor
```

## Related Docs

- [docs/general/search.md](search.md)
- [docs/general/storage.md](storage.md)
- [docs/general/gotchas.md](gotchas.md)
- [docs/spec/protocol_v1.md](../spec/protocol_v1.md)
