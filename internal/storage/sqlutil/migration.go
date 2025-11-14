package sqlutil

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// Migration represents a discrete schema change identified by a monotonically
// increasing version number.
type Migration struct {
	Version int
	Name    string
	Up      string
	Down    string
}

// Migrator coordinates applying schema migrations to a database.
type Migrator struct {
	db         *sql.DB
	migrations []Migration
}

// NewMigrator constructs a Migrator for db.
func NewMigrator(db *sql.DB) *Migrator {
	return &Migrator{db: db}
}

// Add registers a migration to be applied when Migrate is called.
func (m *Migrator) Add(version int, name, up string) *Migrator {
	m.migrations = append(m.migrations, Migration{Version: version, Name: name, Up: up})
	return m
}

// Migrate applies any pending migrations in ascending version order.
func (m *Migrator) Migrate(ctx context.Context) error {
	if m == nil || m.db == nil {
		return fmt.Errorf("sqlutil: migrator not configured")
	}

	if err := m.ensureTable(ctx); err != nil {
		return err
	}

	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return err
	}

	sort.Slice(m.migrations, func(i, j int) bool {
		return m.migrations[i].Version < m.migrations[j].Version
	})

	seen := make(map[int]struct{}, len(m.migrations))
	for _, migration := range m.migrations {
		if migration.Version <= 0 {
			return fmt.Errorf("sqlutil: migration %q must have positive version", migration.Name)
		}
		if migration.Up == "" {
			return fmt.Errorf("sqlutil: migration %q has empty up statement", migration.Name)
		}
		if _, dup := seen[migration.Version]; dup {
			return fmt.Errorf("sqlutil: duplicate migration version %d", migration.Version)
		}
		seen[migration.Version] = struct{}{}

		if _, already := applied[migration.Version]; already {
			continue
		}

		if err := m.applyMigration(ctx, migration); err != nil {
			return err
		}
	}

	return nil
}

func (m *Migrator) ensureTable(ctx context.Context) error {
	ddl := `
CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        name TEXT NOT NULL,
        applied_at TEXT NOT NULL
);
`
	if _, err := m.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("sqlutil: ensure schema_migrations: %w", err)
	}
	return nil
}

func (m *Migrator) appliedVersions(ctx context.Context) (applied map[int]struct{}, err error) {
	rows, err := m.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("sqlutil: select migrations: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("sqlutil: close migration rows: %w", closeErr)
		}
	}()

	applied = make(map[int]struct{})
	for rows.Next() {
		var version int
		if scanErr := rows.Scan(&version); scanErr != nil {
			return nil, fmt.Errorf("sqlutil: scan migration version: %w", scanErr)
		}
		applied[version] = struct{}{}
	}
	if iterErr := rows.Err(); iterErr != nil {
		return nil, fmt.Errorf("sqlutil: iterate migration versions: %w", iterErr)
	}
	return applied, nil
}

func (m *Migrator) applyMigration(ctx context.Context, migration Migration) error {
	return WithTransaction(ctx, m.db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, migration.Up); err != nil {
			return fmt.Errorf("sqlutil: apply migration %d %q: %w", migration.Version, migration.Name, err)
		}
		_, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO schema_migrations(version, name, applied_at) VALUES(?, ?, ?)`,
			migration.Version,
			migration.Name,
			FormatTimestamp(time.Now().UTC()),
		)
		if err != nil {
			return fmt.Errorf("sqlutil: record migration %d %q: %w", migration.Version, migration.Name, err)
		}
		return nil
	})
}
