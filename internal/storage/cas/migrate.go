package cas

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/storage"
)

// MigrateResult contains statistics about the migration.
type MigrateResult struct {
	ObjectsMigrated int   `json:"objects_migrated"`
	ObjectsSkipped  int   `json:"objects_skipped"`
	BytesMigrated   int64 `json:"bytes_migrated"`
	Errors          int   `json:"errors"`
}

// Migrate copies all objects from source to destination store.
func Migrate(ctx context.Context, src, dst storage.CASStore) (MigrateResult, error) {
	var result MigrateResult

	objects, err := src.List(ctx)
	if err != nil {
		return result, fmt.Errorf("cas: list source: %w", err)
	}

	for _, obj := range objects {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		// Check if object already exists in destination
		if _, err := dst.Head(ctx, obj.Digest); err == nil {
			result.ObjectsSkipped++
			continue
		}

		// Get content from source
		reader, meta, err := src.Get(ctx, obj.Digest)
		if err != nil {
			result.Errors++
			continue
		}

		// Put content to destination
		_, err = dst.Put(ctx, reader, meta.Kind, meta.Tags)
		reader.Close()
		if err != nil {
			result.Errors++
			continue
		}

		// Preserve pinned status
		if obj.Pinned {
			if err := dst.Pin(ctx, obj.Digest); err != nil {
				// Non-fatal error
				fmt.Fprintf(os.Stderr, "cas: warning: failed to pin %s: %v\n", obj.Digest, err)
			}
		}

		result.ObjectsMigrated++
		result.BytesMigrated += obj.Size
	}

	return result, nil
}

// MigrateFromFile migrates from a file-based CAS to the destination store.
func MigrateFromFile(ctx context.Context, sourcePath string, dst storage.CASStore) (MigrateResult, error) {
	src, err := NewStore(sourcePath)
	if err != nil {
		return MigrateResult{}, fmt.Errorf("cas: open source: %w", err)
	}
	defer src.Close()

	return Migrate(ctx, src, dst)
}

// autoMigrate checks if migration is needed and performs it.
func autoMigrate(ctx context.Context, cfg Config, dst storage.CASStore) error {
	if cfg.Migration.SourcePath == "" {
		return nil
	}

	// Check if source directory exists and has content
	sha256Dir := filepath.Join(cfg.Migration.SourcePath, "sha256")
	entries, err := os.ReadDir(sha256Dir)
	if err != nil {
		if os.IsNotExist(err) {
			// No legacy CAS, nothing to migrate
			return nil
		}
		return fmt.Errorf("cas: check source: %w", err)
	}

	// Count actual content files (not .json metadata)
	contentCount := 0
	for _, entry := range entries {
		if !entry.IsDir() && len(entry.Name()) == 64 {
			contentCount++
		}
	}

	if contentCount == 0 {
		// No content to migrate
		return nil
	}

	// Check if destination already has content
	dstObjects, err := dst.List(ctx)
	if err != nil {
		return fmt.Errorf("cas: check destination: %w", err)
	}

	if len(dstObjects) > 0 {
		// Destination already has content, skip migration
		// This prevents repeated migration attempts
		return nil
	}

	// Perform migration
	fmt.Fprintf(os.Stderr, "cas: auto-migrating %d objects from %s\n", contentCount, cfg.Migration.SourcePath)

	result, err := MigrateFromFile(ctx, cfg.Migration.SourcePath, dst)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "cas: migrated %d objects (%d bytes), skipped %d, errors %d\n",
		result.ObjectsMigrated, result.BytesMigrated, result.ObjectsSkipped, result.Errors)

	return nil
}

// MigrationStatus checks if migration is needed and returns status.
type MigrationStatus struct {
	NeedsMigration bool   `json:"needs_migration"`
	SourcePath     string `json:"source_path,omitempty"`
	SourceObjects  int    `json:"source_objects,omitempty"`
	DestObjects    int    `json:"dest_objects,omitempty"`
}

// CheckMigration checks if migration is needed without performing it.
func CheckMigration(ctx context.Context, cfg Config, dst storage.CASStore) (MigrationStatus, error) {
	var status MigrationStatus

	if cfg.Migration.SourcePath == "" {
		return status, nil
	}

	status.SourcePath = cfg.Migration.SourcePath

	// Check source
	sha256Dir := filepath.Join(cfg.Migration.SourcePath, "sha256")
	entries, err := os.ReadDir(sha256Dir)
	if err != nil {
		if os.IsNotExist(err) {
			return status, nil
		}
		return status, fmt.Errorf("cas: check source: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && len(entry.Name()) == 64 {
			status.SourceObjects++
		}
	}

	// Check destination
	dstObjects, err := dst.List(ctx)
	if err != nil {
		return status, fmt.Errorf("cas: check destination: %w", err)
	}
	status.DestObjects = len(dstObjects)

	// Migration is needed if source has content and destination is empty
	status.NeedsMigration = status.SourceObjects > 0 && status.DestObjects == 0

	return status, nil
}
