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
	ErrPathEscape    = errors.New("path escapes workspace")
	ErrSymlinkEscape = errors.New("symlink points outside workspace")
	ErrInvalidPath   = errors.New("invalid path")
	ErrNullByte      = errors.New("path contains null byte")
	ErrNotAbsolute   = errors.New("path must resolve to an absolute location")
)

type PathValidator struct {
	workspace      string
	allowedRoots   []string
	followSymlinks bool
}

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
		workspace:    canonicalWorkspace,
		allowedRoots: canonicalRoots,
	}, nil
}

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

	canonical, err := v.resolve(absPath)
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(canonical) {
		return "", ErrNotAbsolute
	}

	if v.hasPrefix(canonical, v.workspace) {
		return canonical, nil
	}

	for _, root := range v.allowedRoots {
		if v.hasPrefix(canonical, root) {
			return canonical, nil
		}
	}

	return "", ErrPathEscape
}

func (v *PathValidator) Workspace() string {
	if v == nil {
		return ""
	}
	return v.workspace
}

func (v *PathValidator) AllowedRoots() []string {
	if v == nil {
		return nil
	}
	out := make([]string, len(v.allowedRoots))
	copy(out, v.allowedRoots)
	return out
}

func (v *PathValidator) resolve(absPath string) (string, error) {
	if v.followSymlinks {
		canonical, err := filepath.EvalSymlinks(absPath)
		if err == nil {
			return canonical, nil
		}

		parent := filepath.Dir(absPath)
		if parent == absPath {
			return "", fmt.Errorf("resolve path %q: %w", absPath, err)
		}

		canonicalParent, perr := filepath.EvalSymlinks(parent)
		if perr != nil {
			return "", fmt.Errorf("resolve parent %q: %w", parent, perr)
		}

		return filepath.Join(canonicalParent, filepath.Base(absPath)), nil
	}

	if err := ensureNoSymlink(absPath); err != nil {
		return "", err
	}

	return absPath, nil
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
