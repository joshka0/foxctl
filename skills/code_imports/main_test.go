package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/platform/config"
	errs "github.com/joshka0/foxctl/internal/platform/errors"
)

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, workspace string) *skillmain.RunContext {
	t.Helper()
	t.Setenv("FOXCTL_WORKSPACE", workspace)
	state := t.TempDir()
	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   state + "/cas",
			Jobs:  state + "/jobs",
			Cache: state + "/cache",
		},
	}
	rc, err := skillmain.BuildRunContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func TestRunCodeImports(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(cwd) //nolint:errcheck
	}()

	code := `package main
import (
	"fmt"
	"os"
	"github.com/pkg/errors"
)
func main() {}`

	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	rc := newTestRunnerContext(t, stdout, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path:       "main.go",
		QueryType:  "list",
		Language:   "go",
		IncludeStd: true,
		MaxResults: 100,
		MaxDepth:   3,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Errorf("run failed: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}

	data := env["data"].(map[string]any)
	// Check for nil preview
	if data["preview"] == nil {
		t.Fatalf("preview is nil")
	}

	preview := data["preview"].([]any)

	if len(preview) != 3 {
		t.Errorf("expected 3 imports, got %d. Preview: %v", len(preview), preview)
	}
}

func TestRunCodeImportsGraph(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(cwd) //nolint:errcheck
	}()

	// File A imports B
	if err := os.WriteFile(filepath.Join(work, "a.js"), []byte("import * as b from './b.js';"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "b.js"), []byte("console.log('b');"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	rc := newTestRunnerContext(t, stdout, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path:       work,
		QueryType:  "graph",
		Language:   "javascript",
		MaxResults: 100,
		MaxDepth:   3,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Errorf("run failed: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data := env["data"].(map[string]any)
	preview := data["preview"].([]any)

	if len(preview) < 2 {
		t.Errorf("expected at least 2 nodes in graph, got %d", len(preview))
	}
}

func TestParserDirect(t *testing.T) {
	work := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(cwd) //nolint:errcheck
	}()

	code := `package main
import (
	"fmt"
	"os"
	"github.com/pkg/errors"
)
func main() {}`

	if err := os.WriteFile("main.go", []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(f.Imports) != 3 {
		t.Errorf("direct parser found %d imports, expected 3", len(f.Imports))
	}
}

func TestRunCodeImportsRejectsInvalidQueryType(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	stdout := &bytes.Buffer{}
	rc := newTestRunnerContext(t, stdout, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	err := run(ctx, rc, input{
		Path:      work,
		QueryType: "owners",
	})
	if err == nil {
		t.Fatal("expected invalid query_type to fail")
	}
	if !skillerr.IsCode(err, skillerr.CodeArg) {
		t.Fatalf("expected EARG, got %v", err)
	}
}

func TestRunCodeImportsReportsUnusedAsUnsupported(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	stdout := &bytes.Buffer{}
	rc := newTestRunnerContext(t, stdout, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	err := run(ctx, rc, input{
		Path:      work,
		QueryType: "unused",
	})
	if err == nil {
		t.Fatal("expected unused query_type to fail until implemented")
	}
	if !skillerr.IsCode(err, skillerr.CodeCapability) {
		t.Fatalf("expected ECAPABILITY, got %v", err)
	}
}

func TestGetDependenciesRespectsMaxDepth(t *testing.T) {
	fileImports := map[string][]string{
		"a.go": {"b.go"},
		"b.go": {"c.go"},
		"c.go": {"d.go"},
		"d.go": nil,
	}

	if got, want := getDependencies("a.go", fileImports, 0), []string(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("maxDepth=0 dependencies=%v want %v", got, want)
	}
	if got, want := getDependencies("a.go", fileImports, 1), []string{"b.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("maxDepth=1 dependencies=%v want %v", got, want)
	}
	if got, want := getDependencies("a.go", fileImports, 2), []string{"b.go", "c.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("maxDepth=2 dependencies=%v want %v", got, want)
	}
}

func TestGetDependentsRespectsMaxDepth(t *testing.T) {
	fileImports := map[string][]string{
		"a.go": {"b.go"},
		"b.go": {"c.go"},
		"c.go": {"d.go"},
		"d.go": nil,
	}

	if got, want := getDependents("d.go", fileImports, 0), []string(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("maxDepth=0 dependents=%v want %v", got, want)
	}
	if got, want := getDependents("d.go", fileImports, 1), []string{"c.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("maxDepth=1 dependents=%v want %v", got, want)
	}
	if got, want := getDependents("d.go", fileImports, 2), []string{"c.go", "b.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("maxDepth=2 dependents=%v want %v", got, want)
	}
}

func TestGetDependenciesGeneratedDepthBound(t *testing.T) {
	cfg := &quick.Config{MaxCount: 100}

	err := quick.Check(func(edgeCount uint8, rawDepth uint8) bool {
		edges := int(edgeCount%8) + 1
		maxDepth := int(rawDepth % 10)
		fileImports := make(map[string][]string, edges+1)
		for i := 0; i < edges; i++ {
			fileImports[chainFile(i)] = []string{chainFile(i + 1)}
		}
		fileImports[chainFile(edges)] = nil

		got := getDependencies(chainFile(0), fileImports, maxDepth)
		wantLen := maxDepth
		if wantLen > edges {
			wantLen = edges
		}
		if len(got) != wantLen {
			t.Logf("dependencies exceeded depth bound: edges=%d maxDepth=%d got=%v", edges, maxDepth, got)
			return false
		}
		for i, dep := range got {
			if dep != chainFile(i+1) {
				t.Logf("dependency order changed: edges=%d maxDepth=%d got=%v", edges, maxDepth, got)
				return false
			}
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("dependency depth property failed: %v", err)
	}
}

func chainFile(index int) string {
	return fmt.Sprintf("file%d.go", index)
}
