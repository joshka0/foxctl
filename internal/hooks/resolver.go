package hooks

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/domain/skill"
)

// SkillResolver finds skill manifests and artifact paths.
type SkillResolver interface {
	// Resolve finds the manifest and artifact path for a skill by name.
	// skillName is typically in format "category/name" (e.g., "hooks/task_guard").
	Resolve(skillName string) (skill.Manifest, string, error)
}

// DefaultResolver implements SkillResolver using the standard skills directory.
type DefaultResolver struct {
	// SkillsDir is the root directory for installed skills (e.g., ~/.agentctl/skills).
	SkillsDir string
}

// NewDefaultResolver creates a resolver using the given skills directory.
func NewDefaultResolver(skillsDir string) *DefaultResolver {
	return &DefaultResolver{SkillsDir: skillsDir}
}

// Resolve finds a skill's manifest and binary path.
func (r *DefaultResolver) Resolve(skillName string) (skill.Manifest, string, error) {
	// Convert skill name to path: "hooks/task_guard" → "hooks/task_guard"
	skillPath := filepath.Join(r.SkillsDir, skillName)

	// Load manifest
	manifestPath := filepath.Join(skillPath, "skill.yaml")
	manifest, err := skill.LoadManifest(manifestPath)
	if err != nil {
		return skill.Manifest{}, "", fmt.Errorf("load manifest: %w", err)
	}

	// Find artifact
	artifactPath := findArtifact(skillPath)
	if artifactPath == "" {
		return skill.Manifest{}, "", fmt.Errorf("no artifact found in %s", skillPath)
	}

	return manifest, artifactPath, nil
}

// findArtifact locates the skill binary in the standard locations.
func findArtifact(skillPath string) string {
	// Candidates in priority order:
	// 1. bin-cgo - CGO build (has more capabilities)
	// 2. bin - Pure Go build
	candidates := []string{"bin-cgo", "bin"}

	for _, name := range candidates {
		p := filepath.Join(skillPath, name)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return p
		}
	}

	return ""
}

// ResolverFunc is a function adapter for SkillResolver.
type ResolverFunc func(skillName string) (skill.Manifest, string, error)

// Resolve implements SkillResolver.
func (f ResolverFunc) Resolve(skillName string) (skill.Manifest, string, error) {
	return f(skillName)
}

// ChainResolver tries multiple resolvers in order until one succeeds.
type ChainResolver struct {
	Resolvers []SkillResolver
}

// NewChainResolver creates a resolver that tries each resolver in order.
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
