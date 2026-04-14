// Package main implements the build/unity skill for building, testing, and exporting Unity projects.
// Uses the Unity Editor CLI (-batchmode) for headless operations.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
)

const skillName = "build/unity"

// Action constants.
const (
	ActionListTargets = "list_targets"
	ActionBuild       = "build"
	ActionTest        = "test"
	ActionExport      = "export"
	ActionClean       = "clean"
)

// BuildTarget represents a Unity build target platform.
type BuildTarget struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// knownTargets lists the supported Unity build target platforms.
var knownTargets = []BuildTarget{
	{Name: "StandaloneWindows64", Description: "Windows 64-bit"},
	{Name: "StandaloneOSX", Description: "macOS"},
	{Name: "StandaloneLinux64", Description: "Linux 64-bit"},
	{Name: "Android", Description: "Android"},
	{Name: "iOS", Description: "iOS"},
	{Name: "WebGL", Description: "WebGL"},
}

// Input represents the skill input parameters for build/unity operations.
type Input struct {
	Action        string `json:"action"`
	BuildTarget   string `json:"build_target"`
	OutputPath    string `json:"output_path"`
	UnityPath     string `json:"unity_path"`
	Development   bool   `json:"development"`
	ExecuteMethod string `json:"execute_method"`
	TestPlatform  string `json:"test_platform"`
	TestFilter    string `json:"test_filter"`
	TestResults   string `json:"test_results"`
	ProjectPath   string `json:"project_path"`
}

func main() {
	skillmain.Main(skillName, run)
}

// run orchestrates build/unity operations based on the specified action.
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if strings.TrimSpace(in.Action) == "" {
		return skillerr.Arg(
			"action is required",
			skillerr.WithHint("Provide action=list_targets, build, test, export, or clean."),
		)
	}

	workspace := rc.PathValidator.Workspace()

	// Resolve project path
	projectPath := in.ProjectPath
	if projectPath == "" {
		projectPath = workspace
	}
	if !filepath.IsAbs(projectPath) {
		projectPath = filepath.Join(workspace, projectPath)
	}

	switch in.Action {
	case ActionListTargets:
		return listTargets(rc)
	case ActionBuild:
		return buildProject(ctx, rc, projectPath, in)
	case ActionTest:
		return testProject(ctx, rc, projectPath, in)
	case ActionExport:
		return exportProject(ctx, rc, projectPath, in)
	case ActionClean:
		return cleanProject(rc, projectPath)
	default:
		return skillerr.Arg(
			fmt.Sprintf("unknown action: %q", in.Action),
			skillerr.WithHint("Valid actions: list_targets, build, test, export, clean."),
		)
	}
}

// listTargets returns the list of known Unity build target platforms.
func listTargets(rc *skillmain.RunContext) error {
	result := map[string]any{
		"action":  ActionListTargets,
		"targets": knownTargets,
		"count":   len(knownTargets),
		"summary": fmt.Sprintf("Unity supports %d build targets", len(knownTargets)),
	}
	return emitSuccess(rc, result)
}

// buildProject builds the Unity project for the specified target platform.
func buildProject(ctx context.Context, rc *skillmain.RunContext, projectPath string, in Input) error {
	if strings.TrimSpace(in.BuildTarget) == "" {
		return skillerr.Arg(
			"build_target is required for build action",
			skillerr.WithHint("Example targets: StandaloneOSX, StandaloneWindows64, StandaloneLinux64, Android, iOS, WebGL"),
		)
	}

	if err := validateProjectPath(projectPath); err != nil {
		return err
	}

	unityPath, err := resolveUnityPath(ctx, in.UnityPath)
	if err != nil {
		return err
	}

	// Resolve output path
	outputPath := in.OutputPath
	if outputPath == "" {
		outputPath = filepath.Join(projectPath, "Builds", in.BuildTarget)
	}
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(projectPath, outputPath)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return skillerr.WrapIO("create output directory", err, skillerr.WithHint("Check write permissions and that the parent directory exists."))
	}

	args := []string{
		"-batchmode", "-quit", "-nographics",
		"-projectPath", projectPath,
		"-buildTarget", in.BuildTarget,
	}

	if in.ExecuteMethod != "" {
		args = append(args, "-executeMethod", in.ExecuteMethod)
	}

	if in.Development {
		args = append(args, "-development")
	}

	cmdResult := executil.Run(ctx, projectPath, unityPath, args...)
	return emitBuildResult(rc, ActionBuild, in.BuildTarget, outputPath, cmdResult)
}

