# LadybugDB Phase 0 CLI Gate

Status: passed on 2026-05-11

This note records the disposable CLI proof for
[docs/plans/features/ladybugdb-graph-projection-spike.md](../plans/features/ladybugdb-graph-projection-spike.md).

Phase 0 stayed outside foxctl runtime code. It used the standalone `lbug` CLI,
temporary CSV fixtures, and a temporary on-disk database under
`/private/tmp/ladybug-phase0`.

## Environment

- Machine: Darwin arm64
- CLI: `Lbug 0.16.1`
- Install source:
  `https://github.com/LadybugDB/ladybug/releases/latest/download/lbug_cli-osx-arm64.tar.gz`
- Install method:

```bash
mkdir -p /private/tmp/ladybug-phase0/bin
curl -sS -L -o /private/tmp/ladybug-phase0/lbug_cli-osx-arm64.tar.gz \
  https://github.com/LadybugDB/ladybug/releases/latest/download/lbug_cli-osx-arm64.tar.gz
tar -xzf /private/tmp/ladybug-phase0/lbug_cli-osx-arm64.tar.gz \
  -C /private/tmp/ladybug-phase0/bin
chmod +x /private/tmp/ladybug-phase0/bin/lbug
HOME=/private/tmp/ladybug-phase0 /private/tmp/ladybug-phase0/bin/lbug -v
```

Version output:

```text
Lbug 0.16.1
```

`HOME=/private/tmp/ladybug-phase0` is required in this sandbox because `lbug`
creates `~/.lbdb` on startup.

## Fixture

The proof uses one strict node table and two typed relationship tables. This
validates strict table creation, CSV import, relationship endpoint resolution,
read-only reopen, bounded traversal, and shortest-path traversal. Phase 1 now
uses the same single graph-node CSV shape.

`nodes_graph.csv`:

```csv
id,kind,repo_key,repo_root_identity,pkg,file,name,signature,span_start,span_end,exported,doc,summary,hash,updated_at,meta_json
repo-fixture,repo,repo-fixture,repo-fixture,,,repo-fixture,,0,0,false,,,,,
repo-fixture::pkg:go:example,package,repo-fixture,repo-fixture,go:example,,example,,0,0,false,,package summary,pkg-hash,2026-05-11T12:00:00Z,"{""a"":1,""z"":2}"
repo-fixture::file:go:example:internal/a.go,file,repo-fixture,repo-fixture,go:example,internal/a.go,a.go,,0,0,false,,,file-a-hash,2026-05-11T12:00:00Z,
repo-fixture::file:go:example:internal/b.go,file,repo-fixture,repo-fixture,go:example,internal/b.go,b.go,,0,0,false,,,file-b-hash,2026-05-11T12:00:00Z,
repo-fixture::sym:go:example:A,symbol,repo-fixture,repo-fixture,go:example,internal/a.go,A,func A(),3,5,true,A docs,,symbol-a-hash,2026-05-11T12:00:00Z,
repo-fixture::sym:go:example:B,symbol,repo-fixture,repo-fixture,go:example,internal/b.go,B,func B(),7,9,true,,,symbol-b-hash,2026-05-11T12:00:00Z,
```

`edges_contains.csv`:

```csv
src_id,dst_id,weight,source,meta_json
repo-fixture,repo-fixture::pkg:go:example,1,export_projection,
repo-fixture::pkg:go:example,repo-fixture::file:go:example:internal/a.go,1,repoindex,
repo-fixture::pkg:go:example,repo-fixture::file:go:example:internal/b.go,1,repoindex,
repo-fixture::file:go:example:internal/a.go,repo-fixture::sym:go:example:A,1,repoindex,
repo-fixture::file:go:example:internal/b.go,repo-fixture::sym:go:example:B,1,repoindex,
```

`edges_calls.csv`:

```csv
src_id,dst_id,weight,source,meta_json
repo-fixture::sym:go:example:A,repo-fixture::sym:go:example:B,1,repoindex,"{""kind"":""call""}"
```

## Schema And Import

Script:

