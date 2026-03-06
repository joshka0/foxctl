package blackboard

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/oklog/ulid/v2"
)

// BoardStore defines the persistence interface for workspace coordination messages and reservations.
type BoardStore interface {
	Close() error

	// Message operations
	SendMessage(ctx context.Context, msg *agent.BoardMessage) error
	Inbox(ctx context.Context, filter agent.InboxFilter) ([]agent.BoardMessage, error)
	UpsertRoom(ctx context.Context, room agent.Room) (agent.Room, error)
	EnsureRoom(ctx context.Context, workspaceID, roomID, title string) (agent.Room, error)
	ReplaceRoomMembers(ctx context.Context, workspaceID, roomID string, members []agent.RoomMember) ([]agent.RoomMember, error)
	ListRooms(ctx context.Context, workspaceID, actorID string, limit int) ([]agent.RoomSummary, error)
	GetRoom(ctx context.Context, workspaceID, roomID, actorID string) (agent.RoomSummary, error)
	ListRoomMessages(ctx context.Context, workspaceID, roomID string, limit int) ([]agent.BoardMessage, error)
	// MarkSurfaced marks messages as surfaced (shown in context, but not explicitly read).
	MarkSurfaced(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error)
	MarkRead(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error)
	AckMessages(ctx context.Context, workspaceID, actorID string, messageIDs []string) (int, error)
	// CountMessagesByTask counts unread messages per task grouped by sender type
	CountMessagesByTask(ctx context.Context, workspaceID, taskID string) (admin, overseer, total int, err error)

	// Reservation operations
	Reserve(ctx context.Context, res *agent.FileReservation) error
	CheckConflicts(ctx context.Context, workspaceID string, paths []string, holder string, mode agent.ReservationMode) ([]agent.ReservationConflict, error)
	Release(ctx context.Context, workspaceID, actorID string, paths []string) (int, error)
	ReleaseByID(ctx context.Context, reservationIDs []string) (int, error)
	ListReservations(ctx context.Context, workspaceID string) ([]agent.FileReservation, error)
}

type boardSQLStore struct {
	db    *sql.DB
	close func() error
}

const (
	defaultRoomDispatchPolicy = "all_subtree"
)

// OpenBoardStore initializes a BoardStore backed by a SQLite database stored at
// root/board.db. It applies the package migration function before returning the
// store and returns an error if the database cannot be opened or migrated.
func OpenBoardStore(ctx context.Context, root string) (BoardStore, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "BOARD", "board.db", migrateBoard)
	if err != nil {
		return nil, fmt.Errorf("board: open db: %w", err)
	}
	return &boardSQLStore{db: db, close: closeFn}, nil
}

