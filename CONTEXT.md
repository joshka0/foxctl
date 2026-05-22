# Foxctl

Foxctl is the agent/operator control plane for coordinating workspace work through skills, rooms, durable context, and reviewable artifacts. This context defines the project language that humans and agents should use when discussing foxctl's domain model.

## Language

**Foxctl Context**:
The root domain-language entry point for foxctl's agent/operator model.
_Avoid_: treating it as a full implementation glossary, architecture map, or generated code index.

**Glossary**:
The broader canonical terminology reference for foxctl docs, plans, agent instructions, and command output.
_Avoid_: CONTEXT.md, context map

**Workspace**:
The project boundary foxctl operates on, including repository files, docs, local foxctl metadata, and associated context for that project.
_Avoid_: Room, Session, Vault

**ContextWiki**:
The human- and agent-facing workspace knowledge layer that orients current work through top-of-mind state, handoffs, observations, durable docs, retrieval evidence, and promoted knowledge.
_Avoid_: context, memory, wiki

**Context system**:
The implementation behind **ContextWiki**.
_Avoid_: ContextWiki when discussing package boundaries or code internals.

**Context engine**:
The typed evidence and retrieval substrate behind evidence packs, retrieval episodes, feedback, impact edges, and stale markers.
_Avoid_: ContextWiki, memory system

**Room**:
A durable collaboration space where participants coordinate through messages, inboxes, tasks, and delivery state.
_Avoid_: chat, thread, channel, pane

**Participant**:
Any actor attached to a **Room**, including a human, **Agent**, **Coordinator**, **Operator**, relayed pane, or integration-backed presence.
_Avoid_: Agent when room membership, not autonomy, is the point

**Message**:
A durable communication item inside a **Room**.
_Avoid_: chat line, transcript entry

**Event**:
A time-ordered fact emitted by a **Runtime**, **Actor**, **Skill**, **Room**, **Transport**, or **Adapter**.
_Avoid_: Message

**Inbox**:
A participant-specific view of pending **Room** messages and work.
_Avoid_: queue, notification list

**Mailbox**:
A transport or storage mechanism for agent ask/reply or message delivery.
_Avoid_: Inbox, Room

**Task**:
A durable unit of work tracked inside a **Room**.
_Avoid_: todo, chat request

**Session**:
A runtime execution or conversation lifecycle.
_Avoid_: Room, durable collaboration space

**Transcript**:
An ordered record of interaction content from a **Console**, **Session**, **Room**, or external platform conversation.
_Avoid_: Event history, log

**Skill**:
A foxctl executable tool unit with a manifest, inputs, outputs, and an execution boundary.
_Avoid_: agent prompt, agent guide, integration

**Skill Manifest**:
Declarative metadata for a **Skill**, including identity, inputs, outputs, runtime, and capabilities.
_Avoid_: Skill implementation, Agent Skill Document

**Skill Invocation**:
A concrete request to execute a **Skill**.
_Avoid_: Job, Command, Run

**Invocation Path**:
The command and lifecycle route used for a **Skill Invocation**.
_Avoid_: Input Mode, binary path

**Input Mode**:
The payload source or shape used by a **Skill Invocation**.
_Avoid_: Invocation Path

**Runtime**:
The execution environment or engine that runs a **Skill**, **Agent**, **Session**, or service lifecycle.
_Avoid_: foxctl system, whole platform

**Command**:
The invocation name or CLI/API action used to run behavior.
_Avoid_: Skill when referring only to the call name

**Job**:
A tracked **Command** or **Skill** execution with persistence, status, and optional **Artifacts**.
_Avoid_: Run, Session, Room

**Actor**:
An addressable runtime entity that can receive messages and produce replies or events.
_Avoid_: Agent when autonomy is not implied

**Agent**:
An autonomous or semi-autonomous actor that can coordinate work, use **Skills**, and communicate through **Rooms**.
_Avoid_: Skill, command

**Role**:
The declared responsibility or behavioral posture an **Agent** or **Participant** is operating under.
_Avoid_: permission set, capability grant

**Capability**:
An explicitly available action surface or permissioned ability for an **Agent**, **Participant**, **Role**, **Skill**, or runtime.
_Avoid_: Role, Skill

**Policy**:
A durable rule set that constrains behavior, permissions, routing, safety, or lifecycle decisions.
_Avoid_: Agent Prompt, Shared Constitution

**Contract**:
An explicit schema, protocol, API, message shape, or behavior guarantee that other components or agents rely on.
_Avoid_: Policy

