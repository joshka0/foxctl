// Package main implements the build/godot skill for exporting Godot projects.
package main

import (
	"bufio"
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

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/domain/envelope"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

const skillName = "build/godot"

// Action constants.
const (
	ActionListPresets = "list_presets"
	ActionExport      = "export"
	ActionValidate    = "validate"
	ActionBuild       = "build"
	ActionRestore     = "restore"
	ActionClean       = "clean"
)

// Input represents the skill input parameters.
type Input struct {
	Action      string `json:"action"`
	Preset      string `json:"preset"`
	OutputPath  string `json:"output_path"`
	Debug       bool   `json:"debug"`
	GodotPath   string `json:"godot_path"`
	PackOnly    bool   `json:"pack_only"`
	ExportDebug bool   `json:"export_debug"`

	// C# build parameters
	Configuration string `json:"configuration"` // Debug or Release
	Verbosity     string `json:"verbosity"`     // quiet, minimal, normal, detailed, diagnostic
	Target        string `json:"target"`        // Build target (e.g., Build, Rebuild, Clean)
	DotnetPath    string `json:"dotnet_path"`   // Path to dotnet executable
}

// ExportPreset represents a parsed export preset.
type ExportPreset struct {
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	ExportPath string `json:"export_path,omitempty"`
	Runnable   bool   `json:"runnable"`
}

func main() {
	ctx := context.Background()
	cfg, err := config.Load(ctx)
	if err != nil {
		fail("ERUNTIME", err, "Check AGENTCTL_HOME and config file permissions")
	}

	rc, err := runner.NewRunnerContext(cfg, os.Stdout)
	if err != nil {
		fail("ERUNTIME", err, "Failed to initialize runner context")
	}
	defer func() {
		errs.Ignore(rc.Close(), "runner context close")
	}()

	in, err := parseInput(os.Stdin)
	if err != nil {
		fail("EARG", err, "Provide valid JSON input with required action field")
	}

	if err := run(ctx, rc, in); err != nil {
		fail("ERUNTIME", err, "Check godot/dotnet installation and project structure")
	}
}

func parseInput(r io.Reader) (Input, error) {
	var in Input
	if err := json.NewDecoder(r).Decode(&in); err != nil {
		return Input{}, fmt.Errorf("decode input: %w", err)
	}

	// Apply defaults
	if in.GodotPath == "" {
		in.GodotPath = "godot"
	}

	// Validate action
	if strings.TrimSpace(in.Action) == "" {
		return Input{}, fmt.Errorf("action is required")
	}

	return in, nil
}

func run(ctx context.Context, rc *runner.RunnerContext, in Input) error {
	workspace := rc.PathValidator.Workspace()

	switch in.Action {
	case ActionListPresets:
		return listPresets(ctx, rc, workspace)
	case ActionExport:
		return exportProject(ctx, rc, workspace, in, false)
	case ActionValidate:
		return exportProject(ctx, rc, workspace, in, true)
	case ActionBuild:
		return buildCSharp(ctx, rc, workspace, in)
	case ActionRestore:
		return restoreCSharp(ctx, rc, workspace, in)
	case ActionClean:
		return cleanCSharp(ctx, rc, workspace, in)
	default:
		return fmt.Errorf("unknown action: %q (valid: list_presets, export, validate, build, restore, clean)", in.Action)
	}
}

func listPresets(ctx context.Context, rc *runner.RunnerContext, workspace string) error {
	presets, err := parseExportPresets(workspace)
	if err != nil {
		return err
	}

	// Ensure presets is not nil for proper JSON serialization ([] instead of null)
	if presets == nil {
		presets = []ExportPreset{}
	}

	result := map[string]any{
		"action":  ActionListPresets,
		"presets": presets,
		"count":   len(presets),
		"summary": fmt.Sprintf("Found %d export preset(s)", len(presets)),
	}

	return emitSuccess(rc, result)
}

func exportProject(ctx context.Context, rc *runner.RunnerContext, workspace string, in Input, dryRun bool) error {
	// Validate preset is provided
	if strings.TrimSpace(in.Preset) == "" {
		return fmt.Errorf("preset is required for action %q", in.Action)
	}

	// Parse presets to validate the requested one exists
	presets, err := parseExportPresets(workspace)
	if err != nil {
		return err
	}

	var matchedPreset *ExportPreset
	var presetNames []string
	for i := range presets {
		presetNames = append(presetNames, presets[i].Name)
		if presets[i].Name == in.Preset {
			matchedPreset = &presets[i]
		}
	}

	if matchedPreset == nil {
		return fmt.Errorf("preset %q not found (available: %s)", in.Preset, strings.Join(presetNames, ", "))
	}

	// Determine output path
	outputPath := in.OutputPath
	if outputPath == "" {
		outputPath = matchedPreset.ExportPath
	}
	if outputPath == "" {
		return fmt.Errorf("output_path is required (preset %q has no default export path)", in.Preset)
	}

	// Make output path absolute if relative
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(workspace, outputPath)
	}

	// Ensure output directory exists
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	// Dry run - just return what would happen
	if dryRun {
		result := map[string]any{
			"action":      ActionValidate,
			"dry_run":     true,
			"preset":      in.Preset,
			"platform":    matchedPreset.Platform,
			"output_path": outputPath,
			"debug":       in.ExportDebug,
			"pack_only":   in.PackOnly,
			"summary":     fmt.Sprintf("Would export %q to %s", in.Preset, outputPath),
		}
		return emitSuccess(rc, result)
	}

	// Build godot command
	args := []string{"--headless"}

	// Determine export type
	exportFlag := "--export-release"
	if in.ExportDebug {
		exportFlag = "--export-debug"
	}
	if in.PackOnly {
		exportFlag = "--export-pack"
	}

	args = append(args, exportFlag, in.Preset, outputPath)

	// Execute godot
	cmd := exec.CommandContext(ctx, in.GodotPath, args...)
	cmd.Dir = workspace

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			// Command failed to run (e.g., godot not found)
			return fmt.Errorf("failed to run godot: %w (is godot installed and in PATH?)", err)
		}
	}

	// Check if output file was created
	outputExists := false
	var outputSize int64
	if info, statErr := os.Stat(outputPath); statErr == nil {
		outputExists = true
		outputSize = info.Size()
	}

	result := map[string]any{
		"action":        ActionExport,
		"preset":        in.Preset,
		"platform":      matchedPreset.Platform,
		"output_path":   outputPath,
		"output_exists": outputExists,
		"exit_code":     exitCode,
		"duration_ms":   duration.Milliseconds(),
		"stdout":        stdout.String(),
		"stderr":        stderr.String(),
	}

	if outputExists {
		result["output_size_bytes"] = outputSize
		result["summary"] = fmt.Sprintf("Exported %q to %s (%d bytes) in %v", in.Preset, outputPath, outputSize, duration.Round(time.Millisecond))
	} else if exitCode != 0 {
		result["summary"] = fmt.Sprintf("Export failed with exit code %d", exitCode)
	} else {
		result["summary"] = fmt.Sprintf("Export completed but output file not found at %s", outputPath)
	}

	return emitSuccess(rc, result)
}

