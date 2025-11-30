package artifacts

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cas"
)

func TestStoreReviewArtifact_StoresJSONInCAS(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()

	cfg := config.Config{
		Home: tmp,
		Paths: config.Paths{
			CAS: filepath.Join(tmp, "cas"),
		},
	}

	art := agent.ReviewArtifact{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Status:      "pending",
		Summary:     "test artifact",
	}

	got, err := StoreReviewArtifact(ctx, cfg, art, nil)
	if err != nil {
		t.Fatalf("StoreReviewArtifact error: %v", err)
	}
	if got.ID == "" {
		t.Fatal("expected artifact ID to be set")
	}
	if got.CASDigest != "" {
		t.Fatalf("expected CASDigest to be empty when no body provided, got %q", got.CASDigest)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}

	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open cas store: %v", err)
	}
	defer store.Close()

	rc, meta, err := store.Get(ctx, got.ID)
	if err != nil {
		t.Fatalf("Get artifact from CAS: %v", err)
	}
	defer rc.Close()

	if meta.Digest != got.ID {
		t.Fatalf("expected digest %q, got %q", got.ID, meta.Digest)
	}

	var decoded agent.ReviewArtifact
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(rc); err != nil {
		t.Fatalf("read artifact body: %v", err)
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal artifact json: %v", err)
	}
	if decoded.TaskID != art.TaskID {
		t.Fatalf("expected task_id %q, got %q", art.TaskID, decoded.TaskID)
	}
}

func TestStoreReviewArtifact_WithBodyStoresCASDigest(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	tmp := t.TempDir()

	cfg := config.Config{
		Home: tmp,
		Paths: config.Paths{
			CAS: filepath.Join(tmp, "cas"),
		},
	}

	art := agent.ReviewArtifact{
		WorkspaceID: "ws-1",
		TaskID:      "task-1",
		Status:      "pending",
		Summary:     "with body",
		CreatedAt:   time.Now().UTC(),
	}

	body := []byte("log output")
	got, err := StoreReviewArtifact(ctx, cfg, art, body)
	if err != nil {
		t.Fatalf("StoreReviewArtifact error: %v", err)
	}
	if got.CASDigest == "" {
		t.Fatal("expected CASDigest to be set when body provided")
	}
	if got.ID == "" {
		t.Fatal("expected artifact ID to be set")
	}

	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		t.Fatalf("open cas store: %v", err)
	}
	defer store.Close()

	// Ensure body blob exists
	bodyRC, bodyMeta, err := store.Get(ctx, got.CASDigest)
	if err != nil {
		t.Fatalf("Get body from CAS: %v", err)
	}
	defer bodyRC.Close()
	if bodyMeta.Digest != got.CASDigest {
		t.Fatalf("expected body digest %q, got %q", got.CASDigest, bodyMeta.Digest)
	}
}
