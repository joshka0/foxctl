package coordination

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	workspaceutil "github.com/joshka0/foxctl/internal/platform/workspace"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

// Store provides coordination primitives (leader leases) backed by coordination.db.
type Store struct {
	db    *sql.DB
	close func() error
}

// Lease describes a named leader lease.
type Lease struct {
	Name      string
	OwnerID   string
	ExpiresAt time.Time
	UpdatedAt time.Time
}

// RoomLoop stores persisted runtime/policy state for one room loop.
type RoomLoop struct {
	WorkspaceID                  string
	RoomID                       string
	Enabled                      bool
	ManagedBy                    string
	LastTickAt                   *time.Time
	DeliveryLeaseName            string
	DeliveryOwnerID              string
	DeliveryCursorMessageID      string
	DeliveryCursorAt             *time.Time
	LastDeliveryTrace            *RoomLoopDeliveryTrace
	ReplyPulseState              map[string]RoomLoopPulseState
	TaskPulseState               map[string]RoomLoopPulseState
	TaskFollowupState            map[string]time.Time
	CoordinatorPulseState        map[string]time.Time
	PulseInterval                time.Duration
	TaskFollowupInterval         time.Duration
	ReplyStaleAfter              time.Duration
	TaskStaleAfter               time.Duration
	MinPulseFloor                time.Duration
	InterruptAttemptLimit        int
	ReminderBackoffCap           int
	CoordinatorPulseEnabled      bool
	CoordinatorEscalationEnabled bool
	UpdatedAt                    time.Time
}

type RoomLoopDeliveryTrace struct {
	WorkspaceID             string    `json:"workspace_id,omitempty"`
	RoomID                  string    `json:"room_id,omitempty"`
	MessageID               string    `json:"message_id,omitempty"`
	TaskID                  string    `json:"task_id,omitempty"`
	Recipient               string    `json:"recipient,omitempty"`
	DeliveryLeaseName       string    `json:"delivery_lease_name,omitempty"`
	DeliveryOwnerID         string    `json:"delivery_owner_id,omitempty"`
	RelayBackend            string    `json:"relay_backend,omitempty"`
	ChosenActorID           string    `json:"chosen_actor_id,omitempty"`
	ChosenMuxBackend        string    `json:"chosen_mux_backend,omitempty"`
	ChosenMuxSession        string    `json:"chosen_mux_session,omitempty"`
	ChosenMuxPaneID         string    `json:"chosen_mux_pane_id,omitempty"`
	ChosenTransportEndpoint string    `json:"chosen_transport_endpoint,omitempty"`
	ChosenTransportKind     string    `json:"chosen_transport_kind,omitempty"`
	ChosenSubmitMode        string    `json:"chosen_submit_mode,omitempty"`
	FallbackAttempted       bool      `json:"fallback_attempted,omitempty"`
	DeliveredCount          int       `json:"delivered_count,omitempty"`
	FailedCount             int       `json:"failed_count,omitempty"`
	DeliveredTo             []string  `json:"delivered_to,omitempty"`
	FailedMembers           []string  `json:"failed_members,omitempty"`
	Outcome                 string    `json:"outcome,omitempty"`
	CursorBeforeMessageID   string    `json:"cursor_before_message_id,omitempty"`
	CursorAfterMessageID    string    `json:"cursor_after_message_id,omitempty"`
	CursorAdvanced          bool      `json:"cursor_advanced,omitempty"`
	ObservedAt              time.Time `json:"observed_at,omitempty"`
}

type RoomLoopPulseState struct {
	LastSentAt *time.Time `json:"last_sent_at,omitempty"`
	Count      int        `json:"count,omitempty"`
	Escalated  bool       `json:"escalated,omitempty"`
}

