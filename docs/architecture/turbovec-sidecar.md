# Turbovec Sidecar Architecture

Status: current first slice

This document describes the turbovec sidecar integration: a compressed vector
search service that accelerates semantic retrieval in foxctl workspaces.

## Intent

Turbovec accelerates the VectorRecall path in `internal/intelligence/searchindex`
by delegating approximate nearest-neighbour search to a dedicated Unix domain
socket sidecar (`turbovecd`). The sidecar uses the TurboQuant algorithm
(2–4 bit scalar product quantization with SIMD search) to achieve fast
approximate search over high-dimensional embedding vectors, then a second-stage
exact cosine rerank restores recall quality.

The SQL store (SQLite/Postgres) remains the source of truth for document
metadata and exact embeddings. Turbovec is a best-effort acceleration layer
that falls back to brute-force SQL cosine similarity when the sidecar is
unavailable.

## Architecture Overview

```text
┌─────────────────────────┐       Unix socket        ┌──────────────────┐
│  foxctl (Go)            │  ~/.foxctl/turbovecd.sock │  turbovecd (Rust)│
│                         │ ───────────────────────── │                  │
│  turbovecStore          │       binary frames       │  Index manager   │
│   ├─ sqlStore (SQL)     │                           │  PQ codes        │
│   └─ VectorIndex        │                           │  SIMD search     │
│       ├─ Client         │                           │                  │
│       └─ IdMapIndex     │                           │                  │
└─────────────────────────┘                           └──────────────────┘
        │                                                    │
        ▼                                                    ▼
  SQLite / Postgres                                   .tvim + .idmap.json
  (metadata + exact                                    (persisted index)
   embeddings)
```

### Component responsibilities

- **turbovecd** (Rust sidecar): Owns compressed vector indices in memory.
  Handles create, add, search, search_filtered, remove, save, load, drop, and
  prepare operations over a binary frame protocol.
- **turbovec.Client** (Go): Thread-safe client that connects to the sidecar
  over a Unix domain socket. Each call acquires a mutex so frames are not
  interleaved.
- **turbovec.VectorIndex** (Go): Manages the sidecar connection for a single
  workspace's vector search. Handles index lifecycle (create/load/save), maps
  foxctl string document IDs to turbovec uint64 external IDs, and translates
  search results back.
- **searchindex.turbovecStore** (Go): Wraps a SQL-backed `Store` and
  accelerates `VectorRecall` via the turbovec sidecar. All other operations
  (LexicalRecall, ExactRecall, Upsert, Delete, etc.) delegate to the
  underlying `sqlStore`.

### IdMapIndex

The sidecar uses uint64 external IDs internally. Foxctl uses string document
IDs. The `VectorIndex` maintains a bidirectional map:

- `idMap`: string doc ID → uint64 external ID
- `reverse`: uint64 external ID → string doc ID

This map is persisted as a `.idmap.json` file alongside the `.tvim` index file.

## Protocol

The sidecar communicates over a simple binary frame protocol on a Unix domain
socket. All multi-byte integers are big-endian unless otherwise noted in the
payload encoding (the Go client uses little-endian for most payload fields).

### Frame format

```text
[cmd:u8] [payload_len:u32 BE] [payload:[]byte]
```

### Commands

| Command    | Byte   | Direction    | Description                                      |
|------------|--------|--------------|--------------------------------------------------|
| PING       | `0x00` | client→server| Liveness check.                                  |
| CREATE     | `0x01` | client→server| Create a named index with dimension and bit width.|
| ADD        | `0x02` | client→server| Add a single vector with an external ID.          |
| ADD_BATCH  | `0x0B` | client→server| Add multiple vectors with explicit IDs.           |
| SEARCH     | `0x03` | client→server| Top-k approximate search.                         |
| SEARCH_FILTERED | `0x04` | client→server| Top-k search restricted to an allowlist of IDs. |
| REMOVE     | `0x05` | client→server| Remove a vector by external ID.                   |
| SAVE       | `0x06` | client→server| Persist a named index to a file path.             |
| LOAD       | `0x07` | client→server| Load an index from disk into a named slot.        |
| INFO       | `0x08` | client→server| Return metadata (dim, n_vectors, bit_width).      |
| DROP       | `0x09` | client→server| Remove a named index from memory.                 |
| PREPARE    | `0x0A` | client→server| Eagerly populate search caches for an index.      |

### Response status byte

Every response payload starts with a status byte:

| Status      | Byte   | Meaning                       |
|-------------|--------|-------------------------------|
| OK          | `0x00` | Success.                      |
| ERR         | `0x01` | Generic error (message follows). |
| NOT_FOUND   | `0x02` | Named index not found.        |

## Go Integration

### Package layout

```text
internal/intelligence/turbovec/
  client.go         — Client, frame protocol, Dial, Ping, Create, Add, etc.
  vector_index.go   — VectorIndex, IndexConfig, bidirectional ID map.
  idmap.go          — .idmap.json persistence for the ID map.
  client_test.go    — Unit tests for the client.
  integration_test.go — Integration tests (require turbovecd binary).

internal/intelligence/searchindex/
  turbovec_store.go — turbovecStore, TurboVecConfig, WrapWithTurboVec,
                      oversample+rerank VectorRecall pipeline.
```

