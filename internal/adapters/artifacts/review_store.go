package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

// ReviewArtifactMediaType is the content type used when storing review artifacts in CAS.
const ReviewArtifactMediaType = "application/vnd.foxctl.review+json"

// StoreReviewArtifact persists a ReviewArtifact via CAS and returns the updated
// artifact with ID and optional CASDigest populated.
//
// Phase 1 behavior:
//   - Always stores the artifact record itself in CAS as JSON.
//   - Optionally stores a separate body payload when provided, recording its
//     digest in CASDigest.
func StoreReviewArtifact(ctx context.Context, cfg config.Config, art agent.ReviewArtifact, body []byte) (agent.ReviewArtifact, error) {
	if art.WorkspaceID == "" {
		return art, fmt.Errorf("review artifact requires workspace_id")
	}
	if art.TaskID == "" {
		return art, fmt.Errorf("review artifact requires task_id")
	}
	if art.Status == "" {
		art.Status = "pending"
	}
	if art.CreatedAt.IsZero() {
		art.CreatedAt = time.Now().UTC()
	}

	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return art, fmt.Errorf("open cas store: %w", err)
	}
	defer func() { errs.Ignore(store.Close(), "close cas store") }()

	// Optionally store a large body payload first.
	if len(body) > 0 {
		obj, err := store.Put(ctx, bytes.NewReader(body), "application/octet-stream", []string{
			"review",
			"review:body",
			"workspace:" + art.WorkspaceID,
			"task:" + art.TaskID,
		})
		if err != nil {
			return art, fmt.Errorf("store review body: %w", err)
		}
		art.CASDigest = obj.Digest
	}

	buf, err := json.Marshal(art)
	if err != nil {
		return art, fmt.Errorf("marshal review artifact: %w", err)
	}

	obj, err := store.Put(ctx, bytes.NewReader(buf), ReviewArtifactMediaType, []string{
		"review",
		"review:artifact",
		"workspace:" + art.WorkspaceID,
		"task:" + art.TaskID,
	})
	if err != nil {
		return art, fmt.Errorf("store review artifact: %w", err)
	}
	art.ID = obj.Digest

	return art, nil
}
