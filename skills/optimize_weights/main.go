// Package main implements the optimize/weights skill for managing learnable scorer weights in task prioritization.
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

const command = "optimize/weights"

// input defines the skill input parameters for weight management operations with action selection.
type input struct {
	Action    string `json:"action"`
	Workspace string `json:"workspace"`
}

// main is the skill entry point for optimize/weights with weight management capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates weight management operations with workspace resolution and scorer initialization.
//
// Index:
//
//	Purpose: Manage learnable scorer weights for task prioritization with show and learn actions
//	Keywords: optimize/weights, task_prioritization, machine_learning, weight_optimization, learnable_scorer
//	Related: showWeights, learnWeights, optimization.NewLearnableScorer, optimization.NewInMemoryWeightStore
//	Flow: resolve workspace → open trajectory store → create weight store and scorer → execute action → emit results
//	Resources: trajectory store, in-memory weight store
//	Events: weight learning events
//	OutputFields: weights, update, workspace
//
// [[domain:learnable-scorer-weights]]
// [[protocol:weight-action-dispatch]]
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

	// Open trajectory store
	trajStore, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return skillerr.WrapIO("open trajectory store", err)
	}
	defer trajStore.Close()

	// Create weight store and scorer
	weightStore := optimization.NewInMemoryWeightStore()
	scorer := optimization.NewLearnableScorer(trajStore, weightStore, optimization.DefaultLearnerConfig())

	switch in.Action {
	case "show":
		return showWeights(ctx, rc, scorer, absWorkspace)
	case "learn":
		return learnWeights(ctx, rc, scorer, absWorkspace)
	default:
		return skillerr.Arg(
			fmt.Sprintf("unknown action: %s", in.Action),
			skillerr.WithHint("Use action=show or action=learn."),
		)
	}
}

// showWeights displays current learnable scorer weights with detailed breakdown by factor type.
func showWeights(ctx context.Context, rc *skillmain.RunContext, scorer *optimization.LearnableScorer, workspace string) error {
	weights, err := scorer.GetCurrentWeights(ctx, workspace)
	if err != nil {
		return skillerr.WrapRuntime("get weights", err)
	}

	return skillout.Emit(rc, command, map[string]any{
		"weights": map[string]any{
			"critical_path": weights.CriticalPath,
			"page_rank":     weights.PageRank,
			"admin_mail":    weights.AdminMail,
			"overseer_mail": weights.OverseerMail,
			"recency":       weights.Recency,
		},
		"workspace": workspace,
	})
}

// learnWeights performs machine learning optimization on scorer weights using trajectory outcome data.
func learnWeights(ctx context.Context, rc *skillmain.RunContext, scorer *optimization.LearnableScorer, workspace string) error {
	update, err := scorer.LearnFromOutcomes(ctx, workspace)
	if err != nil {
		return skillerr.WrapRuntime("learn weights", err)
	}

	return skillout.Emit(rc, command, map[string]any{
		"update": map[string]any{
			"timestamp":   update.Timestamp,
			"sample_size": update.SampleSize,
			"reason":      update.Reason,
			"previous_weights": map[string]any{
				"critical_path": update.PreviousWeights.CriticalPath,
				"page_rank":     update.PreviousWeights.PageRank,
				"admin_mail":    update.PreviousWeights.AdminMail,
				"overseer_mail": update.PreviousWeights.OverseerMail,
				"recency":       update.PreviousWeights.Recency,
			},
			"new_weights": map[string]any{
				"critical_path": update.NewWeights.CriticalPath,
				"page_rank":     update.NewWeights.PageRank,
				"admin_mail":    update.NewWeights.AdminMail,
				"overseer_mail": update.NewWeights.OverseerMail,
				"recency":       update.NewWeights.Recency,
			},
		},
		"workspace": workspace,
	})
}
