package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the merged hook configuration.
type Config struct {
	Version int       `yaml:"version" json:"version"`
	Hooks   []HookDef `yaml:"hooks" json:"hooks"`

	// Index for fast lookup by event
	byEvent map[Event][]HookDef
}

// HooksForEvent returns all enabled hooks for the given event.
func (c *Config) HooksForEvent(event Event) []HookDef {
	if c.byEvent == nil {
		c.buildIndex()
	}
	return c.byEvent[event]
}

// buildIndex creates the event lookup index.
func (c *Config) buildIndex() {
	c.byEvent = make(map[Event][]HookDef)
	for _, h := range c.Hooks {
		if h.Enabled {
			c.byEvent[h.Event] = append(c.byEvent[h.Event], h)
		}
	}
	for event := range c.byEvent {
		hooks := c.byEvent[event]
		sort.SliceStable(hooks, func(i, j int) bool {
			return hooks[i].Priority < hooks[j].Priority
		})
		c.byEvent[event] = hooks
	}
}

// DefaultConfigPaths returns the default config file paths in precedence order.
// First path (workspace) takes precedence over second (global).
func DefaultConfigPaths(workspaceRoot string) []string {
	paths := make([]string, 0, 2)

	// Workspace config (highest precedence)
	if workspaceRoot != "" {
		paths = append(paths, filepath.Join(workspaceRoot, ".agentctl", "hooks.yaml"))
	}

	// Global config
	homeDir, err := os.UserHomeDir()
	if err == nil {
		paths = append(paths, filepath.Join(homeDir, ".agentctl", "hooks.yaml"))
	}

	return paths
}

// LoadConfig loads and merges hook configuration from the given paths.
// Files are loaded in order; later files override earlier ones by hook ID.
func LoadConfig(paths ...string) (*Config, error) {
	merged := &Config{
		Version: 1,
		Hooks:   make([]HookDef, 0),
	}

	// Track hooks by ID for merging
	byID := make(map[string]int) // ID -> index in merged.Hooks

	for _, path := range paths {
		cfg, err := loadConfigFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Skip missing files
			}
			return nil, fmt.Errorf("loading %s: %w", path, err)
		}

		// Merge hooks by ID
		for _, h := range cfg.Hooks {
			// Set default enabled=true if not specified
			if h.ID == "" {
				continue // Skip hooks without ID
			}

			if idx, exists := byID[h.ID]; exists {
				// Replace existing hook
				merged.Hooks[idx] = h
			} else {
				// Append new hook
				byID[h.ID] = len(merged.Hooks)
				merged.Hooks = append(merged.Hooks, h)
			}
		}
	}

	// Build index
	merged.buildIndex()

	return merged, nil
}

// LoadConfigWithDefaults loads config from default paths.
func LoadConfigWithDefaults(workspaceRoot string) (*Config, error) {
	paths := DefaultConfigPaths(workspaceRoot)
	// Reverse order so workspace takes precedence (loaded last)
	for i, j := 0, len(paths)-1; i < j; i, j = i+1, j-1 {
		paths[i], paths[j] = paths[j], paths[i]
	}
	return LoadConfig(paths...)
}

// loadConfigFile loads a single config file.
func loadConfigFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw HookConfigYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing yaml: %w", err)
	}

	cfg := &Config{
		Version: raw.Version,
		Hooks:   make([]HookDef, 0, len(raw.Hooks)),
	}

	for _, h := range raw.Hooks {
		hook, err := toHookDef(h, raw.Defaults)
		if err != nil {
			return nil, fmt.Errorf("hook %q: %w", h.ID, err)
		}
		if err := validateHook(&hook); err != nil {
			return nil, fmt.Errorf("hook %q: %w", hook.ID, err)
		}
		setHookDefaults(&hook)
		cfg.Hooks = append(cfg.Hooks, hook)
	}

	cfg.buildIndex()
	return cfg, nil
}

// setHookDefaults sets default values for a hook definition.
func setHookDefaults(h *HookDef) {
	for i := range h.Run {
		if h.Run[i].TimeoutMS == 0 {
			h.Run[i].TimeoutMS = 2000 // 2 second default
		}
	}
}

// EmptyConfig returns an empty but valid configuration.
func EmptyConfig() *Config {
	return &Config{
		Version: 1,
		Hooks:   make([]HookDef, 0),
		byEvent: make(map[Event][]HookDef),
	}
}

// ConfigFromHooks creates a config from a slice of hook definitions.
// Useful for testing.
func ConfigFromHooks(hooks []HookDef) *Config {
	cfg := &Config{
		Version: 1,
		Hooks:   hooks,
	}
	// Set defaults for each hook
	for i := range cfg.Hooks {
		setHookDefaults(&cfg.Hooks[i])
	}
	cfg.buildIndex()
	return cfg
}

