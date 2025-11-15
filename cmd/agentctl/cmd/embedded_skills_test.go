package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestFindSkillInstallsEmbeddedWASI(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{
		Home:           tmp,
		InlineOutputKB: config.DefaultInlineOutputKB,
		MaxCaptureKB:   config.DefaultMaxCaptureKB,
		Paths: config.Paths{
			CAS:    filepath.Join(tmp, "cas"),
			Jobs:   filepath.Join(tmp, "jobs"),
			Cache:  filepath.Join(tmp, "cache"),
			Skills: filepath.Join(tmp, "skills"),
		},
	}

	handle, err := findSkill(cfg, "wasi/echo")
	if err != nil {
		t.Fatalf("find skill: %v", err)
	}
	if handle.Manifest.Metadata.Name != "wasi/echo" {
		t.Fatalf("unexpected skill name %s", handle.Manifest.Metadata.Name)
	}

	modulePath := filepath.Join(cfg.Paths.Skills, filepath.FromSlash("wasi/echo/module.wasm"))
	if _, err := os.Stat(modulePath); err != nil {
		t.Fatalf("module not installed: %v", err)
	}

	// Second call should reuse installed skill without error.
	if _, err := findSkill(cfg, "wasi/echo"); err != nil {
		t.Fatalf("find skill (second) failed: %v", err)
	}
}

func TestFindSkillRepairsCorruptInstall(t *testing.T) {
	tmp := t.TempDir()
	cfg := config.Config{
		Home:           tmp,
		InlineOutputKB: config.DefaultInlineOutputKB,
		MaxCaptureKB:   config.DefaultMaxCaptureKB,
		Paths: config.Paths{
			CAS:    filepath.Join(tmp, "cas"),
			Jobs:   filepath.Join(tmp, "jobs"),
			Cache:  filepath.Join(tmp, "cache"),
			Skills: filepath.Join(tmp, "skills"),
		},
	}

	corruptDir := filepath.Join(cfg.Paths.Skills, filepath.FromSlash("wasi/echo"))
	if err := os.MkdirAll(corruptDir, 0o755); err != nil {
		t.Fatalf("mkdir corrupt dir: %v", err)
	}
	copySkillFile(t,
		filepath.Join(repoRoot(t), "skills", "wasi_echo", "skill.yaml"),
		filepath.Join(corruptDir, "skill.yaml"),
	)
	// Intentionally omit module.wasm to simulate incomplete install.

	handle, err := findSkill(cfg, "wasi/echo")
	if err != nil {
		t.Fatalf("find skill: %v", err)
	}
	if handle.Manifest.Metadata.Name != "wasi/echo" {
		t.Fatalf("unexpected skill: %s", handle.Manifest.Metadata.Name)
	}
	if _, err := os.Stat(filepath.Join(corruptDir, "module.wasm")); err != nil {
		t.Fatalf("module still missing: %v", err)
	}
}

func TestLoadSkillDirRejectsWASINetwork(t *testing.T) {
	dir := t.TempDir()
	manifest := `
apiVersion: agentctl/v1
kind: Skill
metadata:
  name: test/wasi
  version: 0.1.0
  description: bad wasi
distribution:
  type: wasi
  wasi:
    module: module.wasm
io:
  format: JSON
signature:
  command: test/wasi
capabilities:
  network: "egress"
`
	if err := os.WriteFile(filepath.Join(dir, "skill.yaml"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "module.wasm"), []byte{0x0}, 0o644); err != nil {
		t.Fatalf("write module: %v", err)
	}
	if _, err := loadSkillDir(dir); err == nil {
		t.Fatalf("expected validation error")
	}
}
