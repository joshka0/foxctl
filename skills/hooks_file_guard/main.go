// Package main implements the hooks/file_guard skill.
// This skill manages file reservations to prevent edit conflicts between agents,
// per mailbox_blackboard.md spec.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/hookutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/context/sessionkit"
	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/runtime/hooks"
	"github.com/joshka0/foxctl/internal/runtime/hooks/pathutil"
	"github.com/joshka0/foxctl/internal/runtime/hooks/toolutil"
	"github.com/joshka0/foxctl/internal/storage/blackboard"
	"github.com/joshka0/foxctl/internal/storage/tasks"
)

// Mode controls file_guard behavior.
type Mode string

const (
	// ModeAdvisory warns about conflicts but allows the operation.
	ModeAdvisory Mode = "advisory"
	// ModeStrict blocks operations when conflicts exist.
	ModeStrict Mode = "strict"
)

const (
	// DefaultReservationTTL is the default TTL for auto-created reservations.
	DefaultReservationTTL = 10 * time.Minute
)

// main is the skill entry point for hooks/file_guard.
func main() {
	skillmain.Main("hooks/file_guard", run)
}

// run orchestrates file reservation management to prevent edit conflicts between agents.
//
// Index:
//
//	Purpose: Manage file reservations to prevent edit conflicts between agents with advisory/strict modes
//	Keywords: hooks/file_guard, file_reservations, conflict_prevention, agent_coordination
//	Related: getTaskContext, formatConflicts, emitOutput
//	Flow: detect write operations → resolve workspace/actor → extract file path → check conflicts → create reservation → emit decision
//	Resources: blackboard store (SQLite); task store
//	Events: file-reserved, file-conflict-detected
//	OutputFields: decision, reservation_id, conflicts, workspace_id, actor_id
//
// [[invariant:exclusive-file-reservation]]
// [[risk:concurrent-edit-conflict]]
func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	paths := sessionkit.ResolvePaths(rc.Config)

	// Skip non-write operations using cross-platform detection
	// Supports CC tools (Edit, Write, etc.), canonical tools (edit.*, fs.write_*), and explicit tool_kind
	if !toolutil.IsWriteOperation(in.ToolName, in.ToolCanonical, string(in.ToolKind)) {
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionApprove,
			Reason:   "non-write operation",
		})
	}

	// Determine mode from environment
	mode := ModeAdvisory
	if m := os.Getenv("FOXCTL_FILE_GUARD_MODE"); m == "strict" {
		mode = ModeStrict
	}

	workspaceRoot := hookutil.ResolveWorkspaceRoot(in, "")
	workspaceID := hookutil.ResolveWorkspaceID(in, workspaceRoot)
	actorID := hookutil.ResolveActorID(in)

	// Extract file path from tool input using cross-platform path extraction
	// Checks file_path, path, file, current_path fields
	filePath := pathutil.ExtractPath(in.ToolInput)
	if filePath == "" {
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionApprove,
			Reason:   "no file path detected",
		})
	}

	// Make path relative to workspace using pathutil
	relPath := pathutil.RelativePath(filePath, workspaceRoot)

	// Open board store
	boardStore, err := blackboard.OpenBoardStore(ctx, paths.StorageRoot)
	if err != nil {
		// If we can't open the store, allow the operation with a warning
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionApprove,
			Reason:   "file_guard: could not open board store, allowing operation",
			Context:  "**Warning:** File reservation system unavailable. Proceeding without conflict checking.",
		})
	}
	defer boardStore.Close()

	// Check for conflicts
	conflicts, err := boardStore.CheckConflicts(ctx, workspaceID, []string{relPath}, actorID, agent.ReservationModeExclusive)
	if err != nil {
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionApprove,
			Reason:   "file_guard: conflict check failed, allowing operation",
		})
	}

	if len(conflicts) > 0 {
		// There are conflicts
		conflictMsg := formatConflicts(conflicts)

		if mode == ModeStrict {
			return emitOutput(rc, hooks.Output{
				Decision: hooks.DecisionBlock,
				Reason:   fmt.Sprintf("file conflict: %s is reserved by %s", relPath, conflicts[0].Holder),
				Context:  conflictMsg,
				Meta: map[string]any{
					"conflicts":    conflicts,
					"workspace_id": workspaceID,
					"actor_id":     actorID,
					"file_path":    relPath,
				},
			})
		}

		// Advisory mode: warn but allow
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionApprove,
			Reason:   fmt.Sprintf("file conflict warning: %s may be in use by another agent", relPath),
			Context:  conflictMsg,
			Meta: map[string]any{
				"conflicts":    conflicts,
				"workspace_id": workspaceID,
				"actor_id":     actorID,
				"file_path":    relPath,
				"mode":         "advisory",
			},
		})
	}

	var taskStore tasks.Store
	if store, err := rc.Stores.Tasks(ctx); err == nil {
		taskStore = store
	}

	// Get active task context for the reservation reason
	taskID, reason := getTaskContext(ctx, taskStore, workspaceID, in.ToolName, relPath)

	// No conflicts - create a reservation for this actor
	reservation := agent.FileReservation{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		Path:        relPath,
		Holder:      actorID,
		Mode:        agent.ReservationModeExclusive,
		Reason:      reason,
		ExpiresAt:   time.Now().Add(DefaultReservationTTL),
	}

	if err := boardStore.Reserve(ctx, &reservation); err != nil {
		// Failed to reserve, but no conflicts - allow the operation
		return emitOutput(rc, hooks.Output{
			Decision: hooks.DecisionApprove,
			Reason:   fmt.Sprintf("file_guard: reservation failed but no conflicts for %s", relPath),
			Meta: map[string]any{
				"workspace_id": workspaceID,
				"actor_id":     actorID,
				"file_path":    relPath,
			},
		})
	}

	return emitOutput(rc, hooks.Output{
		Decision: hooks.DecisionApprove,
		Reason:   fmt.Sprintf("reserved %s for %s", relPath, actorID),
		Meta: map[string]any{
			"reservation_id": reservation.ID,
			"workspace_id":   workspaceID,
			"actor_id":       actorID,
			"task_id":        taskID,
			"reason":         reason,
			"file_path":      relPath,
			"expires_at":     reservation.ExpiresAt.Format(time.RFC3339),
		},
	})
}

