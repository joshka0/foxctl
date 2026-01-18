package fs

import (
	"fmt"
	"os"
	"path/filepath"
)

// CollectOptions configures file collection across one or more paths.
type CollectOptions struct {
	Paths         []string
	Include       []string
	Exclude       []string
	IncludeHidden bool
	Extensions    []string
	ValidatePath  func(string) (string, error)
}

// CollectEntries collects file entries from the provided paths.
func CollectEntries(opts CollectOptions) ([]FileEntry, error) {
	var entries []FileEntry
	seen := make(map[string]bool)

	for _, p := range opts.Paths {
		resolved := p
		if opts.ValidatePath != nil {
			var err error
			resolved, err = opts.ValidatePath(p)
			if err != nil {
				return nil, err
			}
		}

		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}

		if !info.IsDir() {
			if !seen[resolved] && MatchesExtension(resolved, opts.Extensions) {
				entries = append(entries, FileEntry{Path: resolved, Info: info})
				seen[resolved] = true
			}
			continue
		}

		exclude := AddHiddenExclude(opts.Exclude, opts.IncludeHidden)
		listOpts := ListOptions{
			BasePath: resolved,
			Include:  opts.Include,
			Exclude:  exclude,
		}
		found, err := WalkFiles(listOpts)
		if err != nil {
			return nil, fmt.Errorf("walk %s: %w", p, err)
		}
		for _, entry := range found {
			if seen[entry.Path] {
				continue
			}
			if !MatchesExtension(entry.Path, opts.Extensions) {
				continue
			}
			entries = append(entries, entry)
			seen[entry.Path] = true
		}
	}

	return entries, nil
}

// MatchesExtension reports whether the path matches any of the extensions.
func MatchesExtension(path string, extensions []string) bool {
	if len(extensions) == 0 {
		return true
	}
	ext := filepath.Ext(path)
	for _, e := range extensions {
		if ext == e || ext == "."+e {
			return true
		}
	}
	return false
}
