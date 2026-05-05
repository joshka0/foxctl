# Vector Helpers

`internal/storage/vector` contains pure Go helpers for encoding float32
embeddings and computing fallback cosine similarity.

Vector-capable persistence is handled by the canonical storage backends:

- Turso for local/remote SQLite-family storage.
- Postgres/pgvector for server deployments.

This package does not load an in-process SQLite vector extension and does not
require CGO.
