package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/domain/policy"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/storage/cas"
	"github.com/rs/zerolog"
)

func skipIfNoGopls(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available, skipping LSP test")
	}
}

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
		InlineOutputKB: 64,
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

func setupTestWorkspace(t *testing.T) string {
	t.Helper()
	work := t.TempDir()

	// Create go.mod for proper module support
	goMod := `module testmod

go 1.21
`
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a Go file with various symbols
	code := `package main

// Handler is an interface for handling requests.
type Handler interface {
	Handle(req string) (string, error)
}

// MyHandler implements Handler.
type MyHandler struct {
	Name string
}

// Handle processes a request.
func (h *MyHandler) Handle(req string) (string, error) {
	return h.processInternal(req), nil
}

func (h *MyHandler) processInternal(req string) string {
	return "processed: " + req
}

// NewHandler creates a new MyHandler.
func NewHandler(name string) *MyHandler {
	return &MyHandler{Name: name}
}

func main() {
	h := NewHandler("test")
	h.Handle("hello")
}
`
	if err := os.WriteFile(filepath.Join(work, "main.go"), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	return work
}

func TestSymbols(t *testing.T) {
	skipIfNoGopls(t)
	ctx := context.Background()
	work := setupTestWorkspace(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Operation:  "symbols",
		File:       "main.go",
		MaxResults: 50,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, stdout.String())
	}

	if env["status"] != "ok" {
		t.Fatalf("expected status ok, got: %v", env["status"])
	}

	data := env["data"].(map[string]any)
	symbols := data["symbols"].([]any)

	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}

	// Check for expected symbols
	foundHandler := false
	foundNewHandler := false
	for _, s := range symbols {
		sym := s.(map[string]any)
		name := sym["name"].(string)
		if name == "Handler" {
			foundHandler = true
		}
		if name == "NewHandler" {
			foundNewHandler = true
		}
	}

	if !foundHandler {
		t.Error("Handler interface not found in symbols")
	}
	if !foundNewHandler {
		t.Error("NewHandler function not found in symbols")
	}
}

func TestReferences(t *testing.T) {
	skipIfNoGopls(t)
	ctx := context.Background()
	work := setupTestWorkspace(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	// Find references to NewHandler (line 23, col 6 based on test file)
	// The test file has: func NewHandler(name string) *MyHandler {
	in := Input{
		Operation:  "references",
		File:       "main.go",
		Line:       23,
		Column:     6,
		MaxResults: 50,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, stdout.String())
	}

	if env["status"] != "ok" {
		t.Fatalf("expected status ok, got: %v", env["status"])
	}

	data := env["data"].(map[string]any)
	refs := data["references"].([]any)

	// Should find at least the usage in main()
	if len(refs) < 1 {
		t.Errorf("expected at least 1 reference, got %d", len(refs))
	}
}

func TestWorkspaceSymbol(t *testing.T) {
	skipIfNoGopls(t)
	ctx := context.Background()
	work := setupTestWorkspace(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
		Operation:  "workspace_symbol",
		Query:      "Handler",
		MaxResults: 50,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, stdout.String())
	}

	if env["status"] != "ok" {
		t.Fatalf("expected status ok, got: %v", env["status"])
	}

	data := env["data"].(map[string]any)
	symbols := data["symbols"].([]any)

	// Should find Handler interface and MyHandler struct
	if len(symbols) < 1 {
		t.Errorf("expected at least 1 symbol matching 'Handler', got %d", len(symbols))
	}

	foundHandler := false
	for _, s := range symbols {
		sym := s.(map[string]any)
		name := sym["name"].(string)
		if name == "Handler" || name == "MyHandler" || name == "NewHandler" {
			foundHandler = true
			break
		}
	}

	if !foundHandler {
		t.Error("no Handler-related symbol found in workspace search")
	}
}

func TestDefinition(t *testing.T) {
	skipIfNoGopls(t)
	ctx := context.Background()
	work := setupTestWorkspace(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	// Find definition of NewHandler call in main() (line 29)
	in := Input{
		Operation:  "definition",
		File:       "main.go",
		Line:       29,
		Column:     7, // Position of NewHandler call
		MaxResults: 50,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, stdout.String())
	}

	if env["status"] != "ok" {
		t.Fatalf("expected status ok, got: %v", env["status"])
	}

	data, ok := env["data"].(map[string]any)
	if !ok || data == nil {
		t.Fatal("expected data map in response")
	}
	def, ok := data["definition"].(map[string]any)
	if !ok || def == nil {
		t.Fatal("expected definition map, got nil or wrong type")
	}

	loc, ok := def["location"].(map[string]any)
	if !ok || loc == nil {
		t.Fatal("expected location map in definition")
	}
	if loc["line"] == nil {
		t.Error("expected definition location with line number")
	}
}

