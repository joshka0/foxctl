package optimization

import (
	"context"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

// HumanFeedback represents feedback provided by a human on a trajectory.
type HumanFeedback struct {
	// TrajectoryID is the ID of the trajectory being rated.
	TrajectoryID string `json:"trajectory_id"`

	// WorkspaceID scopes the feedback to a workspace.
	WorkspaceID string `json:"workspace_id"`

	// Rating is the human-provided rating (1-5 scale).
	// 1 = poor, 2 = below average, 3 = average, 4 = good, 5 = excellent
	Rating int `json:"rating"`

	// Feedback is optional text feedback.
	Feedback string `json:"feedback,omitempty"`

	// Timestamp is when the feedback was provided.
	Timestamp time.Time `json:"timestamp"`
}

// FeedbackStats aggregates feedback statistics for an agent role.
type FeedbackStats struct {
	// AgentRole identifies the agent type.
	AgentRole string `json:"agent_role"`

	// TotalFeedback is the total number of feedback entries.
	TotalFeedback int `json:"total_feedback"`

	// AverageRating is the mean rating across all feedback.
	AverageRating float64 `json:"average_rating"`

	// RatingDistribution maps rating (1-5) to count.
	RatingDistribution map[int]int `json:"rating_distribution"`

	// RecentFeedback holds the most recent feedback entries.
	RecentFeedback []HumanFeedback `json:"recent_feedback,omitempty"`
}

// FeedbackCollector handles human feedback collection and storage.
type FeedbackCollector struct {
	trajStore trajectory.Store
}

// NewFeedbackCollector creates a new feedback collector.
func NewFeedbackCollector(trajStore trajectory.Store) *FeedbackCollector {
	return &FeedbackCollector{trajStore: trajStore}
}

// RecordFeedback records human feedback for a trajectory.
func (f *FeedbackCollector) RecordFeedback(ctx context.Context, fb HumanFeedback) error {
	if fb.Rating < 1 || fb.Rating > 5 {
		return fmt.Errorf("feedback: rating must be between 1 and 5, got %d", fb.Rating)
	}

	if fb.Timestamp.IsZero() {
		fb.Timestamp = timeutil.NowUTC()
	}

	// Get the existing trajectory to preserve other outcome fields
	traj, err := f.trajStore.GetTrajectory(ctx, fb.WorkspaceID, fb.TrajectoryID)
	if err != nil {
		return fmt.Errorf("feedback: get trajectory: %w", err)
	}

	// Create or update outcome with human feedback
	var outcome trajectory.Outcome
	if traj.Outcome != nil {
		outcome = *traj.Outcome
	}
	outcome.HumanRating = &fb.Rating
	outcome.Feedback = fb.Feedback
	outcome.RecordedAt = fb.Timestamp

	// Note: We don't infer Success from Rating to avoid overriding explicit outcomes.
	// If the outcome was explicitly marked as failed, a high rating shouldn't change that.
	// Users should explicitly set Success if needed.

	return f.trajStore.SetOutcome(ctx, fb.WorkspaceID, fb.TrajectoryID, outcome)
}

// GetFeedbackStats returns aggregated feedback statistics for an agent role.
func (f *FeedbackCollector) GetFeedbackStats(ctx context.Context, workspaceID, agentRole string) (*FeedbackStats, error) {
	// Query trajectories with human ratings
	minRating := 1
	trajs, err := f.trajStore.ListByOutcome(ctx, trajectory.OutcomeFilter{
		WorkspaceID: workspaceID,
		AgentRole:   agentRole,
		MinRating:   &minRating,
		Limit:       1000, // Get all rated trajectories
	})
	if err != nil {
		return nil, fmt.Errorf("feedback: list by outcome: %w", err)
	}

	stats := &FeedbackStats{
		AgentRole:          agentRole,
		RatingDistribution: make(map[int]int),
		RecentFeedback:     make([]HumanFeedback, 0),
	}

	var totalRating int
	for _, traj := range trajs {
		if traj.Outcome == nil || traj.Outcome.HumanRating == nil {
			continue
		}

		rating := *traj.Outcome.HumanRating
		stats.TotalFeedback++
		totalRating += rating
		stats.RatingDistribution[rating]++

		// Collect recent feedback (up to 10)
		if len(stats.RecentFeedback) < 10 {
			stats.RecentFeedback = append(stats.RecentFeedback, HumanFeedback{
				TrajectoryID: traj.ID,
				WorkspaceID:  traj.WorkspaceID,
				Rating:       rating,
				Feedback:     traj.Outcome.Feedback,
				Timestamp:    traj.Outcome.RecordedAt,
			})
		}
	}

	if stats.TotalFeedback > 0 {
		stats.AverageRating = float64(totalRating) / float64(stats.TotalFeedback)
	}

	return stats, nil
}

// GetHighRatedTrajectories returns trajectories with rating >= minRating.
func (f *FeedbackCollector) GetHighRatedTrajectories(ctx context.Context, workspaceID, agentRole string, minRating int) ([]trajectory.Trajectory, error) {
	return f.trajStore.ListByOutcome(ctx, trajectory.OutcomeFilter{
		WorkspaceID: workspaceID,
		AgentRole:   agentRole,
		MinRating:   &minRating,
		Limit:       100,
	})
}

// GetLowRatedTrajectories returns trajectories with rating <= maxRating.
// Useful for identifying areas that need improvement.
func (f *FeedbackCollector) GetLowRatedTrajectories(ctx context.Context, workspaceID, agentRole string, maxRating int) ([]trajectory.Trajectory, error) {
	minRating := 1 // Must have a rating
	return f.trajStore.ListByOutcome(ctx, trajectory.OutcomeFilter{
		WorkspaceID: workspaceID,
		AgentRole:   agentRole,
		MinRating:   &minRating,
		MaxRating:   &maxRating,
		Limit:       100,
	})
}

// GetTrajectoriesWithFeedback returns trajectories that have text feedback.
func (f *FeedbackCollector) GetTrajectoriesWithFeedback(ctx context.Context, workspaceID, agentRole string) ([]trajectory.Trajectory, error) {
	hasFeedback := true
	return f.trajStore.ListByOutcome(ctx, trajectory.OutcomeFilter{
		WorkspaceID: workspaceID,
		AgentRole:   agentRole,
		HasFeedback: &hasFeedback,
		Limit:       100,
	})
}
