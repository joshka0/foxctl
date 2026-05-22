package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/repoindex"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semanticanchors"
	"github.com/joshka0/foxctl/internal/intelligence/searchindex"
)

func TestSemanticAnchorEnvelopeProviderBuildsSearchEnvelope(t *testing.T) {
	ctx := context.Background()
	workspace := t.TempDir()
	store, err := repoindex.Open(ctx, t.TempDir(), workspace)
	if err != nil {
		t.Fatalf("open repoindex: %v", err)
	}
	defer store.Close()

	repoKey := store.RepoKey()
	pkg := "go:demo"
	owner := repoindex.Node{
		ID:        repoindex.SymbolID(repoKey, pkg, "internal/demo.go:Guard"),
		Kind:      repoindex.NodeSymbol,
		Pkg:       pkg,
		File:      "internal/demo.go",
		Name:      "Guard",
		UpdatedAt: time.Now().UTC(),
	}
	resolution := semanticAnchorResolutionForEnvelopeTest(t, owner)
	edge, err := repoindex.NewSemanticAnchorEdge(resolution, semanticanchors.AnchorOwner{
		NodeID:    owner.ID,
		Kind:      string(owner.Kind),
		StableKey: "symbol:go:demo:Guard",
		Path:      owner.File,
		Name:      owner.Name,
	})
	if err != nil {
		t.Fatalf("new semantic edge: %v", err)
	}
	targetID, err := semanticanchors.AnchorTargetNodeID(repoKey, resolution.Occurrence.TargetID)
	if err != nil {
		t.Fatalf("target node id: %v", err)
	}
	target := repoindex.Node{ID: string(targetID), Kind: repoindex.NodeConcept, Name: "no-send-without-read", UpdatedAt: time.Now().UTC()}
	if err := store.ReplaceAll(ctx, []repoindex.Node{owner, target}, []repoindex.Edge{edge}); err != nil {
		t.Fatalf("replace all: %v", err)
	}

	provider := &SemanticAnchorEnvelopeProvider{Store: store}
	bits, err := provider.BuildCodeEnvelope(ctx, searchindex.CodeEnvelopeRequest{Document: searchindex.Document{
		Kind:       searchindex.KindSymbol,
		Path:       owner.File,
		SymbolName: owner.Name,
	}})
	if err != nil {
		t.Fatalf("BuildCodeEnvelope: %v", err)
	}
	if bits.ProviderVersion == "" || len(bits.TextSections) != 1 || len(bits.DigestParts) != 1 {
		t.Fatalf("unexpected bits: %+v", bits)
	}
	if bits.TextSections[0].Name != "semantic_anchor" {
		t.Fatalf("section name=%q want semantic_anchor", bits.TextSections[0].Name)
	}
	if bits.Metadata.OwnerNodeID != owner.ID {
		t.Fatalf("owner metadata=%#v want %q", bits.Metadata.OwnerNodeID, owner.ID)
	}
	if len(bits.Metadata.Anchors) != 1 {
		t.Fatalf("anchors=%#v want one anchor", bits.Metadata.Anchors)
	}

	bits, err = provider.BuildCodeEnvelope(ctx, searchindex.CodeEnvelopeRequest{Document: searchindex.Document{
		Kind:       searchindex.KindSymbol,
		Path:       "moved/locator.go",
		SymbolID:   repoIndexNodeScopedSymbolID(owner),
		SymbolName: "stale-name",
	}})
	if err != nil {
		t.Fatalf("BuildCodeEnvelope by symbol ref: %v", err)
	}
	if bits.Metadata.OwnerNodeID != owner.ID {
		t.Fatalf("symbol-ref owner metadata=%#v want %q", bits.Metadata.OwnerNodeID, owner.ID)
	}
}

func semanticAnchorResolutionForEnvelopeTest(t *testing.T, owner repoindex.Node) semanticanchors.AnchorResolution {
	t.Helper()
	policy := semanticanchors.DefaultAnchorPolicy("repo", nil)
	occ, findings := semanticanchors.ParseInlineAnchor(policy, "[[invariant:no-send-without-read]]")
	if len(findings) != 0 {
		t.Fatalf("parse findings: %+v", findings)
	}
	occ.Span = semanticanchors.SourceSpan{Path: owner.File, LineStart: 3, LineEnd: 3}
	occ.OwnerBinding = semanticanchors.AnchorOwnerBinding{
		OwnerNodeID:    owner.ID,
		OwnerKind:      string(owner.Kind),
		OwnerStableKey: "symbol:go:demo:Guard",
	}
	resolution, err := semanticanchors.ResolveAnchorOccurrence(context.Background(), policy, occ, nil)
	if err != nil {
		t.Fatalf("resolve anchor: %v", err)
	}
	return resolution
}
