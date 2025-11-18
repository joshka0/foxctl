package main

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"testing"

	runner "github.com/jkatigb/agentctl/internal/adapters/skillslib/runner"
	"github.com/jkatigb/agentctl/internal/platform/config"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
)

func newTestContext(t *testing.T, stdout *bytes.Buffer, workspace string) *runner.Context {
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
	rc, err := runner.NewContext(cfg, stdout)
	if err != nil {
		t.Fatalf("runner context: %v", err)
	}
	return rc
}

func TestAnalyzeGoFile(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	code := `package main
func complex(x int) int {
	if x > 0 {
		if x > 10 {
			return 1
		}
		return 2
	}
	for i := 0; i < x; i++ {
		if i % 2 == 0 {
			continue
		}
	}
	return 0
}`
	path := filepath.Join(work, "main.go")
	if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	rc := newTestContext(t, stdout, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path:         "main.go",
		AnalysisMode: "hotspots",
		Metric:       "cyclomatic",
		Threshold:    1,
		Language:     "go",
		MaxResults:   100,
	}

	err := run(ctx, rc, in)
	if err != nil {
		t.Errorf("run failed: %v", err)
	}

	var output struct {
		Results []complexityResult `json:"results"`
	}

	// Parse envelope
	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	dataBytes, err := json.Marshal(env["data"])
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(dataBytes, &output); err != nil {
		t.Fatal(err)
	}

	if len(output.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(output.Results))
	}
	if output.Results[0].CyclomaticComplexity <= 1 {
		t.Errorf("expected complexity > 1, got %d", output.Results[0].CyclomaticComplexity)
	}
}

func TestAnalyzeDirectory(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	cwd, _ := os.Getwd()
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	if err := os.WriteFile(filepath.Join(work, "a.go"), []byte("package main\nfunc a(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "b.py"), []byte("def b():\n  pass"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	rc := newTestContext(t, stdout, work)
	defer func() { errs.Ignore(rc.Close(), "cleanup") }()

	in := input{
		Path:         ".",
		AnalysisMode: "hotspots",
		Metric:       "cyclomatic",
		Threshold:    0,
		Language:     "auto",
		MaxResults:   100,
	}

	err := run(ctx, rc, in)
	if err != nil {
		t.Errorf("run failed: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	data := env["data"].(map[string]any)
	count := data["result_count"].(float64)

	if count != 2 {
		t.Errorf("expected 2 results, got %v", count)
	}
}

func TestCalculateGoCognitiveComplexity(t *testing.T) {
	tests := []struct {
		name string
		code string
		want int
	}{
		{
			name: "simple",
			code: `package main
func f() {
	if true {
		return
	}
}`,
			want: 1,
		},
		{
			name: "nested",
			code: `package main
func f() {
	if true {
		if true {
			return
		}
	}
}`,
			want: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			fn := file.Decls[0].(*ast.FuncDecl)
			got := calculateGoCognitiveComplexity(fn)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCalculateGoNestingDepth(t *testing.T) {
	tests := []struct {
		name string
		code string
		want int
	}{
		{
			name: "flat",
			code: `package main
func f() {
	x := 1
}`,
			want: 0,
		},
		{
			name: "one level",
			code: `package main
func f() {
	if true {
		x := 1
	}
}`,
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.code, 0)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}

			fn := file.Decls[0].(*ast.FuncDecl)
			got := calculateGoNestingDepth(fn)
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}
