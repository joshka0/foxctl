# agentctl “hooks” for Codex (prompt emulation)

Codex does not support Claude/OpenCode-style hook events (PreToolUse/PostToolUse/Stop). Treat the rules below as “always-on hooks” you must self-apply.

## 1) Task guard (before edits)

When you are about to change files (edit/write/multi-edit):

- Ensure there is an explicit task for the work.
  - If none exists, create one (or ask for a title) before editing.
  - Keep the task description updated as scope changes.
- Prefer small, reviewable diffs.
- Do not commit or push unless explicitly requested.

## 2) Smart read (before reading code)

When you need to read a code file to answer a question or make a change:

- Prefer structure-first navigation.
  - Find symbols/entrypoints first.
  - Read only the relevant sections, not the entire file.

## 3) Smart grep + semantic search (when searching)

When you are searching for implementation details:

- For literal/identifier searches: use a fast text search (and expand to full function/class blocks when a match looks relevant).
- For conceptual searches (“how does X work”, “where is auth handled”, “what validates paths”): run semantic search and use the results to pick files/symbols to inspect.

## Skill packs (pick one)

Prefer these skill packs over the long list of individual skills:

- `agentctl-all`: single combined entrypoint (use when unsure)
- `agentctl-core`: files + fast search
- `agentctl-code`: code analysis + semantic search
- `agentctl-dev`: tests + CI + change verification
- `agentctl-orchestrate`: tasks + sessions + inbox
- `agentctl-integrations`: MCP + OpenAPI + provider sync
- `agentctl-mobile`: iOS + Android automation

## 4) Security scanner (before/after sensitive changes)

When you touch auth, crypto, path validation, network policy, serialization, or anything that could affect the wire contract:

- Run a targeted security scan of the changed area (or file) and address high/critical findings.
- Never log or paste secrets; redact values.

## 5) LSP diagnostics + test feedback (after edits)

After code changes:

- Run the quickest relevant check first (typecheck/lint for the touched language) and fix errors.
- If the repo has tests, run the narrowest tests that cover your change, then broaden as needed.
- If a test/lint command fails: acknowledge it and either fix now (if quick) or record a follow-up task with the error + likely cause.

## 6) Todo sync + stop guard (don’t stop early)

If you are in the middle of a multi-step task:

- Maintain an explicit checklist and keep exactly one item “in progress”.
- Do not conclude the session while tasks remain unfinished unless the user explicitly tells you to stop or defer.

## 7) Memory detector + memory prompt (capture learnings)

When a user says “remember”, “gotcha”, “decision”, “note that”, or you discover a reusable fix/pitfall:

- Save it as a short memory entry (1–2 sentences) with a clear name.

---

If any of the above rules conflict with a repo-local `AGENTS.md`, the repo-local instructions win for that repository.
