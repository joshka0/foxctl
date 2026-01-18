// Package main implements the setup/install skill for programmatic agentctl installation.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/fsutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

const command = "setup/install"

type input struct {
	Provider     string `json:"provider"`
	SkipHooks    bool   `json:"skip_hooks"`
	SkipSkills   bool   `json:"skip_skills"`
	RepoRoot     string `json:"repo_root"`
	ValidateOnly bool   `json:"validate_only"`
}

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

type directoryStatus struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Created bool   `json:"created,omitempty"`
}

type hooksStatus struct {
	Provider  string   `json:"provider"`
	Installed bool     `json:"installed"`
	HookCount int      `json:"hook_count,omitempty"`
	Hooks     []string `json:"hooks,omitempty"`
}

type binaryStatus struct {
	Path    string `json:"path"`
	Exists  bool   `json:"exists"`
	Version string `json:"version,omitempty"`
}

type envStatus struct {
	Name     string `json:"name"`
	Set      bool   `json:"set"`
	Required bool   `json:"required"`
}

func main() {
	skillmain.Main(command, run)
}

func run(_ context.Context, rc *skillmain.RunContext, in input) error {
	if in.Provider == "" {
		in.Provider = "claude-code"
	}

	var out output
	if in.ValidateOnly {
		out = validate(in)
	} else {
		out = install(in)
	}

	return skillout.Emit(rc, command, out)
}

func validate(in input) output {
	out := output{Status: "ok", Provider: in.Provider}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		out.Status = "error"
		out.Errors = append(out.Errors, fmt.Sprintf("cannot get home directory: %v", err))
		return out
	}

	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		agentctlHome = filepath.Join(homeDir, ".agentctl")
	}

	dirs := []string{
		agentctlHome,
		filepath.Join(agentctlHome, "storage"),
		filepath.Join(agentctlHome, "skills"),
		filepath.Join(agentctlHome, "cache"),
		filepath.Join(agentctlHome, "cas"),
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

	binaryPath, err := exec.LookPath("agentctl")
	if err != nil {
		out.Binary = binaryStatus{Exists: false}
		out.Warnings = append(out.Warnings, "agentctl binary not found in PATH")
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

func install(in input) output {
	out := output{Status: "ok", Provider: in.Provider}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		out.Status = "error"
		out.Errors = append(out.Errors, fmt.Sprintf("cannot get home directory: %v", err))
		return out
	}

	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		agentctlHome = filepath.Join(homeDir, ".agentctl")
	}

	dirs := []string{
		agentctlHome,
		filepath.Join(agentctlHome, "storage"),
		filepath.Join(agentctlHome, "skills"),
		filepath.Join(agentctlHome, "cache"),
		filepath.Join(agentctlHome, "cas"),
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

	binaryPath, err := exec.LookPath("agentctl")
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

func checkHooks(homeDir, provider string) hooksStatus {
	status := hooksStatus{Provider: provider}

	switch provider {
	case "claude-code", "claude":
		hooksDir := filepath.Join(homeDir, ".claude", "hooks", "agentctl")
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
		pluginDir := filepath.Join(homeDir, ".opencode", "plugins", "agentctl")
		if _, err := os.Stat(pluginDir); err == nil {
			status.Installed = true
			status.HookCount = 1
			status.Hooks = []string{"agentctl-opencode-hooks"}
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
		filepath.Join(homeDir, "repos", "personal", "agentctl"),
		filepath.Join(homeDir, "code", "agentctl"),
		filepath.Join(homeDir, "src", "agentctl"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "Makefile")); err == nil {
			return c
		}
	}

	return ""
}

func installClaudeCodeHooks(homeDir, repoRoot string) hooksStatus {
	status := hooksStatus{Provider: "claude-code"}

	if repoRoot == "" {
		return status
	}

	claudeDir := filepath.Join(homeDir, ".claude")
	hooksDir := filepath.Join(claudeDir, "hooks", "agentctl")
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

	target := filepath.Join(pluginsDir, "agentctl")
	os.Remove(target)
	if err := os.Symlink(sourceDir, target); err == nil {
		status.Installed = true
		status.HookCount = 1
		status.Hooks = []string{"agentctl-opencode-hooks"}
	}

	return status
}

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

func checkEnvironment() []envStatus {
	envVars := []struct {
		name     string
		required bool
	}{
		{"VOYAGE_API_KEY", true},
		{"ANTHROPIC_API_KEY", false},
		{"AGENTCTL_HOME", false},
		{"AGENTCTL_SEMANTIC_RERANK", false},
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
