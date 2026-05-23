# Companion Manual Smokes

These programs exercise companion flows against live providers and temporary
local storage. They are examples, not automated tests, and are excluded from
normal builds by the `manualsmoke` build tag.

Run them explicitly:

```bash
go run -tags manualsmoke ./examples/manual-smoke/companion/chat
go run -tags manualsmoke ./examples/manual-smoke/companion/memory
go run -tags manualsmoke ./examples/manual-smoke/companion/mailbox
```

The chat and mailbox smokes need `CEREBRAS_API_KEY` or `OPENROUTER_API_KEY`.
The memory smoke runs with local temporary storage and uses embedding config
when available.
