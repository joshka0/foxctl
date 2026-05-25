package rlm

import (
	"strings"
	"testing"
	"testing/quick"
)

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

func TestSanitizeOutputTextStripsDelimitedToolPayloadFromVisibleAnswer(t *testing.T) {
	t.Parallel()

	got, info := SanitizeOutputText("Before\n<|tool_call>{\"cmd\":\"rm -rf /tmp/work\"}<tool_call|>\nAfter")
	if got != "Before\n\nAfter" {
		t.Fatalf("got=%q", got)
	}
	if strings.Contains(got, "rm -rf") || strings.Contains(got, "<|tool_call>") || strings.Contains(got, "<tool_call|>") {
		t.Fatalf("sanitized output leaked tool payload or markers: %q", got)
	}
	if !info.Changed {
		t.Fatal("Changed=false")
	}
}

func TestSanitizeOutputTextTruncatesUnclosedToolPayload(t *testing.T) {
	t.Parallel()

	got, info := SanitizeOutputText("Visible answer\n<|tool_call>{\"secret\":\"do not show\"}")
	if got != "Visible answer" {
		t.Fatalf("got=%q", got)
	}
	if strings.Contains(got, "do not show") || strings.Contains(got, "<|tool_call>") {
		t.Fatalf("sanitized output leaked unclosed tool payload: %q", got)
	}
	if !info.Changed {
		t.Fatal("Changed=false")
	}
}

func TestSanitizeOutputTextPropertyDelimitedToolPayloadNeverSurvives(t *testing.T) {
	t.Parallel()

	property := func(rawPrefix, rawPayload, rawSuffix string) bool {
		prefix := sanitizerVisibleText(rawPrefix)
		payload := "SECRET_TOOL_PAYLOAD:" + sanitizerVisibleText(rawPayload)
		suffix := sanitizerVisibleText(rawSuffix)
		response := prefix + "\n<|tool_call>" + payload + "<tool_call|>\n" + suffix

		got, _ := SanitizeOutputText(response)
		want := strings.TrimSpace(prefix + "\n\n" + suffix)
		return got == want &&
			!strings.Contains(got, "SECRET_TOOL_PAYLOAD") &&
			!strings.Contains(got, "<|tool_call>") &&
			!strings.Contains(got, "<tool_call|>")
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 300}); err != nil {
		t.Fatal(err)
	}
}

func TestDetectOutputArtifactsReportsUniqueLabels(t *testing.T) {
	t.Parallel()

	got := DetectOutputArtifacts("<|channel>thought<|channel>thought<channel|><channel|><|tool_call><|tool_call><tool_call|><tool_call|>")
	want := []string{
		"reasoning_channel_open_thought",
		"reasoning_channel_close",
		"tool_call_markup_open",
		"tool_call_markup_close",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got=%v want=%v", got, want)
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

func sanitizerVisibleText(raw string) string {
	replacer := strings.NewReplacer(
		"\r\n", "\n",
		"\r", "\n",
		"<|channel>thought", "",
		"<|channel>}", "",
		"<channel|>", "",
		"<|tool_call>", "",
		"<tool_call|>", "",
		"SECRET_TOOL_PAYLOAD", "",
	)
	return strings.TrimSpace(replacer.Replace(raw))
}