// WriteConfig writes configuration to a file.
func WriteConfig(cfg *Config, path string) error {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling yaml: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

// HookConfigYAML is the raw YAML structure for unmarshaling.
// This allows handling the enabled field properly.
type HookConfigYAML struct {
	Version  int              `yaml:"version"`
	Defaults HookDefaultsYAML `yaml:"defaults,omitempty"`
	Hooks    []HookDefYAML    `yaml:"hooks"`
}

// HookDefaultsYAML holds default values for hook execution.
type HookDefaultsYAML struct {
	TimeoutMS int    `yaml:"timeout_ms,omitempty"`
	FailOpen  *bool  `yaml:"fail_open,omitempty"`
	FailMode  string `yaml:"fail_mode,omitempty"`
	Required  *bool  `yaml:"required,omitempty"`
	Ephemeral *bool  `yaml:"ephemeral,omitempty"`
}

// HookDefYAML is the YAML representation of a hook definition.
type HookDefYAML struct {
	ID       string             `yaml:"id"`
	Enabled  *bool              `yaml:"enabled,omitempty"` // Pointer to distinguish unset from false
	Event    Event              `yaml:"event"`
	Priority int                `yaml:"priority,omitempty"`
	Match    *HookMatcher       `yaml:"match,omitempty"`
	Run      []HookRunEntryYAML `yaml:"run"`
}

// toHookDef converts the YAML representation to the canonical HookDef.
func toHookDef(h HookDefYAML, defaults HookDefaultsYAML) (HookDef, error) {
	enabled := true // Default to enabled
	if h.Enabled != nil {
		enabled = *h.Enabled
	}

	run := make([]HookRunEntry, len(h.Run))
	for i, entry := range h.Run {
		failOpen, err := resolveFailOpen(entry, defaults)
		if err != nil {
			return HookDef{}, err
		}
		timeoutMS := entry.TimeoutMS
		if timeoutMS == 0 {
			timeoutMS = defaults.TimeoutMS
		}
		run[i] = HookRunEntry{
			Skill:     entry.Skill,
			TimeoutMS: timeoutMS,
			FailOpen:  failOpen,
			Ephemeral: resolveEphemeral(entry, defaults),
			Config:    entry.Config,
		}
	}

	return HookDef{
		ID:       h.ID,
		Enabled:  enabled,
		Event:    h.Event,
		Priority: h.Priority,
		Match:    h.Match,
		Run:      run,
	}, nil
}

// HookRunEntryYAML is the YAML representation of a hook run entry.
type HookRunEntryYAML struct {
	Skill     string         `yaml:"skill"`
	TimeoutMS int            `yaml:"timeout_ms,omitempty"`
	FailOpen  *bool          `yaml:"fail_open,omitempty"` // Pointer to distinguish unset from false
	FailMode  string         `yaml:"fail_mode,omitempty"`
	Required  *bool          `yaml:"required,omitempty"`
	Ephemeral *bool          `yaml:"ephemeral,omitempty"`
	Config    map[string]any `yaml:"config,omitempty"`
}

func resolveFailOpen(entry HookRunEntryYAML, defaults HookDefaultsYAML) (bool, error) {
	if entry.FailOpen != nil {
		return *entry.FailOpen, nil
	}
	if entry.FailMode != "" {
		return parseFailMode(entry.FailMode)
	}
	if entry.Required != nil {
		return !*entry.Required, nil
	}
	if defaults.FailOpen != nil {
		return *defaults.FailOpen, nil
	}
	if defaults.FailMode != "" {
		return parseFailMode(defaults.FailMode)
	}
	if defaults.Required != nil {
		return !*defaults.Required, nil
	}
	return true, nil
}

func resolveEphemeral(entry HookRunEntryYAML, defaults HookDefaultsYAML) *bool {
	if entry.Ephemeral != nil {
		return entry.Ephemeral
	}
	if defaults.Ephemeral != nil {
		return defaults.Ephemeral
	}
	return nil
}

func parseFailMode(value string) (bool, error) {
	mode := strings.ToLower(value)
	mode = strings.ReplaceAll(mode, "-", "")
	mode = strings.ReplaceAll(mode, "_", "")

	switch mode {
	case "open", "failopen":
		return true, nil
	case "closed", "failclosed":
		return false, nil
	default:
		return false, fmt.Errorf("invalid fail_mode: %s", value)
	}
}

// validateHook validates a hook definition.
func validateHook(h *HookDef) error {
	if h.ID == "" {
		return fmt.Errorf("id is required")
	}
	if h.Event == "" {
		return fmt.Errorf("event is required")
	}
	if !h.Event.IsValid() {
		return fmt.Errorf("invalid event: %s", h.Event)
	}
	if len(h.Run) == 0 {
		return fmt.Errorf("run is required (at least one skill)")
	}
	for i, entry := range h.Run {
		if entry.Skill == "" {
			return fmt.Errorf("run[%d].skill is required", i)
		}
	}
	return nil
}
