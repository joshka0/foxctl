package pathutil

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"
)

func TestExtractPathFromInputUsesFieldPrecedence(t *testing.T) {
	input := ToolPathInput{
		FilePath:    "/first.go",
		Path:        "/second.go",
		File:        "/third.go",
		CurrentPath: "/fourth.go",
	}

	if got := ExtractPathFromInput(input); got != "/first.go" {
		t.Fatalf("ExtractPathFromInput() = %q, want /first.go", got)
	}
}

func TestDecodeToolPathInputToleratesMixedArrays(t *testing.T) {
	raw := json.RawMessage(`{
		"file_path": "/root.go",
		"edits": [
			{"path": "/a.go"},
			42,
			{"file": "/b.go"},
			{"ignored": "value"}
		],
		"files": ["/c.go", 17, "", "/d.go"]
	}`)

	input, ok := DecodeToolPathInput(raw)
	if !ok {
		t.Fatal("DecodeToolPathInput returned !ok")
	}

	want := []string{"/a.go", "/b.go", "/c.go", "/d.go", "/root.go"}
	if got := ExtractPathsFromInput(input); !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractPathsFromInput() = %#v, want %#v", got, want)
	}
}

func TestWorkspaceBoundaryDistinguishesDotDotNamesFromTraversal(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "workspace")
	dotDotChild := filepath.Join(root, "..cache", "file.go")
	if !IsUnderWorkspace(dotDotChild, root) {
		t.Fatalf("IsUnderWorkspace(%q, %q) = false, want true", dotDotChild, root)
	}
	if got := RelativePath(dotDotChild, root); got != filepath.Join("..cache", "file.go") {
		t.Fatalf("RelativePath(%q, %q) = %q", dotDotChild, root, got)
	}

	sibling := filepath.Join(filepath.Dir(root), "workspace-sibling", "file.go")
	if IsUnderWorkspace(sibling, root) {
		t.Fatalf("IsUnderWorkspace(%q, %q) = true, want false", sibling, root)
	}
	if got := RelativePath(sibling, root); got != sibling {
		t.Fatalf("RelativePath outside workspace = %q, want original %q", got, sibling)
	}
}

func TestContainedRelativePathDistinguishesDotDotNamesFromTraversal(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "workspace")
	if got := ContainedRelativePath(filepath.Join(root, "internal", "auth.go"), root); got != "internal/auth.go" {
		t.Fatalf("absolute workspace path = %q, want internal/auth.go", got)
	}
	if got := ContainedRelativePath(filepath.Join("internal", "auth.go"), root); got != "internal/auth.go" {
		t.Fatalf("relative workspace path = %q, want internal/auth.go", got)
	}
	if got := ContainedRelativePath(filepath.Join("..", "outside.go"), root); got != "" {
		t.Fatalf("parent-relative escape = %q, want empty", got)
	}
	if got := ContainedRelativePath(filepath.Join(filepath.Dir(root), "outside.go"), root); got != "" {
		t.Fatalf("absolute sibling escape = %q, want empty", got)
	}
	if got := ContainedRelativePath(filepath.Join(root, "..cache", "demo.go"), root); got != "..cache/demo.go" {
		t.Fatalf("dot-dot-prefixed child = %q, want ..cache/demo.go", got)
	}
	if got := ContainedRelativePath(root, root); got != "" {
		t.Fatalf("workspace root = %q, want empty", got)
	}
}

func TestContainedRelativePathPropertyRejectsGeneratedEscapes(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(seed string) bool {
		name := dotDotPathSegment(seed)
		if got := ContainedRelativePath(filepath.Join("..", name, "file.go"), root); got != "" {
			t.Logf("relative escape normalized to %q", got)
			return false
		}
		if got := ContainedRelativePath(filepath.Join(parent, name, "file.go"), root); got != "" {
			t.Logf("absolute escape normalized to %q", got)
			return false
		}
		want := filepath.ToSlash(filepath.Join(name, "file.go"))
		return ContainedRelativePath(filepath.Join(root, name, "file.go"), root) == want
	}, cfg)
	if err != nil {
		t.Fatalf("contained relative path property failed: %v", err)
	}
}

func TestIsUnderWorkspacePropertyKeepsDotDotPrefixedChildrenInside(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "workspace")
	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(seed string) bool {
		name := dotDotPathSegment(seed)
		candidate := filepath.Join(root, name, "file.go")
		return IsUnderWorkspace(candidate, root) &&
			RelativePath(candidate, root) == filepath.Join(name, "file.go")
	}, cfg)
	if err != nil {
		t.Fatalf("dot-dot child property failed: %v", err)
	}
}

func TestIsUnderWorkspacePropertyRejectsSiblingPrefixes(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	root := filepath.Join(parent, "workspace")
	cfg := &quick.Config{MaxCount: 250}
	err := quick.Check(func(seed string) bool {
		sibling := filepath.Join(parent, "workspace-"+dotDotPathSegment(seed), "file.go")
		return !IsUnderWorkspace(sibling, root) && RelativePath(sibling, root) == sibling
	}, cfg)
	if err != nil {
		t.Fatalf("sibling prefix property failed: %v", err)
	}
}

func dotDotPathSegment(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return ".." + hex.EncodeToString(sum[:4])
}
