// Package daemon provides skill resolution for the daemon service.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

// SkillHandle captures manifest and artifact metadata required for execution.
type SkillHandle struct {
	Manifest     skill.Manifest
	ManifestPath string
	ArtifactPath string
}

// SkillResolver resolves skill names to executable handles.
type SkillResolver struct {
	cfg         config.Config
	searchPaths []string
}

// NewSkillResolver creates a skill resolver with paths from config.
func NewSkillResolver(cfg config.Config) *SkillResolver {
	var searchPaths []string

	// Environment override takes highest precedence and supports list format.
	if env := os.Getenv("AGENTCTL_SKILLS_PATH"); env != "" {
		searchPaths = append(searchPaths, filepath.SplitList(env)...)
	}

	// Configured skills path (defaults to ~/.agentctl/skills).
	searchPaths = append(searchPaths, cfg.Paths.Skills)

	// Development paths near the current working directory.
	if pwd, err := os.Getwd(); err == nil {
		searchPaths = append(searchPaths,
			filepath.Join(pwd, "dist", "skills"),
			filepath.Join(pwd, "skills"),
		)
	}

	return &SkillResolver{
		cfg:         cfg,
		searchPaths: dedupeCleanPaths(searchPaths),
	}
}

// Resolve finds a skill by name and returns an executable handle.
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
func loadSkillDir(dir string) (*SkillHandle, error) {
	manifestPath := filepath.Join(dir, "skill.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		return nil, err
	}

	manifest, err := skill.LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	if err := policy.ValidateWASIPolicy(manifest); err != nil {
		return nil, err
	}

	var artifact string
	switch manifest.Distribution.Type {
	case "exec":
		// Check candidates in priority order:
		// 1. CGO binary (bin-cgo) if running CGO build - for Turso/native features
		// 2. Standard binary (bin)
		// 3. Source-tree skill directory itself (skills/<name>/ with main.go)
		var candidates []string
		if buildinfo.IsCGO() {
			candidates = append(candidates, filepath.Join(dir, "bin-cgo"))
		}
		candidates = append(candidates, filepath.Join(dir, "bin"))
		// For source-tree skills, check if main.go exists (Go skill)
		if _, err := os.Stat(filepath.Join(dir, "main.go")); err == nil {
			candidates = append(candidates, dir)
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				artifact = c
				break
			}
		}
	case "wasi":
		candidates := []string{
			filepath.Join(dir, "module.wasm"),
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				artifact = c
				break
			}
		}
	default:
		return nil, fmt.Errorf("unsupported distribution %q", manifest.Distribution.Type)
	}

	if artifact == "" {
		return nil, fmt.Errorf("skill artifacts missing under %s; run 'make skills-build' to compile skills", dir)
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

// dedupeCleanPaths removes duplicates and cleans paths.
func dedupeCleanPaths(paths []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, p := range paths {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		cleaned := filepath.Clean(trimmed)
		if cleaned == "" || cleaned == "." {
			continue
		}
		if _, ok := seen[cleaned]; ok {
			continue
		}
		seen[cleaned] = struct{}{}
		out = append(out, cleaned)
	}
	return out
}
