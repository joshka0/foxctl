package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/protocol"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/console"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
)

var consoleCmd = &cobra.Command{
	Use:   "console",
	Short: "Manage actor consoles",
	Long: `Manage interactive console sessions for actors.

Console sessions provide a TUI-based interface for interacting with actors
through the mailbox transport layer. All I/O goes through existing Poll/Ack/Nack
semantics.

Commands:
  attach   Attach to an actor's interactive console
  list     List attachable console sessions
  rm       Remove a console session`,
}

var consoleAttachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach to an actor console",
	Long: `Attach to an actor's interactive console, creating one if needed.

This launches the agentctl viewer in console mode, connected to the specified
actor through its mailbox. Messages are exchanged using correlation IDs to
match asks with replies.

Examples:
  # Attach to existing or new console for actor
  agentctl console attach --actor my-coder

  # Attach to specific console session
  agentctl console attach --actor my-coder --console 01JFXYZ...

  # Create console linked to AI session
  agentctl console attach --actor my-coder --session 01JFABC...`,
	RunE: runConsoleAttach,
}

var consoleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List attachable consoles",
	Long: `List all console sessions that can be attached to.

Shows console ID, actor ID, session ID, workspace, and last attached time.

Examples:
  # List all consoles
  agentctl console list

  # List consoles in specific workspace
  agentctl console list --workspace /path/to/project`,
	RunE: runConsoleList,
}

var consoleRemoveCmd = &cobra.Command{
	Use:     "rm <console-id>",
	Aliases: []string{"remove", "delete"},
	Short:   "Remove a console session",
	Long: `Remove a console session from the registry.

This does not affect the actor or any existing messages in the mailbox.

Examples:
  agentctl console rm 01JFXYZ...`,
	Args: cobra.ExactArgs(1),
	RunE: runConsoleRemove,
}

// Flags for console commands
var (
	consoleActorID   string
	consoleID        string
	consoleSessionID string
	consoleWorkspace string
	consoleLimit     int
	consoleDryRun    bool
)

func init() {
	rootCmd.AddCommand(consoleCmd)

	consoleCmd.AddCommand(consoleAttachCmd)
	consoleCmd.AddCommand(consoleListCmd)
	consoleCmd.AddCommand(consoleRemoveCmd)

	// Attach flags
	consoleAttachCmd.Flags().StringVar(&consoleActorID, "actor", "", "Actor namespace (required)")
	consoleAttachCmd.Flags().StringVar(&consoleID, "console", "", "Console ID (creates new if not specified)")
	consoleAttachCmd.Flags().StringVar(&consoleSessionID, "session", "", "Link to AI session ID")
	consoleAttachCmd.Flags().StringVar(&consoleWorkspace, "workspace", "", "Workspace path (defaults to cwd)")
	consoleAttachCmd.Flags().BoolVar(&consoleDryRun, "dry-run", false, "Preview without creating/modifying session")
	_ = consoleAttachCmd.MarkFlagRequired("actor")

	// List flags
	consoleListCmd.Flags().StringVar(&consoleWorkspace, "workspace", "", "Filter by workspace")
	consoleListCmd.Flags().IntVar(&consoleLimit, "limit", 50, "Maximum sessions to list")

	// Remove flags
	consoleRemoveCmd.Flags().BoolVar(&consoleDryRun, "dry-run", false, "Preview without deleting session")
}

