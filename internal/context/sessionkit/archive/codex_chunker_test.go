package archive

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/quick"
	"time"

	"github.com/joshka0/foxctl/internal/context/sessionkit/codexjsonl"
)

func TestChunkCodexHonorsMaxChunksForBatchedArchival(t *testing.T) {
	t.Parallel()

	result, err := ChunkCodexFromReader(strings.NewReader(codexJSONL(t, []codexjsonl.Message{
		codexMessage(t, "user", "one"),
		codexMessage(t, "assistant", "two"),
		codexMessage(t, "user", "three"),
	})), ChunkOptions{SessionID: "session-1", MaxChunks: 2})
	if err != nil {
		t.Fatalf("ChunkCodexFromReader() error = %v", err)
	}

	if !result.HasMore {
		t.Fatalf("HasMore = false, want true when MaxChunks stops before EOF")
	}
	if result.NextChunkIndex != 2 {
		t.Fatalf("NextChunkIndex = %d, want 2", result.NextChunkIndex)
	}
	if result.NextWindowIndex != 0 {
		t.Fatalf("NextWindowIndex = %d, want 0", result.NextWindowIndex)
	}
	if len(result.Chunks) != 2 {
		t.Fatalf("chunks = %d, want 2", len(result.Chunks))
	}
	for i, chunk := range result.Chunks {
		if chunk.SessionID != "session-1" {
			t.Fatalf("chunk[%d].SessionID = %q", i, chunk.SessionID)
		}
		if chunk.ChunkIndex != i {
			t.Fatalf("chunk[%d].ChunkIndex = %d, want %d", i, chunk.ChunkIndex, i)
		}
		if len(chunk.ContentHash) != 64 {
			t.Fatalf("chunk[%d].ContentHash length = %d, want 64", i, len(chunk.ContentHash))
		}
	}
	if len(result.Windows) != 0 {
		t.Fatalf("partial batch returned %d windows, want none before boundary/EOF", len(result.Windows))
	}
}

func TestChunkCodexBatchContinuationStartsAtRequestedChunkAndWindow(t *testing.T) {
	t.Parallel()

	input := codexJSONL(t, []codexjsonl.Message{
		codexMessage(t, "user", "one"),
		codexMessage(t, "assistant", "two"),
		codexMessage(t, "user", "three"),
	})
	first, err := ChunkCodexFromReader(strings.NewReader(input), ChunkOptions{SessionID: "session-1", MaxChunks: 2})
	if err != nil {
		t.Fatalf("first ChunkCodexFromReader() error = %v", err)
	}
	second, err := ChunkCodexFromReader(strings.NewReader(input), ChunkOptions{
		SessionID:        "session-1",
		SkipToChunk:      first.NextChunkIndex,
		StartWindowIndex: first.NextWindowIndex,
		MaxChunks:        2,
	})
	if err != nil {
		t.Fatalf("second ChunkCodexFromReader() error = %v", err)
	}

	if second.HasMore {
		t.Fatalf("second batch HasMore = true, want false at EOF")
	}
	if len(second.Chunks) != 1 {
		t.Fatalf("second batch chunks = %d, want 1", len(second.Chunks))
	}
	if second.Chunks[0].ChunkIndex != 2 {
		t.Fatalf("continued chunk index = %d, want 2", second.Chunks[0].ChunkIndex)
	}
	if len(second.Windows) != 1 {
		t.Fatalf("second batch windows = %d, want 1", len(second.Windows))
	}
	window := second.Windows[0]
	if window.WindowIndex != first.NextWindowIndex {
		t.Fatalf("window index = %d, want %d", window.WindowIndex, first.NextWindowIndex)
	}
	if window.ChunkStart != 2 || window.ChunkEnd != 2 || window.MessageCount != 1 {
		t.Fatalf("window range/count = %d-%d/%d, want 2-2/1", window.ChunkStart, window.ChunkEnd, window.MessageCount)
	}
}

