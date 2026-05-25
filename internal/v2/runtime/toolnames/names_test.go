package toolnames

import (
	"strings"
	"testing"
	"testing/quick"
)

func TestCanonical(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   string
		want string
	}{
		{in: "code/search", want: "code_search"},
		{in: "code.search", want: "code_search"},
		{in: "code_search", want: "code_search"},
		{in: " Code//Search ", want: "code_search"},
		{in: "memory/query", want: "memory_query"},
		{in: "repo/index/dag/grep", want: "repo_index_dag_grep"},
		{in: "fs.read_file", want: "fs_read_file"},
		{in: "", want: ""},
		{in: "   ", want: ""},
		{in: "___", want: ""},
		{in: "code search", want: ""},
		{in: "code-search", want: ""},
		{in: "code/search;rm", want: ""},
		{in: "code/search\nrm", want: ""},
		{in: "code/search\xff", want: ""},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := Canonical(tc.in); got != tc.want {
				t.Fatalf("Canonical(%q)=%q want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCanonicalPropertyAliasEquivalentIdempotentAndClean(t *testing.T) {
	t.Parallel()

	property := func(toolSeed uint8, aliasSeed uint8) bool {
		base := generatedCanonicalToolName(toolSeed)
		alias := generatedToolNameAlias(base, aliasSeed)

		got := Canonical(alias)
		if got != base {
			t.Logf("Canonical(%q)=%q want %q", alias, got, base)
			return false
		}
		if Canonical(got) != got {
			t.Logf("Canonical not idempotent: Canonical(%q)=%q", got, Canonical(got))
			return false
		}
		if got == "" || strings.ContainsAny(got, "./ ") || strings.Contains(got, "__") || strings.HasPrefix(got, "_") || strings.HasSuffix(got, "_") {
			t.Logf("Canonical(%q) produced dirty name %q", alias, got)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalPropertySeparatorOnlyInputsCollapseToEmpty(t *testing.T) {
	t.Parallel()

	property := func(raw []uint8) bool {
		var b strings.Builder
		for _, seed := range raw {
			switch seed % 3 {
			case 0:
				b.WriteByte('_')
			case 1:
				b.WriteByte('/')
			default:
				b.WriteByte('.')
			}
			if b.Len() >= 32 {
				break
			}
		}
		input := " " + b.String() + " "
		if got := Canonical(input); got != "" {
			t.Logf("Canonical(%q)=%q want empty", input, got)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalPropertyFailClosedOrSafeIdentifier(t *testing.T) {
	t.Parallel()

	property := func(raw []byte) bool {
		if len(raw) > 96 {
			raw = raw[:96]
		}

		got := Canonical(string(raw))
		if got == "" {
			return true
		}
		if !isCanonicalToolIdentifier(got) {
			t.Logf("Canonical(%q) produced unsafe identifier %q", string(raw), got)
			return false
		}
		if Canonical(got) != got {
			t.Logf("Canonical is not idempotent for %q: %q", got, Canonical(got))
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalPropertyUnsupportedCharactersRejectWholeName(t *testing.T) {
	t.Parallel()

	property := func(toolSeed uint8, badSeed uint8) bool {
		base := generatedCanonicalToolName(toolSeed)
		bad := unsupportedToolNameByte(badSeed)
		input := base[:len(base)/2] + string([]byte{bad}) + base[len(base)/2:]
		got := Canonical(input)
		if got != "" {
			t.Logf("Canonical(%q)=%q want empty", input, got)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func generatedCanonicalToolName(seed uint8) string {
	names := []string{
		"code_search",
		"context_retrieve",
		"fs_read_file",
		"memory_query",
		"obsidian_related",
		"repo_index_dag_grep",
		"todo_set_active",
	}
	return names[int(seed)%len(names)]
}

func isCanonicalToolIdentifier(s string) bool {
	if s == "" || strings.HasPrefix(s, "_") || strings.HasSuffix(s, "_") || strings.Contains(s, "__") {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			continue
		}
		return false
	}
	return true
}

func unsupportedToolNameByte(seed uint8) byte {
	chars := []byte{' ', '-', ';', ':', '\n', '\t', '\'', '"', '\\', '$', '#', '@', '!', '?', '*'}
	return chars[int(seed)%len(chars)]
}

func generatedToolNameAlias(canonical string, seed uint8) string {
	switch seed % 8 {
	case 0:
		return canonical
	case 1:
		return strings.ReplaceAll(canonical, "_", "/")
	case 2:
		return strings.ReplaceAll(canonical, "_", ".")
	case 3:
		return strings.ToUpper(strings.ReplaceAll(canonical, "_", "/"))
	case 4:
		return " " + strings.ReplaceAll(canonical, "_", "//") + " "
	case 5:
		return "__" + canonical + "__"
	case 6:
		return strings.ReplaceAll(strings.ToUpper(canonical), "_", ".")
	default:
		return strings.ReplaceAll(canonical, "_", "___")
	}
}
