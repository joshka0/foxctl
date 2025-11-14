package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	errs "github.com/jkatigb/agentctl/internal/errors"
	"github.com/jkatigb/agentctl/internal/policy"
	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/jkatigb/agentctl/internal/workspace"
)

func resolveWorkspaceContext(ctx context.Context, workspaceOverride string) context.Context {
	ws := workspace.Normalize(workspaceOverride)
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
	dest := filepath.Join(skillsRoot, manifest.Metadata.Name)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", err
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

func matchesQuery(m skill.Manifest, query string) bool {
	q := filepath.Base(query)
	if filepath.Base(m.Metadata.Name) == q {
		return true
	}
	if m.Metadata.Description != "" && contains(m.Metadata.Description, query) {
		return true
	}
	for _, tag := range m.Metadata.Tags {
		if contains(tag, query) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	return anySubstring(s, substr)
}

func anySubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if matchesAt(s, substr, i) {
			return true
		}
	}
	return false
}

func matchesAt(s, substr string, offset int) bool {
	for i := 0; i < len(substr); i++ {
		if toLower(s[offset+i]) != toLower(substr[i]) {
			return false
		}
	}
	return true
}

func toLower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}
