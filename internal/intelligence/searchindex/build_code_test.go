package searchindex

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/codefilter"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semantic"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/semanticanchors"
	"github.com/joshka0/foxctl/internal/intelligence/indexing/symbol"
	"github.com/joshka0/foxctl/internal/storage"
)

type fakeBootstrapSource struct {
	items map[string][]storage.NamedEntry
	err   error

	errType string
}

func (f fakeBootstrapSource) ListByType(_ context.Context, _ string, entryType string, _ int) ([]storage.NamedEntry, error) {
	if f.errType == entryType {
		return nil, f.err
	}
	if f.items == nil {
		return nil, nil
	}
	return f.items[entryType], nil
}

type fakeEmbeddingProvider struct {
	batchCalls  int
	singleCalls int
	texts       []string
}

func (f *fakeEmbeddingProvider) Embed(_ context.Context, text string) ([]float32, error) {
	f.singleCalls++
	f.texts = append(f.texts, text)
	return []float32{0.5}, nil
}

func (f *fakeEmbeddingProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	f.batchCalls++
	f.texts = append(f.texts, texts...)
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i + 1)}
	}
	return out, nil
}

func (f *fakeEmbeddingProvider) Model() string   { return "fake-batch-model" }
func (f *fakeEmbeddingProvider) Dimensions() int { return 1 }

var _ semantic.EmbeddingProvider = (*fakeEmbeddingProvider)(nil)

type fakeCodeEnvelopeProvider struct {
	err      error
	requests []CodeEnvelopeRequest
}

func (f *fakeCodeEnvelopeProvider) BuildCodeEnvelope(_ context.Context, req CodeEnvelopeRequest) (SemanticEnvelopeBits, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return SemanticEnvelopeBits{}, f.err
	}
	return SemanticEnvelopeBits{
		ProviderVersion: "test-provider-v1",
		TextSections: []EnvelopeSection{
			{Name: "semantic_anchor", Text: "ENFORCES anchor:foxctl:invariant:no-send-without-read"},
		},
		Keywords:          []string{"read-before-write"},
		Metadata:          map[string]any{"anchor_count": 1},
		DigestParts:       []string{"anchor:foxctl:invariant:no-send-without-read", "ENFORCES"},
		CoChangeNeighbors: []string{"pkg/audit.go"},
	}, nil
}

func TestBuildCodeDocuments(t *testing.T) {
	testCtx := context.Background()

	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	validSymbol := storage.NamedEntry{
		Name:      "symbol://workspace/pkg/main.go:Authenticate",
		Type:      symbol.SymbolType,
		Workspace: "workspace",
		Summary:   "Authenticate user with token",
	}
	rawSymbol, err := json.Marshal(symbol.Result{Symbol: symbol.Symbol{
		ID:            symbol.ID("pkg/main.go", "Authenticate"),
		FilePath:      "pkg/main.go",
		Name:          "Authenticate",
		Language:      "go",
		Kind:          symbol.KindFunction,
		StartLine:     1,
		EndLine:       20,
		StartByte:     4,
		EndByte:       32,
		Signature:     "func Authenticate(ctx context.Context) error",
		Documentation: "Auth entrypoint.",
	}})
	if err != nil {
		t.Fatalf("marshal symbol payload: %v", err)
	}
	validSymbol.Result = rawSymbol

	validFileSummary := storage.NamedEntry{
		Name:      "file://workspace/pkg/main.go",
		Type:      symbol.FileSummaryType,
		Workspace: "workspace",
		Summary:   "File summary for login flow",
	}
	rawSummary, err := json.Marshal(symbol.FileSummaryResult{
		FilePath:  "pkg/main.go",
		Package:   "pkg",
		Symbols:   []string{"Authenticate"},
		Language:  "go",
		LineCount: 200,
	})
	if err != nil {
		t.Fatalf("marshal file summary payload: %v", err)
	}
	validFileSummary.Result = rawSummary

	legacyPathSummary := storage.NamedEntry{
		Name:      "file://workspace/pkg/legacy.go",
		Type:      symbol.FileSummaryType,
		Workspace: "workspace",
		Summary:   "Legacy style file summary",
	}
	rawLegacySummary, err := json.Marshal(symbol.FileSummaryResult{
		FilePath:  "",
		Package:   "pkg",
		Symbols:   []string{"Legacy"},
		Language:  "go",
		LineCount: 120,
	})
	if err != nil {
		t.Fatalf("marshal legacy file summary payload: %v", err)
	}
	legacyPathSummary.Result = rawLegacySummary

	source := fakeBootstrapSource{
		items: map[string][]storage.NamedEntry{
			symbol.SymbolType: {
				validSymbol,
				{Workspace: "workspace", Type: symbol.SymbolType, Name: "symbol://workspace/pkg/main.go:bad", Result: []byte("not-json")},
			},
			symbol.FileSummaryType: {
				validFileSummary,
				legacyPathSummary,
			},
		},
	}

	result, err := BuildCodeDocuments(testCtx, source, store, "workspace", BuildCodeOptions{Limit: 12})
	if err != nil {
		t.Fatalf("build code documents: %v", err)
	}

	if got, want := result.SymbolFetched, 2; got != want {
		t.Fatalf("symbol fetched mismatch: got=%d want=%d", got, want)
	}
	if got, want := result.SymbolBuilt, 1; got != want {
		t.Fatalf("symbol built mismatch: got=%d want=%d", got, want)
	}
	if got, want := result.FileFetched, 2; got != want {
		t.Fatalf("file fetched mismatch: got=%d want=%d", got, want)
	}
	if got, want := result.FileBuilt, 2; got != want {
		t.Fatalf("file built mismatch: got=%d want=%d", got, want)
	}
	if got, want := result.Upserted, 3; got != want {
		t.Fatalf("upserted mismatch: got=%d want=%d", got, want)
	}
	if got, want := result.Skipped, 1; got != want {
		t.Fatalf("skipped mismatch: got=%d want=%d", got, want)
	}
	if got, want := result.Errors, 1; got != want {
		t.Fatalf("errors mismatch: got=%d want=%d", got, want)
	}

	hits, err := store.LexicalRecall(testCtx, "workspace", "Authenticate", RecallOptions{})
	if err != nil {
		t.Fatalf("lexical recall: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 built documents for authenticate query, got %d", len(hits))
	}
}

