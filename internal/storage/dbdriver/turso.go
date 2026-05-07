package dbdriver

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	turso "turso.tech/database/tursogo"
)

// tursoDB wraps the Rust-backed Turso driver to implement our DB interface.
type tursoDB struct {
	db                 *sql.DB
	syncDB             *turso.TursoSyncDb
	enableVectorSearch bool
	vectorDimensions   int
	driverType         DriverType
	syncURL            string
	syncStop           chan struct{}
	syncDone           chan struct{}
}

// openTurso opens a local Turso database or a local replica synced to a remote Turso database.
func openTurso(ctx context.Context, cfg TursoConfig, migrate MigrationFunc) (DB, error) {
	vectorDims := cfg.VectorDimensions
	if vectorDims == 0 {
		vectorDims = 384
	}

	dbPath := firstNonEmpty(cfg.Path, cfg.ReplicaPath)
	if dbPath == "" {
		return nil, fmt.Errorf("turso path is required")
	}
	if dbPath != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, fmt.Errorf("failed to create turso database directory: %w", err)
		}
	}

	store := &tursoDB{
		enableVectorSearch: cfg.EnableVectorSearch,
		vectorDimensions:   vectorDims,
		driverType:         DriverTurso,
		syncURL:            cfg.URL,
	}

	var err error
	if cfg.URL != "" {
		store.syncDB, err = turso.NewTursoSyncDb(ctx, turso.TursoSyncDbConfig{
			Path:      dbPath,
			RemoteUrl: cfg.URL,
			AuthToken: cfg.AuthToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create turso sync database: %w", err)
		}
		store.db, err = store.syncDB.Connect(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to turso sync database: %w", err)
		}
	} else {
		store.db, err = sql.Open("turso", dbPath)
		if err != nil {
			return nil, fmt.Errorf("failed to open turso database: %w", err)
		}
	}

	cleanup := func() {
		_ = store.Close()
	}

	if err := store.db.PingContext(ctx); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to connect to turso database: %w", err)
	}

	if migrate != nil {
		if err := migrate(ctx, store.db); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
	}

	if cfg.EnableVectorSearch {
		if err := ensureVectorSupport(ctx, store.db, vectorDims); err != nil {
			cleanup()
			return nil, fmt.Errorf("failed to enable vector search: %w", err)
		}
	}

	if cfg.SyncInterval > 0 && store.syncDB != nil {
		interval := time.Duration(cfg.SyncInterval) * time.Second
		store.syncStop = make(chan struct{})
		store.syncDone = make(chan struct{})
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			defer close(store.syncDone)

			for {
				select {
				case <-store.syncStop:
					return
				case <-ticker.C:
					_ = store.Sync()
				}
			}
		}()
	}

	return store, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// ensureVectorSupport verifies that vector search is available.
func ensureVectorSupport(ctx context.Context, db *sql.DB, dimensions int) error {
	_ = dimensions
	testQuery := `
		CREATE TEMP TABLE IF NOT EXISTS _vector_test (
			id INTEGER PRIMARY KEY,
			embedding BLOB
		)
	`

	if _, err := db.ExecContext(ctx, testQuery); err != nil {
		return fmt.Errorf("vector search not available in turso: %w", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO _vector_test (id, embedding)
		VALUES (1, vector32('[0.1,0.2,0.3,0.4]'))
	`); err != nil {
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS _vector_test") //nolint:errcheck
		return fmt.Errorf("vector search not available in turso: %w", err)
	}

	var distance float64
	if err := db.QueryRowContext(ctx, `
		SELECT vector_distance_cos(embedding, vector32('[0.1,0.2,0.3,0.4]'))
		FROM _vector_test
		WHERE id = 1
	`).Scan(&distance); err != nil {
		_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS _vector_test") //nolint:errcheck
		return fmt.Errorf("vector search not available in turso: %w", err)
	}

	_, _ = db.ExecContext(ctx, "DROP TABLE IF EXISTS _vector_test") //nolint:errcheck
	return nil
}

func (t *tursoDB) Close() error {
	var err error

	if t.syncStop != nil {
		close(t.syncStop)
		<-t.syncDone
		t.syncStop = nil
		t.syncDone = nil
	}

	if t.db != nil {
		err = t.db.Close()
	}
	return err
}

func (t *tursoDB) Exec(query string, args ...any) (sql.Result, error) {
	return t.db.Exec(query, args...)
}

func (t *tursoDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.db.ExecContext(ctx, query, args...)
}

func (t *tursoDB) Query(query string, args ...any) (*sql.Rows, error) {
	return t.db.Query(query, args...)
}

func (t *tursoDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.db.QueryContext(ctx, query, args...)
}

func (t *tursoDB) QueryRow(query string, args ...any) *sql.Row {
	return t.db.QueryRow(query, args...)
}

func (t *tursoDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.db.QueryRowContext(ctx, query, args...)
}

func (t *tursoDB) Begin() (*sql.Tx, error) {
	return t.db.Begin()
}

func (t *tursoDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	return t.db.BeginTx(ctx, opts)
}

func (t *tursoDB) Ping() error {
	return t.db.Ping()
}

func (t *tursoDB) PingContext(ctx context.Context) error {
	return t.db.PingContext(ctx)
}

func (t *tursoDB) SetMaxOpenConns(n int) {
	t.db.SetMaxOpenConns(n)
}

func (t *tursoDB) SetMaxIdleConns(n int) {
	t.db.SetMaxIdleConns(n)
}

func (t *tursoDB) SetConnMaxLifetime(d any) {
	if dur, ok := parseConnDuration(d); ok {
		t.db.SetConnMaxLifetime(dur)
	}
}

func (t *tursoDB) SetConnMaxIdleTime(d any) {
	if dur, ok := parseConnDuration(d); ok {
		t.db.SetConnMaxIdleTime(dur)
	}
}

func (t *tursoDB) Stats() sql.DBStats {
	return t.db.Stats()
}

func (t *tursoDB) GetUnderlyingDB() (*sql.DB, bool) {
	return t.db, true
}

func (t *tursoDB) IsVectorSearchEnabled() bool {
	return t.enableVectorSearch
}

func (t *tursoDB) GetDriverType() DriverType {
	return t.driverType
}

func (t *tursoDB) GetDialect() Dialect {
	return SQLiteDialect{}
}

func (t *tursoDB) GetVectorDimensions() int {
	return t.vectorDimensions
}

func (t *tursoDB) Sync() error {
	if t.syncDB == nil {
		return nil
	}
	if err := t.syncDB.Push(context.Background()); err != nil {
		return fmt.Errorf("turso push failed: %w", err)
	}
	if _, err := t.syncDB.Pull(context.Background()); err != nil {
		return fmt.Errorf("turso pull failed: %w", err)
	}
	return nil
}

func (t *tursoDB) IsSyncEnabled() bool {
	return t.syncDB != nil
}

func (t *tursoDB) GetSyncURL() string {
	return t.syncURL
}
