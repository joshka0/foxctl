// Package main implements the calibration/feedback skill for user-facing insights.
// It generates actionable feedback about communication patterns and preferences.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/calibration"
)

const command = "calibration/feedback"

// titleCase capitalizes the first character of a string (simple ASCII-only).
func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// Input represents the skill input parameters for calibration feedback.
type Input struct {
	Workspace string `json:"workspace,omitempty"`
	Format    string `json:"format,omitempty"` // "full" (default), "tips", or "summary"
}

// Output represents the skill output with calibration insights and recommendations.
type Output struct {
	Found            bool              `json:"found"`
	Insights         []Insight         `json:"insights,omitempty"`
	ExpertiseSummary *ExpertiseSummary `json:"expertise_summary,omitempty"`
	Trends           []Trend           `json:"trends,omitempty"`
	Tips             []string          `json:"tips,omitempty"`
	Status           string            `json:"status"`
	Message          string            `json:"message,omitempty"`
}

// Insight represents an actionable insight about the user's communication patterns.
type Insight struct {
	Category    string `json:"category"`    // Communication, Cognition, Trust, etc.
	Title       string `json:"title"`       // Short title
	Description string `json:"description"` // Full explanation
	Suggestion  string `json:"suggestion"`  // Actionable suggestion
}

// ExpertiseSummary summarizes the user's expertise profile with strengths and learning areas.
type ExpertiseSummary struct {
	Strong   []string `json:"strong"`
	Learning []string `json:"learning"`
	Gaps     []string `json:"gaps,omitempty"`
	Tip      string   `json:"tip"`
}

// Trend represents a detected preference trend over time.
type Trend struct {
	Dimension string `json:"dimension"`
	Direction string `json:"direction"` // increasing, decreasing, stable
	Note      string `json:"note"`
}

// main is the skill entry point for calibration/feedback.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates calibration feedback generation with multiple output formats and insight types.
//
// Index:
// - Purpose: Generate actionable feedback about communication patterns and preferences from calibration profiles
// - Flow: resolve workspace → open memory store → load profile → generate insights/tips/trends → emit formatted output
// - SideEffects: profile loading; insight generation; trend analysis; expertise summarization
// - FailureModes: workspace resolution failures, memory store access errors, missing profiles
// - Observability: emits insight counts, expertise summaries, trend analysis, and actionable communication tips
// - Related: generateInsights, generateExpertiseSummary, detectTrends, generateTips
// - Keywords: calibration/feedback, user_profile, communication_patterns, expertise_analysis, preference_trends
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Resolve workspace
	workspace := workspaceutil.Resolve(in.Workspace, "", rc.Workspace)
	if workspace == "" {
		return skillerr.Arg("workspace is required", skillerr.WithHint("Provide workspace path or run from a project directory"))
	}

	// Default format
	format := in.Format
	if format == "" {
		format = "full"
	}

	// Open memory store
	store, err := rc.Stores.Memory(ctx)
	if err != nil {
		return skillerr.IO("open memory store", skillerr.WithCause(err))
	}

	// Load profile
	profile, err := calibration.LoadProfile(ctx, store, workspace)
	if err != nil {
		return skillerr.IO("load profile", skillerr.WithCause(err))
	}

	if profile == nil {
		return skillout.Emit(rc, command, Output{
			Found:   false,
			Status:  "no_profile",
			Message: "No calibration profile found. Run calibration/generate first.",
		})
	}

	out := Output{
		Found:  true,
		Status: "ok",
	}

	// Generate insights based on format
	switch format {
	case "tips":
		out.Tips = generateTips(profile)
	case "summary":
		out.ExpertiseSummary = generateExpertiseSummary(profile)
		tips := generateTips(profile)
		n := len(tips)
		if n > 3 {
			n = 3
		}
		out.Tips = tips[:n] // Just top 3 tips
	case "full":
		out.Insights = generateInsights(profile)
		out.ExpertiseSummary = generateExpertiseSummary(profile)
		out.Trends = detectTrends(profile)
		out.Tips = generateTips(profile)
	default:
		return skillerr.Arg(fmt.Sprintf("invalid format: %s", format),
			skillerr.WithHint("Use one of: full, tips, summary"))
	}

	out.Message = fmt.Sprintf("Generated feedback based on %d analyzed windows", len(profile.WindowsAnalyzed))

	return skillout.Emit(rc, command, out)
}

