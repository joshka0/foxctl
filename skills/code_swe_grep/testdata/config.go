package config

import (
	"context"
	"fmt"
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

func Load(ctx context.Context) (*Config, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	cfg := &Config{
		Port:     8080,
		Host:     "localhost",
		Debug:    false,
		LogLevel: "info",
	}

	if port := os.Getenv("PORT"); port != "" {
		p, err := strconv.Atoi(port)
		if err != nil {
			return nil, &ConfigError{
				Code:    "EARG",
				Field:   "port",
				Message: fmt.Sprintf("invalid PORT value %q", port),
				Hint:    "PORT must be set to a numeric value between 1 and 65535",
			}
		}
		cfg.Port = p
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

	return cfg, nil
}

// Validate checks if the configuration is valid.

func (c *Config) Validate(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	if c.Port < 1 || c.Port > 65535 {
		return ErrInvalidPort
	}
	return nil
}

var ErrInvalidPort = &ConfigError{
	Code:    "EARG",
	Field:   "port",
	Message: "must be between 1 and 65535",
	Hint:    "PORT must be set to a value between 1 and 65535",
}

type ConfigError struct {
	Code    string
	Field   string
	Message string
	Hint    string
}

func (e *ConfigError) Error() string {
	return e.Code + ": " + e.Field + ": " + e.Message
}
