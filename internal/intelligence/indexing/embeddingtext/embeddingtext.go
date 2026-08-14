// Package embeddingtext re-exports the foxsearch embeddingtext module.
//
// The implementation moved to github.com/joshka0/foxsearch/embeddingtext as
// part of the foxsearch v0.2.0 extraction; this shim preserves the foxctl
// import path for existing consumers. New code may import either path.
package embeddingtext

import "github.com/joshka0/foxsearch/embeddingtext"

type (
	// SymbolDigestInput carries the fields hashed into a symbol content digest.
	SymbolDigestInput = embeddingtext.SymbolDigestInput
	// SemanticEnvelopeDigestInput carries the fields hashed into an envelope digest.
	SemanticEnvelopeDigestInput = embeddingtext.SemanticEnvelopeDigestInput
	// SemanticEnvelopeAnchorDigest is one anchor's contribution to an envelope digest.
	SemanticEnvelopeAnchorDigest = embeddingtext.SemanticEnvelopeAnchorDigest
	// SemanticEnvelopeDigestSection is a named digest section.
	SemanticEnvelopeDigestSection = embeddingtext.SemanticEnvelopeDigestSection
	// SymbolTextOptions controls symbol embedding text construction.
	SymbolTextOptions = embeddingtext.SymbolTextOptions
	// SymbolInfo describes a symbol for text construction.
	SymbolInfo = embeddingtext.SymbolInfo
	// SymbolTextMetrics reports construction statistics for symbol text.
	SymbolTextMetrics = embeddingtext.SymbolTextMetrics
)

var (
	// DigestSHA256 returns the hex SHA-256 of the text.
	DigestSHA256 = embeddingtext.DigestSHA256
	// DigestSHA256Prefix returns the first n hex chars of the SHA-256.
	DigestSHA256Prefix = embeddingtext.DigestSHA256Prefix
	// BuildSymbolContentDigest builds a stable digest over symbol content fields.
	BuildSymbolContentDigest = embeddingtext.BuildSymbolContentDigest
	// BuildSemanticEnvelopeContentDigest builds a stable digest over envelope fields.
	BuildSemanticEnvelopeContentDigest = embeddingtext.BuildSemanticEnvelopeContentDigest
	// NormalizeDoc normalizes a doc comment for embedding text.
	NormalizeDoc = embeddingtext.NormalizeDoc
	// NormalizeFirstComment normalizes the first comment of a block.
	NormalizeFirstComment = embeddingtext.NormalizeFirstComment
	// NormalizeForDigest normalizes text for digesting.
	NormalizeForDigest = embeddingtext.NormalizeForDigest
	// DefaultSymbolTextOptions returns the default construction options.
	DefaultSymbolTextOptions = embeddingtext.DefaultSymbolTextOptions
	// DefaultSymbolTextOptionsDocEnriched returns doc-enriched options.
	DefaultSymbolTextOptionsDocEnriched = embeddingtext.DefaultSymbolTextOptionsDocEnriched
	// DefaultSymbolTextOptionsSummaryOnly returns summary-only options.
	DefaultSymbolTextOptionsSummaryOnly = embeddingtext.DefaultSymbolTextOptionsSummaryOnly
	// BuildSymbolEmbeddingText builds the embedding text for a symbol.
	BuildSymbolEmbeddingText = embeddingtext.BuildSymbolEmbeddingText
	// BuildSymbolEmbeddingTextWithMetrics builds symbol text and reports metrics.
	BuildSymbolEmbeddingTextWithMetrics = embeddingtext.BuildSymbolEmbeddingTextWithMetrics
	// BuildSymbolAliases builds the alias list for a symbol.
	BuildSymbolAliases = embeddingtext.BuildSymbolAliases
)
