package calibration

import (
	"fmt"
	"strings"
)

// FormatCompact generates a token-efficient representation for system prompt injection.
// Target: ~200 tokens
func FormatCompact(p *Profile) string {
	if p == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## User Calibration\n\n")

	// Communication style
	sb.WriteString(fmt.Sprintf("**Communication**: %s explanations, %s technical depth, prefers %s\n",
		p.Communication.Verbosity,
		p.Communication.TechnicalDepth,
		p.Communication.CodePreference))

	// Tone
	sb.WriteString(fmt.Sprintf("**Tone**: %s style, %s approach, %s pace\n",
		p.Tone.Formality,
		p.Tone.Assertiveness,
		p.Tone.Patience))

	// Working style
	sb.WriteString(fmt.Sprintf("**Working Style**: %s development, %s feedback, %s collaboration\n",
		p.WorkingStyle.ProblemApproach,
		p.WorkingStyle.FeedbackStyle,
		p.WorkingStyle.CollaborationMode))

	// Cognition
	motivations := "balanced"
	if len(p.Cognition.Motivations) > 0 {
		motivations = strings.Join(p.Cognition.Motivations[:min(2, len(p.Cognition.Motivations))], ", ")
	}
	sb.WriteString(fmt.Sprintf("**Cognition**: %s thinker, %s learner, prioritizes %s\n",
		p.Cognition.MentalModel,
		p.Cognition.LearningStyle,
		motivations))

	// Expertise (compact)
	if len(p.Expertise.StrongDomains) > 0 || len(p.Expertise.LearningAreas) > 0 {
		sb.WriteString("**Expertise**: ")
		if len(p.Expertise.StrongDomains) > 0 {
			strong := make([]string, 0, 3)
			for i, d := range p.Expertise.StrongDomains {
				if i >= 3 {
					break
				}
				strong = append(strong, d.Name)
			}
			sb.WriteString(fmt.Sprintf("Strong in %s", strings.Join(strong, ", ")))
		}
		if len(p.Expertise.LearningAreas) > 0 {
			if len(p.Expertise.StrongDomains) > 0 {
				sb.WriteString(" | ")
			}
			learning := make([]string, 0, 2)
			for i, d := range p.Expertise.LearningAreas {
				if i >= 2 {
					break
				}
				learning = append(learning, d.Name)
			}
			sb.WriteString(fmt.Sprintf("Learning: %s", strings.Join(learning, ", ")))
		}
		sb.WriteString("\n")
	}

	// Trust
	sb.WriteString(fmt.Sprintf("**Trust**: %s autonomy, %s verification, %s corrections\n",
		p.Trust.AutonomyLevel,
		p.Trust.VerificationNeed,
		p.Trust.CorrectionsStyle))

	return sb.String()
}

