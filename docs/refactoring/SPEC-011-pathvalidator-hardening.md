# SPEC-011: PathValidator Hardening

## Status
**Not Started** | Priority: Critical | Complexity: Low | Security: High

## Problem Statement

The PathValidator in `internal/domain/policy/policy.go` is responsible for preventing workspace escapes and ensuring skills cannot access files outside their allowed boundaries. While the basic implementation exists, it needs hardening against edge cases and attack vectors.

### Security Risks
1. **Symlink attacks**: Following symlinks outside workspace
2. **Path traversal**: `../../../etc/passwd` style attacks
3. **Race conditions**: TOCTOU (Time-of-Check-Time-of-Use) vulnerabilities
4. **Unicode normalization**: Exploiting different Unicode representations
5. **Case sensitivity**: Platform-specific bypass attempts
6. **Null bytes**: Path truncation attacks

### Current State
```go
// internal/domain/policy/policy.go
type PathValidator struct {
    workspace      string
    allowedRoots   []string
}
```

Basic validation exists but needs systematic hardening.

## Proposed Solution

### 1. Enhanced Path Validation

```go
// internal/domain/policy/pathvalidator.go
package policy

import (
    "errors"
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "unicode/utf8"
)

var (
    ErrPathEscape       = errors.New("path escapes workspace")
    ErrSymlinkEscape    = errors.New("symlink points outside workspace")
    ErrInvalidPath      = errors.New("invalid path")
    ErrNullByte         = errors.New("path contains null byte")
    ErrNotAbsolute      = errors.New("path must be absolute after resolution")
)

type PathValidator struct {
    workspace      string   // Canonical absolute workspace path
    allowedRoots   []string // Additional canonical allowed roots
    followSymlinks bool     // Whether to allow symlinks
}

// NewPathValidator creates a validator with canonical paths.
func NewPathValidator(workspace string, allowedRoots []string) (*PathValidator, error) {
    // Canonicalize workspace
    absWorkspace, err := filepath.Abs(workspace)
    if err != nil {
        return nil, fmt.Errorf("resolve workspace: %w", err)
    }

    canonical, err := filepath.EvalSymlinks(absWorkspace)
    if err != nil {
        return nil, fmt.Errorf("canonicalize workspace: %w", err)
    }

    // Canonicalize allowed roots
    canonicalRoots := make([]string, 0, len(allowedRoots))
    for _, root := range allowedRoots {
        absRoot, err := filepath.Abs(root)
        if err != nil {
            return nil, fmt.Errorf("resolve root %s: %w", root, err)
        }

        canonicalRoot, err := filepath.EvalSymlinks(absRoot)
        if err != nil {
            return nil, fmt.Errorf("canonicalize root %s: %w", root, err)
        }

        canonicalRoots = append(canonicalRoots, canonicalRoot)
    }

    return &PathValidator{
        workspace:      canonical,
        allowedRoots:   canonicalRoots,
        followSymlinks: false, // Default: no symlinks
    }, nil
}

// ValidatePath ensures path is within workspace or allowed roots.
// Returns the canonical absolute path if valid.
func (v *PathValidator) ValidatePath(userPath string) (string, error) {
    // 1. Check for null bytes (security)
    if !utf8.ValidString(userPath) || strings.ContainsRune(userPath, 0) {
        return "", ErrNullByte
    }

    // 2. Clean the path (removes .., ., etc.)
    cleaned := filepath.Clean(userPath)

    // 3. Make absolute relative to workspace
    var absPath string
    if filepath.IsAbs(cleaned) {
        absPath = cleaned
    } else {
        absPath = filepath.Join(v.workspace, cleaned)
    }

    // 4. Resolve symlinks if policy allows
    var canonical string
    var err error
    if v.followSymlinks {
        canonical, err = filepath.EvalSymlinks(absPath)
        if err != nil {
            // Path doesn't exist yet - validate parent directory
            parent := filepath.Dir(absPath)
            canonical, err = filepath.EvalSymlinks(parent)
            if err != nil {
                return "", fmt.Errorf("resolve parent: %w", err)
            }
            canonical = filepath.Join(canonical, filepath.Base(absPath))
        }
    } else {
        // Don't follow symlinks - check if it is a symlink
        info, err := os.Lstat(absPath)
        if err == nil && info.Mode()&os.ModeSymlink != 0 {
            return "", ErrSymlinkEscape
        }
        canonical = absPath
    }

    // 5. Must be absolute after resolution
    if !filepath.IsAbs(canonical) {
        return "", ErrNotAbsolute
    }

    // 6. Check if within workspace
    if v.hasPrefix(canonical, v.workspace) {
        return canonical, nil
    }

    // 7. Check if within any allowed root
    for _, root := range v.allowedRoots {
        if v.hasPrefix(canonical, root) {
            return canonical, nil
        }
    }

    return "", ErrPathEscape
}

// hasPrefix checks if path is within root (secure prefix check).
func (v *PathValidator) hasPrefix(path, root string) bool {
    // Ensure both are clean and absolute
    path = filepath.Clean(path)
    root = filepath.Clean(root)

    // Add separator to prevent partial matches
    // /workspace-evil should not match /workspace
    if !strings.HasSuffix(root, string(filepath.Separator)) {
        root += string(filepath.Separator)
    }

    if !strings.HasSuffix(path, string(filepath.Separator)) {
        path += string(filepath.Separator)
    }

    return strings.HasPrefix(path, root)
}

// Workspace returns the canonical workspace path.
func (v *PathValidator) Workspace() string {
    return v.workspace
}

// AllowedRoots returns the canonical allowed roots.
func (v *PathValidator) AllowedRoots() []string {
    return append([]string{}, v.allowedRoots...)
}
```

