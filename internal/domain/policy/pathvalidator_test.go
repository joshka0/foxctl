package policy_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
	if rel == "." {
		return
	}
	assert.False(t, strings.HasPrefix(rel, ".."), "expected %q to be within %q", candidate, root)
}

func assertPathWithinAllowedRoots(t *testing.T, roots []string, candidate string) {
	t.Helper()
	for _, root := range roots {
		rel, err := filepath.Rel(root, candidate)
		require.NoError(t, err)
		if rel == "." || !strings.HasPrefix(rel, "..") {
			return
		}
	}
	t.Fatalf("candidate %q was not within allowed roots %v", candidate, roots)
}
