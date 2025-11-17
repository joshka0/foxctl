// Package blackboard implements SQLite-backed persistence for shared coordination.
package blackboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
)

// Store defines the persistence interface for blackboard coordination.
type Store interface {
	Close() error
	Post(ctx context.Context, record agent.BlackboardRecord) error
	Get(ctx context.Context, id string) (agent.BlackboardRecord, error)
	Search(ctx context.Context, ns, topic string, limit int) ([]agent.BlackboardRecord, error)
	Claim(ctx context.Context, id, agentID string, leaseDuration time.Duration) (agent.BlackboardRecord, error)
	Release(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	ListByTopic(ctx context.Context, ns, topic string, limit int) ([]agent.BlackboardRecord, error)
	Watch(ctx context.Context, ns, topic string, fromTS int64) (<-chan agent.BlackboardRecord, <-chan error)
}

type sqlStore struct {
	db *sql.DB
}

// Open initializes the blackboard store rooted at the provided path.
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "blackboard.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("blackboard: open db: %w", err)
	}
	return &sqlStore{db: db}, nil
}

func (s *sqlStore) Close() error {
	return s.db.Close()
}

func (s *sqlStore) Post(ctx context.Context, record agent.BlackboardRecord) error {
	leaseJSON := "null"
	if record.Lease != nil {
		b, err := json.Marshal(record.Lease)
		if err != nil {
			return fmt.Errorf("blackboard: marshal lease: %w", err)
		}
		leaseJSON = string(b)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO blackboard (id, ns, topic, ts, ttl_sec, payload, cas_ref, lease)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.NS, record.Topic, record.TS, record.TTLSec, string(record.Payload), record.CASRef, leaseJSON)
	if err != nil {
		return fmt.Errorf("blackboard: post: %w", err)
	}
	return nil
}

func (s *sqlStore) Get(ctx context.Context, id string) (agent.BlackboardRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, ns, topic, ts, ttl_sec, payload, cas_ref, lease
		FROM blackboard WHERE id = ?`, id)

	record, err := scanRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent.BlackboardRecord{}, ErrNotFound
		}
		return agent.BlackboardRecord{}, err
	}
	return record, nil
}

func (s *sqlStore) Search(ctx context.Context, ns, topic string, limit int) ([]agent.BlackboardRecord, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, ns, topic, ts, ttl_sec, payload, cas_ref, lease
		FROM blackboard
		WHERE ns = ? AND topic = ?
		ORDER BY ts DESC
		LIMIT ?`, ns, topic, limit)
	if err != nil {
		return nil, fmt.Errorf("blackboard: search: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close blackboard search rows")
	}()

	var records []agent.BlackboardRecord
	for rows.Next() {
		record, err := scanRecordFromRows(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *sqlStore) Claim(ctx context.Context, id, agentID string, leaseDuration time.Duration) (agent.BlackboardRecord, error) {
	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return agent.BlackboardRecord{}, fmt.Errorf("blackboard: begin transaction: %w", err)
	}
	defer func() {
		errs.Ignore(tx.Rollback(), "rollback blackboard claim transaction")
	}()

	// Fetch the record
	row := tx.QueryRowContext(ctx, `
		SELECT id, ns, topic, ts, ttl_sec, payload, cas_ref, lease
		FROM blackboard WHERE id = ?`, id)

	record, err := scanRecord(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return agent.BlackboardRecord{}, ErrNotFound
		}
		return agent.BlackboardRecord{}, err
	}

	// Check if already leased
	if record.IsLeased() {
		return agent.BlackboardRecord{}, ErrAlreadyLeased
	}

	// Create new lease
	lease := &agent.Lease{
		Holder: agentID,
		Until:  time.Now().Add(leaseDuration).Unix(),
	}

	leaseJSON, err := json.Marshal(lease)
	if err != nil {
		return agent.BlackboardRecord{}, fmt.Errorf("blackboard: marshal lease: %w", err)
	}

	// Update the lease
	_, err = tx.ExecContext(ctx, `UPDATE blackboard SET lease = ? WHERE id = ?`, string(leaseJSON), id)
	if err != nil {
		return agent.BlackboardRecord{}, fmt.Errorf("blackboard: update lease: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return agent.BlackboardRecord{}, fmt.Errorf("blackboard: commit claim: %w", err)
	}

	record.Lease = lease
	return record, nil
}

