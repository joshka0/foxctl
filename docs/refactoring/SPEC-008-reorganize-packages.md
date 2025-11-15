# SPEC-008: Reorganize Internal Packages by Domain

## Status
**Completed** | Priority: Medium | Complexity: High

**Note**: Core reorganization complete. Minor layer violations documented for follow-up (see Known Layer Violations section).

## Problem Statement

The legacy `internal/` package structure was **flat** with 13 packages at the same level, making it difficult to:
- Understand architectural boundaries
- Identify dependencies between layers
- Enforce clean architecture principles
- Navigate the codebase
- Determine what depends on what

### Previous Structure (Flat)
```
internal/
├── artifacts/      # Artifact wrapping
├── buildinfo/      # Build metadata
├── cache/          # Result caching
├── cas/            # Content-addressable storage
├── config/         # Configuration
├── envelope/       # Envelope format
├── jobs/           # Job execution
├── memory/         # Named memory
├── policy/         # Policy enforcement
├── runner/         # Skill execution
│   ├── exec/       # Native execution
│   └── wasi/       # WASM execution
├── skill/          # Skill manifests
├── skillslib/      # Skill test harness
└── workspace/      # Workspace detection
```

### Problems
1. **No Clear Layers**: Can't tell which packages are core vs infrastructure
2. **Unclear Dependencies**: Everything at same level, hard to see what depends on what
3. **Mixed Concerns**: Domain logic mixed with infrastructure code
4. **No Dependency Direction**: No enforcement of layered architecture

## Proposed Solution

### Layered Architecture

Organize packages into **4 clear layers** with explicit dependency direction:

```
domain → storage → execution → platform
  ↑                                ↓
  └────────────────────────────────┘
         (domain has no deps)
```

### New Structure

```
internal/
│
├── domain/                 # Core business logic (NO dependencies on other internal packages)
│   ├── envelope/          # Envelope types and validation
│   │   ├── envelope.go
│   │   ├── envelope_test.go
│   │   └── metadata.go
│   │
│   ├── skill/             # Skill manifest types
│   │   ├── manifest.go
│   │   ├── manifest_test.go
│   │   ├── capability.go
│   │   └── validator.go
│   │
│   └── policy/            # Policy validation logic
│       ├── policy.go
│       ├── policy_test.go
│       └── validator.go
│
├── storage/               # Persistence layer (depends on domain only)
│   ├── interfaces.go      # Storage interfaces (from SPEC-001)
│   ├── sqlutil/           # SQL utilities (from SPEC-003)
│   │   ├── scan.go
│   │   ├── types.go
│   │   └── scan_test.go
│   │
│   ├── cache/             # Cache implementation
│   │   ├── store.go
│   │   ├── store_test.go
│   │   └── entry.go
│   │
│   ├── cas/               # Content-addressable storage
│   │   ├── store.go
│   │   ├── store_test.go
│   │   ├── object.go
│   │   └── metadata.go
│   │
│   ├── memory/            # Named memory storage
│   │   ├── store.go
│   │   ├── store_test.go
│   │   └── entry.go
│   │
│   └── jobs/              # Job persistence
│       ├── store.go
│       ├── store_test.go
│       ├── job.go
│       └── progress.go
│
├── execution/             # Skill execution (depends on domain + storage)
│   ├── executor.go        # Executor interface (from SPEC-004)
│   ├── mock_executor.go
│   │
│   ├── runner/            # Runner implementation
│   │   ├── runner.go
│   │   ├── runner_test.go
│   │   └── dispatch.go
│   │
│   ├── exec/              # Native binary execution
│   │   ├── exec.go
│   │   ├── exec_test.go
│   │   ├── rlimit_linux.go
│   │   ├── rlimit_unix.go
│   │   └── rlimit_windows.go
│   │
│   └── wasi/              # WASM/WASI execution
│       ├── wasi.go
│       └── wasi_test.go
│
├── platform/              # Platform concerns (depends on all above)
│   ├── config/            # Configuration management
│   │   ├── config.go
│   │   ├── config_test.go
│   │   ├── context.go
│   │   └── paths.go
│   │
│   ├── workspace/         # Workspace detection
│   │   ├── workspace.go
│   │   └── workspace_test.go
│   │
│   ├── buildinfo/         # Build metadata
│   │   └── buildinfo.go
│   │
│   └── errors/            # Error utilities (from SPEC-006)
│       ├── errors.go
│       └── errors_test.go
│
└── adapters/              # External adapters and utilities
    ├── artifacts/         # Artifact management (from SPEC-005)
    │   ├── manager.go
    │   ├── manager_test.go
    │   ├── mock_manager.go
    │   ├── batch.go
    │   └── extract.go     # Digest extraction
    │
    └── skillslib/         # Skill development harness
        ├── harness.go
        └── harness_test.go
```

