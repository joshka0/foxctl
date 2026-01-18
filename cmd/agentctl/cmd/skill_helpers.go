package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/execution/runner"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/runservice"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/spf13/cobra"
)

// SkillHandle captures manifest and artifact path for execution.
type SkillHandle = runservice.SkillHandle

func loadSkillInput(cmd *cobra.Command, cfg config.Config, inline, file string) ([]byte, error) {
	trimmed := strings.TrimSpace(inline)
	switch {
	case file == "-":
		return io.ReadAll(cmd.InOrStdin())
	case file != "":
		return os.ReadFile(file)
	case strings.EqualFold(trimmed, "stdin"):
		data, err := extractEnvelopeData(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("read stdin envelope: %w", err)
		}
		return data, nil
	case strings.HasPrefix(trimmed, "sha256:"):
		store, err := cas.NewStore(cfg.Paths.CAS)
		if err != nil {
			return nil, fmt.Errorf("open cas store: %w", err)
		}
		defer func() { _ = store.Close() }()
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		rc, _, err := store.Get(ctx, trimmed)
		if err != nil {
			return nil, fmt.Errorf("read cas %s: %w", trimmed, err)
		}
		data, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read cas %s: %w", trimmed, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("verify cas %s: %w", trimmed, closeErr)
		}
		return data, nil
	case trimmed != "":
		return []byte(inline), nil
	default:
		return []byte("{}"), nil
	}
}

func extractEnvelopeData(r io.Reader) ([]byte, error) {
	dec := json.NewDecoder(r)
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := dec.Decode(&env); err != nil {
		return nil, err
	}
	if len(env.Data) == 0 {
		return []byte("null"), nil
	}
	trimmed := bytes.TrimSpace(env.Data)
	if len(trimmed) == 0 {
		return []byte("null"), nil
	}
	return trimmed, nil
}

func findSkill(cfg config.Config, requested string) (SkillHandle, error) {
	// Use the Resolver to find the skill
	resolver := createSkillResolver(cfg)
	var firstErr error
	var allowReinstall bool
	handle, err := resolver.Resolve(requested)
	if err == nil {
		if result, ok := loadResolvedSkill(handle, cfg.Paths.Skills, &firstErr, &allowReinstall); ok {
			return result, nil
		}
		if result, ok := resolveAlternateSkill(resolver, requested, handle, &firstErr); ok {
			return result, nil
		}
	}

	// Not found via resolver, try to install embedded skill
	if installed, err := installEmbeddedSkill(cfg, requested, allowReinstall); err == nil {
		if installed {
			return findSkill(cfg, requested)
		}
	} else if !errors.Is(err, errUnknownEmbeddedSkill) {
		return SkillHandle{}, err
	}
	if firstErr != nil {
		return SkillHandle{}, firstErr
	}
	return SkillHandle{}, fmt.Errorf("skill %s not found; run make skills-build or agentctl skills install", requested)
}

// createSkillResolver creates a resolver with paths from config.
func createSkillResolver(cfg config.Config) *skill.Resolver {
	searchPaths := append([]string{}, skill.EnvSearchPaths()...)
	if cfg.Paths.Skills != "" {
		searchPaths = append(searchPaths, cfg.Paths.Skills)
	}
	searchPaths = append(searchPaths, skill.UserSearchPaths()...)
	searchPaths = append(searchPaths, skill.BuiltinSearchPaths()...)
	searchPaths = append(searchPaths, skill.DevSearchPaths()...)
	return skill.NewResolver(skill.WithSearchPaths(skill.NormalizeSearchPaths(searchPaths)...))
}

func loadSkillDir(dir string) (SkillHandle, error) {
	manifestPath := filepath.Join(dir, "skill.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		return SkillHandle{}, err
	}
	manifest, err := skill.LoadManifest(manifestPath)
	if err != nil {
		return SkillHandle{}, err
	}
	if err := skill.ValidateWASIPolicy(manifest); err != nil {
		return SkillHandle{}, err
	}
	artifact, err := skill.ResolveArtifactPath(dir, manifest, skill.ArtifactOptions{
		PreferCGO: buildinfo.IsCGO(),
	})
	if err != nil {
		if errors.Is(err, skill.ErrArtifactsMissing) {
			return SkillHandle{}, fmt.Errorf("skill artifacts missing under %s; run 'make skills-build' to compile skills", dir)
		}
		return SkillHandle{}, err
	}
	return SkillHandle{
		Manifest:     manifest,
		ManifestPath: manifestPath,
		ArtifactPath: artifact,
	}, nil
}

func executeSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, []byte, error) {
	return runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        input,
	})
}

func loadResolvedSkill(handle skill.Handle, skillsRoot string, firstErr *error, allowReinstall *bool) (SkillHandle, bool) {
	dir := filepath.Dir(handle.ManifestPath)
	result, err := loadSkillDir(dir)
	if err == nil {
		return result, true
	}
	if allowReinstall != nil && skillsRoot != "" && withinDir(skillsRoot, dir) {
		*allowReinstall = true
	}
	if firstErr != nil && *firstErr == nil {
		*firstErr = err
	}
	return SkillHandle{}, false
}

func resolveAlternateSkill(resolver *skill.Resolver, requested string, failed skill.Handle, firstErr *error) (SkillHandle, bool) {
	searchPaths := resolver.SearchPaths()
	candidates := []string{requested}
	if norm := normalizeSkillCandidate(requested); norm != requested {
		candidates = append(candidates, norm)
	}
	skipSource := filepath.Clean(failed.Source)
	skipDir := filepath.Clean(filepath.Dir(failed.ManifestPath))
	for _, base := range searchPaths {
		if base == "" {
			continue
		}
		base = filepath.Clean(base)
		if skipSource != "" && base == skipSource {
			continue
		}
		for _, candidate := range candidates {
			dir := filepath.Join(base, filepath.FromSlash(candidate))
			if skipDir != "" && filepath.Clean(dir) == skipDir {
				continue
			}
			if _, err := os.Stat(filepath.Join(dir, "skill.yaml")); err != nil {
				continue
			}
			result, err := loadSkillDir(dir)
			if err == nil {
				return result, true
			}
			if firstErr != nil && *firstErr == nil {
				*firstErr = err
			}
		}
	}
	return SkillHandle{}, false
}

func normalizeSkillCandidate(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	return strings.ReplaceAll(name, "-", "_")
}

func withinDir(root, target string) bool {
	root = filepath.Clean(root)
	target = filepath.Clean(target)
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
}
