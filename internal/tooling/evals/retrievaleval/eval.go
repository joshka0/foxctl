package retrievaleval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Query struct {
	ID             string   `yaml:"id" json:"id"`
	Query          string   `yaml:"query" json:"query"`
	ExpectedAnyOf  []string `yaml:"expected_any_of" json:"expected_any_of"`
	ForbiddenAnyOf []string `yaml:"forbidden_any_of,omitempty" json:"forbidden_any_of,omitempty"`
	Notes          string   `yaml:"notes,omitempty" json:"notes,omitempty"`
}

type Suite struct {
	Name    string  `yaml:"name" json:"name"`
	Queries []Query `yaml:"queries" json:"queries"`
}

type ModeResult struct {
	Mode             string   `json:"mode"`
	Available        bool     `json:"available"`
	Error            string   `json:"error,omitempty"`
	TotalHits        int      `json:"total_hits"`
	DurationMS       int64    `json:"duration_ms,omitempty"`
	TopPaths         []string `json:"top_paths,omitempty"`
	Budget           *Budget  `json:"budget,omitempty"`
	FirstCorrectRank int      `json:"first_correct_rank,omitempty"`
	ForbiddenHit     bool     `json:"forbidden_hit,omitempty"`
	ForbiddenPaths   []string `json:"forbidden_paths,omitempty"`
	HitAt5           bool     `json:"hit_at_5"`
	HitAt10          bool     `json:"hit_at_10"`
}

type Budget struct {
	Limit                     int  `json:"limit,omitempty"`
	ReturnedHits              int  `json:"returned_hits"`
	ReturnedPaths             int  `json:"returned_paths"`
	ReturnedPathBytes         int  `json:"returned_path_bytes"`
	ReturnedPathTokenEstimate int  `json:"returned_path_token_estimate"`
	LimitReached              bool `json:"limit_reached,omitempty"`
}

type QueryResult struct {
	ID    string                `json:"id"`
	Query string                `json:"query"`
	Notes string                `json:"notes,omitempty"`
	Modes map[string]ModeResult `json:"modes"`
}

type Summary struct {
	Mode               string  `json:"mode"`
	Queries            int     `json:"queries"`
	Available          int     `json:"available"`
	ForbiddenHits      int     `json:"forbidden_hits,omitempty"`
	HitRateAt5         float64 `json:"hit_rate_at_5"`
	HitRateAt10        float64 `json:"hit_rate_at_10"`
	MeanReciprocalRank float64 `json:"mean_reciprocal_rank"`
	MeanDurationMS     float64 `json:"mean_duration_ms,omitempty"`
	MeanReturnedHits   float64 `json:"mean_returned_hits,omitempty"`
	MeanPathBytes      float64 `json:"mean_returned_path_bytes,omitempty"`
	MeanPathTokens     float64 `json:"mean_returned_path_token_estimate,omitempty"`
}

type RunResult struct {
	Suite       string        `json:"suite"`
	Workspace   string        `json:"workspace"`
	VaultPath   string        `json:"vault_path"`
	Limit       int           `json:"limit"`
	Queries     []QueryResult `json:"queries"`
	Summaries   []Summary     `json:"summaries"`
	GeneratedAt time.Time     `json:"generated_at"`
}

func LoadSuite(path string) (Suite, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Suite{}, err
	}
	var suite Suite
	if err := yaml.Unmarshal(body, &suite); err != nil {
		return Suite{}, fmt.Errorf("decode suite yaml: %w", err)
	}
	if strings.TrimSpace(suite.Name) == "" {
		suite.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return suite, nil
}

func EvaluateMode(mode string, hitPaths, expected []string, totalHits int, err error) ModeResult {
	return EvaluateModeWithForbidden(mode, hitPaths, expected, nil, totalHits, err)
}

func EvaluateModeWithForbidden(mode string, hitPaths, expected, forbidden []string, totalHits int, err error) ModeResult {
	return EvaluateModeWithBudget(mode, hitPaths, expected, forbidden, totalHits, 0, err)
}

