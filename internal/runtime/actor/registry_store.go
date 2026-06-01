package actor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/sqlutil"
)

// RegistryStore persists actor configurations for supervisor auto-respawn.
//
// When the supervisor restarts, it can load all registered actors and
// respawn them automatically. This enables durable actor systems.
type RegistryStore interface {
	// Close closes the store.
	Close() error

	// RegisterActor persists an actor configuration.
	RegisterActor(ctx context.Context, reg ActorRecord) error

	// UnregisterActor removes an actor configuration.
	UnregisterActor(ctx context.Context, namespace string) error

	// GetActor retrieves an actor configuration by namespace.
	GetActor(ctx context.Context, namespace string) (ActorRecord, error)

	// ListActors returns all registered actors.
	ListActors(ctx context.Context) ([]ActorRecord, error)

	// UpdateStatus updates an actor's status.
	UpdateStatus(ctx context.Context, namespace string, status ActorStatus) error
}

// ActorStatus represents the persisted status of an actor.
type ActorStatus string

const (
	ActorStatusRegistered ActorStatus = "registered"
	ActorStatusRunning    ActorStatus = "running"
	ActorStatusStopped    ActorStatus = "stopped"
	ActorStatusError      ActorStatus = "error"
)

// ActorRecord represents a persisted actor configuration.
type ActorRecord struct {
	// Namespace is the unique identifier (primary key)
	Namespace string `json:"namespace"`

	// Role defines the actor type (coder, planner, reviewer)
	Role string `json:"role"`

	// ConfigJSON holds the full actor.Config as JSON
	ConfigJSON string `json:"config_json"`

	// Status is the current actor status
	Status ActorStatus `json:"status"`

	// CreatedAt is when the actor was registered
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the actor was last updated
	UpdatedAt time.Time `json:"updated_at"`

	// Parsed config (not persisted, populated on read)
	Config Config `json:"-"`
}

var (
	// ErrActorNotFound indicates the actor was not found.
	ErrActorNotFound = errors.New("actor: not found")
	// ErrInvalidActorStatus indicates a persisted actor lifecycle status is not recognized.
	ErrInvalidActorStatus = errors.New("actor: invalid status")
)

// SQLiteRegistryStore implements RegistryStore using SQLite.
type SQLiteRegistryStore struct {
	db *sql.DB
}

// NewRegistryStore creates a new SQLite-backed registry store.
// The db connection should already be open.
func NewRegistryStore(ctx context.Context, db *sql.DB) (*SQLiteRegistryStore, error) {
	store := &SQLiteRegistryStore{db: db}
	if err := store.ensureSchema(ctx); err != nil {
		return nil, fmt.Errorf("registry store: ensure schema: %w", err)
	}
	return store, nil
}

// ensureSchema creates the actor_registry table if it doesn't exist.
func (s *SQLiteRegistryStore) ensureSchema(ctx context.Context) error {
	ddl := `
CREATE TABLE IF NOT EXISTS actor_registry (
	namespace    TEXT PRIMARY KEY,
	role         TEXT NOT NULL,
	config_json  TEXT NOT NULL,
	status       TEXT DEFAULT 'registered' CHECK (status IN ('registered','running','stopped','error')),
	created_at   TEXT NOT NULL,
	updated_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_actor_registry_role ON actor_registry(role);
CREATE INDEX IF NOT EXISTS idx_actor_registry_status ON actor_registry(status);
`
	if _, err := s.db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("create actor_registry: %w", err)
	}
	return nil
}

// Close is a no-op since we don't own the db connection.
func (s *SQLiteRegistryStore) Close() error {
	return nil
}

// RegisterActor persists an actor configuration.
func (s *SQLiteRegistryStore) RegisterActor(ctx context.Context, reg ActorRecord) error {
	normalizedStatus, err := normalizeActorStatus(reg.Status, true)
	if err != nil {
		return err
	}
	reg.Status = normalizedStatus

	now := time.Now().UTC()

	// Use UPSERT to handle re-registration
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO actor_registry (namespace, role, config_json, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(namespace) DO UPDATE SET
			role = excluded.role,
			config_json = excluded.config_json,
			status = excluded.status,
			updated_at = excluded.updated_at
	`, reg.Namespace, reg.Role, reg.ConfigJSON, reg.Status,
		sqlutil.FormatTimestamp(now), sqlutil.FormatTimestamp(now))
	if err != nil {
		return fmt.Errorf("register actor: %w", err)
	}
	return nil
}

// UnregisterActor removes an actor configuration.
func (s *SQLiteRegistryStore) UnregisterActor(ctx context.Context, namespace string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM actor_registry WHERE namespace = ?`, namespace)
	if err != nil {
		return fmt.Errorf("unregister actor: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("unregister actor rows affected: %w", err)
	}
	if rows == 0 {
		return ErrActorNotFound
	}
	return nil
}

// GetActor retrieves an actor configuration by namespace.
func (s *SQLiteRegistryStore) GetActor(ctx context.Context, namespace string) (ActorRecord, error) {
	var rec ActorRecord
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx, `
		SELECT namespace, role, config_json, status, created_at, updated_at
		FROM actor_registry
		WHERE namespace = ?
	`, namespace).Scan(&rec.Namespace, &rec.Role, &rec.ConfigJSON, &rec.Status, &createdAt, &updatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ActorRecord{}, ErrActorNotFound
		}
		return ActorRecord{}, fmt.Errorf("get actor: %w", err)
	}
	if err := validateActorRecordStatus(&rec); err != nil {
		return ActorRecord{}, err
	}

	if err := parseActorTimestamps(&rec, createdAt, updatedAt); err != nil {
		return ActorRecord{}, err
	}

	// Parse config from JSON
	if err := json.Unmarshal([]byte(rec.ConfigJSON), &rec.Config); err != nil {
		return rec, fmt.Errorf("unmarshal config: %w", err)
	}

	return rec, nil
}

