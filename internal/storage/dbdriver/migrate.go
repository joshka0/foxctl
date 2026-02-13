package dbdriver

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// MigrationStats holds statistics about a migration operation
type MigrationStats struct {
	TablesProcessed int
	RowsMigrated    int64
	Errors          []error
}

// MigrationOptions configures a database migration
type MigrationOptions struct {
	// Tables to migrate (if empty, all tables are migrated)
	Tables []string

	// BatchSize for bulk inserts (default: 100)
	BatchSize int

	// DropTargetTables drops existing tables in target before migration
	DropTargetTables bool

	// ContinueOnError continues migration even if some rows fail
	ContinueOnError bool

	// SkipSchemaCreation skips CREATE TABLE from source (target already has schema).
	// Use when migrating to PostgreSQL where the target's own migrate() creates tables.
	SkipSchemaCreation bool

	// OnConflictDoNothing appends ON CONFLICT DO NOTHING to INSERT statements,
	// making re-running migration idempotent. Works on both PostgreSQL and SQLite 3.24+.
	OnConflictDoNothing bool

	// DryRun performs the migration inside a transaction that is rolled back,
	// collecting MigrationStats without persisting any changes.
	DryRun bool

	// ProgressCallback is called after each batch with current stats
	ProgressCallback func(stats MigrationStats)
}

// DefaultMigrationOptions returns default migration options
func DefaultMigrationOptions() MigrationOptions {
	return MigrationOptions{
		Tables:           nil,
		BatchSize:        100,
		DropTargetTables: false,
		ContinueOnError:  false,
	}
}

// sqlIdentifierPattern matches valid SQL identifiers (letters, numbers, underscores, no leading numbers)
var sqlIdentifierPatternMigrate = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// validateSQLIdentifier checks if a string is a safe SQL identifier
// to prevent SQL injection through table/column names
func validateSQLIdentifierMigrate(identifier string) error {
	if identifier == "" {
		return fmt.Errorf("SQL identifier cannot be empty")
	}
	if len(identifier) > 64 {
		return fmt.Errorf("SQL identifier too long: %s", identifier)
	}
	if !sqlIdentifierPatternMigrate.MatchString(identifier) {
		return fmt.Errorf("invalid SQL identifier: %s (must contain only letters, numbers, and underscores)", identifier)
	}
	return nil
}

// Migrator handles database migration from one backend to another
type Migrator struct {
	source  DB
	target  DB
	options MigrationOptions
}

// NewMigrator creates a new database migrator
func NewMigrator(source, target DB, options MigrationOptions) *Migrator {
	if options.BatchSize <= 0 {
		options.BatchSize = 100
	}
	return &Migrator{
		source:  source,
		target:  target,
		options: options,
	}
}

// Migrate migrates data from source to target database.
// When DryRun is true, the entire migration runs inside a transaction
// that is rolled back, so no data is persisted in the target.
func (m *Migrator) Migrate(ctx context.Context) (MigrationStats, error) {
	stats := MigrationStats{}

	// Get list of tables to migrate
	tables, err := m.getTablesToMigrate(ctx)
	if err != nil {
		return stats, fmt.Errorf("failed to get tables: %w", err)
	}

	// For dry-run mode, wrap all writes in a transaction that we roll back.
	var dryRunTx *sql.Tx
	if m.options.DryRun {
		underlyingDB, ok := m.target.GetUnderlyingDB()
		if !ok {
			return stats, fmt.Errorf("dry-run requires underlying *sql.DB access")
		}
		dryRunTx, err = underlyingDB.BeginTx(ctx, nil)
		if err != nil {
			return stats, fmt.Errorf("dry-run: begin transaction: %w", err)
		}
		defer dryRunTx.Rollback() //nolint:errcheck
		// Replace the target with a tx-wrapped DB so migrateTable writes into the tx.
		origTarget := m.target
		m.target = &txDB{tx: dryRunTx, driverType: origTarget.GetDriverType(), dialect: origTarget.GetDialect(), vectorEnabled: origTarget.IsVectorSearchEnabled()}
		defer func() { m.target = origTarget }()
	}

	// Migrate each table
	for _, table := range tables {
		if err := m.migrateTable(ctx, table, &stats); err != nil {
			if !m.options.ContinueOnError {
				return stats, fmt.Errorf("failed to migrate table %s: %w", table, err)
			}
			stats.Errors = append(stats.Errors, fmt.Errorf("table %s: %w", table, err))
		}
		stats.TablesProcessed++

		if m.options.ProgressCallback != nil {
			m.options.ProgressCallback(stats)
		}
	}

	// In dry-run mode the deferred Rollback undoes everything.
	return stats, nil
}

