# Doc Comment Snippets: A Copy/Paste Cookbook

This document provides ready-to-use GoDoc comment templates with Index: blocks
for improved semantic search and code navigation.

Note: Examples are illustrative. Adjust symbol/package names to match actual APIs. Avoid placeholder types unless they exist in the repo.

---

## 1. Internal Package Doc (`doc.go`)

For subsystem boundaries. Describes the package's role and key types.

**Example: Package retrieval**

```go
// Package retrieval implements vector-based code search and retrieval.
//
// It provides embedding generation, similarity search, and result ranking
// for code symbols, memories, and documentation. The package bridges
// raw embedding providers with the semantic search skill layer.
//
// # Key Types
//
//   - [Embedder] generates vector embeddings from text
//   - [Index] stores and queries embedded vectors
//   - [Ranker] scores and filters search results
//
// # Usage
//
//	embedder := retrieval.NewEmbedder(voyageClient)
//	index := retrieval.OpenIndex(ctx, dbPath)
//	results, err := index.Search(ctx, embedder.Embed(query), 10)
//
// Index: semantic search, vector embeddings, similarity search, code retrieval
package retrieval
```

---

## 2. Skill Package Doc (`skills/<name>/main.go`)

Include the command string and output keys for discoverability.

**Example: code/smart_write**

```go
// Skill code/smart_write performs symbol-aware code editing.
//
// Given a file path, symbol name, and new content, it locates the symbol
// (function, type, method) and replaces its entire body while preserving
// surrounding code. Falls back to append mode if the symbol doesn't exist.
//
// # Command
//
//	foxctl run code/smart_write --input '{"path":"...", "symbol":"...", "content":"..."}'
//
// # Input
//
//	{
//	  "path": "/abs/path/to/file.go",     // Required: target file
//	  "symbol": "ProcessItem",             // Required: function/type name
//	  "content": "func ProcessItem() {}", // Required: new implementation
//	  "mode": "replace"                    // Optional: replace|append|insert
//	}
//
// # Output
//
//	{
//	  "success": true,
//	  "path": "/abs/path/to/file.go",
//	  "symbol": "ProcessItem",
//	  "action": "replaced",
//	  "lines_changed": 15
//	}
//
// # Output Keys
//
//	success       - whether the write succeeded
//	action        - replaced|appended|inserted|created
//	lines_changed - number of lines modified
//
// Index: smart write, symbol editing, code modification, function replacement
package main
```

---

## 3. Exported Type (API Surface)

Concise with Index: for hub types that others need to discover.

**Example: Worker struct**

```go
// Worker processes background tasks with controlled concurrency.
//
// It manages a pool of goroutines that pull from a task queue,
// execute handlers, and report results. Workers are safe for
// concurrent use and support graceful shutdown.
//
// # Example
//
//	w := NewWorker(ctx, WorkerConfig{
//	    Concurrency: 4,
//	    QueueSize:   100,
//	})
//	w.Submit(task)
//	w.Wait()
//
// Index: background processing, task queue, worker pool, concurrency
type Worker struct {
	tasks   chan Task
	results chan Result
	wg      sync.WaitGroup
	cancel  context.CancelFunc
}
```

---

## 4. Exported Interface

Keep minimal. List what implementers must provide.

**Example: EmbeddingProvider**

```go
// EmbeddingProvider generates vector embeddings from text.
//
// Implementations must be safe for concurrent use. The returned
// vectors should be normalized to unit length for cosine similarity.
//
// Index: embeddings, vector generation, voyage, openai
type EmbeddingProvider interface {
	// Embed converts text into a vector embedding.
	// Returns a normalized float32 slice of the provider's dimension.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch converts multiple texts in a single API call.
	// More efficient than calling Embed repeatedly.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the vector dimension (e.g., 1024 for voyage-code-3).
	Dimensions() int
}
```

**Example: QueryEmbeddingProvider (extends EmbeddingProvider)**

```go
// QueryEmbeddingProvider extends [EmbeddingProvider] with query-specific embedding.
//
// Some providers (e.g., Voyage) use different models or parameters for
// queries vs documents. This interface allows optimizing query embeddings
// for retrieval accuracy.
//
// Index: query embedding, asymmetric search, voyage query
type QueryEmbeddingProvider interface {
	EmbeddingProvider

	// EmbedQuery generates a query-optimized embedding.
	// May use different model or parameters than document embedding.
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}
```

---

## 5. Orchestrator Function (Hub)

Where Index: pays off most. These are the "where does X happen?" functions.

**Example: Generator.Generate**

