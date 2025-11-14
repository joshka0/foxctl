# agentctl Roadmap to v1.0

**Status**: Phase 6 (OpenAPI Implementation)
**Target Release**: Q1 2026 (3-4 months)
**Current Completion**: ~60%

---

## Vision

**agentctl v1.0** will be a production-ready CLI for LLM-powered workflows featuring:
- ✅ **Structured I/O**: JSON envelopes (canonical, deterministic)
- ✅ **Skill System**: Sandboxed tools (WASI + exec)
- ✅ **Job Persistence**: Durable async execution
- ✅ **Content Storage**: SHA-256 CAS with integrity checks
- ✅ **Memory**: Auto-cache (24h) + named persistence
- 🚧 **OpenAPI Client**: Universal REST API access (in progress)
- 🔜 **Security**: Hardened path validation
- 🔜 **Documentation**: Comprehensive user guides

---

## Phase Status

### ✅ Phase 0-2: Foundation (100% complete)
**Shipped**: Bootstrap, JSON envelopes, CAS, CLI foundation

- Repository scaffolding & CI/CD
- JSON envelope format (Version 1)
- Content-addressable storage
- Configuration system (Viper)
- Logging infrastructure (Zerolog)

### ✅ Phase 3: Jobs Subsystem (100% complete)
**Shipped**: SQLite-backed durable jobs with crash recovery

- Job persistence & lifecycle management
- Async execution with progress tracking (NDJSON)
- Deduplication (`--dedupe` flag)
- Crash recovery (orphaned jobs → error state)
- Job CLI: `list`, `get`, `result`, `tail`, `cancel`, `submit`

### ✅ Phase 4: Runners & Skills (100% complete)
**Shipped**: Skill execution with WASI and exec runners

- Skill manifest parser & validator
- WASI runner (wazero, pure Go)
- Exec runner (native processes with rlimits)
- Built-in skills: `fs/ls`, `fs/read`, `text/grep`, `todo/manage`, `wasi/echo`
- Skill discovery & installation
- Policy enforcement (workspace confinement, egress control)

### ✅ Phase 5: Cache & Memory (100% complete)
**Shipped**: Deterministic caching + named memories

- Auto-cache (24h TTL, keyed by canonical inputs)
- Cache modes: `off`, `auto`, `only`
- Named memory storage
- Memory search & relevance ranking
- SQLite-backed persistence

### 🚧 Phase 6: OpenAPI Skill (5% complete, in progress)
**Target**: Universal REST API client without codegen

**Current State**: Dry-run stub only

**Remaining Work** (~55h):
1. **Spec Loader** (SPEC-012, 10h)
   - Load from file, CAS, or memory
   - Parse OpenAPI 3.0.x/3.1.x
   - Index operations by operationId
   - Import command: `agentctl openapi import`

2. **Request Builder** (SPEC-013, 12h)
   - Path parameter resolution
   - Query/header/body handling
   - Parameter validation
   - Actionable error messages

3. **HTTP Client** (SPEC-014, 15h)
   - Execute requests
   - Small responses → inline
   - Large responses → CAS with preview
   - Timing metrics
   - Header sanitization

4. **Pagination** (SPEC-015, 10h)
   - Link headers (RFC 5988)
   - Cursor-based pagination
   - Offset/limit strategies

5. **Retry Logic** (SPEC-016, 8h)
   - Exponential backoff with jitter
   - Respect `Retry-After` headers
   - Retry 429/5xx errors

**Acceptance**: Call GitHub, Stripe, Slack APIs successfully

### 🔜 Phase 7: Plugin SPI (0% complete)
**Status**: DEFERRED to v1.1 (not blocking v1.0)

Plugin protocol for:
- Custom authentication schemes
- Vendor-specific pagination
- Subprocess communication via envelopes

**Rationale**: Built-in auth/pagination sufficient for v1.0

### 🔜 Phase 8: Jobs Integration & UX (75% complete)
**Remaining**:
- Golden test fixtures (SPEC-018, 8h)
- Root README & documentation (SPEC-019, 5h)
- E2E test expansion
- GC memory reference completion

---

## Timeline to v1.0

