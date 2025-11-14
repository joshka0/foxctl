# Refactoring Specifications

This directory contains detailed specifications for refactoring and improving the agentctl codebase according to Go best practices.

## Overview

These specifications address code quality issues identified through comprehensive codebase analysis, focusing on:
- **Architecture**: Better separation of concerns and layered design
- **Maintainability**: Reducing code duplication and complexity
- **Testability**: Introducing interfaces and dependency injection
- **Reliability**: Improving error handling and data integrity

## Specifications Index

### Critical Priority

| Spec | Title | Complexity | Est. Time | Status |
|------|-------|------------|-----------|--------|
| [SPEC-001](SPEC-001-storage-interfaces.md) | Storage Interfaces | High | 18h | Draft |
| [SPEC-002](SPEC-002-refactor-run-command.md) | Refactor Run Command | High | 18h | Draft |
| [SPEC-006](SPEC-006-fix-error-handling.md) | Fix Error Handling | Medium | 14h | Draft |

**Total Critical**: 50 hours

### High Priority

| Spec | Title | Complexity | Est. Time | Status |
|------|-------|------------|-----------|--------|
| [SPEC-003](SPEC-003-database-scanning-helpers.md) | Database Scanning Helpers | Medium | 8h | Draft |
| [SPEC-004](SPEC-004-skill-executor-interface.md) | SkillExecutor Interface | Medium | 11.5h | Draft |
| [SPEC-005](SPEC-005-artifact-management.md) | Artifact Management Extraction | Medium | 13h | Draft |

**Total High**: 32.5 hours

### Medium Priority

| Spec | Title | Complexity | Est. Time | Status |
|------|-------|------------|-----------|--------|
| [SPEC-007](SPEC-007-replace-long-parameter-lists.md) | Replace Long Parameter Lists | Low | 9.5h | Draft |
| [SPEC-008](SPEC-008-reorganize-packages.md) | Reorganize Internal Packages | High | 15h | Draft |
| [SPEC-009](SPEC-009-extract-skill-discovery.md) | Extract Skill Discovery Logic | Low | 10.5h | Draft |

**Total Medium**: 35 hours

### Low Priority

| Spec | Title | Complexity | Est. Time | Status |
|------|-------|------------|-----------|--------|
| [SPEC-010](SPEC-010-sql-utilities.md) | Create SQL Utilities Package | Low | 10h | Draft |

**Total Low**: 10 hours

## Grand Total Estimate

**127.5 hours** (~16 working days or 3-4 weeks)

## Implementation Sequence

### Phase 1: Foundation (Critical) - 50 hours
Build the architectural foundation by introducing interfaces and fixing critical issues.

1. **SPEC-001: Storage Interfaces** (18h)
   - Creates the interface layer for all storage implementations
   - **Enables**: Better testing, dependency injection
   - **Blocks**: SPEC-004, SPEC-005

2. **SPEC-002: Refactor Run Command** (18h)
   - Breaks down 180-line monolithic function
   - **Enables**: Better maintainability, clearer code
   - **Uses**: SPEC-001 interfaces

3. **SPEC-006: Fix Error Handling** (14h)
   - Eliminates 306 ignored errors
   - **Enables**: Better reliability, data integrity
   - **Critical**: Prevents data corruption

### Phase 2: Cleanup (High Priority) - 32.5 hours
Reduce duplication and improve architecture.

4. **SPEC-003: Database Scanning Helpers** (8h)
   - Eliminates repeated scanning code
   - **Enables**: Consistent error handling
   - **Depends on**: SPEC-006 (error handling patterns)

5. **SPEC-004: SkillExecutor Interface** (11.5h)
   - Decouples jobs from execution
   - **Enables**: Better testing, flexibility
   - **Depends on**: SPEC-001

6. **SPEC-005: Artifact Management** (13h)
   - Centralizes artifact lifecycle management
   - **Enables**: Consistent artifact handling
   - **Depends on**: SPEC-001

### Phase 3: Organization (Medium Priority) - 35 hours
Improve code organization and API design.

7. **SPEC-007: Replace Long Parameter Lists** (9.5h)
   - Makes APIs cleaner and more maintainable
   - **Enables**: Better API design
   - **Can run in parallel** with other specs

8. **SPEC-008: Reorganize Packages** (15h)
   - Creates layered architecture
   - **Enables**: Clear boundaries, enforced dependencies
   - **Should be done after**: All other specs (moves code created in earlier specs)

9. **SPEC-009: Extract Skill Discovery** (10.5h)
   - Moves business logic from cmd to domain
   - **Enables**: Reusability, better architecture
   - **Can run in parallel** with other specs

### Phase 4: Polish (Low Priority) - 10 hours
Add nice-to-have utilities.

10. **SPEC-010: SQL Utilities** (10h)
    - Adds transaction and query helpers
    - **Enables**: Less boilerplate
    - **Complementary to**: SPEC-003

## Dependency Graph

