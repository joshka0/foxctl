package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// Install installs a skill from a source.
// Future implementations will support:
// - Local path (copy to install dir)
// - Git repository
// - URL (download archive)
// - Registry (future)
func (i *Installer) Install(ctx context.Context, source string) (Handle, error) {
	// TODO: Implement skill installation
	// For now, this is a placeholder for future functionality
	return Handle{}, fmt.Errorf("skill installation not yet implemented")
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
