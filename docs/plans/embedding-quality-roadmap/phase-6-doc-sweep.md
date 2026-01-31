# Phase 6: Repo-wide Doc Sweep

> **Goal:** Achieve 80%+ GoDoc coverage on exported symbols with proper `Index:` blocks for semantic navigation.

## Overview

This phase systematically documents the codebase by:
1. Building tooling to identify documentation gaps
2. Establishing linting to maintain quality going forward
3. Executing a structured sweep across subsystems

## Dependencies

- **Phase 0.1** (Doc standards) - `Index:` block format must be defined first
- **Phase 4** (Repo graph) - Fan-in/fan-out analysis uses graph edges

## PRs

---

### PR 6.1: Doc Coverage Report Tool

**Summary:**
Create a CLI tool that analyzes the codebase and outputs actionable Markdown checklists identifying documentation gaps. The tool leverages the repo graph to identify "hub" symbols that would benefit most from documentation.

**Files Touched:**
- `cmd/agentctl/cmd/doc_coverage.go` - CLI command implementation
- `internal/analysis/doc_coverage.go` - Core analysis logic
- `internal/analysis/doc_coverage_test.go` - Unit tests

**Implementation Details:**

```go
// internal/analysis/doc_coverage.go

// Report represents documentation coverage analysis results
type Report struct {
    Packages []PackageReport
    Summary  Summary
}

type PackageReport struct {
    Path            string
    HasDocGo        bool
    ExportedSymbols []SymbolReport
    HubCandidates   []HubCandidate
}

type SymbolReport struct {
    Name       string
    Kind       string // func, type, const, var
    HasGoDoc   bool
    HasIndex   bool   // has Index: block
    LineNumber int
}

type HubCandidate struct {
    Name     string
    FanIn    int  // symbols that call/reference this
    FanOut   int  // symbols this calls/references
    Priority int  // calculated priority score
}

// Analyze scans packages and returns coverage report
func Analyze(ctx context.Context, patterns []string, graphDB *sql.DB) (*Report, error)

// FormatMarkdown outputs actionable checklists per package
func (r *Report) FormatMarkdown(w io.Writer) error
```

**CLI Interface:**
```bash
# Full repo scan
agentctl doc-coverage ./...

# Specific packages
agentctl doc-coverage ./internal/indexing/... ./skills/...

# Output to file for tracking
agentctl doc-coverage ./... > docs/doc-coverage-checklist.md

# With hub analysis (requires repo graph)
agentctl doc-coverage ./... --include-hubs
```

**Output Format:**
```markdown
# Documentation Coverage Report

Generated: 2024-01-15T10:30:00Z

## Summary
- Packages scanned: 45
- Packages missing doc.go: 12
- Exported symbols: 342
- Symbols with GoDoc: 198 (57.9%)
- Symbols with Index block: 45 (13.2%)

---

## internal/indexing/repoindex/store

- [ ] Add `doc.go` for package

### Missing GoDoc

- [ ] `Store` (type) - line 23
- [ ] `NewStore` (func) - line 45
- [ ] `PutNode` (func) - line 89

### Hub Candidates (high fan-in/fan-out)

| Symbol | Fan-In | Fan-Out | Priority |
|--------|--------|---------|----------|
| `Store.Query` | 15 | 8 | HIGH |
| `Store.Expand` | 12 | 6 | HIGH |

---

## internal/retrieval

- [x] Has `doc.go`

### Missing GoDoc

- [ ] `SearchOptions` (type) - line 34
```

**Testing Strategy:**
- Unit tests with fixture packages containing various doc states
- Test fan-in/fan-out calculation with mock graph data
- Test Markdown output formatting
- Integration test against actual codebase packages

**Acceptance Criteria:**
- [ ] `agentctl doc-coverage ./...` runs without error
- [ ] Report correctly identifies exported symbols missing GoDoc
- [ ] Report correctly identifies packages missing `doc.go`
- [ ] Hub candidates sorted by priority (fan-in * fan-out weight)
- [ ] Output Markdown is valid and actionable as GitHub issue checklist
- [ ] Report includes line numbers for easy navigation
- [ ] `--include-hubs` flag works with repo graph DB

---