// getTablesToMigrate returns the list of tables to migrate
func (m *Migrator) getTablesToMigrate(ctx context.Context) ([]string, error) {
	// If specific tables are specified, use those
	if len(m.options.Tables) > 0 {
		return m.options.Tables, nil
	}

	// Otherwise, get all tables from source
	query := `
		SELECT name FROM sqlite_master
		WHERE type='table'
		AND name NOT LIKE 'sqlite_%'
		AND name NOT LIKE '_%'
		ORDER BY name
	`

	rows, err := m.source.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Log error but don't fail the operation
			_ = err // Connection leak prevented
		}
	}()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		// Skip internal tables
		if table != "schema_migrations" {
			tables = append(tables, table)
		}
	}

	return tables, rows.Err()
}

// migrateTable migrates a single table from source to target
func (m *Migrator) migrateTable(ctx context.Context, tableName string, stats *MigrationStats) error {
	// Drop target table if requested (works regardless of SkipSchemaCreation).
	if m.options.DropTargetTables {
		if _, err := m.target.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)); err != nil {
			return fmt.Errorf("failed to drop target table: %w", err)
		}
	}

	if !m.options.SkipSchemaCreation {
		// Get table schema from source (SQLite)
		schema, err := m.getTableSchema(ctx, tableName)
		if err != nil {
			return fmt.Errorf("failed to get schema: %w", err)
		}

		// Create table with schema
		if _, err := m.target.ExecContext(ctx, schema); err != nil {
			// Ignore error if table already exists
			if !strings.Contains(err.Error(), "already exists") {
				return fmt.Errorf("failed to create target table: %w", err)
			}
		}
	}

	// Get column names from source
	srcColumns, err := m.getTableColumns(ctx, tableName)
	if err != nil {
		return fmt.Errorf("failed to get source columns: %w", err)
	}

	// When target schema is managed externally, intersect with target columns
	// to handle column drift (e.g. source has old columns removed from codebase).
	columns := srcColumns
	if m.options.SkipSchemaCreation {
		tgtColumns, err := m.getTargetTableColumns(ctx, tableName)
		if err != nil {
			return fmt.Errorf("failed to get target columns: %w", err)
		}
		columns = intersectColumns(srcColumns, tgtColumns)
	}

	// Migrate data in batches
	if err := m.migrateTableData(ctx, tableName, columns, stats); err != nil {
		return fmt.Errorf("failed to migrate data: %w", err)
	}

	return nil
}

// getTableSchema gets the CREATE TABLE statement for a table
func (m *Migrator) getTableSchema(ctx context.Context, tableName string) (string, error) {
	var schema string
	query := "SELECT sql FROM sqlite_master WHERE type='table' AND name=?"
	err := m.source.QueryRowContext(ctx, query, tableName).Scan(&schema)
	if err != nil {
		return "", err
	}
	return schema, nil
}

// getTableColumns gets the column names for a table
func (m *Migrator) getTableColumns(ctx context.Context, tableName string) ([]string, error) {
	// Validate SQL identifier to prevent injection
	if err := validateSQLIdentifierMigrate(tableName); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}

	rows, err := m.source.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s LIMIT 0", tableName))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Log error but don't fail the operation
			_ = err // Connection leak prevented
		}
	}()

	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	return columns, nil
}

