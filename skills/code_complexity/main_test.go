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
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/domain/policy"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/rs/zerolog"
)

func newTestRunContext(t *testing.T, stdout *bytes.Buffer, workspace string) *skillmain.RunContext {
	t.Helper()
	t.Setenv("AGENTCTL_WORKSPACE", workspace)
	state := t.TempDir()
	casPath := filepath.Join(state, "cas")
	casStore, err := cas.NewStore(casPath)
	if err != nil {
		t.Fatalf("open cas: %v", err)
	}

	pv, err := policy.NewPathValidator(workspace, nil)
	if err != nil {
		t.Fatalf("path validator: %v", err)
	}

	cfg := config.Config{
		Home:           state,
		InlineOutputKB: 32,
		MaxCaptureKB:   10240,
		Paths: config.Paths{
			CAS:   casPath,
			Jobs:  filepath.Join(state, "jobs"),
			Cache: filepath.Join(state, "cache"),
		},
	}

	return &skillmain.RunContext{
		Config:        cfg,
		CASStore:      casStore,
		Workspace:     workspace,
		Logger:        zerolog.Nop(),
		PathValidator: pv,
		Validator:     validator.New(),
		Stdout:        stdout,
		Now:           time.Now,
		InlineKB:      cfg.InlineOutputKB,
		MaxPreview:    100,
	}
}

func TestAnalyzeGoFile(t *testing.T) {
	ctx := context.Background()
	work := t.TempDir()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(cwd) //nolint:errcheck
	}()

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
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Path:         "main.go",
		AnalysisMode: "hotspots",
		Metric:       "cyclomatic",
		Threshold:    1,
		Language:     "go",
		MaxResults:   100,
	}

	err = run(ctx, rc, in)
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
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.Chdir(cwd) //nolint:errcheck
	}()

	if err := os.WriteFile(filepath.Join(work, "a.go"), []byte("package main\nfunc a(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "b.py"), []byte("def b():\n  pass"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Path:         ".",
		AnalysisMode: "hotspots",
		Metric:       "cyclomatic",
		Threshold:    1, // Use 1 since 0 defaults to 10 and simple functions have complexity=1
		Language:     "auto",
		MaxResults:   100,
	}

	err = run(ctx, rc, in)
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
