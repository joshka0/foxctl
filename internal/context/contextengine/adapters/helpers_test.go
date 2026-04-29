package adapters

import (
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

func TestParseStringRefs(t *testing.T) {
	tests := []struct {
		name    string
		refs    []string
		want    int
		wantErr bool
	}{
		{"nil refs", nil, 0, false},
		{"empty refs", []string{}, 0, false},
		{"valid refs", []string{"path:foo.go", "task:abc123", "session:xyz"}, 3, false},
		{"invalid ref", []string{"invalid_no_colon"}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStringRefs(tt.refs)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseStringRefs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(got) != tt.want {
				t.Errorf("ParseStringRefs() got %d refs, want %d", len(got), tt.want)
			}
		})
	}
}

func TestFormatStringRefs(t *testing.T) {
	refs := []contextengine.EvidenceRef{
		{Type: contextengine.RefTypePath, Ref: "foo.go"},
		{Type: contextengine.RefTypeTask, Ref: "abc"},
	}
	got := FormatStringRefs(refs)
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
	if got[0] != "path:foo.go" {
		t.Errorf("got[0] = %q, want %q", got[0], "path:foo.go")
	}
	if got[1] != "task:abc" {
		t.Errorf("got[1] = %q, want %q", got[1], "task:abc")
	}
}

func TestFormatStringRefsEmpty(t *testing.T) {
	got := FormatStringRefs(nil)
	if got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}
}

func TestInferRefType(t *testing.T) {
	tests := []struct {
		raw  string
		want contextengine.RefType
	}{
		{"", ""},
		{"path:foo.go", contextengine.RefTypePath},
		{"task:abc", contextengine.RefTypeTask},
		{"session:xyz", contextengine.RefTypeSession},
		{"note:my-note", contextengine.RefTypeNote},
		{"internal/foo.go", contextengine.RefTypePath},
		{"some-random-string", contextengine.RefTypePath},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got := InferRefType(tt.raw)
			if got != tt.want {
				t.Errorf("InferRefType(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseOrInferRef(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		typ  contextengine.RefType
		ref  string
	}{
		{"empty", "", "", ""},
		{"typed", "path:foo.go", contextengine.RefTypePath, "foo.go"},
		{"untyped file", "internal/foo.go", contextengine.RefTypePath, "internal/foo.go"},
		{"task type", "task:abc123", contextengine.RefTypeTask, "abc123"},
		{"session type", "session:xyz789", contextengine.RefTypeSession, "xyz789"},
		{"note type", "note:my-note", contextengine.RefTypeNote, "my-note"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseOrInferRef(tt.raw)
			if got.Type != tt.typ {
				t.Errorf("ParseOrInferRef(%q).Type = %q, want %q", tt.raw, got.Type, tt.typ)
			}
			if got.Ref != tt.ref {
				t.Errorf("ParseOrInferRef(%q).Ref = %q, want %q", tt.raw, got.Ref, tt.ref)
			}
		})
	}
}

func TestParseOrInferRefs(t *testing.T) {
	refs := ParseOrInferRefs([]string{"path:foo.go", "task:abc", "raw_path.go"})
	if len(refs) != 3 {
		t.Fatalf("expected 3 refs, got %d", len(refs))
	}
	if refs[0].Type != contextengine.RefTypePath {
		t.Errorf("refs[0].Type = %q", refs[0].Type)
	}
	if refs[1].Type != contextengine.RefTypeTask {
		t.Errorf("refs[1].Type = %q", refs[1].Type)
	}
	if refs[2].Ref != "raw_path.go" {
		t.Errorf("refs[2].Ref = %q", refs[2].Ref)
	}
}

func TestMustTime(t *testing.T) {
	tests := []struct {
		s    string
		want bool // non-zero
	}{
		{"", false},
		{"not-a-time", false},
		{"2025-01-15T12:00:00Z", true},
	}
	for _, tt := range tests {
		got := mustTime(tt.s)
		if got.IsZero() == tt.want {
			t.Errorf("mustTime(%q).IsZero() = %v, want opposite", tt.s, got.IsZero())
		}
	}
}

func TestSafeFirst(t *testing.T) {
	if safeFirst(nil) != "" {
		t.Error("expected empty for nil")
	}
	if safeFirst([]string{}) != "" {
		t.Error("expected empty for empty slice")
	}
	if safeFirst([]string{"a", "b"}) != "a" {
		t.Error("expected first element")
	}
}

func TestRoundTripStringRefs(t *testing.T) {
	original := []string{"path:foo.go", "task:abc", "session:xyz"}
	refs, err := ParseStringRefs(original)
	if err != nil {
		t.Fatal(err)
	}
	got := FormatStringRefs(refs)
	if len(got) != len(original) {
		t.Fatalf("round-trip length mismatch: %d vs %d", len(got), len(original))
	}
	for i, s := range original {
		if got[i] != s {
			t.Errorf("round-trip[%d]: got %q, want %q", i, got[i], s)
		}
	}
}

func TestFmtRef(t *testing.T) {
	s := fmtRef("ws1", 3)
	if s == "" {
		t.Error("expected non-empty ref")
	}
}

func init() {
	// Ensure time comparisons work
	_ = time.Now()
}
