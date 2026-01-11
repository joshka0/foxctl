// Package main implements the optimize/patterns skill.
// This skill manages learned tool usage patterns for agent optimization.
package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

const command = "optimize/patterns"

type input struct {
	Action    string `json:"action"`
	Workspace string `json:"workspace"`
	Role      string `json:"role"`
	Context   string `json:"context"`
	Limit     int    `json:"limit"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Resolve workspace
	workspace := in.Workspace
	if workspace == "" {
		workspace = "."
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	// Open pattern store
	patternStore, err := optimization.OpenPatternStore(ctx, rc.Config.Storage.Root)
	if err != nil {
		return fmt.Errorf("open pattern store: %w", err)
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
		return fmt.Errorf("unknown action: %s (use: list, clear, hints)", in.Action)
	}
}

func listPatterns(ctx context.Context, rc *skillmain.RunContext, store optimization.PatternStore, in input) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}

	patterns, err := store.List(ctx, in.Role, limit)
	if err != nil {
		return fmt.Errorf("list patterns: %w", err)
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

func clearPatterns(ctx context.Context, rc *skillmain.RunContext, store optimization.PatternStore, in input) error {
	if err := store.Clear(ctx, in.Role); err != nil {
		return fmt.Errorf("clear patterns: %w", err)
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

func getHints(ctx context.Context, rc *skillmain.RunContext, patternStore optimization.PatternStore, workspace string, in input) error {
	if in.Role == "" {
		return fmt.Errorf("role is required for hints")
	}
	if in.Context == "" {
		return fmt.Errorf("context is required for hints")
	}

	// Open trajectory store for collector
	trajStore, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return fmt.Errorf("open trajectory store: %w", err)
	}
	defer trajStore.Close()

	collector := optimization.NewMCPPatternCollector(patternStore, trajStore)
	hints, err := collector.GetHints(ctx, in.Role, in.Context)
	if err != nil {
		return fmt.Errorf("get hints: %w", err)
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
