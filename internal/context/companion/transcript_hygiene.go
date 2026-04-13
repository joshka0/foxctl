package companion

import "strings"

const (
	TranscriptArtifactKindNone          = ""
	TranscriptArtifactKindReferenceBlob = "reference_blob"
)

// NormalizeTranscriptTurnText removes transcript control artifacts and compacts
// large pasted reference blobs into short deterministic summaries before they
// enter memory derivation.
func NormalizeTranscriptTurnText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if IsTranscriptControlText(text) {
		return ""
	}
	if normalized, ok := ExtractReferenceBlob(text); ok {
		summary := SummarizeReferenceBlobDeterministic(normalized)
		return summary
	}
	return text
}

// IsTranscriptControlText returns true for transcript control/meta content that
// should not be treated as conversational memory.
func IsTranscriptControlText(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return true
	}
	for _, prefix := range []string{
		"<subagent_notification>",
		"<turn_aborted>",
		"# AGENTS.md instructions for ",
		"{\"status\":{",
		"{\"agent_id\":",
	} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

// DetectTranscriptArtifactKind identifies cache-worthy transcript artifacts.
func DetectTranscriptArtifactKind(text string) string {
	if _, ok := ExtractReferenceBlob(text); ok {
		return TranscriptArtifactKindReferenceBlob
	}
	return TranscriptArtifactKindNone
}

// ExtractReferenceBlob returns normalized reference content suitable for
// summary caching when the input looks like a pasted article or long design blob.
func ExtractReferenceBlob(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}

	if strings.Contains(text, "<article>") && strings.Contains(text, "</article>") {
		article := strings.TrimSpace(between(text, "<article>", "</article>"))
		if article != "" {
			return article, true
		}
	}
	if len(text) > 500 && strings.Contains(text, "<article>") {
		article := strings.TrimSpace(after(text, "<article>"))
		if article != "" {
			return article, true
		}
	}

	if len(text) > 2200 && (strings.Contains(text, "Phase 1:") || strings.Contains(text, "## Phase 1")) {
		return text, true
	}

	return "", false
}

// SummarizeReferenceBlobDeterministic produces a stable deterministic summary
// for large pasted reference content.
func SummarizeReferenceBlobDeterministic(normalizedText string) string {
	normalizedText = strings.TrimSpace(normalizedText)
	if normalizedText == "" {
		return ""
	}

	title := nonEmptyFirstLine(normalizedText)
	if title == "" {
		title = "reference article"
	}
	summaryParts := []string{title}

	lower := strings.ToLower(normalizedText)
	if strings.Contains(lower, "auto dream") || strings.Contains(lower, "auto-memory") || strings.Contains(lower, "auto memory") {
		summaryParts = append(summaryParts, "describes periodic memory consolidation")
	}
	if strings.Contains(lower, "contradiction") || strings.Contains(lower, "contradictions") {
		summaryParts = append(summaryParts, "resolves contradictions")
	}
	if strings.Contains(lower, "relative dates") || strings.Contains(lower, "\"yesterday\"") {
		summaryParts = append(summaryParts, "converts relative dates to durable time references")
	}
	if strings.Contains(lower, "phase 1") && strings.Contains(lower, "phase 2") && strings.Contains(lower, "phase 3") {
		summaryParts = append(summaryParts, "uses a multi-phase gather and consolidate loop")
	}
	if strings.Contains(lower, "24 hours") || strings.Contains(lower, "5 sessions") {
		summaryParts = append(summaryParts, "runs periodically rather than every turn")
	}
	if len(summaryParts) == 1 {
		summaryParts = append(summaryParts, "long pasted design/reference content omitted from direct memoryization; treat as supporting context rather than conversational memory")
	}

	return "Reference document summary: " + strings.Join(dedupeTranscriptStrings(summaryParts), "; ")
}

func between(text, start, end string) string {
	startIdx := strings.Index(text, start)
	if startIdx < 0 {
		return ""
	}
	startIdx += len(start)
	endIdx := strings.Index(text[startIdx:], end)
	if endIdx < 0 {
		return strings.TrimSpace(text[startIdx:])
	}
	return strings.TrimSpace(text[startIdx : startIdx+endIdx])
}

func after(text, start string) string {
	startIdx := strings.Index(text, start)
	if startIdx < 0 {
		return ""
	}
	return strings.TrimSpace(text[startIdx+len(start):])
}

func nonEmptyFirstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func dedupeTranscriptStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}
