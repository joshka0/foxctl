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
	ID            string   `yaml:"id" json:"id"`
	Query         string   `yaml:"query" json:"query"`
	ExpectedAnyOf []string `yaml:"expected_any_of" json:"expected_any_of"`
	Notes         string   `yaml:"notes,omitempty" json:"notes,omitempty"`
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
	TopPaths         []string `json:"top_paths,omitempty"`
	FirstCorrectRank int      `json:"first_correct_rank,omitempty"`
	HitAt5           bool     `json:"hit_at_5"`
	HitAt10          bool     `json:"hit_at_10"`
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
	HitRateAt5         float64 `json:"hit_rate_at_5"`
	HitRateAt10        float64 `json:"hit_rate_at_10"`
	MeanReciprocalRank float64 `json:"mean_reciprocal_rank"`
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
	result := ModeResult{
		Mode:      mode,
		Available: err == nil,
		TotalHits: totalHits,
		TopPaths:  append([]string(nil), hitPaths...),
	}
	if err != nil {
		result.Error = err.Error()
		return result
	}
	normalizedExpected := normalizeExpected(expected)
	for i, hit := range hitPaths {
		if containsPath(normalizedExpected, hit) {
			result.FirstCorrectRank = i + 1
			result.HitAt5 = i < 5
			result.HitAt10 = i < 10
			break
		}
	}
	return result
}

func Summarize(results []QueryResult, modes []string) []Summary {
	out := make([]Summary, 0, len(modes))
	for _, mode := range modes {
		s := Summary{Mode: mode, Queries: len(results)}
		var rrSum float64
		for _, query := range results {
			modeResult, ok := query.Modes[mode]
			if !ok || !modeResult.Available {
				continue
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
		}
		if s.Available > 0 {
			s.HitRateAt5 /= float64(s.Available)
			s.HitRateAt10 /= float64(s.Available)
			s.MeanReciprocalRank = rrSum / float64(s.Available)
		}
		out = append(out, s)
	}
	return out
}

func RenderMarkdown(result RunResult) string {
	var b strings.Builder
	b.WriteString("# Retrieval Eval\n\n")
	b.WriteString(fmt.Sprintf("- Suite: `%s`\n", result.Suite))
	b.WriteString(fmt.Sprintf("- Workspace: `%s`\n", result.Workspace))
	b.WriteString(fmt.Sprintf("- Vault: `%s`\n", result.VaultPath))
	b.WriteString(fmt.Sprintf("- Limit: `%d`\n\n", result.Limit))
	b.WriteString("## Summary\n\n")
	b.WriteString("| Mode | Available | hit@5 | hit@10 | MRR |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: |\n")
	for _, s := range result.Summaries {
		b.WriteString(fmt.Sprintf("| %s | %d/%d | %.2f | %.2f | %.2f |\n", s.Mode, s.Available, s.Queries, s.HitRateAt5, s.HitRateAt10, s.MeanReciprocalRank))
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
			b.WriteString(line + fmt.Sprintf("rank=%d hit@5=%t hit@10=%t\n", m.FirstCorrectRank, m.HitAt5, m.HitAt10))
			for _, path := range m.TopPaths[:minInt(len(m.TopPaths), 3)] {
				b.WriteString("  - `" + path + "`\n")
			}
		}
		b.WriteString("\n")
	}
	return b.String()
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
		"baseline":                   0,
		"lexical":                    1,
		"semantic":                   2,
		"blended":                    3,
		"skill_default":              4,
		"skill_context":              5,
		"skill_default_plus_context": 6,
		"aca_control_only":           7,
		"aca_vault_only":             8,
		"aca_repo_hints":             9,
		"aca_canonical_only":         10,
		"aca_package_fallback":       11,
		"aca_query_typed":            12,
		"repoindex_search":           13,
		"repoindex_dag":              14,
		"rlm_llm":                    15,
		"rlm_llm_codeintel":          16,
		"rlm_llm_code_staged":        17,
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
