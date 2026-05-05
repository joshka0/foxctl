package persist

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/joshka0/foxcular"
	_ "modernc.org/sqlite"
)

// SQLiteSink stores events in a SQLite database. Events are stored in a
// queryable form preserving all required fields and custom data.
type SQLiteSink struct {
	mu sync.Mutex
	db *sql.DB

	// insertStmt is a prepared statement for inserting events.
	insertStmt *sql.Stmt
}

// SQLiteOption configures a SQLiteSink.
type SQLiteOption func(*sqliteOpts)

type sqliteOpts struct{}

// NewSQLiteSink creates a SQLiteSink that stores events in the database at the
// given path. The database and events table are created if they do not exist.
func NewSQLiteSink(path string, _ ...SQLiteOption) (*SQLiteSink, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err)
	}

	// Enable WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: set WAL mode: %w", err)
	}

	s := &SQLiteSink{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: migrate: %w", err)
	}
	return s, nil
}

// migrate creates the events table if it does not exist.
func (s *SQLiteSink) migrate() error {
	const createTable = `
	CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp TEXT NOT NULL,
		trace_id TEXT NOT NULL DEFAULT '',
		span_id TEXT NOT NULL DEFAULT '',
		parent_id TEXT NOT NULL DEFAULT '',
		operation TEXT NOT NULL,
		name TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT '',
		duration_ms INTEGER NOT NULL DEFAULT 0,
		message TEXT NOT NULL DEFAULT '',
		error_type TEXT NOT NULL DEFAULT '',
		error_code TEXT NOT NULL DEFAULT '',
		error_message TEXT NOT NULL DEFAULT '',
		forced INTEGER NOT NULL DEFAULT 0,
		data TEXT NOT NULL DEFAULT '{}',
		audit_hash TEXT NOT NULL DEFAULT '',
		audit_prev_hash TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp);
	CREATE INDEX IF NOT EXISTS idx_events_trace_id ON events(trace_id);
	CREATE INDEX IF NOT EXISTS idx_events_operation ON events(operation);`
	if _, err := s.db.Exec(createTable); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	stmt, err := s.db.Prepare(`
		INSERT INTO events (
			timestamp, trace_id, span_id, parent_id,
			operation, name, status, duration_ms,
			message, error_type, error_code, error_message,
			forced, data, audit_hash, audit_prev_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	s.insertStmt = stmt
	return nil
}

// Send stores one event in the database.
func (s *SQLiteSink) Send(_ context.Context, event *foxcular.Event) error {
	if event == nil {
		return nil
	}

	dataJSON, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("sqlite: marshal data: %w", err)
	}

	forced := 0
	if event.Forced {
		forced = 1
	}

	// Get audit hashes from the event data if present.
	auditHash, _ := event.Data["_audit_hash"].(string)
	auditPrevHash, _ := event.Data["_audit_prev_hash"].(string)

	s.mu.Lock()
	defer s.mu.Unlock()

	_, err = s.insertStmt.Exec(
		event.Timestamp.UTC().Format(time.RFC3339Nano),
		event.TraceID,
		event.SpanID,
		event.ParentID,
		event.Operation,
		event.Name,
		string(event.Status),
		event.Duration.Milliseconds(),
		event.Message,
		event.ErrorType,
		event.ErrorCode,
		event.ErrorMessage,
		forced,
		string(dataJSON),
		auditHash,
		auditPrevHash,
	)
	if err != nil {
		return fmt.Errorf("sqlite: insert: %w", err)
	}
	return nil
}

// Flush is a no-op for SQLite since each Send commits immediately.
func (s *SQLiteSink) Flush(_ context.Context) error {
	return nil
}

// Close releases the prepared statement and database connection.
// Safe to call multiple times.
func (s *SQLiteSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.insertStmt != nil {
		_ = s.insertStmt.Close()
		s.insertStmt = nil
	}
	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		return err
	}
	return nil
}

// QueryEvents queries stored events with the given SQL query suffix (appended
// after "SELECT * FROM events"). Returns the raw rows for caller inspection.
// The caller must call Close() on the returned rows.
func (s *SQLiteSink) QueryEvents(querySuffix string, args ...any) (*sql.Rows, error) {
	q := "SELECT id, timestamp, trace_id, span_id, parent_id, operation, name, status, duration_ms, message, error_type, error_code, error_message, forced, data, audit_hash, audit_prev_hash FROM events"
	if querySuffix != "" {
		q += " " + querySuffix
	}
	return s.db.Query(q, args...)
}

// QueryAllEvents returns all stored events as typed Event structs.
func (s *SQLiteSink) QueryAllEvents() ([]*foxcular.Event, error) {
	rows, err := s.QueryEvents("ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []*foxcular.Event
	for rows.Next() {
		var (
			id         int
			timestamp  string
			traceID    string
			spanID     string
			parentID   string
			operation  string
			name       string
			status     string
			durationMs int64
			message    string
			errorType  string
			errorCode  string
			errorMsg   string
			forced     int
			dataJSON   string
			auditHash  string
			auditPrev  string
		)
		if err := rows.Scan(&id, &timestamp, &traceID, &spanID, &parentID,
			&operation, &name, &status, &durationMs, &message,
			&errorType, &errorCode, &errorMsg, &forced, &dataJSON,
			&auditHash, &auditPrev); err != nil {
			return nil, fmt.Errorf("sqlite: scan row: %w", err)
		}

		ts, err := time.Parse(time.RFC3339Nano, timestamp)
		if err != nil {
			ts = time.Time{}
		}

		var data map[string]any
		if dataJSON != "" && dataJSON != "{}" {
			if err := json.Unmarshal([]byte(dataJSON), &data); err != nil {
				data = make(map[string]any)
			}
		}
		if data == nil {
			data = make(map[string]any)
		}

		events = append(events, &foxcular.Event{
			Timestamp:    ts,
			TraceID:      traceID,
			SpanID:       spanID,
			ParentID:     parentID,
			Operation:    operation,
			Name:         name,
			Status:       foxcular.Status(status),
			Duration:     time.Duration(durationMs) * time.Millisecond,
			Message:      message,
			ErrorType:    errorType,
			ErrorCode:    errorCode,
			ErrorMessage: errorMsg,
			Forced:       forced == 1,
			Data:         data,
		})
	}
	return events, rows.Err()
}
