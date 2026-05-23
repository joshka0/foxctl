package searchindex

import (
	"context"
	"testing"
)

func TestStoreLexicalRecall(t *testing.T) {
	testCtx := context.Background()

	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	baseWorkspace := "ws-alpha"

	if err := store.Upsert(testCtx, Document{
		ID:          "search://ws-alpha/symbol/one",
		WorkspaceID: baseWorkspace,
		Scope:       ScopeCode,
		Kind:        KindSymbol,
		GroupKey:    "pkg/service.go",
		Path:        "pkg/service.go",
		Title:       "authenticate",
		Summary:     "Authenticate users with token checks",
		SearchText:  "authenticate token login service",
		Keywords:    []string{"auth", "login", "token"},
	}); err != nil {
		t.Fatalf("upsert symbol doc: %v", err)
	}

	if err := store.Upsert(testCtx, Document{
		ID:          "search://ws-alpha/file/pkg/types.go",
		WorkspaceID: baseWorkspace,
		Scope:       ScopeCode,
		Kind:        KindFile,
		GroupKey:    "pkg/types.go",
		Path:        "pkg/types.go",
		Title:       "pkg/types.go",
		Summary:     "Type declarations and helper methods",
		SearchText:  "types helpers utility",
		Keywords:    []string{"type", "helper"},
	}); err != nil {
		t.Fatalf("upsert file doc: %v", err)
	}

	if err := store.Upsert(testCtx, Document{
		ID:          "search://ws-other/symbol/other",
		WorkspaceID: "other-workspace",
		Scope:       ScopeCode,
		Kind:        KindSymbol,
		GroupKey:    "pkg/other.go",
		Path:        "pkg/other.go",
		Title:       "authenticate",
		Summary:     "Different workspace noise",
		SearchText:  "authenticate token login",
	}); err != nil {
		t.Fatalf("upsert cross-workspace doc: %v", err)
	}

	hits, err := store.LexicalRecall(testCtx, baseWorkspace, "authenticate token", RecallOptions{Limit: 3, MinScore: 0.1})
	if err != nil {
		t.Fatalf("lexical recall: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 lexical hit, got %d", len(hits))
	}
	if hits[0].Doc.ID != "search://ws-alpha/symbol/one" {
		t.Fatalf("expected symbol hit first, got %s", hits[0].Doc.ID)
	}

	if hits, err := store.LexicalRecall(testCtx, baseWorkspace, "missing", RecallOptions{MinScore: 0.9}); err != nil {
		t.Fatalf("lexical recall missing query: %v", err)
	} else if len(hits) != 0 {
		t.Fatalf("expected zero hits with high min-score filter, got %d", len(hits))
	}
}

func TestStoreVectorRecall(t *testing.T) {
	testCtx := context.Background()

	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	workspace := "ws-vector"

	if err := store.Upsert(testCtx, Document{
		ID:             "search://ws-vector/symbol/a",
		WorkspaceID:    workspace,
		Scope:          ScopeCode,
		Kind:           KindSymbol,
		GroupKey:       "alpha.go",
		Path:           "alpha.go",
		Title:          "alpha",
		Summary:        "alpha document",
		SearchText:     "alpha function",
		Keywords:       []string{"alpha"},
		Embedding:      []float32{0.20, 0.90, 0},
		EmbeddingModel: "model-a",
	}); err != nil {
		t.Fatalf("upsert vector doc: %v", err)
	}

	if err := store.Upsert(testCtx, Document{
		ID:             "search://ws-vector/symbol/b",
		WorkspaceID:    workspace,
		Scope:          ScopeCode,
		Kind:           KindSymbol,
		GroupKey:       "beta.go",
		Path:           "beta.go",
		Title:          "beta",
		Summary:        "beta document",
		SearchText:     "beta function",
		Keywords:       []string{"beta"},
		Embedding:      []float32{0.90, 0.20, 0},
		EmbeddingModel: "model-a",
	}); err != nil {
		t.Fatalf("upsert vector doc: %v", err)
	}

	if err := store.Upsert(testCtx, Document{
		ID:          "search://ws-vector/symbol/c",
		WorkspaceID: workspace,
		Scope:       ScopeCode,
		Kind:        KindSymbol,
		GroupKey:    "gamma.go",
		Path:        "gamma.go",
		Title:       "gamma",
		Summary:     "gamma without embedding",
		SearchText:  "gamma function",
		Keywords:    []string{"gamma"},
	}); err != nil {
		t.Fatalf("upsert unembedded doc: %v", err)
	}

	query := []float32{0.2, 0.9, 0}
	hits, err := store.VectorRecall(testCtx, workspace, query, VectorRecallOptions{Limit: 2, EmbeddingModel: "model-a"})
	if err != nil {
		t.Fatalf("vector recall: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 vector hits, got %d", len(hits))
	}
	if hits[0].Doc.ID != "search://ws-vector/symbol/a" {
		t.Fatalf("expected alpha hit first, got %s", hits[0].Doc.ID)
	}

	_, err = store.VectorRecall(testCtx, workspace, []float32{0.9, 0.2, 0}, VectorRecallOptions{EmbeddingModel: "other-model"})
	if err == nil {
		t.Fatalf("expected model mismatch error")
	}

	_, err = store.VectorRecall(testCtx, workspace, []float32{1, 0}, VectorRecallOptions{})
	if err == nil {
		t.Fatalf("expected dimension mismatch error")
	}

	if _, err := store.VectorRecall(testCtx, workspace, []float32{}, VectorRecallOptions{}); err == nil {
		t.Fatalf("expected empty embedding error")
	}
}

