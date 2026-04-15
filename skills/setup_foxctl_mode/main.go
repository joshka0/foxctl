package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	_ "modernc.org/sqlite"
)

const command = "setup/foxctl_mode"

// Input defines the skill input parameters for foxctl mode management with get/set operations.
type Input struct {
	Operation   string `json:"operation"`    // "get" or "set"
	WorkspaceID string `json:"workspace_id"` // optional, defaults to cwd
	Enabled     *bool  `json:"enabled"`      // for "set" operation
}

// Output defines the skill output with workspace mode status and identification information.
type Output struct {
	Enabled     bool   `json:"enabled"`
	WorkspaceID string `json:"workspace_id"`
}

// ModeValue represents the stored foxctl mode configuration with enabled state.
type ModeValue struct {
	Enabled bool `json:"enabled"`
}

// main is the skill entry point for setup/foxctl_mode.
func main() {
	skillmain.Main(command, skillmain.Chain(run,
		skillmain.WithTimeout[Input](5*time.Second),
		skillmain.WithRecover[Input](),
	))
}

// run orchestrates foxctl mode management with get/set operations and workspace resolution.
//
// Index:
// - Purpose: Manage foxctl mode settings per workspace with get/set operations
// - Flow: validate operation → resolve workspace → open database → ensure schema → execute get/set → emit results
// - SideEffects: database schema creation; mode value storage/retrieval; workspace configuration updates
// - FailureModes: invalid operations, database access failures, JSON marshaling errors, workspace resolution failures
// - Observability: emits current mode status, workspace ID, and operation results
// - Related: ensureSchema, getMode, setMode, workspaceutil.ResolveID
// - Keywords: setup/foxctl_mode, workspace_settings, mode_management, database_storage
func run(ctx context.Context, rc *skillmain.RunContext, input Input) error {
	if input.Operation == "" {
		return skillerr.Arg("operation is required", skillerr.WithHint("Use 'get' or 'set'."))
	}

	// Default workspace using standard detection chain.
	workspace := workspaceutil.ResolveID(input.WorkspaceID, rc.Workspace)

	// Get DB path
	foxctlHome := os.Getenv("FOXCTL_HOME")
	if foxctlHome == "" {
		home, _ := os.UserHomeDir()
		foxctlHome = filepath.Join(home, ".foxctl")
	}
	dbPath := filepath.Join(foxctlHome, "storage", "tasks.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return skillerr.WrapIO("open db", err)
	}
	defer db.Close()

	if err := ensureSchema(ctx, db); err != nil {
		return err
	}

	switch input.Operation {
	case "get":
		enabled, err := getMode(ctx, db, workspace)
		if err != nil {
			return skillerr.WrapIO("get mode", err)
		}
		return skillout.Emit(rc, command, Output{Enabled: enabled, WorkspaceID: workspace})

	case "set":
		if input.Enabled == nil {
			return skillerr.Arg("'enabled' field required for 'set' operation")
		}
		if err := setMode(ctx, db, workspace, *input.Enabled); err != nil {
			return skillerr.WrapIO("set mode", err)
		}
		return skillout.Emit(rc, command, Output{Enabled: *input.Enabled, WorkspaceID: workspace})

	default:
		return skillerr.Arg(
			fmt.Sprintf("unknown operation: %s", input.Operation),
			skillerr.WithHint("Use 'get' or 'set'."),
		)
	}
}

// ensureSchema creates the workspace_settings table if it doesn't exist for persistent storage.
func ensureSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS workspace_settings (
			workspace_id TEXT NOT NULL,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (workspace_id, key)
		)
	`)
	if err != nil {
		return skillerr.WrapIO("create table", err)
	}
	return nil
}

// getMode retrieves the foxctl mode setting for a workspace with default fallback to disabled.
func getMode(ctx context.Context, db *sql.DB, workspace string) (bool, error) {
	var valueStr string
	err := db.QueryRowContext(ctx,
		`SELECT value FROM workspace_settings WHERE workspace_id = ? AND key = 'foxctl_mode'`,
		workspace,
	).Scan(&valueStr)

	if err == sql.ErrNoRows {
		return false, nil // Default to disabled
	}
	if err != nil {
		return false, err
	}

	var val ModeValue
	if err := json.Unmarshal([]byte(valueStr), &val); err != nil {
		return false, err
	}
	return val.Enabled, nil
}

// setMode stores the foxctl mode setting for a workspace with upsert behavior and timestamp tracking.
func setMode(ctx context.Context, db *sql.DB, workspace string, enabled bool) error {
	val := ModeValue{Enabled: enabled}
	valueJSON, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("marshal value: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)

	_, err = db.ExecContext(ctx, `
		INSERT OR REPLACE INTO workspace_settings (workspace_id, key, value, updated_at)
		VALUES (?, 'foxctl_mode', ?, ?)
	`, workspace, string(valueJSON), now)
	return err
}
