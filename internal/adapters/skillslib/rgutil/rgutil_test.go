package rgutil

import (
	"reflect"
	"testing"
	"testing/quick"
)

func TestNormalizeAppliesBoundedDefaults(t *testing.T) {
	got := Normalize(SearchInput{MaxMatches: 0, ContextLines: -3})
	if got.MaxMatches != DefaultMaxMatches {
		t.Fatalf("MaxMatches = %d, want default %d", got.MaxMatches, DefaultMaxMatches)
	}
	if got.ContextLines != 0 {
		t.Fatalf("ContextLines = %d, want 0", got.ContextLines)
	}
}

func TestNormalizeGeneratedInvariants(t *testing.T) {
	prop := func(maxMatches int16, contextLines int16) bool {
		in := SearchInput{MaxMatches: int(maxMatches), ContextLines: int(contextLines)}
		got := Normalize(in)
		if got.MaxMatches <= 0 {
			return false
		}
		if in.MaxMatches > 0 && got.MaxMatches != in.MaxMatches {
			return false
		}
		if got.ContextLines < 0 {
			return false
		}
		return in.ContextLines < 0 || got.ContextLines == in.ContextLines
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Fatal(err)
	}
}

func TestBuildSearchOptsDoesNotExposeDefaultExcludeBackingArray(t *testing.T) {
	original := append([]string(nil), DefaultExcludeGlobs...)
	t.Cleanup(func() {
		DefaultExcludeGlobs = original
	})

	opts := BuildSearchOpts(SearchInput{Pattern: "needle"}, "/repo", ".", nil)
	if !reflect.DeepEqual(opts.ExcludeGlobs, original) {
		t.Fatalf("ExcludeGlobs = %v, want %v", opts.ExcludeGlobs, original)
	}

	opts.ExcludeGlobs[0] = "mutated"
	next := BuildSearchOpts(SearchInput{Pattern: "needle"}, "/repo", ".", nil)
	if !reflect.DeepEqual(next.ExcludeGlobs, original) {
		t.Fatalf("default excludes were mutated through returned opts: %v, want %v", next.ExcludeGlobs, original)
	}
}

func TestBuildSearchOptsCopiesCallerProvidedSlices(t *testing.T) {
	in := SearchInput{
		Pattern: "needle",
		Glob:    []string{"*.go"},
		GlobNot: []string{"vendor"},
	}
	opts := BuildSearchOpts(in, "/repo", "src", nil)

	opts.IncludeGlobs[0] = "*.ts"
	opts.ExcludeGlobs[0] = "node_modules"
	if in.Glob[0] != "*.go" {
		t.Fatalf("input include glob mutated through opts: %v", in.Glob)
	}
	if in.GlobNot[0] != "vendor" {
		t.Fatalf("input exclude glob mutated through opts: %v", in.GlobNot)
	}
}

func TestBuildSearchOptsCopiesCustomDefaultExcludes(t *testing.T) {
	defaultExclude := []string{"dist", "build"}
	opts := BuildSearchOpts(SearchInput{Pattern: "needle"}, "/repo", ".", defaultExclude)

	opts.ExcludeGlobs[0] = "mutated"
	if defaultExclude[0] != "dist" {
		t.Fatalf("custom defaults mutated through opts: %v", defaultExclude)
	}
}
