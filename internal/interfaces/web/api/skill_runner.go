package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/execution/runner"
)

// SkillRunner executes skills via subprocess.
type SkillRunner struct {
	cfg         config.Config
	searchPaths []string
}

// NewSkillRunner creates a new skill runner.
func NewSkillRunner(cfg config.Config) *SkillRunner {
	searchPaths := append([]string{}, skill.EnvSearchPaths()...)
	if cfg.Paths.Skills != "" {
		searchPaths = append(searchPaths, cfg.Paths.Skills)
	}
	searchPaths = append(searchPaths, skill.UserSearchPaths()...)
	searchPaths = append(searchPaths, skill.BuiltinSearchPaths()...)
	searchPaths = append(searchPaths, skill.DevSearchPaths()...)
	searchPaths = skill.NormalizeSearchPaths(searchPaths)

	return &SkillRunner{
		cfg:         cfg,
		searchPaths: searchPaths,
	}
}

// SkillHandle captures manifest and artifact metadata.
type SkillHandle struct {
	Manifest     skill.Manifest
	ManifestPath string
	ArtifactPath string
}

// RunResult contains the result of a skill execution.
type RunResult struct {
	Success  bool            `json:"success"`
	Output   json.RawMessage `json:"output,omitempty"`
	Error    string          `json:"error,omitempty"`
	Duration time.Duration   `json:"duration_ms"`
}

// Resolve finds a skill by name.
func (r *SkillRunner) Resolve(skillName string) (*SkillHandle, error) {
	if skillName == "" {
		return nil, fmt.Errorf("skill name is required")
	}

	// Validate skill name to prevent path traversal
	if strings.Contains(skillName, "..") {
		return nil, fmt.Errorf("invalid skill name: %s", skillName)
	}

	// Try each search path
	for _, base := range r.searchPaths {
		if base == "" {
			continue
		}

		absBase, err := filepath.Abs(base)
		if err != nil {
			continue
		}

		// Try direct path first
		dir := filepath.Join(base, filepath.FromSlash(skillName))
		if isWithinBase(dir, absBase) {
			if handle, err := loadSkillDir(dir); err == nil {
				return handle, nil
			}
		}

		// Try normalized path (underscore)
		norm := strings.ReplaceAll(skillName, "/", "_")
		norm = strings.ReplaceAll(norm, "-", "_")
		if norm != skillName {
			dir = filepath.Join(base, filepath.FromSlash(norm))
			if isWithinBase(dir, absBase) {
				if handle, err := loadSkillDir(dir); err == nil {
					return handle, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("skill %s not found in search paths: %v", skillName, r.searchPaths)
}

// isWithinBase checks if path is within the base directory.
func isWithinBase(path, absBase string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absPath, absBase+string(filepath.Separator)) || absPath == absBase
}

// Run executes a skill with the given input.
func (r *SkillRunner) Run(ctx context.Context, skillName string, input map[string]any) (*RunResult, error) {
	start := time.Now()

	handle, err := r.Resolve(skillName)
	if err != nil {
		return nil, err
	}

	// Marshal input to JSON
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal input: %w", err)
	}

	switch handle.Manifest.Distribution.Type {
	case "exec", "wasi":
	default:
		return nil, fmt.Errorf("unknown distribution type: %s", handle.Manifest.Distribution.Type)
	}

	stdout, stderr, runErr := runner.RunWithOptions(ctx, runner.RunOptions{
		Manifest:     handle.Manifest,
		ArtifactPath: handle.ArtifactPath,
		Input:        inputJSON,
		WorkDir:      filepath.Dir(handle.ManifestPath),
		ExtraEnv: []string{
			"AGENTCTL_HOME=" + r.cfg.Home,
			"AGENTCTL_STORAGE_ROOT=" + r.cfg.Storage.Root,
			"AGENTCTL_CACHE_ROOT=" + r.cfg.Paths.Cache,
		},
	})

	duration := time.Since(start)

	if runErr != nil {
		errMsg := runErr.Error()
		if len(stderr) > 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, strings.TrimSpace(string(stderr)))
		}
		return nil, fmt.Errorf("skill run failed: %s", errMsg)
	}

	// Try to parse output as JSON
	var rawOutput json.RawMessage
	if len(stdout) > 0 {
		if json.Valid(stdout) {
			rawOutput = stdout
		} else {
			// Wrap non-JSON output
			rawOutput, _ = json.Marshal(map[string]string{"raw": string(stdout)})
		}
	}

	return &RunResult{
		Success:  true,
		Output:   rawOutput,
		Duration: duration,
	}, nil
}

// loadSkillDir loads a skill from a directory.
func loadSkillDir(dir string) (*SkillHandle, error) {
	manifestPath := filepath.Join(dir, "skill.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		return nil, err
	}

	manifest, artifact, err := skill.LoadManifestAndArtifactFromDir(dir, skill.ArtifactOptions{})
	if err != nil {
		if errors.Is(err, skill.ErrArtifactsMissing) {
			return nil, fmt.Errorf("skill artifact not found in %s", dir)
		}
		return nil, err
	}

	return &SkillHandle{
		Manifest:     manifest,
		ManifestPath: manifestPath,
		ArtifactPath: artifact,
	}, nil
}

// Close is a no-op for SkillRunner (no resources to release).
func (r *SkillRunner) Close() error {
	return nil
}

// ReadInputFromReader reads JSON input from a reader.
func ReadInputFromReader(r io.Reader) (map[string]any, error) {
	var input map[string]any
	if err := json.NewDecoder(r).Decode(&input); err != nil {
		if err == io.EOF {
			return make(map[string]any), nil
		}
		return nil, err
	}
	return input, nil
}
