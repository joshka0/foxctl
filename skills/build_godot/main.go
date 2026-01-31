// Package main implements the build/godot skill for exporting and building Godot projects.
// Provides export preset management, project export, and C# build operations.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
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

// Input represents the skill input parameters for build/godot operations.
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

// ExportPreset represents a parsed export preset configuration.
type ExportPreset struct {
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	ExportPath string `json:"export_path,omitempty"`
	Runnable   bool   `json:"runnable"`
}

// main is the skill entry point for build/godot.
func main() {
	skillmain.Main(skillName, run)
}

// run orchestrates build/godot operations based on the specified action.
//
// Index:
// - Purpose: Execute Godot project operations including export, build, and preset management
// - Flow: validate action → apply defaults → route to action handler (listPresets/exportProject/buildCSharp/restoreCSharp/cleanCSharp)
// - SideEffects: file system operations (exports, builds, directory creation); external process execution (godot, dotnet)
// - FailureModes: missing export_presets.cfg, invalid preset, godot/dotnet not found, build failures, I/O errors
// - Observability: emits action-specific results (presets/count, export results, build results)
// - Related: listPresets, exportProject, buildCSharp, restoreCSharp, cleanCSharp, emitSuccess, executil.Run
// - Keywords: build/godot, action, preset, export, build, list_presets, validate, restore, clean
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate
	if strings.TrimSpace(in.Action) == "" {
		return skillerr.Arg(
			"action is required",
			skillerr.WithHint("Provide action=list_presets, export, validate, build, restore, or clean."),
		)
	}
	// Apply defaults
	if in.GodotPath == "" {
		in.GodotPath = "godot"
	}

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
		return skillerr.Arg(
			fmt.Sprintf("unknown action: %q", in.Action),
			skillerr.WithHint("Valid actions: list_presets, export, validate, build, restore, clean."),
		)
	}
}