```go
// Generate produces a codemap tracing relationships from a query.
//
// It performs multi-step retrieval:
//  1. Semantic search to find seed symbols matching the query
//  2. Graph expansion to discover callers, callees, and references
//  3. LLM-guided pruning to keep only relevant relationships
//  4. Markdown rendering with linked source locations
//
// The result is a navigable map showing how code flows through
// the codebase, suitable for understanding feature implementations.
//
// # Example
//
//	gen := NewGenerator(cfg)
//	codemap, err := gen.Generate(ctx, "authentication middleware")
//	// codemap.Markdown contains the rendered map
//	// codemap.Nodes contains structured graph data
//
// Index: codemap generation, code tracing, relationship mapping,
// graph expansion, LLM pruning, feature understanding
func (g *Generator) Generate(ctx context.Context, query string) (*Codemap, error) {
	// ...
}
```

---

## 6. Store Method with Transaction Boundary

Explicit about atomicity and what gets persisted together.

**Example: Store.Complete**

```go
// Complete marks a task as done and records its outcome.
//
// This is a transactional operation that atomically:
//  1. Updates the task status to "completed"
//  2. Sets the completion timestamp
//  3. Stores the result summary
//  4. Decrements the parent's pending_children count (if any)
//  5. Emits a task.completed event
//
// If the parent has no remaining pending children after this call,
// it triggers parent completion recursively.
//
// Returns [ErrTaskNotFound] if the task doesn't exist, or
// [ErrAlreadyCompleted] if already in a terminal state.
//
// Index: task completion, transaction boundary, parent rollup,
// status update, atomic state change
func (s *Store) Complete(ctx context.Context, taskID string, result TaskResult) error {
	return s.db.Transaction(func(tx *sql.Tx) error {
		// ...
	})
}
```

---

## 7. Indexer Entrypoint

Navigation hubs that kick off batch operations.

**Example: Indexer.Index**

```go
// Index builds or updates the semantic index for a workspace.
//
// It scans the workspace for indexable files, extracts symbols,
// generates embeddings, and stores them in the vector database.
// Incremental indexing skips files unchanged since last run.
//
// # Phases
//
//  1. Discovery: glob patterns filter files to index
//  2. Parsing: extract symbols (functions, types, methods)
//  3. Chunking: split large symbols for embedding limits
//  4. Embedding: batch API calls to embedding provider
//  5. Storage: upsert vectors with metadata
//
// Progress is reported via the [ProgressReporter] if provided.
// Returns summary statistics including files processed and errors.
//
// Index: semantic indexing, symbol extraction, embedding generation,
// incremental update, workspace scan, batch embedding
func (idx *Indexer) Index(ctx context.Context, workspace string, opts IndexOptions) (*IndexResult, error) {
	// ...
}
```

---

## 8. CLI Command Constructor

Short with "why" and related commands for navigation.

**Example: newSemanticIndexCommand**

```go
// newSemanticIndexCommand creates the "foxctl index semantic" command.
//
// This command builds vector embeddings for code symbols, enabling
// semantic search via "foxctl run code/semantic_search".
//
// # Related Commands
//
//	foxctl index status     - check index freshness
//	foxctl index init       - initialize all index types
//	foxctl index repo build - build call graph (different from semantic)
//
// Index: CLI command, semantic index, embedding build
func newSemanticIndexCommand() *cobra.Command {
	// ...
}
```

---

## 9. Repo Graph Store Workhorse

For future repoindex operations that modify the graph.

**Example: ReplacePackageGraph**

```go
// ReplacePackageGraph atomically replaces all nodes and edges for a package.
//
// This is the main write path for incremental graph updates. It:
//  1. Deletes all existing nodes with the given package path
//  2. Deletes edges where either endpoint was in that package
//  3. Inserts new nodes from the parsed AST
//  4. Inserts new edges (CALLS, REFERS_TO, IMPORTS)
//  5. Updates the package metadata (last_indexed, file_count)
//
// The entire operation runs in a single transaction. On conflict,
// the transaction rolls back and returns an error.
//
// This function is safe to call concurrently for different packages
// but will serialize calls for the same package via row locking.
//
// Index: graph replacement, package update, transaction boundary,
// node insertion, edge insertion, incremental indexing
func (s *Store) ReplacePackageGraph(ctx context.Context, pkg *ParsedPackage) error {
	return s.db.Transaction(func(tx *sql.Tx) error {
		// ...
	})
}
```

---

## 10. Minimal Non-Hub Function

Don't overdo Index: for simple helpers.

**Example: mapMode helper**

```go
// mapMode converts a string mode to the internal Mode constant.
// Returns ModeReplace if the input is empty or unrecognized.
func mapMode(s string) Mode {
	switch strings.ToLower(s) {
	case "append":
		return ModeAppend
	case "insert":
		return ModeInsert
	default:
		return ModeReplace
	}
}
```

No Index: block needed. This is a leaf utility that:
- Has no callers worth discovering
- Is obvious from its name
- Won't be a search target