```
┌─────────────────────────────────────────────────────────────┐
│ Phase 1: Foundation                                         │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  SPEC-001: Storage Interfaces (18h)                        │
│      │                                                       │
│      ├──> SPEC-002: Refactor Run Command (18h)             │
│      │                                                       │
│      └──> SPEC-006: Fix Error Handling (14h)               │
│                │                                             │
└────────────────┼─────────────────────────────────────────────┘
                 │
┌────────────────┼─────────────────────────────────────────────┐
│ Phase 2: Cleanup                                            │
├────────────────┼─────────────────────────────────────────────┤
│                │                                             │
│                └──> SPEC-003: Database Scanning (8h)        │
│                                                              │
│  SPEC-001 ──> SPEC-004: SkillExecutor Interface (11.5h)    │
│           │                                                  │
│           └──> SPEC-005: Artifact Management (13h)          │
│                                                              │
└──────────────────────────────────────────────────────────────┘
                 │
┌────────────────┼─────────────────────────────────────────────┐
│ Phase 3: Organization                                       │
├────────────────┼─────────────────────────────────────────────┤
│                │                                             │
│  (parallel) ──> SPEC-007: Replace Long Params (9.5h)        │
│                │                                             │
│  (parallel) ──> SPEC-009: Extract Skill Discovery (10.5h)   │
│                │                                             │
│  (all above) ──> SPEC-008: Reorganize Packages (15h)        │
│                                                              │
└──────────────────────────────────────────────────────────────┘
                 │
┌────────────────┼─────────────────────────────────────────────┐
│ Phase 4: Polish                                             │
├────────────────┼─────────────────────────────────────────────┤
│                │                                             │
│  SPEC-003 ──> SPEC-010: SQL Utilities (10h)                │
│                                                              │
└──────────────────────────────────────────────────────────────┘
```

## Quick Reference

### By Impact

**Highest Impact** (Do First):
1. SPEC-001: Storage Interfaces - Enables everything else
2. SPEC-006: Fix Error Handling - Prevents data corruption
3. SPEC-002: Refactor Run Command - Most visible improvement

**High Impact**:
4. SPEC-004: SkillExecutor Interface - Major decoupling
5. SPEC-003: Database Scanning - Eliminates most duplication
6. SPEC-008: Reorganize Packages - Clearest architecture

**Medium Impact**:
7. SPEC-005: Artifact Management - Centralizes scattered logic
8. SPEC-009: Extract Skill Discovery - Better reusability

**Lower Impact** (Polish):
9. SPEC-007: Replace Long Parameters - API cleanup
10. SPEC-010: SQL Utilities - Nice-to-have helpers

### By Effort

**High Effort** (15+ hours):
- SPEC-001: Storage Interfaces (18h)
- SPEC-002: Refactor Run Command (18h)
- SPEC-008: Reorganize Packages (15h)

**Medium Effort** (10-14 hours):
- SPEC-006: Fix Error Handling (14h)
- SPEC-005: Artifact Management (13h)
- SPEC-004: SkillExecutor Interface (11.5h)
- SPEC-009: Extract Skill Discovery (10.5h)
- SPEC-010: SQL Utilities (10h)

**Low Effort** (< 10 hours):
- SPEC-007: Replace Long Parameters (9.5h)
- SPEC-003: Database Scanning (8h)

### By Risk

**Low Risk** (Safe to implement):
- SPEC-003: Database Scanning (pure addition)
- SPEC-007: Replace Long Parameters (backward compatible)
- SPEC-009: Extract Skill Discovery (clear extraction)
- SPEC-010: SQL Utilities (optional helpers)

**Medium Risk** (Requires careful migration):
- SPEC-001: Storage Interfaces (big change, but type aliases help)
- SPEC-004: SkillExecutor Interface (dependency injection)
- SPEC-005: Artifact Management (refactoring shared logic)

**Higher Risk** (Complex refactoring):
- SPEC-002: Refactor Run Command (major restructuring)
- SPEC-006: Fix Error Handling (may expose edge cases)
- SPEC-008: Reorganize Packages (moves everything)

## Success Metrics

### Code Quality Improvements
- **Ignored Errors**: 306 → <60 (-80%)
- **Lines of Duplicated Code**: ~145 → ~10 (-93%)
- **Average Function Length**: 30 lines → 15 lines (-50%)
- **Cyclomatic Complexity**: Max 15 → Max 10 (-33%)
- **Package Coupling**: 13 flat packages → 5 layered domains

### Architectural Improvements
- **Interfaces**: 0 → 7+ core interfaces
- **Testability**: Limited mocking → Full mock support
- **Dependency Direction**: Unclear → Explicit layering
- **Separation of Concerns**: Mixed → Clear boundaries

### Maintainability Improvements
- **Code Organization**: Flat → Layered
- **Reusability**: CLI-only → Framework-ready
- **Documentation**: Scattered → Comprehensive specs
- **Onboarding**: Difficult → Clear architecture

## Contributing

When implementing these specs:

1. **Read the full spec** before starting
2. **Follow the implementation plan** step-by-step
3. **Run tests after each step**
4. **Update the spec status** when complete
5. **Document any deviations** from the plan

## Questions or Feedback

For questions or suggestions about these specs:
- Open an issue in the repository
- Reference the specific SPEC number
- Include context about your use case

## License

These specifications are part of the agentctl project and follow the same license.