// runConsoleAttach attaches to an existing actor console or creates a new console session and emits an envelope describing the result.
// It resolves the workspace, opens the console store, optionally performs a dry-run, updates or creates the session, optionally links a session ID, and writes an OK or error envelope to stdout. It returns an error if any store operation, workspace resolution, or envelope write fails.
func runConsoleAttach(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	// Determine workspace
	workspace := consoleWorkspace
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return writeConsoleErrorEnvelope(cmd, "console/attach", string(protocol.ErrorCodeERuntime),
				fmt.Sprintf("failed to get working directory: %v", err))
		}
		workspace = cwd
	}

	// Open console store
	store, closeFn, err := openConsoleStore(ctx, cfg.Storage.Root)
	if err != nil {
		return writeConsoleErrorEnvelope(cmd, "console/attach", string(protocol.ErrorCodeERuntime),
			fmt.Sprintf("failed to open console store: %v", err))
	}
	defer func() { _ = closeFn() }()
	defer store.Close()

	// Determine action and session info for dry-run or actual execution
	var session console.ConsoleSession
	var action string
	if consoleID != "" {
		// Attach to existing console
		session, err = store.Get(ctx, consoleID)
		if err != nil {
			return writeConsoleErrorEnvelope(cmd, "console/attach", string(protocol.ErrorCodeENotFound),
				fmt.Sprintf("console not found: %s", consoleID))
		}
		// Verify actor matches
		if session.ActorID != consoleActorID {
			return writeConsoleErrorEnvelope(cmd, "console/attach", string(protocol.ErrorCodeEARG),
				fmt.Sprintf("console %s belongs to actor %s, not %s",
					consoleID, session.ActorID, consoleActorID))
		}
		action = "attach_existing"
	} else {
		// Check for existing console for this actor
		existing, err := store.GetByActor(ctx, consoleActorID)
		if err != nil {
			return writeConsoleErrorEnvelope(cmd, "console/attach", string(protocol.ErrorCodeERuntime),
				fmt.Sprintf("failed to query consoles: %v", err))
		}
		if len(existing) > 0 {
			// Would reuse most recently attached
			session = existing[0]
			consoleID = session.ConsoleID
			action = "reuse_existing"
		} else {
			// Would create new console session
			consoleID = ulid.Make().String()
			session = console.ConsoleSession{
				ConsoleID:      consoleID,
				ActorID:        consoleActorID,
				SessionID:      consoleSessionID,
				Workspace:      workspace,
				CreatedAt:      time.Now().UTC(),
				LastAttachedAt: time.Now().UTC(),
			}
			action = "create_new"
		}
	}

	// Dry-run: show what would be done
	if consoleDryRun {
		data := map[string]any{
			"dry_run":    true,
			"action":     action,
			"console_id": consoleID,
			"actor_id":   consoleActorID,
			"session_id": session.SessionID,
			"workspace":  session.Workspace,
			"message":    fmt.Sprintf("Dry run: would %s console", action),
		}
		env := envelope.OK("console/attach", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
			m.Source = "run"
		}))
		if err := envelope.Write(os.Stdout, env); err != nil {
			return fmt.Errorf("write envelope: %w", err)
		}
		return nil
	}

	// Execute the action
	switch action {
	case "attach_existing", "reuse_existing":
		if err := store.UpdateAttached(ctx, consoleID); err != nil {
			return writeConsoleErrorEnvelope(cmd, "console/attach", string(protocol.ErrorCodeERuntime),
				fmt.Sprintf("failed to update console: %v", err))
		}
	case "create_new":
		if err := store.Create(ctx, session); err != nil {
			return writeConsoleErrorEnvelope(cmd, "console/attach", string(protocol.ErrorCodeERuntime),
				fmt.Sprintf("failed to create console: %v", err))
		}
	}

	// Link session if provided
	if consoleSessionID != "" && session.SessionID != consoleSessionID {
		if err := store.LinkSession(ctx, consoleID, consoleSessionID); err != nil {
			return writeConsoleErrorEnvelope(cmd, "console/attach", string(protocol.ErrorCodeERuntime),
				fmt.Sprintf("failed to link session: %v", err))
		}
	}

	// Output session info for viewer launch
	data := map[string]any{
		"console_id": consoleID,
		"actor_id":   consoleActorID,
		"session_id": session.SessionID,
		"workspace":  session.Workspace,
		"created_at": session.CreatedAt.Format(time.RFC3339),
		"message":    fmt.Sprintf("Console ready. Launch: agentctl viewer --actor-console %s --console %s", consoleActorID, consoleID),
	}

	env := envelope.OK("console/attach", data, envelope.WithMetaMutator(func(m *envelope.Meta) {
		m.Source = "run"
	}))

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

