package symbol

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// elixirSymbolSnapshot includes byte offsets for thorough validation.
type elixirSymbolSnapshot struct {
	ID         string `json:"id"`
	FilePath   string `json:"file_path"`
	Name       string `json:"name"`
	Language   string `json:"language"`
	Kind       Kind   `json:"kind"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
	StartByte  int    `json:"start_byte"`
	EndByte    int    `json:"end_byte"`
	BodyDigest string `json:"body_digest"`
}

func snapshotElixirSymbols(syms []Symbol) []elixirSymbolSnapshot {
	snapshots := make([]elixirSymbolSnapshot, 0, len(syms))
	for _, sym := range syms {
		snapshots = append(snapshots, elixirSymbolSnapshot{
			ID:         sym.ID,
			FilePath:   sym.FilePath,
			Name:       sym.Name,
			Language:   sym.Language,
			Kind:       sym.Kind,
			StartLine:  sym.StartLine,
			EndLine:    sym.EndLine,
			StartByte:  sym.StartByte,
			EndByte:    sym.EndByte,
			BodyDigest: sym.BodyDigest,
		})
	}
	return snapshots
}

func TestElixirExtractorExtract(t *testing.T) {
	source := `defmodule MyApp.Users do
  @moduledoc "User management"

  @type user_id :: integer()
  @typep internal_state :: map()

  @callback on_create(user_id()) :: :ok | {:error, term()}

  def create(name, email) do
    # Create user
    {:ok, %{name: name, email: email}}
  end

  defp validate(data) do
    is_map(data)
  end

  defmacro debug(msg) do
    quote do
      IO.puts("[DEBUG] " <> unquote(msg))
    end
  end

  defmacrop internal_debug(msg), do: IO.inspect(msg)
end
`

	extractor := NewElixirExtractor()
	syms, err := extractor.Extract(context.Background(), "lib/my_app/users.ex", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	expected := map[string]Kind{
		"MyApp.Users":    KindClass,
		"user_id":        KindType,
		"internal_state": KindType,
		"on_create":      KindInterface,
		"create":         KindFunction,
		"validate":       KindFunction,
		"debug":          KindFunction,
		"internal_debug": KindFunction,
	}

	if len(syms) != len(expected) {
		t.Errorf("expected %d symbols, got %d", len(expected), len(syms))
		for _, sym := range syms {
			t.Logf("  found: %s (%s)", sym.Name, sym.Kind)
		}
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
		if sym.Language != "elixir" {
			t.Errorf("symbol %s language: expected elixir, got %s", sym.Name, sym.Language)
		}
		if sym.StartLine == 0 || sym.EndLine == 0 {
			t.Errorf("symbol %s missing line info", sym.Name)
		}
		if sym.StartByte > sym.EndByte {
			t.Errorf("symbol %s: start_byte (%d) > end_byte (%d)", sym.Name, sym.StartByte, sym.EndByte)
		}
		if sym.BodyDigest == "" {
			t.Errorf("symbol %s missing body digest", sym.Name)
		}
	}
	for name := range expected {
		if _, ok := seen[name]; !ok {
			t.Errorf("missing symbol: %s", name)
		}
	}

	// Snapshot comparison
	snapshots := snapshotElixirSymbols(syms)
	expectedPath := filepath.Join("testdata", "elixir_symbols.json")
	data, err := os.ReadFile(expectedPath)
	if err != nil {
		t.Logf("golden file not found, creating: %s", expectedPath)
		out, _ := json.MarshalIndent(snapshots, "", "  ")
		if writeErr := os.WriteFile(expectedPath, out, 0o644); writeErr != nil {
			t.Fatalf("write golden file: %v", writeErr)
		}
		return
	}
	var expectedSnapshots []elixirSymbolSnapshot
	if err := json.Unmarshal(data, &expectedSnapshots); err != nil {
		t.Fatalf("unmarshal golden file: %v", err)
	}
	if !reflect.DeepEqual(expectedSnapshots, snapshots) {
		expectedJSON, _ := json.MarshalIndent(expectedSnapshots, "", "  ")
		actualJSON, _ := json.MarshalIndent(snapshots, "", "  ")
		t.Errorf("symbol snapshots mismatch\nexpected:\n%s\nactual:\n%s", expectedJSON, actualJSON)
	}
}

func TestElixirExtractorTableDriven(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected []struct {
			name      string
			kind      Kind
			startLine int
			endLine   int
		}
	}{
		{
			name: "module definition",
			source: `defmodule Foo do
end`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"Foo", KindClass, 1, 2},
			},
		},
		{
			name: "public function multi-line",
			source: `def greet(name) do
  "Hello, #{name}"
end`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"greet", KindFunction, 1, 3},
			},
		},
		{
			name:   "private function single-line do:",
			source: `defp add(a, b), do: a + b`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"add", KindFunction, 1, 1},
			},
		},
		{
			name: "macro definition",
			source: `defmacro unless(condition) do
  quote do
    if !unquote(condition), do: nil
  end
end`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"unless", KindFunction, 1, 5},
			},
		},
		{
			name: "private macro",
			source: `defmacrop debug(msg) do
  quote do
    IO.inspect(unquote(msg))
  end
end`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"debug", KindFunction, 1, 5},
			},
		},
		{
			name:   "type definition",
			source: `@type name :: String.t()`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"name", KindType, 1, 1},
			},
		},
		{
			name:   "private type definition",
			source: `@typep state :: map()`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"state", KindType, 1, 1},
			},
		},
		{
			name:   "callback definition",
			source: `@callback handle_call(term(), pid(), state()) :: {:reply, term(), state()}`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"handle_call", KindInterface, 1, 1},
			},
		},
		{
			name: "nested blocks",
			source: `def process(items) do
  Enum.map(items, fn item ->
    case item do
      :ok -> handle_ok()
      :error -> handle_error()
    end
  end)
end`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"process", KindFunction, 1, 8},
			},
		},
		{
			name: "function with guards",
			source: `def valid?(x) when is_integer(x) and x > 0 do
  true
end`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"valid?", KindFunction, 1, 3},
			},
		},
		{
			name:   "function with bang",
			source: `def fetch!(key), do: Map.fetch!(data, key)`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"fetch!", KindFunction, 1, 1},
			},
		},
		{
			name: "empty module",
			source: `defmodule Empty do
end`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"Empty", KindClass, 1, 2},
			},
		},
		{
			name: "comment and blank lines",
			source: `# This is a comment
defmodule Commented do
  # Another comment
  def foo do
    :bar
  end
end`,
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"Commented", KindClass, 2, 7},
				{"foo", KindFunction, 4, 6},
			},
		},
		{
			name:   "function head without body",
			source: `def broken(x)`,
			// Elixir allows multi-clause functions, so a head without do is valid
			expected: []struct {
				name      string
				kind      Kind
				startLine int
				endLine   int
			}{
				{"broken", KindFunction, 1, 1},
			},
		},
		{
			name:     "empty source",
			source:   ``,
			expected: nil,
		},
		{
			name:     "only comments",
			source:   `# Just a comment`,
			expected: nil,
		},
	}

	extractor := NewElixirExtractor()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			syms, err := extractor.Extract(context.Background(), "test.ex", []byte(tc.source))
			if err != nil {
				t.Fatalf("extract: %v", err)
			}

			if len(syms) != len(tc.expected) {
				t.Errorf("expected %d symbols, got %d", len(tc.expected), len(syms))
				for _, sym := range syms {
					t.Logf("  found: %s (%s) lines %d-%d", sym.Name, sym.Kind, sym.StartLine, sym.EndLine)
				}
				return
			}

			for i, exp := range tc.expected {
				sym := syms[i]
				if sym.Name != exp.name {
					t.Errorf("symbol %d name: expected %s, got %s", i, exp.name, sym.Name)
				}
				if sym.Kind != exp.kind {
					t.Errorf("symbol %s kind: expected %s, got %s", sym.Name, exp.kind, sym.Kind)
				}
				if sym.StartLine != exp.startLine {
					t.Errorf("symbol %s start_line: expected %d, got %d", sym.Name, exp.startLine, sym.StartLine)
				}
				if sym.EndLine != exp.endLine {
					t.Errorf("symbol %s end_line: expected %d, got %d", sym.Name, exp.endLine, sym.EndLine)
				}
				if sym.Language != "elixir" {
					t.Errorf("symbol %s language: expected elixir, got %s", sym.Name, sym.Language)
				}
				// Verify byte offsets are valid
				if sym.StartByte > sym.EndByte {
					t.Errorf("symbol %s: start_byte (%d) > end_byte (%d)", sym.Name, sym.StartByte, sym.EndByte)
				}
				// Verify body digest is stable
				if sym.BodyDigest == "" {
					t.Errorf("symbol %s: missing body digest", sym.Name)
				}
			}
		})
	}
}

