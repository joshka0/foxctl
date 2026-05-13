# Official Docs Production Release Plan

## Status

- State: active plan
- Owner: docs/platform
- Release posture: Starlight site is a production documentation surface, but
  existing docs under `docs/general/`, `docs/architecture/`, and `docs/spec/`
  remain canonical until a page is explicitly migrated.
- First implementation slice: scaffold `packages/docs-site` with a small,
  buildable Starlight shell and task-oriented navigation.

## Dependency Vetting Note

This note was recorded before any package or lockfile edits for the Starlight
site.

### Official setup path

Starlight's current official docs describe two setup paths:

- Quick start creates a new Astro + Starlight project with
  `npm create astro@latest -- --template starlight`.
- Manual setup adds `@astrojs/starlight` as an Astro integration, configures it
  in `astro.config.mjs`, and creates a `docs` content collection with
  `docsLoader()` and `docsSchema()`.

For foxctl, use the manual setup shape inside `packages/docs-site` instead of a
generator-driven repo rewrite.

References:

- https://starlight.astro.build/getting-started/
- https://starlight.astro.build/manual-setup/
- https://starlight.astro.build/guides/sidebar/
- https://starlight.astro.build/reference/configuration/
- https://bun.sh/docs/pm/lifecycle
- https://socket.dev/blog/tanstack-npm-packages-compromised-mini-shai-hulud-supply-chain-attack

### Direct package decision

Do not use the newest Starlight pair today.

| Package | Latest checked | Result | Decision |
|---|---:|---|---|
| `astro` | `6.3.1` | Published 2026-05-07, inside the local 7-day release-age window | Reject for this slice |
| `@astrojs/starlight` | `0.39.2` | Published 2026-05-08, inside the local 7-day release-age window | Reject for this slice |

Use exact older pins that satisfy the release-age gate:

| Package | Selected version | Publish date | License | Notes |
|---|---:|---|---|---|
| `astro` | `6.2.2` | 2026-05-04 | MIT | Peer-compatible with Starlight `^6.0.0` |
| `@astrojs/starlight` | `0.38.4` | 2026-04-23 | MIT | Peer dependency is `astro: ^6.0.0` |

### Socket and SFW evidence

Commands run in temp workspaces under `/private/tmp`, not in the repo:

- `codex mcp list` shows `socket-mcp https://mcp.socket.dev/` enabled.
- Tool discovery did not expose a callable Socket MCP tool in this session.
  Treat that as a tooling gap, not a clean bill of health.
- `sfw npm install --package-lock-only --ignore-scripts astro@6.3.1 @astrojs/starlight@0.39.2`
  was rejected before fetch because the packages were newer than the local
  release-age policy.
- `sfw npm install --package-lock-only --ignore-scripts --save-exact --omit=optional astro@6.2.2 @astrojs/starlight@0.38.4`
  resolved and `npm audit` reported zero known vulnerabilities.
- `sfw` reported no detected package fetch attempts, even with an isolated npm
  cache. This means SFW was used as the execution wrapper, but it did not provide
  positive network-filter evidence for this package-manager path.
- `sfw bun install ...` did not make forward progress in a temp workspace and
  was killed. Do not use SFW+Bun as the repo install mechanism until separately
  debugged.
- `socket package shallow npm ...` later succeeded for the transitive override
  candidates `defu@6.1.7`, `vite@7.3.2`, `rollup@4.60.3`,
  `postcss@8.5.14`, `diff@8.0.4`, and `picomatch@2.3.2`. Socket reported
  vulnerability scores of 100 for each package. The remaining alerts were
  package-self capability/noise alerts expected for build tools, such as
  network access, filesystem access, env var reads, eval use, URL strings, and
  a medium `potentialVulnerability` marker on `picomatch@2.3.2`.
- `socket package shallow npm picomatch@4.0.4` later hit Socket API quota.
  `sfw npm install --package-lock-only --ignore-scripts --save-exact
  picomatch@4.0.4` and npm metadata were used for that nested lock bump.
