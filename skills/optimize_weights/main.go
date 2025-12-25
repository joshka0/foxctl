// Package main implements the optimize/weights skill.
// This skill manages learnable scorer weights for task prioritization.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

const command = "optimize/weights"

type input struct {
	Action    string `json:"action"`
	Workspace string `json:"workspace"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("ERUNTIME", err, "Check AGENTCTL_HOME and config file permissions")
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("ERUNTIME", err, "Failed to initialize runner context")
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("EARG", fmt.Errorf("decode input: %w", err), "Provide valid JSON input with action field (show or learn)")
	}

	if err := run(ctx, rc, cfg, in); err != nil {
		fail("ERUNTIME", err, "Check trajectory store and workspace path")
	}
}

func run(ctx context.Context, rc *runner.RunnerContext, cfg config.Config, in input) error {
	// Resolve workspace
	workspace := in.Workspace
	if workspace == "" {
		workspace = "."
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("resolve workspace: %w", err)
	}

	// Open trajectory store
	trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open trajectory store: %w", err)
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
		return fmt.Errorf("unknown action: %s (use: show, learn)", in.Action)
	}
}

func showWeights(ctx context.Context, rc *runner.RunnerContext, scorer *optimization.LearnableScorer, workspace string) error {
	weights, err := scorer.GetCurrentWeights(ctx, workspace)
	if err != nil {
		return fmt.Errorf("get weights: %w", err)
	}

	return rc.Emit(command, map[string]any{
		"weights": map[string]any{
			"critical_path": weights.CriticalPath,
			"page_rank":     weights.PageRank,
			"admin_mail":    weights.AdminMail,
			"overseer_mail": weights.OverseerMail,
			"recency":       weights.Recency,
		},
		"workspace": workspace,
	}, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func learnWeights(ctx context.Context, rc *runner.RunnerContext, scorer *optimization.LearnableScorer, workspace string) error {
	update, err := scorer.LearnFromOutcomes(ctx, workspace)
	if err != nil {
		return fmt.Errorf("learn weights: %w", err)
	}

	return rc.Emit(command, map[string]any{
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
	}, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func fail(code string, err error, hint string) {
	data := map[string]any{"hint": hint}
	env := envelope.Error(command, code, err.Error(), data)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit failure")
	os.Exit(1)
}
