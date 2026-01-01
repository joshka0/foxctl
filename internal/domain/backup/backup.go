// Package backup defines types and interfaces for backup operations.
package backup

import (
	"time"
)

// Component represents a backupable component of agentctl.
type Component string

const (
	// ComponentDatabases includes all SQLite databases in storage/.
	ComponentDatabases Component = "databases"
	// ComponentCAS includes the content-addressable storage.
	ComponentCAS Component = "cas"
	// ComponentMemory includes the memory store files.
	ComponentMemory Component = "memory"
	// ComponentSessions includes session-related files.
	ComponentSessions Component = "sessions"
	// ComponentJobs includes job artifacts.
	ComponentJobs Component = "jobs"
	// ComponentObservability includes wide event logs.
	ComponentObservability Component = "observability"
)

// AllComponents returns all available backup components.
func AllComponents() []Component {
	return []Component{
		ComponentDatabases,
		ComponentCAS,
		ComponentMemory,
		ComponentSessions,
		ComponentJobs,
		ComponentObservability,
	}
}

// Manifest describes the contents and metadata of a backup archive.
type Manifest struct {
	// Version is the backup format version.
	Version string `json:"version"`
	// CreatedAt is when the backup was created.
	CreatedAt time.Time `json:"created_at"`
	// AgentctlVersion is the agentctl version that created the backup.
	AgentctlVersion string `json:"agentctl_version"`
	// Components lists which components are included.
	Components []Component `json:"components"`
	// Files lists all files included in the backup with their metadata.
	Files []FileEntry `json:"files"`
	// Stats contains summary statistics.
	Stats BackupStats `json:"stats"`
}

// FileEntry describes a single file in the backup.
type FileEntry struct {
	// Path is the relative path within the backup archive.
	Path string `json:"path"`
	// OriginalPath is the original absolute path on the source system.
	OriginalPath string `json:"original_path"`
	// Size is the file size in bytes.
	Size int64 `json:"size"`
	// ModTime is the file modification time.
	ModTime time.Time `json:"mod_time"`
	// Checksum is the SHA256 checksum of the file contents.
	Checksum string `json:"checksum"`
	// Component indicates which backup component this file belongs to.
	Component Component `json:"component"`
}

// BackupStats contains summary statistics about a backup.
type BackupStats struct {
	// TotalFiles is the total number of files in the backup.
	TotalFiles int `json:"total_files"`
	// TotalSize is the total uncompressed size in bytes.
	TotalSize int64 `json:"total_size"`
	// CompressedSize is the final archive size in bytes.
	CompressedSize int64 `json:"compressed_size"`
	// DatabaseCount is the number of SQLite databases.
	DatabaseCount int `json:"database_count"`
}

// Info describes a backup archive without its full manifest.
type Info struct {
	// Name is the backup filename.
	Name string `json:"name"`
	// Path is the full path to the backup archive.
	Path string `json:"path"`
	// CreatedAt is when the backup was created.
	CreatedAt time.Time `json:"created_at"`
	// Size is the archive size in bytes.
	Size int64 `json:"size"`
	// Components lists which components are included.
	Components []Component `json:"components"`
}

// CreateOptions configures backup creation.
type CreateOptions struct {
	// OutputPath overrides the default backup location.
	OutputPath string
	// Components specifies which components to include (default: all).
	Components []Component
	// ExcludeComponents specifies which components to exclude.
	ExcludeComponents []Component
	// Name is an optional custom name for the backup.
	Name string
}

// RestoreOptions configures backup restoration.
type RestoreOptions struct {
	// Components specifies which components to restore (default: all in backup).
	Components []Component
	// Force overwrites existing files without prompting.
	Force bool
	// DryRun simulates restoration without making changes.
	DryRun bool
}

// Result describes the outcome of a backup or restore operation.
type Result struct {
	// Path is the backup archive path.
	Path string `json:"path"`
	// Manifest is the backup manifest (for create operations).
	Manifest *Manifest `json:"manifest,omitempty"`
	// FilesProcessed is the number of files processed.
	FilesProcessed int `json:"files_processed"`
	// BytesProcessed is the total bytes processed.
	BytesProcessed int64 `json:"bytes_processed"`
	// Duration is how long the operation took.
	Duration time.Duration `json:"duration,format:units"`
	// Warnings contains any non-fatal issues encountered.
	Warnings []string `json:"warnings,omitempty"`
}