**Operator**:
A human or delegated role responsible for steering **Rooms**, reviewing state, making intervention decisions, and approving or redirecting work.
_Avoid_: Agent, Pi

**Coordinator**:
A **Room** or **Agent** role responsible for routing, readiness decisions, stale-work handling, and final handoffs inside a specific collaboration scope.
_Avoid_: Operator, supervisor

**Overseer**:
The protocol-level agent coordination authority that owns spawn control and cross-agent plan changes.
_Avoid_: Coordinator, Operator, supervisor

**Handoff**:
A durable transfer of responsibility, context, or next action between **Participants**, **Agents**, **Coordinators**, **Operators**, or **Rooms**.
_Avoid_: loose summary, status update

**Integration**:
An external service, adapter surface, or tool ecosystem that foxctl connects to.
_Avoid_: Skill when referring to the external system itself

**Adapter**:
Foxctl-side code or configuration that translates between foxctl contracts and an **Integration**.
_Avoid_: Integration, Skill

**Agent Skill Document**:
A markdown or MDX instruction artifact that teaches an agent domain-specific behavior, workflow, or context.
_Avoid_: Skill when discussing foxctl executable tools

**Agent Prompt**:
Run-specific or role-specific instructions given to an **Agent** for a task.
_Avoid_: Skill, Agent Skill Document

**Shared Constitution**:
A shared prompt artifact that every **Agent** in a room, pipeline, or coordinated run must obey.
_Avoid_: policy, system prompt

**Room Agile**:
The planning layer attached to **Rooms** for epics, milestones, stories, review, validation, and next-action selection.
_Avoid_: project management, planning state

**Epic**:
A larger outcome tracked through **Room Agile**.
_Avoid_: project, initiative

**Milestone**:
A bounded phase or checkpoint inside an **Epic**.
_Avoid_: stage when referring to Room Agile state

**Story**:
An actionable unit of work inside **Room Agile** that can be started, reviewed, and validated.
_Avoid_: task when referring to Room Agile story lifecycle

**Review**:
Human, agent, or coordinator judgment over quality, risk, readiness, correctness, or taste.
_Avoid_: Validation, approval

**Validation**:
An explicit outcome check for a **Story** or deliverable against stated criteria.
_Avoid_: Review, tests passed, approval

**Herdr**:
A live relay surface for **Room** participants and operator-visible panes.
_Avoid_: Room, provisioning backend

**Pi**:
An operator **Viewer** for inspecting **Rooms**, sending **Messages**, and driving **Room Agile** state.
_Avoid_: agent runtime, Room

**Viewer**:
A UI or terminal surface used to observe or interact with a **Room**, **Session**, **Agent**, **Pane**, or **Artifact**.
_Avoid_: Room, Transport

**Pane**:
The unit inside a **Viewer** where an operator or participant observes or interacts with a **Console**, **Agent**, **Session**, command shell, or live process.
_Avoid_: Room, Participant, Session

**Console**:
An interactive control surface for a process, shell, service, **Agent**, or **Session**, often presented inside a **Pane**.
_Avoid_: Viewer, Pane, Room

**Transport**:
The communication mechanism used to move messages, events, or requests.
_Avoid_: Relay, Room

**Relay**:
A bridge that moves **Room** messages or events between foxctl's durable **Room** and a live external surface.
_Avoid_: provisioning, room creation

**Provisioning**:
Creating or attaching live agent runtimes or panes for **Room** participants.
_Avoid_: Relay

**Deliverable**:
An outcome-level output intended to be handed to a user, **Operator**, **Room**, or external system as completed work.
_Avoid_: Artifact

**Artifact**:
A durable output that can be inspected, referenced, reviewed, or delivered.
_Avoid_: message, evidence

**Evidence**:
Source-backed material used to justify a claim, review finding, decision, or recommendation.
_Avoid_: artifact when referring only to support for a claim

**CAS Artifact**:
A large or reusable **Artifact** stored by content digest.
_Avoid_: file, blob

**Envelope**:
The canonical JSON wrapper for foxctl command and **Skill** results.
_Avoid_: raw response, output blob

**Payload**:
The command-specific data inside an **Envelope**.
_Avoid_: envelope

**Error**:
Structured failure information inside an **Envelope**.
_Avoid_: stderr, panic text

**Turbovec**:
The compressed vector search engine (TurboQuant algorithm) that accelerates semantic retrieval for foxctl workspaces.
_Avoid_: vector database, vector store, ANN index

**Turbovec sidecar**:
The turbovecd Unix domain socket service that manages compressed vector indices alongside foxctl.
_Avoid_: turbovec daemon, vector server

