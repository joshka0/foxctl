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

	// Aliases lists normalized alternate forms used in embedding text.
	Aliases []string
}

type SemanticEnvelopeDigestInput struct {
	ProviderVersion                string
	IncludeCoChangeNeighborsInText bool
	Anchors                        []SemanticEnvelopeAnchorDigest
	TextSections                   []SemanticEnvelopeDigestSection
	Keywords                       []string
	DigestParts                    []string
	CoChangeNeighborPaths          []string
}

type SemanticEnvelopeAnchorDigest struct {
	TargetID         string
	Relation         string
	TargetType       string
	ValidationStatus string
}

type SemanticEnvelopeDigestSection struct {
	Name string
	Text string
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
	if len(input.Aliases) > 0 {
		aliases := sortDedupStrings(input.Aliases)
		if len(aliases) > 0 {
			builder.WriteString("\naliases:")
			builder.WriteString(strings.Join(aliases, ","))
		}
	}

	return DigestSHA256(builder.String())
}

// BuildSemanticEnvelopeContentDigest returns a stable digest for semantic
// envelope content. It intentionally excludes volatile line numbers,
// timestamps, commit hashes, scores, and freshness values.
// [[invariant:semantic-envelope-digest-excludes-volatile-metadata]]
// [[doc:docs/plans/features/semantic-code-anchors.md#Remaining Work]]
// [[test:internal/intelligence/indexing/embeddingtext/digest_test.go#TestBuildSemanticEnvelopeContentDigestStableAndMeaningful]]
func BuildSemanticEnvelopeContentDigest(input SemanticEnvelopeDigestInput) string {
	var builder strings.Builder
	builder.Grow(256)
	builder.WriteString("semantic-envelope-v1\n")
	builder.WriteString("provider:")
	builder.WriteString(strings.TrimSpace(input.ProviderVersion))
	builder.WriteString("\ninclude_cochange_text:")
	if input.IncludeCoChangeNeighborsInText {
		builder.WriteString("true")
	} else {
		builder.WriteString("false")
	}
	for _, anchor := range sortSemanticEnvelopeAnchors(input.Anchors) {
		builder.WriteString("\nanchor:")
		builder.WriteString(strings.Join([]string{
			strings.TrimSpace(anchor.TargetID),
			strings.TrimSpace(anchor.Relation),
			strings.TrimSpace(anchor.TargetType),
			strings.TrimSpace(anchor.ValidationStatus),
		}, "|"))
	}
	for _, section := range sortSemanticEnvelopeSections(input.TextSections) {
		builder.WriteString("\nsection:")
		builder.WriteString(section.Name)
		builder.WriteString("=")
		builder.WriteString(section.Text)
	}
	for _, part := range sortDedupStrings(input.DigestParts) {
		builder.WriteString("\npart:")
		builder.WriteString(part)
	}
	for _, keyword := range sortDedupStrings(input.Keywords) {
		builder.WriteString("\nkeyword:")
		builder.WriteString(keyword)
	}
	if input.IncludeCoChangeNeighborsInText {
		for _, path := range sortDedupStrings(input.CoChangeNeighborPaths) {
			builder.WriteString("\ncochange:")
			builder.WriteString(path)
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

func sortSemanticEnvelopeAnchors(items []SemanticEnvelopeAnchorDigest) []SemanticEnvelopeAnchorDigest {
	if len(items) == 0 {
		return nil
	}
	out := make([]SemanticEnvelopeAnchorDigest, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item.TargetID = strings.TrimSpace(item.TargetID)
		item.Relation = strings.TrimSpace(item.Relation)
		item.TargetType = strings.TrimSpace(item.TargetType)
		item.ValidationStatus = strings.TrimSpace(item.ValidationStatus)
		if item.TargetID == "" && item.Relation == "" && item.TargetType == "" && item.ValidationStatus == "" {
			continue
		}
		key := strings.Join([]string{item.TargetID, item.Relation, item.TargetType, item.ValidationStatus}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		a := strings.Join([]string{out[i].TargetID, out[i].Relation, out[i].TargetType, out[i].ValidationStatus}, "\x00")
		b := strings.Join([]string{out[j].TargetID, out[j].Relation, out[j].TargetType, out[j].ValidationStatus}, "\x00")
		return a < b
	})
	return out
}

func sortSemanticEnvelopeSections(items []SemanticEnvelopeDigestSection) []SemanticEnvelopeDigestSection {
	if len(items) == 0 {
		return nil
	}
	out := make([]SemanticEnvelopeDigestSection, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		item.Name = strings.TrimSpace(item.Name)
		item.Text = strings.TrimSpace(item.Text)
		if item.Name == "" || item.Text == "" {
			continue
		}
		key := item.Name + "\x00" + item.Text
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Text < out[j].Text
	})
	return out
}
