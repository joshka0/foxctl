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
	// Root directory for SQLite databases (default: ~/.agentctl)
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

// loadConfig loads configuration for a specific database
// prefix is the environment variable prefix (e.g., "CACHE", "JOBS", "MEMORY")
// defaultPath is the default SQLite database path
func (cl *ConfigLoader) loadConfig(prefix, defaultPath string) Config {
	// Check if driver is specified via environment variable
	// Format: AGENTCTL_<PREFIX>_DB_DRIVER (e.g., AGENTCTL_CACHE_DB_DRIVER)
	driverEnv := fmt.Sprintf("AGENTCTL_%s_DB_DRIVER", strings.ToUpper(prefix))
	driver := os.Getenv(driverEnv)

	// Default to SQLite if not specified
	if driver == "" {
		driver = string(DriverSQLite)
	}

	driverType := DriverType(strings.ToLower(driver))

	switch driverType {
	case DriverLibSQL:
		return cl.loadLibSQLConfig(prefix, defaultPath)
	case DriverTurso:
		return cl.loadTursoConfig(prefix, strings.ToLower(prefix))
	case DriverSQLite:
		fallthrough
	default:
		return cl.loadSQLiteConfig(prefix, defaultPath)
	}
}

// loadSQLiteConfig loads SQLite configuration
func (cl *ConfigLoader) loadSQLiteConfig(prefix, defaultPath string) Config {
	// Check for custom path via environment variable
	// Format: AGENTCTL_<PREFIX>_DB_PATH (e.g., AGENTCTL_CACHE_DB_PATH)
	pathEnv := fmt.Sprintf("AGENTCTL_%s_DB_PATH", strings.ToUpper(prefix))
	dbPath := os.Getenv(pathEnv)

	if dbPath == "" {
		dbPath = filepath.Join(cl.rootDir, defaultPath)
	}

	// Check for WAL mode setting
	walEnv := fmt.Sprintf("AGENTCTL_%s_DB_WAL", strings.ToUpper(prefix))
	enableWAL := true
	if walStr := os.Getenv(walEnv); walStr != "" {
		enableWAL = strings.ToLower(walStr) != "false" && walStr != "0"
	}

	// Check for busy timeout setting
	timeoutEnv := fmt.Sprintf("AGENTCTL_%s_DB_TIMEOUT", strings.ToUpper(prefix))
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

// loadLibSQLConfig loads local libSQL configuration
func (cl *ConfigLoader) loadLibSQLConfig(prefix, defaultPath string) Config {
	// Check for custom path via environment variable
	// Format: AGENTCTL_<PREFIX>_DB_PATH (e.g., AGENTCTL_MEMORY_DB_PATH)
	pathEnv := fmt.Sprintf("AGENTCTL_%s_DB_PATH", strings.ToUpper(prefix))
	dbPath := os.Getenv(pathEnv)

	if dbPath == "" {
		// Default to .libsql extension to distinguish from SQLite
		dbPath = filepath.Join(cl.rootDir, strings.Replace(defaultPath, ".db", ".libsql", 1))
	}

	// Check if vector search should be enabled (default: true for MEMORY, false for others)
	vectorEnv := fmt.Sprintf("AGENTCTL_%s_VECTOR_SEARCH", strings.ToUpper(prefix))
	// Default to true for memory database since it benefits most from vector search
	enableVector := strings.ToUpper(prefix) == "MEMORY"
	if vectorStr := os.Getenv(vectorEnv); vectorStr != "" {
		enableVector = strings.ToLower(vectorStr) == "true" || vectorStr == "1"
	}

	// Get vector dimensions (check per-database env var, then global default)
	dimsEnv := fmt.Sprintf("AGENTCTL_%s_VECTOR_DIMS", strings.ToUpper(prefix))
	vectorDims := GetDefaultVectorDimensions()
	if dimsStr := os.Getenv(dimsEnv); dimsStr != "" {
		if dims, err := strconv.Atoi(dimsStr); err == nil && dims > 0 {
			vectorDims = dims
		}
	}

	// Check for remote sync URL (enables embedded replica mode)
	// Format: AGENTCTL_<PREFIX>_SYNC_URL or AGENTCTL_LIBSQL_SYNC_URL (fallback)
	syncURLEnv := fmt.Sprintf("AGENTCTL_%s_SYNC_URL", strings.ToUpper(prefix))
	syncURL := os.Getenv(syncURLEnv)
	if syncURL == "" {
		syncURL = os.Getenv("AGENTCTL_LIBSQL_SYNC_URL")
	}

	// Check for sync auth token
	// Format: AGENTCTL_<PREFIX>_SYNC_TOKEN or AGENTCTL_LIBSQL_SYNC_TOKEN (fallback)
	syncTokenEnv := fmt.Sprintf("AGENTCTL_%s_SYNC_TOKEN", strings.ToUpper(prefix))
	syncToken := os.Getenv(syncTokenEnv)
	if syncToken == "" {
		syncToken = os.Getenv("AGENTCTL_LIBSQL_SYNC_TOKEN")
	}

	// Check for sync interval (seconds, 0 = sync on demand)
	syncIntervalEnv := fmt.Sprintf("AGENTCTL_%s_SYNC_INTERVAL", strings.ToUpper(prefix))
	syncInterval := 0
	if intervalStr := os.Getenv(syncIntervalEnv); intervalStr != "" {
		if interval, err := strconv.Atoi(intervalStr); err == nil && interval > 0 {
			syncInterval = interval
		}
	}

	return Config{
		Driver: DriverLibSQL,
		LibSQL: LibSQLConfig{
			Path:               dbPath,
			EnableVectorSearch: enableVector,
			VectorDimensions:   vectorDims,
			SyncURL:            syncURL,
			AuthToken:          syncToken,
			SyncInterval:       syncInterval,
		},
	}
}

// loadTursoConfig loads Turso configuration
func (cl *ConfigLoader) loadTursoConfig(prefix, dbName string) Config {
	// Get Turso URL
	// Format: AGENTCTL_<PREFIX>_DB_URL or AGENTCTL_TURSO_URL (fallback)
	urlEnv := fmt.Sprintf("AGENTCTL_%s_DB_URL", strings.ToUpper(prefix))
	url := os.Getenv(urlEnv)
	if url == "" {
		url = os.Getenv("AGENTCTL_TURSO_URL")
	}

	// Get Turso auth token
	// Format: AGENTCTL_<PREFIX>_DB_TOKEN or AGENTCTL_TURSO_TOKEN (fallback)
	tokenEnv := fmt.Sprintf("AGENTCTL_%s_DB_TOKEN", strings.ToUpper(prefix))
	token := os.Getenv(tokenEnv)
	if token == "" {
		token = os.Getenv("AGENTCTL_TURSO_TOKEN")
	}

	// Check if vector search should be enabled (default: true for MEMORY, false for others)
	vectorEnv := fmt.Sprintf("AGENTCTL_%s_VECTOR_SEARCH", strings.ToUpper(prefix))
	// Default to true for memory database since it benefits most from vector search
	enableVector := strings.ToUpper(prefix) == "MEMORY"
	if vectorStr := os.Getenv(vectorEnv); vectorStr != "" {
		enableVector = strings.ToLower(vectorStr) == "true" || vectorStr == "1"
	}

	// Get vector dimensions (check per-database env var, then global default)
	dimsEnv := fmt.Sprintf("AGENTCTL_%s_VECTOR_DIMS", strings.ToUpper(prefix))
	vectorDims := GetDefaultVectorDimensions()
	if dimsStr := os.Getenv(dimsEnv); dimsStr != "" {
		if dims, err := strconv.Atoi(dimsStr); err == nil && dims > 0 {
			vectorDims = dims
		}
	}

	return Config{
		Driver: DriverTurso,
		Turso: TursoConfig{
			URL:                url,
			AuthToken:          token,
			DatabaseName:       dbName,
			EnableVectorSearch: enableVector,
			VectorDimensions:   vectorDims,
		},
	}
}

// GetConfigSummary returns a human-readable summary of the configuration
func GetConfigSummary(cfg Config) string {
	switch cfg.Driver {
	case DriverSQLite:
		return fmt.Sprintf("SQLite: %s", cfg.SQLite.Path)
	case DriverLibSQL:
		if cfg.LibSQL.SyncURL != "" {
			return fmt.Sprintf("LibSQL: %s (sync: %s, vector: %v)", cfg.LibSQL.Path, cfg.LibSQL.SyncURL, cfg.LibSQL.EnableVectorSearch)
		}
		return fmt.Sprintf("LibSQL: %s (local-only, vector: %v)", cfg.LibSQL.Path, cfg.LibSQL.EnableVectorSearch)
	case DriverTurso:
		return fmt.Sprintf("Turso: %s (vector: %v)", cfg.Turso.URL, cfg.Turso.EnableVectorSearch)
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
				URL:                settings.TursoURL,
				AuthToken:          settings.TursoAuthToken,
				DatabaseName:       dbName,
				EnableVectorSearch: settings.VectorEnabled,
				VectorDimensions:   dims,
			},
		}

	case DriverLibSQL:
		dims := settings.VectorDimensions
		if dims == 0 {
			dims = GetDefaultVectorDimensions()
		}
		return Config{
			Driver: DriverLibSQL,
			LibSQL: LibSQLConfig{
				Path:               filepath.Join(cl.rootDir, dbName+".db"),
				EnableVectorSearch: settings.VectorEnabled,
				VectorDimensions:   dims,
			},
		}

	default:
		// SQLite (default)
		return DefaultSQLiteConfig(filepath.Join(cl.rootDir, dbName+".db"))
	}
}

