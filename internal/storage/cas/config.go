package cas

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DriverType specifies the CAS storage backend.
type DriverType string

const (
	// DriverFile uses the filesystem for storage (legacy).
	DriverFile DriverType = "file"
	// DriverSQLite uses SQLite for storage (default).
	DriverSQLite DriverType = "sqlite"
	// DriverTurso uses Turso cloud for storage.
	DriverTurso DriverType = "turso"
	// DriverS3 uses S3/MinIO for storage (enterprise).
	DriverS3 DriverType = "s3"
)

// Config holds CAS storage configuration.
type Config struct {
	// Driver specifies which storage backend to use.
	Driver DriverType `json:"driver" yaml:"driver"`

	// File is the configuration for file-based storage.
	File FileConfig `json:"file,omitempty" yaml:"file,omitempty"`

	// SQLite is the configuration for SQLite-based storage.
	SQLite SQLiteConfig `json:"sqlite,omitempty" yaml:"sqlite,omitempty"`

	// Turso is the configuration for Turso-based storage.
	Turso TursoConfig `json:"turso,omitempty" yaml:"turso,omitempty"`

	// S3 is the configuration for S3/MinIO-based storage.
	S3 S3Config `json:"s3,omitempty" yaml:"s3,omitempty"`

	// Migration controls auto-migration behavior.
	Migration MigrationConfig `json:"migration,omitempty" yaml:"migration,omitempty"`
}

// FileConfig configures file-based CAS storage.
type FileConfig struct {
	// Path is the root directory for CAS storage.
	Path string `json:"path" yaml:"path"`
}

// SQLiteConfig configures SQLite-based CAS storage.
type SQLiteConfig struct {
	// DBPath is the path to the SQLite database file.
	DBPath string `json:"db_path" yaml:"db_path"`

	// BlobThreshold is the size threshold in bytes for inline vs external storage.
	// Objects smaller than this are stored inline in the database.
	// Default: 1MB (1048576 bytes).
	BlobThreshold int64 `json:"blob_threshold,omitempty" yaml:"blob_threshold,omitempty"`

	// EnableWAL enables Write-Ahead Logging.
	EnableWAL bool `json:"enable_wal" yaml:"enable_wal"`

	// BusyTimeout in milliseconds.
	BusyTimeout int `json:"busy_timeout" yaml:"busy_timeout"`
}

// TursoConfig configures Turso-based CAS storage.
type TursoConfig struct {
	// URL is the Turso database URL.
	URL string `json:"url" yaml:"url"`

	// AuthToken is the Turso authentication token.
	AuthToken string `json:"auth_token" yaml:"auth_token"`

	// BlobThreshold is the size threshold for inline storage.
	// Objects smaller than this are stored inline in Turso.
	// Larger objects require external bucket storage (future).
	// Default: 10MB (10485760 bytes).
	BlobThreshold int64 `json:"blob_threshold,omitempty" yaml:"blob_threshold,omitempty"`

	// ReplicaPath is the local path for embedded replica.
	ReplicaPath string `json:"replica_path,omitempty" yaml:"replica_path,omitempty"`
}

