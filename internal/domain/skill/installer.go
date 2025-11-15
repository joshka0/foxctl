package skill

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Installer manages skill installation and removal.
type Installer struct {
	installPath string
}

// NewInstaller creates a new skill installer.
// installPath is the directory where skills will be installed.
func NewInstaller(installPath string) *Installer {
	return &Installer{installPath: installPath}
}

// InstallOptions configures the installation process.
type InstallOptions struct {
	// ManifestPath is the path to the skill.yaml file.
	ManifestPath string

	// BinaryPath is the path to the executable binary (for exec distribution type).
	BinaryPath string

	// ModulePath is the path to the WASM module (for wasi distribution type).
	ModulePath string

	// Force allows reinstalling an already installed skill.
	Force bool
}

// Install installs a skill from local source files.
// Future implementations will support:
// - Git repository (git:// or https://)
// - URL (download archive)
// - Registry (future)
func (i *Installer) Install(ctx context.Context, opts InstallOptions) (Handle, error) {
	// Reject non-local sources for now
	if strings.HasPrefix(opts.ManifestPath, "http://") ||
		strings.HasPrefix(opts.ManifestPath, "https://") ||
		strings.HasPrefix(opts.ManifestPath, "git://") {
		return Handle{}, fmt.Errorf("remote skill installation from %q not yet implemented", opts.ManifestPath)
	}

	// Strip file:// scheme if present
	manifestPath := strings.TrimPrefix(opts.ManifestPath, "file://")

	// Resolve to absolute path
	absManifestPath, err := filepath.Abs(manifestPath)
	if err != nil {
		return Handle{}, fmt.Errorf("resolve manifest path: %w", err)
	}

	// Verify manifest exists
	if _, err := os.Stat(absManifestPath); err != nil {
		return Handle{}, fmt.Errorf("manifest not found at %q: %w", absManifestPath, err)
	}

	// Load and validate manifest
	manifest, err := i.loadAndValidateManifest(absManifestPath)
	if err != nil {
		return Handle{}, err
	}

	// Check if already installed
	if !opts.Force && i.IsInstalled(manifest.Metadata.Name) {
		return Handle{}, fmt.Errorf("skill %q already installed (use --force to reinstall)", manifest.Metadata.Name)
	}

	// Create destination directory
	destDir, err := i.ensureSkillDir(manifest)
	if err != nil {
		return Handle{}, err
	}

	// Copy manifest
	if err := i.writeManifest(destDir, absManifestPath); err != nil {
		return Handle{}, fmt.Errorf("write manifest: %w", err)
	}

	// Copy distribution artifacts
	if err := i.writeDistributionArtifacts(destDir, manifest, opts); err != nil {
		// Clean up on failure
		cleanupErr := os.RemoveAll(destDir)
		if cleanupErr != nil {
			return Handle{}, fmt.Errorf("write distribution artifacts: %w (cleanup failed: %v)", err, cleanupErr)
		}
		return Handle{}, fmt.Errorf("write distribution artifacts: %w", err)
	}

	// Resolve artifact path
	artifactPath, err := i.resolveArtifactPath(destDir, manifest)
	if err != nil {
		return Handle{}, err
	}

	return Handle{
		Name:         manifest.Metadata.Name,
		ManifestPath: filepath.Join(destDir, "skill.yaml"),
		ArtifactPath: artifactPath,
		Source:       "installed",
	}, nil
}

// loadAndValidateManifest loads a manifest and validates it against policies.
func (i *Installer) loadAndValidateManifest(path string) (Manifest, error) {
	manifest, err := LoadManifest(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("load manifest: %w", err)
	}

	if err := ValidateWASIPolicy(manifest); err != nil {
		return Manifest{}, fmt.Errorf("policy validation failed: %w", err)
	}

	if err := ValidateFilesystemCapabilities(manifest); err != nil {
		return Manifest{}, fmt.Errorf("filesystem capabilities validation failed: %w", err)
	}

	return manifest, nil
}