// testProject runs Unity tests via the Test Runner.
func testProject(ctx context.Context, rc *skillmain.RunContext, projectPath string, in Input) error {
	if err := validateProjectPath(projectPath); err != nil {
		return err
	}

	unityPath, err := resolveUnityPath(ctx, in.UnityPath)
	if err != nil {
		return err
	}

	testPlatform := in.TestPlatform
	if testPlatform == "" {
		testPlatform = "EditMode"
	}

	// Resolve test results path
	testResults := in.TestResults
	if testResults == "" {
		testResults = filepath.Join(projectPath, "TestResults", testPlatform+"-results.xml")
	}
	if !filepath.IsAbs(testResults) {
		testResults = filepath.Join(projectPath, testResults)
	}

	if err := os.MkdirAll(filepath.Dir(testResults), 0o755); err != nil {
		return skillerr.WrapIO("create test results directory", err, skillerr.WithHint("Check write permissions and that the parent directory exists."))
	}

	args := []string{
		"-batchmode", "-nographics",
		"-projectPath", projectPath,
		"-runTests",
		"-testPlatform", testPlatform,
		"-testResults", testResults,
	}

	if in.TestFilter != "" {
		args = append(args, "-testFilter", in.TestFilter)
	}

	cmdResult := executil.Run(ctx, projectPath, unityPath, args...)

	// Check if test results file was created
	resultsExist := false
	if _, statErr := os.Stat(testResults); statErr == nil {
		resultsExist = true
	}

	result := map[string]any{
		"action":        ActionTest,
		"test_platform": testPlatform,
		"exit_code":     cmdResult.ExitCode,
		"duration_ms":   cmdResult.Duration.Milliseconds(),
		"stdout":        string(cmdResult.Stdout),
		"stderr":        string(cmdResult.Stderr),
	}

	if in.TestFilter != "" {
		result["test_filter"] = in.TestFilter
	}

	if resultsExist {
		result["test_results_path"] = testResults
		result["summary"] = fmt.Sprintf("%s tests completed in %v (results: %s)",
			testPlatform, cmdResult.Duration.Round(time.Millisecond), testResults)
	} else if cmdResult.ExitCode != 0 {
		result["summary"] = fmt.Sprintf("%s tests failed with exit code %d", testPlatform, cmdResult.ExitCode)
	} else {
		result["summary"] = fmt.Sprintf("%s tests completed in %v", testPlatform, cmdResult.Duration.Round(time.Millisecond))
	}

	if cmdResult.Err != nil && cmdResult.ExitCode == -1 {
		return skillerr.Runtime(
			"failed to run Unity",
			skillerr.WithCause(cmdResult.Err),
			skillerr.WithHint("Ensure Unity Editor is installed. Provide unity_path or install via Unity Hub."),
		)
	}

	return emitSuccess(rc, result)
}

// exportProject builds and exports a standalone player.
func exportProject(ctx context.Context, rc *skillmain.RunContext, projectPath string, in Input) error {
	if strings.TrimSpace(in.BuildTarget) == "" {
		return skillerr.Arg(
			"build_target is required for export action",
			skillerr.WithHint("Example targets: StandaloneOSX, StandaloneWindows64, StandaloneLinux64, Android, iOS, WebGL"),
		)
	}

	if err := validateProjectPath(projectPath); err != nil {
		return err
	}

	unityPath, err := resolveUnityPath(ctx, in.UnityPath)
	if err != nil {
		return err
	}

	outputPath := in.OutputPath
	if outputPath == "" {
		outputPath = filepath.Join(projectPath, "Builds", in.BuildTarget)
	}
	if !filepath.IsAbs(outputPath) {
		outputPath = filepath.Join(projectPath, outputPath)
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return skillerr.WrapIO("create output directory", err, skillerr.WithHint("Check write permissions and that the parent directory exists."))
	}

	args := []string{
		"-batchmode", "-quit", "-nographics",
		"-projectPath", projectPath,
		"-buildTarget", in.BuildTarget,
	}

	if in.ExecuteMethod != "" {
		args = append(args, "-executeMethod", in.ExecuteMethod)
	}

	if in.Development {
		args = append(args, "-development")
	}

	cmdResult := executil.Run(ctx, projectPath, unityPath, args...)
	return emitBuildResult(rc, ActionExport, in.BuildTarget, outputPath, cmdResult)
}

// cleanProject removes Library/ and build artifacts.
func cleanProject(rc *skillmain.RunContext, projectPath string) error {
	if err := validateProjectPath(projectPath); err != nil {
		return err
	}

	var cleaned []string
	var errs []string

	cleanDirs := []string{
		filepath.Join(projectPath, "Library"),
		filepath.Join(projectPath, "Temp"),
		filepath.Join(projectPath, "Builds"),
		filepath.Join(projectPath, "Logs"),
	}

	for _, dir := range cleanDirs {
		if _, statErr := os.Stat(dir); statErr != nil {
			continue // doesn't exist, skip
		}
		if err := os.RemoveAll(dir); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", filepath.Base(dir), err))
		} else {
			cleaned = append(cleaned, filepath.Base(dir))
		}
	}

	sort.Strings(cleaned)

	result := map[string]any{
		"action":  ActionClean,
		"cleaned": cleaned,
	}

	if len(errs) > 0 {
		result["errors"] = errs
		result["summary"] = fmt.Sprintf("Cleaned %d directories with %d errors", len(cleaned), len(errs))
	} else if len(cleaned) == 0 {
		result["summary"] = "Nothing to clean"
	} else {
		result["summary"] = fmt.Sprintf("Cleaned %d directories: %s", len(cleaned), strings.Join(cleaned, ", "))
	}

	return emitSuccess(rc, result)
}

