package rlm

import (
	"fmt"
	"strings"
	"unicode"
)

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
	PlanModeLambda PlanMode = "lambda-retrieval"
)

// QueryRoute represents the typed route classification produced by the structured classifier.
type QueryRoute string

const (
	QueryRouteCode          QueryRoute = "code"
	QueryRouteMemory        QueryRoute = "memory"
	QueryRouteMixed         QueryRoute = "mixed"
	QueryRouteEvidenceAudit QueryRoute = "evidence_audit"
)

// QueryPlan is the typed output of the structured classifier.
// It replaces keyword-based ClassifyRouteProfile with an explicit, typed structure.
type QueryPlan struct {
	Route      QueryRoute `json:"route"`
	Confidence float64    `json:"confidence"`
	Rationale  string     `json:"rationale,omitempty"`
}

// StructuredClassifier classifies a query into a typed QueryPlan.
// Implementations must NOT use keyword/substring matching for routing decisions.
type StructuredClassifier interface {
	Classify(query string) QueryPlan
}

// defaultClassifier uses typed task-based classification.
// It replaces the old keyword-based ClassifyRouteProfile.
type defaultClassifier struct{}

func (defaultClassifier) Classify(query string) QueryPlan {
	signals := collectQuerySignals(query)
	if signals.CodeScore >= 2 {
		return QueryPlan{
			Route:      QueryRouteCode,
			Confidence: clampConfidence(0.45 + float64(signals.CodeScore)*0.12),
			Rationale:  "structural code signals",
		}
	}
	return QueryPlan{
		Route:      QueryRouteMixed,
		Confidence: 0.35,
		Rationale:  "insufficient structural route signal",
	}
}

type querySignals struct {
	CodeScore int
}

func collectQuerySignals(query string) querySignals {
	fields := strings.Fields(query)
	signals := querySignals{}
	for _, field := range fields {
		rawToken := strings.Trim(field, " \t\r\n.,;:!?[]{}<>\"'")
		token := strings.Trim(rawToken, "()")
		if token == "" {
			continue
		}
		if looksLikePath(token) {
			signals.CodeScore += 2
		}
		if looksLikeSymbol(token) {
			signals.CodeScore++
		}
		if looksLikeCall(rawToken) {
			signals.CodeScore += 2
		}
		if strings.ContainsAny(token, "{}[]=|&;") {
			signals.CodeScore++
		}
	}
	return signals
}

