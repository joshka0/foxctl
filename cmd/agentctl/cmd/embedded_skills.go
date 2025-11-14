package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jkatigb/agentctl/internal/platform/config"
	skillassets "github.com/jkatigb/agentctl/skills"
)

var errUnknownEmbeddedSkill = errors.New("embedded skill not found")

func installEmbeddedSkill(cfg config.Config, name string) (bool, error) {
	switch name {
	case "wasi/echo":
		return installWasiEcho(cfg)
	default:
		return false, errUnknownEmbeddedSkill
	}
}

func installWasiEcho(cfg config.Config) (bool, error) {
	dest := filepath.Join(cfg.Paths.Skills, filepath.FromSlash("wasi/echo"))
	if _, err := os.Stat(filepath.Join(dest, "skill.yaml")); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return false, fmt.Errorf("wasi/echo: mkdir skill dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "skill.yaml"), skillassets.WasiEchoManifest, 0o644); err != nil {
		return false, fmt.Errorf("wasi/echo: write manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dest, "module.wasm"), skillassets.WasiEchoModule, 0o755); err != nil {
		return false, fmt.Errorf("wasi/echo: write module: %w", err)
	}
	return true, nil
}
