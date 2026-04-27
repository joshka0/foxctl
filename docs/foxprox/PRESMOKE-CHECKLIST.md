# ATCP pre-smoke checklist

Use this before running [LIVE-SMOKE.md](LIVE-SMOKE.md). The goal is to
remove environment noise before debugging PTY transport behavior.

## Agent dependencies

- List every registered MCP or sidecar dependency for each agent CLI.
  Examples: `codex mcp list`, `droid mcp list`, or the agent's equivalent.
- Start required local servers before spawning the ATCP room, or disable
  unreachable MCP entries for the smoke.
- If an agent keeps redrawing an init spinner, run `atcp-live` with
  `--warmup-timeout 10s` so idle init stalls are visible in stderr.

## Terminal environment

- Launch `atcpd` from a shell with `TERM=xterm-256color`.
- Confirm the command you pass to `--agent NAME=CMD` is on `PATH`.
- Prefer explicit dimensions during reproduction when visual layout matters:
  `atcpctl session create --cmd "..."` can be extended with rows/cols through
  the HTTP API; `atcp-live` currently uses daemon defaults.

## Socket hygiene

- Use a dedicated socket per run:

```sh
export SOCK=/tmp/atcp-live.sock
rm -f "$SOCK"
/tmp/atcpd --socket "$SOCK"
```

- Keep a second shell pointed at the same socket:

```sh
atcpctl --socket "$SOCK" room list
atcpctl --socket "$SOCK" session list
```

## Cleanup

- Stop the driver with `Ctrl+C` or `Ctrl+D`.
- Stop the daemon with `Ctrl+C`.
- If a prior smoke was interrupted, clean stale processes before retrying:

```sh
pkill -INT atcp-live
pkill -INT atcpd
rm -f "$SOCK"
```
