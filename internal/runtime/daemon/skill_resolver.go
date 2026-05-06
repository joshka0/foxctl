package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/buildinfo"
	"github.com/joshka0/foxctl/internal/platform/config"
)

// SkillHandle captures manifest and artifact metadata required for execution.
type SkillHandle struct {
	Manifest     skill.Manifest
	ManifestPath string
	ArtifactPath string
}

// SkillResolver resolves skill names to executable handles.
type SkillResolver struct {
	searchPaths []string
}

// NewSkillResolver creates a skill resolver with paths from config.
//
// Index:
//   Purpose: Build a skill resolver with configured search paths
//   Keywords: skill_resolver, search_paths, skill.yaml, builtin_paths
//   Related: SkillResolver.Resolve, skill.NormalizeSearchPaths
//   Flow: gather env/user/builtin paths → normalize → return resolver
//   Resources: config.Config, environment variables
//   Events: none
//   OutputFields: *SkillResolver
func NewSkillResolver(cfg config.Config) *SkillResolver {
	searchPaths := append([]string{}, skill.EnvSearchPaths()...)
	if cfg.Paths.Skills != "" {
		searchPaths = append(searchPaths, cfg.Paths.Skills)
	}
	searchPaths = append(searchPaths, skill.UserSearchPaths()...)
	searchPaths = append(searchPaths, skill.BuiltinSearchPaths()...)
	searchPaths = append(searchPaths, skill.DevSearchPaths()...)

	return &SkillResolver{
		searchPaths: skill.NormalizeSearchPaths(searchPaths),
	}
}

// Resolve finds a skill by name and returns an executable handle.
//
// Index:
//   Purpose: Resolve a skill name to manifest and artifact paths
//   Keywords: skill_resolve, manifest_path, artifact_path, skill_name
//   Related: loadSkillDir, resolveAlternate
//   Flow: resolve via resolver → load skill dir → fallback to alternate names
//   Resources: filesystem, skill.Resolver
//   Events: none
//   OutputFields: *SkillHandle
//
// [[domain:skill-resolution]]
func (r *SkillResolver) Resolve(skillName string) (*SkillHandle, error) {
	if skillName == "" {
		return nil, fmt.Errorf("skill name is required")
	}

	// Use the skill package resolver
	resolver := skill.NewResolver(skill.WithSearchPaths(r.searchPaths...))

	// Try to resolve the skill
	handle, err := resolver.Resolve(skillName)
	if err == nil {
		dir := filepath.Dir(handle.ManifestPath)
		result, loadErr := loadSkillDir(dir)
		if loadErr == nil {
			return result, nil
		}
		// Try alternate path normalization
		if result, ok := r.resolveAlternate(resolver, skillName, handle); ok {
			return result, nil
		}
		return nil, loadErr
	}

	// Try alternate names (underscore normalization)
	if result, ok := r.resolveAlternate(resolver, skillName, skill.Handle{}); ok {
		return result, nil
	}

	return nil, fmt.Errorf("skill %s not found in search paths: %v", skillName, r.searchPaths)
}

// resolveAlternate tries alternate skill name normalizations.
//
// Index:
//   Purpose: Resolve skill via alternate normalized names
//   Keywords: skill_resolve, normalize, search_paths, skill.yaml
//   Related: loadSkillDir, normalizeSkillCandidate
//   Flow: build candidates → scan search paths → load skill dir
//   Resources: filesystem, search paths
//   Events: none
//   OutputFields: *SkillHandle, bool
func (r *SkillResolver) resolveAlternate(resolver *skill.Resolver, requested string, failed skill.Handle) (*SkillHandle, bool) {
	candidates := []string{requested}
	if norm := normalizeSkillCandidate(requested); norm != requested {
		candidates = append(candidates, norm)
	}

	skipDir := ""
	if failed.ManifestPath != "" {
		skipDir = filepath.Clean(filepath.Dir(failed.ManifestPath))
	}

	for _, base := range r.searchPaths {
		if base == "" {
			continue
		}
		base = filepath.Clean(base)

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
		}
	}
	return nil, false
}

// loadSkillDir loads a skill from a directory containing skill.yaml.
//
// Index:
//   Purpose: Load manifest and artifact paths from a skill directory
//   Keywords: skill.yaml, manifest, artifact, cgo, load_manifest
//   Related: skill.LoadManifestAndArtifactFromDir
//   Flow: validate skill.yaml → load manifest/artifact → return handle
//   Resources: filesystem, skill package
//   Events: none
//   OutputFields: *SkillHandle
func loadSkillDir(dir string) (*SkillHandle, error) {
	manifestPath := filepath.Join(dir, "skill.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		return nil, err
	}

	manifest, artifact, err := skill.LoadManifestAndArtifactFromDir(dir, skill.ArtifactOptions{
		PreferCGO: buildinfo.IsCGO(),
	})
	if err != nil {
		if errors.Is(err, skill.ErrArtifactsMissing) {
			return nil, fmt.Errorf("skill artifacts missing under %s; run 'make skills-build' to compile skills", dir)
		}
		return nil, err
	}

	return &SkillHandle{
		Manifest:     manifest,
		ManifestPath: manifestPath,
		ArtifactPath: artifact,
	}, nil
}

// normalizeSkillCandidate normalizes skill names for alternate lookups.
func normalizeSkillCandidate(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	return strings.ReplaceAll(name, "-", "_")
}
