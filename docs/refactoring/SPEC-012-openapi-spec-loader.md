# SPEC-012: OpenAPI Skill - Spec Loader

## Status
**Not Started** | Priority: Critical | Complexity: Medium

## Problem Statement

The `http/openapi` skill is currently a stub that only supports dry-run mode. To enable real API calls, we need a robust OpenAPI specification loader that can:
- Load specs from multiple sources (file path, CAS digest, named memory)
- Validate OpenAPI 3.0.x and 3.1.x specifications
- Parse and cache specs efficiently
- Extract operation definitions for execution

### Current State
```go
// skills/http_openapi/main.go:1-115
// Only dry-run planner exists, no spec loading
```

## Proposed Solution

### 1. Spec Loader Architecture

```go
// internal/openapi/loader/loader.go
package loader

import (
    "context"
    "encoding/json"
    "fmt"
    "os"

    "github.com/getkin/kin-openapi/openapi3"
    "github.com/jkatigb/agentctl/internal/storage"
)

// Spec represents a loaded and validated OpenAPI specification.
type Spec struct {
    Doc       *openapi3.T           // Parsed OpenAPI document
    Source    string                // Where it was loaded from
    Version   string                // OpenAPI version (3.0.x or 3.1.x)
    Operations map[string]*Operation // operationId -> Operation
}

// Operation represents a single API operation.
type Operation struct {
    ID          string
    Method      string
    Path        string
    Summary     string
    Description string
    Parameters  []*openapi3.ParameterRef
    RequestBody *openapi3.RequestBodyRef
    Responses   openapi3.Responses
    Security    *openapi3.SecurityRequirements
}

// Loader loads and caches OpenAPI specifications.
type Loader struct {
    casStore    storage.CASStore
    memoryStore storage.MemoryStore
    cache       map[string]*Spec // In-memory cache
}

// NewLoader creates a new spec loader.
func NewLoader(cas storage.CASStore, mem storage.MemoryStore) *Loader {
    return &Loader{
        casStore:    cas,
        memoryStore: mem,
        cache:       make(map[string]*Spec),
    }
}

// Load loads a spec from the given reference.
// Reference formats:
//   - "/path/to/spec.yaml" or "/path/to/spec.json" (file path)
//   - "sha256:abc123..." (CAS digest)
//   - "memory:api-spec" (named memory)
func (l *Loader) Load(ctx context.Context, ref string) (*Spec, error) {
    // Check cache
    if spec, ok := l.cache[ref]; ok {
        return spec, nil
    }

    // Determine source type and load
    var data []byte
    var err error
    var source string

    switch {
    case isFilePath(ref):
        data, err = os.ReadFile(ref)
        source = "file:" + ref

    case isCASDigest(ref):
        data, err = l.loadFromCAS(ctx, ref)
        source = ref

    case isMemoryRef(ref):
        data, err = l.loadFromMemory(ctx, ref)
        source = ref

    default:
        return nil, fmt.Errorf("invalid spec reference: %s", ref)
    }

    if err != nil {
        return nil, fmt.Errorf("load spec: %w", err)
    }

    // Parse and validate
    spec, err := l.parse(data, source)
    if err != nil {
        return nil, fmt.Errorf("parse spec: %w", err)
    }

    // Cache and return
    l.cache[ref] = spec
    return spec, nil
}

// parse parses and validates an OpenAPI specification.
func (l *Loader) parse(data []byte, source string) (*Spec, error) {
    loader := openapi3.NewLoader()
    loader.IsExternalRefsAllowed = true

    var doc *openapi3.T
    var err error

    // Try JSON first, then YAML
    if json.Valid(data) {
        doc, err = loader.LoadFromData(data)
    } else {
        doc, err = loader.LoadFromData(data)
    }

    if err != nil {
        return nil, fmt.Errorf("parse OpenAPI: %w", err)
    }

    // Validate
    if err := doc.Validate(loader.Context); err != nil {
        return nil, fmt.Errorf("invalid OpenAPI spec: %w", err)
    }

    // Check version
    if doc.OpenAPI < "3.0.0" || doc.OpenAPI >= "4.0.0" {
        return nil, fmt.Errorf("unsupported OpenAPI version: %s (require 3.0.x or 3.1.x)", doc.OpenAPI)
    }

    // Build operation index
    operations := make(map[string]*Operation)
    for path, pathItem := range doc.Paths.Map() {
        for method, op := range pathItem.Operations() {
            if op.OperationID == "" {
                continue // Skip operations without ID
            }

            operations[op.OperationID] = &Operation{
                ID:          op.OperationID,
                Method:      method,
                Path:        path,
                Summary:     op.Summary,
                Description: op.Description,
                Parameters:  op.Parameters,
                RequestBody: op.RequestBody,
                Responses:   op.Responses,
                Security:    op.Security,
            }
        }
    }

    return &Spec{
        Doc:        doc,
        Source:     source,
        Version:    doc.OpenAPI,
        Operations: operations,
    }, nil
}

// loadFromCAS loads spec from content-addressable storage.
func (l *Loader) loadFromCAS(ctx context.Context, digest string) ([]byte, error) {
    obj, err := l.casStore.Get(ctx, digest)
    if err != nil {
        return nil, err
    }
    defer obj.Close()

    return io.ReadAll(obj)
}

// loadFromMemory loads spec from named memory.
func (l *Loader) loadFromMemory(ctx context.Context, ref string) ([]byte, error) {
    name := strings.TrimPrefix(ref, "memory:")

    entry, err := l.memoryStore.Get(ctx, name)
    if err != nil {
        return nil, err
    }

    // Memory entry contains either:
    // 1. The spec directly as JSON/YAML
    // 2. A CAS digest reference
    if isCASDigest(entry.Content) {
        return l.loadFromCAS(ctx, entry.Content)
    }

    return []byte(entry.Content), nil
}

// GetOperation retrieves an operation by ID.
func (s *Spec) GetOperation(operationID string) (*Operation, error) {
    op, ok := s.Operations[operationID]
    if !ok {
        available := make([]string, 0, len(s.Operations))
        for id := range s.Operations {
            available = append(available, id)
        }
        return nil, fmt.Errorf("operation %q not found (available: %v)", operationID, available)
    }
    return op, nil
}

// Helper functions
func isFilePath(ref string) bool {
    return strings.HasPrefix(ref, "/") || strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../")
}

func isCASDigest(ref string) bool {
    return strings.HasPrefix(ref, "sha256:")
}

func isMemoryRef(ref string) bool {
    return strings.HasPrefix(ref, "memory:")
}
```

