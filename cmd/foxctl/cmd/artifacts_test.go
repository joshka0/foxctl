package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/platform/config"
)

func TestArtifactFile(t *testing.T) {
	cfg := config.Config{Paths: config.Paths{Jobs: "/tmp/test-jobs"}}
	jobID := "job123"

	result := artifactFile(cfg, jobID)
	expected := filepath.Join("/tmp/test-jobs", "job123", "artifacts.json")

	if result != expected {
		t.Errorf("artifactFile() = %s, want %s", result, expected)
	}
}

func TestHandleArtifactsNoDigests(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	cfg := config.Config{Paths: config.Paths{
		CAS:  filepath.Join(tmpDir, "cas"),
		Jobs: filepath.Join(tmpDir, "jobs"),
	}}

	if err := os.MkdirAll(cfg.Paths.CAS, 0o755); err != nil {
		t.Fatalf("failed to create CAS dir: %v", err)
	}

	result := []byte(`{"status":"ok","data":{"result":"success"}}`)
	if err := handleArtifacts(ctx, cfg, "job1", result); err != nil {
		t.Errorf("handleArtifacts() with no digests should not error: %v", err)
	}

	if _, err := os.Stat(artifactFile(cfg, "job1")); !os.IsNotExist(err) {
		t.Fatalf("expected artifacts file not to exist when no digests present")
	}
}

func TestReleaseArtifactsNonExistentFile(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	cfg := config.Config{Paths: config.Paths{
		CAS:  filepath.Join(tmpDir, "cas"),
		Jobs: filepath.Join(tmpDir, "jobs"),
	}}

	if err := releaseArtifacts(ctx, cfg, "missing"); err != nil {
		t.Errorf("releaseArtifacts() should not error for non-existent file: %v", err)
	}
}

func TestReleaseArtifactsInvalidJSON(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	cfg := config.Config{Paths: config.Paths{
		CAS:  filepath.Join(tmpDir, "cas"),
		Jobs: filepath.Join(tmpDir, "jobs"),
	}}

	jobDir := filepath.Join(cfg.Paths.Jobs, "job1")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatalf("failed to create job dir: %v", err)
	}

	artifactPath := artifactFile(cfg, "job1")
	if err := os.WriteFile(artifactPath, []byte("not json"), 0o644); err != nil {
		t.Fatalf("failed to write artifacts file: %v", err)
	}

	if err := releaseArtifacts(ctx, cfg, "job1"); err == nil {
		t.Fatal("expected error for invalid JSON metadata")
	}
}

func TestReleaseArtifactsValidJSON(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	cfg := config.Config{Paths: config.Paths{
		CAS:  filepath.Join(tmpDir, "cas"),
		Jobs: filepath.Join(tmpDir, "jobs"),
	}}

	if err := os.MkdirAll(cfg.Paths.CAS, 0o755); err != nil {
		t.Fatalf("failed to create CAS dir: %v", err)
	}
	jobDir := filepath.Join(cfg.Paths.Jobs, "job1")
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		t.Fatalf("failed to create job dir: %v", err)
	}

	artifactPath := artifactFile(cfg, "job1")
	meta := map[string]any{"digests": []string{}}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	if err := os.WriteFile(artifactPath, data, 0o644); err != nil {
		t.Fatalf("failed to write artifacts file: %v", err)
	}

	if err := releaseArtifacts(ctx, cfg, "job1"); err != nil {
		t.Fatalf("releaseArtifacts() failed: %v", err)
	}
	if _, err := os.Stat(artifactPath); !os.IsNotExist(err) {
		t.Fatalf("expected artifacts file to be removed after release")
	}
}
