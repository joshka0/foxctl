// Package main implements the setup/install skill for programmatic foxctl installation.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/executil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/fsutil"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain/lite"
)

const command = "setup/install"

// input defines the skill input parameters for foxctl installation with provider selection and installation options.
type input struct {
	Provider     string `json:"provider"`
	SkipHooks    bool   `json:"skip_hooks"`
	SkipSkills   bool   `json:"skip_skills"`
	RepoRoot     string `json:"repo_root"`
	ValidateOnly bool   `json:"validate_only"`
}

// output contains the skill result data with installation status, directory information, and configuration details.
type output struct {
	Status       string            `json:"status"`
	Provider     string            `json:"provider,omitempty"`
	Directories  []directoryStatus `json:"directories,omitempty"`
	Hooks        hooksStatus       `json:"hooks,omitempty"`
	Binary       binaryStatus      `json:"binary,omitempty"`
	Environment  []envStatus       `json:"environment,omitempty"`
	Errors       []string          `json:"errors,omitempty"`
	Warnings     []string          `json:"warnings,omitempty"`
	Instructions []string          `json:"instructions,omitempty"`
}

// directoryStatus represents the status of a directory with existence and creation tracking.
type directoryStatus struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Created bool   `json:"created,omitempty"`
}

// hooksStatus represents the installation status of provider hooks with hook enumeration.
type hooksStatus struct {
	Provider  string   `json:"provider"`
	Installed bool     `json:"installed"`
	HookCount int      `json:"hook_count,omitempty"`
	Hooks     []string `json:"hooks,omitempty"`
}

// binaryStatus represents the status of the foxctl binary with path and version information.
type binaryStatus struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Version string `json:"version,omitempty"`
}

// envStatus represents the status of an environment variable with requirement tracking.
type envStatus struct {
	Name     string `json:"name"`
	Set      bool   `json:"set"`
	Required bool   `json:"required"`
}

// main is the skill entry point for setup/install with comprehensive foxctl installation capabilities.
func main() {
	lite.Main(command, run)
}

// run orchestrates foxctl installation with validation and setup operations for multiple providers.
//
// Index:
//
//	Purpose: Install and validate foxctl setup with directory creation, hook installation, and environment configuration
//	Keywords: setup/install, foxctl_installation, provider_hooks, environment_setup, directory_management
//	Related: validate, install, checkHooks, installHooks, checkEnvironment
//	Flow: validate input → execute validation or installation → check directories → install hooks → verify binary → check environment → emit results
//	Resources: filesystem, provider config directories
//	Events: installation events
//	OutputFields: status, provider, directories, hooks, binary, environment
//
// [[domain:foxctl-installation]]
// [[protocol:provider-hook-installation]]
func run(_ context.Context, rc *lite.RunContext, in input) error {
	if in.Provider == "" {
		in.Provider = "claude-code"
	}

	var out output
	if in.ValidateOnly {
		out = validate(in)
	} else {
		out = install(in)
	}

	return lite.Emit(rc, command, out)
}

// validate performs comprehensive validation of foxctl setup without making changes.
func validate(in input) output {
	out := output{Status: "ok", Provider: in.Provider}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		out.Status = "error"
		out.Errors = append(out.Errors, fmt.Sprintf("cannot get home directory: %v", err))
		return out
	}

	foxctlHome := os.Getenv("FOXCTL_HOME")
	if foxctlHome == "" {
		foxctlHome = filepath.Join(homeDir, ".foxctl")
	}

	dirs := []string{
		foxctlHome,
		filepath.Join(foxctlHome, "storage"),
		filepath.Join(foxctlHome, "skills"),
		filepath.Join(foxctlHome, "cache"),
		filepath.Join(foxctlHome, "cas"),
	}

	for _, dir := range dirs {
		status := directoryStatus{Path: dir, Exists: false}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			status.Exists = true
		}
		out.Directories = append(out.Directories, status)
		if !status.Exists {
			out.Warnings = append(out.Warnings, fmt.Sprintf("directory missing: %s", dir))
		}
	}

	binaryPath, err := exec.LookPath("foxctl")
	if err != nil {
		out.Binary = binaryStatus{Exists: false}
		out.Warnings = append(out.Warnings, "foxctl binary not found in PATH")
	} else {
		out.Binary = binaryStatus{Path: binaryPath, Exists: true}
		result := executil.Run(context.Background(), "", binaryPath, "--version")
		if result.Err == nil {
			out.Binary.Version = strings.TrimSpace(string(result.Stdout))
		}
	}

	out.Hooks = checkHooks(homeDir, in.Provider)
	if !out.Hooks.Installed {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%s hooks not installed", in.Provider))
	}

	out.Environment = checkEnvironment()
	for _, env := range out.Environment {
		if env.Required && !env.Set {
			out.Warnings = append(out.Warnings, fmt.Sprintf("required environment variable not set: %s", env.Name))
		}
	}

	if len(out.Errors) > 0 {
		out.Status = "error"
	} else if len(out.Warnings) > 0 {
		out.Status = "warning"
	}

	return out
}