// getTargetTableColumns gets the column names for a table from the target database.
func (m *Migrator) getTargetTableColumns(ctx context.Context, tableName string) ([]string, error) {
	if err := validateSQLIdentifierMigrate(tableName); err != nil {
		return nil, fmt.Errorf("invalid table name: %w", err)
	}

	rows, err := m.target.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s LIMIT 0", tableName))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			_ = err
		}
	}()

	return rows.Columns()
}

// intersectColumns returns columns present in both src and tgt, preserving src order.
func intersectColumns(src, tgt []string) []string {
	set := make(map[string]bool, len(tgt))
	for _, c := range tgt {
		set[c] = true
	}
	var result []string
	for _, c := range src {
		if set[c] {
			result = append(result, c)
		}
	}
	return result
}

// migrateTableData migrates the data from a table
func (m *Migrator) migrateTableData(ctx context.Context, tableName string, columns []string, stats *MigrationStats) error {
	// Validate SQL identifiers to prevent injection
	if err := validateSQLIdentifierMigrate(tableName); err != nil {
		return fmt.Errorf("invalid table name: %w", err)
	}
	for _, col := range columns {
		if err := validateSQLIdentifierMigrate(col); err != nil {
			return fmt.Errorf("invalid column name: %w", err)
		}
	}

	// Query all data from source
	query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), tableName)
	rows, err := m.source.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer func() {
		if err := rows.Close(); err != nil {
			// Log error but don't fail the operation
			_ = err // Connection leak prevented
		}
	}()

	// Prepare insert statement with $N positional placeholders (works for both SQLite and PostgreSQL)
	placeholders := make([]string, len(columns))
	for i := range placeholders {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	insertQuery := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		tableName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)
	if m.options.OnConflictDoNothing {
		insertQuery += " ON CONFLICT DO NOTHING"
	}

	// Migrate in batches
	batch := make([][]any, 0, m.options.BatchSize)
	totalRows := int64(0)

	for rows.Next() {
		// Create slice to hold column values
		values := make([]any, len(columns))
		valuePtrs := make([]any, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			if !m.options.ContinueOnError {
				return err
			}
			stats.Errors = append(stats.Errors, fmt.Errorf("scan error: %w", err))
			continue
		}

		batch = append(batch, values)

		// Insert batch when it reaches batch size
		if len(batch) >= m.options.BatchSize {
			if err := m.insertBatch(ctx, insertQuery, batch); err != nil {
				if !m.options.ContinueOnError {
					return err
				}
				stats.Errors = append(stats.Errors, fmt.Errorf("insert error: %w", err))
			} else {
				totalRows += int64(len(batch))
			}
			batch = batch[:0]
		}
	}

	// Insert remaining rows
	if len(batch) > 0 {
		if err := m.insertBatch(ctx, insertQuery, batch); err != nil {
			if !m.options.ContinueOnError {
				return err
			}
			stats.Errors = append(stats.Errors, fmt.Errorf("insert error: %w", err))
		} else {
			totalRows += int64(len(batch))
		}
	}

	stats.RowsMigrated += totalRows
	return rows.Err()
}

