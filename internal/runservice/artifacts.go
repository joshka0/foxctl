package runservice

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

func (e *Executor) handleArtifacts(jobID string, result []byte) error {
	casStore, err := cas.NewStore(e.cfg.Paths.CAS)
	if err != nil {
		return err
	}
	mgr := artifacts.NewManager(casStore)
	digests, err := mgr.PinFromEnvelope(e.ctx, result)
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
	return os.WriteFile(artifactFile(e.cfg.Paths.Jobs, jobID), buf, 0o644)
}

func artifactFile(jobsPath, jobID string) string {
	return filepath.Join(jobsPath, jobID, "artifacts.json")
}

// ReleaseArtifacts removes pins for artifacts associated with a job result.
func ReleaseArtifacts(ctx context.Context, cfgPaths config.Paths, jobID string) error {
	path := artifactFile(cfgPaths.Jobs, jobID)
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
	casStore, err := cas.NewStore(cfgPaths.CAS)
	if err != nil {
		return err
	}
	mgr := artifacts.NewManager(casStore)
	if err := mgr.Unpin(ctx, meta.Digests...); err != nil {
		return fmt.Errorf("unpin artifacts: %w", err)
	}
	return os.Remove(path)
}
