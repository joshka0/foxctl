# Agent-Driven Semantic Codemaps

## Overview

A dspy-go Claude agent generates semantic codemaps by gathering rich context
from multiple infrastructure sources, then synthesizing a coherent map. The
agent has full autonomy over how to interpret the query and structure the
output.

## Design Philosophy

**Gather everything, let the agent synthesize.**

Rather than prescribing specific query types or flows, we:
1. Run all available context-gathering methods
2. Aggregate results into comprehensive context
3. Give clear instructions to the agent on output format
4. Let the agent decide how to structure the codemap

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                           Codemap Generation Agent                          │
│                              (dspy-go Claude)                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  Query: "map connections between ripgrep and pagerank"                      │
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                 Initial Context Gathering (Parallel)                 │   │
│  ├──────────────────┬──────────────────┬───────────────────────────────┤   │
│  │                  │                  │                               │   │
│  │  Option A:       │  Option B:       │  Option C:                    │   │
│  │  Graph Query     │  Symbol/Import   │  Pattern Search               │   │
│  │                  │  Analysis        │                               │   │
│  │  - File nodes    │  - Extract       │  - Grep for query             │   │
│  │  - Edge paths    │    symbols       │    terms                      │   │
│  │  - PageRank      │  - Parse imports │  - Cross-reference            │   │
│  │    scores        │  - Find shared   │    search                     │   │
│  │                  │    dependencies  │  - Block expansion            │   │
│  │                  │                  │                               │   │
│  └──────────────────┴──────────────────┴───────────────────────────────┘   │
│                                    │                                        │
│                                    ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                      Initial Context                                 │   │
│  │                                                                      │   │
│  │  {                                                                   │   │
│  │    "graph": { nodes, edges, pagerank_scores },                       │   │
│  │    "symbols": { definitions, imports, shared_deps },                 │   │
│  │    "matches": { files, blocks, cross_references }                    │   │
│  │  }                                                                   │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                    │                                        │
│                                    ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    Agent Loop (LLM + Tools)                          │   │
│  │                                                                      │   │
│  │  Given: query + initial context + exploration tools                  │   │
│  │                                                                      │   │
│  │  ┌─────────────────────────────────────────────────────────────┐    │   │
│  │  │                   Exploration Tools                          │    │   │
│  │  ├─────────────────────────────────────────────────────────────┤    │   │
│  │  │  read_file(path)           - Read specific file contents    │    │   │
│  │  │  search_pattern(pattern)   - Grep for pattern, get blocks   │    │   │
│  │  │  get_symbols(path)         - Extract symbols from file      │    │   │
│  │  │  get_graph_neighbors(node) - Get connected files            │    │   │
│  │  │  semantic_search(query)    - Find related content           │    │   │
│  │  │  get_file_imports(path)    - Get imports for a file         │    │   │
│  │  └─────────────────────────────────────────────────────────────┘    │   │
│  │                                                                      │   │
│  │  Loop: Explore → Gather more context → Decide if done → Synthesize  │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                    │                                        │
│                                    ▼                                        │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                    Final Output                                      │   │
│  │                                                                      │   │
│  │  Windsurf-style codemap with traces and annotations                  │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Context Gathering Options

All three options run in parallel for every query. The agent receives all
results and decides what's relevant.

### Option A: Graph Query

**Purpose**: Understand file-level relationships and importance

**Implementation**:
```go
type GraphContext struct {
    Nodes         []graph.Node   `json:"nodes"`          // Files matching query
    Edges         []graph.Edge   `json:"edges"`          // Dependencies between files
    TopByPageRank []graph.Node   `json:"top_by_pagerank"`// Most important files
    Paths         [][]string     `json:"paths"`          // Shortest paths between query terms
}

func gatherGraphContext(ctx context.Context, query string, workspace string) (*GraphContext, error) {
    store, _ := graph.Open(ctx, dataDir)

    // 1. Get all nodes matching query terms
    terms := extractTerms(query) // ["ripgrep", "pagerank"]
    var matchingNodes []graph.Node
    for _, term := range terms {
        nodes, _ := store.SearchNodes(ctx, workspace, term)
        matchingNodes = append(matchingNodes, nodes...)
    }

    // 2. Get edges between matching nodes
    edges, _ := store.GetEdgesBetween(ctx, workspace, nodeIDs(matchingNodes))

    // 3. Get top files by PageRank for broader context
    topNodes, _ := store.TopNodes(ctx, graph.TopNodesOptions{
        Workspace: workspace,
        Limit:     20,
    })

    // 4. Find shortest paths between query term clusters
    paths := findPathsBetweenClusters(store, matchingNodes, terms)

    return &GraphContext{
        Nodes:         matchingNodes,
        Edges:         edges,
        TopByPageRank: topNodes,
        Paths:         paths,
    }, nil
}
```

