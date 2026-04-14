// Package main implements the optimize/bootstrap skill for generating few-shot examples from successful trajectories.
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

const command = "optimize/bootstrap"

// input defines the skill input parameters for trajectory bootstrapping with filtering and formatting options.
type input struct {
	Workspace      string  `json:"workspace"`
	Role           string  `json:"role"`
	MaxExamples    int     `json:"max_examples"`
	MinSuccessRate float64 `json:"min_success_rate"`
	Format         string  `json:"format"`
}

// main is the skill entry point for optimize/bootstrap with trajectory bootstrapping capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates trajectory bootstrapping with pattern analysis, example generation, and formatting.
//
// Index:
// - Purpose: Generate few-shot examples from successful trajectories for agent optimization and prompt engineering
// - Flow: validate input → resolve workspace → open stores → build config → get statistics → generate examples → format output
// - SideEffects: reads trajectory store; accesses pattern store; processes successful trajectories; generates training examples
// - FailureModes: missing role parameter, workspace resolution errors, store access failures, example generation errors
// - Observability: emits example statistics, generated examples, formatted prompts, and comprehensive bootstrapping metrics
// - Related: optimization.NewBootstrapOptimizer, optimization.DefaultBootstrapConfig
// - Keywords: optimize/bootstrap, trajectory_analysis, few_shot_learning, example_generation, agent_optimization
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

	// Build config
	bootConfig := optimization.DefaultBootstrapConfig()
	if in.MaxExamples > 0 {
		bootConfig.MaxExamples = in.MaxExamples
	}
	if in.MinSuccessRate > 0 {
		bootConfig.MinSuccessRate = in.MinSuccessRate
	}

	optimizer := optimization.NewBootstrapOptimizer(trajStore, patternStore, bootConfig)

	// Get stats first
	stats, err := optimizer.GetExampleStats(ctx, absWorkspace, in.Role)
	if err != nil {
		return skillerr.WrapRuntime("get stats", err)
	}

	// Generate examples
	examples, err := optimizer.GenerateExamples(ctx, absWorkspace, in.Role)
	if err != nil {
		return skillerr.WrapRuntime("generate examples", err)
	}

	// Convert to output format
	exampleList := make([]map[string]any, len(examples))
	for i, ex := range examples {
		exampleList[i] = map[string]any{
			"input":  ex.Input,
			"output": ex.Output,
			"tools":  ex.Tools,
		}
	}

	// Format for prompt if requested
	formatted := ""
	if in.Format == "prompt" || in.Format == "" {
		formatted = optimizer.FormatExamplesForPrompt(examples)
	}

	statsMap := map[string]any{
		"total_available":       stats.TotalAvailable,
		"avg_tools_per_example": stats.AvgToolsPerExample,
		"has_ratings":           stats.HasRatings,
	}
	if stats.AvgRating != nil {
		statsMap["avg_rating"] = *stats.AvgRating
	}

	return skillout.Emit(rc, command, map[string]any{
		"examples":  exampleList,
		"count":     len(examples),
		"formatted": formatted,
		"stats":     statsMap,
	})
}
