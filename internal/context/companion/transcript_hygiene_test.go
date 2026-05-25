package companion

import (
	"strings"
	"testing"
	"testing/quick"
)

func TestNormalizeTranscriptTurnText_DropsControlArtifacts(t *testing.T) {
	for _, input := range []string{
		"<subagent_notification>\n{\"agent_id\":\"x\"}",
		"<turn_aborted>\nThe user interrupted the previous turn.",
		"# AGENTS.md instructions for /tmp/repo",
	} {
		if got := NormalizeTranscriptTurnText(input); got != "" {
			t.Fatalf("NormalizeTranscriptTurnText(%q) = %q, want empty", input, got)
		}
	}
}

func TestTranscriptControlTextPropertyDropsGeneratedPrefixedArtifacts(t *testing.T) {
	t.Parallel()

	prefixes := []string{
		"<subagent_notification>",
		"<turn_aborted>",
		"# AGENTS.md instructions for ",
		"{\"status\":{",
		"{\"agent_id\":",
	}
	property := func(raw string, prefixSeed uint8) bool {
		prefix := prefixes[int(prefixSeed)%len(prefixes)]
		payload := "payload-" + strings.ToValidUTF8(raw, "\uFFFD")
		if len(payload) > 160 {
			payload = payload[:160]
			payload = strings.ToValidUTF8(payload, "\uFFFD")
		}
		input := " \n\t" + prefix + payload
		if !IsTranscriptControlText(input) {
			t.Logf("IsTranscriptControlText(%q)=false", input)
			return false
		}
		if got := NormalizeTranscriptTurnText(input); got != "" {
			t.Logf("NormalizeTranscriptTurnText(%q)=%q want empty", input, got)
			return false
		}
		return true
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 500}); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeTranscriptTurnText_SummarizesArticleBlob(t *testing.T) {
	input := `here's the article: <article>
Claude Code Dreams: Anthropic's Brand New Memory Consolidation Feature

Auto Dream consolidates memory files automatically, resolves contradictions,
converts relative dates, and runs every 24 hours plus 5 sessions.

Phase 1: Orientation
Phase 2: Gather Signal
Phase 3: Consolidation
</article>`

	got := NormalizeTranscriptTurnText(input)
	if got == "" {
		t.Fatal("expected summarized article text")
	}
	for _, want := range []string{
		"Reference document summary:",
		"Claude Code Dreams: Anthropic's Brand New Memory Consolidation Feature",
		"describes periodic memory consolidation",
		"resolves contradictions",
		"converts relative dates",
	} {
		if !containsFold(got, want) {
			t.Fatalf("summary=%q missing %q", got, want)
		}
	}
}

func TestExtractReferenceBlob_HandlesTruncatedArticleWrapper(t *testing.T) {
	input := "here's the article: <article>\n" + strings.Repeat("Claude Code Dreams. ", 80)

	got, ok := ExtractReferenceBlob(input)
	if !ok {
		t.Fatal("expected reference blob extraction")
	}
	if got == "" || got == input {
		t.Fatalf("got=%q want extracted article body", got)
	}
	if !containsFold(got, "Claude Code Dreams") {
		t.Fatalf("extracted blob=%q missing title", got)
	}
}

func TestExtractReferenceBlob_DoesNotMatchShortDiagnosticMentions(t *testing.T) {
	input := "I’m fixing handling for `<article>` without a closing tag."
	if got, ok := ExtractReferenceBlob(input); ok || got != "" {
		t.Fatalf("expected no extraction, got=%q ok=%v", got, ok)
	}
}

func TestNormalizeTranscriptTurnText_PreservesNormalConversation(t *testing.T) {
	input := "Should we do something like anchor_state_t + user_t -> assistant_t -> user_t+1?"
	if got := NormalizeTranscriptTurnText(input); got != input {
		t.Fatalf("NormalizeTranscriptTurnText() = %q want %q", got, input)
	}
}

func containsFold(haystack, needle string) bool {
	return len(needle) == 0 || (len(haystack) >= len(needle) && stringContainsFold(haystack, needle))
}

func stringContainsFold(s, substr string) bool {
Outer:
	for i := 0; i+len(substr) <= len(s); i++ {
		for j := range len(substr) {
			a := s[i+j]
			b := substr[j]
			if 'A' <= a && a <= 'Z' {
				a += 'a' - 'A'
			}
			if 'A' <= b && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				continue Outer
			}
		}
		return true
	}
	return false
}