### PR 6.2: Optional Doc Linter (Warn-Only Initially)

**Summary:**
Create a doc linter that can run in CI to enforce documentation standards. Initially warn-only to avoid blocking PRs, with path to enforcement.

**Files Touched:**
- `scripts/lint-doc.sh` - CI entrypoint script
- `internal/analysis/doc_lint.go` - Linting rules implementation
- `internal/analysis/doc_lint_test.go` - Unit tests
- `Makefile` - Add `lint-doc` target
- `.github/workflows/ci.yml` - Optional CI integration (warn-only)

**Implementation Details:**

```go
// internal/analysis/doc_lint.go

// Rule represents a documentation lint rule
type Rule interface {
    ID() string
    Name() string
    Check(ctx context.Context, pkg *ast.Package, fset *token.FileSet) []Violation
}

// Violation represents a lint rule violation
type Violation struct {
    Rule     string
    File     string
    Line     int
    Symbol   string
    Message  string
    Severity Severity // warn, error
}

// Rules implemented:

// ExportedDocRule - exported items must have first sentence
type ExportedDocRule struct{}

// IndexBlockRule - if structured Index: block exists, must have Purpose and Related
type IndexBlockRule struct{}

// SkillKeywordsRule - skill manifests must include command string
type SkillKeywordsRule struct{}

// Linter orchestrates rule checking
type Linter struct {
    Rules    []Rule
    Severity Severity // minimum severity to report
}

func (l *Linter) Lint(ctx context.Context, patterns []string) ([]Violation, error)
```

**Lint Rules:**

| Rule ID | Description | Severity |
|---------|-------------|----------|
| `DOC001` | Exported symbol missing GoDoc | warn |
| `DOC002` | GoDoc missing first sentence (period-terminated) | warn |
| `DOC003` | Structured `Index:` block missing `Purpose` field | warn |
| `DOC004` | Structured `Index:` block missing `Related` field | warn |
| `DOC005` | Skill manifest `Keywords` missing command string | warn |

**CLI Interface:**
```bash
# Run doc linter
make lint-doc

# Or directly
agentctl lint-doc ./...

# Specific packages
agentctl lint-doc ./internal/indexing/...

# Exit with error on warnings (for enforcement)
agentctl lint-doc ./... --fail-on-warn
```

**scripts/lint-doc.sh:**
```bash
#!/bin/bash
set -e

# Run doc linter in warn-only mode
echo "Running documentation linter..."
agentctl lint-doc ./... 2>&1 | tee lint-doc-output.txt

# Count violations
VIOLATIONS=$(grep -c "^WARN:" lint-doc-output.txt || true)

if [ "$VIOLATIONS" -gt 0 ]; then
    echo ""
    echo "Found $VIOLATIONS documentation warnings."
    echo "See docs/standards/doc-comments.md for guidelines."
    # Exit 0 for now (warn-only mode)
    exit 0
fi

echo "Documentation linting passed!"
```

**Makefile Addition:**
```makefile
.PHONY: lint-doc
lint-doc:
	@./scripts/lint-doc.sh
```

**Testing Strategy:**
- Unit tests for each lint rule with positive/negative cases
- Test `Index:` block parsing
- Test skill manifest keyword checking
- Integration test against known-good and known-bad fixtures

**Acceptance Criteria:**
- [ ] `make lint-doc` runs and reports violations
- [ ] DOC001: Detects missing GoDoc on exported symbols
- [ ] DOC002: Detects GoDoc without proper first sentence
- [ ] DOC003/004: Validates structured `Index:` block fields (keywords-only shorthand is allowed)
- [ ] DOC005: Checks skill manifests for command in Keywords
- [ ] Warn-only mode exits 0 (no CI breakage initially)
- [ ] `--fail-on-warn` flag for future enforcement
- [ ] Clear violation messages with file:line references

---

### PR 6.3+: Doc Sweep PRs by Subsystem

**Summary:**
Execute the documentation sweep in focused PRs per subsystem. Each PR adds `doc.go` files and GoDoc comments with `Index:` blocks to a logical grouping of packages.

**PR Breakdown:**

