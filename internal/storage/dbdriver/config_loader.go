package dbdriver

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ConfigLoader loads database configuration from environment variables
type ConfigLoader struct {
	// Root directory for SQLite databases (default: ~/.foxctl)
	rootDir string
}

// NewConfigLoader creates a new configuration loader
func NewConfigLoader(rootDir string) *ConfigLoader {
	return &ConfigLoader{
		rootDir: rootDir,
	}
}

// LoadCacheConfig loads configuration for the cache database
func (cl *ConfigLoader) LoadCacheConfig() Config {
	return cl.loadConfig("CACHE", "cache.db")
}

// LoadJobsConfig loads configuration for the jobs database
func (cl *ConfigLoader) LoadJobsConfig() Config {
	return cl.loadConfig("JOBS", "jobs.db")
}

// LoadMemoryConfig loads configuration for the memory database
func (cl *ConfigLoader) LoadMemoryConfig() Config {
	return cl.loadConfig("MEMORY", "memory.db")
}

// LoadConfig loads configuration for an arbitrary store database.
// This is a generalization of the named helpers (CACHE/JOBS/MEMORY).
//
// Index:
// - Purpose: Allow non-core stores to use the same driver selection and env-var configuration model
// - Flow: normalize store name → select driver from env → build driver-specific config → return
// - SideEffects: none
// - FailureModes: none (invalid/missing required fields surface during OpenDB via cfg.Validate)
// - Related: LoadCacheConfig, LoadJobsConfig, LoadMemoryConfig
// - Keywords: dbdriver, config_loader, store_config
func (cl *ConfigLoader) LoadConfig(storeName, defaultPath string) Config {
	return cl.loadConfig(storeName, defaultPath)
}

// loadConfig loads configuration for a specific database
// prefix is the environment variable prefix (e.g., "CACHE", "JOBS", "MEMORY")
// defaultPath is the default SQLite database path
func (cl *ConfigLoader) loadConfig(prefix, defaultPath string) Config {
	// Check if driver is specified via environment variable
	// Format: FOXCTL_<PREFIX>_DB_DRIVER (e.g., FOXCTL_CACHE_DB_DRIVER)
	driverEnv := fmt.Sprintf("FOXCTL_%s_DB_DRIVER", strings.ToUpper(prefix))
	driver := os.Getenv(driverEnv)

	// Global fallback (applies to all stores) when per-store driver is not set.
	if driver == "" {
		driver = os.Getenv("FOXCTL_DB_DRIVER")
	}

	// Default to SQLite if not specified
	if driver == "" {
		driver = string(DriverSQLite)
	}

	driverType := DriverType(strings.ToLower(driver))

	switch driverType {
	case DriverTurso:
		return cl.loadTursoConfig(prefix, defaultPath, strings.ToLower(prefix))
	case DriverPostgres:
		return cl.loadPostgresConfig(prefix)
	case DriverSQLite:
		return cl.loadSQLiteConfig(prefix, defaultPath)
	default:
		return Config{Driver: driverType}
	}
}

// loadSQLiteConfig loads SQLite configuration
func (cl *ConfigLoader) loadSQLiteConfig(prefix, defaultPath string) Config {
	// Check for custom path via environment variable
	// Format: FOXCTL_<PREFIX>_DB_PATH (e.g., FOXCTL_CACHE_DB_PATH)
	pathEnv := fmt.Sprintf("FOXCTL_%s_DB_PATH", strings.ToUpper(prefix))
	dbPath := os.Getenv(pathEnv)

	if dbPath == "" {
		dbPath = filepath.Join(cl.rootDir, defaultPath)
	}

	// Check for WAL mode setting
	walEnv := fmt.Sprintf("FOXCTL_%s_DB_WAL", strings.ToUpper(prefix))
	enableWAL := true
	if walStr := os.Getenv(walEnv); walStr != "" {
		enableWAL = strings.ToLower(walStr) != "false" && walStr != "0"
	}

	// Check for busy timeout setting
	timeoutEnv := fmt.Sprintf("FOXCTL_%s_DB_TIMEOUT", strings.ToUpper(prefix))
	busyTimeout := 5000
	if timeoutStr := os.Getenv(timeoutEnv); timeoutStr != "" {
		if timeout, err := strconv.Atoi(timeoutStr); err == nil {
			busyTimeout = timeout
		}
	}

	return Config{
		Driver: DriverSQLite,
		SQLite: SQLiteConfig{
			Path:        dbPath,
			EnableWAL:   enableWAL,
			BusyTimeout: busyTimeout,
		},
	}
}