### Month 1 (Weeks 1-4): Cleanup + Security
**Goal**: Close out refactoring, harden security

- Week 1: Complete SPEC-008 (package reorganization)
- Week 2: Complete SPEC-009 (skill discovery extraction)
- Week 3-4: SPEC-011 (PathValidator hardening)

**Milestone**: Clean architecture, secure path validation

### Month 2 (Weeks 5-8): OpenAPI Core
**Goal**: Spec loading, request building, HTTP execution

- Week 5: SPEC-012 (Spec Loader)
- Week 6: SPEC-013 (Request Builder)
- Week 7-8: SPEC-014 (HTTP Client & Response)

**Milestone**: Basic API calls working (no pagination/retry yet)

### Month 3 (Weeks 9-12): OpenAPI Complete
**Goal**: Production-ready OpenAPI skill

- Week 9-10: SPEC-015 (Pagination) + SPEC-016 (Retry) [parallel]
- Week 11: SPEC-018 (Golden Test Fixtures)
- Week 12: SPEC-019 (Documentation)

**Milestone**: Full-featured OpenAPI skill

### Month 4 (Weeks 13-14): Release Prep
**Goal**: v1.0 release

- Week 13: Integration testing, bug fixes, v1.0-rc1
- Week 14: Final testing, v1.0 release

**Deliverable**: agentctl v1.0 🎉

---

## Critical Path (Gantt Chart)

```
Weeks 1-2:  [SPEC-008] [SPEC-009]
             └────────┬────────┘
Weeks 3-4:          [SPEC-011 Security] (can run parallel with below)
                        ↓
Weeks 5-6:          [SPEC-012 Loader] → [SPEC-013 Builder]
                                              ↓
Weeks 7-8:                              [SPEC-014 HTTP Client]
                                         ↓             ↓
Weeks 9-10:                    [SPEC-015 Paging] [SPEC-016 Retry]
                                         ↓             ↓
Weeks 11-12:                    [SPEC-018 Tests] [SPEC-019 Docs]
                                              ↓
Weeks 13-14:                         [Testing & Release]
```

**Longest path**: SPEC-012 → SPEC-013 → SPEC-014 → SPEC-015/16 (~55h)

---

## Effort Summary

| Phase | Status | Effort | Remaining |
|-------|--------|--------|-----------|
| Phase 0-2 | ✅ Complete | - | 0h |
| Phase 3 | ✅ Complete | - | 0h |
| Phase 4 | ✅ Complete | - | 0h |
| Phase 5 | ✅ Complete | - | 0h |
| **Phase 6** | 🚧 5% | **~55h** | **55h** |
| Phase 7 | 🔜 Deferred | ~20h | 0h (v1.1) |
| Phase 8 | 🔜 75% | ~13h | **13h** |
| **Refactoring** | 🚧 80% | **~9h** | **9h** |
| **Security** | 🔜 Not started | **~5.5h** | **5.5h** |
| **Documentation** | 🔜 Not started | **~5h** | **5h** |
| | | **Total** | **~87.5h** |

**Approximately 11 weeks** at 8h/week = ~3 months

---

## Success Criteria

### Functional Requirements
- [ ] Call any OpenAPI 3.0.x/3.1.x REST API
- [ ] Support all auth methods: Bearer, API Key, Basic, OAuth2
- [ ] Automatic pagination (Link, cursor, offset/limit)
- [ ] Retry logic with exponential backoff
- [ ] Small responses inline, large → CAS
- [ ] PathValidator prevents all escapes
- [ ] Skill discovery reusable (daemon-ready)

### Quality Requirements
- [ ] Test coverage ≥ 85%
- [ ] Golden fixtures for all scenarios
- [ ] Zero critical security warnings
- [ ] CI/CD passing (lint, test, race, coverage)
- [ ] E2E tests for complete workflows

### Documentation Requirements
- [ ] Root README with quick start
- [ ] CONTRIBUTING guide for developers
- [ ] Security policy (CVE reporting)
- [ ] Troubleshooting guide
- [ ] 5+ real-world API examples

