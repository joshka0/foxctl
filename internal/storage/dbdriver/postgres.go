package dbdriver

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // Register pgx as "pgx" driver for database/sql
)

// defaultDBTimeout is the timeout used by non-context DB methods on postgresDB
// where the operation completes within the function call (Exec, Ping).
const defaultDBTimeout = 30 * time.Second

// postgresDB wraps *sql.DB for PostgreSQL with per-store schema isolation.
type postgresDB struct {
	db            *sql.DB
	schema        string
	vectorEnabled bool
	vectorDims    int
}

// openPostgres opens a PostgreSQL database connection with per-store schema isolation.
// It creates the schema if it doesn't exist, sets the search_path via the DSN
// (so every pooled connection inherits it), runs migrations guarded by an advisory lock,
// and configures the connection pool.
func openPostgres(ctx context.Context, cfg PostgresConfig, migrate MigrationFunc) (DB, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("postgres dsn is required")
	}

	schema := cfg.Schema
	if schema == "" {
		schema = "public"
	}
	// Sanitize schema name: only allow alphanumeric + underscore
	schema, err := sanitizeIdentifier(schema)
	if err != nil {
		return nil, fmt.Errorf("postgres: invalid schema name %q: %w", cfg.Schema, err)
	}

	// Append search_path to the DSN so every pooled connection uses the correct schema.
	dsn, err := appendSearchPath(cfg.DSN, schema)
	if err != nil {
		return nil, fmt.Errorf("postgres: configure search_path in DSN: %w", err)
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: open: %w", err)
	}

	// Configure connection pool
	maxOpen := cfg.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 5
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 2
	}
	connLifetime := time.Duration(cfg.ConnMaxLifetimeSeconds) * time.Second
	if connLifetime <= 0 {
		connLifetime = time.Hour
	}
	connIdleTime := time.Duration(cfg.ConnMaxIdleTimeSeconds) * time.Second
	if connIdleTime <= 0 {
		connIdleTime = 30 * time.Minute
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(connLifetime)
	db.SetConnMaxIdleTime(connIdleTime)

	// Verify connectivity
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}

	// Create schema for store isolation if it doesn't exist
	if schema != "public" {
		if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema)); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("postgres: create schema %q: %w", schema, err)
		}
	}

	// Create PostgreSQL compatibility shims for SQLite DDL reuse.
	// BLOB domain: SQLite uses BLOB (PostgreSQL uses BYTEA).
	_, _ = db.ExecContext(ctx, `DO $$ BEGIN CREATE DOMAIN blob AS bytea; EXCEPTION WHEN duplicate_object THEN NULL; END $$`)
	// json_extract function: SQLite's json_extract(doc, '$.key') compatibility.
	_, _ = db.ExecContext(ctx, `
		CREATE OR REPLACE FUNCTION json_extract(doc text, path text)
		RETURNS text
		LANGUAGE sql IMMUTABLE STRICT AS
		$func$
			SELECT doc::jsonb #>> string_to_array(regexp_replace(path, '^\$\.?', ''), '.')
		$func$;
	`)

	// Run migrations guarded by an advisory lock to prevent races in multi-pod deployments.
	if migrate != nil {
		if err := runMigrationsWithAdvisoryLock(ctx, db, schema, migrate); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("postgres: migrations: %w", err)
		}
	}

	// Detect pgvector availability
	vectorEnabled, vectorErr := detectPgvector(ctx, db, cfg.EnableVectorSearch)
	if vectorErr != nil && cfg.RequireVector {
		_ = db.Close()
		return nil, fmt.Errorf("postgres: pgvector required but not available: %w", vectorErr)
	}

	dims := cfg.VectorDimensions
	if dims <= 0 {
		dims = GetDefaultVectorDimensions()
	}

	return &postgresDB{
		db:            db,
		schema:        schema,
		vectorEnabled: vectorEnabled,
		vectorDims:    dims,
	}, nil
}

// appendSearchPath adds search_path as a query parameter to a PostgreSQL DSN
// so every connection in the pool inherits the correct schema.
// Only URI-format DSNs (postgres://...) are supported; keyword-style DSNs
// (host=... dbname=...) will return an error.
func appendSearchPath(dsn, schema string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", fmt.Errorf("parse DSN: %w", err)
	}
	// Reject keyword-style DSNs (e.g. "host=localhost dbname=mydb")
	if u.Scheme != "postgres" && u.Scheme != "postgresql" {
		return "", fmt.Errorf("DSN must use URI format (postgres://...); got scheme %q", u.Scheme)
	}
	q := u.Query()
	q.Set("search_path", schema+",public")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// runMigrationsWithAdvisoryLock acquires a schema-scoped advisory lock before running
