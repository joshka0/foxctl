package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

func newTestRunnerContext(t *testing.T, stdout *bytes.Buffer, workspace string) *runner.RunnerContext {
	t.Helper()
	t.Setenv("AGENTCTL_WORKSPACE", workspace)
	cfg := config.Config{
		Home:           workspace,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   workspace + "/cas",
			Jobs:  workspace + "/jobs",
			Cache: workspace + "/cache",
		},
	}
	rc, err := runner.NewRunnerContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func TestRunCodeSymbols(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	code := `package main
type MyStruct struct {
	Field1 string
}
func MyFunc() {}
`
	if err := os.WriteFile(work+"/main.go", []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	rc := newTestRunnerContext(t, stdout, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path:       "main.go",
		Language:   "go",
		SymbolType: "all",
		MaxResults: 100,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Errorf("run failed: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}

	data := env["data"].(map[string]any)
	if data["preview"] == nil {
		t.Fatalf("preview is nil")
	}

	preview := data["preview"].([]any)

	foundStruct := false
	foundFunc := false

	for _, item := range preview {
		m := item.(map[string]any)
		if m["name"] == "MyStruct" && m["type"] == "struct" {
			foundStruct = true
		}
		if m["name"] == "MyFunc" && m["type"] == "function" {
			foundFunc = true
		}
	}

	if !foundStruct {
		t.Errorf("MyStruct not found. Got: %v", preview)
	}

	if !foundFunc {
		t.Errorf("MyFunc not found. Got: %v", preview)
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
	defer func() { _ = os.Chdir(cwd) }()

	code := `package main
type MyStruct struct {
	Field1 string
}
func MyFunc() {}
`
	path := work + "/main.go"
	if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	foundStruct := false
	foundFunc := false

	for _, decl := range f.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok {
			for _, spec := range gen.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok {
					if ts.Name.Name == "MyStruct" {
						foundStruct = true
					}
				}
			}
		}
		if fn, ok := decl.(*ast.FuncDecl); ok {
			if fn.Name.Name == "MyFunc" {
				foundFunc = true
			}
		}
	}

	if !foundStruct {
		t.Error("Parser failed to find MyStruct")
	}
	if !foundFunc {
		t.Error("Parser failed to find MyFunc")
	}
}