// install performs complete foxctl setup with directory creation, hook installation, and configuration.
func install(in input) output {
	out := output{Status: "ok", Provider: in.Provider}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		out.Status = "error"
		out.Errors = append(out.Errors, fmt.Sprintf("cannot get home directory: %v", err))
		return out
	}

	foxctlHome := os.Getenv("FOXCTL_HOME")
	if foxctlHome == "" {
		foxctlHome = filepath.Join(homeDir, ".foxctl")
	}

	dirs := []string{
		foxctlHome,
		filepath.Join(foxctlHome, "storage"),
		filepath.Join(foxctlHome, "skills"),
		filepath.Join(foxctlHome, "cache"),
		filepath.Join(foxctlHome, "cas"),
		filepath.Join(homeDir, ".local", "bin"),
	}

	for _, dir := range dirs {
		status := directoryStatus{Path: dir, Exists: false}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			status.Exists = true
		} else {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				out.Errors = append(out.Errors, fmt.Sprintf("create directory %s: %v", dir, err))
			} else {
				status.Exists = true
				status.Created = true
			}
		}
		out.Directories = append(out.Directories, status)
	}

	if !in.SkipHooks {
		out.Hooks = installHooks(homeDir, in.Provider, in.RepoRoot)
		if !out.Hooks.Installed {
			out.Errors = append(out.Errors, fmt.Sprintf("failed to install %s hooks", in.Provider))
		}
	}

	binaryPath, err := exec.LookPath("foxctl")
	if err != nil {
		out.Binary = binaryStatus{Exists: false}
		out.Instructions = append(out.Instructions, fmt.Sprintf("Add %s to your PATH", filepath.Join(homeDir, ".local", "bin")))
	} else {
		out.Binary = binaryStatus{Path: binaryPath, Exists: true}
		result := executil.Run(context.Background(), "", binaryPath, "--version")
		if result.Err == nil {
			out.Binary.Version = strings.TrimSpace(string(result.Stdout))
		}
	}

	out.Environment = checkEnvironment()
	for _, env := range out.Environment {
		if env.Required && !env.Set {
			out.Instructions = append(out.Instructions, fmt.Sprintf("Set %s environment variable", env.Name))
		}
	}

	if len(out.Errors) > 0 {
		out.Status = "error"
	}

	return out
}

// checkHooks validates the installation status of provider hooks with detailed enumeration.
func checkHooks(homeDir, provider string) hooksStatus {
	status := hooksStatus{Provider: provider}

	switch provider {
	case "claude-code", "claude":
		hooksDir := filepath.Join(homeDir, ".claude", "hooks", "foxctl")
		if entries, err := os.ReadDir(hooksDir); err == nil && len(entries) > 0 {
			status.Installed = true
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".sh") {
					status.Hooks = append(status.Hooks, e.Name())
				}
			}
			status.HookCount = len(status.Hooks)
		}
	case "opencode":
		pluginDir := filepath.Join(homeDir, ".opencode", "plugins", "foxctl")
		if _, err := os.Stat(pluginDir); err == nil {
			status.Installed = true
			status.HookCount = 1
			status.Hooks = []string{"foxctl-opencode-hooks"}
		}
	case "codex":
		agentsFile := filepath.Join(homeDir, ".codex", "AGENTS.md")
		if _, err := os.Stat(agentsFile); err == nil {
			status.Installed = true
			status.HookCount = 1
			status.Hooks = []string{"AGENTS.md"}
		}
	case "all":
		claudeHooks := checkHooks(homeDir, "claude-code")
		opencodeHooks := checkHooks(homeDir, "opencode")
		codexHooks := checkHooks(homeDir, "codex")
		status.Installed = claudeHooks.Installed || opencodeHooks.Installed || codexHooks.Installed
		status.HookCount = claudeHooks.HookCount + opencodeHooks.HookCount + codexHooks.HookCount
	}

	return status
}

