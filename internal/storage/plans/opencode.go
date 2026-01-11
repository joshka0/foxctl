// Package plans provides types and utilities for parsing plan files.
// This file adds support for OpenCode's storage (SQLite sessions + JSON todos).
package plans

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // Pure Go SQLite driver
)

// Provider identifies the source of plans/todos.
type Provider string

const (
	// ProviderClaude is Claude Code (~/.claude/plans/*.md).
	ProviderClaude Provider = "claude"
	// ProviderOpenCode is OpenCode (.opencode/storage/todo/*.json).
	ProviderOpenCode Provider = "opencode"
)

// OpenCodeTodo represents a single todo item from OpenCode's storage.
// Based on OpenCode's session/todo.ts Info type.
type OpenCodeTodo struct {
	Content string `json:"content"`
	Status  string `json:"status"` // "pending", "in_progress", "completed"
}

// OpenCodeSession represents a session from OpenCode's SQLite database.
type OpenCodeSession struct {
	ID             string    `json:"id"`
	ParentID       string    `json:"parent_session_id,omitempty"`
	Title          string    `json:"title"`
	MessageCount   int       `json:"message_count"`
	PromptTokens   int       `json:"prompt_tokens"`
	CompletionToks int       `json:"completion_tokens"`
	Cost           float64   `json:"cost"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// OpenCodeTodoFile represents a parsed OpenCode todo file.
type OpenCodeTodoFile struct {
	SessionID   string         `json:"session_id"`
	FilePath    string         `json:"file_path"`
	Todos       []OpenCodeTodo `json:"todos"`
	ContentHash string         `json:"content_hash"`
	ModTime     time.Time      `json:"mod_time"`
}

// OpenCodeParser handles parsing of OpenCode todo files and SQLite sessions.
type OpenCodeParser struct {
	storageDir string // e.g., ".opencode/storage" or "~/.opencode/storage"
	dbPath     string // e.g., ".opencode/opencode.db"
}

// NewOpenCodeParser creates a parser for OpenCode todos.
// If storageDir is empty, it will check both workspace and home directories.
func NewOpenCodeParser(storageDir string) *OpenCodeParser {
	return &OpenCodeParser{storageDir: storageDir}
}

// NewOpenCodeParserWithDB creates a parser that prioritizes SQLite database.
func NewOpenCodeParserWithDB(dbPath string) *OpenCodeParser {
	return &OpenCodeParser{dbPath: dbPath}
}

// DetectOpenCodeDirs returns all directories containing OpenCode todos.
// Checks both workspace (.opencode/storage) and home (~/.opencode/storage).
func DetectOpenCodeDirs(workspace string) []string {
	var dirs []string

	// Check workspace-local .opencode/storage/todo
	if workspace != "" {
		localDir := filepath.Join(workspace, ".opencode", "storage", "todo")
		if info, err := os.Stat(localDir); err == nil && info.IsDir() {
			dirs = append(dirs, localDir)
		}
	}

	// Check home ~/.opencode/storage/todo
	if home, err := os.UserHomeDir(); err == nil {
		homeDir := filepath.Join(home, ".opencode", "storage", "todo")
		if info, err := os.Stat(homeDir); err == nil && info.IsDir() {
			dirs = append(dirs, homeDir)
		}
	}

	return dirs
}

// DetectOpenCodeDBs returns all SQLite databases containing OpenCode sessions.
// Checks both workspace (.opencode/opencode.db) and home (~/.opencode/opencode.db).
func DetectOpenCodeDBs(workspace string) []string {
	var dbs []string

	// Check workspace-local .opencode/opencode.db
	if workspace != "" {
		localDB := filepath.Join(workspace, ".opencode", "opencode.db")
		if info, err := os.Stat(localDB); err == nil && !info.IsDir() {
			dbs = append(dbs, localDB)
		}
	}

	// Check home ~/.opencode/opencode.db
	if home, err := os.UserHomeDir(); err == nil {
		homeDB := filepath.Join(home, ".opencode", "opencode.db")
		if info, err := os.Stat(homeDB); err == nil && !info.IsDir() {
			dbs = append(dbs, homeDB)
		}
	}

	return dbs
}

// ReadSessionsFromDB reads sessions from an OpenCode SQLite database.
func ReadSessionsFromDB(ctx context.Context, dbPath string) ([]OpenCodeSession, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("opencode: open db: %w", err)
	}
	defer db.Close()

	query := `
		SELECT id, COALESCE(parent_session_id, ''), title, message_count,
		       prompt_tokens, completion_tokens, cost, created_at, updated_at
		FROM sessions
		WHERE message_count > 0
		ORDER BY updated_at DESC
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("opencode: query sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]OpenCodeSession, 0, 16)
	for rows.Next() {
		var s OpenCodeSession
		var createdMS, updatedMS int64
		if err := rows.Scan(&s.ID, &s.ParentID, &s.Title, &s.MessageCount,
			&s.PromptTokens, &s.CompletionToks, &s.Cost, &createdMS, &updatedMS); err != nil {
			return nil, fmt.Errorf("opencode: scan session: %w", err)
		}

		// Convert milliseconds to time.Time
		s.CreatedAt = time.UnixMilli(createdMS)
		s.UpdatedAt = time.UnixMilli(updatedMS)

		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("opencode: iterate sessions: %w", err)
	}

	return sessions, nil
}

// SessionToPlanInfo converts an OpenCode session to a PlanInfo.
func SessionToPlanInfo(s OpenCodeSession, dbPath string) *PlanInfo {
	// Create hash from session ID and update time
	hashInput := fmt.Sprintf("%s:%d", s.ID, s.UpdatedAt.UnixMilli())
	hash := sha256.Sum256([]byte(hashInput))
	hashStr := "sha256:" + hex.EncodeToString(hash[:])

	// Create a section for the session info
	sections := []Section{
		{
			Level:      2,
			Title:      s.Title,
			Content:    fmt.Sprintf("Messages: %d, Tokens: %d/%d", s.MessageCount, s.PromptTokens, s.CompletionToks),
			LineNumber: 1,
		},
	}

	return &PlanInfo{
		FilePath:    dbPath + "#session:" + s.ID,
		FileName:    s.ID + ".session",
		Title:       s.Title,
		ContentHash: hashStr,
		ModTime:     s.UpdatedAt,
		Sections:    sections,
		Status:      StatusActive,
	}
}

// ParseFile reads and parses an OpenCode todo JSON file.
func (p *OpenCodeParser) ParseFile(path string) (*OpenCodeTodoFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("opencode: read file: %w", err)
	}

	// Get file info for mod time
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("opencode: stat file: %w", err)
	}

	// Compute hash
	hash := sha256.Sum256(data)
	hashStr := "sha256:" + hex.EncodeToString(hash[:])

	// Parse JSON array of todos
	var todos []OpenCodeTodo
	if err := json.Unmarshal(data, &todos); err != nil {
		return nil, fmt.Errorf("opencode: parse json: %w", err)
	}

	// Extract session ID from filename (e.g., "abc123.json" -> "abc123")
	sessionID := strings.TrimSuffix(filepath.Base(path), ".json")

	return &OpenCodeTodoFile{
		SessionID:   sessionID,
		FilePath:    path,
		Todos:       todos,
		ContentHash: hashStr,
		ModTime:     info.ModTime(),
	}, nil
}