// RoomReminder stores one durable scheduled room follow-up.
type RoomReminder struct {
	ID            string        `json:"id"`
	WorkspaceID   string        `json:"workspace_id"`
	RoomID        string        `json:"room_id"`
	RootMessageID string        `json:"root_message_id"`
	TaskID        string        `json:"task_id,omitempty"`
	StoryID       string        `json:"story_id,omitempty"`
	MilestoneID   string        `json:"milestone_id,omitempty"`
	Sender        string        `json:"sender"`
	Recipient     string        `json:"recipient"`
	Subject       string        `json:"subject"`
	Body          string        `json:"body"`
	AckRequired   bool          `json:"ack_required"`
	ReplyExpected bool          `json:"reply_expected"`
	Interrupt     bool          `json:"interrupt"`
	Passive       bool          `json:"passive"`
	Interval      time.Duration `json:"interval"`
	MaxIterations int           `json:"max_iterations"`
	SentCount     int           `json:"sent_count"`
	Active        bool          `json:"active"`
	LastSentAt    *time.Time    `json:"last_sent_at,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

// Open opens the coordination store rooted at storageRoot/coordination.db.
// The database driver is selected via the dbdriver env var conventions (e.g., FOXCTL_COORDINATION_DB_DRIVER).
//
// Index:
//   Purpose: Provide a stable coordination store for single-leader daemon leases
//   Keywords: coordination, leader_lease, daemon, single_leader
//   Related: Store.TryAcquireLease, Store.ReleaseLease
//   Flow: open db via dbutil → migrate schema → return store
//   Resources: coordination.db, daemon_leases table, room_loops table, room_reminders table
//   Events: none
//   OutputFields: *Store
// [[protocol:leader-lease-semantics]]
// [[invariant:lease-expires-at-enforced-by-upsert-conditional]]
func Open(ctx context.Context, storageRoot string) (*Store, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, storageRoot, "COORDINATION", "coordination.db", migrate)
	if err != nil {
		return nil, fmt.Errorf("coordination: open db: %w", err)
	}
	return &Store{db: db, close: closeFn}, nil
}

// Close releases store resources.
func (s *Store) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// MigrateSchema runs the coordination store DDL migrations against the given database.
func MigrateSchema(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db)
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS daemon_leases (
	name           TEXT PRIMARY KEY,
	owner_id       TEXT NOT NULL,
	expires_at_ms  INTEGER NOT NULL,
	updated_at_ms  INTEGER NOT NULL,
	created_at_ms  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_daemon_leases_expires ON daemon_leases(expires_at_ms);

CREATE TABLE IF NOT EXISTS room_loops (
	workspace_id                TEXT NOT NULL,
	room_id                     TEXT NOT NULL,
	enabled                     INTEGER NOT NULL DEFAULT 1,
	managed_by                  TEXT NOT NULL DEFAULT '',
	last_tick_at_ms             INTEGER,
	delivery_lease_name         TEXT NOT NULL DEFAULT '',
	delivery_owner_id           TEXT NOT NULL DEFAULT '',
	delivery_cursor_message_id  TEXT NOT NULL DEFAULT '',
	delivery_cursor_at_ms       INTEGER,
	last_delivery_trace_json    TEXT NOT NULL DEFAULT '',
	reply_pulse_state_json      TEXT NOT NULL DEFAULT '',
	task_pulse_state_json       TEXT NOT NULL DEFAULT '',
	task_followup_state_json    TEXT NOT NULL DEFAULT '',
	coordinator_pulse_state_json TEXT NOT NULL DEFAULT '',
	pulse_interval_ms           INTEGER NOT NULL,
	task_followup_interval_ms   INTEGER NOT NULL DEFAULT 0,
	reply_stale_after_ms        INTEGER NOT NULL,
	task_stale_after_ms         INTEGER NOT NULL,
	min_pulse_floor_ms          INTEGER NOT NULL,
	interrupt_attempt_limit     INTEGER NOT NULL DEFAULT 2,
	reminder_backoff_cap        INTEGER NOT NULL DEFAULT 8,
	coordinator_pulse_enabled   INTEGER NOT NULL DEFAULT 1,
	coordinator_escalation_enabled INTEGER NOT NULL DEFAULT 1,
	updated_at_ms               INTEGER NOT NULL,
	created_at_ms               INTEGER NOT NULL,
	PRIMARY KEY (workspace_id, room_id)
);

CREATE TABLE IF NOT EXISTS room_reminders (
	id                TEXT PRIMARY KEY,
	workspace_id      TEXT NOT NULL,
	room_id           TEXT NOT NULL,
	root_message_id   TEXT NOT NULL,
	task_id           TEXT NOT NULL DEFAULT '',
	story_id          TEXT NOT NULL DEFAULT '',
	milestone_id      TEXT NOT NULL DEFAULT '',
	sender            TEXT NOT NULL,
	recipient         TEXT NOT NULL,
	subject           TEXT NOT NULL,
	body              TEXT NOT NULL,
	ack_required      INTEGER NOT NULL DEFAULT 0,
	reply_expected    INTEGER NOT NULL DEFAULT 0,
	interrupt         INTEGER NOT NULL DEFAULT 0,
	passive           INTEGER NOT NULL DEFAULT 0,
	interval_ms       INTEGER NOT NULL,
	max_iterations    INTEGER NOT NULL,
	sent_count        INTEGER NOT NULL DEFAULT 0,
	active            INTEGER NOT NULL DEFAULT 1,
	last_sent_at_ms   INTEGER,
	created_at_ms     INTEGER NOT NULL,
	updated_at_ms     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_room_reminders_room ON room_reminders(workspace_id, room_id, active, updated_at_ms);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("coordination: migrate: %w", err)
	}
	for _, stmt := range []string{
		`ALTER TABLE room_reminders ADD COLUMN task_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE room_reminders ADD COLUMN story_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE room_reminders ADD COLUMN milestone_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE room_reminders ADD COLUMN passive INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE room_loops ADD COLUMN interrupt_attempt_limit INTEGER NOT NULL DEFAULT 2`,
		`ALTER TABLE room_loops ADD COLUMN reminder_backoff_cap INTEGER NOT NULL DEFAULT 8`,
		`ALTER TABLE room_loops ADD COLUMN coordinator_escalation_enabled INTEGER NOT NULL DEFAULT 1`,
		`ALTER TABLE room_loops ADD COLUMN task_followup_interval_ms INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE room_loops ADD COLUMN delivery_lease_name TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE room_loops ADD COLUMN delivery_owner_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE room_loops ADD COLUMN delivery_cursor_message_id TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE room_loops ADD COLUMN delivery_cursor_at_ms INTEGER`,
		`ALTER TABLE room_loops ADD COLUMN last_delivery_trace_json TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE room_loops ADD COLUMN reply_pulse_state_json TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE room_loops ADD COLUMN task_pulse_state_json TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE room_loops ADD COLUMN task_followup_state_json TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE room_loops ADD COLUMN coordinator_pulse_state_json TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			errMsg := strings.ToLower(strings.TrimSpace(err.Error()))
			if !strings.Contains(errMsg, "duplicate column") && !strings.Contains(errMsg, "already exists") {
				return fmt.Errorf("coordination: migrate room loop columns: %w", err)
			}
		}
	}
	return nil
}

