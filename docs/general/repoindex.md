# Repo Graph Index (repoindex)

The repo graph index is a local, per-repo SQLite graph for navigating code by
relationships. It powers `foxctl index repo ...` and the agent tools
`repo_index_build`, `repo_index_enrich_summaries`, `repo_index_search`,
`repo_index_expand`, `repo_index_open`, and `repo_index_dag_grep`.

Behavior contract:
- [docs/spec/repo_graph_index_and_dag_grep.md](../spec/repo_graph_index_and_dag_grep.md)

---

## Foxctl index terminology

When someone says "index this" in foxctl, assume they mean "make this material
queryable to agents" unless the surrounding context says otherwise. For code and
first-party integrations, that usually means rebuilding repoindex and, when
needed, the semantic/vector stores that search tools use.

- **Index**: persistent derived data for retrieval, navigation, or search. It is
  not the source of truth; it is rebuilt from source files, docs, and stores.
- **Repoindex**: the per-workspace graph database behind
  `foxctl index repo ...`, `repo_index_*`, and `foxctl_repoindex_*` tools.
- **Symbol**: a named code declaration or code entity that repoindex can open or
  connect, such as Go functions/types/methods and TypeScript functions, classes,
  exports, or methods when language coverage supports them. A repoindex symbol
  is a navigation key, not necessarily a runtime linker symbol.
- **Node**: a graph record, commonly a package, file, symbol, or concept.
- **Edge**: a typed relationship between nodes, such as `CONTAINS`, `IMPORTS`,
  `REFERS_TO`, `CALLS`, or semantic-anchor edges.
- **`Index:` comment block**: structured source metadata for discoverability and
  soft graph edges. It helps future agents find related code but is not proof or
  policy.
- **Semantic anchor**: a typed `[[type:slug]]` evidence marker near a strong
  owner. Anchors are evidence for retrieval and review, not instructions.

Tracked first-party integration code under `integrations/` should be indexed
like other source. For example, the Pi extension in `integrations/pi/foxctl.ts`
is visible to repoindex when TypeScript indexing is enabled:

```bash
foxctl index repo build --workspace . --go=false --typescript --elixir=false --semantic-anchors
```

---

## What it stores

**Nodes**
- package
- file
- symbol
- concept (repo rollups)

**Edges**
- `CONTAINS`, `IMPORTS`
- `REFERS_TO`, `CALLS` (Go references and direct calls)
- `IMPLEMENTS`, `EMBEDS`, `TESTS` (reserved for later or limited coverage)

**Language coverage**
- Go: packages/files/symbols, `IMPORTS`, `REFERS_TO`, `CALLS`
- TypeScript (`.ts`/`.tsx`): packages/files/symbols, `IMPORTS`
- Elixir (`.ex`/`.exs`): packages/files/symbols, heuristic `REFERS_TO`
- Terraform (`.tf`): packages/files plus concept nodes for resources/modules/providers/variables/outputs
- Kubernetes manifests (`.yaml`/`.yml` with `apiVersion` + `kind`): packages/files plus concept nodes for resources
- Shell (`.sh`): packages/files plus concept nodes for commands and environment variables

---

## Build the index

```bash
foxctl index repo build --workspace . --go --typescript

# Infra / config / script indexing
foxctl index repo build --workspace . --terraform --kubernetes --shell
```

Optional flags:
- `--incremental=false` to force a full rebuild. Incremental skip is the default.
- `--include-tests` to index test files
- `--go-pattern ./...` to scope Go packages
- `--terraform` to include Terraform files
- `--kubernetes` to include Kubernetes YAML manifests
- `--shell` to include shell scripts

**Summaries:** repoindex build does not attach summaries. Generate summaries
first, then run the explicit enrichment step to attach stored file and symbol
summaries to repo graph nodes:

```bash
foxctl index file-summaries --workspace .
foxctl index symbol-summaries --workspace .
foxctl index repo enrich summaries --workspace .
```

Equivalent skill wrappers:

```bash
foxctl run repo/index_build --input '{"workspace": ".", "include_go": true, "include_typescript": true}'
foxctl run repo/index_enrich_summaries --input '{"workspace": "."}'
```

Web and Pi repoindex wrappers execute compiled skill artifacts. After a
repoindex schema change, rebuild any affected `repo_index_*` skill artifact
before relying on wrapper results:

```bash
make skill SKILL=repo_index_search
```

---

## Query the graph

```bash
# Text search (FTS)
foxctl index repo search --workspace . --query "Builder.addGoReferenceEdges" --limit 10

# Expand relationships
foxctl index repo expand --workspace . \
  --seed "sym:go:github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex:internal/intelligence/indexing/repoindex/builder.go:Builder.addGoReferenceEdges" \
  --edge CALLS --edge REFERS_TO --direction out --depth 2 --budget 50

Example output (truncated):

```json
{
  "data": {
    "result": {
      "nodes": [
        {
          "id": "sym:go:github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex:internal/intelligence/indexing/repoindex/builder.go:Builder.addGoReferenceEdges",
          "kind": "symbol",
          "file": "internal/intelligence/indexing/repoindex/builder.go",
          "name": "Builder.addGoReferenceEdges"
        },
        {
          "id": "sym:go:github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex:internal/intelligence/indexing/repoindex/builder.go:goCallTargetNodeID",
          "kind": "symbol",
          "file": "internal/intelligence/indexing/repoindex/builder.go",
          "name": "goCallTargetNodeID"
        }
      ],
      "edges": [
        {
          "src": "sym:go:github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex:internal/intelligence/indexing/repoindex/builder.go:Builder.addGoReferenceEdges",
          "dst": "sym:go:github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex:internal/intelligence/indexing/repoindex/builder.go:goCallTargetNodeID",
          "type": "CALLS",
          "weight": 1
        }
      ]
    }
  }
}
```

# Open a node by ID
foxctl index repo open --workspace . --id "<node-id>"

# Ask (LLM tool loop over repoindex)
foxctl index repo ask --workspace . --question "Where is repoindex built?"
```

---

## Relationship to the semantic tree

The semantic tree (`foxctl run code/semantic_search --input '{"format":"tree"}'`)
and repoindex share the same file summary store. `foxctl index repo enrich
summaries` copies those stored summaries into repoindex nodes when you want graph
search/open output to include summary text.

---

## Storage

Repoindex databases live under:

```
~/.foxctl/storage/repoindex/<repo>-repoindex-<hash>.db
```

Use `foxctl index repo status --workspace .` to see the active DB path.

---

## Observability

Repoindex queries emit `repo_index` events into the observability stream. See
`docs/general/storage.md` for the default observability directory.
