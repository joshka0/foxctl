package companion

import (
	"fmt"
	"strings"
)

// AnchoredMemoryCandidateScope describes how far a candidate should persist.
type AnchoredMemoryCandidateScope string

const (
	CandidateScopeSession     AnchoredMemoryCandidateScope = "session"
	CandidateScopeDurable     AnchoredMemoryCandidateScope = "durable"
	CandidateScopeProvisional AnchoredMemoryCandidateScope = "provisional"
)

// AnchoredMemoryCandidate is a conservative memory candidate derived from one anchored frame.
type AnchoredMemoryCandidate struct {
	Type          string                       `json:"type"`
	Scope         AnchoredMemoryCandidateScope `json:"scope"`
	Text          string                       `json:"text"`
	Confidence    float64                      `json:"confidence"`
	Rationale     string                       `json:"rationale,omitempty"`
	Source        string                       `json:"source,omitempty"`
	SourceEventID int64                        `json:"source_event_id,omitempty"`
}

// AnchoredMemoryDerivation is the current pipeline output for one frame.
type AnchoredMemoryDerivation struct {
	FrameIndex         int                           `json:"frame_index"`
	InteractionSummary string                        `json:"interaction_summary"`
	Resolution         AnchoredInteractionResolution `json:"resolution"`
	Reaction           FollowUpReaction              `json:"reaction"`
	Candidates         []AnchoredMemoryCandidate     `json:"candidate_memories,omitempty"`
}

// DeriveMemoryCandidatesFromFrames converts interaction frames into conservative memory candidates.
func DeriveMemoryCandidatesFromFrames(frames []AnchoredInteractionFrame) []AnchoredMemoryDerivation {
	out := make([]AnchoredMemoryDerivation, 0, len(frames))
	for idx, frame := range frames {
		derivation := AnchoredMemoryDerivation{
			FrameIndex:         idx,
			InteractionSummary: summarizeFrame(frame),
			Resolution:         frame.Resolution,
			Reaction:           frame.Reaction,
			Candidates:         deriveCandidatesForFrame(frame),
		}
		out = append(out, derivation)
	}
	return out
}

func deriveCandidatesForFrame(frame AnchoredInteractionFrame) []AnchoredMemoryCandidate {
	candidates := make([]AnchoredMemoryCandidate, 0, 8)

	switch frame.Resolution {
	case InteractionResolutionResolved:
		candidates = append(candidates, extractCandidatesFromText(frame.UserEvent.ID, frame.UserEvent.Content, "user", CandidateScopeDurable, "resolved interaction")...)
		candidates = append(candidates, extractCandidatesFromText(frame.AssistantEvent.ID, frame.AssistantEvent.Content, "assistant", CandidateScopeDurable, "assistant proposal accepted by follow-up")...)
		if guidanceCandidate := deriveAcceptedAssistantGuidance(frame); guidanceCandidate != nil {
			candidates = append(candidates, *guidanceCandidate)
		}
	case InteractionResolutionCorrected:
		if frame.FollowUpUser != nil {
			candidates = append(candidates, extractCandidatesFromText(frame.FollowUpUser.ID, frame.FollowUpUser.Content, "followup_user", CandidateScopeDurable, "user correction updates prior interaction")...)
			candidates = append(candidates, memoryCandidate("follow_up_needed", CandidateScopeSession, frame.FollowUpUser.Content, 0.72, "correction indicates prior response was not sufficient", "followup_user", frame.FollowUpUser.ID))
		}
	case InteractionResolutionUnresolved:
		candidates = append(candidates, extractCandidatesFromText(frame.UserEvent.ID, frame.UserEvent.Content, "user", CandidateScopeProvisional, "interaction unresolved without user confirmation")...)
		candidates = append(candidates, memoryCandidate("follow_up_needed", CandidateScopeSession, frame.UserEvent.Content, 0.68, "no confirming follow-up yet", "user", frame.UserEvent.ID))
		if guidanceCandidate := deriveTentativeAssistantGuidance(frame); guidanceCandidate != nil {
			candidates = append(candidates, *guidanceCandidate)
		}
	default:
		candidates = append(candidates, extractCandidatesFromText(frame.UserEvent.ID, frame.UserEvent.Content, "user", CandidateScopeSession, "interaction still in progress")...)
		if guidanceCandidate := deriveTentativeAssistantGuidance(frame); guidanceCandidate != nil {
			candidates = append(candidates, *guidanceCandidate)
		}
	}

	if reactionCandidate := deriveReactionCandidate(frame); reactionCandidate != nil {
		candidates = append(candidates, *reactionCandidate)
	}
	candidates = append(candidates, deriveToolReceiptCandidates(frame)...)

	return dedupeCandidates(candidates)
}