func (s *boardSQLStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// SendMessage inserts a new BoardMessage.
// The msg.ID is populated with a ULID if not already set.
func (s *boardSQLStore) SendMessage(ctx context.Context, msg *agent.BoardMessage) error {
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

func (s *boardSQLStore) UpsertRoom(ctx context.Context, room agent.Room) (agent.Room, error) {
	room.WorkspaceID = strings.TrimSpace(room.WorkspaceID)
	room.ID = strings.TrimSpace(room.ID)
	room.Title = strings.TrimSpace(room.Title)
	room.Description = strings.TrimSpace(room.Description)
	if room.WorkspaceID == "" {
		return agent.Room{}, fmt.Errorf("board: upsert room: workspace_id is required")
	}
	if room.ID == "" {
		return agent.Room{}, fmt.Errorf("board: upsert room: room id is required")
	}
	if room.Stream == "" {
		room.Stream = agent.RoomStreamName(room.ID)
	}
	if room.Title == "" {
		room.Title = room.ID
	}
	room.DispatchPolicy = normalizeRoomDispatchPolicy(room.DispatchPolicy)
	room.DispatchAgentIDs = normalizeRoomDispatchAgentIDs(room.DispatchAgentIDs)
	dispatchAgentIDsJSON, err := json.Marshal(room.DispatchAgentIDs)
	if err != nil {
		return agent.Room{}, fmt.Errorf("board: upsert room marshal dispatch ids: %w", err)
	}
	now := time.Now().UTC()
	if room.CreatedAt.IsZero() {
		room.CreatedAt = now
	}
	room.UpdatedAt = now

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO room_metadata (workspace_id, room_id, stream, title, description, dispatch_policy, dispatch_agent_ids, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, room_id) DO UPDATE SET
			stream = excluded.stream,
			title = excluded.title,
			description = excluded.description,
			dispatch_policy = excluded.dispatch_policy,
			dispatch_agent_ids = excluded.dispatch_agent_ids,
			updated_at = excluded.updated_at`,
		room.WorkspaceID, room.ID, room.Stream, room.Title, room.Description,
		room.DispatchPolicy, string(dispatchAgentIDsJSON), room.CreatedAt.Unix(), room.UpdatedAt.Unix(),
	)
	if err != nil {
		return agent.Room{}, fmt.Errorf("board: upsert room: %w", err)
	}
	if len(room.Members) > 0 {
		members, err := s.ReplaceRoomMembers(ctx, room.WorkspaceID, room.ID, room.Members)
		if err != nil {
			return agent.Room{}, err
		}
		room.Members = members
	} else {
		members, err := s.listRoomMembers(ctx, room.WorkspaceID, room.ID)
		if err != nil {
			return agent.Room{}, err
		}
		room.Members = members
	}
	return room, nil
}

func (s *boardSQLStore) EnsureRoom(ctx context.Context, workspaceID, roomID, title string) (agent.Room, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	roomID = strings.TrimSpace(roomID)
	if workspaceID == "" {
		return agent.Room{}, fmt.Errorf("board: ensure room: workspace_id is required")
	}
	if roomID == "" {
		return agent.Room{}, fmt.Errorf("board: ensure room: room id is required")
	}
	now := time.Now().UTC()
	stream := agent.RoomStreamName(roomID)
	title = strings.TrimSpace(title)
	if title == "" {
		title = roomID
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO room_metadata (workspace_id, room_id, stream, title, description, dispatch_policy, dispatch_agent_ids, created_at, updated_at)
		VALUES (?, ?, ?, ?, '', ?, '[]', ?, ?)
		ON CONFLICT(workspace_id, room_id) DO UPDATE SET
			updated_at = excluded.updated_at`,
		workspaceID, roomID, stream, title, defaultRoomDispatchPolicy, now.Unix(), now.Unix(),
	)
	if err != nil {
		return agent.Room{}, fmt.Errorf("board: ensure room: %w", err)
	}
	meta, err := s.getRoomMetadata(ctx, workspaceID, roomID)
	if err != nil {
		return agent.Room{}, err
	}
	return roomMetadataToRoom(meta, nil), nil
}

func (s *boardSQLStore) ReplaceRoomMembers(ctx context.Context, workspaceID, roomID string, members []agent.RoomMember) ([]agent.RoomMember, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	roomID = strings.TrimSpace(roomID)
	if workspaceID == "" {
		return nil, fmt.Errorf("board: replace room members: workspace_id is required")
	}
	if roomID == "" {
		return nil, fmt.Errorf("board: replace room members: room id is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("board: replace room members begin tx: %w", err)
	}
	defer func() {
		errs.Ignore(tx.Rollback(), "rollback replace room members")
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM room_members WHERE workspace_id = ? AND room_id = ?`, workspaceID, roomID); err != nil {
		return nil, fmt.Errorf("board: delete room members: %w", err)
	}

	out := make([]agent.RoomMember, 0, len(members))
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		member.ActorID = strings.TrimSpace(member.ActorID)
		member.Role = strings.TrimSpace(member.Role)
		if member.ActorID == "" {
			continue
		}
		if _, ok := seen[member.ActorID]; ok {
			continue
		}
		seen[member.ActorID] = struct{}{}
		if member.JoinedAt.IsZero() {
			member.JoinedAt = time.Now().UTC()
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO room_members (workspace_id, room_id, actor_id, role, joined_at)
			VALUES (?, ?, ?, ?, ?)`,
			workspaceID, roomID, member.ActorID, member.Role, member.JoinedAt.Unix(),
		); err != nil {
			return nil, fmt.Errorf("board: insert room member: %w", err)
		}
		out = append(out, member)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE room_metadata
		SET updated_at = ?
		WHERE workspace_id = ? AND room_id = ?`, time.Now().UTC().Unix(), workspaceID, roomID); err != nil {
		return nil, fmt.Errorf("board: touch room metadata after members replace: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("board: replace room members commit: %w", err)
	}
	return out, nil
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
		WHERE workspace_id = ?`
	args := []any{filter.WorkspaceID}

	// Filter by recipient unless ActorID is empty (show all)
	if filter.ActorID != "" {
		query += ` AND (recipient = ? OR recipient = '*')`
		args = append(args, filter.ActorID)
	}

	if filter.TaskID != "" {
		query += ` AND (task_id = ? OR task_id = '')`
		args = append(args, filter.TaskID)
	}
	if filter.Stream != "" {
		query += ` AND stream = ?`
		args = append(args, filter.Stream)
	}
	// Status filtering
	// - OnlyUnsurfaced: strictly "unread" (never surfaced)
	// - OnlyUnread: "unread" + "surfaced" (not explicitly read yet)
	if filter.OnlyUnsurfaced {
		query += ` AND status = ?`
		args = append(args, agent.BoardMessageStatusUnread)
	} else if filter.OnlyUnread {
		query += ` AND status IN (?, ?)`
		args = append(args, agent.BoardMessageStatusUnread, agent.BoardMessageStatusSurfaced)
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

func (s *boardSQLStore) ListRooms(ctx context.Context, workspaceID, actorID string, limit int) ([]agent.RoomSummary, error) {
	if limit <= 0 {
		limit = 50
	}

	metas, err := s.listRoomMetadata(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	streams, err := s.listDerivedRoomStreams(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{}, len(metas)+len(streams))
	rooms := make([]agent.RoomSummary, 0, len(metas)+len(streams))
	for _, meta := range metas {
		summary, err := s.roomSummary(ctx, workspaceID, meta.Stream, actorID)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, summary)
		seen[summary.Stream] = struct{}{}
	}
	for _, stream := range streams {
		if _, ok := seen[stream]; ok {
			continue
		}
		summary, err := s.roomSummary(ctx, workspaceID, stream, actorID)
		if err != nil {
			return nil, err
		}
		rooms = append(rooms, summary)
	}

	sort.SliceStable(rooms, func(i, j int) bool {
		leftLatest := rooms[i].LatestMessageAt.Unix()
		rightLatest := rooms[j].LatestMessageAt.Unix()
		if leftLatest != rightLatest {
			return leftLatest > rightLatest
		}
		leftUpdated := rooms[i].UpdatedAt.Unix()
		rightUpdated := rooms[j].UpdatedAt.Unix()
		if leftUpdated != rightUpdated {
			return leftUpdated > rightUpdated
		}
		return rooms[i].Stream < rooms[j].Stream
	})
	if len(rooms) > limit {
		rooms = rooms[:limit]
	}
	return rooms, nil
}

func (s *boardSQLStore) GetRoom(ctx context.Context, workspaceID, roomID, actorID string) (agent.RoomSummary, error) {
	stream := agent.RoomStreamName(roomID)
	return s.roomSummary(ctx, workspaceID, stream, actorID)
}

func (s *boardSQLStore) ListRoomMessages(ctx context.Context, workspaceID, roomID string, limit int) ([]agent.BoardMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	stream := agent.RoomStreamName(roomID)

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_id, task_id, stream, sender, recipient, kind, priority, ack_required, status, subject, body, created_at
		FROM board_messages
		WHERE workspace_id = ? AND stream = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, workspaceID, stream, limit)
	if err != nil {
		return nil, fmt.Errorf("board: list room messages: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close room messages rows")
	}()

	var messages []agent.BoardMessage
	for rows.Next() {
		msg, err := scanBoardMessage(rows)
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	if len(messages) == 0 {
		if _, err := s.getRoomMetadata(ctx, workspaceID, roomID); err == nil {
			return []agent.BoardMessage{}, nil
		} else if !errors.Is(err, ErrRoomNotFound) {
			return nil, err
		}
		return nil, ErrRoomNotFound
	}

	// Convert the latest-first query into chronological order for transcript rendering.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// MarkRead marks messages as read.
func (s *boardSQLStore) MarkRead(ctx context.Context, workspaceID, _ string, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	// Build placeholders for IN clause
	placeholders := "?"
	for i := 1; i < len(messageIDs); i++ {
		placeholders += ", ?"
	}

	// Mark both "unread" and "surfaced" as "read" (explicit read action)
	args := []any{
		agent.BoardMessageStatusRead,
		workspaceID,
		agent.BoardMessageStatusUnread,
		agent.BoardMessageStatusSurfaced,
	}
	for _, id := range messageIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		UPDATE board_messages
		SET status = ?
		WHERE workspace_id = ? AND status IN (?, ?) AND id IN (%s)`, placeholders)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("board: mark read: %w", err)
	}
	// RowsAffected error is nil for SQLite.
	affected, _ := res.RowsAffected() //nolint:errcheck
	return int(affected), nil
}

// MarkSurfaced marks messages as surfaced (injected into AI context).
// This is used by hooks to suppress re-injecting the same messages forever,
// without claiming the user has explicitly read them.
func (s *boardSQLStore) MarkSurfaced(ctx context.Context, workspaceID, _ string, messageIDs []string) (int, error) {
	if len(messageIDs) == 0 {
		return 0, nil
	}

	placeholders := "?"
	for i := 1; i < len(messageIDs); i++ {
		placeholders += ", ?"
	}

	args := []any{
		agent.BoardMessageStatusSurfaced,
		workspaceID,
		agent.BoardMessageStatusUnread,
	}
	for _, id := range messageIDs {
		args = append(args, id)
	}

	query := fmt.Sprintf(`
		UPDATE board_messages
		SET status = ?
		WHERE workspace_id = ? AND status = ? AND id IN (%s)`, placeholders)

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("board: mark surfaced: %w", err)
	}
	affected, _ := res.RowsAffected() //nolint:errcheck
	return int(affected), nil
}

// AckMessages marks messages as acknowledged.
func (s *boardSQLStore) AckMessages(ctx context.Context, workspaceID, _ string, messageIDs []string) (int, error) {
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
	// RowsAffected error is nil for SQLite.
	affected, _ := res.RowsAffected() //nolint:errcheck
	return int(affected), nil
}

// Reserve creates a file reservation.
// The res.ID is populated with a ULID if not already set.
func (s *boardSQLStore) Reserve(ctx context.Context, res *agent.FileReservation) error {
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
	// Best-effort cleanup of expired reservations.
	_, _ = s.db.ExecContext(ctx, `DELETE FROM file_reservations WHERE expires_at < ?`, now) //nolint:errcheck

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
	// RowsAffected error is nil for SQLite.
	affected, _ := res.RowsAffected() //nolint:errcheck
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
	// RowsAffected error is nil for SQLite.
	affected, _ := res.RowsAffected() //nolint:errcheck
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

CREATE TABLE IF NOT EXISTS room_metadata (
	workspace_id TEXT NOT NULL,
	room_id      TEXT NOT NULL,
	stream       TEXT NOT NULL,
	title        TEXT NOT NULL,
	description  TEXT NOT NULL DEFAULT '',
	dispatch_policy TEXT NOT NULL DEFAULT 'all_subtree',
	dispatch_agent_ids TEXT NOT NULL DEFAULT '[]',
	created_at   INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL,
	PRIMARY KEY (workspace_id, room_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_room_metadata_workspace_stream ON room_metadata(workspace_id, stream);
CREATE INDEX IF NOT EXISTS idx_room_metadata_workspace_updated ON room_metadata(workspace_id, updated_at);

CREATE TABLE IF NOT EXISTS room_members (
	workspace_id TEXT NOT NULL,
	room_id      TEXT NOT NULL,
	actor_id     TEXT NOT NULL,
	role         TEXT NOT NULL DEFAULT '',
	joined_at    INTEGER NOT NULL,
	PRIMARY KEY (workspace_id, room_id, actor_id)
);
CREATE INDEX IF NOT EXISTS idx_room_members_workspace_room ON room_members(workspace_id, room_id);

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
	for _, stmt := range []string{
		`ALTER TABLE room_metadata ADD COLUMN dispatch_policy TEXT NOT NULL DEFAULT 'all_subtree'`,
		`ALTER TABLE room_metadata ADD COLUMN dispatch_agent_ids TEXT NOT NULL DEFAULT '[]'`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			errMsg := strings.ToLower(strings.TrimSpace(err.Error()))
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				return fmt.Errorf("board: migrate room metadata columns: %w", err)
			}
		}
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
var (
	ErrReservationConflict = errors.New("board: reservation conflict")
	ErrRoomNotFound        = errors.New("board: room not found")
)

type roomMetadataRow struct {
	WorkspaceID      string
	RoomID           string
	Stream           string
	Title            string
	Description      string
	DispatchPolicy   string
	DispatchAgentIDs []string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func normalizeRoomDispatchPolicy(policy string) string {
	switch strings.TrimSpace(policy) {
	case "children_only", "lead_only", "selected":
		return strings.TrimSpace(policy)
	default:
		return defaultRoomDispatchPolicy
	}
}

func normalizeRoomDispatchAgentIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// CountMessagesByTask counts unread messages per task grouped by sender type.
func (s *boardSQLStore) CountMessagesByTask(ctx context.Context, workspaceID, taskID string) (admin, overseer, total int, err error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT sender FROM board_messages
		WHERE workspace_id = ? AND task_id = ? AND status IN (?, ?)`,
		workspaceID, taskID, agent.BoardMessageStatusUnread, agent.BoardMessageStatusSurfaced)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("board: count messages: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close count messages rows")
	}()

	for rows.Next() {
		var sender string
		if err := rows.Scan(&sender); err != nil {
			return 0, 0, 0, fmt.Errorf("board: scan sender: %w", err)
		}
		total++
		if agent.IsAdminSender(sender) {
			admin++
		} else if agent.IsOverseerSender(sender) {
			overseer++
		}
	}
	return admin, overseer, total, nil
}

func (s *boardSQLStore) roomSummary(ctx context.Context, workspaceID, stream, actorID string) (agent.RoomSummary, error) {
	meta, metaErr := s.getRoomMetadataByStream(ctx, workspaceID, stream)
	hasMeta := metaErr == nil
	if metaErr != nil && !errors.Is(metaErr, ErrRoomNotFound) {
		return agent.RoomSummary{}, metaErr
	}

	lastMessage, err := s.lastRoomMessage(ctx, workspaceID, stream)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) && !hasMeta {
			return agent.RoomSummary{}, ErrRoomNotFound
		} else if errors.Is(err, sql.ErrNoRows) {
			lastMessage = agent.BoardMessage{WorkspaceID: workspaceID, Stream: stream}
		} else {
			return agent.RoomSummary{}, err
		}
	}

	messageCount, unreadCount, err := s.roomCounts(ctx, workspaceID, stream, actorID)
	if err != nil {
		return agent.RoomSummary{}, err
	}
	participants, err := s.roomParticipants(ctx, workspaceID, stream)
	if err != nil {
		return agent.RoomSummary{}, err
	}
	taskIDs, err := s.roomTaskIDs(ctx, workspaceID, stream)
	if err != nil {
		return agent.RoomSummary{}, err
	}
	members, err := s.listRoomMembers(ctx, workspaceID, agent.RoomIDFromStream(stream))
	if err != nil {
		return agent.RoomSummary{}, err
	}

	roomID := agent.RoomIDFromStream(stream)
	if hasMeta && meta.RoomID != "" {
		roomID = meta.RoomID
	}
	title := strings.TrimSpace(lastMessage.Subject)
	if hasMeta && strings.TrimSpace(meta.Title) != "" {
		title = strings.TrimSpace(meta.Title)
	}
	if title == "" {
		title = roomID
	}
	description := ""
	var createdAt time.Time
	updatedAt := lastMessage.CreatedAt
	if hasMeta {
		description = meta.Description
		createdAt = meta.CreatedAt
		updatedAt = meta.UpdatedAt
		if lastMessage.CreatedAt.After(updatedAt) {
			updatedAt = lastMessage.CreatedAt
		}
	} else {
		createdAt = lastMessage.CreatedAt
	}

	participants = mergeParticipantLists(participants, members)

	return agent.RoomSummary{
		ID:               roomID,
		WorkspaceID:      workspaceID,
		Stream:           stream,
		Title:            title,
		Description:      description,
		DispatchPolicy:   normalizeRoomDispatchPolicy(meta.DispatchPolicy),
		DispatchAgentIDs: append([]string(nil), meta.DispatchAgentIDs...),
		CreatedAt:        createdAt,
		UpdatedAt:        updatedAt,
		LatestSubject:    lastMessage.Subject,
		LatestPreview:    summarizeRoomPreview(lastMessage.Body),
		LatestSender:     lastMessage.Sender,
		LatestMessageAt:  lastMessage.CreatedAt,
		MessageCount:     messageCount,
		UnreadCount:      unreadCount,
		Participants:     participants,
		TaskIDs:          taskIDs,
		Members:          members,
	}, nil
}

func (s *boardSQLStore) lastRoomMessage(ctx context.Context, workspaceID, stream string) (agent.BoardMessage, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_id, task_id, stream, sender, recipient, kind, priority, ack_required, status, subject, body, created_at
		FROM board_messages
		WHERE workspace_id = ? AND stream = ?
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, workspaceID, stream)
	if err != nil {
		return agent.BoardMessage{}, fmt.Errorf("board: last room message: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close last room message rows")
	}()
	if !rows.Next() {
		return agent.BoardMessage{}, sql.ErrNoRows
	}
	return scanBoardMessage(rows)
}