func (s *sqlStore) Release(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE blackboard SET lease = null WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("blackboard: release: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("blackboard: release rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM blackboard WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("blackboard: delete: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("blackboard: delete rows affected: %w", err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *sqlStore) ListByTopic(ctx context.Context, ns, topic string, limit int) ([]agent.BlackboardRecord, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, ns, topic, ts, ttl_sec, payload, cas_ref, lease
		FROM blackboard
		WHERE ns = ? AND topic = ?
		ORDER BY ts DESC
		LIMIT ?`, ns, topic, limit)
	if err != nil {
		return nil, fmt.Errorf("blackboard: list by topic: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close blackboard list rows")
	}()

	var records []agent.BlackboardRecord
	for rows.Next() {
		record, err := scanRecordFromRows(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, nil
}

func (s *sqlStore) Watch(ctx context.Context, ns, topic string, fromTS int64) (<-chan agent.BlackboardRecord, <-chan error) {
	recordCh := make(chan agent.BlackboardRecord, 10)
	errCh := make(chan error, 1)

	go func() {
		defer close(recordCh)
		defer close(errCh)

		lastTS := fromTS
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Query for new records since lastTS
				rows, err := s.db.QueryContext(ctx, `
					SELECT id, ns, topic, ts, ttl_sec, payload, cas_ref, lease
					FROM blackboard
					WHERE ns = ? AND topic = ? AND ts > ?
					ORDER BY ts ASC`, ns, topic, lastTS)
				if err != nil {
					errCh <- fmt.Errorf("blackboard: watch query: %w", err)
					return
				}

				var newRecords []agent.BlackboardRecord
				for rows.Next() {
					record, err := scanRecordFromRows(rows)
					if err != nil {
						errs.Ignore(rows.Close(), "close blackboard watch rows")
						errCh <- fmt.Errorf("blackboard: watch scan: %w", err)
						return
					}
					newRecords = append(newRecords, record)
					if record.TS > lastTS {
						lastTS = record.TS
					}
				}
				errs.Ignore(rows.Close(), "close blackboard watch rows")

				// Send new records
				for _, record := range newRecords {
					select {
					case <-ctx.Done():
						return
					case recordCh <- record:
					}
				}
			}
		}
	}()

	return recordCh, errCh
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS blackboard (
	id        TEXT PRIMARY KEY,
	ns        TEXT NOT NULL,
	topic     TEXT NOT NULL,
	ts        INTEGER NOT NULL,
	ttl_sec   INTEGER NOT NULL,
	payload   TEXT NOT NULL,
	cas_ref   TEXT,
	lease     TEXT
);
CREATE INDEX IF NOT EXISTS idx_bb_ns_topic ON blackboard(ns, topic);
CREATE INDEX IF NOT EXISTS idx_bb_ts ON blackboard(ts);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("blackboard: migrate: %w", err)
	}
	return nil
}

type scanner interface {
	Scan(dest ...interface{}) error
}

func scanRecord(s scanner) (agent.BlackboardRecord, error) {
	var record agent.BlackboardRecord
	var payloadJSON string
	var leaseJSON sql.NullString
	if err := s.Scan(&record.ID, &record.NS, &record.Topic, &record.TS, &record.TTLSec, &payloadJSON, &record.CASRef, &leaseJSON); err != nil {
		return agent.BlackboardRecord{}, fmt.Errorf("blackboard: scan: %w", err)
	}

	// Set payload as RawMessage
	record.Payload = json.RawMessage(payloadJSON)

	// Parse lease if present
	if leaseJSON.Valid && leaseJSON.String != "null" && leaseJSON.String != "" {
		var lease agent.Lease
		if err := json.Unmarshal([]byte(leaseJSON.String), &lease); err != nil {
			return agent.BlackboardRecord{}, fmt.Errorf("blackboard: unmarshal lease: %w", err)
		}
		record.Lease = &lease
	}

	return record, nil
}

func scanRecordFromRows(rows *sql.Rows) (agent.BlackboardRecord, error) {
	var record agent.BlackboardRecord
	var payloadJSON string
	var leaseJSON sql.NullString
	if err := rows.Scan(&record.ID, &record.NS, &record.Topic, &record.TS, &record.TTLSec, &payloadJSON, &record.CASRef, &leaseJSON); err != nil {
		return agent.BlackboardRecord{}, fmt.Errorf("blackboard: scan: %w", err)
	}

	// Set payload as RawMessage
	record.Payload = json.RawMessage(payloadJSON)

	// Parse lease if present
	if leaseJSON.Valid && leaseJSON.String != "null" && leaseJSON.String != "" {
		var lease agent.Lease
		if err := json.Unmarshal([]byte(leaseJSON.String), &lease); err != nil {
			return agent.BlackboardRecord{}, fmt.Errorf("blackboard: unmarshal lease: %w", err)
		}
		record.Lease = &lease
	}

	return record, nil
}

// ErrNotFound indicates the record was not found.
var ErrNotFound = errors.New("blackboard: not found")

// ErrAlreadyLeased indicates the record is already leased.
var ErrAlreadyLeased = errors.New("blackboard: already leased")
