// Package policy provides centralized policy enforcement for skills.
package policy

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/skill"
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
	// If network capability is "none", deny all network access
	if v.manifest.Capabilities.Network == "none" || v.manifest.Capabilities.Network == "" {
		return fmt.Errorf("network access denied: skill has network capability set to 'none'")
	}

	// If network capability is "egress" with no egressAllow list, allow all
	if v.manifest.Capabilities.Network == "egress" && len(v.manifest.Capabilities.EgressAllow) == 0 {
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
func ValidateWASIPolicy(m skill.Manifest) error {
	if m.Distribution.Type == "wasi" && m.Capabilities.Network != "none" {
		return fmt.Errorf("WASI skills must have capabilities.network set to 'none', got %q", m.Capabilities.Network)
	}
	return nil
}

// matchesEgressPattern checks if a host:port matches an egress pattern.
// Patterns can be:
// - "api.github.com:443" - exact match
// - "*.amazonaws.com:443" - wildcard domain match
// - "10.0.0.0/8:*" - CIDR with any port
// - "example.com:*" - any port on domain
func matchesEgressPattern(host string, port int, pattern string) bool {
	// Split pattern into host and port parts
	parts := strings.Split(pattern, ":")
	if len(parts) != 2 {
		return false
	}

	patternHost := parts[0]
	patternPort := parts[1]

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

// ValidateFilesystemCapabilities checks that the manifest's filesystem capabilities are valid.
func ValidateFilesystemCapabilities(m skill.Manifest) error {
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

	// If path is within workdir, it shouldn't start with ".."
	return !strings.HasPrefix(rel, "..")
}

// PathValidator enforces that skill paths stay within the workspace (or approved roots).
type PathValidator struct {
	workspaceRoot string
	allowedRoots  []string
}

// NewPathValidator builds a validator rooted at workspace with additional allowed roots.
func NewPathValidator(workspace string, allowedRoots []string) (*PathValidator, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, fmt.Errorf("workspace cannot be empty")
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}
	absWorkspace = filepath.Clean(absWorkspace)
	if resolved, err := filepath.EvalSymlinks(absWorkspace); err == nil {
		absWorkspace = filepath.Clean(resolved)
	}

	cleanRoots := make([]string, 0, len(allowedRoots))
	seen := map[string]struct{}{absWorkspace: {}}
	for _, root := range allowedRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		abs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			abs = filepath.Clean(resolved)
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		cleanRoots = append(cleanRoots, abs)
	}

	return &PathValidator{
		workspaceRoot: absWorkspace,
		allowedRoots:  cleanRoots,
	}, nil
}

// ValidatePath ensures the requested path stays within workspace or allowed roots.
func (pv *PathValidator) ValidatePath(requested string) (string, error) {
	if pv == nil {
		return "", fmt.Errorf("path validator not configured")
	}
	if strings.TrimSpace(requested) == "" {
		return "", fmt.Errorf("path is required")
	}

	clean := filepath.Clean(requested)
	abs := clean
	if !filepath.IsAbs(clean) {
		abs = filepath.Join(pv.workspaceRoot, clean)
	}
	abs = filepath.Clean(abs)

	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	} else {
		// If the requested path itself is a symlink that fails to resolve (e.g. dangling),
		// treat it as an error to prevent sandbox escapes via late-bound links.
		if info, lerr := os.Lstat(abs); lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path %q resolves through an invalid symlink: %w", requested, err)
		}
		if dirResolved, derr := filepath.EvalSymlinks(filepath.Dir(abs)); derr == nil {
			abs = filepath.Join(dirResolved, filepath.Base(abs))
		}
	}

	if pv.isWithinRoot(abs, pv.workspaceRoot) {
		return abs, nil
	}
	for _, root := range pv.allowedRoots {
		if pv.isWithinRoot(abs, root) {
			return abs, nil
		}
	}
	return "", fmt.Errorf("path %q is outside workspace %q", requested, pv.workspaceRoot)
}

// Workspace reports the configured workspace root.
func (pv *PathValidator) Workspace() string {
	if pv == nil {
		return ""
	}
	return pv.workspaceRoot
}

func (pv *PathValidator) isWithinRoot(path, root string) bool {
	if path == root {
		return true
	}
	root = filepath.Clean(root)
	if root == "." || root == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}
