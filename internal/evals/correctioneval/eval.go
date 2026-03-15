package correctioneval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Case struct {
	ID                     string   `yaml:"id" json:"id"`
	Method                 string   `yaml:"method" json:"method"`
	Query                  string   `yaml:"query" json:"query"`
	ExpectedAnyOf          []string `yaml:"expected_any_of,omitempty" json:"expected_any_of,omitempty"`
	ExpectedClassification string   `yaml:"expected_classification" json:"expected_classification"`
	ExpectedFixContains    string   `yaml:"expected_fix_contains,omitempty" json:"expected_fix_contains,omitempty"`
	Notes                  string   `yaml:"notes,omitempty" json:"notes,omitempty"`
}

type Suite struct {
	Name  string `yaml:"name" json:"name"`
	Cases []Case `yaml:"cases" json:"cases"`
}

type CaseResult struct {
	ID                     string `json:"id"`
	Method                 string `json:"method"`
	Query                  string `json:"query"`
	ExpectedClassification string `json:"expected_classification"`
	ActualClassification   string `json:"actual_classification"`
	ClassificationMatch    bool   `json:"classification_match"`
	ExpectedFixContains    string `json:"expected_fix_contains,omitempty"`
	ActualFix              string `json:"actual_fix,omitempty"`
	FixMatch               bool   `json:"fix_match"`
	FixChecked             bool   `json:"fix_checked"`
	Notes                  string `json:"notes,omitempty"`
	Error                  string `json:"error,omitempty"`
}

type Summary struct {
	Method                 string  `json:"method"`
	Cases                  int     `json:"cases"`
	Available              int     `json:"available"`
	ClassificationAccuracy float64 `json:"classification_accuracy"`
	FixAccuracy            float64 `json:"fix_accuracy"`
	FixChecks              int     `json:"fix_checks"`
}

type RunResult struct {
	Suite       string       `json:"suite"`
	Workspace   string       `json:"workspace"`
	VaultPath   string       `json:"vault_path,omitempty"`
	Cases       []CaseResult `json:"cases"`
	Summaries   []Summary    `json:"summaries"`
	GeneratedAt time.Time    `json:"generated_at"`
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

func Summarize(results []CaseResult) []Summary {
	grouped := map[string][]CaseResult{}
	for _, item := range results {
		grouped[item.Method] = append(grouped[item.Method], item)
	}
	methods := make([]string, 0, len(grouped))
	for method := range grouped {
		methods = append(methods, method)
	}
	sortStrings(methods)
	out := make([]Summary, 0, len(methods))
	for _, method := range methods {
		items := grouped[method]
		summary := Summary{
			Method: method,
			Cases:  len(items),
		}
		var classHits int
		var fixChecks int
		var fixHits int
		for _, item := range items {
			if item.Error == "" {
				summary.Available++
			}
			if item.ClassificationMatch {
				classHits++
			}
			if item.FixChecked {
				fixChecks++
				if item.FixMatch {
					fixHits++
				}
			}
		}
		if summary.Cases > 0 {
			summary.ClassificationAccuracy = float64(classHits) / float64(summary.Cases)
		}
		if fixChecks > 0 {
			summary.FixAccuracy = float64(fixHits) / float64(fixChecks)
		}
		summary.FixChecks = fixChecks
		out = append(out, summary)
	}
	return out
}

func RenderMarkdown(result RunResult) string {
	var b strings.Builder
	b.WriteString("# Correction Eval\n\n")
	b.WriteString(fmt.Sprintf("- Suite: `%s`\n", result.Suite))
	b.WriteString(fmt.Sprintf("- Workspace: `%s`\n", result.Workspace))
	if strings.TrimSpace(result.VaultPath) != "" {
		b.WriteString(fmt.Sprintf("- Vault: `%s`\n", result.VaultPath))
	}
	b.WriteString("\n## Summary\n\n")
	b.WriteString("| Method | Available | Class Acc | Fix Acc |\n")
	b.WriteString("| --- | ---: | ---: | ---: |\n")
	for _, s := range result.Summaries {
		fixValue := "n/a"
		if s.FixChecks > 0 {
			fixValue = fmt.Sprintf("%.2f", s.FixAccuracy)
		}
		b.WriteString(fmt.Sprintf("| %s | %d/%d | %.2f | %s |\n", s.Method, s.Available, s.Cases, s.ClassificationAccuracy, fixValue))
	}
	b.WriteString("\n## Cases\n\n")
	for _, c := range result.Cases {
		b.WriteString("### " + c.ID + "\n\n")
		b.WriteString("- Method: `" + c.Method + "`\n")
		b.WriteString("- Query: `" + c.Query + "`\n")
		b.WriteString("- Expected classification: `" + c.ExpectedClassification + "`\n")
		b.WriteString("- Actual classification: `" + c.ActualClassification + "`\n")
		b.WriteString(fmt.Sprintf("- Classification match: `%t`\n", c.ClassificationMatch))
		if c.FixChecked {
			b.WriteString("- Expected fix contains: `" + c.ExpectedFixContains + "`\n")
			b.WriteString("- Actual fix: `" + c.ActualFix + "`\n")
			b.WriteString(fmt.Sprintf("- Fix match: `%t`\n", c.FixMatch))
		}
		if c.Notes != "" {
			b.WriteString("- Notes: " + c.Notes + "\n")
		}
		if c.Error != "" {
			b.WriteString("- Error: `" + c.Error + "`\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func sortStrings(items []string) {
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j] < items[i] {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}