## Relationships

- **Foxctl Context** defines the domain-language boundary for humans and agents.
- The **Glossary** remains the broader implementation and documentation terminology reference.
- A term should be promoted into **Foxctl Context** only when it is meaningful to foxctl users, operators, or agent coordinators as part of the domain model.
- A **Workspace** is the project boundary for foxctl work; **Rooms**, **Sessions**, **Jobs**, **Artifacts**, and ContextWiki state may be associated with a workspace without being the workspace itself.
- **ContextWiki** is powered by the **Context system** and may use the **Context engine** for evidence-backed retrieval.
- Agents should say **ContextWiki** when they mean the workspace knowledge layer, **Context system** when they mean the implementation, and **Context engine** when they mean the evidence/retrieval substrate.
- A **Room** has **Participants**, contains **Messages**, exposes participant **Inboxes**, and tracks **Tasks**.
- A **Message** is directed communication; an **Event** is a time-ordered fact about something that happened.
- An **Inbox** is the participant-facing room view; a **Mailbox** is the delivery or storage mechanism an agent daemon or transport may poll.
- A **Session** may participate in a **Room**, but a **Room** is the durable collaboration artifact when a runtime process, pane, or relay restarts.
- A **Transcript** records interaction content; an **Event** records lifecycle or observability facts.
- An **Agent** may run a **Skill** through a **Command**.
- A **Skill Manifest** describes a **Skill** but is not the skill implementation.
- A **Runtime** executes a **Skill**, **Agent**, **Session**, or service lifecycle; it is not the whole foxctl system.
- A **Skill Invocation** has a **Skill**, an **Invocation Path**, and an **Input Mode**.
- `foxctl run <skill> --input ...` is the job-tracked **Invocation Path**; it normally creates a **Job** and is the default when job history, async/dedupe behavior, CAS, or trajectory metadata matters.
- `foxctl run <skill> --ephemeral --input ...` is the ephemeral **Invocation Path**; it skips job persistence and is useful for hooks, sandboxed agents, smoke tests, and one-off retrieval.
- `foxctl skills run <skill> --param value` is the direct **Invocation Path**; it uses manifest-derived parameter flags and is useful for validating skill parameters or when flags are clearer than raw JSON.
- `--input`, `--input-file`, `--input stdin`, and `--input sha256:<digest>` are **Input Modes**, not separate **Invocation Paths**.
- `foxctl` versus `./bin/foxctl` chooses the binary being executed; it is not an **Invocation Path**.
- An **Agent** is an autonomous or semi-autonomous **Actor**, but not every **Actor** is an **Agent**.
- An **Agent** may run individually or participate in a **Room**.
- An **Agent** becomes a **Participant** only when attached to a **Room**.
- A **Role** describes what an **Agent** or **Participant** is responsible for; it does not automatically define permissions unless a specific runtime or policy doc says so.
- A **Capability** describes what an **Agent**, **Participant**, **Role**, **Skill**, or runtime can actually do.
- A **Policy** constrains which **Capabilities** are available and which behaviors or lifecycle actions are allowed.
- A **Contract** defines a shape or guarantee that callers, agents, skills, adapters, transports, or rooms rely on.
- An **Operator** may use **Pi** or another console to steer **Rooms** and **Room Agile** state.
- A **Coordinator** makes scoped routing/readiness decisions, while an **Operator** owns human-level intervention and direction.
- An **Overseer** owns protocol-level spawn control and cross-agent plan changes; a **Coordinator** owns scoped routing and readiness decisions inside a collaboration scope.
- A **Handoff** should point to durable state such as a **Message**, **Artifact**, **Story**, **Review**, or **Validation** result.
- A **Skill** may talk to an **Integration**, but the integration is not the skill.
- A **Skill** may use an **Adapter** to translate between foxctl contracts and an **Integration**.
- An **Agent Skill Document** can instruct an **Agent**, but it is not a foxctl executable **Skill**.
- An **Agent Prompt** instructs an **Agent** for a specific role or run.
- A **Shared Constitution** constrains multiple **Agent Prompts** inside the same coordinated run.
- **Room Agile** belongs to a **Room** and tracks **Epics**, **Milestones**, **Stories**, **Review**, and **Validation**.
- A **Story** is different from a **Room** **Task**: a **Task** coordinates room work, while a **Story** carries Room Agile lifecycle and validation semantics.
- **Review** judges whether something is good enough, risky, unclear, or ready; **Validation** checks whether explicit criteria were satisfied.
- A **Transport** is the communication mechanism; a **Relay** is the bridge behavior that moves **Room** messages or events across a transport or external surface.
- **Herdr** participates through **Relay**, not **Provisioning**.
- **Pi** can operate on **Rooms** and **Room Agile** state, but it is not the durable source of truth.
- A **Viewer** is an interface surface such as **Pi**, **Herdr**, tmux, zellij, or a browser UI.
- A **Pane** belongs to a **Viewer** and is the unit where users observe or interact with agents, shells, sessions, or live processes.
- A **Console** may be hosted in a **Pane**, and a **Pane** may expose different kinds of consoles such as shells, REPLs, service consoles, or foxctl actor consoles.
- A **Pane** may correspond to a **Session** or **Participant**, but it is not itself the durable **Room** state.
- The **Room** is the durable source of truth when **Provisioning** or **Relay** surfaces restart.
- A **Deliverable** may contain or point to one or more **Artifacts**, but the deliverable is the completed outcome, not each stored object.
- A **Message** may point to an **Artifact**.
- **Evidence** may be contained in an **Artifact**, but evidence is the source-backed support, not the artifact itself.
- A **CAS Artifact** is an **Artifact** stored by digest when inline output is too large or should be reused.
- An **Envelope** contains a **Payload** on success or an **Error** on failure.
- Large **Payloads** should move into **CAS Artifacts** instead of bloating the **Envelope**.