### Performance Requirements
- [ ] Spec loading < 100ms (cached)
- [ ] Request building < 10ms
- [ ] CAS operations < 50ms (10MB files)
- [ ] Job submission < 50ms
- [ ] Memory search < 100ms (1000 entries)

---

## Real-World Examples (Acceptance Tests)

### Example 1: GitHub API
```bash
# Import GitHub OpenAPI spec
agentctl openapi import https://api.github.com/openapi.yaml --as github

# List repositories for a user
agentctl run http/openapi \
  --spec memory:github \
  --operationId repos/listForUser \
  --params '{"username": "torvalds"}'

# Create an issue (requires auth)
agentctl run http/openapi \
  --spec memory:github \
  --operationId issues/create \
  --params '{
    "owner": "jkatigb",
    "repo": "agentctl",
    "body": {"title": "Add feature X", "body": "Description"}
  }' \
  --auth '{"type": "bearer", "token": "$GITHUB_TOKEN"}'
```

### Example 2: Stripe API (Paginated)
```bash
# Import Stripe OpenAPI spec
agentctl openapi import https://raw.githubusercontent.com/stripe/openapi/master/openapi/spec3.yaml --as stripe

# List all customers (auto-paginate)
agentctl run http/openapi \
  --spec memory:stripe \
  --operationId CustomerList \
  --auth '{"type": "bearer", "token": "$STRIPE_SECRET_KEY"}' \
  --paging '{"strategy": "cursor", "max_pages": 10}'
```

### Example 3: Slack API (Async Job)
```bash
# Search messages (long-running, use job)
JOB_ID=$(agentctl jobs submit http/openapi \
  --spec memory:slack \
  --operationId search.messages \
  --params '{"query": "error"}'| jq -r '.data.job_id')

# Tail progress
agentctl jobs tail $JOB_ID

# Get result
agentctl jobs result $JOB_ID
```

---

## Post-v1.0 Roadmap (v1.1+)

### v1.1: Extensibility
- Plugin protocol (SPEC-017)
- Custom auth handlers
- Custom pagination strategies
- Example plugins: HMAC auth, vendor-specific paging

### v1.2: Developer Experience
- Skill codegen (per-operation wrappers)
- Interactive mode (REPL)
- Better error messages with hints
- Autocomplete support

### v1.3: Observability
- Prometheus metrics endpoint
- OpenTelemetry tracing
- Structured audit logging
- Performance profiling

### v2.0: Platform Features
- Daemon mode (REST API server)
- Web UI for job management
- Multi-tenant workspaces
- Skill registry (discovery + installation)
- Advanced memory (vector embeddings, semantic search)

---

## Contributing

We're actively working toward v1.0! Here's how you can help:

### High-Impact Areas
1. **OpenAPI Implementation** (SPEC-012-016) - Core feature gap
2. **Security** (SPEC-011) - PathValidator hardening
3. **Testing** (SPEC-018) - Golden fixtures
4. **Documentation** (SPEC-019) - README, guides, examples

### Getting Started
1. Read [AGENTS.md](AGENTS.md) for contribution guidelines
2. Pick a spec from [docs/refactoring/](docs/refactoring/)
3. Follow the implementation plan in the spec
4. Open a PR to `main` (branch: `codex/<feature-name>`)

See [IMPLEMENTATION_PRIORITY.md](IMPLEMENTATION_PRIORITY.md) for detailed task breakdown.

---

## Communication

- **Issues**: [GitHub Issues](https://github.com/jkatigb/agentctl/issues)
- **Specs**: [docs/refactoring/](docs/refactoring/)
- **Roadmap**: This document (updated monthly)

---

## References

- [Core Profile v1 Spec](docs/spec/core_profile_v1.md) - Authoritative design
- [OpenAPI Skill Spec](docs/spec/openapi_skill.md) - Detailed OpenAPI design
- [Implementation Priority](IMPLEMENTATION_PRIORITY.md) - Task breakdown
- [Refactoring Specs](docs/refactoring/README.md) - Detailed implementation specs
- [Implementation Plan](impl_plan.md) - Original 9-phase plan

---

**Last Updated**: November 14, 2025
**Next Review**: December 1, 2025
**Target v1.0 Release**: February 2026
