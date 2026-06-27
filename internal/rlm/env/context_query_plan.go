package env

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/joshka0/foxctl/internal/context/contextengine"
)

const contextQueryPlanSchemaVersion = "context_query_plan/v1"

type planContextQueryInput struct {
	Question string   `json:"question"`
	Query    string   `json:"query,omitempty"`
	Goal     string   `json:"goal,omitempty"`
	Lanes    []string `json:"lanes,omitempty"`
	Limit    int      `json:"limit,omitempty"`
}

type contextQueryPlanOutput struct {
	SchemaVersion        string                              `json:"schema_version"`
	Question             string                              `json:"question"`
	AnswerType           string                              `json:"answer_type"`
	RequiredEvidence     []string                            `json:"required_evidence,omitempty"`
	CoverageRequirements []contextengine.CoverageRequirement `json:"coverage_requirements,omitempty"`
	GatherContext        plannedGatherContextCall            `json:"gather_context"`
	FallbackProbes       []plannedGatherContextCall          `json:"fallback_probes,omitempty"`
	SufficiencyChecks    []string                            `json:"sufficiency_checks,omitempty"`
	AbstainIf            []string                            `json:"abstain_if,omitempty"`
}

type plannedGatherContextCall struct {
	Query                string                              `json:"query"`
	Goal                 string                              `json:"goal,omitempty"`
	RequiredEvidence     []string                            `json:"required_evidence,omitempty"`
	CoverageRequirements []contextengine.CoverageRequirement `json:"coverage_requirements,omitempty"`
	Limit                int                                 `json:"limit,omitempty"`
	Lanes                []string                            `json:"lanes,omitempty"`
	ResponseMode         string                              `json:"response_mode,omitempty"`
}

func (a *ReadOnlyAdapter) planContextQuery(_ context.Context, args json.RawMessage) (map[string]any, error) {
	var input planContextQueryInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, err
	}
	plan, err := buildContextQueryPlan(input)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(plan)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func buildContextQueryPlan(input planContextQueryInput) (contextQueryPlanOutput, error) {
	question := strings.TrimSpace(input.Question)
	if question == "" {
		question = strings.TrimSpace(input.Query)
	}
	if question == "" {
		return contextQueryPlanOutput{}, fmt.Errorf("plan_context_query: missing question")
	}
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		goal = "recall"
	}
	limit := input.Limit
	if limit <= 0 {
		limit = 10
	}
	lanes := normalizeContextPlanLanes(input.Lanes)
	answerType := classifyContextQueryAnswerType(question)
	requiredEvidence := inferContextRequiredEvidence(question)
	coverageRequirements := contextQueryCoverageRequirements(answerType, requiredEvidence)
	gather := plannedGatherContextCall{
		Query:                question,
		Goal:                 goal,
		RequiredEvidence:     requiredEvidence,
		CoverageRequirements: coverageRequirements,
		Limit:                limit,
		Lanes:                lanes,
		ResponseMode:         "answer_surface",
	}
	return contextQueryPlanOutput{
		SchemaVersion:        contextQueryPlanSchemaVersion,
		Question:             question,
		AnswerType:           answerType,
		RequiredEvidence:     requiredEvidence,
		CoverageRequirements: coverageRequirements,
		GatherContext:        gather,
		FallbackProbes:       contextQueryFallbackProbes(gather, answerType),
		SufficiencyChecks:    contextQuerySufficiencyChecks(answerType),
		AbstainIf: []string{
			"no loaded evidence directly states the requested answer slot",
			"candidate evidence is only topical, adjacent, or missing one required evidence anchor",
		},
	}, nil
}

func normalizeContextPlanLanes(raw []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(raw))
	for _, lane := range raw {
		lane = strings.TrimSpace(strings.ToLower(lane))
		switch lane {
		case "code", "memory", "context", "task":
		default:
			continue
		}
		if _, ok := seen[lane]; ok {
			continue
		}
		seen[lane] = struct{}{}
		out = append(out, lane)
	}
	return out
}