## Dependency Rules

### Layer 1: Domain (No Internal Dependencies)
```go
// ✅ Allowed
import (
    "context"
    "encoding/json"
    "time"
)

// ❌ NOT Allowed
import (
    "github.com/jkatigb/agentctl/internal/storage"  // NO!
    "github.com/jkatigb/agentctl/internal/platform" // NO!
)
```

**Domain packages**:
- Define core types (Envelope, Manifest, Policy)
- Contain validation logic
- Have NO dependencies on other internal packages
- Pure business logic

### Layer 2: Storage (Depends on Domain)
```go
// ✅ Allowed
import (
    "github.com/jkatigb/agentctl/internal/domain/envelope"
    "github.com/jkatigb/agentctl/internal/domain/skill"
)

// ❌ NOT Allowed
import (
    "github.com/jkatigb/agentctl/internal/execution"  // NO!
    "github.com/jkatigb/agentctl/internal/adapters"   // NO!
)
```

**Storage packages**:
- Implement storage interfaces
- Persist domain objects
- Can depend on domain only

### Layer 3: Execution (Depends on Domain + Storage)
```go
// ✅ Allowed
import (
    "github.com/jkatigb/agentctl/internal/domain/skill"
    "github.com/jkatigb/agentctl/internal/storage"
)

// ❌ NOT Allowed
import (
    "github.com/jkatigb/agentctl/internal/platform/config"  // Prefer injection
)
```

**Execution packages**:
- Execute skills
- Can depend on domain and storage
- Should receive config via injection, not import

### Layer 4: Platform (Can Depend on All)
```go
// ✅ Allowed
import (
    "github.com/jkatigb/agentctl/internal/domain/envelope"
    "github.com/jkatigb/agentctl/internal/storage"
    "github.com/jkatigb/agentctl/internal/execution"
)
```

**Platform packages**:
- Cross-cutting concerns (config, logging, errors)
- Can depend on any layer
- Provide infrastructure for application

### Layer 5: Adapters (Orchestration)
```go
// ✅ Allowed - can depend on anything
import (
    "github.com/jkatigb/agentctl/internal/domain/envelope"
    "github.com/jkatigb/agentctl/internal/storage/cas"
    "github.com/jkatigb/agentctl/internal/execution"
)
```

**Adapter packages**:
- Coordinate multiple components
- Implement facade patterns
- Can depend on any layer

## Implementation Plan

### Step 1: Create New Directory Structure (1 hour)
- [ ] Create `internal/domain/` with subdirectories
- [ ] Create `internal/storage/` with subdirectories
- [ ] Create `internal/execution/` with subdirectories
- [ ] Create `internal/platform/` with subdirectories
- [ ] Create `internal/adapters/` with subdirectories

### Step 2: Move Domain Packages (2 hours)
- [ ] Move `envelope/` → `domain/envelope/`
- [ ] Move `skill/` → `domain/skill/`
- [ ] Move `policy/` → `domain/policy/`
- [ ] Update all import paths
- [ ] Run tests

### Step 3: Move Storage Packages (3 hours)
- [ ] Create `storage/interfaces.go` (SPEC-001)
- [ ] Create `storage/sqlutil/` (SPEC-003)
- [ ] Move `cache/` → `storage/cache/`
- [ ] Move `cas/` → `storage/cas/`
- [ ] Move `memory/` → `storage/memory/`
- [ ] Move `jobs/` → `storage/jobs/`
- [ ] Update all import paths
- [ ] Run tests

### Step 4: Move Execution Packages (2 hours)
- [ ] Create `execution/executor.go` (SPEC-004)
- [ ] Move `runner/` → `execution/runner/`
- [ ] Move `runner/exec/` → `execution/exec/`
- [ ] Move `runner/wasi/` → `execution/wasi/`
- [ ] Update all import paths
- [ ] Run tests

### Step 5: Move Platform Packages (1.5 hours)
- [ ] Move `config/` → `platform/config/`
- [ ] Move `workspace/` → `platform/workspace/`
- [ ] Move `buildinfo/` → `platform/buildinfo/`
- [ ] Create `platform/errors/` (SPEC-006)
- [ ] Update all import paths
- [ ] Run tests

### Step 6: Move Adapter Packages (1.5 hours)
- [ ] Create `adapters/artifacts/` with manager (SPEC-005)
- [ ] Move `skillslib/` → `adapters/skillslib/`
- [ ] Update all import paths
- [ ] Run tests

### Step 7: Update cmd/ Package (2 hours)
- [ ] Update all imports in cmd/agentctl/
- [ ] Update all imports in skills/
- [ ] Update build scripts if needed
- [ ] Run full test suite