- `socket scan create --tmp --report joshka0 .` created and completed a temp
  scan, but failed to fetch the org security policy with 403. Treat Socket CLI
  org-policy verdicts as unavailable in this session.

Initial Socket package-score checks against the latest pair found:

- `astro@6.3.1` self score was good overall, but Socket flagged a high
  `obfuscatedFile` alert in Astro's env import-meta transform path. This is not
  evidence of malware, but it is relevant for secret-leak review.
- `@astrojs/starlight@0.39.2` self score was good overall, with only low URL
  string noise.
- The transitive graph was large, with hundreds of packages and alerts around
  install scripts, shell/network capabilities, and native binary packages.

Candidate lockfile review for the selected older pair found:

- Lockfile package count: 435 packages.
- Direct dependencies: `astro: 6.2.2`, `@astrojs/starlight: 0.38.4`.
- No `@tanstack/*` packages were present in the candidate lockfile.
- No non-registry `resolved` URLs were present.
- `npm audit --package-lock-only --audit-level=moderate` reported zero known
  vulnerabilities.
- Packages with install scripts in the lockfile:
  - `esbuild@0.27.7`, not optional, `postinstall: node install.js`.
  - `sharp@0.34.5`, optional, `install: node install/check.js || npm run build`.
  - `fsevents@2.3.3`, optional macOS dependency, `install: node-gyp rebuild`.

### Install controls

Use Bun for the actual repo workspace because the repo already uses Bun
workspaces and `bun.lock`. Bun's lifecycle documentation says arbitrary package
lifecycle scripts are not executed by default and `--ignore-scripts` disables
them for all packages.

The first attempt used `--omit optional`, but that left `esbuild@0.27.7`
paired with an older `@esbuild/darwin-arm64@0.27.2` binary already present in
`node_modules`. Astro could not load the config with that host/binary mismatch.
Because `esbuild` uses optional platform packages for its binary, the actual
repo install keeps optional dependencies but disables lifecycle scripts:

```bash
bun install --ignore-scripts
```

Do not add any package to `trustedDependencies` for this slice. Do not use
`astro add` or `create astro`, because those commands hide dependency and file
changes behind a generator.

Because `bun audit` found known advisories in the selected transitive graph,
the root package adds top-level Bun overrides for vulnerable build-tool
metadependencies:

| Package | Override | Reason |
|---|---:|---|
| `defu` | `6.1.7` | Fix prototype pollution advisory in `<=6.1.4` |
| `diff` | `8.0.4` | Fix jsdiff DoS advisory in `<8.0.3` |
| `postcss` | `8.5.14` | Fix PostCSS stringify XSS advisory in `<8.5.10` |
| `rollup` | `4.60.3` | Fix Rollup path traversal advisory in `<4.59.0` |
| `vite` | `7.3.2` | Fix Vite dev-server advisories in `<=7.3.1` |

The lockfile also pins nested `picomatch` ranges to the vetted fixed tarballs
`2.3.2` and `4.0.4`. Bun does not currently support nested overrides, so those
lock entries are reviewed explicitly rather than forcing one global major
version for all `picomatch` consumers.

After these fixes, `bun audit` still exits non-zero because of pre-existing
workspace findings outside `packages/docs-site`:

- `@redwoodjs/agent-ci`: `protobufjs`, `minimatch`, `brace-expansion`
- `packages/gui-agent`: `eslint`/`typescript-eslint` transitive advisories
- `packages/gui-auth-gateway`: `better-auth`/`kysely`, `nodemailer`,
  `http-proxy`, `express`
- `packages/foxterm`: `@opentui/core`/`file-type`

The docs-site gate for this slice is: no `workspace:@foxctl/docs-site` entries
may remain in `bun audit`. A workspace-wide production release should remediate
or explicitly accept the older non-docs findings separately.

## Target Audience

