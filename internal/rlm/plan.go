package rlm

import "strings"

type RouteProfile string

const (
	RouteProfileAuto          RouteProfile = "auto"
	RouteProfileCodeRetrieval RouteProfile = "code_retrieval"
	RouteProfileMemoryRecall  RouteProfile = "memory_recall"
	RouteProfileMixed         RouteProfile = "mixed"
	RouteProfileEvidenceAudit RouteProfile = "evidence_audit"
)

type PlanMode string

const (
	PlanModeFree   PlanMode = "free"
	PlanModeGuided PlanMode = "guided"
	PlanModeStaged PlanMode = "staged"
	PlanModeHard   PlanMode = "hard"
	PlanModeLambda PlanMode = "lambda"
)

type Plan struct {
	RouteProfile RouteProfile `json:"route_profile"`
	Mode         PlanMode     `json:"mode"`
	Phases       []Phase      `json:"phases,omitempty"`
}

type Phase struct {
	Name           string   `json:"name"`
	Objective      string   `json:"objective"`
	AllowedTools   []string `json:"allowed_tools,omitempty"`
	RequireOneOf   []string `json:"require_one_of,omitempty"`
	MaxIterations  int      `json:"max_iterations,omitempty"`
	RequireToolUse bool     `json:"require_tool_use,omitempty"`
}

func NormalizeRouteProfile(value string) RouteProfile {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(RouteProfileAuto):
		return RouteProfileAuto
	case string(RouteProfileCodeRetrieval), "code", "code-retrieval":
		return RouteProfileCodeRetrieval
	case string(RouteProfileMemoryRecall), "memory", "memory-recall":
		return RouteProfileMemoryRecall
	case string(RouteProfileMixed):
		return RouteProfileMixed
	case string(RouteProfileEvidenceAudit), "evidence", "audit":
		return RouteProfileEvidenceAudit
	default:
		return RouteProfileAuto
	}
}

func NormalizePlanMode(value string) PlanMode {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", string(PlanModeFree):
		return PlanModeFree
	case string(PlanModeGuided):
		return PlanModeGuided
	case string(PlanModeStaged):
		return PlanModeStaged
	case string(PlanModeHard):
		return PlanModeHard
	case string(PlanModeLambda):
		return PlanModeLambda
	default:
		return PlanModeFree
	}
}

func ClassifyRouteProfile(prompt string) RouteProfile {
	query := strings.ToLower(strings.TrimSpace(prompt))
	switch {
	case containsAny(query, "thread", "scene", "handoff", "session", "decision", "artifact", "timeline"):
		return RouteProfileMemoryRecall
	case containsAny(query, "code", "repo", "package", "file", "handler", "api", "storage", "auth", "runtime", "function", "symbol", "path"):
		return RouteProfileCodeRetrieval
	default:
		return RouteProfileCodeRetrieval
	}
}

func ResolveRouteProfile(prompt string, requested RouteProfile) RouteProfile {
	requested = NormalizeRouteProfile(string(requested))
	if requested != RouteProfileAuto {
		return requested
	}
	return ClassifyRouteProfile(prompt)
}

func BuildPlan(prompt string, requestedRoute RouteProfile, requestedMode PlanMode) Plan {
	mode := NormalizePlanMode(string(requestedMode))
	route := ResolveRouteProfile(prompt, requestedRoute)
	plan := Plan{
		RouteProfile: route,
		Mode:         mode,
	}
	if mode != PlanModeStaged {
		return plan
	}
	switch route {
	case RouteProfileCodeRetrieval:
		plan.Phases = []Phase{
			{
				Name:           "discovery",
				Objective:      "Find likely repository files or canonical notes that answer the query.",
				AllowedTools:   []string{"code_search_ensemble", "semantic_search_code", "smart_search_code", "search_repo", "search_vault"},
				RequireOneOf:   []string{"code_search_ensemble"},
				MaxIterations:  3,
				RequireToolUse: true,
			},
			{
				Name:           "inspection",
				Objective:      "Open and inspect the strongest candidates from discovery.",
				AllowedTools:   []string{"load_file", "read_note", "ripgrep_code"},
				RequireOneOf:   []string{"load_file", "read_note"},
				MaxIterations:  3,
				RequireToolUse: true,
			},
			{
				Name:           "verification",
				Objective:      "Cross-check the strongest candidate and confirm the best supporting paths.",
				AllowedTools:   []string{"load_file", "read_note", "ripgrep_code", "expand_repo_graph"},
				RequireOneOf:   []string{"load_file", "read_note", "ripgrep_code", "expand_repo_graph"},
				MaxIterations:  2,
				RequireToolUse: true,
			},
		}
	}
	return plan
}

func filterToolsByNames(tools []Tool, allowed []string) []Tool {
	if len(allowed) == 0 {
		return append([]Tool(nil), tools...)
	}
	allow := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		allow[strings.TrimSpace(name)] = struct{}{}
	}
	out := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if _, ok := allow[strings.TrimSpace(tool.Name)]; ok {
			out = append(out, tool)
		}
	}
	return out
}

func containsAny(query string, parts ...string) bool {
	for _, part := range parts {
		if strings.Contains(query, part) {
			return true
		}
	}
	return false
}
