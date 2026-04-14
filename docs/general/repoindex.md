# Repo Graph Index (repoindex)

The repo graph index is a local, per-repo SQLite graph for navigating code by
relationships. It powers `foxctl index repo ...` and the agent tools
`repo_index_search`, `repo_index_expand`, and `repo_index_open`.

Behavior contract:
- [docs/spec/repo_graph_index_and_dag_grep.md](../spec/repo_graph_index_and_dag_grep.md)

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
- `--include-tests` to index test files
- `--go-pattern ./...` to scope Go packages
- `--terraform` to include Terraform files
- `--kubernetes` to include Kubernetes YAML manifests
- `--shell` to include shell scripts

**Summaries:** repoindex reuses file summaries and symbol summaries. Run file
summary generation to populate file node summaries and package/repo rollups,
then generate symbol summaries to populate symbol node summaries:

```bash
foxctl index file-summaries --workspace .
foxctl index symbol-summaries --workspace .
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
and repoindex share the same file summary store. File summaries become file node
summaries in repoindex, and package/repo rollups are generated from those file
summaries. This lets you navigate top-down (tree) and sideways (graph edges)
using the same source summaries.

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
