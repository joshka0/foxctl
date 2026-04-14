# v1 Foundational Specifications

Core protocol and profile specifications that define the foxctl system architecture.

## Specs

| Spec | Description |
|------|-------------|
| [protocol_v1.md](protocol_v1.md) | JSON envelope wire protocol, error codes, versioning |
| [plugin_protocol.md](plugin_protocol.md) | Plugin system architecture, skill manifests, runners |
| [daemon_protocol.md](daemon_protocol.md) | MCP server protocol, SSE streaming, skill registration |
| [core_profile_v1.md](core_profile_v1.md) | Core agent profile, capabilities, WASI isolation |
| [agent_profile_v1.md](agent_profile_v1.md) | Agent identity, hierarchy, capability inheritance |

## Status

All v1 specs are **Final** - they define the stable contract for:
- CLI ↔ Skill communication (JSON envelopes)
- Skill manifest format and discovery
- Agent capability model
- Security boundaries (WASI `network:"none"`)

Changes to these specs require versioning (v2) rather than modification.

---

*See `docs/spec/` for implementation specs and roadmap items.*
