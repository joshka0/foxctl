package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/agent/daemon"
	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/agents"
	"github.com/jkatigb/agentctl/internal/storage/cache"
	"github.com/jkatigb/agentctl/internal/storage/contextbuffer"
	"github.com/jkatigb/agentctl/internal/storage/coordination"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
	"github.com/jkatigb/agentctl/internal/storage/graph"
	"github.com/jkatigb/agentctl/internal/storage/jobs/persist"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/jkatigb/agentctl/internal/storage/tasks"
	"github.com/jkatigb/agentctl/internal/storage/testwatch"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
	"github.com/spf13/cobra"
)

// storeSpec describes one logical store for migration.
type storeSpec struct {
	name       string
	sqliteFile string
	pgSchema   string
	tables     []string
	migrate    func(context.Context, *sql.DB) error
}

var allStores = []storeSpec{
	{
		name:       "memory",
		sqliteFile: "memory.db",
		pgSchema:   "memory",
		tables:     []string{"named_memory", "embedding_metadata", "indexer_state"},
		migrate:    memory.MigrateSchema,
	},
	{
		name:       "tasks",
		sqliteFile: "tasks.db",
		pgSchema:   "tasks",
		tables:     []string{"tasks", "active_tasks", "epics", "active_epics"},
		migrate:    tasks.MigrateSchema,
	},
	{
		name:       "sessions",
		sqliteFile: "sessions.db",
		pgSchema:   "sessions",
		tables:     []string{"sessions", "session_turns"},
		migrate:    sessions.MigrateSchema,
	},
	{
		name:       "graph",
		sqliteFile: "graph.db",
		pgSchema:   "graph",
		tables:     []string{"graph_nodes", "graph_edges"},
		migrate:    graph.MigrateSchema,
	},
	{
		name:       "jobs",
		sqliteFile: "jobs.db",
		pgSchema:   "jobs",
		tables:     []string{"jobs"},
		migrate:    persist.MigrateSchema,
	},
	{
		name:       "cache",
		sqliteFile: "cache.db",
		pgSchema:   "cache",
		tables:     []string{"auto_cache"},
		migrate:    cache.MigrateSchema,
	},
	{
		name:       "agents",
		sqliteFile: "agents.db",
		pgSchema:   "agents",
		tables:     []string{"agents"},
		migrate:    agents.MigrateSchema,
	},
	{
		name:       "coordination",
		sqliteFile: "coordination.db",
		pgSchema:   "coordination",
		tables:     []string{"daemon_leases"},
		migrate:    coordination.MigrateSchema,
	},
	{
		name:       "testwatch",
		sqliteFile: "test_watch.db",
		pgSchema:   "testwatch",
		tables:     []string{"test_status"},
		migrate:    testwatch.MigrateSchema,
	},
	{
		name:       "contextbuffer",
		sqliteFile: "contextbuffer.db",
		pgSchema:   "contextbuffer",
		tables:     []string{"context_entries"},
		migrate:    contextbuffer.MigrateSchema,
	},
	{
		name:       "trajectory",
		sqliteFile: "trajectory.db",
		pgSchema:   "trajectory",
		tables:     []string{"trajectories", "user_requests", "trajectory_events"},
		migrate:    trajectory.MigrateSchema,
	},
	{
		name:       "dedupe",
		sqliteFile: "daemon_dedupe.db",
		pgSchema:   "dedupe",
		tables:     []string{"daemon_dedupe"},
		migrate:    daemon.MigrateDedupe,
	},
	{
		name:       "companion",
		sqliteFile: "companion.db",
		pgSchema:   "companion",
		tables:     []string{"companion_turns", "companion_deleted_conversations", "companion_conversation_titles", "companion_characters", "companion_character_overlays", "companion_generated_backgrounds", "companion_generated_voices", "companion_presence_bundles", "companion_events", "companion_hard_state_entries", "companion_soft_episodes", "companion_evidence_snippets", "companion_assumptions_ledger", "companion_memory_mode_state", "companion_open_episode", "companion_open_tool_runs", "companion_extraction_staging", "companion_hard_state_cache", "companion_evidence_fts"},
		migrate:    companion.MigrateSchema,
	},
}

func newDBCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "db",
		Short: "Database management commands",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(newDBMigrateCommand())
	return cmd
}

// dbEnvelope emits a JSON envelope to w with the standard fields.
func dbEnvelope(enc *json.Encoder, now func() time.Time, status string, data map[string]any, errMsg string) {
	var errVal any
	if errMsg != "" {
		errVal = errMsg
	}
	_ = enc.Encode(map[string]any{
		"version": "1",
		"command": "db migrate",
		"status":  status,
		"data":    data,
		"meta": map[string]any{
			"ts": now().UTC().Format(time.RFC3339),
		},
		"error": errVal,
	})
}

