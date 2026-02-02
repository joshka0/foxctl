package parser

import "testing"

func TestParse_ShorthandIndex(t *testing.T) {
	doc := "Index: Foo, bar\n\nHello world."
	parsed := Parse(doc)
	if !parsed.HasIndex {
		t.Fatalf("expected HasIndex")
	}
	if parsed.Doc != "Hello world." {
		t.Fatalf("unexpected doc: %q", parsed.Doc)
	}
	if len(parsed.Index.Keywords) != 2 {
		t.Fatalf("expected 2 keywords, got %d", len(parsed.Index.Keywords))
	}
	if parsed.Index.Keywords[0] != "bar" || parsed.Index.Keywords[1] != "foo" {
		t.Fatalf("unexpected keywords: %#v", parsed.Index.Keywords)
	}
}

func TestParse_StructuredIndex(t *testing.T) {
	doc := "Index:\n  Purpose: Provides things\n  Related: Foo, Bar\n  Keywords: baz, qux\n\nMore docs."
	parsed := Parse(doc)
	if !parsed.HasIndex {
		t.Fatalf("expected HasIndex")
	}
	if parsed.Doc != "More docs." {
		t.Fatalf("unexpected doc: %q", parsed.Doc)
	}
	if parsed.Index.Purpose != "Provides things" {
		t.Fatalf("unexpected purpose: %q", parsed.Index.Purpose)
	}
	if len(parsed.Index.Related) != 2 {
		t.Fatalf("expected 2 related, got %d", len(parsed.Index.Related))
	}
	if len(parsed.Index.Keywords) != 2 {
		t.Fatalf("expected 2 keywords, got %d", len(parsed.Index.Keywords))
	}
}

func TestParse_BulletFields(t *testing.T) {
	doc := "Index:\n- Events: Alpha, Beta\n- OutputFields: foo, bar\n\nDoc text."
	parsed := Parse(doc)
	if parsed.Doc != "Doc text." {
		t.Fatalf("unexpected doc: %q", parsed.Doc)
	}
	if len(parsed.Index.Events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(parsed.Index.Events))
	}
	if len(parsed.Index.OutputFields) != 2 {
		t.Fatalf("expected 2 output fields, got %d", len(parsed.Index.OutputFields))
	}
}
