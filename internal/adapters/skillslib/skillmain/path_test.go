package skillmain

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/domain/policy"
)

func TestResolvePathsRejectsGlobPatternWhoseBaseEscapesWorkspace(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	rc := testPathRunContext(t, workspace)
	resolved, err := ResolvePaths(rc, filepath.Join("..", "outside", "*.txt"), nil)
	if err == nil {
		t.Fatalf("expected escaping glob to be rejected, got paths %v", resolved)
	}
}

func TestResolvePathsAllowsWorkspaceGlob(t *testing.T) {
	workspace := t.TempDir()
	nested := filepath.Join(workspace, "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	want := []string{
		filepath.Join(workspace, "a.txt"),
		filepath.Join(nested, "b.txt"),
	}
	for _, path := range want {
		if err := os.WriteFile(path, []byte("ok"), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	got, err := ResolvePaths(testPathRunContext(t, workspace), "*.txt", []string{filepath.Join("nested", "*.txt")})
	if err != nil {
		t.Fatalf("ResolvePaths: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved paths = %v, want %v", got, want)
	}
}

func TestResolvePathsRejectsGeneratedEscapingGlobBases(t *testing.T) {
	base := t.TempDir()
	workspace := filepath.Join(base, "workspace")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	rc := testPathRunContext(t, workspace)

	prop := func(seed uint8) bool {
		outside := filepath.Join(base, fmt.Sprintf("outside-%d", seed))
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Logf("mkdir outside: %v", err)
			return false
		}
		if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
			t.Logf("write outside file: %v", err)
			return false
		}

		got, err := ResolvePaths(rc, filepath.Join("..", filepath.Base(outside), "*.txt"), nil)
		if err == nil {
			t.Logf("ResolvePaths accepted escaping glob, got %v", got)
			return false
		}
		return true
	}

	if err := quick.Check(prop, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
}

func testPathRunContext(t *testing.T, workspace string) *RunContext {
	t.Helper()
	pv, err := policy.NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("path validator: %v", err)
	}
	return &RunContext{PathValidator: pv}
}