func newDBMigrateCommand() *cobra.Command {
	var (
		targetDSN       string
		storeNames      string
		batchSize       int
		continueOnError bool
		dropTables      bool
		dryRun          bool
	)

	now := func() time.Time { return time.Now() }

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate data from SQLite to PostgreSQL",
		Long: `Migrate data from local SQLite stores to a PostgreSQL database.

Each store (memory, tasks, sessions) is migrated independently:
  1. Opens the local SQLite database as the source
  2. Opens the PostgreSQL target with per-store schema isolation
  3. Creates tables in PostgreSQL via the store's own migration DDL
  4. Copies rows using INSERT ... ON CONFLICT DO NOTHING for idempotency

Examples:
  # Migrate all stores
  agentctl db migrate --target-dsn "postgres://agentctl:dev@localhost:5432/agentctl?sslmode=disable"

  # Migrate only memory and tasks
  agentctl db migrate --target-dsn "postgres://..." --stores memory,tasks

  # Drop and recreate target tables before migrating
  agentctl db migrate --target-dsn "postgres://..." --drop

  # Preview migration without persisting changes
  agentctl db migrate --target-dsn "postgres://..." --dry-run`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			enc := json.NewEncoder(cmd.OutOrStdout())

			if targetDSN == "" {
				dbEnvelope(enc, now, "error", map[string]any{
					"hint": "provide PostgreSQL connection string, e.g., postgres://user:pass@host:5432/db",
				}, "--target-dsn is required")
				return fmt.Errorf("--target-dsn is required (hint: provide PostgreSQL connection string, e.g., postgres://user:pass@host:5432/db)")
			}

			ctx := cmd.Context()
			cfg, err := loadConfig(ctx)
			if err != nil {
				return fmt.Errorf("load config: %w", err)
			}

			stores := filterStores(storeNames)
			if len(stores) == 0 {
				dbEnvelope(enc, now, "error", map[string]any{
					"hint": "use --stores with comma-separated names; available: memory, tasks, sessions, graph, jobs, cache, agents, coordination, testwatch, contextbuffer, trajectory, dedupe, companion",
				}, "no valid stores specified")
				return fmt.Errorf("no valid stores specified (hint: use --stores with comma-separated names; available: memory, tasks, sessions, graph, jobs, cache, agents, coordination, testwatch, contextbuffer, trajectory, dedupe, companion)")
			}

			totalStats := dbdriver.MigrationStats{}
			for _, spec := range stores {
				dbEnvelope(enc, now, "progress", map[string]any{
					"store":   spec.name,
					"message": "starting",
				}, "")

				stats, err := migrateStore(ctx, cfg, spec, targetDSN, batchSize, continueOnError, dropTables, dryRun, now, cmd.OutOrStdout())
				if err != nil {
					if !continueOnError {
						dbEnvelope(enc, now, "error", map[string]any{
							"store": spec.name,
							"hint":  "use --continue-on-error to skip failing stores",
						}, fmt.Sprintf("store %s: %v", spec.name, err))
						return fmt.Errorf("store %s: %w", spec.name, err)
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "  ERROR: %s: %v\n", spec.name, err)
					totalStats.Errors = append(totalStats.Errors, fmt.Errorf("%s: %w", spec.name, err))
					continue
				}
				totalStats.TablesProcessed += stats.TablesProcessed
				totalStats.RowsMigrated += stats.RowsMigrated
				totalStats.Errors = append(totalStats.Errors, stats.Errors...)

				dbEnvelope(enc, now, "progress", map[string]any{
					"store":            spec.name,
					"tables_processed": stats.TablesProcessed,
					"rows_migrated":    stats.RowsMigrated,
					"dry_run":          dryRun,
				}, "")
			}

			dbEnvelope(enc, now, "complete", map[string]any{
				"tables_processed": totalStats.TablesProcessed,
				"rows_migrated":    totalStats.RowsMigrated,
				"errors":           len(totalStats.Errors),
				"dry_run":          dryRun,
			}, "")
			return nil
		},
	}

	cmd.Flags().StringVar(&targetDSN, "target-dsn", "", "PostgreSQL connection string (required)")
	cmd.Flags().StringVar(&storeNames, "stores", "memory,tasks,sessions,graph,jobs,cache,agents,coordination,testwatch,contextbuffer,trajectory,dedupe,companion", "Comma-separated list of stores to migrate")
	cmd.Flags().IntVar(&batchSize, "batch-size", 100, "Number of rows per INSERT batch")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Continue migrating even if some rows/tables fail")
	cmd.Flags().BoolVar(&dropTables, "drop", false, "Drop and recreate target tables before migration")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview migration without persisting changes")

	return cmd
}

