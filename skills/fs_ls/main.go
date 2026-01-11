// Package main implements the fs/ls skill.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

// Input defines the input parameters for fs/ls.
type Input struct {
	Path       string   `json:"path"`
	Include    []string `json:"include"`
	Exclude    []string `json:"exclude"`
	MaxEntries int      `json:"max_entries" validate:"gte=0"`
	ShowHidden bool     `json:"show_hidden"`
}

type entry struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	IsDir     bool   `json:"is_dir"`
	SizeBytes int64  `json:"size_bytes"`
	Mode      string `json:"mode"`
	ModTime   string `json:"mod_time"`
}

func main() {
	skillmain.Main("fs/ls", run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Apply defaults
	if strings.TrimSpace(in.Path) == "" {
		in.Path = "."
	}
	if in.MaxEntries <= 0 {
		in.MaxEntries = 200
	}

	validDir, err := rc.PathValidator.ValidatePath(in.Path)
	if err != nil {
		return fmt.Errorf("path validation failed: %w", err)
	}

	allEntries, err := readDir(validDir, in)
	if err != nil {
		return err
	}

	totalEntries := len(allEntries)
	limitedEntries, limited := limitEntries(allEntries, in.MaxEntries)

	preview, previewTruncated := skillout.PreparePreview(limitedEntries, rc.MaxPreview)

	// Persist artifact if truncated
	var artifactDigest string
	if previewTruncated {
		artifact, err := skillout.PersistJSON(ctx, rc, limitedEntries, "fs_ls")
		if err != nil {
			return err
		}
		artifactDigest = artifact.Digest
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
		"preview":     preview,
	}
	if limited {
		data["truncated"] = true
		data["limited_entries"] = len(limitedEntries)
	}
	if artifactDigest != "" {
		data["artifact"] = artifactDigest
	}

	return skillout.Emit(rc, "fs/ls", data)
}

func limitEntries(entries []entry, limit int) ([]entry, bool) {
	if limit <= 0 || len(entries) <= limit {
		return entries, false
	}
	clipped := make([]entry, limit)
	copy(clipped, entries[:limit])
	return clipped, true
}

func readDir(path string, in Input) ([]entry, error) {
	ents, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", path, err)
	}
	var out []entry
	for _, e := range ents {
		name := e.Name()
		if !in.ShowHidden && strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(path, name)
		if len(in.Include) > 0 && !matches(full, in.Include) {
			continue
		}
		if matches(full, append([]string{".git", "node_modules"}, in.Exclude...)) {
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

func fileSize(info fs.FileInfo) int64 {
	if info.IsDir() {
		return 0
	}
	return info.Size()
}

func matches(path string, globs []string) bool {
	if len(globs) == 0 {
		return false
	}
	for _, g := range globs {
		if ok, err := filepath.Match(g, filepath.Base(path)); err == nil && ok {
			return true
		}
		if ok, err := filepath.Match(g, filepath.ToSlash(path)); err == nil && ok {
			return true
		}
	}
	return false
}
