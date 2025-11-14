// Package main implements the fs/ls skill.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
	errs "github.com/jkatigb/agentctl/internal/errors"
	"github.com/jkatigb/agentctl/internal/skillslib"
)

type input struct {
	Path       string   `json:"path"`
	Include    []string `json:"include"`
	Exclude    []string `json:"exclude"`
	MaxEntries int      `json:"max_entries"`
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
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("fs/ls", "ECONFIG", err)
	}
	rc, err := skillslib.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("fs/ls", "ERUNTIME", err)
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("fs/ls", "EARG", err)
	}
	if err := run(ctx, rc, in); err != nil {
		fail("fs/ls", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *skillslib.RunnerContext, in input) error {
	validDir, err := resolveWorkspace(rc, in.Path)
	if err != nil {
		return err
	}

	allEntries, err := readDir(validDir, in)
	if err != nil {
		return err
	}

	totalEntries := len(allEntries)
	limitedEntries, limited := limitEntries(allEntries, in.MaxEntries)

	preview, previewTruncated := preparePreview(limitedEntries, rc.MaxPreview)
	artifact, err := persistListingArtifact(ctx, rc, limitedEntries, previewTruncated)
	if err != nil {
		return err
	}

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
	if artifact.Digest != "" {
		data["artifact"] = artifact.Digest
		data["artifact_kind"] = artifact.Kind
		data["artifact_size_bytes"] = artifact.Size
	}

	return rc.Emit("fs/ls", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
}

func parseInput(r io.Reader) (input, error) {
	var in input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return input{}, fmt.Errorf("decode input: %w", err)
	}
	if strings.TrimSpace(in.Path) == "" {
		in.Path = "."
	}
	if in.MaxEntries <= 0 {
		in.MaxEntries = 200
	}
	return in, nil
}

func resolveWorkspace(rc *skillslib.RunnerContext, path string) (string, error) {
	valid, err := rc.PathValidator.ValidatePath(path)
	if err != nil {
		return "", fmt.Errorf("path validation failed: %w", err)
	}
	return valid, nil
}

func preparePreview(entries []entry, max int) ([]entry, bool) {
	preview, truncated := skillslib.PreparePreview(entries, max)
	if truncated {
		dup := make([]entry, len(preview))
		copy(dup, preview)
		preview = dup
	}
	return preview, truncated
}

func persistListingArtifact(ctx context.Context, rc *skillslib.RunnerContext, entries []entry, truncated bool) (skillslib.Artifact, error) {
	if !truncated {
		return skillslib.Artifact{}, nil
	}
	artifact, err := skillslib.PersistJSON(ctx, rc, entries, "fs_ls")
	if err != nil {
		return skillslib.Artifact{}, err
	}
	return artifact, nil
}

func limitEntries(entries []entry, max int) ([]entry, bool) {
	if max <= 0 || len(entries) <= max {
		return entries, false
	}
	clipped := make([]entry, max)
	copy(clipped, entries[:max])
	return clipped, true
}

func readDir(path string, in input) ([]entry, error) {
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

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit fs/ls failure")
	os.Exit(1)
}