- New foxctl users who need a fast path from install to useful workflows.
- Agent operators running repoindex, context gathering, rooms, and task
  continuity.
- Contributors who need current architecture boundaries before changing code.
- Maintainers preparing releases, CI checks, docs QA, and operational guidance.

## Information Architecture

The Starlight site should be task-oriented, not a mirror of the filesystem.

### Start Here

- What foxctl is
- Install and local prerequisites
- First useful commands
- Documentation map
- Safety model and invariants

### Workflows

- Design a feature safely
- Add a skill
- Search and understand a repo
- Build and query repoindex
- Run skills
- Gather context and continuity
- Coordinate agents and rooms
- Operate room-agile milestones and evidence

### Reference

- CLI command families
- Skill runtime
- Agent daemon
- Repoindex and retrieval
- Storage and CAS
- Integrations, MCP, and hooks
- Kubernetes and deployment

### Architecture

- Runtime architecture
- Package topology
- Context architecture
- Memory and sessions
- Agent hierarchy and overseer model
- Chat platform adapter
- Kubernetes runtime
- PostgreSQL and CAS storage

### Production Readiness

- Verification commands
- CI and docs checks
- Security and secret-handling rules
- Troubleshooting and gotchas
- Release checklists

### Roadmap and Archive

- Active plans
- Experimental areas
- Historical docs
- Generated codemaps

## Feature and Workflow Documentation Matrix