// FormatDetailed generates a comprehensive profile representation.
func FormatDetailed(p *Profile) string {
	if p == nil {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("# User Calibration Profile\n\n")
	sb.WriteString(fmt.Sprintf("**Version**: %d | **Updated**: %s\n\n",
		p.Version, p.UpdatedAt.Format("2006-01-02 15:04")))

	// Communication
	sb.WriteString("## Communication Style\n\n")
	sb.WriteString("| Dimension | Value | Confidence |\n")
	sb.WriteString("|-----------|-------|------------|\n")
	sb.WriteString(fmt.Sprintf("| Verbosity | %s | %.0f%% |\n", p.Communication.Verbosity, p.Communication.Confidence*100))
	sb.WriteString(fmt.Sprintf("| Technical Depth | %s | - |\n", p.Communication.TechnicalDepth))
	sb.WriteString(fmt.Sprintf("| Explanation Style | %s | - |\n", p.Communication.ExplanationStyle))
	sb.WriteString(fmt.Sprintf("| Code Preference | %s | - |\n", p.Communication.CodePreference))
	sb.WriteString("\n")

	// Tone
	sb.WriteString("## Tone Profile\n\n")
	sb.WriteString("| Dimension | Value | Confidence |\n")
	sb.WriteString("|-----------|-------|------------|\n")
	sb.WriteString(fmt.Sprintf("| Formality | %s | %.0f%% |\n", p.Tone.Formality, p.Tone.Confidence*100))
	sb.WriteString(fmt.Sprintf("| Assertiveness | %s | - |\n", p.Tone.Assertiveness))
	sb.WriteString(fmt.Sprintf("| Patience | %s | - |\n", p.Tone.Patience))
	sb.WriteString("\n")

	// Working style
	sb.WriteString("## Working Style\n\n")
	sb.WriteString(fmt.Sprintf("- **Problem Approach**: %s\n", p.WorkingStyle.ProblemApproach))
	sb.WriteString(fmt.Sprintf("- **Feedback Style**: %s\n", p.WorkingStyle.FeedbackStyle))
	sb.WriteString(fmt.Sprintf("- **Collaboration Mode**: %s\n", p.WorkingStyle.CollaborationMode))
	if len(p.WorkingStyle.Patterns) > 0 {
		sb.WriteString("\n**Observed Patterns**:\n")
		for _, pat := range p.WorkingStyle.Patterns {
			sb.WriteString(fmt.Sprintf("- %s (seen %d times)\n", pat.Pattern, pat.Frequency))
		}
	}
	sb.WriteString("\n")

	// Cognition
	sb.WriteString("## Cognition Profile\n\n")
	sb.WriteString(fmt.Sprintf("- **Mental Model**: %s\n", p.Cognition.MentalModel))
	sb.WriteString(fmt.Sprintf("- **Learning Style**: %s\n", p.Cognition.LearningStyle))
	sb.WriteString(fmt.Sprintf("- **Problem Approach**: %s\n", p.Cognition.ProblemApproach))
	sb.WriteString(fmt.Sprintf("- **Decision Style**: %s\n", p.Cognition.DecisionStyle))
	if len(p.Cognition.Motivations) > 0 {
		sb.WriteString(fmt.Sprintf("- **Motivations**: %s\n", strings.Join(p.Cognition.Motivations, ", ")))
	}
	sb.WriteString("\n")

	// Expertise
	sb.WriteString("## Expertise Map\n\n")
	if len(p.Expertise.StrongDomains) > 0 {
		sb.WriteString("**Strong Domains**:\n")
		for _, d := range p.Expertise.StrongDomains {
			sb.WriteString(fmt.Sprintf("- %s (%s)\n", d.Name, d.Level))
		}
		sb.WriteString("\n")
	}
	if len(p.Expertise.LearningAreas) > 0 {
		sb.WriteString("**Learning Areas**:\n")
		for _, d := range p.Expertise.LearningAreas {
			sb.WriteString(fmt.Sprintf("- %s (%s)\n", d.Name, d.Level))
		}
		sb.WriteString("\n")
	}
	if len(p.Expertise.KnowledgeGaps) > 0 {
		sb.WriteString("**Knowledge Gaps**:\n")
		for _, d := range p.Expertise.KnowledgeGaps {
			sb.WriteString(fmt.Sprintf("- %s\n", d.Name))
		}
		sb.WriteString("\n")
	}

	// Trust
	sb.WriteString("## Trust Profile\n\n")
	sb.WriteString(fmt.Sprintf("- **Autonomy Level**: %s\n", p.Trust.AutonomyLevel))
	sb.WriteString(fmt.Sprintf("- **Verification Need**: %s\n", p.Trust.VerificationNeed))
	sb.WriteString(fmt.Sprintf("- **Pushback Pattern**: %s\n", p.Trust.PushbackPattern))
	sb.WriteString(fmt.Sprintf("- **Corrections Style**: %s\n", p.Trust.CorrectionsStyle))
	sb.WriteString("\n")

	// Timeline summary
	if len(p.Timeline) > 0 {
		sb.WriteString("## Recent Changes\n\n")
		// Show last 5 snapshots
		start := 0
		if len(p.Timeline) > 5 {
			start = len(p.Timeline) - 5
		}
		for _, snap := range p.Timeline[start:] {
			sb.WriteString(fmt.Sprintf("**%s** (%s):\n", snap.Timestamp.Format("Jan 2"), snap.Trigger))
			for _, change := range snap.Changes {
				sb.WriteString(fmt.Sprintf("- %s: %s → %s\n", change.Dimension, change.PreviousValue, change.NewValue))
			}
		}
		sb.WriteString("\n")
	}

	// Analysis stats
	sb.WriteString("## Analysis Stats\n\n")
	sb.WriteString(fmt.Sprintf("- **Windows Analyzed**: %d\n", len(p.WindowsAnalyzed)))
	sb.WriteString(fmt.Sprintf("- **Timeline Snapshots**: %d\n", len(p.Timeline)))

	return sb.String()
}

// FormatForInjection wraps the compact format in XML delimiters for session injection.
func FormatForInjection(p *Profile) string {
	if p == nil {
		return ""
	}
	compact := FormatCompact(p)
	return fmt.Sprintf("<user-calibration>\n%s</user-calibration>", compact)
}
