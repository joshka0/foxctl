# Skillslib Consolidation & Refactoring Plan

## Executive Summary

This plan consolidates the skill SDK foundation by eliminating duplication, fixing critical bugs, and creating high-leverage helpers that will reduce skill boilerplate by 40-60%. The refactoring enables clean implementation of `smart_read_file` and `code_counsel` skills.

**Estimated Scope:** ~50 files modified, ~2000 lines removed, ~800 lines added

---

## Phase 0: Critical Bug Fixes (Must Do First)

These issues affect correctness and must be fixed before any refactoring.

### 0.1 Fix `IsUnderWorkspace` Logic Bug

**Problem:** The skillslib version (`internal/adapters/skillslib/workspace/workspace.go:75-91`) uses:
```go
return len(rel) > 0 && rel[0] != '.'
```
This incorrectly rejects `.gitignore`, `.env`, and any dotfile even when inside workspace.

**Fix Location:** `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/workspace/workspace.go`

**Correct Logic:**
```go
func IsUnderWorkspace(workspace, path string) bool {
    absPath, err := filepath.Abs(path)
    if err != nil {
        return false
    }
    absWorkspace, err := filepath.Abs(workspace)
    if err != nil {
        return false
    }
    rel, err := filepath.Rel(absWorkspace, absPath)
    if err != nil {
        return false
    }
    // Path is under workspace if it doesn't escape via ..
    if rel == "." {
        return true // Workspace itself
    }
    if rel == ".." {
        return false
    }
    if strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
        return false
    }
    return true
}
```

**Also fix:** Consolidate with `internal/runtime/hooks/pathutil/extract.go:157-174` which has different parameter order. Pick one canonical location.

**Tests to add:**
- `.gitignore` under workspace → true
- `.env` under workspace → true
- `../outside` → false
- workspace itself → true

**Dependencies:** None
**Risk:** Low - function is currently unused in production

---

### 0.2 Fix CAS Pinning Race Condition

**Problem:** In `internal/runtime/runservice/result.go`, the order is:
1. `handleArtifacts()` - pins digests found in original result
2. `enforceOutputLimit()` - may create NEW CAS object for truncated output
3. The new digest from step 2 is NOT pinned

If CAS GC runs, the truncated output reference becomes dangling.

**Fix Location:** `~/repos/personal/claude-migration-to-v2/internal/runtime/runservice/result.go`

**Solution Options:**

**Option A (Recommended):** Run `enforceOutputLimit` BEFORE `handleArtifacts`
```go
func (e *Executor) HandleResult(jobID string, result []byte) error {
    // Step 1: TRUNCATE first (may create new CAS object)
    result = e.enforceOutputLimit(e.ctx, result, jobID)

    // Step 2: PIN all artifacts (now includes truncated output digest)
    if err := e.handleArtifacts(jobID, result); err != nil {
        // ... handle error
    }
    // ... rest unchanged
}
```

**Option B:** Have `enforceOutputLimit` return extra digests to pin
```go
func (e *Executor) enforceOutputLimit(...) ([]byte, []string) {
    // ... if truncated, return wrapper + []string{obj.Digest}
}
```

**Tests to add:**
- Large output → truncated → digest is pinned
- Verify `artifacts.Digests()` extracts the wrapper's digest

**Dependencies:** None
**Risk:** Medium - affects all skill outputs >32KB

---

### 0.3 Fix `RunContext.Close()` to Close CAS

**Problem:** Both `skillmain.RunContext.Close()` and `runner.RunnerContext.Close()` are no-ops but CAS stores may need cleanup.

**Fix Locations:**
- `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/skillmain/context.go`
- `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/runner/context.go`

**Solution:**
```go
func (rc *RunContext) Close() error {
    if rc.CAS != nil {
        return rc.CAS.Close()
    }
    return nil
}
```

**Dependencies:** None
**Risk:** Low - currently no-op, adding proper cleanup

---

## Phase 1: Consolidate Duplicate Helpers

### 1.1 Unify RunContext Types

**Problem:** Two nearly identical context types exist:
- `skillmain.RunContext` (newer, has Logger + Validator)
- `runner.RunnerContext` (older, used in tests)

**File Locations:**
- `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/skillmain/context.go`
- `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/runner/context.go`

**Solution:** Make `runner.RunnerContext` embed or alias `skillmain.RunContext`

```go
// runner/context.go
package runner

import "github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"

// RunnerContext wraps skillmain.RunContext for backward compatibility.
// Deprecated: Use skillmain.RunContext directly.
type RunnerContext = skillmain.RunContext

// NewRunnerContext creates a RunContext (deprecated wrapper).
func NewRunnerContext(cfg config.Config, stdout io.Writer) (*RunnerContext, error) {
    return skillmain.BuildRunContext(cfg, stdout)
}
```

