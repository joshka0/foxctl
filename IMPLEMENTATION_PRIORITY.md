# Implementation Priority

This document outlines the current priority order for implementing features and fixes in agentctl as we approach v1.0.

## Current Phase: 6 (OpenAPI Skill Implementation)

**Timeline**: 3-4 months to v1.0 release

## P0: Critical (Blocking v1.0)

### 1. Complete SPEC-008: Reorganize Packages (~3h remaining)
**Status**: 90% complete
**Effort**: 3 hours
**Impact**: Clean architecture, enforced layering

Final import cleanup and dependency graph verification after moving packages to domain/storage/execution/platform structure.

**Files to update**:
- Import statements across codebase
- Dependency verification tests

---

### 2. Complete SPEC-009: Extract Skill Discovery (~7h remaining)
**Status**: 30% complete
**Effort**: 7 hours
**Impact**: Reusability, clean architecture

Move skill discovery logic from `cmd/agentctl/cmd/skill_helpers.go` to `internal/domain/skill/discovery.go`.

**Key changes**:
```go
// Before: cmd layer
func findSkill(name string) (SkillHandle, error) { ... }

// After: domain layer
package skill

type Resolver struct { ... }
func (r *Resolver) Resolve(ctx context.Context, ref string) (*Handle, error) { ... }
```

**Enables**: Daemon mode, API access, better testing

---

### 3. SPEC-011: PathValidator Hardening (5.5h)
**Status**: Not started
**Effort**: 5.5 hours
**Impact**: Security - prevent workspace escapes

**Security vulnerabilities to address**:
- Symlink attacks
- Path traversal (`../../../etc/passwd`)
- Null byte injection
- Unicode normalization exploits
- Partial path matching bypasses

**Example hardening**:
```go
// Current: Basic validation
func (v *PathValidator) ValidatePath(path string) error { ... }

// Enhanced: Comprehensive security checks
func (v *PathValidator) ValidatePath(userPath string) (canonical string, error) {
    // 1. Null byte check
    // 2. Clean path
    // 3. Resolve symlinks (or reject them)
    // 4. Canonicalize
    // 5. Verify within workspace
    // 6. Prevent partial matches
}
```

**Files**:
- `internal/domain/policy/policy.go`
- `internal/domain/policy/pathvalidator_test.go` (100% coverage target)

**Acceptance**: All security tests pass, zero escape attempts succeed

---

### 4. SPEC-012: OpenAPI Spec Loader (10h)
**Status**: Not started
**Effort**: 10 hours
**Impact**: Foundation for OpenAPI skill

Load OpenAPI specs from multiple sources:
- File paths (`/path/to/spec.yaml`)
- CAS digests (`sha256:abc123...`)
- Named memory (`memory:github-api`)

**Key components**:
```go
// internal/openapi/loader/loader.go
type Loader struct {
    casStore    storage.CASStore
    memoryStore storage.MemoryStore
    cache       map[string]*Spec
}

func (l *Loader) Load(ctx context.Context, ref string) (*Spec, error)
func (s *Spec) GetOperation(operationID string) (*Operation, error)
```

**Deliverables**:
- Parse OpenAPI 3.0.x and 3.1.x
- Reject Swagger 2.0
- Index operations by operationId
- Import command: `agentctl openapi import spec.yaml --as github-api`

---

### 5. SPEC-013: OpenAPI Request Builder (12h)
**Status**: Not started
**Effort**: 12 hours
**Impact**: Parameter validation, request construction

Build HTTP requests from OpenAPI operation definitions:
- Path parameter resolution (`/users/{userId}` → `/users/123`)
- Query parameter encoding
- Header application
- Body serialization (JSON, form-data)
- Content-Type negotiation

**Key features**:
- Pre-flight parameter validation
- Actionable error messages (EARG with hints)
- Schema validation

**Depends on**: SPEC-012

---

### 6. SPEC-014: OpenAPI HTTP Client & Response Processing (15h)
**Status**: Not started
**Effort**: 15 hours
**Impact**: Execute real API calls

Execute HTTP requests and process responses:
- Small responses (<32KB) → inline in envelope
- Large responses → CAS with summary + preview
- 4xx errors → inline (for debugging)
- Timing metrics (DNS, connect, TLS, total)
- Header sanitization (redact sensitive headers)

**Response processing**:
```go
type Response struct {
    StatusCode  int               `json:"status_code"`
    Headers     map[string]string `json:"headers"`
    Body        interface{}       `json:"body,omitempty"`        // Small responses
    Digest      string            `json:"digest,omitempty"`      // Large → CAS
    Preview     string            `json:"preview,omitempty"`     // First 5 records
    RecordCount int               `json:"record_count,omitempty"`
    Timing      Timing            `json:"timing"`
}
```

**Depends on**: SPEC-012, SPEC-013

---

## P1: High Priority (Should have for v1.0)

### 7. SPEC-015: OpenAPI Pagination (10h)
**Status**: Not started
**Effort**: 10 hours
**Impact**: Multi-page API responses

Support automatic pagination strategies:
- **Link headers** (RFC 5988): `Link: <url>; rel="next"`
- **Cursor-based**: `{"next_cursor": "abc", "data": [...]}`
- **Offset/limit**: `?offset=100&limit=50` with total count