// Environment variable documentation:
//
// For SQLite databases:
//   AGENTCTL_<DB>_DB_DRIVER=sqlite       # Database driver (sqlite, libsql, or turso)
//   AGENTCTL_<DB>_DB_PATH=/path/to/db    # Path to SQLite database file
//   AGENTCTL_<DB>_DB_WAL=true            # Enable WAL mode (default: true)
//   AGENTCTL_<DB>_DB_TIMEOUT=5000        # Busy timeout in milliseconds
//
// For libSQL databases (local-first with optional sync):
//   AGENTCTL_<DB>_DB_DRIVER=libsql       # Database driver
//   AGENTCTL_<DB>_DB_PATH=/path/to/db    # Path to libSQL database file
//   AGENTCTL_<DB>_SYNC_URL=http://...    # Remote sqld URL for sync (optional)
//   AGENTCTL_<DB>_SYNC_TOKEN=...         # Auth token for sync (optional)
//   AGENTCTL_<DB>_SYNC_INTERVAL=60       # Background sync interval in seconds (optional, 0=on-demand)
//   AGENTCTL_LIBSQL_SYNC_URL=http://...  # Fallback sync URL for all libSQL databases
//   AGENTCTL_LIBSQL_SYNC_TOKEN=...       # Fallback sync token for all libSQL databases
//
// For Turso databases (cloud-native):
//   AGENTCTL_<DB>_DB_DRIVER=turso        # Database driver
//   AGENTCTL_<DB>_DB_URL=libsql://...    # Turso database URL
//   AGENTCTL_<DB>_DB_TOKEN=...           # Turso auth token
//   AGENTCTL_TURSO_URL=libsql://...      # Fallback Turso URL for all databases
//   AGENTCTL_TURSO_TOKEN=...             # Fallback Turso token for all databases
//
// For vector search (memory database only):
//   AGENTCTL_MEMORY_VECTOR_SEARCH=true   # Enable vector search
//   AGENTCTL_MEMORY_VECTOR_DIMS=1024     # Per-database dimensions override
//   AGENTCTL_VECTOR_DIMS=1024            # Global default (1024 for Voyage, 3072 for Gemini)
//
// Where <DB> is one of: CACHE, JOBS, or MEMORY
