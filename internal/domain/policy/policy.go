package policy

import (
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/joshka0/foxctl/internal/domain/skill"
)

// Validator provides policy validation for skill execution.
type Validator struct {
	manifest skill.Manifest
}

// NewValidator creates a new policy validator for the given manifest.
func NewValidator(m skill.Manifest) *Validator {
	return &Validator{manifest: m}
}

// ValidateNetworkAccess checks if network access to the given host:port is allowed.
// Returns an error if access is denied by the egressAllow policy.
func (v *Validator) ValidateNetworkAccess(host string, port int) error {
	if !validNetworkPort(port) {
		return fmt.Errorf("network access denied: invalid port %d", port)
	}
	rawHost := host
	var ok bool
	host, ok = canonicalEgressHost(host)
	if !ok {
		return fmt.Errorf("network access denied: invalid host %q", rawHost)
	}

	// Network access is only granted by an explicit egress capability.
	if v.manifest.Capabilities.Network != "egress" {
		return fmt.Errorf("network access denied: skill has network capability %q", v.manifest.Capabilities.Network)
	}

	// If network capability is "egress" with no egressAllow list, allow all
	if len(v.manifest.Capabilities.EgressAllow) == 0 {
		return nil
	}

	// Check against egressAllow list
	target := fmt.Sprintf("%s:%d", host, port)
	for _, pattern := range v.manifest.Capabilities.EgressAllow {
		if matchesEgressPattern(host, port, pattern) {
			return nil
		}
	}

	return fmt.Errorf("network access denied: %s not in egressAllow list", target)
}

// ValidateWASIPolicy ensures WASI skills have network set to "none".
// This is a convenience wrapper around skill.ValidateWASIPolicy.
func ValidateWASIPolicy(m skill.Manifest) error {
	return skill.ValidateWASIPolicy(m)
}

// matchesEgressPattern checks if a host:port matches an egress pattern.
// Patterns can be:
// - "api.github.com:443" - exact match
// - "*.amazonaws.com:443" - wildcard domain match
// - "10.0.0.0/8:*" - CIDR with any port
// - "example.com:*" - any port on domain
func matchesEgressPattern(host string, port int, pattern string) bool {
	if !validNetworkPort(port) {
		return false
	}

	patternHost, patternPort, ok := splitEgressPattern(pattern)
	if !ok {
		return false
	}
	host, hostOK := canonicalEgressHost(host)
	if !hostOK {
		return false
	}
	patternHost, patternOK := canonicalEgressHost(patternHost)
	if !patternOK {
		return false
	}

	// Check port match
	if patternPort != "*" {
		patternPortNum, err := strconv.Atoi(patternPort)
		if err != nil {
			return false
		}
		if port != patternPortNum {
			return false
		}
	}

	// Check host match
	// 1. Try CIDR match
	if strings.Contains(patternHost, "/") {
		if matchesCIDR(host, patternHost) {
			return true
		}
	}

	// 2. Try wildcard domain match
	if strings.HasPrefix(patternHost, "*.") {
		suffix := patternHost[1:] // Remove leading *
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}

	// 3. Try exact match
	if host == patternHost {
		return true
	}

	return false
}

func validNetworkPort(port int) bool {
	return port >= 1 && port <= 65535
}

func splitEgressPattern(pattern string) (string, string, bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", "", false
	}
	idx := strings.LastIndex(pattern, ":")
	if idx <= 0 || idx == len(pattern)-1 {
		return "", "", false
	}
	host := strings.TrimSpace(pattern[:idx])
	port := strings.TrimSpace(pattern[idx+1:])
	if host == "" || port == "" {
		return "", "", false
	}
	return host, port, true
}

func canonicalEgressHost(raw string) (string, bool) {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" || raw != strings.TrimSpace(raw) || strings.IndexFunc(host, unicode.IsSpace) != -1 {
		return "", false
	}
	if strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]") {
		if !strings.HasPrefix(host, "[") || !strings.HasSuffix(host, "]") {
			return "", false
		}
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
		if net.ParseIP(host) == nil {
			return "", false
		}
	}
	if strings.ContainsAny(host, "[]") {
		return "", false
	}
	return host, true
}

// matchesCIDR checks if a host (IP or hostname) matches a CIDR pattern.
func matchesCIDR(host, cidr string) bool {
	// Parse CIDR
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}

	// Try parsing host as IP
	ip := net.ParseIP(host)
	if ip == nil {
		// If host is not an IP, try resolving it
		// For security, we only check if it could be resolved
		// Actual DNS resolution should happen at runtime with logging
		return false
	}

	return ipnet.Contains(ip)
}

// ValidateFilesystemAccess checks if filesystem access to the given path is allowed.
// Note: For exec skills, enforcement is best-effort. The skill runs with the user's
// permissions and can technically access any path. This validation serves as a
// manifest compliance check.
// For WASI skills, wazero provides true filesystem sandboxing.
func (v *Validator) ValidateFilesystemAccess(requestedPath, workDir string) error {
	// Check if filesystem access is declared
	if len(v.manifest.Capabilities.Filesystem) == 0 {
		return fmt.Errorf("filesystem access denied: no filesystem capabilities declared")
	}

	// Check each filesystem capability
	for _, fs := range v.manifest.Capabilities.Filesystem {
		switch fs.Type {
		case "workdir":
			// Skill should only access paths within workdir
			if isWithinWorkdir(requestedPath, workDir) {
				return nil
			}
		case "home":
			// Future: allow access to user's home directory
			// Not implemented yet
		case "tmp":
			// Future: allow access to system temp directory
			// Not implemented yet
		}
	}

	return fmt.Errorf("filesystem access denied: %q not allowed by manifest", requestedPath)
}

// ValidateFilesystemCapabilities checks that filesystem capabilities are valid.
// This is a convenience wrapper around skill.ValidateFilesystemCapabilities.
func ValidateFilesystemCapabilities(m skill.Manifest) error {
	return skill.ValidateFilesystemCapabilities(m)
}

// isWithinWorkdir checks if a path is within the workdir.
// This is a best-effort check for manifest compliance.
func isWithinWorkdir(path, workDir string) bool {
	// Clean the workdir
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return false
	}

	// For relative paths, treat them as relative to workDir
	var absPath string
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(absWorkDir, path)
	} else {
		absPath = path
	}

	// Clean the path
	absPath = filepath.Clean(absPath)
	absWorkDir = filepath.Clean(absWorkDir)

	// Check if path is within workdir
	rel, err := filepath.Rel(absWorkDir, absPath)
	if err != nil {
		return false
	}

	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