func TestDocumentFromSymbolUsesPackageScopedSymbolRef(t *testing.T) {
	raw, err := json.Marshal(symbol.Result{Symbol: symbol.Symbol{
		ID:       symbol.ID("pkg/a/foo.go", "Helper"),
		FilePath: "pkg/a/foo.go",
		Name:     "Helper",
		Language: "go",
		Kind:     symbol.KindFunction,
		Key:      symbol.GoSymbolKey("Helper"),
	}})
	if err != nil {
		t.Fatalf("marshal symbol: %v", err)
	}
	doc, ok := documentFromSymbol(storage.NamedEntry{
		Name:      "symbol://workspace/go:pkg/a::Helper",
		Type:      symbol.SymbolType,
		Workspace: "workspace",
		Result:    raw,
	})
	if !ok {
		t.Fatal("documentFromSymbol returned false")
	}
	if doc.SymbolID != "go:pkg/a::Helper" {
		t.Fatalf("SymbolID=%q want package-scoped symbol ref", doc.SymbolID)
	}
	if doc.Metadata["symbol_key"] != "Helper" || doc.Metadata["symbol_ref"] != "go:pkg/a::Helper" {
		t.Fatalf("metadata=%#v", doc.Metadata)
	}
}

func TestBuildCodeDocumentsBatchesEmbeddings(t *testing.T) {
	testCtx := context.Background()
	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() { _ = store.Close() }()

	makeSymbolEntry := func(path, name string) storage.NamedEntry {
		raw, err := json.Marshal(symbol.Result{Symbol: symbol.Symbol{
			ID:       symbol.ID(path, name),
			FilePath: path,
			Name:     name,
			Language: "go",
			Kind:     symbol.KindFunction,
		}})
		if err != nil {
			t.Fatalf("marshal symbol payload: %v", err)
		}
		return storage.NamedEntry{
			Name:      "symbol://workspace/" + path + ":" + name,
			Type:      symbol.SymbolType,
			Workspace: "workspace",
			Summary:   "summary " + name,
			Result:    raw,
		}
	}

	source := fakeBootstrapSource{
		items: map[string][]storage.NamedEntry{
			symbol.SymbolType: {
				makeSymbolEntry("pkg/a.go", "Alpha"),
				makeSymbolEntry("pkg/b.go", "Beta"),
			},
		},
	}
	provider := &fakeEmbeddingProvider{}
	result, err := BuildCodeDocuments(testCtx, source, store, "workspace", BuildCodeOptions{
		EmbedProvider: provider,
	})
	if err != nil {
		t.Fatalf("build code documents: %v", err)
	}
	if result.Upserted != 2 {
		t.Fatalf("upserted = %d, want 2", result.Upserted)
	}
	if provider.batchCalls == 0 {
		t.Fatalf("expected EmbedBatch to be used")
	}
	if provider.singleCalls != 0 {
		t.Fatalf("expected no per-document fallback embeds, got %d", provider.singleCalls)
	}
}

