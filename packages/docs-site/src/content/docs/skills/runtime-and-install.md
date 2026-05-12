---
title: Skills runtime and install
description: Run, inspect, install, and verify foxctl skills.
---

Status: Current shell, canonical runtime details linked.

Skills are foxctl's task plugins. The production path is to run installed
skills through `foxctl run` when job history or envelopes matter, and through
`foxctl skills run` when direct parameter flags are clearer.

## Run a skill

Job-tracked execution:

```bash
foxctl run code/semantic_search --input '{"query":"error handling","limit":10}'
```

Ephemeral execution for one-off retrieval or sandboxed hooks:

```bash
foxctl run code/semantic_search --ephemeral --input '{"format":"tree"}'
```

Direct skill execution:

```bash
foxctl skills run code/semantic_search --query "session restore"
```

## Input modes

| Mode | Use when |
|---|---|
| `--input '{"key":"value"}'` | Small raw JSON inputs |
| `--input-file input.json` | Repeatable local inputs |
| `--input-file -` | Piped raw JSON |
| `--input stdin` | Envelope input where only `data` should be passed |
| `--input sha256:<hex>` | Loading JSON from CAS |

## Runtime contract

- Skill stdout is reserved for JSON envelopes.
- Logs go to stderr.
- Large outputs should go to CAS and return a summary plus artifact pointer.
- WASI skills keep `network: "none"` unless the relevant spec changes.
- Generated artifacts should be rebuildable or explicitly pinned.

The repo-level agent guide currently uses a 64KB large-output threshold, while
the Protocol v1 spec describes a lower default inline threshold for `data.body`.
When in doubt, follow the stricter Protocol v1 artifactization rule and link to
the exact spec in review notes.

## Install and inspect

```bash
foxctl skills list
```

```bash
foxctl skills info <skill-name>
```

```bash
foxctl skills install <source>
```

## Add a skill

Use the dedicated [add a skill](/guides/add-a-skill/) guide when creating a
new public command. The short version is:

1. Add `skills/<skill_dir>/skill.yaml`.
2. Implement the narrow runtime entrypoint.
3. Keep stdout as Protocol v1 envelopes.
4. Store large output in CAS.
5. Add tests and docs.
6. Verify through `foxctl run` and `foxctl skills run`.

## Canonical sources

- [docs/general/skills.md](https://github.com/joshka0/foxctl/blob/main/docs/general/skills.md)
- [docs/spec/skills_spec/README.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/skills_spec/README.md)
- [docs/spec/protocol_v1.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/protocol_v1.md)
- [AGENTS.md](https://github.com/joshka0/foxctl/blob/main/AGENTS.md)
