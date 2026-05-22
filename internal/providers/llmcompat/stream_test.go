package llmcompat

import (
	"encoding/json"
	"testing"
)

func TestChatCompletionStreamChunkDecodesContentToolCallAndUsage(t *testing.T) {
	t.Parallel()

	var chunk ChatCompletionStreamChunk
	raw := []byte(`{
		"id":"chunk-1",
		"choices":[{
			"index":0,
			"delta":{
				"role":"assistant",
				"content":"Hello",
				"tool_calls":[{
					"index":0,
					"id":"call-1",
					"type":"function",
					"function":{"name":"lookup","arguments":"{\"q\":\"fox\"}"}
				}]
			},
			"finish_reason":"tool_calls"
		}],
		"usage":{"prompt_tokens":12,"completion_tokens":5}
	}`)
	if err := json.Unmarshal(raw, &chunk); err != nil {
		t.Fatalf("unmarshal stream chunk: %v", err)
	}

	if chunk.ID != "chunk-1" || len(chunk.Choices) != 1 {
		t.Fatalf("chunk=%+v", chunk)
	}
	choice := chunk.Choices[0]
	if choice.Delta.Content != "Hello" || choice.FinishReason != "tool_calls" {
		t.Fatalf("choice=%+v", choice)
	}
	if len(choice.Delta.ToolCalls) != 1 {
		t.Fatalf("tool calls=%+v", choice.Delta.ToolCalls)
	}
	call := choice.Delta.ToolCalls[0]
	if call.Index != 0 || call.ID != "call-1" || call.Function.Name != "lookup" || call.Function.Arguments != `{"q":"fox"}` {
		t.Fatalf("call=%+v", call)
	}
	if chunk.Usage == nil || chunk.Usage.PromptTokens != 12 || chunk.Usage.CompletionTokens != 5 {
		t.Fatalf("usage=%+v", chunk.Usage)
	}
}

func TestStreamDeltaJSONUsesSharedNormalizedShape(t *testing.T) {
	t.Parallel()

	delta := StreamDelta{
		ContentDelta: "Hello",
		ToolCallDelta: &ToolCallDelta{
			Index:          2,
			ID:             "call-2",
			Name:           "lookup",
			ArgumentsDelta: `{"q"`,
		},
		FinishReason: "tool_calls",
	}

	raw, err := json.Marshal(delta)
	if err != nil {
		t.Fatalf("marshal delta: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal delta map: %v", err)
	}
	for _, key := range []string{"content_delta", "tool_call_delta", "finish_reason"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing JSON key %q in %s", key, raw)
		}
	}
}