// listPresets parses and returns available export presets from export_presets.cfg.
//
// Index:
// - Purpose: List all available export presets for the Godot project
// - Flow: parse export_presets.cfg → return preset list with count
// - SideEffects: file read (export_presets.cfg)
// - FailureModes: missing export_presets.cfg, file parse errors
// - Observability: emits action/presets/count/summary
// - Related: parseExportPresets, emitSuccess
// - Keywords: list_presets, export_presets.cfg, presets, count, parseExportPresets
func listPresets(ctx context.Context, rc *skillmain.RunContext, workspace string) error {
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

// exportProject exports the Godot project using the specified preset.
//
// Index:
// - Purpose: Export Godot project to target platform using preset configuration
// - Flow: validate preset → parse presets → match preset → resolve output path → create output dir → export via godot CLI
// - SideEffects: file system writes (exported files); external process execution (godot)
// - FailureModes: invalid preset, missing export path, godot execution failures, permission errors
// - Observability: emits action/preset/platform/output_path/output_exists/exit_code/duration_ms/stdout/stderr/output_size_bytes/summary
// - Related: parseExportPresets, emitSuccess, executil.Run
// - Keywords: export, preset, platform, output_path, godot, export_debug, pack_only, dry_run
func exportProject(ctx context.Context, rc *skillmain.RunContext, workspace string, in Input, dryRun bool) error {
	// Validate preset is provided
	if strings.TrimSpace(in.Preset) == "" {
		return skillerr.Arg(
			fmt.Sprintf("preset is required for action %q", in.Action),
			skillerr.WithHint("Run action=list_presets to see available presets."),
		)
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
		return skillerr.NotFound(
			fmt.Sprintf("preset %q not found", in.Preset),
			skillerr.WithHint("Available presets: "+strings.Join(presetNames, ", ")),
		)
	}

	// Determine output path
	outputPath := in.OutputPath
	if outputPath == "" {
		outputPath = matchedPreset.ExportPath
	}
	if outputPath == "" {
		return skillerr.Arg(
			fmt.Sprintf("output_path is required (preset %q has no default export path)", in.Preset),
		)
	}

	// Make output path absolute if relative
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(workspace, outputPath)
	}

	// Ensure output directory exists
	outputDir := filepath.Dir(outputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return skillerr.WrapIO("create output directory", err)
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
	cmdResult := executil.Run(ctx, workspace, in.GodotPath, args...)
	if cmdResult.Err != nil && cmdResult.ExitCode == -1 {
		// Command failed to run (e.g., godot not found)
		return skillerr.Runtime(
			"failed to run godot",
			skillerr.WithCause(cmdResult.Err),
			skillerr.WithHint("Ensure godot is installed and available in PATH."),
		)
	}
	exitCode := cmdResult.ExitCode
	duration := cmdResult.Duration

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
		"stdout":        string(cmdResult.Stdout),
		"stderr":        string(cmdResult.Stderr),
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

// parseExportPresets parses the export_presets.cfg file into ExportPreset structs.
func parseExportPresets(workspace string) ([]ExportPreset, error) {
	cfgPath := filepath.Join(workspace, "export_presets.cfg")

	file, err := os.Open(cfgPath)
	if os.IsNotExist(err) {
		return nil, skillerr.NotFound(
			fmt.Sprintf("no export_presets.cfg found in %s", workspace),
			skillerr.WithHint("Create export presets in Project > Export..."),
		)
	}
	if err != nil {
		return nil, skillerr.WrapIO("open export_presets.cfg", err)
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
		return nil, skillerr.WrapIO("read export_presets.cfg", err)
	}

	return presets, nil
}

// buildCSharp builds the C# project using dotnet.
func buildCSharp(ctx context.Context, rc *skillmain.RunContext, workspace string, in Input) error {
	return runDotnet(ctx, rc, workspace, in, "build")
}

// restoreCSharp restores NuGet packages for the C# project.
func restoreCSharp(ctx context.Context, rc *skillmain.RunContext, workspace string, in Input) error {
	return runDotnet(ctx, rc, workspace, in, "restore")
}

// cleanCSharp cleans the C# project build outputs.
func cleanCSharp(ctx context.Context, rc *skillmain.RunContext, workspace string, in Input) error {
	return runDotnet(ctx, rc, workspace, in, "clean")
}

// runDotnet executes a dotnet command (build/restore/clean) on the project.
//
// Index:
// - Purpose: Execute dotnet commands for C# Godot project management
// - Flow: find .csproj file → build dotnet args → execute dotnet command → emit results
// - SideEffects: external process execution (dotnet); file system operations (build outputs)
// - FailureModes: missing .csproj file, dotnet not found, build failures
// - Observability: emits action/csproj/exit_code/duration_ms/stdout/stderr/configuration/summary
// - Related: findCsproj, emitSuccess, executil.Run
// - Keywords: dotnet, build, restore, clean, csproj, configuration, verbosity, target
func runDotnet(ctx context.Context, rc *skillmain.RunContext, workspace string, in Input, command string) error {
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
	cmdResult := executil.Run(ctx, workspace, dotnetPath, args...)
	if cmdResult.Err != nil && cmdResult.ExitCode == -1 {
		return skillerr.Runtime(
			"failed to run dotnet",
			skillerr.WithCause(cmdResult.Err),
			skillerr.WithHint("Ensure the .NET SDK is installed and available in PATH."),
		)
	}
	exitCode := cmdResult.ExitCode
	duration := cmdResult.Duration

	result := map[string]any{
		"action":      command,
		"csproj":      csprojPath,
		"exit_code":   exitCode,
		"duration_ms": duration.Milliseconds(),
		"stdout":      string(cmdResult.Stdout),
		"stderr":      string(cmdResult.Stderr),
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

// findCsproj locates the .csproj file in the workspace directory.
func findCsproj(workspace string) (string, error) {
	// Look for .csproj files in workspace
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return "", skillerr.WrapIO("read workspace directory", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".csproj") {
			return filepath.Join(workspace, entry.Name()), nil
		}
	}

	return "", skillerr.NotFound(
		fmt.Sprintf("no .csproj file found in %s", workspace),
		skillerr.WithHint("Ensure this is a C# Godot project with a .csproj file."),
	)
}

// emitSuccess emits a successful result with the provided data.
func emitSuccess(rc *skillmain.RunContext, result map[string]any) error {
	return skillout.Emit(rc, skillName, result)
}
