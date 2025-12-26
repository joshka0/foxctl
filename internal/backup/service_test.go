package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jkatigb/agentctl/internal/domain/backup"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

func TestBackupCreateAndRestore(t *testing.T) {
	// Create a temporary directory for test data
	tmpDir := t.TempDir()

	// Create test directory structure
	storageDir := filepath.Join(tmpDir, "storage")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a test database file
	testDB := filepath.Join(storageDir, "test.db")
	if err := os.WriteFile(testDB, []byte("test database content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create config
	cfg := config.Config{
		Home: tmpDir,
		Storage: config.StorageSettings{
			Root: storageDir,
		},
		Paths: config.Paths{
			CAS:  filepath.Join(tmpDir, "cas"),
			Jobs: filepath.Join(tmpDir, "jobs"),
		},
	}

	// Create service
	svc := NewService(cfg)
	ctx := context.Background()

	// Test Create
	t.Run("Create", func(t *testing.T) {
		result, err := svc.Create(ctx, backup.CreateOptions{
			Components: []backup.Component{backup.ComponentDatabases},
		})
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}

		if result.FilesProcessed != 1 {
			t.Errorf("Expected 1 file processed, got %d", result.FilesProcessed)
		}

		if result.Manifest == nil {
			t.Fatal("Expected manifest to be set")
		}

		if len(result.Manifest.Components) != 1 {
			t.Errorf("Expected 1 component, got %d", len(result.Manifest.Components))
		}

		// Verify archive was created
		if _, err := os.Stat(result.Path); err != nil {
			t.Errorf("Archive not created: %v", err)
		}
	})

	// Test List
	t.Run("List", func(t *testing.T) {
		backups, err := svc.List(ctx)
		if err != nil {
			t.Fatalf("List failed: %v", err)
		}

		if len(backups) != 1 {
			t.Errorf("Expected 1 backup, got %d", len(backups))
		}
	})

	// Test GetManifest
	t.Run("GetManifest", func(t *testing.T) {
		backups, _ := svc.List(ctx)
		if len(backups) == 0 {
			t.Skip("No backups to test")
		}

		manifest, err := svc.GetManifest(ctx, backups[0].Path)
		if err != nil {
			t.Fatalf("GetManifest failed: %v", err)
		}

		if manifest.Version != BackupVersion {
			t.Errorf("Expected version %s, got %s", BackupVersion, manifest.Version)
		}
	})

	// Test Restore (dry-run)
	t.Run("Restore_DryRun", func(t *testing.T) {
		backups, _ := svc.List(ctx)
		if len(backups) == 0 {
			t.Skip("No backups to test")
		}

		result, err := svc.Restore(ctx, backups[0].Path, backup.RestoreOptions{
			DryRun: true,
		})
		if err != nil {
			t.Fatalf("Restore failed: %v", err)
		}

		if result.FilesProcessed != 1 {
			t.Errorf("Expected 1 file processed, got %d", result.FilesProcessed)
		}
	})
}

func TestComponentFiltering(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple component directories
	storageDir := filepath.Join(tmpDir, "storage")
	memoryDir := filepath.Join(tmpDir, "memory")
	_ = os.MkdirAll(storageDir, 0755)
	_ = os.MkdirAll(memoryDir, 0755)

	// Create test files
	_ = os.WriteFile(filepath.Join(storageDir, "test.db"), []byte("db"), 0644)
	_ = os.WriteFile(filepath.Join(memoryDir, "mem.json"), []byte("mem"), 0644)

	cfg := config.Config{
		Home: tmpDir,
		Storage: config.StorageSettings{
			Root: storageDir,
		},
		Paths: config.Paths{
			CAS:  filepath.Join(tmpDir, "cas"),
			Jobs: filepath.Join(tmpDir, "jobs"),
		},
	}

	svc := NewService(cfg)
	ctx := context.Background()

	// Test with only databases
	result, err := svc.Create(ctx, backup.CreateOptions{
		Components: []backup.Component{backup.ComponentDatabases},
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if len(result.Manifest.Components) != 1 {
		t.Errorf("Expected 1 component, got %d", len(result.Manifest.Components))
	}

	if result.Manifest.Components[0] != backup.ComponentDatabases {
		t.Errorf("Expected databases component, got %s", result.Manifest.Components[0])
	}
}

func TestExcludeComponents(t *testing.T) {
	tmpDir := t.TempDir()

	storageDir := filepath.Join(tmpDir, "storage")
	_ = os.MkdirAll(storageDir, 0755)
	_ = os.WriteFile(filepath.Join(storageDir, "test.db"), []byte("db"), 0644)

	cfg := config.Config{
		Home: tmpDir,
		Storage: config.StorageSettings{
			Root: storageDir,
		},
		Paths: config.Paths{
			CAS:  filepath.Join(tmpDir, "cas"),
			Jobs: filepath.Join(tmpDir, "jobs"),
		},
	}

	svc := NewService(cfg)
	ctx := context.Background()

	// Exclude databases, leaving nothing (since other dirs don't exist)
	result, err := svc.Create(ctx, backup.CreateOptions{
		ExcludeComponents: []backup.Component{
			backup.ComponentDatabases,
			backup.ComponentCAS,
			backup.ComponentMemory,
			backup.ComponentSessions,
			backup.ComponentJobs,
		},
	})

	// Should fail because no files to backup
	if err == nil {
		t.Errorf("Expected error when excluding all components, got result: %+v", result)
	}
}