### 2. Import Command

```go
// cmd/agentctl/cmd/openapi.go (new)
package cmd

import (
    "context"
    "fmt"
    "os"

    "github.com/spf13/cobra"
    "github.com/jkatigb/agentctl/internal/openapi/loader"
)

var openAPICmd = &cobra.Command{
    Use:   "openapi",
    Short: "Manage OpenAPI specifications",
}

var importCmd = &cobra.Command{
    Use:   "import <file|url>",
    Short: "Import an OpenAPI spec into CAS and optionally save to memory",
    Args:  cobra.ExactArgs(1),
    RunE:  runImport,
}

var (
    importAs     string
    importStrict bool
)

func init() {
    importCmd.Flags().StringVar(&importAs, "as", "", "Save to named memory with this name")
    importCmd.Flags().BoolVar(&importStrict, "strict", false, "Use strict validation")

    openAPICmd.AddCommand(importCmd)
    rootCmd.AddCommand(openAPICmd)
}

func runImport(cmd *cobra.Command, args []string) error {
    ctx := context.Background()
    specRef := args[0]

    // Load stores
    cfg := mustLoadConfig()
    cas := mustOpenCAS(cfg)
    defer cas.Close()

    mem := mustOpenMemory(cfg)
    defer mem.Close()

    // Load and validate spec
    l := loader.NewLoader(cas, mem)
    spec, err := l.Load(ctx, specRef)
    if err != nil {
        return fmt.Errorf("load spec: %w", err)
    }

    // Store in CAS
    specData, err := os.ReadFile(specRef)
    if err != nil {
        return err
    }

    digest, _, err := cas.Put(ctx, bytes.NewReader(specData), "application/openapi+yaml", nil)
    if err != nil {
        return fmt.Errorf("store in CAS: %w", err)
    }

    fmt.Fprintf(os.Stderr, "Stored spec in CAS: %s\n", digest)
    fmt.Fprintf(os.Stderr, "OpenAPI version: %s\n", spec.Version)
    fmt.Fprintf(os.Stderr, "Operations: %d\n", len(spec.Operations))

    // Optionally save to memory
    if importAs != "" {
        err = mem.Save(ctx, importAs, digest, map[string]string{
            "type":    "openapi-spec",
            "version": spec.Version,
            "source":  specRef,
        })
        if err != nil {
            return fmt.Errorf("save to memory: %w", err)
        }
        fmt.Fprintf(os.Stderr, "Saved to memory: %s\n", importAs)
    }

    // Output envelope with operation list
    operations := make([]map[string]string, 0, len(spec.Operations))
    for _, op := range spec.Operations {
        operations = append(operations, map[string]string{
            "id":      op.ID,
            "method":  op.Method,
            "path":    op.Path,
            "summary": op.Summary,
        })
    }

    return emitOk("openapi/import", map[string]any{
        "digest":     digest,
        "version":    spec.Version,
        "operations": operations,
    })
}
```

