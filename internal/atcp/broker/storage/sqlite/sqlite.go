// Package sqlite is the persistent Store implementation for the ATCP broker.
//
// The schema mirrors the plan §5a.7 tables for atcp_rooms and
// atcp_room_members, plus an atcp_messages + atcp_message_deliveries audit
// log. Schema is created idempotently on Open; migrations are currently
// append-only — when a compatibility break is needed we'll add a migrations
// tracking table, but today's three-table surface is stable enough that the
// IF NOT EXISTS approach is safer than versioned DDL scripts.
//
// Driver: modernc.org/sqlite (pure Go, no CGO). Chosen for parity with
// every other storage package in this repo.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"time"

	"github.com/joshka0/foxctl/internal/atcp/broker/room"
	"github.com/joshka0/foxctl/internal/atcp/broker/storage"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS atcp_rooms (
	id          TEXT PRIMARY KEY,
	workspace   TEXT NOT NULL,
	title       TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	created_at  TEXT NOT NULL,
	archived_at TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS atcp_rooms_workspace_idx ON atcp_rooms(workspace);

CREATE TABLE IF NOT EXISTS atcp_room_members (
	room_id     TEXT NOT NULL,
	agent_id    TEXT NOT NULL,
	session_id  TEXT NOT NULL,
	inbox_id    TEXT NOT NULL DEFAULT '',
	role        TEXT NOT NULL DEFAULT '',
	role_custom TEXT NOT NULL DEFAULT '',
	can_mutate  INTEGER NOT NULL DEFAULT 0,
	import_hint TEXT NOT NULL DEFAULT '',
	joined_at   TEXT NOT NULL,
	left_at     TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (room_id, agent_id, session_id, joined_at),
	FOREIGN KEY (room_id) REFERENCES atcp_rooms(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS atcp_room_members_room_idx ON atcp_room_members(room_id);
-- Partial unique index on the session column for active rows only:
-- enforces plan §5a.7 "a session_id appears in at most one active member".
CREATE UNIQUE INDEX IF NOT EXISTS atcp_room_members_active_session_uniq
	ON atcp_room_members(session_id)
	WHERE left_at = '';

CREATE TABLE IF NOT EXISTS atcp_messages (
	id         TEXT PRIMARY KEY,
	room_id    TEXT NOT NULL,
	source     TEXT NOT NULL DEFAULT '',
	text       TEXT NOT NULL,
	delivery   TEXT NOT NULL DEFAULT '',
	sent_at    TEXT NOT NULL,
	delivered  INTEGER NOT NULL DEFAULT 0,
	failed     INTEGER NOT NULL DEFAULT 0,
	FOREIGN KEY (room_id) REFERENCES atcp_rooms(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS atcp_messages_room_idx ON atcp_messages(room_id, sent_at);

CREATE TABLE IF NOT EXISTS atcp_message_deliveries (
	message_id TEXT NOT NULL,
	agent_id   TEXT NOT NULL,
	session_id TEXT NOT NULL,
	delivered  INTEGER NOT NULL,
	error      TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (message_id, agent_id),
	FOREIGN KEY (message_id) REFERENCES atcp_messages(id) ON DELETE CASCADE
);
`

// Store is the SQLite-backed implementation of storage.Store.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) a SQLite database at dsn and ensures the schema is
// present. dsn may be a file path, a ":memory:" DSN, or any driver-native
// DSN; common pragmas (busy_timeout, foreign_keys, journal_mode=WAL) are
// applied unless the caller already set them in the query string.
func Open(ctx context.Context, dsn string) (*Store, error) {
	full, err := applyDefaultPragmas(dsn)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", full)
	if err != nil {
		return nil, fmt.Errorf("atcp sqlite: open: %w", err)
	}
	// Single connection per database keeps the modernc driver's
	// transactional semantics predictable and avoids surprise deadlocks
	// when multiple goroutines write concurrently.
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("atcp sqlite: ping: %w", err)
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("atcp sqlite: schema: %w", err)
	}
	return &Store{db: db}, nil
}

// applyDefaultPragmas ensures the DSN has sensible defaults for a
// durable-but-responsive broker. Callers can override any of these by
// setting the same key in their DSN.
func applyDefaultPragmas(dsn string) (string, error) {
	// In-memory DSNs skip WAL + foreign_keys pragmas; they're no-ops there
	// and the cache=shared semantics in tests matter more.
	if dsn == ":memory:" || dsn == "" {
		return dsn, nil
	}
	// Parse as file:... URL when possible so query params merge cleanly.
	if len(dsn) >= 5 && dsn[:5] == "file:" {
		u, err := url.Parse(dsn)
		if err != nil {
			return "", fmt.Errorf("atcp sqlite: parse dsn: %w", err)
		}
		q := u.Query()
		setSingleIfAbsent(q, "_busy_timeout", "5000")
		addPragmaIfAbsent(q, "foreign_keys", "foreign_keys(1)")
		addPragmaIfAbsent(q, "journal_mode", "journal_mode(WAL)")
		u.RawQuery = q.Encode()
		return u.String(), nil
	}
	// Plain path: rewrite as file: URL so we can attach pragmas uniformly.
	u := &url.URL{Scheme: "file", Opaque: dsn}
	q := u.Query()
	q.Set("_busy_timeout", "5000")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "journal_mode(WAL)")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// setSingleIfAbsent sets a single-valued query key only if the caller did
// not already supply one. Used for _busy_timeout, which the driver treats
// as scalar.
func setSingleIfAbsent(q url.Values, key, value string) {
	if q.Get(key) == "" {
		q.Set(key, value)
	}
}

// addPragmaIfAbsent appends a _pragma value if none of the caller-supplied
// _pragma entries already target the same pragma name. _pragma is
// deliberately multi-valued in the driver's DSN format, so a naive
// q.Get("_pragma") only sees the first entry and would mis-skip our
// defaults when the caller set an unrelated pragma like synchronous(NORMAL).
func addPragmaIfAbsent(q url.Values, name, value string) {
	if hasPragma(q, name) {
		return
	}
	q.Add("_pragma", value)
}

func hasPragma(q url.Values, name string) bool {
	for _, p := range q["_pragma"] {
		// Prefix match on "<name>(" avoids matching a pragma whose name
		// is a prefix of another (e.g. "foreign_keys" is a prefix of a
		// hypothetical "foreign_keys_extra").
		if len(p) > len(name) && p[:len(name)] == name && p[len(name)] == '(' {
			return true
		}
	}
	return false
}

// Close implements storage.Store.
func (s *Store) Close() error { return s.db.Close() }

// SaveRoom implements storage.Store via upsert.
func (s *Store) SaveRoom(ctx context.Context, r room.Room) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO atcp_rooms (id, workspace, title, description, created_at, archived_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			workspace   = excluded.workspace,
			title       = excluded.title,
			description = excluded.description,
			archived_at = excluded.archived_at
	`, r.ID, r.Workspace, r.Title, r.Description, timeToText(r.CreatedAt), timeToText(r.ArchivedAt))
	if err != nil {
		return fmt.Errorf("atcp sqlite: SaveRoom: %w", err)
	}
	return nil
}

// SaveMember implements storage.Store. The (room_id, agent_id, session_id,
// joined_at) primary key means a single join writes exactly one row and
// subsequent LeftAt updates target that row.
func (s *Store) SaveMember(ctx context.Context, m room.Member) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO atcp_room_members
			(room_id, agent_id, session_id, inbox_id, role, role_custom, can_mutate, import_hint, joined_at, left_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(room_id, agent_id, session_id, joined_at) DO UPDATE SET
			inbox_id    = excluded.inbox_id,
			role        = excluded.role,
			role_custom = excluded.role_custom,
			can_mutate  = excluded.can_mutate,
			import_hint = excluded.import_hint,
			left_at     = excluded.left_at
	`,
		m.RoomID, m.AgentID, m.SessionID, m.InboxID, string(m.Role), m.RoleCustom,
		boolToInt(m.CanMutate), m.ImportHint, timeToText(m.JoinedAt), timeToText(m.LeftAt),
	)
	if err != nil {
		return fmt.Errorf("atcp sqlite: SaveMember: %w", err)
	}
	return nil
}

// AppendMessage implements storage.Store. Writes the parent + deliveries in
// one transaction so a partial write never leaves an orphaned message row.
func (s *Store) AppendMessage(ctx context.Context, rec storage.MessageRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("atcp sqlite: AppendMessage begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO atcp_messages (id, room_id, source, text, delivery, sent_at, delivered, failed)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, rec.ID, rec.RoomID, rec.Source, rec.Text, rec.Delivery, timeToText(rec.SentAt), rec.Delivered, rec.Failed); err != nil {
		return fmt.Errorf("atcp sqlite: AppendMessage insert: %w", err)
	}
	for _, d := range rec.Members {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO atcp_message_deliveries (message_id, agent_id, session_id, delivered, error)
			VALUES (?, ?, ?, ?, ?)
		`, rec.ID, d.AgentID, d.SessionID, boolToInt(d.Delivered), d.ErrText); err != nil {
			return fmt.Errorf("atcp sqlite: AppendMessage delivery: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("atcp sqlite: AppendMessage commit: %w", err)
	}
	return nil
}

// LoadRooms implements storage.Store, returning rooms in deterministic
// created_at order so callers see a stable list.
func (s *Store) LoadRooms(ctx context.Context) ([]room.Room, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace, title, description, created_at, archived_at
		FROM atcp_rooms
		ORDER BY created_at ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("atcp sqlite: LoadRooms: %w", err)
	}
	defer rows.Close()
	var out []room.Room
	for rows.Next() {
		var r room.Room
		var createdText, archivedText string
		if err := rows.Scan(&r.ID, &r.Workspace, &r.Title, &r.Description, &createdText, &archivedText); err != nil {
			return nil, fmt.Errorf("atcp sqlite: LoadRooms scan: %w", err)
		}
		r.CreatedAt = textToTime(createdText)
		r.ArchivedAt = textToTime(archivedText)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("atcp sqlite: LoadRooms rows: %w", err)
	}
	return out, nil
}

// LoadMembers implements storage.Store.
func (s *Store) LoadMembers(ctx context.Context, roomID string) ([]room.Member, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT room_id, agent_id, session_id, inbox_id, role, role_custom, can_mutate, import_hint, joined_at, left_at
		FROM atcp_room_members
		WHERE room_id = ?
		ORDER BY joined_at ASC
	`, roomID)
	if err != nil {
		return nil, fmt.Errorf("atcp sqlite: LoadMembers: %w", err)
	}
	defer rows.Close()
	var out []room.Member
	for rows.Next() {
		var m room.Member
		var roleStr, joinedText, leftText string
		var canMutate int
		if err := rows.Scan(&m.RoomID, &m.AgentID, &m.SessionID, &m.InboxID, &roleStr, &m.RoleCustom,
			&canMutate, &m.ImportHint, &joinedText, &leftText); err != nil {
			return nil, fmt.Errorf("atcp sqlite: LoadMembers scan: %w", err)
		}
		m.Role = room.Role(roleStr)
		m.CanMutate = canMutate != 0
		m.JoinedAt = textToTime(joinedText)
		m.LeftAt = textToTime(leftText)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("atcp sqlite: LoadMembers rows: %w", err)
	}
	return out, nil
}

// timeToText serialises a time.Time as RFC3339Nano UTC, using an empty
// string for zero values. Keeping the "zero = empty" convention in SQL
// makes the `WHERE left_at = ”` partial index trivially correct and avoids
// confusing "0001-01-01T00:00:00Z" strings appearing in real audit
// queries.
func timeToText(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func textToTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		// Rows written by this package always round-trip; a parse error
		// here means an external writer corrupted the column. Return zero
		// and let the caller notice rather than hard-fail a load.
		return time.Time{}
	}
	return t
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Compile-time check that *Store implements storage.Store.
var _ storage.Store = (*Store)(nil)