**Output**: File relationships, importance scores, connection paths

### Option B: Symbol/Import Analysis

**Purpose**: Understand code-level dependencies and shared infrastructure

**Implementation**:
```go
type SymbolContext struct {
    SymbolsByFile  map[string][]Symbol `json:"symbols_by_file"`
    ImportsByFile  map[string][]string `json:"imports_by_file"`
    SharedImports  []string            `json:"shared_imports"`
    ExportedAPIs   []Symbol            `json:"exported_apis"`
}

func gatherSymbolContext(ctx context.Context, query string, workspace string) (*SymbolContext, error) {
    // 1. Find files matching query via semantic search
    searchResult := semanticSearch(ctx, query, []string{"symbols"}, 50)
    relevantFiles := extractFiles(searchResult)

    // 2. Extract symbols from each file
    symbolsByFile := make(map[string][]Symbol)
    importsByFile := make(map[string][]string)

    for _, file := range relevantFiles {
        // Run code/symbols skill
        symbols := runSkill("code/symbols", map[string]any{
            "path":           file,
            "include_docs":   true,
            "include_private": false,
        })
        symbolsByFile[file] = symbols

        // Extract imports (language-specific parsing)
        imports := extractImports(file)
        importsByFile[file] = imports
    }

    // 3. Find shared imports across query-related files
    sharedImports := findSharedImports(importsByFile)

    // 4. Identify exported APIs (public interface)
    var exportedAPIs []Symbol
    for _, syms := range symbolsByFile {
        for _, s := range syms {
            if s.Exported {
                exportedAPIs = append(exportedAPIs, s)
            }
        }
    }

    return &SymbolContext{
        SymbolsByFile: symbolsByFile,
        ImportsByFile: importsByFile,
        SharedImports: sharedImports,
        ExportedAPIs:  exportedAPIs,
    }, nil
}
```

**Output**: Symbol definitions, import trees, shared dependencies

### Option C: Pattern Search (Cross-References)

**Purpose**: Find direct textual references between components

**Implementation**:
```go
type PatternContext struct {
    MatchesByTerm   map[string][]Block `json:"matches_by_term"`
    CrossReferences []CrossRef         `json:"cross_references"`
    FileBlocks      []Block            `json:"file_blocks"`
}

type CrossRef struct {
    SourceFile string `json:"source_file"`
    TargetTerm string `json:"target_term"`
    Line       int    `json:"line"`
    Context    string `json:"context"` // Surrounding code block
}

func gatherPatternContext(ctx context.Context, query string, workspace string) (*PatternContext, error) {
    terms := extractTerms(query) // ["ripgrep", "pagerank"]

    matchesByTerm := make(map[string][]Block)
    var crossRefs []CrossRef

    // 1. Search for each term
    for _, term := range terms {
        result := runSkill("code/context_ripgrep", map[string]any{
            "pattern":    term,
            "path":       workspace,
            "max_blocks": 30,
        })
        matchesByTerm[term] = result.Blocks
    }

    // 2. Find cross-references (term A files mentioning term B)
    for termA, blocksA := range matchesByTerm {
        filesA := uniqueFiles(blocksA)
        for termB := range matchesByTerm {
            if termA == termB {
                continue
            }
            // Search for termB within termA's files
            for _, file := range filesA {
                refs := runSkill("code/context_ripgrep", map[string]any{
                    "pattern": termB,
                    "path":    file,
                })
                for _, block := range refs.Blocks {
                    crossRefs = append(crossRefs, CrossRef{
                        SourceFile: file,
                        TargetTerm: termB,
                        Line:       block.StartLine,
                        Context:    block.Source,
                    })
                }
            }
        }
    }

    return &PatternContext{
        MatchesByTerm:   matchesByTerm,
        CrossReferences: crossRefs,
    }, nil
}
```

