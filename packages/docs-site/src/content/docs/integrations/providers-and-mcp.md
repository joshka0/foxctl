---
title: Providers, OpenAPI, and MCP
description: Connect provider auth, OpenAPI skills, MCP serving, and plugin wiring.
---

Status: Current shell, provider-specific setup stays in canonical docs.

foxctl can expose skills and provider-backed tools through local runtime
surfaces such as MCP, OpenAPI-generated skills, and plugin wiring.

## MCP

```bash
foxctl mcp status
```

```bash
foxctl mcp serve --skills
```

For shared local use, the README documents starting the MCP daemon with skills
enabled:

```bash
foxctl mcp serve --daemon --skills
```

## OpenAPI skills

Use OpenAPI integration when a provider can be described as a typed API surface
instead of hand-written shell glue.

```bash
foxctl openapi --help
```

## Auth

```bash
foxctl auth --help
```

## Production boundaries

- Keep provider credentials out of docs and logs.
- Prefer typed provider configuration over ad hoc environment reads.
- Document whether a provider path is local-only, daemon-backed, or remote.
- Do not imply a plugin or provider is production-ready if its current source is
  still plan-backed.

## Canonical sources

- [docs/start/openapi_and_plugins.md](https://github.com/joshka0/foxctl/blob/main/docs/start/openapi_and_plugins.md)
- [docs/architecture/auth-identity.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/auth-identity.md)
- [docs/general/api-server.md](https://github.com/joshka0/foxctl/blob/main/docs/general/api-server.md)
- [docs/spec/openapi_skill.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/openapi_skill.md)
- [docs/spec/plugin_protocol.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/plugin_protocol.md)

