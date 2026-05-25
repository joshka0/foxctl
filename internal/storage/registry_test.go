package storage

import (
	"strings"
	"testing"
	"unicode"
)

func TestFindStoreLookupIsCaseInsensitiveForCanonicalStores(t *testing.T) {
	for _, spec := range CanonicalStores() {
		name := string(spec.Name)
		tests := []struct {
			name  string
			input string
		}{
			{name: "canonical", input: name},
			{name: "lowercase", input: strings.ToLower(name)},
			{name: "mixed case", input: alternatingCase(name)},
			{name: "trimmed whitespace", input: " \t" + strings.ToLower(name) + "\n"},
		}

		for _, tt := range tests {
			t.Run(name+"/"+tt.name, func(t *testing.T) {
				got, ok := FindStore(tt.input)
				if !ok {
					t.Fatalf("FindStore(%q) returned ok=false", tt.input)
				}
				if got != spec {
					t.Fatalf("FindStore(%q)=%+v want %+v", tt.input, got, spec)
				}
			})
		}
	}
}

func TestFindStoreRejectsUnknownStoreName(t *testing.T) {
	if got, ok := FindStore("not-a-store"); ok {
		t.Fatalf("FindStore returned %+v for unknown store", got)
	}
}

func alternatingCase(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i, r := range s {
		if i%2 == 0 {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune(unicode.ToUpper(r))
		}
	}
	return b.String()
}
