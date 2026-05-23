package cas

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshka0/foxctl/internal/platform/observability"
	"github.com/joshka0/foxctl/internal/storage"
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
				observability.Emit(ctx, observability.NewEvent("cas.pin_failed").
					WithComponent("cas").
					WithData("digest", obj.Digest).
					Error(err, 0))
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

	// Only count legacy objects that have metadata.
	// Some very old layouts can leave content files without a matching `.json` metadata file.
	// Those cannot be migrated via the Store List API and should not trigger repeated auto-migration noise.
	sourceDigests := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || len(entry.Name()) != 64 {
			continue
		}
		if _, err := os.Stat(filepath.Join(sha256Dir, entry.Name()+".json")); err != nil {
			continue
		}
		sourceDigests = append(sourceDigests, "sha256:"+entry.Name())
	}

	if len(sourceDigests) == 0 {
		// No migratable objects.
		return nil
	}

	// Check destination to estimate new objects to migrate
	dstObjects, err := dst.List(ctx)
	if err != nil {
		return fmt.Errorf("cas: check destination: %w", err)
	}

	// Build a set of existing destination digests for quick lookup
	dstDigests := make(map[string]struct{}, len(dstObjects))
	for _, obj := range dstObjects {
		dstDigests[obj.Digest] = struct{}{}
	}

	// Count objects that need migration (exist in source but not destination)
	newCount := 0
	for _, digest := range sourceDigests {
		if _, exists := dstDigests[digest]; !exists {
			newCount++
		}
	}

	contentCount := len(sourceDigests)

	if newCount == 0 {
		// All source objects already exist in destination
		return nil
	}

	// Perform incremental migration
	observability.Emit(ctx, observability.NewEvent("cas.auto_migration_start").
		WithComponent("cas").
		WithData("new_count", newCount).
		WithData("source_path", cfg.Migration.SourcePath).
		WithData("source_count", contentCount).
		WithData("dest_count", len(dstObjects)).
		Success(0))

	result, err := MigrateFromFile(ctx, cfg.Migration.SourcePath, dst)
	if err != nil {
		return err
	}

	observability.Emit(ctx, observability.NewEvent("cas.auto_migration_complete").
		WithComponent("cas").
		WithData("objects_migrated", result.ObjectsMigrated).
		WithData("bytes_migrated", result.BytesMigrated).
		WithData("objects_skipped", result.ObjectsSkipped).
		WithData("errors", result.Errors).
		Success(0))

	return nil
}

// MigrationStatus checks if migration is needed and returns status.
type MigrationStatus struct {
	NeedsMigration   bool   `json:"needs_migration"`
	SourcePath       string `json:"source_path,omitempty"`
	SourceObjects    int    `json:"source_objects,omitempty"`
	DestObjects      int    `json:"dest_objects,omitempty"`
	PendingMigration int    `json:"pending_migration,omitempty"` // Objects in source not in dest
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

	// Collect source digests
	sourceDigests := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && len(entry.Name()) == 64 {
			status.SourceObjects++
			sourceDigests = append(sourceDigests, "sha256:"+entry.Name())
		}
	}

	// Check destination
	dstObjects, err := dst.List(ctx)
	if err != nil {
		return status, fmt.Errorf("cas: check destination: %w", err)
	}
	status.DestObjects = len(dstObjects)

	// Build destination digest set
	dstDigests := make(map[string]struct{}, len(dstObjects))
	for _, obj := range dstObjects {
		dstDigests[obj.Digest] = struct{}{}
	}

	// Count objects needing migration
	for _, digest := range sourceDigests {
		if _, exists := dstDigests[digest]; !exists {
			status.PendingMigration++
		}
	}

	// Migration is needed if there are objects in source not in destination
	status.NeedsMigration = status.PendingMigration > 0

	return status, nil
}
