// Package main implements the todo/sync_from_provider skill.
//
// This skill syncs todos FROM a provider (e.g., Claude Code) INTO agentctl's
// task management system. It is the inbound sync direction.
package main

import (
	"context"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/providers/claude/todos"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/todosync"
)

const command = "todo/sync_from_provider"

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

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
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