func (s *boardSQLStore) roomCounts(ctx context.Context, workspaceID, stream, actorID string) (int, int, error) {
	var query string
	var args []any
	if strings.TrimSpace(actorID) != "" {
		query = `
			SELECT COUNT(*),
			       SUM(CASE WHEN status IN (?, ?) AND (recipient = ? OR recipient = '*') THEN 1 ELSE 0 END)
			FROM board_messages
			WHERE workspace_id = ? AND stream = ?`
		args = []any{
			agent.BoardMessageStatusUnread,
			agent.BoardMessageStatusSurfaced,
			actorID,
			workspaceID,
			stream,
		}
	} else {
		query = `
			SELECT COUNT(*),
			       SUM(CASE WHEN status IN (?, ?) THEN 1 ELSE 0 END)
			FROM board_messages
			WHERE workspace_id = ? AND stream = ?`
		args = []any{
			agent.BoardMessageStatusUnread,
			agent.BoardMessageStatusSurfaced,
			workspaceID,
			stream,
		}
	}

	var messageCount int
	var unreadCount sql.NullInt64
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&messageCount, &unreadCount); err != nil {
		return 0, 0, fmt.Errorf("board: room counts: %w", err)
	}
	return messageCount, int(unreadCount.Int64), nil
}

func (s *boardSQLStore) roomParticipants(ctx context.Context, workspaceID, stream string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT participant
		FROM (
			SELECT sender AS participant FROM board_messages WHERE workspace_id = ? AND stream = ?
			UNION
			SELECT recipient AS participant FROM board_messages WHERE workspace_id = ? AND stream = ?
		)
		WHERE participant != '' AND participant != '*'
		ORDER BY participant ASC`, workspaceID, stream, workspaceID, stream)
	if err != nil {
		return nil, fmt.Errorf("board: room participants: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close room participants rows")
	}()

	var participants []string
	for rows.Next() {
		var participant string
		if err := rows.Scan(&participant); err != nil {
			return nil, fmt.Errorf("board: scan room participant: %w", err)
		}
		participants = append(participants, participant)
	}
	return participants, nil
}

func (s *boardSQLStore) roomTaskIDs(ctx context.Context, workspaceID, stream string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT task_id
		FROM board_messages
		WHERE workspace_id = ? AND stream = ? AND task_id != ''
		ORDER BY task_id ASC`, workspaceID, stream)
	if err != nil {
		return nil, fmt.Errorf("board: room task ids: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close room task ids rows")
	}()

	var taskIDs []string
	for rows.Next() {
		var taskID string
		if err := rows.Scan(&taskID); err != nil {
			return nil, fmt.Errorf("board: scan room task id: %w", err)
		}
		taskIDs = append(taskIDs, taskID)
	}
	return taskIDs, nil
}

