package embeddingtext

import "testing"

func TestDigestSHA256Stability(t *testing.T) {
	a := DigestSHA256("hello   world")
	b := DigestSHA256("hello world")
	if a != b {
		t.Fatalf("expected stable digest, got %s vs %s", a, b)
	}
}

func TestDigestSHA256Prefix(t *testing.T) {
	digest := DigestSHA256("hello world")
	prefix := DigestSHA256Prefix("hello world", 10)
	if len(prefix) != 10 {
		t.Fatalf("expected prefix length 10, got %d", len(prefix))
	}
	full := DigestSHA256Prefix("hello world", 200)
	if full != digest {
		t.Fatalf("expected full digest when n > len, got %s", full)
	}
}

func TestBuildSymbolContentDigestStable(t *testing.T) {
	input := SymbolDigestInput{
		Model:      "model-x",
		Kind:       "function",
		Name:       "Search",
		FilePath:   "internal/search/search.go",
		Signature:  "func Search(q string) []Result",
		Doc:        "// Search does things.\n//\n// Returns results.\n",
		BodyDigest: "sha256:abc123",
		Calls:      []string{"sym:two", "sym:one", "sym:two"},
	}

	d1 := BuildSymbolContentDigest(input)
	d2 := BuildSymbolContentDigest(input)
	if d1 != d2 {
		t.Fatalf("expected stable digest, got %s vs %s", d1, d2)
	}
}

func TestBuildSymbolContentDigestChangesOnDoc(t *testing.T) {
	base := SymbolDigestInput{
		Model:      "model-x",
		Kind:       "function",
		Name:       "Search",
		FilePath:   "internal/search/search.go",
		Signature:  "func Search(q string) []Result",
		BodyDigest: "sha256:abc123",
	}

	withDoc := base
	withDoc.Doc = "Find results."
	withoutDoc := base

	if BuildSymbolContentDigest(withDoc) == BuildSymbolContentDigest(withoutDoc) {
		t.Fatalf("expected digest to change when doc changes")
	}
}