### 2. Comprehensive Test Suite

```go
// internal/domain/policy/pathvalidator_test.go
package policy_test

import (
    "os"
    "path/filepath"
    "testing"

    "github.com/jkatigb/agentctl/internal/domain/policy"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestPathValidator_BasicValidation(t *testing.T) {
    tmpDir := t.TempDir()
    validator, err := policy.NewPathValidator(tmpDir, nil)
    require.NoError(t, err)

    tests := []struct {
        name    string
        path    string
        wantErr error
    }{
        {
            name:    "absolute path within workspace",
            path:    filepath.Join(tmpDir, "file.txt"),
            wantErr: nil,
        },
        {
            name:    "relative path within workspace",
            path:    "file.txt",
            wantErr: nil,
        },
        {
            name:    "subdirectory path",
            path:    "subdir/file.txt",
            wantErr: nil,
        },
        {
            name:    "path traversal attempt",
            path:    "../../../etc/passwd",
            wantErr: policy.ErrPathEscape,
        },
        {
            name:    "absolute path outside workspace",
            path:    "/etc/passwd",
            wantErr: policy.ErrPathEscape,
        },
        {
            name:    "null byte injection",
            path:    "file\x00.txt",
            wantErr: policy.ErrNullByte,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := validator.ValidatePath(tt.path)
            if tt.wantErr != nil {
                assert.ErrorIs(t, err, tt.wantErr)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}

func TestPathValidator_SymlinkAttacks(t *testing.T) {
    tmpDir := t.TempDir()

    // Create a directory outside workspace
    outsideDir := t.TempDir()
    outsideFile := filepath.Join(outsideDir, "secret.txt")
    err := os.WriteFile(outsideFile, []byte("secret"), 0600)
    require.NoError(t, err)

    // Create symlink inside workspace pointing outside
    symlinkPath := filepath.Join(tmpDir, "evil-link")
    err = os.Symlink(outsideFile, symlinkPath)
    require.NoError(t, err)

    validator, err := policy.NewPathValidator(tmpDir, nil)
    require.NoError(t, err)

    // Should reject symlink to outside path
    _, err = validator.ValidatePath(symlinkPath)
    assert.ErrorIs(t, err, policy.ErrSymlinkEscape)
}

func TestPathValidator_EdgeCases(t *testing.T) {
    tmpDir := t.TempDir()
    validator, err := policy.NewPathValidator(tmpDir, nil)
    require.NoError(t, err)

    tests := []struct {
        name    string
        path    string
        wantErr error
    }{
        {
            name:    "empty path",
            path:    "",
            wantErr: nil, // Resolves to workspace
        },
        {
            name:    "current directory",
            path:    ".",
            wantErr: nil,
        },
        {
            name:    "parent directory within workspace",
            path:    "subdir/../file.txt",
            wantErr: nil,
        },
        {
            name:    "multiple slashes",
            path:    "sub//dir///file.txt",
            wantErr: nil,
        },
        {
            name:    "trailing slash",
            path:    "subdir/",
            wantErr: nil,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            _, err := validator.ValidatePath(tt.path)
            if tt.wantErr != nil {
                assert.ErrorIs(t, err, tt.wantErr)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}

func TestPathValidator_AllowedRoots(t *testing.T) {
    tmpWorkspace := t.TempDir()
    tmpShared := t.TempDir()

    validator, err := policy.NewPathValidator(tmpWorkspace, []string{tmpShared})
    require.NoError(t, err)

    // File in workspace - should pass
    workspaceFile := filepath.Join(tmpWorkspace, "file.txt")
    _, err = validator.ValidatePath(workspaceFile)
    assert.NoError(t, err)

    // File in allowed root - should pass
    sharedFile := filepath.Join(tmpShared, "shared.txt")
    _, err = validator.ValidatePath(sharedFile)
    assert.NoError(t, err)

    // File outside both - should fail
    _, err = validator.ValidatePath("/etc/passwd")
    assert.ErrorIs(t, err, policy.ErrPathEscape)
}

func TestPathValidator_PartialMatchPrevention(t *testing.T) {
    tmpDir := t.TempDir()

    // Create workspace at /tmp/workspace
    workspace := filepath.Join(tmpDir, "workspace")
    err := os.MkdirAll(workspace, 0755)
    require.NoError(t, err)

    // Create evil workspace at /tmp/workspace-evil
    evilWorkspace := filepath.Join(tmpDir, "workspace-evil")
    err = os.MkdirAll(evilWorkspace, 0755)
    require.NoError(t, err)

    validator, err := policy.NewPathValidator(workspace, nil)
    require.NoError(t, err)

    // Should reject path in workspace-evil
    evilFile := filepath.Join(evilWorkspace, "evil.txt")
    _, err = validator.ValidatePath(evilFile)
    assert.ErrorIs(t, err, policy.ErrPathEscape)
}
```

