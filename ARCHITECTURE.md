# Architecture

agentctl follows a layered architecture with explicit dependency direction to ensure clean separation of concerns and maintainability.

## Package Organization

The codebase is organized into five distinct layers:

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

## Layer Descriptions

### 1. Domain Layer (`internal/domain/`)

**Purpose**: Core business logic with zero dependencies on other internal packages.

**Packages**:
- `envelope/`: Envelope types and validation
- `skill/`: Skill manifest types and validation
- `policy/`: Policy validation logic

**Rules**:
- ✅ Can depend on: Standard library, external packages only
- ❌ Cannot depend on: Any other internal packages

**Example**:
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

### 2. Storage Layer (`internal/storage/`)

**Purpose**: Persistence implementations that store and retrieve domain objects.

**Packages**:
- `interfaces.go`: Storage interface definitions
- `sqlutil/`: SQL scanning utilities
- `cache/`: Result caching
- `cas/`: Content-addressable storage
- `memory/`: Named memory storage
- `jobs/`: Job persistence

**Rules**:
- ✅ Can depend on: Domain layer only
- ❌ Cannot depend on: Execution, Platform (prefer injection), Adapters

**Example**:
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

### 3. Execution Layer (`internal/execution/`)

**Purpose**: Skill execution logic and runtime environments.

**Packages**:
- `executor.go`: Executor interface
- `runner/`: Runner implementation
- `exec/`: Native binary execution
- `wasi/`: WASM/WASI execution

**Rules**:
- ✅ Can depend on: Domain and Storage layers
- ❌ Cannot depend on: Adapters
- ⚠️ Prefer injection over importing Platform/Config

**Example**:
```go
// ✅ Allowed
import (
    "github.com/jkatigb/agentctl/internal/domain/skill"
    "github.com/jkatigb/agentctl/internal/storage"
)

// ❌ NOT Allowed
import (
    "github.com/jkatigb/agentctl/internal/adapters"  // NO!
)

// ⚠️ Discouraged (prefer injection)
import (
    "github.com/jkatigb/agentctl/internal/platform/config"
)
```

### 4. Platform Layer (`internal/platform/`)

**Purpose**: Cross-cutting concerns and infrastructure.

**Packages**:
- `config/`: Configuration management
- `workspace/`: Workspace detection
- `buildinfo/`: Build metadata
- `errors/`: Error utilities
- `logging/`: Logging utilities
- `secrets/`: Secret management
- `metrics/`: Metrics collection

**Rules**:
- ✅ Can depend on: Any layer (cross-cutting concerns)
- Should be used via injection where possible

### 5. Adapter Layer (`internal/adapters/`)

**Purpose**: Orchestration, facades, and coordination between components.

**Packages**:
- `artifacts/`: Artifact lifecycle management
- `skillslib/`: Skill development test harness

**Rules**:
- ✅ Can depend on: Any layer
- Coordinates multiple components
- Implements facade patterns

## Dependency Rules Enforcement

Layer violations are automatically detected via `golangci-lint` with the `depguard` linter.

Run the linter to check for violations:

```bash
golangci-lint run ./...
```

## Design Principles

### 1. Dependency Inversion

Higher-level layers define interfaces that lower layers implement:

```go
// Domain layer defines interface
type SkillStore interface {
    GetSkill(ctx context.Context, id string) (*Skill, error)
}

// Storage layer implements
type sqliteSkillStore struct { ... }
func (s *sqliteSkillStore) GetSkill(...) { ... }
```

### 2. Explicit Dependencies

Use constructor injection rather than globals or direct imports:

```go
// ✅ Good - explicit dependency
type Runner struct {
    store  storage.SkillStore
    config *config.Config
}

func NewRunner(store storage.SkillStore, cfg *config.Config) *Runner {
    return &Runner{store: store, config: cfg}
}

// ❌ Bad - implicit dependency
import "github.com/jkatigb/agentctl/internal/platform/config"

func doSomething() {
    cfg := config.Load() // Global state!
}
```

### 3. Single Responsibility

Each layer has a clear, focused responsibility:

- **Domain**: Business rules and validation
- **Storage**: Persistence only
- **Execution**: Running skills only
- **Platform**: Infrastructure concerns
- **Adapters**: Orchestration and coordination

## Migration Notes

This layered structure was established through **SPEC-008: Reorganize Internal Packages** to improve:

- ✅ Clear architectural boundaries
- ✅ Enforced dependency direction
- ✅ Easier code navigation
- ✅ Better testability
- ✅ Simpler onboarding

### Before: Flat Structure

All 13 packages at the same level with unclear relationships:

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

### After: Layered Structure

Clear hierarchy with explicit dependencies:

```
internal/
├── domain/      # Core logic, no internal deps
├── storage/     # Persistence, depends on domain
├── execution/   # Execution, depends on domain+storage
├── platform/    # Cross-cutting, can depend on all
└── adapters/    # Orchestration, can depend on all
```

## Testing

### Unit Tests

Each layer can be tested independently:

```go
// Domain layer - pure logic, no mocks needed
func TestEnvelope_Validate(t *testing.T) { ... }

// Storage layer - test with in-memory DB or mocks
func TestSQLiteStore_GetSkill(t *testing.T) { ... }

// Execution layer - mock storage dependencies
func TestRunner_Execute(t *testing.T) {
    mockStore := &mockSkillStore{...}
    runner := NewRunner(mockStore, cfg)
    ...
}
```

### Integration Tests

Higher layers naturally test lower layers:

```go
// Adapter tests exercise multiple layers
func TestArtifactManager_PinFromEnvelope(t *testing.T) {
    // Tests: domain (envelope), storage (CAS), adapter (manager)
}
```

## Common Patterns

### Repository Pattern (Storage)

```go
// Domain defines interface
type JobRepository interface {
    Save(ctx context.Context, job *Job) error
    Get(ctx context.Context, id string) (*Job, error)
}

// Storage implements
type sqliteJobRepository struct { db *sql.DB }
```

### Service Pattern (Execution)

```go
// Execution layer service
type SkillRunner struct {
    executor  Executor
    jobStore  storage.JobRepository
}

func (r *SkillRunner) RunSkill(ctx context.Context, ...) error {
    // Orchestrates execution and persistence
}
```

### Facade Pattern (Adapters)

```go
// Adapter simplifies complex interactions
type ArtifactManager struct {
    cas      storage.CASStore
    pinStore storage.PinStore
}

func (m *ArtifactManager) PinFromEnvelope(env *envelope.Envelope) error {
    // Coordinates multiple storage operations
}
```

## References

This architecture follows principles from:

- **Clean Architecture** by Robert C. Martin
- **Hexagonal Architecture** (Ports and Adapters)
- **Domain-Driven Design** by Eric Evans
- **Go Project Layout** (golang-standards/project-layout)

## Related Documentation

- [SPEC-008](docs/refactoring/SPEC-008-reorganize-packages.md): Package reorganization specification
- [Protocol v1 Implementation](docs/guides/protocol_v1_implementation.md): Protocol details
- [Contributing Guide](CONTRIBUTING.md): Development guidelines