func TestChunkCodexCreatesCompactBoundaryAndFinalWindows(t *testing.T) {
	t.Parallel()

	result, err := ChunkCodexFromReader(strings.NewReader(codexJSONL(t, []codexjsonl.Message{
		codexMessage(t, "user", "before compact"),
		codexCompactBoundary(t),
		codexMessage(t, "assistant", "after compact"),
	})), ChunkOptions{SessionID: "session-compact"})
	if err != nil {
		t.Fatalf("ChunkCodexFromReader() error = %v", err)
	}

	if result.HasMore {
		t.Fatalf("HasMore = true, want false")
	}
	if len(result.Chunks) != 3 {
		t.Fatalf("chunks = %d, want 3", len(result.Chunks))
	}
	if len(result.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(result.Windows))
	}
	first := result.Windows[0]
	if first.Trigger != "context_compacted" || first.ChunkStart != 0 || first.ChunkEnd != 1 || first.MessageCount != 2 {
		t.Fatalf("first window = trigger %q range %d-%d count %d", first.Trigger, first.ChunkStart, first.ChunkEnd, first.MessageCount)
	}
	second := result.Windows[1]
	if second.ChunkStart != 2 || second.ChunkEnd != 2 || second.MessageCount != 1 {
		t.Fatalf("second window = range %d-%d count %d, want 2-2 count 1", second.ChunkStart, second.ChunkEnd, second.MessageCount)
	}
}

func TestChunkCodexPropertyBatchSizeAndIndexes(t *testing.T) {
	t.Parallel()

	property := func(rawCount, rawLimit uint8) bool {
		count := int(rawCount%12) + 1
		limit := int(rawLimit%6) + 1
		messages := make([]codexjsonl.Message, count)
		for i := range messages {
			role := "user"
			if i%2 == 1 {
				role = "assistant"
			}
			messages[i] = codexMessage(t, role, strings.Repeat("x", i+1))
		}

		result, err := ChunkCodexFromReader(strings.NewReader(codexJSONL(t, messages)), ChunkOptions{
			SessionID: "property-session",
			MaxChunks: limit,
		})
		if err != nil {
			t.Logf("ChunkCodexFromReader error: %v", err)
			return false
		}
		wantChunks := count
		if limit < wantChunks {
			wantChunks = limit
		}
		if len(result.Chunks) != wantChunks {
			t.Logf("chunks=%d want %d count=%d limit=%d", len(result.Chunks), wantChunks, count, limit)
			return false
		}
		if result.HasMore != (count > limit) {
			t.Logf("HasMore=%v want %v count=%d limit=%d", result.HasMore, count > limit, count, limit)
			return false
		}
		for i, chunk := range result.Chunks {
			if chunk.ChunkIndex != i || chunk.SessionID != "property-session" || chunk.ContextWindowIndex != 0 {
				t.Logf("bad chunk[%d]=%+v", i, chunk)
				return false
			}
		}
		if result.HasMore {
			return result.NextChunkIndex == limit && result.NextWindowIndex == 0
		}
		return result.NextChunkIndex == 0 && result.NextWindowIndex == 0
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 200}); err != nil {
		t.Fatal(err)
	}
}

func TestFormatTimestamp(t *testing.T) {
	t.Parallel()

	if got := FormatTimestamp(time.Time{}); got != "" {
		t.Fatalf("FormatTimestamp(zero) = %q, want empty", got)
	}
	ts := time.Date(2026, 5, 25, 12, 34, 56, 0, time.UTC)
	if got := FormatTimestamp(ts); got != "2026-05-25T12:34:56Z" {
		t.Fatalf("FormatTimestamp() = %q", got)
	}
}

func codexJSONL(t *testing.T, messages []codexjsonl.Message) string {
	t.Helper()
	lines := make([]string, len(messages))
	for i, msg := range messages {
		lines[i] = string(mustArchiveJSON(t, msg))
	}
	return strings.Join(lines, "\n") + "\n"
}

func codexMessage(t *testing.T, role, text string) codexjsonl.Message {
	t.Helper()
	blockType := "input_text"
	if role == "assistant" {
		blockType = "output_text"
	}
	return codexjsonl.Message{
		Type:      "response_item",
		Timestamp: "2026-05-25T12:00:00Z",
		Payload: mustArchiveJSON(t, codexjsonl.ResponseItem{
			Type:    "message",
			Role:    role,
			Content: []codexjsonl.ContentBlock{{Type: blockType, Text: text}},
		}),
	}
}

func codexCompactBoundary(t *testing.T) codexjsonl.Message {
	t.Helper()
	return codexjsonl.Message{
		Type:      "event_msg",
		Timestamp: "2026-05-25T12:01:00Z",
		Payload:   mustArchiveJSON(t, map[string]any{"type": "context_compacted"}),
	}
}

func mustArchiveJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return data
}
