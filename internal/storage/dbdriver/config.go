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
	// DriverTurso uses the Rust-backed Turso database locally or with remote sync.
	DriverTurso DriverType = "turso"
	// DriverPostgres uses PostgreSQL database (enterprise, shared state)
	DriverPostgres DriverType = "postgres"
)

const (
	// DefaultVectorDimensions is the fallback when no dimensions are configured.
	// This can be overridden via FOXCTL_VECTOR_DIMS environment variable.
	// Common sizes: 384 (MiniLM), 768 (BERT), 1024 (Voyage), 1536 (OpenAI), 3072 (Gemini)
	DefaultVectorDimensions = 1024
)

// GetDefaultVectorDimensions returns the default vector dimensions from environment
// or the built-in default. This allows global configuration of embedding dimensions.
func GetDefaultVectorDimensions() int {
	if dimsStr := os.Getenv("FOXCTL_VECTOR_DIMS"); dimsStr != "" {
		if dims, err := strconv.Atoi(dimsStr); err == nil && dims > 0 {
			return dims
		}
	}
	return DefaultVectorDimensions
}

// Config holds database configuration
type Config struct {
	// Driver specifies which database driver to use (sqlite, turso, or postgres)
	Driver DriverType `json:"driver" yaml:"driver"`

	// SQLite specific configuration
	SQLite SQLiteConfig `json:"sqlite,omitempty" yaml:"sqlite,omitempty"`

	// Turso specific configuration (local Rust-backed database or remote sync)
	Turso TursoConfig `json:"turso,omitempty" yaml:"turso,omitempty"`

	// Postgres specific configuration (enterprise shared state)
	Postgres PostgresConfig `json:"postgres,omitempty" yaml:"postgres,omitempty"`
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

// TursoConfig holds Turso-specific configuration
type TursoConfig struct {
	// Path is the local Turso database file path. It is required for local mode
	// and used as the sync replica path when URL is set.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`

	// URL is the remote Turso database URL for sync (e.g., libsql://your-database.turso.io).
	// If empty, Turso opens Path as a local-only database.
	URL string `json:"url,omitempty" yaml:"url,omitempty"`

	// AuthToken is the authentication token for remote Turso sync.
	AuthToken string `json:"auth_token,omitempty" yaml:"auth_token,omitempty"`

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
	// If 0, uses GetDefaultVectorDimensions() (configurable via FOXCTL_VECTOR_DIMS).
	// Common sizes: 384 (MiniLM), 768 (BERT), 1024 (Voyage), 1536 (OpenAI), 3072 (Gemini)
	VectorDimensions int `json:"vector_dimensions" yaml:"vector_dimensions"`
}

// PostgresConfig holds PostgreSQL-specific configuration
type PostgresConfig struct {
	// DSN is the PostgreSQL connection string
	// Example: "postgres://user:pass@host:5432/dbname?sslmode=require"
	DSN string `json:"dsn" yaml:"dsn"`

	// Schema is the PostgreSQL schema for store isolation (default: store name lowercased).
	// Each store gets its own schema to avoid table name collisions.
	Schema string `json:"schema,omitempty" yaml:"schema,omitempty"`

	// MaxOpenConns is the maximum number of open connections (default: 5)
	MaxOpenConns int `json:"max_open_conns,omitempty" yaml:"max_open_conns,omitempty"`

	// MaxIdleConns is the maximum number of idle connections (default: 2)
	MaxIdleConns int `json:"max_idle_conns,omitempty" yaml:"max_idle_conns,omitempty"`

	// ConnMaxLifetimeSeconds is the maximum lifetime of a connection in seconds (default: 3600)
	ConnMaxLifetimeSeconds int `json:"conn_max_lifetime_seconds,omitempty" yaml:"conn_max_lifetime_seconds,omitempty"`

	// ConnMaxIdleTimeSeconds is the maximum idle time of a connection in seconds (default: 1800)
	ConnMaxIdleTimeSeconds int `json:"conn_max_idle_time_seconds,omitempty" yaml:"conn_max_idle_time_seconds,omitempty"`

	// EnableVectorSearch enables pgvector-based vector search
	EnableVectorSearch bool `json:"enable_vector_search" yaml:"enable_vector_search"`

	// VectorDimensions specifies the dimension of vector embeddings.
	// If 0, uses GetDefaultVectorDimensions().
	VectorDimensions int `json:"vector_dimensions" yaml:"vector_dimensions"`

	// RequireVector fails startup if pgvector extension is not available
	RequireVector bool `json:"require_vector" yaml:"require_vector"`
}

// Validate validates the configuration
func (c *Config) Validate() error {
	switch c.Driver {
	case DriverSQLite:
		if c.SQLite.Path == "" {
			return errors.New("sqlite path is required")
		}
	case DriverTurso:
		if c.Turso.Path == "" && c.Turso.ReplicaPath == "" {
			return errors.New("turso path is required")
		}
		if c.Turso.URL != "" && c.Turso.AuthToken == "" {
			return errors.New("turso auth_token is required")
		}
	case DriverPostgres:
		if c.Postgres.DSN == "" {
			return errors.New("postgres dsn is required")
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

// DefaultTursoConfig returns a default Turso configuration
func DefaultTursoConfig(url, authToken, dbName string) Config {
	path := ""
	if dbName != "" {
		path = dbName + ".turso"
	}
	return Config{
		Driver: DriverTurso,
		Turso: TursoConfig{
			Path:               path,
			URL:                url,
			AuthToken:          authToken,
			DatabaseName:       dbName,
			ReplicaPath:        path,
			EnableVectorSearch: false,
			VectorDimensions:   GetDefaultVectorDimensions(),
		},
	}
}

// DefaultTursoLocalConfig returns a local Rust-backed Turso configuration.
func DefaultTursoLocalConfig(path string, enableVectors bool) Config {
	return Config{
		Driver: DriverTurso,
		Turso: TursoConfig{
			Path:               path,
			EnableVectorSearch: enableVectors,
			VectorDimensions:   GetDefaultVectorDimensions(),
		},
	}
}

// DefaultPostgresConfig returns a default PostgreSQL configuration
func DefaultPostgresConfig(dsn string) Config {
	return Config{
		Driver: DriverPostgres,
		Postgres: PostgresConfig{
			DSN:              dsn,
			MaxOpenConns:     5,
			MaxIdleConns:     2,
			VectorDimensions: GetDefaultVectorDimensions(),
		},
	}
}
