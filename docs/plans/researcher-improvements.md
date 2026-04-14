# Implementation Plan: foxctl Researcher Improvements

## Problem Statement

- **Repo graph search returns 0 results for natural language queries**: FTS5 uses implicit AND — `"hybrid memory pipeline episode evidence"` requires ALL 5 words in a single node. No node has all 5, so 0 results. The fallback in `query.go:34` only triggers on FTS5 syntax errors, not on empty results.
- **foxctl-research skill underutilizes available tools**: Comparison testing showed the research agent was slower and shallower than a direct file-reading agent. Key tools like `summarize: true`, `repo ask`, and `codemap/generate` are ignored or poorly integrated.
- **Data from comparison test**: Reviewer agent (Glob/Grep/Read) completed in 2m31s with 15 tool calls and produced a deeply detailed report. foxctl-research (Bash/CLI) took 2m53s with 30 tool calls but produced comparable depth only after falling back to direct file reading because CLI tools returned shallow results.

## Decisions Made

- OR fallback triggers on **zero results**, not just syntax errors
- Auto-retry in existing `QueryEngine.Search` method (no new API)
- Update both `~/.claude/skills/foxctl-research/SKILL.md` AND `configs/opencode/agents-pack/foxctl-research.md`
- Include unit tests AND benchmarks for the code change

---

## File Changes

### 1. `internal/intelligence/indexing/repoindex/query.go` (modified)

**Purpose**: Add OR-fallback when multi-word FTS5 queries return 0 results.

**New helpers:**

```go
// buildFallbackCandidates returns candidate queries in priority order:
// 1. Raw trimmed query (existing AND behavior)
// 2. Quoted fallback (existing syntax-error repair)
// 3. OR fallback (for multi-word queries only)
func buildFallbackCandidates(query string) []string {
    trimmed := strings.TrimSpace(query)
    if trimmed == "" {
        return nil
    }
    candidates := []string{trimmed}

    quoted := quoteFTSQuery(trimmed)
    if quoted != "" && quoted != trimmed {
        candidates = append(candidates, quoted)
    }

    if isMultiWordQuery(trimmed) {
        if orQuery := buildOrFallbackQuery(trimmed); orQuery != "" && orQuery != trimmed && orQuery != quoted {
            candidates = append(candidates, orQuery)
        }
    }
    return candidates
}

// isMultiWordQuery returns true if the query contains more than one word.
func isMultiWordQuery(query string) bool {
    return len(strings.Fields(strings.TrimSpace(query))) > 1
}

// buildOrFallbackQuery converts "a b c" to "a OR b OR c", stripping FTS5 operators.
func buildOrFallbackQuery(query string) string {
    raw := strings.Fields(strings.TrimSpace(query))
    terms := make([]string, 0, len(raw))
    for _, term := range raw {
        t := strings.TrimSpace(term)
        if t == "" {
            continue
        }
        upper := strings.ToUpper(t)
        if upper == "AND" || upper == "OR" || upper == "NOT" {
            continue
        }
        t = strings.Trim(t, "\"'")
        if t == "" {
            continue
        }
        terms = append(terms, t)
    }
    if len(terms) < 2 {
        return ""
    }
    return strings.Join(terms, " OR ")
}
```

**Modified `Search` and `SearchScored`**: Use shared fallback executor that tries candidates in order, advancing to next candidate when current returns 0 results (for multi-word) or syntax error.

```go
func searchWithFallback[T any](
    ctx context.Context,
    candidates []string,
    limit int,
    searchFn func(context.Context, string, int) ([]T, error),
    originalQuery string,
) ([]T, error) {
    var lastErr error
    isMulti := isMultiWordQuery(originalQuery)

    for i, candidate := range candidates {
        results, err := searchFn(ctx, candidate, limit)
        if err == nil {
            if len(results) > 0 || !isMulti || i == len(candidates)-1 {
                return results, nil
            }
            continue // zero results on multi-word, try next candidate
        }
        if !isFTSSyntaxError(err) {
            return nil, err // non-syntax errors fail fast
        }
        lastErr = err
    }

    if lastErr != nil {
        return nil, lastErr
    }
    return nil, nil
}
```

**Behavioral changes:**
- Multi-word queries that return 0 results now automatically retry with OR-separated terms
- Single-word queries are unchanged
- Existing quoted fallback for syntax errors is preserved
- Non-FTS errors still fail fast

---

### 2. `internal/intelligence/indexing/repoindex/query_test.go` (new)

**Purpose**: Unit tests and benchmarks for OR-fallback behavior.

**Unit tests:**

