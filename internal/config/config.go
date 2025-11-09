// Package config handles layered configuration loading for agentctl.
package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

const (
	// DefaultInlineOutputKB is the fallback size threshold for inline envelopes.
	DefaultInlineOutputKB = 256
	// DefaultMaxCaptureKB limits captured stdout/stderr data per job.
	DefaultMaxCaptureKB = 10240
)

// Config represents the fully materialized runtime configuration.
type Config struct {
	Home           string `mapstructure:"home" json:"home"`
	InlineOutputKB int    `mapstructure:"inline_output_kb" json:"inline_output_kb"`
	MaxCaptureKB   int    `mapstructure:"max_capture_kb" json:"max_capture_kb"`
	Paths          Paths  `mapstructure:"paths" json:"paths"`
}

// Paths include common on-disk locations rooted at the agentctl home directory.
type Paths struct {
	CAS    string `mapstructure:"cas" json:"cas"`
	Jobs   string `mapstructure:"jobs" json:"jobs"`
	Cache  string `mapstructure:"cache" json:"cache"`
	Skills string `mapstructure:"skills" json:"skills"`
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
	l := &loader{}
	for _, opt := range opts {
		opt(l)
	}

	home, err := userHomeDir()
	if err != nil {
		return Config{}, err
	}

	v := viper.New()
	v.SetConfigType("yaml")
	v.SetEnvPrefix("AGENTCTL")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	defaultHome := filepath.Join(home, ".agentctl")
	v.SetDefault("home", defaultHome)
	v.SetDefault("inline_output_kb", DefaultInlineOutputKB)
	v.SetDefault("max_capture_kb", DefaultMaxCaptureKB)
	v.SetDefault("paths.cas", filepath.Join(defaultHome, "cas"))
	v.SetDefault("paths.jobs", filepath.Join(defaultHome, "jobs"))
	v.SetDefault("paths.cache", filepath.Join(defaultHome, "cache"))
	v.SetDefault("paths.skills", filepath.Join(defaultHome, "skills"))

	if l.configFile != "" {
		v.SetConfigFile(l.configFile)
	} else {
		v.SetConfigName("config")
		v.AddConfigPath(defaultHome)
	}

	if err := v.ReadInConfig(); err != nil {
		var configErr viper.ConfigFileNotFoundError
		if !errors.As(err, &configErr) && l.configFile != "" {
			return Config{}, fmt.Errorf("config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("config decode: %w", err)
	}

	cfg.Home = absPath(cfg.Home, home)
	cfg.Paths.CAS = resolvePath(cfg.Paths.CAS, cfg.Home, home)
	cfg.Paths.Jobs = resolvePath(cfg.Paths.Jobs, cfg.Home, home)
	cfg.Paths.Cache = resolvePath(cfg.Paths.Cache, cfg.Home, home)
	cfg.Paths.Skills = resolvePath(cfg.Paths.Skills, cfg.Home, home)

	if cfg.InlineOutputKB <= 0 {
		cfg.InlineOutputKB = DefaultInlineOutputKB
	}
	if cfg.MaxCaptureKB <= 0 {
		cfg.MaxCaptureKB = DefaultMaxCaptureKB
	}

	return cfg, nil
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
