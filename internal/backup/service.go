// Package backup implements backup and restore operations for agentctl data.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/backup"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

const (
	// BackupVersion is the current backup format version.
	BackupVersion = "1.0"
	// ManifestFile is the name of the manifest file in the archive.
	ManifestFile = "manifest.json"
	// DefaultBackupDir is the default directory for backups.
	DefaultBackupDir = "backups"
)

// Service provides backup and restore operations.
type Service struct {
	cfg config.Config
}

// NewService creates a new backup service.
func NewService(cfg config.Config) *Service {
	return &Service{cfg: cfg}
}

// Create creates a new backup archive.
//
// Index:
// - Purpose: Build a backup archive and manifest for selected components
// - Flow: resolve components/exclusions → determine output path → collect files → write tar.gz + manifest → compute stats
// - SideEffects: reads files; creates directories; writes backup archive
// - FailureModes: no files found, file IO errors, manifest marshal errors
// - Related: Service.collectFiles, Service.addFileToArchive
// - Keywords: backup_create, components, exclude_components, output_path, manifest, files_processed, bytes_processed
func (s *Service) Create(ctx context.Context, opts backup.CreateOptions) (*backup.Result, error) {
	startTime := time.Now()
	result := &backup.Result{}

	// Determine which components to backup
	components := opts.Components
	if len(components) == 0 {
		components = backup.AllComponents()
	}

	// Remove excluded components
	if len(opts.ExcludeComponents) > 0 {
		excludeSet := make(map[backup.Component]bool)
		for _, c := range opts.ExcludeComponents {
			excludeSet[c] = true
		}
		var filtered []backup.Component
		for _, c := range components {
			if !excludeSet[c] {
				filtered = append(filtered, c)
			}
		}
		components = filtered
	}

	// Determine output path
	outputPath := opts.OutputPath
	if outputPath == "" {
		backupDir := filepath.Join(s.cfg.Home, DefaultBackupDir)
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return nil, fmt.Errorf("create backup directory: %w", err)
		}

		name := opts.Name
		if name == "" {
			name = time.Now().Format("2006-01-02_150405")
		}
		outputPath = filepath.Join(backupDir, fmt.Sprintf("backup_%s.tar.gz", name))
	}

	// Collect files to backup
	var files []backup.FileEntry
	for _, component := range components {
		componentFiles, err := s.collectFiles(component)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("warning: collecting %s: %v", component, err))
			continue
		}
		files = append(files, componentFiles...)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files to backup")
	}

	// Create the archive
	f, err := os.Create(outputPath)
	if err != nil {
		return nil, fmt.Errorf("create archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gw := gzip.NewWriter(f)
	defer func() { _ = gw.Close() }()

	tw := tar.NewWriter(gw)
	defer func() { _ = tw.Close() }()

	// Calculate total size and add files
	var totalSize int64
	var dbCount int
	for i := range files {
		file := &files[i]

		// Read file and calculate checksum
		checksum, err := s.addFileToArchive(tw, file)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("warning: adding %s: %v", file.OriginalPath, err))
			continue
		}
		file.Checksum = checksum
		totalSize += file.Size
		result.FilesProcessed++

		if strings.HasSuffix(file.Path, ".db") {
			dbCount++
		}
	}

	result.BytesProcessed = totalSize

	// Create manifest
	manifest := &backup.Manifest{
		Version:         BackupVersion,
		CreatedAt:       time.Now().UTC(),
		AgentctlVersion: buildinfo.Current().Version,
		Components:      components,
		Files:           files,
		Stats: backup.BackupStats{
			TotalFiles:    len(files),
			TotalSize:     totalSize,
			DatabaseCount: dbCount,
		},
	}

	// Add manifest to archive
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal manifest: %w", err)
	}

	manifestHeader := &tar.Header{
		Name:    ManifestFile,
		Size:    int64(len(manifestData)),
		Mode:    0o644,
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(manifestHeader); err != nil {
		return nil, fmt.Errorf("write manifest header: %w", err)
	}
	if _, err := tw.Write(manifestData); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	// Close writers to finalize archive
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar writer: %w", err)
	}
	if err := gw.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close file: %w", err)
	}

	// Get final archive size
	stat, err := os.Stat(outputPath)
	if err != nil {
		return nil, fmt.Errorf("stat archive: %w", err)
	}
	manifest.Stats.CompressedSize = stat.Size()

	result.Path = outputPath
	result.Manifest = manifest
	result.Duration = time.Since(startTime)

	return result, nil
}