**Output**: Pattern matches, cross-file references, code blocks

## Agent Exploration Tools

The agent has tools to follow up on interesting leads from the initial context.
This enables deeper exploration when the initial context reveals something worth
investigating further.

### Tool Definitions

```go
// Tools available to the codemap generation agent
var codemapTools = []dspy.Tool{
    {
        Name:        "read_file",
        Description: "Read the contents of a specific file. Use when you see a file path in the context and want to understand its full contents.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "path": map[string]any{
                    "type":        "string",
                    "description": "File path relative to workspace",
                },
                "start_line": map[string]any{
                    "type":        "integer",
                    "description": "Optional: start reading from this line",
                },
                "end_line": map[string]any{
                    "type":        "integer",
                    "description": "Optional: stop reading at this line",
                },
            },
            "required": []string{"path"},
        },
    },
    {
        Name:        "search_pattern",
        Description: "Search for a pattern in the codebase and get matching code blocks with full function bodies. Use to find usages of a function, references to a type, or occurrences of a term.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "pattern": map[string]any{
                    "type":        "string",
                    "description": "Regex pattern to search for",
                },
                "path": map[string]any{
                    "type":        "string",
                    "description": "Optional: limit search to this path",
                },
                "glob": map[string]any{
                    "type":        "array",
                    "items":       map[string]any{"type": "string"},
                    "description": "Optional: file patterns like '*.go', '*.py'",
                },
                "max_blocks": map[string]any{
                    "type":        "integer",
                    "description": "Max code blocks to return (default: 20)",
                },
            },
            "required": []string{"pattern"},
        },
    },
    {
        Name:        "get_symbols",
        Description: "Extract function, type, and class definitions from a file with line numbers. Use to understand the structure of a file.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "path": map[string]any{
                    "type":        "string",
                    "description": "File path to extract symbols from",
                },
                "symbol_type": map[string]any{
                    "type":        "string",
                    "enum":        []string{"all", "function", "type", "method", "const", "var"},
                    "description": "Filter by symbol type (default: all)",
                },
                "include_docs": map[string]any{
                    "type":        "boolean",
                    "description": "Include documentation comments (default: true)",
                },
            },
            "required": []string{"path"},
        },
    },
    {
        Name:        "get_graph_neighbors",
        Description: "Get files connected to a given file in the dependency graph. Use to explore what a file imports or what imports it.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "node_id": map[string]any{
                    "type":        "string",
                    "description": "File path or node ID to get neighbors for",
                },
                "direction": map[string]any{
                    "type":        "string",
                    "enum":        []string{"in", "out", "both"},
                    "description": "Direction: 'in' (what depends on this), 'out' (what this depends on), 'both'",
                },
                "edge_types": map[string]any{
                    "type":        "array",
                    "items":       map[string]any{"type": "string"},
                    "description": "Optional: filter by edge types like 'imports', 'calls'",
                },
            },
            "required": []string{"node_id"},
        },
    },
    {
        Name:        "semantic_search",
        Description: "Search for code, symbols, or documentation semantically related to a query. Use when you want to find conceptually related content that might not match exact patterns.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "query": map[string]any{
                    "type":        "string",
                    "description": "Natural language query",
                },
                "scopes": map[string]any{
                    "type":        "array",
                    "items":       map[string]any{"type": "string"},
                    "description": "Search scopes: 'symbols', 'memories', 'sessions', 'tasks'",
                },
                "limit": map[string]any{
                    "type":        "integer",
                    "description": "Max results (default: 10)",
                },
            },
            "required": []string{"query"},
        },
    },
    {
        Name:        "get_call_hierarchy",
        Description: "Get the call hierarchy for a function (what it calls, what calls it). Only works for Go files with gopls. Use for precise call tracing.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "path": map[string]any{
                    "type":        "string",
                    "description": "File path containing the function",
                },
                "line": map[string]any{
                    "type":        "integer",
                    "description": "Line number of the function",
                },
                "direction": map[string]any{
                    "type":        "string",
                    "enum":        []string{"incoming", "outgoing"},
                    "description": "Direction: 'outgoing' (what this calls), 'incoming' (what calls this)",
                },
            },
            "required": []string{"path", "line", "direction"},
        },
    },
    {
        Name:        "finish_codemap",
        Description: "Call this when you have gathered enough context and are ready to produce the final codemap. Pass the complete codemap JSON.",
        Parameters: map[string]any{
            "type": "object",
            "properties": map[string]any{
                "codemap": map[string]any{
                    "type":        "object",
                    "description": "The complete codemap object with title, description, and traces",
                },
            },
            "required": []string{"codemap"},
        },
    },
}
```