// loadTursoConfig loads Turso configuration.
func (cl *ConfigLoader) loadTursoConfig(prefix, defaultPath, dbName string) Config {
	// Get Turso URL
	// Format: FOXCTL_<PREFIX>_DB_URL or FOXCTL_TURSO_URL (fallback)
	urlEnv := fmt.Sprintf("FOXCTL_%s_DB_URL", strings.ToUpper(prefix))
	url := os.Getenv(urlEnv)
	if url == "" {
		url = os.Getenv("FOXCTL_TURSO_URL")
	}

	// Get Turso auth token
	// Format: FOXCTL_<PREFIX>_DB_TOKEN or FOXCTL_TURSO_TOKEN (fallback)
	tokenEnv := fmt.Sprintf("FOXCTL_%s_DB_TOKEN", strings.ToUpper(prefix))
	token := os.Getenv(tokenEnv)
	if token == "" {
		token = os.Getenv("FOXCTL_TURSO_TOKEN")
	}

	// Replica path (persistent embedded replica file).
	// Use FOXCTL_<PREFIX>_DB_PATH when set, otherwise default under rootDir.
	replicaPathEnv := fmt.Sprintf("FOXCTL_%s_DB_PATH", strings.ToUpper(prefix))
	replicaPath := os.Getenv(replicaPathEnv)
	if replicaPath == "" && defaultPath != "" {
		// Match existing memory factory behavior (e.g., memory.turso.replica).
		name := strings.Replace(defaultPath, ".db", ".turso.replica", 1)
		replicaPath = filepath.Join(cl.rootDir, name)
	}

	// Check if vector search should be enabled (default: true for MEMORY, false for others)
	vectorEnv := fmt.Sprintf("FOXCTL_%s_VECTOR_SEARCH", strings.ToUpper(prefix))
	// Default to true for memory database since it benefits most from vector search
	enableVector := strings.ToUpper(prefix) == "MEMORY"
	if vectorStr := os.Getenv(vectorEnv); vectorStr != "" {
		enableVector = strings.ToLower(vectorStr) == "true" || vectorStr == "1"
	}

	// Get vector dimensions (check per-database env var, then global default)
	dimsEnv := fmt.Sprintf("FOXCTL_%s_VECTOR_DIMS", strings.ToUpper(prefix))
	vectorDims := GetDefaultVectorDimensions()
	if dimsStr := os.Getenv(dimsEnv); dimsStr != "" {
		if dims, err := strconv.Atoi(dimsStr); err == nil && dims > 0 {
			vectorDims = dims
		}
	}

	// Check for sync interval (seconds, 0 = sync on demand).
	syncIntervalEnv := fmt.Sprintf("FOXCTL_%s_SYNC_INTERVAL", strings.ToUpper(prefix))
	syncInterval := 0
	if intervalStr := os.Getenv(syncIntervalEnv); intervalStr != "" {
		if interval, err := strconv.Atoi(intervalStr); err == nil && interval > 0 {
			syncInterval = interval
		}
	}

	return Config{
		Driver: DriverTurso,
		Turso: TursoConfig{
			Path:               replicaPath,
			URL:                url,
			AuthToken:          token,
			DatabaseName:       dbName,
			ReplicaPath:        replicaPath,
			SyncInterval:       syncInterval,
			EnableVectorSearch: enableVector,
			VectorDimensions:   vectorDims,
		},
	}
}

