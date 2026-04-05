package coordination

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/dbutil"
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
	WorkspaceID              string
	RoomID                   string
	Enabled                  bool
	ManagedBy                string
	LastTickAt               *time.Time
	PulseInterval            time.Duration
	ReplyStaleAfter          time.Duration
	TaskStaleAfter           time.Duration
	MinPulseFloor            time.Duration
	CoordinatorPulseEnabled  bool
	UpdatedAt                time.Time
}

// Open opens the coordination store rooted at storageRoot/coordination.db.
// The database driver is selected via the dbdriver env var conventions (e.g., AGENTCTL_COORDINATION_DB_DRIVER).
//
// Index:
// - Purpose: Provide a stable coordination store for single-leader daemon leases
// - Flow: open db via dbutil → migrate schema → return store
// - SideEffects: creates coordination.db and schema if missing
// - FailureModes: open/migration errors
// - Related: Store.TryAcquireLease, Store.ReleaseLease
// - Keywords: coordination, leader_lease, daemon, single_leader
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
	pulse_interval_ms           INTEGER NOT NULL,
	reply_stale_after_ms        INTEGER NOT NULL,
	task_stale_after_ms         INTEGER NOT NULL,
	min_pulse_floor_ms          INTEGER NOT NULL,
	coordinator_pulse_enabled   INTEGER NOT NULL DEFAULT 1,
	updated_at_ms               INTEGER NOT NULL,
	created_at_ms               INTEGER NOT NULL,
	PRIMARY KEY (workspace_id, room_id)
);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("coordination: migrate: %w", err)
	}
	return nil
}

// GetRoomLoop returns the persisted loop policy/runtime state for one room.
func (s *Store) GetRoomLoop(ctx context.Context, workspaceID, roomID string) (*RoomLoop, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("coordination: store not initialized")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	roomID = strings.TrimSpace(roomID)
	if workspaceID == "" || roomID == "" {
		return nil, fmt.Errorf("coordination: workspace_id and room_id are required")
	}

	var loop RoomLoop
	var (
		lastTickMS sql.NullInt64
		updatedMS  int64
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT workspace_id, room_id, enabled, managed_by, last_tick_at_ms,
		       pulse_interval_ms, reply_stale_after_ms, task_stale_after_ms,
		       min_pulse_floor_ms, coordinator_pulse_enabled, updated_at_ms
		FROM room_loops
		WHERE workspace_id = $1 AND room_id = $2
	`, workspaceID, roomID).Scan(
		&loop.WorkspaceID,
		&loop.RoomID,
		(*intBool)(&loop.Enabled),
		&loop.ManagedBy,
		&lastTickMS,
		(*durationMillis)(&loop.PulseInterval),
		(*durationMillis)(&loop.ReplyStaleAfter),
		(*durationMillis)(&loop.TaskStaleAfter),
		(*durationMillis)(&loop.MinPulseFloor),
		(*intBool)(&loop.CoordinatorPulseEnabled),
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
	loop.UpdatedAt = time.UnixMilli(updatedMS).UTC()
	return &loop, nil
}

// UpsertRoomLoop inserts or updates the persisted loop state for a room.
func (s *Store) UpsertRoomLoop(ctx context.Context, loop RoomLoop) (RoomLoop, error) {
	if s == nil || s.db == nil {
		return RoomLoop{}, fmt.Errorf("coordination: store not initialized")
	}
	loop.WorkspaceID = strings.TrimSpace(loop.WorkspaceID)
	loop.RoomID = strings.TrimSpace(loop.RoomID)
	loop.ManagedBy = strings.TrimSpace(loop.ManagedBy)
	if loop.WorkspaceID == "" || loop.RoomID == "" {
		return RoomLoop{}, fmt.Errorf("coordination: workspace_id and room_id are required")
	}
	now := time.Now().UTC()
	loop.UpdatedAt = now

	var lastTick any
	if loop.LastTickAt != nil && !loop.LastTickAt.IsZero() {
		lastTick = loop.LastTickAt.UnixMilli()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO room_loops (
			workspace_id, room_id, enabled, managed_by, last_tick_at_ms,
			pulse_interval_ms, reply_stale_after_ms, task_stale_after_ms,
			min_pulse_floor_ms, coordinator_pulse_enabled, updated_at_ms, created_at_ms
		)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(workspace_id, room_id) DO UPDATE SET
			enabled = excluded.enabled,
			managed_by = excluded.managed_by,
			last_tick_at_ms = excluded.last_tick_at_ms,
			pulse_interval_ms = excluded.pulse_interval_ms,
			reply_stale_after_ms = excluded.reply_stale_after_ms,
			task_stale_after_ms = excluded.task_stale_after_ms,
			min_pulse_floor_ms = excluded.min_pulse_floor_ms,
			coordinator_pulse_enabled = excluded.coordinator_pulse_enabled,
			updated_at_ms = excluded.updated_at_ms
	`,
		loop.WorkspaceID,
		loop.RoomID,
		boolToIntCoord(loop.Enabled),
		loop.ManagedBy,
		lastTick,
		loop.PulseInterval.Milliseconds(),
		loop.ReplyStaleAfter.Milliseconds(),
		loop.TaskStaleAfter.Milliseconds(),
		loop.MinPulseFloor.Milliseconds(),
		boolToIntCoord(loop.CoordinatorPulseEnabled),
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
// - Purpose: Implement single-leader semantics for daemon-style background loops
// - Flow: compute now+ttl → upsert-with-conditional-update → rowsAffected indicates acquisition
// - SideEffects: writes daemon_leases
// - FailureModes: DB errors
// - Related: Store.ReleaseLease, Store.GetLease
// - Keywords: lease, leader_election, coordination_db, expires_at
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
// - Purpose: Allow a leader to voluntarily relinquish a lease (best-effort)
// - Flow: delete by (name, owner_id)
// - SideEffects: deletes daemon_leases row
// - FailureModes: DB errors
// - Related: Store.TryAcquireLease
// - Keywords: lease_release, leader_election
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
