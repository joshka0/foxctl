package consoleapp

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/quick"

	"github.com/joshka0/foxctl/internal/providers/llmcompat"
)

func TestParseSSEStreamAccumulatesContentAndToolCall(t *testing.T) {
	t.Parallel()

	input := []byte(`data: {"choices":[{"index":0,"delta":{"content":"Hello "},"finish_reason":""}]}
data: {"choices":[{"index":0,"delta":{"content":"world"},"finish_reason":""}]}
data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"city\""}}]},"finish_reason":""}]}
data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Tallinn\"}"}}]},"finish_reason":"tool_calls"}]}
data: [DONE]
`)

	var deltas []StreamDelta
	acc, err := ParseSSEStream(input, func(delta StreamDelta) {
		deltas = append(deltas, delta)
	})
	if err != nil {
		t.Fatalf("ParseSSEStream: %v", err)
	}

	if got, want := acc.GetContent(), "Hello world"; got != want {
		t.Fatalf("content=%q want %q", got, want)
	}
	if acc.FinishReason != "tool_calls" {
		t.Fatalf("finish reason=%q", acc.FinishReason)
	}
	calls := acc.GetToolCalls()
	if len(calls) != 1 {
		t.Fatalf("tool calls=%+v", calls)
	}
	if calls[0].ID != "call-1" || calls[0].Name != "lookup" || calls[0].Arguments != `{"city":"Tallinn"}` {
		t.Fatalf("call=%+v", calls[0])
	}
	if len(deltas) != 4 {
		t.Fatalf("deltas=%+v", deltas)
	}
	if deltas[2].ToolCallDelta == nil || deltas[2].ToolCallDelta.Name != "lookup" {
		t.Fatalf("tool delta=%+v", deltas[2])
	}
}

func TestParseSSEStreamStopsAtDone(t *testing.T) {
	t.Parallel()

	var input bytes.Buffer
	writeContentChunk(t, &input, "before", "")
	input.WriteString("data: [DONE]\n")
	writeContentChunk(t, &input, "after", "")

	acc, err := ParseSSEStream(input.Bytes(), nil)
	if err != nil {
		t.Fatalf("ParseSSEStream: %v", err)
	}
	if got := acc.GetContent(); got != "before" {
		t.Fatalf("content after DONE was parsed: got %q", got)
	}
}

func TestParseSSEStreamPropertyContentChunksAccumulateInOrder(t *testing.T) {
	t.Parallel()

	property := func(rawA, rawB, rawC string) bool {
		parts := []string{
			streamTestContent(rawA),
			streamTestContent(rawB),
			streamTestContent(rawC),
		}

		var input bytes.Buffer
		for _, part := range parts {
			writeContentChunkForProperty(&input, part)
		}
		input.WriteString(":\n\n")
		input.WriteString("data: [DONE]\n")

		var deltas []StreamDelta
		acc, err := ParseSSEStream(input.Bytes(), func(delta StreamDelta) {
			deltas = append(deltas, delta)
		})
		if err != nil {
			t.Logf("ParseSSEStream error: %v", err)
			return false
		}
		if acc.GetContent() != strings.Join(parts, "") {
			t.Logf("content=%q want %q", acc.GetContent(), strings.Join(parts, ""))
			return false
		}
		if len(deltas) != len(parts) {
			t.Logf("deltas=%d want %d", len(deltas), len(parts))
			return false
		}
		for i, delta := range deltas {
			if delta.ContentDelta != parts[i] {
				t.Logf("delta[%d]=%q want %q", i, delta.ContentDelta, parts[i])
				return false
			}
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func writeContentChunk(t *testing.T, b *bytes.Buffer, content, finishReason string) {
	t.Helper()
	raw, err := json.Marshal(llmcompat.ChatCompletionStreamChunk{
		Choices: []llmcompat.ChatCompletionStreamChoice{{
			Delta:        llmcompat.ChatCompletionStreamDelta{Content: content},
			FinishReason: finishReason,
		}},
	})
	if err != nil {
		t.Fatalf("marshal stream chunk: %v", err)
	}
	b.WriteString("data: ")
	b.Write(raw)
	b.WriteByte('\n')
}

func writeContentChunkForProperty(b *bytes.Buffer, content string) {
	raw, err := json.Marshal(llmcompat.ChatCompletionStreamChunk{
		Choices: []llmcompat.ChatCompletionStreamChoice{{
			Delta: llmcompat.ChatCompletionStreamDelta{Content: content},
		}},
	})
	if err != nil {
		panic(err)
	}
	b.WriteString("data: ")
	b.Write(raw)
	b.WriteByte('\n')
}

func streamTestContent(raw string) string {
	raw = strings.ToValidUTF8(raw, "\uFFFD")
	if len(raw) > 80 {
		raw = raw[:80]
		raw = strings.ToValidUTF8(raw, "\uFFFD")
	}
	return raw
}
