# Tmux Collaboration

Status: current first slice

This document defines the first tmux collaboration surface for `agentctl`:

- tmux is the live coordination plane
- `agentctl tmux` is the structured read surface
- `tmux-bridge` is the write/send helper
- ACA and the Obsidian vault remain the durable continuity layers

The point is to let multiple Codex or Claude panes interact while the TUI stays open, without turning tmux scrollback into canonical state.

## Why This Exists

Opening several AI agents in one tmux session is useful because:

- the operators can see the agents talk in real time
- agents can inspect nearby work without leaving their own pane
- cross-pane communication can happen without standing up a separate message bus

But tmux is still a terminal substrate, not a durable protocol. That means:

- use tmux for live coordination
- use mailbox, ACA, sessions, and vault notes for durable state

## Command Surface

### Structured Reads

`agentctl` exposes read-only tmux inspection:

```bash
agentctl tmux prepare --session agentctl-collab --panes 3 --attach
agentctl tmux list
agentctl tmux read agent-b --lines 80
agentctl tmux observe agent-b --lines 80
agentctl tmux doctor
```

`prepare` creates or extends a detached tmux session, tiles it, and returns an attach command plus bridge examples.

Without `--agent`, panes default to labels like `agent-a`, `agent-b`, `agent-c`.
With `--agent claude`, `--agent codex`, or `--agent gemini`, the default labels become `claude-a`, `codex-a`, or `gemini-a`.

With `--attach`, `agentctl` jumps directly into the prepared session. Outside tmux it uses `attach-session`; inside tmux it uses `switch-client`.

To launch a specific agent CLI in each pane, use `--agent` plus repeated `--agent-arg` flags:

```bash
agentctl tmux prepare --session claude-collab --panes 3 \
  --agent claude \
  --agent-arg=--model \
  --agent-arg=sonnet \
  --agent-arg=--permission-mode \
  --agent-arg=default \
  --attach
```

This keeps `agentctl` neutral while still letting each CLI use its own option surface.

To resume an existing Codex or Claude session in tmux:

```bash
agentctl tmux prepare --session codex-resume \
  --panes 1 \
  --agent codex \
  --agent-session-id 123e4567-e89b-12d3-a456-426614174000 \
  --attach

agentctl tmux prepare --session claude-resume \
  --panes 1 \
  --agent claude \
  --agent-session-id 123e4567-e89b-12d3-a456-426614174000 \
  --attach
```

`--agent-session-id` currently supports `codex` and `claude` only, and requires `--panes 1`.

The other commands return structured envelopes with pane metadata and bounded scrollback captures.

### Interactive Sends

The bundled tmux skill includes `scripts/tmux-bridge` for pane-to-pane interaction:

```bash
agentctl tmux prepare --session agentctl-collab --panes 3 --pane-command codex --attach
```

After the session is attached, the bundled tmux skill includes `scripts/tmux-bridge` for pane-to-pane interaction:

```bash
./scripts/tmux-bridge read agent-b 40
./scripts/tmux-bridge send agent-b "Review internal/storage/mailbox/store.go for lease races."
```

`send` enforces read-before-send per sender pane. One agent reading a target pane does not satisfy the guard for another agent.

## Message Shape

Bridge messages use a stable ASCII header:

```text
[tmux-bridge from=agent-a pane=%3 reply_to=agent-a] review mailbox retry logic
```

That keeps the exchange human-readable while still giving other agents a parseable cue.

## ACA Fit

Promote only derived facts into ACA. Good tmux-derived records are:

- observations
- tensions
- handoffs

Examples:

```bash
agentctl tmux observe agent-b --lines 80
agentctl tmux observe agent-b \
  --statement "agent-b is reviewing mailbox ack semantics in internal/actor/supervisor.go"
```

`agentctl tmux observe` reads the latest bridge message in the target pane, converts it into an ACA observation, and stores pane/session bridge refs as evidence.

Do not promote raw pane dumps directly into the vault. If a tmux exchange produces durable repo knowledge, summarize it first, then use the normal ACA and Obsidian promotion path.

## Scope Boundary

What tmux should do:

- expose live pane layout and scrollback
- support direct agent-to-agent nudges
- give operators a visible coordination layer

What tmux should not replace:

- mailbox request/reply semantics
- session continuity
- ACA top-of-mind, observations, tensions, or handoffs
- reviewed Obsidian notes

## Related

- [docs/architecture/context-architecture.md](../architecture/context-architecture.md)
- [docs/general/runtime-orchestration.md](runtime-orchestration.md)
- [configs/skills-pack/agentctl-tmux/SKILL.md](../../configs/skills-pack/agentctl-tmux/SKILL.md)
