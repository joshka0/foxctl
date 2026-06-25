# embedding/worker

Processes queued embedding jobs for named memories. Reads from the embedding
queue store, generates vectors via the configured embedding model, and persists
them to the memory store.

## Usage

```bash
foxctl skills run embedding/worker --process-all true --batch-size 50
```

## Parameters

- `process_all` (bool): Process all queued jobs, not just one batch
- `batch_size` (int): Number of jobs to process per batch (default: 10)
- `max_duration` (int): Maximum runtime in seconds (default: 300)
- `kind` (string): Job kind to process (default: "memory")
- `workspace_id` (string): Target workspace ID
- `stats_only` (bool): Return queue statistics without processing
