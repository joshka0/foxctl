// Package main implements the hooks/subagent_stop skill.
// This skill handles SubagentStop events to:
//  1. Release any file reservations owned by the subagent
//  2. Emit observability wide events for the subagent lifecycle
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/hooks"
)

const command = "hooks/subagent_stop"

// SubagentStopPayload represents the SubagentStop event payload.
// SubagentStopPayload represents the SubagentStop event payload.
type SubagentStopPayload struct {
	SubagentName string `json:"subagent_name"`
	SubagentType string `json:"subagent_type"`
	AgentID      string `json:"agent_id"`
	ExitCode     int    `json:"exit_code,omitempty"`
	Error        string `json:"error,omitempty"`
}

// main is the skill entry point for hooks/subagent_stop.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates subagent cleanup with reservation release and lifecycle logging.
//
// Index:
// - Purpose: Handle SubagentStop events to release reservations and emit lifecycle observability events
// - Flow: parse payload → extract subagent info → release reservations → log completion → emit approve output
// - SideEffects: reservation cleanup; lifecycle logging; observability events
// - FailureModes: payload parsing errors, reservation release failures (non-fatal)
// - Observability: emits subagent name, exit code, and reservation release counts
// - Related: (future) releaseReservations function when reservation system is integrated
// - Keywords: hooks/subagent_stop, subagent_lifecycle, reservation_cleanup, lifecycle_logging
func run(ctx context.Context, rc *skillmain.RunContext, in hooks.Input) error {
	// Parse subagent payload from tool input
	var payload SubagentStopPayload
	if len(in.ToolInput) > 0 {
		_ = json.Unmarshal(in.ToolInput, &payload)
	}

	// Get subagent name from payload or hook config
	subagentName := payload.SubagentName
	if subagentName == "" {
		subagentName = payload.SubagentType
	}
	if subagentName == "" && in.HookConfig != nil {
		if name, ok := in.HookConfig["subagent_name"].(string); ok {
			subagentName = name
		}
	}

	agentID := payload.AgentID
	if agentID == "" {
		agentID = in.ActorID
	}

	// Release file reservations for this agent
	// TODO: Integrate with file reservation system when available
	// releasedCount = releaseReservations(ctx, agentID)
	releasedCount := 0
	_ = agentID // Reserved for future use

	// Log completion
	rc.Logger.Info().
		Str("subagent", subagentName).
		Str("agent_id", agentID).
		Int("exit_code", payload.ExitCode).
		Int("reservations_released", releasedCount).
		Msg("subagent stopped")

	// Build output - always approve (no blocking on stop)
	output := hooks.NewApprove("subagent stopped", map[string]any{
		"subagent_name":         subagentName,
		"agent_id":              agentID,
		"exit_code":             payload.ExitCode,
		"reservations_released": releasedCount,
	})

	data := map[string]any{
		"hook_output":           output,
		"subagent_name":         subagentName,
		"agent_id":              agentID,
		"reservations_released": releasedCount,
		"summary":               fmt.Sprintf("Subagent %q stopped (exit code: %d)", subagentName, payload.ExitCode),
	}

	return skillout.Emit(rc, command, data)
}
