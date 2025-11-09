// Package skillslib provides utilities for skill binaries (envelopes, CAS, fs walkers).
package skillslib

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/cas"
	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/envelope"
)

// RunnerContext bundles dependencies skill binaries need.
type RunnerContext struct {
	Config     config.Config
	CASStore   *cas.Store
	InlineKB   int
	Stdout     io.Writer
	Now        func() time.Time
	MaxPreview int
}

// NewRunnerContext initializes a RunnerContext from configuration.
func NewRunnerContext(cfg config.Config, stdout io.Writer) (*RunnerContext, error) {
	store, err := cas.NewStore(cfg.Paths.CAS)
	if err != nil {
		return nil, fmt.Errorf("skills: cas store: %w", err)
	}
	return &RunnerContext{
		Config:     cfg,
		CASStore:   store,
		InlineKB:   cfg.InlineOutputKB,
		Stdout:     stdout,
		Now:        time.Now,
		MaxPreview: 5,
	}, nil
}

// Close releases resources held by the runner context.
func (rc *RunnerContext) Close() error {
	// CAS store currently has no Close; placeholder for future resources.
	return nil
}

// Emit OK envelope with automatic CAS wrapping for large payloads.
func (rc *RunnerContext) Emit(command string, data any, _ string, meta envelope.Meta) error {
	if meta.CASDigest == "" {
		if digest := artifactDigest(data); digest != "" {
			meta.CASDigest = digest
		}
	}
	env := envelope.OK(command, data, envelope.WithMeta(meta))
	return envelope.Write(rc.Stdout, env)
}

// ListOptions configures how filesystem entries are discovered.
type ListOptions struct {
	BasePath string
	Include  []string
	Exclude  []string
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
				rel, _ := filepath.Rel(baseAbs, path)
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
		ok, _ := filepath.Match(g, filepath.Base(path))
		if ok {
			return true
		}
		ok, _ = filepath.Match(g, rel)
		if ok {
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

func artifactDigest(data any) string {
	if data == nil {
		return ""
	}
	switch v := data.(type) {
	case map[string]any:
		return extractArtifact(v)
	case map[string]interface{}:
		return extractArtifact(v)
	default:
		val := reflect.ValueOf(data)
		if val.Kind() == reflect.Map && val.Type().Key().Kind() == reflect.String {
			iter := val.MapRange()
			for iter.Next() {
				if key := iter.Key().String(); key == "artifact" {
					if s, ok := iter.Value().Interface().(string); ok && strings.HasPrefix(s, "sha256:") {
						return s
					}
				}
			}
		}
	}
	return ""
}

func extractArtifact(m map[string]any) string {
	val, ok := m["artifact"]
	if !ok {
		return ""
	}
	if digest, ok := val.(string); ok && strings.HasPrefix(digest, "sha256:") {
		return digest
	}
	return ""
}
