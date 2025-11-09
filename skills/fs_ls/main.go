// Package main implements the fs/ls skill.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
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
	defer func() { _ = rc.Close() }()

	var in input
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fail("fs/ls", "EARG", fmt.Errorf("decode input: %w", err))
	}
	if err := run(ctx, rc, in); err != nil {
		fail("fs/ls", "ERUNTIME", err)
	}
}

func run(ctx context.Context, rc *skillslib.RunnerContext, in input) error {
	dir := in.Path
	if dir == "" {
		dir = "."
	}
	if in.MaxEntries <= 0 {
		in.MaxEntries = 200
	}

	entries, err := readDir(dir, in)
	if err != nil {
		return err
	}

	total := len(entries)
	preview := entries
	if len(preview) > rc.MaxPreview {
		preview = preview[:rc.MaxPreview]
	}

	var artifact string
	var artifactSize int64
	if len(entries) > rc.MaxPreview {
		digest, size, err := storeEntries(ctx, rc, entries)
		if err != nil {
			return err
		}
		artifact = digest
		artifactSize = size
	}

	files := 0
	dirs := 0
	var totalSize int64
	for _, e := range entries {
		if e.IsDir {
			dirs++
		} else {
			files++
			totalSize += e.SizeBytes
		}
	}

	data := map[string]any{
		"path":        dir,
		"entry_count": total,
		"files":       files,
		"directories": dirs,
		"total_size":  totalSize,
		"preview":     preview,
	}
	if artifact != "" {
		data["artifact"] = artifact
		data["artifact_kind"] = "application/json"
		data["artifact_size_bytes"] = artifactSize
	}

	return rc.Emit("fs/ls", data, "application/json", envelope.Meta{
		Source: "run",
		Runner: "exec",
	})
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
		if ok, _ := filepath.Match(g, filepath.Base(path)); ok {
			return true
		}
		if ok, _ := filepath.Match(g, filepath.ToSlash(path)); ok {
			return true
		}
	}
	return false
}

func storeEntries(ctx context.Context, rc *skillslib.RunnerContext, entries []entry) (string, int64, error) {
	buf, err := json.Marshal(entries)
	if err != nil {
		return "", 0, fmt.Errorf("marshal entries: %w", err)
	}
	obj, err := rc.CASStore.Put(ctx, bytes.NewReader(buf), "application/json", []string{"fs_ls"})
	if err != nil {
		return "", 0, fmt.Errorf("cas put: %w", err)
	}
	return obj.Digest, obj.Size, nil
}

func fail(command, code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	_ = envelope.Write(os.Stdout, env)
	os.Exit(1)
}
