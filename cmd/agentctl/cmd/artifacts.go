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
	casStore, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return err
	}
	mgr := artifacts.NewManager(casStore)

	digests, err := mgr.PinFromEnvelope(ctx, result)
	if err != nil {
		return fmt.Errorf("pin artifacts: %w", err)
	}
	if len(digests) == 0 {
		return nil
	}

	meta := map[string]any{"digests": digests}
	buf, err := json.Marshal(meta)
	if err != nil {
		return err
	}
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

	casStore, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return err
	}
	mgr := artifacts.NewManager(casStore)

	if err := mgr.Unpin(ctx, meta.Digests...); err != nil {
		return fmt.Errorf("unpin artifacts: %w", err)
	}

	return os.Remove(path)
}

func artifactFile(cfg config.Config, jobID string) string {
	return filepath.Join(cfg.Paths.Jobs, jobID, "artifacts.json")
}