// generateInsights creates actionable insights from the calibration profile across multiple dimensions.
func generateInsights(p *calibration.Profile) []Insight {
	var insights []Insight

	// Communication insights
	if p.Communication.Confidence > 0.5 {
		insights = append(insights, generateCommunicationInsight(p))
	}

	// Cognition insights
	if p.Cognition.Confidence > 0.3 {
		insights = append(insights, generateCognitionInsight(p))
	}

	// Trust insights
	if p.Trust.Confidence > 0.3 {
		insights = append(insights, generateTrustInsight(p))
	}

	// Working style insights
	if p.WorkingStyle.Confidence > 0.3 {
		insights = append(insights, generateWorkingStyleInsight(p))
	}

	return insights
}

// generateCommunicationInsight creates insights about verbosity and communication preferences.
func generateCommunicationInsight(p *calibration.Profile) Insight {
	var desc, suggestion string

	switch p.Communication.Verbosity {
	case calibration.VerbosityConcise:
		desc = "Analysis shows you prefer concise, to-the-point responses. You tend to ask follow-up questions when responses are too detailed."
		suggestion = "Start prompts with 'Briefly...' or 'In one sentence...' to signal your preference for concise responses."
	case calibration.VerbosityDetailed:
		desc = "You appreciate thorough, detailed explanations. You often ask for more context or 'why' behind decisions."
		suggestion = "Ask for 'comprehensive' or 'detailed' explanations explicitly when you need depth. The agent will match your preference."
	default:
		desc = "You adapt your communication needs based on context, sometimes wanting detail, sometimes brevity."
		suggestion = "Specify 'brief' or 'detailed' at the start of prompts when you have a specific need."
	}

	return Insight{
		Category:    "Communication",
		Title:       fmt.Sprintf("You prefer %s responses", p.Communication.Verbosity),
		Description: desc,
		Suggestion:  suggestion,
	}
}

// generateCognitionInsight creates insights about mental models and learning styles.
func generateCognitionInsight(p *calibration.Profile) Insight {
	var desc, suggestion string

	switch p.Cognition.MentalModel {
	case "visual":
		desc = "You often ask for diagrams, visual representations, or ASCII art. You think in pictures and spatial relationships."
		suggestion = "Ask for mermaid diagrams, ASCII trees, or visual explanations. Say 'draw me a diagram' to trigger visual responses."
	case "hierarchical":
		desc = "You organize information in tree structures and nested hierarchies. You like to see relationships and categories."
		suggestion = "Ask for bulleted lists, outlines, or 'break this down into components' to match your mental model."
	case "sequential":
		desc = "You prefer step-by-step explanations and ordered processes. You like to understand the flow."
		suggestion = "Ask for 'step by step' or numbered instructions when learning something new."
	default:
		desc = "You connect ideas through associations and relationships. You see patterns across domains."
		suggestion = "Ask 'how does this relate to X?' or 'what's similar to this?' to leverage your associative thinking."
	}

	return Insight{
		Category:    "Cognition",
		Title:       fmt.Sprintf("You're a %s thinker", p.Cognition.MentalModel),
		Description: desc,
		Suggestion:  suggestion,
	}
}

// generateTrustInsight creates insights about autonomy preferences and correction styles.
func generateTrustInsight(p *calibration.Profile) Insight {
	var desc, suggestion string

	switch p.Trust.AutonomyLevel {
	case "high":
		desc = "You're comfortable letting the AI work autonomously. You delegate tasks and trust the results."
		suggestion = "Your trust is well-calibrated! Consider using phrases like 'just do it' or 'handle this for me' to give explicit permission for autonomous action."
	case "low":
		desc = "You prefer to stay in control and verify each step. You review suggestions carefully before accepting."
		suggestion = "Ask the AI to 'explain before changing' or 'show me the plan first' to maintain your preferred level of control."
	default:
		desc = "You balance delegation with oversight, verifying critical changes while trusting routine ones."
		suggestion = "Explicitly mark important files or operations as 'critical - verify first' so the agent knows when to pause for approval."
	}

	return Insight{
		Category:    "Trust",
		Title:       fmt.Sprintf("%s autonomy preference", titleCase(p.Trust.AutonomyLevel)),
		Description: desc,
		Suggestion:  suggestion,
	}
}

