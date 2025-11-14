package sqlutil

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("close db: %v", err)
		}
	})
	return db
}

func setupNumbersTable(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`CREATE TABLE numbers (value INTEGER NOT NULL)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
}

func countRows(t *testing.T, db *sql.DB) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM numbers`).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}

func TestWithTransaction_Commits(t *testing.T) {
	db := openTestDB(t)
	setupNumbersTable(t, db)

	err := WithTransaction(context.Background(), db, func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(context.Background(), `INSERT INTO numbers(value) VALUES (1)`) //nolint:contextcheck
		return execErr
	})
	if err != nil {
		t.Fatalf("WithTransaction commit: %v", err)
	}
	if got := countRows(t, db); got != 1 {
		t.Fatalf("expected 1 row, got %d", got)
	}
}

func TestWithTransaction_RollbackOnError(t *testing.T) {
	db := openTestDB(t)
	setupNumbersTable(t, db)

	sentinel := errors.New("boom")
	err := WithTransaction(context.Background(), db, func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(context.Background(), `INSERT INTO numbers(value) VALUES (1)`); execErr != nil { //nolint:contextcheck
			return execErr
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if got := countRows(t, db); got != 0 {
		t.Fatalf("expected rollback, rows=%d", got)
	}
}

func TestWithTransaction_RollbackOnPanic(t *testing.T) {
	db := openTestDB(t)
	setupNumbersTable(t, db)

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic to propagate")
		}
		if got := countRows(t, db); got != 0 {
			t.Fatalf("expected rollback after panic, rows=%d", got)
		}
	}()

	_ = WithTransaction(context.Background(), db, func(tx *sql.Tx) error { //nolint:errcheck // panic path verified via defer
		if _, execErr := tx.ExecContext(context.Background(), `INSERT INTO numbers(value) VALUES (1)`); execErr != nil { //nolint:contextcheck
			return execErr
		}
		panic("uh oh")
	})
}

func TestWithTx_ReturnsValue(t *testing.T) {
	db := openTestDB(t)
	setupNumbersTable(t, db)

	type payload struct {
		Inserted int
		Sum      int
	}

	res, err := WithTx(context.Background(), db, func(tx *sql.Tx) (payload, error) {
		if _, execErr := tx.ExecContext(context.Background(), `INSERT INTO numbers(value) VALUES (1)`); execErr != nil { //nolint:contextcheck
			return payload{}, execErr
		}
		return payload{Inserted: 1, Sum: 1}, nil
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if res.Inserted != 1 || res.Sum != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestWithTx_ErrorReturnsZeroValue(t *testing.T) {
	db := openTestDB(t)
	setupNumbersTable(t, db)

	res, err := WithTx(context.Background(), db, func(tx *sql.Tx) (int, error) {
		return 0, errors.New("fail")
	})
	if err == nil {
		t.Fatalf("expected error")
	}
	if res != 0 {
		t.Fatalf("expected zero value, got %d", res)
	}
}
