# Refactor Surface Snapshot — 2026-03-29

This note captures the current refactor surface exposed by the new wrapper-driven
two-stage workflow:

1. `agentctl refactor scout` for local deterministic hotspot retrieval
2. `agentctl refactor advisor` for second-stage shortlist ranking via
   `openrouter:google/gemini-3.1-flash-lite-preview`

This is a working note, not a canonical plan.

Framing:

- The main purpose of this workflow is to surface deterministic refactors that
  reduce complexity, simplify orchestration, and remove repeated or overloaded
  logic.
- The strongest signals today are still Go-centric.
- TypeScript is directionally useful but mostly file-structure oriented.
- Python and Elixir currently have little or no urgent surface in this repo.

## Commands Used

```bash
agentctl refactor advisor --language go --path ./internal --max-findings 6 --shortlist-size 5
agentctl refactor advisor --language go --path . --max-findings 8 --shortlist-size 5
agentctl refactor advisor --language typescript --path ./packages --max-findings 8 --shortlist-size 5
agentctl refactor advisor --language python --path ./scripts --max-findings 8 --shortlist-size 5
agentctl refactor scout --language elixir --path . --max-results 8
```

## Go — Internal Surface

Top shortlist:

1. `internal/runtime/actor/agent_actor.go` — `*AgentActor.handleAsk`
2. `internal/adapters/skillslib/codeblocks/expander.go` — `buildGoIndex`
3. `internal/agent/runtime/runtime.go` — `sessionResearchSummary`
4. `internal/adapters/skillslib/codeblocks/matches.go` — `ExpandMatchesWithOptions`
5. `internal/adapters/skillslib/codeblocks/expander.go` — file-level cluster

Interpretation:

- `*AgentActor.handleAsk` is a strong function-level target for complexity
  reduction and orchestration cleanup.
- `buildGoIndex` is both a direct hotspot and part of a larger
  `expander.go` cluster, so it is a good “fix one function, reveal the file
  boundaries” candidate.
- `sessionResearchSummary` and `ExpandMatchesWithOptions` look like smaller,
  tractable cleanup refactors rather than broad reorganizations.

Most useful reading of this surface:

- `*AgentActor.handleAsk` is a “simplify the decision tree” refactor.
- `buildGoIndex` is a “split orchestration from branch-heavy helper logic”
  refactor.
- `sessionResearchSummary` and `ExpandMatchesWithOptions` are “flatten and
  extract helpers” refactors.

## Go — Repo-Wide Surface

Top shortlist:

1. `cmd/agentctl/cmd/agent.go` — `runAgentWatch`
2. `cmd/agentctl/cmd/agent.go` — `runAgentAskWithRoute`
3. `cmd/agentctl/cmd/optimize_dataset_claude.go` — `buildClaudeSessionFromMessages`
4. `cmd/agentctl_viewer/tui.go` — `model.renderStatusBar`
5. `cmd/agentctl/cmd/index.go` — file-level cluster

Interpretation:

- `runAgentWatch` and `runAgentAskWithRoute` are the clearest “reduce repeated
  logic” targets in the CLI surface. They share the same file and look like a
  good pair for extracting common orchestration/helpers.
- `buildClaudeSessionFromMessages` is a narrower branch-flattening target.
- `index.go` is real debt, but it should come after function-level extraction,
  not before.

Most useful reading of this surface:

- `agent.go` is the best current place to remove duplicated orchestration and
  converge adjacent command paths onto shared helpers.
- `index.go` is real complexity debt, but it is a second-order target; the
  function-level wins should come first.

## TypeScript Surface

Top shortlist:

1. `packages/gui-agent/src/components/agents/AgentDetailView.tsx`
2. `packages/gui-agent/src/api/client.ts`
3. `packages/tui/src/views/OrchestrationView.tsx`
4. `packages/data/src/client.ts`
5. `packages/tui/src/hooks/useData.ts`

Interpretation:

- The TypeScript surface is currently dominated by file-level “god file”
  signals: large line count plus high top-level symbol density.
