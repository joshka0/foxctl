# Goal: Starlight Docs Production Release Plan

## Goal

Set up a vetted Astro Starlight documentation site for foxctl, then create a
meticulous production documentation release plan that explains foxctl's major
features, supported workflows, and official ways to use the tool.

This goal is complete only when:

- Starlight dependency and setup choices have been vetted before installation or
  package edits.
- A Starlight docs site scaffold exists in the repo and builds locally.
- A production documentation release plan exists and is specific enough to drive
  follow-up implementation work.
- Existing canonical docs remain the source of truth until deliberately migrated
  or mirrored.

## Context

- Repo docs map: `docs/README.md`
- Docs lifecycle policy: `docs/DOC_LIFECYCLE.md`
- Active plans index: `docs/plans/README.md`
- Current Starlight setup references:
  - https://starlight.astro.build/getting-started/
  - https://starlight.astro.build/manual-setup/
  - https://starlight.astro.build/guides/sidebar/
  - https://starlight.astro.build/reference/configuration/
- Current repo package manager context:
  - root `package.json` uses Bun workspaces with `packages/*`
  - existing frontend packages live under `packages/`

The docs release plan should cover official production documentation for at
least these foxctl areas:

- installation and first-run setup
- CLI command families and common workflows
- skills and skill runtime behavior
- repoindex, semantic search, smart search, DAG grep, and code navigation
- ContextWiki architecture, memory, sessions, and task continuity
- agent daemon, agent hierarchy, overseer/subagent model, and room workflows
- room-agile workflows, milestones, tasks, and evidence policy
- GUI/web/runtime surfaces where they are current enough to document
- integrations, MCP, hooks, local development, Kubernetes/deployment docs
- operational troubleshooting, gotchas, and verification commands
- examples, tutorials, and production-readiness checklists

## Milestones

### Milestone 0: Orient and Protect the Worktree

Done when:

- `git status --short` has been inspected.
- Existing unrelated user changes are identified and left untouched.
- If on `main`, create or recommend a feature branch before implementation
  edits.
- Existing docs structure and package workspace layout have been inspected.
- The agent has read `AGENTS.md`, `docs/DOC_LIFECYCLE.md`, and
  `docs/README.md`.

### Milestone 1: Dependency and Supply-Chain Vetting

Done when:

- The agent confirms the current official Starlight setup path from the
  Starlight docs.
- The agent identifies the exact packages required for the chosen scaffold
  before installing them.
- The agent vets `astro`, `@astrojs/starlight`, and any additional docs-site
  package with at least one approved supply-chain check:
  - Socket.dev MCP if available
  - Socket CLI when MCP is unavailable
  - Socket Firewall (`sfw`) package vetting
  - package registry metadata, repository health, license, maintainer, release
    cadence, install script, dependency-risk, and lockfile review if no richer
    vetting tool is available
- Any transitive security override or lockfile security bump is vetted as a
  dependency change, not treated as incidental lockfile churn.
- The agent records a short dependency-vetting note in the production docs plan.
- No package install or lockfile change happens before this vetting note exists.

Stop if:

- required packages show high supply-chain risk
- package install requires secrets or private registry credentials
- dependency vetting cannot be performed with any available method
- the setup would require a package manager migration unrelated to the docs site

### Milestone 2: Starlight Site Scaffold

Done when:

- A Starlight site exists in an appropriate repo location, preferably
  `packages/docs-site` unless repository inspection proves a better location.
- The scaffold follows the current official Starlight setup pattern:
  `astro.config.*`, Starlight integration config, and content collection config
  using Starlight's docs loader/schema where applicable.
- The root workspace scripts are updated only as needed to support docs-site
  development/build commands.
- Existing `docs/` markdown files are not bulk-moved or rewritten.
- The initial Starlight content shell clearly distinguishes:
  - production-ready docs
  - migrated docs
  - planned docs
  - archive/historical docs
- Sidebar/navigation is intentional and matches foxctl product areas rather
  than merely mirroring the filesystem.

### Milestone 3: Production Documentation Release Plan

Done when:

- A new plan document exists under `docs/plans/features/`, with a name such as
  `official-docs-production-release.md`.
- The plan contains:
  - target audience and release posture
  - information architecture
  - feature/workflow documentation matrix
  - current docs inventory and migration strategy
  - Starlight content model and sidebar strategy
  - editorial style and terminology rules
  - canonical vs generated vs archived docs policy
  - examples/tutorials/checklists required for production release
  - docs QA gates and link/build checks
  - dependency and deployment risk register
  - phased implementation plan with acceptance criteria
  - explicit out-of-scope items
- The plan says which docs remain canonical in `docs/general/`,
  `docs/architecture/`, and `docs/spec/`, and how Starlight pages should link to
  or adapt them.
- The plan does not claim features are production-ready unless the current repo
  docs and source support that claim.

### Milestone 4: Verification and Final Review

Done when:

- The docs site builds with the repo's chosen package manager.
- Markdown link checks pass.
- Formatting/diff hygiene checks pass.
- A final self-review identifies residual risks and the next implementation
  slice.

## Constraints

- Do not start by installing dependencies. Vet first.
- Do not add dependencies with high or unknown supply-chain risk.
- Do not migrate the whole repository to a different JavaScript package manager.
- Do not bulk-move existing `docs/` content into Starlight.
- Do not rewrite canonical docs unless required for a small, verified site
  scaffold.
- Do not present aspirational or planned features as official production
  behavior.
- Do not create a marketing landing page. Build the usable docs experience.
- Do not change Go runtime behavior, CLI behavior, storage schemas, protocol
  envelopes, skills, or deployment manifests unless a docs build genuinely
  requires a narrow supporting change.
- Follow `AGENTS.md`, especially docs link hygiene, package-topology rules,
  envelope invariants, and the ban on keyword heuristics for behavior.
- Stop after 3 failed attempts at the same verification failure and summarize
  the blocker.

## Verification

Run focused verification after each phase and full verification before final
completion.

Expected checks:

```bash
git status --short
```

```bash
make check-doc-links
```

```bash
git diff --check
```

Docs-site checks should use the actual scripts created by the scaffold. Likely
examples:

```bash
bun run --cwd packages/docs-site build
```

```bash
bun run --cwd packages/docs-site check
```

```bash
bun audit
```

If the final scaffold uses different scripts, replace these with the exact
commands in the final response and in the production docs plan.

## Done Criteria

- Dependency vetting is documented before package installation.
- Starlight setup is isolated to the docs-site package and necessary workspace
  wiring.
- The docs site builds locally.
- The production documentation release plan is detailed enough for a later agent
  to execute without re-deciding the docs strategy.
- Existing canonical docs remain linked and accurate.
- `bun audit` has no `workspace:@foxctl/docs-site` entries. Existing non-docs
  workspace advisories may remain only if they are explicitly listed as
  separate residual risk.
- `make check-doc-links` and `git diff --check` pass.
- Final response lists changed files, verification results, residual risks, and
  confidence score.

## Stop Conditions

- Stop before installing dependencies if vetting tools are unavailable or
  package risk is unclear.
- Stop before changing package-manager strategy for the repo.
- Stop before moving canonical docs wholesale.
- Stop before documenting a feature as official production behavior when the
  current code/docs do not support that claim.
- Stop before adding deployment infrastructure for the docs site unless
  explicitly requested.
- Stop after 3 failed attempts at the same failing check and summarize the exact
  command and failure.
