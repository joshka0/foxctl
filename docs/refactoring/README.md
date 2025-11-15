# Refactoring Specifications

This directory contains detailed specifications for improving the agentctl codebase.

## Status Overview

### ✅ Completed (8 specs - moved to completed/)

All Phase 1-2 refactoring specs have been implemented:

| Spec | Title | Status |
|------|-------|--------|
| SPEC-001 | Storage Interfaces | ✅ Completed |
| SPEC-002 | Refactor Run Command | ✅ Completed |
| SPEC-003 | Database Scanning Helpers | ✅ Completed |
| SPEC-004 | SkillExecutor Interface | ✅ Completed |
| SPEC-005 | Artifact Management | ✅ Completed |
| SPEC-006 | Fix Error Handling | ✅ Completed |
| SPEC-007 | Replace Long Parameter Lists | ✅ Completed |
| SPEC-010 | SQL Utilities | ✅ Completed |

See [completed/README.md](completed/README.md) for details.

### ✅ Recently Completed

| Spec | Title | Status |
|------|-------|--------|
| [SPEC-008](SPEC-008-reorganize-packages.md) | Reorganize Packages | ✅ Completed (minor violations to fix) |

### 🚧 In Progress (1 spec)

| Spec | Title | Progress | Effort Remaining |
|------|-------|----------|------------------|
| [SPEC-009](SPEC-009-extract-skill-discovery.md) | Extract Skill Discovery | 30% | 7h |

### 📋 Phase 6-8: Road to v1.0 (9 new specs)

#### Critical Priority (Security + OpenAPI Core)

| Spec | Title | Complexity | Estimate | Blocks |
|------|-------|------------|----------|--------|
| [SPEC-011](SPEC-011-pathvalidator-hardening.md) | PathValidator Hardening | Low | 5.5h | Security |
| [SPEC-012](SPEC-012-openapi-spec-loader.md) | OpenAPI Spec Loader | Medium | 10h | SPEC-013/14 |
| [SPEC-013](SPEC-013-openapi-request-builder.md) | OpenAPI Request Builder | High | 12h | SPEC-014 |
| [SPEC-014](SPEC-014-openapi-http-client.md) | OpenAPI HTTP Client | High | 15h | SPEC-15/16 |

**Subtotal Critical**: 42.5 hours

#### High Priority (OpenAPI Complete)

| Spec | Title | Complexity | Estimate |
|------|-------|------------|----------|
| [SPEC-015](SPEC-015-openapi-pagination.md) | OpenAPI Pagination | Medium | 10h |
| [SPEC-016](SPEC-016-openapi-retry-logic.md) | OpenAPI Retry Logic | Low | 8h |

**Subtotal High**: 18 hours

#### Medium Priority (Extensibility + Quality)

| Spec | Title | Complexity | Estimate |
|------|-------|------------|----------|
| [SPEC-017](SPEC-017-plugin-protocol.md) | Plugin Protocol | High | 20h |
| [SPEC-018](SPEC-018-golden-test-fixtures.md) | Golden Test Fixtures | Low | 8h |
| [SPEC-019](SPEC-019-documentation-readme.md) | Root README & Docs | Low | 5h |

**Subtotal Medium**: 33 hours

## Total Effort to v1.0

**Remaining Work**: ~99 hours (12.5 working days)
- In Progress (SPEC-009): ~7h
- Follow-up (SPEC-008 violations): ~3h
- Critical (SPEC-011-014): ~42.5h
- High (SPEC-015-016): ~18h
- Medium (SPEC-017-019): ~33h

## Implementation Sequence

### Phase 3A: Complete In-Progress (1 week)
1. ~~**SPEC-008**: Finish package reorganization~~ ✅ **COMPLETED**
   - Follow-up: Fix layer violations (3h) - can be deferred
2. **SPEC-009**: Extract skill discovery (7h)

### Phase 3B: Security (1 week)
3. **SPEC-011**: PathValidator hardening (5.5h)

