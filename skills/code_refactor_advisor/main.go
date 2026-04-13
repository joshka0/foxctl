package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/domain/skill"
	"github.com/jkatigb/agentctl/internal/intelligence/verification"
	"github.com/jkatigb/agentctl/internal/platform/buildinfo"
	"github.com/jkatigb/agentctl/internal/platform/workspace"
	"github.com/jkatigb/agentctl/internal/skillrun"
)

const command = "code/refactor_advisor"

type input struct {
	Path          string  `json:"path"`
	Language      string  `json:"language" validate:"required,oneof=go python javascript typescript elixir rust"`
	Focus         string  `json:"focus" validate:"omitempty,oneof=all slop"`
	RuleSet       string  `json:"rule_set" validate:"omitempty,oneof=conservative default aggressive"`
	MinScore      int     `json:"min_score" validate:"gte=0,lte=100"`
	MaxFindings   int     `json:"max_findings" validate:"gte=1,lte=20"`
	ShortlistSize int     `json:"shortlist_size" validate:"gte=1,lte=10"`
	Provider      string  `json:"provider" validate:"omitempty,oneof=openrouter openai groq cerebras"`
	Model         string  `json:"model"`
	BaseURL       string  `json:"base_url"`
	APIKey        string  `json:"api_key"`
	TimeoutSec    int     `json:"timeout_sec" validate:"gte=0,lte=600"`
	Temperature   float64 `json:"temperature" validate:"gte=0,lte=2"`
	MaxTokens     int     `json:"max_tokens" validate:"gte=0,lte=4000"`
}

type scoutFinding struct {
	RuleID            string         `json:"rule_id"`
	Category          string         `json:"category"`
	Severity          string         `json:"severity"`
	Score             int            `json:"score"`
	Title             string         `json:"title"`
	Detail            string         `json:"detail"`
	SuggestedRefactor string         `json:"suggested_refactor,omitempty"`
	File              string         `json:"file"`
	Line              int            `json:"line,omitempty"`
	Symbol            string         `json:"symbol,omitempty"`
	Language          string         `json:"language"`
	Confidence        string         `json:"confidence,omitempty"`
	Signals           []string       `json:"signals,omitempty"`
	Evidence          map[string]any `json:"evidence,omitempty"`
}

type scoutData struct {
	Findings []scoutFinding `json:"findings"`
	Summary  map[string]any `json:"summary"`
}

type candidateBrief struct {
	File              string   `json:"file"`
	Symbol            string   `json:"symbol,omitempty"`
	Line              int      `json:"line,omitempty"`
	Score             int      `json:"score"`
	RuleID            string   `json:"rule_id"`
	Severity          string   `json:"severity"`
	Title             string   `json:"title"`
	Detail            string   `json:"detail"`
	SuggestedRefactor string   `json:"suggested_refactor,omitempty"`
	Signals           []string `json:"signals,omitempty"`
}

type advisorItem struct {
	Path             string `json:"path"`
	Symbol           string `json:"symbol,omitempty"`
	Priority         int    `json:"priority"`
	Why              string `json:"why"`
	RefactorBoundary string `json:"refactor_boundary,omitempty"`
	Risk             string `json:"risk,omitempty"`
}