---

## Quick Rules: When to Include an Index: Block

### Add Index: for

| Category | Examples | Why |
|----------|----------|-----|
| **Skill `run()` functions** | `code/semantic_search`, `todo/manage` | Entry points users invoke |
| **Indexers** | `Indexer.Index`, `BuildRepoGraph` | "How do I rebuild X?" |
| **Workers/Supervisors** | `Worker.Process`, `Supervisor.Start` | Background processing hubs |
| **Store transaction boundaries** | `Store.Complete`, `Store.CreateWithChildren` | "What gets persisted together?" |
| **Orchestrators** | `Generator.Generate`, `Processor.Process` | Multi-step coordination |
| **"Where does X happen?" functions** | `handleWebhook`, `dispatchEvent` | Discovery targets |
| **Package docs** | `doc.go` files | Subsystem boundaries |
| **Key exported types** | `Worker`, `Index`, `Generator` | API surface |

### Skip Index: for

| Category | Examples | Why |
|----------|----------|-----|
| **Tiny helpers** | `mapMode`, `trimPrefix` | Not worth indexing overhead |
| **Obvious getters/setters** | `GetID()`, `SetName()` | Name is self-documenting |
| **Leaf utilities** | `formatDuration`, `parseJSON` | No discovery value |
| **Private functions** | `doThing`, `helperFunc` | Internal implementation |
| **Test functions** | `TestFoo`, `BenchmarkBar` | Test discovery uses different patterns |

### Index Block Formats

**Structured (canonical):**

```go
// Index:
//   Purpose: Describe the symbol intent in one sentence
//   Related: RelatedSymbol, OtherSymbol
//   Keywords: term1, term2, multi-word term
```

**Keywords-only shorthand (optional):**

```go
// Index: term1, term2, multi-word term, term4
```

Rules:
- Structured fields create edges (Related/Flow/Observability/etc.)
- Shorthand is keywords-only (no Related/Flow edges)
- Keep Index as the last block in the doc comment
- Include synonyms users might search for

---

## Appendix: Real-World Example - Complete Skill with Index: Blocks

This complete skill implementation demonstrates all the patterns above applied consistently.

