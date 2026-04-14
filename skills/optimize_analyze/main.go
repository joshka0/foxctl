// Package main implements the optimize/analyze skill for analyzing trajectory data and generating optimization insights.
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

const command = "optimize/analyze"

// input defines the skill input parameters for trajectory analysis with workspace, role, and time range filtering.
type input struct {
	Workspace string `json:"workspace"`
	Role      string `json:"role"`
	Days      int    `json:"days"`
}

// main is the skill entry point for optimize/analyze with trajectory analysis capabilities.
func main() {
	skillmain.Main(command, run)
}

// agentStats represents computed performance metrics for agent trajectory analysis.
type agentStats struct {
	TotalTrajectories int
	SuccessRate       float64
	AvgToolCalls      float64
	AvgDuration       time.Duration
}

// run orchestrates trajectory analysis with performance metrics computation and optimization recommendations.
//
// Index:
// - Purpose: Analyze agent trajectory data to compute performance metrics and generate optimization recommendations
// - Flow: validate input → resolve workspace → open trajectory store → filter trajectories → compute stats → analyze tools → generate recommendations
// - SideEffects: reads trajectory store; processes large datasets; computes statistical metrics; analyzes performance patterns
// - FailureModes: missing role parameter, workspace resolution errors, trajectory store access failures, data processing errors
// - Observability: emits performance statistics, tool usage analysis, optimization recommendations, and comprehensive metrics tracking
// - Related: computeStats, analyzeToolUsage, generateRecommendations
// - Keywords: optimize/analyze, trajectory_analysis, performance_metrics, optimization_recommendations, agent_performance
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

	// Open trajectory store
	trajStore, err := trajectory.Open(ctx, rc.Config.Storage.Root)
	if err != nil {
		return skillerr.WrapIO("open trajectory store", err)
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
		return skillerr.WrapIO("list trajectories", err)
	}

	// Compute stats from trajectories
	stats := computeStats(trajs)

	// Analyze tool usage
	toolUsage := analyzeToolUsage(trajs)

	// Generate recommendations
	recommendations := generateRecommendations(stats, toolUsage)

	return skillout.Emit(rc, command, map[string]any{
		"stats": map[string]any{
			"total_trajectories": stats.TotalTrajectories,
			"success_rate":       stats.SuccessRate,
			"avg_tool_calls":     stats.AvgToolCalls,
			"avg_duration_ms":    stats.AvgDuration.Milliseconds(),
		},
		"tool_usage":      toolUsage,
		"recommendations": recommendations,
		"period_days":     days,
	})
}

// computeStats calculates performance statistics from trajectory data with success rates and timing metrics.
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

// analyzeToolUsage analyzes tool usage patterns across trajectories with success rate calculations.
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

// generateRecommendations creates optimization recommendations based on performance metrics and tool usage analysis.
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
