// Package observability provides wide event observability following loggingsucks.com principles.
// Wide events capture comprehensive per-operation context instead of narrow per-component events.
//
// Persistence Options:
//
// Events can be persisted in multiple ways:
//   - NDJSON file (default): Fast append-only writes to $FOXCTL_OBS_DIR/events/wide_events.ndjson
//   - SQLite: Direct writes to $FOXCTL_OBS_DIR/events.db for queryability
//   - Hybrid: NDJSON + background SQLite sync (recommended for high-value events)
//
// The hybrid approach provides the best of both worlds:
//   - Fast NDJSON writes on the hot path (no blocking)
//   - Background goroutine syncs NDJSON to SQLite for querying
//   - Full replay capability from NDJSON files
//   - Rich querying via SQLite
//
// Usage:
//
//	// Use default persistence (NDJSON only)
//	obs.Emit(ctx, event.Success(duration))
//
//	// Enable SQL persistence for high-value events
//	obs.Emit(ctx, event.
//	    WithPersistence(obs.PersistSQL).
//	    Success(duration))
//
//	// Write to a custom NDJSON file
//	obs.Emit(ctx, event.
//	    WithPersistenceFile("skill_runs").
//	    Success(duration))
package observability