| Area | Starlight route | Source docs | Release label | Notes |
|---|---|---|---|---|
| Overview and install | `/start/overview`, `/start/install-first-run` | `README.md`, `docs/README.md`, `docs/start/README.md` | current | Keep commands verified and short |
| Documentation taxonomy | `/start/docs-map` | `docs/README.md`, `docs/DOC_LIFECYCLE.md` | current | Explain canonical vs plan vs archive |
| Feature design guide | `/guides/designing-foxctl-features`, `/architecture/design-principles` | `AGENTS.md`, `docs/architecture/package-topology.md`, `docs/DOC_LIFECYCLE.md` | current | Explain source of truth, projections, package families, and dependency posture |
| CLI command families | `/reference/cli`, `/reference/command-map` | `README.md`, `docs/general/` | current | Start with common commands, link deep docs |
| Protocol v1 | `/reference/protocol-v1` | `docs/spec/v1/protocol_v1.md` | current | Link stable v1 spec, not the moved stub |
| Skills runtime and authoring | `/skills/runtime-and-install`, `/guides/add-a-skill` | `docs/general/skills.md`, `docs/spec/skills_spec/README.md`, `skills/` | current | Include WASI/network constraints, manifest shape, CAS output, and verification |
| Repoindex and retrieval | `/retrieval/search-and-index`, `/retrieval/repoindex-and-dag-grep`, `/retrieval/repoindex-model` | `docs/general/repoindex.md`, `docs/general/search.md`, `docs/spec/repo_graph_index_and_dag_grep.md` | current | Cover semantic search, smart search, context grep, DAG grep, nodes, edges, anchors |
| ACA, context engine, and context | `/context/aca`, `/context/context-engine`, `/context/obsidian-bridge` | `docs/architecture/context-architecture.md`, `internal/context/contextengine`, `internal/storage/contextengine` | current | Keep dual-plane language precise and separate ACA from typed evidence substrate |
| Memory and continuity | `/memory/continuity` | `docs/general/memory.md`, `docs/general/sessions.md`, `docs/general/task-continuity.md` | current | Separate agent-facing and hook-facing commands |
| Agent daemon | `/agents/lifecycle` | `docs/general/agent-daemon.md`, `docs/general/runtime-orchestration.md` | current | Distinguish classic and Jido-backed paths |
| Agent hierarchy | `/agents/orchestration` | `docs/spec/agent_hierarchy.md`, `docs/spec/overseer_profile.md` | current | Explain overseer spawn control |
| Rooms | `/collaboration/rooms` | `docs/general/tmux-collaboration.md`, `docs/general/message-passing.md` | current | Link room transport and viewer docs |
| Room-agile | `/collaboration/rooms`, `/roadmap/planned-and-archive` | `docs/plans/features/room-*`, `configs/skills-pack/foxctl-room-agile/SKILL.md` | mixed | Mark plan-backed parts clearly |
| GUI and web runtime | `/reference/gui-and-web` | `packages/gui-agent`, `packages/foxterm`, `docs/architecture/*` | current where verified | Avoid unsupported UI claims |
| API server | `/architecture/api-server` | `docs/general/api-server.md`, `docs/general/api-server.openapi.yaml` | current | Keep `/api` base path exact |
| Auth and identity | `/architecture/auth-and-identity` | `docs/architecture/auth-identity.md` | current with known gaps | Keep OAuth/Casbin TODOs visible |
| Integrations and MCP | `/integrations/providers-and-mcp`, `/integrations/openapi-and-plugins` | `integrations/`, `.mcp.json`, docs | current | Include provider auth boundaries |
| Chat platforms | `/integrations/chat-platforms` | `docs/architecture/chat-platform-adapter.md` | current with provider risks | Note Telegram dependency maintenance risk |
| Hooks | `/integrations/hooks` | `.claude/`, `configs/hooks/`, `docs/general/hooks.md` | current | Include envelope and stdout rules |
| Storage and CAS | `/storage/cas-and-persistence` | `docs/architecture/postgres-storage.md`, `docs/general/storage.md`, `docs/general/persistence.md` | current | Separate local SQLite/Turso/Postgres posture |
| Kubernetes/deployment | `/deployment/kubernetes` | `docs/guides/kubernetes.md`, `deploy/kubernetes/` | current | Link manifests, avoid untested deploy claims |
| Troubleshooting and gotchas | `/operations/troubleshooting`, `/operations/gotchas` | `docs/general/gotchas.md` | current | Use symptom to fix format |
| Observability | `/operations/observability` | `docs/general/context-and-observability.md`, `docs/general/events.md` | current | Link events, jobs, CAS, sessions |
| CI and release QA | `/quality/ci-and-evals`, `/production/verification` | workflows, Makefile, docs policy | current | Include docs build and link checks |
| Experimental runtime work | `/architecture/runtime`, `/roadmap/planned-and-archive` | active plans | planned | Keep out of current behavior pages |
| Historical/generated docs | `/roadmap/planned-and-archive` | `docs/archive/`, `docs/codemaps/` | archive | Provide navigation and warnings only |

## Current Docs Inventory and Migration Strategy

- `docs/general/` contains detailed subsystem guides and should remain canonical
  for behavior that is already implemented.
- `docs/architecture/` contains as-built architecture maps and should remain
  canonical for package and subsystem boundaries.
- `docs/spec/` contains stable and evolving protocol documents. Starlight should
  separate stable specs from evolving specs rather than flattening them.
- `docs/plans/` contains active plans and implementation roadmaps. Starlight
  pages may summarize them, but must label plan-backed material as planned or
  experimental.
- `docs/archive/` and `docs/codemaps/` are historical/generated support areas.
  The production site should expose them through an archive section, not as
  primary current guidance.

Migration should be incremental:

1. Create short Starlight task pages that link to canonical docs.
2. Promote a page to "migrated" only after the canonical source is reviewed and
   the Starlight page is verified against current code/docs.
3. Leave deep reference content in canonical docs until a deliberate migration
   issue exists.
4. Do not bulk move existing markdown into `packages/docs-site`.

## Starlight Content Model

- Use `src/content/docs/` for authored Starlight pages.
- Use explicit sidebar groups, not filesystem autogeneration, for the first
  production shell.
- Page badges should distinguish `Current`, `Planned`, `Experimental`, and
  `Archive`.
- Use links back to canonical repo docs for deeper reference.
- Avoid generated API pages in the first slice.