// installHooks installs provider-specific hooks with support for multiple IDE providers.
func installHooks(homeDir, provider, repoRoot string) hooksStatus {
	status := hooksStatus{Provider: provider}

	if repoRoot == "" {
		repoRoot = findRepoRoot(homeDir)
	}

	switch provider {
	case "claude-code", "claude":
		status = installClaudeCodeHooks(homeDir, repoRoot)
	case "opencode":
		status = installOpenCodeHooks(homeDir, repoRoot)
	case "codex":
		status = installCodexHooks(homeDir, repoRoot)
	case "all":
		claudeStatus := installClaudeCodeHooks(homeDir, repoRoot)
		opencodeStatus := installOpenCodeHooks(homeDir, repoRoot)
		codexStatus := installCodexHooks(homeDir, repoRoot)
		status.Installed = claudeStatus.Installed || opencodeStatus.Installed || codexStatus.Installed
		status.HookCount = claudeStatus.HookCount + opencodeStatus.HookCount + codexStatus.HookCount
	}

	return status
}

// findRepoRoot attempts to locate the foxctl repository root using multiple search strategies.
func findRepoRoot(homeDir string) string {
	if exePath, err := os.Executable(); err == nil {
		dir := filepath.Dir(exePath)
		for i := 0; i < 5; i++ {
			if _, err := os.Stat(filepath.Join(dir, "Makefile")); err == nil {
				if _, err := os.Stat(filepath.Join(dir, "configs")); err == nil {
					return dir
				}
			}
			dir = filepath.Dir(dir)
		}
	}

	candidates := []string{
		filepath.Join(homeDir, "repos", "personal", "foxctl"),
		filepath.Join(homeDir, "code", "foxctl"),
		filepath.Join(homeDir, "src", "foxctl"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "Makefile")); err == nil {
			return c
		}
	}

	return ""
}

// installClaudeCodeHooks installs Claude Code provider hooks with symlink creation and settings merging.
func installClaudeCodeHooks(homeDir, repoRoot string) hooksStatus {
	status := hooksStatus{Provider: "claude-code"}

	if repoRoot == "" {
		return status
	}

	claudeDir := filepath.Join(homeDir, ".claude")
	hooksDir := filepath.Join(claudeDir, "hooks", "foxctl")
	sourceDir := filepath.Join(repoRoot, "configs", "hooks")

	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return status
	}

	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return status
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sh") {
			continue
		}

		// Skip if source is already a symlink (prevents loops)
		if fsutil.IsSymlinkMode(entry.Type()) {
			continue
		}

		source := filepath.Join(sourceDir, entry.Name())
		target := filepath.Join(hooksDir, entry.Name())

		// Resolve source to catch any existing symlink issues
		resolvedSource, err := filepath.EvalSymlinks(source)
		if err != nil {
			// Source doesn't resolve properly, skip it
			continue
		}

		// Prevent circular references: resolved source shouldn't point to target dir
		if strings.HasPrefix(resolvedSource, hooksDir) {
			continue
		}

		os.Remove(target)
		if err := os.Symlink(source, target); err == nil {
			status.Hooks = append(status.Hooks, entry.Name())
		}
	}

	status.HookCount = len(status.Hooks)
	status.Installed = status.HookCount > 0

	settingsSource := filepath.Join(repoRoot, "configs", "claude-settings.json")
	settingsTarget := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(settingsSource); err == nil {
		if err := mergeSettings(settingsSource, settingsTarget); err != nil {
			// Best-effort; hook install succeeds even if settings merge fails.
			_ = err
		}
	}

	return status
}