- This is directionally useful, but weaker than the Go function-level results.
- The best TS candidates are still credible:
  - split `AgentDetailView.tsx` into sub-components and hooks
  - split `packages/gui-agent/src/api/client.ts` by domain/API area
  - split `packages/data/src/client.ts` between transport and mapping logic
  - split `useData.ts` into narrower data hooks

Most useful reading of this surface:

- TS currently tells us more about “this module is too broad” than “this exact
  function is the right extraction point.”
- The two highest-value TS simplification targets are probably:
  - `packages/gui-agent/src/api/client.ts`
  - `packages/gui-agent/src/components/agents/AgentDetailView.tsx`

## Python Surface

Result:

- No refactor findings above threshold under `./scripts`

Interpretation:

- The Python scripts are small enough that the current heuristics do not flag
  urgent complexity or decomposition issues.
- This likely means no immediate Python refactor work is required.

## Elixir Surface

Result:

- No Elixir files detected in the repo
- No Elixir findings

Interpretation:

- There is currently no Elixir refactor surface to review in this repo.

## Current Limits

The tool is good enough to trust for deterministic hotspot discovery in Go, but
not yet equally strong in every language.

Current limits:

- Go:
  - Strongest surface because we have AST-backed complexity signals plus
    cross-language signature/call extraction.
- TypeScript:
  - Mostly file-level overload detection rather than precise function-level
    simplification cues.
- Python:
  - Low-surface repo area; no urgent findings right now.
- Elixir:
  - No files in this repo, so no current surface.

The next deterministic gains should come from:

1. Repeated orchestration/call-sequence fingerprints across functions
2. Cross-file helper extraction candidates
3. Better cohesion scoring for large receivers/modules
4. Stronger TS/Python/Elixir function-level simplification heuristics

## Current Tool Read

What the workflow is good at today:

- Go function hotspots with multi-signal evidence
- Large TypeScript “god files” that should be split by responsibility
- Producing a short ranked queue instead of a flat hotspot dump

What it is weaker at today:

- TypeScript function-level precision (still more file-level than function-level)
- Python/Elixir surface depth when the repo barely contains those languages
- Detecting repeated logic directly; repeated-logic candidates are inferred from
  adjacent hotspots rather than scored separately

## Most Actionable Refactors

If the goal is to reduce complexity, simplify code paths, and remove repeated
logic, the strongest current candidates are:

1. `cmd/agentctl/cmd/agent.go`
   - Extract shared orchestration from `runAgentWatch` and `runAgentAskWithRoute`
   - This is the clearest repeated-logic reduction target
2. `internal/runtime/actor/agent_actor.go`
   - Decompose `*AgentActor.handleAsk`
   - Good complexity-reduction target in a core runtime path
3. `internal/adapters/skillslib/codeblocks/expander.go`
   - Refactor `buildGoIndex` first, then revisit the file-level cluster
   - Good candidate for “simplify one function, then split the file”
4. `packages/gui-agent/src/api/client.ts`
   - Split by API domain
   - Best TS structural cleanup target

## Current Recommendation

If the team wants immediate value from this tool, use it like this:

1. Run `agentctl refactor scout` or `agentctl refactor advisor` with an explicit
   language and narrow path.
2. Treat the top 2-3 Go function hotspots as likely real work.
3. Treat the TS shortlist as “module split candidates,” not precise
   function-level truth yet.
4. Use the second-stage model only to rank and sequence local scout findings,
   not to discover them.

## Recommendation

If the goal is to reduce complexity, simplify control flow, and remove repeated
logic, the first refactor should be:

1. `cmd/agentctl/cmd/agent.go`
   - Extract shared orchestration from `runAgentWatch` and
     `runAgentAskWithRoute`
   - This is the best immediate simplification target because it combines
     repeated logic reduction with complexity reduction in a core CLI surface

## Caveats

- Go results are stronger than TS/Python because the current scout has richer
  Go AST-backed signals.
- TypeScript findings are currently more file-level than function-level, so they
  should be treated as shortlist candidates rather than precise proof.
- The second-stage model is best used for ranking and sequencing, not for
  retrieval. The local scout remains the primary truth source.