## Example dialogue

> **Dev:** "Should I add every new package or command term to **Foxctl Context**?"
> **Domain expert:** "No. Put project-domain language here. Keep implementation details and one-off command names in the **Glossary** or the relevant architecture doc."

> **Dev:** "Is this room the workspace?"
> **Domain expert:** "No. The **Workspace** is the project boundary. The **Room** is a durable collaboration space associated with that work."

> **Dev:** "The Codex pane restarted, but Pi still shows the review item. Is that the same session?"
> **Domain expert:** "No. The live process was a **Session**. The surviving collaboration state is in the **Room**."

> **Dev:** "Is 'job state changed to ok' a message?"
> **Domain expert:** "No. That is an **Event**. A directed note asking another participant to review it would be a **Message**."

> **Dev:** "Is the full content of a console conversation an event history?"
> **Domain expert:** "No. The ordered interaction content is a **Transcript**. Lifecycle facts like start, cancel, or completion are **Events**."

> **Dev:** "Is every participant in a room an agent?"
> **Domain expert:** "No. An **Agent** can be a **Participant**, but so can an **Operator**, a relayed pane, or an integration-backed presence."

> **Dev:** "Is a console bridge an agent?"
> **Domain expert:** "Not necessarily. It may be an **Actor** because it receives messages and emits replies or events, but it is only an **Agent** if it has autonomous or semi-autonomous work behavior."

> **Dev:** "Is an agent daemon polling its room inbox?"
> **Domain expert:** "Usually say it polls a **Mailbox**. The **Inbox** is the participant-facing room view of pending messages and work."

> **Dev:** "In the Praze launch room, are `youtube_researcher` and `pastoral_tone_specialist` separate participant types?"
> **Domain expert:** "No. They are **Participants** or **Agents** operating under different **Roles**."

> **Dev:** "If the reviewer role can read files but cannot write them, is that a role or a skill?"
> **Domain expert:** "The reviewer is the **Role**. File read access is a **Capability**. A reusable executable unit it invokes would be a **Skill**."

> **Dev:** "Is the manifest the skill?"
> **Domain expert:** "No. The **Skill Manifest** describes the **Skill**. The executable code or WASI module is the implementation."

> **Dev:** "Is WASI a skill?"
> **Domain expert:** "No. WASI is a **Runtime** that can execute a **Skill** whose manifest targets it."

> **Dev:** "Is `foxctl run code/semantic_search` a Run?"
> **Domain expert:** "No. `foxctl run` is the **Command**. If it uses the job-tracked path, the persisted execution record is a **Job**."

> **Dev:** "Is `--input-file` a different way to call a skill than `--input`?"
> **Domain expert:** "It is a different **Input Mode**, not a different **Invocation Path**."

> **Dev:** "Is `./bin/foxctl run code/semantic_search` a different invocation path from `foxctl run code/semantic_search`?"
> **Domain expert:** "No. That is binary selection. The **Invocation Path** is still job-tracked `foxctl run` unless `--ephemeral` or another execution route changes it."

