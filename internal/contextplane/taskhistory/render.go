package taskhistory

import (
	"fmt"
	"strings"
)

func RenderHookContext(pack Pack) string {
	return RenderHookContextWithArtifact(pack, "")
}

func RenderHookContextWithArtifact(pack Pack, artifactDigest string) string {
	var b strings.Builder
	b.WriteString("## Task Continuity\n\n")
	b.WriteString("**Task:** ")
	b.WriteString(strings.TrimSpace(pack.Task.Title))
	if status := strings.TrimSpace(pack.Task.Status); status != "" {
		b.WriteString(" (")
		b.WriteString(status)
		b.WriteString(")")
	}
	if len(pack.Handoffs) > 0 {
		b.WriteString("\n\n**Latest handoff:** ")
		b.WriteString(strings.TrimSpace(pack.Handoffs[0].Handoff.Summary))
	}
	if len(pack.Sessions) > 0 {
		b.WriteString("\n\n**Recent session:** ")
		b.WriteString(strings.TrimSpace(pack.Sessions[0].Summary))
	}
	if pack.Transcript != nil && strings.TrimSpace(pack.Transcript.AgentBrief) != "" {
		b.WriteString("\n\n**Transcript history:**\n")
		b.WriteString(strings.TrimSpace(pack.Transcript.AgentBrief))
	}
	if pack.Transcript != nil && len(pack.Transcript.ContinueWith) > 0 {
		b.WriteString("\n\n**Transcript next work:** ")
		b.WriteString(strings.Join(shortenStrings(pack.Transcript.ContinueWith, 3), " | "))
	}
	if pack.Transcript != nil && len(pack.Transcript.WatchOutFor) > 0 {
		b.WriteString("\n\n**Transcript watch-outs:** ")
		b.WriteString(strings.Join(shortenStrings(pack.Transcript.WatchOutFor, 3), " | "))
	}
	if pack.Transcript != nil && len(pack.Transcript.Regressions) > 0 {
		b.WriteString("\n\n**Transcript regressions:** ")
		b.WriteString(strings.Join(shortenStrings(pack.Transcript.Regressions, 3), " | "))
	}
	if pack.Transcript != nil && len(pack.Transcript.RecurringMistakes) > 0 {
		b.WriteString("\n\n**Recurring mistakes:** ")
		b.WriteString(strings.Join(shortenStrings(pack.Transcript.RecurringMistakes, 3), " | "))
	}
	if pack.Transcript != nil && len(pack.Transcript.RecentLearnings) > 0 {
		b.WriteString("\n\n**Transcript learnings:** ")
		b.WriteString(strings.Join(shortenStrings(pack.Transcript.RecentLearnings, 3), " | "))
	}
	if pack.Transcript != nil && len(pack.Transcript.RecentSurprises) > 0 {
		b.WriteString("\n\n**Transcript surprises:** ")
		b.WriteString(strings.Join(shortenStrings(pack.Transcript.RecentSurprises, 3), " | "))
	}
	if pack.Transcript != nil && len(pack.Transcript.RetrievedHighlights) > 0 {
		b.WriteString("\n\n**Transcript highlights:** ")
		b.WriteString(strings.Join(shortenStrings(pack.Transcript.RetrievedHighlights, 3), " | "))
	}
	if len(pack.FilesTouched) > 0 {
		b.WriteString("\n\n**Files:** ")
		b.WriteString(strings.Join(shortenStrings(pack.FilesTouched, 4), ", "))
	}
	if len(pack.ACANotes) > 0 {
		b.WriteString("\n\n**ACA notes:** ")
		notes := make([]string, 0, minInt(3, len(pack.ACANotes)))
		for _, hit := range pack.ACANotes[:minInt(3, len(pack.ACANotes))] {
			notes = append(notes, hit.Path)
		}
		b.WriteString(strings.Join(notes, ", "))
	}
	if len(pack.ExternalRefs) > 0 {
		b.WriteString("\n\n**External refs:** ")
		b.WriteString(strings.Join(shortenStrings(pack.ExternalRefs, 2), ", "))
	}
	if strings.TrimSpace(artifactDigest) != "" {
		b.WriteString("\n\n**Artifact:** ")
		b.WriteString(strings.TrimSpace(artifactDigest))
	}
	return strings.TrimSpace(b.String())
}

func RenderJidoState(pack Pack) map[string]any {
	return RenderJidoStateWithArtifact(pack, "")
}

