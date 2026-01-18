// Package memory implements named memory storage for skill execution results and context data.
package memory

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

// OpenWithConfig opens a memory store based on the provided configuration.
// It automatically selects the appropriate backend:
//   - Turso: when database.driver=turso and Turso URL is configured
//   - LibSQL: when database.driver=libsql (with optional sync)
//   - SQLite: default fallback
//
// This is the recommended way to open a memory store in skills.
func OpenWithConfig(ctx context.Context, cfg config.Config) (storage.MemoryStore, error) {
	driver := dbdriver.DriverType(cfg.Database.Driver)

	// Check environment variable override
	if envDriver := os.Getenv("AGENTCTL_MEMORY_DB_DRIVER"); envDriver != "" {
		driver = dbdriver.DriverType(envDriver)
	}

	switch driver {
	case dbdriver.DriverTurso:
		return openTursoFromConfig(ctx, cfg)
	case dbdriver.DriverLibSQL:
		return openLibSQLFromConfig(ctx, cfg)
	default:
		// SQLite fallback
		return Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	}
}

// openTursoFromConfig opens a TursoStore from platform config.
func openTursoFromConfig(ctx context.Context, cfg config.Config) (*TursoStore, error) {
	if cfg.Database.Turso.URL == "" {
		return nil, fmt.Errorf("memory: turso URL not configured (set database.turso.url or AGENTCTL_TURSO_URL)")
	}

	tursoCfg := dbdriver.TursoConfig{
		URL:                cfg.Database.Turso.URL,
		AuthToken:          cfg.Database.Turso.AuthToken,
		EnableVectorSearch: true, // Always enable for memory
		VectorDimensions:   cfg.Database.Vector.Dimensions,
		// Use storage root for embedded replica persistence
		ReplicaPath: cfg.Storage.Root + "/memory.turso.replica",
	}

	store, err := OpenTurso(ctx, tursoCfg)
	if err != nil {
		return nil, fmt.Errorf("memory: open turso: %w", err)
	}

	log.Printf("[memory] opened Turso store: %s (vector: true)", cfg.Database.Turso.URL)
	return store, nil
}

// openLibSQLFromConfig opens a LibSQL store from platform config.
// This uses the new local-first sync architecture.
// Falls back to SQLite if no sync URL is configured.
func openLibSQLFromConfig(ctx context.Context, cfg config.Config) (storage.MemoryStore, error) {
	// Build LibSQL config from platform config and environment
	loader := dbdriver.NewConfigLoader(cfg.Storage.Root)
	dbCfg := loader.LoadMemoryConfig()

	// If driver is libsql but config returned something else, force libsql
	if dbCfg.Driver != dbdriver.DriverLibSQL {
		dbCfg = dbdriver.Config{
			Driver: dbdriver.DriverLibSQL,
			LibSQL: dbdriver.LibSQLConfig{
				Path:               cfg.Storage.Root + "/memory.libsql",
				EnableVectorSearch: true,
				VectorDimensions:   dbdriver.GetDefaultVectorDimensions(),
			},
		}
	}

	// Ensure vector search is enabled for memory
	dbCfg.LibSQL.EnableVectorSearch = true

	// Check for sync URL from environment (already loaded by ConfigLoader)
	syncEnabled := dbCfg.LibSQL.SyncURL != ""

	// Use TursoConfig which works with embedded replicas (same underlying driver)
	tursoCfg := dbdriver.TursoConfig{
		URL:                dbCfg.LibSQL.SyncURL,
		AuthToken:          dbCfg.LibSQL.AuthToken,
		ReplicaPath:        dbCfg.LibSQL.Path,
		EnableVectorSearch: true,
		VectorDimensions:   dbCfg.LibSQL.VectorDimensions,
	}

	// If no sync URL, fall back to SQLite (local-only mode)
	if tursoCfg.URL == "" {
		log.Printf("[memory] libsql configured but no sync URL, falling back to SQLite")
		return Open(ctx, cfg.Storage.Root, cfg.Paths.CAS)
	}

	store, err := OpenTurso(ctx, tursoCfg)
	if err != nil {
		return nil, fmt.Errorf("memory: open libsql: %w", err)
	}

	if syncEnabled {
		log.Printf("[memory] opened LibSQL store with sync: %s -> %s", dbCfg.LibSQL.Path, dbCfg.LibSQL.SyncURL)
	} else {
		log.Printf("[memory] opened LibSQL store (local-only): %s", dbCfg.LibSQL.Path)
	}

	return store, nil
}
