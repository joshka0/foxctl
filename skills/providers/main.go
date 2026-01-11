// Package main implements the providers/config skill.
// Unified configuration management for AI coding assistants.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
)

// Provider represents a supported AI coding assistant.
type Provider struct {
	Name         string `json:"name"`
	ConfigPath   string `json:"config_path"`
	ConfigType   string `json:"config_type"` // json, toml
	SkillsPath   string `json:"skills_path,omitempty"`
	HooksPath    string `json:"hooks_path,omitempty"`
	CommandsPath string `json:"commands_path,omitempty"`
	MCPKey       string `json:"mcp_key"` // key in config for MCP servers
	Installed    bool   `json:"installed"`
	Version      string `json:"version,omitempty"`
}

// Providers registry
var providers = map[string]Provider{
	"claude": {
		Name:         "claude",
		ConfigPath:   "~/.claude.json",
		ConfigType:   "json",
		SkillsPath:   "~/.claude/skills",
		HooksPath:    "~/.claude/hooks",
		CommandsPath: "~/.claude/commands",
		MCPKey:       "mcpServers",
	},
	"codex": {
		Name:       "codex",
		ConfigPath: "~/.codex/config.toml",
		ConfigType: "toml",
		SkillsPath: "~/.codex/skills",
		MCPKey:     "mcp_servers",
	},
	"opencode": {
		Name:         "opencode",
		ConfigPath:   "~/.config/opencode/opencode.json",
		ConfigType:   "json",
		SkillsPath:   "~/.config/opencode/agent",
		CommandsPath: "~/.config/opencode/commands",
		MCPKey:       "mcpServers",
	},
	"factory": {
		Name:         "factory",
		ConfigPath:   "~/.factory/settings.json",
		ConfigType:   "json",
		SkillsPath:   "~/.factory/droids",
		CommandsPath: "~/.factory/commands",
		MCPKey:       "mcpServers", // Actually in mcp.json
	},
	"gemini": {
		Name:       "gemini",
		ConfigPath: "~/.gemini/settings.json",
		ConfigType: "json",
		MCPKey:     "mcpServers",
	},
}

type input struct {
	Operation  string         `json:"operation"`
	Provider   string         `json:"provider"`
	MCP        *mcpConfig     `json:"mcp,omitempty"`
	Skill      *skillConfig   `json:"skill,omitempty"`
	Setting    *settingConfig `json:"setting,omitempty"`
	SyncConfig *syncConfig    `json:"sync_config,omitempty"`
	File       string         `json:"file,omitempty"`
	DryRun     bool           `json:"dry_run"`
}

type mcpConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Type    string            `json:"type,omitempty"` // stdio, http, sse
	URL     string            `json:"url,omitempty"`
	Scope   string            `json:"scope,omitempty"` // user, project
}

type skillConfig struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

type settingConfig struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
}

type syncConfig struct {
	From string   `json:"from"`
	To   []string `json:"to"`
	What []string `json:"what"` // mcp, settings, skills, all
}

type output struct {
	Providers []providerStatus `json:"providers,omitempty"`
	Config    interface{}      `json:"config,omitempty"`
	Changes   []change         `json:"changes,omitempty"`
	Errors    []string         `json:"errors,omitempty"`
}

type providerStatus struct {
	Name         string `json:"name"`
	Installed    bool   `json:"installed"`
	ConfigExists bool   `json:"config_exists"`
	ConfigPath   string `json:"config_path"`
	SkillsPath   string `json:"skills_path,omitempty"`
	SkillCount   int    `json:"skill_count,omitempty"`
	MCPCount     int    `json:"mcp_count,omitempty"`
}

type change struct {
	Provider string `json:"provider"`
	Type     string `json:"type"` // add_mcp, remove_mcp, add_skill, set, etc.
	Target   string `json:"target"`
	Applied  bool   `json:"applied"`
}

func main() {
	skillmain.Main(command, run)
}