### Tool Implementations

```go
func (a *CodemapAgent) handleToolCall(ctx context.Context, call dspy.ToolCall) (any, error) {
    switch call.Name {
    case "read_file":
        var params struct {
            Path      string `json:"path"`
            StartLine int    `json:"start_line"`
            EndLine   int    `json:"end_line"`
        }
        json.Unmarshal(call.Arguments, &params)
        return a.readFile(ctx, params.Path, params.StartLine, params.EndLine)

    case "search_pattern":
        var params struct {
            Pattern   string   `json:"pattern"`
            Path      string   `json:"path"`
            Glob      []string `json:"glob"`
            MaxBlocks int      `json:"max_blocks"`
        }
        json.Unmarshal(call.Arguments, &params)
        if params.MaxBlocks == 0 {
            params.MaxBlocks = 20
        }
        return a.runSkill(ctx, "code/context_ripgrep", map[string]any{
            "pattern":    params.Pattern,
            "path":       params.Path,
            "glob":       params.Glob,
            "max_blocks": params.MaxBlocks,
        })

    case "get_symbols":
        var params struct {
            Path        string `json:"path"`
            SymbolType  string `json:"symbol_type"`
            IncludeDocs bool   `json:"include_docs"`
        }
        json.Unmarshal(call.Arguments, &params)
        return a.runSkill(ctx, "code/symbols", map[string]any{
            "path":         params.Path,
            "symbol_type":  params.SymbolType,
            "include_docs": params.IncludeDocs,
        })

    case "get_graph_neighbors":
        var params struct {
            NodeID    string   `json:"node_id"`
            Direction string   `json:"direction"`
            EdgeTypes []string `json:"edge_types"`
        }
        json.Unmarshal(call.Arguments, &params)
        return a.graphStore.GetNeighbors(ctx, a.workspace, params.NodeID, graph.NeighborOptions{
            Direction: params.Direction,
            EdgeTypes: toEdgeTypes(params.EdgeTypes),
        })

    case "semantic_search":
        var params struct {
            Query  string   `json:"query"`
            Scopes []string `json:"scopes"`
            Limit  int      `json:"limit"`
        }
        json.Unmarshal(call.Arguments, &params)
        if params.Limit == 0 {
            params.Limit = 10
        }
        return a.runSkill(ctx, "code/semantic_search", map[string]any{
            "query":  params.Query,
            "scopes": params.Scopes,
            "limit":  params.Limit,
        })

    case "get_call_hierarchy":
        var params struct {
            Path      string `json:"path"`
            Line      int    `json:"line"`
            Direction string `json:"direction"`
        }
        json.Unmarshal(call.Arguments, &params)

        operation := "outgoingCalls"
        if params.Direction == "incoming" {
            operation = "incomingCalls"
        }
        return a.runSkill(ctx, "lsp/gopls", map[string]any{
            "operation": operation,
            "path":      params.Path,
            "line":      params.Line,
        })

    case "finish_codemap":
        var params struct {
            Codemap json.RawMessage `json:"codemap"`
        }
        json.Unmarshal(call.Arguments, &params)
        a.finalCodemap = params.Codemap
        return map[string]string{"status": "codemap_complete"}, nil

    default:
        return nil, fmt.Errorf("unknown tool: %s", call.Name)
    }
}
```

### Example Exploration Flow