func RenderJidoStateWithArtifact(pack Pack, artifactDigest string) map[string]any {
	state := map[string]any{
		"task_id":         pack.Task.ID,
		"task_title":      strings.TrimSpace(pack.Task.Title),
		"task_status":     strings.TrimSpace(pack.Task.Status),
		"summary":         strings.TrimSpace(pack.Summary),
		"files_touched":   append([]string(nil), shortenStrings(pack.FilesTouched, 6)...),
		"external_refs":   append([]string(nil), shortenStrings(pack.ExternalRefs, 3)...),
		"repo_anchor_cnt": len(pack.RepoAnchors),
		"dag_anchor_cnt":  len(pack.DAGAnchors),
		"aca_note_cnt":    len(pack.ACANotes),
		"session_cnt":     len(pack.Sessions),
		"handoff_cnt":     len(pack.Handoffs),
	}
	if len(pack.Handoffs) > 0 {
		state["latest_handoff"] = strings.TrimSpace(pack.Handoffs[0].Handoff.Summary)
	}
	if len(pack.ACANotes) > 0 {
		notes := make([]string, 0, minInt(3, len(pack.ACANotes)))
		for _, hit := range pack.ACANotes[:minInt(3, len(pack.ACANotes))] {
			notes = append(notes, hit.Path)
		}
		state["aca_notes"] = notes
	}
	if len(pack.Sessions) > 0 {
		state["recent_session_summaries"] = collectSessionSummaries(pack.Sessions, 2)
	}
	if pack.Transcript != nil {
		if strings.TrimSpace(pack.Transcript.ObjectiveLabel) != "" {
			state["transcript_history_objective_label"] = strings.TrimSpace(pack.Transcript.ObjectiveLabel)
		}
		if strings.TrimSpace(pack.Transcript.Overview) != "" {
			state["transcript_history_overview"] = strings.TrimSpace(pack.Transcript.Overview)
		}
		if strings.TrimSpace(pack.Transcript.AgentBrief) != "" {
			state["transcript_history_agent_brief"] = strings.TrimSpace(pack.Transcript.AgentBrief)
		}
		if len(pack.Transcript.ContinueWith) > 0 {
			state["transcript_history_continue_with"] = append([]string(nil), shortenStrings(pack.Transcript.ContinueWith, 3)...)
		}
		if len(pack.Transcript.WatchOutFor) > 0 {
			state["transcript_history_watch_out_for"] = append([]string(nil), shortenStrings(pack.Transcript.WatchOutFor, 3)...)
		}
		if len(pack.Transcript.Regressions) > 0 {
			state["transcript_history_regressions"] = append([]string(nil), shortenStrings(pack.Transcript.Regressions, 3)...)
		}
		if len(pack.Transcript.RecurringMistakes) > 0 {
			state["transcript_history_recurring_mistakes"] = append([]string(nil), shortenStrings(pack.Transcript.RecurringMistakes, 3)...)
		}
		if len(pack.Transcript.RecentLearnings) > 0 {
			state["transcript_history_recent_learnings"] = append([]string(nil), shortenStrings(pack.Transcript.RecentLearnings, 3)...)
		}
		if len(pack.Transcript.RecentSurprises) > 0 {
			state["transcript_history_recent_surprises"] = append([]string(nil), shortenStrings(pack.Transcript.RecentSurprises, 3)...)
		}
		if strings.TrimSpace(pack.Transcript.RetrievedBrief) != "" {
			state["transcript_history_retrieved_brief"] = strings.TrimSpace(pack.Transcript.RetrievedBrief)
		}
		if len(pack.Transcript.RetrievedHighlights) > 0 {
			state["transcript_history_retrieved_highlights"] = append([]string(nil), shortenStrings(pack.Transcript.RetrievedHighlights, 3)...)
		}
		if len(pack.Transcript.EvidenceRefs) > 0 {
			state["transcript_history_evidence_refs"] = append([]string(nil), shortenStrings(pack.Transcript.EvidenceRefs, 4)...)
		}
		if len(pack.Transcript.SourceNames) > 0 {
			state["transcript_history_sources"] = append([]string(nil), shortenStrings(pack.Transcript.SourceNames, 3)...)
		}
	}
	if strings.TrimSpace(artifactDigest) != "" {
		state["artifact"] = strings.TrimSpace(artifactDigest)
	}
	return state
}

