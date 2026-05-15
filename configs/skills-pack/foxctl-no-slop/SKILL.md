---
name: foxctl-no-slop
description: "Rework a change from the intended final UX and architecture, using foxctl caller-proof tools to delete compatibility cruft, dead paths, and accidental complexity."
user-invocable: true
---

# Foxctl No Slop

Rework the change from the intended end state, not from the historical path that
produced the current patch. This skill is stricter than cleanup: any legacy mode,
prop, wrapper, route alias, fallback, or duplicated rule needs live caller proof
before it survives.

## When to Use

- A patch contains compatibility branches, aliases, fallbacks, or mode flags.
- The desired UX or architecture is clear enough to shape the code around it.
- You need foxctl evidence before deleting or preserving old surfaces.
- You are doing a hard cut but need caller proof first.

## Workflow

1. State the intended end state in one or two sentences.
2. List the compatibility suspects: old names, modes, props, wrappers, route
   aliases, fallback branches, duplicated rules, stale fixtures, and migration
   helpers.
3. Prove callers with foxctl before preserving anything:
   - Exact use-sites: `code/context_grep` with literal patterns.
   - Graph evidence: `repo/index_search`, `repo/index_expand`, and
     `code/dag_grep`.
   - Conceptual reach: `code/semantic_search` when names changed or callers are
     not obvious by text.
4. Decide for each suspect:
   - Delete when foxctl finds no live first-party caller and there is no current
     public contract, persisted-data migration need, or user-visible route
     obligation.
   - Keep only with a named caller or contract and a reason it still belongs in
     the intended final shape.
5. Reshape around the final product surface. Prefer one clear component, command,
   or flow over mode flags. Split only around real boundaries such as state,
   layout, controls, permissions, routing, or domain commands.
6. Move shared rules to one place. Feature flags, permissions, route gating, URL
   state, command names, and policy checks should not be duplicated across pages
   or hidden in view components.
7. Verify the intended flow and the deletion assumptions with targeted tests,
   relevant manual checks, and final foxctl searches for removed names.

For the command cookbook and evidence table format, use
`configs/skills-pack/foxctl-no-slop/references/caller-proof.md`.

## Caller-Proof Standard

Treat tests, fixtures, comments, and docs as evidence, not automatically as live
callers. A test that only preserves the old shape should usually be deleted or
rewritten to the intended behavior.

A compatibility path may remain only when at least one of these is true:

- Current first-party code calls it.
- A documented public API, CLI, config, route, or persisted data shape still
  requires it.
- A one-time migration is required, and ongoing runtime fallback is removed after
  the migration boundary.

## Output

End with:

- `End state`: the final UX or architecture in one or two sentences.
- `Deleted`: removed compatibility paths and the foxctl evidence that made the
  deletion safe.
- `Kept`: compatibility paths that remain, each with a named live caller or
  contract.
- `Consolidated rules`: shared rules moved to a single owner.
- `Verification`: commands/tests/manual checks run and results.
- `Residual risk`: remaining uncertainty, if any.

## Rules

- Optimize for the code that should exist, not the smallest diff from the old
  shape.
- Delete dead compatibility paths instead of making them better.
- Do not invent a generic framework for one feature.
- Keep the refactor scoped to what makes the final shape coherent.
- Prefer names that describe product intent over implementation history.
- Do not route decisions through ad hoc keyword heuristics; use explicit fields,
  typed signals, graph evidence, or tests.
