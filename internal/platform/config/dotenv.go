package config

import (
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// LoadDotEnv loads .env files in priority order (later files override earlier ones):
// 1. ~/.foxctl/.env (global defaults)
// 2. $FOXCTL_HOME/.env (if FOXCTL_HOME is set and different from ~/.foxctl)
// 3. $PWD/.env (project-level overrides)
//
// This function should be called early in the application lifecycle,
// before config.Load() to ensure environment variables are available.
// It does not return errors - missing files are silently ignored.
func LoadDotEnv() {
	var envFiles []string

	// 1. Global: ~/.foxctl/.env
	if home, err := os.UserHomeDir(); err == nil {
		globalEnv := filepath.Join(home, ".foxctl", ".env")
		envFiles = append(envFiles, globalEnv)
	}

	// 2. Custom FOXCTL_HOME if set and different from default
	if foxctlHome := os.Getenv("FOXCTL_HOME"); foxctlHome != "" {
		customEnv := filepath.Join(foxctlHome, ".env")
		// Avoid duplicates
		if len(envFiles) == 0 || envFiles[0] != customEnv {
			envFiles = append(envFiles, customEnv)
		}
	}

	// 3. Project-level: $PWD/.env
	if cwd, err := os.Getwd(); err == nil {
		projectEnv := filepath.Join(cwd, ".env")
		envFiles = append(envFiles, projectEnv)
	}

	// Load files in order (godotenv does NOT override existing env vars by default)
	// We want later files to override, so we load in reverse order
	for i := len(envFiles) - 1; i >= 0; i-- {
		// godotenv.Load does not override existing env vars
		// Use Overload if you want later files to take precedence
		_ = godotenv.Load(envFiles[i])
	}
}

// LoadDotEnvFrom loads a specific .env file. Returns error if the file
// exists but cannot be parsed.
func LoadDotEnvFrom(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil // File doesn't exist, that's OK
	}
	return godotenv.Load(path)
}

// LoadDotEnvOverride loads .env files and overrides existing environment variables.
// This is useful when you want project-level .env to take precedence over shell exports.
func LoadDotEnvOverride() {
	var envFiles []string

	// 1. Global: ~/.foxctl/.env
	if home, err := os.UserHomeDir(); err == nil {
		globalEnv := filepath.Join(home, ".foxctl", ".env")
		envFiles = append(envFiles, globalEnv)
	}

	// 2. Custom FOXCTL_HOME if set and different from default
	if foxctlHome := os.Getenv("FOXCTL_HOME"); foxctlHome != "" {
		customEnv := filepath.Join(foxctlHome, ".env")
		if len(envFiles) == 0 || envFiles[0] != customEnv {
			envFiles = append(envFiles, customEnv)
		}
	}

	// 3. Project-level: $PWD/.env
	if cwd, err := os.Getwd(); err == nil {
		projectEnv := filepath.Join(cwd, ".env")
		envFiles = append(envFiles, projectEnv)
	}

	// Load in order - Overload means later values win
	for _, f := range envFiles {
		_ = godotenv.Overload(f)
	}
}