// GetRoomReminder returns one persisted reminder by id.
func (s *Store) GetRoomReminder(ctx context.Context, workspaceID, reminderID string) (*RoomReminder, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("coordination: store not initialized")
	}
	workspaceID = workspaceutil.CanonicalWorkspaceKey(workspaceID)
	reminderID = strings.TrimSpace(reminderID)
	if workspaceID == "" || reminderID == "" {
		return nil, fmt.Errorf("coordination: workspace_id and reminder id are required")
	}
	var reminder RoomReminder
	var (
		lastSentMS sql.NullInt64
		createdMS  int64
		updatedMS  int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, room_id, root_message_id, task_id, story_id, milestone_id, sender, recipient, subject, body,
		       ack_required, reply_expected, interrupt, passive, interval_ms, max_iterations, sent_count,
		       active, last_sent_at_ms, created_at_ms, updated_at_ms
		FROM room_reminders
		WHERE workspace_id = $1 AND id = $2
	`, workspaceID, reminderID).Scan(
		&reminder.ID,
		&reminder.WorkspaceID,
		&reminder.RoomID,
		&reminder.RootMessageID,
		&reminder.TaskID,
		&reminder.StoryID,
		&reminder.MilestoneID,
		&reminder.Sender,
		&reminder.Recipient,
		&reminder.Subject,
		&reminder.Body,
		(*intBool)(&reminder.AckRequired),
		(*intBool)(&reminder.ReplyExpected),
		(*intBool)(&reminder.Interrupt),
		(*intBool)(&reminder.Passive),
		(*durationMillis)(&reminder.Interval),
		&reminder.MaxIterations,
		&reminder.SentCount,
		(*intBool)(&reminder.Active),
		&lastSentMS,
		&createdMS,
		&updatedMS,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("coordination: get room reminder: %w", err)
	}
	if lastSentMS.Valid {
		ts := time.UnixMilli(lastSentMS.Int64).UTC()
		reminder.LastSentAt = &ts
	}
	reminder.CreatedAt = time.UnixMilli(createdMS).UTC()
	reminder.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return &reminder, nil
}

// ListRoomReminders lists persisted reminders for one room.
func (s *Store) ListRoomReminders(ctx context.Context, workspaceID, roomID string, includeInactive bool) ([]RoomReminder, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("coordination: store not initialized")
	}
	workspaceID = workspaceutil.CanonicalWorkspaceKey(workspaceID)
	roomID = strings.TrimSpace(roomID)
	if workspaceID == "" || roomID == "" {
		return nil, fmt.Errorf("coordination: workspace_id and room_id are required")
	}
	query := `
		SELECT id, workspace_id, room_id, root_message_id, task_id, story_id, milestone_id, sender, recipient, subject, body,
		       ack_required, reply_expected, interrupt, passive, interval_ms, max_iterations, sent_count,
		       active, last_sent_at_ms, created_at_ms, updated_at_ms
		FROM room_reminders
		WHERE workspace_id = $1 AND room_id = $2`
	if !includeInactive {
		query += ` AND active = 1`
	}
	query += ` ORDER BY created_at_ms ASC`
	rows, err := s.db.QueryContext(ctx, query, workspaceID, roomID)
	if err != nil {
		return nil, fmt.Errorf("coordination: list room reminders: %w", err)
	}
	defer rows.Close()
	var reminders []RoomReminder
	for rows.Next() {
		var reminder RoomReminder
		var (
			lastSentMS sql.NullInt64
			createdMS  int64
			updatedMS  int64
		)
		if err := rows.Scan(
			&reminder.ID,
			&reminder.WorkspaceID,
			&reminder.RoomID,
			&reminder.RootMessageID,
			&reminder.TaskID,
			&reminder.StoryID,
			&reminder.MilestoneID,
			&reminder.Sender,
			&reminder.Recipient,
			&reminder.Subject,
			&reminder.Body,
			(*intBool)(&reminder.AckRequired),
			(*intBool)(&reminder.ReplyExpected),
			(*intBool)(&reminder.Interrupt),
			(*intBool)(&reminder.Passive),
			(*durationMillis)(&reminder.Interval),
			&reminder.MaxIterations,
			&reminder.SentCount,
			(*intBool)(&reminder.Active),
			&lastSentMS,
			&createdMS,
			&updatedMS,
		); err != nil {
			return nil, fmt.Errorf("coordination: scan room reminder: %w", err)
		}
		if lastSentMS.Valid {
			ts := time.UnixMilli(lastSentMS.Int64).UTC()
			reminder.LastSentAt = &ts
		}
		reminder.CreatedAt = time.UnixMilli(createdMS).UTC()
		reminder.UpdatedAt = time.UnixMilli(updatedMS).UTC()
		reminders = append(reminders, reminder)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("coordination: iterate room reminders: %w", err)
	}
	return reminders, nil
}

// UpsertRoomReminder inserts or updates one persisted room reminder.
func (s *Store) UpsertRoomReminder(ctx context.Context, reminder RoomReminder) (RoomReminder, error) {
	if s == nil || s.db == nil {
		return RoomReminder{}, fmt.Errorf("coordination: store not initialized")
	}
	reminder.ID = strings.TrimSpace(reminder.ID)
	reminder.WorkspaceID = workspaceutil.CanonicalWorkspaceKey(reminder.WorkspaceID)
	reminder.RoomID = strings.TrimSpace(reminder.RoomID)
	reminder.RootMessageID = strings.TrimSpace(reminder.RootMessageID)
	reminder.TaskID = strings.TrimSpace(reminder.TaskID)
	reminder.StoryID = strings.TrimSpace(reminder.StoryID)
	reminder.MilestoneID = strings.TrimSpace(reminder.MilestoneID)
	reminder.Sender = strings.TrimSpace(reminder.Sender)
	reminder.Recipient = strings.TrimSpace(reminder.Recipient)
	reminder.Subject = strings.TrimSpace(reminder.Subject)
	reminder.Body = strings.TrimSpace(reminder.Body)
	if reminder.ID == "" || reminder.WorkspaceID == "" || reminder.RoomID == "" || reminder.RootMessageID == "" {
		return RoomReminder{}, fmt.Errorf("coordination: reminder id, workspace_id, room_id, and root_message_id are required")
	}
	if reminder.Recipient == "" {
		return RoomReminder{}, fmt.Errorf("coordination: recipient is required")
	}
	if reminder.Interval <= 0 {
		return RoomReminder{}, fmt.Errorf("coordination: interval must be positive")
	}
	if reminder.MaxIterations <= 0 {
		return RoomReminder{}, fmt.Errorf("coordination: max_iterations must be positive")
	}
	now := time.Now().UTC()
	if reminder.CreatedAt.IsZero() {
		reminder.CreatedAt = now
	}
	reminder.UpdatedAt = now
	var lastSent any
	if reminder.LastSentAt != nil && !reminder.LastSentAt.IsZero() {
		lastSent = reminder.LastSentAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO room_reminders (
			id, workspace_id, room_id, root_message_id, task_id, story_id, milestone_id, sender, recipient, subject, body,
			ack_required, reply_expected, interrupt, passive, interval_ms, max_iterations, sent_count,
			active, last_sent_at_ms, created_at_ms, updated_at_ms
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)
		ON CONFLICT(id) DO UPDATE SET
			workspace_id = excluded.workspace_id,
			room_id = excluded.room_id,
			root_message_id = excluded.root_message_id,
			task_id = excluded.task_id,
			story_id = excluded.story_id,
			milestone_id = excluded.milestone_id,
			sender = excluded.sender,
			recipient = excluded.recipient,
			subject = excluded.subject,
			body = excluded.body,
			ack_required = excluded.ack_required,
			reply_expected = excluded.reply_expected,
			interrupt = excluded.interrupt,
			passive = excluded.passive,
			interval_ms = excluded.interval_ms,
			max_iterations = excluded.max_iterations,
			sent_count = excluded.sent_count,
			active = excluded.active,
			last_sent_at_ms = excluded.last_sent_at_ms,
			updated_at_ms = excluded.updated_at_ms
	`,
		reminder.ID,
		reminder.WorkspaceID,
		reminder.RoomID,
		reminder.RootMessageID,
		reminder.TaskID,
		reminder.StoryID,
		reminder.MilestoneID,
		reminder.Sender,
		reminder.Recipient,
		reminder.Subject,
		reminder.Body,
		boolToIntCoord(reminder.AckRequired),
		boolToIntCoord(reminder.ReplyExpected),
		boolToIntCoord(reminder.Interrupt),
		boolToIntCoord(reminder.Passive),
		reminder.Interval.Milliseconds(),
		reminder.MaxIterations,
		reminder.SentCount,
		boolToIntCoord(reminder.Active),
		lastSent,
		reminder.CreatedAt.UnixMilli(),
		reminder.UpdatedAt.UnixMilli(),
	)
	if err != nil {
		return RoomReminder{}, fmt.Errorf("coordination: upsert room reminder: %w", err)
	}
	return reminder, nil
}