**Migration Steps:**
1. Add type alias
2. Update `NewRunnerContext` to delegate to `skillmain.BuildRunContext`
3. Mark as deprecated
4. Migrate tests over time

**Dependencies:** Phase 0.3 (Close fix)
**Risk:** Medium - many tests use RunnerContext

---

### 1.2 Consolidate Preview Helpers

**Problem:** Preview logic exists in two places:
- `internal/adapters/skillslib/preview.go` - simple `PreparePreview[T]`
- `internal/adapters/skillslib/skillout/preview.go` - rich version with NDJSON, PreviewResult, etc.

**Solution:** Delete `skillslib/preview.go`, keep only `skillout/preview.go`

**Files to modify:**
- DELETE: `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/preview.go`
- DELETE: `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/preview_test.go`
- KEEP: `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/skillout/preview.go`

**Update imports in skills that use `skillslib.PreparePreview`:**
```go
// Before
import "github.com/jkatigb/agentctl/internal/adapters/skillslib"
preview, truncated := skillslib.PreparePreview(items, limit)

// After
import "github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
preview, truncated := skillout.PreparePreview(items, limit)
```

**Dependencies:** None
**Risk:** Low - simple import change

---

### 1.3 Consolidate CAS Persistence Helpers

**Problem:** `Artifact`, `PersistJSON`, `PersistBuffer`, `BuildCASHint` are duplicated in:
- `skillmain/context.go` (lines 82-108)
- `skillout/emit.go` (lines 38-92)
- `runner/context.go` (lines 151-264)

**Solution:** Keep ONLY in `skillout`, remove from `skillmain` and `runner`

**Canonical Location:** `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/skillout/emit.go`

**Changes:**

1. **skillout/emit.go** - Keep and enhance:
```go
// Artifact represents a CAS-stored object.
type Artifact struct {
    Digest string `json:"digest"`
    Size   int64  `json:"size"`
    Kind   string `json:"kind"`
}

// PersistJSON marshals value to JSON and stores in CAS.
func PersistJSON(ctx context.Context, rc *skillmain.RunContext, value any, tags ...string) (Artifact, error)

// PersistBuffer stores buffer contents in CAS.
func PersistBuffer(ctx context.Context, rc *skillmain.RunContext, buf *bytes.Buffer, kind string, tags ...string) (Artifact, error)

// BuildCASHint creates a user-friendly CAS retrieval hint.
func BuildCASHint(artifact Artifact, linesPerPage int) envelope.CASHint
```

2. **skillmain/context.go** - Remove Artifact, PersistJSON, PersistBuffer (lines 82-108)

3. **runner/context.go** - Remove Artifact, PersistJSON, PersistBuffer, BuildCASHint (lines 151-264)

**Update skill imports:**
```go
// Before (using skillmain)
artifact, err := skillmain.PersistJSON(ctx, rc, data, "tag")

// After
artifact, err := skillout.PersistJSON(ctx, rc, data, "tag")
hint := skillout.BuildCASHint(artifact, 50)
```

**Dependencies:** Phase 1.1 (RunContext unification)
**Risk:** Medium - many skills use these helpers

---

## Phase 2: Create New High-Leverage Helpers

### 2.1 Add `skillout.PreviewAndPersistNDJSON` Helper

**Location:** `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/skillout/preview.go`

**New Functions:**
```go
// PreviewArtifact holds preview data and optional CAS artifact reference.
type PreviewArtifact struct {
    Preview   any      // Truncated slice for inline display
    Truncated bool     // Whether full data was truncated
    Artifact  Artifact // CAS reference (empty if not persisted)
}

// PreviewAndPersistNDJSON truncates items for preview and optionally persists full data as NDJSON.
// If persist=true OR truncation occurs, full data is stored in CAS.
// Returns preview slice, truncation status, and artifact (if persisted).
func PreviewAndPersistNDJSON[T any](
    ctx context.Context,
    rc *skillmain.RunContext,
    items []T,
    previewLimit int,
    artifactName string,
    persist bool,
) (PreviewArtifact, error) {
    preview, truncated := PreparePreview(items, previewLimit)

    result := PreviewArtifact{
        Preview:   preview,
        Truncated: truncated,
    }

    // Persist if explicitly requested or if truncated
    if (persist || truncated) && !rc.NoCAS && len(items) > 0 {
        var buf bytes.Buffer
        enc := json.NewEncoder(&buf)
        for _, item := range items {
            if err := enc.Encode(item); err != nil {
                return result, fmt.Errorf("encode item: %w", err)
            }
        }

        artifact, err := PersistBuffer(ctx, rc, &buf, "application/x-ndjson", artifactName)
        if err != nil {
            return result, fmt.Errorf("persist ndjson: %w", err)
        }
        result.Artifact = artifact
    }

    return result, nil
}

// PersistNDJSON stores items as NDJSON in CAS without preview logic.
func PersistNDJSON[T any](
    ctx context.Context,
    rc *skillmain.RunContext,
    items []T,
    artifactName string,
) (Artifact, error) {
    var buf bytes.Buffer
    enc := json.NewEncoder(&buf)
    for _, item := range items {
        if err := enc.Encode(item); err != nil {
            return Artifact{}, fmt.Errorf("encode item: %w", err)
        }
    }
    return PersistBuffer(ctx, rc, &buf, "application/x-ndjson", artifactName)
}
```

