---
title: Integration status
description: Current and in-progress foxctl integration surfaces.
---

foxctl integrations are split by maturity so operators and agents know which
surfaces are current behavior and which remain plan-backed work.

## Status legend

| Status | Meaning |
|---|---|
| Current | Documented behavior that can be used as operator guidance |
| In progress | Active plan or experimental work; do not treat as canonical behavior |
| Planned | Known direction with no production contract yet |

## Current integrations

| Integration | Status | Documentation |
|---|---|---|
| LLM providers | Current | [Providers and MCP](/integrations/providers-and-mcp/) |
| MCP server | Current | [Providers and MCP](/integrations/providers-and-mcp/) |
| OpenAPI skill | Current | [OpenAPI and plugins](/integrations/openapi-and-plugins/) |
| OpenAPI auth and pagination plugins | Current | [OpenAPI and plugins](/integrations/openapi-and-plugins/) |
| Hooks | Current | [Hooks](/integrations/hooks/) |
| Discord, Telegram, Teams adapters | Current | [Chat platforms](/integrations/chat-platforms/) |
| Obsidian bridge | Current | [Obsidian bridge](/context/obsidian-bridge/) |

## In progress

| Area | Status | Tracking |
|---|---|---|
| Durable execution recovery | In progress | [Progress](/roadmap/progress/) |
| Runtime side-effect safety | In progress | [Progress](/roadmap/progress/) |
| RLM helper runtime and eval loop | In progress | [Progress](/roadmap/progress/) |
| OpenSandbox workspace integration | In progress | [Progress](/roadmap/progress/) |
| Refactor intelligence and slop detection | In progress | [Progress](/roadmap/progress/) |
| ContextWiki memory and retrieval ensemble | In progress | [Progress](/roadmap/progress/) |

## Operator rule

Current integration pages describe behavior that should match the repository.
In-progress pages describe intended direction and validation work. When an
in-progress feature becomes default behavior, promote the stable contract into
the matching current docs page and update this status table in the same change.
