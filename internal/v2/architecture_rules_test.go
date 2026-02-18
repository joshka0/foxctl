package v2

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCorePackagesDoNotImportAdaptersOrPorts(t *testing.T) {
	t.Parallel()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	coreRoot := filepath.Join(repoRoot, "internal", "v2", "core")

	var files []string
	err := filepath.WalkDir(coreRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk core package failed: %v", err)
	}

	fset := token.NewFileSet()
	for _, path := range files {
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			t.Fatalf("parse imports for %s failed: %v", path, parseErr)
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			if strings.Contains(importPath, "/internal/v2/adapters/") ||
				strings.HasSuffix(importPath, "/internal/v2/adapters") ||
				strings.Contains(importPath, "/internal/v2/ports/") ||
				strings.HasSuffix(importPath, "/internal/v2/ports") {
				t.Fatalf("core import boundary violated in %s: %s", path, importPath)
			}
		}
	}
}