### turbovec.Client

The client is safe for concurrent use — each call acquires a mutex so frames
are not interleaved on the shared connection.

```go
client, err := turbovec.Dial("/home/user/.foxctl/turbovecd.sock")
client.Create("my-index", 4096, 4)
client.AddBatch("my-index", flatVectors, 4096, ids)
hits, _ := client.Search("my-index", queryEmbedding, 20)
```

### VectorIndex

`VectorIndex` manages the full lifecycle for one workspace:

- Lazy connection on first use via `EnsureConnected()`.
- Loads an existing `.tvim` index and `.idmap.json` if present.
- Creates a fresh index when no saved state exists.
- On `Upsert`, removes any old entry for the same doc ID before adding the new vector.
- Translates between string doc IDs and uint64 external IDs transparently.

### turbovecStore wrapper

`turbovecStore` wraps any `Store` (currently `sqlStore`):

- **Upsert**: stores in SQL (source of truth) and adds the embedding to turbovec (best-effort).
- **Delete**: removes from both SQL and turbovec.
- **VectorRecall**: oversample+rerank pipeline (see below).
- All other methods delegate to the underlying store.

## Oversample + Rerank Pipeline

The `VectorRecall` path in `turbovecStore` uses a two-stage retrieval strategy:

1. **Oversample**: Request `limit × oversample_factor` (default 3×) candidates
   from the turbovec approximate index.
2. **Fetch exact embeddings**: Retrieve the full float32 embeddings for the
   candidate doc IDs from the SQL store.
3. **Exact cosine rerank**: Compute true cosine similarity between the query
   and each candidate, sort by exact score, and return only the top `limit`
   results.

When `CandidateIDs` is provided (e.g., from a BM25-first pipeline), the
turbovec search is restricted to those IDs via `SearchFiltered`, enabling
BM25-first → vector-rerank pipelines.

If the turbovec sidecar is unavailable or returns no results, the wrapper
falls back to brute-force SQL cosine similarity transparently.

## Configuration

### Environment variables

| Variable                       | Default                       | Description                                    |
|--------------------------------|-------------------------------|------------------------------------------------|
| `FOXCTL_TURBOVEC_ENABLED`      | `false`                       | Enable the turbovec sidecar for vector recall. |
| `FOXCTL_TURBOVEC_SOCKET`       | `~/.foxctl/turbovecd.sock`    | Path to the turbovecd Unix socket.             |
| `FOXCTL_TURBOVEC_BIT_WIDTH`    | `4`                           | Quantization bit width (2, 3, or 4).           |

### Config file

```yaml
turbovec:
  enabled: true
  socket_path: /home/user/.foxctl/turbovecd.sock
  bit_width: 4
```

## Systemd Service

The turbovecd sidecar runs as a user-level systemd service:

```ini
# ~/.config/systemd/user/turbovecd.service
[Unit]
Description=Turbovec Vector Search Sidecar
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/turbovecd --socket %h/.foxctl/turbovecd.sock
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
```

Enable and start:

```bash
systemctl --user enable turbovecd
systemctl --user start turbovecd
```

## Benchmarks

Benchmark results from `cmd/bench-turbovec` (d=4096, Qwen3-Embedding-8B
production dimensions, bit_width=4):

### Raw turbovec

| Metric          | Value                          |
|-----------------|--------------------------------|
| Dimensions      | 4096                           |
| Vectors         | 5000                           |
| Raw turbovec    | 4.6 ms                         |
| Speedup vs JSON | 1313×                          |
| Recall@20       | 0.70                           |

### Turbovec + oversample/rerank

| Metric                | Value                          |
|-----------------------|--------------------------------|
| Dimensions            | 4096                           |
| Vectors               | 1000                           |
| Total (search+rerank) | 3.6 ms                         |
| Speedup vs JSON       | 341×                           |
| Recall@20 (reranked)  | 0.90                           |

### Memory compression

| Metric                | Value                          |
|-----------------------|--------------------------------|
| Raw (float32)         | ~19.5 MB (1000 × 4096 × 4)    |
| Compressed (4-bit PQ) | ~2.4 MB (1000 × 4096 × 4/8)   |
| Compression ratio     | ~8×                            |

## Persisted Files

| File                 | Location                                  | Description                                       |
|----------------------|-------------------------------------------|---------------------------------------------------|
| `.tvim` index        | `~/.foxctl/storage/<workspace>.tvim`      | Compressed turbovec index (PQ codes + metadata).   |
| `.idmap.json`        | `~/.foxctl/storage/<workspace>.idmap.json`| String doc ID → uint64 ID mapping.                 |
| Unix socket          | `~/.foxctl/turbovecd.sock`                | Sidecar communication socket.                      |

## Source Layout

```text
internal/intelligence/turbovec/     — Go client, VectorIndex, ID map
internal/intelligence/searchindex/  — turbovecStore wrapper, TurboVecConfig
internal/platform/config/config.go  — TurbovecSettings, env var bindings
cmd/bench-turbovec/main.go          — Benchmark harness
turbovec-server/                    — Rust sidecar (turbovecd binary)
```