// runConsoleList lists attachable console sessions and writes an OK envelope
// containing session details (console_id, actor_id, session_id, workspace,
// created_at, last_attached_at) to stdout.
// If opening the store, listing sessions, or writing the envelope fails, it
// emits an error envelope and returns a non-nil error.
func runConsoleList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	store, closeFn, err := openConsoleStore(ctx, cfg.Storage.Root)
	if err != nil {
		return writeConsoleErrorEnvelope(cmd, "console/list", string(protocol.ErrorCodeERuntime),
			fmt.Sprintf("failed to open console store: %v", err))
	}
	defer func() { _ = closeFn() }()

	sessions, err := store.List(ctx, consoleWorkspace, consoleLimit)
	if err != nil {
		return writeConsoleErrorEnvelope(cmd, "console/list", string(protocol.ErrorCodeERuntime),
			fmt.Sprintf("failed to list consoles: %v", err))
	}

	// Format sessions for output
	items := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		items = append(items, map[string]any{
			"console_id":       s.ConsoleID,
			"actor_id":         s.ActorID,
			"session_id":       s.SessionID,
			"workspace":        s.Workspace,
			"created_at":       s.CreatedAt.Format(time.RFC3339),
			"last_attached_at": s.LastAttachedAt.Format(time.RFC3339),
		})
	}

	env := envelope.OK("console/list", map[string]any{
		"count":    len(items),
		"sessions": items,
	})

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

// delete, or write the envelope are returned as errors.
func runConsoleRemove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	cfg := config.MustFromContext(ctx)

	targetConsoleID := args[0]

	store, closeFn, err := openConsoleStore(ctx, cfg.Storage.Root)
	if err != nil {
		return writeConsoleErrorEnvelope(cmd, "console/rm", string(protocol.ErrorCodeERuntime),
			fmt.Sprintf("failed to open console store: %v", err))
	}
	defer func() { _ = closeFn() }()

	// Check if console exists first (for both dry-run and actual delete)
	session, err := store.Get(ctx, targetConsoleID)
	if err != nil {
		if errors.Is(err, console.ErrNotFound) {
			return writeConsoleErrorEnvelope(cmd, "console/rm", string(protocol.ErrorCodeENotFound),
				fmt.Sprintf("console not found: %s", targetConsoleID))
		}
		return writeConsoleErrorEnvelope(cmd, "console/rm", string(protocol.ErrorCodeERuntime),
			fmt.Sprintf("failed to lookup console: %v", err))
	}

	// Dry-run: show what would be done
	if consoleDryRun {
		env := envelope.OK("console/rm", map[string]any{
			"dry_run":    true,
			"console_id": targetConsoleID,
			"actor_id":   session.ActorID,
			"workspace":  session.Workspace,
			"deleted":    false,
			"message":    "Dry run: console would be deleted",
		})
		if err := envelope.Write(os.Stdout, env); err != nil {
			return fmt.Errorf("write envelope: %w", err)
		}
		return nil
	}

	if err := store.Delete(ctx, targetConsoleID); err != nil {
		return writeConsoleErrorEnvelope(cmd, "console/rm", string(protocol.ErrorCodeERuntime),
			fmt.Sprintf("failed to delete console: %v", err))
	}

	env := envelope.OK("console/rm", map[string]any{
		"console_id": targetConsoleID,
		"message":    "Console session removed",
	})

	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write envelope: %w", err)
	}

	return nil
}

// openConsoleStore opens the console store from the AGENTS store database (agents.db by default).
// openConsoleStore opens the console store DB at storageRoot/agents.db (or the configured driver/path)
// and returns a console store plus a close function that releases the underlying database handle.
//
// The returned close function must be called by the caller to release resources. If opening the database
// or initializing the store fails, an error is returned. If store initialization fails after the DB is
// opened, the DB handle is closed before the error is returned.
func openConsoleStore(ctx context.Context, storageRoot string) (*console.SQLiteStore, func() error, error) {
	db, closeFn, err := dbutil.OpenStoreDB(ctx, storageRoot, string(storage.StoreAgents), "agents.db", nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open agents store: %w", err)
	}

	store, err := console.NewStore(ctx, db)
	if err != nil {
		_ = closeFn()
		return nil, nil, err
	}

	return store, closeFn, nil
}

// writeConsoleErrorEnvelope writes an error envelope and returns an error.
func writeConsoleErrorEnvelope(_ *cobra.Command, command, code, message string) error {
	env := envelope.Error(command, code, message, nil)
	if err := envelope.Write(os.Stdout, env); err != nil {
		return fmt.Errorf("write error envelope: %w", err)
	}
	return fmt.Errorf("%s", message)
}
