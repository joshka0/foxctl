package rlm

import "testing"

func TestSanitizeOutputTextReasoningChannel(t *testing.T) {
	t.Parallel()

	got, info := SanitizeOutputText("<|channel>thought\n<channel|>solution = 42")
	if got != "solution = 42" {
		t.Fatalf("got=%q", got)
	}
	if !info.Changed {
		t.Fatal("Changed=false")
	}
}

func TestSanitizeOutputTextToolCallOnly(t *testing.T) {
	t.Parallel()

	got, info := SanitizeOutputText(`<|tool_call>call:python_repl{}<tool_call|>`)
	if got != "" {
		t.Fatalf("got=%q want empty", got)
	}
	if !info.Changed {
		t.Fatal("Changed=false")
	}
}

func TestExtractSolutionLine(t *testing.T) {
	t.Parallel()

	got, ok := ExtractSolutionLine("Reasoning\nsolution = 1\nextra\nsolution = 42")
	if !ok {
		t.Fatal("ok=false")
	}
	if got != "solution = 42" {
		t.Fatalf("got=%q", got)
	}
}
