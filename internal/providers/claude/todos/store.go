// Package todos provides access to Claude Code's native todo storage.
//
// Claude Code stores todos in ~/.claude/todos/<session-uuid>-agent-<session-uuid>.json
// as a JSON array of {content, status, activeForm} objects.
//
// SECURITY NOTE: Writing to ~/.claude/todos is outside the workspace and must
// be restricted to privileged hooks/daemons. Use AGENTCTL_ALLOW_PROVIDER_STATE=1
// to enable writes.
package todos

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/todosync"
)

// Store provides read/write access to Claude Code todo files
type Store struct {
	claudeHome string // ~/.claude
}

// NewStore creates a new Claude todos store
func NewStore(claudeHome string) *Store {
	if claudeHome == "" {
		home, _ := os.UserHomeDir()
		claudeHome = filepath.Join(home, ".claude")
	}
	return &Store{claudeHome: claudeHome}
}

// TodosDir returns the directory containing todo files
func (s *Store) TodosDir() string {
	return filepath.Join(s.claudeHome, "todos")
}

// FilePathForSession returns the expected file path for a session's todos
func (s *Store) FilePathForSession(sessionID string) string {
	// Claude Code uses format: <session-uuid>-agent-<session-uuid>.json
	filename := fmt.Sprintf("%s-agent-%s.json", sessionID, sessionID)
	return filepath.Join(s.TodosDir(), filename)
}

// FindSessionFile attempts to locate the todo file for a session.
// If sessionID is provided, it looks for that specific file.
// Otherwise, it returns the most recently modified file.
func (s *Store) FindSessionFile(sessionID string) (string, error) {
	todosDir := s.TodosDir()

	// If session ID provided, look for exact match
	if sessionID != "" {
		expectedPath := s.FilePathForSession(sessionID)
		if _, err := os.Stat(expectedPath); err == nil {
			return expectedPath, nil
		}

		// Try scanning for files containing the session ID
		entries, err := os.ReadDir(todosDir)
		if err != nil {
			return "", fmt.Errorf("read todos dir: %w", err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if strings.Contains(entry.Name(), sessionID) {
				return filepath.Join(todosDir, entry.Name()), nil
			}
		}

		return "", fmt.Errorf("no todo file found for session: %s", sessionID)
	}

	// No session ID - find most recent file
	entries, err := os.ReadDir(todosDir)
	if err != nil {
		return "", fmt.Errorf("read todos dir: %w", err)
	}

	type fileInfo struct {
		path    string
		modTime int64
	}
	var files []fileInfo

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{
			path:    filepath.Join(todosDir, entry.Name()),
			modTime: info.ModTime().UnixNano(),
		})
	}

	if len(files) == 0 {
		return "", fmt.Errorf("no todo files found in %s", todosDir)
	}

	// Sort by modification time descending
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime > files[j].modTime
	})

	return files[0].path, nil
}

// Read loads todos from a session's file
func (s *Store) Read(sessionID string) ([]todosync.ClaudeTodo, error) {
	filePath, err := s.FindSessionFile(sessionID)
	if err != nil {
		return nil, err
	}

	return s.ReadFile(filePath)
}

// ReadFile loads todos from a specific file path
func (s *Store) ReadFile(filePath string) ([]todosync.ClaudeTodo, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read todo file: %w", err)
	}

	var todos []todosync.ClaudeTodo
	if err := json.Unmarshal(data, &todos); err != nil {
		return nil, fmt.Errorf("parse todo file: %w", err)
	}

	return todos, nil
}

// WriteOptions configures write behavior
type WriteOptions struct {
	// AllowProviderState must be true to enable writes (security gate)
	AllowProviderState bool
	// LastHash is the expected file hash for conflict detection
	LastHash string
}

// Write saves todos to a session's file using atomic write.
// Returns the new file hash for conflict detection.
func (s *Store) Write(sessionID string, todos []todosync.ClaudeTodo, opts WriteOptions) (string, error) {
	if !opts.AllowProviderState {
		return "", fmt.Errorf("write denied: AGENTCTL_ALLOW_PROVIDER_STATE not set")
	}

	filePath := s.FilePathForSession(sessionID)
	return s.WriteFile(filePath, todos, opts)
}

// WriteFile saves todos to a specific file path using atomic write.
// Returns the new file hash for conflict detection.
func (s *Store) WriteFile(filePath string, todos []todosync.ClaudeTodo, opts WriteOptions) (string, error) {
	if !opts.AllowProviderState {
		return "", fmt.Errorf("write denied: AGENTCTL_ALLOW_PROVIDER_STATE not set")
	}

	// Conflict detection
	if opts.LastHash != "" {
		currentHash, err := hashFile(filePath)
		if err == nil && currentHash != opts.LastHash {
			return "", fmt.Errorf("conflict detected: file was modified (expected %s, got %s)", opts.LastHash, currentHash)
		}
	}

	// Ensure directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create todos dir: %w", err)
	}

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(todos, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal todos: %w", err)
	}

	// Atomic write: temp file -> fsync -> rename
	tmpFile, err := os.CreateTemp(dir, ".todo-*.json.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // Clean up on error

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("write temp file: %w", err)
	}

	// Fsync for durability
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return "", fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return "", fmt.Errorf("close temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, filePath); err != nil {
		return "", fmt.Errorf("rename temp to final: %w", err)
	}

	// Return hash of new content
	return hashBytes(data), nil
}

// FileHash returns the SHA256 hash of a todo file's contents
func (s *Store) FileHash(sessionID string) (string, error) {
	filePath, err := s.FindSessionFile(sessionID)
	if err != nil {
		return "", err
	}
	return hashFile(filePath)
}

// hashFile computes SHA256 hash of a file
func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashBytes computes SHA256 hash of bytes
func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ExtractSessionFromPath extracts the session ID from a Claude todo file path.
// Format: <session-uuid>-agent-<session-uuid>.json
func ExtractSessionFromPath(filePath string) string {
	base := filepath.Base(filePath)
	base = strings.TrimSuffix(base, ".json")

	// Split by "-agent-" to get the first UUID
	parts := strings.Split(base, "-agent-")
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}
