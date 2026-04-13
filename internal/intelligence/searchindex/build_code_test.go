package searchindex

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jkatigb/agentctl/internal/intelligence/indexing/codefilter"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/semantic"
	"github.com/jkatigb/agentctl/internal/intelligence/indexing/symbol"
	"github.com/jkatigb/agentctl/internal/storage"
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
}

func (f *fakeEmbeddingProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	f.singleCalls++
	return []float32{0.5}, nil
}

func (f *fakeEmbeddingProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	f.batchCalls++
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{float32(i + 1)}
	}
	return out, nil
}

func (f *fakeEmbeddingProvider) Model() string   { return "fake-batch-model" }
func (f *fakeEmbeddingProvider) Dimensions() int { return 1 }

var _ semantic.EmbeddingProvider = (*fakeEmbeddingProvider)(nil)

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
