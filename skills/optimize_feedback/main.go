// Package main implements the optimize/feedback skill for collecting and analyzing human feedback for agent optimization.
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

const command = "optimize/feedback"

// input defines the skill input parameters for feedback collection and analysis with action selection.
type input struct {
	Action       string `json:"action"`
	Workspace    string `json:"workspace"`
	TrajectoryID string `json:"trajectory_id"`
	Rating       int    `json:"rating"`
	Comment      string `json:"comment"`
	Role         string `json:"role"`
}

// main is the skill entry point for optimize/feedback with comprehensive feedback management capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates feedback operations with workspace resolution, store management, and action dispatch.
//
// Index:
// - Purpose: Collect and analyze human feedback for agent optimization with rating and comment tracking
// - Flow: resolve workspace → open trajectory store → create feedback collector → dispatch action (add/stats) → emit results
// - SideEffects: writes feedback records to trajectory store; reads feedback statistics; manages optimization data
// - FailureModes: missing trajectory IDs, invalid ratings, store access failures, workspace resolution errors
// - Observability: emits feedback records, statistics, rating distributions, and comprehensive feedback analytics
// - Related: addFeedback, getFeedbackStats, optimization.NewFeedbackCollector
// - Keywords: optimize/feedback, human_feedback, optimization, rating_collection, feedback_analytics
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

// addFeedback records human feedback for a trajectory with validation and timestamp tracking.
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

// getFeedbackStats retrieves and analyzes feedback statistics for a workspace and role with rating distribution.
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