func filterStores(names string) []storeSpec {
	if names == "" {
		return allStores
	}
	wanted := make(map[string]bool)
	for _, n := range strings.Split(names, ",") {
		wanted[strings.TrimSpace(n)] = true
	}
	var result []storeSpec
	for _, s := range allStores {
		if wanted[s.name] {
			result = append(result, s)
		}
	}
	return result
}

func migrateStore(
	ctx context.Context,
	cfg config.Config,
	spec storeSpec,
	targetDSN string,
	batchSize int,
	continueOnError bool,
	dropTables bool,
	dryRun bool,
	now func() time.Time,
	out io.Writer,
) (dbdriver.MigrationStats, error) {
	// Open SQLite source (no migration — read-only)
	sqlitePath := filepath.Join(cfg.Storage.Root, spec.sqliteFile)
	srcDB, srcClose, err := sqliteutil.OpenDBShared(ctx, sqlitePath, nil)
	if err != nil {
		return dbdriver.MigrationStats{}, fmt.Errorf("open sqlite source %s: %w", sqlitePath, err)
	}
	defer func() { _ = srcClose() }()

	// Open PostgreSQL target with store schema isolation and run DDL migrations
	pgCfg := dbdriver.Config{
		Driver: dbdriver.DriverPostgres,
		Postgres: dbdriver.PostgresConfig{
			DSN:                targetDSN,
			Schema:             spec.pgSchema,
			EnableVectorSearch: true,
		},
	}
	// Wrap migrate to create PostgreSQL compatibility shims first.
	pgMigrate := func(ctx context.Context, db *sql.DB) error {
		// BLOB domain: SQLite DDL uses BLOB which doesn't exist in PostgreSQL (it's BYTEA).
		_, err := db.ExecContext(ctx, `DO $$ BEGIN CREATE DOMAIN blob AS bytea; EXCEPTION WHEN duplicate_object THEN NULL; END $$`)
		if err != nil {
			return fmt.Errorf("create blob domain: %w", err)
		}
		// json_extract shim: SQLite's json_extract(doc, '$.key') → PostgreSQL's doc::jsonb ->> 'key'.
		_, err = db.ExecContext(ctx, `
			CREATE OR REPLACE FUNCTION json_extract(doc text, path text)
			RETURNS text
			LANGUAGE sql IMMUTABLE STRICT AS
			$func$
				SELECT doc::jsonb #>> string_to_array(regexp_replace(path, '^\$\.?', ''), '.')
			$func$;
		`)
		if err != nil {
			return fmt.Errorf("create json_extract function: %w", err)
		}
		return spec.migrate(ctx, db)
	}
	targetDB, err := dbdriver.OpenDB(ctx, pgCfg, pgMigrate)
	if err != nil {
		return dbdriver.MigrationStats{}, fmt.Errorf("open postgres target (schema %s): %w", spec.pgSchema, err)
	}
	defer targetDB.Close()

	// Wrap source and target as dbdriver.DB for the Migrator
	srcWrapped := dbdriver.WrapSQLDB(srcDB, dbdriver.DriverSQLite)

	enc := json.NewEncoder(out)
	opts := dbdriver.MigrationOptions{
		Tables:              spec.tables,
		BatchSize:           batchSize,
		SkipSchemaCreation:  true,
		OnConflictDoNothing: true,
		ContinueOnError:     continueOnError,
		DropTargetTables:    dropTables,
		DryRun:              dryRun,
		ProgressCallback: func(stats dbdriver.MigrationStats) {
			dbEnvelope(enc, now, "progress", map[string]any{
				"store":            spec.name,
				"tables_processed": stats.TablesProcessed,
				"rows_migrated":    stats.RowsMigrated,
			}, "")
		},
	}

	migrator := dbdriver.NewMigrator(srcWrapped, targetDB, opts)
	stats, err := migrator.Migrate(ctx)
	if err != nil {
		return stats, err
	}

	// Post-migration: convert embedding columns from bytea to vector(1024) and create HNSW index.
	// The store DDL creates embedding as BLOB (bytea via domain), but pgvector needs vector(N).
	if !dryRun && targetDB.IsVectorSearchEnabled() {
		for _, tbl := range spec.tables {
			if err := convertEmbeddingColumn(ctx, targetDB, tbl, now, out); err != nil {
				if continueOnError {
					stats.Errors = append(stats.Errors, fmt.Errorf("vector setup %s: %w", tbl, err))
				} else {
					return stats, fmt.Errorf("vector setup %s: %w", tbl, err)
				}
			}
		}
	}

	return stats, nil
}

