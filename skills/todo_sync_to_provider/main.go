// Package main implements the todo/sync_to_provider skill.
//
// This skill syncs todos TO a provider (e.g., Claude Code) FROM agentctl's
// task management system. It is the outbound sync direction.
//
// SECURITY NOTE: This skill writes to ~/.claude/todos which is outside the
// workspace. It requires AGENTCTL_ALLOW_PROVIDER_STATE=1 to enable writes.
package main

import (
	"context"
	"os"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/providers/claude/todos"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/todosync"
)

const command = "todo/sync_to_provider"

type input struct {
	// Provider is the target provider ("claude" is currently the only supported value)
	Provider string `json:"provider" validate:"required,oneof=claude"`

	// WorkspaceID is the workspace path
	WorkspaceID string `json:"workspace_id" validate:"required"`

	// SessionID is the session identifier (optional - will auto-detect if empty)
	SessionID string `json:"session_id"`

	// Order controls task ordering: "agentctl_rank", "stable", or "off"
	Order string `json:"order"`

	// MaxItems limits the number of tasks projected (0 = no limit)
	MaxItems int `json:"max_items"`

	// IncludeGlyphs adds status glyphs (▶, □, ✓) to content
	IncludeGlyphs *bool `json:"include_glyphs"`

	// IncludeDepHints adds dependency hints (⛓n) to content
	IncludeDepHints *bool `json:"include_dep_hints"`

	// DryRun shows what would be written without making changes
	DryRun bool `json:"dry_run"`
}

type output struct {
	Written   int                   `json:"written"`
	Updated   int                   `json:"updated"`
	Unchanged int                   `json:"unchanged"`
	FilePath  string                `json:"file_path,omitempty"`
	FileHash  string                `json:"file_hash,omitempty"`
	Todos     []todosync.ClaudeTodo `json:"todos,omitempty"`
	Warnings  []string              `json:"warnings,omitempty"`
	DryRun    bool                  `json:"dry_run,omitempty"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Check write permission
	allowProviderState := os.Getenv("AGENTCTL_ALLOW_PROVIDER_STATE") == "1"

	// Open task store
	taskStore, cleanup, err := sessionkit.OpenTasks(ctx, rc.Config)
	if err != nil {
		return err
	}
	defer cleanup()

	// Resolve session ID
	sessionID := in.SessionID
	if sessionID == "" {
		sessionID = rc.SessionID
	}

	// Build projection config
	cfg := todosync.DefaultProjectionConfig()
	if in.IncludeGlyphs != nil {
		cfg.IncludeGlyphs = *in.IncludeGlyphs
	}
	if in.IncludeDepHints != nil {
		cfg.IncludeDepHints = *in.IncludeDepHints
	}

	// Default order
	order := in.Order
	if order == "" {
		order = "agentctl_rank"
	}

	// Create sync service and run outbound sync
	syncService := todosync.NewService(taskStore)
	result, err := syncService.SyncToProvider(ctx, todosync.OutboundSyncInput{
		WorkspaceID: in.WorkspaceID,
		SessionID:   sessionID,
		Order:       order,
		MaxItems:    in.MaxItems,
		Config:      cfg,
		DryRun:      in.DryRun,
	})
	if err != nil {
		return err
	}

	// Build output
	out := output{
		Written:   result.Written,
		Updated:   result.Updated,
		Unchanged: result.Unchanged,
		Warnings:  result.Warnings,
		DryRun:    in.DryRun,
	}

	// Include todos in output for dry run
	if in.DryRun {
		out.Todos = result.Todos
	}

	// Write to file if not dry run and we have permission
	if !in.DryRun && allowProviderState && sessionID != "" {
		store := todos.NewStore("")
		filePath := store.FilePathForSession(sessionID)

		// Get current file hash for conflict detection
		lastHash, _ := store.FileHash(sessionID)

		newHash, err := store.Write(sessionID, result.Todos, todos.WriteOptions{
			AllowProviderState: true,
			LastHash:           lastHash,
		})
		if err != nil {
			out.Warnings = append(out.Warnings, "Failed to write file: "+err.Error())
		} else {
			out.FilePath = filePath
			out.FileHash = newHash
		}
	} else if !in.DryRun && !allowProviderState {
		out.Warnings = append(out.Warnings, "Write skipped: AGENTCTL_ALLOW_PROVIDER_STATE not set")
	}

	return skillout.Emit(rc, command, out)
}
