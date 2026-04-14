package embeddingtext

import "testing"

func TestNormalizeDoc_LineComments_UnwrapsParagraphs(t *testing.T) {
	in := "// Foo\n//   Bar\tbaz\n//\n// Quux\n"
	want := "Foo Bar baz\n\nQuux"
	got := NormalizeDoc(in)
	if got != want {
		t.Fatalf("NormalizeDoc mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestNormalizeDoc_BlockComment_StripsDecorators(t *testing.T) {
	in := "/*\n * Foo\n * Bar\n */"
	want := "Foo Bar"
	got := NormalizeDoc(in)
	if got != want {
		t.Fatalf("NormalizeDoc mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestNormalizeDoc_PreservesFencedCodeBlocks(t *testing.T) {
	in := "// Intro\n//\n// ```go\n// x := 1\n// y := 2\n// ```\n// Outro\n"
	want := "Intro\n\n```go\nx := 1\ny := 2\n```\nOutro"
	got := NormalizeDoc(in)
	if got != want {
		t.Fatalf("NormalizeDoc mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestNormalizeFirstComment_PreservesLineStructureAndIndent(t *testing.T) {
	in := "// Title\n//\n// - item1\n//   - sub\n//\n// End\n"
	want := "Title\n\n- item1\n  - sub\n\nEnd"
	got := NormalizeFirstComment(in)
	if got != want {
		t.Fatalf("NormalizeFirstComment mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestNormalizeForDigest_PreservesLineBoundaries(t *testing.T) {
	in := "Kind:\tfunction  \nSignature:\tfunc(x int)  error\n\nDoc:\n  Foo   Bar\n"
	want := "Kind: function\nSignature: func(x int) error\n\nDoc:\n  Foo Bar"
	got := NormalizeForDigest(in)
	if got != want {
		t.Fatalf("NormalizeForDigest mismatch\nwant:\n%q\ngot:\n%q", want, got)
	}
}
