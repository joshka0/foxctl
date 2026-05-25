package annotations

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
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

func TestAnnotationReadsRejectCorruptTimestamps(t *testing.T) {
	ctx := context.Background()

	for _, column := range []string{"timestamp", "created_at", "updated_at"} {
		t.Run(column, func(t *testing.T) {
			store := openTestStore(t, ctx)
			ann := testTurn("corrupt-"+column, "s1", 1, "decision", []string{"a.go"}, nil)
			ann.Timestamp = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
			require.NoError(t, store.Save(ctx, ann))

			_, err := store.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE turn_annotations SET %s = ? WHERE id = ?`, column),
				"not-a-timestamp", ann.ID)
			require.NoError(t, err)

			_, err = store.Get(ctx, ann.ID)
			require.Error(t, err)
			require.Contains(t, err.Error(), column)
		})
	}
}

func TestAnnotationReadsRejectCorruptJSONColumns(t *testing.T) {
	ctx := context.Background()

	for _, column := range []string{"code_blocks", "commands", "errors", "file_paths", "symbols", "tools_used"} {
		t.Run(column, func(t *testing.T) {
			store := openTestStore(t, ctx)
			ann := testTurn("corrupt-"+column, "s1", 1, "decision", []string{"a.go"}, []any{"error"})
			ann.CodeBlocks = []any{map[string]any{"language": "go"}}
			ann.Commands = []any{map[string]any{"tool": "bash"}}
			ann.Symbols = []any{map[string]any{"name": "main"}}
			ann.ToolsUsed = []string{"bash"}
			require.NoError(t, store.Save(ctx, ann))

			_, err := store.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE turn_annotations SET %s = ? WHERE id = ?`, column),
				"{", ann.ID)
			require.NoError(t, err)

			_, err = store.Get(ctx, ann.ID)
			require.Error(t, err)
			require.Contains(t, err.Error(), column)
		})
	}
}

func TestAnnotationSaveRejectsNegativeSourceCoordinates(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		name   string
		mutate func(*storage.TurnAnnotation)
		want   string
	}{
		{name: "turn index", mutate: func(ann *storage.TurnAnnotation) { ann.TurnIndex = -1 }, want: "turn_index"},
		{name: "context window index", mutate: func(ann *storage.TurnAnnotation) { ann.ContextWindowIndex = -1 }, want: "context_window_index"},
		{name: "byte offset", mutate: func(ann *storage.TurnAnnotation) { ann.ByteOffset = -1 }, want: "byte_offset"},
		{name: "byte length", mutate: func(ann *storage.TurnAnnotation) { ann.ByteLength = -1 }, want: "byte_length"},
		{name: "line number", mutate: func(ann *storage.TurnAnnotation) { ann.LineNum = -1 }, want: "line_num"},
		{name: "pre compact tokens", mutate: func(ann *storage.TurnAnnotation) { ann.PreCompactTokens = -1 }, want: "pre_compact_tokens"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := openTestStore(t, ctx)
			ann := testTurn("invalid-"+strings.ReplaceAll(tt.want, "_", "-"), "s1", 1, "decision", []string{"a.go"}, nil)
			tt.mutate(ann)

			err := store.Save(ctx, ann)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)

			count, err := store.Count(ctx)
			require.NoError(t, err)
			require.Equal(t, 0, count)
		})
	}
}

func TestAnnotationReadsRejectCorruptInvariants(t *testing.T) {
	ctx := context.Background()

	for _, tt := range []struct {
		column string
		value  any
		want   string
	}{
		{column: "turn_index", value: -1, want: "turn_index"},
		{column: "context_window_index", value: -1, want: "context_window_index"},
		{column: "byte_offset", value: -1, want: "byte_offset"},
		{column: "byte_length", value: -1, want: "byte_length"},
		{column: "line_num", value: -1, want: "line_num"},
		{column: "pre_compact_tokens", value: -1, want: "pre_compact_tokens"},
		{column: "has_error", value: 2, want: "has_error"},
		{column: "is_compact_boundary", value: 2, want: "is_compact_boundary"},
		{column: "created_at", value: "", want: "created_at"},
		{column: "updated_at", value: "", want: "updated_at"},
	} {
		t.Run(tt.column, func(t *testing.T) {
			store := openTestStore(t, ctx)
			ann := testTurn("corrupt-"+tt.column, "s1", 1, "decision", []string{"a.go"}, nil)
			require.NoError(t, store.Save(ctx, ann))

			_, err := store.db.ExecContext(ctx, fmt.Sprintf(`
				UPDATE turn_annotations SET %s = ? WHERE id = ?`, tt.column),
				tt.value, ann.ID)
			require.NoError(t, err)

			_, err = store.Get(ctx, ann.ID)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)

			_, err = store.ListBySession(ctx, "s1")
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestValidateAnnotationInvariantsProperty(t *testing.T) {
	rejectsGeneratedNegativeCoordinates := func(raw uint16, selector uint8) bool {
		ann := testTurn("generated", "s1", 1, "decision", nil, nil)
		negative := -int(raw) - 1
		switch selector % 6 {
		case 0:
			ann.TurnIndex = negative
		case 1:
			ann.ContextWindowIndex = negative
		case 2:
			ann.ByteOffset = int64(negative)
		case 3:
			ann.ByteLength = int64(negative)
		case 4:
			ann.LineNum = negative
		default:
			ann.PreCompactTokens = negative
		}
		return validateAnnotationInvariants(ann) != nil
	}
	if err := quick.Check(rejectsGeneratedNegativeCoordinates, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("negative annotation coordinate property failed: %v", err)
	}

	acceptsGeneratedNonNegativeCoordinates := func(raw uint16) bool {
		value := int(raw)
		ann := testTurn("generated", "s1", value, "decision", nil, nil)
		ann.ContextWindowIndex = value
		ann.ByteOffset = int64(value)
		ann.ByteLength = int64(value)
		ann.LineNum = value
		ann.PreCompactTokens = value
		return validateAnnotationInvariants(ann) == nil
	}
	if err := quick.Check(acceptsGeneratedNonNegativeCoordinates, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("nonnegative annotation coordinate property failed: %v", err)
	}
}

func TestSummarizeByFilePathRejectsCorruptTimestamps(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t, ctx)

	ann := testTurn("summary-corrupt-time", "s1", 1, "decision", []string{"a.go"}, nil)
	ann.Timestamp = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	require.NoError(t, store.Save(ctx, ann))

	_, err := store.db.ExecContext(ctx, `
		UPDATE turn_annotations SET timestamp = ? WHERE id = ?`,
		"not-a-timestamp", ann.ID)
	require.NoError(t, err)

	_, err = store.SummarizeByFilePath(ctx, "a.go", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "first_seen")
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