```
Initial context shows:
  - graph_context.paths: ["skills/code_context_ripgrep" → "internal/adapters/skillslib" → "skills/graph_pagerank"]
  - pattern_context.cross_references: empty (no direct mentions)

Agent thinks: "I see a path through skillslib. Let me investigate that shared dependency."

Agent calls: get_symbols(path: "internal/adapters/skillslib/runner/context.go")
  → Returns: RunnerContext struct, NewRunnerContext func, Emit method, etc.

Agent thinks: "Both skills use RunnerContext. Let me see how each uses it."

Agent calls: search_pattern(pattern: "NewRunnerContext", path: "skills/code_context_ripgrep")
  → Returns: Block showing initialization in main.go:94

Agent calls: search_pattern(pattern: "NewRunnerContext", path: "skills/graph_pagerank")
  → Returns: Block showing initialization in main.go:40

Agent thinks: "Now I understand the connection. Let me also check if there's any data flow."

Agent calls: get_graph_neighbors(node_id: "skills/code_context_ripgrep/main.go", direction: "out")
  → Returns: Dependencies list

Agent thinks: "I have enough context. Time to synthesize."

Agent calls: finish_codemap(codemap: { ... })
```

## Agent Instructions

The agent receives initial context and has tools to explore further before
producing the final codemap.

### System Prompt

```markdown
You are a code cartographer. Your job is to create semantic codemaps that help
developers understand how different parts of a codebase connect and interact.

## Your Task

Given a query and initial context about a codebase, explore as needed and
produce a codemap. The codemap should:

1. **Answer the query** - Focus on what the user asked about
2. **Show relationships** - How components connect, call each other, or share dependencies
3. **Provide navigation** - Every annotation must have an exact @file:line reference
4. **Explain clearly** - Each trace tells a coherent story about an execution path or relationship

## Initial Context You Receive

You start with three types of pre-gathered context:

### graph_context
- `nodes`: Files relevant to the query with their PageRank scores
- `edges`: Dependencies between files (imports, calls, references)
- `top_by_pagerank`: Most important files in the codebase
- `paths`: Shortest paths connecting query-related files

### symbol_context
- `symbols_by_file`: Function/class/type definitions per file with line numbers
- `imports_by_file`: What each file imports
- `shared_imports`: Packages imported by multiple query-related files
- `exported_apis`: Public interfaces

### pattern_context
- `matches_by_term`: Code blocks containing each query term
- `cross_references`: Where files related to term A mention term B
- `file_blocks`: Full function bodies containing matches

## Exploration Tools

You have tools to gather additional context. USE THEM when the initial context
is insufficient or when you discover something interesting to follow up on.

### When to use each tool:

| Tool | Use when... |
|------|-------------|
| `read_file` | You see a file path and need to understand its full contents |
| `search_pattern` | You found a function/type name and want to see where it's used |
| `get_symbols` | You want to understand the structure of a specific file |
| `get_graph_neighbors` | You want to explore what depends on or is depended by a file |
| `semantic_search` | You want to find conceptually related code that might not match patterns |
| `get_call_hierarchy` | You have a Go function and want precise call tracing (Go only) |
| `finish_codemap` | You have enough context and are ready to produce the final output |

### Exploration Depth

You are given a `depth` parameter that controls how deeply you should explore:

| Depth | Behavior | Use Case |
|-------|----------|----------|
| 1 | Use initial context only, minimal tool calls (0-2) | Quick overview |
| 2 | Follow immediate connections, verify key details (2-5 calls) | Standard query |
| 3 | Trace through intermediate files, explore shared deps (5-10 calls) | Detailed mapping |
| 4 | Deep exploration, follow call chains multiple levels (10-15 calls) | Complex relationships |
| 5 | Exhaustive exploration, leave no stone unturned (15+ calls) | Comprehensive audit |

**Respect the depth parameter.** If depth=1, synthesize quickly from initial context.
If depth=5, explore thoroughly before synthesizing.

### Exploration Strategy

1. **Review initial context** - Look at graph paths, cross-references, shared imports
2. **Identify gaps** - What's missing? Are there unclear connections?
3. **Follow interesting leads** - If you see a file mentioned but don't have its contents, read it
4. **Trace connections** - Use search_pattern to find how components actually interact
5. **Verify line numbers** - Before including @file:line, make sure you have the actual line
6. **Match depth to exploration** - More depth = more tool calls before synthesizing

### Example Exploration

Query: "map connections between authentication and database"

Initial context shows auth files import some db package. You might:
1. `get_symbols("internal/auth/service.go")` - understand auth structure
2. `search_pattern("dbPool|DBConn", path: "internal/auth")` - find actual db usage
3. `get_graph_neighbors("internal/db/pool.go", direction: "in")` - see what else uses db
4. `finish_codemap(...)` - synthesize findings

## How to Use Initial Context

1. **Start with graph_context.paths** to understand high-level connections
2. **Use symbol_context** to identify the specific functions/types involved
3. **Use pattern_context.cross_references** to find where components interact
4. **Use pattern_context.file_blocks** to get exact line numbers and code snippets
5. **Explore with tools** when you need more detail or find interesting leads

## Output Format

Produce a JSON codemap with this structure:

```json
{
  "title": "Short descriptive title",
  "description": "2-3 sentences summarizing what this codemap shows",
  "traces": [
    {
      "number": 1,
      "title": "Trace title (e.g., 'Shared Infrastructure')",
      "summary": "One sentence describing this trace",
      "tree": "ASCII tree diagram (see format below)",
      "annotations": [
        {
          "label": "1a",
          "title": "Short title (3-5 words)",
          "description": "One sentence explanation",
          "path": "@file/path.go:123"
        }
      ]
    }
  ]
}
```

### ASCII Tree Format

Use box-drawing characters for the tree:

```
file_a.go
└── function_a()  <-- 1a
    ├── calls file_b.go::function_b()  <-- 1b
    │   └── uses shared/util.go::helper()  <-- 1c
    └── also calls file_c.go::function_c()  <-- 1d
