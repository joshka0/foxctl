package fs

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ListOptions configures how filesystem entries are discovered.
type ListOptions struct {
	BasePath string
	Include  []string
	Exclude  []string
	// MaxDepth limits directory traversal depth. A depth of 0 means no limit.
	// Directories at depth >= MaxDepth are pruned, which means files at exactly
	// MaxDepth are included (since they reside in a parent directory at MaxDepth-1).
	MaxDepth int
}

// FileEntry describes a file discovered by the walker.
type FileEntry struct {
	Path string
	Info os.FileInfo
}

// WalkFiles walks files under base respecting include/exclude globs.
func WalkFiles(opts ListOptions) ([]FileEntry, error) {
	base := opts.BasePath
	if base == "" {
		base = "."
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}

	var entries []FileEntry
	err = filepath.Walk(baseAbs, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == baseAbs {
			return nil
		}
		rel, relErr := filepath.Rel(baseAbs, path)
		if relErr != nil {
			return relErr
		}
		if info.IsDir() {
			if matchesPath(path, rel, opts.Exclude) {
				return filepath.SkipDir
			}
			if opts.MaxDepth > 0 {
				if depth(rel) >= opts.MaxDepth {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if len(opts.Include) > 0 && !matchesPath(path, rel, opts.Include) {
			return nil
		}
		if matchesPath(path, rel, opts.Exclude) {
			return nil
		}
		entries = append(entries, FileEntry{Path: path, Info: info})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func matches(path string, globs []string) bool {
	return matchesPath(path, "", globs)
}

func matchesPath(path, rel string, globs []string) bool {
	slashPath := filepath.ToSlash(path)
	slashRel := filepath.ToSlash(rel)
	for _, g := range globs {
		if ok, err := filepath.Match(g, filepath.Base(path)); err == nil && ok {
			return true
		}
		if slashRel != "" {
			if ok, err := filepath.Match(g, slashRel); err == nil && ok {
				return true
			}
		}
		if ok, err := filepath.Match(g, slashPath); err == nil && ok {
			return true
		}
	}
	return false
}

func shouldSkip(path string, globs []string) bool {
	return matches(path, globs)
}

func depth(rel string) int {
	if rel == "." {
		return 0
	}
	rel = filepath.ToSlash(rel)
	return strings.Count(rel, "/") + 1
}
