package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
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
	t.Setenv("FOXCTL_WORKSPACE", workspace)
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

func TestInputAcceptsLangAlias(t *testing.T) {
	decoder := json.NewDecoder(strings.NewReader(`{"path":"main.go","lang":"go"}`))
	decoder.DisallowUnknownFields()

	var in Input
	if err := decoder.Decode(&in); err != nil {
		t.Fatalf("decode input: %v", err)
	}

	got := normalizeInput(in)
	if got.Language != "go" {
		t.Fatalf("language=%q want go", got.Language)
	}
	if got.SymbolType != "all" {
		t.Fatalf("symbol_type=%q want all", got.SymbolType)
	}
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
	defer func() {
		_ = os.Chdir(cwd) //nolint:errcheck
	}()

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
	rc := newTestRunContext(t, stdout, work)
	defer rc.Close()

	in := Input{
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
	defer func() {
		_ = os.Chdir(cwd) //nolint:errcheck
	}()

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

func TestExprToStringRendersModernGoTypes(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{name: "generic instantiation", expr: "Box[T]", want: "Box[T]"},
		{name: "generic instantiation with multiple args", expr: "Pair[K, V]", want: "Pair[K, V]"},
		{name: "qualified generic instantiation", expr: "pkg.Option[T]", want: "pkg.Option[T]"},
		{name: "map of generic values", expr: "map[string]Box[T]", want: "map[string]Box[T]"},
		{name: "fixed array of generic values", expr: "[3]Box[T]", want: "[3]Box[T]"},
		{name: "parenthesized channel", expr: "chan (Box[T])", want: "chan Box[T]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exprToString(parseGoExpr(t, tt.expr))
			if got != tt.want {
				t.Fatalf("exprToString(%q)=%q want %q", tt.expr, got, tt.want)
			}
			if strings.Contains(got, "*ast.") {
				t.Fatalf("exprToString(%q) leaked AST type name: %q", tt.expr, got)
			}
		})
	}
}

func TestExprToStringGeneratedGenericInstantiationRoundTrip(t *testing.T) {
	cfg := &quick.Config{MaxCount: 100}

	err := quick.Check(func(rawType uint8, rawArg uint8) bool {
		expr := fmt.Sprintf("Type%d[Arg%d]", rawType%20, rawArg%20)
		got := exprToString(parseGoExpr(t, expr))
		if got != expr {
			t.Logf("generic type should render source-like: expr=%q got=%q", expr, got)
			return false
		}
		return true
	}, cfg)
	if err != nil {
		t.Fatalf("generic instantiation property failed: %v", err)
	}
}

func TestExtractGoSymbolsRendersGenericSignatures(t *testing.T) {
	work := t.TempDir()
	path := filepath.Join(work, "generic.go")
	code := `package main

type Box[T any] struct {
	Items [2]T
	Options map[string]Option[T]
}

type Option[T any] struct {
	Value T
}

func Convert[T any](input []T, lookup map[string]Box[T]) (Option[T], error) {
	return Option[T]{}, nil
}

func (b *Box[T]) First() T {
	return b.Items[0]
}
`
	if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	syms, err := extractGoSymbols(path, work, Input{IncludePrivate: true})
	if err != nil {
		t.Fatalf("extractGoSymbols: %v", err)
	}

	box := findSymbol(t, syms, "Box")
	if !containsString(box.Fields, "Items:[2]T") {
		t.Fatalf("Box fields=%v want Items:[2]T", box.Fields)
	}
	if !containsString(box.Fields, "Options:map[string]Option[T]") {
		t.Fatalf("Box fields=%v want Options:map[string]Option[T]", box.Fields)
	}

	convert := findSymbol(t, syms, "Convert")
	if strings.Contains(convert.Signature, "*ast.") {
		t.Fatalf("Convert signature leaked AST type name: %q", convert.Signature)
	}
	if !containsString(convert.Parameters, "lookup:map[string]Box[T]") {
		t.Fatalf("Convert parameters=%v want lookup:map[string]Box[T]", convert.Parameters)
	}
	if !containsString(convert.Returns, "Option[T]") {
		t.Fatalf("Convert returns=%v want Option[T]", convert.Returns)
	}

	first := findSymbol(t, syms, "First")
	if first.Receiver != "*Box[T]" {
		t.Fatalf("First receiver=%q want *Box[T]", first.Receiver)
	}
}

func TestExtractElixirSymbols(t *testing.T) {
	work := t.TempDir()
	path := filepath.Join(work, "sample.ex")
	code := `defmodule MyApp.Users do
  @doc "Creates a user."
  def create(name), do: {:ok, name}

  defp hidden(), do: :ok
end
`
	if err := os.WriteFile(path, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}

	syms, err := extractElixirSymbols(path, work, Input{IncludeDocs: true})
	if err != nil {
		t.Fatalf("extractElixirSymbols failed: %v", err)
	}

	foundModule := false
	foundCreate := false
	foundHidden := false
	for _, sym := range syms {
		switch sym.Name {
		case "MyApp.Users":
			foundModule = sym.Type == "type" && sym.Exported
		case "create":
			foundCreate = sym.Type == "function" && sym.Exported && sym.Doc == "Creates a user."
		case "hidden":
			foundHidden = !sym.Exported
		}
	}
	if !foundModule {
		t.Fatal("expected MyApp.Users module symbol")
	}
	if !foundCreate {
		t.Fatal("expected public create/1 symbol with docs")
	}
	if !foundHidden {
		t.Fatal("expected private hidden/0 symbol")
	}
}

func parseGoExpr(t *testing.T, src string) ast.Expr {
	t.Helper()
	expr, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("parse expr %q: %v", src, err)
	}
	return expr
}

func findSymbol(t *testing.T, symbols []symbol, name string) symbol {
	t.Helper()
	for _, sym := range symbols {
		if sym.Name == name {
			return sym
		}
	}
	t.Fatalf("symbol %q not found in %+v", name, symbols)
	return symbol{}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