func run(_ context.Context, rc *skillmain.RunContext, in input) error {
	// Default provider
	if in.Provider == "" {
		in.Provider = "claude"
	}

	var out *output
	var err error

	switch in.Operation {
	case "list":
		out, err = listProviders()
	case "get":
		out, err = getConfig(in.Provider)
	case "set":
		out, err = setConfig(in.Provider, in.Setting, in.DryRun)
	case "add-mcp":
		out, err = addMCP(in.Provider, in.MCP, in.DryRun)
	case "remove-mcp":
		if in.MCP == nil {
			return fmt.Errorf("mcp config is required for remove-mcp operation")
		}
		out, err = removeMCP(in.Provider, in.MCP.Name, in.DryRun)
	case "add-skill":
		out, err = addSkill(in.Provider, in.Skill, in.DryRun)
	case "remove-skill":
		if in.Skill == nil {
			return fmt.Errorf("skill config is required for remove-skill operation")
		}
		out, err = removeSkill(in.Provider, in.Skill.Name, in.DryRun)
	case "sync":
		out, err = syncProviders(in.SyncConfig, in.DryRun)
	case "export":
		out, err = exportConfig(in.Provider, in.File)
	case "import":
		out, err = importConfig(in.Provider, in.File, in.DryRun)
	default:
		return fmt.Errorf("unknown operation: %s", in.Operation)
	}

	if err != nil {
		return err
	}

	return skillout.Emit(rc, command, out)
}

func listProviders() (*output, error) {
	var statuses []providerStatus

	for name, p := range providers {
		path := expandPath(p.ConfigPath)
		configExists := fileExists(path)

		status := providerStatus{
			Name:         name,
			Installed:    configExists || dirExists(filepath.Dir(path)),
			ConfigExists: configExists,
			ConfigPath:   path,
		}

		// Count skills if path exists
		if p.SkillsPath != "" {
			skillsPath := expandPath(p.SkillsPath)
			status.SkillsPath = skillsPath
			if entries, err := os.ReadDir(skillsPath); err == nil {
				status.SkillCount = len(entries)
			}
		}

		// Count MCP servers
		if configExists {
			if mcpCount, err := countMCPServers(path, p.ConfigType, p.MCPKey); err == nil {
				status.MCPCount = mcpCount
			}
		}

		statuses = append(statuses, status)
	}

	return &output{Providers: statuses}, nil
}

func getConfig(providerName string) (*output, error) {
	p, ok := providers[providerName]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	path := expandPath(p.ConfigPath)
	if !fileExists(path) {
		return &output{Config: map[string]interface{}{"error": "config not found"}}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg interface{}
	if p.ConfigType == "toml" {
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, err
		}
	}

	return &output{Config: cfg}, nil
}

func addMCP(providerName string, mcp *mcpConfig, dryRun bool) (*output, error) {
	if mcp == nil || mcp.Name == "" {
		return nil, fmt.Errorf("mcp config with name is required")
	}

	targetProviders := getTargetProviders(providerName)
	var changes []change
	var errors []string

	for _, pName := range targetProviders {
		p := providers[pName]
		path := expandPath(p.ConfigPath)

		// Special case for Factory - uses separate mcp.json
		if pName == "factory" {
			path = expandPath("~/.factory/mcp.json")
		}

		ch := change{
			Provider: pName,
			Type:     "add_mcp",
			Target:   mcp.Name,
		}

		if dryRun {
			ch.Applied = false
			changes = append(changes, ch)
			continue
		}

		// Load or create config
		cfg := make(map[string]interface{})
		if fileExists(path) {
			data, err := os.ReadFile(path)
			if err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
				continue
			}
			if p.ConfigType == "toml" {
				if _, err := toml.Decode(string(data), &cfg); err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
					continue
				}
			} else {
				if err := json.Unmarshal(data, &cfg); err != nil {
					errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
					continue
				}
			}
		}

		// Get or create MCP servers map
		mcpKey := p.MCPKey
		if pName == "factory" {
			mcpKey = "mcpServers"
		}

		mcpServers, ok := cfg[mcpKey].(map[string]interface{})
		if !ok {
			mcpServers = make(map[string]interface{})
		}

		// Build MCP server entry
		serverEntry := buildMCPEntry(mcp, pName)
		mcpServers[mcp.Name] = serverEntry
		cfg[mcpKey] = mcpServers

		// Write config
		if err := writeConfig(path, cfg, p.ConfigType); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
			continue
		}

		ch.Applied = true
		changes = append(changes, ch)
	}

	out := &output{Changes: changes}
	if len(errors) > 0 {
		out.Errors = errors
	}
	return out, nil
}

