// Package blackboard implements SQLite-backed persistence for workspace coordination.
// This file contains the BoardStore for BoardMessage and FileReservation types
// per mailbox_blackboard.md spec.
package blackboard

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/oklog/ulid/v2"
)

// BoardStore defines the persistence interface for workspace coordination messages and reservations.
type BoardStore interface {
	Close() error

	// Message operations
	SendMessage(ctx context.Context, msg agent.BoardMessage) error
	Inbox(ctx context.Context, filter agent.InboxFilter) ([]agent.BoardMessage, error)
	MarkRead(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error)
	AckMessages(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error)

	// Reservation operations
	Reserve(ctx context.Context, res agent.FileReservation) error
	CheckConflicts(ctx context.Context, workspaceID string, paths []string, holder string, mode agent.ReservationMode) ([]agent.ReservationConflict, error)
	Release(ctx context.Context, workspaceID, actorID string, paths []string) (int, error)
	ReleaseByID(ctx context.Context, reservationIDs []string) (int, error)
	ListReservations(ctx context.Context, workspaceID string) ([]agent.FileReservation, error)
}

type boardSQLStore struct {
	db *sql.DB
}

// OpenBoardStore initializes the board store rooted at the provided path.
func OpenBoardStore(ctx context.Context, root string) (BoardStore, error) {
	dbPath := filepath.Join(root, "board.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrateBoard)
	if err != nil {
		return nil, fmt.Errorf("board: open db: %w", err)
	}
	return &boardSQLStore{db: db}, nil
}

func (s *boardSQLStore) Close() error {
	return s.db.Close()
}

// SendMessage inserts a new BoardMessage.
func (s *boardSQLStore) SendMessage(ctx context.Context, msg agent.BoardMessage) error {
	if msg.ID == "" {
		msg.ID = ulid.Make().String()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now().UTC()
	}
	if msg.Status == "" {
		msg.Status = agent.BoardMessageStatusUnread
	}
	if msg.Stream == "" {
		msg.Stream = agent.DefaultStream
	}
	if msg.Priority == 0 {
		msg.Priority = agent.DefaultPriority
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO board_messages 
		(id, workspace_id, task_id, stream, sender, recipient, kind, priority, ack_required, status, subject, body, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.WorkspaceID, msg.TaskID, msg.Stream, msg.Sender, msg.Recipient,
		msg.Kind, msg.Priority, msg.AckRequired, msg.Status, msg.Subject, msg.Body,
		msg.CreatedAt.Unix())
	if err != nil {
		return fmt.Errorf("board: send message: %w", err)
	}
	return nil
}

// Inbox retrieves messages for an actor based on filter criteria.
func (s *boardSQLStore) Inbox(ctx context.Context, filter agent.InboxFilter) ([]agent.BoardMessage, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}

	// Build query with optional filters
	query := `
		SELECT id, workspace_id, task_id, stream, sender, recipient, kind, priority, ack_required, status, subject, body, created_at
		FROM board_messages
		WHERE workspace_id = ? AND (recipient = ? OR recipient = '*')`
	args := []any{filter.WorkspaceID, filter.ActorID}

	if filter.TaskID != "" {
		query += ` AND (task_id = ? OR task_id = '')`
		args = append(args, filter.TaskID)
	}
	if filter.Stream != "" {
		query += ` AND stream = ?`
		args = append(args, filter.Stream)
	}
	if filter.OnlyUnread {
		query += ` AND status = ?`
		args = append(args, agent.BoardMessageStatusUnread)
	}

	// Order by priority (1 first), then created_at (newest first)
	query += ` ORDER BY priority ASC, created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("board: inbox query: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close board inbox rows")
	}()

	var messages []agent.BoardMessage
	for rows.Next() {
		msg, err := scanBoardMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

// MarkRead marks messages as read.
func (s *boardSQLStore) MarkRead(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	// Build placeholders for IN clause
	placeholders := "?"
	for i := 1; i < len(messageIDs); i++ {
		placeholders += ", ?"
	}

	args := []any{agent.BoardMessageStatusRead, workspaceID}
	for _, id := range messageIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		UPDATE board_messages 
		SET status = ?
		WHERE workspace_id = ? AND id IN (%s) AND status = 'unread'`, placeholders)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("board: mark read: %w", err)
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

// AckMessages marks messages as acknowledged.
func (s *boardSQLStore) AckMessages(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	placeholders := "?"
	for i := 1; i < len(messageIDs); i++ {
		placeholders += ", ?"
	}

	args := []any{agent.BoardMessageStatusAcked, workspaceID}
	for _, id := range messageIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		UPDATE board_messages 
		SET status = ?
		WHERE workspace_id = ? AND id IN (%s)`, placeholders)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("board: ack messages: %w", err)
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

// Reserve creates a file reservation.
func (s *boardSQLStore) Reserve(ctx context.Context, res agent.FileReservation) error {
	if res.ID == "" {
		res.ID = ulid.Make().String()
	}
	if res.CreatedAt.IsZero() {
		res.CreatedAt = time.Now().UTC()
	}
	if res.ExpiresAt.IsZero() {
		res.ExpiresAt = res.CreatedAt.Add(agent.DefaultReservationTTL)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO file_reservations (id, workspace_id, task_id, path, holder, mode, reason, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		res.ID, res.WorkspaceID, res.TaskID, res.Path, res.Holder, res.Mode, res.Reason, res.ExpiresAt.Unix(), res.CreatedAt.Unix())
	if err != nil {
		return fmt.Errorf("board: reserve: %w", err)
	}
	return nil
}

// CheckConflicts checks for existing reservations that would conflict with the requested paths.
func (s *boardSQLStore) CheckConflicts(ctx context.Context, workspaceID string, paths []string, holder string, mode agent.ReservationMode) ([]agent.ReservationConflict, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	// Clean up expired reservations first
	now := time.Now().UTC().Unix()
	_, _ = s.db.ExecContext(ctx, `DELETE FROM file_reservations WHERE expires_at < ?`, now)

	// Build query for conflicting reservations
	placeholders := "?"
	for i := 1; i < len(paths); i++ {
		placeholders += ", ?"
	}

	args := []any{workspaceID}
	for _, p := range paths {
		args = append(args, p)
	}
	args = append(args, holder, now)

	// Conflict rules:
	// - Any existing exclusive reservation by another holder = conflict
	// - New exclusive request conflicts with any existing reservation by another holder
	// - Shared can coexist with shared from other holders
	var query string
	if mode == agent.ReservationModeExclusive {
		// Exclusive conflicts with any existing by other holders
		query = fmt.Sprintf(`
			SELECT path, holder, mode, task_id, reason, expires_at FROM file_reservations
			WHERE workspace_id = ? AND path IN (%s) AND holder != ? AND expires_at >= ?`, placeholders)
	} else {
		// Shared only conflicts with exclusive by other holders
		query = fmt.Sprintf(`
			SELECT path, holder, mode, task_id, reason, expires_at FROM file_reservations
			WHERE workspace_id = ? AND path IN (%s) AND holder != ? AND mode = 'exclusive' AND expires_at >= ?`, placeholders)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("board: check conflicts: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close check conflicts rows")
	}()

	var conflicts []agent.ReservationConflict
	for rows.Next() {
		var c agent.ReservationConflict
		var expiresAt int64
		if err := rows.Scan(&c.Path, &c.Holder, &c.Mode, &c.TaskID, &c.Reason, &expiresAt); err != nil {
			return nil, fmt.Errorf("board: scan conflict: %w", err)
		}
		c.ExpiresAt = time.Unix(expiresAt, 0).UTC()
		conflicts = append(conflicts, c)
	}
	return conflicts, nil
}

// Release releases reservations by path and holder.
func (s *boardSQLStore) Release(ctx context.Context, workspaceID, actorID string, paths []string) (int, error) {
	if len(paths) == 0 {
		return 0, nil
	}

	placeholders := "?"
	for i := 1; i < len(paths); i++ {
		placeholders += ", ?"
	}

	args := []any{workspaceID, actorID}
	for _, p := range paths {
		args = append(args, p)
	}

	query := fmt.Sprintf(`
		DELETE FROM file_reservations 
		WHERE workspace_id = ? AND holder = ? AND path IN (%s)`, placeholders)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("board: release: %w", err)
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

// ReleaseByID releases reservations by ID.
func (s *boardSQLStore) ReleaseByID(ctx context.Context, reservationIDs []string) (int, error) {
	if len(reservationIDs) == 0 {
		return 0, nil
	}

	placeholders := "?"
	for i := 1; i < len(reservationIDs); i++ {
		placeholders += ", ?"
	}

	args := make([]any, len(reservationIDs))
	for i, id := range reservationIDs {
		args[i] = id
	}

	query := fmt.Sprintf(`DELETE FROM file_reservations WHERE id IN (%s)`, placeholders)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("board: release by id: %w", err)
	}
	affected, _ := res.RowsAffected()
	return int(affected), nil
}

// ListReservations lists all active reservations for a workspace.
func (s *boardSQLStore) ListReservations(ctx context.Context, workspaceID string) ([]agent.FileReservation, error) {
	now := time.Now().UTC().Unix()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_id, task_id, path, holder, mode, reason, expires_at, created_at
		FROM file_reservations
		WHERE workspace_id = ? AND expires_at >= ?
		ORDER BY created_at DESC`, workspaceID, now)
	if err != nil {
		return nil, fmt.Errorf("board: list reservations: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close list reservations rows")
	}()

	var reservations []agent.FileReservation
	for rows.Next() {
		res, err := scanReservation(rows)
		if err != nil {
			return nil, err
		}
		reservations = append(reservations, res)
	}
	return reservations, nil
}

