// Package main implements the fs/ls skill.
package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	fshelpers "github.com/joshka0/foxctl/internal/adapters/skillslib/fs"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/sliceutil"
)

// Input defines the input parameters for fs/ls operations.
type Input struct {
	Path       string   `json:"path"`
	Include    []string `json:"include"`
	Exclude    []string `json:"exclude"`
	MaxEntries int      `json:"max_entries" validate:"gte=0"`
	ShowHidden bool     `json:"show_hidden"`
}

// entry represents a directory entry with metadata.
type entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"is_dir"`
	SizeBytes int64  `json:"size_bytes"`
	Mode      string `json:"mode"`
	ModTime   string `json:"mod_time"`
}

// main is the skill entry point for fs/ls.
func main() {
	skillmain.Main("fs/ls", run)
}

// run orchestrates directory listing with filtering, statistics, and result persistence.
//
// Index:
//   Purpose: List directory contents with filtering, statistics, and optional result persistence
//   Flow: validate input → resolve path → read directory → apply filters → calculate stats → emit results
//   SideEffects: directory traversal; file system access; CAS storage for large result sets
//   FailureModes: invalid paths, permission errors, directory read errors
//   Observability: emits entry counts, file/directory statistics, and artifact hints for large sets
//   Related: readDir, fileSize
//   Keywords: fs/ls, directory_listing, filtering, statistics, file_system
//
// [[domain:directory-listing]]
// [[protocol:fs-metadata-extraction]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if strings.TrimSpace(in.Path) == "" {
		in.Path = "."
	}
	if in.MaxEntries <= 0 {
		in.MaxEntries = 200
	}

	_, validDir, err := skillmain.ResolvePath(rc, in.Path)
	if err != nil {
		return err
	}

	allEntries, err := readDir(validDir, in)
	if err != nil {
		return err
	}

	totalEntries := len(allEntries)
	limitedEntries, limited := sliceutil.LimitWithTruncated(allEntries, in.MaxEntries)

	previewResult, err := skillout.PreviewAndPersistNDJSON(ctx, rc, limitedEntries, rc.MaxPreview, "fs_ls", true)
	if err != nil {
		return err
	}

	// Calculate stats
	files := 0
	dirs := 0
	var totalSize int64
	for _, e := range allEntries {
		if e.IsDir {
			dirs++
		} else {
			files++
			totalSize += e.SizeBytes
		}
	}

	data := map[string]any{
		"path":        validDir,
		"entry_count": totalEntries,
		"files":       files,
		"directories": dirs,
		"total_size":  totalSize,
		"preview":     previewResult.Preview,
	}
	if limited {
		data["truncated"] = true
		data["limited_entries"] = len(limitedEntries)
	}
	if previewResult.Truncated {
		data["preview_truncated"] = true
	}
	skillout.AddArtifact(data, previewResult.Artifact)

	return skillout.Emit(rc, "fs/ls", data)
}

// readDir reads directory entries and applies filtering rules.
func readDir(path string, in Input) ([]entry, error) {
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, skillerr.WrapIO("read dir "+path, err)
	}
	var out []entry
	for _, e := range ents {
		name := e.Name()
		if fshelpers.ShouldSkipHidden(name, in.ShowHidden) {
			continue
		}
		if fsutil.IsSymlinkMode(e.Type()) {
			continue
		}
		full := filepath.Join(path, name)
		if len(in.Include) > 0 && !fshelpers.Matches(full, in.Include) {
			continue
		}
		if fshelpers.Matches(full, fshelpers.AppendCommonExcludes(in.Exclude)) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, entry{
			Name:      name,
			Path:      full,
			IsDir:     e.IsDir(),
			SizeBytes: fileSize(info),
			Mode:      info.Mode().String(),
			ModTime:   info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// fileSize returns the size for files, 0 for directories.
func fileSize(info fs.FileInfo) int64 {
	if info.IsDir() {
		return 0
	}
	return info.Size()
}