func parseExportPresets(workspace string) ([]ExportPreset, error) {
	cfgPath := filepath.Join(workspace, "export_presets.cfg")

	file, err := os.Open(cfgPath)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no export_presets.cfg found in %s (create export presets in Project > Export...)", workspace)
	}
	if err != nil {
		return nil, fmt.Errorf("open export_presets.cfg: %w", err)
	}
	defer func() {
		errs.Ignore(file.Close(), "close export_presets.cfg")
	}()

	var presets []ExportPreset
	var current *ExportPreset

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// New preset section
		if strings.HasPrefix(line, "[preset.") {
			if current != nil && current.Name != "" {
				presets = append(presets, *current)
			}
			current = &ExportPreset{}
			continue
		}

		// End of presets (options section)
		if strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "[preset.") {
			if current != nil && current.Name != "" {
				presets = append(presets, *current)
			}
			current = nil
			continue
		}

		// Parse key=value
		if current != nil && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, "\"")

			switch key {
			case "name":
				current.Name = value
			case "platform":
				current.Platform = value
			case "export_path":
				current.ExportPath = value
			case "runnable":
				current.Runnable = value == "true"
			}
		}
	}

	// Don't forget the last preset
	if current != nil && current.Name != "" {
		presets = append(presets, *current)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read export_presets.cfg: %w", err)
	}

	return presets, nil
}