// generateWorkingStyleInsight creates insights about problem-solving approaches and collaboration modes.
func generateWorkingStyleInsight(p *calibration.Profile) Insight {
	var desc, suggestion string

	switch p.WorkingStyle.ProblemApproach {
	case "iterative":
		desc = "You prefer to start simple and build up incrementally. You like frequent feedback loops."
		suggestion = "Use 'let's start with the basics' or 'MVP first' to signal your iterative approach."
	case "big-picture":
		desc = "You like to understand the full architecture before diving into details. You think in systems."
		suggestion = "Ask for 'the big picture' or 'overall architecture' before implementation details."
	case "test-driven":
		desc = "You write tests first and let them guide implementation. You value verified correctness."
		suggestion = "Say 'write tests first' or 'TDD approach' to ensure test-first development."
	default:
		desc = "You explore and experiment to understand a problem space before committing to a solution."
		suggestion = "Ask for 'exploration' or 'let's experiment' when you want to understand options before deciding."
	}

	return Insight{
		Category:    "Working Style",
		Title:       fmt.Sprintf("%s development approach", titleCase(p.WorkingStyle.ProblemApproach)),
		Description: desc,
		Suggestion:  suggestion,
	}
}

// generateExpertiseSummary creates a summary of expertise areas with actionable tips.
func generateExpertiseSummary(p *calibration.Profile) *ExpertiseSummary {
	summary := &ExpertiseSummary{}

	for _, d := range p.Expertise.StrongDomains {
		summary.Strong = append(summary.Strong, d.Name)
	}

	for _, d := range p.Expertise.LearningAreas {
		summary.Learning = append(summary.Learning, d.Name)
	}

	for _, d := range p.Expertise.KnowledgeGaps {
		summary.Gaps = append(summary.Gaps, d.Name)
	}

	// Generate tip based on expertise
	if len(summary.Strong) > 0 && len(summary.Learning) > 0 {
		summary.Tip = fmt.Sprintf("The agent will explain %s concepts more thoroughly while assuming your %s knowledge.",
			summary.Learning[0], summary.Strong[0])
	} else if len(summary.Strong) > 0 {
		summary.Tip = fmt.Sprintf("The agent recognizes your expertise in %s and will use appropriate technical depth.",
			strings.Join(summary.Strong, ", "))
	} else if len(summary.Learning) > 0 {
		summary.Tip = fmt.Sprintf("The agent will provide extra context for %s topics as you're learning them.",
			strings.Join(summary.Learning, ", "))
	} else {
		summary.Tip = "Build your expertise profile by working on more sessions. The agent will learn your strengths."
	}

	return summary
}

// detectTrends analyzes the timeline for preference trends with directional analysis.
func detectTrends(p *calibration.Profile) []Trend {
	var trends []Trend

	// Need at least 3 snapshots for trend detection
	if len(p.Timeline) < 3 {
		return trends
	}

	// Count dimension changes in recent snapshots (last 10)
	recentCount := 10
	if len(p.Timeline) < recentCount {
		recentCount = len(p.Timeline)
	}
	recentSnapshots := p.Timeline[len(p.Timeline)-recentCount:]

	// Track direction of changes per dimension
	directionCounts := make(map[string]map[string]int) // dimension -> direction -> count

	for _, snap := range recentSnapshots {
		for _, change := range snap.Changes {
			if directionCounts[change.Dimension] == nil {
				directionCounts[change.Dimension] = make(map[string]int)
			}
			// Determine direction based on value change
			direction := getChangeDirection(change.PreviousValue, change.NewValue)
			directionCounts[change.Dimension][direction]++
		}
	}

	// Report trends with clear direction
	for dim, counts := range directionCounts {
		inc := counts["increasing"]
		dec := counts["decreasing"]

		if inc > dec && inc >= 2 {
			trends = append(trends, Trend{
				Dimension: dim,
				Direction: "increasing",
				Note:      fmt.Sprintf("Your %s has been trending upward over the last %d sessions.", formatDimension(dim), recentCount),
			})
		} else if dec > inc && dec >= 2 {
			trends = append(trends, Trend{
				Dimension: dim,
				Direction: "decreasing",
				Note:      fmt.Sprintf("Your %s has been trending downward over the last %d sessions.", formatDimension(dim), recentCount),
			})
		}
	}

	return trends
}

