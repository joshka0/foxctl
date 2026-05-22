package sqlutil

import (
	"context"
	"database/sql"
	"fmt"
)

// TransactionStarter is the minimal database capability needed to start a SQL
// transaction. Both *sql.DB and the repository DB driver abstraction implement it.
type TransactionStarter interface {
	BeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error)
}

// WithTransaction executes fn within a SQL transaction.
//
// If fn returns an error the transaction is rolled back and the error is
// returned. Any panic inside fn will trigger a rollback before re-panicking.
func WithTransaction(ctx context.Context, db TransactionStarter, fn func(*sql.Tx) error) error {
	if db == nil {
		return fmt.Errorf("sqlutil: db is nil")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	committed := false

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback() //nolint:errcheck // best effort rollback on panic
			panic(p)
		}
		if !committed {
			_ = tx.Rollback() //nolint:errcheck // best effort rollback if commit not reached
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback error: %v)", err, rbErr)
		}
		committed = true
		return err
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("commit transaction: %w (rollback error: %v)", err, rbErr)
		}
		committed = true
		return fmt.Errorf("commit transaction: %w", err)
	}

	committed = true
	return nil
}

// WithTx executes fn within a SQL transaction and returns its result.
func WithTx[T any](ctx context.Context, db TransactionStarter, fn func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	var result T
	err := WithTransaction(ctx, db, func(tx *sql.Tx) error {
		var err error
		result, err = fn(tx)
		return err
	})
	if err != nil {
		return zero, err
	}
	return result, nil
}