// installOpenCodeHooks installs OpenCode provider hooks with build process and plugin linking.
func installOpenCodeHooks(homeDir, repoRoot string) hooksStatus {
	status := hooksStatus{Provider: "opencode"}

	if repoRoot == "" {
		return status
	}

	pluginsDir := filepath.Join(homeDir, ".opencode", "plugins")
	sourceDir := filepath.Join(repoRoot, "configs", "opencode-hooks")

	if _, err := os.Stat(sourceDir); err != nil {
		return status
	}

	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		return status
	}

	distDir := filepath.Join(sourceDir, "dist")
	if _, err := os.Stat(distDir); err != nil {
		var args []string
		if _, err := exec.LookPath("bun"); err == nil {
			args = []string{"bun", "run", "build"}
		} else if _, err := exec.LookPath("npm"); err == nil {
			args = []string{"npm", "run", "build"}
		}
		if len(args) > 0 {
			_ = executil.Run(context.Background(), sourceDir, args[0], args[1:]...)
		}
	}

	// Verify source isn't a symlink and resolves properly
	resolvedSource, err := filepath.EvalSymlinks(sourceDir)
	if err != nil {
		return status
	}

	// Prevent circular references
	if strings.HasPrefix(resolvedSource, pluginsDir) {
		return status
	}

	target := filepath.Join(pluginsDir, "foxctl")
	os.Remove(target)
	if err := os.Symlink(sourceDir, target); err == nil {
		status.Installed = true
		status.HookCount = 1
		status.Hooks = []string{"foxctl-opencode-hooks"}
	}

	return status
}

// installCodexHooks installs Codex provider hooks with AGENTS.md file copying.
func installCodexHooks(homeDir, repoRoot string) hooksStatus {
	status := hooksStatus{Provider: "codex"}

	if repoRoot == "" {
		return status
	}

	codexDir := filepath.Join(homeDir, ".codex")
	sourceFile := filepath.Join(repoRoot, "configs", "codex", "AGENTS.md")
	targetFile := filepath.Join(codexDir, "AGENTS.md")

	if _, err := os.Stat(sourceFile); err != nil {
		return status
	}

	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		return status
	}

	content, err := os.ReadFile(sourceFile)
	if err != nil {
		return status
	}

	if err := os.WriteFile(targetFile, content, 0o644); err == nil {
		status.Installed = true
		status.HookCount = 1
		status.Hooks = []string{"AGENTS.md"}
	}

	return status
}

// mergeSettings merges Claude Code settings files with hook configuration preservation.
func mergeSettings(sourceFile, targetFile string) error {
	sourceData, err := os.ReadFile(sourceFile)
	if err != nil {
		return err
	}

	var source map[string]interface{}
	if err := json.Unmarshal(sourceData, &source); err != nil {
		return err
	}

	if _, err := os.Stat(targetFile); err != nil {
		return os.WriteFile(targetFile, sourceData, 0o600)
	}

	targetData, err := os.ReadFile(targetFile)
	if err != nil {
		return err
	}

	var target map[string]interface{}
	if err := json.Unmarshal(targetData, &target); err != nil {
		return err
	}

	sourceHooks, _ := source["hooks"].(map[string]interface{})
	targetHooks, _ := target["hooks"].(map[string]interface{})

	if targetHooks == nil {
		targetHooks = make(map[string]interface{})
	}

	for k, v := range sourceHooks {
		if existingVal, exists := targetHooks[k]; exists {
			existingArr, ok1 := existingVal.([]interface{})
			newArr, ok2 := v.([]interface{})
			if ok1 && ok2 {
				targetHooks[k] = append(existingArr, newArr...)
				continue
			}
		}
		targetHooks[k] = v
	}

	target["hooks"] = targetHooks

	merged, err := json.MarshalIndent(target, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(targetFile, merged, 0o600)
}

// checkEnvironment validates required and optional environment variables for foxctl operation.
func checkEnvironment() []envStatus {
	envVars := []struct {
		name     string
		required bool
	}{
		{"FOXCTL_EMBEDDING_PROVIDER", false},
		{"FOXCTL_EMBEDDING_MODEL", false},
		{"FOXCTL_EMBEDDING_BASE_URL", false},
		{"ANTHROPIC_API_KEY", false},
		{"FOXCTL_HOME", false},
	}

	var statuses []envStatus
	for _, env := range envVars {
		statuses = append(statuses, envStatus{
			Name:     env.name,
			Set:      os.Getenv(env.name) != "",
			Required: env.required,
		})
	}

	return statuses
}

var _ = skillerr.Arg
