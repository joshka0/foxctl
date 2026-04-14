// Package main implements the optimize/patterns skill for managing learned tool usage patterns in agent optimization.
package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

const command = "optimize/patterns"

// input defines the skill input parameters for pattern management operations with action selection and filtering.
type input struct {
	Action    string `json:"action"`
	Workspace string `json:"workspace"`
	Role      string `json:"role"`
	Context   string `json:"context"`
	Limit     int    `json:"limit"`
}

// main is the skill entry point for optimize/patterns with pattern management capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates pattern management operations with workspace resolution and store initialization.
//
// Index:
// - Purpose: Manage learned tool usage patterns for agent optimization with list, clear, and hints operations
// - Flow: resolve workspace → open pattern store → dispatch action → execute operation → emit results
// - SideEffects: reads and writes pattern store; manages tool usage patterns; provides optimization hints
// - FailureModes: workspace resolution errors, pattern store access failures, invalid actions, missing required parameters
// - Observability: emits pattern lists, operation confirmations, optimization hints, and comprehensive pattern statistics
// - Related: listPatterns, clearPatterns, getHints, optimization.OpenPatternStore
// - Keywords: optimize/patterns, tool_usage_patterns, agent_optimization, pattern_management, learning_system
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Resolve workspace
	workspace := in.Workspace
	if workspace == "" {
		workspace = "."
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return skillerr.WrapIO("resolve workspace", err)
	}

	// Open pattern store
	patternStore, err := optimization.OpenPatternStore(ctx, rc.Config.Storage.Root)
	if err != nil {
		return skillerr.WrapIO("open pattern store", err)
	}
	defer patternStore.Close()

	switch in.Action {
	case "list":
		return listPatterns(ctx, rc, patternStore, in)
	case "clear":
		return clearPatterns(ctx, rc, patternStore, in)
	case "hints":
		return getHints(ctx, rc, patternStore, absWorkspace, in)
	default:
		return skillerr.Arg(
			fmt.Sprintf("unknown action: %s", in.Action),
			skillerr.WithHint("Use action=list, action=clear, or action=hints."),
		)
	}
}

// listPatterns retrieves and formats stored tool usage patterns with success rate calculations and statistics.
func listPatterns(ctx context.Context, rc *skillmain.RunContext, store optimization.PatternStore, in input) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}

	patterns, err := store.List(ctx, in.Role, limit)
	if err != nil {
		return skillerr.WrapRuntime("list patterns", err)
	}

	// Convert to output format
	result := make([]map[string]any, len(patterns))
	for i, p := range patterns {
		result[i] = map[string]any{
			"id":              p.ID,
			"agent_role":      p.AgentRole,
			"context":         p.Context,
			"tool_sequence":   p.ToolSequence,
			"outcome":         p.Outcome,
			"count":           p.Count,
			"success_count":   p.SuccessCount,
			"success_rate":    p.SuccessRate(),
			"avg_duration_ms": p.AvgDurationMS,
			"last_seen":       p.LastSeen,
		}
	}

	return skillout.Emit(rc, command, map[string]any{
		"patterns": result,
		"count":    len(result),
	})
}

// clearPatterns removes stored patterns with optional role filtering and confirmation messaging.
func clearPatterns(ctx context.Context, rc *skillmain.RunContext, store optimization.PatternStore, in input) error {
	if err := store.Clear(ctx, in.Role); err != nil {
		return skillerr.WrapRuntime("clear patterns", err)
	}

	msg := "all patterns cleared"
	if in.Role != "" {
		msg = fmt.Sprintf("patterns cleared for role: %s", in.Role)
	}

	return skillout.Emit(rc, command, map[string]any{
		"message": msg,
		"role":    in.Role,
	})
}

// getHints generates optimization hints based on learned patterns and current context with confidence scoring.
func getHints(ctx context.Context, rc *skillmain.RunContext, patternStore optimization.PatternStore, workspace string, in input) error {
	if in.Role == "" {
		return skillerr.Arg("role is required for hints")
	}
	if in.Context == "" {
		return skillerr.Arg("context is required for hints")
	}

	// Open trajectory store for collector
	trajStore, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return skillerr.WrapIO("open trajectory store", err)
	}
	defer trajStore.Close()

	collector := optimization.NewMCPPatternCollector(patternStore, trajStore)
	hints, err := collector.GetHints(ctx, in.Role, in.Context)
	if err != nil {
		return skillerr.WrapRuntime("get hints", err)
	}

	// Convert to output format
	result := make([]map[string]any, len(hints))
	for i, h := range hints {
		result[i] = map[string]any{
			"tool_name":  h.ToolName,
			"confidence": h.Confidence,
			"reason":     h.Reason,
			"sequence":   h.Sequence,
		}
	}

	// Also include formatted prompt
	formatted := collector.FormatHintsForPrompt(hints)

	return skillout.Emit(rc, command, map[string]any{
		"hints":     result,
		"count":     len(result),
		"formatted": formatted,
	})
}
