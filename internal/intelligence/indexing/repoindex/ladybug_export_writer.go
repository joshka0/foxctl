package repoindex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WriteLadybugExport replaces the explicit target export directory with the
// provided artifact files. It does not compute or clear broader projection
// state.
func WriteLadybugExport(dir string, artifact LadybugExportArtifact) error {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return fmt.Errorf("repoindex: ladybug export directory is required")
	}
	dir = filepath.Clean(dir)

	files, err := validatedLadybugExportFiles(artifact)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("repoindex: create ladybug export directory: %w", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("repoindex: read ladybug export directory: %w", err)
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return fmt.Errorf("repoindex: remove stale ladybug export file %q: %w", entry.Name(), err)
		}
	}

	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, file.Name), file.Data, 0o644); err != nil {
			return fmt.Errorf("repoindex: write ladybug export file %q: %w", file.Name, err)
		}
	}
	return nil
}

func validatedLadybugExportFiles(artifact LadybugExportArtifact) ([]LadybugExportFile, error) {
	seen := make(map[string]struct{}, len(artifact.Files))
	files := make([]LadybugExportFile, 0, len(artifact.Files))
	for _, file := range artifact.Files {
		name, err := validateLadybugExportFileName(file.Name)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[name]; ok {
			return nil, fmt.Errorf("repoindex: duplicate ladybug export file %q", name)
		}
		seen[name] = struct{}{}
		file.Name = name
		files = append(files, file)
	}
	return files, nil
}

func validateLadybugExportFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name {
		return "", fmt.Errorf("repoindex: invalid ladybug export file name %q", name)
	}
	return name, nil
}
