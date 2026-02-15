package embeddingtext

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// DigestSHA256 computes a SHA256 digest of the normalized text.
// The input is normalized before hashing to ensure stable digests
// across insignificant formatting changes.
//
// Index:
//
//	Purpose: Generate stable content hash for change detection
//	Related: NormalizeForDigest
//	Keywords: sha256, hash, content digest, change detection
func DigestSHA256(text string) string {
	normalized := NormalizeForDigest(text)
	hash := sha256.Sum256([]byte(normalized))
	return "sha256:" + hex.EncodeToString(hash[:])
}

// SymbolDigestInput captures normalized components used for symbol content digests.
//
// Index:
//
//	Purpose: Provide structured inputs for symbol content hashing
//	Related: BuildSymbolContentDigest, SymbolInfo
//	Keywords: content digest, symbol hash, embedding stability
type SymbolDigestInput struct {
	// Model is the embedding model name, used to ensure model changes invalidate digests.
	Model string

	// Kind is the symbol kind (function, type, etc.).
	Kind string

	// Name is the symbol name.
	Name string

	// SymbolKey is the stable, file-path-independent key for the symbol.
	// When set, replaces FilePath in the digest computation for stability across file moves.
	SymbolKey string

	// FilePath is the symbol file path (workspace-relative).
	FilePath string

	// Signature is the symbol signature.
	Signature string

	// Doc is the raw documentation comment (GoDoc).
	Doc string

	// BodyDigest is the sha256:<hex> digest of the symbol body.
	BodyDigest string

	// Calls lists related call targets (optional).
	Calls []string
}

// BuildSymbolContentDigest returns a stable digest for symbol embeddings.
// It uses normalized doc text plus structured identifiers and digests, so
// whitespace-only edits won't trigger re-embedding while semantic changes will.
//
// Index:
//
//	Purpose: Generate deterministic digest for symbol embedding deduplication
//	Related: NormalizeDoc, DigestSHA256
//	Keywords: symbol digest, doc-enriched, deduplication
func BuildSymbolContentDigest(input SymbolDigestInput) string {
	doc := strings.TrimSpace(NormalizeDoc(input.Doc))
	body := strings.TrimSpace(input.BodyDigest)
	name := strings.TrimSpace(input.Name)
	kind := strings.TrimSpace(input.Kind)
	symbolKey := strings.TrimSpace(input.SymbolKey)
	if symbolKey == "" {
		symbolKey = strings.TrimSpace(input.FilePath)
	}
	signature := strings.TrimSpace(input.Signature)
	model := strings.TrimSpace(input.Model)

	var builder strings.Builder
	builder.Grow(256)
	builder.WriteString("v2\n")
	builder.WriteString("model:")
	builder.WriteString(model)
	builder.WriteString("\nkind:")
	builder.WriteString(kind)
	builder.WriteString("\nname:")
	builder.WriteString(name)
	builder.WriteString("\nkey:")
	builder.WriteString(symbolKey)
	builder.WriteString("\nsignature:")
	builder.WriteString(signature)
	builder.WriteString("\ndoc:")
	builder.WriteString(doc)
	builder.WriteString("\nbody:")
	builder.WriteString(body)

	if len(input.Calls) > 0 {
		calls := sortDedupStrings(input.Calls)
		if len(calls) > 0 {
			builder.WriteString("\ncalls:")
			builder.WriteString(strings.Join(calls, ","))
		}
	}

	return DigestSHA256(builder.String())
}

// DigestSHA256Prefix returns the first n characters of the digest.
// Useful for shorter identifiers when full digest is not needed.
//
// Index:
//
//	Purpose: Generate short content identifier
//	Related: DigestSHA256
//	Keywords: short hash, prefix
func DigestSHA256Prefix(text string, n int) string {
	if n <= 0 {
		return ""
	}
	digest := DigestSHA256(text)
	if n > len(digest) {
		return digest
	}
	return digest[:n]
}

func sortDedupStrings(items []string) []string {
	if len(items) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(items))
	filtered := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		filtered = append(filtered, item)
	}
	sort.Strings(filtered)
	return filtered
}
