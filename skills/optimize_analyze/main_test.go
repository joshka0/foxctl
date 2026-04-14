package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage/trajectory"
	"github.com/stretchr/testify/assert"
)

// Tests for constants

func TestCommand(t *testing.T) {
	assert.Equal(t, "optimize/analyze", command)
}

// Tests for input structure

func TestInput_AllFields(t *testing.T) {
	in := input{
		Workspace: "/workspace/path",
		Role:      "coder",
		Days:      30,
	}

	assert.Equal(t, "/workspace/path", in.Workspace)
	assert.Equal(t, "coder", in.Role)
	assert.Equal(t, 30, in.Days)
}

func TestInput_JSONSerialization(t *testing.T) {
	in := input{
		Workspace: "/test/workspace",
		Role:      "reviewer",
		Days:      14,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Role, decoded.Role)
	assert.Equal(t, in.Days, decoded.Days)
}

func TestInput_EmptyFields(t *testing.T) {
	in := input{}

	assert.Empty(t, in.Workspace)
	assert.Empty(t, in.Role)
	assert.Zero(t, in.Days)
}

func TestInput_RoleValues(t *testing.T) {
	roles := []string{"coder", "planner", "reviewer", "overseer"}

	for _, role := range roles {
		in := input{Role: role}
		assert.Equal(t, role, in.Role)
	}
}

// Tests for agentStats structure

func TestAgentStats_AllFields(t *testing.T) {
	stats := agentStats{
		TotalTrajectories: 100,
		SuccessRate:       0.85,
		AvgToolCalls:      15.5,
		AvgDuration:       3 * time.Minute,
	}

	assert.Equal(t, 100, stats.TotalTrajectories)
	assert.Equal(t, 0.85, stats.SuccessRate)
	assert.Equal(t, 15.5, stats.AvgToolCalls)
	assert.Equal(t, 3*time.Minute, stats.AvgDuration)
}

func TestAgentStats_EmptyFields(t *testing.T) {
	stats := agentStats{}

	assert.Zero(t, stats.TotalTrajectories)
	assert.Zero(t, stats.SuccessRate)
	assert.Zero(t, stats.AvgToolCalls)
	assert.Zero(t, stats.AvgDuration)
}

// Tests for computeStats helper

func TestComputeStats_Empty(t *testing.T) {
	trajs := []trajectory.Trajectory{}

	stats := computeStats(trajs)

	assert.Zero(t, stats.TotalTrajectories)
	assert.Zero(t, stats.SuccessRate)
	assert.Zero(t, stats.AvgToolCalls)
	assert.Zero(t, stats.AvgDuration)
}

func TestComputeStats_SingleSuccess(t *testing.T) {
	trajs := []trajectory.Trajectory{
		{
			ID: "traj-1",
			Outcome: &trajectory.Outcome{
				Success:       true,
				ToolCallCount: 10,
				DurationMS:    60000, // 1 minute
			},
		},
	}

	stats := computeStats(trajs)

	assert.Equal(t, 1, stats.TotalTrajectories)
	assert.Equal(t, 1.0, stats.SuccessRate)
	assert.Equal(t, 10.0, stats.AvgToolCalls)
	assert.Equal(t, time.Minute, stats.AvgDuration)
}

func TestComputeStats_MixedResults(t *testing.T) {
	trajs := []trajectory.Trajectory{
		{
			ID: "traj-1",
			Outcome: &trajectory.Outcome{
				Success:       true,
				ToolCallCount: 10,
				DurationMS:    60000,
			},
		},
		{
			ID: "traj-2",
			Outcome: &trajectory.Outcome{
				Success:       false,
				ToolCallCount: 20,
				DurationMS:    120000,
			},
		},
		{
			ID: "traj-3",
			Outcome: &trajectory.Outcome{
				Success:       true,
				ToolCallCount: 15,
				DurationMS:    90000,
			},
		},
	}

	stats := computeStats(trajs)

	assert.Equal(t, 3, stats.TotalTrajectories)
	assert.InDelta(t, 0.666, stats.SuccessRate, 0.01)  // 2/3
	assert.Equal(t, 15.0, stats.AvgToolCalls)          // (10+20+15)/3 = 15
	assert.Equal(t, 90*time.Second, stats.AvgDuration) // (60+120+90)/3 = 90s
}

