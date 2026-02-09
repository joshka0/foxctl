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
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("coordination: migrate: %w", err)
	}
	return nil
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
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(name) DO UPDATE SET
			owner_id = excluded.owner_id,
			expires_at_ms = excluded.expires_at_ms,
			updated_at_ms = excluded.updated_at_ms
		WHERE daemon_leases.expires_at_ms <= ? OR daemon_leases.owner_id = ?
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

	_, err := s.db.ExecContext(ctx, `DELETE FROM daemon_leases WHERE name = ? AND owner_id = ?`, leaseName, ownerID)
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
		WHERE name = ?
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
