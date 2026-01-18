// Package main implements the optimize/feedback skill.
// This skill collects and analyzes human feedback for optimization.
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/agent/optimization"
	"github.com/jkatigb/agentctl/internal/storage/trajectory"
)

const command = "optimize/feedback"

type input struct {
	Action       string `json:"action"`
	Workspace    string `json:"workspace"`
	TrajectoryID string `json:"trajectory_id"`
	Rating       int    `json:"rating"`
	Comment      string `json:"comment"`
	Role         string `json:"role"`
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

	// Create feedback collector
	collector := optimization.NewFeedbackCollector(trajStore)

	switch in.Action {
	case "add":
		return addFeedback(ctx, rc, collector, absWorkspace, in)
	case "stats":
		return getFeedbackStats(ctx, rc, collector, absWorkspace, in)
	default:
		return skillerr.Arg(
			fmt.Sprintf("unknown action: %s", in.Action),
			skillerr.WithHint("Use action=add or action=stats."),
		)
	}
}

func addFeedback(ctx context.Context, rc *skillmain.RunContext, collector *optimization.FeedbackCollector, workspace string, in input) error {
	if in.TrajectoryID == "" {
		return skillerr.Arg("trajectory_id is required for add action")
	}
	if in.Rating < 1 || in.Rating > 5 {
		return skillerr.Validation("rating must be between 1 and 5")
	}

	feedback := optimization.HumanFeedback{
		WorkspaceID:  workspace,
		TrajectoryID: in.TrajectoryID,
		Rating:       in.Rating,
		Feedback:     in.Comment,
		Timestamp:    time.Now(),
	}

	if err := collector.RecordFeedback(ctx, feedback); err != nil {
		return skillerr.WrapRuntime("record feedback", err)
	}

	return skillout.Emit(rc, command, map[string]any{
		"feedback": map[string]any{
			"trajectory_id": in.TrajectoryID,
			"rating":        in.Rating,
			"comment":       in.Comment,
			"timestamp":     feedback.Timestamp,
		},
		"message": "feedback recorded successfully",
	})
}

func getFeedbackStats(ctx context.Context, rc *skillmain.RunContext, collector *optimization.FeedbackCollector, workspace string, in input) error {
	if in.Role == "" {
		return skillerr.Arg("role is required for stats action")
	}

	stats, err := collector.GetFeedbackStats(ctx, workspace, in.Role)
	if err != nil {
		return skillerr.WrapRuntime("get stats", err)
	}

	// Build rating distribution
	ratingDist := make(map[string]int)
	for rating, count := range stats.RatingDistribution {
		ratingDist[fmt.Sprintf("%d", rating)] = count
	}

	return skillout.Emit(rc, command, map[string]any{
		"stats": map[string]any{
			"total_feedback":      stats.TotalFeedback,
			"average_rating":      stats.AverageRating,
			"rating_distribution": ratingDist,
			"agent_role":          stats.AgentRole,
		},
		"workspace": workspace,
		"role":      in.Role,
	})
}
