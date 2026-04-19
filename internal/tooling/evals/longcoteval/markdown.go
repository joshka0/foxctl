package longcoteval

import (
	"fmt"
	"sort"
	"strings"
)

const leaderboardWarning = "Internal paired A/B eval. Scaffolded/tool-augmented conditions are not LongCoT leaderboard comparable."

func RenderMarkdown(result RunResult) string {
	var b strings.Builder
	b.WriteString("# LongCoT × RLM Eval\n\n")
	b.WriteString("> " + leaderboardWarning + "\n\n")
	if strings.TrimSpace(result.RunID) != "" {
		fmt.Fprintf(&b, "- Run: `%s`\n", result.RunID)
	}
	if !result.GeneratedAt.IsZero() {
		fmt.Fprintf(&b, "- Generated: `%s`\n", result.GeneratedAt.UTC().Format("2006-01-02T15:04:05Z"))
	}
	fmt.Fprintf(&b, "- Questions: `%d`\n", len(result.Questions))
	fmt.Fprintf(&b, "- Attempts: `%d`\n\n", len(result.Attempts))

	b.WriteString("## Condition Summary\n\n")
	b.WriteString("| Condition | attempts | verified | correct | wrong-format | leaked | mean tokens | mean cost | mean ms |\n")
	b.WriteString("| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	conditions := append([]ConditionSummary(nil), result.Summary.Conditions...)
	sort.SliceStable(conditions, func(i, j int) bool { return conditions[i].ConditionID < conditions[j].ConditionID })
	for _, item := range conditions {
		fmt.Fprintf(&b, "| `%s` | %d | %d | %d | %d | %d | %.0f | %.4f | %.0f |\n",
			item.ConditionID,
			item.Attempts,
			item.VerifiedAttempts,
			item.CorrectAttempts,
			item.WrongFormatting,
			item.LeakedAttempts,
			item.MeanTotalTokens,
			item.MeanCostUSD,
			item.MeanDurationMS,
		)
	}

	if len(result.Summary.Comparisons) > 0 {
		b.WriteString("\n## Paired Comparisons\n\n")
		b.WriteString("| Baseline | Candidate | pairs | wins | losses | tie correct | tie incorrect | mean token Δ | mean cost Δ | mean ms Δ |\n")
		b.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
		comparisons := append([]ComparisonSummary(nil), result.Summary.Comparisons...)
		sort.SliceStable(comparisons, func(i, j int) bool {
			left := string(comparisons[i].Baseline) + "\x00" + string(comparisons[i].Candidate)
			right := string(comparisons[j].Baseline) + "\x00" + string(comparisons[j].Candidate)
			return left < right
		})
		for _, item := range comparisons {
			fmt.Fprintf(&b, "| `%s` | `%s` | %d | %d | %d | %d | %d | %.0f | %.4f | %.0f |\n",
				item.Baseline,
				item.Candidate,
				item.Pairs,
				item.Wins,
				item.Losses,
				item.TieCorrect,
				item.TieIncorrect,
				item.MeanTokenDelta,
				item.MeanCostDeltaUSD,
				item.MeanDurationDelta,
			)
		}
	}

	if result.Summary.DuplicateAttempts > 0 {
		fmt.Fprintf(&b, "\nDuplicate attempts replaced by latest input order: `%d`\n", result.Summary.DuplicateAttempts)
	}
	return b.String()
}