func migrateBoard(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS board_messages (
	id           TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	task_id      TEXT,
	stream       TEXT NOT NULL DEFAULT 'coordination',
	sender       TEXT NOT NULL,
	recipient    TEXT NOT NULL,
	kind         TEXT NOT NULL DEFAULT 'info',
	priority     INTEGER NOT NULL DEFAULT 3,
	ack_required INTEGER NOT NULL DEFAULT 0,
	status       TEXT NOT NULL DEFAULT 'unread',
	subject      TEXT NOT NULL,
	body         TEXT NOT NULL,
	created_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_board_msg_workspace_recipient ON board_messages(workspace_id, recipient);
CREATE INDEX IF NOT EXISTS idx_board_msg_workspace_task ON board_messages(workspace_id, task_id);
CREATE INDEX IF NOT EXISTS idx_board_msg_priority_created ON board_messages(priority, created_at);

CREATE TABLE IF NOT EXISTS file_reservations (
	id           TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL,
	task_id      TEXT,
	path         TEXT NOT NULL,
	holder       TEXT NOT NULL,
	mode         TEXT NOT NULL DEFAULT 'exclusive',
	reason       TEXT,
	expires_at   INTEGER NOT NULL,
	created_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_res_workspace_path ON file_reservations(workspace_id, path);
CREATE INDEX IF NOT EXISTS idx_res_expires ON file_reservations(expires_at);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("board: migrate: %w", err)
	}
	return nil
}

func scanBoardMessage(rows *sql.Rows) (agent.BoardMessage, error) {
	var msg agent.BoardMessage
	var createdAt int64
	var ackRequired int
	if err := rows.Scan(&msg.ID, &msg.WorkspaceID, &msg.TaskID, &msg.Stream, &msg.Sender, &msg.Recipient,
		&msg.Kind, &msg.Priority, &ackRequired, &msg.Status, &msg.Subject, &msg.Body, &createdAt); err != nil {
		return agent.BoardMessage{}, fmt.Errorf("board: scan message: %w", err)
	}
	msg.CreatedAt = time.Unix(createdAt, 0).UTC()
	msg.AckRequired = ackRequired != 0
	return msg, nil
}

func scanReservation(rows *sql.Rows) (agent.FileReservation, error) {
	var res agent.FileReservation
	var expiresAt, createdAt int64
	if err := rows.Scan(&res.ID, &res.WorkspaceID, &res.TaskID, &res.Path, &res.Holder, &res.Mode, &res.Reason, &expiresAt, &createdAt); err != nil {
		return agent.FileReservation{}, fmt.Errorf("board: scan reservation: %w", err)
	}
	res.ExpiresAt = time.Unix(expiresAt, 0).UTC()
	res.CreatedAt = time.Unix(createdAt, 0).UTC()
	return res, nil
}

// ErrReservationConflict indicates a reservation conflict.
var ErrReservationConflict = errors.New("board: reservation conflict")
