package generalsolver

import (
	"fmt"
	"sort"
	"strings"
)

func RenderStateMarkdown(state *SolverState) string {
	if state == nil {
		return "# Solver State\n\n(nil state)\n"
	}
	summary := SummarizeState(state)
	var b strings.Builder
	b.WriteString("# Solver State\n\n")
	fmt.Fprintf(&b, "- Total items: %d\n", summary.TotalItems)
	fmt.Fprintf(&b, "- Solved: %d\n", len(summary.SolvedIDs))
	fmt.Fprintf(&b, "- Blocked/failed: %d\n", len(summary.BlockedIDs))
	fmt.Fprintf(&b, "- Ready: %d\n", summary.ReadyCount)
	fmt.Fprintf(&b, "- Artifacts: %d\n", summary.Artifacts)
	fmt.Fprintf(&b, "- Failures: %d\n", summary.FailureCount)
	fmt.Fprintf(&b, "- Compaction digests: %d\n", summary.DigestCount)

	if len(summary.ByArchetype) > 0 {
		b.WriteString("\n## By Archetype\n\n")
		b.WriteString(renderArchetypeMap(summary.ByArchetype))
	}

	if len(summary.SolvedIDs) > 0 {
		b.WriteString("\n## Solved\n\n")
		for _, id := range summary.SolvedIDs {
			artifact, hasArtifact := state.Artifacts[id]
			if hasArtifact {
				answerPreview := artifactAnswerPreview(artifact)
				fmt.Fprintf(&b, "- %s: %s\n", id, answerPreview)
			} else {
				fmt.Fprintf(&b, "- %s\n", id)
			}
		}
	}

	if len(summary.BlockedIDs) > 0 {
		b.WriteString("\n## Blocked/Failed\n\n")
		for _, id := range summary.BlockedIDs {
			item := state.Items[id]
			fmt.Fprintf(&b, "- %s (%s, attempts: %d)\n", id, item.Status, item.Attempts)
		}
	}

	if len(state.ReadyQueue) > 0 {
		b.WriteString("\n## Ready Queue\n\n")
		for _, id := range state.ReadyQueue {
			item := state.Items[id]
			fmt.Fprintf(&b, "- %s [%s] priority=%.1f risk=%.1f\n", id, item.Archetype, item.Priority, item.Risk)
		}
	}

	if len(state.Digests) > 0 {
		b.WriteString("\n## Compaction Digests\n\n")
		for _, d := range state.Digests {
			fmt.Fprintf(&b, "- %s\n", d)
		}
	}

	return b.String()
}

func RenderWorkItemMarkdown(item WorkItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Work Item %s\n\n", item.ID)
	fmt.Fprintf(&b, "- Goal: %s\n", item.Goal)
	fmt.Fprintf(&b, "- Archetype: %s\n", item.Archetype)
	fmt.Fprintf(&b, "- Status: %s\n", item.Status)
	fmt.Fprintf(&b, "- Priority: %.2f\n", item.Priority)
	fmt.Fprintf(&b, "- Risk: %.2f\n", item.Risk)
	fmt.Fprintf(&b, "- Attempts: %d / %d\n", item.Attempts, item.MaxAttempts)
	if len(item.DependsOn) > 0 {
		b.WriteString("- Depends on: ")
		b.WriteString(strings.Join(item.DependsOn, ", "))
		b.WriteString("\n")
	}
	return b.String()
}

func RenderArtifactMarkdown(artifact WorkArtifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Artifact for %s\n\n", artifact.WorkItemID)
	fmt.Fprintf(&b, "- Status: %s\n", artifact.Status)
	fmt.Fprintf(&b, "- Confidence: %.2f\n", artifact.Confidence)
	if artifact.Answer != nil {
		fmt.Fprintf(&b, "- Answer: %v\n", artifact.Answer)
	} else {
		b.WriteString("- Answer: (no answer)\n")
	}
	if artifact.Code != "" {
		b.WriteString("- Code:\n```\n")
		b.WriteString(artifact.Code)
		b.WriteString("\n```\n")
	}
	if len(artifact.Checks) > 0 {
		b.WriteString("- Checks:\n")
		for _, c := range artifact.Checks {
			fmt.Fprintf(&b, "  - %s\n", c)
		}
	}
	if len(artifact.Counterexamples) > 0 {
		b.WriteString("- Counterexamples:\n")
		for _, ce := range artifact.Counterexamples {
			parts := make([]string, 0, len(ce))
			for k, v := range ce {
				parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			}
			sort.Strings(parts)
			fmt.Fprintf(&b, "  - %s\n", strings.Join(parts, ", "))
		}
	}
	return b.String()
}

func artifactAnswerPreview(a WorkArtifact) string {
	if a.Answer == nil {
		return "(no answer)"
	}
	s, ok := a.Answer.(string)
	if ok {
		return truncateString(s, 120)
	}
	return truncateString(fmt.Sprintf("%v", a.Answer), 120)
}

func sortedArchetypeKeys(m map[ProblemArchetype]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	return keys
}

func renderArchetypeMap(m map[ProblemArchetype]int) string {
	keys := sortedArchetypeKeys(m)
	var b strings.Builder
	for _, ks := range keys {
		a := ProblemArchetype(ks)
		fmt.Fprintf(&b, "- %s: %d\n", a, m[a])
	}
	return b.String()
}