// validateProjectPath checks that the path looks like a Unity project.
func validateProjectPath(projectPath string) error {
	assetsDir := filepath.Join(projectPath, "Assets")
	if _, err := os.Stat(assetsDir); err != nil {
		return skillerr.NotFound(
			fmt.Sprintf("no Assets/ directory found in %s", projectPath),
			skillerr.WithHint("Ensure project_path points to a Unity project root (must contain Assets/)."),
		)
	}
	return nil
}

// resolveUnityPath finds the Unity Editor executable.
// Priority: explicit path > Unity Hub installed editors > common install paths.
func resolveUnityPath(ctx context.Context, explicit string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", skillerr.NotFound(
				fmt.Sprintf("Unity Editor not found at %s", explicit),
				skillerr.WithHint("Check the unity_path value points to a valid Unity Editor executable."),
			)
		}
		return explicit, nil
	}

	// Try common macOS Hub install paths (newest first via glob sort)
	hubPattern := "/Applications/Unity/Hub/Editor/*/Unity.app/Contents/MacOS/Unity"
	matches, _ := filepath.Glob(hubPattern)
	if len(matches) > 0 {
		// Sort descending to prefer newer versions
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		return matches[0], nil
	}

	// Try standalone macOS install
	standalone := "/Applications/Unity.app/Contents/MacOS/Unity"
	if _, err := os.Stat(standalone); err == nil {
		return standalone, nil
	}

	// Try Unity Hub CLI to list installed editors
	hubCLI := "/Applications/Unity Hub.app/Contents/MacOS/Unity Hub"
	if _, err := os.Stat(hubCLI); err == nil {
		result := executil.Run(ctx, "", hubCLI, "--", "--headless", "editors", "--installed")
		if result.Success() {
			// Parse output lines like "2022.3.10f1 , installed at /Applications/Unity/Hub/Editor/2022.3.10f1/Unity.app/Contents/MacOS/Unity"
			for _, line := range result.Lines() {
				if idx := strings.Index(line, "installed at "); idx >= 0 {
					path := strings.TrimSpace(line[idx+len("installed at "):])
					if _, err := os.Stat(path); err == nil {
						return path, nil
					}
				}
			}
		}
	}

	// Try PATH lookup
	if path, err := exec.LookPath("Unity"); err == nil {
		return path, nil
	}

	return "", skillerr.NotFound(
		"Unity Editor not found",
		skillerr.WithHint("Install Unity via Unity Hub or provide unity_path. Common paths: /Applications/Unity/Hub/Editor/<version>/Unity.app/Contents/MacOS/Unity"),
	)
}

// emitBuildResult emits the result of a build or export operation.
func emitBuildResult(rc *skillmain.RunContext, action, buildTarget, outputPath string, cmdResult executil.CmdResult) error {
	if cmdResult.Err != nil && cmdResult.ExitCode == -1 {
		return skillerr.Runtime(
			"failed to run Unity",
			skillerr.WithCause(cmdResult.Err),
			skillerr.WithHint("Ensure Unity Editor is installed. Provide unity_path or install via Unity Hub."),
		)
	}

	outputExists := false
	var outputSize int64
	if info, statErr := os.Stat(outputPath); statErr == nil {
		outputExists = true
		outputSize = info.Size()
	}

	result := map[string]any{
		"action":        action,
		"build_target":  buildTarget,
		"output_path":   outputPath,
		"output_exists": outputExists,
		"exit_code":     cmdResult.ExitCode,
		"duration_ms":   cmdResult.Duration.Milliseconds(),
		"stdout":        string(cmdResult.Stdout),
		"stderr":        string(cmdResult.Stderr),
	}

	if outputExists {
		result["output_size_bytes"] = outputSize
		result["summary"] = fmt.Sprintf("Built %s to %s (%d bytes) in %v",
			buildTarget, outputPath, outputSize, cmdResult.Duration.Round(time.Millisecond))
	} else if cmdResult.ExitCode != 0 {
		result["summary"] = fmt.Sprintf("Build failed with exit code %d", cmdResult.ExitCode)
	} else {
		result["summary"] = fmt.Sprintf("Build completed but output not found at %s", outputPath)
	}

	return emitSuccess(rc, result)
}

// emitSuccess emits a successful result with the provided data.
func emitSuccess(rc *skillmain.RunContext, result map[string]any) error {
	return skillout.Emit(rc, skillName, result)
}