**Config**:
```json
{
  "paging": {
    "strategy": "link",
    "max_pages": 10,
    "max_records": 1000
  }
}
```

**Depends on**: SPEC-014

---

### 8. SPEC-016: OpenAPI Retry Logic (8h)
**Status**: Not started
**Effort**: 8 hours
**Impact**: Resilience to transient errors

Exponential backoff with jitter:
- Retry 429 (rate limit), 500, 502, 503, 504
- Respect `Retry-After` header
- Max attempts configurable (default: 3)
- Jitter to prevent thundering herd

**Depends on**: SPEC-014

---

### 9. SPEC-018: Golden Test Fixtures (8h)
**Status**: Not started
**Effort**: 8 hours
**Impact**: Regression prevention, test quality

Create comprehensive golden fixtures:
```
test/golden/
├── envelopes/      # All envelope formats
├── openapi/        # API responses (inline, CAS, paginated)
└── skills/         # Skill outputs
```

**Coverage**:
- All error codes (EARG, ERUNTIME, EAUTH, etc.)
- All envelope types (ok, error, progress)
- OpenAPI responses (success, error, pagination)
- CAS wrappers with summaries

---

### 10. SPEC-019: Root README & Documentation (5h)
**Status**: Not started
**Effort**: 5 hours
**Impact**: User onboarding, project visibility

Create missing documentation:
- `README.md` - Quick start, features, installation
- `CONTRIBUTING.md` - Development guide
- `docs/SECURITY.md` - Security policy
- `docs/TROUBLESHOOTING.md` - Common issues

**Current gap**: Only `AGENTS.md` exists (for AI assistants)

---

## P2: Medium Priority (Nice to have, can defer)

### 11. SPEC-017: Plugin Protocol Implementation (20h)
**Status**: Not started
**Effort**: 20 hours
**Impact**: Extensibility for custom auth/pagination

**Can defer to v1.1** - Built-in auth/pagination sufficient for v1.0

Implement plugin protocol for:
- Custom authentication schemes (HMAC, custom signatures)
- Vendor-specific pagination strategies
- Subprocess communication via JSON envelopes

**Plugin types**:
- `plugin/auth` - Custom auth handlers
- `plugin/pagination` - Custom paging strategies

**Example plugins**:
- `auth-hmac/` - HMAC signature auth
- `paging-custom/` - Vendor-specific pagination

---

## Implementation Sequence (Gantt-style)

```
Week 1-2:   SPEC-008 (finish), SPEC-009 (finish), SPEC-011 (security)
            ↓
Week 3-4:   SPEC-012 (spec loader)
            ↓
Week 5-6:   SPEC-013 (request builder)
            ↓
Week 7-8:   SPEC-014 (HTTP client)
            ↓
Week 9-10:  SPEC-015 (pagination) + SPEC-016 (retry) [parallel]
            ↓
Week 11-12: SPEC-018 (golden tests) + SPEC-019 (docs) [parallel]
            ↓
Week 13:    Integration testing, bug fixes, v1.0-rc1
            ↓
Week 14:    v1.0 release

Optional (v1.1):
Week 15+:   SPEC-017 (plugin protocol)
```

## Effort Summary

| Priority | Specs | Total Hours | Weeks |
|----------|-------|-------------|-------|
| P0 (Critical) | 6 | ~52.5h | 6-7 weeks |
| P1 (High) | 4 | ~31h | 4 weeks |
| P2 (Medium) | 1 | ~20h | 2-3 weeks |
| **Total to v1.0** | **10** | **~83.5h** | **10-11 weeks** |
| Optional (v1.1) | 1 | ~20h | 2-3 weeks |

## Success Criteria for v1.0

### Functionality
- ✅ OpenAPI skill works end-to-end with real APIs
- ✅ GitHub API: list repos, create issue, search
- ✅ Stripe API: list customers, create charge
- ✅ Pagination handles 100+ pages
- ✅ Retry survives rate limits and transient errors
- ✅ PathValidator prevents all escape attempts

### Quality
- ✅ Test coverage ≥ 85%
- ✅ Golden fixtures for all scenarios
- ✅ Zero critical security warnings
- ✅ CI/CD green (lint, test, race, coverage)
- ✅ E2E tests for complete workflows

### Documentation
- ✅ Root README
- ✅ CONTRIBUTING guide
- ✅ 5+ real-world API examples
- ✅ Troubleshooting guide
- ✅ Security policy

### Performance
- ✅ Spec loading < 100ms (cached)
- ✅ Request building < 10ms
- ✅ CAS operations < 50ms for 10MB files

## References

- [Refactoring Specs](docs/refactoring/README.md) - Detailed specs for each item
- [Core Profile v1](docs/spec/core_profile_v1.md) - Authoritative specification
- [OpenAPI Skill Spec](docs/spec/openapi_skill.md) - Detailed OpenAPI design
- [ROADMAP_TO_V1.md](ROADMAP_TO_V1.md) - High-level roadmap

## Notes

- **Previous P1 (PathValidator)** is now SPEC-011 in critical path
- **Plugin protocol** deferred to v1.1 - not blocking
- **SPEC-008/009** nearly done, just cleanup needed
- **OpenAPI (SPEC-012-016)** is the longest critical path (~55h)
- **Parallel work possible**: Security (SPEC-011) can run alongside OpenAPI specs
