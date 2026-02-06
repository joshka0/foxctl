package symbol

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

type symbolSnapshot struct {
	ID        string `json:"id"`
	FilePath  string `json:"file_path"`
	Name      string `json:"name"`
	Language  string `json:"language"`
	Kind      Kind   `json:"kind"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func snapshotSymbols(syms []Symbol) []symbolSnapshot {
	snapshots := make([]symbolSnapshot, 0, len(syms))
	for _, sym := range syms {
		snapshots = append(snapshots, symbolSnapshot{
			ID:        sym.ID,
			FilePath:  sym.FilePath,
			Name:      sym.Name,
			Language:  sym.Language,
			Kind:      sym.Kind,
			StartLine: sym.StartLine,
			EndLine:   sym.EndLine,
		})
	}
	sortSnapshots(snapshots)
	return snapshots
}

func sortSnapshots(snapshots []symbolSnapshot) {
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].Name == snapshots[j].Name {
			return snapshots[i].ID < snapshots[j].ID
		}
		return snapshots[i].Name < snapshots[j].Name
	})
}

func TestTypeScriptExtractorExtract(t *testing.T) {
	source := `export function foo() {
  return 1
}

export default function() {
  return 0
}

class Bar {}
interface Baz { }
type Alias = string
enum Mode { A }
const VALUE = 1
let count = 0
var flag = true
`

	extractor := NewTypeScriptExtractor()
	syms, err := extractor.Extract(context.Background(), "src/example.ts", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	expected := map[string]Kind{
		"foo":     KindFunction,
		"default": KindFunction,
		"Bar":     KindClass,
		"Baz":     KindInterface,
		"Alias":   KindType,
		"Mode":    KindType,
		"VALUE":   KindConstant,
		"count":   KindVariable,
		"flag":    KindVariable,
	}

	if len(syms) != len(expected) {
		t.Errorf("expected %d symbols, got %d", len(expected), len(syms))
	}

	seen := make(map[string]struct{}, len(syms))
	for _, sym := range syms {
		seen[sym.Name] = struct{}{}
		kind, ok := expected[sym.Name]
		if !ok {
			t.Errorf("unexpected symbol: %s", sym.Name)
			continue
		}
		if sym.Kind != kind {
			t.Errorf("symbol %s kind: expected %s, got %s", sym.Name, kind, sym.Kind)
		}
		if sym.Language != "typescript" {
			t.Errorf("symbol %s language: expected typescript, got %s", sym.Name, sym.Language)
		}
		if sym.StartLine == 0 || sym.EndLine == 0 {
			t.Errorf("symbol %s missing line info", sym.Name)
		}
	}
	for name := range expected {
		if _, ok := seen[name]; !ok {
			t.Errorf("missing symbol: %s", name)
		}
	}

	snapshots := snapshotSymbols(syms)
	expectedPath := filepath.Join("testdata", "typescript_symbols.json")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}
	var expectedSnapshots []symbolSnapshot
	if err := json.Unmarshal(data, &expectedSnapshots); err != nil {
		t.Fatalf("unmarshal golden file: %v", err)
	}
	sortSnapshots(expectedSnapshots)
	if !reflect.DeepEqual(expectedSnapshots, snapshots) {
		expectedJSON, _ := json.MarshalIndent(expectedSnapshots, "", "  ")
		actualJSON, _ := json.MarshalIndent(snapshots, "", "  ")
		t.Errorf("symbol snapshots mismatch\nexpected: %s\nactual: %s", expectedJSON, actualJSON)
	}
}

func TestTypeScriptExtractorDocs(t *testing.T) {
	source := "// Adds numbers\n// and returns the sum.\nexport function add(a: number, b: number) {\n  return a + b\n}\n\n/** Greets a user. */\nexport class Greeter {}\n"

	extractor := NewTypeScriptExtractor()
	syms, err := extractor.Extract(context.Background(), "src/docs.ts", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	docs := map[string]string{}
	for _, sym := range syms {
		docs[sym.Name] = sym.Documentation
	}

	if docs["add"] != "Adds numbers\nand returns the sum." {
		t.Errorf("unexpected doc for add: %q", docs["add"])
	}
	if docs["Greeter"] != "Greets a user." {
		t.Errorf("unexpected doc for Greeter: %q", docs["Greeter"])
	}
}

func TestTypeScriptExtractorExtractCalls(t *testing.T) {
	source := `export function bar() { return 1 }
export class Zoo {}
export function foo(
  x: number,
): number {
  bar()
  baz.qux()
  new Zoo()
  function inner() { zap() }
  inner()
  if (true) { return 0 }
  return x
}`

	extractor := NewTypeScriptExtractor()
	syms, err := extractor.Extract(context.Background(), "src/calls.ts", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var foo Symbol
	for _, sym := range syms {
		if sym.Name == "foo" {
			foo = sym
			break
		}
	}
	if foo.Name == "" {
		t.Fatalf("missing foo symbol")
	}

	calls, err := extractor.ExtractCalls(context.Background(), foo, []byte(source))
	if err != nil {
		t.Fatalf("extract calls: %v", err)
	}

	// Best-effort heuristics: ensure we capture the obvious ones and ignore keywords.
	want := map[string]bool{
		"bar":   true,
		"Zoo":   true,
		"qux":   true,
		"inner": true,
		"zap":   true,
	}
	for name := range want {
		found := false
		for _, call := range calls {
			if call == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected call %q in %v", name, calls)
		}
	}
	for _, call := range calls {
		if call == "if" || call == "function" || call == "return" {
			t.Errorf("unexpected keyword in calls: %q (calls=%v)", call, calls)
		}
	}
}
