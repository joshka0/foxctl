package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

// SkillRunner executes skills via subprocess.
type SkillRunner struct {
	cfg         config.Config
	searchPaths []string
}

// NewSkillRunner creates a new skill runner.
func NewSkillRunner(cfg config.Config) *SkillRunner {
	var searchPaths []string

	// Environment override
	if env := os.Getenv("AGENTCTL_SKILLS_PATH"); env != "" {
		searchPaths = append(searchPaths, filepath.SplitList(env)...)
	}

	// Configured skills path
	searchPaths = append(searchPaths, cfg.Paths.Skills)

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
	Success  bool           `json:"success"`
	Output   json.RawMessage `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Duration time.Duration  `json:"duration_ms"`
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
		return &RunResult{
			Success:  false,
			Error:    err.Error(),
			Duration: time.Since(start),
		}, nil
	}

	// Marshal input to JSON
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return &RunResult{
			Success:  false,
			Error:    fmt.Sprintf("failed to marshal input: %v", err),
			Duration: time.Since(start),
		}, nil
	}

	// Execute skill based on distribution type
	var output []byte
	var runErr error

	switch handle.Manifest.Distribution.Type {
	case "exec":
		output, runErr = r.runExec(ctx, handle, inputJSON)
	case "wasi":
		return &RunResult{
			Success:  false,
			Error:    "WASI execution not yet supported via web API",
			Duration: time.Since(start),
		}, nil
	default:
		return &RunResult{
			Success:  false,
			Error:    fmt.Sprintf("unknown distribution type: %s", handle.Manifest.Distribution.Type),
			Duration: time.Since(start),
		}, nil
	}

	duration := time.Since(start)

	if runErr != nil {
		return &RunResult{
			Success:  false,
			Error:    runErr.Error(),
			Duration: duration,
		}, nil
	}

	// Try to parse output as JSON
	var rawOutput json.RawMessage
	if len(output) > 0 {
		if json.Valid(output) {
			rawOutput = output
		} else {
			// Wrap non-JSON output
			rawOutput, _ = json.Marshal(map[string]string{"raw": string(output)})
		}
	}

	return &RunResult{
		Success:  true,
		Output:   rawOutput,
		Duration: duration,
	}, nil
}

// runExec runs an exec-type skill.
func (r *SkillRunner) runExec(ctx context.Context, handle *SkillHandle, input []byte) ([]byte, error) {
	// Create command
	cmd := exec.CommandContext(ctx, handle.ArtifactPath)

	// Set up environment
	cmd.Env = append(os.Environ(),
		"AGENTCTL_HOME="+r.cfg.Home,
		"AGENTCTL_STORAGE_ROOT="+r.cfg.Storage.Root,
		"AGENTCTL_CACHE_ROOT="+r.cfg.Paths.Cache,
	)

	// Set working directory to skill directory
	cmd.Dir = filepath.Dir(handle.ManifestPath)

	// Pipe input to stdin
	cmd.Stdin = bytes.NewReader(input)

	// Capture output
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Run the command
	err := cmd.Run()
	if err != nil {
		// Include stderr in error message
		errMsg := err.Error()
		if stderr.Len() > 0 {
			errMsg = fmt.Sprintf("%s: %s", errMsg, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("skill execution failed: %s", errMsg)
	}

	return stdout.Bytes(), nil
}

// loadSkillDir loads a skill from a directory.
func loadSkillDir(dir string) (*SkillHandle, error) {
	manifestPath := filepath.Join(dir, "skill.yaml")
	if _, err := os.Stat(manifestPath); err != nil {
		return nil, err
	}

	manifest, err := skill.LoadManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	var artifact string
	switch manifest.Distribution.Type {
	case "exec":
		// Check for binary
		candidates := []string{
			filepath.Join(dir, "bin-cgo"),
			filepath.Join(dir, "bin"),
		}
		// Also check for main.go (source skill)
		if _, err := os.Stat(filepath.Join(dir, "main.go")); err == nil {
			candidates = append(candidates, dir)
		}
		for _, c := range candidates {
			if info, err := os.Stat(c); err == nil && !info.IsDir() {
				artifact = c
				break
			}
		}
	case "wasi":
		artifact = filepath.Join(dir, "module.wasm")
		if _, err := os.Stat(artifact); err != nil {
			artifact = ""
		}
	}

	if artifact == "" {
		return nil, fmt.Errorf("skill artifact not found in %s", dir)
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