// loadPostgresConfig loads PostgreSQL configuration from environment variables.
func (cl *ConfigLoader) loadPostgresConfig(prefix string) Config {
	// DSN: per-store override, then global FOXCTL_POSTGRES_DSN, then DATABASE_URL
	dsnEnv := fmt.Sprintf("FOXCTL_%s_POSTGRES_DSN", strings.ToUpper(prefix))
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		dsn = os.Getenv("FOXCTL_POSTGRES_DSN")
	}
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}

	// Schema: default to lowercase store name for isolation
	schemaEnv := fmt.Sprintf("FOXCTL_%s_POSTGRES_SCHEMA", strings.ToUpper(prefix))
	schema := os.Getenv(schemaEnv)
	if schema == "" {
		schema = strings.ToLower(prefix)
	}

	// Connection pool settings
	maxOpenConns := 5
	if v := os.Getenv("FOXCTL_POSTGRES_MAX_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxOpenConns = n
		}
	}
	maxIdleConns := 2
	if v := os.Getenv("FOXCTL_POSTGRES_MAX_IDLE_CONNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxIdleConns = n
		}
	}

	// Vector search settings (same pattern as other vector-capable stores)
	vectorEnv := fmt.Sprintf("FOXCTL_%s_VECTOR_SEARCH", strings.ToUpper(prefix))
	enableVector := strings.ToUpper(prefix) == "MEMORY"
	if vectorStr := os.Getenv(vectorEnv); vectorStr != "" {
		enableVector = strings.ToLower(vectorStr) == "true" || vectorStr == "1"
	}

	dimsEnv := fmt.Sprintf("FOXCTL_%s_VECTOR_DIMS", strings.ToUpper(prefix))
	vectorDims := GetDefaultVectorDimensions()
	if dimsStr := os.Getenv(dimsEnv); dimsStr != "" {
		if dims, err := strconv.Atoi(dimsStr); err == nil && dims > 0 {
			vectorDims = dims
		}
	}

	requireVector := false
	if v := os.Getenv("FOXCTL_POSTGRES_REQUIRE_VECTOR"); v == "true" || v == "1" {
		requireVector = true
	}

	return Config{
		Driver: DriverPostgres,
		Postgres: PostgresConfig{
			DSN:                dsn,
			Schema:             schema,
			MaxOpenConns:       maxOpenConns,
			MaxIdleConns:       maxIdleConns,
			EnableVectorSearch: enableVector,
			VectorDimensions:   vectorDims,
			RequireVector:      requireVector,
		},
	}
}

// GetConfigSummary returns a human-readable summary of the configuration
func GetConfigSummary(cfg Config) string {
	switch cfg.Driver {
	case DriverSQLite:
		return fmt.Sprintf("SQLite: %s", cfg.SQLite.Path)
	case DriverTurso:
		if cfg.Turso.URL != "" {
			return fmt.Sprintf("Turso: %s (path: %s, vector: %v)", cfg.Turso.URL, cfg.Turso.Path, cfg.Turso.EnableVectorSearch)
		}
		return fmt.Sprintf("Turso: %s (local-only, vector: %v)", cfg.Turso.Path, cfg.Turso.EnableVectorSearch)
	case DriverPostgres:
		// Redact password from DSN for display
		dsn := cfg.Postgres.DSN
		idxAt := strings.Index(dsn, "@")
		idxProto := strings.Index(dsn, "://")
		if idxAt > 0 && idxProto >= 0 && idxProto+3 < len(dsn) {
			dsn = dsn[:idxProto+3] + "***@" + dsn[idxAt+1:]
		}
		return fmt.Sprintf("Postgres: %s (schema: %s, vector: %v)", dsn, cfg.Postgres.Schema, cfg.Postgres.EnableVectorSearch)
	default:
		return "Unknown driver"
	}
}