// dimensionOrderings defines the orderings for known dimensions.
var dimensionOrderings = map[string][]string{
	"verbosity": {"concise", "moderate", "detailed"},
	"depth":     {"high-level", "moderate", "deep-dive"},
	"formality": {"casual", "adaptive", "formal"},
	"patience":  {"impatient", "moderate", "patient"},
	"autonomy":  {"low", "moderate", "high"},
	"code":      {"pseudocode", "snippets", "full-code"},
}

// getChangeDirection returns "increasing", "decreasing", or "unknown" based on value change.
func getChangeDirection(prev, next string) string {
	for _, order := range dimensionOrderings {
		prevIdx, nextIdx := -1, -1
		for i, v := range order {
			if v == prev {
				prevIdx = i
			}
			if v == next {
				nextIdx = i
			}
		}
		if prevIdx >= 0 && nextIdx >= 0 {
			if nextIdx > prevIdx {
				return "increasing"
			}
			if nextIdx < prevIdx {
				return "decreasing"
			}
			return "unknown" // same value
		}
	}
	return "unknown" // values not in any known ordering
}

// formatDimension converts a dimension key to human-readable text.
func formatDimension(dim string) string {
	parts := strings.Split(dim, ".")
	if len(parts) == 2 {
		return strings.ReplaceAll(parts[1], "_", " ")
	}
	return strings.ReplaceAll(dim, "_", " ")
}

// generateTips creates actionable communication tips based on profile preferences.
func generateTips(p *calibration.Profile) []string {
	var tips []string

	// Tip based on explanation style
	switch p.Communication.ExplanationStyle {
	case "examples-first":
		tips = append(tips, "Your profile shows you prefer examples before theory - prompts like 'Show me an example of X, then explain' will work well.")
	case "theory-first":
		tips = append(tips, "You prefer understanding concepts before seeing examples - ask 'Explain how X works, then show me an example'.")
	}

	// Tip based on corrections style
	switch p.Trust.CorrectionsStyle {
	case "direct":
		tips = append(tips, "You tend to give direct corrections - this is efficient! The agent won't take offense.")
	case "diplomatic":
		tips = append(tips, "You prefer diplomatic corrections - feel free to be more direct, the agent handles feedback well.")
	case "questioning":
		tips = append(tips, "You correct through questions - this helps the agent understand your thinking process.")
	}

	// Tip based on code preference
	switch p.Communication.CodePreference {
	case calibration.CodeFull:
		tips = append(tips, "You prefer complete, runnable code - say 'full implementation' to ensure you get complete code blocks.")
	case calibration.CodeSnippets:
		tips = append(tips, "You like focused code snippets - the agent will show key parts without boilerplate unless you ask.")
	case calibration.CodePseudocode:
		tips = append(tips, "You prefer pseudocode for understanding logic - ask for 'implementation' when you need real code.")
	}

	// Tip based on learning style
	switch p.Cognition.LearningStyle {
	case "hands-on":
		tips = append(tips, "You learn by doing - ask for 'try this' exercises or 'let me implement it' to reinforce learning.")
	case "reading":
		tips = append(tips, "You learn through reading - ask for documentation links or detailed written explanations.")
	}

	// Tip based on collaboration mode
	switch p.WorkingStyle.CollaborationMode {
	case "pair-programming":
		tips = append(tips, "You enjoy collaborative coding - the agent works well as a pair programming partner, thinking out loud with you.")
	case "autonomous":
		tips = append(tips, "You prefer autonomous work - give the agent clear goals and let it execute without micro-management.")
	case "review-based":
		tips = append(tips, "You prefer reviewing AI work - ask for proposals and plans before implementation.")
	}

	// General tip about profile age
	if len(p.WindowsAnalyzed) < 5 {
		tips = append(tips, "Your profile is still building - keep using the agent and it will better understand your preferences over time.")
	} else if len(p.WindowsAnalyzed) > 50 {
		tips = append(tips, fmt.Sprintf("Your profile is well-established with %d analyzed windows. The agent has a good understanding of your style.", len(p.WindowsAnalyzed)))
	}

	// Limit to 8 tips max
	if len(tips) > 8 {
		tips = tips[:8]
	}

	return tips
}

// Helper to title-case a string (deprecated in newer Go, but simple enough)
func init() {
	// Nothing needed, using strings.Title for simplicity
	_ = time.Now() // Avoid unused import
}
