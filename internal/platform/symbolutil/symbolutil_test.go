package symbolutil

import (
	"fmt"
	"strings"
	"testing"
	"testing/quick"
)

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

func TestScopedSymbolIDTrimsAndHandlesMissingSegments(t *testing.T) {
	tests := []struct {
		name      string
		pkg       string
		symbolKey string
		want      string
	}{
		{
			name:      "package and symbol key",
			pkg:       " go:pkg/a ",
			symbolKey: "\thelper.go/Helper\n",
			want:      "go:pkg/a::helper.go/Helper",
		},
		{
			name:      "symbol key without package",
			pkg:       "  ",
			symbolKey: " helper.go/Helper ",
			want:      "helper.go/Helper",
		},
		{
			name:      "package without symbol key",
			pkg:       " go:pkg/a ",
			symbolKey: "  ",
			want:      "go:pkg/a",
		},
		{
			name:      "empty package and symbol key",
			pkg:       "  ",
			symbolKey: "\t",
			want:      "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := ScopedSymbolID(tt.pkg, tt.symbolKey); got != tt.want {
				t.Fatalf("ScopedSymbolID(%q, %q)=%q want %q", tt.pkg, tt.symbolKey, got, tt.want)
			}
		})
	}
}

func TestScopedSymbolIDFromKeyEntryNameRejectsNonKeyEntries(t *testing.T) {
	t.Parallel()

	validName := KeyEntryName("ws", "go:pkg/a", "helper.go/Helper")
	if scoped, ok := ScopedSymbolIDFromKeyEntryName(" ws ", "\n"+validName+"\t"); !ok || scoped != "go:pkg/a::helper.go/Helper" {
		t.Fatalf("trimmed parse=%q/%v want scoped key", scoped, ok)
	}

	rejects := []struct {
		name      string
		workspace string
		entryName string
	}{
		{
			name:      "wrong workspace",
			workspace: "other",
			entryName: validName,
		},
		{
			name:      "legacy file entry",
			workspace: "ws",
			entryName: EntryName("ws", "pkg/a/helper.go", "Helper"),
		},
		{
			name:      "file meta entry",
			workspace: "ws",
			entryName: FileMetaEntryName("ws", "pkg/a/helper.go"),
		},
		{
			name:      "symbol entry without scoped delimiter",
			workspace: "ws",
			entryName: "symbol://ws/go:pkg/a",
		},
	}

	for _, tt := range rejects {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if scoped, ok := ScopedSymbolIDFromKeyEntryName(tt.workspace, tt.entryName); ok || scoped != "" {
				t.Fatalf("ScopedSymbolIDFromKeyEntryName(%q, %q)=%q/%v want reject", tt.workspace, tt.entryName, scoped, ok)
			}
		})
	}
}

func TestKeyEntryNameScopedIDRoundTripProperty(t *testing.T) {
	t.Parallel()

	property := func(seed uint64) bool {
		workspace := generatedSymbolSegment("ws", seed)
		pkg := "go:" + generatedSymbolPath("pkg", seed)
		symbolKey := generatedSymbolPath("symbol.go", seed>>8) + "/" + generatedSymbolSegment("Symbol", seed>>16)

		wantScoped := pkg + "::" + symbolKey
		if got := ScopedSymbolID(" "+pkg+" ", "\t"+symbolKey+"\n"); got != wantScoped {
			return false
		}

		name := KeyEntryName(workspace, " "+pkg+" ", "\t"+symbolKey+"\n")
		scoped, ok := ScopedSymbolIDFromKeyEntryName(" "+workspace+" ", "\n"+name+"\t")
		if !ok || scoped != wantScoped {
			return false
		}

		wrongScoped, wrongOK := ScopedSymbolIDFromKeyEntryName(workspace+"-other", name)
		return !wrongOK && wrongScoped == ""
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 256}); err != nil {
		t.Fatalf("key entry scoped id round trip property failed: %v", err)
	}
}

func generatedSymbolPath(prefix string, seed uint64) string {
	return fmt.Sprintf("%s/%03d/%03d", prefix, seed%997, (seed/997)%997)
}

func generatedSymbolSegment(prefix string, seed uint64) string {
	value := fmt.Sprintf("%s-%03d", prefix, seed%997)
	return strings.ReplaceAll(value, "::", "_")
}