// insertBatch inserts a batch of rows
func (m *Migrator) insertBatch(ctx context.Context, insertQuery string, batch [][]any) error {
	tx, err := m.target.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, insertQuery)
	if err != nil {
		return err
	}
	defer func() {
		if err := stmt.Close(); err != nil {
			// Log error but don't fail the operation
			_ = err // Resource leak prevented
		}
	}()

	for _, values := range batch {
		if _, err := stmt.ExecContext(ctx, values...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// ExportToSQL exports the source database to SQL statements
// This can be used to manually inspect or import data
func (m *Migrator) ExportToSQL(ctx context.Context, writer io.Writer) error {
	tables, err := m.getTablesToMigrate(ctx)
	if err != nil {
		return err
	}

	for _, table := range tables {
		// Write CREATE TABLE statement
		schema, err := m.getTableSchema(ctx, table)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(writer, "%s;\n\n", schema); err != nil {
			return err
		}

		// Write INSERT statements
		columns, err := m.getTableColumns(ctx, table)
		if err != nil {
			return err
		}

		// Validate table and column identifiers (defensive, already validated by getTableColumns)
		if err := validateSQLIdentifierMigrate(table); err != nil {
			return fmt.Errorf("invalid table name: %w", err)
		}
		for _, col := range columns {
			if err := validateSQLIdentifierMigrate(col); err != nil {
				return fmt.Errorf("invalid column name: %w", err)
			}
		}

		query := fmt.Sprintf("SELECT %s FROM %s", strings.Join(columns, ", "), table)
		rows, err := m.source.QueryContext(ctx, query)
		if err != nil {
			return err
		}

		for rows.Next() {
			values := make([]any, len(columns))
			valuePtrs := make([]any, len(columns))
			for i := range values {
				valuePtrs[i] = &values[i]
			}

			if err := rows.Scan(valuePtrs...); err != nil {
				_ = rows.Close() //nolint:errcheck
				return err
			}

			// Format values
			valueStrs := make([]string, len(values))
			for i, v := range values {
				switch val := v.(type) {
				case nil:
					valueStrs[i] = "NULL"
				case string:
					valueStrs[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(val, "'", "''"))
				case []byte:
					valueStrs[i] = fmt.Sprintf("'%s'", strings.ReplaceAll(string(val), "'", "''"))
				default:
					valueStrs[i] = fmt.Sprintf("%v", val)
				}
			}

			if _, err := fmt.Fprintf(writer, "INSERT INTO %s (%s) VALUES (%s);\n",
				table, strings.Join(columns, ", "), strings.Join(valueStrs, ", ")); err != nil {
				_ = rows.Close() //nolint:errcheck
				return err
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close() //nolint:errcheck
			return err
		}
		_ = rows.Close() //nolint:errcheck

		if _, err := fmt.Fprintf(writer, "\n"); err != nil {
			return err
		}
	}

	return nil
}

// txDB wraps a *sql.Tx to implement the DB interface for dry-run migrations.
// It routes all queries through the transaction so they can be rolled back.
type txDB struct {
	tx            *sql.Tx
	driverType    DriverType
	dialect       Dialect
	vectorEnabled bool
}

func (t *txDB) Close() error                                   { return nil }
func (t *txDB) Exec(q string, args ...any) (sql.Result, error) { return t.tx.Exec(q, args...) }
func (t *txDB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, q, args...)
}
func (t *txDB) Query(q string, args ...any) (*sql.Rows, error) { return t.tx.Query(q, args...) }
func (t *txDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, q, args...)
}
func (t *txDB) QueryRow(q string, args ...any) *sql.Row { return t.tx.QueryRow(q, args...) }
func (t *txDB) QueryRowContext(ctx context.Context, q string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, q, args...)
}
func (t *txDB) Begin() (*sql.Tx, error) {
	return nil, fmt.Errorf("nested transactions not supported in dry-run mode")
}
func (t *txDB) BeginTx(_ context.Context, _ *sql.TxOptions) (*sql.Tx, error) {
	return nil, fmt.Errorf("nested transactions not supported in dry-run mode")
}
func (t *txDB) Ping() error                         { return nil }
func (t *txDB) PingContext(_ context.Context) error { return nil }
func (t *txDB) SetMaxOpenConns(_ int)               {}
func (t *txDB) SetMaxIdleConns(_ int)               {}
func (t *txDB) SetConnMaxLifetime(_ any)            {}
func (t *txDB) SetConnMaxIdleTime(_ any)            {}
func (t *txDB) Stats() sql.DBStats                  { return sql.DBStats{} }
func (t *txDB) GetUnderlyingDB() (*sql.DB, bool)    { return nil, false }
func (t *txDB) IsVectorSearchEnabled() bool         { return t.vectorEnabled }
func (t *txDB) GetDriverType() DriverType           { return t.driverType }
func (t *txDB) GetDialect() Dialect                 { return t.dialect }
