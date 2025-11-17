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

	// Check if vector search should be enabled
	vectorEnv := fmt.Sprintf("AGENTCTL_%s_VECTOR_SEARCH", strings.ToUpper(prefix))
	enableVector := false
	if vectorStr := os.Getenv(vectorEnv); vectorStr != "" {
		enableVector = strings.ToLower(vectorStr) == "true" || vectorStr == "1"
	}

	// Get vector dimensions
	dimsEnv := fmt.Sprintf("AGENTCTL_%s_VECTOR_DIMS", strings.ToUpper(prefix))
	vectorDims := 384 // default
	if dimsStr := os.Getenv(dimsEnv); dimsStr != "" {
		if dims, err := strconv.Atoi(dimsStr); err == nil && dims > 0 {
			vectorDims = dims
		}
	}

	return Config{
		Driver: DriverLibSQL,
		LibSQL: LibSQLConfig{
			Path:               dbPath,
			EnableVectorSearch: enableVector,
			VectorDimensions:   vectorDims,
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

	// Check if vector search should be enabled (only relevant for memory database)
	vectorEnv := fmt.Sprintf("AGENTCTL_%s_VECTOR_SEARCH", strings.ToUpper(prefix))
	enableVector := false
	if vectorStr := os.Getenv(vectorEnv); vectorStr != "" {
		enableVector = strings.ToLower(vectorStr) == "true" || vectorStr == "1"
	}

	// Get vector dimensions
	dimsEnv := fmt.Sprintf("AGENTCTL_%s_VECTOR_DIMS", strings.ToUpper(prefix))
	vectorDims := 384 // default
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
	case DriverTurso:
		return fmt.Sprintf("Turso: %s (vector: %v)", cfg.Turso.URL, cfg.Turso.EnableVectorSearch)
	default:
		return "Unknown driver"
	}
}

// Environment variable documentation:
//
// For SQLite databases:
//   AGENTCTL_<DB>_DB_DRIVER=sqlite       # Database driver (sqlite or turso)
//   AGENTCTL_<DB>_DB_PATH=/path/to/db    # Path to SQLite database file
//   AGENTCTL_<DB>_DB_WAL=true            # Enable WAL mode (default: true)
//   AGENTCTL_<DB>_DB_TIMEOUT=5000        # Busy timeout in milliseconds
//
// For Turso databases:
//   AGENTCTL_<DB>_DB_DRIVER=turso        # Database driver
//   AGENTCTL_<DB>_DB_URL=libsql://...    # Turso database URL
//   AGENTCTL_<DB>_DB_TOKEN=...           # Turso auth token
//   AGENTCTL_TURSO_URL=libsql://...      # Fallback Turso URL for all databases
//   AGENTCTL_TURSO_TOKEN=...             # Fallback Turso token for all databases
//
// For vector search (memory database only):
//   AGENTCTL_MEMORY_VECTOR_SEARCH=true   # Enable vector search
//   AGENTCTL_MEMORY_VECTOR_DIMS=384      # Vector dimensions (default: 384)
//
// Where <DB> is one of: CACHE, JOBS, or MEMORY
