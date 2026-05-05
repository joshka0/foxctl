package evidence

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidencePackageDoesNotImportHigherLayers(t *testing.T) {
	forbidden := []string{
		"/internal/intelligence/indexing/repoindex",
		"/internal/intelligence/searchindex",
		"/internal/intelligence/indexing/semanticanchors",
		"/internal/context/contextplane",
		"/internal/context/memorycore",
		"/internal/storage",
		"/internal/tooling/tools/obsidian",
		"/internal/v2",
	}
	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, banned := range forbidden {
				if strings.Contains(importPath, banned) {
					t.Fatalf("%s imports forbidden package %s", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