// ListActors returns all registered actors.
func (s *SQLiteRegistryStore) ListActors(ctx context.Context) ([]ActorRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT namespace, role, config_json, status, created_at, updated_at
		FROM actor_registry
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("list actors: %w", err)
	}
	defer rows.Close()

	records := []ActorRecord{}
	for rows.Next() {
		var rec ActorRecord
		var createdAt, updatedAt string

		if err := rows.Scan(&rec.Namespace, &rec.Role, &rec.ConfigJSON, &rec.Status, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan actor: %w", err)
		}
		if err := validateActorRecordStatus(&rec); err != nil {
			return nil, err
		}

		if err := parseActorTimestamps(&rec, createdAt, updatedAt); err != nil {
			return nil, err
		}

		// Parse config from JSON
		if err := json.Unmarshal([]byte(rec.ConfigJSON), &rec.Config); err != nil {
			// Log but continue - malformed records shouldn't block listing
			continue
		}

		records = append(records, rec)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list actors iteration: %w", err)
	}

	return records, nil
}

// UpdateStatus updates an actor's status.
func (s *SQLiteRegistryStore) UpdateStatus(ctx context.Context, namespace string, status ActorStatus) error {
	normalizedStatus, err := normalizeActorStatus(status, false)
	if err != nil {
		return err
	}
	status = normalizedStatus

	now := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
		UPDATE actor_registry
		SET status = ?, updated_at = ?
		WHERE namespace = ?
	`, status, sqlutil.FormatTimestamp(now), namespace)
	if err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update status rows affected: %w", err)
	}
	if rows == 0 {
		return ErrActorNotFound
	}
	return nil
}

// ListActorsByStatus returns actors with the given status.
func (s *SQLiteRegistryStore) ListActorsByStatus(ctx context.Context, status ActorStatus) ([]ActorRecord, error) {
	normalizedStatus, err := normalizeActorStatus(status, false)
	if err != nil {
		return nil, err
	}
	status = normalizedStatus

	rows, err := s.db.QueryContext(ctx, `
		SELECT namespace, role, config_json, status, created_at, updated_at
		FROM actor_registry
		WHERE status = ?
		ORDER BY created_at ASC
	`, status)
	if err != nil {
		return nil, fmt.Errorf("list actors by status: %w", err)
	}
	defer rows.Close()

	var records []ActorRecord
	for rows.Next() {
		var rec ActorRecord
		var createdAt, updatedAt string

		if err := rows.Scan(&rec.Namespace, &rec.Role, &rec.ConfigJSON, &rec.Status, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan actor: %w", err)
		}
		if err := validateActorRecordStatus(&rec); err != nil {
			return nil, err
		}

		if err := parseActorTimestamps(&rec, createdAt, updatedAt); err != nil {
			return nil, err
		}

		// Parse config from JSON
		if err := json.Unmarshal([]byte(rec.ConfigJSON), &rec.Config); err != nil {
			continue
		}

		records = append(records, rec)
	}

	return records, rows.Err()
}

func validateActorRecordStatus(rec *ActorRecord) error {
	normalizedStatus, err := normalizeActorStatus(rec.Status, false)
	if err != nil {
		return err
	}
	rec.Status = normalizedStatus
	return nil
}

func normalizeActorStatus(status ActorStatus, defaultEmpty bool) (ActorStatus, error) {
	status = ActorStatus(strings.TrimSpace(string(status)))
	if status == "" {
		if defaultEmpty {
			return ActorStatusRegistered, nil
		}
		return "", fmt.Errorf("%w: empty", ErrInvalidActorStatus)
	}

	switch status {
	case ActorStatusRegistered, ActorStatusRunning, ActorStatusStopped, ActorStatusError:
		return status, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidActorStatus, status)
	}
}

func parseActorTimestamps(rec *ActorRecord, createdAt, updatedAt string) error {
	var err error
	rec.CreatedAt, err = sqlutil.ScanTimestamp(createdAt)
	if err != nil {
		return fmt.Errorf("actor: scan created_at: %w", err)
	}
	rec.UpdatedAt, err = sqlutil.ScanTimestamp(updatedAt)
	if err != nil {
		return fmt.Errorf("actor: scan updated_at: %w", err)
	}
	return nil
}

// MarshalConfig serializes an actor.Config to JSON for storage.
func MarshalConfig(cfg Config) (string, error) {
	b, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal config: %w", err)
	}
	return string(b), nil
}

// Ensure SQLiteRegistryStore implements RegistryStore.
var _ RegistryStore = (*SQLiteRegistryStore)(nil)
