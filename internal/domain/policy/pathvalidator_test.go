package policy_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/domain/policy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathValidator_BasicValidation(t *testing.T) {
	tmpDir := t.TempDir()
	validator, err := policy.NewPathValidator(tmpDir, nil)
	require.NoError(t, err)

	outsidePath := filepath.Join(tmpDir, "..", "outside.txt")

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
			path:    filepath.Join("subdir", "file.txt"),
			wantErr: nil,
		},
		{
			name:    "path traversal attempt",
			path:    filepath.Join("..", "..", "etc", "passwd"),
			wantErr: policy.ErrPathEscape,
		},
		{
			name:    "absolute path outside workspace",
			path:    outsidePath,
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
			resolved, err := validator.ValidatePath(tt.path)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assertPathWithin(t, validator.Workspace(), resolved)
		})
	}
}

func TestPathValidator_SymlinkAttacks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	tmpDir := t.TempDir()
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "secret.txt")
	require.NoError(t, os.WriteFile(outsideFile, []byte("secret"), 0o600))

	validator, err := policy.NewPathValidator(tmpDir, nil)
	require.NoError(t, err)

	symlinkPath := filepath.Join(tmpDir, "evil-link")
	require.NoError(t, os.Symlink(outsideFile, symlinkPath))

	_, err = validator.ValidatePath("evil-link")
	assert.ErrorIs(t, err, policy.ErrSymlinkEscape)
}

func TestPathValidator_EdgeCases(t *testing.T) {
	tmpDir := t.TempDir()
	validator, err := policy.NewPathValidator(tmpDir, nil)
	require.NoError(t, err)

	tests := []string{
		"",
		".",
		filepath.Join("subdir", "..", "file.txt"),
		filepath.Join("sub", "", "dir", "file.txt"),
		"subdir/",
	}

	for _, path := range tests {
		t.Run("edge:"+path, func(t *testing.T) {
			resolved, err := validator.ValidatePath(path)
			require.NoError(t, err)
			assertPathWithin(t, validator.Workspace(), resolved)
		})
	}
}

func TestPathValidator_AllowsWorkspaceChildStartingWithDots(t *testing.T) {
	tmpDir := t.TempDir()
	validator, err := policy.NewPathValidator(tmpDir, nil)
	require.NoError(t, err)

	resolved, err := validator.ValidatePath(filepath.Join("..cache", "state.json"))
	require.NoError(t, err)
	assertPathWithin(t, validator.Workspace(), resolved)
	assert.Equal(t, filepath.Join(validator.Workspace(), "..cache", "state.json"), resolved)
}

func TestPathValidator_AllowedRoots(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()

	validator, err := policy.NewPathValidator(workspace, []string{shared})
	require.NoError(t, err)

	workspaceFile := filepath.Join("notes", "todo.txt")
	resolved, err := validator.ValidatePath(workspaceFile)
	require.NoError(t, err)
	assertPathWithin(t, validator.Workspace(), resolved)

	sharedFile := filepath.Join(shared, "shared.txt")
	resolved, err = validator.ValidatePath(sharedFile)
	require.NoError(t, err)
	assertPathWithinAllowedRoots(t, validator.AllowedRoots(), resolved)

	_, err = validator.ValidatePath(filepath.Join(shared, "..", "..", "etc", "passwd"))
	assert.ErrorIs(t, err, policy.ErrPathEscape)
}

// A vanished/un-canonicalizable additional allowed root (e.g. a transient
// /tmp/foxctl-* dir removed between the caller's glob and construction) must be
// skipped rather than aborting the validator — additional roots are best-effort
// and skipping is fail-closed. The valid workspace must still work.
func TestPathValidator_SkipsUnresolvableAllowedRoots(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()
	missing := filepath.Join(t.TempDir(), "was-removed")

	validator, err := policy.NewPathValidator(workspace, []string{missing, shared})
	require.NoError(t, err)

	resolved, err := validator.ValidatePath(filepath.Join("notes", "todo.txt"))
	require.NoError(t, err)
	assertPathWithin(t, validator.Workspace(), resolved)

	// The surviving allowed root is still honored.
	resolved, err = validator.ValidatePath(filepath.Join(shared, "shared.txt"))
	require.NoError(t, err)
	assertPathWithinAllowedRoots(t, validator.AllowedRoots(), resolved)

	// The missing root conferred no access, so paths under it are rejected.
	_, err = validator.ValidatePath(filepath.Join(missing, "leak.txt"))
	require.Error(t, err)
}

// The primary workspace remains a hard requirement even after the allowed-root
// tolerance above.
func TestPathValidator_MissingWorkspaceStillErrors(t *testing.T) {
	missingWorkspace := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := policy.NewPathValidator(missingWorkspace, nil)
	require.Error(t, err)
}

func TestPathValidator_PartialMatchPrevention(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	evilWorkspace := filepath.Join(base, "workspace-evil")

	require.NoError(t, os.MkdirAll(workspace, 0o755))
	require.NoError(t, os.MkdirAll(evilWorkspace, 0o755))

	validator, err := policy.NewPathValidator(workspace, nil)
	require.NoError(t, err)

	_, err = validator.ValidatePath(filepath.Join(evilWorkspace, "evil.txt"))
	assert.ErrorIs(t, err, policy.ErrPathEscape)
}

