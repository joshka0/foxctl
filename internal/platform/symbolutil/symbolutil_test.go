package symbolutil

import "testing"

func TestDeriveSymbolPackage(t *testing.T) {
	tests := []struct {
		name string
		path string
		lang string
		want string
	}{
		{name: "go nested", path: "internal/app/main.go", lang: "go", want: "go:internal/app"},
		{name: "go root", path: "main.go", lang: "go", want: "go:root"},
		{name: "typescript", path: "src/app.ts", lang: "typescript", want: "ts:local:src"},
		{name: "javascript", path: "src/app.js", lang: "javascript", want: "ts:local:src"},
		{name: "python", path: "pkg/tool.py", lang: "python", want: "py:pkg"},
		{name: "elixir", path: "lib/app.ex", lang: "elixir", want: "ex:lib"},
		{name: "default", path: "README.md", lang: "markdown", want: "file:root"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveSymbolPackage(tt.path, tt.lang); got != tt.want {
				t.Fatalf("DeriveSymbolPackage(%q, %q) = %q, want %q", tt.path, tt.lang, got, tt.want)
			}
		})
	}
}

func TestScopedSymbolIDAndKeyEntryName(t *testing.T) {
	if got := ScopedSymbolID("go:pkg/a", "helper.go/Helper"); got != "go:pkg/a::helper.go/Helper" {
		t.Fatalf("ScopedSymbolID = %q", got)
	}
	name := KeyEntryName("ws", "go:pkg/a", "helper.go/Helper")
	if name != "symbol://ws/go:pkg/a::helper.go/Helper" {
		t.Fatalf("KeyEntryName = %q", name)
	}
	scoped, ok := ScopedSymbolIDFromKeyEntryName("ws", name)
	if !ok || scoped != "go:pkg/a::helper.go/Helper" {
		t.Fatalf("ScopedSymbolIDFromKeyEntryName = %q/%v", scoped, ok)
	}
	if scoped, ok := ScopedSymbolIDFromKeyEntryName("ws", EntryName("ws", "pkg/a/helper.go", "Helper")); ok || scoped != "" {
		t.Fatalf("legacy EntryName parsed as scoped: %q/%v", scoped, ok)
	}
}
