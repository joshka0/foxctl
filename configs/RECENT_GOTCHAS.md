# Recent Errors, Gotchas, and Time Sinks
#
# Append-only log of recent errors/gotchas and slow-to-resolve issues.
# Format: - YYYY-MM-DD [gotcha|time]: note
- 2026-01-16 [gotcha]: 3 pre-existing test failures identified as unrelated to current changes
- 2026-01-16 [time]: Cleanup of 82+ stale tasks
- 2026-01-17 [refactor]: FC/IS audit found 24 violations - tracked in #168
  - Critical: envelope.go time.Now(), planning/llm env reads, indexing/semantic env reads
  - High: consolews detached goroutine, actor timestamps, codemap fs ops, daemon warmup race
  - See AGENTS.md "Engineering Principles" section for guidance
