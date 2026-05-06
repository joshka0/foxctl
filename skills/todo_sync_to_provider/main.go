// Package main implements the todo/sync_to_provider skill for syncing tasks from foxctl to external providers.
//
// This skill syncs todos TO a provider (e.g., Claude Code) FROM foxctl's
// task management system. It is the outbound sync direction.
//
// SECURITY NOTE: This skill writes to ~/.claude/todos which is outside the
// workspace. It requires FOXCTL_ALLOW_PROVIDER_STATE=1 to enable writes.
package main

import (
	"context"
	"os"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/context/todosync"
	"github.com/joshka0/foxctl/internal/providers/claude/todos"
)

const command = "todo/sync_to_provider"

// input defines the skill input parameters for outbound todo synchronization with comprehensive configuration options.
type input struct {
	// Provider is the target provider ("claude" is currently the only supported value)
	Provider string `json:"provider" validate:"required,oneof=claude"`

	// WorkspaceID is the workspace path
	WorkspaceID string `json:"workspace_id" validate:"required"`

	// SessionID is the session identifier (optional - will auto-detect if empty)
	SessionID string `json:"session_id"`

	// Order controls task ordering: "foxctl_rank", "stable", or "off"
	Order string `json:"order"`

	// MaxItems limits the number of tasks projected (0 = no limit)
	MaxItems int `json:"max_items"`

	// IncludeGlyphs adds status glyphs (▶, □, ✓) to content
	IncludeGlyphs *bool `json:"include_glyphs"`

	// IncludeDepHints adds dependency hints (⛓n) to content
	IncludeDepHints *bool `json:"include_dep_hints"`

	// DryRun shows what would be written without making changes
	DryRun bool `json:"dry_run"`

	// AllowProviderState enables writes without relying solely on process env.
	AllowProviderState bool `json:"allow_provider_state,omitempty"`
}

// output contains the skill result data with synchronization statistics and file operation details.
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

// main is the skill entry point for todo/sync_to_provider with comprehensive outbound sync capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates outbound todo synchronization with permission checks, task projection, and provider file writing.
//
// Index:
//
//	Purpose: Sync tasks from foxctl to external providers (Claude Code) with configurable formatting and ordering
//	Keywords: todo/sync_to_provider, outbound_sync, provider_integration, task_projection, claude_code_sync
//	Related: todosync.NewService, todos.NewStore, sessionkit.OpenTasks
//	Flow: check permissions → open task store → resolve session → build projection config → sync tasks → write provider file → emit results
//	Resources: task store, provider state files
//	Events: none
//	OutputFields: written, updated, unchanged, file_path, file_hash, todos, warnings, dry_run
//
// [[protocol:outbound_todo_sync]]
// [[risk:provider_state_write]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Check write permission
	allowProviderState := in.AllowProviderState || os.Getenv("FOXCTL_ALLOW_PROVIDER_STATE") == "1"

	// Open task store
	taskStore, err := rc.Stores.Tasks(ctx)
	if err != nil {
		return err
	}

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
		order = "foxctl_rank"
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
		out.Warnings = append(out.Warnings, "Write skipped: FOXCTL_ALLOW_PROVIDER_STATE not set")
	}

	return skillout.Emit(rc, command, out)
}
