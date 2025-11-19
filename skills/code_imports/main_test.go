package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, workspace string) *runner.RunnerContext {
	t.Helper()
	t.Setenv("AGENTCTL_WORKSPACE", workspace)
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
	rc, err := runner.NewRunnerContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func TestRunCodeImports(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

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
	cwd, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

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
	cwd, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

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