type advisorOutput struct {
	Summary     string        `json:"summary"`
	Prioritized []advisorItem `json:"prioritized"`
	Defer       []advisorItem `json:"defer,omitempty"`
	Sequence    []string      `json:"sequence,omitempty"`
	Notes       []string      `json:"notes,omitempty"`
}

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in input) error {
	if in.Focus == "" {
		in.Focus = "all"
	}
	if in.RuleSet == "" {
		in.RuleSet = "default"
	}
	if in.MinScore <= 0 {
		in.MinScore = 70
	}
	if in.MaxFindings <= 0 {
		in.MaxFindings = 8
	}
	if in.ShortlistSize <= 0 {
		in.ShortlistSize = 3
	}
	if in.Provider == "" {
		in.Provider = "openrouter"
	}
	if strings.TrimSpace(in.Model) == "" {
		if strings.EqualFold(in.Provider, "openrouter") {
			in.Model = "google/gemini-3.1-flash-lite-preview"
		}
	}
	if in.TimeoutSec <= 0 {
		in.TimeoutSec = 120
	}
	if in.MaxTokens <= 0 {
		in.MaxTokens = 900
	}
	if in.Temperature == 0 {
		in.Temperature = 0.1
	}

	resolver := skill.NewResolver(skill.WithSearchPaths(
		filepath.Join(rc.Workspace, "dist", "skills"),
		filepath.Join(rc.Workspace, "skills"),
	))
	ctx = workspace.WithContext(ctx, rc.Workspace)

	scoutIn := map[string]any{
		"path":        normalizedScoutPath(in.Path),
		"language":    in.Language,
		"focus":       in.Focus,
		"rule_set":    in.RuleSet,
		"min_score":   in.MinScore,
		"max_results": in.MaxFindings,
	}

	var scoutRes scoutData
	_, err := skillrun.RunAndDecodeInto(ctx, resolver, "code/refactor_scout", scoutIn, skillrun.Options{
		PreferCGO: buildinfo.IsCGO(),
		EntryRoot: rc.Workspace,
	}, &scoutRes)
	if err != nil {
		return skillerr.WrapRuntime("run code/refactor_scout", err)
	}
	if len(scoutRes.Findings) == 0 {
		return skillout.Emit(rc, command, map[string]any{
			"summary":         "No refactor findings met the requested threshold.",
			"scout_findings":  []scoutFinding{},
			"scout_summary":   scoutRes.Summary,
			"advisor_summary": "No shortlist generated.",
			"prioritized":     []advisorItem{},
			"provider":        in.Provider,
			"model":           in.Model,
			"focus":           in.Focus,
		})
	}

	candidates := toCandidateBriefs(scoutRes.Findings, in.MaxFindings)
	client, provider, model, err := resolveAdvisorLLM(rc, in)
	if err != nil {
		return skillerr.WrapRuntime("resolve llm", err)
	}

	systemPrompt := buildAdvisorSystemPrompt(in.ShortlistSize)
	userPrompt := buildAdvisorUserPrompt(in, candidates)

	callCtx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSec)*time.Second)
	defer cancel()
	raw, err := client.Chat(callCtx, systemPrompt, userPrompt, verification.LLMCallOptions{
		MaxTokens:   in.MaxTokens,
		Temperature: in.Temperature,
	})
	if err != nil {
		return skillerr.WrapRuntime("advisor llm chat", err)
	}
	if strings.TrimSpace(raw) == "" {
		fallback := fallbackFromTopCandidates(candidates, in.ShortlistSize, "Advisor model returned an empty response; using deterministic shortlist from local scout rankings.")
		data := map[string]any{
			"summary":        fallback.Summary,
			"prioritized":    fallback.Prioritized,
			"defer":          fallback.Defer,
			"sequence":       fallback.Sequence,
			"notes":          fallback.Notes,
			"scout_findings": candidates,
			"scout_summary":  scoutRes.Summary,
			"provider":       provider,
			"model":          model,
			"focus":          in.Focus,
			"raw_response":   raw,
		}
		return skillout.Emit(rc, command, data)
	}

	advice, err := parseAdvisorOutput(raw, candidates, in.ShortlistSize)
	if err != nil {
		return skillerr.WrapParse("parse advisor output", err, skillerr.WithHint("The advisor model must return JSON matching the documented schema."))
	}
	advice = clampAdvisorOutput(advice, candidates, in.ShortlistSize)

	data := map[string]any{
		"summary":        advice.Summary,
		"prioritized":    advice.Prioritized,
		"defer":          advice.Defer,
		"sequence":       advice.Sequence,
		"notes":          advice.Notes,
		"scout_findings": candidates,
		"scout_summary":  scoutRes.Summary,
		"provider":       provider,
		"model":          model,
		"focus":          in.Focus,
		"raw_response":   raw,
	}
	return skillout.Emit(rc, command, data)
}

func normalizedScoutPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "."
	}
	return path
}

func toCandidateBriefs(findings []scoutFinding, limit int) []candidateBrief {
	if limit > 0 && len(findings) > limit {
		findings = findings[:limit]
	}
	out := make([]candidateBrief, 0, len(findings))
	for _, item := range findings {
		out = append(out, candidateBrief{
			File:              item.File,
			Symbol:            item.Symbol,
			Line:              item.Line,
			Score:             item.Score,
			RuleID:            item.RuleID,
			Severity:          item.Severity,
			Title:             item.Title,
			Detail:            item.Detail,
			SuggestedRefactor: item.SuggestedRefactor,
			Signals:           append([]string(nil), item.Signals...),
		})
	}
	return out
}

func resolveAdvisorLLM(rc *skillmain.RunContext, in input) (verification.LLMClient, string, string, error) {
	provider := strings.TrimSpace(in.Provider)
	model := strings.TrimSpace(in.Model)
	baseURL := strings.TrimSpace(in.BaseURL)
	apiKey := strings.TrimSpace(in.APIKey)

	if baseURL == "" {
		baseURL = rc.Config.LLM.ResolveBaseURL(provider)
	}
	if apiKey == "" {
		apiKey = rc.Config.LLM.ResolveAPIKey(provider)
	}
	if model == "" {
		model = rc.Config.LLM.ResolveModel(provider)
	}
	if apiKey == "" {
		return nil, provider, model, fmt.Errorf("API key not configured for provider %q", provider)
	}
	client, err := verification.NewOpenAIClient(verification.OpenAIConfig{
		Provider: provider,
		BaseURL:  baseURL,
		APIKey:   apiKey,
		Model:    model,
		Timeout:  time.Duration(in.TimeoutSec) * time.Second,
	})
	if err != nil {
		return nil, provider, model, err
	}
	return client, provider, model, nil
}

func buildAdvisorSystemPrompt(shortlistSize int) string {
	return fmt.Sprintf(`You are a refactor planning judge.
You are given a ranked list of local structural refactor findings from a deterministic scout.
Your job is to choose the best %d starting points, explain why they outrank the others, and suggest a safe refactor order.

Return ONLY valid JSON with this exact shape. Do not use markdown. Do not use code fences. Do not add prose before or after the JSON.
{
  "summary": "...",
  "prioritized": [
    {
      "path": "repo/relative/path.go",
      "symbol": "OptionalSymbol",
      "priority": 1,
      "why": "...",
      "refactor_boundary": "...",
      "risk": "low|medium|high"
    }
  ],
  "defer": [
    {
      "path": "repo/relative/path.go",
      "symbol": "OptionalSymbol",
      "priority": 0,
      "why": "...",
      "refactor_boundary": "...",
      "risk": "low|medium|high"
    }
  ],
  "sequence": ["...", "..."],
  "notes": ["..."]
}

Rules:
- Only choose from the provided candidates.
- Prefer concrete function hotspots over broad file-only backlog items when the evidence supports it.
- Keep prioritized length <= %d.
- If two items are in the same file, explain why both still matter.
- Do not invent paths or symbols.
- If uncertain, still return valid JSON using the strongest available candidates.
`, shortlistSize, shortlistSize)
}

func buildAdvisorUserPrompt(in input, candidates []candidateBrief) string {
	payload := map[string]any{
		"path":           normalizedScoutPath(in.Path),
		"language":       in.Language,
		"focus":          in.Focus,
		"rule_set":       in.RuleSet,
		"shortlist_size": in.ShortlistSize,
		"candidates":     candidates,
	}
	body, _ := json.MarshalIndent(payload, "", "  ")
	return "Choose the best refactor starting points from these local scout findings.\n\n" + string(body)
}

