# libSQL Server Deployment

Self-hosted libSQL (sqld) server for cross-machine agentctl sync with native vector search.

## Quick Start

```bash
# Start the server
cd deployments/libsql
docker compose up -d

# Preview migration (dry run)
agentctl run libsql/migrate --input '{"libsql_url": "http://localhost:8080", "dry_run": true}'

# Run migration
agentctl run libsql/migrate --input '{"libsql_url": "http://localhost:8080"}'
```

## Tailscale Access

For cross-machine access via Tailscale:

```bash
# From remote machine, use Tailscale IP of the host
agentctl run libsql/migrate --input '{"libsql_url": "http://100.x.x.x:8080"}'
```

## Migration Options

| Parameter | Default | Description |
|-----------|---------|-------------|
| `libsql_url` | (required) | sqld server URL |
| `auth_token` | - | JWT auth token if server requires auth |
| `scope` | `all` | What to migrate: `all`, `memories`, `sessions` |
| `source_dir` | `~/.agentctl/storage` | Source SQLite directory |
| `batch_size` | `100` | Records per batch |
| `dry_run` | `false` | Preview without writing |
| `vector_dims` | `1024` | Embedding dimensions (1024 for Voyage) |

## Data Migrated

- **named_memory** - Memories, gotchas, codemaps, symbols with embeddings
- **sessions** - Session history with embeddings

Both tables use `F32_BLOB(1024)` columns for native libsql vector search.

## Vector Search

After migration, query vectors directly:

```sql
-- Find similar memories using cosine similarity
SELECT name, summary,
       vector_distance_cos(embedding, vector32('[0.1, 0.2, ...]')) as distance
FROM named_memory
WHERE embedding IS NOT NULL
ORDER BY distance
LIMIT 10;
```

## Production Considerations

1. **Authentication**: Set `SQLD_AUTH_JWT_KEY_FILE` for JWT auth
2. **Backup**: Volume `sqld-data` contains all data
3. **TLS**: Use reverse proxy (Caddy/nginx) for HTTPS
4. **Replication**: Use gRPC port 5001 for replica sync