func removeMCP(providerName string, mcpName string, dryRun bool) (*output, error) {
	if mcpName == "" {
		return nil, fmt.Errorf("mcp name is required")
	}

	targetProviders := getTargetProviders(providerName)
	var changes []change
	var errors []string

	for _, pName := range targetProviders {
		p := providers[pName]
		path := expandPath(p.ConfigPath)
		if pName == "factory" {
			path = expandPath("~/.factory/mcp.json")
		}

		ch := change{
			Provider: pName,
			Type:     "remove_mcp",
			Target:   mcpName,
		}

		if !fileExists(path) {
			continue
		}

		if dryRun {
			ch.Applied = false
			changes = append(changes, ch)
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
			continue
		}

		cfg := make(map[string]interface{})
		if p.ConfigType == "toml" {
			if _, err := toml.Decode(string(data), &cfg); err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
				continue
			}
		} else {
			if err := json.Unmarshal(data, &cfg); err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
				continue
			}
		}

		mcpKey := p.MCPKey
		if pName == "factory" {
			mcpKey = "mcpServers"
		}

		if mcpServers, ok := cfg[mcpKey].(map[string]interface{}); ok {
			delete(mcpServers, mcpName)
			cfg[mcpKey] = mcpServers

			if err := writeConfig(path, cfg, p.ConfigType); err != nil {
				errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
				continue
			}
			ch.Applied = true
		}

		changes = append(changes, ch)
	}

	out := &output{Changes: changes}
	if len(errors) > 0 {
		out.Errors = errors
	}
	return out, nil
}

func addSkill(providerName string, skill *skillConfig, dryRun bool) (*output, error) {
	if skill == nil || skill.Name == "" || skill.Source == "" {
		return nil, fmt.Errorf("skill name and source are required")
	}

	targetProviders := getTargetProviders(providerName)
	var changes []change
	var errors []string

	for _, pName := range targetProviders {
		p := providers[pName]
		if p.SkillsPath == "" {
			continue
		}

		skillsDir := expandPath(p.SkillsPath)
		targetPath := filepath.Join(skillsDir, skill.Name)
		sourcePath := expandPath(skill.Source)

		ch := change{
			Provider: pName,
			Type:     "add_skill",
			Target:   skill.Name,
		}

		if dryRun {
			ch.Applied = false
			changes = append(changes, ch)
			continue
		}

		// Ensure skills directory exists
		if err := os.MkdirAll(skillsDir, 0o755); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
			continue
		}

		// Remove existing if it's a symlink
		if info, err := os.Lstat(targetPath); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				os.Remove(targetPath)
			}
		}

		// Create symlink
		if err := os.Symlink(sourcePath, targetPath); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
			continue
		}

		ch.Applied = true
		changes = append(changes, ch)
	}

	out := &output{Changes: changes}
	if len(errors) > 0 {
		out.Errors = errors
	}
	return out, nil
}

func removeSkill(providerName string, skillName string, dryRun bool) (*output, error) {
	if skillName == "" {
		return nil, fmt.Errorf("skill name is required")
	}

	targetProviders := getTargetProviders(providerName)
	var changes []change
	var errors []string

	for _, pName := range targetProviders {
		p := providers[pName]
		if p.SkillsPath == "" {
			continue
		}

		skillPath := filepath.Join(expandPath(p.SkillsPath), skillName)

		ch := change{
			Provider: pName,
			Type:     "remove_skill",
			Target:   skillName,
		}

		if !fileExists(skillPath) && !dirExists(skillPath) {
			continue
		}

		if dryRun {
			ch.Applied = false
			changes = append(changes, ch)
			continue
		}

		if err := os.RemoveAll(skillPath); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", pName, err))
			continue
		}

		ch.Applied = true
		changes = append(changes, ch)
	}

	out := &output{Changes: changes}
	if len(errors) > 0 {
		out.Errors = errors
	}
	return out, nil
}

