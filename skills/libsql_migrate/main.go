//go:build cgo

// Package main implements the libsql/migrate skill for migrating SQLite to libsql.
package main

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	_ "github.com/mattn/go-sqlite3"
	"github.com/tursodatabase/go-libsql"
)

const command = "libsql/migrate"

// Input is the skill input schema.
type Input struct {
	// LibsqlURL is the libsql server URL (e.g., "http://100.x.x.x:8080")
	LibsqlURL string `json:"libsql_url" validate:"required"`

	// AuthToken is optional auth token for libsql server
	AuthToken string `json:"auth_token,omitempty"`

	// Scope determines what to migrate: "all", "memories", "sessions"
	Scope string `json:"scope" validate:"omitempty,oneof=all memories sessions"`

	// SourceDir is the source SQLite storage directory (defaults to ~/.agentctl/storage)
	SourceDir string `json:"source_dir,omitempty"`

	// BatchSize is the number of records to migrate per batch
	BatchSize int `json:"batch_size,omitempty"`

	// DryRun if true, shows what would be migrated without writing
	DryRun bool `json:"dry_run,omitempty"`

	// VectorDims is the embedding dimension (default 1024 for Voyage)
	VectorDims int `json:"vector_dims,omitempty"`
}

// Output is the skill output.
type Output struct {
	Scope            string            `json:"scope"`
	MemoriesMigrated int               `json:"memories_migrated"`
	SessionsMigrated int               `json:"sessions_migrated"`
	Errors           int               `json:"errors"`
	DurationMs       int64             `json:"duration_ms"`
	ErrorDetails     []string          `json:"error_details,omitempty"`
	DryRun           bool              `json:"dry_run,omitempty"`
	Tables           []TableStats      `json:"tables,omitempty"`
	MemoryTypes      []MemoryTypeStats `json:"memory_types"`
}

// TableStats shows migration stats per table.
type TableStats struct {
	Name      string `json:"name"`
	Total     int    `json:"total"`
	WithEmbed int    `json:"with_embedding"`
	Migrated  int    `json:"migrated"`
	Skipped   int    `json:"skipped"`
}