**Skills that will benefit:**
- `code/context_ripgrep` - delete `prepareBlockPreview`, `persistBlocksArtifact`
- `fs/find` - delete `preparePreview`, `persistResultsArtifact`
- `code/symbols` - simplify artifact persistence
- `code/complexity` - simplify artifact persistence

**Dependencies:** Phase 1.3 (CAS helpers consolidated)
**Risk:** Low - additive change

---

### 2.2 Add `skillslib/pathutil` Package

**Location:** `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/pathutil/pathutil.go`

**New Package:**
```go
package pathutil

import (
    "path/filepath"
    "strings"

    "github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
)

// ResolveSearchPath validates and resolves a path for searching.
// Returns workspace root and validated search path.
func ResolveSearchPath(rc *skillmain.RunContext, candidate string) (workspace, searchPath string, err error) {
    workspace = rc.PathValidator.Workspace()

    if candidate == "" {
        return workspace, workspace, nil
    }

    validated, err := rc.PathValidator.ValidatePath(candidate)
    if err != nil {
        return "", "", err
    }

    return workspace, validated, nil
}

// RelTo returns the relative path from workspace to absPath.
// If absPath is not under workspace, returns absPath unchanged.
func RelTo(workspace, absPath string) string {
    rel, err := filepath.Rel(workspace, absPath)
    if err != nil {
        return absPath
    }
    if strings.HasPrefix(rel, "..") {
        return absPath
    }
    return rel
}

// IsUnderWorkspace checks if path is under workspace root.
// Handles dotfiles correctly (unlike the buggy skillslib/workspace version).
func IsUnderWorkspace(workspace, path string) bool {
    absPath, err := filepath.Abs(path)
    if err != nil {
        return false
    }
    absWorkspace, err := filepath.Abs(workspace)
    if err != nil {
        return false
    }
    rel, err := filepath.Rel(absWorkspace, absPath)
    if err != nil {
        return false
    }
    if rel == "." {
        return true
    }
    if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
        return false
    }
    return true
}
```

**Skills that will benefit:**
- `code/context_ripgrep` - delete `relativeTo`, `resolveWorkspace`
- `fs/find` - delete local `relativeTo`
- `html_edit` - delete local path helpers

**Dependencies:** Phase 0.1 (IsUnderWorkspace fix)
**Risk:** Low - additive change

---

### 2.3 Add `skillslib/executil` Package

**Location:** `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/executil/executil.go`

**New Package:**
```go
package executil

import (
    "bytes"
    "context"
    "fmt"
    "os/exec"
)

// CmdResult holds command execution results.
type CmdResult struct {
    Stdout   []byte
    Stderr   []byte
    ExitCode int
}

// RequireTool checks if a tool exists in PATH.
// Returns the resolved path or an error with install hint.
func RequireTool(name, installHint string) (string, error) {
    path, err := exec.LookPath(name)
    if err != nil {
        if installHint != "" {
            return "", fmt.Errorf("%s not found in PATH. Install: %s", name, installHint)
        }
        return "", fmt.Errorf("%s not found in PATH", name)
    }
    return path, nil
}

// Run executes a command and returns stdout, stderr, and exit code.
func Run(ctx context.Context, dir string, name string, args ...string) (CmdResult, error) {
    cmd := exec.CommandContext(ctx, name, args...)
    cmd.Dir = dir

    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    err := cmd.Run()

    result := CmdResult{
        Stdout:   stdout.Bytes(),
        Stderr:   stderr.Bytes(),
        ExitCode: 0,
    }

    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            result.ExitCode = exitErr.ExitCode()
        } else {
            return result, err
        }
    }

    return result, nil
}

// ExitCode extracts exit code from error (0 if not an ExitError).
func ExitCode(err error) int {
    if err == nil {
        return 0
    }
    if exitErr, ok := err.(*exec.ExitError); ok {
        return exitErr.ExitCode()
    }
    return -1
}

// IsNoMatch returns true if exit code indicates "no matches" (common for grep/rg).
func IsNoMatch(result CmdResult) bool {
    return result.ExitCode == 1 && len(result.Stderr) == 0
}
```

**Skills that will benefit:**
- `code/context_ripgrep` - standardize rg execution
- `text/ripgrep` - standardize rg execution
- `git/worktree` - standardize git execution
- `codemap/check` - standardize git calls

**Dependencies:** None
**Risk:** Low - additive change

