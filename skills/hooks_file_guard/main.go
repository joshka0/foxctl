// Package main implements the hooks/file_guard skill.
// This skill manages file reservations to prevent edit conflicts between agents,
// per mailbox_blackboard.md spec.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/domain/hook"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
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

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("hooks/file_guard", "ECONFIG", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("hooks/file_guard", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in hook.Input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("hooks/file_guard", "EARG", fmt.Errorf("decode input: %w", err))
	}

	if err := run(ctx, rc, cfg, in); err != nil {
		fail("hooks/file_guard", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, cfg config.Config, in hook.Input) error {
	// Skip non-write operations
	if !hook.IsWriteOperation(in.ToolName) {
		return emitOutput(rc, hook.Output{
			Decision: hook.DecisionApprove,
			Reason:   "non-write operation",
		})
	}

	// Determine mode from environment
	mode := ModeAdvisory
	if m := os.Getenv("AGENTCTL_FILE_GUARD_MODE"); m == "strict" {
		mode = ModeStrict
	}

	// Get workspace ID
	workspaceID := in.WorkspaceRoot
	if workspaceID == "" {
		workspaceID, _ = os.Getwd()
	}

	// Get actor ID from environment
	actorID := os.Getenv("AGENTCTL_AGENT_NAME")
	if actorID == "" {
		actorID = fmt.Sprintf("actor:agent:%s", in.SessionID)
	}

	// Extract file path from tool input
	filePath := extractFilePath(in.ToolInput)
	if filePath == "" {
		return emitOutput(rc, hook.Output{
			Decision: hook.DecisionApprove,
			Reason:   "no file path detected",
		})
	}

	// Make path relative to workspace
	relPath := filePath
	if in.WorkspaceRoot != "" && filepath.IsAbs(filePath) {
		if rel, err := filepath.Rel(in.WorkspaceRoot, filePath); err == nil && !strings.HasPrefix(rel, "..") {
			relPath = rel
		}
	}

	// Open board store
	boardStore, err := blackboard.OpenBoardStore(ctx, cfg.Storage.Root)
	if err != nil {
		// If we can't open the store, allow the operation with a warning
		return emitOutput(rc, hook.Output{
			Decision: hook.DecisionApprove,
			Reason:   "file_guard: could not open board store, allowing operation",
			Context:  "**Warning:** File reservation system unavailable. Proceeding without conflict checking.",
		})
	}
	defer boardStore.Close()

	// Check for conflicts
	conflicts, err := boardStore.CheckConflicts(ctx, workspaceID, []string{relPath}, actorID, agent.ReservationModeExclusive)
	if err != nil {
		return emitOutput(rc, hook.Output{
			Decision: hook.DecisionApprove,
			Reason:   "file_guard: conflict check failed, allowing operation",
		})
	}

	if len(conflicts) > 0 {
		// There are conflicts
		conflictMsg := formatConflicts(conflicts)

		if mode == ModeStrict {
			return emitOutput(rc, hook.Output{
				Decision: hook.DecisionBlock,
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
		return emitOutput(rc, hook.Output{
			Decision: hook.DecisionApprove,
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

	// No conflicts - create a reservation for this actor
	reservation := agent.FileReservation{
		WorkspaceID: workspaceID,
		Path:        relPath,
		Holder:      actorID,
		Mode:        agent.ReservationModeExclusive,
		ExpiresAt:   time.Now().Add(DefaultReservationTTL),
	}

	if err := boardStore.Reserve(ctx, reservation); err != nil {
		// Failed to reserve, but no conflicts - allow the operation
		return emitOutput(rc, hook.Output{
			Decision: hook.DecisionApprove,
			Reason:   fmt.Sprintf("file_guard: reservation failed but no conflicts for %s", relPath),
			Meta: map[string]any{
				"workspace_id": workspaceID,
				"actor_id":     actorID,
				"file_path":    relPath,
			},
		})
	}

	return emitOutput(rc, hook.Output{
		Decision: hook.DecisionApprove,
		Reason:   fmt.Sprintf("reserved %s for %s", relPath, actorID),
		Meta: map[string]any{
			"reservation_id": reservation.ID,
			"workspace_id":   workspaceID,
			"actor_id":       actorID,
			"file_path":      relPath,
			"expires_at":     reservation.ExpiresAt.Format(time.RFC3339),
		},
	})
}

// extractFilePath extracts the file_path from tool input JSON.
func extractFilePath(toolInput json.RawMessage) string {
	if len(toolInput) == 0 {
		return ""
	}

	var input struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return ""
	}
	return input.FilePath
}

// formatConflicts creates a warning message for conflicts.
func formatConflicts(conflicts []agent.ReservationConflict) string {
	var sb strings.Builder
	sb.WriteString("## File Reservation Conflict\n\n")
	sb.WriteString("**Warning:** The following files are currently reserved by other agents:\n\n")

	for _, c := range conflicts {
		sb.WriteString(fmt.Sprintf("- `%s` - held by **%s** (%s mode)\n", c.Path, c.Holder, c.Mode))
	}

	sb.WriteString("\n**Recommendation:** Coordinate with the other agent before proceeding, or wait for the reservation to expire.\n")
	return sb.String()
}

func emitOutput(rc *runner.RunnerContext, output hook.Output) error {
	data := map[string]any{
		"hook_output": output,
	}
	return rc.Emit("hooks/file_guard", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit hook failure")
	os.Exit(1)
}
