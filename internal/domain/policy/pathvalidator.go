package policy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

var (
	// ErrPathEscape indicates the path resolves outside the workspace.
	ErrPathEscape = errors.New("path escapes workspace")
	// ErrSymlinkEscape indicates a symlink resolves outside the workspace.
	ErrSymlinkEscape = errors.New("symlink points outside workspace")
	// ErrInvalidPath indicates the path is invalid or malformed.
	ErrInvalidPath = errors.New("invalid path")
	// ErrNullByte indicates the path contains a null byte.
	ErrNullByte = errors.New("path contains null byte")
	// ErrNotAbsolute indicates the path must resolve to an absolute location.
	ErrNotAbsolute = errors.New("path must resolve to an absolute location")
)

// PathValidator ensures paths are safe and within the workspace or allowed roots.
type PathValidator struct {
	workspace      string
	allowedRoots   []string
	followSymlinks bool
}

// NewPathValidator creates a new validator anchored to the workspace.
func NewPathValidator(workspace string, allowedRoots []string) (*PathValidator, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, fmt.Errorf("workspace cannot be empty")
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace: %w", err)
	}

	canonicalWorkspace, err := filepath.EvalSymlinks(absWorkspace)
	if err != nil {
		return nil, fmt.Errorf("canonicalize workspace: %w", err)
	}
	canonicalWorkspace = filepath.Clean(canonicalWorkspace)

	canonicalRoots := make([]string, 0, len(allowedRoots))
	seen := map[string]struct{}{canonicalWorkspace: {}}
	for _, root := range allowedRoots {
		if strings.TrimSpace(root) == "" {
			continue
		}

		absRoot, err := filepath.Abs(root)
		if err != nil {
			return nil, fmt.Errorf("resolve root %q: %w", root, err)
		}

		canonicalRoot, err := filepath.EvalSymlinks(absRoot)
		if err != nil {
			return nil, fmt.Errorf("canonicalize root %q: %w", root, err)
		}
		canonicalRoot = filepath.Clean(canonicalRoot)

		if _, dup := seen[canonicalRoot]; dup {
			continue
		}
		seen[canonicalRoot] = struct{}{}
		canonicalRoots = append(canonicalRoots, canonicalRoot)
	}

	return &PathValidator{
		workspace:      canonicalWorkspace,
		allowedRoots:   canonicalRoots,
		followSymlinks: true,
	}, nil
}

// ValidatePath validates and resolves a user-provided path.
// It returns the absolute canonical path if valid, or an error if it escapes the workspace.
func (v *PathValidator) ValidatePath(userPath string) (string, error) {
	if v == nil {
		return "", fmt.Errorf("path validator not configured")
	}

	if !utf8.ValidString(userPath) {
		return "", ErrInvalidPath
	}
	if strings.ContainsRune(userPath, 0) {
		return "", ErrNullByte
	}

	cleaned := filepath.Clean(userPath)
	var absPath string
	if filepath.IsAbs(cleaned) {
		absPath = cleaned
	} else {
		absPath = filepath.Join(v.workspace, cleaned)
	}
	absPath = filepath.Clean(absPath)

	canonical, usedSymlink, err := v.resolve(absPath)
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(canonical) {
		return "", ErrNotAbsolute
	}

	if v.hasPrefix(canonical, v.workspace) {
		return canonical, nil
	}

	if v.pathInsideRoots(canonical) {
		return canonical, nil
	}

	if usedSymlink && v.pathInsideRoots(absPath) {
		return "", ErrSymlinkEscape
	}
	return "", ErrPathEscape
}

// Workspace returns the configured workspace path.
func (v *PathValidator) Workspace() string {
	if v == nil {
		return ""
	}
	return v.workspace
}

// AllowedRoots returns the list of additional allowed root directories.
func (v *PathValidator) AllowedRoots() []string {
	if v == nil {
		return nil
	}
	out := make([]string, len(v.allowedRoots))
	copy(out, v.allowedRoots)
	return out
}

func (v *PathValidator) resolve(absPath string) (string, bool, error) {
	if v.followSymlinks {
		return resolveWithSymlinks(absPath)
	}

	if err := ensureNoSymlink(absPath); err != nil {
		return "", false, err
	}

	return absPath, false, nil
}

func resolveWithSymlinks(path string) (string, bool, error) {
	cleaned := filepath.Clean(path)
	pending := []string{}
	current := cleaned
	usedSymlink := false

	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				usedSymlink = true
			}
			break
		}
		if os.IsNotExist(err) {
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			pending = append([]string{filepath.Base(current)}, pending...)
			current = parent
			continue
		}
		return "", false, fmt.Errorf("stat %q: %w", current, err)
	}

	canonical, err := filepath.EvalSymlinks(current)
	if err != nil {
		return "", false, fmt.Errorf("resolve path %q: %w", current, err)
	}
	if canonical != current {
		usedSymlink = true
	}
	for _, part := range pending {
		canonical = filepath.Join(canonical, part)
	}

	return filepath.Clean(canonical), usedSymlink, nil
}

func ensureNoSymlink(path string) error {
	current := filepath.Clean(path)
	for {
		info, err := os.Lstat(current)
		if err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return ErrSymlinkEscape
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("stat %q: %w", current, err)
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return nil
}

func (v *PathValidator) hasPrefix(path, root string) bool {
	if root == "" {
		return false
	}

	path = filepath.Clean(path)
	root = filepath.Clean(root)

	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
		root = strings.ToLower(root)
	}

	if path == root {
		return true
	}

	sep := string(filepath.Separator)
	if !strings.HasSuffix(root, sep) {
		root += sep
	}
	if !strings.HasSuffix(path, sep) {
		path += sep
	}

	return strings.HasPrefix(path, root)
}

func (v *PathValidator) pathInsideRoots(path string) bool {
	if v.hasPrefix(path, v.workspace) {
		return true
	}
	for _, root := range v.allowedRoots {
		if v.hasPrefix(path, root) {
			return true
		}
	}
	return false
}