// Detect finds all OpenCode todo files (from JSON) or sessions (from SQLite).
func (p *OpenCodeParser) Detect(workspace string) ([]OpenCodeTodoFile, error) {
	// First, try to detect from SQLite databases
	dbs := DetectOpenCodeDBs(workspace)
	if p.dbPath != "" {
		dbs = append([]string{p.dbPath}, dbs...)
	}

	var allFiles []OpenCodeTodoFile

	// Read sessions from SQLite databases
	for _, dbPath := range dbs {
		sessions, err := ReadSessionsFromDB(context.Background(), dbPath)
		if err != nil {
			continue // Skip databases that can't be read
		}

		for _, session := range sessions {
			// Convert session to OpenCodeTodoFile format for compatibility
			todoFile := OpenCodeTodoFile{
				SessionID:   session.ID,
				FilePath:    dbPath + "#session:" + session.ID,
				ContentHash: fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%d", session.ID, session.UpdatedAt.UnixMilli())))),
				ModTime:     session.UpdatedAt,
				Todos: []OpenCodeTodo{
					{
						Content: session.Title,
						Status:  "pending", // Sessions are treated as pending plans
					},
				},
			}
			allFiles = append(allFiles, todoFile)
		}
	}

	// Fall back to JSON files if no SQLite sessions found
	if len(allFiles) == 0 {
		dirs := DetectOpenCodeDirs(workspace)
		if p.storageDir != "" {
			todoDir := filepath.Join(p.storageDir, "todo")
			if info, err := os.Stat(todoDir); err == nil && info.IsDir() {
				dirs = append(dirs, todoDir)
			}
		}

		for _, dir := range dirs {
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}

			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
					continue
				}

				path := filepath.Join(dir, entry.Name())
				todoFile, err := p.ParseFile(path)
				if err != nil {
					continue // Skip unparseable files
				}

				// Only include files with non-empty todos
				if len(todoFile.Todos) > 0 {
					allFiles = append(allFiles, *todoFile)
				}
			}
		}
	}

	// Sort by modification time (most recent first)
	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].ModTime.After(allFiles[j].ModTime)
	})

	return allFiles, nil
}

