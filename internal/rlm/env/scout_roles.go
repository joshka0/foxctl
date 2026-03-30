package env

import (
	"strings"

	"github.com/jkatigb/agentctl/internal/rlm"
)

const (
	ScoutRoleMemoryFact     = "memory_fact_scout"
	ScoutRoleMemoryTimeline = "memory_timeline_scout"
	ScoutRoleACAContext     = "aca_context_scout"
)

// NormalizeScoutRole returns the canonical scout role name or empty string when unsupported.
func NormalizeScoutRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case ScoutRoleMemoryFact:
		return ScoutRoleMemoryFact
	case ScoutRoleMemoryTimeline:
		return ScoutRoleMemoryTimeline
	case ScoutRoleACAContext:
		return ScoutRoleACAContext
	default:
		return ""
	}
}

// FilterToolsForScoutRole narrows the experimental RLM tool surface for a specific scout role.
func FilterToolsForScoutRole(tools []rlm.Tool, role string) []rlm.Tool {
	role = NormalizeScoutRole(role)
	if role == "" {
		return append([]rlm.Tool(nil), tools...)
	}

	var allowed map[string]struct{}
	switch role {
	case ScoutRoleMemoryFact:
		allowed = map[string]struct{}{
			"search_artifacts": {},
			"load_artifact":    {},
			"search_scenes":    {},
			"get_scene":        {},
			"search_vault":     {},
			"read_note":        {},
		}
	case ScoutRoleMemoryTimeline:
		allowed = map[string]struct{}{
			"search_scenes":      {},
			"get_scene":          {},
			"search_artifacts":   {},
			"load_artifact":      {},
			"get_latest_handoff": {},
		}
	case ScoutRoleACAContext:
		allowed = map[string]struct{}{
			"get_top_of_mind":    {},
			"get_latest_handoff": {},
			"search_vault":       {},
			"read_note":          {},
		}
	}

	out := make([]rlm.Tool, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allowed[strings.TrimSpace(tool.Name)]; ok {
			out = append(out, tool)
		}
	}
	return out
}

// DecoratePromptForScoutRole adds bounded role instructions for scout subcalls.
func DecoratePromptForScoutRole(role, prompt string) string {
	prompt = strings.TrimSpace(prompt)
	switch NormalizeScoutRole(role) {
	case ScoutRoleMemoryFact:
		return strings.TrimSpace(`You are a memory fact scout.
Recover explicit current facts, preferences, decisions, goals, and technical context.
Prefer direct evidence over implication. Return compact factual findings only.

Task:
` + prompt)
	case ScoutRoleMemoryTimeline:
		return strings.TrimSpace(`You are a memory timeline scout.
Reconstruct updates, retractions, and supersession in chronological order.
Return the smallest chronology that explains the current best view.

Task:
` + prompt)
	case ScoutRoleACAContext:
		return strings.TrimSpace(`You are an ACA context scout.
Recover durable top-of-mind, handoff, and vault-backed context relevant to the task.
Return only the most useful durable context blocks.

Task:
` + prompt)
	default:
		return prompt
	}
}
