# agentctl

> **Bash for LLMs** — A single-binary CLI for structured, deterministic AI workflows

[![Go Report Card](https://goreportcard.com/badge/github.com/jkatigb/agentctl)](https://goreportcard.com/report/github.com/jkatigb/agentctl)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/badge/go-1.22+-blue.svg)](https://golang.org/dl/)

**agentctl** implements the [Core Profile v1](docs/spec/core_profile_v1.md) specification, providing a universal toolkit for building reliable, reproducible AI-powered workflows with structured JSON I/O, content-addressable storage, and deterministic caching.

---

## 🎯 What is agentctl?

Think of agentctl as **Unix pipelines for AI agents**. It provides:

- **Structured I/O**: Canonical JSON envelopes (Version 1) for deterministic communication
- **Skills**: Discoverable, sandboxed tools (like bash commands) that AI agents can invoke
- **Job Persistence**: Durable async execution with crash recovery and progress tracking
- **Content-Addressable Storage (CAS)**: SHA-256 integrity for large outputs
- **Memory System**: Auto-cache (24h TTL) + named persistent memories for context reuse
- **Universal API Client**: Call any OpenAPI 3.x REST API without code generation (in development)

### Design Principles

1. **Token Efficiency**: Large outputs go to CAS, only summaries in envelopes
2. **Deterministic**: Same inputs → same outputs (via cryptographic caching)
3. **Memory-First**: Recent work auto-cached, important work explicitly named
4. **Zero Config**: Works out-of-the-box, advanced features opt-in
5. **Composable**: Unix-style piping, digest chaining, skill composition
6. **Secure**: Workspace confinement, egress policies, path validation

---

## 🚀 Quick Start

### Installation

```bash
# From source (requires Go 1.22+)
git clone https://github.com/jkatigb/agentctl.git
cd agentctl
make build
./bin/agentctl version

# Build bundled skills
make skills-build
```

### Basic Usage

```bash
# List files in a directory
agentctl run fs/ls --path ./src

# Search for patterns
agentctl run text/grep --pattern "TODO" --path ./src

# Read a file with automatic preview
agentctl run fs/read --path ./main.go

# Manage tasks with dependencies
agentctl run todo/manage --input '{"operation":"add","title":"Implement feature X"}'
```

### Working with Jobs (Async)

```bash
# Submit a long-running job
JOB_ID=$(agentctl jobs submit text/grep --pattern "error" --path ./logs | jq -r '.data.job_id')

# Tail progress in real-time
agentctl jobs tail $JOB_ID

# Get final result
agentctl jobs result $JOB_ID
```

### Using Memory

```bash
# Run a skill and remember the result
agentctl run fs/ls --path ./src --remember project-structure

# Retrieve from memory
agentctl memory get project-structure

# Search memories
agentctl memory search "project structure"
```

### Content-Addressable Storage

```bash
# Store large content
DIGEST=$(agentctl cas put < large-file.json)

# Retrieve by digest
agentctl cas get $DIGEST

# Pin important artifacts (prevent GC)
agentctl cas pin $DIGEST
```

---

## ✨ Key Features

### 📦 Structured JSON Envelopes

All I/O uses a canonical envelope format for deterministic communication:

```json
{
  "version": 1,
  "command": "fs/ls",
  "status": "ok",
  "data": {
    "files": ["main.go", "README.md"]
  },
  "meta": {
    "source": "run",
    "timestamp": "2025-11-14T12:00:00Z"
  }
}
```

### 🔧 Built-in Skills

| Skill | Purpose | Distribution |
|-------|---------|--------------|
| `fs/ls` | List directory contents | exec |
| `fs/read` | Read files with preview | exec |
| `text/grep` | Regex search across files | exec |
| `todo/manage` | Task management with dependencies | exec |
| `wasi/echo` | WASM demo skill | WASI |
| `http/openapi` | Universal REST API client | exec (in development) |

### 💾 Content-Addressable Storage

Large outputs automatically stored with SHA-256 integrity:

```bash
# Small response - inline
agentctl run fs/read --path small.txt
# → {"data": {"content": "..."}}

# Large response - CAS with summary
agentctl run fs/read --path large.json
# → {"data": {"digest": "sha256:abc123...", "preview": "...", "size": 1048576}}
```

### 📋 Durable Job Execution

```bash
# Jobs persist across crashes
agentctl jobs submit http/openapi --spec memory:github --operationId listRepos

# Automatic deduplication
agentctl jobs submit --dedupe text/grep --pattern "error"

# Cancel running jobs
agentctl jobs cancel <job-id>
```

### 🧠 Memory System

Two memory types:

1. **Auto-cache** (24h TTL): Recent skill executions automatically cached
2. **Named memory**: Explicitly saved, persistent, workspace-scoped

```bash
# Cache modes
agentctl run fs/ls --cache auto   # Use cache if available (default)
agentctl run fs/ls --cache only   # Error if not cached
agentctl run fs/ls --cache off    # Skip cache

# Named memory with metadata
agentctl memory save api-spec \
  --content "$(cat openapi.yaml)" \
  --tags "api,v1,production"
```

### 🌐 OpenAPI Skill (In Development)

Universal REST API client without code generation:

```bash
# Import an OpenAPI spec
agentctl openapi import https://api.github.com/openapi.yaml --as github

# Call any operation
agentctl run http/openapi \
  --spec memory:github \
  --operationId repos/listForUser \
  --params '{"username": "torvalds"}' \
  --auth '{"type": "bearer", "token": "$GITHUB_TOKEN"}'

# Automatic pagination
agentctl run http/openapi \
  --spec memory:stripe \
  --operationId CustomerList \
  --paging '{"strategy": "cursor", "max_pages": 10}'
```

**Status**: Core loader and auth in progress (see [ROADMAP_TO_V1.md](ROADMAP_TO_V1.md))

---

## 🏗️ Architecture

### Layered Structure

```
internal/
├── domain/          # Pure business logic (no external deps)
│   ├── envelope/   # JSON envelope types & validation
│   ├── skill/      # Skill manifests & discovery
│   └── policy/     # Security policies (path validation, egress)
│
├── storage/        # Persistence layer
│   ├── cas/        # Content-addressable storage (SHA-256)
│   ├── cache/      # Auto-cache (24h TTL)
│   ├── memory/     # Named memories
│   └── jobs/       # Job state & execution
│
├── execution/      # Skill runners
│   ├── exec/       # Native process executor (with rlimits)
│   └── wasi/       # WASM executor (wazero, pure Go)
│
├── adapters/       # External integrations
│   ├── artifacts/  # Artifact lifecycle management
│   └── skillslib/  # Skill execution helpers
│
└── platform/       # Infrastructure
    ├── config/     # Configuration (Viper)
    ├── logging/    # Structured logging (Zerolog)
    ├── secrets/    # Secrets redaction
    └── workspace/  # Workspace detection
```

### Skill Execution Flow

```
User Command
    ↓
Skill Discovery (internal/domain/skill)
    ↓
Job Submission (internal/storage/jobs)
    ↓
Executor Selection (internal/execution)
    ├─→ WASI Runner (network isolated)
    └─→ Exec Runner (rlimits, ephemeral /work)
    ↓
Policy Enforcement (workspace confinement)
    ↓
CAS Storage (large outputs)
    ↓
JSON Envelope Output
```

---

## 📊 Current Status

### Phase Completion

| Phase | Status | Features |
|-------|--------|----------|
| 0-2: Foundation | ✅ 100% | Bootstrap, envelopes, CAS, CLI |
| 3: Jobs | ✅ 100% | SQLite persistence, async execution, crash recovery |
| 4: Runners | ✅ 100% | WASI + exec, skill manifests, sandboxing |
| 5: Memory | ✅ 100% | Auto-cache, named memories, search |
| **6: OpenAPI** | 🚧 5% | **Spec loader, request builder, HTTP client (in progress)** |
| 7: Plugins | 🔜 0% | Custom auth/pagination (deferred to v1.1) |
| 8: UX | 🔜 75% | Docs, golden tests, polish |

**Overall Progress**: ~60% toward v1.0

### What's Working

- ✅ All core infrastructure (envelopes, CAS, jobs, memory)
- ✅ Skill execution (WASI + exec runners)
- ✅ 6 built-in skills (fs/ls, fs/read, text/grep, todo/manage, wasi/echo, http/openapi stub)
- ✅ Deterministic caching with 24h TTL
- ✅ SQLite-backed persistence
- ✅ ~70% test coverage with strict CI/CD

### What's In Progress

- 🚧 OpenAPI skill implementation (SPEC-012 through SPEC-016)
  - Spec loader (file, CAS, memory sources)
  - Request builder (parameter validation)
  - HTTP client (response processing, CAS integration)
  - Pagination (Link headers, cursor, offset/limit)
  - Retry logic (exponential backoff)

See [ROADMAP_TO_V1.md](ROADMAP_TO_V1.md) for detailed timeline.

---

## 📚 Documentation

### Specifications

- **[Core Profile v1](docs/spec/core_profile_v1.md)** (802 lines) — Authoritative specification
- **[OpenAPI Skill](docs/spec/openapi_skill.md)** (2156 lines) — Detailed design (implementation in progress)
- **[Plugin Protocol](docs/spec/plugin_protocol.md)** — Plugin design (v1.1)

### Implementation Guides

- **[ROADMAP_TO_V1.md](ROADMAP_TO_V1.md)** — High-level roadmap and timeline
- **[IMPLEMENTATION_PRIORITY.md](IMPLEMENTATION_PRIORITY.md)** — Prioritized task breakdown
- **[Refactoring Specs](docs/refactoring/README.md)** — Detailed implementation specs (SPEC-001 through SPEC-019)
- **[AGENTS.md](AGENTS.md)** — Guide for AI coding assistants

### Examples

- **[Minimum Workflow Skills](docs/examples/minimum_workflow_skills.md)** — Essential skills guide
- **[Skills Chain](docs/examples/skills_chain.md)** — Composing skills together

---

## 🛠️ Development

### Prerequisites

- **Go 1.22+** (modules enabled, `CGO_ENABLED=0` for pure Go builds)
- **Make** (optional, for convenience targets)
- **golangci-lint** (for linting)
- **gofumpt** (for formatting)

### Building

```bash
# Build CLI
make build
# → ./bin/agentctl

# Build skills
make skills-build
# → ./dist/skills/*/

# Run tests
make test

# Run with race detection
make test-race

# Check coverage (target: 85%)
make cover

# Lint
make lint

# Format
make fmt

# All checks (format, lint, vet, test)
make check
```

### Project Structure

```
agentctl/
├── cmd/agentctl/           # CLI entry point (Cobra)
├── internal/               # Internal packages (see Architecture)
├── skills/                 # Built-in skills
│   ├── fs_ls/
│   ├── fs_read/
│   ├── text_grep/
│   ├── todo/
│   ├── wasi_echo/
│   └── http_openapi/
├── docs/
│   ├── spec/               # Specifications
│   ├── refactoring/        # Implementation specs
│   └── examples/           # Usage examples
├── test/
│   ├── golden/             # Golden fixtures (in progress)
│   └── integration/        # Integration tests (in progress)
└── scripts/                # Build and utility scripts
```

### Testing Philosophy

- **Unit tests**: All packages have `*_test.go` files
- **Integration tests**: E2E workflows in `cmd/agentctl/cmd/e2e_test.go`
- **Golden tests**: Envelope fixtures for regression prevention (in progress)
- **CI/CD**: Lint → Test → Race → Coverage (50% threshold, targeting 85%)

---

## 🤝 Contributing

We welcome contributions! Here's how to get started:

### Quick Contribution Path

1. **Pick a task** from [IMPLEMENTATION_PRIORITY.md](IMPLEMENTATION_PRIORITY.md)
2. **Read the spec** in [docs/refactoring/](docs/refactoring/)
3. **Create a branch**: `codex/<feature-name>` or `<username>/<feature-name>`
4. **Implement** following the spec's step-by-step plan
5. **Test**: `make check` must pass
6. **Open a PR** to `main` (never push directly to main)

### High-Impact Areas (Help Wanted!)

| Area | Specs | Impact | Effort |
|------|-------|--------|--------|
| **OpenAPI Implementation** | SPEC-012-016 | 🔥 Critical | 55h |
| **Security Hardening** | SPEC-011 | 🔒 High | 5.5h |
| **Golden Test Fixtures** | SPEC-018 | ✅ Medium | 8h |
| **Documentation** | SPEC-019 | 📖 Medium | 5h |

See [ROADMAP_TO_V1.md](ROADMAP_TO_V1.md#contributing) for detailed contribution guide.

### Development Guidelines

- **Read [AGENTS.md](AGENTS.md)** for AI assistant guidelines (applies to humans too!)
- **Follow Go conventions**: gofumpt formatting, golangci-lint rules
- **Write tests**: Aim for 80%+ coverage on new code
- **Update docs**: Specs, README, examples as needed
- **Conventional commits**: `feat:`, `fix:`, `docs:`, `test:`, `refactor:`

### Code Review Process

1. CI must pass (lint, test, race, coverage)
2. At least one human approval required
3. AI-generated PRs labeled `codex/*` auto-labeled `ai-generated`
4. Squash merge to main
5. Release handled by maintainers (goreleaser)

---

## 🗺️ Roadmap

### v1.0 (Target: Q1 2026)

**Remaining Work**: ~87.5 hours over 11 weeks

#### Critical Path
1. **Complete refactoring** (SPEC-008, SPEC-009) — 9h
2. **Security hardening** (SPEC-011) — 5.5h
3. **OpenAPI implementation** (SPEC-012-016) — 55h
   - Spec loader
   - Request builder
   - HTTP client & response processing
   - Pagination
   - Retry logic
4. **Quality & docs** (SPEC-018, SPEC-019) — 13h

#### Success Criteria

- ✅ Call any OpenAPI 3.x REST API (GitHub, Stripe, Slack)
- ✅ Automatic pagination handles 100+ pages
- ✅ Retry logic resilient to rate limits
- ✅ PathValidator prevents all workspace escapes
- ✅ 85%+ test coverage
- ✅ Comprehensive documentation (README, CONTRIBUTING, examples)

### v1.1+ (Future)

- **Plugin System** (SPEC-017): Custom auth/pagination strategies
- **Skill Codegen**: Per-operation wrappers for OpenAPI specs
- **Daemon Mode**: REST API server for multi-tenant access
- **Interactive Mode**: REPL with autocomplete
- **Observability**: Prometheus, OpenTelemetry, audit logging
- **Skill Registry**: Centralized discovery and installation

See [ROADMAP_TO_V1.md](ROADMAP_TO_V1.md) for detailed timeline.

---

## 🔐 Security

### Security Model

- **Workspace Confinement**: Skills cannot access files outside allowed paths
- **Egress Policies**: Network access controlled per skill
- **WASI Isolation**: WASM skills have no network access by default
- **Path Validation**: Prevents traversal, symlink, null byte attacks
- **Secrets Redaction**: Automatic redaction in logs and envelopes
- **CAS Integrity**: SHA-256 verification on all reads

### Reporting Security Issues

**Do not open public issues for security vulnerabilities.**

Email security reports to: [security@agentctl.dev](mailto:security@agentctl.dev) (or create private security advisory on GitHub)

See [docs/SECURITY.md](docs/SECURITY.md) for our security policy (coming soon).

---

## 📄 License

Apache License 2.0 — See [LICENSE](LICENSE) for details.

---

## 🙏 Acknowledgments

agentctl builds on excellent open source projects:

- **[Cobra](https://github.com/spf13/cobra)** — CLI framework
- **[Viper](https://github.com/spf13/viper)** — Configuration
- **[wazero](https://github.com/tetratelabs/wazero)** — Pure Go WASM runtime
- **[modernc.org/sqlite](https://modernc.org/sqlite)** — Pure Go SQLite
- **[zerolog](https://github.com/rs/zerolog)** — Structured logging
- **[kin-openapi](https://github.com/getkin/kin-openapi)** — OpenAPI parser

---

## 📞 Community & Support

- **Documentation**: [docs/](docs/)
- **Specifications**: [docs/spec/](docs/spec/)
- **Roadmap**: [ROADMAP_TO_V1.md](ROADMAP_TO_V1.md)
- **Contributing**: [AGENTS.md](AGENTS.md)

---

## 🚀 Getting Started

Ready to dive in?

1. **Read** [Core Profile v1](docs/spec/core_profile_v1.md) for the vision
2. **Build** with `make build && make skills-build`
3. **Try** the examples in [docs/examples/](docs/examples/)
4. **Contribute** by picking a task from [IMPLEMENTATION_PRIORITY.md](IMPLEMENTATION_PRIORITY.md)
5. **Explore** the [roadmap to v1.0](ROADMAP_TO_V1.md)

---

<div align="center">

**agentctl** — Structured, deterministic, composable AI workflows

Built with ❤️ by the agentctl community

[Documentation](docs/) • [Roadmap](ROADMAP_TO_V1.md) • [Contributing](AGENTS.md) • [Specifications](docs/spec/)

</div>