// S3Config configures S3/MinIO-based CAS storage.
type S3Config struct {
	// Bucket is the S3 bucket name (required).
	Bucket string `json:"bucket" yaml:"bucket"`

	// Region is the AWS region (default: us-east-1).
	Region string `json:"region,omitempty" yaml:"region,omitempty"`

	// Endpoint is the S3 endpoint URL (required for MinIO, optional for AWS).
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`

	// Prefix is the key prefix for all objects (default: "cas/").
	Prefix string `json:"prefix,omitempty" yaml:"prefix,omitempty"`

	// ForcePathStyle uses path-style addressing (required for MinIO).
	ForcePathStyle bool `json:"force_path_style,omitempty" yaml:"force_path_style,omitempty"`

	// DisableSSL disables HTTPS for the endpoint (for local MinIO).
	DisableSSL bool `json:"disable_ssl,omitempty" yaml:"disable_ssl,omitempty"`
}

// MigrationConfig controls auto-migration behavior.
type MigrationConfig struct {
	// AutoMigrate enables automatic migration from file-based CAS.
	AutoMigrate bool `json:"auto_migrate" yaml:"auto_migrate"`

	// SourcePath is the path to the legacy file-based CAS.
	// If empty, defaults to ~/.foxctl/cas.
	SourcePath string `json:"source_path,omitempty" yaml:"source_path,omitempty"`
}

// Validate checks the configuration for errors.
func (c *Config) Validate() error {
	switch c.Driver {
	case DriverFile:
		if c.File.Path == "" {
			return errors.New("cas: file path is required")
		}
	case DriverSQLite:
		if c.SQLite.DBPath == "" {
			return errors.New("cas: sqlite db_path is required")
		}
	case DriverTurso:
		if c.Turso.URL == "" {
			return errors.New("cas: turso url is required")
		}
		if c.Turso.AuthToken == "" {
			return errors.New("cas: turso auth_token is required")
		}
	case DriverS3:
		if c.S3.Bucket == "" {
			return errors.New("cas: s3 bucket is required")
		}
	case "":
		return errors.New("cas: driver is required")
	default:
		return fmt.Errorf("cas: unsupported driver: %s", c.Driver)
	}
	return nil
}

// DefaultConfig returns the default CAS configuration.
func DefaultConfig(rootDir string) Config {
	return Config{
		Driver: DriverSQLite,
		SQLite: SQLiteConfig{
			DBPath:        filepath.Join(rootDir, "storage", "cas.db"),
			BlobThreshold: 1 << 20, // 1MB
			EnableWAL:     true,
			BusyTimeout:   5000,
		},
		Migration: MigrationConfig{
			AutoMigrate: true,
			SourcePath:  filepath.Join(rootDir, "cas"),
		},
	}
}

// DefaultFileConfig returns a file-based CAS configuration.
func DefaultFileConfig(rootDir string) Config {
	return Config{
		Driver: DriverFile,
		File: FileConfig{
			Path: filepath.Join(rootDir, "cas"),
		},
	}
}

// LoadConfig loads CAS configuration from environment variables.
func LoadConfig(rootDir string) Config {
	driver := os.Getenv("FOXCTL_CAS_DRIVER")
	if driver == "" {
		driver = string(DriverSQLite)
	}

	driverType := DriverType(strings.ToLower(driver))

	switch driverType {
	case DriverFile:
		return loadFileConfig(rootDir)
	case DriverTurso:
		return loadTursoConfig(rootDir)
	case DriverS3:
		return loadS3Config()
	case DriverSQLite:
		fallthrough
	default:
		return loadSQLiteConfig(rootDir)
	}
}

func loadFileConfig(rootDir string) Config {
	path := os.Getenv("FOXCTL_CAS_PATH")
	if path == "" {
		path = filepath.Join(rootDir, "cas")
	}

	return Config{
		Driver: DriverFile,
		File: FileConfig{
			Path: path,
		},
	}
}

func loadSQLiteConfig(rootDir string) Config {
	dbPath := os.Getenv("FOXCTL_CAS_DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(rootDir, "storage", "cas.db")
	}

	// Parse blob threshold (default 1MB)
	threshold := int64(1 << 20)
	if thresholdStr := os.Getenv("FOXCTL_CAS_BLOB_THRESHOLD"); thresholdStr != "" {
		if parsed, err := strconv.ParseInt(thresholdStr, 10, 64); err == nil && parsed > 0 {
			threshold = parsed
		}
	}

	// Parse WAL mode (default true)
	enableWAL := true
	if walStr := os.Getenv("FOXCTL_CAS_DB_WAL"); walStr != "" {
		enableWAL = strings.ToLower(walStr) != "false" && walStr != "0"
	}

	// Parse busy timeout (default 5000ms)
	busyTimeout := 5000
	if timeoutStr := os.Getenv("FOXCTL_CAS_DB_TIMEOUT"); timeoutStr != "" {
		if parsed, err := strconv.Atoi(timeoutStr); err == nil {
			busyTimeout = parsed
		}
	}

	// Parse auto-migrate (default true)
	autoMigrate := true
	if migrateStr := os.Getenv("FOXCTL_CAS_AUTO_MIGRATE"); migrateStr != "" {
		autoMigrate = strings.ToLower(migrateStr) != "false" && migrateStr != "0"
	}

	sourcePath := os.Getenv("FOXCTL_CAS_PATH")
	if sourcePath == "" {
		sourcePath = filepath.Join(rootDir, "cas")
	}

	return Config{
		Driver: DriverSQLite,
		SQLite: SQLiteConfig{
			DBPath:        dbPath,
			BlobThreshold: threshold,
			EnableWAL:     enableWAL,
			BusyTimeout:   busyTimeout,
		},
		Migration: MigrationConfig{
			AutoMigrate: autoMigrate,
			SourcePath:  sourcePath,
		},
	}
}

func loadS3Config() Config {
	bucket := os.Getenv("FOXCTL_CAS_S3_BUCKET")
	region := os.Getenv("FOXCTL_CAS_S3_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}
	endpoint := os.Getenv("FOXCTL_CAS_S3_ENDPOINT")
	prefix := os.Getenv("FOXCTL_CAS_S3_PREFIX")
	if prefix == "" {
		prefix = "cas/"
	}
	forcePathStyle, _ := strconv.ParseBool(os.Getenv("FOXCTL_CAS_S3_FORCE_PATH_STYLE"))
	disableSSL, _ := strconv.ParseBool(os.Getenv("FOXCTL_CAS_S3_DISABLE_SSL"))

	return Config{
		Driver: DriverS3,
		S3: S3Config{
			Bucket:         bucket,
			Region:         region,
			Endpoint:       endpoint,
			Prefix:         prefix,
			ForcePathStyle: forcePathStyle,
			DisableSSL:     disableSSL,
		},
		Migration: MigrationConfig{
			AutoMigrate: false,
		},
	}
}

func loadTursoConfig(rootDir string) Config {
	// Get URL (prefer CAS-specific, fallback to global)
	url := os.Getenv("FOXCTL_CAS_DB_URL")
	if url == "" {
		url = os.Getenv("FOXCTL_TURSO_URL")
	}

	// Get auth token (prefer CAS-specific, fallback to global)
	token := os.Getenv("FOXCTL_CAS_DB_TOKEN")
	if token == "" {
		token = os.Getenv("FOXCTL_TURSO_TOKEN")
	}

	// Parse blob threshold (default 10MB for Turso)
	threshold := int64(10 << 20)
	if thresholdStr := os.Getenv("FOXCTL_CAS_BLOB_THRESHOLD"); thresholdStr != "" {
		if parsed, err := strconv.ParseInt(thresholdStr, 10, 64); err == nil && parsed > 0 {
			threshold = parsed
		}
	}

	// Replica path for local caching
	replicaPath := os.Getenv("FOXCTL_CAS_REPLICA_PATH")

	// Parse auto-migrate
	autoMigrate := true
	if migrateStr := os.Getenv("FOXCTL_CAS_AUTO_MIGRATE"); migrateStr != "" {
		autoMigrate = strings.ToLower(migrateStr) != "false" && migrateStr != "0"
	}

	sourcePath := os.Getenv("FOXCTL_CAS_PATH")
	if sourcePath == "" {
		sourcePath = filepath.Join(rootDir, "cas")
	}

	return Config{
		Driver: DriverTurso,
		Turso: TursoConfig{
			URL:           url,
			AuthToken:     token,
			BlobThreshold: threshold,
			ReplicaPath:   replicaPath,
		},
		Migration: MigrationConfig{
			AutoMigrate: autoMigrate,
			SourcePath:  sourcePath,
		},
	}
}