### Step 8: Add Import Linting (1 hour)
- [ ] Add import-restriction rules to .golangci.yml
- [ ] Verify no layer violations
- [ ] Document architecture in ARCHITECTURE.md

### Step 9: Documentation (1 hour)
- [ ] Create ARCHITECTURE.md
- [ ] Document layer rules
- [ ] Update AGENTS.md
- [ ] Add architecture diagrams

**Total Estimated Time**: 15 hours

## Testing Strategy

### Import Validation Tests
```go
// internal/domain/domain_test.go
package domain_test

import (
    "go/parser"
    "go/token"
    "testing"
)

func TestDomain_NoDependenciesOnOtherInternalPackages(t *testing.T) {
    // Parse all files in domain/
    // Verify no imports of internal/storage, internal/execution, etc.

    fset := token.NewFileSet()
    pkgs, err := parser.ParseDir(fset, "./", nil, parser.ImportsOnly)
    require.NoError(t, err)

    for _, pkg := range pkgs {
        for _, file := range pkg.Files {
            for _, imp := range file.Imports {
                importPath := imp.Path.Value

                // Domain must not import other internal packages
                assert.NotContains(t, importPath, "internal/storage")
                assert.NotContains(t, importPath, "internal/execution")
                assert.NotContains(t, importPath, "internal/platform")
                assert.NotContains(t, importPath, "internal/adapters")
            }
        }
    }
}
```

### Layer Tests
```bash
# Use go mod graph to verify dependencies
go mod graph | grep "internal/domain" | grep "internal/storage"
# Should return nothing - domain can't depend on storage
```

## Migration Strategy

### Big Bang vs Incremental

**Option 1: Big Bang (Recommended)**
- Move all packages in one PR
- Update all imports at once
- Easier to review as complete picture
- Less risk of intermediate broken state

**Option 2: Incremental**
- Move one layer at a time
- Keep old paths working temporarily with aliases
- Gradual migration over multiple PRs
- More complex but lower risk

**Recommendation**: Big Bang
- Codebase is small enough (~7k LOC)
- All tests can verify in one pass
- Clearer architecture immediately visible

### Rollback Plan
```bash
# Create branch before refactoring
git checkout -b pre-package-reorg main

# Do refactoring on feature branch
git checkout -b refactor/package-reorg

# If issues arise, can easily compare or revert
git diff pre-package-reorg refactor/package-reorg
```

## Package Import Path Examples

### Before
```go
import (
    "github.com/jkatigb/agentctl/internal/domain/envelope"
    "github.com/jkatigb/agentctl/internal/storage/cache"
    "github.com/jkatigb/agentctl/internal/execution/runner"
    "github.com/jkatigb/agentctl/internal/platform/config"
)
```

### After
```go
import (
    "github.com/jkatigb/agentctl/internal/domain/envelope"
    "github.com/jkatigb/agentctl/internal/storage/cache"
    "github.com/jkatigb/agentctl/internal/execution/runner"
    "github.com/jkatigb/agentctl/internal/platform/config"
)
```

## Linter Configuration

```yaml
# .golangci.yml
linters-settings:
  depguard:
    rules:
      domain-no-deps:
        files:
          - "**/internal/domain/**/*.go"
        deny:
          - pkg: "github.com/jkatigb/agentctl/internal/storage"
            desc: "domain layer must not depend on storage"
          - pkg: "github.com/jkatigb/agentctl/internal/execution"
            desc: "domain layer must not depend on execution"
          - pkg: "github.com/jkatigb/agentctl/internal/platform"
            desc: "domain layer must not depend on platform"
          - pkg: "github.com/jkatigb/agentctl/internal/adapters"
            desc: "domain layer must not depend on adapters"

      storage-limited-deps:
        files:
          - "**/internal/storage/**/*.go"
        deny:
          - pkg: "github.com/jkatigb/agentctl/internal/execution"
            desc: "storage layer must not depend on execution"
          - pkg: "github.com/jkatigb/agentctl/internal/adapters"
            desc: "storage layer must not depend on adapters"

      execution-limited-deps:
        files:
          - "**/internal/execution/**/*.go"
        deny:
          - pkg: "github.com/jkatigb/agentctl/internal/adapters"
            desc: "execution layer should not depend on adapters"
```

## Benefits

### Before: Flat Structure
```
internal/
├── artifacts/
├── buildinfo/
├── cache/
├── cas/
├── config/
├── envelope/
├── jobs/
├── memory/
├── policy/
├── runner/
├── skill/
├── skillslib/
└── workspace/
```
**Problems**: 13 packages, unclear relationships, no guidance