```

Rules:
- Every node in the tree MUST have a corresponding annotation
- Annotations use format: number + letter (1a, 1b, 2a, 2b, etc.)
- Number = trace number, letter = position in trace (a, b, c, ...)
- Include `::function_name()` when you know the specific function
- Use `<-- label` markers aligned to the right

## Important Guidelines

1. **Be selective** - Don't include everything. Focus on what answers the query.
2. **Verify line numbers** - Only use @file:line if you have it from the context.
   If you don't have exact line numbers, use @file without line number.
3. **Explain relationships** - Don't just list files. Explain HOW they connect.
4. **Multiple traces are OK** - Use separate traces for different aspects
   (e.g., "Shared Dependencies" vs "Direct Interactions")
5. **Handle missing data gracefully** - If some context is empty, work with
   what you have. Note limitations in the description if needed.
```

### Agent Invocation

```go
// CodemapAgent wraps the dspy-go agent with tool implementations
type CodemapAgent struct {
    workspace    string
    graphStore   *graph.SQLiteStore
    config       *config.Config
    finalCodemap json.RawMessage
}

// GenerateOptions configures codemap generation
type GenerateOptions struct {
    Query     string
    Workspace string
    Depth     int  // 1-5, controls exploration depth
}

// depthToMaxIterations maps depth to max tool calls
func depthToMaxIterations(depth int) int {
    switch depth {
    case 1:
        return 3
    case 2:
        return 6
    case 3:
        return 12
    case 4:
        return 18
    case 5:
        return 30
    default:
        return 6 // default to depth=2
    }
}

func generateCodemap(ctx context.Context, opts GenerateOptions) (*Codemap, error) {
    if opts.Depth < 1 {
        opts.Depth = 2 // default depth
    }
    if opts.Depth > 5 {
        opts.Depth = 5
    }

    // Gather initial context in parallel
    var graphCtx *GraphContext
    var symbolCtx *SymbolContext
    var patternCtx *PatternContext

    g, _ := errgroup.WithContext(ctx)
    g.Go(func() error {
        graphCtx, _ = gatherGraphContext(ctx, opts.Query, opts.Workspace)
        return nil
    })
    g.Go(func() error {
        symbolCtx, _ = gatherSymbolContext(ctx, opts.Query, opts.Workspace)
        return nil
    })
    g.Go(func() error {
        patternCtx, _ = gatherPatternContext(ctx, opts.Query, opts.Workspace)
        return nil
    })
    g.Wait()

    // Build initial context with depth parameter
    initialContext := map[string]any{
        "graph_context":   graphCtx,
        "symbol_context":  symbolCtx,
        "pattern_context": patternCtx,
        "depth":           opts.Depth,
    }

    // Create agent with exploration tools
    agent := &CodemapAgent{
        workspace: opts.Workspace,
    }

    dsAgent := dspy.NewAgent(dspy.AgentConfig{
        Model:         "claude-sonnet-4-20250514",
        SystemPrompt:  codemapSystemPrompt,
        Tools:         codemapTools,
        MaxIterations: depthToMaxIterations(opts.Depth),
    })

    // Run agent loop
    result, err := dsAgent.Run(ctx, dspy.AgentInput{
        Query:       opts.Query,
        Context:     initialContext,
        ToolHandler: agent.handleToolCall,
    })
    if err != nil {
        return nil, err
    }

    // Agent should have called finish_codemap
    if agent.finalCodemap == nil {
        return nil, fmt.Errorf("agent did not produce a codemap")
    }

    // Parse and validate codemap
    var codemap Codemap
    if err := json.Unmarshal(agent.finalCodemap, &codemap); err != nil {
        return nil, fmt.Errorf("invalid codemap output: %w", err)
    }

    codemap.ID = ulid.Make().String()
    codemap.Query = opts.Query
    codemap.Workspace = opts.Workspace
    codemap.CreatedAt = time.Now()

    // Count files and symbols mentioned
    codemap.FileCount = countUniqueFiles(codemap)
    codemap.SymbolCount = countSymbols(codemap)
    codemap.Terms = extractTerms(opts.Query)

    return &codemap, nil
}

// runSkill executes an foxctl skill and returns parsed output
func (a *CodemapAgent) runSkill(ctx context.Context, skillName string, input map[string]any) (any, error) {
    inputJSON, _ := json.Marshal(input)

    cmd := exec.CommandContext(ctx, "foxctl", "run", skillName, "--input", string(inputJSON))
    cmd.Dir = a.workspace

    output, err := cmd.Output()
    if err != nil {
        return nil, fmt.Errorf("skill %s failed: %w", skillName, err)
    }

    // Parse envelope response
    var env envelope.Envelope
    if err := json.Unmarshal(output, &env); err != nil {
        return nil, fmt.Errorf("parse envelope: %w", err)
    }

    return env.Data, nil
}

// readFile reads file contents with optional line range
func (a *CodemapAgent) readFile(ctx context.Context, path string, startLine, endLine int) (map[string]any, error) {
    fullPath := filepath.Join(a.workspace, path)

    content, err := os.ReadFile(fullPath)
    if err != nil {
        return nil, err
    }

    lines := strings.Split(string(content), "\n")

    // Apply line range if specified
    if startLine > 0 || endLine > 0 {
        if startLine < 1 {
            startLine = 1
        }
        if endLine < 1 || endLine > len(lines) {
            endLine = len(lines)
        }
        if startLine > len(lines) {
            startLine = len(lines)
        }
        lines = lines[startLine-1 : endLine]
    }

    return map[string]any{
        "path":       path,
        "content":    strings.Join(lines, "\n"),
        "line_count": len(lines),
        "start_line": startLine,
    }, nil
}
```

