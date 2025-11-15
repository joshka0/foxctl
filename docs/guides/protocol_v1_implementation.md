# Protocol v1 Implementation Guide

**Version:** 1.0.0
**Status:** Implementation Roadmap
**Last Updated:** 2025-11-15

> **Purpose:** This document provides a complete build-out plan for implementing Protocol v1 in agentctl, including code organization, phased tasks, acceptance criteria, and practical guidance.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Code Organization](#2-code-organization)
3. [Build-Out Plan](#3-build-out-plan)
4. [Acceptance Criteria](#4-acceptance-criteria)
5. [Implementation Guidance](#5-implementation-guidance)
6. [Testing Strategy](#6-testing-strategy)

---

## 1. Overview

### 1.1 What is Protocol v1?

Protocol v1 is the **frozen wire contract** for agentctl that defines:
- Canonical JSON envelope structure
- Command namespace (skills, plugins, jobs, agents)
- Artifactization (CAS) rules
- Error catalog and handling
- Progress streaming format
- Security invariants

### 1.2 Implementation Goals

1. **Freeze the wire**: Adopt envelope + invariants as authoritative contract
2. **Land OpenAPI end-to-end**: Loader → Builder → Client → Pagination → Retry
3. **Back with fixtures & conformance**: Make wire regression impossible
4. **Enable plugins & agent profile**: Optional extensions without breaking core

### 1.3 Timeline

**Target**: v1.0 RC in **11 weeks** (~87.5 hours total work)

**Phases**:
- Phase 3A: Finish refactors (1 week)
- Phase 3B: Security hardening (parallel)
- Phase 4: OpenAPI core (3-4 weeks)
- Phase 5: Quality & extensibility (2 weeks)

---

## 2. Code Organization

### 2.1 Directory Structure

```
agentctl/
├── internal/
│   ├── protocol/                    # NEW: Envelope types, validation, error codes
│   │   ├── envelope.go             # Core envelope types
│   │   ├── errors.go               # Error code catalog + helpers
│   │   ├── validate.go             # Envelope validation
│   │   └── jsonschema/             # Embedded JSON schemas (go:embed)
│   │       ├── envelope.schema.json
│   │       ├── progress.schema.json
│   │       └── error.schema.json
│   │
│   ├── openapi/                     # OpenAPI skill implementation
│   │   ├── loader/                 # SPEC-012: Spec loading (file/CAS/memory)
│   │   │   ├── loader.go
│   │   │   ├── cache.go
│   │   │   └── index.go
│   │   ├── request/                # SPEC-013: Request builder
│   │   │   ├── builder.go
│   │   │   ├── validator.go
│   │   │   └── template.go
│   │   ├── client/                 # SPEC-014: HTTP client
│   │   │   ├── client.go
│   │   │   ├── response.go
│   │   │   └── summary.go
│   │   ├── pagination/             # SPEC-015: Pagination strategies
│   │   │   ├── link.go
│   │   │   ├── cursor.go
│   │   │   ├── offset.go
│   │   │   └── detector.go
│   │   └── retry/                  # SPEC-016: Retry logic
│   │       ├── backoff.go
│   │       └── policy.go
│   │
│   ├── plugin/                      # Plugin system
│   │   ├── runtime/                # Subprocess exec, timeouts, IO caps
│   │   │   ├── executor.go
│   │   │   ├── limits.go
│   │   │   └── discovery.go
│   │   ├── auth/                   # Auth plugin wrappers
│   │   │   └── invoker.go
│   │   └── pagination/             # Pagination plugin wrappers
│   │       └── invoker.go
│   │
│   ├── domain/
│   │   ├── policy/                 # SPEC-011: PathValidator hardening
│   │   │   ├── pathvalidator.go
│   │   │   ├── validator_test.go
│   │   │   └── rules.go
│   │   └── envelope/               # Will be replaced by protocol/
│   │
│   └── agentloop/                   # Agent loop substrate (optional)
│       ├── types.go
│       ├── interfaces.go
│       ├── prompt.go
│       ├── parse.go
│       ├── loop.go
│       ├── invoker_cli.go
│       └── invoker_direct.go
│
├── cmd/agentctl/cmd/
│   ├── openapi.go                   # openapi import/validate/describe CLI
│   ├── proto.go                     # proto validate CLI (schema+invariant checks)
│   └── run.go                       # Updated to use new protocol types
│
├── test/
│   └── golden/                      # SPEC-018: Golden test fixtures
│       ├── envelopes/
│       │   ├── ok-inline.json
│       │   ├── ok-cas.json
│       │   ├── error-earg.json
│       │   ├── error-eauth.json
│       │   └── progress-stream.ndjson
│       ├── openapi/
│       │   ├── response-inline.json
│       │   ├── response-cas.json
│       │   ├── response-paginated.json
│       │   └── error-401.json
│       └── README.md
│
└── docs/
    ├── spec/
    │   ├── protocol_v1.md           # NEW: Canonical wire contract
    │   ├── plugin_protocol.md       # Updated: Complete plugin spec
    │   ├── core_profile_v1.md       # Existing: Full agentctl spec
    │   ├── openapi_skill.md         # Existing: OpenAPI skill detail
    │   └── agent_profile_v1.md      # NEW: Multi-agent extensions
    └── guides/
        ├── agent_loop.md            # NEW: Agent loop implementation
        └── protocol_v1_implementation.md  # THIS FILE
```

### 2.2 Import Linting Rules

Add to `.golangci.yml`:

```yaml
linters-settings:
  depguard:
    rules:
      main:
        deny:
          - pkg: "github.com/jkatigb/agentctl/internal/domain/envelope"
            desc: "Use internal/protocol instead"
          - pkg: "github.com/jkatigb/agentctl/internal/openapi/*"
            desc: "OpenAPI packages should not import each other cyclically"
```

---

## 3. Build-Out Plan

### 3.1 Phase 3A — Finish Refactors (1 week, 9h)

#### Task 1: SPEC-008 Complete Package Reorganization (4h)

**Goal**: Clean architecture lanes with enforced boundaries

**Steps**:
1. Move `internal/domain/envelope` → `internal/protocol`
2. Update all imports across codebase
3. Add `depguard` rules to prevent regressions
4. Run `make check` to verify no breakage

**Acceptance**:
- [ ] All imports updated
- [ ] `depguard` rules enforced in CI
- [ ] No cyclic dependencies
- [ ] All tests pass

#### Task 2: SPEC-009 Extract Skill Discovery (5h)

**Goal**: Reusable discovery mechanism

**Steps**:
1. Create `internal/domain/skill/resolver.go`
2. Implement discovery from `AGENTCTL_SKILL_PATH`
3. Support file, CAS, and memory-backed skills
4. Update CLI to use new resolver
5. Add unit tests for discovery logic

**Acceptance**:
- [ ] Skill discovery extracted to reusable package
- [ ] CLI updated to use resolver
- [ ] Tests cover all discovery modes
- [ ] Documentation updated

---

### 3.2 Phase 3B — Security Hardening (parallel, 5.5h)

#### Task 3: SPEC-011 PathValidator Hardening (5.5h)

**Goal**: Security invariants in place before networking

**Steps**:
1. Implement symlink detection and blocking
2. Add canonical path resolution
3. Implement allow-list checking
4. Add null-byte and traversal attack prevention
5. Wire into all file-touching paths
6. Achieve 100% function coverage

**Test Cases**:
- [ ] Symlink detection blocks access
- [ ] Canonical paths enforced
- [ ] Allow-list respected
- [ ] Traversal attacks (`../../../etc/passwd`) blocked
- [ ] Null-byte attacks blocked
- [ ] Performance acceptable (< 1ms per check)

**Acceptance**:
- [ ] PathValidator test suite at 100% function coverage
- [ ] All file operations use PathValidator
- [ ] Security tests pass (symlink, traversal, null-byte)
- [ ] Documentation updated with security model

---

### 3.3 Phase 4 — OpenAPI Core (3-4 weeks, 55h)

#### Task 4: SPEC-012 OpenAPI Spec Loader (12h)

**Goal**: Load specs from file, CAS, memory with caching

**Steps**:
1. Implement file loader with validation
2. Implement CAS loader
3. Implement memory loader (integrates with memory system)
4. Add spec cache with 24h TTL
5. Build operation index for fast lookups
6. Add lenient/strict validation modes

**Deliverables**:
- `internal/openapi/loader/loader.go`
- `internal/openapi/loader/cache.go`
- `internal/openapi/loader/index.go`
- Unit tests with real specs (GitHub, Stripe)

**Acceptance**:
- [ ] Load from file/CAS/memory
- [ ] Operation index for O(1) lookups
- [ ] Cache reduces redundant parsing
- [ ] Lenient mode handles real-world specs
- [ ] Error messages actionable

#### Task 5: SPEC-013 Request Builder (13h)

**Goal**: Validate params and build HTTP requests

**Steps**:
1. Implement parameter validation (required/optional/types)
2. Implement path template resolution
3. Implement query string encoding
4. Implement body serialization with content-type negotiation
5. Add header management
6. Implement dry-run mode

**Deliverables**:
- `internal/openapi/request/builder.go`
- `internal/openapi/request/validator.go`
- `internal/openapi/request/template.go`

**Test Cases**:
- [ ] Required params enforced
- [ ] Path templates resolved correctly
- [ ] Query encoding handles arrays/objects
- [ ] Content-type negotiation (JSON/form/multipart)
- [ ] Dry-run emits redacted preview

**Acceptance**:
- [ ] Parameter validation matches spec
- [ ] Path templating handles edge cases
- [ ] Content negotiation works
- [ ] Dry-run mode functional
- [ ] Tests cover common patterns

#### Task 6: SPEC-014 HTTP Client & Response Processing (15h)

**Goal**: Execute requests with CAS integration

**Steps**:
1. Implement HTTP client with pooling
2. Implement response capture with size limits
3. Implement CAS artifactization logic
4. Generate summaries (status, headers, preview, count)
5. Add header curation (rate-limit, etag, pagination)
6. Implement inline vs CAS threshold logic

**Deliverables**:
- `internal/openapi/client/client.go`
- `internal/openapi/client/response.go`
- `internal/openapi/client/summary.go`

**Test Cases**:
- [ ] Small responses inlined
- [ ] Large responses artifactized with summary
- [ ] Headers curated (rate-limit, etag, link)
- [ ] Record count calculated for arrays
- [ ] Preview bounded and deterministic

**Acceptance**:
- [ ] CAS integration works
- [ ] Summaries include actionable info
- [ ] Size thresholds enforced
- [ ] Live API tests (GitHub/Stripe) pass
- [ ] Error cases handled

#### Task 7: SPEC-015 Pagination (8h)

**Goal**: Automatic pagination with multiple strategies

**Steps**:
1. Implement Link header pagination (GitHub style)
2. Implement cursor pagination (Stripe style)
3. Implement offset/limit pagination
4. Implement total-count heuristics
5. Add aggregation logic with partial error handling
6. Add pagination metadata to summary

**Deliverables**:
- `internal/openapi/pagination/link.go`
- `internal/openapi/pagination/cursor.go`
- `internal/openapi/pagination/offset.go`
- `internal/openapi/pagination/detector.go`

**Test Cases**:
- [ ] Link header pagination (rel="next")
- [ ] Cursor field extraction (configurable)
- [ ] Offset/limit with total count
- [ ] Auto-detection heuristics
- [ ] Max pages/items limits respected
- [ ] Partial aggregation on error

**Acceptance**:
- [ ] 3+ pagination strategies implemented
- [ ] Auto-detection works for common APIs
- [ ] Manual override supported
- [ ] Metadata captured in envelope
- [ ] Live tests with paginated APIs

#### Task 8: SPEC-016 Retry Logic (7h)

**Goal**: Resilient retries with backoff

**Steps**:
1. Implement exponential backoff with jitter
2. Implement `Retry-After` header respect
3. Implement configurable retry codes (429, 502, 503, 504)
4. Add retry budget tracking
5. Add transparent reporting (attempts, delays)

**Deliverables**:
- `internal/openapi/retry/backoff.go`
- `internal/openapi/retry/policy.go`

**Test Cases**:
- [ ] Exponential backoff with jitter
- [ ] `Retry-After` honored (seconds and HTTP-date)
- [ ] Max attempts enforced
- [ ] Retry budget transparent in errors
- [ ] Non-retryable codes fail immediately

**Acceptance**:
- [ ] Retry logic deterministic
- [ ] Backoff respects `Retry-After`
- [ ] Error includes retry metadata
- [ ] Tests cover edge cases
- [ ] Live tests with rate-limited APIs

---

### 3.4 Phase 5 — Quality & Extensibility (2 weeks, 13h)

#### Task 9: SPEC-018 Golden Test Fixtures (8h)

**Goal**: Regression prevention with golden envelopes

**Steps**:
1. Create `test/golden/envelopes/` directory
2. Generate canonical fixtures:
   - `ok-inline.json`
   - `ok-cas.json`
   - `error-*.json` (one per error code)
   - `progress-stream.ndjson`
3. Create `test/golden/openapi/` directory
4. Generate OpenAPI fixtures:
   - `response-inline.json`
   - `response-cas.json`
   - `response-paginated.json`
   - `error-401.json`, `error-429.json`
5. Add CI checks to verify envelope conformance

**Deliverables**:
- Golden fixtures with README
- `agentctl proto validate` command
- CI job to validate all fixtures

**Acceptance**:
- [ ] Golden fixtures stable
- [ ] CI verifies conformance
- [ ] Fixtures cover all major paths
- [ ] Documentation explains fixtures

#### Task 10: SPEC-019 Documentation & README (5h)

**Goal**: Comprehensive user-facing docs

**Steps**:
1. Update root README with Protocol v1 links
2. Create OpenAPI user guide with examples
3. Create troubleshooting guide (auth, pagination, rate limits)
4. Update CONTRIBUTING.md
5. Add code examples to docs

**Deliverables**:
- Updated README.md
- `docs/guides/openapi_guide.md`
- `docs/guides/troubleshooting.md`
- CONTRIBUTING.md updates

**Acceptance**:
- [ ] README comprehensive
- [ ] OpenAPI guide has working examples
- [ ] Troubleshooting covers common issues
- [ ] Contributing guide updated

---

### 3.5 Cross-Cutting Tasks (Early)

#### Task: Add Protocol Package (2h)

**Do early in Phase 3A**

**Steps**:
1. Create `internal/protocol/envelope.go`
2. Create `internal/protocol/errors.go`
3. Create `internal/protocol/validate.go`
4. Add embedded JSON schemas
5. Implement `ValidateEnvelope()` function

**Code**:
```go
// internal/protocol/envelope.go
package protocol

import "time"

type Envelope struct {
    Version int         `json:"version"`
    Status  string      `json:"status"`
    Command string      `json:"command"`
    Data    interface{} `json:"data,omitempty"`
    Meta    Meta        `json:"meta"`
    Error   *Err        `json:"error"`
}

type Meta struct {
    TS           time.Time `json:"ts"`
    DurationMS   int64     `json:"duration_ms"`
    Runner       string    `json:"runner,omitempty"`
    Workspace    string    `json:"workspace,omitempty"`
    JobID        string    `json:"job_id,omitempty"`
    TraceID      string    `json:"trace_id,omitempty"`
    Profiles     []string  `json:"profiles,omitempty"`
    Source       string    `json:"source,omitempty"`
    CASDigest    string    `json:"cas_digest,omitempty"`
    SkillVersion string    `json:"skill_version,omitempty"`
    CacheKey     string    `json:"cache_key,omitempty"`
}

type Err struct {
    Code    string                 `json:"code"`
    Message string                 `json:"message"`
    Details map[string]interface{} `json:"details,omitempty"`
}
```

```go
// internal/protocol/errors.go
package protocol

const (
    EOPENAPI     = "EOPENAPI"
    EARG         = "EARG"
    EAUTH        = "EAUTH"
    ERATELIMIT   = "ERATELIMIT"
    EPAGINATION  = "EPAGINATION"
    ERUNTIME     = "ERUNTIME"
    ENOTFOUND    = "ENOTFOUND"
    ETIMEOUT     = "ETIMEOUT"
    EPOLICY      = "EPOLICY"
    ESKILLDOWN   = "ESKILLDOWN"
    EPARSE       = "EPARSE"
    EOUTPUTTOOLARGE = "EOUTPUT_TOO_LARGE"
    EENVELOPE    = "EENVELOPE"
    EIO          = "EIO"
    ECANCELED    = "ECANCELED"
)

var ErrorDescriptions = map[string]string{
    EOPENAPI:        "OpenAPI spec invalid or operationId missing",
    EARG:            "Invalid arguments or missing required parameters",
    EAUTH:           "Authentication failed or credentials missing",
    ERATELIMIT:      "Rate limit exceeded after retry budget exhausted",
    EPAGINATION:     "Pagination detection or logic failure",
    ERUNTIME:        "Runtime error (network, server, IO)",
    ENOTFOUND:       "Resource, memory, or spec not found",
    ETIMEOUT:        "Operation timed out",
    EPOLICY:         "Workspace, path, or network policy violation",
    ESKILLDOWN:      "Skill unavailable (circuit breaker open)",
    EPARSE:          "JSON parse error or invalid UTF-8",
    EOUTPUTTOOLARGE: "Output exceeded capture limit",
    EENVELOPE:       "Invalid or malformed envelope",
    EIO:             "Filesystem or I/O error",
    ECANCELED:       "Operation canceled by user",
}

func NewError(code, message string) *Err {
    return &Err{
        Code:    code,
        Message: message,
        Details: make(map[string]interface{}),
    }
}

func (e *Err) WithDetail(key string, value interface{}) *Err {
    e.Details[key] = value
    return e
}
```

#### Task: Add `agentctl proto validate` Command (2h)

**Steps**:
1. Create `cmd/agentctl/cmd/proto.go`
2. Implement envelope validation
3. Add JSON schema validation
4. Add invariant checks

**Code**:
```go
// cmd/agentctl/cmd/proto.go
package cmd

import (
    "encoding/json"
    "fmt"
    "os"

    "github.com/jkatigb/agentctl/internal/protocol"
    "github.com/spf13/cobra"
)

var protoCmd = &cobra.Command{
    Use:   "proto",
    Short: "Protocol v1 utilities",
}

var protoValidateCmd = &cobra.Command{
    Use:   "validate",
    Short: "Validate envelope against Protocol v1",
    RunE:  runProtoValidate,
}

var protoValidateInput string

func init() {
    protoValidateCmd.Flags().StringVar(&protoValidateInput, "input", "-", "Input file (- for stdin)")
    protoCmd.AddCommand(protoValidateCmd)
    rootCmd.AddCommand(protoCmd)
}

func runProtoValidate(cmd *cobra.Command, args []string) error {
    var input *os.File
    var err error

    if protoValidateInput == "-" {
        input = os.Stdin
    } else {
        input, err = os.Open(protoValidateInput)
        if err != nil {
            return fmt.Errorf("open input: %w", err)
        }
        defer input.Close()
    }

    var env protocol.Envelope
    if err := json.NewDecoder(input).Decode(&env); err != nil {
        return fmt.Errorf("parse envelope: %w", err)
    }

    if err := protocol.ValidateEnvelope(&env); err != nil {
        fmt.Fprintf(os.Stderr, "Validation failed: %v\n", err)
        os.Exit(1)
    }

    fmt.Println("✓ Envelope valid")
    return nil
}
```

---

## 4. Acceptance Criteria

### 4.1 Go/No-Go Gates for v1.0 RC

**Wire Frozen**:
- [ ] `docs/spec/protocol_v1.md` merged and stable
- [ ] All skills emit conformant envelopes
- [ ] `agentctl proto validate` available
- [ ] Golden fixtures created and passing

**Security**:
- [ ] PathValidator test suite at 100% function coverage
- [ ] Symlink & traversal tests pass
- [ ] All file operations use PathValidator
- [ ] Security model documented

**OpenAPI Skill**:
- [ ] Passes live smoke tests (GitHub, Stripe, Slack)
- [ ] Pagination across ≥2 strategies
- [ ] Automatic pagination handles 100+ pages
- [ ] Retry logic resilient to rate limits
- [ ] Dry-run mode functional

**Retry/Resilience**:
- [ ] Exponential backoff deterministic
- [ ] `Retry-After` honored
- [ ] Partial aggregation on error returns useful info

**Quality**:
- [ ] 85%+ test coverage overall
- [ ] Golden fixtures stable
- [ ] CI verifies conformance
- [ ] Documentation comprehensive

**Documentation**:
- [ ] Root README updated
- [ ] OpenAPI guide with examples
- [ ] Troubleshooting guide (auth, pagination, rate limits)
- [ ] Contributing guide updated

---

## 5. Implementation Guidance

### 5.1 Rough Edges to Avoid

**Don't leak secrets**:
- Redaction helper first
- Call it everywhere (dry-run, errors, logs, envelopes)
- Test with real secrets in CI (use dummy values)

**Don't inline big arrays**:
- Keep CAS rule unconditional
- Even small JSON arrays of 100 items can be 10KB+

**Don't let plugins grow tentacles**:
- Tight IO caps (default 32KB output)
- Strict timeouts (default 500ms)
- No network unless explicitly allowed

**Don't make pagination guessy**:
- Require `max_pages` or `max_items` default caps
- Tell user in `hint` when caps are hit
- Document override in error messages

---

### 5.2 Testing Philosophy

**Tier 0: Unit Tests**
- Minimal specs (bearer, apiKey, basic)
- Small responses (inline)
- Error cases (401, 404, 500, 429)
- Pagination with 2-3 pages

**Tier 1: Live API Tests**
- GitHub (Link header pagination, bearer auth)
- Stripe (cursor pagination, API key)
- OpenWeatherMap (API key, simple JSON)

**Tier 2: Tricky Specs**
- Multiple auth schemes
- Non-standard pagination
- Huge responses (> 1MB)
- Malformed specs (lenient mode)

**Tier 3: Golden Fixtures**
- Stable, deterministic outputs
- CI regression checks
- Documentation examples

---

### 5.3 Development Workflow

**For each SPEC**:
1. Read spec document in `docs/refactoring/`
2. Create branch: `codex/spec-XXX-description`
3. Implement step-by-step per spec plan
4. Write tests as you go
5. Run `make check` frequently
6. Update docs as needed
7. Open PR to `main`
8. Wait for CI + review
9. Squash merge

**CI checks** (must pass):
- Lint (`make lint`)
- Test (`make test`)
- Race detector (`make test-race`)
- Coverage (`make cover` - 50% threshold, target 85%)

---

### 5.4 Debugging Tips

**Envelope validation errors**:
```bash
# Validate envelope
agentctl proto validate --input envelope.json

# Pretty-print envelope
cat envelope.json | jq

# Check envelope against schema
ajv validate -s docs/spec/jsonschema/envelope.schema.json -d envelope.json
```

**OpenAPI issues**:
```bash
# Validate spec
agentctl openapi validate memory:github --strict

# Dry-run request
agentctl run http/openapi \
  --spec memory:github \
  --operationId listRepos \
  --dry_run

# Live call with verbose output
AGENTCTL_LOG_LEVEL=debug agentctl run http/openapi \
  --spec memory:github \
  --operationId listRepos
```

**Plugin issues**:
```bash
# Test plugin directly
echo '{...}' | agentctl-plugin-aws-sigv4

# Check plugin handshake
agentctl-plugin-aws-sigv4 --handshake

# Verify plugin in path
which agentctl-plugin-aws-sigv4
```

---

## 6. Testing Strategy

### 6.1 Unit Tests

**Location**: `*_test.go` files alongside implementation

**Coverage target**: 85%

**Key areas**:
- Envelope validation (100% of error codes)
- PathValidator (100% function coverage)
- OpenAPI loader (file/CAS/memory paths)
- Request builder (all param types)
- Pagination detection (all strategies)
- Retry backoff (jitter, Retry-After)

### 6.2 Integration Tests

**Location**: `cmd/agentctl/cmd/e2e_test.go`

**Scenarios**:
- End-to-end skill execution
- OpenAPI call with pagination
- CAS integration
- Memory storage and retrieval
- Job submission and completion

### 6.3 Golden Tests

**Location**: `test/golden/`

**Purpose**: Prevent wire format regressions

**Process**:
1. Generate golden fixtures from real outputs
2. Commit fixtures to git
3. CI compares new outputs to golden files
4. Fail on any diff

**Coverage**:
- All envelope statuses (`ok`, `error`, `progress`)
- All error codes
- Inline vs CAS responses
- Streaming (NDJSON)

### 6.4 Live API Tests

**Location**: `test/integration/openapi_live_test.go`

**APIs**:
- GitHub (https://api.github.com)
- Stripe (https://api.stripe.com) - test mode
- OpenWeatherMap (https://api.openweathermap.org)

**Scenarios**:
- Auth (bearer, API key)
- Pagination (Link headers, cursor, offset)
- Error handling (401, 404, 429, 500)
- Large responses (>100KB)

**Note**: Use `// +build integration` tag and `AGENTCTL_INTEGRATION_TESTS=1` env var

---

## Appendix A: Phased Checklist

### Phase 3A: Refactors ✅
- [ ] SPEC-008: Package reorganization + depguard
- [ ] SPEC-009: Skill discovery extraction

### Phase 3B: Security ✅
- [ ] SPEC-011: PathValidator hardening (100% coverage)

### Phase 4: OpenAPI Core ✅
- [ ] SPEC-012: Spec loader (file/CAS/memory, cache, index)
- [ ] SPEC-013: Request builder (validation, templating, dry-run)
- [ ] SPEC-014: HTTP client (response, CAS, summary)
- [ ] SPEC-015: Pagination (link/cursor/offset, auto-detect)
- [ ] SPEC-016: Retry logic (backoff, Retry-After)

### Phase 5: Quality ✅
- [ ] SPEC-018: Golden fixtures + conformance CLI
- [ ] SPEC-019: Documentation + README

### Cross-Cutting ✅
- [ ] Protocol package (`internal/protocol`)
- [ ] `agentctl proto validate` command
- [ ] Redaction helpers
- [ ] CI enforcement

---

## Appendix B: Quick Reference

**Useful Commands**:
```bash
# Validate envelope
agentctl proto validate --input envelope.json

# Import OpenAPI spec
agentctl openapi import github.yaml --as github

# Dry-run OpenAPI call
agentctl run http/openapi --spec memory:github --operationId listRepos --dry_run

# Live call with pagination
agentctl run http/openapi --spec memory:github --operationId listRepos --paging.max_items=500

# Run tests
make test

# Check coverage
make cover

# Lint code
make lint

# Run golden fixture tests
make test-golden
```

**File Paths**:
- Protocol spec: `docs/spec/protocol_v1.md`
- Plugin protocol: `docs/spec/plugin_protocol.md`
- Core profile: `docs/spec/core_profile_v1.md`
- OpenAPI skill: `docs/spec/openapi_skill.md`
- Agent loop guide: `docs/guides/agent_loop.md`
- Implementation plan: `docs/guides/protocol_v1_implementation.md` (this file)

---

**Document Status**: Implementation Roadmap
**Target Completion**: v1.0 RC in 11 weeks
**Related Specs**: Protocol v1, Core Profile v1, All SPECs (008-019)
