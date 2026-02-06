package companion

import "testing"

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
