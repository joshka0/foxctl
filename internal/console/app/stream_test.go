package consoleapp

import "testing"

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
