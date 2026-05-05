---
name: hybrid-research
description: "Hybrid research agent combining native Claude Code tools (Read/Grep/Glob) with foxctl MCP tools (semantic_search, snippet_extract, memory_query). Best of both: fast direct file access + semantic discovery. Use for: hybrid research, smart research, investigate code, deep search, codebase analysis, architecture questions."
---

# hybrid-research

A hybrid research agent that combines **native Claude Code tools** (fast, exact) with **foxctl MCP tools** (semantic, conceptual) for optimal codebase investigation. Read-only.

## Command

`/hybrid-research <query>` — Run a hybrid research investigation.

### Arguments

- **`<query>`** — Natural language question or investigation target

## Strategy: Right Tool for the Job

### Discovery Phase (conceptual → files)
Use foxctl MCP tools to find relevant code by concept:

| Tool | When to Use |
|------|-------------|
| `mcp__agentctl__code_semantic_search` | "Where is X?" — find files by concept |
| `mcp__agentctl__code_smart_search` | Don't know which files — auto-discover + extract |
| `mcp__agentctl__code_dag_grep` | "What calls/uses X?" — relationship graphs |
| `mcp__agentctl__memory_query` | "What relevant memory records exist?" — past evidence and decisions |
| `mcp__agentctl__session_recall` | "What did we do before?" — session history |
| `mcp__agentctl__session_timeline` | "What happened in past sessions?" — timeline |

### Extraction Phase (files → code)
Use native Claude Code tools for fast, exact extraction:

| Tool | When to Use |
|------|-------------|
| `Read` | Read specific files identified in discovery (instant) |
| `Grep` | Find exact patterns, function calls, references (instant) |
| `Glob` | Find files by name pattern (instant) |

### Deep Analysis (when needed)
Use foxctl MCP tools for AI-powered analysis:

| Tool | When to Use |
|------|-------------|
| `mcp__agentctl__code_snippet_extract` | AI-selected relevant sections from multiple files |
| `mcp__agentctl__code_symbols` | Type signatures and API shapes |
| `mcp__agentctl__code_context_grep` | Regex + full function body expansion |

## Pipeline

```
Round 1: DISCOVER (foxctl MCP — semantic)
  ├─ code_semantic_search: find files by concept
  ├─ memory_query: check canonical memory records
  └─ session_recall: check past session context

Round 2: EXTRACT (native tools — fast)
  ├─ Read: read top files from Round 1
  ├─ Grep: find exact patterns, callsites
  └─ Glob: find related files by naming convention

Round 3: ANALYZE (foxctl MCP — deep, if needed)
  ├─ code_snippet_extract: AI-extract relevant sections
  ├─ code_symbols: type signatures
  └─ code_dag_grep: relationship graphs
```

### Key Principle

- **Semantic discovery → exact extraction**: Use MCP tools to find WHAT matters, native tools to read it fast
- **Never use Bash for foxctl**: MCP tools are first-class — call them directly, not via `foxctl run`
- **Parallelize**: Run independent MCP + native tool calls in the same message

## Execution

### Step 1: Classify Query

| Query Type | Discovery | Extraction |
|------------|-----------|------------|
| **Location** ("where is X?") | `code_semantic_search` | — (sufficient) |
| **Explanation** ("how does X work?") | `code_semantic_search` + `memory_query` | `Read` key files |
| **Impact** ("what uses X?") | `code_dag_grep` | `Grep` for callsites |
| **Architecture** ("how is it structured?") | `code_smart_search` | `Read` + `Glob` for related files |
| **History** ("what did we decide?") | `memory_query` + `session_recall` | — (sufficient) |

### Step 2: Execute Pipeline

**Round 1** — Run discovery tools in parallel:
- `mcp__agentctl__code_semantic_search` with `query`, `summarize: true`, `limit: 25`
- `mcp__agentctl__memory_query` with `query`, `kinds: "semantic_fact,decision,procedural_skill"`
- (if history question) `mcp__agentctl__session_recall` with `query`

**Round 2** — From Round 1 results, extract with native tools in parallel:
- `Read` the top 3-5 files identified
- `Grep` for specific patterns, function names, or callsites
- `Glob` for related test files, configs, or siblings

**Round 3** (if deeper analysis needed):
- `mcp__agentctl__code_snippet_extract` on remaining candidates
- `mcp__agentctl__code_symbols` for type signatures

### Step 3: Report

```markdown
## Research: <query>

### Summary
<1-3 sentence answer>

### Key Findings
- <finding with file:line references>

### Code References
<relevant snippets from Read/Grep>

### Memory/History (if relevant)
- <relevant memory records and past decisions>

### Open Questions (if any)
```

## Prerequisites

Requires the foxctl MCP server configured in `.mcp.json`:

```json
{
  "mcpServers": {
    "foxctl": {
      "command": "foxctl",
      "args": ["mcp", "serve", "--groups", "code-intel,project"]
    }
  }
}
```

## Composition

Use as a Task subagent:

```
Task(
  subagent_type="Explore",
  description="Hybrid research: <topic>",
  prompt="You are a hybrid research agent. Use BOTH native tools (Read, Grep, Glob) AND foxctl MCP tools for investigation.

STRATEGY:
1. DISCOVER with MCP tools: code_semantic_search, memory_query
2. EXTRACT with native tools: Read, Grep, Glob (fast, exact)
3. ANALYZE with MCP tools if needed: code_snippet_extract, code_symbols

RESEARCH TASK: <query>

Return structured findings with file:line references and code snippets."
)
```
