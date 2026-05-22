package idmap

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	tursoadapter "github.com/joshka0/foxctl/internal/v2/adapters/turso"
	v2events "github.com/joshka0/foxctl/internal/v2/core/events"
)

// ErrConflict indicates a conflicting immutable mapping.
var ErrConflict = errors.New("v2 idmap: conflict")

// Store persists immutable mappings between legacy and v2 identifiers.
type Store struct {
	db  tursoadapter.StoreDB
	now func() time.Time
}

func NewStore(db tursoadapter.StoreDB) *Store {
	return &Store{
		db: db,
		now: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// Put stores an immutable mapping, returning conflict when remapped.
func (s *Store) Put(ctx context.Context, entityType, legacyID, v2ID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 idmap put: nil store")
	}
	entityType = strings.TrimSpace(entityType)
	legacyID = strings.TrimSpace(legacyID)
	v2ID = strings.TrimSpace(v2ID)
	if entityType == "" || legacyID == "" || v2ID == "" {
		return fmt.Errorf("v2 idmap put: entity_type, legacy_id, and v2_id are required")
	}

	return sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		var existing string
		err := tx.QueryRowContext(
			ctx,
			`SELECT v2_id FROM v2_id_map WHERE entity_type = $1 AND legacy_id = $2`,
			entityType, legacyID,
		).Scan(&existing)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// continue
		case err != nil:
			return fmt.Errorf("query legacy mapping: %w", err)
		default:
			if existing == v2ID {
				return nil
			}
			return fmt.Errorf("%w: legacy_id=%s existing=%s new=%s", ErrConflict, legacyID, existing, v2ID)
		}

		var existingLegacy string
		err = tx.QueryRowContext(
			ctx,
			`SELECT legacy_id FROM v2_id_map WHERE entity_type = $1 AND v2_id = $2`,
			entityType, v2ID,
		).Scan(&existingLegacy)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			// continue
		case err != nil:
			return fmt.Errorf("query v2 mapping: %w", err)
		default:
			if existingLegacy == legacyID {
				return nil
			}
			return fmt.Errorf("%w: v2_id=%s existing=%s new=%s", ErrConflict, v2ID, existingLegacy, legacyID)
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO v2_id_map (entity_type, legacy_id, v2_id, created_at)
			VALUES ($1, $2, $3, $4)
		`, entityType, legacyID, v2ID, sqlutil.FormatTimestamp(s.now()))
		if err != nil {
			if isUniqueConstraintErr(err) {
				return fmt.Errorf("%w: %v", ErrConflict, err)
			}
			return fmt.Errorf("insert id_map: %w", err)
		}
		return nil
	})
}

// ResolveV2ID resolves a legacy ID to a v2 ID.
func (s *Store) ResolveV2ID(ctx context.Context, entityType, legacyID string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("v2 idmap resolve v2: nil store")
	}
	var v2ID string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT v2_id FROM v2_id_map WHERE entity_type = $1 AND legacy_id = $2`,
		strings.TrimSpace(entityType), strings.TrimSpace(legacyID),
	).Scan(&v2ID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", v2events.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("query resolve v2: %w", err)
	}
	return v2ID, nil
}

// ResolveLegacyID resolves a v2 ID to legacy ID.
func (s *Store) ResolveLegacyID(ctx context.Context, entityType, v2ID string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("v2 idmap resolve legacy: nil store")
	}
	var legacyID string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT legacy_id FROM v2_id_map WHERE entity_type = $1 AND v2_id = $2`,
		strings.TrimSpace(entityType), strings.TrimSpace(v2ID),
	).Scan(&legacyID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", v2events.ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("query resolve legacy: %w", err)
	}
	return legacyID, nil
}

func isUniqueConstraintErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "constraint failed") ||
		strings.Contains(msg, "duplicate key")
}