| PR | Subsystem | Packages | Priority |
|----|-----------|----------|----------|
| 6.3 | Storage core | `internal/storage/*` | HIGH |
| 6.4 | Indexing | `internal/indexing/*` | HIGH |
| 6.5 | Retrieval | `internal/retrieval/*` | HIGH |
| 6.6 | Actor system | `internal/actor/*` | MEDIUM |
| 6.7 | Skills (batch 1) | `skills/code/*`, `skills/memory/*` | HIGH |
| 6.8 | Skills (batch 2) | `skills/todo/*`, `skills/repo_index/*` | MEDIUM |
| 6.9 | Platform | `internal/platform/*` | MEDIUM |
| 6.10 | Companion | `internal/companion/*` | LOW |

**Files per PR (template):**

For each package in the subsystem:
- `doc.go` - Package-level documentation
- Updates to exported symbols in `*.go` files

**doc.go Template:**
```go
// Package repoindex provides a SQLite-backed graph index for code navigation.
//
// # Overview
//
// The repoindex package implements a directed acyclic graph (DAG) store that
// captures code relationships including containment, imports, and call edges.
// This enables semantic navigation from any symbol to related symbols.
//
// # Index
//
// Purpose: Graph-based code navigation and relationship queries
// Keywords: repo index, code graph, symbol relationships, navigation
// Related:
//   - [Store] - Core DAG storage operations
//   - [Builder] - Graph construction from AST
//   - [Query] - Search and expansion engine
//   - internal/retrieval - Uses graph for enhanced retrieval
//   - skills/repo_index_search - Agent-facing search tool
package repoindex
```

**Symbol Doc Template:**
```go
// Store manages the SQLite-backed repository graph.
//
// Store provides CRUD operations for nodes and edges in the repo graph,
// supporting containment (file contains symbol), import (file imports package),
// and call (symbol calls symbol) relationships.
//
// # Index
//
// Purpose: Persistent graph storage with transactional updates
// Keywords: sqlite, graph store, nodes, edges, transactions
// Related:
//   - [NewStore] - Constructor with migration support
//   - [Node] - Graph node representation
//   - [Edge] - Graph edge representation
//   - internal/indexing/repoindex/builder - Populates store
type Store struct {
    // ...
}
```

**Focus Areas (ordered by impact):**

1. **Package docs (`doc.go`)** - Every package should have one
2. **Orchestrators** - Indexers, Workers, Stores, Skills entry points
3. **Critical interfaces/types** - Core abstractions used across packages
4. **Hub symbols** - High fan-in/fan-out from coverage report

**Testing Strategy:**
- Verify `godoc` renders correctly for all documented packages
- Run `make lint-doc` to ensure no new violations
- Manual review for accuracy and usefulness of `Index:` blocks

**Acceptance Criteria (per PR):**
- [ ] All packages in scope have `doc.go`
- [ ] All exported symbols have GoDoc with first sentence
- [ ] Orchestrator types/funcs have `Index:` blocks
- [ ] `make lint-doc` passes for packages in scope
- [ ] No unrelated changes included
- [ ] PR description lists symbols documented


## LLM Safety Rails

- Do not invent Related symbols or rename identifiers.
- Only edit comment blocks; never change code behavior.
- If unsure, omit Related/Flow rather than guessing.

## PR Batching Rules

- Keep PRs small (<=20 files).
- Include before/after coverage excerpt in the PR body.
- List all symbols documented in the PR description.
- No unrelated refactors or formatting-only churn.

**Review Guidelines:**

To keep reviews manageable:
- Maximum 20 files per PR
- Group by logical subsystem
- Include coverage report diff showing improvement
- PR description should list all symbols being documented

---

## Tracking

After PR 6.1 ships, generate the initial coverage report:

```bash
agentctl doc-coverage ./... > docs/doc-coverage-baseline.md
```

Track progress by regenerating after each sweep PR and comparing coverage percentages.

## Success Metrics

| Metric | Baseline | Target |
|--------|----------|--------|
| Exported symbols with GoDoc | ~60% | 80%+ |
| Symbols with `Index:` blocks | ~10% | 50%+ |
| Packages with `doc.go` | ~70% | 100% |
| Hub symbols documented | ~30% | 90%+ |