func collectSessionSummaries(items []SessionSummary, limit int) []string {
	out := make([]string, 0, minInt(limit, len(items)))
	for _, item := range items {
		if len(out) >= limit {
			break
		}
		summary := strings.TrimSpace(item.Summary)
		if summary == "" {
			continue
		}
		out = append(out, summary)
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func RenderHookArtifactHint(pack Pack) string {
	return fmt.Sprintf("Task continuity pack ready: %s", strings.TrimSpace(pack.Summary))
}

func RenderTranscriptFamilyOverview(overview TranscriptFamilyOverview, artifactDigest string) string {
	var b strings.Builder
	b.WriteString("## Transcript Family Overview\n")
	if strings.TrimSpace(overview.SummaryMode) != "" {
		b.WriteString("\n**Summary mode:** ")
		b.WriteString(strings.TrimSpace(overview.SummaryMode))
		if strings.TrimSpace(overview.SummaryModel) != "" {
			b.WriteString(" (")
			b.WriteString(strings.TrimSpace(overview.SummaryModel))
			b.WriteString(")")
		}
	}
	if strings.TrimSpace(overview.Overview) != "" {
		b.WriteString("\n**Overview:** ")
		b.WriteString(strings.TrimSpace(overview.Overview))
	}
	if strings.TrimSpace(overview.DateFrom) != "" || strings.TrimSpace(overview.DateTo) != "" {
		b.WriteString("\n**Date range:** ")
		switch {
		case strings.TrimSpace(overview.DateFrom) != "" && strings.TrimSpace(overview.DateTo) != "":
			b.WriteString(strings.TrimSpace(overview.DateFrom))
			b.WriteString(" to ")
			b.WriteString(strings.TrimSpace(overview.DateTo))
		case strings.TrimSpace(overview.DateFrom) != "":
			b.WriteString("from ")
			b.WriteString(strings.TrimSpace(overview.DateFrom))
		default:
			b.WriteString("through ")
			b.WriteString(strings.TrimSpace(overview.DateTo))
		}
	}
	if len(overview.CurrentFocus) > 0 {
		b.WriteString("\n\n**Current focus:** ")
		b.WriteString(strings.Join(shortenStrings(overview.CurrentFocus, 4), " | "))
	}
	if len(overview.RecentChanges) > 0 {
		b.WriteString("\n\n**Recent changes:** ")
		b.WriteString(strings.Join(shortenStrings(overview.RecentChanges, 4), " | "))
	}
	if len(overview.TopLearnings) > 0 {
		b.WriteString("\n\n**Top learnings:** ")
		b.WriteString(strings.Join(shortenStrings(overview.TopLearnings, 4), " | "))
	}
	if len(overview.RecurringLearnings) > 0 {
		b.WriteString("\n\n**Recurring learnings:** ")
		b.WriteString(strings.Join(shortenStrings(overview.RecurringLearnings, 4), " | "))
	}
	if len(overview.TopRisks) > 0 {
		b.WriteString("\n\n**Top risks:** ")
		b.WriteString(strings.Join(shortenStrings(overview.TopRisks, 4), " | "))
	}
	if len(overview.TopSurprises) > 0 {
		b.WriteString("\n\n**Top surprises:** ")
		b.WriteString(strings.Join(shortenStrings(overview.TopSurprises, 4), " | "))
	}
	if len(overview.NextWork) > 0 {
		b.WriteString("\n\n**Next work:** ")
		b.WriteString(strings.Join(shortenStrings(overview.NextWork, 4), " | "))
	}
	if len(overview.RecurringMistakes) > 0 {
		b.WriteString("\n\n**Recurring mistakes:** ")
		b.WriteString(strings.Join(shortenStrings(overview.RecurringMistakes, 4), " | "))
	}
	if len(overview.SupportMetadata) > 0 {
		b.WriteString("\n\n**Support metadata:** ")
		b.WriteString(strings.Join(shortenStrings(renderTranscriptFamilySupportMetadata(overview.SupportMetadata), 4), " | "))
	}
	if len(overview.SourceOwners) > 0 {
		b.WriteString("\n\n**Source owners:** ")
		b.WriteString(strings.Join(shortenStrings(overview.SourceOwners, 6), ", "))
	}
	if strings.TrimSpace(artifactDigest) != "" {
		b.WriteString("\n\n**Artifact:** ")
		b.WriteString(strings.TrimSpace(artifactDigest))
	}
	return strings.TrimSpace(b.String())
}

func RenderTranscriptFamilyOverviewHint(overview TranscriptFamilyOverview) string {
	if strings.TrimSpace(overview.Overview) != "" {
		return fmt.Sprintf("Transcript family overview ready: %s", strings.TrimSpace(overview.Overview))
	}
	if len(overview.CurrentFocus) > 0 {
		return fmt.Sprintf("Transcript family overview ready: focus=%s", strings.Join(shortenStrings(overview.CurrentFocus, 2), " | "))
	}
	return "Transcript family overview ready"
}

func renderTranscriptFamilySupportMetadata(items []TranscriptFamilySupportMetadata) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.Text) == "" || strings.TrimSpace(item.Category) == "" {
			continue
		}
		text := item.Category + ": " + item.Text
		details := make([]string, 0, 2)
		if item.OwnerCount > 0 {
			details = append(details, fmt.Sprintf("owners=%d", item.OwnerCount))
		}
		if item.LatestAgeDays >= 0 && strings.TrimSpace(item.LatestUpdatedAt) != "" {
			details = append(details, fmt.Sprintf("age=%dd", item.LatestAgeDays))
		}
		if len(details) > 0 {
			text += " [" + strings.Join(details, ", ") + "]"
		}
		out = append(out, text)
	}
	return out
}
