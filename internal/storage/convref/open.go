package convref

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // register pgx for database/sql
	"github.com/joshka0/foxctl/internal/storage/sqliteutil"
)

// OpenSQLite opens a SQLite-backed conversation ref store at the given path and runs migrations.
func OpenSQLite(ctx context.Context, path string) (Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("convref: sqlite path is required")
	}

	db, err := sqliteutil.OpenDB(ctx, filepath.Clean(path), func(ctx context.Context, db *sql.DB) error {
		return NewSQLiteStore(db, nil).Migrate(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("convref: open sqlite: %w", err)
	}

	return &closeableStore{
		Store:   NewSQLiteStore(db, nil),
		closeFn: db.Close,
	}, nil
}

// OpenPostgres opens a PostgreSQL-backed conversation ref store and runs migrations.
func OpenPostgres(ctx context.Context, dsn string) (Store, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("convref: postgres dsn is required")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("convref: open postgres: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("convref: ping postgres: %w", err)
	}

	store := NewPostgresStore(db, nil)
	if err := store.Migrate(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("convref: migrate postgres: %w", err)
	}

	return &closeableStore{
		Store:   store,
		closeFn: db.Close,
	}, nil
}

type closeableStore struct {
	Store
	closeFn func() error
}

func (s *closeableStore) Close() error {
	if s == nil {
		return nil
	}
	if err := s.Store.Close(); err != nil {
		return err
	}
	if s.closeFn != nil {
		return s.closeFn()
	}
	return nil
}
