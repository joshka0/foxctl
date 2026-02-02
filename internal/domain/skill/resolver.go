package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Resolver finds skills by name or path.
type Resolver struct {
	searchPaths []string
}

// ResolverOption configures a Resolver.
type ResolverOption func(*Resolver)

// NewResolver creates a new skill resolver with default search paths.
func NewResolver(opts ...ResolverOption) *Resolver {
	r := &Resolver{
		searchPaths: DefaultSearchPaths(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// WithSearchPaths sets custom search paths, replacing the defaults.
func WithSearchPaths(paths ...string) ResolverOption {
	return func(r *Resolver) {
		r.searchPaths = NormalizeSearchPaths(paths)
	}
}

// WithAdditionalPaths adds paths to the default search paths.
func WithAdditionalPaths(paths ...string) ResolverOption {
	return func(r *Resolver) {
		r.searchPaths = NormalizeSearchPaths(append(r.searchPaths, paths...))
	}
}

// Handle represents a resolved skill location.
type Handle struct {
	Name         string // Skill name
	ManifestPath string // Absolute path to skill.yaml
	ArtifactPath string // Directory containing skill artifact
	Source       string // Where it was found (path, builtin, etc.)
}

// Resolve finds a skill by name or path.
// It first checks if the input is a filesystem path, then searches
// configured search paths.
func (r *Resolver) Resolve(nameOrPath string) (Handle, error) {
	// 1. Check if it's a filesystem path
	if r.isPath(nameOrPath) {
		return r.resolveFromPath(nameOrPath)
	}

	// 2. Search in configured paths
	return r.resolveFromSearchPaths(nameOrPath)
}

// List returns all discoverable skills across all search paths.
// If a skill appears in multiple paths, only the first occurrence is returned.
func (r *Resolver) List() ([]Handle, error) {
	var handles []Handle
	seen := make(map[string]bool)

	for _, basePath := range r.searchPaths {
		entries, err := os.ReadDir(basePath)
		if err != nil {
			continue // Path might not exist, skip
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			name := entry.Name()
			if seen[name] {
				continue // Already found in earlier path
			}

			manifestPath := filepath.Join(basePath, name, "skill.yaml")
			if _, err := os.Stat(manifestPath); err == nil {
				handles = append(handles, Handle{
					Name:         name,
					ManifestPath: manifestPath,
					ArtifactPath: filepath.Dir(manifestPath),
					Source:       basePath,
				})
				seen[name] = true
			}
		}
	}

	return handles, nil
}

// SearchPaths returns the configured search paths.
func (r *Resolver) SearchPaths() []string {
	return append([]string{}, r.searchPaths...)
}

// isPath checks if the name looks like a filesystem path.
// This distinguishes between skill names (like "text/grep") and actual paths.
func (r *Resolver) isPath(name string) bool {
	// Absolute paths are always paths
	if filepath.IsAbs(name) {
		return true
	}

	// Check for path indicators like ./ or ../ or .\
	if strings.HasPrefix(name, "./") || strings.HasPrefix(name, "../") ||
		strings.HasPrefix(name, ".\\") || strings.HasPrefix(name, "..\\") {
		return true
	}

	// If it contains a slash with more than one component beyond a simple namespace,
	// or if it contains backslashes, treat it as a path
	if strings.Contains(name, "\\") {
		return true
	}

	// Count the number of slashes - skill names typically have at most one (for namespace)
	// More than one slash suggests a path
	slashCount := strings.Count(name, "/")
	if slashCount > 1 {
		return true
	}

	// Single slash could be either a namespaced skill name or a relative path
	// If it looks like a path component (e.g., contains .), treat as path
	if slashCount == 1 && (strings.Contains(name, ".yaml") || strings.Contains(name, ".")) {
		return true
	}

	return false
}

// resolveFromPath resolves a skill from an explicit path.
func (r *Resolver) resolveFromPath(path string) (Handle, error) {
	manifestPath := path
	if filepath.Base(path) != "skill.yaml" {
		manifestPath = filepath.Join(path, "skill.yaml")
	}

	absPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return Handle{}, fmt.Errorf("absolute path: %w", err)
	}

	if _, err := os.Stat(absPath); err != nil {
		return Handle{}, fmt.Errorf("skill manifest not found: %s", absPath)
	}

	return Handle{
		Name:         filepath.Base(filepath.Dir(absPath)),
		ManifestPath: absPath,
		ArtifactPath: filepath.Dir(absPath),
		Source:       "path",
	}, nil
}

// resolveFromSearchPaths searches configured paths for a skill.
func (r *Resolver) resolveFromSearchPaths(name string) (Handle, error) {
	// Normalize the skill name (replace / and - with _)
	normalizedName := normalizeSkillName(name)

	for _, basePath := range r.searchPaths {
		// Try both the original name and the normalized name
		candidates := []string{normalizedName, name}

		for _, candidate := range candidates {
			manifestPath := filepath.Join(basePath, candidate, "skill.yaml")

			if _, err := os.Stat(manifestPath); err == nil {
				return Handle{
					Name:         name,
					ManifestPath: manifestPath,
					ArtifactPath: filepath.Dir(manifestPath),
					Source:       basePath,
				}, nil
			}
		}
	}

	return Handle{}, fmt.Errorf("skill not found: %s (searched: %v)", name, r.searchPaths)
}

// normalizeSkillName is an alias for NormalizeSkillName for internal use.
func normalizeSkillName(name string) string {
	return NormalizeSkillName(name)
}
