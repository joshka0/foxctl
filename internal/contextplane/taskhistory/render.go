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