// getTaskContext retrieves active task info to provide context for the reservation.
func getTaskContext(ctx context.Context, taskStore tasks.Store, workspaceID, toolName, filePath string) (taskID, reason string) {
	if taskStore == nil {
		// Fallback to tool-based reason
		return "", fmt.Sprintf("%s on %s", toolName, filepath.Base(filePath))
	}

	task, found, err := taskStore.GetActive(ctx, workspaceID)
	if err != nil || !found {
		return "", fmt.Sprintf("%s on %s", toolName, filepath.Base(filePath))
	}

	// Build reason from task title and description
	reason = task.Title
	if task.Description != "" {
		// Truncate long descriptions
		desc := task.Description
		if len(desc) > 100 {
			desc = desc[:97] + "..."
		}
		reason = fmt.Sprintf("%s: %s", task.Title, desc)
	}

	return task.ID, reason
}

// formatConflicts creates a warning message for conflicts with context about the other agent's work.
func formatConflicts(conflicts []agent.ReservationConflict) string {
	var sb strings.Builder
	sb.WriteString("## File Reservation Conflict\n\n")
	sb.WriteString("**Warning:** The following files are currently reserved by other agents:\n\n")

	for _, c := range conflicts {
		sb.WriteString(fmt.Sprintf("### `%s`\n", c.Path))
		sb.WriteString(fmt.Sprintf("- **Held by:** %s (%s mode)\n", c.Holder, c.Mode))
		if c.Reason != "" {
			sb.WriteString(fmt.Sprintf("- **Purpose:** %s\n", c.Reason))
		}
		if c.TaskID != "" {
			sb.WriteString(fmt.Sprintf("- **Task ID:** %s\n", c.TaskID))
		}
		sb.WriteString(fmt.Sprintf("- **Expires:** %s\n\n", c.ExpiresAt.Format(time.RFC3339)))
	}

	sb.WriteString("**Recommendation:** Review the other agent's purpose above. If your changes are compatible, coordinate with them. Otherwise, wait for their reservation to expire or ask them to release it.\n")
	return sb.String()
}

// emitOutput writes the hook output to the skill output.
func emitOutput(rc *skillmain.RunContext, output hooks.Output) error {
	return hookutil.EmitOutput(rc, "hooks/file_guard", output, nil)
}