---

### 2.4 Add `skillslib/diffutil` Package

**Location:** `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/diffutil/diffutil.go`

**New Package:**
```go
package diffutil

import (
    "path/filepath"

    "github.com/pmezard/go-difflib/difflib"
)

// UnifiedDiff generates a unified diff between original and modified content.
func UnifiedDiff(fromFile, toFile, original, modified string, contextLines int) (string, error) {
    diff := difflib.UnifiedDiff{
        A:        difflib.SplitLines(original),
        B:        difflib.SplitLines(modified),
        FromFile: "a/" + filepath.Base(fromFile),
        ToFile:   "b/" + filepath.Base(toFile),
        Context:  contextLines,
    }
    return difflib.GetUnifiedDiffString(diff)
}

// UnifiedDiffWithPaths generates a unified diff with full paths in header.
func UnifiedDiffWithPaths(fromPath, toPath, original, modified string, contextLines int) (string, error) {
    diff := difflib.UnifiedDiff{
        A:        difflib.SplitLines(original),
        B:        difflib.SplitLines(modified),
        FromFile: fromPath,
        ToFile:   toPath,
        Context:  contextLines,
    }
    return difflib.GetUnifiedDiffString(diff)
}
```

**Skills that will benefit:**
- `html_edit` - delete local `generateUnifiedDiff` (lines 606-620)
- `code/smart_write` - delete local `generateUnifiedDiff` (lines 269-283)
- `fs/apply_edit` - delete massive custom diff generator (lines 430-528)

**Dependencies:** None
**Risk:** Low - additive change, uses proven go-difflib

---

### 2.5 Add `skillslib/oputil` Package

**Location:** `~/repos/personal/claude-migration-to-v2/internal/adapters/skillslib/oputil/oputil.go`

**New Package:**
```go
package oputil

import (
    "fmt"
    "strings"

    "github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
)

// Op normalizes an operation string (lowercase, trimmed).
func Op(in string) string {
    return strings.ToLower(strings.TrimSpace(in))
}

// Require returns an error if ptr is nil.
func Require[T any](ptr *T, field string) (*T, error) {
    if ptr == nil {
        return nil, skillerr.Arg(fmt.Sprintf("%s is required", field))
    }
    return ptr, nil
}

// RequireNonEmpty returns an error if s is empty.
func RequireNonEmpty(s, field string) error {
    if strings.TrimSpace(s) == "" {
        return skillerr.Arg(fmt.Sprintf("%s is required", field))
    }
    return nil
}

// RequireSlice returns an error if slice is empty.
func RequireSlice[T any](xs []T, field string) error {
    if len(xs) == 0 {
        return skillerr.Arg(fmt.Sprintf("%s requires at least one item", field))
    }
    return nil
}

// Handler is a function that handles an operation.
type Handler func() (map[string]any, error)

// Dispatch routes to the appropriate handler based on operation.
func Dispatch(op string, handlers map[string]Handler) (map[string]any, error) {
    op = Op(op)
    handler, ok := handlers[op]
    if !ok {
        return nil, skillerr.Arg(fmt.Sprintf("unknown operation: %s", op))
    }
    return handler()
}
```

**Skills that will benefit:**
- `mailbox/manage` - simplify operation dispatch
- `git/worktree` - simplify operation dispatch
- `json/transform` - simplify operation dispatch

**Dependencies:** None
**Risk:** Low - additive change

---

## Phase 3: Consolidate External Tool Logic

### 3.1 Create `internal/tools/ripgrep` Package

**Location:** `~/repos/personal/claude-migration-to-v2/internal/tools/ripgrep/ripgrep.go`

**Problem:** Ripgrep execution logic duplicated in:
- `internal/intelligence/retrieval/ripgrep.go` (text output, files-with-matches)
- `skills/code_context_ripgrep/main.go` (JSON output, match expansion)
- `skills/text_ripgrep/main.go` (JSON output, snippet extraction)
- `internal/agent/tools/code_tools.go` (text output, simple parsing)

**Solution:** Create unified ripgrep wrapper

