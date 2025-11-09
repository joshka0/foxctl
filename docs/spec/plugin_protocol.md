# Plugin Protocol Specification (Stub)

This placeholder captures the intent for out-of-process plugins used by the OpenAPI skill (auth + pagination).

Key sections to detail:

1. Envelope format and commands (`plugin/auth`, `plugin/pagination`)
2. Transport expectations (stdin/stdout JSON, WASI vs exec runners)
3. Timeout/cancellation semantics (context propagation)
4. Error handling and retry semantics
5. Packaging + discovery (`AGENTCTL_OPENAPI_PLUGIN_PATH`, skill manifest hints)

The plan is to elevate the snippets from `core_profile_v1.md` into this file once the implementation lands.
