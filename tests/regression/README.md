# Regression Harness

This directory contains deterministic regression cases for bugs and invariants that
are important enough to keep as a stable, explicit CI entrypoint.

Run all cases locally with:

```bash
bash tests/regression/run.sh
```

For room-runtime work, treat that command as the first verification step before
building ad hoc test bundles. If a failure specifically involves live tmux pane
submission, follow it with:

```bash
FOXCTL_INTEGRATION_TMUX=1 go test -tags='integration libsqlite3' \
  ./cmd/foxctl/cmd \
  -run 'TestIntegrationRelayRoomMessageTmuxConsumesInputRealTmux' \
  -count=1 -v
```

That targeted integration test proves a relayed room message is consumed by the
target terminal process instead of merely appearing in the pane input area.

Each case lives in its own numbered directory and contains:

- `README.md`: what broke and what is being protected
- `run.sh`: one stable entrypoint for that case

The cases should stay:

- deterministic
- self-contained
- fast
- framework-first when the repo already has a native test runner