```go
package ripgrep

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"

    "github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
)

// DefaultExcludeGlobs are common directories to exclude from searches.
var DefaultExcludeGlobs = []string{
    ".git",
    "node_modules",
    "vendor",
    "__pycache__",
    "dist",
    "build",
    ".godot",
}

// Match represents a ripgrep match result.
type Match struct {
    File       string
    LineNumber int
    LineText   string
    Submatches []Submatch
}

// Submatch represents a match within a line.
type Submatch struct {
    Start int
    End   int
    Text  string
}

// SearchOpts configures ripgrep search behavior.
type SearchOpts struct {
    MaxCount      int      // --max-count
    IgnoreCase    bool     // --ignore-case
    Hidden        bool     // --hidden
    ContextLines  int      // -C
    IncludeGlobs  []string // --glob (include patterns)
    ExcludeGlobs  []string // --glob ! (exclude patterns, defaults added)
    FileTypes     []string // --type
}

// SearchJSON runs ripgrep with JSON output and returns parsed matches.
func SearchJSON(ctx context.Context, dir, pattern string, opts SearchOpts) ([]Match, error) {
    args := buildArgs(pattern, opts, true)

    result, err := executil.Run(ctx, dir, "rg", args...)
    if err != nil && !executil.IsNoMatch(result) {
        return nil, fmt.Errorf("ripgrep: %w", err)
    }

    if executil.IsNoMatch(result) {
        return nil, nil
    }

    return parseJSONOutput(result.Stdout)
}

// FilesWithMatches runs ripgrep and returns only matching file paths.
func FilesWithMatches(ctx context.Context, dir, pattern string, opts SearchOpts) ([]string, error) {
    args := buildArgs(pattern, opts, false)
    args = append([]string{"--files-with-matches"}, args...)

    result, err := executil.Run(ctx, dir, "rg", args...)
    if err != nil && !executil.IsNoMatch(result) {
        return nil, fmt.Errorf("ripgrep: %w", err)
    }

    if executil.IsNoMatch(result) {
        return nil, nil
    }

    var files []string
    scanner := bufio.NewScanner(bytes.NewReader(result.Stdout))
    for scanner.Scan() {
        files = append(files, scanner.Text())
    }
    return files, scanner.Err()
}

func buildArgs(pattern string, opts SearchOpts, jsonOutput bool) []string {
    var args []string

    if jsonOutput {
        args = append(args, "--json")
    }

    args = append(args, "--no-heading", "--line-number", "--no-messages")

    if opts.MaxCount > 0 {
        args = append(args, fmt.Sprintf("--max-count=%d", opts.MaxCount))
    }
    if opts.IgnoreCase {
        args = append(args, "--ignore-case")
    }
    if opts.Hidden {
        args = append(args, "--hidden")
    }
    if opts.ContextLines > 0 {
        args = append(args, fmt.Sprintf("-C%d", opts.ContextLines))
    }

    // File types
    for _, t := range opts.FileTypes {
        args = append(args, "--type", t)
    }

    // Include globs
    for _, g := range opts.IncludeGlobs {
        args = append(args, "--glob", g)
    }

    // Exclude globs (merge with defaults)
    excludes := append(DefaultExcludeGlobs, opts.ExcludeGlobs...)
    for _, g := range excludes {
        args = append(args, "--glob", "!"+g)
    }

    args = append(args, "--", pattern)

    return args
}
```

**Migration:**
1. Update `internal/intelligence/retrieval/ripgrep.go` to use `tools/ripgrep.FilesWithMatches`
2. Update `skills/code_context_ripgrep` to use `tools/ripgrep.SearchJSON`
3. Update `skills/text_ripgrep` to use `tools/ripgrep.SearchJSON`

**Dependencies:** Phase 2.3 (executil)
**Risk:** Medium - changes multiple call sites

---

### 3.2 Create `internal/lsp/jsonrpc` Package

**Location:** `~/repos/personal/claude-migration-to-v2/internal/lsp/jsonrpc/client.go`

**Problem:** JSON-RPC transport duplicated in:
- `internal/lsp/gopls/daemon.go` (lines 281-376) - persistent daemon
- `skills/lsp_tsserver/main.go` (lines 516-596) - per-request
- `skills/lsp_pylsp/main.go` (lines 635-719) - per-request

All three implement Content-Length framing, request/response handling, and notification skipping.

**Solution:** Create unified JSON-RPC client