// MemoryTypeStats shows memory counts by type in named_memory.
type MemoryTypeStats struct {
	Type      string `json:"type"`
	Total     int    `json:"total"`
	WithEmbed int    `json:"with_embedding"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Set defaults
	if in.Scope == "" {
		in.Scope = "all"
	}
	if in.BatchSize <= 0 {
		in.BatchSize = 100
	}
	if in.VectorDims <= 0 {
		in.VectorDims = 1024 // Voyage default
	}
	if in.SourceDir == "" {
		in.SourceDir = filepath.Join(os.Getenv("HOME"), ".agentctl", "storage")
	}

	start := time.Now()
	output := Output{
		Scope:  in.Scope,
		DryRun: in.DryRun,
	}

	// Open libsql connection (unless dry run)
	var destDB *sql.DB
	var connector *libsql.Connector
	var tempDir string

	if !in.DryRun {
		var err error
		tempDir, err = os.MkdirTemp("", "libsql-migrate-*")
		if err != nil {
			return skillerr.Runtime("create temp dir", skillerr.WithCause(err))
		}
		defer os.RemoveAll(tempDir)

		replicaPath := filepath.Join(tempDir, "replica.db")

		// Build connector options
		opts := []libsql.Option{}
		if in.AuthToken != "" {
			opts = append(opts, libsql.WithAuthToken(in.AuthToken))
		}

		connector, err = libsql.NewEmbeddedReplicaConnector(replicaPath, in.LibsqlURL, opts...)
		if err != nil {
			return skillerr.Runtime("connect to libsql", skillerr.WithCause(err),
				skillerr.WithHint("Ensure sqld is running at "+in.LibsqlURL))
		}
		defer connector.Close()

		destDB = sql.OpenDB(connector)
		defer destDB.Close()

		// Test connection
		if err := destDB.PingContext(ctx); err != nil {
			return skillerr.Runtime("ping libsql", skillerr.WithCause(err))
		}
	}

	// Migrate memories
	if in.Scope == "all" || in.Scope == "memories" {
		stats, err := migrateMemories(ctx, in, destDB, &output)
		if err != nil {
			output.ErrorDetails = append(output.ErrorDetails, "memories: "+err.Error())
			output.Errors++
		} else {
			output.Tables = append(output.Tables, stats)
			output.MemoriesMigrated = stats.Migrated
		}
	}

	// Migrate sessions
	if in.Scope == "all" || in.Scope == "sessions" {
		stats, err := migrateSessions(ctx, in, destDB, &output)
		if err != nil {
			output.ErrorDetails = append(output.ErrorDetails, "sessions: "+err.Error())
			output.Errors++
		} else {
			output.Tables = append(output.Tables, stats)
			output.SessionsMigrated = stats.Migrated
		}
	}

	// Sync to remote if not dry run
	if !in.DryRun && connector != nil {
		if _, err := connector.Sync(); err != nil {
			output.ErrorDetails = append(output.ErrorDetails, "sync: "+err.Error())
			output.Errors++
		}
	}

	output.DurationMs = time.Since(start).Milliseconds()
	return skillout.Emit(rc, command, output)
}

func migrateMemories(ctx context.Context, in Input, destDB *sql.DB, output *Output) (TableStats, error) {
	stats := TableStats{Name: "named_memory"}

	// Open source SQLite
	srcPath := filepath.Join(in.SourceDir, "memory.db")
	srcDB, err := sql.Open("sqlite3", srcPath+"?mode=ro")
	if err != nil {
		return stats, fmt.Errorf("open source: %w", err)
	}
	defer srcDB.Close()

	// Count records
	row := srcDB.QueryRowContext(ctx, `
		SELECT COUNT(*), SUM(CASE WHEN embedding IS NOT NULL THEN 1 ELSE 0 END)
		FROM named_memory
	`)
	if err := row.Scan(&stats.Total, &stats.WithEmbed); err != nil {
		return stats, fmt.Errorf("count records: %w", err)
	}

	typeStats, err := loadMemoryTypeStats(ctx, srcDB)
	if err != nil {
		output.ErrorDetails = append(output.ErrorDetails, "memory types: "+err.Error())
		output.Errors++
	} else {
		output.MemoryTypes = typeStats
	}

	if in.DryRun {
		stats.Migrated = stats.WithEmbed
		return stats, nil
	}

	// Create table in libsql with vector column
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS named_memory (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT NOT NULL,
			workspace TEXT NOT NULL,
			summary TEXT,
			result BLOB NOT NULL,
			digests TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			last_accessed TEXT NOT NULL,
			access_count INTEGER NOT NULL DEFAULT 0,
			embedding F32_BLOB(%d),
			session_id TEXT,
			pinned_at TEXT DEFAULT NULL,
			UNIQUE(name, workspace)
		)
	`, in.VectorDims)
	if _, err := destDB.ExecContext(ctx, createSQL); err != nil {
		return stats, fmt.Errorf("create table: %w", err)
	}

	// Create indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_named_memory_ws_updated ON named_memory(workspace, updated_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_session ON named_memory(session_id)`,
	}
	for _, idx := range indexes {
		if _, err := destDB.ExecContext(ctx, idx); err != nil {
			output.ErrorDetails = append(output.ErrorDetails, "index: "+err.Error())
		}
	}

	// Get existing IDs from destination to skip them
	existingIDs := make(map[string]bool)
	existingRows, err := destDB.QueryContext(ctx, `SELECT id FROM named_memory`)
	if err == nil {
		defer existingRows.Close()
		for existingRows.Next() {
			var id string
			if existingRows.Scan(&id) == nil {
				existingIDs[id] = true
			}
		}
	}

	// Migrate only records not in destination
	rows, err := srcDB.QueryContext(ctx, `
		SELECT id, name, type, workspace, summary, result, digests,
		       created_at, updated_at, last_accessed, access_count,
		       embedding, session_id, pinned_at
		FROM named_memory
		WHERE embedding IS NOT NULL
	`)
	if err != nil {
		return stats, fmt.Errorf("query source: %w", err)
	}
	defer rows.Close()

	insertSQL := `
		INSERT INTO named_memory
		(id, name, type, workspace, summary, result, digests,
		 created_at, updated_at, last_accessed, access_count,
		 embedding, session_id, pinned_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	for rows.Next() {
		var id, name, typ, workspace, summary, digests string
		var result, embedding []byte
		var createdAt, updatedAt, lastAccessed string
		var accessCount int
		var sessionID, pinnedAt sql.NullString

		if err := rows.Scan(&id, &name, &typ, &workspace, &summary, &result, &digests,
			&createdAt, &updatedAt, &lastAccessed, &accessCount,
			&embedding, &sessionID, &pinnedAt); err != nil {
			output.ErrorDetails = append(output.ErrorDetails, "scan: "+err.Error())
			output.Errors++
			continue
		}

		// Skip if already exists in destination
		if existingIDs[id] {
			stats.Skipped++
			continue
		}

		// Convert embedding to binary float32 if stored as JSON text
		binaryEmbed, err := normalizeEmbedding(embedding, in.VectorDims)
		if err != nil {
			stats.Skipped++
			continue
		}

		_, err = destDB.ExecContext(ctx, insertSQL,
			id, name, typ, workspace, summary, result, digests,
			createdAt, updatedAt, lastAccessed, accessCount,
			binaryEmbed, sessionID, pinnedAt)
		if err != nil {
			output.ErrorDetails = append(output.ErrorDetails, fmt.Sprintf("insert %s: %v", name, err))
			output.Errors++
			continue
		}
		stats.Migrated++
	}

	return stats, nil
}

func loadMemoryTypeStats(ctx context.Context, srcDB *sql.DB) ([]MemoryTypeStats, error) {
	rows, err := srcDB.QueryContext(ctx, `
		SELECT COALESCE("type", 'unknown') AS memory_type,
		       COUNT(*) AS total,
		       SUM(CASE WHEN embedding IS NOT NULL THEN 1 ELSE 0 END) AS with_embedding
		FROM named_memory
		GROUP BY memory_type
		ORDER BY total DESC, memory_type
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []MemoryTypeStats
	for rows.Next() {
		var typ string
		var total int
		var withEmbed sql.NullInt64
		if err := rows.Scan(&typ, &total, &withEmbed); err != nil {
			return nil, err
		}
		count := 0
		if withEmbed.Valid {
			count = int(withEmbed.Int64)
		}
		stats = append(stats, MemoryTypeStats{
			Type:      typ,
			Total:     total,
			WithEmbed: count,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return stats, nil
}

func migrateSessions(ctx context.Context, in Input, destDB *sql.DB, output *Output) (TableStats, error) {
	stats := TableStats{Name: "sessions"}

	// Open source SQLite
	srcPath := filepath.Join(in.SourceDir, "sessions.db")
	srcDB, err := sql.Open("sqlite3", srcPath+"?mode=ro")
	if err != nil {
		return stats, fmt.Errorf("open source: %w", err)
	}
	defer srcDB.Close()

	// Count records
	row := srcDB.QueryRowContext(ctx, `
		SELECT COUNT(*), SUM(CASE WHEN embedding IS NOT NULL THEN 1 ELSE 0 END)
		FROM sessions
	`)
	if err := row.Scan(&stats.Total, &stats.WithEmbed); err != nil {
		return stats, fmt.Errorf("count records: %w", err)
	}

	if in.DryRun {
		stats.Migrated = stats.WithEmbed
		return stats, nil
	}

	// Create table in libsql with vector column
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			workspace_path TEXT NOT NULL,
			project_name TEXT,
			git_branch TEXT,
			claude_version TEXT,
			started_at TEXT,
			ended_at TEXT,
			summary TEXT,
			accomplished TEXT,
			decisions TEXT,
			gotchas TEXT,
			tags TEXT,
			key_files TEXT,
			tools_pattern TEXT,
			message_count INTEGER DEFAULT 0,
			user_turns INTEGER DEFAULT 0,
			tool_invocations INTEGER DEFAULT 0,
			total_tokens INTEGER DEFAULT 0,
			raw_jsonl_path TEXT,
			embedding F32_BLOB(%d),
			embedding_model TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			user_insights TEXT,
			parent_session_id TEXT,
			agent_id TEXT NOT NULL DEFAULT 'agentctl',
			status TEXT NOT NULL DEFAULT 'ok',
			key_questions TEXT
		)
	`, in.VectorDims)
	if _, err := destDB.ExecContext(ctx, createSQL); err != nil {
		return stats, fmt.Errorf("create table: %w", err)
	}

	// Create indexes
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_sessions_workspace ON sessions(workspace_path)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_project ON sessions(project_name)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_started ON sessions(started_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_parent ON sessions(parent_session_id)`,
	}
	for _, idx := range indexes {
		if _, err := destDB.ExecContext(ctx, idx); err != nil {
			output.ErrorDetails = append(output.ErrorDetails, "index: "+err.Error())
		}
	}

	// Get existing IDs from destination to skip them
	existingIDs := make(map[string]bool)
	existingRows, err := destDB.QueryContext(ctx, `SELECT id FROM sessions`)
	if err == nil {
		defer existingRows.Close()
		for existingRows.Next() {
			var id string
			if existingRows.Scan(&id) == nil {
				existingIDs[id] = true
			}
		}
	}

	// Migrate only records not in destination
	rows, err := srcDB.QueryContext(ctx, `
		SELECT id, workspace_path, project_name, git_branch, claude_version,
		       started_at, ended_at, summary, accomplished, decisions, gotchas,
	       tags, key_files, tools_pattern, message_count, user_turns,
	       tool_invocations, total_tokens, raw_jsonl_path, embedding,
	       embedding_model, created_at, updated_at, user_insights,
	       parent_session_id, agent_id, agent_type, status, key_questions
		FROM sessions
		WHERE embedding IS NOT NULL
	`)
	if err != nil {
		return stats, fmt.Errorf("query source: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id, workspacePath, createdAt, updatedAt, agentID, status string
		var projectName, gitBranch, claudeVersion sql.NullString
		var startedAt, endedAt, summary sql.NullString
		var accomplished, decisions, gotchas, tags, keyFiles, toolsPattern sql.NullString
		var messageCount, userTurns, toolInvocations, totalTokens sql.NullInt64
		var rawJSONLPath, embeddingModel, userInsights, parentSessionID, agentType, keyQuestions sql.NullString
		var embedding []byte

		if err := rows.Scan(&id, &workspacePath, &projectName, &gitBranch, &claudeVersion,
			&startedAt, &endedAt, &summary, &accomplished, &decisions, &gotchas,
			&tags, &keyFiles, &toolsPattern, &messageCount, &userTurns,
			&toolInvocations, &totalTokens, &rawJSONLPath, &embedding,
			&embeddingModel, &createdAt, &updatedAt, &userInsights,
			&parentSessionID, &agentID, &agentType, &status, &keyQuestions); err != nil {
			output.ErrorDetails = append(output.ErrorDetails, "scan session: "+err.Error())
			output.Errors++
			continue
		}

		// Skip if already exists in destination
		if existingIDs[id] {
			stats.Skipped++
			continue
		}

		// Convert embedding to binary float32 if stored as JSON text
		binaryEmbed, err := normalizeEmbedding(embedding, in.VectorDims)
		if err != nil {
			stats.Skipped++
			continue
		}

		_, err = destDB.ExecContext(ctx, `
			INSERT INTO sessions
			(id, workspace_path, project_name, git_branch, claude_version,
			 started_at, ended_at, summary, accomplished, decisions, gotchas,
			 tags, key_files, tools_pattern, message_count, user_turns,
			 tool_invocations, total_tokens, raw_jsonl_path, embedding,
			 embedding_model, created_at, updated_at, user_insights,
		 parent_session_id, agent_id, agent_type, status, key_questions)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, workspacePath, projectName, gitBranch, claudeVersion,
			startedAt, endedAt, summary, accomplished, decisions, gotchas,
			tags, keyFiles, toolsPattern, messageCount, userTurns,
			toolInvocations, totalTokens, rawJSONLPath, binaryEmbed,
			embeddingModel, createdAt, updatedAt, userInsights,
			parentSessionID, agentID, agentType, status, keyQuestions)
		if err != nil {
			output.ErrorDetails = append(output.ErrorDetails, fmt.Sprintf("insert session %s: %v", id, err))
			output.Errors++
			continue
		}
		stats.Migrated++
	}

	return stats, nil
}

// normalizeEmbedding converts embeddings to binary float32 format.
// It handles both JSON text arrays (like "[-0.1, 0.2, ...]") and raw binary float32.
// Returns error if the embedding doesn't match expectedDims.
func normalizeEmbedding(data []byte, expectedDims int) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty embedding")
	}

	// Check if it's already binary float32 (correct size)
	expectedBytes := expectedDims * 4
	if len(data) == expectedBytes {
		return data, nil
	}

	// Check if it looks like JSON (starts with '[')
	if data[0] == '[' {
		var floats []float64
		if err := json.Unmarshal(data, &floats); err != nil {
			return nil, fmt.Errorf("parse JSON embedding: %w", err)
		}

		// Check dimensions match
		if len(floats) != expectedDims {
			return nil, fmt.Errorf("dimension mismatch: got %d, expected %d", len(floats), expectedDims)
		}

		// Convert to binary float32
		buf := make([]byte, expectedBytes)
		for i, f := range floats {
			bits := math.Float32bits(float32(f))
			binary.LittleEndian.PutUint32(buf[i*4:], bits)
		}
		return buf, nil
	}

	// Unknown format or dimension mismatch
	return nil, fmt.Errorf("unexpected embedding size: %d bytes", len(data))
}
