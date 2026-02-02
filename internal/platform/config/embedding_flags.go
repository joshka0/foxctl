package config

import (
	"os"
	"strings"
)

// EnvEmbedQueryMode controls query embedding strategy (auto|embed|embed_query).
const EnvEmbedQueryMode = "EMBED_QUERY_MODE"

// EnvEmbedSymbolTextMode controls symbol embedding text format (raw|doc_enriched).
const EnvEmbedSymbolTextMode = "EMBED_SYMBOL_TEXT_MODE"

// EnvEmbedFileTextMode controls file embedding text format (raw|intent).
const EnvEmbedFileTextMode = "EMBED_FILE_TEXT_MODE"

// EmbedQueryMode controls how query embeddings are generated.
type EmbedQueryMode string

const (
	// EmbedQueryModeAuto uses EmbedQuery when supported, otherwise Embed.
	EmbedQueryModeAuto EmbedQueryMode = "auto"
	// EmbedQueryModeEmbed forces Embed for queries.
	EmbedQueryModeEmbed EmbedQueryMode = "embed"
	// EmbedQueryModeEmbedQuery forces EmbedQuery for queries when available.
	EmbedQueryModeEmbedQuery EmbedQueryMode = "embed_query"
)

// EmbedSymbolTextMode controls how symbol text is prepared for embedding.
type EmbedSymbolTextMode string

const (
	// EmbedSymbolTextModeRaw embeds the original symbol content as-is.
	EmbedSymbolTextModeRaw EmbedSymbolTextMode = "raw"
	// EmbedSymbolTextModeDocEnriched combines doc comments, signatures, and hints.
	EmbedSymbolTextModeDocEnriched EmbedSymbolTextMode = "doc_enriched"
)

// EmbedFileTextMode controls how file content is prepared for embedding.
type EmbedFileTextMode string

const (
	// EmbedFileTextModeRaw embeds the file content as-is.
	EmbedFileTextModeRaw EmbedFileTextMode = "raw"
	// EmbedFileTextModeIntent embeds a table-of-contents style summary.
	EmbedFileTextModeIntent EmbedFileTextMode = "intent"
)

// EmbeddingFlags holds embedding-related feature flags.
type EmbeddingFlags struct {
	QueryMode      EmbedQueryMode      `mapstructure:"query_mode" json:"query_mode"`
	SymbolTextMode EmbedSymbolTextMode `mapstructure:"symbol_text_mode" json:"symbol_text_mode"`
	FileTextMode   EmbedFileTextMode   `mapstructure:"file_text_mode" json:"file_text_mode"`
}

// ResolveEmbedQueryMode returns the active query mode, honoring env overrides.
func ResolveEmbedQueryMode(mode EmbedQueryMode) EmbedQueryMode {
	if env := strings.TrimSpace(os.Getenv(EnvEmbedQueryMode)); env != "" {
		if parsed, ok := parseEmbedQueryMode(env); ok {
			return parsed
		}
	}
	if parsed, ok := parseEmbedQueryMode(string(mode)); ok {
		return parsed
	}
	return EmbedQueryModeAuto
}

// ResolveEmbedSymbolTextMode returns the active symbol text mode, honoring env overrides.
func ResolveEmbedSymbolTextMode(mode EmbedSymbolTextMode) EmbedSymbolTextMode {
	if env := strings.TrimSpace(os.Getenv(EnvEmbedSymbolTextMode)); env != "" {
		if parsed, ok := parseEmbedSymbolTextMode(env); ok {
			return parsed
		}
	}
	if parsed, ok := parseEmbedSymbolTextMode(string(mode)); ok {
		return parsed
	}
	return EmbedSymbolTextModeRaw
}

// ResolveEmbedFileTextMode returns the active file text mode, honoring env overrides.
func ResolveEmbedFileTextMode(mode EmbedFileTextMode) EmbedFileTextMode {
	if env := strings.TrimSpace(os.Getenv(EnvEmbedFileTextMode)); env != "" {
		if parsed, ok := parseEmbedFileTextMode(env); ok {
			return parsed
		}
	}
	if parsed, ok := parseEmbedFileTextMode(string(mode)); ok {
		return parsed
	}
	return EmbedFileTextModeRaw
}

func parseEmbedQueryMode(raw string) (EmbedQueryMode, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "auto":
		return EmbedQueryModeAuto, true
	case "embed":
		return EmbedQueryModeEmbed, true
	case "embed_query":
		return EmbedQueryModeEmbedQuery, true
	default:
		return "", false
	}
}

func parseEmbedSymbolTextMode(raw string) (EmbedSymbolTextMode, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "raw":
		return EmbedSymbolTextModeRaw, true
	case "doc_enriched":
		return EmbedSymbolTextModeDocEnriched, true
	default:
		return "", false
	}
}

func parseEmbedFileTextMode(raw string) (EmbedFileTextMode, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "raw":
		return EmbedFileTextModeRaw, true
	case "intent":
		return EmbedFileTextModeIntent, true
	default:
		return "", false
	}
}