func classifyContextQueryAnswerType(question string) string {
	q := " " + strings.ToLower(strings.TrimSpace(question)) + " "
	switch {
	case strings.Contains(q, " how many ") || strings.Contains(q, " count ") || strings.Contains(q, " number of "):
		return "count"
	case strings.Contains(q, " list ") || strings.Contains(q, " which ") || (strings.Contains(q, " what ") && containsAnyContextQueryTerm(q, " items ", " things ", " products ", " places ")):
		return "list"
	case strings.Contains(q, " where "):
		return "location"
	case strings.Contains(q, " how long ") || strings.Contains(q, " duration ") || strings.Contains(q, " commute "):
		return "duration"
	case strings.Contains(q, " when ") || strings.Contains(q, " date ") || strings.Contains(q, " latest ") || strings.Contains(q, " most recent "):
		return "temporal"
	case strings.Contains(q, " compare ") || strings.Contains(q, " versus ") || strings.Contains(q, " vs "):
		return "comparison"
	default:
		return "fact"
	}
}

func inferContextRequiredEvidence(question string) []string {
	tokens := tokenizeContextQuestion(question)
	out := make([]string, 0, 10)
	add := func(value string) {
		value = normalizeContextEvidenceTerm(value)
		if value == "" || contextQuestionStopwords[value] {
			return
		}
		for _, existing := range out {
			if existing == value {
				return
			}
		}
		out = append(out, value)
	}
	for _, match := range contextQueryValuePattern.FindAllString(question, -1) {
		add(match)
	}
	for n := 3; n >= 2; n-- {
		for i := 0; i+n <= len(tokens); i++ {
			window := tokens[i : i+n]
			if containsContextStopwordToken(window) {
				continue
			}
			add(joinContextQuestionTokens(window))
		}
	}
	for _, token := range tokens {
		if token.stopword {
			continue
		}
		if len(token.value) < 3 && !strings.HasPrefix(token.value, "$") {
			continue
		}
		add(token.value)
	}
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func contextQueryCoverageRequirements(answerType string, requiredEvidence []string) []contextengine.CoverageRequirement {
	requirements := []contextengine.CoverageRequirement{
		{
			ID:       "answer-" + contextQueryCoverageID(answerType),
			Kind:     "answer_slot",
			Label:    contextQueryAnswerSlotLabel(answerType),
			Terms:    contextQueryAnswerSlotTerms(answerType),
			Required: true,
			MinPaths: 1,
			Weight:   2,
		},
	}
	for _, evidence := range prioritizedContextEvidence(requiredEvidence, 4) {
		id := "evidence-" + contextQueryCoverageID(evidence)
		requirements = append(requirements, contextengine.CoverageRequirement{
			ID:       id,
			Kind:     "concept",
			Label:    evidence,
			Terms:    splitContextEvidenceTerms(evidence),
			Required: true,
			MinPaths: 1,
			Weight:   1.5,
		})
	}
	return requirements
}

func contextQueryFallbackProbes(base plannedGatherContextCall, answerType string) []plannedGatherContextCall {
	probes := make([]plannedGatherContextCall, 0, 3)
	add := func(query string, required []string) {
		query = strings.TrimSpace(query)
		if query == "" || query == base.Query {
			return
		}
		for _, probe := range probes {
			if probe.Query == query {
				return
			}
		}
		probe := base
		probe.Query = query
		probe.RequiredEvidence = cleanStringList(required)
		probe.CoverageRequirements = contextQueryCoverageRequirements(answerType, probe.RequiredEvidence)
		probes = append(probes, probe)
	}
	anchors := prioritizedContextEvidence(base.RequiredEvidence, 3)
	if len(anchors) > 0 {
		add(strings.TrimSpace(base.Query+" "+strings.Join(anchors, " ")), anchors)
	}
	if answerType == "count" || answerType == "list" {
		add(strings.TrimSpace("distinct items "+strings.Join(base.RequiredEvidence, " ")), base.RequiredEvidence)
	}
	for _, anchor := range anchors {
		add(strings.TrimSpace(base.Query+" "+anchor), []string{anchor})
	}
	if len(probes) > 3 {
		return probes[:3]
	}
	return probes
}

func contextQuerySufficiencyChecks(answerType string) []string {
	checks := []string{
		"load every candidate ref used for the final answer before answering",
		"use only loaded evidence that directly states the requested answer slot",
		"keep the final answer concise and omit tool names",
	}
	switch answerType {
	case "count":
		checks = append(checks, "enumerate distinct items before giving the count")
	case "list":
		checks = append(checks, "return only items directly supported by loaded evidence")
	case "location":
		checks = append(checks, "the loaded evidence must state a place or location")
	case "duration":
		checks = append(checks, "the loaded evidence must state a duration or enough endpoints to derive one")
	case "temporal":
		checks = append(checks, "the loaded evidence must state a date, time, or ordering cue")
	}
	return checks
}

type contextQuestionToken struct {
	value    string
	stopword bool
}

var contextQueryValuePattern = regexp.MustCompile(`(?i)\$?\b\d+(?:\.\d+)?\b(?:\s*(?:minutes?|mins?|hours?|days?|weeks?|months?|years?|miles?|km))?`)

var contextQueryTokenPattern = regexp.MustCompile(`(?i)\$?\d+(?:\.\d+)?|[a-z][a-z0-9'-]*`)

var contextQuestionStopwords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"did": true, "do": true, "does": true, "for": true, "from": true,
	"had": true, "have": true, "how": true, "i": true, "in": true,
	"is": true, "me": true, "my": true, "of": true, "on": true,
	"or": true, "the": true, "to": true, "was": true, "were": true,
	"what": true, "when": true, "where": true, "which": true, "who": true,
	"with": true,
}

