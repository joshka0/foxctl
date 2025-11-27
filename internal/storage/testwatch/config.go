package testwatch

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// DefaultConfigFileName is the canonical config file name within .agentctl/.
const DefaultConfigFileName = "test-watch.yaml"

// Config represents the test watch configuration for a workspace.
type Config struct {
	// Debounce is the default debounce duration for all watchers.
	Debounce time.Duration `yaml:"-"`

	// DebounceRaw is the raw string value from YAML (e.g., "2s").
	DebounceRaw string `yaml:"debounce,omitempty"`

	// Watchers is the list of configured test watchers.
	Watchers []WatcherConfig `yaml:"watchers,omitempty"`
}

// WatcherConfig represents a single test watcher configuration.
type WatcherConfig struct {
	// ID is a unique identifier for the watcher (e.g., "go", "js", "python").
	ID string `yaml:"id"`

	// Command is the shell command to run tests.
	Command string `yaml:"command"`

	// Include is a list of glob patterns for files that trigger this watcher.
	Include []string `yaml:"include,omitempty"`

	// Exclude is a list of glob patterns for files to ignore.
	Exclude []string `yaml:"exclude,omitempty"`

	// Debounce is the debounce duration for this watcher (overrides Config.Debounce).
	Debounce time.Duration `yaml:"-"`

	// DebounceRaw is the raw string value from YAML.
	DebounceRaw string `yaml:"debounce,omitempty"`

	// MinInterval is the minimum time between test runs.
	MinInterval time.Duration `yaml:"-"`

	// MinIntervalRaw is the raw string value from YAML.
	MinIntervalRaw string `yaml:"min_interval,omitempty"`
}

// DefaultDebounce is the default debounce duration if not specified.
const DefaultDebounce = 2 * time.Second

// DefaultMinInterval is the default minimum interval between runs if not specified.
const DefaultMinInterval = 20 * time.Second

// ConfigPath returns the path to the test-watch.yaml config file for a workspace.
func ConfigPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".agentctl", DefaultConfigFileName)
}

// LoadConfig reads and parses the test watch config from a workspace.
func LoadConfig(workspaceRoot string) (*Config, error) {
	path := ConfigPath(workspaceRoot)
	return LoadConfigFromPath(path)
}

// LoadConfigFromPath reads and parses the test watch config from a specific path.
func LoadConfigFromPath(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("testwatch: config not found: %s", path)
		}
		return nil, fmt.Errorf("testwatch: read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("testwatch: parse config: %w", err)
	}

	// Parse durations
	if err := cfg.parseDurations(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SaveConfig writes the config to the workspace's .agentctl/ directory.
func SaveConfig(workspaceRoot string, cfg *Config) error {
	path := ConfigPath(workspaceRoot)
	return SaveConfigToPath(path, cfg)
}

// SaveConfigToPath writes the config to a specific path.
func SaveConfigToPath(path string, cfg *Config) error {
	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("testwatch: create config dir: %w", err)
	}

	// Serialize durations back to strings
	cfg.serializeDurations()

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("testwatch: marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("testwatch: write config: %w", err)
	}
	return nil
}

// ConfigExists returns true if the config file exists for the workspace.
func ConfigExists(workspaceRoot string) bool {
	path := ConfigPath(workspaceRoot)
	_, err := os.Stat(path)
	return err == nil
}

// GetWatcher returns the watcher config by ID, or nil if not found.
func (c *Config) GetWatcher(id string) *WatcherConfig {
	for i := range c.Watchers {
		if c.Watchers[i].ID == id {
			return &c.Watchers[i]
		}
	}
	return nil
}

// UpsertWatcher adds or updates a watcher by ID.
func (c *Config) UpsertWatcher(w WatcherConfig) {
	for i := range c.Watchers {
		if c.Watchers[i].ID == w.ID {
			c.Watchers[i] = w
			return
		}
	}
	c.Watchers = append(c.Watchers, w)
}

// RemoveWatcher removes a watcher by ID. Returns true if removed.
func (c *Config) RemoveWatcher(id string) bool {
	for i := range c.Watchers {
		if c.Watchers[i].ID == id {
			c.Watchers = append(c.Watchers[:i], c.Watchers[i+1:]...)
			return true
		}
	}
	return false
}

// EffectiveDebounce returns the effective debounce for a watcher,
// falling back to the config-level debounce or default.
func (w *WatcherConfig) EffectiveDebounce(cfg *Config) time.Duration {
	if w.Debounce > 0 {
		return w.Debounce
	}
	if cfg != nil && cfg.Debounce > 0 {
		return cfg.Debounce
	}
	return DefaultDebounce
}

// EffectiveMinInterval returns the effective min interval for a watcher,
// falling back to the default.
func (w *WatcherConfig) EffectiveMinInterval() time.Duration {
	if w.MinInterval > 0 {
		return w.MinInterval
	}
	return DefaultMinInterval
}

func (c *Config) parseDurations() error {
	if c.DebounceRaw != "" {
		d, err := time.ParseDuration(c.DebounceRaw)
		if err != nil {
			return fmt.Errorf("testwatch: invalid debounce %q: %w", c.DebounceRaw, err)
		}
		c.Debounce = d
	}

	for i := range c.Watchers {
		w := &c.Watchers[i]
		if w.DebounceRaw != "" {
			d, err := time.ParseDuration(w.DebounceRaw)
			if err != nil {
				return fmt.Errorf("testwatch: watcher %q: invalid debounce %q: %w", w.ID, w.DebounceRaw, err)
			}
			w.Debounce = d
		}
		if w.MinIntervalRaw != "" {
			d, err := time.ParseDuration(w.MinIntervalRaw)
			if err != nil {
				return fmt.Errorf("testwatch: watcher %q: invalid min_interval %q: %w", w.ID, w.MinIntervalRaw, err)
			}
			w.MinInterval = d
		}
	}
	return nil
}

func (c *Config) serializeDurations() {
	if c.Debounce > 0 {
		c.DebounceRaw = c.Debounce.String()
	}
	for i := range c.Watchers {
		w := &c.Watchers[i]
		if w.Debounce > 0 {
			w.DebounceRaw = w.Debounce.String()
		}
		if w.MinInterval > 0 {
			w.MinIntervalRaw = w.MinInterval.String()
		}
	}
}

// NewConfig creates a new empty config with default values.
func NewConfig() *Config {
	return &Config{
		Debounce: DefaultDebounce,
		Watchers: []WatcherConfig{},
	}
}
