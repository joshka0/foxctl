package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/domain/policy"
	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/env"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
	"github.com/joshka0/foxctl/internal/platform/workspace"
)

func resolveWorkspaceContext(ctx context.Context, workspaceOverride string) context.Context {
	ws := workspace.Normalize(workspaceOverride)
	if ws == "" {
		if envWS := env.GetString("FOXCTL_WORKSPACE"); envWS != "" {
			ws = workspace.Normalize(envWS)
		}
	}
	if ws == "" {
		ws = workspace.Detect("")
	}
	if ws == "" {
		return ctx
	}
	return workspace.WithContext(ctx, ws)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() {
		errs.Ignore(in.Close(), "close source file")
	}()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() {
		errs.Ignore(out.Close(), "close dest file")
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	return os.Chmod(dst, info.Mode())
}

func ensureSkillDir(skillsRoot string, manifest skill.Manifest) (string, error) {
	name := strings.TrimSpace(manifest.Metadata.Name)
	if name == "" {
		return "", fmt.Errorf("skill metadata name is required")
	}
	// Validate the canonical name before normalization
	if name == "." || name == ".." || filepath.IsAbs(name) {
		return "", fmt.Errorf("invalid skill name %q", manifest.Metadata.Name)
	}
	// Normalize: "code/semantic_search" -> "code_semantic_search"
	// This creates flat directories instead of nested ones
	normalizedName := skill.NormalizeSkillName(name)
	// Prevent path traversal in normalized name
	if strings.Contains(normalizedName, "..") || strings.Contains(normalizedName, string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid skill name %q", manifest.Metadata.Name)
	}
	root := filepath.Clean(skillsRoot)
	dest := filepath.Join(root, normalizedName)
	dest = filepath.Clean(dest)
	// Final safety check: ensure dest is within root
	if rel, err := filepath.Rel(root, dest); err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid skill name %q (path traversal detected)", manifest.Metadata.Name)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
	}
	return dest, nil
}

// skillDirPath validates and returns the skill directory path without requiring a valid manifest.
// This is useful for operations like uninstall where the manifest may be corrupted.
// Accepts both canonical (text/grep) and normalized (text_grep) names.
func skillDirPath(skillsRoot, name string) (string, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return "", fmt.Errorf("skill name is required")
	}
	// Validate the canonical name before normalization
	if cleanName == "." || cleanName == ".." || filepath.IsAbs(cleanName) {
		return "", fmt.Errorf("invalid skill name %q", name)
	}
	// Normalize: "text/grep" -> "text_grep" for flat directory structure
	normalizedName := skill.NormalizeSkillName(cleanName)
	// Prevent path traversal in normalized name
	if strings.Contains(normalizedName, "..") || strings.Contains(normalizedName, string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid skill name %q", name)
	}
	root := filepath.Clean(skillsRoot)
	dest := filepath.Join(root, normalizedName)
	dest = filepath.Clean(dest)
	// Final safety check: ensure dest is within root
	if rel, err := filepath.Rel(root, dest); err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid skill name %q (path traversal detected)", name)
	}
	return dest, nil
}

func loadValidatedManifest(path string) (skill.Manifest, error) {
	manifest, err := skill.LoadManifest(path)
	if err != nil {
		return skill.Manifest{}, err
	}
	if err := policy.ValidateWASIPolicy(manifest); err != nil {
		return skill.Manifest{}, err
	}
	return manifest, nil
}

func loadValidatedUpgradeManifest(path, skillName string) (skill.Manifest, error) {
	manifest, err := loadValidatedManifest(path)
	if err != nil {
		return skill.Manifest{}, err
	}
	if manifest.Metadata.Name != skillName {
		return skill.Manifest{}, fmt.Errorf("manifest name %q does not match skill name %q", manifest.Metadata.Name, skillName)
	}
	return manifest, nil
}

func writeManifest(destDir, manifestPath string) error {
	return copyFile(manifestPath, filepath.Join(destDir, "skill.yaml"))
}

func writeDistributionArtifacts(dest string, manifest skill.Manifest, binaryPath, modulePath string) error {
	switch manifest.Distribution.Type {
	case "exec":
		if binaryPath == "" {
			return fmt.Errorf("--binary is required for exec skills")
		}
		return copyFile(binaryPath, filepath.Join(dest, "bin"))
	case "wasi":
		if modulePath == "" {
			return fmt.Errorf("--module is required for wasi skills")
		}
		return copyFile(modulePath, filepath.Join(dest, "module.wasm"))
	default:
		return fmt.Errorf("unsupported distribution: %s", manifest.Distribution.Type)
	}
}

func summarizeSkills(manifests []skill.Manifest) []map[string]string {
	var out []map[string]string
	for _, m := range manifests {
		out = append(out, map[string]string{
			"name":        m.Metadata.Name,
			"version":     m.Metadata.Version,
			"description": m.Metadata.Description,
		})
	}
	return out
}