### Phase 4: OpenAPI Core (3-4 weeks) 🎯
4. **SPEC-012**: Spec Loader (10h)
5. **SPEC-013**: Request Builder (12h)
6. **SPEC-014**: HTTP Client & Response (15h)
7. **SPEC-015**: Pagination (10h)
8. **SPEC-016**: Retry Logic (8h)

**Milestone**: Working OpenAPI skill, API calls functional

### Phase 5: Quality & Extensibility (2 weeks)
9. **SPEC-018**: Golden Fixtures (8h)
10. **SPEC-019**: Documentation (5h)
11. **SPEC-017**: Plugin Protocol (20h) - *optional for v1.0*

**Milestone**: v1.0 Release Candidate

## Dependency Graph

```
Phase 3A (Complete Refactoring)
├─ SPEC-008: Reorganize Packages (90% done)
└─ SPEC-009: Extract Skill Discovery (30% done)

Phase 3B (Security)
└─ SPEC-011: PathValidator Hardening
    └─ Can run in parallel with Phase 4

Phase 4 (OpenAPI - Critical Path)
└─ SPEC-012: Spec Loader
    └─ SPEC-013: Request Builder
        └─ SPEC-014: HTTP Client
            ├─ SPEC-015: Pagination
            └─ SPEC-016: Retry Logic

Phase 5 (Quality)
├─ SPEC-018: Golden Fixtures (depends on OpenAPI completion)
├─ SPEC-019: Documentation (can run anytime)
└─ SPEC-017: Plugin Protocol (depends on SPEC-012-016)
```

## Success Metrics for v1.0

### Functionality
- ✅ All Phase 0-5 features implemented
- ✅ OpenAPI skill works with real APIs (GitHub, Stripe, Slack)
- ✅ Pagination handles 100+ page responses
- ✅ Retry logic resilient to 429/5xx errors
- ✅ PathValidator prevents all escape attempts

### Quality
- ✅ Test coverage ≥ 85%
- ✅ Golden fixtures for all envelope types
- ✅ Zero critical security warnings
- ✅ CI/CD passing (lint, test, race, coverage)

### Documentation
- ✅ Root README for new users
- ✅ CONTRIBUTING guide
- ✅ Security policy
- ✅ Troubleshooting guide
- ✅ API examples (5+ real-world cases)

### Performance
- ✅ Spec loading < 100ms (cached)
- ✅ Request building < 10ms
- ✅ CAS write/read < 50ms for 10MB files

## Quick Reference

### By Priority

**Critical** (Must have for v1.0):
- SPEC-008, 009, 011, 012, 013, 014

**High** (Should have for v1.0):
- SPEC-015, 016, 018, 019

**Medium** (Nice to have, can defer):
- SPEC-017 (plugins optional for v1.0)

### By Effort

**Quick wins** (< 10h):
- SPEC-008 (3h remaining)
- SPEC-011 (5.5h)
- SPEC-016 (8h)
- SPEC-018 (8h)
- SPEC-019 (5h)

**Medium effort** (10-15h):
- SPEC-009 (7h remaining)
- SPEC-012 (10h)
- SPEC-013 (12h)
- SPEC-014 (15h)
- SPEC-015 (10h)

**Large effort** (15+h):
- SPEC-017 (20h)

## Contributing

When implementing specs:

1. **Read the full spec** before starting
2. **Follow the implementation plan** step-by-step
3. **Run tests after each step** (`make test`)
4. **Update spec status** when complete
5. **Move to completed/** when merged

## Notes

- **Plugin protocol (SPEC-017)** is optional for v1.0 - can ship as v1.1
- **SPEC-008/009** are 80% done, just need final cleanup
- **OpenAPI specs (012-016)** are the critical path - ~55h of focused work
- All completed specs moved to `completed/` for reference

See [../spec/core_profile_v1.md](../spec/core_profile_v1.md) for the authoritative specification.