> **Dev:** "Is 'only the overseer may spawn subagents' a shared constitution?"
> **Domain expert:** "No. That is a **Policy**. A **Shared Constitution** is prompt context shared by agents in a coordinated run."

> **Dev:** "Is the envelope JSON shape a policy?"
> **Domain expert:** "No. The envelope shape is a **Contract**. A rule that forbids changing it without review is a **Policy**."

> **Dev:** "Is `social/youtube_collect` the YouTube integration?"
> **Domain expert:** "No. `social/youtube_collect` is the **Skill** and YouTube is the **Integration**. The Codex researcher using it is the **Agent**."

> **Dev:** "What is the code that converts YouTube API results into foxctl evidence?"
> **Domain expert:** "That is an **Adapter**. YouTube is the **Integration**. `social/youtube_collect` is the **Skill** using it."

> **Dev:** "Is `agents/youtube_research.md` a foxctl skill?"
> **Domain expert:** "No. It is an **Agent Prompt** for a generated run. A reusable markdown workflow like `grill-with-docs` is an **Agent Skill Document**. Neither is a foxctl executable **Skill**."

> **Dev:** "In the Praze room, is `call_supervisor` the operator?"
> **Domain expert:** "No. `call_supervisor` is the **Coordinator** for that collaboration scope. The human steering through Pi is the **Operator**."

> **Dev:** "Should we add a Supervisor term for `call_supervisor`?"
> **Domain expert:** "No. Treat that as a **Coordinator** **Role** unless a future spec defines a distinct lifecycle responsibility."

> **Dev:** "Is the room coordinator allowed to spawn more agents?"
> **Domain expert:** "Only if the active **Policy** grants that **Capability**. Protocol-level spawn control belongs to the **Overseer** model."

> **Dev:** "Can I just paste a summary as the handoff?"
> **Domain expert:** "A summary can be part of a **Handoff**, but the handoff should point to durable state like a **Message**, **Artifact**, **Story**, or **Validation** result."

> **Dev:** "For the Praze launch, is 'verify X collector dry-run behavior' a task or a story?"
> **Domain expert:** "Use **Story** when it participates in **Room Agile** start/review/validation lifecycle. Use **Task** for room-level coordination work."

> **Dev:** "The Pastoral Tone Specialist says the launch copy feels manipulative. Is that validation?"
> **Domain expert:** "No. That is **Review**. **Validation** is the explicit check that the accepted copy satisfies the agreed criteria, such as no unverified product claims and no spiritual manipulation."

> **Dev:** "Should the pipeline create a Herdr room?"
> **Domain expert:** "No. Create a foxctl **Room**, provision live participants if needed, and add **Herdr** **Relay** for live surfaces."

> **Dev:** "Is a tmux pane a room participant?"
> **Domain expert:** "No. tmux is a **Viewer**, and the tmux pane is a **Pane**. A room **Participant** may be observed or controlled through that pane."

> **Dev:** "If I interact with a shell inside a Herdr pane, is the shell the pane?"
> **Domain expert:** "No. Herdr is the **Viewer**, the Herdr pane is the **Pane**, and the shell is a **Console** hosted inside that pane."

> **Dev:** "Is Herdr's Unix socket the relay?"
> **Domain expert:** "No. The Unix socket is the **Transport**. The foxctl component forwarding room messages through it is the **Relay**."

> **Dev:** "Is the research compiler's markdown file a message?"
> **Domain expert:** "No. The room note asking for review is a **Message**. The markdown file is an **Artifact**. The source-backed snippets inside it are **Evidence**."

> **Dev:** "Is the final Praze launch pack an artifact?"
> **Domain expert:** "The launch pack is the **Deliverable**. The markdown files, scripts, screenshots, or CAS blobs inside it are **Artifacts**."

> **Dev:** "`launch/praze_pipeline` produced a large file pack. Should the result inline every file?"
> **Domain expert:** "No. Keep the **Envelope** small and point to a **CAS Artifact** for the large **Payload**."

## Flagged ambiguities

- **Foxctl Context** is intentionally short. Use it for root domain shorthand, not every feature, implementation, or skill-specific term.
- **Glossary** carries broader vocabulary, deferred terms, ordinary-prose terms, and layer-specific naming rules.
- Keep feature-family language such as social research, collectors, analysts, platforms, trends, sentiment, and competitors in feature plans or skill docs unless it becomes cross-cutting foxctl language.
- Keep overloaded implementation language such as pipeline, workflow, orchestration, provider, index, search, state, status, run, profile, gate, decision, and approval in the **Glossary** or the relevant spec until the root boundary is settled.
