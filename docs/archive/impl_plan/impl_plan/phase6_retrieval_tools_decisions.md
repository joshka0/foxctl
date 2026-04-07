# Phase 6 Retrieval & Edit Tools – Final Decisions

## 1. Context

Phase 6 introduces a retrieval funnel for code work:

- `semantic_file_index` (if present) → broad semantic file candidates
- Symbol index via `code.symbol_search`
- Snippet extraction via `code.swe_grep`
- Edits via `edit.*` tools (including structured diffs)

This note records the final decisions for:

- Edit tool surface (`edit.apply_patch` vs structured diffs)
- Role/tool matrix for agents (Coder, Planner, Reviewer/others)
- Knowledge/skills covering the retrieval funnel

No wire-contract, envelope, or runner changes were made for Phase 6.

---

## 2. Edit Tools: `edit.apply_patch` vs `edit.apply_structured_diff`

### Options Considered (from Phase 6 spec)

1. **Migrate** `edit.apply_patch` to accept structured `diff_json` (code/diff
   output).
2. **Add** a new structured-diff tool and keep `edit.apply_patch` as a simple
   helper.

### Decision

We chose **Option 2**:

- Keep `edit.apply_patch` as a **simple text replacement helper**:
  - Inputs: `path`, `old_text`, `new_text`.
  - Best for single-location, low-complexity edits.
- Add **`edit.apply_structured_diff`** as the canonical diff-based edit tool:
  - Inputs: `path`, `diff_json`, optional `dry_run`.
  - `diff_json` matches the structured output from the `code/diff` skill.
  - Supports multiple hunks, context verification, and dry-run mode.

### Rationale

- Preserves backwards compatibility for existing flows that rely on
  `edit.apply_patch` semantics.
- Makes the structured diff semantics explicit, instead of overloading the
  simpler helper.
- Aligns better with the Phase 6 retrieval funnel: `code/diff` produces
  structured hunks, and `edit.apply_structured_diff` consumes them.

### Notes

- All new tests and knowledge docs treat `edit.apply_structured_diff` as the
  preferred tool for complex/multi-hunk changes.
- `edit.apply_patch` remains available for small, targeted replacements.

---

## 3. Role / Tool Matrix (Coder, Planner, Reviewer)

### Current Implementation

The runtime builds role-specific signatures in
`internal/agent/runtime/runtime.go`:

- **Coder (`types.RoleCoder`)**
  - **Code search & retrieval**
    - `code.symbol_search`
    - `code.swe_grep`
    - `code.search`
  - **File operations**
    - `fs.read_file`
    - `fs.list_dir`
  - **Edit tools**
    - `edit.create_file`
    - `edit.apply_patch`
    - `edit.apply_structured_diff`
  - **Tests**
    - `tests.run`
  - Signature text includes a short workflow description:
    - Use symbol search → SWE Grep → edits via patch or structured diff.

- **Planner (`types.RolePlanner`)**
  - **Planning & coordination only**
    - `todo.add`
    - `todo.query`
    - `todo.graph_insights`
    - `mail.send`
  - **No** direct access to low-level code/edit tools.
  - Keeps Planner focused on orchestration and task management.

- **Reviewer (`types.RoleReviewer`)**
  - **Code search & retrieval (read/inspect)**
    - `code.symbol_search`
    - `code.swe_grep`
    - `code.search`
  - **File operations (read-only)**
    - `fs.read_file`
    - `fs.list_dir`
  - **Validation**
    - `tests.run`
  - **Coordination**
    - `mail.send`
    - `todo.add`
  - **Explicitly NOT exposed**
    - `edit.create_file`
    - `edit.apply_patch`
    - `edit.apply_structured_diff`
  - Signature text emphasizes: "do not directly apply edits yourself".

### Decision

For Phase 6, we adopt the following matrix:

| Role     | Retrieval tools                                                                     | Edit tools                                                           | Coordination tools                   |
| -------- | ----------------------------------------------------------------------------------- | -------------------------------------------------------------------- | ------------------------------------ |
| Coder    | `code.symbol_search`, `code.swe_grep`, `code.search`                                | `edit.create_file`, `edit.apply_patch`, `edit.apply_structured_diff` | —                                    |
| Planner  | —                                                                                   | —                                                                    | `todo.*`, `mail.send`                |
| Reviewer | `code.symbol_search`, `code.swe_grep`, `code.search`, `fs.read_file`, `fs.list_dir` | **None** (read-only role)                                            | `tests.run`, `mail.send`, `todo.add` |

### Rationale

- Coder needs full access to the retrieval funnel and both edit tools to
  implement end-to-end changes.
- Planner should remain orchestration-only to avoid mixing planning and
  low-level editing responsibilities.
- Reviewer has full retrieval access to understand code context but **no edit
  tools**, preserving separation of concerns. Reviewer can suggest patches in
  natural language but leaves application to Coder or humans.

---

## 4. Knowledge & Skills for the Retrieval Funnel

Phase 6 adds an explicit knowledge skill for the retrieval funnel under
`docs/knowledge/`:

- **Skill:** `code-retrieval`
  - Location: `docs/knowledge/code-retrieval/`.
  - Content:
    - `SKILL.md`: overview and funnel diagram: `semantic_file_index` →
      `code.symbol_search` → `code.swe_grep` → edits.
    - `SYMBOL_SEARCH.md`: `code.symbol_search` schema, modes, and best
      practices.
    - `SWE_GREP.md`: `code.swe_grep` schema, candidate file format, workflows.
    - `EDIT_TOOLS.md`: comparison of `edit.apply_patch` vs
      `edit.apply_structured_diff` with examples.
    - `GUARDRAILS.md`: guardrails and anti-patterns for each tool, plus a
      decision matrix.
- **Skill rules:** `docs/knowledge/skill-rules.json`
  - Adds `code-retrieval` with prompt and file triggers for:
    - Queries mentioning symbol search, SWE Grep, diffs, refactors, etc.
    - Internal code paths related to agent tools and skills.

### Decision

- Retrieval funnel knowledge lives in the `code-retrieval` skill and is exposed
  via the existing skills/knowledge router.
- No schema or wire-contract changes were required; only content and
  keyword/trigger updates.

---

## 5. Deviations vs Initial Phase 6 Spec

- **Edit tool strategy:**
  - The spec allowed either migrating `edit.apply_patch` or adding a new
    structured-diff tool.
  - We chose the "add new tool" path and documented it as the canonical way to
    apply `code/diff` outputs.
- **Reviewer role:**
  - The spec called out Reviewer as a possible consumer of retrieval/edit tools.
  - Initially deferred, but subsequently implemented with a read-only profile:
    - Full retrieval tools (`code.symbol_search`, `code.swe_grep`,
      `code.search`)
    - Read-only FS tools (`fs.read_file`, `fs.list_dir`)
    - Validation (`tests.run`) and coordination (`mail.send`, `todo.add`)
    - **No edit tools** — Reviewer suggests but does not apply changes.

No other deviations from the Phase 6 spec were identified. Existing runtime,
envelope, and runner contracts remain unchanged.