func TestBuildCodeDocumentsAppliesSemanticEnvelopeProvider(t *testing.T) {
	testCtx := context.Background()
	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() { _ = store.Close() }()

	source := fakeBootstrapSource{items: map[string][]storage.NamedEntry{
		symbol.SymbolType: {makeSearchIndexSymbolEntry(t, "pkg/main.go", "Guard")},
	}}
	provider := &fakeCodeEnvelopeProvider{}
	embedder := &fakeEmbeddingProvider{}

	result, err := BuildCodeDocuments(testCtx, source, store, "workspace", BuildCodeOptions{
		EmbedProvider:    embedder,
		EnvelopeProvider: provider,
		EmbedBatchSize:   1,
	})
	if err != nil {
		t.Fatalf("build code documents: %v", err)
	}
	if result.Upserted != 1 {
		t.Fatalf("upserted=%d want 1", result.Upserted)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("provider requests=%d want 1", len(provider.requests))
	}
	if len(embedder.texts) != 1 || !strings.Contains(embedder.texts[0], "no-send-without-read") {
		t.Fatalf("embedding text did not include semantic section: %#v", embedder.texts)
	}
	if strings.Contains(embedder.texts[0], "pkg/audit.go") {
		t.Fatalf("co-change neighbor entered embedding text by default: %q", embedder.texts[0])
	}

	hits, err := store.LexicalRecall(testCtx, "workspace", "no-send-without-read", RecallOptions{Limit: 5})
	if err != nil {
		t.Fatalf("lexical recall: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("semantic anchor text not searchable, hits=%d", len(hits))
	}
	doc := hits[0].Doc
	if doc.Metadata[metadataKeySemanticEnvelope] == nil {
		t.Fatalf("missing semantic envelope metadata: %#v", doc.Metadata)
	}
	if doc.Metadata[metadataKeyCoChangeNeighbors] == nil {
		t.Fatalf("missing metadata-only cochange neighbors: %#v", doc.Metadata)
	}
}

func TestBuildCodeDocumentsCanIncludeCoChangeInEmbeddingTextExplicitly(t *testing.T) {
	testCtx := context.Background()
	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() { _ = store.Close() }()

	embedder := &fakeEmbeddingProvider{}
	provider := &fakeCodeEnvelopeProvider{}
	_, err = BuildCodeDocuments(testCtx, fakeBootstrapSource{items: map[string][]storage.NamedEntry{
		symbol.SymbolType: {makeSearchIndexSymbolEntry(t, "pkg/main.go", "Guard")},
	}}, store, "workspace", BuildCodeOptions{
		EmbedProvider:                      embedder,
		EnvelopeProvider:                   provider,
		IncludeCoChangeNeighborsInEnvelope: true,
	})
	if err != nil {
		t.Fatalf("build code documents: %v", err)
	}
	if len(embedder.texts) != 1 || !strings.Contains(embedder.texts[0], "pkg/audit.go") {
		t.Fatalf("co-change neighbor missing from explicit embedding text: %#v", embedder.texts)
	}
	if len(provider.requests) != 1 || !provider.requests[0].IncludeCoChangeNeighborsInEnvelope {
		t.Fatalf("provider did not receive explicit cochange flag: %#v", provider.requests)
	}
}

func TestSemanticEnvelopeDigestTracksStableAnchorContractParts(t *testing.T) {
	base := SemanticEnvelopeBits{
		ProviderVersion: "semantic-anchors-v1",
		TextSections: []EnvelopeSection{
			{Name: "test_target", Text: "internal/intelligence/searchindex/build_code_test.go#TestBuildCodeDocumentsAppliesSemanticEnvelopeProvider"},
			{Name: "doc_target", Text: "docs/plans/features/semantic-code-anchors.md#Remaining Work"},
		},
		DigestParts: []string{
			"anchor:repo:protocol:semantic-envelope-retrieval-evidence",
			"IMPLEMENTS_PROTOCOL",
			"cap:include_cochange_text=false",
		},
	}
	doc := applySemanticEnvelope(Document{SearchText: "base"}, base, BuildCodeOptions{})
	envelope, ok := doc.Metadata[metadataKeySemanticEnvelope].(map[string]any)
	if !ok {
		t.Fatalf("missing semantic envelope metadata: %#v", doc.Metadata)
	}
	digest, _ := envelope["digest"].(string)
	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("digest=%q", digest)
	}
	if envelope["provider_version"] != "semantic-anchors-v1" {
		t.Fatalf("provider_version=%#v", envelope["provider_version"])
	}
	if envelope["include_cochange_text"] != false || envelope["cochange_metadata_only"] != true {
		t.Fatalf("unexpected cochange flags: %#v", envelope)
	}
	if sections := envelopeTextSectionsFromMetadata(doc.Metadata); len(sections) != 2 {
		t.Fatalf("sections=%#v want doc/test sections", sections)
	}

	changedProvider := base
	changedProvider.ProviderVersion = "semantic-anchors-v2"
	changedDoc := applySemanticEnvelope(Document{SearchText: "base"}, changedProvider, BuildCodeOptions{})
	changedEnvelope := changedDoc.Metadata[metadataKeySemanticEnvelope].(map[string]any)
	if changedEnvelope["digest"] == digest {
		t.Fatal("digest did not change after provider version changed")
	}

	changedCap := base
	withCoChange := applySemanticEnvelope(Document{SearchText: "base"}, changedCap, BuildCodeOptions{IncludeCoChangeNeighborsInEnvelope: true})
	withCoChangeEnvelope := withCoChange.Metadata[metadataKeySemanticEnvelope].(map[string]any)
	if withCoChangeEnvelope["digest"] == digest {
		t.Fatal("digest did not change after co-change text cap changed")
	}
}