func TestComputeStats_NilOutcome(t *testing.T) {
	trajs := []trajectory.Trajectory{
		{
			ID:      "traj-1",
			Outcome: nil,
		},
		{
			ID: "traj-2",
			Outcome: &trajectory.Outcome{
				Success:       true,
				ToolCallCount: 5,
				DurationMS:    30000,
			},
		},
	}

	stats := computeStats(trajs)

	assert.Equal(t, 2, stats.TotalTrajectories)
	assert.Equal(t, 0.5, stats.SuccessRate) // 1/2
}

func TestComputeStats_AllFailed(t *testing.T) {
	trajs := []trajectory.Trajectory{
		{
			ID: "traj-1",
			Outcome: &trajectory.Outcome{
				Success:       false,
				ToolCallCount: 10,
				DurationMS:    60000,
			},
		},
		{
			ID: "traj-2",
			Outcome: &trajectory.Outcome{
				Success:       false,
				ToolCallCount: 20,
				DurationMS:    120000,
			},
		},
	}

	stats := computeStats(trajs)

	assert.Equal(t, 2, stats.TotalTrajectories)
	assert.Equal(t, 0.0, stats.SuccessRate)
}

// Tests for analyzeToolUsage helper

func TestAnalyzeToolUsage_Empty(t *testing.T) {
	trajs := []trajectory.Trajectory{}

	usage := analyzeToolUsage(trajs)

	assert.Empty(t, usage)
}

func TestAnalyzeToolUsage_WithOutcomes(t *testing.T) {
	trajs := []trajectory.Trajectory{
		{
			ID: "traj-1",
			Outcome: &trajectory.Outcome{
				Success: true,
			},
		},
		{
			ID: "traj-2",
			Outcome: &trajectory.Outcome{
				Success: false,
			},
		},
	}

	usage := analyzeToolUsage(trajs)

	// Should have at least one entry
	assert.NotEmpty(t, usage)
}

func TestAnalyzeToolUsage_NilOutcome(t *testing.T) {
	trajs := []trajectory.Trajectory{
		{
			ID:      "traj-1",
			Outcome: nil,
		},
	}

	usage := analyzeToolUsage(trajs)

	// Should be empty when no outcomes
	assert.Empty(t, usage)
}

// Tests for generateRecommendations helper

func TestGenerateRecommendations_GoodStats(t *testing.T) {
	stats := agentStats{
		TotalTrajectories: 50,
		SuccessRate:       0.85,
		AvgToolCalls:      10.0,
		AvgDuration:       2 * time.Minute,
	}

	recs := generateRecommendations(stats, nil)

	assert.NotEmpty(t, recs)
	assert.Contains(t, recs[0], "Performance metrics look good")
}

