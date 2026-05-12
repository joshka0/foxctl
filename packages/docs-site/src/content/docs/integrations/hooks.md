---
title: Hooks
description: Use foxctl hooks safely for context, review, test feedback, and workflow automation.
---

Status: Current shell, hook-specific contracts linked.

Hooks connect editor, CI, tests, and agent events to foxctl skills. Production
hook docs should identify the triggering event, input envelope, output envelope,
and failure behavior.

## Runtime checks

```bash
foxctl hooks --help
```

```bash
foxctl run hooks/test_feedback --input-file -
```

## Hook documentation checklist

- Trigger source and event name.
- Input shape and required fields.
- Whether output is advisory, blocking, or stored.
- Whether the hook writes CAS, jobs, sessions, or memory.
- What happens on timeout or malformed output.

## Safety rules

- Skill stdout must remain JSON envelopes only.
- Hook logs should go to stderr.
- Secrets must be redacted.
- Path access should go through policy validation.
- Test-feedback and review hooks should be deterministic enough for CI.

## Canonical sources

- [docs/general/hooks.md](https://github.com/joshka0/foxctl/blob/main/docs/general/hooks.md)
- [docs/spec/test_watch_feedback.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/test_watch_feedback.md)
- [docs/spec/task_hooks_memory.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/task_hooks_memory.md)
- [docs/spec/protocol_v1.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/protocol_v1.md)

