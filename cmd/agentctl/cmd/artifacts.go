package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/artifacts"
	"github.com/jkatigb/agentctl/internal/cas"
	"github.com/jkatigb/agentctl/internal/config"
)

func handleArtifacts(ctx context.Context, cfg config.Config, jobID string, result []byte) error {
	digests := artifacts.Digests(result)
	if len(digests) == 0 {
		return nil
	}
	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return err
	}
	for _, d := range digests {
		if err := store.Pin(ctx, d); err != nil {
			return fmt.Errorf("pin artifact %s: %w", d, err)
		}
	}
	meta := map[string]any{"digests": digests}
	buf, _ := json.Marshal(meta)
	return os.WriteFile(artifactFile(cfg, jobID), buf, 0o644)
}

func releaseArtifacts(ctx context.Context, cfg config.Config, jobID string) error {
	path := artifactFile(cfg, jobID)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var meta struct {
		Digests []string `json:"digests"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return err
	}
	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return err
	}
	for _, d := range meta.Digests {
		if err := store.Unpin(ctx, d); err != nil {
			return fmt.Errorf("unpin artifact %s: %w", d, err)
		}
	}
	return os.Remove(path)
}

func artifactFile(cfg config.Config, jobID string) string {
	return filepath.Join(cfg.Paths.Jobs, jobID, "artifacts.json")
}
