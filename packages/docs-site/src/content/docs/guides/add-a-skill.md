---
title: Add a skill
description: How to add, install, verify, and document a foxctl skill.
---

Status: Current guide.

Skills are foxctl's task plugins. A good skill has a small manifest, a narrow
runtime boundary, protocol-compliant output, and tests that prove the behavior
users rely on.

## When to add a skill

Add a skill when the operation is:

- useful as a repeatable command
- safe to expose through `foxctl run` or `foxctl skills run`
- small enough to keep its own input and output contract clear
- valuable to agents, hooks, CI, or humans outside one local workflow

Do not add a skill only to hide ordinary Go helpers. Keep domain logic in the
right package family, then expose it through a skill if the command surface is
worth supporting.

## Files to add

A typical exec skill has:

```text
skills/<skill_dir>/
  main.go
  main_test.go
  skill.yaml
```

The manifest is the user-facing contract. The implementation should keep IO in
the shell and push deterministic logic into ordinary functions that can be
tested without running the CLI.

## Manifest checklist

`skill.yaml` must declare:

| Field | Purpose |
|---|---|
| `apiVersion` and `kind` | Identifies the manifest as a foxctl skill |
| `metadata.name` | Stable command name, for example `fs/read` |
| `metadata.version` | Skill contract version |
| `distribution.type` | `exec` or `wasi` |
| `signature.command` | Command emitted in envelopes |
| `signature.parameters` | Direct flags for `foxctl skills run` |
| `capabilities` | Runtime policy, especially filesystem and network |

Use `capabilities.network: "none"` unless the skill genuinely requires egress.
WASI skills must keep network disabled.

## Minimal exec shape

Most Go skills use `skillmain.Main`:

```go
func main() {
    skillmain.Main("fs/read", run)
}
```

The run function receives parsed JSON input and a `RunContext`. It should:

1. Validate input and paths at the boundary.
2. Perform the narrow side effect.
3. Store large or blob-like output in CAS.
4. Emit a Protocol v1 envelope through shared helpers.

## Output contract

Skill stdout is reserved for JSON envelopes. Logs belong on stderr. Large
outputs should return a compact `data.summary` plus a CAS artifact pointer
instead of inline blobs.

The repo currently has a historical 64KB rule in some agent docs and a stricter
Protocol v1 inline rule for `data.body`. For new skills, prefer the stricter
Protocol v1 behavior and explain any exception in review notes.

## Build and install

Build the repo and install skills through the repo's normal path:

```bash
make build
```

```bash
make skills-install
```

Then verify discovery:

```bash
foxctl skills list
```

```bash
foxctl skills info <skill-name>
```

## Run paths

Use job-tracked execution when history, dedupe, or trajectory capture matters:

```bash
foxctl run <skill-name> --input '{"key":"value"}'
```

Use ephemeral execution for one-off retrieval, hooks, or sandboxed agents:

```bash
foxctl run <skill-name> --ephemeral --input '{"key":"value"}'
```

Use direct flags when validating the manifest's parameter schema:

```bash
foxctl skills run <skill-name> --param value
```

## Tests

Add focused tests at the smallest useful layer:

| Test | What it should prove |
|---|---|
| Pure function tests | Deterministic planning, parsing, ranking, or formatting |
| Skill run tests | Input validation, envelope shape, CAS behavior, and errors |
| Golden tests | Stable JSON output when the output is a contract |
| Integration smoke | CLI discovery and one representative command |

Keep generated timestamps, UUIDs, and ordering deterministic. Inject clocks and
IDs where the code would otherwise make output unstable.

## Documentation

Update the user-facing docs when the skill adds a new command or changes a
contract:

- add the skill to the relevant guide or command map
- link the canonical source docs
- mark planned or experimental behavior clearly
- run the docs link checker

```bash
make check-doc-links
```

## Review gate

Before opening a PR, check:

- The skill keeps stdout envelope-only.
- WASI skills use `network: "none"`.
- Path handling uses the shared path validation boundary.
- Large output goes to CAS.
- Tests cover the behavior users will depend on.
- The command is listed in the docs when it is public.

## Canonical sources

- [docs/general/skills.md](https://github.com/joshka0/foxctl/blob/main/docs/general/skills.md)
- [docs/spec/skills_spec/README.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/skills_spec/README.md)
- [docs/spec/v1/protocol_v1.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/v1/protocol_v1.md)
- [skills/fs_read/skill.yaml](https://github.com/joshka0/foxctl/blob/main/skills/fs_read/skill.yaml)

