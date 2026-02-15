package embeddingtext

import (
	"strings"
	"testing"
)

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

func TestBuildSymbolContentDigest_V2SymbolKey(t *testing.T) {
	// Ensure v2 format is used for content digests.
	digest := BuildSymbolContentDigest(SymbolDigestInput{
		Model:      "model-x",
		Kind:       "function",
		Name:       "Build",
		SymbolKey:  "Builder.Build",
		FilePath:   "builder.go",
		Signature:  "func Build() {}",
		BodyDigest: "sha256:abc",
	})
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("expected digest to use sha256 format, got %q", digest)
	}

	// With SymbolKey set - should use key even if file path changes.
	input1 := SymbolDigestInput{
		Kind:      "function",
		Name:      "Build",
		SymbolKey: "Builder.Build",
		FilePath:  "builder.go",
	}
	digest1 := BuildSymbolContentDigest(input1)
	if digest1 == "" {
		t.Fatal("expected non-empty digest")
	}

	input2 := SymbolDigestInput{
		Kind:      "function",
		Name:      "Build",
		SymbolKey: "Builder.Build",
		FilePath:  "new_location/builder.go",
	}
	digest2 := BuildSymbolContentDigest(input2)
	if digest1 != digest2 {
		t.Errorf("expected same digest for same SymbolKey regardless of FilePath, got %q vs %q", digest1, digest2)
	}

	input3 := SymbolDigestInput{
		Kind:     "function",
		Name:     "Build",
		FilePath: "builder.go",
	}
	digest3 := BuildSymbolContentDigest(input3)
	if digest3 == "" {
		t.Fatal("expected non-empty digest")
	}

	if digest1 == digest3 {
		t.Error("expected different digest when using FilePath fallback vs SymbolKey")
	}

	_ = digest
}