## Data Types

```go
// Codemap is the final output stored and retrieved via semantic search
type Codemap struct {
    ID          string    `json:"id"`
    Title       string    `json:"title"`
    Description string    `json:"description"`
    Query       string    `json:"query"`
    Workspace   string    `json:"workspace"`
    Traces      []Trace   `json:"traces"`
    CreatedAt   time.Time `json:"created_at"`

    // Metadata for search/filtering
    FileCount   int      `json:"file_count"`
    SymbolCount int      `json:"symbol_count"`
    Terms       []string `json:"terms"` // Extracted query terms
}

type Trace struct {
    Number      int          `json:"number"`
    Title       string       `json:"title"`
    Summary     string       `json:"summary"`
    Tree        string       `json:"tree"`
    Annotations []Annotation `json:"annotations"`
}

type Annotation struct {
    Label       string `json:"label"`       // "1a", "1b", "2a"
    Title       string `json:"title"`       // "Skill Discovery"
    Description string `json:"description"` // Full explanation
    Path        string `json:"path"`        // "@internal/skill/discovery.go:78"
}
```

## Storage & Retrieval

### Storing Codemaps

```go
func storeCodemap(ctx context.Context, codemap *Codemap) error {
    // Generate embedding from title + description
    embeddingText := codemap.Title + "\n" + codemap.Description
    embedding, _ := generateEmbedding(ctx, embeddingText)

    // Store in memory.db
    entry := memory.NamedEntry{
        ID:        codemap.ID,
        Name:      "codemap-" + codemap.ID,
        Type:      "codemap",
        Workspace: codemap.Workspace,
        Summary:   embeddingText,
        Result:    mustJSON(codemap),
        Embedding: embedding,
    }

    return memoryStore.Put(ctx, entry)
}
```