func tokenizeContextQuestion(question string) []contextQuestionToken {
	raw := contextQueryTokenPattern.FindAllString(strings.ToLower(question), -1)
	tokens := make([]contextQuestionToken, 0, len(raw))
	for _, value := range raw {
		value = normalizeContextEvidenceTerm(value)
		if value == "" {
			continue
		}
		tokens = append(tokens, contextQuestionToken{
			value:    value,
			stopword: contextQuestionStopwords[value],
		})
	}
	return tokens
}

func normalizeContextEvidenceTerm(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Trim(value, " \t\n\r.,;:!?()[]{}\"'")
	value = strings.Join(strings.Fields(value), " ")
	return value
}

func containsContextStopwordToken(tokens []contextQuestionToken) bool {
	for _, token := range tokens {
		if token.stopword {
			return true
		}
	}
	return false
}

func joinContextQuestionTokens(tokens []contextQuestionToken) string {
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		parts = append(parts, token.value)
	}
	return strings.Join(parts, " ")
}

func containsAnyContextQueryTerm(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func prioritizedContextEvidence(requiredEvidence []string, limit int) []string {
	out := make([]string, 0, limit)
	add := func(value string) {
		if value == "" {
			return
		}
		for _, existing := range out {
			if existing == value {
				return
			}
		}
		out = append(out, value)
	}
	for _, value := range requiredEvidence {
		if strings.HasPrefix(value, "$") || strings.Contains(value, " ") {
			add(value)
		}
		if len(out) >= limit {
			return out
		}
	}
	for _, value := range requiredEvidence {
		add(value)
		if len(out) >= limit {
			return out
		}
	}
	return out
}

func splitContextEvidenceTerms(value string) []string {
	tokens := tokenizeContextQuestion(value)
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if token.stopword {
			continue
		}
		out = append(out, token.value)
	}
	if len(out) == 0 {
		out = append(out, normalizeContextEvidenceTerm(value))
	}
	return cleanStringList(out)
}

func contextQueryAnswerSlotLabel(answerType string) string {
	switch answerType {
	case "count":
		return "answer count"
	case "list":
		return "answer item list"
	case "location":
		return "answer location"
	case "duration":
		return "answer duration"
	case "temporal":
		return "answer time"
	case "comparison":
		return "answer comparison"
	default:
		return "answer fact"
	}
}

func contextQueryAnswerSlotTerms(answerType string) []string {
	switch answerType {
	case "count":
		return []string{"count", "number", "distinct"}
	case "list":
		return []string{"items", "list", "distinct"}
	case "location":
		return []string{"location", "place", "where"}
	case "duration":
		return []string{"duration", "time", "how long"}
	case "temporal":
		return []string{"date", "time", "when"}
	case "comparison":
		return []string{"compare", "difference"}
	default:
		return []string{"answer", "fact"}
	}
}

func contextQueryCoverageID(value string) string {
	value = strings.ToLower(value)
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		return "value"
	}
	if len(id) > 48 {
		id = strings.TrimRight(id[:48], "-")
	}
	return id
}
