package optimization_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/joshka0/foxctl/internal/agent/optimization"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

func TestFeedbackCollectorRejectsRatingsOutsideOneToFive(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	collector := optimization.NewFeedbackCollector(trajStore)
	for _, rating := range generatedOutOfRangeRatings() {
		t.Run(fmt.Sprintf("rating_%d", rating), func(t *testing.T) {
			traj, err := trajStore.InsertTrajectory(ctx, trajectory.Trajectory{
				WorkspaceID: "ws-test",
				Status:      trajectory.StatusOK,
			})
			if err != nil {
				t.Fatalf("insert trajectory: %v", err)
			}

			err = collector.RecordFeedback(ctx, optimization.HumanFeedback{
				WorkspaceID:  "ws-test",
				TrajectoryID: traj.ID,
				Rating:       rating,
				Feedback:     "invalid rating",
			})
			if err == nil {
				t.Fatalf("expected rating %d to be rejected", rating)
			}

			got, err := trajStore.GetTrajectory(ctx, "ws-test", traj.ID)
			if err != nil {
				t.Fatalf("get trajectory: %v", err)
			}
			if got.Outcome != nil && got.Outcome.HumanRating != nil {
				t.Fatalf("invalid rating %d persisted outcome %+v", rating, got.Outcome)
			}
		})
	}
}

func TestFeedbackCollectorAcceptsBoundaryRatings(t *testing.T) {
	ctx := context.Background()
	trajStore := openTestTrajStore(t)
	defer trajStore.Close() //nolint:errcheck

	collector := optimization.NewFeedbackCollector(trajStore)
	for _, rating := range []int{1, 5} {
		t.Run(fmt.Sprintf("rating_%d", rating), func(t *testing.T) {
			traj, err := trajStore.InsertTrajectory(ctx, trajectory.Trajectory{
				WorkspaceID: "ws-test",
				Status:      trajectory.StatusOK,
			})
			if err != nil {
				t.Fatalf("insert trajectory: %v", err)
			}

			err = collector.RecordFeedback(ctx, optimization.HumanFeedback{
				WorkspaceID:  "ws-test",
				TrajectoryID: traj.ID,
				Rating:       rating,
				Feedback:     "boundary rating",
			})
			if err != nil {
				t.Fatalf("record feedback: %v", err)
			}

			got, err := trajStore.GetTrajectory(ctx, "ws-test", traj.ID)
			if err != nil {
				t.Fatalf("get trajectory: %v", err)
			}
			if got.Outcome == nil || got.Outcome.HumanRating == nil {
				t.Fatalf("rating %d was not persisted in outcome %+v", rating, got.Outcome)
			}
			if *got.Outcome.HumanRating != rating {
				t.Fatalf("human rating=%d want %d", *got.Outcome.HumanRating, rating)
			}
		})
	}
}

func generatedOutOfRangeRatings() []int {
	ratings := make([]int, 0, 22)
	for rating := -10; rating <= 0; rating++ {
		ratings = append(ratings, rating)
	}
	for rating := 6; rating <= 16; rating++ {
		ratings = append(ratings, rating)
	}
	return ratings
}
