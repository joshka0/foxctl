# agentctl Roadmap to v1.0

**Status**: Phase 6 (OpenAPI Implementation)  
**Target Release**: Q1 2026 (~3 months)  
**Current Completion**: ~60%

---

## Vision

**agentctl v1.0** will be a production-ready CLI for LLM-powered workflows:

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

| Phase | Description | Status |
|-------|-------------|--------|
| 0–2 | Foundation (bootstrap, envelopes, CAS) | ✅ 100% |
| 3 | Jobs subsystem (SQLite, crash recovery) | ✅ 100% |
| 4 | Runners & Skills (WASI, exec, manifests) | ✅ 100% |
| 5 | Cache & Memory (auto-cache, named memory) | ✅ 100% |
| **6** | **OpenAPI Skill** (spec loader, HTTP, pagination) | 🚧 5% |
| 7 | Plugin SPI (auth/pagination plugins) | 🔜 Deferred to v1.1 |
| 8 | Jobs integration & UX polish | 🔜 75% |

---

## Remaining Work

### P0: Critical (Blocking v1.0)

| Task | Effort | Description |
|------|--------|-------------|
| OpenAPI Spec Loader | 10h | Load from file/CAS/memory, parse 3.0.x/3.1.x |
| Request Builder | 12h | Path/query/header/body handling, validation |
| HTTP Client | 15h | Execute requests, CAS for large responses |
| Pagination | 10h | Link headers, cursor, offset/limit |
| Retry Logic | 8h | Exponential backoff, Retry-After |
| PathValidator Hardening | 5.5h | Symlink attacks, traversal, null bytes |

### P1: High Priority

| Task | Effort | Description |
|------|--------|-------------|
| Golden Test Fixtures | 8h | Envelope/OpenAPI/skill fixtures |
| Documentation | 5h | README, CONTRIBUTING updates |

**Total remaining**: ~73.5h (~9–10 weeks at 8h/week)

---

## Timeline

```
Month 1 (Weeks 1–4):   Cleanup + Security + Spec Loader
Month 2 (Weeks 5–8):   Request Builder + HTTP Client
Month 3 (Weeks 9–12):  Pagination + Retry + Golden Tests + Docs
Month 4 (Weeks 13–14): Integration testing, v1.0 release
```

---

## Success Criteria

### Functional
- [ ] Call any OpenAPI 3.0.x/3.1.x REST API
- [ ] Support auth: Bearer, API Key, Basic, OAuth2
- [ ] Automatic pagination (Link, cursor, offset/limit)
- [ ] Retry logic with exponential backoff
- [ ] PathValidator prevents all escapes

### Quality
- [ ] Test coverage ≥ 85%
- [ ] Golden fixtures for all scenarios
- [ ] CI/CD passing (lint, test, race, coverage)

### Performance
- [ ] Spec loading < 100ms (cached)
- [ ] Request building < 10ms
- [ ] CAS operations < 50ms (10MB files)

---

## Post-v1.0 Roadmap

| Version | Focus |
|---------|-------|
| **v1.1** | Plugin protocol (custom auth/pagination) |
| **v1.2** | Developer experience (codegen, REPL, autocomplete) |
| **v1.3** | Observability (Prometheus, OpenTelemetry) |
| **v2.0** | Platform (daemon mode, web UI, multi-tenant) |

---

## References

- [`docs/spec/core_profile_v1.md`](../../spec/core_profile_v1.md) — Authoritative spec
- [`docs/spec/openapi_skill.md`](../../spec/openapi_skill.md) — OpenAPI design
- [`docs/impl_plan/`](../../impl_plan/) — Detailed implementation plans
- [`AGENTS.md`](../../../AGENTS.md) — AI assistant conventions

---

**Last Updated**: November 30, 2025