// GetRoomLoop returns the persisted loop policy/runtime state for one room.
func (s *Store) GetRoomLoop(ctx context.Context, workspaceID, roomID string) (*RoomLoop, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("coordination: store not initialized")
	}
	workspaceID = workspaceutil.CanonicalWorkspaceKey(workspaceID)
	roomID = strings.TrimSpace(roomID)
	if workspaceID == "" || roomID == "" {
		return nil, fmt.Errorf("coordination: workspace_id and room_id are required")
	}

	var loop RoomLoop
	var (
		lastTickMS                sql.NullInt64
		cursorAtMS                sql.NullInt64
		lastDeliveryTraceJSON     string
		replyPulseStateJSON       string
		taskPulseStateJSON        string
		taskFollowupStateJSON     string
		coordinatorPulseStateJSON string
		updatedMS                 int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT workspace_id, room_id, enabled, managed_by, last_tick_at_ms,
		       delivery_lease_name, delivery_owner_id, delivery_cursor_message_id, delivery_cursor_at_ms,
		       last_delivery_trace_json,
		       reply_pulse_state_json, task_pulse_state_json, task_followup_state_json, coordinator_pulse_state_json,
		       pulse_interval_ms, task_followup_interval_ms, reply_stale_after_ms, task_stale_after_ms,
		       min_pulse_floor_ms, interrupt_attempt_limit, reminder_backoff_cap,
		       coordinator_pulse_enabled, coordinator_escalation_enabled, updated_at_ms
		FROM room_loops
		WHERE workspace_id = $1 AND room_id = $2
	`, workspaceID, roomID).Scan(
		&loop.WorkspaceID,
		&loop.RoomID,
		(*intBool)(&loop.Enabled),
		&loop.ManagedBy,
		&lastTickMS,
		&loop.DeliveryLeaseName,
		&loop.DeliveryOwnerID,
		&loop.DeliveryCursorMessageID,
		&cursorAtMS,
		&lastDeliveryTraceJSON,
		&replyPulseStateJSON,
		&taskPulseStateJSON,
		&taskFollowupStateJSON,
		&coordinatorPulseStateJSON,
		(*durationMillis)(&loop.PulseInterval),
		(*durationMillis)(&loop.TaskFollowupInterval),
		(*durationMillis)(&loop.ReplyStaleAfter),
		(*durationMillis)(&loop.TaskStaleAfter),
		(*durationMillis)(&loop.MinPulseFloor),
		&loop.InterruptAttemptLimit,
		&loop.ReminderBackoffCap,
		(*intBool)(&loop.CoordinatorPulseEnabled),
		(*intBool)(&loop.CoordinatorEscalationEnabled),
		&updatedMS,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("coordination: get room loop: %w", err)
	}
	if lastTickMS.Valid {
		ts := time.UnixMilli(lastTickMS.Int64).UTC()
		loop.LastTickAt = &ts
	}
	if cursorAtMS.Valid {
		ts := time.UnixMilli(cursorAtMS.Int64).UTC()
		loop.DeliveryCursorAt = &ts
	}
	if err := decodeRoomLoopDeliveryTrace(lastDeliveryTraceJSON, &loop.LastDeliveryTrace); err != nil {
		return nil, fmt.Errorf("coordination: decode last delivery trace: %w", err)
	}
	if err := decodeRoomLoopPulseStateMap(replyPulseStateJSON, &loop.ReplyPulseState); err != nil {
		return nil, fmt.Errorf("coordination: decode reply pulse state: %w", err)
	}
	if err := decodeRoomLoopPulseStateMap(taskPulseStateJSON, &loop.TaskPulseState); err != nil {
		return nil, fmt.Errorf("coordination: decode task pulse state: %w", err)
	}
	if err := decodeRoomLoopTimeMap(taskFollowupStateJSON, &loop.TaskFollowupState); err != nil {
		return nil, fmt.Errorf("coordination: decode task followup state: %w", err)
	}
	if err := decodeRoomLoopTimeMap(coordinatorPulseStateJSON, &loop.CoordinatorPulseState); err != nil {
		return nil, fmt.Errorf("coordination: decode coordinator pulse state: %w", err)
	}
	loop.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return &loop, nil
}

// UpsertRoomLoop inserts or updates the persisted loop state for a room.
func (s *Store) UpsertRoomLoop(ctx context.Context, loop RoomLoop) (RoomLoop, error) {
	if s == nil || s.db == nil {
		return RoomLoop{}, fmt.Errorf("coordination: store not initialized")
	}
	loop.WorkspaceID = workspaceutil.CanonicalWorkspaceKey(loop.WorkspaceID)
	loop.RoomID = strings.TrimSpace(loop.RoomID)
	loop.ManagedBy = strings.TrimSpace(loop.ManagedBy)
	loop.DeliveryLeaseName = strings.TrimSpace(loop.DeliveryLeaseName)
	loop.DeliveryOwnerID = strings.TrimSpace(loop.DeliveryOwnerID)
	loop.DeliveryCursorMessageID = strings.TrimSpace(loop.DeliveryCursorMessageID)
	if loop.WorkspaceID == "" || loop.RoomID == "" {
		return RoomLoop{}, fmt.Errorf("coordination: workspace_id and room_id are required")
	}
	now := time.Now().UTC()
	loop.UpdatedAt = now

	var lastTick any
	if loop.LastTickAt != nil && !loop.LastTickAt.IsZero() {
		lastTick = loop.LastTickAt.UnixMilli()
	}
	var cursorAt any
	if loop.DeliveryCursorAt != nil && !loop.DeliveryCursorAt.IsZero() {
		cursorAt = loop.DeliveryCursorAt.UnixMilli()
	}
	replyPulseStateJSON, err := encodeRoomLoopPulseStateMap(loop.ReplyPulseState)
	if err != nil {
		return RoomLoop{}, fmt.Errorf("coordination: encode reply pulse state: %w", err)
	}
	lastDeliveryTraceJSON, err := encodeRoomLoopDeliveryTrace(loop.LastDeliveryTrace)
	if err != nil {
		return RoomLoop{}, fmt.Errorf("coordination: encode last delivery trace: %w", err)
	}
	taskPulseStateJSON, err := encodeRoomLoopPulseStateMap(loop.TaskPulseState)
	if err != nil {
		return RoomLoop{}, fmt.Errorf("coordination: encode task pulse state: %w", err)
	}
	taskFollowupStateJSON, err := encodeRoomLoopTimeMap(loop.TaskFollowupState)
	if err != nil {
		return RoomLoop{}, fmt.Errorf("coordination: encode task followup state: %w", err)
	}
	coordinatorPulseStateJSON, err := encodeRoomLoopTimeMap(loop.CoordinatorPulseState)
	if err != nil {
		return RoomLoop{}, fmt.Errorf("coordination: encode coordinator pulse state: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO room_loops (
			workspace_id, room_id, enabled, managed_by, last_tick_at_ms,
			delivery_lease_name, delivery_owner_id, delivery_cursor_message_id, delivery_cursor_at_ms,
			last_delivery_trace_json,
			reply_pulse_state_json, task_pulse_state_json, task_followup_state_json, coordinator_pulse_state_json,
			pulse_interval_ms, task_followup_interval_ms, reply_stale_after_ms, task_stale_after_ms,
			min_pulse_floor_ms, interrupt_attempt_limit, reminder_backoff_cap,
			coordinator_pulse_enabled, coordinator_escalation_enabled, updated_at_ms, created_at_ms
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
		ON CONFLICT(workspace_id, room_id) DO UPDATE SET
			enabled = excluded.enabled,
			managed_by = excluded.managed_by,
			last_tick_at_ms = excluded.last_tick_at_ms,
			delivery_lease_name = excluded.delivery_lease_name,
			delivery_owner_id = excluded.delivery_owner_id,
			delivery_cursor_message_id = excluded.delivery_cursor_message_id,
			delivery_cursor_at_ms = excluded.delivery_cursor_at_ms,
			last_delivery_trace_json = excluded.last_delivery_trace_json,
			reply_pulse_state_json = excluded.reply_pulse_state_json,
			task_pulse_state_json = excluded.task_pulse_state_json,
			task_followup_state_json = excluded.task_followup_state_json,
			coordinator_pulse_state_json = excluded.coordinator_pulse_state_json,
			pulse_interval_ms = excluded.pulse_interval_ms,
			task_followup_interval_ms = excluded.task_followup_interval_ms,
			reply_stale_after_ms = excluded.reply_stale_after_ms,
			task_stale_after_ms = excluded.task_stale_after_ms,
			min_pulse_floor_ms = excluded.min_pulse_floor_ms,
			interrupt_attempt_limit = excluded.interrupt_attempt_limit,
			reminder_backoff_cap = excluded.reminder_backoff_cap,
			coordinator_pulse_enabled = excluded.coordinator_pulse_enabled,
			coordinator_escalation_enabled = excluded.coordinator_escalation_enabled,
			updated_at_ms = excluded.updated_at_ms
	`,
		loop.WorkspaceID,
		loop.RoomID,
		boolToIntCoord(loop.Enabled),
		loop.ManagedBy,
		lastTick,
		loop.DeliveryLeaseName,
		loop.DeliveryOwnerID,
		loop.DeliveryCursorMessageID,
		cursorAt,
		lastDeliveryTraceJSON,
		replyPulseStateJSON,
		taskPulseStateJSON,
		taskFollowupStateJSON,
		coordinatorPulseStateJSON,
		loop.PulseInterval.Milliseconds(),
		loop.TaskFollowupInterval.Milliseconds(),
		loop.ReplyStaleAfter.Milliseconds(),
		loop.TaskStaleAfter.Milliseconds(),
		loop.MinPulseFloor.Milliseconds(),
		loop.InterruptAttemptLimit,
		loop.ReminderBackoffCap,
		boolToIntCoord(loop.CoordinatorPulseEnabled),
		boolToIntCoord(loop.CoordinatorEscalationEnabled),
		now.UnixMilli(),
		now.UnixMilli(),
	)
	if err != nil {
		return RoomLoop{}, fmt.Errorf("coordination: upsert room loop: %w", err)
	}
	return loop, nil
}