// PlatformDatabaseSettings represents the database settings from platform config.
// This is a simplified interface to avoid import cycles with platform/config.
type PlatformDatabaseSettings struct {
	Driver           string
	TursoURL         string
	TursoAuthToken   string
	PostgresDSN      string
	PostgresSchema   string
	VectorEnabled    bool
	VectorDimensions int
}

// ConfigFromPlatformSettings creates a dbdriver.Config from platform config settings.
// This is the preferred way to configure database connections when using the full
// platform config system rather than environment variables.
func (cl *ConfigLoader) ConfigFromPlatformSettings(settings PlatformDatabaseSettings, dbName string) Config {
	driver := DriverType(strings.ToLower(settings.Driver))
	if driver == "" {
		driver = DriverSQLite
	}

	switch driver {
	case DriverTurso:
		dims := settings.VectorDimensions
		if dims == 0 {
			dims = GetDefaultVectorDimensions()
		}
		return Config{
			Driver: DriverTurso,
			Turso: TursoConfig{
				Path:               filepath.Join(cl.rootDir, dbName+".turso"),
				URL:                settings.TursoURL,
				AuthToken:          settings.TursoAuthToken,
				DatabaseName:       dbName,
				ReplicaPath:        filepath.Join(cl.rootDir, dbName+".turso"),
				EnableVectorSearch: settings.VectorEnabled,
				VectorDimensions:   dims,
			},
		}

	case DriverPostgres:
		dims := settings.VectorDimensions
		if dims == 0 {
			dims = GetDefaultVectorDimensions()
		}
		schema := settings.PostgresSchema
		if schema == "" {
			schema = strings.ToLower(dbName)
		}
		return Config{
			Driver: DriverPostgres,
			Postgres: PostgresConfig{
				DSN:                settings.PostgresDSN,
				Schema:             schema,
				MaxOpenConns:       5,
				MaxIdleConns:       2,
				EnableVectorSearch: settings.VectorEnabled,
				VectorDimensions:   dims,
			},
		}

	case DriverSQLite:
		return DefaultSQLiteConfig(filepath.Join(cl.rootDir, dbName+".db"))

	default:
		return Config{Driver: driver}
	}
}

// Environment variable documentation:
//
// For SQLite databases:
//   FOXCTL_<DB>_DB_DRIVER=sqlite       # Database driver (sqlite, turso, or postgres)
//   FOXCTL_<DB>_DB_PATH=/path/to/db    # Path to SQLite database file
//   FOXCTL_<DB>_DB_WAL=true            # Enable WAL mode (default: true)
//   FOXCTL_<DB>_DB_TIMEOUT=5000        # Busy timeout in milliseconds
//
// For Turso databases (local or remote sync):
//   FOXCTL_<DB>_DB_DRIVER=turso        # Database driver
//   FOXCTL_<DB>_DB_PATH=/path/to/db    # Local Turso database path
//   FOXCTL_<DB>_DB_URL=libsql://...    # Optional remote Turso database URL
//   FOXCTL_<DB>_DB_TOKEN=...           # Turso auth token when DB_URL is set
//   FOXCTL_TURSO_URL=libsql://...      # Fallback Turso URL for all databases
//   FOXCTL_TURSO_TOKEN=...             # Fallback Turso token for all databases
//
// For vector search (memory database only):
//   FOXCTL_MEMORY_VECTOR_SEARCH=true   # Enable vector search
//   FOXCTL_MEMORY_VECTOR_DIMS=1024     # Per-database dimensions override
//   FOXCTL_VECTOR_DIMS=4096            # Global default (4096 for Qwen3 Embedding 8B)
//
// Where <DB> is one of: CACHE, JOBS, or MEMORY
//
// For store configuration beyond those core stores, foxctl uses the same
// `FOXCTL_<STORE>_...` environment variable pattern (e.g., `FOXCTL_SESSIONS_DB_DRIVER`).
