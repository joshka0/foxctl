package dbdriver

import (
	"errors"
	"fmt"
	"os"
	"strconv"
)

// DriverType represents the database driver type
type DriverType string

const (
	// DriverSQLite uses local SQLite database (standard SQLite, no vector search)
	DriverSQLite DriverType = "sqlite"
	// DriverLibSQL uses local libSQL database file (supports vector search locally)
	DriverLibSQL DriverType = "libsql"
	// DriverTurso uses Turso cloud database with libSQL (cloud, replicated)
	DriverTurso DriverType = "turso"
)

const (
	// DefaultVectorDimensions is the fallback when no dimensions are configured.
	// This can be overridden via AGENTCTL_VECTOR_DIMS environment variable.
	// Common sizes: 384 (MiniLM), 768 (BERT), 1024 (Voyage), 1536 (OpenAI), 3072 (Gemini)
	DefaultVectorDimensions = 1024
)

// GetDefaultVectorDimensions returns the default vector dimensions from environment
// or the built-in default. This allows global configuration of embedding dimensions.
func GetDefaultVectorDimensions() int {
	if dimsStr := os.Getenv("AGENTCTL_VECTOR_DIMS"); dimsStr != "" {
		if dims, err := strconv.Atoi(dimsStr); err == nil && dims > 0 {
			return dims
		}
	}
	return DefaultVectorDimensions
}

// Config holds database configuration
type Config struct {
	// Driver specifies which database driver to use (sqlite, libsql, or turso)
	Driver DriverType `json:"driver" yaml:"driver"`

	// SQLite specific configuration
	SQLite SQLiteConfig `json:"sqlite,omitempty" yaml:"sqlite,omitempty"`

	// LibSQL specific configuration (local file-based libSQL)
	LibSQL LibSQLConfig `json:"libsql,omitempty" yaml:"libsql,omitempty"`

	// Turso specific configuration (cloud libSQL)
	Turso TursoConfig `json:"turso,omitempty" yaml:"turso,omitempty"`
}

// SQLiteConfig holds SQLite-specific configuration
type SQLiteConfig struct {
	// Path to the SQLite database file
	Path string `json:"path" yaml:"path"`

	// EnableWAL enables Write-Ahead Logging (default: true)
	EnableWAL bool `json:"enable_wal" yaml:"enable_wal"`

	// BusyTimeout in milliseconds (default: 5000)
	BusyTimeout int `json:"busy_timeout" yaml:"busy_timeout"`
}

// LibSQLConfig holds local libSQL-specific configuration
type LibSQLConfig struct {
	// Path to the libSQL database file
	Path string `json:"path" yaml:"path"`

	// EnableVectorSearch enables vector search capabilities
	EnableVectorSearch bool `json:"enable_vector_search" yaml:"enable_vector_search"`

	// VectorDimensions specifies the dimension of vector embeddings.
	// If 0, uses GetDefaultVectorDimensions() (configurable via AGENTCTL_VECTOR_DIMS).
	VectorDimensions int `json:"vector_dimensions" yaml:"vector_dimensions"`

	// SyncURL is the remote sqld URL for sync (optional).
	// When set, enables embedded replica mode with automatic sync.
	// Example: "http://localhost:8080" or "libsql://your-db.turso.io"
	SyncURL string `json:"sync_url,omitempty" yaml:"sync_url,omitempty"`

	// AuthToken for remote sync authentication (optional, required for Turso cloud)
	AuthToken string `json:"auth_token,omitempty" yaml:"auth_token,omitempty"`

	// SyncInterval in seconds for periodic background sync (optional).
	// When 0 (default), sync happens on-demand via Sync() calls.
	// When > 0, a background goroutine syncs every N seconds.
	SyncInterval int `json:"sync_interval,omitempty" yaml:"sync_interval,omitempty"`
}

// TursoConfig holds Turso-specific configuration
type TursoConfig struct {
	// URL is the Turso database URL (e.g., libsql://your-database.turso.io)
	URL string `json:"url" yaml:"url"`

	// AuthToken is the authentication token for Turso
	AuthToken string `json:"auth_token" yaml:"auth_token"`

	// DatabaseName is the logical name of the database (cache, jobs, or memory)
	DatabaseName string `json:"database_name,omitempty" yaml:"database_name,omitempty"`

	// ReplicaPath is the local path for the embedded replica (if empty, uses temp dir).
	// When set, the replica persists across restarts for faster sync; otherwise
	// a temp directory is created and cleaned up on Close().
	ReplicaPath string `json:"replica_path,omitempty" yaml:"replica_path,omitempty"`

	// SyncInterval in seconds for periodic background sync (optional).
	// When 0 (default), sync happens on-demand via Sync() calls.
	SyncInterval int `json:"sync_interval,omitempty" yaml:"sync_interval,omitempty"`

	// EnableVectorSearch enables vector search capabilities (only for memory database)
	EnableVectorSearch bool `json:"enable_vector_search" yaml:"enable_vector_search"`

	// VectorDimensions specifies the dimension of vector embeddings.
	// If 0, uses GetDefaultVectorDimensions() (configurable via AGENTCTL_VECTOR_DIMS).
	// Common sizes: 384 (MiniLM), 768 (BERT), 1024 (Voyage), 1536 (OpenAI), 3072 (Gemini)
	VectorDimensions int `json:"vector_dimensions" yaml:"vector_dimensions"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	switch c.Driver {
	case DriverSQLite:
		if c.SQLite.Path == "" {
			return errors.New("sqlite path is required")
		}
	case DriverLibSQL:
		if c.LibSQL.Path == "" {
			return errors.New("libsql path is required")
		}
	case DriverTurso:
		if c.Turso.URL == "" {
			return errors.New("turso url is required")
		}
		if c.Turso.AuthToken == "" {
			return errors.New("turso auth_token is required")
		}
	case "":
		return errors.New("driver type is required")
	default:
		return fmt.Errorf("unsupported driver type: %s", c.Driver)
	}
	return nil
}

// DefaultSQLiteConfig returns a default SQLite configuration
func DefaultSQLiteConfig(path string) Config {
	return Config{
		Driver: DriverSQLite,
		SQLite: SQLiteConfig{
			Path:        path,
			EnableWAL:   true,
			BusyTimeout: 5000,
		},
	}
}

// DefaultLibSQLConfig returns a default local libSQL configuration
func DefaultLibSQLConfig(path string, enableVectors bool) Config {
	return Config{
		Driver: DriverLibSQL,
		LibSQL: LibSQLConfig{
			Path:               path,
			EnableVectorSearch: enableVectors,
			VectorDimensions:   GetDefaultVectorDimensions(),
		},
	}
}

// DefaultTursoConfig returns a default Turso configuration
func DefaultTursoConfig(url, authToken, dbName string) Config {
	return Config{
		Driver: DriverTurso,
		Turso: TursoConfig{
			URL:                url,
			AuthToken:          authToken,
			DatabaseName:       dbName,
			EnableVectorSearch: false,
			VectorDimensions:   GetDefaultVectorDimensions(),
		},
	}
}