func deriveAcceptedAssistantGuidance(frame AnchoredInteractionFrame) *AnchoredMemoryCandidate {
	if frame.Resolution != InteractionResolutionResolved {
		return nil
	}
	text := summarizeAcceptedGuidance(frame.AssistantEvent.Content)
	if text == "" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(text), strings.TrimSpace(frame.UserEvent.Content)) {
		return nil
	}
	if frame.FollowUpUser != nil && strings.EqualFold(strings.TrimSpace(text), strings.TrimSpace(frame.FollowUpUser.Content)) {
		return nil
	}
	if len(text) < 60 {
		return nil
	}
	return &AnchoredMemoryCandidate{
		Type:          EntryTypeTechnicalContext,
		Scope:         CandidateScopeDurable,
		Text:          text,
		Confidence:    0.74,
		Rationale:     "accepted assistant guidance condensed into a durable technical context candidate",
		Source:        "assistant_guidance",
		SourceEventID: frame.AssistantEvent.ID,
	}
}

func deriveTentativeAssistantGuidance(frame AnchoredInteractionFrame) *AnchoredMemoryCandidate {
	text := summarizeAcceptedGuidance(frame.AssistantEvent.Content)
	if text == "" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(text), strings.TrimSpace(frame.UserEvent.Content)) {
		return nil
	}
	if frame.FollowUpUser != nil && strings.EqualFold(strings.TrimSpace(text), strings.TrimSpace(frame.FollowUpUser.Content)) {
		return nil
	}
	if len(text) < 50 {
		return nil
	}
	return &AnchoredMemoryCandidate{
		Type:          EntryTypeTechnicalContext,
		Scope:         CandidateScopeProvisional,
		Text:          text,
		Confidence:    0.66,
		Rationale:     "assistant guidance may contain reusable context even before explicit confirmation",
		Source:        "assistant_guidance",
		SourceEventID: frame.AssistantEvent.ID,
	}
}

func deriveReactionCandidate(frame AnchoredInteractionFrame) *AnchoredMemoryCandidate {
	if frame.FollowUpUser == nil {
		return nil
	}
	text := strings.TrimSpace(frame.FollowUpUser.Content)
	if text == "" {
		return nil
	}

	switch frame.Reaction.Outcome {
	case ReactionOutcomeFrustrated:
		candidate := memoryCandidate(
			"user_pain_point",
			CandidateScopeSession,
			text,
			0.78,
			"follow-up shows frustration; remember the triggering issue, not the emotion alone",
			"followup_user",
			frame.FollowUpUser.ID,
		)
		candidate.Rationale = appendAffect(candidate.Rationale, frame.Reaction.InferredAffect)
		return &candidate
	case ReactionOutcomeConfused:
		candidate := memoryCandidate(
			"user_pain_point",
			CandidateScopeSession,
			text,
			0.7,
			"follow-up shows confusion; interaction likely needs clarification",
			"followup_user",
			frame.FollowUpUser.ID,
		)
		candidate.Rationale = appendAffect(candidate.Rationale, frame.Reaction.InferredAffect)
		return &candidate
	case ReactionOutcomeCorrected:
		candidate := memoryCandidate(
			"user_correction",
			CandidateScopeSession,
			text,
			0.76,
			"user correction should stay visible for subsequent steps",
			"followup_user",
			frame.FollowUpUser.ID,
		)
		return &candidate
	default:
		return nil
	}
}

func appendAffect(rationale, affect string) string {
	affect = strings.TrimSpace(affect)
	if affect == "" {
		return rationale
	}
	return fmt.Sprintf("%s (inferred affect: %s)", rationale, affect)
}