```cypher
:mode json
CREATE NODE TABLE GraphNode(
  id STRING PRIMARY KEY,
  kind STRING,
  repo_key STRING,
  repo_root_identity STRING,
  pkg STRING,
  file STRING,
  name STRING,
  signature STRING,
  span_start STRING,
  span_end STRING,
  exported STRING,
  doc STRING,
  summary STRING,
  hash STRING,
  updated_at STRING,
  meta_json STRING
);
CREATE REL TABLE Contains(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
CREATE REL TABLE Calls(
  FROM GraphNode TO GraphNode,
  weight DOUBLE,
  source STRING,
  meta_json STRING
);
COPY GraphNode FROM "/private/tmp/ladybug-phase0/fixtures/nodes_graph.csv" (header=true);
COPY Contains FROM "/private/tmp/ladybug-phase0/fixtures/edges_contains.csv" (header=true);
COPY Calls FROM "/private/tmp/ladybug-phase0/fixtures/edges_calls.csv" (header=true);
MATCH (n:GraphNode) RETURN count(n) AS node_count;
MATCH ()-[r:Contains]->() RETURN count(r) AS contains_count;
MATCH ()-[r:Calls]->() RETURN count(r) AS calls_count;
```

Command:

```bash
HOME=/private/tmp/ladybug-phase0 \
  /private/tmp/ladybug-phase0/bin/lbug \
  /private/tmp/ladybug-phase0/proof-graphnode16.lbug -b -s \
  < /private/tmp/ladybug-phase0/schema_import.cypher
```

Output:

```text
mode set as json
[
{"result":"Table GraphNode has been created."}
]
[
{"result":"Table Contains has been created."}
]
[
{"result":"Table Calls has been created."}
]
[
{"result":"6 tuples have been copied to the GraphNode table."}
]
[
{"result":"5 tuples have been copied to the Contains table."}
]
[
{"result":"1 tuples have been copied to the Calls table."}
]
[
{"node_count":6}
]
[
{"contains_count":5}
]
[
{"calls_count":1}
]
```

## Query Proofs

All query commands reopened the database read-only with `-r`.

Bounded path:

```cypher
:mode json
MATCH p = (repo:GraphNode)-[:Contains*1..3]->(sym:GraphNode)
WHERE repo.id = "repo-fixture" AND sym.id = "repo-fixture::sym:go:example:A"
RETURN length(p) AS path_len, properties(nodes(p), 'id') AS node_ids;
```

Output:

```text
mode set as json
[
{"path_len":3,"node_ids":["repo-fixture","repo-fixture::pkg:go:example","repo-fixture::file:go:example:internal/a.go","repo-fixture::sym:go:example:A"]}
]
```

Shortest path over containment/calls:

```cypher
:mode json
MATCH p = (a:GraphNode)-[e:Contains|:Calls* SHORTEST 1..5]->(b:GraphNode)
WHERE a.id = "repo-fixture" AND b.id = "repo-fixture::sym:go:example:B"
RETURN length(e) AS path_len, properties(nodes(p), 'id') AS node_ids;
```

Output:

```text
mode set as json
[
{"path_len":3,"node_ids":["repo-fixture","repo-fixture::pkg:go:example","repo-fixture::file:go:example:internal/b.go","repo-fixture::sym:go:example:B"]}
]
```

Shortest direct call path:

```cypher
:mode json
MATCH p = (a:GraphNode)-[e:Calls* SHORTEST 1..3]->(b:GraphNode)
WHERE a.id = "repo-fixture::sym:go:example:A" AND b.id = "repo-fixture::sym:go:example:B"
RETURN length(e) AS path_len, properties(nodes(p), 'id') AS node_ids;
```

Output:

```text
mode set as json
[
{"path_len":1,"node_ids":["repo-fixture::sym:go:example:A","repo-fixture::sym:go:example:B"]}
]
```

## Failure Modes Found

- The older documented macOS universal asset URL
  `lbug_cli-osx-universal.tar.gz` returned `Not Found`; the current install
  script selects `lbug_cli-osx-arm64.tar.gz` on Darwin arm64.
- In this sandbox, default startup failed because `lbug` tried to create
  `/Users/joshka/.lbdb`. Setting `HOME=/private/tmp/ladybug-phase0` kept the
  proof self-contained.
- Neo4j-style list comprehension over `nodes(p)` did not bind as expected.
  `properties(nodes(p), 'id')` returned path node IDs correctly.
- Shortest-path syntax is relationship-pattern local:
  `[e:Contains|:Calls* SHORTEST 1..5]`, not `MATCH p = SHORTEST 1 ...`.

## Phase 0 Decision

Phase 0 passed for a CLI-only proof:

- strict node and relationship tables can be created
- CSV node import works with `header=true`
- CSV relationship import works after endpoint nodes exist
- an on-disk projection can be reopened read-only after import
- bounded path traversal works
- shortest path traversal works
- no Go dependency, cgo setup, Go binding, or foxctl runtime integration was
  required

The Phase 1 export contract was revised after this proof: `nodes_graph.csv`
now matches the strict `GraphNode` shape proven here, while relationship CSVs
remain typed by edge kind.