// List returns information about available backups.
func (s *Service) List(ctx context.Context) ([]backup.Info, error) {
	backupDir := filepath.Join(s.cfg.Home, DefaultBackupDir)

	entries, err := os.ReadDir(backupDir)
	if os.IsNotExist(err) {
		return nil, nil // No backups yet
	}
	if err != nil {
		return nil, fmt.Errorf("read backup directory: %w", err)
	}

	var backups []backup.Info
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".tar.gz") {
			continue
		}

		path := filepath.Join(backupDir, entry.Name())
		info, err := s.getBackupInfo(path)
		if err != nil {
			continue // Skip invalid backups
		}
		backups = append(backups, *info)
	}

	// Sort by creation time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].CreatedAt.After(backups[j].CreatedAt)
	})

	return backups, nil
}

// GetManifest reads the manifest from a backup archive.
func (s *Service) GetManifest(ctx context.Context, path string) (*backup.Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}

		if header.Name == ManifestFile {
			data, err := io.ReadAll(tr)
			if err != nil {
				return nil, fmt.Errorf("read manifest: %w", err)
			}

			var manifest backup.Manifest
			if err := json.Unmarshal(data, &manifest); err != nil {
				return nil, fmt.Errorf("unmarshal manifest: %w", err)
			}
			return &manifest, nil
		}
	}

	return nil, fmt.Errorf("manifest not found in archive")
}

// Restore restores data from a backup archive.
//
// Index:
// - Purpose: Restore selected components from a backup archive to disk
// - Flow: read manifest → choose components → iterate archive entries → write files (unless dry_run) → restore metadata
// - SideEffects: reads archive; creates directories; writes files; chmod/chtimes
// - FailureModes: manifest read errors, archive read errors, file IO errors, permission errors
// - Related: Service.GetManifest
// - Keywords: backup_restore, components, dry_run, force, manifest, files_processed, bytes_processed
func (s *Service) Restore(ctx context.Context, path string, opts backup.RestoreOptions) (*backup.Result, error) {
	startTime := time.Now()
	result := &backup.Result{Path: path}

	// Read manifest first
	manifest, err := s.GetManifest(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}

	// Determine which components to restore
	components := opts.Components
	if len(components) == 0 {
		components = manifest.Components
	}

	// Build set of components to restore
	componentSet := make(map[backup.Component]bool)
	for _, c := range components {
		componentSet[c] = true
	}

	// Open archive
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer func() { _ = f.Close() }()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("create gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)

	// Build file to component mapping
	fileToComponent := make(map[string]backup.Component)
	for _, file := range manifest.Files {
		fileToComponent[file.Path] = file.Component
	}

	// Extract files
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar header: %w", err)
		}

		// Skip manifest
		if header.Name == ManifestFile {
			continue
		}

		// Check if this component should be restored
		component, ok := fileToComponent[header.Name]
		if !ok || !componentSet[component] {
			continue
		}

		// Find original path
		var originalPath string
		for _, file := range manifest.Files {
			if file.Path == header.Name {
				originalPath = file.OriginalPath
				break
			}
		}

		if originalPath == "" {
			// Reconstruct path from archive structure
			originalPath = filepath.Join(s.cfg.Home, header.Name)
		}

		if opts.DryRun {
			result.FilesProcessed++
			result.BytesProcessed += header.Size
			continue
		}

		// Check if file exists
		if _, err := os.Stat(originalPath); err == nil && !opts.Force {
			result.Warnings = append(result.Warnings, fmt.Sprintf("skipped (exists): %s", originalPath))
			continue
		}

		// Create directory if needed
		dir := filepath.Dir(originalPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("warning: create dir for %s: %v", header.Name, err))
			continue
		}

		// Extract file
		outFile, err := os.Create(originalPath)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("warning: create %s: %v", originalPath, err))
			continue
		}

		if _, err := io.Copy(outFile, tr); err != nil {
			_ = outFile.Close()
			result.Warnings = append(result.Warnings, fmt.Sprintf("warning: write %s: %v", originalPath, err))
			continue
		}
		_ = outFile.Close()

		// Restore file mode and times
		if err := os.Chmod(originalPath, os.FileMode(header.Mode)); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("warning: chmod %s: %v", originalPath, err))
		}
		if err := os.Chtimes(originalPath, header.AccessTime, header.ModTime); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("warning: chtimes %s: %v", originalPath, err))
		}

		result.FilesProcessed++
		result.BytesProcessed += header.Size
	}

	result.Duration = time.Since(startTime)
	return result, nil
}

