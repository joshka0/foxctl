package skill

import (
	"os"
	"path/filepath"
	"strings"
)

// EnvSearchPaths returns skill search paths from FOXCTL_SKILLS_PATH.
func EnvSearchPaths() []string {
	if skillsPath := os.Getenv("FOXCTL_SKILLS_PATH"); skillsPath != "" {
		return filepath.SplitList(skillsPath)
	}
	return nil
}

// UserSearchPaths returns the default user skills directory.
func UserSearchPaths() []string {
	if homeDir, err := os.UserHomeDir(); err == nil {
		return []string{filepath.Join(homeDir, ".foxctl", "skills")}
	}
	return nil
}

// BuiltinSearchPaths returns the built-in skills directory relative to the executable.
func BuiltinSearchPaths() []string {
	if exePath, err := os.Executable(); err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(exePath); resolveErr == nil {
			exePath = resolved
		}
		exeDir := filepath.Dir(exePath)
		paths := []string{filepath.Join(exeDir, "skills")}
		if filepath.Base(exeDir) == "bin" {
			paths = append(paths, filepath.Join(filepath.Dir(exeDir), "skills"))
		}
		return NormalizeSearchPaths(paths)
	}
	return nil
}

// DevSearchPaths returns skill search paths for local development.
func DevSearchPaths() []string {
	if pwd, err := os.Getwd(); err == nil {
		return []string{
			filepath.Join(pwd, "skills"),
			filepath.Join(pwd, "dist", "skills"),
		}
	}
	return nil
}

// DefaultSearchPaths returns the default skill search paths.
// Search order:
// 1. FOXCTL_SKILLS_PATH environment variable (can be multiple paths)
// 2. User skills directory (~/.foxctl/skills)
// 3. Built-in skills (relative to executable)
// 4. Development paths (./skills, ./dist/skills)
func DefaultSearchPaths() []string {
	paths := append([]string{}, EnvSearchPaths()...)
	paths = append(paths, UserSearchPaths()...)
	paths = append(paths, BuiltinSearchPaths()...)
	paths = append(paths, DevSearchPaths()...)
	return NormalizeSearchPaths(paths)
}

// NormalizeSearchPaths cleans and de-duplicates search paths while preserving order.
func NormalizeSearchPaths(paths []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(paths))
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