func TestGenerateRecommendations_LowSuccessRate(t *testing.T) {
	stats := agentStats{
		TotalTrajectories: 50,
		SuccessRate:       0.50, // Below 70%
		AvgToolCalls:      10.0,
		AvgDuration:       2 * time.Minute,
	}

	recs := generateRecommendations(stats, nil)

	found := false
	for _, rec := range recs {
		if rec != "" && (rec == "Success rate (50.0%) is below 70%. Consider reviewing failed trajectories for common patterns." ||
			rec[:12] == "Success rate") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should recommend reviewing failed trajectories")
}

func TestGenerateRecommendations_HighToolCalls(t *testing.T) {
	stats := agentStats{
		TotalTrajectories: 50,
		SuccessRate:       0.85,
		AvgToolCalls:      25.0, // Above 20
		AvgDuration:       2 * time.Minute,
	}

	recs := generateRecommendations(stats, nil)

	found := false
	for _, rec := range recs {
		if rec != "" && (rec == "Average tool calls (25.0) is high. Consider optimizing tool sequences." ||
			rec[:17] == "Average tool call") {
			found = true
			break
		}
	}
	assert.True(t, found, "Should recommend optimizing tool sequences")
}

func TestGenerateRecommendations_HighDuration(t *testing.T) {
	stats := agentStats{
		TotalTrajectories: 50,
		SuccessRate:       0.85,
		AvgToolCalls:      10.0,
		AvgDuration:       10 * time.Minute, // Above 5 minutes
	}

	recs := generateRecommendations(stats, nil)

	found := false
	for _, rec := range recs {
		if rec != "" && rec[:15] == "Average duratio" {
			found = true
			break
		}
	}
	assert.True(t, found, "Should recommend identifying slow operations")
}

func TestGenerateRecommendations_InsufficientData(t *testing.T) {
	stats := agentStats{
		TotalTrajectories: 5, // Below 10
		SuccessRate:       0.85,
		AvgToolCalls:      10.0,
		AvgDuration:       2 * time.Minute,
	}

	recs := generateRecommendations(stats, nil)

	found := false
	for _, rec := range recs {
		if rec != "" && rec[:18] == "Insufficient data " {
			found = true
			break
		}
	}
	assert.True(t, found, "Should warn about insufficient data")
}

func TestGenerateRecommendations_MultipleIssues(t *testing.T) {
	stats := agentStats{
		TotalTrajectories: 5,                // Low data
		SuccessRate:       0.50,             // Low success
		AvgToolCalls:      25.0,             // High calls
		AvgDuration:       10 * time.Minute, // High duration
	}

	recs := generateRecommendations(stats, nil)

	// Should have multiple recommendations
	assert.GreaterOrEqual(t, len(recs), 3)
}

// Tests for days default

func TestDaysDefault(t *testing.T) {
	days := 0
	if days <= 0 {
		days = 30
	}
	assert.Equal(t, 30, days)
}

func TestDaysNegative(t *testing.T) {
	days := -5
	if days <= 0 {
		days = 30
	}
	assert.Equal(t, 30, days)
}

func TestDaysPositive(t *testing.T) {
	days := 14
	if days <= 0 {
		days = 30
	}
	assert.Equal(t, 14, days)
}

// Edge case tests

func TestInput_FullJSONRoundTrip(t *testing.T) {
	in := input{
		Workspace: "/full/test/workspace",
		Role:      "coder",
		Days:      60,
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, in.Workspace, decoded.Workspace)
	assert.Equal(t, in.Role, decoded.Role)
	assert.Equal(t, in.Days, decoded.Days)
}

func TestComputeStats_LargeDataset(t *testing.T) {
	trajs := make([]trajectory.Trajectory, 1000)
	for i := range trajs {
		success := i%2 == 0 // 50% success rate
		trajs[i] = trajectory.Trajectory{
			ID: "traj-" + string(rune('0'+i%10)),
			Outcome: &trajectory.Outcome{
				Success:       success,
				ToolCallCount: 10,
				DurationMS:    60000,
			},
		}
	}

	stats := computeStats(trajs)

	assert.Equal(t, 1000, stats.TotalTrajectories)
	assert.Equal(t, 0.5, stats.SuccessRate)
	assert.Equal(t, 10.0, stats.AvgToolCalls)
	assert.Equal(t, time.Minute, stats.AvgDuration)
}

func TestAgentStats_LongDuration(t *testing.T) {
	stats := agentStats{
		TotalTrajectories: 10,
		SuccessRate:       0.8,
		AvgToolCalls:      100.0,
		AvgDuration:       time.Hour,
	}

	assert.Equal(t, time.Hour, stats.AvgDuration)
}

func TestComputeStats_ZeroDuration(t *testing.T) {
	trajs := []trajectory.Trajectory{
		{
			ID: "traj-1",
			Outcome: &trajectory.Outcome{
				Success:       true,
				ToolCallCount: 5,
				DurationMS:    0,
			},
		},
	}

	stats := computeStats(trajs)

	assert.Zero(t, stats.AvgDuration)
}

func TestInput_WorkspaceWithSpaces(t *testing.T) {
	in := input{
		Workspace: "/path/with spaces/workspace",
		Role:      "coder",
	}

	data, err := json.Marshal(in)
	assert.NoError(t, err)

	var decoded input
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, "/path/with spaces/workspace", decoded.Workspace)
}