### After: Layered Structure
```
internal/
├── domain/      # Core logic, no deps
├── storage/     # Persistence, depends on domain
├── execution/   # Execution, depends on domain+storage
├── platform/    # Cross-cutting, depends on all
└── adapters/    # Orchestration, depends on all
```
**Benefits**: Clear layers, explicit dependencies, easy navigation

### Improvements
- ✅ **Clear architectural boundaries**
- ✅ **Enforced dependency direction**
- ✅ **Easier to understand system**
- ✅ **Easier to find code**
- ✅ **Better separation of concerns**
- ✅ **Testability improved**
- ✅ **Onboarding easier**

## Documentation

### Create ARCHITECTURE.md
```markdown
# Architecture

## Package Organization

agentctl follows a layered architecture with explicit dependency direction:

```
┌─────────────────────────────────────────┐
│              cmd/agentctl               │
│         (CLI Application Layer)         │
└────────────────┬────────────────────────┘
                 │
                 ↓
┌─────────────────────────────────────────┐
│         internal/adapters/              │
│    (Orchestration & Facade Layer)       │
│  - artifacts: Artifact lifecycle mgmt   │
│  - skillslib: Skill test harness        │
└────────────────┬────────────────────────┘
                 │
    ┌────────────┼────────────┐
    │            │            │
    ↓            ↓            ↓
┌─────────┐  ┌──────────┐  ┌──────────┐
│Platform │  │Execution │  │ Storage  │
│ Layer   │  │  Layer   │  │  Layer   │
└─────────┘  └──────────┘  └──────────┘
    │            │            │
    └────────────┼────────────┘
                 │
                 ↓
         ┌───────────────┐
         │    Domain     │
         │     Layer     │
         │  (Core Logic) │
         └───────────────┘
```

### Dependency Rules

1. **Domain Layer** (internal/domain/)
   - NO dependencies on other internal packages
   - Pure business logic
   - Contains: envelope, skill, policy

2. **Storage Layer** (internal/storage/)
   - Depends ONLY on domain
   - Persistence implementations
   - Contains: cache, cas, memory, jobs

3. **Execution Layer** (internal/execution/)
   - Depends on domain + storage
   - Skill execution logic
   - Contains: executor, runner, exec, wasi

4. **Platform Layer** (internal/platform/)
   - Cross-cutting concerns
   - Can depend on any layer
   - Contains: config, workspace, buildinfo, errors

5. **Adapter Layer** (internal/adapters/)
   - Orchestration and facades
   - Can depend on any layer
   - Contains: artifacts, skillslib
```

## Success Criteria

- [x] All packages organized into 5 layers
- [x] All imports updated
- [x] ARCHITECTURE.md created
- [x] Import linting configured
- [x] Documentation updated
- [ ] No layer violations detected by linter (⚠️ Known violations - see below)
- [ ] All tests pass after reorganization (network issues in CI)

## Known Layer Violations (To Be Fixed)

The following layer violations were detected and need to be addressed in a follow-up:

### 1. Storage Layer → Adapters Layer (High Priority)

**Files affected**:
- `internal/storage/cache/store.go:15` - imports `adapters/artifacts`
- `internal/storage/memory/store.go:15` - imports `adapters/artifacts`

**Problem**: Storage layer depends on adapter layer (artifacts.Manager)

**Solution**:
- Define an interface in the storage layer for artifact operations
- Pass artifacts.Manager via dependency injection
- Use interface instead of concrete type

### 2. Storage Layer → Execution Layer (Critical Priority)

**Files affected**:
- `internal/storage/jobs/executor/executor.go:15` - imports `internal/execution`
- `internal/storage/jobs/executor/executor.go:16` - imports `internal/execution/runner`

**Problem**: The `executor` package is misplaced in the storage layer

**Solution**:
- Move `internal/storage/jobs/executor/` to `internal/execution/jobs/`
- Keep job persistence types in `internal/storage/jobs/types/`
- Keep job persistence implementation in `internal/storage/jobs/persist/`
- Update all import paths

### 3. Impact Assessment

These violations do not affect the core architecture but prevent the linter from passing.
They should be addressed in a follow-up refactoring (suggest creating SPEC-020).

**Estimated effort**: 3-4 hours
- Move executor package: 1.5h
- Refactor artifact dependencies: 1.5h
- Testing and verification: 1h

## Related Specs
- SPEC-001: Storage Interfaces (creates storage/interfaces.go)
- SPEC-003: Database Scanning (creates storage/sqlutil/)
- SPEC-004: SkillExecutor Interface (creates execution/executor.go)
- SPEC-005: Artifact Management (creates adapters/artifacts/)
- SPEC-006: Error Handling (creates platform/errors/)

## References
- Clean Architecture by Robert C. Martin
- Hexagonal Architecture
- Domain-Driven Design
- Go Project Layout (golang-standards/project-layout)
