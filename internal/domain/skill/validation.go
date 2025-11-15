package skill

import "fmt"

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
	for _, fs := range m.Capabilities.Filesystem {
		switch fs.Type {
		case "workdir":
			// Valid
		case "home", "tmp":
			// Future capabilities - warn but don't error
			// Could add warning logging here
		default:
			return fmt.Errorf("unknown filesystem capability type: %q", fs.Type)
		}
	}
	return nil
}
