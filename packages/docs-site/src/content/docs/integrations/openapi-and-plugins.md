---
title: OpenAPI and plugins
description: Use generic HTTP/OpenAPI skills, auth strategies, pagination, and plugin envelopes.
---

Status: Current shell.

OpenAPI integration is the preferred shape when a provider can be represented
as a typed HTTP surface rather than one-off shell glue.

## HTTP/OpenAPI skill path

```bash
foxctl openapi --help
```

The generic HTTP/OpenAPI path supports dry runs, auth strategies, pagination,
and plugin extension points.

## Plugin boundaries

- Plugins communicate through envelopes.
- Auth and pagination plugins should stay out-of-process unless there is a
  specific reason to embed them.
- Example snippets in older docs may be schematic. Prefer real helpers and
  generated types when writing production code.

## Documentation checklist

- API base URL and auth source.
- Operation name and request schema.
- Pagination behavior.
- Retry and error handling.
- Secret redaction expectations.

## Canonical sources

- [docs/start/openapi_and_plugins.md](https://github.com/joshka0/foxctl/blob/main/docs/start/openapi_and_plugins.md)
- [docs/spec/openapi_skill.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/openapi_skill.md)
- [docs/spec/plugin_protocol.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/plugin_protocol.md)
- [docs/spec/v1/plugin_protocol.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/v1/plugin_protocol.md)