func setConfig(providerName string, setting *settingConfig, dryRun bool) (*output, error) {
	if setting == nil || setting.Key == "" {
		return nil, fmt.Errorf("setting key is required")
	}

	p, ok := providers[providerName]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	path := expandPath(p.ConfigPath)

	ch := change{
		Provider: providerName,
		Type:     "set",
		Target:   setting.Key,
	}

	if dryRun {
		return &output{Changes: []change{ch}}, nil
	}

	// Load or create config
	cfg := make(map[string]interface{})
	if fileExists(path) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if p.ConfigType == "toml" {
			if _, err := toml.Decode(string(data), &cfg); err != nil {
				return nil, err
			}
		} else {
			if err := json.Unmarshal(data, &cfg); err != nil {
				return nil, err
			}
		}
	}

	// Set nested key using dot notation
	setNestedValue(cfg, setting.Key, setting.Value)

	if err := writeConfig(path, cfg, p.ConfigType); err != nil {
		return nil, err
	}

	ch.Applied = true
	return &output{Changes: []change{ch}}, nil
}

func syncProviders(sc *syncConfig, dryRun bool) (*output, error) {
	if sc == nil || sc.From == "" || len(sc.To) == 0 {
		return nil, fmt.Errorf("sync_config with from and to is required")
	}

	sourceProvider, ok := providers[sc.From]
	if !ok {
		return nil, fmt.Errorf("unknown source provider: %s", sc.From)
	}

	var changes []change
	var errors []string

	// What to sync
	syncMCP := contains(sc.What, "mcp") || contains(sc.What, "all")
	syncSkills := contains(sc.What, "skills") || contains(sc.What, "all")

	// Get source MCP servers
	var sourceMCPs map[string]interface{}
	if syncMCP {
		sourcePath := expandPath(sourceProvider.ConfigPath)
		if fileExists(sourcePath) {
			data, _ := os.ReadFile(sourcePath)
			var cfg map[string]interface{}
			if err := json.Unmarshal(data, &cfg); err == nil {
				if mcps, ok := cfg[sourceProvider.MCPKey].(map[string]interface{}); ok {
					sourceMCPs = mcps
				}
			}
		}
	}

	// Get source skills
	var sourceSkills []string
	if syncSkills && sourceProvider.SkillsPath != "" {
		skillsPath := expandPath(sourceProvider.SkillsPath)
		if entries, err := os.ReadDir(skillsPath); err == nil {
			for _, e := range entries {
				sourceSkills = append(sourceSkills, e.Name())
			}
		}
	}

	// Apply to target providers
	for _, targetName := range sc.To {
		targetProvider, ok := providers[targetName]
		if !ok {
			errors = append(errors, fmt.Sprintf("unknown target provider: %s", targetName))
			continue
		}

		// Sync MCP servers
		if syncMCP && len(sourceMCPs) > 0 {
			for name, mcpCfg := range sourceMCPs {
				ch := change{
					Provider: targetName,
					Type:     "sync_mcp",
					Target:   name,
				}

				if dryRun {
					changes = append(changes, ch)
					continue
				}

				// Convert and add MCP
				mcp := convertMCPConfig(mcpCfg, name)
				if _, err := addMCP(targetName, mcp, false); err != nil {
					errors = append(errors, fmt.Sprintf("%s/%s: %v", targetName, name, err))
					continue
				}
				ch.Applied = true
				changes = append(changes, ch)
			}
		}

		// Sync skills
		if syncSkills && targetProvider.SkillsPath != "" {
			sourceSkillsPath := expandPath(sourceProvider.SkillsPath)
			for _, skillName := range sourceSkills {
				sourcePath := filepath.Join(sourceSkillsPath, skillName)

				// Resolve symlink to get actual source
				if target, err := os.Readlink(sourcePath); err == nil {
					sourcePath = target
				}

				skill := &skillConfig{
					Name:   skillName,
					Source: sourcePath,
				}

				ch := change{
					Provider: targetName,
					Type:     "sync_skill",
					Target:   skillName,
				}

				if dryRun {
					changes = append(changes, ch)
					continue
				}

				if _, err := addSkill(targetName, skill, false); err != nil {
					errors = append(errors, fmt.Sprintf("%s/%s: %v", targetName, skillName, err))
					continue
				}
				ch.Applied = true
				changes = append(changes, ch)
			}
		}
	}

	out := &output{Changes: changes}
	if len(errors) > 0 {
		out.Errors = errors
	}
	return out, nil
}

func exportConfig(providerName string, filePath string) (*output, error) {
	if filePath == "" {
		filePath = fmt.Sprintf("%s-config-export.json", providerName)
	}

	result, err := getConfig(providerName)
	if err != nil {
		return nil, err
	}

	data, err := json.MarshalIndent(result.Config, "", "  ")
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return nil, err
	}

	return &output{
		Changes: []change{{
			Provider: providerName,
			Type:     "export",
			Target:   filePath,
			Applied:  true,
		}},
	}, nil
}

