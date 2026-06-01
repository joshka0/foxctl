package stringutil

import (
	"reflect"
	"strings"
	"testing"
	"testing/quick"
)

func TestNormalizeStrings(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "nil returns non nil empty slice",
			in:   nil,
			want: []string{},
		},
		{
			name: "trims and drops blanks",
			in:   []string{" first ", "", "\t", "\nsecond\n", "third"},
			want: []string{"first", "second", "third"},
		},
		{
			name: "preserves duplicates and order",
			in:   []string{"alpha", " beta ", "alpha", "beta"},
			want: []string{"alpha", "beta", "alpha", "beta"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeStrings(tt.in)
			if got == nil {
				t.Fatal("NormalizeStrings returned nil")
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("NormalizeStrings(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeStringsPropertyTrimDropAndIdempotent(t *testing.T) {
	property := func(a, b, c uint8) bool {
		in := generatedStringList(a, b, c)
		got := NormalizeStrings(in)
		if got == nil {
			t.Log("NormalizeStrings returned nil")
			return false
		}
		if !reflect.DeepEqual(got, expectedNormalizedStrings(in)) {
			t.Logf("NormalizeStrings output mismatch for input len=%d", len(in))
			return false
		}
		for _, item := range got {
			if item == "" || strings.TrimSpace(item) != item {
				t.Logf("normalized item %q is blank or not trimmed", item)
				return false
			}
		}
		again := NormalizeStrings(got)
		if !reflect.DeepEqual(again, got) {
			t.Logf("NormalizeStrings is not idempotent: got=%v again=%v", got, again)
			return false
		}
		return true
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("NormalizeStrings property failed: %v", err)
	}
}

func expectedNormalizedStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func generatedStringList(a, b, c uint8) []string {
	corpus := []string{
		"",
		" ",
		"\t",
		"\n",
		" alpha ",
		"\tbeta\n",
		"gamma",
		"delta delta",
		"\u00a0epsilon\u00a0",
	}
	return []string{
		corpus[int(a)%len(corpus)],
		corpus[int(b)%len(corpus)],
		corpus[int(c)%len(corpus)],
	}
}
