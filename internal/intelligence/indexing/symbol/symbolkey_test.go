package symbol

import "testing"

func TestGoSymbolKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected SymbolKey
	}{
		{"simple function", "main", SymbolKey("main")},
		{"method", "Builder.Build", SymbolKey("Builder.Build")},
		{"nested method", "Builder.addGoReferenceEdges", SymbolKey("Builder.addGoReferenceEdges")},
		{"trims whitespace", "  Foo  ", SymbolKey("Foo")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GoSymbolKey(tt.input)
			if got != tt.expected {
				t.Errorf("GoSymbolKey(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestGoInitSymbolKey(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		expected SymbolKey
	}{
		{"simple", "store.go", SymbolKey("init@store.go")},
		{"complex name", "my_module.go", SymbolKey("init@my_module.go")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GoInitSymbolKey(tt.filename)
			if got != tt.expected {
				t.Errorf("GoInitSymbolKey(%q) = %q, want %q", tt.filename, got, tt.expected)
			}
		})
	}
}

func TestGoNonExportedSymbolKey(t *testing.T) {
	tests := []struct {
		name         string
		symName      string
		fileBasename string
		expected     SymbolKey
	}{
		{"private function", "helper", "store.go", SymbolKey("store.go/helper")},
		{"private method", "Store.load", "store.go", SymbolKey("store.go/Store.load")},
		{"missing file keeps name", "helper", "", SymbolKey("helper")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GoNonExportedSymbolKey(tt.symName, tt.fileBasename); got != tt.expected {
				t.Errorf("GoNonExportedSymbolKey(%q, %q) = %q, want %q", tt.symName, tt.fileBasename, got, tt.expected)
			}
		})
	}
}

func TestTSSymbolKey(t *testing.T) {
	tests := []struct {
		name         string
		symName      string
		exported     bool
		fileBasename string
		expected     SymbolKey
	}{
		{"exported", "ConversationsList", true, "ConversationsList.tsx", SymbolKey("ConversationsList")},
		{"non-exported", "helperFunc", false, "utils.tsx", SymbolKey("utils.tsx/helperFunc")},
		{"exported ignores filename", "App", true, "main.tsx", SymbolKey("App")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TSSymbolKey(tt.symName, tt.exported, tt.fileBasename)
			if got != tt.expected {
				t.Errorf("TSSymbolKey(%q, %v, %q) = %q, want %q", tt.symName, tt.exported, tt.fileBasename, got, tt.expected)
			}
		})
	}
}

func TestElixirSymbolKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected SymbolKey
	}{
		{"module function", "MyApp.Server.handle_call", SymbolKey("MyApp.Server.handle_call")},
		{"simple", "start_link", SymbolKey("start_link")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ElixirSymbolKey(tt.input)
			if got != tt.expected {
				t.Errorf("ElixirSymbolKey(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestPythonSymbolKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected SymbolKey
	}{
		{"function", "run", SymbolKey("run")},
		{"method", "Runner.run", SymbolKey("Runner.run")},
		{"trims", "  load  ", SymbolKey("load")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PythonSymbolKey(tt.input); got != tt.expected {
				t.Errorf("PythonSymbolKey(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestRustSymbolKey(t *testing.T) {
	tests := []struct {
		name         string
		symName      string
		exported     bool
		fileBasename string
		expected     SymbolKey
	}{
		{"public", "api", true, "lib.rs", SymbolKey("api")},
		{"private", "helper", false, "lib.rs", SymbolKey("lib.rs/helper")},
		{"private missing file", "helper", false, "", SymbolKey("helper")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RustSymbolKey(tt.symName, tt.exported, tt.fileBasename); got != tt.expected {
				t.Errorf("RustSymbolKey(%q, %v, %q) = %q, want %q", tt.symName, tt.exported, tt.fileBasename, got, tt.expected)
			}
		})
	}
}

func TestSymbolKeyString(t *testing.T) {
	tests := []struct {
		key      SymbolKey
		expected string
	}{
		{SymbolKey("Builder.Build"), "Builder.Build"},
		{SymbolKey("  trimmed  "), "trimmed"},
		{SymbolKey(""), ""},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			got := tt.key.String()
			if got != tt.expected {
				t.Errorf("SymbolKey(%q).String() = %q, want %q", string(tt.key), got, tt.expected)
			}
		})
	}
}

func TestSymbolKeyName(t *testing.T) {
	tests := []struct {
		key      SymbolKey
		expected string
	}{
		{SymbolKey("Builder.Build"), "Builder.Build"},
		{SymbolKey("utils.tsx/helperFunc"), "helperFunc"},
		{SymbolKey("init@store.go"), "init@store.go"},
		{SymbolKey("a/b/c"), "c"},
		{SymbolKey(""), ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.key), func(t *testing.T) {
			got := tt.key.Name()
			if got != tt.expected {
				t.Errorf("SymbolKey(%q).Name() = %q, want %q", string(tt.key), got, tt.expected)
			}
		})
	}
}

func TestSymbolEffectiveID(t *testing.T) {
	tests := []struct {
		name     string
		sym      Symbol
		expected string
	}{
		{
			name:     "with key",
			sym:      Symbol{ID: "old.go:Func", Key: SymbolKey("Func")},
			expected: "Func",
		},
		{
			name:     "without key falls back to ID",
			sym:      Symbol{ID: "old.go:Func"},
			expected: "old.go:Func",
		},
		{
			name:     "empty key falls back to ID",
			sym:      Symbol{ID: "old.go:Func", Key: SymbolKey("")},
			expected: "old.go:Func",
		},
		{
			name:     "whitespace-only key falls back to ID",
			sym:      Symbol{ID: "old.go:Func", Key: SymbolKey("   ")},
			expected: "old.go:Func",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.sym.EffectiveID()
			if got != tt.expected {
				t.Errorf("EffectiveID() = %q, want %q", got, tt.expected)
			}
		})
	}
}
