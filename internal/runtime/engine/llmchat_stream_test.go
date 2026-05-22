package engine

import (
	"strings"
	"testing"
)

func TestLLMChatEngineParseSSEStreamAccumulatesSharedChunks(t *testing.T) {
	t.Parallel()

	input := strings.NewReader(`data: {"choices":[{"index":0,"delta":{"content":"Hello "},"finish_reason":""}]}
data: {"choices":[{"index":0,"delta":{"content":"world"},"finish_reason":""}]}
data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call-1","type":"function","function":{"name":"lookup","arguments":"{\"city\""}}]},"finish_reason":""}]}
data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":":\"Tallinn\"}"}}]},"finish_reason":"tool_calls"}]}
data: [DONE]
`)

	var deltas []StreamDelta
	resp, err := (&LLMChatEngine{}).parseSSEStream(input, StreamConfig{
		OnDelta: func(delta StreamDelta) {
			deltas = append(deltas, delta)
		},
	})
	if err != nil {
		t.Fatalf("parseSSEStream: %v", err)
	}

	if got, want := resp.content, "Hello world"; got != want {
		t.Fatalf("content=%q want %q", got, want)
	}
	if resp.finishReason != "tool_calls" {
		t.Fatalf("finish reason=%q", resp.finishReason)
	}
	if len(resp.toolCalls) != 1 {
		t.Fatalf("tool calls=%+v", resp.toolCalls)
	}
	call := resp.toolCalls[0]
	if call.ID != "call-1" || call.Name != "lookup" || string(call.Arguments) != `{"city":"Tallinn"}` {
		t.Fatalf("call=%+v", call)
	}
	if len(deltas) != 5 {
		t.Fatalf("deltas=%+v", deltas)
	}
	if deltas[2].ToolCallDelta == nil || deltas[2].ToolCallDelta.Name != "lookup" {
		t.Fatalf("tool delta=%+v", deltas[2])
	}
	if deltas[4].FinishReason != "tool_calls" {
		t.Fatalf("finish delta=%+v", deltas[4])
	}
}
