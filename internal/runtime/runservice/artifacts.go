package runservice

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joshka0/foxctl/internal/adapters/artifacts"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/storage/cas"
)

func (e *Executor) handleArtifacts(jobID string, result []byte) error {
	digests := artifacts.Digests(result)
	if len(digests) == 0 {
		return nil
	}

	casStore, err := cas.NewStore(e.cfg.Paths.CAS)
	if err != nil {
		return err
	}
	defer func() { errs.Ignore(casStore.Close(), "close cas store") }()

	mgr := artifacts.NewManager(casStore)
	if err := mgr.Pin(e.ctx, digests...); err != nil {
		return fmt.Errorf("pin artifacts: %w", err)
	}

	// Ensure job directory exists before writing artifacts.json
	out := artifactFile(e.cfg.Paths.Jobs, jobID)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return artifactMetadataError{err: fmt.Errorf("ensure job dir: %w", err)}
	}

	meta := map[string]any{"digests": digests}
	buf, err := json.Marshal(meta)
	if err != nil {
		return artifactMetadataError{err: err}
	}
	if err := os.WriteFile(out, buf, 0o644); err != nil {
		return artifactMetadataError{err: err}
	}
	return nil
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
	defer func() { errs.Ignore(casStore.Close(), "close cas store") }()

	mgr := artifacts.NewManager(casStore)
	if err := mgr.Unpin(ctx, meta.Digests...); err != nil {
		return fmt.Errorf("unpin artifacts: %w", err)
	}
	return os.Remove(path)
}

type artifactMetadataError struct {
	err error
}

func (e artifactMetadataError) Error() string {
	return e.err.Error()
}

func (e artifactMetadataError) Unwrap() error {
	return e.err
}