func TestElixirExtractorBodyDigestStability(t *testing.T) {
	source := `def stable_func do
  :ok
end`

	extractor := NewElixirExtractor()

	// Extract twice
	syms1, _ := extractor.Extract(context.Background(), "test.ex", []byte(source))
	syms2, _ := extractor.Extract(context.Background(), "test.ex", []byte(source))

	if len(syms1) != 1 || len(syms2) != 1 {
		t.Fatal("expected 1 symbol each extraction")
	}

	if syms1[0].BodyDigest != syms2[0].BodyDigest {
		t.Errorf("body digest not stable: %s vs %s", syms1[0].BodyDigest, syms2[0].BodyDigest)
	}
}

func TestElixirExtractorByteOffsets(t *testing.T) {
	source := `defmodule Test do
  def hello do
    :world
  end
end`

	extractor := NewElixirExtractor()
	syms, err := extractor.Extract(context.Background(), "test.ex", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	if len(syms) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(syms))
	}

	// Module should span from start to end
	mod := syms[0]
	if mod.Name != "Test" {
		t.Errorf("expected module Test, got %s", mod.Name)
	}
	if mod.StartByte != 0 {
		t.Errorf("module start_byte: expected 0, got %d", mod.StartByte)
	}
	body := source[mod.StartByte:mod.EndByte]
	if body[0:9] != "defmodule" {
		t.Errorf("module body should start with 'defmodule', got: %s", body[:min(20, len(body))])
	}

	// Function should have correct offsets within module
	fn := syms[1]
	if fn.Name != "hello" {
		t.Errorf("expected function hello, got %s", fn.Name)
	}
	fnBody := source[fn.StartByte:fn.EndByte]
	if len(fnBody) < 3 || fnBody[0:3] != "  d" {
		t.Errorf("function body unexpected start: %s", fnBody[:min(10, len(fnBody))])
	}
}

