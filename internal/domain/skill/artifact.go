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
		if isFileWithinRoot(c, dir) {
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

	if filepath.IsAbs(entry) {
		return ""
	}
	type entryCandidate struct {
		path string
		root string
	}
	entryCandidates := []entryCandidate{}
	if opts.EntryRoot != "" {
		entryCandidates = append(entryCandidates, entryCandidate{
			path: filepath.Join(opts.EntryRoot, entry),
			root: opts.EntryRoot,
		})
	}
	entryCandidates = append(entryCandidates, entryCandidate{
		path: filepath.Join(dir, entry),
		root: dir,
	})

	for _, c := range entryCandidates {
		if isFileWithinRoot(c.path, c.root) {
			return c.path
		}
	}

	return ""
}

func isFileWithinRoot(path, root string) bool {
	if !isFile(path) {
		return false
	}
	return isPathWithinRoot(path, root)
}

func isPathWithinRoot(path, root string) bool {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if resolvedRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolvedRoot
	}
	if resolvedPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolvedPath
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
