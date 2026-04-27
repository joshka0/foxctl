---
name: hard-cut
description: "Deliberately replace a legacy contract with one canonical shape and no compatibility layer for names, fields, routes, or product language."
---

## What I do

- Drive a clean cut from an old outward contract to one canonical new shape.
- Remove compatibility behavior instead of preserving or policing the old path.
- Keep internal stable-core names only when they are not user-facing and do not force dual-shape runtime behavior.

## When to use me

- The user explicitly asks for a hard cut, breaking rename, canonicalization, or legacy cleanup.
- You are changing product nouns, routes, APIs, events, config shapes, prompts, or workflow names and backward compatibility is not desired.
- The codebase is carrying transitional logic that should be deleted rather than extended.

## Hard-Cut Rules

Apply these rules in order:

1. Do not add fallback behavior.
2. Do not add compatibility branches.
3. Do not add shims, adapters, coercions, aliases, or dual-shape support.
4. Do not add fail-fast guards whose purpose is to detect or reject old shapes.
5. Do not add tests whose purpose is to assert rejection of old or legacy shapes.
6. Prefer deleting old-shape handling over preserving or policing it.
7. Update producers, consumers, fixtures, tests, docs, and examples to use only the canonical shape.
8. Remove dead code, dead conditionals, obsolete comments, and migration helpers related to old shapes.
9. Keep validation only for the current canonical contract. Validation may reject malformed current-shape input, but must not branch on legacy discriminators, old field names, aliases, old enum members, or draft formats.
10. When choosing between backward compatibility and simplification, choose simplification unless the user explicitly asks for compatibility.

## Working Method

- Decide the canonical outward contract first.
- Change edge surfaces hard: UI, CLI, API, config, docs, prompts, fixtures, and tests.
- Keep internal names only if they stay inert and invisible to users and other live callers.
- If stored data must change, prefer one explicit migration over ongoing runtime fallback.
- Delete transitional TODOs and comments that imply dual support will remain.

## Avoid

- "Temporary" support for both old and new names.
- Runtime mapping from old shape to new shape "just in case."
- Legacy-discriminator branches, alias fields, or compatibility enums.
- Tests that lock the old contract in place after the cut.

## Final Check

- Search for old names and shapes after the change and remove remaining outward references.
- Confirm there is no branching on legacy inputs or names.
- Confirm fixtures, docs, examples, and tests use only the canonical shape.
- If the cut is intentionally breaking for callers, state that plainly in the summary or changelog.