```go
package jsonrpc

import (
    "bufio"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
)

// Client handles JSON-RPC 2.0 communication over stdio.
type Client struct {
    stdin  io.Writer
    stdout *bufio.Reader
    nextID atomic.Int64
    mu     sync.Mutex
}

// NewClient creates a JSON-RPC client from stdin/stdout pipes.
func NewClient(stdin io.Writer, stdout io.Reader) *Client {
    return &Client{
        stdin:  stdin,
        stdout: bufio.NewReader(stdout),
    }
}

// Request sends a JSON-RPC request and waits for response.
func (c *Client) Request(ctx context.Context, method string, params, result any) error {
    id := c.nextID.Add(1)

    req := struct {
        JSONRPC string `json:"jsonrpc"`
        ID      int64  `json:"id"`
        Method  string `json:"method"`
        Params  any    `json:"params,omitempty"`
    }{
        JSONRPC: "2.0",
        ID:      id,
        Method:  method,
        Params:  params,
    }

    if err := c.writeMessage(req); err != nil {
        return fmt.Errorf("write request: %w", err)
    }

    return c.readResponse(ctx, id, result)
}

// Notify sends a JSON-RPC notification (no response expected).
func (c *Client) Notify(method string, params any) error {
    notif := struct {
        JSONRPC string `json:"jsonrpc"`
        Method  string `json:"method"`
        Params  any    `json:"params,omitempty"`
    }{
        JSONRPC: "2.0",
        Method:  method,
        Params:  params,
    }

    return c.writeMessage(notif)
}

func (c *Client) writeMessage(msg any) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    content, err := json.Marshal(msg)
    if err != nil {
        return err
    }

    header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(content))
    if _, err := c.stdin.Write([]byte(header)); err != nil {
        return err
    }
    if _, err := c.stdin.Write(content); err != nil {
        return err
    }
    return nil
}

func (c *Client) readResponse(ctx context.Context, expectedID int64, result any) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // Read Content-Length header
        contentLength, err := c.readContentLength()
        if err != nil {
            return fmt.Errorf("read header: %w", err)
        }

        // Read body
        body := make([]byte, contentLength)
        if _, err := io.ReadFull(c.stdout, body); err != nil {
            return fmt.Errorf("read body: %w", err)
        }

        // Check if this is a notification (no ID)
        var hasID struct {
            ID *int64 `json:"id"`
        }
        if err := json.Unmarshal(body, &hasID); err == nil && hasID.ID == nil {
            continue // Skip notifications
        }

        // Parse response
        var resp struct {
            ID     int64           `json:"id"`
            Result json.RawMessage `json:"result"`
            Error  *struct {
                Code    int    `json:"code"`
                Message string `json:"message"`
            } `json:"error"`
        }
        if err := json.Unmarshal(body, &resp); err != nil {
            continue // Skip malformed messages
        }

        // Check ID matches
        if resp.ID != expectedID {
            continue // Skip responses for other requests
        }

        // Handle error response
        if resp.Error != nil {
            return fmt.Errorf("LSP error %d: %s", resp.Error.Code, resp.Error.Message)
        }

        // Unmarshal result
        if result != nil && len(resp.Result) > 0 {
            if err := json.Unmarshal(resp.Result, result); err != nil {
                return fmt.Errorf("unmarshal result: %w", err)
            }
        }

        return nil
    }
}

func (c *Client) readContentLength() (int, error) {
    for {
        line, err := c.stdout.ReadString('\n')
        if err != nil {
            return 0, err
        }
        line = strings.TrimSpace(line)

        if line == "" {
            continue // Skip empty lines between messages
        }

        if strings.HasPrefix(line, "Content-Length: ") {
            lengthStr := strings.TrimPrefix(line, "Content-Length: ")
            length, err := strconv.Atoi(lengthStr)
            if err != nil {
                return 0, fmt.Errorf("invalid content-length: %s", lengthStr)
            }

            // Read until empty line (end of headers)
            for {
                headerLine, err := c.stdout.ReadString('\n')
                if err != nil {
                    return 0, err
                }
                if strings.TrimSpace(headerLine) == "" {
                    break
                }
            }

            return length, nil
        }
    }
}
```

**Migration:**
1. Update `internal/lsp/gopls/daemon.go` to use `jsonrpc.Client`
2. Update `skills/lsp_tsserver` to use `jsonrpc.Client`
3. Update `skills/lsp_pylsp` to use `jsonrpc.Client`

**Dependencies:** None
**Risk:** Medium - affects all LSP functionality

---

## Phase 4: Skill Refactoring

### 4.1 Refactor `code/context_ripgrep`

**File:** `~/repos/personal/claude-migration-to-v2/skills/code_context_ripgrep/main.go`

**Changes:**
1. Use `tools/ripgrep.SearchJSON` instead of manual rg execution
2. Use `skillout.PreviewAndPersistNDJSON` instead of `prepareBlockPreview` + `persistBlocksArtifact`
3. Use `pathutil.ResolveSearchPath` instead of `resolveWorkspace`
4. Use `pathutil.RelTo` instead of `relativeTo`

**Lines to delete:** ~150 (helpers replaced by shared packages)

**Dependencies:** Phase 2.1, 2.2, 3.1

---

### 4.2 Refactor `fs/find`

**File:** `~/repos/personal/claude-migration-to-v2/skills/fs_find/main.go`

**Changes:**
1. Use `skillout.PreviewAndPersistNDJSON` for results
2. Use `fsutil.IsCommonExclude` instead of local `isCommonExclude`
3. Use `pathutil.RelTo` instead of local `relativeTo`

**Lines to delete:** ~80

**Dependencies:** Phase 2.1, 2.2

---

### 4.3 Refactor `fs/apply_edit`

**File:** `~/repos/personal/claude-migration-to-v2/skills/fs_apply_edit/main.go`

**Changes:**
1. Use `diffutil.UnifiedDiff` instead of massive custom `generateUnifiedDiff` (lines 430-528)

**Lines to delete:** ~100

**Dependencies:** Phase 2.4

---