func extractCandidatesFromText(sourceEventID int64, text, source string, scope AnchoredMemoryCandidateScope, rationale string) []AnchoredMemoryCandidate {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	// Use the extraction policy to extract entries for all categories
	policy := NewDefaultPatternExtractionPolicy()
	allCategories := []string{
		ExtractionCategoryPreference, ExtractionCategoryDecision,
		ExtractionCategoryQuestion, ExtractionCategoryGoalChange,
		ExtractionCategoryRetraction,
	}
	extractions := policy.ExtractEntries(text, allCategories)
	extractions = append(extractions, extractExplicitFacts(text)...)

	out := make([]AnchoredMemoryCandidate, 0, len(extractions))
	for _, entry := range extractions {
		value := strings.TrimSpace(entry.Value)
		if value == "" {
			value = strings.TrimSpace(entry.RawText)
		}
		if value == "" {
			continue
		}

		candidateType := strings.TrimSpace(entry.EntryType)
		if candidateType == "retraction" {
			candidateType = "follow_up_needed"
			if scope == CandidateScopeDurable {
				scope = CandidateScopeSession
			}
		}

		out = append(out, memoryCandidate(
			candidateType,
			scope,
			value,
			defaultConfidence(entry.Confidence),
			rationale,
			source,
			sourceEventID,
		))
	}
	return out
}

func memoryCandidate(typ string, scope AnchoredMemoryCandidateScope, text string, confidence float64, rationale, source string, sourceEventID int64) AnchoredMemoryCandidate {
	return AnchoredMemoryCandidate{
		Type:          strings.TrimSpace(typ),
		Scope:         scope,
		Text:          strings.TrimSpace(text),
		Confidence:    defaultConfidence(confidence),
		Rationale:     strings.TrimSpace(rationale),
		Source:        strings.TrimSpace(source),
		SourceEventID: sourceEventID,
	}
}