## Editorial Rules

- Prefer task titles: "Build the repo index", "Run a skill", "Resume a room".
- Avoid describing planned features as current behavior.
- Keep command examples copy-pasteable and specify working directory when it
  matters.
- Name the exact runtime path when behavior differs, for example classic
  `agent run` versus Jido-backed `agent ask`.
- Keep protocol and envelope language exact. Do not paraphrase fields in a way
  that changes the contract.
- For security guidance, state the control and the verification command.

## QA Gates

Required before declaring the site production-ready:

```bash
bun run --cwd packages/docs-site build
```

```bash
bun run --cwd packages/docs-site check
```

```bash
make check-doc-links
```

```bash
git diff --check
```

```bash
bun audit
```

Additional release checks:

- Verify every Starlight page has frontmatter `title` and `description`.
- Verify no Starlight page claims production support for planned docs.
- Verify sidebar routes build and do not orphan required first-slice pages.
- Verify dependency changes were made with exact pins and no trusted lifecycle
  scripts.
- Verify `bun audit` contains no `workspace:@foxctl/docs-site` findings. The
  current repo may still have pre-existing non-docs workspace findings, which
  should be handled by a separate remediation plan before a workspace-wide
  production claim.

## Risks and Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Recent npm supply-chain compromise pattern | Malicious dependency or install script | Release-age gate, Socket/SFW checks, exact pins, Bun `--ignore-scripts`, no `trustedDependencies` |
| Socket MCP unavailable as callable tool | Vetting is less complete | Record gap, use Socket CLI/SFW, stop on high-risk or unknown direct package |
| Socket API quota/policy gaps | Missing deep policy verdict | Use npm metadata, lockfile review, `npm audit`, and defer broad dependency adoption |
| Existing non-docs workspace advisories | Full `bun audit` exits non-zero | Keep docs-site free of new advisories; track root remediation separately |
| Large transitive dependency graph | More supply-chain surface | Keep first slice to only Astro/Starlight, no themes/plugins/search extras |
| Optional native packages | Install script/native binary risk | Keep Bun `ignoreScripts = true`, do not add `trustedDependencies`, and avoid image optimization features in first slice |
| Docs drift from canonical markdown | Conflicting guidance | Link to canonical docs and mark migration status explicitly |
| Build churn in root workspace | Breaks existing JS packages | Add only docs-specific scripts and verify root status/diffs |

## Phased Implementation Plan

### Phase 1: Vetted Scaffold

Acceptance criteria:

- `packages/docs-site` exists.
- Direct deps are exactly pinned to `astro@6.2.2` and
  `@astrojs/starlight@0.38.4`.
- The docs package has `dev`, `build`, `preview`, and `check` scripts.
- Root scripts expose `dev:docs`, `build:docs`, and `check:docs`.
- The site builds.

### Phase 2: Production IA Shell

Acceptance criteria:

- Sidebar groups match the information architecture above.
- Initial pages exist for start, workflows, reference, architecture,
  operations, production, and roadmap.
- Pages link to canonical docs and clearly state release labels.

### Phase 3: Feature Coverage Pass

Acceptance criteria:

- Every matrix row has either a current page or an explicit planned page.
- Current pages include verification commands or canonical links.
- Planned pages are not mixed into current behavior.

### Phase 4: Release QA

Acceptance criteria:

- Build, check, link check, and diff hygiene pass.
- A residual risk note is added to this plan.
- Follow-up docs implementation issues can be created from the matrix without
  re-deciding the IA.

## Explicit Out of Scope

- No deployment infrastructure for the docs site in this slice.
- No bulk migration of `docs/`.
- No Starlight theme/plugin marketplace exploration.
- No custom search extension beyond Starlight defaults.
- No Go runtime, CLI, storage, protocol, skill, or deployment behavior changes.
- No dependency upgrade to latest Starlight until the release-age gate and
  Socket checks pass.
