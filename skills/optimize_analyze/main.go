// Package main implements the optimize/analyze skill.
// This skill analyzes trajectory data for optimization insights.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

const command = "optimize/analyze"

type input struct {
	Workspace string `json:"workspace"`
	Role      string `json:"role"`
	Days      int    `json:"days"`
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

type agentStats struct {
	TotalTrajectories int
	SuccessRate       float64
	AvgToolCalls      float64
	AvgDuration       time.Duration
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

	// Set days
	days := in.Days
	if days <= 0 {
		days = 30
	}

	// Build filter for recent trajectories
	filter := trajectory.OutcomeFilter{
		WorkspaceID: absWorkspace,
		AgentRole:   in.Role,
		Since:       time.Now().AddDate(0, 0, -days),
	}

	trajs, err := trajStore.ListByOutcome(ctx, filter)
	if err != nil {
		return fmt.Errorf("list trajectories: %w", err)
	}

	// Compute stats from trajectories
	stats := computeStats(trajs)

	// Analyze tool usage
	toolUsage := analyzeToolUsage(trajs)

	// Generate recommendations
	recommendations := generateRecommendations(stats, toolUsage)

	return rc.Emit(command, map[string]any{
		"stats": map[string]any{
			"total_trajectories": stats.TotalTrajectories,
			"success_rate":       stats.SuccessRate,
			"avg_tool_calls":     stats.AvgToolCalls,
			"avg_duration_ms":    stats.AvgDuration.Milliseconds(),
		},
		"tool_usage":      toolUsage,
		"recommendations": recommendations,
		"period_days":     days,
	}, "application/json", envelope.Meta{Source: "run", Runner: "exec"})
}

func computeStats(trajs []trajectory.Trajectory) agentStats {
	if len(trajs) == 0 {
		return agentStats{}
	}

	var successCount int
	var totalToolCalls int
	var totalDuration time.Duration

	for _, t := range trajs {
		if t.Outcome != nil && t.Outcome.Success {
			successCount++
		}
		if t.Outcome != nil {
			totalToolCalls += t.Outcome.ToolCallCount
			totalDuration += time.Duration(t.Outcome.DurationMS) * time.Millisecond
		}
	}

	return agentStats{
		TotalTrajectories: len(trajs),
		SuccessRate:       float64(successCount) / float64(len(trajs)),
		AvgToolCalls:      float64(totalToolCalls) / float64(len(trajs)),
		AvgDuration:       totalDuration / time.Duration(len(trajs)),
	}
}

func analyzeToolUsage(trajs []trajectory.Trajectory) []map[string]any {
	// Count tool usage from trajectories
	toolCounts := make(map[string]int)
	toolSuccess := make(map[string]int)

	for _, traj := range trajs {
		if traj.Outcome == nil {
			continue
		}
		// Note: This is simplified - actual implementation would need to
		// query events for each trajectory to get tool calls
		toolCounts["all"]++
		if traj.Outcome.Success {
			toolSuccess["all"]++
		}
	}

	// Convert to output format
	var result []map[string]any
	for tool, count := range toolCounts {
		successRate := 0.0
		if count > 0 {
			successRate = float64(toolSuccess[tool]) / float64(count)
		}
		result = append(result, map[string]any{
			"tool":         tool,
			"count":        count,
			"success_rate": successRate,
		})
	}

	return result
}

func generateRecommendations(stats agentStats, toolUsage []map[string]any) []string {
	var recommendations []string

	// Check success rate
	if stats.SuccessRate < 0.7 {
		recommendations = append(recommendations,
			fmt.Sprintf("Success rate (%.1f%%) is below 70%%. Consider reviewing failed trajectories for common patterns.",
				stats.SuccessRate*100))
	}

	// Check tool call count
	if stats.AvgToolCalls > 20 {
		recommendations = append(recommendations,
			fmt.Sprintf("Average tool calls (%.1f) is high. Consider optimizing tool sequences.",
				stats.AvgToolCalls))
	}

	// Check duration
	if stats.AvgDuration > 5*time.Minute {
		recommendations = append(recommendations,
			fmt.Sprintf("Average duration (%v) is high. Consider identifying slow operations.",
				stats.AvgDuration.Round(time.Second)))
	}

	// General recommendations
	if stats.TotalTrajectories < 10 {
		recommendations = append(recommendations,
			"Insufficient data for reliable analysis. Need at least 10 trajectories.")
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations,
			"Performance metrics look good. Continue monitoring for trends.")
	}

	return recommendations
}

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit failure")
	os.Exit(1)
}
