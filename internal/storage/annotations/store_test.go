package annotations

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/vector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchSimilarFiltered(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	require.NoError(t, store.Save(ctx, &storage.TurnAnnotation{
		ID:          "a1",
		SessionID:   "s1",
		TurnIndex:   1,
		ByteOffset:  0,
		ByteLength:  1,
		LineNum:     1,
		ChunkType:   "assistant_response",
		Role:        "assistant",
		ContentHash: "h1",
		TOCCategory: "decision",
		Errors:      []any{"error"},
		Embedding:   vector.SerializeF32([]float32{1, 0}),
	}))
	require.NoError(t, store.Save(ctx, &storage.TurnAnnotation{
		ID:          "a2",
		SessionID:   "s2",
		TurnIndex:   2,
		ByteOffset:  2,
		ByteLength:  1,
		LineNum:     2,
		ChunkType:   "assistant_response",
		Role:        "assistant",
		ContentHash: "h2",
		TOCCategory: "debug",
		Embedding:   vector.SerializeF32([]float32{0.9, 0.1}),
	}))
	require.NoError(t, store.Save(ctx, &storage.TurnAnnotation{
		ID:          "a3",
		SessionID:   "s1",
		TurnIndex:   3,
		ByteOffset:  3,
		ByteLength:  1,
		LineNum:     3,
		ChunkType:   "assistant_response",
		Role:        "assistant",
		ContentHash: "h3",
		TOCCategory: "decision",
		Embedding:   vector.SerializeF32([]float32{0, 1}),
	}))

	results, err := store.SearchSimilarFiltered(ctx, []float32{1, 0}, AnnotationSearchOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 3)
	assert.Equal(t, "a1", results[0].ID)

	decisionOnly, err := store.SearchSimilarFiltered(ctx, []float32{1, 0}, AnnotationSearchOptions{Limit: 10, TOCCategory: "decision"})
	require.NoError(t, err)
	require.Len(t, decisionOnly, 2)
	for _, ann := range decisionOnly {
		assert.Equal(t, "decision", ann.TOCCategory)
	}

	errorsOnly, err := store.SearchSimilarFiltered(ctx, []float32{1, 0}, AnnotationSearchOptions{Limit: 10, HasErrors: true})
	require.NoError(t, err)
	require.Len(t, errorsOnly, 1)
	assert.Equal(t, "a1", errorsOnly[0].ID)

	sessionScope, err := store.SearchSimilarFiltered(ctx, []float32{1, 0}, AnnotationSearchOptions{Limit: 10, SessionIDs: []string{"s2"}})
	require.NoError(t, err)
	require.Len(t, sessionScope, 1)
	assert.Equal(t, "s2", sessionScope[0].SessionID)
}

func TestListBySessionTurnRange(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	require.NoError(t, store.Save(ctx, testTurn("a1", "s1", 1, "debug", nil, nil)))
	require.NoError(t, store.Save(ctx, testTurn("a2", "s1", 2, "code_change", nil, nil)))
	require.NoError(t, store.Save(ctx, testTurn("a3", "s1", 3, "code_change", nil, nil)))
	require.NoError(t, store.Save(ctx, testTurn("a4", "s1", 4, "decision", nil, nil)))

	all, err := store.ListBySessionTurnRange(ctx, "s1", 1, 4, "", 10)
	require.NoError(t, err)
	require.Len(t, all, 3)
	assert.Equal(t, 2, all[0].TurnIndex)
	assert.Equal(t, 4, all[2].TurnIndex)

	codeChanges, err := store.ListBySessionTurnRange(ctx, "s1", 1, 4, "code_change", 10)
	require.NoError(t, err)
	require.Len(t, codeChanges, 2)
	assert.Equal(t, 2, codeChanges[0].TurnIndex)
	assert.Equal(t, 3, codeChanges[1].TurnIndex)
}

func TestListAndSummarizeByFilePath(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

	ann1 := testTurn("a1", "s1", 1, "decision", []string{"a.go"}, nil)
	ann1.Timestamp = t1
	require.NoError(t, store.Save(ctx, ann1))

	ann2 := testTurn("a2", "s1", 2, "debug", []string{"a.go", "b.go"}, nil)
	ann2.Timestamp = t2
	require.NoError(t, store.Save(ctx, ann2))

	ann3 := testTurn("a3", "s2", 1, "decision", []string{"dir/a.go"}, nil)
	ann3.Timestamp = t3
	require.NoError(t, store.Save(ctx, ann3))

	list, err := store.ListByFilePath(ctx, "a.go", nil, 10)
	require.NoError(t, err)
	require.Len(t, list, 2)
	assert.Equal(t, "s1", list[0].SessionID)
	assert.Equal(t, 2, list[0].TurnIndex)

	scoped, err := store.ListByFilePath(ctx, "a.go", []string{"s2"}, 10)
	require.NoError(t, err)
	assert.Len(t, scoped, 0)

	summaries, err := store.SummarizeByFilePath(ctx, "a.go", nil)
	require.NoError(t, err)
	require.Len(t, summaries, 1)
	assert.Equal(t, "s1", summaries[0].SessionID)
	assert.Equal(t, 2, summaries[0].TurnCount)
	assert.Equal(t, t1, summaries[0].FirstSeen)
	assert.Equal(t, t2, summaries[0].LastSeen)
	assert.True(t, strings.Contains(summaries[0].Categories, "decision"))
	assert.True(t, strings.Contains(summaries[0].Categories, "debug"))
}

func TestCountByCategory(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	require.NoError(t, store.Save(ctx, testTurn("a1", "s1", 1, "decision", nil, nil)))
	require.NoError(t, store.Save(ctx, testTurn("a2", "s1", 2, "decision", nil, nil)))
	require.NoError(t, store.Save(ctx, testTurn("a3", "s1", 3, "", nil, nil)))
	require.NoError(t, store.Save(ctx, testTurn("a4", "s2", 1, "debug", nil, nil)))

	counts, err := store.CountByCategory(ctx, nil)
	require.NoError(t, err)

	asMap := make(map[string]int, len(counts))
	for _, count := range counts {
		asMap[count.Category] = count.Count
	}
	assert.Equal(t, 2, asMap["decision"])
	assert.Equal(t, 1, asMap["debug"])
	assert.Equal(t, 1, asMap["context"])

	scoped, err := store.CountByCategory(ctx, []string{"s2"})
	require.NoError(t, err)
	require.Len(t, scoped, 1)
	assert.Equal(t, "debug", scoped[0].Category)
	assert.Equal(t, 1, scoped[0].Count)
}

func openTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "annotations.db")
	store, err := Open(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func testTurn(id, sessionID string, turn int, category string, filePaths []string, errors []any) *storage.TurnAnnotation {
	return &storage.TurnAnnotation{
		ID:          id,
		SessionID:   sessionID,
		TurnIndex:   turn,
		ByteOffset:  int64(turn),
		ByteLength:  1,
		LineNum:     turn,
		ChunkType:   "assistant_response",
		Role:        "assistant",
		ContentHash: id + "-hash",
		TOCCategory: category,
		FilePaths:   filePaths,
		Errors:      errors,
		Embedding:   vector.SerializeF32([]float32{1, 0}),
	}
}
