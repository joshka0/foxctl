package agent

import (
	"encoding/json"
	"testing"
	"time"
)

func TestReviewArtifact_JSONSerialization(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	artifact := ReviewArtifact{
		ID:          "review-123",
		WorkspaceID: "ws-456",
		TaskID:      "task-789",
		Kind:        "auto",
		Status:      "ok",
		Summary:     "All checks passed successfully",
		CASDigest:   "sha256:abc123def456",
		CreatedAt:   now,
		CreatedBy:   "agent:reviewer",
	}

	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got ReviewArtifact
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.ID != artifact.ID {
		t.Errorf("ID = %q, want %q", got.ID, artifact.ID)
	}
	if got.WorkspaceID != artifact.WorkspaceID {
		t.Errorf("WorkspaceID = %q, want %q", got.WorkspaceID, artifact.WorkspaceID)
	}
	if got.TaskID != artifact.TaskID {
		t.Errorf("TaskID = %q, want %q", got.TaskID, artifact.TaskID)
	}
	if got.Kind != artifact.Kind {
		t.Errorf("Kind = %q, want %q", got.Kind, artifact.Kind)
	}
	if got.Status != artifact.Status {
		t.Errorf("Status = %q, want %q", got.Status, artifact.Status)
	}
	if got.Summary != artifact.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, artifact.Summary)
	}
	if got.CASDigest != artifact.CASDigest {
		t.Errorf("CASDigest = %q, want %q", got.CASDigest, artifact.CASDigest)
	}
	if got.CreatedBy != artifact.CreatedBy {
		t.Errorf("CreatedBy = %q, want %q", got.CreatedBy, artifact.CreatedBy)
	}
}

func TestReviewArtifact_AllKinds(t *testing.T) {
	kinds := []string{"auto", "human", "mixed"}

	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			artifact := ReviewArtifact{
				ID:          "review-test",
				WorkspaceID: "ws-test",
				TaskID:      "task-test",
				Kind:        kind,
				Status:      "ok",
				CreatedAt:   time.Now().UTC(),
			}

			data, err := json.Marshal(artifact)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			var got ReviewArtifact
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			if got.Kind != kind {
				t.Errorf("Kind = %q, want %q", got.Kind, kind)
			}
		})
	}
}

func TestReviewArtifact_AllStatuses(t *testing.T) {
	statuses := []string{"ok", "failed", "pending"}

	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			artifact := ReviewArtifact{
				ID:          "review-test",
				WorkspaceID: "ws-test",
				TaskID:      "task-test",
				Kind:        "auto",
				Status:      status,
				CreatedAt:   time.Now().UTC(),
			}

			data, err := json.Marshal(artifact)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}

			var got ReviewArtifact
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() error = %v", err)
			}

			if got.Status != status {
				t.Errorf("Status = %q, want %q", got.Status, status)
			}
		})
	}
}

func TestReviewArtifact_OptionalFields(t *testing.T) {
	// Test serialization with minimal fields (optional fields omitted)
	artifact := ReviewArtifact{
		ID:          "review-minimal",
		WorkspaceID: "ws-456",
		TaskID:      "task-789",
		Status:      "pending",
		CreatedAt:   time.Now().UTC(),
	}

	data, err := json.Marshal(artifact)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got ReviewArtifact
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got.Kind != "" {
		t.Errorf("Kind should be empty, got %q", got.Kind)
	}
	if got.Summary != "" {
		t.Errorf("Summary should be empty, got %q", got.Summary)
	}
	if got.CASDigest != "" {
		t.Errorf("CASDigest should be empty, got %q", got.CASDigest)
	}
	if got.CreatedBy != "" {
		t.Errorf("CreatedBy should be empty, got %q", got.CreatedBy)
	}
}
