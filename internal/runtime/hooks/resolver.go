package hooks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
)

// SkillResolver finds skill manifests and artifact paths.
type SkillResolver interface {
	// Resolve finds the manifest and artifact path for a skill by name.
	// skillName is typically in format "category/name" (e.g., "hooks/task_guard").
	Resolve(skillName string) (skill.Manifest, string, error)
}

// DefaultResolver implements SkillResolver using the standard skills directory.
// DefaultResolver resolves skills from a local skills directory.
type DefaultResolver struct {
	// SkillsDir is the root directory for installed skills (e.g., ~/.agentctl/skills).
	SkillsDir string
}

// NewDefaultResolver creates a resolver using the given skills directory.
// NewDefaultResolver creates a resolver rooted at a skills directory.
//
// Index:
// - Purpose: Build a resolver for installed hook skills
// - Flow: capture skillsDir → return resolver
// - Related: DefaultResolver.Resolve
// - Keywords: skill_resolver, skills_dir, skill.yaml, artifact
func NewDefaultResolver(skillsDir string) *DefaultResolver {
	return &DefaultResolver{SkillsDir: skillsDir}
}

// Resolve finds a skill's manifest and binary path.
func (r *DefaultResolver) Resolve(skillName string) (skill.Manifest, string, error) {
	paths := []string{filepath.Join(r.SkillsDir, skillName)}
	if normalized := skill.NormalizeSkillName(skillName); normalized != skillName {
		paths = append(paths, filepath.Join(r.SkillsDir, normalized))
	}

	for _, skillPath := range paths {
		manifestPath := filepath.Join(skillPath, "skill.yaml")
		if _, err := os.Stat(manifestPath); err != nil {
			continue
		}

		manifest, artifactPath, err := skill.LoadManifestAndArtifactFromDir(skillPath, skill.ArtifactOptions{
			PreferCGO: buildinfo.IsCGO(),
		})
		if err != nil {
			if errors.Is(err, skill.ErrArtifactsMissing) {
				return skill.Manifest{}, "", fmt.Errorf("no artifact found in %s", skillPath)
			}
			return skill.Manifest{}, "", err
		}

		return manifest, artifactPath, nil
	}

	return skill.Manifest{}, "", fmt.Errorf("no artifact found for %s", skillName)
}

// ResolverFunc is a function adapter for SkillResolver.
type ResolverFunc func(skillName string) (skill.Manifest, string, error)

// Resolve implements SkillResolver.
func (f ResolverFunc) Resolve(skillName string) (skill.Manifest, string, error) {
	return f(skillName)
}

// ChainResolver tries multiple resolvers in order until one succeeds.
// ChainResolver tries multiple resolvers in order.
type ChainResolver struct {
	Resolvers []SkillResolver
}

// NewChainResolver creates a resolver that tries each resolver in order.
// NewChainResolver builds a resolver that tries each resolver in order.
//
// Index:
// - Purpose: Compose multiple resolvers for fallback resolution
// - Flow: collect resolvers → return chain
// - Related: ChainResolver.Resolve
// - Keywords: chain_resolver, skill_resolver, fallback, resolve
func NewChainResolver(resolvers ...SkillResolver) *ChainResolver {
	return &ChainResolver{Resolvers: resolvers}
}

// Resolve tries each resolver until one succeeds.
func (r *ChainResolver) Resolve(skillName string) (skill.Manifest, string, error) {
	var lastErr error
	for _, resolver := range r.Resolvers {
		manifest, path, err := resolver.Resolve(skillName)
		if err == nil {
			return manifest, path, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return skill.Manifest{}, "", lastErr
	}
	return skill.Manifest{}, "", fmt.Errorf("no resolver found for skill %s", skillName)
}
