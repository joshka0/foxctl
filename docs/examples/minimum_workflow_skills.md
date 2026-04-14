# Minimum Workflow Skills for foxctl

The `AGENTS.md` guardrails describe foxctl as a deterministic, Go-first replacement for ad-hoc bash and MCP tooling. To keep an agent's workflow predictable, we curate a minimal set of skills that cover discovery, inspection, search, execution, and remote integration. Each skill speaks canonical JSON envelopes, stores large outputs in CAS, and can be chained through `foxctl run` or `foxctl skills run` invocations.

## Skill Inventory

| Phase        | Skill      | Purpose                                                                                                     | Notes                                                                                                    |
|--------------|------------|-------------------------------------------------------------------------------------------------------------|----------------------------------------------------------------------------------------------------------|
| Discover     | `fs/ls`    | Enumerate files/directories with filtering, sizes, CAS snapshots for large listings.                        | Primary entry point when grounding in a workspace; respects `.git`/`node_modules` exclusions.            |
| Inspect      | `fs/read`  | Read and preview file contents, stream the full object into CAS, and surface metadata for downstream steps. | New deterministic replacement for `cat`/`sed` that enforces UTF-8 and flags truncation.                  |
| Search       | `text/grep`| Scan multiple files for regex hits, return match previews, and spill exhaustive results into CAS.          | Drives "find the context" conversations without handing control to an unrestricted shell.               |
| Execute/Dry Run | `wasi/echo` (example executor) | Demonstrates WASI-first runners and serves as the template for future deterministic shells.            | Keeps networking disabled per Core Profile v1; use it as a harness for future WASI skills.               |
| Integrate    | `http/openapi` | Call external APIs (with dry-run planners, pagination, auth, CAS artifacts for large bodies).           | Honors spec-driven inputs and redacts sensitive data, replacing bespoke curl/bash sequences.             |
| Plan & Track | `todo/manage`  | Capture tasks, add children/dependencies, and record completion notes/gotchas without rogue formatting.  | Enforces no-backtick policy so task descriptions stay envelope-safe.                                    |

These skills provide the minimum coverage needed for an agent session: list the workspace, inspect files safely, search for relevant snippets, exercise deterministic command runners, and integrate with HTTP APIs.

## Suggested Workflow

1. **Anchor the workspace** with `fs/ls` to understand project layout and pick candidate files.
2. **Inspect critical files** via `fs/read` (now exposed as `foxctl fs read <path>`) to capture previews and CAS digests that downstream steps can reference without cracking open a separate editor.
3. **Search broadly** using `text/grep` to locate TODOs, feature flags, or regression hints.
4. **Dry-run or execute scripted tasks** inside a WASI sandbox (today via `wasi/echo`, soon via richer WASI skills).
5. **Talk to remote systems** through `http/openapi`, which doubles as our auth/pagination extensibility showcase.
6. **Track progress** with `todo/manage`—run `foxctl todo add --title "..."` to capture tasks (with nesting/dependencies) and `foxctl todo complete --id ... --notes ...` when you finish, ensuring every completion records notes/gotchas for future runs.

All skills can be chained by piping their JSON envelopes through standard tools (see `docs/examples/skills_chain.md` for concrete bash pipelines). The new `fs/read` skill slots between `fs/ls` and `text/grep`, giving the CLI a safe, repeatable file-reader primitive without falling back to arbitrary bash commands.