### 4.4 Refactor `html_edit`

**File:** `~/repos/personal/claude-migration-to-v2/skills/html_edit/main.go`

**Changes:**
1. Use `diffutil.UnifiedDiff` instead of local `generateUnifiedDiff`

**Lines to delete:** ~15

**Dependencies:** Phase 2.4

---

### 4.5 Refactor `code/smart_write`

**File:** `~/repos/personal/claude-migration-to-v2/skills/code_smart_write/main.go`

**Changes:**
1. Use `diffutil.UnifiedDiff` instead of local `generateUnifiedDiff`

**Lines to delete:** ~15

**Dependencies:** Phase 2.4

---

### 4.6 Refactor LSP Skills

**Files:**
- `~/repos/personal/claude-migration-to-v2/skills/lsp_tsserver/main.go`
- `~/repos/personal/claude-migration-to-v2/skills/lsp_pylsp/main.go`

**Changes:**
1. Use `lsp/jsonrpc.Client` for transport
2. Keep only init params and operation wrappers

**Lines to delete:** ~200 per skill

**Dependencies:** Phase 3.2

---

### 4.7 Refactor Multi-Op Skills with `oputil`

**Files:**
- `~/repos/personal/claude-migration-to-v2/skills/mailbox/main.go`
- `~/repos/personal/claude-migration-to-v2/skills/git_worktree/main.go`

**Changes:**
1. Use `oputil.Op` for operation normalization
2. Use `oputil.Require*` for validation
3. Use `oputil.Dispatch` for routing

**Dependencies:** Phase 2.5

---

## Phase 5: Future Skills Enablement

### 5.1 Internal Package Structure for `smart_read_file`

Create supporting packages:

```
internal/
  codeintel/
    selector/     # File selection using retrieval
    loader/       # File loading with line-range hints
    guard/        # Secret scanning
```

**Dependencies:** Phase 2, 3

---

### 5.2 `smart_read_file` Skill

**Input:**
```json
{
  "query": "authentication logic",
  "files": [],
  "auto_files": true,
  "max_files": 8,
  "mode": "general",
  "max_bytes_per_file": 200000
}
```

**Pipeline:**
1. Auto-select files via `retrieval.Generator` (if `auto_files=true`)
2. Load content with line-range hints
3. Secret scan (fail if detected)
4. LLM masking (or deterministic for `mode=structure`)
5. Output with `skillout.PreviewAndPersistNDJSON`

**Dependencies:** Phase 5.1, all Phase 2 helpers

---

### 5.3 `code_counsel` Skill

**Input:**
```json
{
  "query": "auth flow",
  "files": [],
  "auto_files": true,
  "use_smart_read": true,
  "questions": [...]
}
```

**Pipeline:**
1. Resolve context (direct load or via smart_read)
2. Secret scan
3. Run questions with perspective profiles
4. Output results with evidence

**Dependencies:** Phase 5.2

---

## Dependency Graph

```
Phase 0 (Critical Bugs)
├── 0.1 IsUnderWorkspace fix
├── 0.2 CAS pinning fix
└── 0.3 RunContext.Close fix
    │
    v
Phase 1 (Consolidation)
├── 1.1 Unify RunContext ──────────────────────┐
├── 1.2 Consolidate preview helpers            │
└── 1.3 Consolidate CAS helpers ◄──────────────┘
    │
    v
Phase 2 (New Helpers)
├── 2.1 PreviewAndPersistNDJSON (depends on 1.3)
├── 2.2 pathutil (depends on 0.1)
├── 2.3 executil (no deps)
├── 2.4 diffutil (no deps)
└── 2.5 oputil (no deps)
    │
    v
Phase 3 (Tool Consolidation)
├── 3.1 tools/ripgrep (depends on 2.3)
└── 3.2 lsp/jsonrpc (no deps)
    │
    v
Phase 4 (Skill Refactoring)
├── 4.1 code/context_ripgrep (depends on 2.1, 2.2, 3.1)
├── 4.2 fs/find (depends on 2.1, 2.2)
├── 4.3 fs/apply_edit (depends on 2.4)
├── 4.4 html_edit (depends on 2.4)
├── 4.5 code/smart_write (depends on 2.4)
├── 4.6 LSP skills (depends on 3.2)
└── 4.7 Multi-op skills (depends on 2.5)
    │
    v
Phase 5 (Future Skills)
├── 5.1 codeintel packages
├── 5.2 smart_read_file
└── 5.3 code_counsel
```

---

## Implementation Order (Recommended)

### Week 1: Foundation
1. **Day 1-2:** Phase 0 (all bug fixes)
2. **Day 3:** Phase 1.2 (preview consolidation)
3. **Day 4-5:** Phase 1.1 + 1.3 (context unification, CAS consolidation)