func importConfig(providerName string, filePath string, dryRun bool) (*output, error) {
	if filePath == "" {
		return nil, fmt.Errorf("file path is required for import")
	}

	p, ok := providers[providerName]
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var importCfg map[string]interface{}
	if err := json.Unmarshal(data, &importCfg); err != nil {
		return nil, err
	}

	ch := change{
		Provider: providerName,
		Type:     "import",
		Target:   filePath,
	}

	if dryRun {
		return &output{Changes: []change{ch}}, nil
	}

	path := expandPath(p.ConfigPath)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	if err := writeConfig(path, importCfg, p.ConfigType); err != nil {
		return nil, err
	}

	ch.Applied = true
	return &output{Changes: []change{ch}}, nil
}

// Helper functions

func getTargetProviders(providerName string) []string {
	if providerName == "all" {
		var names []string
		for name := range providers {
			names = append(names, name)
		}
		return names
	}
	return []string{providerName}
}

func buildMCPEntry(mcp *mcpConfig, providerName string) map[string]interface{} {
	entry := make(map[string]interface{})

	// Different providers have slightly different formats
	switch providerName {
	case "codex":
		// Codex uses different key names
		if mcp.Command != "" {
			entry["command"] = mcp.Command
		}
		if len(mcp.Args) > 0 {
			entry["args"] = mcp.Args
		}
		if len(mcp.Env) > 0 {
			entry["env"] = mcp.Env
		}
	default:
		// Standard MCP format (Claude, OpenCode, Factory, Gemini)
		if mcp.Type != "" && mcp.Type != "stdio" {
			entry["type"] = mcp.Type
		}
		if mcp.Command != "" {
			entry["command"] = mcp.Command
		}
		if len(mcp.Args) > 0 {
			entry["args"] = mcp.Args
		}
		if len(mcp.Env) > 0 {
			entry["env"] = mcp.Env
		}
		if mcp.URL != "" {
			entry["url"] = mcp.URL
		}
	}

	return entry
}

func convertMCPConfig(cfg interface{}, name string) *mcpConfig {
	mcp := &mcpConfig{Name: name}

	if m, ok := cfg.(map[string]interface{}); ok {
		if cmd, ok := m["command"].(string); ok {
			mcp.Command = cmd
		}
		if args, ok := m["args"].([]interface{}); ok {
			for _, a := range args {
				if s, ok := a.(string); ok {
					mcp.Args = append(mcp.Args, s)
				}
			}
		}
		if env, ok := m["env"].(map[string]interface{}); ok {
			mcp.Env = make(map[string]string)
			for k, v := range env {
				if s, ok := v.(string); ok {
					mcp.Env[k] = s
				}
			}
		}
		if t, ok := m["type"].(string); ok {
			mcp.Type = t
		}
		if url, ok := m["url"].(string); ok {
			mcp.URL = url
		}
	}

	return mcp
}

func countMCPServers(path, configType, mcpKey string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	var cfg map[string]interface{}
	if configType == "toml" {
		if _, err := toml.Decode(string(data), &cfg); err != nil {
			return 0, err
		}
	} else {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return 0, err
		}
	}

	if mcps, ok := cfg[mcpKey].(map[string]interface{}); ok {
		return len(mcps), nil
	}
	return 0, nil
}

func setNestedValue(m map[string]interface{}, key string, value interface{}) {
	parts := strings.Split(key, ".")
	current := m

	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
		} else {
			if next, ok := current[part].(map[string]interface{}); ok {
				current = next
			} else {
				next := make(map[string]interface{})
				current[part] = next
				current = next
			}
		}
	}
}

func writeConfig(path string, cfg map[string]interface{}, configType string) error {
	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var data []byte
	var err error

	if configType == "toml" {
		// For TOML, we need to use the encoder
		var buf strings.Builder
		enc := toml.NewEncoder(&buf)
		if err := enc.Encode(cfg); err != nil {
			return err
		}
		data = []byte(buf.String())
	} else {
		data, err = json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}
	}

	return os.WriteFile(path, data, 0o644)
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

const command = "providers/config"