### Retrieving Codemaps

Add `"codemaps"` scope to `code/semantic_search`:

```go
func searchCodemaps(ctx context.Context, query string, limit int) ([]SearchResult, error) {
    embedding, _ := generateEmbedding(ctx, query)

    entries, _ := memoryStore.SearchSimilar(ctx, memory.SimilarityQuery{
        Embedding: embedding,
        Type:      "codemap",
        Limit:     limit,
    })

    var results []SearchResult
    for _, entry := range entries {
        var codemap Codemap
        json.Unmarshal(entry.Result, &codemap)
        results = append(results, SearchResult{
            Type:       "codemap",
            Title:      codemap.Title,
            Summary:    codemap.Description,
            Score:      entry.Score,
            Codemap:    &codemap,
        })
    }
    return results, nil
}
```

## Implementation Plan

### Phase 1: Context Gatherers
- [ ] Implement `gatherGraphContext()` using `internal/storage/graph`
  - [ ] Add `SearchNodes(workspace, term)` method to graph store
  - [ ] Add `GetEdgesBetween(workspace, nodeIDs)` method
  - [ ] Implement `findPathsBetweenClusters()` for shortest paths
- [ ] Implement `gatherSymbolContext()` using `code/symbols` skill
  - [ ] Add import extraction for Go, Python, JS/TS
  - [ ] Implement `findSharedImports()` across files
- [ ] Implement `gatherPatternContext()` using `code/context_ripgrep` skill
  - [ ] Cross-reference search (term A files mentioning term B)
- [ ] Add parallel execution with errgroup

### Phase 2: Agent Tools
- [ ] Implement tool handlers in `CodemapAgent`
  - [ ] `read_file` - file reading with line ranges
  - [ ] `search_pattern` - wraps `code/context_ripgrep`
  - [ ] `get_symbols` - wraps `code/symbols`
  - [ ] `get_graph_neighbors` - wraps graph store
  - [ ] `semantic_search` - wraps `code/semantic_search`
  - [ ] `get_call_hierarchy` - wraps `lsp/gopls` (Go only)
  - [ ] `finish_codemap` - captures final output
- [ ] Define tool schemas in dspy-go format

### Phase 3: Agent Setup
- [ ] Create agent system prompt (as documented above)
- [ ] Set up dspy-go agent with Claude
- [ ] Configure max iterations based on depth parameter (3-30)
- [ ] Implement agent loop with tool handling
- [ ] Add JSON output parsing and validation

### Phase 4: Skill Implementation
- [ ] Create `codemap/generate` skill
- [ ] Add input schema: `{ "query": string, "workspace"?: string, "depth"?: int }`
- [ ] Add output validation for Codemap type
- [ ] Wire up context gatherers + agent invocation

### Phase 5: Storage Integration
- [ ] Add `type="codemap"` to memory store queries
- [ ] Implement embedding generation for codemap summaries
- [ ] Add `"codemaps"` scope to `code/semantic_search`
- [ ] Implement `storeCodemap()` and retrieval

### Phase 6: CLI
- [ ] `foxctl codemap generate "query" [--depth 1-5]` - Generate new codemap
  - Default depth=2, use --depth 5 for exhaustive exploration
- [ ] `foxctl codemap list` - List stored codemaps
- [ ] `foxctl codemap show <id>` - Display formatted codemap
- [ ] `foxctl codemap delete <id>` - Remove codemap

### Phase 7: Testing
- [ ] Unit tests for context gatherers
- [ ] Integration test with mock agent
- [ ] Golden tests for codemap output format
- [ ] End-to-end test on foxctl codebase itself