func TestPathValidator_GeneratedSiblingPrefixPathsRejected(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	require.NoError(t, os.MkdirAll(workspace, 0o755))

	validator, err := policy.NewPathValidator(workspace, nil)
	require.NoError(t, err)

	prop := func(rawSuffix, rawLeaf string) bool {
		suffix := safePathName(rawSuffix)
		leaf := safePathName(rawLeaf)
		sibling := filepath.Join(base, "workspace-"+suffix)
		if err := os.MkdirAll(sibling, 0o755); err != nil {
			t.Logf("mkdir sibling: %v", err)
			return false
		}
		candidate := filepath.Join(sibling, leaf)
		if err := os.WriteFile(candidate, []byte("outside"), 0o644); err != nil {
			t.Logf("write candidate: %v", err)
			return false
		}

		_, err := validator.ValidatePath(candidate)
		if !errors.Is(err, policy.ErrPathEscape) {
			t.Logf("sibling path %q was not rejected as escaping workspace", candidate)
			return false
		}
		return true
	}

	require.NoError(t, quick.Check(prop, &quick.Config{MaxCount: 100}))
}

func TestPathValidator_SymlinkDescendantsCannotEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	workspace := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "nested"), 0o755))

	link := filepath.Join(workspace, "outside-link")
	require.NoError(t, os.Symlink(outside, link))

	validator, err := policy.NewPathValidator(workspace, nil)
	require.NoError(t, err)

	prop := func(rawLeaf string) bool {
		leaf := safePathName(rawLeaf)
		candidate := filepath.Join("outside-link", "nested", leaf)
		_, err := validator.ValidatePath(candidate)
		if !errors.Is(err, policy.ErrSymlinkEscape) {
			t.Logf("symlink descendant %q error = %v, want ErrSymlinkEscape", candidate, err)
			return false
		}
		return true
	}

	require.NoError(t, quick.Check(prop, &quick.Config{MaxCount: 100}))
}

func TestPathValidator_AllowedRootSymlinkDescendantsCannotEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated privileges on Windows")
	}

	workspace := t.TempDir()
	shared := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(outside, "nested"), 0o755))

	link := filepath.Join(shared, "outside-link")
	require.NoError(t, os.Symlink(outside, link))

	validator, err := policy.NewPathValidator(workspace, []string{shared})
	require.NoError(t, err)

	prop := func(rawLeaf string) bool {
		leaf := safePathName(rawLeaf)
		candidate := filepath.Join(shared, "outside-link", "nested", leaf)
		_, err := validator.ValidatePath(candidate)
		if !errors.Is(err, policy.ErrSymlinkEscape) {
			t.Logf("allowed-root symlink descendant %q error = %v, want ErrSymlinkEscape", candidate, err)
			return false
		}
		return true
	}

	require.NoError(t, quick.Check(prop, &quick.Config{MaxCount: 100}))
}

func TestPathValidator_GeneratedSuccessfulPathsStayInsideAuthorizedRoots(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()
	validator, err := policy.NewPathValidator(workspace, []string{shared})
	require.NoError(t, err)

	prop := func(useShared bool, rawDir, rawLeaf string) bool {
		dir := safePathName(rawDir)
		leaf := safePathName(rawLeaf)
		root := workspace
		if useShared {
			root = shared
		}
		candidate := filepath.Join(root, dir, leaf)
		resolved, err := validator.ValidatePath(candidate)
		if err != nil {
			t.Logf("valid path %q rejected: %v", candidate, err)
			return false
		}
		if pathWithinAny(resolved, append([]string{validator.Workspace()}, validator.AllowedRoots()...)) {
			return true
		}
		t.Logf("resolved path %q not under authorized roots", resolved)
		return false
	}

	require.NoError(t, quick.Check(prop, &quick.Config{MaxCount: 100}))
}

func TestPathValidator_InvalidWorkspace(t *testing.T) {
	_, err := policy.NewPathValidator("", nil)
	assert.Error(t, err)
}

func TestPathValidator_InvalidUTF8(t *testing.T) {
	tmpDir := t.TempDir()
	validator, err := policy.NewPathValidator(tmpDir, nil)
	require.NoError(t, err)

	invalid := string([]byte{0xff, 0xfe, 0xfd})
	_, err = validator.ValidatePath(invalid)
	assert.ErrorIs(t, err, policy.ErrInvalidPath)
}

func TestPathValidator_AllowedRootsCopy(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()

	validator, err := policy.NewPathValidator(workspace, []string{shared})
	require.NoError(t, err)

	roots := validator.AllowedRoots()
	require.Len(t, roots, 1)
	roots[0] = "tampered"

	resolved, err := validator.ValidatePath(filepath.Join(shared, "doc.txt"))
	require.NoError(t, err)
	assertPathWithinAllowedRoots(t, validator.AllowedRoots(), resolved)
}

func assertPathWithin(t *testing.T, root, candidate string) {
	t.Helper()
	rel, err := filepath.Rel(root, candidate)
	require.NoError(t, err)
	assert.True(t, relInsideRoot(rel), "expected %q to be within %q", candidate, root)
}

func assertPathWithinAllowedRoots(t *testing.T, roots []string, candidate string) {
	t.Helper()
	for _, root := range roots {
		rel, err := filepath.Rel(root, candidate)
		require.NoError(t, err)
		if relInsideRoot(rel) {
			return
		}
	}
	t.Fatalf("candidate %q was not within allowed roots %v", candidate, roots)
}

func pathWithinAny(candidate string, roots []string) bool {
	for _, root := range roots {
		rel, err := filepath.Rel(root, candidate)
		if err == nil && relInsideRoot(rel) {
			return true
		}
	}
	return false
}

func relInsideRoot(rel string) bool {
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
}

func safePathName(raw string) string {
	name := strings.Map(func(r rune) rune {
		switch r {
		case 0, '/', '\\':
			return '-'
		default:
			return r
		}
	}, strings.TrimSpace(raw))
	name = strings.Trim(name, ". ")
	if name == "" {
		return "x"
	}
	runes := []rune(name)
	if len(runes) > 32 {
		name = string(runes[:32])
	}
	return name
}