func summarizeRoomPreview(body string) string {
	body = strings.TrimSpace(body)
	if len(body) <= 140 {
		return body
	}
	return body[:140] + "..."
}

func (s *boardSQLStore) listDerivedRoomStreams(ctx context.Context, workspaceID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT stream
		FROM board_messages
		WHERE workspace_id = ? AND stream LIKE ?
		GROUP BY stream`, workspaceID, agent.RoomStreamPrefix+"%")
	if err != nil {
		return nil, fmt.Errorf("board: list derived room streams: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close derived room streams rows")
	}()

	var streams []string
	for rows.Next() {
		var stream string
		if err := rows.Scan(&stream); err != nil {
			return nil, fmt.Errorf("board: scan derived room stream: %w", err)
		}
		streams = append(streams, stream)
	}
	return streams, nil
}

func (s *boardSQLStore) listRoomMetadata(ctx context.Context, workspaceID string) ([]roomMetadataRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT workspace_id, room_id, stream, title, description, dispatch_policy, dispatch_agent_ids, created_at, updated_at
		FROM room_metadata
		WHERE workspace_id = ?
		ORDER BY updated_at DESC, room_id ASC`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("board: list room metadata: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close room metadata rows")
	}()

	var metas []roomMetadataRow
	for rows.Next() {
		meta, err := scanRoomMetadataRow(rows)
		if err != nil {
			return nil, err
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

func (s *boardSQLStore) getRoomMetadata(ctx context.Context, workspaceID, roomID string) (roomMetadataRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT workspace_id, room_id, stream, title, description, dispatch_policy, dispatch_agent_ids, created_at, updated_at
		FROM room_metadata
		WHERE workspace_id = ? AND room_id = ?`, workspaceID, roomID)
	return scanRoomMetadataRow(row)
}

func (s *boardSQLStore) getRoomMetadataByStream(ctx context.Context, workspaceID, stream string) (roomMetadataRow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT workspace_id, room_id, stream, title, description, dispatch_policy, dispatch_agent_ids, created_at, updated_at
		FROM room_metadata
		WHERE workspace_id = ? AND stream = ?`, workspaceID, stream)
	return scanRoomMetadataRow(row)
}

func (s *boardSQLStore) listRoomMembers(ctx context.Context, workspaceID, roomID string) ([]agent.RoomMember, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT actor_id, role, joined_at
		FROM room_members
		WHERE workspace_id = ? AND room_id = ?
		ORDER BY joined_at ASC, actor_id ASC`, workspaceID, roomID)
	if err != nil {
		return nil, fmt.Errorf("board: list room members: %w", err)
	}
	defer func() {
		errs.Ignore(rows.Close(), "close room members rows")
	}()

	var members []agent.RoomMember
	for rows.Next() {
		var actorID, role string
		var joinedAt int64
		if err := rows.Scan(&actorID, &role, &joinedAt); err != nil {
			return nil, fmt.Errorf("board: scan room member: %w", err)
		}
		members = append(members, agent.RoomMember{
			ActorID:  actorID,
			Role:     role,
			JoinedAt: time.Unix(joinedAt, 0).UTC(),
		})
	}
	return members, nil
}

func scanRoomMetadataRow(scanner interface{ Scan(dest ...any) error }) (roomMetadataRow, error) {
	var meta roomMetadataRow
	var createdAt, updatedAt int64
	var dispatchAgentIDsJSON string
	if err := scanner.Scan(&meta.WorkspaceID, &meta.RoomID, &meta.Stream, &meta.Title, &meta.Description, &meta.DispatchPolicy, &dispatchAgentIDsJSON, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return roomMetadataRow{}, ErrRoomNotFound
		}
		return roomMetadataRow{}, fmt.Errorf("board: scan room metadata: %w", err)
	}
	meta.DispatchPolicy = normalizeRoomDispatchPolicy(meta.DispatchPolicy)
	if strings.TrimSpace(dispatchAgentIDsJSON) != "" {
		if err := json.Unmarshal([]byte(dispatchAgentIDsJSON), &meta.DispatchAgentIDs); err != nil {
			return roomMetadataRow{}, fmt.Errorf("board: decode room dispatch ids: %w", err)
		}
	}
	meta.DispatchAgentIDs = normalizeRoomDispatchAgentIDs(meta.DispatchAgentIDs)
	meta.CreatedAt = time.Unix(createdAt, 0).UTC()
	meta.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	return meta, nil
}

func roomMetadataToRoom(meta roomMetadataRow, members []agent.RoomMember) agent.Room {
	return agent.Room{
		ID:               meta.RoomID,
		WorkspaceID:      meta.WorkspaceID,
		Stream:           meta.Stream,
		Title:            meta.Title,
		Description:      meta.Description,
		DispatchPolicy:   meta.DispatchPolicy,
		DispatchAgentIDs: append([]string(nil), meta.DispatchAgentIDs...),
		CreatedAt:        meta.CreatedAt,
		UpdatedAt:        meta.UpdatedAt,
		Members:          members,
	}
}

func mergeParticipantLists(derived []string, members []agent.RoomMember) []string {
	seen := make(map[string]struct{}, len(derived)+len(members))
	out := make([]string, 0, len(derived)+len(members))
	for _, member := range members {
		if member.ActorID == "" {
			continue
		}
		if _, ok := seen[member.ActorID]; ok {
			continue
		}
		seen[member.ActorID] = struct{}{}
		out = append(out, member.ActorID)
	}
	for _, participant := range derived {
		if participant == "" {
			continue
		}
		if _, ok := seen[participant]; ok {
			continue
		}
		seen[participant] = struct{}{}
		out = append(out, participant)
	}
	return out
}
