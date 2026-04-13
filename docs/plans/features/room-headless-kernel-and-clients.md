# Room Headless Kernel and Client Architecture

Status: proposed  
Audience: room runtime, web/API, CLI, gui-agent, mux, future desktop/mobile clients

## Canonical Direction

`agentctl` is the headless multi-agent room/runtime kernel.

That kernel owns:

- durable room timeline
- participant state
- task state
- room loop policy and runtime ownership
- delivery planning
- reminder and pulse generation
- authz and policy
- event stream

Everything else is a client or presentation layer on top of that kernel.

That includes:

- CLI
- web API
- `gui-agent`
- tmux
- zellij
- terminal gateway surfaces
- chat adapters
- future Electron/Tauri desktop clients
- future Swift/SwiftUI operator clients

This is not an aspirational compatibility model. It is the canonical architectural
contract we are moving toward.

## Hard Cut

The old mental model to remove is:

- CLI is the product
- web/API sometimes delegates back into CLI
- mux shape leaks into runtime truth
- presentation attachment is used as evidence of delivery/runtime health

The canonical model is:

- runtime behavior lives in shared services
- API is a behavior boundary, not a subprocess bridge
- CLI is one client of that shared runtime
- mux is a presentation/inspection layer, not the room core

## Layer Model

### 1. Kernel

Owns durable and live room behavior.

Responsibilities:

- room creation and mutation
- message append
- reminder scheduling
- loop ownership and heartbeat
- participant transport resolution
- delivery planning
- task transitions
- event emission

### 2. Interface Layer

Owns access into the kernel.

Responsibilities:

- HTTP API
- SSE / WebSocket event streams
- authn / authz
- typed client SDKs

The interface layer must not shell out to CLI commands for canonical room
behavior.

### 3. Clients / Presentation

Render or manually interact with the kernel.

Examples:

- CLI
- `gui-agent`
- tmux and zellij panes
- terminal gateway
- Discord / Teams / Telegram adapters
- desktop and mobile operator apps

These layers must not own room truth. They consume it.

## Non-Negotiable Rules

### API is the real behavior boundary

Shared room behavior must move behind transport-neutral services. The CLI must
stop being the implementation that the server re-enters for core room actions.

### Room runtime ownership is singular

Each room has one live runtime owner for:

- delivery
- reminders
- stale-work nudges
- coordinator pulses
- delivery cursors

### Participant bindings are transport-neutral

Clients consume participant state such as:

- membership
- transport availability
- runtime availability
- presentation attachment

Clients do not reconstruct delivery behavior from mux target names or actor-name
prefixes.

### Event stream is first-class

Multiple clients require one shared stream for:

- room messages
- task changes
- participant transport/runtime changes
- reminder emission
- coordinator pulse
- loop heartbeat and ownership

Polling is acceptable as a bootstrap path, but the canonical direction is one
shared event stream.

## Immediate Architectural Consequences

### CLI

The CLI remains important, but it becomes a client of shared room services.

It should stop owning canonical implementations for:

- relay-once bridging
- room send live delivery
- loop-only truth computation

### Web / API

The web/API layer becomes the service boundary future clients depend on.

That means:

- no CLI shell-out as the canonical delivery path
- no API-only synthetic room truth
- no mux-specific assumptions in outward room state

### Mux

`tmux` and `zellij` are presentation and optional hosting layers.

They may host live runtimes. They may expose PTY state. They do not define room
truth.

### GUI

`gui-agent` should be treated as the first full client of the headless kernel,
not as a special-case control panel that can depend on CLI behavior.

## Rollout Order

### Phase 1

Move room relay, loop, and participant delivery resolution into shared service
code.

### Phase 2

Make API and CLI both call the same room services.

### Phase 3

Expose a stable room event stream.

### Phase 4

Complete transport-neutral participant state and delivery bindings.

### Phase 5

Build or expand alternate clients on top:

- `gui-agent` as the first full client
- Electron/Tauri for full desktop orchestration
- Swift/SwiftUI for operator companion workflows

## Product Framing

The product is not “the CLI plus some attached UIs.”

The product is:

- `agentctl` as the room/runtime kernel
- multiple clients on top of one shared runtime truth

That is the model future work should optimize for.