## Implementation Plan

### Step 1: Enhance PathValidator (2h)
1. Add null byte checking
2. Add robust prefix checking (prevent partial matches)
3. Add symlink handling policy
4. Add canonical path resolution
5. Improve error messages with context

**Files:**
- `internal/domain/policy/pathvalidator.go`

**Tests:** Add test cases for each validation step

### Step 2: Comprehensive Test Suite (2h)
1. Basic validation tests
2. Symlink attack tests
3. Path traversal tests
4. Edge case tests
5. Allowed roots tests
6. Partial match prevention tests

**Files:**
- `internal/domain/policy/pathvalidator_test.go`

**Acceptance:** 100% coverage on PathValidator

### Step 3: Integration with Skill Execution (1h)
1. Ensure all file access goes through PathValidator
2. Add validation to fs/ls skill
3. Add validation to fs/read skill
4. Add validation to runner workspace setup

**Files:**
- `internal/execution/exec/executor.go`
- `skills/fs_ls/main.go`
- `skills/fs_read/main.go`

**Tests:** E2E tests with malicious paths

### Step 4: Documentation (30min)
1. Update IMPLEMENTATION_PRIORITY.md as complete
2. Add security documentation
3. Add examples to skill development guide

**Files:**
- `IMPLEMENTATION_PRIORITY.md`
- `docs/security.md` (new)

## Testing Strategy

### Unit Tests
- ✅ All validation functions
- ✅ Edge cases (null bytes, unicode, empty paths)
- ✅ Platform-specific behavior (Windows vs Unix)

### Integration Tests
- ✅ Skills accessing files through PathValidator
- ✅ Workspace setup in runners
- ✅ CAS file access validation

### Security Tests
- ✅ Symlink attacks
- ✅ Path traversal attempts
- ✅ Partial path matching bypasses
- ✅ TOCTOU scenarios
- ✅ Null byte injection

## Dependencies
- **Depends on:** None
- **Required by:** All file-accessing skills, runners

## Risks and Mitigations

### Risks
1. **Platform differences**: Windows vs Unix path handling
   - **Mitigation**: Comprehensive platform-specific tests

2. **Performance**: Path canonicalization can be slow
   - **Mitigation**: Cache canonical paths where appropriate

3. **Breaking changes**: Stricter validation may break existing skills
   - **Mitigation**: Audit existing skills first, provide migration guide

## Success Criteria
- ✅ 100% test coverage on PathValidator
- ✅ All security test cases pass
- ✅ No skills can escape workspace in tests
- ✅ Documentation complete
- ✅ Zero security warnings from static analysis

## Effort Estimate
**Total: 5.5 hours**
- Implementation: 2h
- Testing: 2h
- Integration: 1h
- Documentation: 30min

## References
- Current implementation: `internal/domain/policy/policy.go`
- Priority document: `IMPLEMENTATION_PRIORITY.md`
- Security best practices: [OWASP Path Traversal](https://owasp.org/www-community/attacks/Path_Traversal)
