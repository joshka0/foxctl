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
5. Verify with `foxctl skills list` and `foxctl run <command> --input ...`.

## Related Docs

- [docs/general/search.md](search.md)
- [docs/general/storage.md](storage.md)
- [docs/general/gotchas.md](gotchas.md)
- [docs/spec/protocol_v1.md](../spec/protocol_v1.md)
