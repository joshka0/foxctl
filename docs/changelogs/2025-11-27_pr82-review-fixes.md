# PR #82 Review Fixes

**Date:** 2025-11-27\
**PR:** #82 (claude harness)

## Summary

Addressed CodeRabbit review comments from PR #82. Fixes include markdown linting
issues, typos, broken links, variable shadowing, and a case-sensitivity bug in
keyword search.

## Changes

### Code Fixes

- **`cmd/agentctl/cmd/knowledge.go`**: Fixed variable shadowing (`err` →
  `pathErr`) in `MatchByPath` call
- **`internal/storage/knowledge/store.go`**: Fixed case mismatch in keyword
  search by lowercasing input keywords to match `LOWER(t.pattern)` in SQL

### Documentation Fixes

- **`.claude/CLAUDE.md`**: Replaced hard tabs with 2-space indentation in JSON
  code blocks (MD010)
- **`docs/changelogs/2024-11-27_*.md`**: Fixed incorrect dates (2024 → 2025) and
  renamed files
- **`docs/knowledge/backend-dev-guidelines/resources/architecture-overview.md`**:
  Added `text` language specifier to fenced code blocks (MD040)
- **`docs/knowledge/backend-dev-guidelines/resources/database-patterns.md`**:
  Fixed broken SKILL.md link (`SKILL.md` → `../SKILL.md`)
- **`docs/knowledge/backend-dev-guidelines/resources/middleware-guide.md`**:
  Removed broken "Validation Middleware" TOC entry (MD051)
- **`docs/knowledge/backend-dev-guidelines/resources/routing-and-controllers.md`**:
  Replaced bold emphasis with proper markdown headings (MD036)
- **`docs/knowledge/frontend-dev-guidelines/resources/complete-examples.md`**:
  - Added `text` language specifier to directory structure code block
  - Fixed find-replace errors: `useBlog` → `useForm`, `@hookblog` → `@hookform`,
    `blogState` → `formState`, `<blog>` → `<form>`, `perblogance` →
    `performance`
- **`docs/knowledge/skill-developer/ADVANCED.md`**: Fixed typo `Dashbord` →
  `Dashboard`
- **`docs/knowledge/skill-developer/SKILL.md`**: Hyphenated `Low-priority` as
  compound adjective
- **`docs/spec/knowledge_registry.md`**:
  - Aligned decision naming (`DecisionNone` → `decision: "none"`)
  - Fixed hard tabs in JSON blocks
  - Made `cosineSimilarity` example valid Go code with `math.Sqrt` and zero-norm
    guard
- **`docs/spec/test_watch_feedback.md`**: Aligned hook directory paths
  (`.agentctl/hooks/` → `.claude/hooks/`)

## Testing

- `go vet ./...` passes
- `go test ./internal/storage/knowledge/...` passes
