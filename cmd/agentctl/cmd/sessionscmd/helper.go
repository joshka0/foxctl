// Package sessionscmd provides helpers for session-related CLI commands.
package sessionscmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
	"github.com/spf13/cobra"
)

// WithConfig loads configuration once for a command invocation and exposes it to the callback.
func WithConfig(cmd *cobra.Command, fn func(context.Context, config.Config) error) error {
	ctx := cmd.Context()
	if cfg, ok := config.FromContext(ctx); ok {
		return fn(ctx, cfg)
	}

	cfg, err := config.Load(ctx)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx = config.WithContext(ctx, cfg)
	cmd.SetContext(ctx)
	return fn(ctx, cfg)
}

// WithSessionStore opens the sessions store for the provided configuration and ensures cleanup.
func WithSessionStore(ctx context.Context, cfg config.Config, fn func(storage.SessionStore) error) error {
	store, err := sessions.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open sessions store: %w", err)
	}
	defer func() {
		errs.Ignore(store.Close(), "close sessions store helper")
	}()
	return fn(store)
}

// WriteOK renders a success envelope with the provided command name and payload.
func WriteOK(out io.Writer, command string, data any) error {
	return protocol.WriteOK(out, command, data, protocol.WithSource("run"))
}

// WriteNotFound renders an ENOTFOUND error envelope for the given session ID.
func WriteNotFound(out io.Writer, command, sessionID string) error {
	data := map[string]any{
		"session_id": sessionID,
		"hint":       fmt.Sprintf("No session with ID %q exists. Use 'agentctl sessions list' to see available sessions.", sessionID),
	}
	env := protocol.Error(
		command,
		protocol.ErrorCodeENotFound,
		fmt.Sprintf("session %q not found", sessionID),
		data,
	)
	return protocol.Write(out, env)
}

// WriteArgError renders an EARG error envelope for invalid arguments.
func WriteArgError(out io.Writer, command, message, hint string) error {
	data := map[string]any{
		"hint": hint,
	}
	env := protocol.Error(
		command,
		protocol.ErrorCodeEARG,
		message,
		data,
	)
	return protocol.Write(out, env)
}

// UncapturedSession represents a Claude Code session JSONL file that hasn't been captured.
type UncapturedSession struct {
	SessionID     string `json:"session_id"`
	ProjectName   string `json:"project_name"`
	WorkspacePath string `json:"workspace_path"`
	JSONLPath     string `json:"jsonl_path"`
	SizeBytes     int64  `json:"size_bytes"`
}

// ClaudeProjectsDir returns the path to Claude Code's projects directory.
func ClaudeProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// uuidRegex matches UUID-format session IDs (e.g., ccf28c56-45c8-494e-a764-9a97e24a358d)
var uuidRegex = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// FindUncapturedSessions scans the Claude projects directory for JSONL files
// that haven't been captured yet.
func FindUncapturedSessions(projectsDir string, captured map[string]bool, projectFilter string) ([]UncapturedSession, error) {
	if projectsDir == "" {
		return nil, fmt.Errorf("projects directory not found")
	}

	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, fmt.Errorf("read projects dir: %w", err)
	}

	uncaptured := []UncapturedSession{}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Parse workspace path from directory name (e.g., -Users-jkatigbak-repos-personal-agentctl)
		workspacePath := decodeWorkspacePath(entry.Name())
		projectName := filepath.Base(workspacePath)

		// Apply project filter
		if projectFilter != "" && projectName != projectFilter {
			continue
		}

		// Scan for JSONL files in this project directory
		projectDir := filepath.Join(projectsDir, entry.Name())
		files, err := os.ReadDir(projectDir)
		if err != nil {
			continue // Skip inaccessible directories
		}

		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
				continue
			}

			// Extract session ID from filename
			sessionID := strings.TrimSuffix(file.Name(), ".jsonl")

			// Only process UUID-format session IDs (skip agent-* files)
			if !uuidRegex.MatchString(sessionID) {
				continue
			}

			// Check if already captured
			if captured[sessionID] {
				continue
			}

			// Get file size
			info, err := file.Info()
			if err != nil {
				continue
			}

			uncaptured = append(uncaptured, UncapturedSession{
				SessionID:     sessionID,
				ProjectName:   projectName,
				WorkspacePath: workspacePath,
				JSONLPath:     filepath.Join(projectDir, file.Name()),
				SizeBytes:     info.Size(),
			})
		}
	}

	// Sort by size descending (largest sessions first)
	sort.Slice(uncaptured, func(i, j int) bool {
		return uncaptured[i].SizeBytes > uncaptured[j].SizeBytes
	})

	return uncaptured, nil
}

// decodeWorkspacePath converts Claude Code's encoded directory name back to a path.
// e.g., "-Users-jkatigbak-repos-personal-agentctl" -> "/Users/jkatigbak/repos/personal/agentctl"
func decodeWorkspacePath(encoded string) string {
	// Replace leading dash with /
	if strings.HasPrefix(encoded, "-") {
		encoded = "/" + encoded[1:]
	}
	// Replace remaining dashes with /
	return strings.ReplaceAll(encoded, "-", "/")
}
