//go:build cgo

// synctest is a simple test to verify libsql sync functionality.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/storage/dbdriver"
)

func main() {
	ctx := context.Background()

	// Configure libsql with sync
	syncURL := os.Getenv("AGENTCTL_LIBSQL_SYNC_URL")
	if syncURL == "" {
		syncURL = "http://127.0.0.1:8080"
	}

	replicaPath := os.Getenv("AGENTCTL_LIBSQL_PATH")
	if replicaPath == "" {
		replicaPath = os.ExpandEnv("$HOME/.agentctl/sync-test/replica.db")
	}

	cfg := dbdriver.Config{
		Driver: dbdriver.DriverLibSQL,
		LibSQL: dbdriver.LibSQLConfig{
			Path:               replicaPath,
			SyncURL:            syncURL,
			EnableVectorSearch: false,
		},
	}

	log.Printf("Opening libsql with sync: path=%s, syncURL=%s", replicaPath, syncURL)

	// Open database with migration
	db, err := dbdriver.OpenDB(ctx, cfg, func(ctx context.Context, db *sql.DB) error {
		_, err := db.ExecContext(ctx, `
			CREATE TABLE IF NOT EXISTS sync_test (
				id INTEGER PRIMARY KEY,
				message TEXT NOT NULL,
				created_at TEXT NOT NULL
			)
		`)
		return err
	})
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	log.Printf("Database opened successfully, driver=%s", db.GetDriverType())

	// Check if sync is enabled
	if syncer, ok := db.(dbdriver.Syncer); ok {
		log.Printf("Sync enabled: %v, URL: %s", syncer.IsSyncEnabled(), syncer.GetSyncURL())
	} else {
		log.Printf("Sync interface not available")
	}

	// Insert a test record
	now := time.Now().UTC().Format(time.RFC3339)
	msg := fmt.Sprintf("Hello from sync test at %s", now)

	_, err = db.ExecContext(ctx, `INSERT INTO sync_test (message, created_at) VALUES (?, ?)`, msg, now)
	if err != nil {
		log.Fatalf("Failed to insert: %v", err)
	}
	log.Printf("Inserted: %s", msg)

	// Trigger sync
	if syncer, ok := db.(dbdriver.Syncer); ok && syncer.IsSyncEnabled() {
		log.Printf("Triggering sync...")
		if err := syncer.Sync(); err != nil {
			log.Printf("Sync failed: %v", err)
		} else {
			log.Printf("Sync completed successfully!")
		}
	}

	// Query to verify
	var count int
	err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sync_test`).Scan(&count)
	if err != nil {
		log.Fatalf("Failed to count: %v", err)
	}
	log.Printf("Total records in sync_test: %d", count)

	fmt.Println("\n=== Test Complete ===")
	fmt.Printf("Replica: %s\n", replicaPath)
	fmt.Printf("Remote: %s\n", syncURL)
	fmt.Println("Check the remote database to verify sync worked!")
}