func parseAdvisorOutput(raw string, candidates []candidateBrief, shortlistSize int) (advisorOutput, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return advisorOutput{}, fmt.Errorf("empty model response")
	}
	var out advisorOutput
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out, nil
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		if err := json.Unmarshal([]byte(raw[start:end+1]), &out); err == nil {
			return out, nil
		}
	}
	if fallback, ok := fallbackAdvisorOutput(raw, candidates, shortlistSize); ok {
		return fallback, nil
	}
	return advisorOutput{}, fmt.Errorf("response was not valid JSON")
}

func clampAdvisorOutput(out advisorOutput, candidates []candidateBrief, shortlistSize int) advisorOutput {
	index := make(map[string]candidateBrief, len(candidates))
	for _, item := range candidates {
		index[item.File+"::"+item.Symbol] = item
		index[item.File+"::"] = item
	}
	filter := func(items []advisorItem, allowPriority bool) []advisorItem {
		outItems := make([]advisorItem, 0, len(items))
		seen := map[string]struct{}{}
		for _, item := range items {
			key := strings.TrimSpace(item.Path) + "::" + strings.TrimSpace(item.Symbol)
			if _, ok := index[key]; !ok {
				if _, ok := index[strings.TrimSpace(item.Path)+"::"]; !ok {
					continue
				}
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			if allowPriority && item.Priority <= 0 {
				item.Priority = len(outItems) + 1
			}
			outItems = append(outItems, item)
			if allowPriority && shortlistSize > 0 && len(outItems) >= shortlistSize {
				break
			}
		}
		return outItems
	}
	out.Prioritized = filter(out.Prioritized, true)
	out.Defer = filter(out.Defer, false)
	return out
}

func fallbackAdvisorOutput(raw string, candidates []candidateBrief, shortlistSize int) (advisorOutput, bool) {
	type mention struct {
		candidate candidateBrief
		index     int
		symbolHit bool
	}
	lower := strings.ToLower(raw)
	mentions := make([]mention, 0, len(candidates))
	for _, candidate := range candidates {
		best := -1
		symbolHit := false
		for _, term := range candidateMentionTerms(candidate) {
			if idx := strings.Index(lower, strings.ToLower(term.value)); idx >= 0 && (best == -1 || idx < best) {
				best = idx
				symbolHit = term.symbol
			}
		}
		if best >= 0 {
			mentions = append(mentions, mention{candidate: candidate, index: best, symbolHit: symbolHit})
		}
	}
	if len(mentions) == 0 {
		return advisorOutput{}, false
	}
	sort.Slice(mentions, func(i, j int) bool {
		if mentions[i].symbolHit != mentions[j].symbolHit {
			return mentions[i].symbolHit
		}
		if mentions[i].index != mentions[j].index {
			return mentions[i].index < mentions[j].index
		}
		return mentions[i].candidate.Score > mentions[j].candidate.Score
	})
	seen := map[string]struct{}{}
	seenFilesWithSymbol := map[string]struct{}{}
	prioritized := make([]advisorItem, 0, shortlistSize)
	for _, item := range mentions {
		key := item.candidate.File + "::" + item.candidate.Symbol
		if _, ok := seen[key]; ok {
			continue
		}
		if item.candidate.Symbol == "" {
			if _, ok := seenFilesWithSymbol[item.candidate.File]; ok {
				continue
			}
		}
		seen[key] = struct{}{}
		if item.candidate.Symbol != "" {
			seenFilesWithSymbol[item.candidate.File] = struct{}{}
		}
		prioritized = append(prioritized, advisorItem{
			Path:             item.candidate.File,
			Symbol:           item.candidate.Symbol,
			Priority:         len(prioritized) + 1,
			Why:              extractSupportLine(raw, item.candidate),
			RefactorBoundary: item.candidate.SuggestedRefactor,
			Risk:             item.candidate.Severity,
		})
		if shortlistSize > 0 && len(prioritized) >= shortlistSize {
			break
		}
	}
	if len(prioritized) == 0 {
		return advisorOutput{}, false
	}
	return advisorOutput{
		Summary:     "Model returned prose instead of JSON; shortlist inferred from mentioned candidates.",
		Prioritized: prioritized,
		Notes: []string{
			skillout.TruncateSingleLine(raw, 240),
		},
	}, true
}

func fallbackFromTopCandidates(candidates []candidateBrief, shortlistSize int, summary string) advisorOutput {
	if shortlistSize <= 0 || shortlistSize > len(candidates) {
		shortlistSize = len(candidates)
	}
	prioritized := make([]advisorItem, 0, shortlistSize)
	deferItems := make([]advisorItem, 0, maxInt(0, len(candidates)-shortlistSize))
	sequence := make([]string, 0, shortlistSize)
	for i, candidate := range candidates {
		item := advisorItem{
			Path:             candidate.File,
			Symbol:           candidate.Symbol,
			Priority:         i + 1,
			Why:              candidate.Detail,
			RefactorBoundary: candidate.SuggestedRefactor,
			Risk:             candidate.Severity,
		}
		if i < shortlistSize {
			prioritized = append(prioritized, item)
			sequence = append(sequence, formatAdvisorSequenceItem(item))
			continue
		}
		item.Priority = 0
		deferItems = append(deferItems, item)
	}
	return advisorOutput{
		Summary:     summary,
		Prioritized: prioritized,
		Defer:       deferItems,
		Sequence:    sequence,
		Notes: []string{
			"Shortlist fell back to local scout ordering.",
		},
	}
}

func formatAdvisorSequenceItem(item advisorItem) string {
	path := strings.TrimSpace(item.Path)
	symbol := strings.TrimSpace(item.Symbol)
	if path == "" {
		return symbol
	}
	if symbol == "" {
		return path
	}
	return path + ":" + symbol
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func extractSupportLine(raw string, candidate candidateBrief) string {
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = sanitizeSupportLine(line)
		if line == "" {
			continue
		}
		lowerLine := strings.ToLower(line)
		for _, term := range candidateMentionTerms(candidate) {
			if strings.Contains(lowerLine, strings.ToLower(term.value)) {
				return line
			}
		}
	}
	if candidate.Detail != "" {
		return candidate.Detail
	}
	return candidate.Title
}

type candidateMentionTerm struct {
	value  string
	symbol bool
}

func candidateMentionTerms(candidate candidateBrief) []candidateMentionTerm {
	terms := make([]candidateMentionTerm, 0, 6)
	add := func(value string, symbol bool) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		for _, existing := range terms {
			if existing.value == value {
				return
			}
		}
		terms = append(terms, candidateMentionTerm{value: value, symbol: symbol})
	}
	add(candidate.File, false)
	add(candidate.Symbol, true)
	symbol := strings.TrimSpace(candidate.Symbol)
	if symbol != "" {
		add(strings.TrimLeft(symbol, "*"), true)
		if idx := strings.LastIndex(symbol, "."); idx >= 0 && idx < len(symbol)-1 {
			add(symbol[idx+1:], true)
		} else {
			add(symbol, true)
		}
		clean := strings.TrimLeft(strings.TrimSpace(symbol), "*")
		if idx := strings.LastIndex(clean, "."); idx >= 0 && idx < len(clean)-1 {
			add(clean[idx+1:], true)
		}
	}
	return terms
}

func sanitizeSupportLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "`")
	line = strings.TrimSuffix(line, ",")
	line = strings.TrimSpace(line)
	line = strings.TrimLeftFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || r == '{'
	})
	line = strings.TrimRightFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || r == '}'
	})
	if strings.HasPrefix(strings.ToLower(line), "summary\":") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "summary\":"))
		line = strings.Trim(line, "\"")
	}
	return strings.TrimSpace(line)
}
