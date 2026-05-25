package hooks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/buildinfo"
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
	// SkillsDir is the root directory for installed skills (e.g., ~/.foxctl/skills).
	SkillsDir string
}

// NewDefaultResolver creates a resolver using the given skills directory.
// NewDefaultResolver creates a resolver rooted at a skills directory.
//
// Index:
//
//	Purpose: Build a resolver for installed hook skills
//	Keywords: skill_resolver, skills_dir, skill.yaml, artifact
//	Related: DefaultResolver.Resolve
//	Flow: capture skillsDir → return resolver
//	Resources: filesystem skills directory
//	Events: none
//	OutputFields: *DefaultResolver
func NewDefaultResolver(skillsDir string) *DefaultResolver {
	return &DefaultResolver{SkillsDir: skillsDir}
}

// Resolve finds a skill's manifest and binary path.
func (r *DefaultResolver) Resolve(skillName string) (skill.Manifest, string, error) {
	skillName = strings.TrimSpace(skillName)
	if err := validateResolverSkillName(skillName); err != nil {
		return skill.Manifest{}, "", err
	}

	absSkillsDir, err := filepath.Abs(r.SkillsDir)
	if err != nil {
		return skill.Manifest{}, "", fmt.Errorf("resolve skills dir: %w", err)
	}

	paths := []string{filepath.Join(r.SkillsDir, filepath.FromSlash(skillName))}
	if normalized := skill.NormalizeSkillName(skillName); normalized != skillName {
		paths = append(paths, filepath.Join(r.SkillsDir, filepath.FromSlash(normalized)))
	}

	for _, skillPath := range paths {
		if !isWithinResolverRoot(skillPath, absSkillsDir) {
			continue
		}

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

func validateResolverSkillName(skillName string) error {
	if skillName == "" {
		return fmt.Errorf("skill name is required")
	}
	if filepath.IsAbs(skillName) {
		return fmt.Errorf("invalid skill name %q: absolute paths are not allowed", skillName)
	}
	for _, part := range strings.FieldsFunc(skillName, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if part == "." || part == ".." {
			return fmt.Errorf("invalid skill name %q: path traversal is not allowed", skillName)
		}
	}
	return nil
}

func isWithinResolverRoot(path, absRoot string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if resolvedRoot, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolvedRoot
	}
	if resolvedPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolvedPath
	}
	rel, err := filepath.Rel(absRoot, absPath)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
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
//
//	Purpose: Compose multiple resolvers for fallback resolution
//	Keywords: chain_resolver, skill_resolver, fallback, resolve
//	Related: ChainResolver.Resolve
//	Flow: collect resolvers → return chain
//	Resources: []SkillResolver
//	Events: none
//	OutputFields: *ChainResolver
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
