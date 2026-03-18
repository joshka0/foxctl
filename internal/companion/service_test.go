package companion

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jkatigb/agentctl/internal/engine"
)

// TestStripThinkTags verifies <think> blocks are removed from model output.
func TestStripThinkTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no tags", "Hello world", "Hello world"},
		{"empty string", "", ""},
		{"only think block", "<think>reasoning</think>", ""},
		{"think then response", "<think>reasoning here</think>Hello!", "Hello!"},
		{"think then response with newline", "<think>reasoning</think>\nHello!", "Hello!"},
		{"unclosed think tag", "<think>partial reasoning", ""},
		{"text before unclosed think", "prefix <think>partial", "prefix"},
		{"text before and after think", "before <think>middle</think> after", "before  after"},
		{"multiple think blocks", "<think>one</think>Hi <think>two</think>there", "Hi there"},
		{"nested angle brackets in think", "<think>if a < b then c > d</think>Result", "Result"},
		{"think tag in middle", "Hello <think>reasoning</think>World", "Hello World"},
		{"no opening think tag", "reasoning here</think>Hello!", "Hello!"},
		{"no opening think tag with newline", "I should respond.\n</think>\nHello!", "Hello!"},
		{"no opening think tag multiline", "Let me think about this.\nThe user wants X.\n</think>Here is my response.", "Here is my response."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripThinkTags(tt.input)
			if got != tt.want {
				t.Errorf("stripThinkTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestShouldRetryGroundedTurn(t *testing.T) {
	tests := []struct {
		name            string
		enforceGrounded bool
		output          engine.EngineOutput
		responseText    string
		contextQueries  int
		want            bool
	}{
		{
			name:            "disabled policy never retries",
			enforceGrounded: false,
			output:          engine.EngineOutput{StopReason: engine.StopReasonError},
			responseText:    "",
			contextQueries:  0,
			want:            false,
		},
		{
			name:            "error retries",
			enforceGrounded: true,
			output:          engine.EngineOutput{StopReason: engine.StopReasonError},
			responseText:    "some text",
			contextQueries:  0,
			want:            true,
		},
		{
			name:            "empty response retries",
			enforceGrounded: true,
			output:          engine.EngineOutput{StopReason: engine.StopReasonEndTurn},
			responseText:    "",
			contextQueries:  1,
			want:            true,
		},
		{
			name:            "no tools and no context query retries",
			enforceGrounded: true,
			output:          engine.EngineOutput{StopReason: engine.StopReasonEndTurn},
			responseText:    "generic answer",
			contextQueries:  0,
			want:            true,
		},
		{
			name:            "tool-backed answer does not retry",
			enforceGrounded: true,
			output:          engine.EngineOutput{StopReason: engine.StopReasonEndTurn, ToolCalls: []engine.ToolCall{{Name: "context_search"}}},
			responseText:    "grounded answer",
			contextQueries:  1,
			want:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRetryGroundedTurn(tt.enforceGrounded, tt.output, tt.responseText, tt.contextQueries)
			if got != tt.want {
				t.Fatalf("shouldRetryGroundedTurn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldRecoverContextToolLeak(t *testing.T) {
	tests := []struct {
		name             string
		responseText     string
		rawAssistantText string
		calls            []engine.ToolCall
		want             bool
	}{
		{
			name:         "no context tools",
			responseText: "normal answer",
			calls:        []engine.ToolCall{{Name: "context_search"}},
			want:         false,
		},
		{
			name:         "raw tool call syntax leaks",
			responseText: `[rlm_context_list(),rlm_context_query(key="tech:owner")]<|tool_call_end|>`,
			calls:        nil,
			want:         true,
		},
		{
			name:         "context mutation json leaks",
			responseText: "{\"key\":\"tech:codename\",\"value\":\"amber-river-19\",\"scope\":\"global\"}",
			calls:        []engine.ToolCall{{Name: "rlm_context_put"}},
			want:         true,
		},
		{
			name:             "raw assistant marker leaks",
			responseText:     "placeholder",
			rawAssistantText: `[rlm_context_query(key="tech:owner")]<|tool_call_end|>`,
			calls:            nil,
			want:             true,
		},
		{
			name:         "natural answer with context tool calls is allowed",
			responseText: "{\"owner\":\"Mina\",\"codename\":\"amber-river-19\"}",
			calls:        []engine.ToolCall{{Name: "rlm_context_query"}},
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRecoverContextToolLeak(tt.responseText, tt.rawAssistantText, tt.calls)
			if got != tt.want {
				t.Fatalf("shouldRecoverContextToolLeak() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestedOutputFormat(t *testing.T) {
	tests := []struct {
		name        string
		question    string
		wantMode    requestedOutputFormatMode
		wantSnippet string
	}{
		{
			name:        "compact json",
			question:    "What is the current codename? Reply as compact JSON.",
			wantMode:    requestedOutputFormatCompactJSON,
			wantSnippet: "compact JSON object",
		},
		{
			name:        "reply only with token",
			question:    "Reply only with updated-codename",
			wantMode:    requestedOutputFormatOnlyValue,
			wantSnippet: "only the requested value or token",
		},
		{
			name:     "no special format",
			question: "Explain the current state.",
			wantMode: requestedOutputFormatNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotInstruction, gotMode := requestedOutputFormat(tt.question)
			if gotMode != tt.wantMode {
				t.Fatalf("requestedOutputFormat() mode = %v, want %v", gotMode, tt.wantMode)
			}
			if tt.wantSnippet != "" && !strings.Contains(gotInstruction, tt.wantSnippet) {
				t.Fatalf("requestedOutputFormat() instruction = %q, want snippet %q", gotInstruction, tt.wantSnippet)
			}
		})
	}
}

func TestRequestedResponseKeys(t *testing.T) {
	got := requestedResponseKeys(ChatRequest{
		ResponseKeys: []string{"owner", "codename", "deploy_window", "rollback_color"},
	})
	want := []string{"owner", "codename", "deploy_window", "rollback_color"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("requestedResponseKeys() = %v, want %v", got, want)
	}
}

func TestRequestedResponseKeys_FallsBackToSchema(t *testing.T) {
	got := requestedResponseKeys(ChatRequest{
		ResponseSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"owner":{"type":"string"},
				"codename":{"type":"string"},
				"deploy_window":{"type":"string"},
				"rollback_color":{"type":"string"}
			}
		}`),
	})
	want := []string{"codename", "deploy_window", "owner", "rollback_color"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("requestedResponseKeys(schema) = %v, want %v", got, want)
	}
}

func TestApplyRequestedOutputFormat(t *testing.T) {
	tests := []struct {
		name string
		text string
		mode requestedOutputFormatMode
		want string
	}{
		{
			name: "compact json object",
			text: "Here you go:\n{\"owner\":\"Mina\",\"codename\":\"amber-river-19\"}",
			mode: requestedOutputFormatCompactJSON,
			want: "{\"owner\":\"Mina\",\"codename\":\"amber-river-19\"}",
		},
		{
			name: "only value first line",
			text: "amber-river-19\nThis is the latest codename.",
			mode: requestedOutputFormatOnlyValue,
			want: "amber-river-19",
		},
		{
			name: "only value trims quotes",
			text: "\"updated-codename\"",
			mode: requestedOutputFormatOnlyValue,
			want: "updated-codename",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyRequestedOutputFormat(tt.text, tt.mode)
			if tt.mode == requestedOutputFormatCompactJSON {
				var gotJSON any
				var wantJSON any
				if err := json.Unmarshal([]byte(got), &gotJSON); err != nil {
					t.Fatalf("unmarshal got json: %v", err)
				}
				if err := json.Unmarshal([]byte(tt.want), &wantJSON); err != nil {
					t.Fatalf("unmarshal want json: %v", err)
				}
				gotBytes, _ := json.Marshal(gotJSON)
				wantBytes, _ := json.Marshal(wantJSON)
				if string(gotBytes) != string(wantBytes) {
					t.Fatalf("applyRequestedOutputFormat() json = %s, want %s", gotBytes, wantBytes)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("applyRequestedOutputFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}