func TestElixirExtractorDocs(t *testing.T) {
	source := `defmodule MyApp.Users do
  @moduledoc "User management"

  @doc "Creates a user."
  def create(), do: :ok

  @doc false
  def hidden(), do: :ok

  @typedoc "User id"
  @type user_id :: integer()
end`

	extractor := NewElixirExtractor()
	syms, err := extractor.Extract(context.Background(), "test.ex", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	docs := map[string]string{}
	for _, sym := range syms {
		docs[sym.Name] = sym.Documentation
	}

	if docs["MyApp.Users"] != "User management" {
		t.Errorf("unexpected doc for module: %q", docs["MyApp.Users"])
	}
	if docs["create"] != "Creates a user." {
		t.Errorf("unexpected doc for create: %q", docs["create"])
	}
	if docs["hidden"] != "" {
		t.Errorf("expected empty doc for hidden, got %q", docs["hidden"])
	}
	if docs["user_id"] != "User id" {
		t.Errorf("unexpected doc for user_id: %q", docs["user_id"])
	}
}

func TestElixirExtractorSupportedLanguages(t *testing.T) {
	extractor := NewElixirExtractor()
	langs := extractor.SupportedLanguages()
	if len(langs) != 1 || langs[0] != "elixir" {
		t.Errorf("expected [elixir], got %v", langs)
	}
}

func TestElixirExtractorExtractCalls(t *testing.T) {
	extractor := NewElixirExtractor()
	calls, err := extractor.ExtractCalls(context.Background(), Symbol{}, nil)
	if err != nil {
		t.Errorf("ExtractCalls should not error: %v", err)
	}
	if calls != nil {
		t.Errorf("ExtractCalls should return nil, got %v", calls)
	}
}

func TestElixirExtractorExtractCalls_ModuleRefs(t *testing.T) {
	source := `defmodule MyApp.A do
  alias MyApp.B

  def foo do
    MyApp.B.bar()
    %MyApp.C{}
  end
end

defmodule MyApp.B do
  def bar, do: :ok
end
`

	extractor := NewElixirExtractor()
	syms, err := extractor.Extract(context.Background(), "lib/app.ex", []byte(source))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}

	var modA Symbol
	for _, sym := range syms {
		if sym.Name == "MyApp.A" {
			modA = sym
			break
		}
	}
	if modA.Name == "" {
		t.Fatalf("missing MyApp.A module symbol")
	}

	calls, err := extractor.ExtractCalls(context.Background(), modA, []byte(source))
	if err != nil {
		t.Fatalf("extract calls: %v", err)
	}

	want := map[string]bool{"MyApp.B": true, "MyApp.C": true}
	for name := range want {
		found := false
		for _, call := range calls {
			if call == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected module ref %q in %v", name, calls)
		}
	}
}