// collectFiles gathers all files for a given component.
func (s *Service) collectFiles(component backup.Component) ([]backup.FileEntry, error) {
	var basePath string
	var pattern string

	switch component {
	case backup.ComponentDatabases:
		basePath = s.cfg.Storage.Root
		pattern = "*.db"
	case backup.ComponentCAS:
		basePath = s.cfg.Paths.CAS
		pattern = "*"
	case backup.ComponentMemory:
		basePath = filepath.Join(s.cfg.Home, "memory")
		pattern = "*"
	case backup.ComponentSessions:
		basePath = filepath.Join(s.cfg.Home, "sessions")
		pattern = "*"
	case backup.ComponentJobs:
		basePath = s.cfg.Paths.Jobs
		pattern = "*"
	case backup.ComponentObservability:
		basePath = s.cfg.Paths.Observability
		pattern = "*.ndjson"
	default:
		return nil, fmt.Errorf("unknown component: %s", component)
	}

	if _, err := os.Stat(basePath); os.IsNotExist(err) {
		return nil, nil // Component directory doesn't exist
	}

	var files []backup.FileEntry

	err := filepath.Walk(basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		// Skip WAL and SHM files for databases - they should be flushed
		if component == backup.ComponentDatabases {
			if strings.HasSuffix(path, "-wal") || strings.HasSuffix(path, "-shm") {
				return nil
			}
		}

		// For jobs, limit to recent jobs to avoid huge backups
		if component == backup.ComponentJobs {
			// Only include files modified in last 7 days
			if time.Since(info.ModTime()) > 7*24*time.Hour {
				return nil
			}
		}

		// For observability, limit to recent event files to avoid huge backups
		if component == backup.ComponentObservability {
			// Only include files modified in last 7 days
			if time.Since(info.ModTime()) > 7*24*time.Hour {
				return nil
			}
		}

		relPath, err := filepath.Rel(s.cfg.Home, path)
		if err != nil {
			relPath = path
		}

		files = append(files, backup.FileEntry{
			Path:         relPath,
			OriginalPath: path,
			Size:         info.Size(),
			ModTime:      info.ModTime(),
			Component:    component,
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", basePath, err)
	}

	// For databases, handle pattern matching
	if component == backup.ComponentDatabases && pattern == "*.db" {
		var filtered []backup.FileEntry
		for _, f := range files {
			if strings.HasSuffix(f.Path, ".db") {
				filtered = append(filtered, f)
			}
		}
		files = filtered
	}

	return files, nil
}

// addFileToArchive adds a file to the tar archive and returns its checksum.
func (s *Service) addFileToArchive(tw *tar.Writer, file *backup.FileEntry) (string, error) {
	f, err := os.Open(file.OriginalPath)
	if err != nil {
		return "", fmt.Errorf("open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("stat file: %w", err)
	}

	header := &tar.Header{
		Name:    file.Path,
		Size:    info.Size(),
		Mode:    int64(info.Mode()),
		ModTime: info.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return "", fmt.Errorf("write header: %w", err)
	}

	// Write file content and calculate checksum
	h := sha256.New()
	mw := io.MultiWriter(tw, h)

	if _, err := io.Copy(mw, f); err != nil {
		return "", fmt.Errorf("copy file: %w", err)
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// getBackupInfo extracts basic info from a backup archive.
func (s *Service) getBackupInfo(path string) (*backup.Info, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	manifest, err := s.GetManifest(context.Background(), path)
	if err != nil {
		return nil, err
	}

	return &backup.Info{
		Name:       filepath.Base(path),
		Path:       path,
		CreatedAt:  manifest.CreatedAt,
		Size:       stat.Size(),
		Components: manifest.Components,
	}, nil
}
