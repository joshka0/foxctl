package config

import (
	"os"
	"strconv"
)

// Config holds application configuration.
type Config struct {
	Port     int
	Host     string
	Debug    bool
	LogLevel string
}

// Load reads configuration from environment variables.
func Load() *Config {
	cfg := &Config{
		Port:     8080,
		Host:     "localhost",
		Debug:    false,
		LogLevel: "info",
	}

	if port := os.Getenv("PORT"); port != "" {
		if p, err := strconv.Atoi(port); err == nil {
			cfg.Port = p
		}
	}

	if host := os.Getenv("HOST"); host != "" {
		cfg.Host = host
	}

	if debug := os.Getenv("DEBUG"); debug == "true" {
		cfg.Debug = true
	}

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		cfg.LogLevel = level
	}

	return cfg
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return ErrInvalidPort
	}
	return nil
}

var ErrInvalidPort = &ConfigError{Field: "port", Message: "must be between 1 and 65535"}

type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return e.Field + ": " + e.Message
}
