# Room Runtime Adoption Pass

This note records the current adoption coverage for the hardened room-runtime
semantics across room-facing docs, skills, and regression entrypoints.

## Covered Semantics

| Hardened semantic | Canonical guidance | Supporting surfaces |
| --- | --- | --- |
| Recurring reminder instance `ack` does not stop the schedule | [docs/general/tmux-collaboration.md](tmux-collaboration.md), [docs/general/api-server.md](api-server.md) | [configs/skills-pack/foxctl-room/SKILL.md](../../configs/skills-pack/foxctl-room/SKILL.md), [configs/skills-pack/foxctl-room-agent/SKILL.md](../../configs/skills-pack/foxctl-room-agent/SKILL.md), [configs/skills-pack/foxctl-room-operator/SKILL.md](../../configs/skills-pack/foxctl-room-operator/SKILL.md), [configs/skills-pack/foxctl-room-view/SKILL.md](../../configs/skills-pack/foxctl-room-view/SKILL.md) |
| Linked-work reminder stop conditions via `task_id` / `story_id` / `milestone_id` | [docs/general/tmux-collaboration.md](tmux-collaboration.md), [docs/general/api-server.md](api-server.md) | [configs/skills-pack/foxctl-room/SKILL.md](../../configs/skills-pack/foxctl-room/SKILL.md), [configs/skills-pack/foxctl-room-agent/SKILL.md](../../configs/skills-pack/foxctl-room-agent/SKILL.md), [configs/skills-pack/foxctl-room-operator/SKILL.md](../../configs/skills-pack/foxctl-room-operator/SKILL.md) |
| Chain-aware reply / resolve semantics | [docs/general/tmux-collaboration.md](tmux-collaboration.md) | [configs/skills-pack/foxctl-room/SKILL.md](../../configs/skills-pack/foxctl-room/SKILL.md), [configs/skills-pack/foxctl-room-agent/SKILL.md](../../configs/skills-pack/foxctl-room-agent/SKILL.md) |
| Strict task / control lifecycle | [docs/general/tmux-collaboration.md](tmux-collaboration.md) | [configs/skills-pack/foxctl-room/SKILL.md](../../configs/skills-pack/foxctl-room/SKILL.md), [configs/skills-pack/foxctl-room-operator/SKILL.md](../../configs/skills-pack/foxctl-room-operator/SKILL.md) |
| Coordinator-only mutation / authorization boundaries | [docs/general/api-server.md](api-server.md), [docs/general/tmux-collaboration.md](tmux-collaboration.md) | [configs/skills-pack/foxctl-room/SKILL.md](../../configs/skills-pack/foxctl-room/SKILL.md), [configs/skills-pack/foxctl-room-operator/SKILL.md](../../configs/skills-pack/foxctl-room-operator/SKILL.md) |
| Durable `last_delivery_trace` observability | [docs/general/api-server.md](api-server.md), [docs/general/tmux-collaboration.md](tmux-collaboration.md), [docs/architecture/system-architecture.md](../architecture/system-architecture.md) | [configs/skills-pack/foxctl-room/SKILL.md](../../configs/skills-pack/foxctl-room/SKILL.md), [configs/skills-pack/foxctl-room-agent/SKILL.md](../../configs/skills-pack/foxctl-room-agent/SKILL.md), [configs/skills-pack/foxctl-room-operator/SKILL.md](../../configs/skills-pack/foxctl-room-operator/SKILL.md), [configs/skills-pack/foxctl-room-view/SKILL.md](../../configs/skills-pack/foxctl-room-view/SKILL.md) |
| `bash tests/regression/run.sh` is the canonical room-runtime regression entrypoint | [tests/regression/README.md](../../tests/regression/README.md), [docs/general/api-server.md](api-server.md), [docs/general/tmux-collaboration.md](tmux-collaboration.md), [docs/architecture/system-architecture.md](../architecture/system-architecture.md) | [configs/skills-pack/foxctl-room/SKILL.md](../../configs/skills-pack/foxctl-room/SKILL.md), [configs/skills-pack/foxctl-room-agent/SKILL.md](../../configs/skills-pack/foxctl-room-agent/SKILL.md), [configs/skills-pack/foxctl-room-operator/SKILL.md](../../configs/skills-pack/foxctl-room-operator/SKILL.md), [configs/skills-pack/foxctl-room-view/SKILL.md](../../configs/skills-pack/foxctl-room-view/SKILL.md) |

## Verification Strategy

Use the room-runtime checks in this order:

1. `bash tests/regression/run.sh`
2. `make check-doc-links` after markdown updates
3. `FOXCTL_INTEGRATION_TMUX=1 go test -tags=integration ./cmd/foxctl/cmd -run 'TestIntegrationRelayRoomMessageTmuxConsumesInputRealTmux' -count=1 -v` when the symptom is "the message is visible in the pane but did not submit"

The first command is the canonical regression bundle. The tmux integration test
is intentionally narrower: it proves the target terminal process consumed the
relayed line, not that the receiving application fully dispatched it.

## Remaining Gap

The main unresolved room-runtime behavior is now:

- room relay can succeed
- terminal-process consumption can succeed
- the receiving application can still leave the message in a local queued draft
  state instead of actually dispatching it

Treat that as a receiver-app dispatch-confirmation problem, not as proof that
room relay failed.

The follow-on runtime story for that gap is:

- `Add receiver dispatch-confirmation regression for queued drafts`
- story id: `01KP1RS9BQNBSN9SSWMVB2X36C`

That story should define the observable contract for a successful dispatch after
consumption and add a regression that fails when the receiver remains queued.