// ensureSkillDir creates and validates the skill installation directory.
func (i *Installer) ensureSkillDir(manifest Manifest) (string, error) {
	name := strings.TrimSpace(manifest.Metadata.Name)
	if name == "" {
		return "", fmt.Errorf("skill metadata name is required")
	}

	// Clean and validate name
	cleanName := filepath.Clean(name)
	if cleanName == "." || cleanName == ".." || filepath.IsAbs(cleanName) {
		return "", fmt.Errorf("invalid skill name %q", manifest.Metadata.Name)
	}

	// Prevent path traversal
	for _, segment := range strings.Split(cleanName, string(os.PathSeparator)) {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("invalid skill name %q", manifest.Metadata.Name)
		}
	}

	// Construct destination path
	root := filepath.Clean(i.installPath)
	dest := filepath.Join(root, cleanName)
	dest = filepath.Clean(dest)

	// Final safety check: ensure dest is within root
	if rel, err := filepath.Rel(root, dest); err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("invalid skill name %q (path traversal detected)", manifest.Metadata.Name)
	}

	// Create directory
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", fmt.Errorf("create skill directory: %w", err)
	}

	return dest, nil
}

// writeManifest copies the manifest file to the destination directory.
func (i *Installer) writeManifest(destDir, manifestPath string) error {
	return copyFile(manifestPath, filepath.Join(destDir, "skill.yaml"))
}

// writeDistributionArtifacts copies the binary or WASM module to the destination.
func (i *Installer) writeDistributionArtifacts(destDir string, manifest Manifest, opts InstallOptions) error {
	switch manifest.Distribution.Type {
	case "exec":
		if opts.BinaryPath == "" {
			return fmt.Errorf("binary path required for exec distribution type")
		}
		absBinary, err := filepath.Abs(opts.BinaryPath)
		if err != nil {
			return fmt.Errorf("resolve binary path: %w", err)
		}
		if _, err := os.Stat(absBinary); err != nil {
			return fmt.Errorf("binary not found at %q: %w", absBinary, err)
		}
		return copyFile(absBinary, filepath.Join(destDir, "bin"))

	case "wasi":
		if opts.ModulePath == "" {
			return fmt.Errorf("module path required for wasi distribution type")
		}
		absModule, err := filepath.Abs(opts.ModulePath)
		if err != nil {
			return fmt.Errorf("resolve module path: %w", err)
		}
		if _, err := os.Stat(absModule); err != nil {
			return fmt.Errorf("module not found at %q: %w", absModule, err)
		}
		return copyFile(absModule, filepath.Join(destDir, "module.wasm"))

	default:
		return fmt.Errorf("unsupported distribution type: %s", manifest.Distribution.Type)
	}
}

// resolveArtifactPath determines the artifact path based on distribution type.
func (i *Installer) resolveArtifactPath(destDir string, manifest Manifest) (string, error) {
	switch manifest.Distribution.Type {
	case "exec":
		return filepath.Join(destDir, "bin"), nil
	case "wasi":
		return filepath.Join(destDir, "module.wasm"), nil
	default:
		return "", fmt.Errorf("unsupported distribution type: %s", manifest.Distribution.Type)
	}
}

// copyFile copies a file from src to dst, preserving permissions.
func copyFile(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() {
		if cerr := in.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("close source: %w", cerr)
		}
	}()

	info, err := in.Stat()
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer func() {
		if cerr := out.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("close destination: %w", cerr)
		}
	}()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}

	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync destination: %w", err)
	}

	if err := os.Chmod(dst, info.Mode()); err != nil {
		return fmt.Errorf("set permissions: %w", err)
	}

	return nil
}

// Uninstall removes an installed skill by name.
func (i *Installer) Uninstall(name string) error {
	skillPath := filepath.Join(i.installPath, name)

	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		return fmt.Errorf("skill not installed: %s", name)
	}

	return os.RemoveAll(skillPath)
}

// IsInstalled checks if a skill is installed.
func (i *Installer) IsInstalled(name string) bool {
	manifestPath := filepath.Join(i.installPath, name, "skill.yaml")
	_, err := os.Stat(manifestPath)
	return err == nil
}

// InstallPath returns the configured installation path.
func (i *Installer) InstallPath() string {
	return i.installPath
}
