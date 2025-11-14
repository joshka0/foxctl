// Package fs provides helpers for working with skill file access.
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
		if info.IsDir() {
			if shouldSkip(path, opts.Exclude) {
				return filepath.SkipDir
			}
			if opts.MaxDepth > 0 {
				rel, relErr := filepath.Rel(baseAbs, path)
				if relErr != nil {
					return relErr
				}
				if depth(rel) >= opts.MaxDepth {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if len(opts.Include) > 0 && !matches(path, opts.Include) {
			return nil
		}
		if matches(path, opts.Exclude) {
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
	rel := filepath.ToSlash(path)
	for _, g := range globs {
		if ok, err := filepath.Match(g, filepath.Base(path)); err == nil && ok {
			return true
		}
		if ok, err := filepath.Match(g, rel); err == nil && ok {
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
