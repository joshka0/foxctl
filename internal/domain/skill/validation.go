package skill

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ValidateWASIPolicy ensures WASI skills comply with network restrictions.
// Per Core Profile v1, WASI skills must have network capability set to "none".
func ValidateWASIPolicy(m Manifest) error {
	if m.Distribution.Type == "wasi" && m.Capabilities.Network != "none" {
		return fmt.Errorf("WASI skills must have capabilities.network set to 'none', got %q", m.Capabilities.Network)
	}
	return nil
}

// ValidateFilesystemCapabilities checks that filesystem capabilities are valid.
func ValidateFilesystemCapabilities(m Manifest) error {
	for i, fs := range m.Capabilities.Filesystem {
		switch strings.TrimSpace(fs.Type) {
		case "workdir":
			// Valid
		case "home", "tmp":
			// Future capabilities - warn but don't error
			// Could add warning logging here
		default:
			return fmt.Errorf("unknown filesystem capability type: %q", fs.Type)
		}
		if err := validateFilesystemCapabilityPath(i, fs.Path); err != nil {
			return err
		}
	}
	return nil
}

func validateFilesystemCapabilityPath(index int, rawPath string) error {
	path := strings.TrimSpace(rawPath)
	if path == "" {
		return nil
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("filesystem[%d].path must be relative to the capability root: %q", index, rawPath)
	}
	if hasParentPathSegment(path) {
		return fmt.Errorf("filesystem[%d].path must not contain parent traversal: %q", index, rawPath)
	}
	return nil
}

func hasParentPathSegment(path string) bool {
	for _, segment := range strings.FieldsFunc(path, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if segment == ".." {
			return true
		}
	}
	return false
}
