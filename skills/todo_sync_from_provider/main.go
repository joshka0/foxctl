// Package main implements the todo/sync_from_provider skill.
//
// This skill syncs todos FROM a provider (e.g., Claude Code) INTO foxctl's
// task management system. It is the inbound sync direction.
package main

import (
	"context"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/context/todosync"
	"github.com/joshka0/foxctl/internal/providers/claude/todos"
)

const command = "todo/sync_from_provider"

// input defines the parameters for syncing todos from a provider.
type input struct {
	// Provider is the source provider ("claude" is currently the only supported value)
	Provider string `json:"provider" validate:"required,oneof=claude"`

	// WorkspaceID is the workspace path
	WorkspaceID string `json:"workspace_id" validate:"required"`

	// SessionID is the session identifier (optional - will auto-detect if empty)
	SessionID string `json:"session_id"`

	// Todos is the list of todos from the provider (optional - will read from file if empty)
	Todos []todosync.ClaudeTodo `json:"todos"`

	// DryRun shows what would happen without making changes
	DryRun bool `json:"dry_run"`
}

// output defines the results of the sync operation.
type output struct {
	Created   int      `json:"created"`
	Updated   int      `json:"updated"`
	Completed int      `json:"completed"`
	Removed   int      `json:"removed"`
	Mapped    int      `json:"mapped"`
	Unmapped  int      `json:"unmapped"`
	DepsAdded int      `json:"deps_added"`
	Warnings  []string `json:"warnings,omitempty"`
	DryRun    bool     `json:"dry_run,omitempty"`
}

// main is the skill entry point for todo/sync_from_provider.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates inbound todo synchronization from external providers into foxctl's task system.
//
// Index:
//
//	Purpose: Sync todos FROM a provider (e.g., Claude Code) INTO foxctl's task management system
//	Keywords: todo/sync_from_provider, todo_sync, inbound_sync, claude_code, task_management
//	Related: todosync.Service, todosync.InboundSyncInput
//	Flow: open task store → resolve session ID → get todos from input or file → run inbound sync → emit results
//	Resources: task store, provider todo file
//	Events: none
//	OutputFields: created, updated, completed, removed, mapped, unmapped, deps_added, warnings, dry_run
//
// [[protocol:inbound_todo_sync]]
// [[domain:provider_integration]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Open task store
	taskStore, err := rc.Stores.Tasks(ctx)
	if err != nil {
		return skillerr.WrapIO("open task store", err)
	}

	// Resolve session ID
	sessionID := in.SessionID
	if sessionID == "" {
		sessionID = rc.SessionID
	}

	// Get todos from input or read from file
	inputTodos := in.Todos
	if len(inputTodos) == 0 && in.Provider == "claude" {
		// Read from Claude Code todo file
		store := todos.NewStore("")
		fileTodos, err := store.Read(sessionID)
		if err != nil {
			// No file found is ok - empty list
			rc.Logger.Debug().Err(err).Msg("no claude todos file found")
			inputTodos = []todosync.ClaudeTodo{}
		} else {
			inputTodos = fileTodos
		}
	}

	// Create sync service and run inbound sync
	syncService := todosync.NewService(taskStore)
	result, err := syncService.SyncFromProvider(ctx, todosync.InboundSyncInput{
		WorkspaceID: in.WorkspaceID,
		SessionID:   sessionID,
		Todos:       inputTodos,
		DryRun:      in.DryRun,
	})
	if err != nil {
		return err
	}

	// Build output
	out := output{
		Created:   result.Created,
		Updated:   result.Updated,
		Completed: result.Completed,
		Removed:   result.Removed,
		Mapped:    result.Mapped,
		Unmapped:  result.Unmapped,
		DepsAdded: result.DepsAdded,
		Warnings:  result.Warnings,
		DryRun:    in.DryRun,
	}

	return skillout.Emit(rc, command, out)
}