func TestStoreWorkspaceStats(t *testing.T) {
	testCtx := context.Background()

	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	workspace := "ws-stats"
	if err := store.Upsert(testCtx, Document{
		ID:             "search://ws-stats/symbol/a",
		WorkspaceID:    workspace,
		Scope:          ScopeCode,
		Kind:           KindSymbol,
		GroupKey:       "alpha.go",
		Path:           "alpha.go",
		Title:          "alpha",
		Summary:        "alpha summary",
		SearchText:     "alpha function",
		Embedding:      []float32{0.20, 0.90, 0},
		EmbeddingModel: "model-a",
	}); err != nil {
		t.Fatalf("upsert vector doc: %v", err)
	}
	if err := store.Upsert(testCtx, Document{
		ID:          "search://ws-stats/file/beta.go",
		WorkspaceID: workspace,
		Scope:       ScopeCode,
		Kind:        KindFile,
		GroupKey:    "beta.go",
		Path:        "beta.go",
		Title:       "beta.go",
		Summary:     "beta summary",
		SearchText:  "beta file",
	}); err != nil {
		t.Fatalf("upsert file doc: %v", err)
	}

	stats, err := store.WorkspaceStats(testCtx, workspace)
	if err != nil {
		t.Fatalf("WorkspaceStats: %v", err)
	}
	if stats.WorkspaceID != workspace || stats.DocumentCount != 2 || stats.EmbeddedCount != 1 {
		t.Fatalf("stats=%+v want workspace=%s docs=2 embedded=1", stats, workspace)
	}
	if stats.EmbeddingMetadata == nil || stats.EmbeddingMetadata.Model != "model-a" || stats.EmbeddingMetadata.Dimensions != 3 {
		t.Fatalf("metadata=%+v want model-a/3", stats.EmbeddingMetadata)
	}
}

func TestStoreEmbeddingMetadataValidation(t *testing.T) {
	testCtx := context.Background()

	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() { _ = store.Close() }()

	workspace := "ws-vector-meta"
	if err := store.Upsert(testCtx, Document{
		ID:             "search://ws-vector-meta/symbol/a",
		WorkspaceID:    workspace,
		Scope:          ScopeCode,
		Kind:           KindSymbol,
		GroupKey:       "alpha.go",
		Path:           "alpha.go",
		Title:          "alpha",
		Summary:        "alpha document",
		SearchText:     "alpha function",
		Embedding:      []float32{0.1, 0.2, 0.3},
		EmbeddingModel: "model-a",
	}); err != nil {
		t.Fatalf("upsert vector doc: %v", err)
	}

	meta, err := store.GetEmbeddingMetadata(testCtx, workspace)
	if err != nil {
		t.Fatalf("GetEmbeddingMetadata: %v", err)
	}
	if meta == nil || meta.Model != "model-a" || meta.Dimensions != 3 {
		t.Fatalf("metadata=%+v", meta)
	}

	if err := store.Upsert(testCtx, Document{
		ID:             "search://ws-vector-meta/symbol/b",
		WorkspaceID:    workspace,
		Scope:          ScopeCode,
		Kind:           KindSymbol,
		GroupKey:       "beta.go",
		Path:           "beta.go",
		Title:          "beta",
		Summary:        "beta document",
		SearchText:     "beta function",
		Embedding:      []float32{0.1, 0.2},
		EmbeddingModel: "model-a",
	}); err == nil {
		t.Fatal("expected dimension mismatch error")
	}

	if err := store.Upsert(testCtx, Document{
		ID:             "search://ws-vector-meta/symbol/c",
		WorkspaceID:    workspace,
		Scope:          ScopeCode,
		Kind:           KindSymbol,
		GroupKey:       "gamma.go",
		Path:           "gamma.go",
		Title:          "gamma",
		Summary:        "gamma document",
		SearchText:     "gamma function",
		Embedding:      []float32{0.1, 0.2, 0.3},
		EmbeddingModel: "model-b",
	}); err == nil {
		t.Fatal("expected model mismatch error")
	}

	if _, err := store.VectorRecall(testCtx, workspace, []float32{1, 0}, VectorRecallOptions{EmbeddingModel: "model-a"}); err == nil {
		t.Fatal("expected vector recall dimension mismatch error")
	}
}

