package codexjsonl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/quick"
)

func TestClassifyResponseItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		item ResponseItem
		want ChunkType
	}{
		{
			name: "user message",
			item: ResponseItem{Type: "message", Role: "user"},
			want: ChunkTypeUserRequest,
		},
		{
			name: "assistant message",
			item: ResponseItem{Type: "message", Role: "assistant"},
			want: ChunkTypeAssistantResponse,
		},
		{
			name: "function call",
			item: ResponseItem{Type: "function_call", Name: "Bash"},
			want: ChunkTypeToolUse,
		},
		{
			name: "custom tool call",
			item: ResponseItem{Type: "custom_tool_call", Name: "shell"},
			want: ChunkTypeToolUse,
		},
		{
			name: "successful function output",
			item: ResponseItem{Type: "function_call_output", Status: "ok"},
			want: ChunkTypeToolOutput,
		},
		{
			name: "successful custom output",
			item: ResponseItem{Type: "custom_tool_call_output", Status: " success "},
			want: ChunkTypeToolOutput,
		},
		{
			name: "uppercase success output",
			item: ResponseItem{Type: "function_call_output", Status: " OK "},
			want: ChunkTypeToolOutput,
		},
		{
			name: "failed function output",
			item: ResponseItem{Type: "function_call_output", Status: "failed"},
			want: ChunkTypeError,
		},
		{
			name: "unknown response item",
			item: ResponseItem{Type: "reasoning", Summary: "thinking"},
			want: ChunkTypeOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(responseItemMessage(t, tt.item)); got != tt.want {
				t.Fatalf("Classify() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompactBoundaryClassification(t *testing.T) {
	t.Parallel()

	if got := Classify(&Message{Type: "compacted"}); got != ChunkTypeCompactBoundary {
		t.Fatalf("Classify(compacted) = %q, want compact boundary", got)
	}

	msg := &Message{
		Type:    "event_msg",
		Payload: mustJSON(t, map[string]any{"type": "context_compacted"}),
	}
	kind, index, ok := IsCompactBoundary(msg)
	if !ok || kind != "context_compacted" || index != 0 {
		t.Fatalf("IsCompactBoundary() = (%q, %d, %v), want context_compacted boundary", kind, index, ok)
	}
	if got := Classify(msg); got != ChunkTypeCompactBoundary {
		t.Fatalf("Classify(context_compacted) = %q, want compact boundary", got)
	}
}

func TestToolErrorStatusProperty(t *testing.T) {
	t.Parallel()

	successes := map[string]bool{
		"ok":      true,
		"success": true,
	}
	property := func(raw string) bool {
		status := strings.TrimSpace(strings.ToLower(raw))
		got := isToolErrorStatus(raw)
		if status == "" || successes[status] {
			return !got
		}
		return got
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestExtractPreviewFiltersContentByRole(t *testing.T) {
	t.Parallel()

	user := responseItemMessage(t, ResponseItem{
		Type: "message",
		Role: "user",
		Content: []ContentBlock{
			{Type: "input_text", Text: "show me tests"},
			{Type: "output_text", Text: "assistant-only"},
			{Type: "text", Text: "include generic text"},
			{Type: "tool_result", Text: "secret tool output"},
		},
	})
	if got := ExtractPreview(user, 200); got != "show me tests\ninclude generic text" {
		t.Fatalf("user preview = %q", got)
	}

	assistant := responseItemMessage(t, ResponseItem{
		Type: "message",
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "input_text", Text: "user-only"},
			{Type: "output_text", Text: "here is the answer"},
			{Type: "text", Text: "include generic text"},
			{Type: "tool_result", Text: "skip tool output"},
		},
	})
	if got := ExtractPreview(assistant, 200); got != "here is the answer\ninclude generic text" {
		t.Fatalf("assistant preview = %q", got)
	}
}

func TestExtractPreviewPropertyNeverIncludesDisallowedBlockText(t *testing.T) {
	t.Parallel()

	property := func(raw string, assistant bool) bool {
		disallowed := "forbidden-" + raw
		role := "user"
		allowedType := "input_text"
		disallowedType := "output_text"
		if assistant {
			role = "assistant"
			allowedType = "output_text"
			disallowedType = "input_text"
		}

		msg := responseItemMessage(t, ResponseItem{
			Type: "message",
			Role: role,
			Content: []ContentBlock{
				{Type: allowedType, Text: "allowed"},
				{Type: disallowedType, Text: disallowed},
				{Type: "tool_result", Text: disallowed},
			},
		})
		preview := ExtractPreview(msg, DefaultMaxPreviewLen)
		return strings.Contains(preview, "allowed") && !strings.Contains(preview, disallowed)
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestExtractToolsOnlyReportsToolCalls(t *testing.T) {
	t.Parallel()

	for _, itemType := range []string{"function_call", "custom_tool_call"} {
		msg := responseItemMessage(t, ResponseItem{Type: itemType, Name: "Bash"})
		if got := ExtractTools(msg); len(got) != 1 || got[0] != "Bash" {
			t.Fatalf("ExtractTools(%s) = %#v, want Bash", itemType, got)
		}
	}

	output := responseItemMessage(t, ResponseItem{Type: "function_call_output", Name: "Bash"})
	if got := ExtractTools(output); got != nil {
		t.Fatalf("ExtractTools(output) = %#v, want nil", got)
	}
}

func TestEstimateTokensUsesMessageTextAndEventFallback(t *testing.T) {
	t.Parallel()

	message := responseItemMessage(t, ResponseItem{
		Type: "message",
		Role: "assistant",
		Content: []ContentBlock{
			{Type: "output_text", Text: strings.Repeat("a", 8)},
			{Type: "text", Text: strings.Repeat("b", 4)},
		},
	})
	if got := EstimateTokens(message); got != 3 {
		t.Fatalf("EstimateTokens(message) = %d, want 3", got)
	}

	event := &Message{
		Type:    "event_msg",
		Payload: mustJSON(t, map[string]any{"type": "user_message", "message": strings.Repeat("x", 12)}),
	}
	if got := EstimateTokens(event); got != 3 {
		t.Fatalf("EstimateTokens(event fallback) = %d, want 3", got)
	}
}

func TestReaderReadAllSkipsBlankAndMalformedLines(t *testing.T) {
	t.Parallel()

	validUser := string(mustJSON(t, Message{
		Type:    "response_item",
		Payload: mustJSON(t, ResponseItem{Type: "message", Role: "user"}),
	}))
	validTool := string(mustJSON(t, Message{
		Type:    "response_item",
		Payload: mustJSON(t, ResponseItem{Type: "function_call", Name: "Bash"}),
	}))
	reader := NewReader(strings.NewReader(validUser + "\n\nnot-json\n" + validTool + "\n"))

	messages, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("ReadAll() returned %d messages, want 2", len(messages))
	}
	if messages[0].LineNum != 1 || messages[1].LineNum != 4 {
		t.Fatalf("line numbers = %d, %d; want 1, 4", messages[0].LineNum, messages[1].LineNum)
	}
	if Classify(messages[0].Message) != ChunkTypeUserRequest || Classify(messages[1].Message) != ChunkTypeToolUse {
		t.Fatalf("unexpected classifications: %q, %q", Classify(messages[0].Message), Classify(messages[1].Message))
	}
}

func TestExtractMetadataFindsNestedCWDAndRepositoryURL(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	lines := []string{
		`{"type":"event_msg","payload":{"text":"Current working directory: /work/repo\nModel: x"}}`,
		`{"type":"event_msg","payload":{"metadata":{"repository_url":"https://example.test/repo.git"}}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatalf("write JSONL: %v", err)
	}

	meta, err := ExtractMetadata(path)
	if err != nil {
		t.Fatalf("ExtractMetadata() error = %v", err)
	}
	if meta.CWD != "/work/repo" {
		t.Fatalf("CWD = %q, want /work/repo", meta.CWD)
	}
	if meta.RepositoryURL != "https://example.test/repo.git" {
		t.Fatalf("RepositoryURL = %q", meta.RepositoryURL)
	}
}

func TestSessionIDFromFilename(t *testing.T) {
	t.Parallel()

	sessionID := "12345678-1234-1234-1234-123456789abc"
	path := "/home/dev/.codex/sessions/2026/05/25/rollout-2026-05-25T12-00-00-" + sessionID + ".jsonl"
	if got := SessionIDFromFilename(path); got != sessionID {
		t.Fatalf("SessionIDFromFilename() = %q, want %q", got, sessionID)
	}
	if got := SessionIDFromFilename("short.jsonl"); got != "short" {
		t.Fatalf("SessionIDFromFilename(short) = %q, want short", got)
	}
}

func responseItemMessage(t *testing.T, item ResponseItem) *Message {
	t.Helper()
	return &Message{
		Type:    "response_item",
		Payload: mustJSON(t, item),
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return data
}
