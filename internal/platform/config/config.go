// Package config handles layered configuration loading for agentctl.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

const (
	// DefaultInlineOutputKB is the fallback size threshold for inline envelopes.
	DefaultInlineOutputKB = 32
	// DefaultMaxCaptureKB limits captured stdout/stderr data per job.
	DefaultMaxCaptureKB = 10240
)

// Config represents the fully materialized runtime configuration.
type Config struct {
	Home           string           `mapstructure:"home" json:"home"`
	InlineOutputKB int              `mapstructure:"inline_output_kb" json:"inline_output_kb"`
	MaxCaptureKB   int              `mapstructure:"max_capture_kb" json:"max_capture_kb"`
	Paths          Paths            `mapstructure:"paths" json:"paths"`
	Storage        StorageSettings  `mapstructure:"storage" json:"storage"`
	Memory         MemorySettings   `mapstructure:"memory" json:"memory"`
	Cache          CacheSettings    `mapstructure:"cache" json:"cache"`
	Logging        LoggingSettings  `mapstructure:"logging" json:"logging"`
	OpenAPI        OpenAPISettings  `mapstructure:"openapi" json:"openapi"`
	Indexing       IndexingSettings `mapstructure:"indexing" json:"indexing"`
}

// Paths include common on-disk locations rooted at the agentctl home directory.
type Paths struct {
	CAS    string `mapstructure:"cas" json:"cas"`
	Jobs   string `mapstructure:"jobs" json:"jobs"`
	Cache  string `mapstructure:"cache" json:"cache"`
	Skills string `mapstructure:"skills" json:"skills"`
}

// StorageSettings configure persistent storage for agents, mailboxes, blackboard, and quotas.
type StorageSettings struct {
	Root string `mapstructure:"root" json:"root"`
}

// MemorySettings influence cache + named memory behavior.
type MemorySettings struct {
	AutoCacheTTL      time.Duration `mapstructure:"auto_cache_ttl" json:"auto_cache_ttl"`
	DefaultNamedTTL   time.Duration `mapstructure:"default_named_ttl" json:"default_named_ttl"`
	AutoLoadWorkspace bool          `mapstructure:"auto_load_workspace" json:"auto_load_workspace"`
}

// CacheSettings describe run-time caching defaults.
type CacheSettings struct {
	DefaultMode string `mapstructure:"default_mode" json:"default_mode"`
}

// LoggingSettings configure CLI logging behavior.
type LoggingSettings struct {
	Level  string `mapstructure:"level" json:"level"`
	Format string `mapstructure:"format" json:"format"`
}

// OpenAPISettings hold configuration for the generic http/openapi skill.
type OpenAPISettings struct {
	// PluginPath is a colon-separated search path that locates plugin binaries.
	// Each entry is resolved relative to the agentctl home directory when not
	// absolute. Environment variables may override this value via
	// AGENTCTL_OPENAPI_PLUGIN_PATH.
	PluginPath []string `mapstructure:"plugin_path" json:"plugin_path"`
}

// IndexingSettings configure post-review indexing pipelines.
// See: docs/spec/semantic_file_index.md §8.2
type IndexingSettings struct {
	// PostReview configures the post-review indexing pipeline.
	PostReview PostReviewSettings `mapstructure:"post_review" json:"post_review"`
}

// PostReviewSettings holds the configuration for post-review indexers.
type PostReviewSettings struct {
	// Enabled controls whether post-review indexing is active.
	Enabled bool `mapstructure:"enabled" json:"enabled"`

	// Async controls whether indexing runs asynchronously (default: true).
	// When false, task completion blocks until indexing completes.
	Async bool `mapstructure:"async" json:"async"`

	// Indexers lists the configured indexers.
	Indexers []IndexerSettings `mapstructure:"indexers" json:"indexers"`
}

// IndexerSettings defines the configuration for a single indexer.
type IndexerSettings struct {
	// ID is a unique identifier for this indexer instance.
	ID string `mapstructure:"id" json:"id"`

	// Kind identifies the indexer type (e.g., "semantic_file_index", "code_symbol_dag").
	Kind string `mapstructure:"kind" json:"kind"`

	// Enabled controls whether this indexer is active.
	Enabled bool `mapstructure:"enabled" json:"enabled"`

	// IncludeGlobs are glob patterns for files to include.
	IncludeGlobs []string `mapstructure:"include_globs" json:"include_globs"`

	// ExcludeGlobs are glob patterns for files to exclude.
	ExcludeGlobs []string `mapstructure:"exclude_globs" json:"exclude_globs"`

	// MaxFileKB is the maximum file size in KB to index (0 = no limit).
	MaxFileKB int `mapstructure:"max_file_kb" json:"max_file_kb"`
}

// Option customizes the loader.
type Option func(*loader)

type loader struct {
	configFile string
}

// WithConfigFile instructs the loader to read the specified file explicitly.
func WithConfigFile(path string) Option {
	return func(l *loader) {
		l.configFile = path
	}
}

// Load returns the hydrated configuration by applying defaults, config file, and env overrides.
func Load(_ context.Context, opts ...Option) (Config, error) {
	l := parseOptions(opts)

	home, err := userHomeDir()
	if err != nil {
		return Config{}, err
	}

	v := newConfiguredViper()
	defaultHome := filepath.Join(home, ".agentctl")
	applyDefaults(v, defaultHome)
	configureConfigFile(v, l, defaultHome)
	if err := readConfig(v, l.configFile); err != nil {
		return Config{}, err
	}

	cfg, err := decodeConfig(v)
	if err != nil {
		return Config{}, err
	}

	return finalizeConfig(cfg, home), nil
}

