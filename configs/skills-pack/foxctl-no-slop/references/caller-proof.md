# Caller Proof with foxctl

Use this reference when `foxctl-no-slop` needs evidence that a compatibility
surface is live, dead, or only test/doc residue.

## Inputs

Start with a small suspect list:

| Suspect | Shape | Intended fate | Notes |
|---------|-------|---------------|-------|
| `<oldName>` | prop / flag / route / function / wrapper | delete or keep | why it looks transitional |

Shapes to check include old product nouns, route aliases, feature flags,
permission branches, config keys, wrapper components, CLI aliases, fallback
functions, migration helpers, and compatibility tests.

## Refresh the Graph

Use the repo-native binary when the installed `foxctl` is a bundled wrapper.

```bash
./bin/foxctl index repo build --dry-run --workspace . --go --typescript --elixir
./bin/foxctl index repo build --workspace . --go --typescript --elixir
```

For repos without Go, add `--go=false`. For semantic-anchor heavy cleanup, add
`--semantic-anchors --include-tests`.

## Exact Caller Search

Run literal searches for the old and new names. Literal mode avoids accidental
regex matches when symbols contain punctuation.

```bash
./bin/foxctl run code/context_grep --input '{
  "mode": "ripgrep",
  "path": ".",
  "pattern": "<oldName>",
  "pattern_mode": "literal",
  "max_blocks": 80,
  "inline_mode": "preview"
}'
```

Repeat for route strings, config keys, prop names, exported functions, command
names, fixture fields, and environment variables.

## Repoindex Caller Search

Find candidate graph nodes:

```bash
./bin/foxctl run repo/index_search --input '{
  "workspace": ".",
  "query": "<symbol-or-surface>",
  "limit": 20,
  "inline_mode": "preview"
}'
```

Expand inbound references from the relevant node IDs:

```bash
./bin/foxctl run repo/index_expand --input '{
  "workspace": ".",
  "seeds": ["<node-id>"],
  "edge_types": ["CALLS", "REFERS_TO", "IMPORTS"],
  "direction": "in",
  "depth": 2,
  "budget": 120,
  "inline_mode": "preview"
}'
```

Use outbound expansion when checking whether a wrapper or adapter is only
delegating to a newer canonical implementation:

```bash
./bin/foxctl run repo/index_expand --input '{
  "workspace": ".",
  "seeds": ["<node-id>"],
  "edge_types": ["CALLS", "REFERS_TO", "IMPORTS"],
  "direction": "out",
  "depth": 2,
  "budget": 120,
  "inline_mode": "preview"
}'
```

## Compact Map

Use DAG grep when you need a readable map of how a surface participates in the
repo graph.

```bash
./bin/foxctl run code/dag_grep --input '{
  "workspace": ".",
  "query": "<surface or flow>",
  "render": "tree",
  "edge_sets": ["structural"],
  "depth": 2,
  "budget": 80,
  "k": 5,
  "inline_mode": "preview"
}'
```

Run with `"direction": "in"` when caller direction matters.

## Semantic Sweep

Use semantic search when the old and new names differ enough that exact text
search may miss the conceptual surface.

```bash
./bin/foxctl run code/semantic_search --input '{
  "workspace": ".",
  "query": "<feature intent and old compatibility surface>",
  "profile": "code",
  "format": "tree",
  "limit": 25,
  "inline_mode": "preview"
}'
```

## Evidence Table

Fill this before keeping compatibility:

| Suspect | Exact hits | Repo graph hits | Semantic hits | Decision | Reason |
|---------|------------|-----------------|---------------|----------|--------|
| `<oldName>` | files/functions or none | inbound callers or none | related surfaces | delete / keep / migrate once | named caller, public contract, or no caller |

Decision guidance:

- `delete`: no current first-party caller, no current public contract, no
  persisted-data migration need.
- `keep`: live caller or public contract exists and belongs in the intended end
  state.
- `migrate once`: persisted data requires a boundary conversion, but runtime
  dual-shape support must be removed after the migration path.

## Final Verification

After editing, rerun the exact searches for deleted names and run the smallest
test set that exercises the final flow:

```bash
./bin/foxctl run code/context_grep --input '{
  "mode": "ripgrep",
  "path": ".",
  "pattern": "<deletedName>",
  "pattern_mode": "literal",
  "max_blocks": 80,
  "inline_mode": "preview"
}'
```

Then run repo-native tests for the affected package or app. If the claim is
subtle, use `verification/cove_verify` with the end-state statement, evidence
table, and test results as the baseline.
