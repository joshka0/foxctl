package symbol

import "testing"

func TestNormalizeFileSummaryInput_DedupAndNormalize(t *testing.T) {
	input := FileSummaryInput{
		FilePath:     "a.go",
		SymbolsHash:  "sha256:abc",
		Package:      "  pkg  ",
		PackageDoc:   "// Foo\n//  Bar\n",
		FirstComment: "// First\n// Comment\n",
		TopSymbols:   []string{"Beta", " Alpha ", "Beta"},
	}

	normalized := NormalizeFileSummaryInput(input)
	if normalized.Package != "pkg" {
		t.Fatalf("expected package trimmed, got %q", normalized.Package)
	}
	if normalized.PackageDoc == "" || normalized.FirstComment == "" {
		t.Fatalf("expected normalized docs to be non-empty")
	}
	if len(normalized.TopSymbols) != 2 || normalized.TopSymbols[0] != "Alpha" || normalized.TopSymbols[1] != "Beta" {
		t.Fatalf("expected sorted/deduped symbols, got %#v", normalized.TopSymbols)
	}
}

func TestComputeFileSummaryDigest_NormalizesInputs(t *testing.T) {
	base := FileSummaryInput{
		FilePath:     "a.go",
		SymbolsHash:  "sha256:abc",
		Package:      "pkg",
		PackageDoc:   "// Foo\n// Bar\n",
		FirstComment: "// First\n// Comment\n",
		TopSymbols:   []string{"Beta", "Alpha", "Beta"},
	}
	alt := FileSummaryInput{
		FilePath:     "a.go",
		SymbolsHash:  "sha256:abc",
		Package:      " pkg ",
		PackageDoc:   "Foo Bar",
		FirstComment: "First\nComment",
		TopSymbols:   []string{" Alpha ", "Beta"},
	}

	if ComputeFileSummaryDigest(base) != ComputeFileSummaryDigest(alt) {
		t.Fatalf("expected digest to normalize equivalent inputs")
	}
}

func TestComputeFileSummaryDigest_DiffersWhenContentChanges(t *testing.T) {
	base := FileSummaryInput{
		FilePath:     "a.go",
		SymbolsHash:  "sha256:abc",
		Package:      "pkg",
		PackageDoc:   "Foo Bar",
		FirstComment: "First Comment",
		TopSymbols:   []string{"Alpha", "Beta"},
	}
	changed := base
	changed.PackageDoc = "Different Doc"

	if ComputeFileSummaryDigest(base) == ComputeFileSummaryDigest(changed) {
		t.Fatalf("expected digest to change when doc changes")
	}
}