### Week 2: New Helpers
4. **Day 1:** Phase 2.3 + 2.4 + 2.5 (executil, diffutil, oputil - no deps)
5. **Day 2:** Phase 2.2 (pathutil)
6. **Day 3:** Phase 2.1 (PreviewAndPersistNDJSON)
7. **Day 4-5:** Phase 3.1 (tools/ripgrep)

### Week 3: LSP & Skill Refactoring
8. **Day 1-2:** Phase 3.2 (lsp/jsonrpc)
9. **Day 3:** Phase 4.3 + 4.4 + 4.5 (diff-based skills)
10. **Day 4-5:** Phase 4.1 + 4.2 (ripgrep-based skills)

### Week 4: Polish & Future
11. **Day 1-2:** Phase 4.6 (LSP skills)
12. **Day 3:** Phase 4.7 (multi-op skills)
13. **Day 4-5:** Phase 5.1 (codeintel packages)

### Week 5+: New Skills
14. Phase 5.2 (smart_read_file)
15. Phase 5.3 (code_counsel)

---

## Acceptance Criteria

### Phase 0
- [ ] `IsUnderWorkspace(".gitignore", "/workspace")` returns `true`
- [ ] Large outputs have their digests pinned in CAS
- [ ] `RunContext.Close()` properly cleans up resources

### Phase 1
- [ ] Only one `PreparePreview` function exists (in skillout)
- [ ] Only one `Artifact` type exists (in skillout)
- [ ] `runner.RunnerContext` is a type alias for `skillmain.RunContext`

### Phase 2
- [ ] `PreviewAndPersistNDJSON` exists with tests
- [ ] `pathutil.ResolveSearchPath` exists with tests
- [ ] `executil.Run` exists with tests
- [ ] `diffutil.UnifiedDiff` exists with tests
- [ ] `oputil.Dispatch` exists with tests

### Phase 3
- [ ] `tools/ripgrep` package exists with JSON and files-with-matches modes
- [ ] `lsp/jsonrpc.Client` handles all Content-Length framing

### Phase 4
- [ ] `code/context_ripgrep` has no local `relativeTo`, `resolveWorkspace`, `prepareBlockPreview`
- [ ] `fs/apply_edit` has no local `generateUnifiedDiff` (100+ lines removed)
- [ ] LSP skills have no local JSON-RPC transport code (200+ lines each removed)

### Overall
- [ ] All existing tests pass
- [ ] Total lines removed > 2000
- [ ] New helper packages have >80% test coverage

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| Breaking existing skills | Run full test suite after each phase |
| Type alias causing import issues | Use embedding as fallback if alias doesn't work |
| JSON-RPC edge cases in LSP | Keep old implementations as fallback for 1 sprint |
| Preview behavior changes | Compare output for sample of skill invocations |

---

## Files Changed Summary

### New Files (~15)
- `internal/adapters/skillslib/pathutil/pathutil.go`
- `internal/adapters/skillslib/pathutil/pathutil_test.go`
- `internal/adapters/skillslib/executil/executil.go`
- `internal/adapters/skillslib/executil/executil_test.go`
- `internal/adapters/skillslib/diffutil/diffutil.go`
- `internal/adapters/skillslib/diffutil/diffutil_test.go`
- `internal/adapters/skillslib/oputil/oputil.go`
- `internal/adapters/skillslib/oputil/oputil_test.go`
- `internal/tools/ripgrep/ripgrep.go`
- `internal/tools/ripgrep/ripgrep_test.go`
- `internal/lsp/jsonrpc/client.go`
- `internal/lsp/jsonrpc/client_test.go`

### Modified Files (~35)
- `internal/adapters/skillslib/workspace/workspace.go` (IsUnderWorkspace fix)
- `internal/runtime/runservice/result.go` (CAS pinning fix)
- `internal/adapters/skillslib/skillmain/context.go` (Close fix, remove CAS helpers)
- `internal/adapters/skillslib/runner/context.go` (type alias, remove duplicates)
- `internal/adapters/skillslib/skillout/emit.go` (keep CAS helpers)
- `internal/adapters/skillslib/skillout/preview.go` (add PreviewAndPersistNDJSON)
- `internal/intelligence/retrieval/ripgrep.go` (use tools/ripgrep)
- `internal/lsp/gopls/daemon.go` (use lsp/jsonrpc)
- `skills/code_context_ripgrep/main.go`
- `skills/text_ripgrep/main.go`
- `skills/fs_find/main.go`
- `skills/fs_apply_edit/main.go`
- `skills/html_edit/main.go`
- `skills/code_smart_write/main.go`
- `skills/lsp_tsserver/main.go`
- `skills/lsp_pylsp/main.go`
- `skills/mailbox/main.go`
- `skills/git_worktree/main.go`
- ... and skill tests

### Deleted Files (~2)
- `internal/adapters/skillslib/preview.go`
- `internal/adapters/skillslib/preview_test.go`