type durationMillis time.Duration

func (d *durationMillis) Scan(src any) error {
	switch v := src.(type) {
	case int64:
		*d = durationMillis(time.Duration(v) * time.Millisecond)
		return nil
	case int:
		*d = durationMillis(time.Duration(v) * time.Millisecond)
		return nil
	case []byte:
		parsed, err := time.ParseDuration(string(v) + "ms")
		if err != nil {
			return err
		}
		*d = durationMillis(parsed)
		return nil
	default:
		return fmt.Errorf("coordination: unsupported duration scan type %T", src)
	}
}

func encodeRoomLoopPulseStateMap(states map[string]RoomLoopPulseState) (string, error) {
	if len(states) == 0 {
		return "", nil
	}
	data, err := json.Marshal(states)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeRoomLoopPulseStateMap(raw string, target *map[string]RoomLoopPulseState) error {
	if target == nil {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*target = map[string]RoomLoopPulseState{}
		return nil
	}
	out := make(map[string]RoomLoopPulseState)
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return err
	}
	*target = out
	return nil
}

func encodeRoomLoopDeliveryTrace(trace *RoomLoopDeliveryTrace) (string, error) {
	if trace == nil {
		return "", nil
	}
	data, err := json.Marshal(trace)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeRoomLoopDeliveryTrace(raw string, target **RoomLoopDeliveryTrace) error {
	if target == nil {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*target = nil
		return nil
	}
	var trace RoomLoopDeliveryTrace
	if err := json.Unmarshal([]byte(raw), &trace); err != nil {
		return err
	}
	*target = &trace
	return nil
}

func encodeRoomLoopTimeMap(states map[string]time.Time) (string, error) {
	if len(states) == 0 {
		return "", nil
	}
	encoded := make(map[string]string, len(states))
	for key, value := range states {
		key = strings.TrimSpace(key)
		if key == "" || value.IsZero() {
			continue
		}
		encoded[key] = value.UTC().Format(time.RFC3339Nano)
	}
	if len(encoded) == 0 {
		return "", nil
	}
	data, err := json.Marshal(encoded)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeRoomLoopTimeMap(raw string, target *map[string]time.Time) error {
	if target == nil {
		return nil
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		*target = map[string]time.Time{}
		return nil
	}
	decoded := make(map[string]string)
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return err
	}
	out := make(map[string]time.Time, len(decoded))
	for key, value := range decoded {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return err
		}
		out[key] = ts.UTC()
	}
	*target = out
	return nil
}