func TestStoreDeleteWorkspaceRemovesEmbeddingMetadata(t *testing.T) {
	testCtx := context.Background()

	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() { _ = store.Close() }()

	workspace := "ws-delete-meta"
	if err := store.Upsert(testCtx, Document{
		ID:             "search://ws-delete-meta/symbol/a",
		WorkspaceID:    workspace,
		Scope:          ScopeCode,
		Kind:           KindSymbol,
		GroupKey:       "alpha.go",
		Path:           "alpha.go",
		Title:          "alpha",
		Summary:        "alpha document",
		SearchText:     "alpha function",
		Embedding:      []float32{0.1, 0.2, 0.3},
		EmbeddingModel: "model-a",
	}); err != nil {
		t.Fatalf("upsert vector doc: %v", err)
	}

	if err := store.DeleteWorkspace(testCtx, workspace); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	meta, err := store.GetEmbeddingMetadata(testCtx, workspace)
	if err != nil {
		t.Fatalf("GetEmbeddingMetadata: %v", err)
	}
	if meta != nil {
		t.Fatalf("expected metadata to be removed, got %+v", meta)
	}
}

func TestStoreExactRecall(t *testing.T) {
	testCtx := context.Background()

	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() { _ = store.Close() }()

	workspace := "ws-exact"
	_ = store.Upsert(testCtx, Document{
		ID:          "search://ws-exact/symbol/login",
		WorkspaceID: workspace,
		Scope:       ScopeCode,
		Kind:        KindSymbol,
		GroupKey:    "auth/login.go",
		Path:        "auth/login.go",
		SymbolName:  "searchSymbolsWithRetrieval",
		Title:       "searchSymbolsWithRetrieval",
		Summary:     "exact symbol",
		SearchText:  "symbol body",
	})
	_ = store.Upsert(testCtx, Document{
		ID:          "search://ws-exact/file/login",
		WorkspaceID: workspace,
		Scope:       ScopeCode,
		Kind:        KindFile,
		GroupKey:    "auth/login.go",
		Path:        "auth/login.go",
		Title:       "auth/login.go",
		Summary:     "exact file",
		SearchText:  "file body",
	})

	hits, err := store.ExactRecall(testCtx, workspace, "searchSymbolsWithRetrieval", ExactRecallOptions{Limit: 5})
	if err != nil {
		t.Fatalf("exact recall: %v", err)
	}
	if len(hits) == 0 {
		t.Fatalf("expected exact hits")
	}
	if hits[0].Doc.SymbolName != "searchSymbolsWithRetrieval" {
		t.Fatalf("expected exact symbol first, got %#v", hits[0].Doc)
	}
}

func TestStoreExactRecall_StructuralQueryIgnoresGenericStructuralIdentifiers(t *testing.T) {
	testCtx := context.Background()

	store, err := Open(testCtx, t.TempDir())
	if err != nil {
		t.Fatalf("open search index: %v", err)
	}
	defer func() { _ = store.Close() }()

	workspace := "ws-struct"
	_ = store.Upsert(testCtx, Document{
		ID:          "search://ws-struct/symbol/dag",
		WorkspaceID: workspace,
		Scope:       ScopeCode,
		Kind:        KindSymbol,
		GroupKey:    "internal/runtime/orchestration/workflow/dag.go",
		Path:        "internal/runtime/orchestration/workflow/dag.go",
		SymbolName:  "DAG",
		Title:       "DAG",
		Summary:     "generic dag symbol",
		SearchText:  "generic dag symbol",
	})
	_ = store.Upsert(testCtx, Document{
		ID:          "search://ws-struct/symbol/real",
		WorkspaceID: workspace,
		Scope:       ScopeCode,
		Kind:        KindSymbol,
		GroupKey:    "internal/intelligence/indexing/repoindex/dag_grep.go",
		Path:        "internal/intelligence/indexing/repoindex/dag_grep.go",
		SymbolName:  "DAGGrepRequest",
		Title:       "DAGGrepRequest",
		Summary:     "repo index dag grep request",
		SearchText:  "repo index dag grep request",
	})

	hits, err := store.ExactRecall(testCtx, workspace, "repo index dag grep", ExactRecallOptions{Limit: 5})
	if err != nil {
		t.Fatalf("exact recall: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("expected no generic exact hits for structural phrase, got %d", len(hits))
	}
}
