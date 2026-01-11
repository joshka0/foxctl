// Package main implements the optimize/bootstrap skill.
// This skill generates few-shot examples from successful trajectories.
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

const command = "optimize/bootstrap"

type input struct {
	Workspace      string  `json:"workspace"`
	Role           string  `json:"role"`
	MaxExamples    int     `json:"max_examples"`
	MinSuccessRate float64 `json:"min_success_rate"`
	Format         string  `json:"format"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	// Validate required fields
	if in.Role == "" {
		return fmt.Errorf("role is required")
	}

	// Resolve workspace
	workspace := in.Workspace
	if workspace == "" {
		workspace = "."
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	// Open stores
	trajStore, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return fmt.Errorf("open trajectory store: %w", err)
	}
	defer trajStore.Close()

	patternStore, err := optimization.OpenPatternStore(ctx, rc.Config.Storage.Root)
	if err != nil {
		return fmt.Errorf("open pattern store: %w", err)
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
		return fmt.Errorf("get stats: %w", err)
	}

	// Generate examples
	examples, err := optimizer.GenerateExamples(ctx, absWorkspace, in.Role)
	if err != nil {
		return fmt.Errorf("generate examples: %w", err)
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