// migrations, preventing concurrent migration races in multi-pod K8s deployments.
// Uses pg_try_advisory_lock with a retry loop to respect context cancellation.
func runMigrationsWithAdvisoryLock(ctx context.Context, db *sql.DB, schema string, migrate MigrationFunc) error {
	// Use a hash of the schema name as the advisory lock key.
	lockKey := int64(hashString(schema))

	// Retry loop with pg_try_advisory_lock to respect context cancellation.
	const retryInterval = 200 * time.Millisecond
	for {
		var acquired bool
		err := db.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", lockKey).Scan(&acquired)
		if err != nil {
			return fmt.Errorf("try advisory lock for schema %q: %w", schema, err)
		}
		if acquired {
			break
		}
		// Lock held by another pod — wait and retry, respecting context.
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled waiting for advisory lock on schema %q: %w", schema, ctx.Err())
		case <-time.After(retryInterval):
		}
	}

	// Ensure we release the lock even if migration fails
	defer func() {
		_, _ = db.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey)
	}()

	return migrate(ctx, db)
}

// detectPgvector checks if the pgvector extension is available.
// Returns (true, nil) if the extension is installed or was created successfully,
// (false, nil) if enableVector is false, or (false, error) if installation failed.
func detectPgvector(ctx context.Context, db *sql.DB, enableVector bool) (bool, error) {
	if !enableVector {
		return false, nil
	}
	// Try to create the extension (requires privileges)
	_, err := db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector")
	if err != nil {
		return false, fmt.Errorf("create pgvector extension: %w", err)
	}
	return true, nil
}

// sanitizeIdentifier validates that a string is safe to use as a SQL identifier.
// Only allows alphanumeric and underscore. Returns the original string unchanged
// if valid, or an error if any character is outside the allowed set.
func sanitizeIdentifier(s string) (string, error) {
	if s == "" {
		return "", fmt.Errorf("identifier must not be empty")
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return "", fmt.Errorf("identifier %q contains invalid character %q at position %d (only alphanumeric and underscore allowed)", s, string(c), i)
		}
	}
	return s, nil
}

// hashString returns a simple hash of a string for use as an advisory lock key.
func hashString(s string) uint32 {
	// FNV-1a 32-bit
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// DB interface implementation for postgresDB

func (p *postgresDB) Close() error { return p.db.Close() }

// Exec uses a bounded timeout since the operation completes within this call.
func (p *postgresDB) Exec(query string, args ...any) (sql.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)
	defer cancel()
	return p.db.ExecContext(ctx, query, args...)
}

func (p *postgresDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return p.db.ExecContext(ctx, query, args...)
}

// Query delegates to the underlying db without a cancel-on-return context,
// because the returned *sql.Rows must remain valid for the caller to iterate.
func (p *postgresDB) Query(query string, args ...any) (*sql.Rows, error) {
	return p.db.QueryContext(context.Background(), query, args...)
}

func (p *postgresDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return p.db.QueryContext(ctx, query, args...)
}

// QueryRow delegates to the underlying db without a cancel-on-return context,
// because the returned *sql.Row must remain valid for the caller to call Scan.
func (p *postgresDB) QueryRow(query string, args ...any) *sql.Row {
	return p.db.QueryRowContext(context.Background(), query, args...)
}

func (p *postgresDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return p.db.QueryRowContext(ctx, query, args...)
}

// Begin delegates without a cancel-on-return context because the returned
// *sql.Tx must remain valid for the caller to commit/rollback.
func (p *postgresDB) Begin() (*sql.Tx, error) {
	return p.db.BeginTx(context.Background(), nil)
}

func (p *postgresDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return p.db.BeginTx(ctx, opts)
}

// Ping uses a bounded timeout since the operation completes within this call.
func (p *postgresDB) Ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), defaultDBTimeout)
	defer cancel()
	return p.db.PingContext(ctx)
}
func (p *postgresDB) PingContext(ctx context.Context) error { return p.db.PingContext(ctx) }
func (p *postgresDB) SetMaxOpenConns(n int)                 { p.db.SetMaxOpenConns(n) }
func (p *postgresDB) SetMaxIdleConns(n int)                 { p.db.SetMaxIdleConns(n) }
func (p *postgresDB) SetConnMaxLifetime(d any) {
	if dur, ok := parseConnDuration(d); ok {
		p.db.SetConnMaxLifetime(dur)
	}
}

func (p *postgresDB) SetConnMaxIdleTime(d any) {
	if dur, ok := parseConnDuration(d); ok {
		p.db.SetConnMaxIdleTime(dur)
	}
}
func (p *postgresDB) Stats() sql.DBStats               { return p.db.Stats() }
func (p *postgresDB) GetUnderlyingDB() (*sql.DB, bool) { return p.db, true }
func (p *postgresDB) IsVectorSearchEnabled() bool      { return p.vectorEnabled }
func (p *postgresDB) GetVectorDimensions() int         { return p.vectorDims }
func (p *postgresDB) GetDriverType() DriverType        { return DriverPostgres }
func (p *postgresDB) GetDialect() Dialect              { return PostgresDialect{} }