func dedupeCandidates(in []AnchoredMemoryCandidate) []AnchoredMemoryCandidate {
	if len(in) == 0 {
		return nil
	}
	out := make([]AnchoredMemoryCandidate, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, item := range in {
		key := strings.ToLower(strings.TrimSpace(item.Type)) + "|" + strings.ToLower(strings.TrimSpace(item.Text)) + "|" + string(item.Scope)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func summarizeFrame(frame AnchoredInteractionFrame) string {
	parts := []string{
		"user: " + truncateInline(frame.UserEvent.Content, 140),
		"assistant: " + truncateInline(frame.AssistantEvent.Content, 140),
	}
	if len(frame.ToolReceipts) > 0 {
		parts = append(parts, "tools: "+truncateInline(strings.Join(frame.ToolReceipts, " | "), 160))
	}
	if frame.FollowUpUser != nil && strings.TrimSpace(frame.FollowUpUser.Content) != "" {
		parts = append(parts, "followup: "+truncateInline(frame.FollowUpUser.Content, 140))
	}
	return strings.Join(parts, " | ")
}

func summarizeAcceptedGuidance(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	paragraphs := strings.Split(text, "\n\n")
	candidates := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		paragraph = strings.Join(strings.Fields(strings.TrimSpace(paragraph)), " ")
		if paragraph == "" || strings.HasPrefix(paragraph, "```") {
			continue
		}
		paragraph = normalizeAssistantGuidanceParagraph(paragraph)
		if paragraph == "" {
			continue
		}
		candidates = append(candidates, paragraph)
	}
	if len(candidates) == 0 {
		return ""
	}
	chosen := candidates[0]
	if len(chosen) < 50 && len(candidates) > 1 {
		chosen = candidates[1]
	}
	return truncateInline(chosen, 220)
}

func normalizeAssistantGuidanceParagraph(paragraph string) string {
	paragraph = strings.TrimSpace(paragraph)
	if paragraph == "" {
		return ""
	}
	lower := strings.ToLower(paragraph)
	if strings.HasPrefix(lower, "i’m ") || strings.HasPrefix(lower, "i'm ") || strings.HasPrefix(lower, "i am ") {
		return ""
	}
	if strings.HasPrefix(lower, "command:") || strings.HasPrefix(lower, "chunk id:") {
		return ""
	}
	if strings.Contains(lower, "process exited with code") || strings.Contains(lower, "original token count") || strings.Contains(lower, "tool_result:") {
		return ""
	}

	paragraph = stripAcknowledgementLead(paragraph)
	lower = strings.ToLower(paragraph)
	if looksCommitStatusParagraph(lower) {
		return ""
	}

	if idx := strings.Index(paragraph, ". I "); idx > 0 {
		firstSentence := strings.TrimSpace(paragraph[:idx+1])
		if !strings.HasPrefix(strings.ToLower(firstSentence), "i ") {
			paragraph = firstSentence
		}
	}

	lower = strings.ToLower(paragraph)
	switch {
	case strings.HasPrefix(lower, "i kept going on ") && strings.Contains(lower, " and made "):
		paragraph = strings.TrimSpace(paragraph[strings.LastIndex(lower, " and ")+5:])
	case strings.HasPrefix(lower, "i updated ") && strings.Contains(lower, " so "):
		paragraph = strings.TrimSpace(paragraph[strings.Index(lower, " so ")+4:])
	case strings.HasPrefix(lower, "i added ") || strings.HasPrefix(lower, "i updated ") || strings.HasPrefix(lower, "i implemented "):
		paragraph = strings.TrimSpace(paragraph[2:])
	}

	paragraph = normalizeGuidanceOutcomeClause(paragraph)
	paragraph = strings.TrimSpace(paragraph)
	if paragraph == "" {
		return ""
	}
	if looksCommitStatusParagraph(strings.ToLower(paragraph)) {
		return ""
	}
	return paragraph
}

func stripAcknowledgementLead(paragraph string) string {
	for _, prefix := range []string{"Agreed. ", "Right. ", "Yes. ", "Exactly. ", "Correct. ", "Sounds good. "} {
		if strings.HasPrefix(paragraph, prefix) {
			return strings.TrimSpace(paragraph[len(prefix):])
		}
	}
	return paragraph
}

func looksCommitStatusParagraph(lower string) bool {
	lower = strings.TrimSpace(lower)
	if lower == "" {
		return false
	}
	if strings.HasPrefix(lower, "committed ") || strings.HasPrefix(lower, "committed as ") {
		return true
	}
	if strings.Contains(lower, " with `feat") || strings.Contains(lower, " with `fix") || strings.Contains(lower, " with `docs") {
		return true
	}
	return false
}

func normalizeGuidanceOutcomeClause(paragraph string) string {
	paragraph = strings.TrimSpace(paragraph)
	if paragraph == "" {
		return ""
	}
	lower := strings.ToLower(paragraph)
	if strings.HasPrefix(lower, "made the ") && strings.Contains(lower, " real instead of ") {
		rest := strings.TrimSpace(paragraph[len("made the "):])
		idx := strings.Index(strings.ToLower(rest), " real instead of ")
		if idx > 0 {
			subject := strings.TrimSpace(rest[:idx])
			tail := strings.TrimSpace(rest[idx+len(" real instead of "):])
			if subject != "" && tail != "" {
				return subject + " are real instead of " + strings.TrimRight(tail, ".!?") + "."
			}
		}
	}
	return paragraph
}

func deriveToolReceiptCandidates(frame AnchoredInteractionFrame) []AnchoredMemoryCandidate {
	if len(frame.ToolReceipts) == 0 {
		return nil
	}

	candidates := make([]AnchoredMemoryCandidate, 0, len(frame.ToolReceipts))
	for _, receipt := range frame.ToolReceipts {
		receipt = strings.TrimSpace(receipt)
		if receipt == "" {
			continue
		}
		lower := strings.ToLower(receipt)
		if !strings.HasPrefix(lower, "tool_result:") {
			continue
		}

		if strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "warning") {
			candidates = append(candidates, memoryCandidate(
				"follow_up_needed",
				CandidateScopeSession,
				receipt,
				0.7,
				"prederived tool output indicates a pending issue in the interaction",
				"tool_receipt",
				frame.AssistantEvent.ID,
			))
			continue
		}

		candidates = append(candidates, memoryCandidate(
			"tool_output_digest",
			CandidateScopeSession,
			receipt,
			0.62,
			"prederived tool output attached to the assistant turn",
			"tool_receipt",
			frame.AssistantEvent.ID,
		))
	}
	return candidates
}