func parseOptions(opts []Option) *loader {
	l := &loader{}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

func newConfiguredViper() *viper.Viper {
	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("AGENTCTL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	return v
}

func applyDefaults(v *viper.Viper, defaultHome string) {
	v.SetDefault("home", defaultHome)
	v.SetDefault("inline_output_kb", DefaultInlineOutputKB)
	v.SetDefault("max_capture_kb", DefaultMaxCaptureKB)
	v.SetDefault("paths.cas", filepath.Join(defaultHome, "cas"))
	v.SetDefault("paths.jobs", filepath.Join(defaultHome, "jobs"))
	v.SetDefault("paths.cache", filepath.Join(defaultHome, "cache"))
	v.SetDefault("paths.skills", filepath.Join(defaultHome, "skills"))
	v.SetDefault("storage.root", filepath.Join(defaultHome, "storage"))
	v.SetDefault("memory.auto_cache_ttl", "24h")
	v.SetDefault("memory.default_named_ttl", "720h") // 30d
	v.SetDefault("memory.auto_load_workspace", true)
	v.SetDefault("cache.default_mode", "auto")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "text")
	v.SetDefault("openapi.plugin_path", filepath.Join(defaultHome, "plugins"))
	v.SetDefault("indexing.post_review.enabled", false)
	v.SetDefault("indexing.post_review.async", true)
	v.SetDefault("indexing.post_review.indexers", []map[string]any{})
}

func configureConfigFile(v *viper.Viper, l *loader, defaultHome string) {
	if l.configFile != "" {
		v.SetConfigFile(l.configFile)
		return
	}
	v.SetConfigName("config")
	v.AddConfigPath(defaultHome)
}

func readConfig(v *viper.Viper, explicit string) error {
	if err := v.ReadInConfig(); err != nil {
		var configErr viper.ConfigFileNotFoundError
		if explicit != "" || !errors.As(err, &configErr) {
			return fmt.Errorf("config: %w", err)
		}
	}
	return nil
}

func decodeConfig(v *viper.Viper) (Config, error) {
	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("config decode: %w", err)
	}
	cfg.OpenAPI.PluginPath = parsePluginPathList(v.Get("openapi.plugin_path"))
	return cfg, nil
}

func finalizeConfig(cfg Config, home string) Config {
	cfg.Home = absPath(cfg.Home, home)
	cfg.Paths.CAS = resolvePath(cfg.Paths.CAS, cfg.Home, home)
	cfg.Paths.Jobs = resolvePath(cfg.Paths.Jobs, cfg.Home, home)
	cfg.Paths.Cache = resolvePath(cfg.Paths.Cache, cfg.Home, home)
	cfg.Paths.Skills = resolvePath(cfg.Paths.Skills, cfg.Home, home)
	cfg.Storage.Root = resolvePath(cfg.Storage.Root, cfg.Home, home)
	if len(cfg.OpenAPI.PluginPath) == 0 {
		cfg.OpenAPI.PluginPath = []string{filepath.Join(cfg.Home, "plugins")}
	}
	cfg.OpenAPI.PluginPath = normalizePluginPaths(cfg.OpenAPI.PluginPath, cfg.Home, home)

	if cfg.InlineOutputKB <= 0 {
		cfg.InlineOutputKB = DefaultInlineOutputKB
	}
	if cfg.MaxCaptureKB <= 0 {
		cfg.MaxCaptureKB = DefaultMaxCaptureKB
	}
	if cfg.Memory.AutoCacheTTL <= 0 {
		cfg.Memory.AutoCacheTTL = 24 * time.Hour
	}
	if cfg.Memory.DefaultNamedTTL <= 0 {
		cfg.Memory.DefaultNamedTTL = 30 * 24 * time.Hour
	}
	if cfg.Cache.DefaultMode == "" {
		cfg.Cache.DefaultMode = "auto"
	}

	cfg.Logging.Level = strings.ToLower(strings.TrimSpace(cfg.Logging.Level))
	if cfg.Logging.Level == "" {
		cfg.Logging.Level = "info"
	}
	cfg.Logging.Format = strings.ToLower(strings.TrimSpace(cfg.Logging.Format))
	if cfg.Logging.Format == "" {
		cfg.Logging.Format = "text"
	}
	return cfg
}

func userHomeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: resolve home: %w", err)
	}
	return home, nil
}

func absPath(p, home string) string {
	if p == "" {
		return home
	}
	return resolvePath(p, home, home)
}

func resolvePath(p, base, userHome string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~") {
		trimmed := strings.TrimPrefix(p, "~")
		trimmed = strings.TrimPrefix(trimmed, string(filepath.Separator))
		return filepath.Join(userHome, trimmed)
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(base, p)
}

func parsePluginPathList(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		return splitPathList(v)
	case []string:
		return normalizeStringSlice(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return normalizeStringSlice(out)
	default:
		return nil
	}
}

func splitPathList(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, string(os.PathListSeparator))
	return normalizeStringSlice(parts)
}

func normalizeStringSlice(parts []string) []string {
	out := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func normalizePluginPaths(paths []string, base, userHome string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		resolved := resolvePath(p, base, userHome)
		if resolved == "" {
			continue
		}
		if _, ok := seen[resolved]; ok {
			continue
		}
		seen[resolved] = struct{}{}
		out = append(out, resolved)
	}
	return out
}