// ToPlanInfo converts an OpenCode todo file to a PlanInfo for compatibility.
func (f *OpenCodeTodoFile) ToPlanInfo() *PlanInfo {
	// Create a title from session ID
	title := fmt.Sprintf("OpenCode Session: %s", f.SessionID)

	// Create sections from todos
	sections := make([]Section, 0, len(f.Todos))
	for i, todo := range f.Todos {
		sections = append(sections, Section{
			Level:      2,
			Title:      todo.Content,
			Content:    fmt.Sprintf("Status: %s", todo.Status),
			LineNumber: i + 1,
		})
	}

	return &PlanInfo{
		FilePath:    f.FilePath,
		FileName:    filepath.Base(f.FilePath),
		Title:       title,
		ContentHash: f.ContentHash,
		ModTime:     f.ModTime,
		Sections:    sections,
		Status:      StatusActive,
	}
}

// ToSteps converts OpenCode todos to Steps for task import.
func (f *OpenCodeTodoFile) ToSteps() []Step {
	steps := make([]Step, 0, len(f.Todos))

	for i, todo := range f.Todos {
		// Skip completed todos for import
		if todo.Status == "completed" {
			continue
		}

		steps = append(steps, Step{
			Title:       todo.Content,
			Description: fmt.Sprintf("From OpenCode session %s", f.SessionID),
			SectionPath: []string{f.SessionID},
			Order:       i + 1,
		})
	}

	return steps
}

// DetectProvider determines which provider to use based on available directories.
// Returns the detected provider and the relevant directory/database path.
func DetectProvider(workspace string) (Provider, string) {
	// Check for OpenCode SQLite databases first (workspace-local takes priority)
	opencodeDbs := DetectOpenCodeDBs(workspace)
	if len(opencodeDbs) > 0 {
		return ProviderOpenCode, opencodeDbs[0]
	}

	// Check for OpenCode JSON todo directories
	opencodeDirs := DetectOpenCodeDirs(workspace)
	if len(opencodeDirs) > 0 {
		return ProviderOpenCode, opencodeDirs[0]
	}

	// Check for Claude Code plans
	if home, err := os.UserHomeDir(); err == nil {
		claudeDir := filepath.Join(home, ".claude", "plans")
		if info, err := os.Stat(claudeDir); err == nil && info.IsDir() {
			return ProviderClaude, claudeDir
		}
	}

	// Default to Claude (for backwards compatibility)
	return ProviderClaude, ""
}

// UnifiedDetector can detect plans from either Claude or OpenCode.
type UnifiedDetector struct {
	workspace string
	provider  Provider
}

// NewUnifiedDetector creates a detector that works with either provider.
func NewUnifiedDetector(workspace string, provider Provider) *UnifiedDetector {
	return &UnifiedDetector{
		workspace: workspace,
		provider:  provider,
	}
}

// Detect finds all plans/todos from the configured provider.
func (d *UnifiedDetector) Detect(opts DetectOptions) ([]PlanInfo, error) {
	switch d.provider {
	case ProviderOpenCode:
		parser := NewOpenCodeParser("")
		files, err := parser.Detect(d.workspace)
		if err != nil {
			return nil, err
		}

		plans := make([]PlanInfo, 0, len(files))
		for _, f := range files {
			plan := f.ToPlanInfo()
			if !opts.Since.IsZero() && f.ModTime.Before(opts.Since) {
				continue
			}
			plans = append(plans, *plan)
		}

		if opts.Limit > 0 && len(plans) > opts.Limit {
			plans = plans[:opts.Limit]
		}

		return plans, nil

	case ProviderClaude:
		fallthrough
	default:
		detector := NewDetector("")
		return detector.Detect(opts)
	}
}

// ExtractSteps extracts steps from plans based on the provider.
func (d *UnifiedDetector) ExtractSteps(plan *PlanInfo) []Step {
	switch d.provider {
	case ProviderOpenCode:
		// Check if this is a SQLite-based session (path contains #session:)
		if strings.Contains(plan.FilePath, "#session:") {
			parts := strings.SplitN(plan.FilePath, "#session:", 2)
			if len(parts) != 2 {
				return nil
			}
			dbPath := parts[0]
			sessionID := parts[1]

			// Read session from database
			sessions, err := ReadSessionsFromDB(context.Background(), dbPath)
			if err != nil {
				return nil
			}

			// Find the matching session
			for _, session := range sessions {
				if session.ID == sessionID {
					// Create a step from the session
					return []Step{
						{
							Title:       session.Title,
							Description: fmt.Sprintf("OpenCode session with %d messages", session.MessageCount),
							SectionPath: []string{sessionID},
							Order:       1,
						},
					}
				}
			}
			return nil
		}

		// Fall back to JSON file parsing
		parser := NewOpenCodeParser("")
		todoFile, err := parser.ParseFile(plan.FilePath)
		if err != nil {
			return nil
		}
		return todoFile.ToSteps()

	case ProviderClaude:
		fallthrough
	default:
		p := NewParser(DefaultParseOptions())
		return p.ExtractSteps(plan)
	}
}
