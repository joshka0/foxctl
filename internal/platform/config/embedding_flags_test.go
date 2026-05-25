package config

import (
	"testing"
	"testing/quick"
)

func TestResolveEmbeddingModesUseValidEnvironmentOverrides(t *testing.T) {
	t.Setenv(EnvEmbedQueryMode, " EMBED_QUERY ")
	t.Setenv(EnvEmbedSymbolTextMode, " DOC_ENRICHED ")
	t.Setenv(EnvEmbedFileTextMode, " INTENT ")

	if got := ResolveEmbedQueryMode(EmbedQueryModeEmbed); got != EmbedQueryModeEmbedQuery {
		t.Fatalf("query mode env override = %q, want %q", got, EmbedQueryModeEmbedQuery)
	}
	if got := ResolveEmbedSymbolTextMode(EmbedSymbolTextModeRaw); got != EmbedSymbolTextModeDocEnriched {
		t.Fatalf("symbol text mode env override = %q, want %q", got, EmbedSymbolTextModeDocEnriched)
	}
	if got := ResolveEmbedFileTextMode(EmbedFileTextModeRaw); got != EmbedFileTextModeIntent {
		t.Fatalf("file text mode env override = %q, want %q", got, EmbedFileTextModeIntent)
	}
}

func TestResolveEmbeddingModesIgnoreInvalidEnvironmentOverrides(t *testing.T) {
	t.Setenv(EnvEmbedQueryMode, "embed_document")
	t.Setenv(EnvEmbedSymbolTextMode, "markdown")
	t.Setenv(EnvEmbedFileTextMode, "summary")

	if got := ResolveEmbedQueryMode(EmbedQueryModeEmbed); got != EmbedQueryModeEmbed {
		t.Fatalf("query mode with invalid env = %q, want %q", got, EmbedQueryModeEmbed)
	}
	if got := ResolveEmbedSymbolTextMode(EmbedSymbolTextModeDocEnriched); got != EmbedSymbolTextModeDocEnriched {
		t.Fatalf("symbol text mode with invalid env = %q, want %q", got, EmbedSymbolTextModeDocEnriched)
	}
	if got := ResolveEmbedFileTextMode(EmbedFileTextModeIntent); got != EmbedFileTextModeIntent {
		t.Fatalf("file text mode with invalid env = %q, want %q", got, EmbedFileTextModeIntent)
	}
}

func TestResolveEmbeddingModesDefaultUnknownInputs(t *testing.T) {
	clearEmbeddingModeEnv(t)

	if got := ResolveEmbedQueryMode("embed_documents"); got != EmbedQueryModeAuto {
		t.Fatalf("unknown query mode default = %q, want %q", got, EmbedQueryModeAuto)
	}
	if got := ResolveEmbedSymbolTextMode("markdown"); got != EmbedSymbolTextModeRaw {
		t.Fatalf("unknown symbol text mode default = %q, want %q", got, EmbedSymbolTextModeRaw)
	}
	if got := ResolveEmbedFileTextMode("summary"); got != EmbedFileTextModeRaw {
		t.Fatalf("unknown file text mode default = %q, want %q", got, EmbedFileTextModeRaw)
	}
}

func TestParseEmbeddingModesNormalizeWhitespaceAndCase(t *testing.T) {
	queryCases := []struct {
		raw  string
		want EmbedQueryMode
	}{
		{raw: " AUTO ", want: EmbedQueryModeAuto},
		{raw: "EMBED", want: EmbedQueryModeEmbed},
		{raw: " embed_query ", want: EmbedQueryModeEmbedQuery},
	}
	for _, tc := range queryCases {
		got, ok := parseEmbedQueryMode(tc.raw)
		if !ok || got != tc.want {
			t.Fatalf("parse query mode %q = (%q, %v), want (%q, true)", tc.raw, got, ok, tc.want)
		}
	}

	symbolCases := []struct {
		raw  string
		want EmbedSymbolTextMode
	}{
		{raw: " RAW ", want: EmbedSymbolTextModeRaw},
		{raw: " doc_enriched ", want: EmbedSymbolTextModeDocEnriched},
	}
	for _, tc := range symbolCases {
		got, ok := parseEmbedSymbolTextMode(tc.raw)
		if !ok || got != tc.want {
			t.Fatalf("parse symbol text mode %q = (%q, %v), want (%q, true)", tc.raw, got, ok, tc.want)
		}
	}

	fileCases := []struct {
		raw  string
		want EmbedFileTextMode
	}{
		{raw: " RAW ", want: EmbedFileTextModeRaw},
		{raw: " intent ", want: EmbedFileTextModeIntent},
	}
	for _, tc := range fileCases {
		got, ok := parseEmbedFileTextMode(tc.raw)
		if !ok || got != tc.want {
			t.Fatalf("parse file text mode %q = (%q, %v), want (%q, true)", tc.raw, got, ok, tc.want)
		}
	}
}

func TestParseEmbeddingModesRejectGeneratedUnknowns(t *testing.T) {
	rejectsUnknown := func(raw string) bool {
		unknown := "unknown:" + raw

		if got, ok := parseEmbedQueryMode(unknown); ok || got != "" {
			return false
		}
		if got, ok := parseEmbedSymbolTextMode(unknown); ok || got != "" {
			return false
		}
		if got, ok := parseEmbedFileTextMode(unknown); ok || got != "" {
			return false
		}
		return true
	}

	if err := quick.Check(rejectsUnknown, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatalf("generated unknown embedding mode accepted: %v", err)
	}
}

func clearEmbeddingModeEnv(t *testing.T) {
	t.Helper()

	t.Setenv(EnvEmbedQueryMode, "")
	t.Setenv(EnvEmbedSymbolTextMode, "")
	t.Setenv(EnvEmbedFileTextMode, "")
}
