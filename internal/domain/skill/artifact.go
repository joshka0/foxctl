package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ArtifactOptions controls how artifact selection behaves.
type ArtifactOptions struct {
	PreferCGO bool
	EntryRoot string
}

// ErrArtifactsMissing indicates a skill artifact could not be found.
var ErrArtifactsMissing = errors.New("skill artifacts missing")

// ResolveArtifactPath finds the executable/module artifact for a resolved skill directory.
func ResolveArtifactPath(dir string, manifest Manifest, opts ArtifactOptions) (string, error) {
	switch manifest.Distribution.Type {
	case "exec":
		if path := resolveExecArtifact(dir, manifest, opts); path != "" {
			return path, nil
		}
	case "wasi":
		path := filepath.Join(dir, "module.wasm")
		if isFile(path) {
			return path, nil
		}
	default:
		return "", fmt.Errorf("unsupported distribution %q", manifest.Distribution.Type)
	}

	return "", fmt.Errorf("%w under %s", ErrArtifactsMissing, dir)
}

func resolveExecArtifact(dir string, manifest Manifest, opts ArtifactOptions) string {
	var candidates []string
	if opts.PreferCGO {
		candidates = append(candidates, filepath.Join(dir, "bin-cgo"))
	}
	candidates = append(candidates, filepath.Join(dir, "bin"))

	for _, c := range candidates {
		if isFile(c) {
			return c
		}
	}

	entry := ""
	if manifest.Distribution.Exec != nil {
		entry = strings.TrimSpace(manifest.Distribution.Exec.Entry)
	}
	if entry == "" {
		return ""
	}

	entryCandidates := []string{}
	if filepath.IsAbs(entry) {
		entryCandidates = append(entryCandidates, entry)
	} else {
		if opts.EntryRoot != "" {
			entryCandidates = append(entryCandidates, filepath.Join(opts.EntryRoot, entry))
		}
		entryCandidates = append(entryCandidates, filepath.Join(dir, entry))
	}

	for _, c := range entryCandidates {
		if isFile(c) {
			return c
		}
	}

	return ""
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
