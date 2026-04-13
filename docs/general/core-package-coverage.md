# Core Package Coverage

Machine-friendly coverage map for core `internal/*` concepts.

## Coverage Matrix

| Group | Core packages | Current docs | Coverage | Recommended action |
|------|----------------|--------------|----------|--------------------|
| Agent runtime/orchestration | `internal/agent`, `internal/runtime/daemon`, `internal/runtime/execution`, `internal/runtime/engine` | `docs/general/agent-daemon.md`, `docs/architecture/system-architecture.md` | Partial | Keep architecture canonical; maintain runtime details in `docs/general/runtime-orchestration.md` |
| Session/run pipeline | `internal/context/sessionkit`, `internal/skillrun`, `internal/runtime/runservice`, `internal/storage/queue`, `internal/runtime/orchestration/workflow` | `docs/architecture/system-architecture.md` | Partial | Documented in `docs/general/runtime-orchestration.md` |
| Policy/prompt layer | `internal/agentpolicy`, `internal/runtime/agentprompt` | `docs/architecture/system-architecture.md` | Previously missing | Documented in `docs/general/agent-policy-and-prompts.md` |
| Context/observability | `internal/context/updater`, `internal/runtime/observability`, `internal/runtime/hooks` | `docs/general/context-and-observability.md`, `docs/general/events.md`, `docs/general/hooks.md` | Partial | Documented in `docs/general/context-and-observability.md` |
| Storage/state | `internal/storage/*` | `docs/general/storage.md` | Covered | Keep as canonical storage reference |
| Retrieval/index/search | `internal/intelligence/indexing/*`, `internal/intelligence/retrieval`, `internal/intelligence/codecontext`, `internal/intelligence/codemap` | `docs/general/search.md`, `docs/general/repoindex.md` | Covered | Keep index/search docs current with indexer changes |
| API/platform adapters | `internal/interfaces/web`, `internal/interfaces/chatadapter`, `internal/interfaces/openapi`, `internal/providers` | `docs/general/api-server.md`, `docs/architecture/chat-platform-adapter.md`, `docs/start/openapi_and_plugins.md` | Covered | Keep adapter/runtime docs in sync with command surface |
| Security/identity/auth | `internal/auth`, `internal/authbroker`, `internal/intelligence/verification`, `internal/domain/identity` | `docs/architecture/auth-identity.md` | Covered | Keep auth/identity architecture doc aligned with OAuth callback and policy backend changes |

## Notes

- Coverage is intentionally focused on core runtime concepts, not every package.
- Use this table when deciding whether a new internal subsystem needs a new doc or a section update in an existing canonical doc.