## Implementation Plan

### Step 1: Core Loader (4h)
1. Create `internal/openapi/loader/` package
2. Implement `Loader` with multi-source support
3. Integrate `kin-openapi` library for parsing
4. Add operation indexing
5. Implement caching

**Files:**
- `internal/openapi/loader/loader.go`
- `internal/openapi/loader/helpers.go`

**Dependencies:**
```bash
go get github.com/getkin/kin-openapi/openapi3@latest
```

### Step 2: Tests (3h)
1. Unit tests for each source type
2. Validation tests (valid/invalid specs)
3. Edge cases (missing operationId, malformed YAML)
4. Cache behavior tests

**Files:**
- `internal/openapi/loader/loader_test.go`
- `internal/openapi/loader/testdata/` (fixtures)

**Test Fixtures:**
- `valid-3.0.json` - OpenAPI 3.0 spec
- `valid-3.1.yaml` - OpenAPI 3.1 spec
- `invalid-version.json` - Swagger 2.0 (should reject)
- `malformed.yaml` - Syntax errors
- `missing-operation-id.json` - Operations without IDs

### Step 3: Import Command (2h)
1. Add `agentctl openapi import` command
2. Add `--as` flag for memory storage
3. Add `--strict` flag for validation mode
4. Output operation summary

**Files:**
- `cmd/agentctl/cmd/openapi.go`

**Acceptance:**
```bash
./bin/agentctl openapi import ./spec.yaml --as github-api
# Output: digest, operation count, memory name
```

### Step 4: Integration (1h)
1. Wire loader into `http/openapi` skill
2. Update skill to accept spec reference
3. Test with real spec files

**Files:**
- `skills/http_openapi/main.go`

## Testing Strategy

### Unit Tests
- ✅ Load from file path
- ✅ Load from CAS digest
- ✅ Load from memory reference
- ✅ Parse JSON and YAML
- ✅ Validate OpenAPI 3.0.x
- ✅ Validate OpenAPI 3.1.x
- ✅ Reject Swagger 2.0
- ✅ Reject OpenAPI 4.0+
- ✅ Handle malformed specs
- ✅ Build operation index
- ✅ Cache behavior

### Integration Tests
- ✅ Import real GitHub API spec
- ✅ Import real Stripe API spec
- ✅ Load from each source type
- ✅ Operation lookup

### Error Cases
- ✅ Invalid reference format
- ✅ File not found
- ✅ CAS digest not found
- ✅ Memory not found
- ✅ Parse errors
- ✅ Validation errors

## Dependencies
- **Depends on:** Storage interfaces (SPEC-001) ✅
- **Required by:** SPEC-013 (Request Builder), SPEC-014 (HTTP Client)
- **Library:** `github.com/getkin/kin-openapi/openapi3`

## Success Criteria
- ✅ Load specs from file, CAS, and memory
- ✅ Parse and validate OpenAPI 3.0.x and 3.1.x
- ✅ Index operations by operationId
- ✅ Cache loaded specs
- ✅ `openapi import` command works
- ✅ 80%+ test coverage

## Effort Estimate
**Total: 10 hours**
- Implementation: 4h
- Testing: 3h
- Import command: 2h
- Integration: 1h

## References
- OpenAPI spec: `docs/spec/openapi_skill.md:1-150`
- kin-openapi library: https://github.com/getkin/kin-openapi
- OpenAPI 3.0 spec: https://spec.openapis.org/oas/v3.0.3
- OpenAPI 3.1 spec: https://spec.openapis.org/oas/v3.1.0