// sqlIdentifierPatternDB matches valid SQL identifiers (alphanumeric + underscore).
var sqlIdentifierPatternDB = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// convertEmbeddingColumn converts an embedding column from bytea (migrated from SQLite BLOB)
// to pgvector's vector(1024) type and creates an HNSW index for cosine search.
// Skips tables that don't have an embedding column.
func convertEmbeddingColumn(ctx context.Context, db dbdriver.DB, tableName string, now func() time.Time, out io.Writer) error {
	// Validate table name to prevent SQL injection in interpolated DDL.
	if !sqlIdentifierPatternDB.MatchString(tableName) || len(tableName) > 64 {
		return fmt.Errorf("invalid table name: %s", tableName)
	}

	enc := json.NewEncoder(out)

	// Check if table has an embedding column (scope to current schema to avoid
	// matching identically-named tables in other schemas).
	var hasColumn bool
	err := db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name='embedding')",
		tableName,
	).Scan(&hasColumn)
	if err != nil {
		return fmt.Errorf("check embedding column for %s: %w", tableName, err)
	}
	if !hasColumn {
		return nil // no embedding column, skip
	}

	// Check if already vector type
	var udtName string
	err = db.QueryRowContext(ctx,
		"SELECT udt_name FROM information_schema.columns WHERE table_schema=current_schema() AND table_name=$1 AND column_name='embedding'",
		tableName,
	).Scan(&udtName)
	if err != nil {
		return fmt.Errorf("check embedding udt_name for %s: %w", tableName, err)
	}
	if udtName == "vector" {
		dbEnvelope(enc, now, "progress", map[string]any{
			"store":   tableName,
			"message": fmt.Sprintf("%s.embedding: already vector type", tableName),
		}, "")
		return nil
	}

	dbEnvelope(enc, now, "progress", map[string]any{
		"store":   tableName,
		"message": fmt.Sprintf("%s.embedding: converting bytea -> vector(1024)...", tableName),
	}, "")

	// Run all DDL steps in a single transaction for atomicity.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Create helper function to decode little-endian binary float32 bytea to vector string.
	// SQLite stores embeddings as raw binary (4 bytes per float32, little-endian).
	_, err = tx.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION _bytea_to_vector(b bytea)
		RETURNS text
		LANGUAGE plpgsql IMMUTABLE STRICT AS $$
		DECLARE
			ndims int := octet_length(b) / 4;
			parts text[] := '{}';
			i int;
			raw int;
			sign int; exp int; mantissa int;
			val float8;
		BEGIN
			FOR i IN 0 .. ndims - 1 LOOP
				-- Read 4 bytes little-endian as a 32-bit integer.
				raw := get_byte(b, i*4)
					 + get_byte(b, i*4+1) * 256
					 + get_byte(b, i*4+2) * 65536
					 + get_byte(b, i*4+3) * 16777216;
				-- IEEE 754 single-precision decode.
				sign := (raw >> 31) & 1;
				exp  := (raw >> 23) & 255;
				mantissa := raw & 8388607;  -- 0x7FFFFF
				IF exp = 0 AND mantissa = 0 THEN
					val := 0.0;
				ELSIF exp = 0 THEN
					-- Denormalized: no implicit leading 1
					val := power(2.0, -126) * (mantissa::float8 / 8388608.0);
				ELSIF exp = 255 AND mantissa = 0 THEN
					val := 'Infinity'::float8;
				ELSIF exp = 255 THEN
					val := 'NaN'::float8;
				ELSE
					val := power(2.0, exp - 127) * (1.0 + mantissa::float8 / 8388608.0);
				END IF;
				IF sign = 1 THEN val := -val; END IF;
				parts := array_append(parts, val::text);
			END LOOP;
			RETURN '[' || array_to_string(parts, ',') || ']';
		END;
		$$;
	`)
	if err != nil {
		return fmt.Errorf("create _bytea_to_vector helper: %w", err)
	}

	// Convert: rename old column, add vector column, copy data, drop old column
	steps := []string{
		"ALTER TABLE " + tableName + " RENAME COLUMN embedding TO embedding_bytea",
		"ALTER TABLE " + tableName + " ADD COLUMN embedding vector(1024)",
		"UPDATE " + tableName + " SET embedding = _bytea_to_vector(embedding_bytea)::vector WHERE embedding_bytea IS NOT NULL",
		"ALTER TABLE " + tableName + " DROP COLUMN embedding_bytea",
	}
	for _, stmt := range steps {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("step %q: %w", stmt, err)
		}
	}

	// Create HNSW index for cosine search
	idxName := "idx_" + tableName + "_embedding_hnsw"
	idxSQL := fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64)",
		idxName, tableName,
	)
	if _, err := tx.ExecContext(ctx, idxSQL); err != nil {
		return fmt.Errorf("create HNSW index: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	dbEnvelope(enc, now, "progress", map[string]any{
		"store":   tableName,
		"message": fmt.Sprintf("%s.embedding: converted + HNSW index created", tableName),
	}, "")
	return nil
}

func init() {
	rootCmd.AddCommand(newDBCommand())
}
