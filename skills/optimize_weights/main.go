// Package main implements the optimize/weights skill.
// This skill manages learnable scorer weights for task prioritization.
package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

const command = "optimize/weights"

type input struct {
	Action    string `json:"action"`
	Workspace string `json:"workspace"`
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
