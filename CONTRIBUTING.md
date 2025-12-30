# Contributing to agentctl

Thank you for your interest in contributing to agentctl! This guide will help you get started with development and understand our contribution process.

---

## 📋 Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [Contribution Workflow](#contribution-workflow)
- [Code Standards](#code-standards)
- [Testing Requirements](#testing-requirements)
- [Pull Request Process](#pull-request-process)
- [Finding Work](#finding-work)
- [Community](#community)

---

## Code of Conduct

We are committed to providing a welcoming and inclusive environment. Please be respectful and constructive in all interactions.

---

## Getting Started

### Prerequisites

- **Go 1.22+** with modules enabled
- **Make** (optional, for convenience targets)
- **golangci-lint** for linting
- **gofumpt** for formatting
- **Git** for version control

### First-Time Setup

```bash
# Clone the repository
git clone https://github.com/jkatigb/agentctl.git
cd agentctl

# Install dependencies
go mod download

# Build the CLI
make build

# Build bundled skills
make skills-build

# Run tests to verify setup
make test

# Run all checks (format, lint, vet, test)
make check
```

---

## Development Setup

### Project Structure

```
agentctl/
├── cmd/agentctl/           # CLI entry point (Cobra)
├── internal/               # Internal packages
│   ├── domain/            # Pure business logic
│   ├── storage/           # Persistence layer
│   ├── execution/         # Skill runners
│   ├── adapters/          # External integrations
│   └── platform/          # Infrastructure
├── skills/                # Built-in skills
├── docs/                  # Documentation
│   ├── spec/             # Specifications
│   └── refactoring/      # Implementation specs
├── test/                  # Test files
└── scripts/               # Build and utility scripts
```

### Key Make Targets

```bash
make build              # Build CLI → ./agentctl
make skills-build       # Build all skills → ./dist/skills/
make test               # Run unit tests
make test-race          # Run tests with race detection
make cover              # Check test coverage (target: 85%)
make lint               # Run linters
make fmt                # Format code with gofumpt
make check              # Run all checks (fmt, lint, vet, test)
make clean              # Clean build artifacts
```

### Environment Configuration

agentctl uses the following directories:
- **Config**: `~/.agentctl/config.yaml`
- **Database**: `~/.agentctl/agentctl.db`
- **CAS**: `~/.agentctl/cas/sha256/`
- **Jobs**: `~/.agentctl/jobs/<ulid>/`

You can override the config path with `--config` flag or `AGENTCTL_CONFIG` environment variable.

---

## Contribution Workflow

### 1. Find or Create an Issue

- Check [IMPLEMENTATION_PRIORITY.md](IMPLEMENTATION_PRIORITY.md) for prioritized tasks
- Browse open issues on GitHub
- For new features, discuss in an issue first

### 2. Create a Branch

**Branch naming conventions:**
- `codex/<feature-name>` — for AI-assisted development
- `<username>/<feature-name>` — for human contributors
- Examples: `codex/openapi-pagination`, `alice/fix-memory-leak`

```bash
git checkout main
git pull origin main
git checkout -b <username>/<feature-name>
```

### 3. Make Changes

- Follow the [Code Standards](#code-standards) below
- Write tests for new functionality
- Update documentation as needed
- Run `make check` frequently

### 4. Commit Your Changes

We use **conventional commits** for clear history:

```bash
# Format: <type>: <description>
git commit -m "feat: add pagination support to OpenAPI skill"
git commit -m "fix: resolve memory leak in job executor"
git commit -m "docs: update OpenAPI skill guide"
git commit -m "test: add integration tests for CAS"
git commit -m "refactor: extract auth logic to separate package"
```

**Commit types:**
- `feat:` — New feature
- `fix:` — Bug fix
- `docs:` — Documentation changes
- `test:` — Test additions or modifications
- `refactor:` — Code refactoring without behavior changes
- `perf:` — Performance improvements
- `chore:` — Maintenance tasks (dependencies, build, etc.)

### 5. Push and Open a Pull Request

```bash
git push -u origin <username>/<feature-name>
```

Open a PR on GitHub targeting the `main` branch. Fill out the PR template with:
- **Description** of changes
- **Related issue** numbers
- **Testing** performed
- **Checklist** items completed

---

## Code Standards

### Go Conventions

1. **Formatting**: Use `gofumpt` (stricter than `gofmt`)
   ```bash
   make fmt
   ```

2. **Linting**: Pass `golangci-lint` checks
   ```bash
   make lint
   ```

3. **Error Handling**:
   - Wrap errors with `%w` for error chains
   - Use sentinel errors in packages
   - Use `errors.Is` and `errors.As` for checks

4. **Context**:
   - First parameter should be `ctx context.Context`
   - Honor cancellation and timeouts
   - Pass context through call chains

5. **Logging**:
   - Use `zerolog` for structured logging
   - Logs go to **stderr**, envelopes to **stdout**
   - Redact secrets with `"***"`

6. **Naming**:
   - Use idiomatic Go naming (mixedCaps, not snake_case)
   - Interfaces: `Reader`, `Writer`, `Executor` (not `IReader`)
   - Package names: lowercase, single word

7. **Nil vs Empty**:
   - Return empty slices/maps, not nil
   - Use `omitempty` in JSON tags as appropriate

8. **Panics**:
   - Never panic in library code
   - Return errors instead

### JSON Envelope Contract

All CLI and skill output must use JSON envelopes:

```go
type Envelope struct {
    Version int            `json:"version"`          // Always 1
    Status  string         `json:"status"`           // "ok" or "error"
    Command string         `json:"command"`          // Skill name
    Data    any            `json:"data"`             // Result data
    Meta    Meta           `json:"meta"`             // Metadata
    Error   Error          `json:"error,omitempty"`  // Error details
}
```

**Critical rules:**
- Envelopes to stdout, logs to stderr
- Large outputs (>32 KB) go to CAS with summary
- Include `meta.cas_digest` when using CAS
- Set `meta.source:"cache"` on cache hits

### Architecture Guidelines

1. **Layered Structure**: Follow the existing architecture
   - `domain/` — Pure business logic (no external deps)
   - `storage/` — Persistence layer
   - `execution/` — Skill runners
   - `adapters/` — External integrations
   - `platform/` — Infrastructure

2. **Dependency Rules**:
   - Domain layer depends on nothing
   - Upper layers can depend on domain
   - No circular dependencies

3. **No CGO**: Keep builds portable (`CGO_ENABLED=0`)

4. **Pure Go**: Prefer pure Go libraries over cgo-based ones

### Security Guidelines

1. **Secrets**: Never log or commit secrets
   - Use `/run/secrets/<name>` for mounted secrets
   - Redact with `"***"` in logs
   - Use 0600 permissions for sensitive files

2. **Path Validation**: All file operations must use `policy.PathValidator`
   - Prevent traversal attacks
   - Anchor to workspace
   - Reject symlinks outside workspace

3. **Network Isolation**:
   - WASI skills: `network:"none"` (mandatory)
   - Exec skills: use `egressAllow` when needed

4. **CAS Integrity**: Always verify digests on read

---

## Testing Requirements

### Coverage Targets

- **Overall**: 85% coverage
- **Lines**: 85%
- **Functions**: 80%
- **Branches**: 75%

### Test Types

1. **Unit Tests** (`*_test.go` in each package)
   ```bash
   make test
   ```

2. **Integration Tests** (E2E workflows)
   ```bash
   go test ./cmd/agentctl/cmd/...
   ```

3. **Race Detection** (required before PR)
   ```bash
   make test-race
   ```

4. **Golden Tests** (envelope fixtures for regression prevention)
   - Place in `testdata/` directories
   - Freeze timestamps and redact host-specific data
   - Update with `UPDATE_GOLDEN=1 go test`

### Test Conventions

- Use **table-driven tests** for multiple scenarios
- Test error cases, not just happy paths
- Mock external dependencies
- Use `t.Parallel()` when tests are independent
- Clean up resources in test cleanup

### Example Test Structure

```go
func TestMyFunction(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {name: "happy path", input: "foo", want: "bar", wantErr: false},
        {name: "error case", input: "", want: "", wantErr: true},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            t.Parallel()
            got, err := MyFunction(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("MyFunction() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("MyFunction() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

---

## Pull Request Process

### Before Opening a PR

1. **Run all checks**: `make check`
2. **Test with race detection**: `make test-race`
3. **Check coverage**: `make cover`
4. **Update documentation**: If you changed behavior, update docs
5. **Add tests**: Ensure new code has tests

### PR Checklist

Copy this into your PR description:

```markdown
- [ ] Branch follows naming convention (`<username>/<feature-name>`)
- [ ] No breaking changes to wire contract (or spec updated)
- [ ] Envelopes on stdout, logs on stderr
- [ ] Large output uses CAS wrapper (summary + artifact)
- [ ] Tests pass: `make test` and `make test-race`
- [ ] Linting passes: `make lint`
- [ ] Code formatted: `make fmt`
- [ ] Coverage maintained or improved
- [ ] Documentation updated (specs, README, examples)
- [ ] Conventional commits used
```

### Review Process

1. **CI must pass**: GitHub Actions runs lint, test, race detection
2. **Human approval required**: At least one maintainer must approve
3. **Address feedback**: Respond to review comments promptly
4. **Squash merge**: PRs are squashed when merged to main

### Common Review Feedback

- Insufficient test coverage
- Missing error handling
- Violating envelope contract
- Not following Go conventions
- Security concerns (path traversal, secret leaks)
- Breaking changes without spec updates

---

## Finding Work

### High-Priority Areas

| Area | Specs | Impact | Effort |
|------|-------|--------|--------|
| **OpenAPI Implementation** | SPEC-012-016 | 🔥 Critical | 55h |
| **Security Hardening** | SPEC-011 | 🔒 High | 5.5h |
| **Golden Test Fixtures** | SPEC-018 | ✅ Medium | 8h |
| **Documentation** | SPEC-019 | 📖 Medium | 5h |

### Where to Start

1. **Read** [IMPLEMENTATION_PRIORITY.md](IMPLEMENTATION_PRIORITY.md)
2. **Pick a spec** from [docs/refactoring/](docs/refactoring/)
3. **Read the spec** thoroughly (includes step-by-step plan)
4. **Ask questions** in GitHub issues if anything is unclear
5. **Implement** following the spec's guidance

### Good First Issues

Look for issues labeled:
- `good first issue` — Suitable for newcomers
- `help wanted` — Maintainers need assistance
- `documentation` — Documentation improvements

### AI Assistants

If you're an AI coding assistant, read [AGENTS.md](AGENTS.md) for specific guidelines, conventions, and decision trees.

---

## Community

### Communication

- **Issues**: GitHub Issues for bugs and feature requests
- **Discussions**: GitHub Discussions for questions and ideas
- **Documentation**: [docs/](docs/) directory

### Getting Help

- Read the [Core Profile v1 spec](docs/spec/core_profile_v1.md)
- Check [ROADMAP_TO_V1.md](ROADMAP_TO_V1.md) for project status
- Review [AGENTS.md](AGENTS.md) for detailed conventions
- Ask questions in GitHub Discussions

### Recognition

Contributors are recognized in:
- Git commit history
- GitHub contributor graphs
- Release notes (for significant contributions)

---

## Specification-Driven Development

agentctl uses **specification-driven development**:

1. **Specs First**: All major features start with a spec in `docs/refactoring/`
2. **Step-by-Step**: Each spec includes detailed implementation steps
3. **Acceptance Criteria**: Clear success metrics
4. **Test Plan**: Required tests documented

When implementing a spec:
1. Read the entire spec first
2. Follow steps in order
3. Write tests as you go
4. Update spec status when complete

---

## Release Process

**Maintainers only** handle releases:

1. Tag release: `git tag v1.0.0`
2. Push tag: `git push origin v1.0.0`
3. GitHub Actions runs `goreleaser`
4. Binaries published to GitHub Releases

Contributors do **not** need to worry about releases.

---

## Additional Resources

- **[Core Profile v1](docs/spec/core_profile_v1.md)** — Complete specification
- **[OpenAPI Skill](docs/spec/openapi_skill.md)** — Universal REST API client
- **[Plugin Protocol](docs/spec/plugin_protocol.md)** — Extensibility via plugins
- **[Protocol v1 Implementation](docs/guides/protocol_v1_implementation.md)** — Build-out plan
- **[AGENTS.md](AGENTS.md)** — Guide for AI coding assistants

---

## Questions?

- Open an issue for bugs or feature requests
- Start a discussion for questions or ideas
- Read existing documentation in [docs/](docs/)

We appreciate your contributions and look forward to building agentctl together!

---

**Thank you for contributing to agentctl!** 🚀