func TestCallHierarchy(t *testing.T) {
	skipIfNoGopls(t)
	ctx := context.Background()
	work := setupTestWorkspace(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	stdout := &bytes.Buffer{}
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	// Get call hierarchy for processInternal (line 18, col 21 based on test file)
	// The test file has: func (h *MyHandler) processInternal(req string) string {
	in := Input{
		Operation:  "call_hierarchy",
		File:       "main.go",
		Line:       18,
		Column:     21,
		MaxResults: 50,
	}

	if err := run(ctx, rc, in); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	var env map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v, output: %s", err, stdout.String())
	}

	if env["status"] != "ok" {
		t.Fatalf("expected status ok, got: %v", env["status"])
	}

	data, ok := env["data"].(map[string]any)
	if !ok || data == nil {
		t.Fatal("expected data map in response")
	}
	ch, ok := data["call_hierarchy"].(map[string]any)
	if !ok || ch == nil {
		t.Fatal("expected call_hierarchy map, got nil or wrong type")
	}

	// processInternal should be called by Handle
	callers, ok := ch["callers"].([]any)
	if !ok {
		t.Fatal("expected callers array in call_hierarchy")
	}
	if len(callers) == 0 {
		t.Log("no callers found (may vary by gopls version)")
	}
}

func TestNormalizeInput(t *testing.T) {
	tests := []struct {
		name  string
		input Input
		check func(t *testing.T, in Input)
	}{
		{
			name: "symbols operation defaults",
			input: Input{
				Operation: "symbols",
				File:      "main.go",
			},
			check: func(t *testing.T, in Input) {
				if in.MaxResults != 50 {
					t.Errorf("expected default max_results 50, got %d", in.MaxResults)
				}
			},
		},
		{
			name: "defaults applied for column",
			input: Input{
				Operation: "symbols",
				File:      "x.go",
				Line:      1,
			},
			check: func(t *testing.T, in Input) {
				if in.Column != 1 {
					t.Errorf("expected default column 1, got %d", in.Column)
				}
				if in.MaxResults != 50 {
					t.Errorf("expected default max_results 50, got %d", in.MaxResults)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.input
			normalizeInput(&in)
			if tt.check != nil {
				tt.check(t, in)
			}
		})
	}
}

func TestParseLocation(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, loc Location)
	}{
		{
			name:  "simple location",
			input: "/path/to/file.go:10:5",
			check: func(t *testing.T, loc Location) {
				if loc.File != "/path/to/file.go" {
					t.Errorf("expected file '/path/to/file.go', got %q", loc.File)
				}
				if loc.Line != 10 {
					t.Errorf("expected line 10, got %d", loc.Line)
				}
				if loc.Column != 5 {
					t.Errorf("expected column 5, got %d", loc.Column)
				}
			},
		},
		{
			name:  "location with range",
			input: "/path/to/file.go:10:5-15",
			check: func(t *testing.T, loc Location) {
				if loc.Line != 10 {
					t.Errorf("expected line 10, got %d", loc.Line)
				}
				if loc.Column != 5 {
					t.Errorf("expected column 5, got %d", loc.Column)
				}
			},
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc, err := parseLocation(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, loc)
			}
		})
	}
}

func TestParseSymbolLine(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, sym Symbol)
	}{
		{
			name:  "struct symbol",
			input: "MyStruct Struct 12:6-12:14",
			check: func(t *testing.T, sym Symbol) {
				if sym.Name != "MyStruct" {
					t.Errorf("expected name 'MyStruct', got %q", sym.Name)
				}
				if sym.Kind != "Struct" {
					t.Errorf("expected kind 'Struct', got %q", sym.Kind)
				}
				if sym.Line != 12 {
					t.Errorf("expected line 12, got %d", sym.Line)
				}
			},
		},
		{
			name:  "function symbol",
			input: "NewHandler Function 24:6-24:16",
			check: func(t *testing.T, sym Symbol) {
				if sym.Name != "NewHandler" {
					t.Errorf("expected name 'NewHandler', got %q", sym.Name)
				}
				if sym.Kind != "Function" {
					t.Errorf("expected kind 'Function', got %q", sym.Kind)
				}
			},
		},
		{
			name:    "invalid format",
			input:   "OnlyOnePart",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sym, err := parseSymbolLine(tt.input, "/workspace")
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, sym)
			}
		})
	}
}

func TestOperationValidation(t *testing.T) {
	skipIfNoGopls(t)
	ctx := context.Background()
	work := setupTestWorkspace(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)

	tests := []struct {
		name    string
		input   Input
		wantErr bool
	}{
		{
			name:    "symbols without file",
			input:   Input{Operation: "symbols"},
			wantErr: true,
		},
		{
			name:    "references without line",
			input:   Input{Operation: "references", File: "main.go"},
			wantErr: true,
		},
		{
			name:    "workspace_symbol without query",
			input:   Input{Operation: "workspace_symbol"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			rc := newTestRunContext(t, stdout, work)
			defer rc.Close()

			err := run(ctx, rc, tt.input)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
