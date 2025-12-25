// Package main implements the optimize/reflect skill.
// This skill generates reflections and insights from trajectory data.
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

const command = "optimize/reflect"

type input struct {
	Workspace    string `json:"workspace"`
	Role         string `json:"role"`
	TrajectoryID string `json:"trajectory_id"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("ERUNTIME", err)
	}
	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("EARG", fmt.Errorf("decode input: %w", err))
	}

	if in.Role == "" {
		fail("EARG", fmt.Errorf("role is required"))
	}

	if err := run(ctx, rc, cfg, in); err != nil {
		fail("ERUNTIME", err)
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

	// Open stores
	trajStore, err := trajectory.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open trajectory store: %w", err)
	}
	defer trajStore.Close()

	patternStore, err := optimization.OpenPatternStore(ctx, cfg.Storage.Root)
	if err != nil {
		return fmt.Errorf("open pattern store: %w", err)
	}
	defer patternStore.Close()

	// Create reflection engine
	engine := optimization.NewReflectionEngine(trajStore, patternStore, optimization.DefaultReflectionConfig())

	if in.TrajectoryID != "" {
		// Reflect on specific trajectory
		reflection, err := engine.ReflectOnTrajectory(ctx, absWorkspace, in.TrajectoryID)
		if err != nil {
			return fmt.Errorf("reflect on trajectory: %w", err)
		}

		return rc.Emit(command, map[string]any{
			"reflection": map[string]any{
				"trajectory_id": reflection.TrajectoryID,
				"strengths":     reflection.Strengths,
				"weaknesses":    reflection.Weaknesses,
				"suggestions":   reflection.Suggestions,
			},
		}, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
	}

	// Generate summary across trajectories
	summary, err := engine.GenerateSummary(ctx, absWorkspace, in.Role)
	if err != nil {
		return fmt.Errorf("generate summary: %w", err)
	}

	// Generate improvements
	improvements, err := engine.GenerateImprovements(ctx, absWorkspace, in.Role)
	if err != nil {
		return fmt.Errorf("generate improvements: %w", err)
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

	return rc.Emit(command, map[string]any{
		"summary": map[string]any{
			"total_trajectories":      summary.TotalTrajectories,
			"successful_trajectories": summary.SuccessfulTrajectories,
			"common_strengths":        strengthList,
			"common_weaknesses":       weaknessList,
		},
		"improvements": improvementList,
		"workspace":    absWorkspace,
		"role":         in.Role,
	}, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit failure")
	os.Exit(1)
}