func buildCSharp(ctx context.Context, rc *runner.RunnerContext, workspace string, in Input) error {
	return runDotnet(ctx, rc, workspace, in, "build")
}

func restoreCSharp(ctx context.Context, rc *runner.RunnerContext, workspace string, in Input) error {
	return runDotnet(ctx, rc, workspace, in, "restore")
}

func cleanCSharp(ctx context.Context, rc *runner.RunnerContext, workspace string, in Input) error {
	return runDotnet(ctx, rc, workspace, in, "clean")
}

func runDotnet(ctx context.Context, rc *runner.RunnerContext, workspace string, in Input, command string) error {
	// Find the .csproj file
	csprojPath, err := findCsproj(workspace)
	if err != nil {
		return err
	}

	// Build dotnet args
	dotnetPath := in.DotnetPath
	if dotnetPath == "" {
		dotnetPath = "dotnet"
	}

	args := []string{command, csprojPath}

	// Add configuration if specified (for build/clean)
	if command != "restore" {
		config := in.Configuration
		if config == "" {
			config = "Debug"
		}
		args = append(args, "--configuration", config)
	}

	// Add verbosity if specified
	if in.Verbosity != "" {
		args = append(args, "--verbosity", in.Verbosity)
	}

	// Add target if specified (for build)
	if command == "build" && in.Target != "" {
		args = append(args, "--target", in.Target)
	}

	// Execute dotnet
	cmd := exec.CommandContext(ctx, dotnetPath, args...)
	cmd.Dir = workspace

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err = cmd.Run()
	duration := time.Since(startTime)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return fmt.Errorf("failed to run dotnet: %w (is dotnet SDK installed?)", err)
		}
	}

	result := map[string]any{
		"action":      command,
		"csproj":      csprojPath,
		"exit_code":   exitCode,
		"duration_ms": duration.Milliseconds(),
		"stdout":      stdout.String(),
		"stderr":      stderr.String(),
	}

	if command != "restore" {
		result["configuration"] = in.Configuration
		if result["configuration"] == "" {
			result["configuration"] = "Debug"
		}
	}

	if exitCode == 0 {
		result["summary"] = fmt.Sprintf("dotnet %s succeeded in %v", command, duration.Round(time.Millisecond))
	} else {
		result["summary"] = fmt.Sprintf("dotnet %s failed with exit code %d", command, exitCode)
	}

	return emitSuccess(rc, result)
}

func findCsproj(workspace string) (string, error) {
	// Look for .csproj files in workspace
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return "", fmt.Errorf("read workspace directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".csproj") {
			return filepath.Join(workspace, entry.Name()), nil
		}
	}

	return "", fmt.Errorf("no .csproj file found in %s (is this a C# Godot project?)", workspace)
}

func emitSuccess(rc *runner.RunnerContext, result map[string]any) error {
	meta := envelope.Meta{
		TS:        time.Now().UTC().Format(time.RFC3339),
		Source:    "run",
		Runner:    "exec",
		Workspace: rc.PathValidator.Workspace(),
	}

	return rc.Emit(skillName, result, "application/json", meta)
}

func fail(code string, err error, hint string) {
	data := map[string]any{"hint": hint}
	env := envelope.Error(skillName, code, err.Error(), data)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit failure envelope")
	os.Exit(1)
}