func TestSemanticSourceAnchorsDoNotAliasRecallAnchors(t *testing.T) {
	if reflect.TypeOf(Anchor{}) == reflect.TypeOf(semanticanchors.AnchorOccurrence{}) {
		t.Fatal("semantic anchor occurrence aliases searchindex recall Anchor")
	}
	if reflect.TypeOf(AnchorType("")) == reflect.TypeOf(semanticanchors.AnchorType("")) {
		t.Fatal("semantic anchor type aliases searchindex AnchorType")
	}
}

func TestBuildCodeDocumentsEnvelopeProviderErrorKeepsPlainDocument(t *testing.T) {
	testCtx := context.Background()
	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() { _ = store.Close() }()

	result, err := BuildCodeDocuments(testCtx, fakeBootstrapSource{items: map[string][]storage.NamedEntry{
		symbol.SymbolType: {makeSearchIndexSymbolEntry(t, "pkg/main.go", "Guard")},
	}}, store, "workspace", BuildCodeOptions{
		EnvelopeProvider: &fakeCodeEnvelopeProvider{err: errors.New("unavailable")},
	})
	if err != nil {
		t.Fatalf("build code documents: %v", err)
	}
	if result.Errors != 0 || result.Upserted != 1 {
		t.Fatalf("result=%+v, want graceful plain document", result)
	}
	hits, err := store.LexicalRecall(testCtx, "workspace", "Guard", RecallOptions{Limit: 5})
	if err != nil {
		t.Fatalf("lexical recall: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits=%d want 1", len(hits))
	}
	if _, ok := hits[0].Doc.Metadata[metadataKeySemanticEnvelope]; ok {
		t.Fatalf("semantic envelope metadata present after provider error: %#v", hits[0].Doc.Metadata)
	}
	if hits[0].Doc.Metadata["symbol_key"] == "" {
		t.Fatalf("missing base symbol metadata after provider error: %#v", hits[0].Doc.Metadata)
	}
}

func TestBuildCodeDocumentsValidateInputs(t *testing.T) {
	testCtx := context.Background()
	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	if _, err := BuildCodeDocuments(testCtx, nil, store, "workspace", BuildCodeOptions{}); err == nil {
		t.Fatalf("expected missing source error")
	}

	if _, err := BuildCodeDocuments(testCtx, fakeBootstrapSource{}, nil, "workspace", BuildCodeOptions{}); err == nil {
		t.Fatalf("expected missing index error")
	}
}

func TestBuildCodeDocumentsPropagatesSourceError(t *testing.T) {
	testCtx := context.Background()
	sourceErr := context.Canceled

	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	source := fakeBootstrapSource{errType: symbol.SymbolType, err: sourceErr}
	if _, err := BuildCodeDocuments(testCtx, source, store, "workspace", BuildCodeOptions{}); err == nil {
		t.Fatalf("expected source error")
	}
}

func TestShouldSkipCodePath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "internal/app/service.go", want: false},
		{path: "internal/app/service_test.go", want: true},
		{path: "test/integration/foo.go", want: true},
		{path: "pkg/testdata/input.go", want: true},
		{path: "pkg/fixtures/input.go", want: true},
		{path: "pkg/golden/input.go", want: true},
		{path: "pkg/foo.spec.ts", want: true},
		{path: "pkg/foo.test.ts", want: true},
	}
	for _, tt := range tests {
		if got := codefilter.ShouldSkipPath(tt.path); got != tt.want {
			t.Fatalf("ShouldSkipPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func makeSearchIndexSymbolEntry(t *testing.T, path, name string) storage.NamedEntry {
	t.Helper()
	raw, err := json.Marshal(symbol.Result{Symbol: symbol.Symbol{
		ID:       symbol.ID(path, name),
		FilePath: path,
		Name:     name,
		Language: "go",
		Kind:     symbol.KindFunction,
	}})
	if err != nil {
		t.Fatalf("marshal symbol payload: %v", err)
	}
	return storage.NamedEntry{
		Name:      "symbol://workspace/" + path + ":" + name,
		Type:      symbol.SymbolType,
		Workspace: "workspace",
		Summary:   "summary " + name,
		Result:    raw,
	}
}
