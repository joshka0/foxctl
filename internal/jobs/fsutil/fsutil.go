package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// JobDir returns the on-disk directory for a job rooted under base.
func JobDir(root, jobID string) string {
	return filepath.Join(root, jobID)
}

// EnsureJobDir creates the job directory if it does not already exist.
func EnsureJobDir(root, jobID string) (string, error) {
	dir := JobDir(root, jobID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("jobs: ensure job dir: %w", err)
	}
	return dir, nil
}

// RecordWorkspace persists the workspace path for a job, without overwriting existing values.
func RecordWorkspace(jobDir, workspacePath string) error {
	if workspacePath == "" {
		return nil
	}
	if err := os.MkdirAll(jobDir, 0o755); err != nil {
		return fmt.Errorf("jobs: ensure job dir: %w", err)
	}
	path := filepath.Join(jobDir, "workspace")
	data, err := os.ReadFile(path)
	if err == nil {
		if strings.TrimSpace(string(data)) != "" {
			return nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("jobs: read workspace: %w", err)
	}
	if err := os.WriteFile(path, []byte(workspacePath), 0o644); err != nil {
		return fmt.Errorf("jobs: write workspace: %w", err)
	}
	return nil
}