type intBool bool

func (b *intBool) Scan(src any) error {
	switch v := src.(type) {
	case int64:
		*b = intBool(v != 0)
		return nil
	case int:
		*b = intBool(v != 0)
		return nil
	case bool:
		*b = intBool(v)
		return nil
	default:
		return fmt.Errorf("coordination: unsupported bool scan type %T", src)
	}
}

func boolToIntCoord(v bool) int {
	if v {
		return 1
	}
	return 0
}

// TryAcquireLease attempts to acquire or renew a lease for leaseName.
// It returns true when the caller becomes (or remains) the lease owner.
//
// Semantics:
// - If the lease does not exist, it is created and acquired.
// - If the lease is expired, it can be taken over by a new owner.
// - If the lease is held by the same ownerID, it is renewed.
// - Otherwise, it is not acquired and false is returned.
//
// Index:
//   Purpose: Implement single-leader semantics for daemon-style background loops
//   Keywords: lease, leader_election, coordination_db, expires_at
//   Related: Store.ReleaseLease, Store.GetLease
//   Flow: compute now+ttl → upsert-with-conditional-update → rowsAffected indicates acquisition
//   Resources: daemon_leases table
//   Events: none
//   OutputFields: bool (acquired)
// [[protocol:leader-lease-semantics]]
// [[invariant:lease-expires-at-enforced-by-upsert-conditional]]
func (s *Store) TryAcquireLease(ctx context.Context, leaseName, ownerID string, ttl time.Duration) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("coordination: store not initialized")
	}
	leaseName = strings.TrimSpace(leaseName)
	ownerID = strings.TrimSpace(ownerID)
	if leaseName == "" {
		return false, fmt.Errorf("coordination: lease name is required")
	}
	if ownerID == "" {
		return false, fmt.Errorf("coordination: owner id is required")
	}
	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	now := time.Now().UTC()
	nowMS := now.UnixMilli()
	expiresMS := now.Add(ttl).UnixMilli()

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO daemon_leases (name, owner_id, expires_at_ms, updated_at_ms, created_at_ms)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT(name) DO UPDATE SET
			owner_id = excluded.owner_id,
			expires_at_ms = excluded.expires_at_ms,
			updated_at_ms = excluded.updated_at_ms
		WHERE daemon_leases.expires_at_ms <= $6 OR daemon_leases.owner_id = $7
	`, leaseName, ownerID, expiresMS, nowMS, nowMS, nowMS, ownerID)
	if err != nil {
		return false, fmt.Errorf("coordination: acquire lease: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("coordination: acquire lease rows affected: %w", err)
	}
	return affected > 0, nil
}

// ReleaseLease releases the lease if it is owned by ownerID.
//
// Index:
//   Purpose: Allow a leader to voluntarily relinquish a lease (best-effort)
//   Keywords: lease_release, leader_election
//   Related: Store.TryAcquireLease
//   Flow: delete by (name, owner_id)
//   Resources: daemon_leases table
//   Events: none
//   OutputFields: none
// [[protocol:leader-lease-semantics]]
// [[risk:lease-not-released-on-crash]]
func (s *Store) ReleaseLease(ctx context.Context, leaseName, ownerID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("coordination: store not initialized")
	}
	leaseName = strings.TrimSpace(leaseName)
	ownerID = strings.TrimSpace(ownerID)
	if leaseName == "" || ownerID == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `DELETE FROM daemon_leases WHERE name = $1 AND owner_id = $2`, leaseName, ownerID)
	if err != nil {
		return fmt.Errorf("coordination: release lease: %w", err)
	}
	return nil
}

// GetLease returns the current lease state, or nil when not found.
func (s *Store) GetLease(ctx context.Context, leaseName string) (*Lease, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("coordination: store not initialized")
	}
	leaseName = strings.TrimSpace(leaseName)
	if leaseName == "" {
		return nil, fmt.Errorf("coordination: lease name is required")
	}

	var l Lease
	var expiresMS, updatedMS int64
	err := s.db.QueryRowContext(ctx, `
		SELECT name, owner_id, expires_at_ms, updated_at_ms
		FROM daemon_leases
		WHERE name = $1
	`, leaseName).Scan(&l.Name, &l.OwnerID, &expiresMS, &updatedMS)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("coordination: get lease: %w", err)
	}
	l.ExpiresAt = time.UnixMilli(expiresMS).UTC()
	l.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return &l, nil
}
