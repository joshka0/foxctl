package env

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/joshka0/foxctl/internal/rlm"
)

type memoryEnsembleInput struct {
	Query                 string   `json:"query"`
	Lanes                 []string `json:"lanes"`
	MaxScouts             int      `json:"max_scouts"`
	MaxIterationsPerScout int      `json:"max_iterations_per_scout"`
	MaxSubcallsPerScout   int      `json:"max_subcalls_per_scout"`
	LimitPerLane          int      `json:"limit_per_lane"`
}

type memoryScoutClaim struct {
	Key          string   `json:"key,omitempty"`
	Value        string   `json:"value,omitempty"`
	Status       string   `json:"status,omitempty"`
	Source       string   `json:"source,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	Confidence   float64  `json:"confidence,omitempty"`
}

type memoryScoutTimelineItem struct {
	TS           string   `json:"ts,omitempty"`
	Kind         string   `json:"kind,omitempty"`
	Value        string   `json:"value,omitempty"`
	Source       string   `json:"source,omitempty"`
	EvidenceRefs []string `json:"evidence_refs,omitempty"`
	Supersedes   string   `json:"supersedes,omitempty"`
	Confidence   float64  `json:"confidence,omitempty"`
}

type memoryScoutContextBlock struct {
	Lane    string   `json:"lane,omitempty"`
	Summary string   `json:"summary,omitempty"`
	Refs    []string `json:"refs,omitempty"`
}

type memoryScoutOutput struct {
	Summary         string                    `json:"summary,omitempty"`
	Claims          []memoryScoutClaim        `json:"claims,omitempty"`
	CurrentBestView string                    `json:"current_best_view,omitempty"`
	Timeline        []memoryScoutTimelineItem `json:"timeline,omitempty"`
	ContextBlocks   []memoryScoutContextBlock `json:"context_blocks,omitempty"`
	Gaps            []string                  `json:"gaps,omitempty"`
}

func (a *ReadOnlyAdapter) memoryEnsembleRetrieve(ctx context.Context, args json.RawMessage) (map[string]any, error) {
	if a.subcall == nil {
		return map[string]any{
			"supported": false,
			"message":   "memory_ensemble_retrieve requires subcall support",
		}, nil
	}

	var input memoryEnsembleInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	input.Query = strings.TrimSpace(input.Query)
	if input.Query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if input.MaxScouts <= 0 {
		input.MaxScouts = 3
	}
	if input.MaxIterationsPerScout <= 0 {
		input.MaxIterationsPerScout = 3
	}
	if input.MaxSubcallsPerScout < 0 {
		input.MaxSubcallsPerScout = 0
	}

	roles := selectMemoryScoutRoles(input.Lanes, input.MaxScouts)
	if len(roles) == 0 {
		return map[string]any{
			"summary":  "",
			"scouts":   []map[string]any{},
			"metadata": map[string]any{"scouts_run": []string{}, "lanes_run": []string{}},
		}, nil
	}

	scouts := make([]map[string]any, 0, len(roles))
	summaries := make([]string, 0, len(roles))
	evidenceRefs := make([]string, 0, len(roles)*4)
	retrievedPaths := make([]string, 0, len(roles)*4)
	claims := make([]memoryScoutClaim, 0, len(roles)*4)
	timeline := make([]memoryScoutTimelineItem, 0, len(roles)*4)
	contextBlocks := make([]memoryScoutContextBlock, 0, len(roles)*4)
	gaps := make([]string, 0, len(roles)*2)
	currentBestViews := make([]string, 0, len(roles))

	for _, role := range roles {
		taskPrompt := buildMemoryScoutPrompt(role, input.Query, input.LimitPerLane)
		result, err := a.subcall(ctx, rlm.Task{
			Prompt:        taskPrompt,
			Role:          role,
			WorkspaceRoot: a.workspaceRoot,
			MaxDepth:      0,
			MaxIterations: input.MaxIterationsPerScout,
			MaxSubcalls:   input.MaxSubcallsPerScout,
		}, a.environment)
		scout := map[string]any{
			"role": role,
		}
		if err != nil {
			scout["ok"] = false
			scout["error"] = err.Error()
			scouts = append(scouts, scout)
			continue
		}

		parsed := parseMemoryScoutOutput(result.Answer)
		parsed = hydrateMemoryScoutOutput(parsed, result.EvidenceRefs)

		scout["ok"] = true
		scout["summary"] = scoutStructuredSummary(parsed)
		scout["raw_answer"] = strings.TrimSpace(result.Answer)
		scout["evidence_refs"] = append([]string(nil), result.EvidenceRefs...)
		scout["retrieved_paths"] = append([]string(nil), result.RetrievedPaths...)
		if len(parsed.Claims) > 0 {
			scout["claims"] = parsed.Claims
		}
		if len(parsed.Timeline) > 0 {
			scout["timeline"] = parsed.Timeline
		}
		if parsed.CurrentBestView != "" {
			scout["current_best_view"] = parsed.CurrentBestView
		}
		if len(parsed.ContextBlocks) > 0 {
			scout["context_blocks"] = parsed.ContextBlocks
		}
		if len(parsed.Gaps) > 0 {
			scout["gaps"] = parsed.Gaps
		}
		if len(result.Metadata) > 0 {
			scout["metadata"] = result.Metadata
		}
		scouts = append(scouts, scout)

		if summary := scoutStructuredSummary(parsed); summary != "" {
			summaries = append(summaries, role+": "+summary)
		}
		if parsed.CurrentBestView != "" {
			currentBestViews = append(currentBestViews, parsed.CurrentBestView)
		}
		claims = append(claims, parsed.Claims...)
		timeline = append(timeline, parsed.Timeline...)
		contextBlocks = append(contextBlocks, parsed.ContextBlocks...)
		gaps = append(gaps, parsed.Gaps...)
		evidenceRefs = append(evidenceRefs, result.EvidenceRefs...)
		retrievedPaths = append(retrievedPaths, result.RetrievedPaths...)
	}

	basis := recommendAnswerBasis(roles, claims, timeline, contextBlocks, currentBestViews)
	return map[string]any{
		"summary":                  buildMemoryEnsembleSummary(basis, summaries, claims, timeline, contextBlocks, currentBestViews),
		"scouts":                   scouts,
		"recommended_answer_basis": basis,
		"claims":                   claims,
		"timeline":                 timeline,
		"context_blocks":           contextBlocks,
		"gaps":                     uniqueStrings(gaps),
		"evidence_refs":            uniqueStrings(evidenceRefs),
		"retrieved_paths":          uniqueStrings(retrievedPaths),
		"metadata": map[string]any{
			"scouts_run": roles,
			"lanes_run":  lanesForRoles(roles),
		},
	}, nil
}

func selectMemoryScoutRoles(lanes []string, maxScouts int) []string {
	if maxScouts <= 0 {
		maxScouts = 3
	}
	roles := make([]string, 0, 3)
	addRole := func(role string) {
		role = NormalizeScoutRole(role)
		if role == "" {
			return
		}
		for _, existing := range roles {
			if existing == role {
				return
			}
		}
		roles = append(roles, role)
	}

	for _, lane := range lanes {
		switch strings.ToLower(strings.TrimSpace(lane)) {
		case "facts", "fact":
			addRole(ScoutRoleMemoryFact)
		case "timeline", "time":
			addRole(ScoutRoleMemoryTimeline)
		case "aca", "context":
			addRole(ScoutRoleACAContext)
		}
	}
	if len(roles) == 0 {
		for _, role := range []string{ScoutRoleMemoryFact, ScoutRoleMemoryTimeline, ScoutRoleACAContext} {
			addRole(role)
		}
	}
	if len(roles) > maxScouts {
		return roles[:maxScouts]
	}
	return roles
}

func buildMemoryScoutPrompt(role, query string, limit int) string {
	limitHint := ""
	if limit > 0 {
		limitHint = fmt.Sprintf(" Limit yourself to about %d high-signal items.", limit)
	}
	switch NormalizeScoutRole(role) {
	case ScoutRoleMemoryFact:
		return strings.TrimSpace("Find the current explicit facts, preferences, decisions, goals, or technical context that answer this query: " + query + "." + limitHint + ` Return JSON only with this shape: {"summary":"...","claims":[{"key":"...","value":"...","status":"current|candidate","source":"tool-name","evidence_refs":["..."],"confidence":0.0}],"gaps":["..."]}.`)
	case ScoutRoleMemoryTimeline:
		return strings.TrimSpace("Reconstruct the update timeline for this query and identify the current best view: " + query + "." + limitHint + ` Return JSON only with this shape: {"summary":"...","current_best_view":"...","timeline":[{"ts":"...","kind":"statement|update|retraction|decision","value":"...","source":"tool-name","evidence_refs":["..."],"supersedes":"...","confidence":0.0}],"gaps":["..."]}.`)
	case ScoutRoleACAContext:
		return strings.TrimSpace("Gather the durable ACA, handoff, and vault-backed context relevant to this query: " + query + "." + limitHint + ` Return JSON only with this shape: {"summary":"...","context_blocks":[{"lane":"top_of_mind|task_continuity|vault|related_note","summary":"...","refs":["..."]}],"gaps":["..."]}.`)
	default:
		return strings.TrimSpace(query)
	}
}

func parseMemoryScoutOutput(raw string) memoryScoutOutput {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return memoryScoutOutput{}
	}
	var out memoryScoutOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err == nil {
			return out
		}
	}
	return memoryScoutOutput{Summary: raw}
}

func hydrateMemoryScoutOutput(out memoryScoutOutput, fallbackEvidence []string) memoryScoutOutput {
	for i := range out.Claims {
		out.Claims[i].EvidenceRefs = fillStringSlice(out.Claims[i].EvidenceRefs, fallbackEvidence)
	}
	for i := range out.Timeline {
		out.Timeline[i].EvidenceRefs = fillStringSlice(out.Timeline[i].EvidenceRefs, fallbackEvidence)
	}
	for i := range out.ContextBlocks {
		out.ContextBlocks[i].Refs = fillStringSlice(out.ContextBlocks[i].Refs, fallbackEvidence)
	}
	return out
}

func fillStringSlice(existing, fallback []string) []string {
	if len(existing) > 0 {
		return uniqueStrings(existing)
	}
	if len(fallback) == 0 {
		return nil
	}
	return uniqueStrings(append([]string(nil), fallback...))
}

func scoutStructuredSummary(out memoryScoutOutput) string {
	if strings.TrimSpace(out.CurrentBestView) != "" {
		return strings.TrimSpace(out.CurrentBestView)
	}
	if strings.TrimSpace(out.Summary) != "" {
		return strings.TrimSpace(out.Summary)
	}
	if len(out.Claims) > 0 {
		lines := make([]string, 0, min(3, len(out.Claims)))
		for _, claim := range out.Claims[:min(3, len(out.Claims))] {
			lines = append(lines, strings.TrimSpace(claim.Key+": "+claim.Value))
		}
		return strings.Join(lines, "; ")
	}
	if len(out.ContextBlocks) > 0 {
		lines := make([]string, 0, min(2, len(out.ContextBlocks)))
		for _, block := range out.ContextBlocks[:min(2, len(out.ContextBlocks))] {
			lines = append(lines, strings.TrimSpace(block.Lane+": "+block.Summary))
		}
		return strings.Join(lines, "; ")
	}
	return ""
}

func recommendAnswerBasis(roles []string, claims []memoryScoutClaim, timeline []memoryScoutTimelineItem, contextBlocks []memoryScoutContextBlock, currentBestViews []string) string {
	switch {
	case len(timeline) > 0 || len(currentBestViews) > 0:
		return "timeline"
	case len(claims) > 0 && len(contextBlocks) == 0:
		return "facts"
	case len(contextBlocks) > 0 && len(claims) == 0:
		return "aca"
	case len(roles) == 1:
		switch roles[0] {
		case ScoutRoleMemoryFact:
			return "facts"
		case ScoutRoleMemoryTimeline:
			return "timeline"
		case ScoutRoleACAContext:
			return "aca"
		}
	}
	return "combined"
}

func buildMemoryEnsembleSummary(basis string, scoutSummaries []string, claims []memoryScoutClaim, timeline []memoryScoutTimelineItem, contextBlocks []memoryScoutContextBlock, currentBestViews []string) string {
	switch basis {
	case "timeline":
		if len(currentBestViews) > 0 {
			return strings.TrimSpace(currentBestViews[0])
		}
		if len(timeline) > 0 {
			return strings.TrimSpace(timeline[len(timeline)-1].Value)
		}
	case "facts":
		if len(claims) > 0 {
			lines := make([]string, 0, min(3, len(claims)))
			for _, claim := range claims[:min(3, len(claims))] {
				lines = append(lines, strings.TrimSpace(claim.Key+": "+claim.Value))
			}
			return strings.Join(lines, "\n")
		}
	case "aca":
		if len(contextBlocks) > 0 {
			lines := make([]string, 0, min(2, len(contextBlocks)))
			for _, block := range contextBlocks[:min(2, len(contextBlocks))] {
				lines = append(lines, strings.TrimSpace(block.Lane+": "+block.Summary))
			}
			return strings.Join(lines, "\n")
		}
	}
	return strings.Join(scoutSummaries, "\n\n")
}

func lanesForRoles(roles []string) []string {
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		switch role {
		case ScoutRoleMemoryFact:
			out = append(out, "facts")
		case ScoutRoleMemoryTimeline:
			out = append(out, "timeline")
		case ScoutRoleACAContext:
			out = append(out, "aca")
		}
	}
	return uniqueStrings(out)
}
