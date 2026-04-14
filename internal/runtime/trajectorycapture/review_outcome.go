package trajectorycapture

import (
	"context"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/platform/secrets"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

// CaptureReviewOutcome records a review outcome event on the latest trajectory.
func CaptureReviewOutcome(ctx context.Context, storageRoot string, review agent.ReviewArtifact, postReviewEventID string) error {
	if strings.TrimSpace(storageRoot) == "" {
		return fmt.Errorf("trajectorycapture: storage root required")
	}
	if strings.TrimSpace(review.WorkspaceID) == "" {
		return fmt.Errorf("trajectorycapture: review workspace_id required")
	}
	if strings.TrimSpace(review.TaskID) == "" {
		return fmt.Errorf("trajectorycapture: review task_id required")
	}
	if strings.TrimSpace(review.ID) == "" {
		return fmt.Errorf("trajectorycapture: review id required")
	}

	store, err := trajectory.Open(ctx, storageRoot)
	if err != nil {
		return err
	}
	defer func() {
		// Cleanup in defer; error is not actionable.
		_ = store.Close() //nolint:errcheck
	}()

	trajs, err := store.ListTrajectories(ctx, trajectory.ListFilter{
		WorkspaceID: review.WorkspaceID,
		TaskID:      review.TaskID,
		Limit:       10,
	})
	if err != nil {
		return err
	}
	if len(trajs) == 0 {
		return nil
	}

	traj := trajs[0]
	artifactDigest := ""
	if strings.HasPrefix(review.ID, "sha256:") {
		artifactDigest = review.ID
	}

	existing, err := store.ListEvents(ctx, trajectory.EventFilter{
		TrajectoryID: traj.ID,
		Kind:         trajectory.EventKindReviewResult,
		Limit:        100,
	})
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.Meta != nil && strings.TrimSpace(e.Meta.ReviewID) == review.ID {
			return nil
		}
		if artifactDigest != "" && strings.TrimSpace(e.DataArtifact) == artifactDigest {
			return nil
		}
	}

	dataInline := map[string]any{
		"summary":     secrets.Redact(strings.TrimSpace(review.Summary)),
		"review_kind": strings.TrimSpace(review.Kind),
		"review_id":   review.ID,
		"task_id":     strings.TrimSpace(review.TaskID),
	}
	if postReviewEventID != "" {
		dataInline["post_review_event_id"] = postReviewEventID
	}
	if strings.HasPrefix(strings.TrimSpace(review.CASDigest), "sha256:") {
		dataInline["review_body_artifact"] = strings.TrimSpace(review.CASDigest)
	}
	dataInline = secrets.RedactMap(dataInline)

	meta := &trajectory.EventMeta{
		TraceID:   strings.TrimSpace(traj.TraceID),
		TaskID:    strings.TrimSpace(review.TaskID),
		ReviewID:  review.ID,
		CreatedBy: "foxctl",
		CASDigest: artifactDigest,
	}

	status := strings.TrimSpace(review.Status)
	if status == "" {
		status = "unknown"
	}

	_, err = store.InsertEvent(ctx, trajectory.Event{
		TrajectoryID: traj.ID,
		Kind:         trajectory.EventKindReviewResult,
		Actor:        "actor:system:overseer",
		Status:       status,
		DataInline:   dataInline,
		DataArtifact: artifactDigest,
		Meta:         meta,
	})
	return err
}
