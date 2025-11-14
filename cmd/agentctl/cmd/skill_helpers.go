package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/config"
	"github.com/jkatigb/agentctl/internal/policy"
	"github.com/jkatigb/agentctl/internal/runner"
	"github.com/jkatigb/agentctl/internal/skill"
	"github.com/spf13/cobra"
)

// SkillHandle captures manifest and artifact path for execution.
type SkillHandle struct {
	Manifest     skill.Manifest
	ManifestPath string
	ArtifactPath string
}

func loadSkillInput(cmd *cobra.Command, inline, file string) ([]byte, error) {
	switch {
	case file == "-":
		return io.ReadAll(cmd.InOrStdin())
	case file != "":
		return os.ReadFile(file)
	case inline != "":
		return []byte(inline), nil
	default:
		return []byte("{}"), nil
	}
}

func writeEnvelope(out io.Writer, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	_, err := out.Write(data)
	return err
}

func findSkill(cfg config.Config, requested string) (SkillHandle, error) {
	paths := []string{
		filepath.Join(cfg.Paths.Skills, requested),
		filepath.Join("dist", "skills", normalizeSkillName(requested)),
		filepath.Join("skills", normalizeSkillName(requested)),
	}
	for _, dir := range paths {
		handle, err := loadSkillDir(dir)
		if err == nil && handle.ArtifactPath != "" {
			return handle, nil
		}
	}
	if installed, err := installEmbeddedSkill(cfg, requested); err == nil {
		if installed {
			return findSkill(cfg, requested)
		}
	} else if !errors.Is(err, errUnknownEmbeddedSkill) {
		return SkillHandle{}, err
	}
	return SkillHandle{}, fmt.Errorf("skill %s not found; run make skills-build or agentctl skills install", requested)
}

func loadSkillDir(dir string) (SkillHandle, error) {
	manifestPath := filepath.Join(dir, "skill.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		return SkillHandle{}, err
	}
	manifest, err := skill.LoadManifest(manifestPath)
	if err != nil {
		return SkillHandle{}, err
	}
	if err := policy.ValidateWASIPolicy(manifest); err != nil {
		return SkillHandle{}, err
	}
	var artifact string
	switch manifest.Distribution.Type {
	case "exec":
		candidates := []string{
			filepath.Join(dir, "bin"),
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
		return SkillHandle{}, fmt.Errorf("unsupported distribution %q", manifest.Distribution.Type)
	}
	if artifact == "" {
		return SkillHandle{}, fmt.Errorf("skill artifacts missing under %s", dir)
	}
	return SkillHandle{
		Manifest:     manifest,
		ManifestPath: manifestPath,
		ArtifactPath: artifact,
	}, nil
}

func normalizeSkillName(name string) string {
	n := strings.ReplaceAll(name, "/", "_")
	n = strings.ReplaceAll(n, "-", "_")
	return n
}

func executeSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, []byte, error) {
	return runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        input,
	})
}