```go
// Package main implements the build/godot skill for exporting and building Godot projects.
// Provides export preset management, project export, and C# build operations.
package main

import (
    "bufio"
    "context"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "time"

    "github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
    "github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
    "github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
    "github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
    errs "github.com/joshka0/foxctl/internal/platform/errors"
)

const skillName = "build/godot"

// Action constants.
const (
    ActionListPresets = "list_presets"
    ActionExport      = "export"
    ActionValidate    = "validate"
    ActionBuild       = "build"
    ActionRestore     = "restore"
    ActionClean       = "clean"
)

// Input represents the skill input parameters for build/godot operations.
type Input struct {
    Action      string `json:"action"`
    Preset      string `json:"preset"`
    OutputPath  string `json:"output_path"`
    Debug       bool   `json:"debug"`
    GodotPath   string `json:"godot_path"`
    PackOnly    bool   `json:"pack_only"`
    ExportDebug bool   `json:"export_debug"`

    // C# build parameters
    Configuration string `json:"configuration"` // Debug or Release
    Verbosity     string `json:"verbosity"`     // quiet, minimal, normal, detailed, diagnostic
    Target        string `json:"target"`        // Build target (e.g., Build, Rebuild, Clean)
    DotnetPath    string `json:"dotnet_path"`   // Path to dotnet executable
}

// ExportPreset represents a parsed export preset configuration.
type ExportPreset struct {
    Name       string `json:"name"`
    Platform   string `json:"platform"`
    ExportPath string `json:"export_path,omitempty"`
    Runnable   bool   `json:"runnable"`
}

// main is the skill entry point for build/godot.
func main() {
    skillmain.Main(skillName, run)
}

// run orchestrates build/godot operations based on the specified action.
//
// Index:
// - Purpose: Execute Godot project operations including export, build, and preset management
// - Flow: validate action → apply defaults → route to action handler (listPresets/exportProject/buildCSharp/restoreCSharp/cleanCSharp)
// - SideEffects: file system operations (exports, builds, directory creation); external process execution (godot, dotnet)
// - FailureModes: missing export_presets.cfg, invalid preset, godot/dotnet not found, build failures, I/O errors
// - Observability: emits action-specific results (presets/count, export results, build results)
// - Related: listPresets, exportProject, buildCSharp, restoreCSharp, cleanCSharp, emitSuccess, executil.Run
// - Keywords: build/godot, action, preset, export, build, list_presets, validate, restore, clean
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
    // Validate
    if strings.TrimSpace(in.Action) == "" {
        return skillerr.Arg(
            "action is required",
            skillerr.WithHint("Provide action=list_presets, export, validate, build, restore, or clean."),
        )
    }
    // Apply defaults
    if in.GodotPath == "" {
        in.GodotPath = "godot"
    }

    workspace := rc.PathValidator.Workspace()

    switch in.Action {
    case ActionListPresets:
        return listPresets(ctx, rc, workspace)
    case ActionExport:
        return exportProject(ctx, rc, workspace, in, false)
    case ActionValidate:
        return exportProject(ctx, rc, workspace, in, true)
    case ActionBuild:
        return buildCSharp(ctx, rc, workspace, in)
    case ActionRestore:
        return restoreCSharp(ctx, rc, workspace, in)
    case ActionClean:
        return cleanCSharp(ctx, rc, workspace, in)
    default:
        return skillerr.Arg(
            fmt.Sprintf("unknown action: %q", in.Action),
            skillerr.WithHint("Valid actions: list_presets, export, validate, build, restore, clean."),
        )
    }
}

// listPresets parses and returns available export presets from export_presets.cfg.
//
// Index:
// - Purpose: List all available export presets for the Godot project
// - Flow: parse export_presets.cfg → return preset list with count
// - SideEffects: file read (export_presets.cfg)
// - FailureModes: missing export_presets.cfg, file parse errors
// - Observability: emits action/presets/count/summary
// - Related: parseExportPresets, emitSuccess
// - Keywords: list_presets, export_presets.cfg, presets, count, parseExportPresets
func listPresets(ctx context.Context, rc *skillmain.RunContext, workspace string) error {
    presets, err := parseExportPresets(workspace)
    if err != nil {
        return err
    }

    // Ensure presets is not nil for proper JSON serialization ([] instead of null)
    if presets == nil {
        presets = []ExportPreset{}
    }

    result := map[string]any{
        "action":  ActionListPresets,
        "presets": presets,
        "count":   len(presets),
        "summary": fmt.Sprintf("Found %d export preset(s)", len(presets)),
    }

    return emitSuccess(rc, result)
}

// exportProject exports the Godot project using the specified preset.
//
// Index:
// - Purpose: Export Godot project to target platform using preset configuration
// - Flow: validate preset → parse presets → match preset → resolve output path → create output dir → export via godot CLI
// - SideEffects: file system writes (exported files); external process execution (godot)
// - FailureModes: invalid preset, missing export path, godot execution failures, permission errors
// - Observability: emits action/preset/platform/output_path/output_exists/exit_code/duration_ms/stdout/stderr/output_size_bytes/summary
// - Related: parseExportPresets, emitSuccess, executil.Run
// - Keywords: export, preset, platform, output_path, godot, export_debug, pack_only, dry_run
func exportProject(ctx context.Context, rc *skillmain.RunContext, workspace string, in Input, dryRun bool) error {
    // ... implementation ...
}

// runDotnet executes a dotnet command (build/restore/clean) on the project.
//
// Index:
// - Purpose: Execute dotnet commands for C# Godot project management
// - Flow: find .csproj file → build dotnet args → execute dotnet command → emit results
// - SideEffects: external process execution (dotnet); file system operations (build outputs)
// - FailureModes: missing .csproj file, dotnet not found, build failures
// - Observability: emits action/csproj/exit_code/duration_ms/stdout/stderr/configuration/summary
// - Related: findCsproj, emitSuccess, executil.Run
// - Keywords: dotnet, build, restore, clean, csproj, configuration, verbosity, target
func runDotnet(ctx context.Context, rc *skillmain.RunContext, workspace string, in Input, command string) error {
    // ... implementation ...
}

// parseExportPresets parses the export_presets.cfg file into ExportPreset structs.
// NOTE: No Index: block - this is a leaf helper function.
func parseExportPresets(workspace string) ([]ExportPreset, error) {
    // ... implementation ...
}

// findCsproj locates the .csproj file in the workspace directory.
// NOTE: No Index: block - simple file lookup helper.
func findCsproj(workspace string) (string, error) {
    // ... implementation ...
}

// emitSuccess emits a successful result with the provided data.
// NOTE: No Index: block - trivial wrapper.
func emitSuccess(rc *skillmain.RunContext, result map[string]any) error {
    return skillout.Emit(rc, skillName, result)
}
```

### Key Observations

1. **Package doc** - Brief, states the skill name and what it does
2. **Main orchestrator (`run`)** - Full Index: block with all 7 fields
3. **Action handlers** - Each has an Index: block because they're navigation targets
4. **Helper functions** - No Index: blocks (`parseExportPresets`, `findCsproj`, `emitSuccess`)
5. **Keywords include** - Skill name (`build/godot`), action names, output field names
6. **Related field** - Points to other functions in the same file and external dependencies
