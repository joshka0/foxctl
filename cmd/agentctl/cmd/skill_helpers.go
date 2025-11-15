package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/execution/runner"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/runservice"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/spf13/cobra"
)

// SkillHandle captures manifest and artifact path for execution.
type SkillHandle = runservice.SkillHandle

func loadSkillInput(cmd *cobra.Command, cfg config.Config, inline, file string) ([]byte, error) {
	trimmed := strings.TrimSpace(inline)
	switch {
	case file == "-":
		return io.ReadAll(cmd.InOrStdin())
	case file != "":
		return os.ReadFile(file)
	case strings.EqualFold(trimmed, "stdin"):
		data, err := extractEnvelopeData(cmd.InOrStdin())
		if err != nil {
			return nil, fmt.Errorf("read stdin envelope: %w", err)
		}
		return data, nil
	case strings.HasPrefix(trimmed, "sha256:"):
		store, err := cas.NewStore(cfg.Paths.CAS)
		if err != nil {
			return nil, fmt.Errorf("open cas store: %w", err)
		}
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		rc, _, err := store.Get(ctx, trimmed)
		if err != nil {
			return nil, fmt.Errorf("read cas %s: %w", trimmed, err)
		}
		data, readErr := io.ReadAll(rc)
		closeErr := rc.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read cas %s: %w", trimmed, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("verify cas %s: %w", trimmed, closeErr)
		}
		return data, nil
	case trimmed != "":
		return []byte(inline), nil
	default:
		return []byte("{}"), nil
	}
}

func extractEnvelopeData(r io.Reader) ([]byte, error) {
	dec := json.NewDecoder(r)
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := dec.Decode(&env); err != nil {
		return nil, err
	}
	if len(env.Data) == 0 {
		return []byte("null"), nil
	}
	trimmed := bytes.TrimSpace(env.Data)
	if len(trimmed) == 0 {
		return []byte("null"), nil
	}
	return trimmed, nil
}

func findSkill(cfg config.Config, requested string) (SkillHandle, error) {
	// Use the Resolver to find the skill
	resolver := createSkillResolver(cfg)
	handle, err := resolver.Resolve(requested)
	if err == nil {
		// Found via resolver, now load the full skill directory
		return loadSkillDir(filepath.Dir(handle.ManifestPath))
	}

	// Not found via resolver, try to install embedded skill
	if installed, err := installEmbeddedSkill(cfg, requested); err == nil {
		if installed {
			return findSkill(cfg, requested)
		}
	} else if !errors.Is(err, errUnknownEmbeddedSkill) {
		return SkillHandle{}, err
	}
	return SkillHandle{}, fmt.Errorf("skill %s not found; run make skills-build or agentctl skills install", requested)
}

// createSkillResolver creates a resolver with paths from config.
func createSkillResolver(cfg config.Config) *skill.Resolver {
	// Build search paths from config
	searchPaths := []string{cfg.Paths.Skills}

	// Add current directory development paths
	if pwd, err := os.Getwd(); err == nil {
		searchPaths = append(searchPaths,
			filepath.Join(pwd, "dist", "skills"),
			filepath.Join(pwd, "skills"),
		)
	}

	return skill.NewResolver(skill.WithSearchPaths(searchPaths...))
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

func executeSkill(ctx context.Context, manifest skill.Manifest, artifactPath string, input []byte) ([]byte, []byte, error) {
	return runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     manifest,
		ArtifactPath: artifactPath,
		Input:        input,
	})
}