func EvaluateModeWithBudget(mode string, hitPaths, expected, forbidden []string, totalHits, limit int, err error) ModeResult {
	return EvaluateModeWithBudgetAndDuration(mode, hitPaths, expected, forbidden, totalHits, limit, 0, err)
}

func EvaluateModeWithBudgetAndDuration(mode string, hitPaths, expected, forbidden []string, totalHits, limit int, duration time.Duration, err error) ModeResult {
	result := ModeResult{
		Mode:       mode,
		Available:  err == nil,
		TotalHits:  totalHits,
		DurationMS: duration.Milliseconds(),
		TopPaths:   append([]string(nil), hitPaths...),
		Budget:     NewBudget(hitPaths, totalHits, limit),
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	normalizedExpected := normalizeExpected(expected)
	normalizedForbidden := normalizeExpected(forbidden)
	for i, hit := range hitPaths {
		if containsPath(normalizedExpected, hit) {
			result.FirstCorrectRank = i + 1
			result.HitAt5 = i < 5
			result.HitAt10 = i < 10
			break
		}
	}
	for _, hit := range hitPaths {
		if containsPath(normalizedForbidden, hit) {
			result.ForbiddenHit = true
			result.ForbiddenPaths = append(result.ForbiddenPaths, filepath.ToSlash(strings.TrimSpace(hit)))
		}
	}
	return result
}

func NewBudget(hitPaths []string, totalHits, limit int) *Budget {
	budget := &Budget{
		Limit:         limit,
		ReturnedHits:  totalHits,
		ReturnedPaths: len(hitPaths),
		LimitReached:  limit > 0 && len(hitPaths) >= limit,
	}
	if totalHits < len(hitPaths) {
		budget.ReturnedHits = len(hitPaths)
	}
	text := strings.Join(hitPaths, "\n")
	budget.ReturnedPathBytes = len([]byte(text))
	budget.ReturnedPathTokenEstimate = estimateTokens(text)
	return budget
}

func Summarize(results []QueryResult, modes []string) []Summary {
	out := make([]Summary, 0, len(modes))
	for _, mode := range modes {
		s := Summary{Mode: mode, Queries: len(results)}
		var rrSum float64
		var returnedHits, pathBytes, pathTokens int
		var durationMS int64
		for _, query := range results {
			modeResult, ok := query.Modes[mode]
			if !ok || !modeResult.Available {
				continue
			}
			if modeResult.ForbiddenHit {
				s.ForbiddenHits++
			}
			s.Available++
			if modeResult.HitAt5 {
				s.HitRateAt5++
			}
			if modeResult.HitAt10 {
				s.HitRateAt10++
			}
			if modeResult.FirstCorrectRank > 0 {
				rrSum += 1.0 / float64(modeResult.FirstCorrectRank)
			}
			durationMS += modeResult.DurationMS
			if modeResult.Budget != nil {
				returnedHits += modeResult.Budget.ReturnedHits
				pathBytes += modeResult.Budget.ReturnedPathBytes
				pathTokens += modeResult.Budget.ReturnedPathTokenEstimate
			}
		}
		if s.Available > 0 {
			s.HitRateAt5 /= float64(s.Available)
			s.HitRateAt10 /= float64(s.Available)
			s.MeanReciprocalRank = rrSum / float64(s.Available)
			s.MeanDurationMS = float64(durationMS) / float64(s.Available)
			s.MeanReturnedHits = float64(returnedHits) / float64(s.Available)
			s.MeanPathBytes = float64(pathBytes) / float64(s.Available)
			s.MeanPathTokens = float64(pathTokens) / float64(s.Available)
		}
		out = append(out, s)
	}
	return out
}

func RenderMarkdown(result RunResult) string {
	var b strings.Builder
	b.WriteString("# Retrieval Eval\n\n")
	fmt.Fprintf(&b, "- Suite: `%s`\n", result.Suite)
	fmt.Fprintf(&b, "- Workspace: `%s`\n", result.Workspace)
	fmt.Fprintf(&b, "- Vault: `%s`\n", result.VaultPath)
	fmt.Fprintf(&b, "- Limit: `%d`\n\n", result.Limit)
	b.WriteString("## Summary\n\n")
	b.WriteString("| Mode | Available | forbidden | hit@5 | hit@10 | MRR | avg ms | avg hits | avg path bytes | avg path tokens |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, s := range result.Summaries {
		b.WriteString(fmt.Sprintf("| %s | %d/%d | %d | %.2f | %.2f | %.2f | %.1f | %.1f | %.1f | %.1f |\n", s.Mode, s.Available, s.Queries, s.ForbiddenHits, s.HitRateAt5, s.HitRateAt10, s.MeanReciprocalRank, s.MeanDurationMS, s.MeanReturnedHits, s.MeanPathBytes, s.MeanPathTokens))
	}
	b.WriteString("\n## Queries\n\n")
	for _, q := range result.Queries {
		b.WriteString("### " + q.ID + "\n\n")
		b.WriteString("- Query: `" + q.Query + "`\n")
		if q.Notes != "" {
			b.WriteString("- Notes: " + q.Notes + "\n")
		}
		for _, mode := range sortedModeKeys(q.Modes) {
			m := q.Modes[mode]
			line := fmt.Sprintf("- %s: ", mode)
			if !m.Available {
				b.WriteString(line + "unavailable")
				if m.Error != "" {
					b.WriteString(" (" + m.Error + ")")
				}
				b.WriteString("\n")
				continue
			}
			b.WriteString(line + fmt.Sprintf("rank=%d hit@5=%t hit@10=%t forbidden=%t", m.FirstCorrectRank, m.HitAt5, m.HitAt10, m.ForbiddenHit))
			if m.DurationMS > 0 {
				b.WriteString(fmt.Sprintf(" ms=%d", m.DurationMS))
			}
			if m.Budget != nil {
				b.WriteString(fmt.Sprintf(" budget=%d/%d paths bytes=%d est_tokens=%d", m.Budget.ReturnedPaths, m.Budget.Limit, m.Budget.ReturnedPathBytes, m.Budget.ReturnedPathTokenEstimate))
			}
			b.WriteString("\n")
			for _, path := range m.TopPaths[:minInt(len(m.TopPaths), 3)] {
				b.WriteString("  - `" + path + "`\n")
			}
			for _, path := range m.ForbiddenPaths {
				b.WriteString("  - forbidden: `" + path + "`\n")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func estimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	runes := len([]rune(text))
	tokens := (runes + 3) / 4
	if tokens < 1 {
		return 1
	}
	return tokens
}

func normalizeExpected(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(strings.TrimPrefix(item, "path:"))
		item = filepath.ToSlash(item)
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func containsPath(expected []string, hit string) bool {
	hit = filepath.ToSlash(strings.TrimSpace(hit))
	for _, item := range expected {
		if hit == item {
			return true
		}
	}
	return false
}

func sortedModeKeys(m map[string]ModeResult) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	// fixed stable order if present
	order := map[string]int{
		"baseline":                        0,
		"lexical":                         1,
		"semantic":                        2,
		"blended":                         3,
		"skill_default":                   4,
		"skill_context":                   5,
		"skill_default_plus_context":      6,
		"contextwiki_control_only":        7,
		"contextwiki_vault_only":          8,
		"contextwiki_repo_hints":          9,
		"contextwiki_canonical_only":      10,
		"contextwiki_package_fallback":    11,
		"contextwiki_query_typed":         12,
		"contextwiki_default":             13,
		"contextwiki_semantic_anchors":    14,
		"contextwiki_cochange":            15,
		"contextwiki_cochange_continuity": 16,
		"cochange_artifacts":              17,
		"repoindex_search":                18,
		"repoindex_semantic_search":       19,
		"repoindex_dag":                   20,
		"repoindex_semantic_dag":          21,
		"rlm_llm":                         22,
		"rlm_llm_codeintel":               23,
		"rlm_llm_code_staged":             24,
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			ri, iok := order[keys[i]]
			rj, jok := order[keys[j]]
			if !iok {
				ri = 99
			}
			if !jok {
				rj = 99
			}
			if rj < ri || (rj == ri && keys[j] < keys[i]) {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
