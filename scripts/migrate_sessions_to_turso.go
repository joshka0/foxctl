//go:build ignore

package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	_ "github.com/tursodatabase/go-libsql"
)

func main() {
	tursoURL := os.Getenv("TURSO_DATABASE_URL")
	tursoToken := os.Getenv("TURSO_AUTH_TOKEN")
	localDB := os.Getenv("LOCAL_SESSIONS_DB")

	if tursoURL == "" || tursoToken == "" {
		log.Fatal("TURSO_DATABASE_URL and TURSO_AUTH_TOKEN must be set")
	}
	if localDB == "" {
		home, _ := os.UserHomeDir()
		localDB = home + "/.agentctl/storage/sessions.db"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Open local SQLite database
	log.Printf("Opening local database: %s", localDB)
	local, err := sql.Open("sqlite3", localDB)
	if err != nil {
		log.Fatalf("Failed to open local database: %v", err)
	}
	defer local.Close()

	// Open Turso database
	dsn := fmt.Sprintf("%s?authToken=%s", tursoURL, tursoToken)
	log.Printf("Connecting to Turso: %s", tursoURL)
	turso, err := sql.Open("libsql", dsn)
	if err != nil {
		log.Fatalf("Failed to open Turso database: %v", err)
	}
	defer turso.Close()

	// Test Turso connection
	if err := turso.PingContext(ctx); err != nil {
		log.Fatalf("Failed to connect to Turso: %v", err)
	}
	log.Println("Connected to Turso successfully")

	// Query sessions with embeddings from local DB
	rows, err := local.QueryContext(ctx, `
		SELECT id, workspace_path, project_name, summary, accomplished, decisions,
		       gotchas, tags, key_files, started_at, ended_at, embedding,
		       embedding_model, created_at, updated_at
		FROM sessions
		WHERE embedding IS NOT NULL
	`)
	if err != nil {
		log.Fatalf("Failed to query local sessions: %v", err)
	}
	defer rows.Close()

	// Prepare insert statement for Turso
	insertSQL := `
		INSERT OR REPLACE INTO sessions
		(id, workspace_path, project_name, summary, accomplished, decisions,
		 gotchas, tags, key_files, started_at, ended_at, embedding,
		 embedding_model, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, vector(?), ?, ?, ?)
	`

	var migrated int
	for rows.Next() {
		var id, workspacePath, summary, accomplished, decisions, gotchas string
		var tags, keyFiles, startedAt, endedAt, createdAt, updatedAt string
		var projectName, embeddingModel sql.NullString
		var embeddingBlob []byte

		err := rows.Scan(&id, &workspacePath, &projectName, &summary, &accomplished,
			&decisions, &gotchas, &tags, &keyFiles, &startedAt, &endedAt,
			&embeddingBlob, &embeddingModel, &createdAt, &updatedAt)
		if err != nil {
			log.Printf("Failed to scan row: %v", err)
			continue
		}

		// Convert embedding blob to vector string format
		vectorStr := blobToVectorString(embeddingBlob)
		if vectorStr == "" {
			log.Printf("Skipping session %s: invalid embedding", id)
			continue
		}

		// Insert into Turso
		_, err = turso.ExecContext(ctx, insertSQL,
			id, workspacePath, nullStr(projectName), summary, accomplished,
			decisions, gotchas, tags, keyFiles, startedAt, endedAt,
			vectorStr, nullStr(embeddingModel), createdAt, updatedAt)
		if err != nil {
			log.Printf("Failed to insert session %s: %v", id, err)
			continue
		}

		migrated++
		log.Printf("Migrated session: %s (%.50s...)", id, summary)
	}

	if err := rows.Err(); err != nil {
		log.Fatalf("Error iterating rows: %v", err)
	}

	log.Printf("Migration complete: %d sessions migrated", migrated)

	// Verify by counting
	var count int
	err = turso.QueryRowContext(ctx, "SELECT COUNT(*) FROM sessions").Scan(&count)
	if err != nil {
		log.Printf("Failed to verify: %v", err)
	} else {
		log.Printf("Turso sessions table now has %d rows", count)
	}
}

func blobToVectorString(blob []byte) string {
	if len(blob) == 0 || len(blob)%4 != 0 {
		return ""
	}

	dims := len(blob) / 4
	values := make([]string, dims)

	for i := 0; i < dims; i++ {
		bits := binary.LittleEndian.Uint32(blob[i*4 : (i+1)*4])
		f := math.Float32frombits(bits)
		values[i] = fmt.Sprintf("%f", f)
	}

	return "[" + strings.Join(values, ",") + "]"
}

func nullStr(ns sql.NullString) any {
	if ns.Valid {
		return ns.String
	}
	return nil
}
