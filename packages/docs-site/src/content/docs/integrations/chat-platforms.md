---
title: Chat platforms
description: Discord, Telegram, Teams, and session-bridge integration posture.
---

Status: Current architecture shell with provider-specific risks.

Chat platform adapters connect external chat events to foxctl sessions, skills,
and agent workflows. They should be documented as adapters, not as independent
runtime sources of truth.

## Adapter flow

| Step | Meaning |
|---|---|
| Inbound platform event | Message or command from chat platform |
| Identity mapping | Platform actor mapped to foxctl principal/session |
| Command/session bridge | Routed into foxctl command or agent surface |
| Response adapter | foxctl output formatted back to platform |

## Production cautions

- Keep platform tokens and webhook secrets out of docs.
- Document platform-specific maintenance risk where known.
- Telegram support has known dependency-maintenance risk in current docs; do not
  market it as risk-free.
- Session bridge behavior should link back to sessions and agent lifecycle docs.

## Canonical sources

- [docs/architecture/chat-platform-adapter.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/chat-platform-adapter.md)
- [docs/architecture/auth-identity.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/auth-identity.md)
- [docs/general/sessions.md](https://github.com/joshka0/foxctl/blob/main/docs/general/sessions.md)
- [docs/general/gotchas.md](https://github.com/joshka0/foxctl/blob/main/docs/general/gotchas.md)

