package skill_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/joshka0/foxctl/internal/domain/skill"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestInstaller creates a temporary installation root and returns an installer.
func setupTestInstaller(t *testing.T) (*skill.Installer, string) {
	t.Helper()
	tmpDir := t.TempDir()
	installer := skill.NewInstaller(tmpDir)
	return installer, tmpDir
}

// createTestSkill creates a temporary skill manifest and binary for testing.
func createTestSkill(t *testing.T, distType string) (manifestPath, artifactPath string) {
	t.Helper()
	tmpDir := t.TempDir()

	// Create manifest
	var manifestContent string
	if distType == "exec" {
		manifestContent = `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: test/skill
  version: 1.0.0
  description: "Test skill"
distribution:
  type: exec
  exec:
    entry: skills/test/bin
signature:
  command: test/skill
capabilities:
  network: "none"
  filesystem:
    - type: workdir
`
	} else {
		manifestContent = `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: test/skill
  version: 1.0.0
  description: "Test skill"
distribution:
  type: wasi
  wasi:
    module: skills/test/module.wasm
signature:
  command: test/skill
capabilities:
  network: "none"
  filesystem:
    - type: workdir
`
	}
	manifestPath = filepath.Join(tmpDir, "skill.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))

	// Create artifact (binary or WASM module)
	if distType == "exec" {
		artifactPath = filepath.Join(tmpDir, "bin")
		require.NoError(t, os.WriteFile(artifactPath, []byte("#!/bin/sh\necho test"), 0o755))
	} else {
		artifactPath = filepath.Join(tmpDir, "module.wasm")
		require.NoError(t, os.WriteFile(artifactPath, []byte("wasm binary"), 0o644))
	}

	return manifestPath, artifactPath
}

func TestInstaller_Install_ExecSkill(t *testing.T) {
	installer, installRoot := setupTestInstaller(t)
	manifestPath, binaryPath := createTestSkill(t, "exec")

	handle, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		BinaryPath:   binaryPath,
	})

	require.NoError(t, err)
	assert.Equal(t, "test/skill", handle.Name)

	// Verify files were copied to normalized path (test/skill -> test_skill)
	expectedManifest := filepath.Join(installRoot, "test_skill", "skill.yaml")
	expectedBinary := filepath.Join(installRoot, "test_skill", "bin")

	assert.FileExists(t, expectedManifest)
	assert.FileExists(t, expectedBinary)
	assert.Equal(t, expectedBinary, handle.ArtifactPath)

	// Verify binary is executable
	info, err := os.Stat(expectedBinary)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&0o111, "binary should be executable")
}

func TestInstaller_Install_WASISkill(t *testing.T) {
	installer, installRoot := setupTestInstaller(t)
	manifestPath, modulePath := createTestSkill(t, "wasi")

	handle, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		ModulePath:   modulePath,
	})

	require.NoError(t, err)
	assert.Equal(t, "test/skill", handle.Name)

	// Verify files were copied to normalized path (test/skill -> test_skill)
	expectedManifest := filepath.Join(installRoot, "test_skill", "skill.yaml")
	expectedModule := filepath.Join(installRoot, "test_skill", "module.wasm")

	assert.FileExists(t, expectedManifest)
	assert.FileExists(t, expectedModule)
	assert.Equal(t, expectedModule, handle.ArtifactPath)
}

func TestInstaller_Install_AlreadyInstalled(t *testing.T) {
	installer, _ := setupTestInstaller(t)
	manifestPath, binaryPath := createTestSkill(t, "exec")

	// First installation should succeed
	_, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		BinaryPath:   binaryPath,
	})
	require.NoError(t, err)

	// Second installation should fail
	_, err = installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		BinaryPath:   binaryPath,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already installed")
}

func TestInstaller_Install_ForceReinstall(t *testing.T) {
	installer, installRoot := setupTestInstaller(t)
	manifestPath, binaryPath := createTestSkill(t, "exec")

	// First installation
	_, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		BinaryPath:   binaryPath,
	})
	require.NoError(t, err)

	// Force reinstall should succeed
	_, err = installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		BinaryPath:   binaryPath,
		Force:        true,
	})
	require.NoError(t, err)

	// Verify files still exist at normalized path
	expectedManifest := filepath.Join(installRoot, "test_skill", "skill.yaml")
	assert.FileExists(t, expectedManifest)
}

func TestInstaller_Install_MissingManifest(t *testing.T) {
	installer, _ := setupTestInstaller(t)

	_, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: "/nonexistent/skill.yaml",
		BinaryPath:   "/tmp/bin",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "manifest not found")
}

func TestInstaller_Install_MissingBinary(t *testing.T) {
	installer, _ := setupTestInstaller(t)
	manifestPath, _ := createTestSkill(t, "exec")

	_, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		BinaryPath:   "/nonexistent/bin",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary not found")
}