| Test | Input | Assertion |
|------|-------|-----------|
| `TestBuildOrFallbackQuery_MultiWord` | `"hybrid memory pipeline"` | Returns `"hybrid OR memory OR pipeline"` |
| `TestBuildOrFallbackQuery_SkipsOperators` | `"cache AND invalid"` | Strips `AND`, returns `"cache OR invalid"` |
| `TestBuildOrFallbackQuery_SingleWord` | `"hybrid"` | Returns `""` (no fallback) |
| `TestBuildFallbackCandidates_Order` | `"hybrid memory"` | Candidates: `["hybrid memory", "\"hybrid memory\"", "hybrid OR memory"]` |
| `TestSearch_ZeroResultMultiWord_UsesOr` | Multi-word query, nodes match individual terms | Non-empty results via OR fallback |
| `TestSearch_ZeroResultSingleWord_NoFallback` | Single-word, no matches | Empty results, no OR attempted |
| `TestSearch_SyntaxError_FallsBackToQuote` | Malformed FTS5 query | Quoted fallback succeeds |

**Benchmarks:**

| Benchmark | Setup | Measures |
|-----------|-------|----------|
| `BenchmarkSearch_ZeroResultMultiWord_OrFallback` | Small corpus, no AND match | Overhead of fallback chain |
| `BenchmarkSearchScored_ZeroResultMultiWord_OrFallback` | Same corpus, scored path | Scored fallback overhead |
| `BenchmarkSearch_SyntaxFallbackPath` | Invalid syntax query | Syntax error detection + quote repair |

**Setup**: Use `testing.T.TempDir()` with ephemeral SQLite store, `store.ReplaceAll` with deterministic nodes.

---

### 3. `~/.claude/skills/foxctl-research/SKILL.md` (modified)

**Purpose**: Integrate `summarize: true`, `repo ask`, `codemap/generate`, parallelization, and standardize on `context_grep`.

**Changes by section:**

#### A) Tier 1: Search & Discovery table

Add two new entries, fix `context_grep`:

| Tool | Command | Best For |
|------|---------|----------|
| **Semantic Search** | `foxctl run code/semantic_search --input '{"query": "...", "format": "tree", "limit": 25, "summarize": true}'` | Conceptual search with LLM synthesis |
| **DAG Grep** | `foxctl run code/dag_grep --input '{"query": "...", "render": "tree", "depth": 2, "budget": 80}'` | Relationship graphs — "what calls/uses X?" |
| **Context Grep** | `foxctl run code/context_grep --input '{"pattern": "...", "path": ".", "mode": "ripgrep", "expand_functions": true}'` | Regex + AST + function bodies — exact matches with context |
| **Smart Search** | `foxctl run code/smart_search --input '{"question": "...", "limits": {"max_snippets": 20}}'` | Auto-candidate + extract — when you don't know which files |
| **Codemap Generate** | `foxctl codemap generate "trace auth flow"` | AI-traced code relationship maps |
| **Text Grep** | `foxctl run text/grep --input '{"pattern": "...", "path": "."}'` | Fast literal search — exact strings |

**Note**: `context_ripgrep` is deprecated. Use `context_grep` which supports ripgrep, ast-grep, and line expansion modes.

#### B) Repo Graph section

Rewrite to promote `repo ask` as PRIMARY:

```markdown
### Repo Graph (requires index)

**`repo ask` is the PRIMARY tool for architecture and relationship questions.** It runs an LLM agent with up to 12 iterations that can autonomously search, expand, open nodes, and extract DAG subgraphs.

| Tool | Command | Best For |
|------|---------|----------|
| **Graph Ask** | `foxctl index repo ask --workspace . --question "..."` | **PRIMARY** — architecture, call graphs, impact, ownership, coupling |
| **Graph Search** | `foxctl index repo search --workspace . --query "..." --limit 10` | Find specific nodes by name |
| **Graph Expand** | `foxctl index repo expand --workspace . --seed "<id>" --edge CALLS --edge REFERS_TO --depth 2` | Manual edge traversal from known node |

**When to use `repo ask` over manual search+expand:**
- "How does X work?" → `repo ask`
- "What calls this function?" → `repo ask`
- "What's the blast radius of changing Y?" → `repo ask`
- "Find the exact node ID for Z" → `graph search` (then optionally `expand`)
```

#### C) Pipeline Rules

Add parallelization guidance and `summarize: true`:

```markdown
### Pipeline Rules

1. **Start broad, narrow fast** — Semantic search first, then targeted extraction
2. **Use `repo ask` for architecture questions** — Don't manually trace call chains with search+expand
3. **Use `summarize: true`** — Get LLM-synthesized answers directly from semantic search
4. **Parallelize independent tools** — Run these simultaneously:
   - `code/semantic_search` (with `summarize: true`)
   - `repo ask` (for relationship questions)
   - `memory/query` + `session/recall` (for context)
5. **Use `codemap/generate` when map freshness is uncertain** — Run before `repo ask` if the codebase changed significantly
6. **Always check memory** — Existing gotchas/decisions may answer the question
7. **Cap depth** — If 3 rounds of search haven't answered, report what you found
8. **Iterative refinement** — If results are noisy, narrow with:
   - Scope filters on semantic search (`scope: ["symbols"]`)
   - `context_grep` with ast-grep patterns for structural matching
   - `dag_grep` with `node_kinds` filter for specific node types
```

#### D) Query Classification (Step 1: Understand the Question)

Update to include `repo ask` and `codemap/generate`:

```markdown
### Step 1: Understand the Question

Classify the query:
- **Location** ("where is X?") → `semantic_search` (Tier 1)
- **Explanation** ("how does X work?") → `repo ask` + `semantic_search` (parallel)
- **Analysis** ("is X good?", "should we do Y?") → Tier 1 + 2 + `counsel`
- **Impact** ("what uses X?", "what breaks if we change Y?") → `repo ask` (primary) + `dag_grep`
- **Architecture** ("how is the system structured?") → `repo ask` + `codemap generate`
- **History** ("what did we decide?", "any gotchas?") → `memory/query` + `session/recall`
```

#### E) Composition Pattern — Standalone examples

Update to show richer `semantic_search` usage:

```markdown
### Basic Research Task

Task(
  subagent_type="Bash",
  description="Research: <short description>",
  prompt="You are a research-only agent using foxctl CLI tools.

WORKSPACE: <repo_dir>

## Research Strategy
1. Run discovery tools IN PARALLEL:
   - `foxctl run code/semantic_search --input '{\"query\": \"...\", \"summarize\": true, \"limit\": 30}'`
   - `foxctl index repo ask --workspace . --question \"...\"` (for architecture/relationship questions)
   - `foxctl run memory/query --input '{\"query\": \"...\", \"types\": \"gotcha,decision,pattern\"}'`

2. If discovery is insufficient, run targeted extraction:
   - `foxctl run code/context_grep --input '{\"pattern\": \"...\", \"path\": \".\", \"mode\": \"ripgrep\", \"expand_functions\": true}'`
   - `foxctl run code/snippet_extract --input '{\"candidates\": [...], \"question\": \"...\"}'`

3. For architecture questions, ensure map freshness:
   - `foxctl codemap generate \"trace <topic>\"` (if needed)

CONSTRAINT: Read-only. Do NOT modify files or run git commands.
RESEARCH TASK: <detailed query>"
)
```

---

### 4. `configs/opencode/agents-pack/foxctl-research.md` (modified)

**Purpose**: Align opencode variant with the updated SKILL.md strategy.

Replace body content (preserve any existing frontmatter) with:

```markdown
# foxctl-research

Research-only agent using foxctl code intelligence tools. Read-only investigation.

## Primary Tools

| Tool | Best For |
|------|----------|
| `code/semantic_search` (with `summarize: true`) | Concept search with LLM synthesis |
| `repo ask` | Architecture, relationships, impact, ownership |
| `context_grep` | AST + ripgrep + function body extraction |
| `codemap generate` | AI-traced code relationship maps |
| `memory/query` | Past learnings, gotchas, decisions |
| `session/recall` | Past session context |

## Strategy

1. **Parallelize**: Run `semantic_search`, `repo ask`, and `memory/query` simultaneously
2. **`repo ask` for architecture**: Use for "how does X work?", "what calls X?", impact analysis
3. **`summarize: true`**: Always include on non-trivial semantic searches
4. **`context_grep` over `context_ripgrep`**: Use the newer multi-mode tool
5. **`codemap generate`** before graph queries when map freshness is uncertain
6. **Cap depth**: 3 rounds max, then report findings and open questions

## Output Format

- Prefer official docs terminology
- Summarize as actionable steps
- Include file:line references
- Do not modify files
```

---

## Testing Strategy

### Unit Tests (`internal/intelligence/indexing/repoindex/query_test.go`)
- 7 test cases covering: multi-word OR, operator stripping, single-word no-fallback, candidate ordering, zero-result fallback with store, syntax error fallback
- Test store setup: `testing.T.TempDir()` + `repoindex.Open` + `store.ReplaceAll` with deterministic nodes

### Benchmarks (`internal/intelligence/indexing/repoindex/query_test.go`)
- 3 benchmarks: multi-word OR fallback overhead, scored fallback overhead, syntax error path
- Small deterministic corpus for stable baselines

### Integration Verification
```bash
# Verify OR fallback works
foxctl index repo search --workspace . --query "hybrid memory pipeline episode evidence" --limit 10
# Should return results now (was 0 before)

# Verify single-word still works
foxctl index repo search --workspace . --query "BuildHybridContextLayers" --limit 5
# Should return same results as before

# Verify SKILL.md loads correctly
# Launch Claude Code, invoke /foxctl-research "how does companion memory work?"
# Verify it uses repo ask and summarize: true
```

---

## Implementation Order

1. **Add OR-fallback helpers** in `query.go` — `buildOrFallbackQuery`, `isMultiWordQuery`, `buildFallbackCandidates`
2. **Add shared fallback executor** — `searchWithFallback` generic function
3. **Modify `Search` and `SearchScored`** to use the shared executor
4. **Write unit tests** — all 7 test cases
5. **Write benchmarks** — all 3 benchmark cases
6. **Run tests** — `go test ./internal/intelligence/indexing/repoindex/... -v -bench=.`
7. **Integration test** — verify with actual repo graph queries
8. **Update SKILL.md** — apply all section changes
9. **Update opencode variant** — apply condensed version
10. **Verify skills load** — test with `/foxctl-research`

## Open Questions

None — all decisions resolved.