func looksLikePath(token string) bool {
	if strings.Contains(token, "/") || strings.Contains(token, `\`) {
		return true
	}
	lastDot := strings.LastIndex(token, ".")
	if lastDot <= 0 || lastDot == len(token)-1 {
		return false
	}
	ext := token[lastDot+1:]
	if len(ext) > 8 {
		return false
	}
	for _, r := range ext {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func looksLikeSymbol(token string) bool {
	if strings.Contains(token, "::") || strings.Contains(token, ".") {
		return true
	}
	if strings.Contains(token, "_") {
		return true
	}
	seenLower := false
	for _, r := range token {
		if unicode.IsLower(r) {
			seenLower = true
			continue
		}
		if seenLower && unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

func looksLikeCall(token string) bool {
	open := strings.Index(token, "(")
	close := strings.LastIndex(token, ")")
	return open > 0 && close > open
}

func clampConfidence(value float64) float64 {
	if value > 0.95 {
		return 0.95
	}
	return value
}

// ClassifyQuery produces a typed QueryPlan from a query string using the structured classifier.
func ClassifyQuery(query string) QueryPlan {
	return defaultClassifier{}.Classify(query)
}

type Plan struct {
	RouteProfile RouteProfile `json:"route_profile"`
	Mode         PlanMode     `json:"mode"`
	QueryPlan    QueryPlan    `json:"query_plan,omitempty"`
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
	case string(RouteProfileCodeRetrieval):
		return RouteProfileCodeRetrieval
	case string(RouteProfileMemoryRecall):
		return RouteProfileMemoryRecall
	case string(RouteProfileMixed):
		return RouteProfileMixed
	case string(RouteProfileEvidenceAudit):
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
	case string(PlanModeLambda), "lambda":
		return PlanModeLambda
	default:
		return PlanModeFree
	}
}

// ResolveRouteProfile resolves the route from a typed QueryPlan.
// When the requested profile is auto, it derives the route from the QueryPlan.
func ResolveRouteProfile(query string, requested RouteProfile) RouteProfile {
	requested = NormalizeRouteProfile(string(requested))
	if requested != RouteProfileAuto {
		return requested
	}
	plan := ClassifyQuery(query)
	return queryRouteToProfile(plan.Route)
}

func queryRouteToProfile(route QueryRoute) RouteProfile {
	switch route {
	case QueryRouteCode:
		return RouteProfileCodeRetrieval
	case QueryRouteMemory:
		return RouteProfileMemoryRecall
	case QueryRouteMixed:
		return RouteProfileMixed
	case QueryRouteEvidenceAudit:
		return RouteProfileEvidenceAudit
	default:
		return RouteProfileCodeRetrieval
	}
}

// BuildPlan builds a Plan with typed QueryPlan and composite-only staged phases.
func BuildPlan(prompt string, requestedRoute RouteProfile, requestedMode PlanMode) Plan {
	mode := NormalizePlanMode(string(requestedMode))
	route := ResolveRouteProfile(prompt, requestedRoute)
	queryPlan := ClassifyQuery(prompt)
	// Override the query plan route to match the resolved route.
	queryPlan.Route = profileToQueryRoute(route)
	return buildPlanWithQueryPlan(route, mode, queryPlan)
}

func profileToQueryRoute(profile RouteProfile) QueryRoute {
	switch profile {
	case RouteProfileCodeRetrieval:
		return QueryRouteCode
	case RouteProfileMemoryRecall:
		return QueryRouteMemory
	case RouteProfileMixed:
		return QueryRouteMixed
	case RouteProfileEvidenceAudit:
		return QueryRouteEvidenceAudit
	default:
		return QueryRouteCode
	}
}

func buildPlanWithQueryPlan(route RouteProfile, mode PlanMode, qp QueryPlan) Plan {
	plan := Plan{
		RouteProfile: route,
		Mode:         mode,
		QueryPlan:    qp,
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
				AllowedTools:   []string{"retrieve_code", "retrieve_mixed"},
				RequireOneOf:   []string{"retrieve_code"},
				MaxIterations:  3,
				RequireToolUse: true,
			},
			{
				Name:           "inspection",
				Objective:      "Open and inspect the strongest candidates from discovery.",
				AllowedTools:   []string{"load_evidence_ref", "retrieve_code"},
				RequireOneOf:   []string{"load_evidence_ref"},
				MaxIterations:  3,
				RequireToolUse: true,
			},
			{
				Name:           "verification",
				Objective:      "Cross-check the strongest candidate and confirm the best supporting paths.",
				AllowedTools:   []string{"load_evidence_ref", "retrieve_code", "retrieve_mixed"},
				RequireOneOf:   []string{"load_evidence_ref", "retrieve_code"},
				MaxIterations:  2,
				RequireToolUse: true,
			},
		}
	case RouteProfileMemoryRecall:
		plan.Phases = []Phase{
			{
				Name:           "recall",
				Objective:      "Retrieve memory, context, and task evidence relevant to the query.",
				AllowedTools:   []string{"gather_context", "retrieve_memory", "retrieve_context", "retrieve_task"},
				RequireOneOf:   []string{"gather_context", "retrieve_memory", "retrieve_context"},
				MaxIterations:  3,
				RequireToolUse: true,
			},
			{
				Name:           "verification",
				Objective:      "Load and verify the strongest recalled evidence.",
				AllowedTools:   []string{"load_evidence_ref", "gather_context", "retrieve_memory", "retrieve_context"},
				RequireOneOf:   []string{"load_evidence_ref"},
				MaxIterations:  2,
				RequireToolUse: true,
			},
		}
	case RouteProfileMixed:
		plan.Phases = []Phase{
			{
				Name:           "discovery",
				Objective:      "Fan out across all retrieval lanes to gather evidence.",
				AllowedTools:   []string{"gather_context", "retrieve_mixed", "retrieve_code", "retrieve_memory", "retrieve_context", "retrieve_task"},
				RequireOneOf:   []string{"gather_context", "retrieve_mixed"},
				MaxIterations:  3,
				RequireToolUse: true,
			},
			{
				Name:           "inspection",
				Objective:      "Load and cross-check the strongest evidence from all lanes.",
				AllowedTools:   []string{"load_evidence_ref", "retrieve_code", "retrieve_memory"},
				RequireOneOf:   []string{"load_evidence_ref"},
				MaxIterations:  2,
				RequireToolUse: true,
			},
		}
	case RouteProfileEvidenceAudit:
		plan.Phases = []Phase{
			{
				Name:           "audit",
				Objective:      "Cross-check claims across multiple sources for consistency.",
				AllowedTools:   []string{"gather_context", "retrieve_mixed", "retrieve_code", "retrieve_memory", "retrieve_context", "retrieve_task"},
				RequireOneOf:   []string{"gather_context", "retrieve_mixed"},
				MaxIterations:  3,
				RequireToolUse: true,
			},
			{
				Name:           "verification",
				Objective:      "Load and verify audited evidence refs.",
				AllowedTools:   []string{"load_evidence_ref", "retrieve_code", "retrieve_memory"},
				RequireOneOf:   []string{"load_evidence_ref"},
				MaxIterations:  2,
				RequireToolUse: true,
			},
		}
	}
	return plan
}

// ValidateQueryPlan checks that a QueryPlan has valid fields.
func ValidateQueryPlan(qp QueryPlan) error {
	switch qp.Route {
	case QueryRouteCode, QueryRouteMemory, QueryRouteMixed, QueryRouteEvidenceAudit:
		return nil
	default:
		return fmt.Errorf("rlm: invalid query plan route %q", qp.Route)
	}
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