func TestInstaller_Install_InvalidSkillName(t *testing.T) {
	installer, _ := setupTestInstaller(t)
	tmpDir := t.TempDir()

	// Create manifest with invalid name (path traversal attempt)
	manifestContent := `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: ../../../etc/passwd
  version: 1.0.0
  description: "Evil skill"
distribution:
  type: exec
  exec:
    entry: skills/evil/bin
signature:
  command: evil/skill
capabilities:
  network: "none"
  filesystem:
    - type: workdir
`
	manifestPath := filepath.Join(tmpDir, "skill.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))

	binaryPath := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.WriteFile(binaryPath, []byte("evil"), 0o755))

	_, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		BinaryPath:   binaryPath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid skill name")
}

func TestInstaller_Install_PolicyViolation(t *testing.T) {
	installer, _ := setupTestInstaller(t)
	tmpDir := t.TempDir()

	// Create manifest that violates policy (WASI with network enabled)
	manifestContent := `apiVersion: foxctl/v1
kind: Skill
metadata:
  name: test/bad-skill
  version: 1.0.0
  description: "Policy-violating skill"
distribution:
  type: wasi
  wasi:
    module: skills/bad/module.wasm
signature:
  command: test/bad
capabilities:
  network: "egress"
  filesystem:
    - type: workdir
`
	manifestPath := filepath.Join(tmpDir, "skill.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifestContent), 0o644))

	modulePath := filepath.Join(tmpDir, "module.wasm")
	require.NoError(t, os.WriteFile(modulePath, []byte("wasm"), 0o644))

	_, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		ModulePath:   modulePath,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "policy validation failed")
}

func TestInstaller_Install_RemoteSourceRejected(t *testing.T) {
	installer, _ := setupTestInstaller(t)

	testCases := []string{
		"https://example.com/skill.yaml",
		"http://example.com/skill.yaml",
		"git://github.com/user/repo",
	}

	for _, source := range testCases {
		t.Run(source, func(t *testing.T) {
			_, err := installer.Install(context.Background(), skill.InstallOptions{
				ManifestPath: source,
				BinaryPath:   "/tmp/bin",
			})

			require.Error(t, err)
			assert.Contains(t, err.Error(), "not yet implemented")
		})
	}
}

func TestInstaller_Install_FileSchemeStripped(t *testing.T) {
	installer, installRoot := setupTestInstaller(t)
	manifestPath, binaryPath := createTestSkill(t, "exec")

	// Use file:// scheme
	_, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: "file://" + manifestPath,
		BinaryPath:   binaryPath,
	})

	require.NoError(t, err)

	// Verify installation succeeded at normalized path
	expectedManifest := filepath.Join(installRoot, "test_skill", "skill.yaml")
	assert.FileExists(t, expectedManifest)
}

func TestInstaller_IsInstalled(t *testing.T) {
	installer, _ := setupTestInstaller(t)
	manifestPath, binaryPath := createTestSkill(t, "exec")

	// Before installation
	assert.False(t, installer.IsInstalled("test/skill"))

	// After installation
	_, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		BinaryPath:   binaryPath,
	})
	require.NoError(t, err)

	assert.True(t, installer.IsInstalled("test/skill"))
	assert.False(t, installer.IsInstalled("other/skill"))
}

func TestInstaller_Uninstall(t *testing.T) {
	installer, installRoot := setupTestInstaller(t)
	manifestPath, binaryPath := createTestSkill(t, "exec")

	// Install first
	_, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		BinaryPath:   binaryPath,
	})
	require.NoError(t, err)

	// Verify installation at normalized path
	skillDir := filepath.Join(installRoot, "test_skill")
	assert.DirExists(t, skillDir)

	// Uninstall using canonical name (should work via normalization)
	err = installer.Uninstall("test/skill")
	require.NoError(t, err)

	// Verify removal
	assert.NoDirExists(t, skillDir)
	assert.False(t, installer.IsInstalled("test/skill"))
}

func TestInstaller_Uninstall_NotInstalled(t *testing.T) {
	installer, _ := setupTestInstaller(t)

	err := installer.Uninstall("nonexistent/skill")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestInstaller_InstallPath(t *testing.T) {
	tmpDir := t.TempDir()
	installer := skill.NewInstaller(tmpDir)

	assert.Equal(t, tmpDir, installer.InstallPath())
}

func TestInstaller_Install_CleanupOnArtifactFailure(t *testing.T) {
	installer, installRoot := setupTestInstaller(t)
	manifestPath, _ := createTestSkill(t, "exec")

	// Try to install without providing binary (should fail)
	_, err := installer.Install(context.Background(), skill.InstallOptions{
		ManifestPath: manifestPath,
		// BinaryPath intentionally omitted
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "binary path required")

	// Verify the skill directory was cleaned up (uses normalized path)
	skillDir := filepath.Join(installRoot, "test_skill")
	assert.NoDirExists(t, skillDir)
}
