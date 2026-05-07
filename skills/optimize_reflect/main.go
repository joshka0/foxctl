// Package main implements the optimize/reflect skill.
// This skill generates reflections and insights from trajectory data.
package main

import (
	"context"
	"path/filepath"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

const command = "optimize/reflect"

// input defines the skill input parameters for optimization reflection with workspace and trajectory targeting.
type input struct {
	Workspace    string `json:"workspace"`
	Role         string `json:"role"`
	TrajectoryID string `json:"trajectory_id"`
}

// main is the skill entry point for optimize/reflect with trajectory analysis capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates optimization reflection with trajectory analysis and improvement generation.
//
// Index:
//
//	Purpose: Generate reflections and insights from trajectory data with pattern analysis and improvement suggestions
//	Keywords: optimize/reflect, trajectory_analysis, pattern_recognition, improvement_generation, optimization
//	Related: optimization.ReflectionEngine, trajectory store, pattern store
//	Flow: validate input → resolve workspace → open stores → create engine → analyze trajectory or generate summary → emit results
//	Resources: trajectory store, pattern store
//	Events: reflection generation events
//	OutputFields: reflection, summary, improvements, workspace, role
//
// [[domain:trajectory-reflection]]
// [[invariant:role-required-for-reflection]]
func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate required fields
	if in.Role == "" {
		return skillerr.Arg("role is required", skillerr.WithHint("Provide the agent role to analyze."))
	}

	// Resolve workspace
	workspace := in.Workspace
	if workspace == "" {
		workspace = "."
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return skillerr.WrapIO("resolve workspace", err)
	}

	// Open stores
	trajStore, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return skillerr.WrapIO("open trajectory store", err)
	}
	defer trajStore.Close()

	patternStore, err := optimization.OpenPatternStore(ctx, rc.Config.Storage.Root)
	if err != nil {
		return skillerr.WrapIO("open pattern store", err)
	}
	defer patternStore.Close()

	// Create reflection engine
	engine := optimization.NewReflectionEngine(trajStore, patternStore, optimization.DefaultReflectionConfig())

	if in.TrajectoryID != "" {
		// Reflect on specific trajectory
		reflection, err := engine.ReflectOnTrajectory(ctx, absWorkspace, in.TrajectoryID)
		if err != nil {
			return skillerr.WrapRuntime("reflect on trajectory", err)
		}

		return skillout.Emit(rc, command, map[string]any{
			"reflection": map[string]any{
				"trajectory_id": reflection.TrajectoryID,
				"strengths":     reflection.Strengths,
				"weaknesses":    reflection.Weaknesses,
				"suggestions":   reflection.Suggestions,
			},
		})
	}

	// Generate summary across trajectories
	summary, err := engine.GenerateSummary(ctx, absWorkspace, in.Role)
	if err != nil {
		return skillerr.WrapRuntime("generate summary", err)
	}

	// Generate improvements
	improvements, err := engine.GenerateImprovements(ctx, absWorkspace, in.Role)
	if err != nil {
		return skillerr.WrapRuntime("generate improvements", err)
	}

	improvementList := make([]map[string]any, len(improvements))
	for i, imp := range improvements {
		improvementList[i] = map[string]any{
			"id":          imp.ID,
			"category":    imp.Category,
			"description": imp.Description,
			"priority":    imp.Priority,
			"evidence":    imp.Evidence,
		}
	}

	// Format common patterns for output
	strengthList := make([]map[string]any, len(summary.CommonStrengths))
	for i, s := range summary.CommonStrengths {
		strengthList[i] = map[string]any{
			"pattern":   s.Pattern,
			"frequency": s.Frequency,
		}
	}

	weaknessList := make([]map[string]any, len(summary.CommonWeaknesses))
	for i, w := range summary.CommonWeaknesses {
		weaknessList[i] = map[string]any{
			"pattern":   w.Pattern,
			"frequency": w.Frequency,
		}
	}

	return skillout.Emit(rc, command, map[string]any{
		"summary": map[string]any{
			"total_trajectories":      summary.TotalTrajectories,
			"successful_trajectories": summary.SuccessfulTrajectories,
			"common_strengths":        strengthList,
			"common_weaknesses":       weaknessList,
		},
		"improvements": improvementList,
		"workspace":    absWorkspace,
		"role":         in.Role,
	})
}
