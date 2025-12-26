// Package main implements the optimize/feedback skill for analyzing session feedback patterns.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// Input defines the skill input parameters.
type Input struct {
	Workspace string `json:"workspace,omitempty"`
	Since     string `json:"since,omitempty"`
	MinRating int    `json:"min_rating,omitempty"`
	MaxRating int    `json:"max_rating,omitempty"`
	Outcome   string `json:"outcome,omitempty"`
}

// SessionFeedback mirrors the structure from session_feedback skill.
type SessionFeedback struct {
	FeedbackID      string    `json:"feedback_id"`
	SessionID       string    `json:"session_id,omitempty"`
	Workspace       string    `json:"workspace"`
	Rating          int       `json:"rating"`
	Outcome         string    `json:"outcome"`
	WhatWorked      []string  `json:"what_worked,omitempty"`
	WhatDidntWork   []string  `json:"what_didnt_work,omitempty"`
	Blockers        []string  `json:"blockers,omitempty"`
	Suggestions     []string  `json:"suggestions,omitempty"`
	TaskID          string    `json:"task_id,omitempty"`
	ToolsUsed       []string  `json:"tools_used,omitempty"`
	DurationMinutes int       `json:"duration_minutes,omitempty"`
	Notes           string    `json:"notes,omitempty"`
	Timestamp       time.Time `json:"timestamp"`
}

// PatternCount tracks frequency of patterns.
type PatternCount struct {
	Pattern string `json:"pattern"`
	Count   int    `json:"count"`
}

// Recommendation represents an optimization recommendation.
type Recommendation struct {
	Priority    string `json:"priority"` // high, medium, low
	Category    string `json:"category"` // workflow, tooling, process
	Description string `json:"description"`
	Evidence    string `json:"evidence"`
}

// Output defines the skill output.
type Output struct {
	FeedbackCount       int              `json:"feedback_count"`
	AvgRating           float64          `json:"avg_rating"`
	OutcomeDistribution map[string]int   `json:"outcome_distribution"`
	TopSuccesses        []PatternCount   `json:"top_successes"`
	TopFailures         []PatternCount   `json:"top_failures"`
	TopBlockers         []PatternCount   `json:"top_blockers"`
	TopSuggestions      []PatternCount   `json:"top_suggestions"`
	ToolUsageStats      map[string]int   `json:"tool_usage_stats"`
	AvgDurationMinutes  float64          `json:"avg_duration_minutes,omitempty"`
	Recommendations     []Recommendation `json:"recommendations"`
}

const command = "optimize/feedback"

func main() {
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("EPARSE", fmt.Errorf("decode input: %w", err))
	}

	// Set defaults
	if input.MinRating == 0 {
		input.MinRating = 1
	}
	if input.MaxRating == 0 {
		input.MaxRating = 5
	}

	var sinceTime time.Time
	if input.Since != "" {
		var err error
		sinceTime, err = time.Parse(time.RFC3339, input.Since)
		if err != nil {
			sinceTime, err = time.Parse("2006-01-02", input.Since)
			if err != nil {
				fail("EINVALID", fmt.Errorf("invalid since date format: %w", err))
			}
		}
	}

	// Get agentctl home
	home := os.Getenv("AGENTCTL_HOME")
	if home == "" {
		homeDir, _ := os.UserHomeDir()
		home = filepath.Join(homeDir, ".agentctl")
	}

	// Open memory store
	cachePath := filepath.Join(home, "cache")
	casPath := filepath.Join(home, "cas")

	memStore, err := memory.Open(ctx, cachePath, casPath)
	if err != nil {
		fail("EIO", fmt.Errorf("open memory store: %w", err))
	}
	defer func() { errs.Ignore(memStore.Close(), "close memory store") }()

	// Get all feedback entries
	// Use a workspace of "*" to signify all, but memory.List requires a workspace
	// So we need to list entries and filter
	workspace := input.Workspace
	if workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			workspace = wd
		}
	}

	entries, err := memStore.List(ctx, workspace, 1000)
	if err != nil {
		fail("EIO", fmt.Errorf("list memory entries: %w", err))
	}

	// Filter for session_feedback type
	var feedbacks []SessionFeedback
	for _, entry := range entries {
		if entry.Type != "session_feedback" {
			continue
		}

		var fb SessionFeedback
		if err := json.Unmarshal(entry.Result, &fb); err != nil {
			continue
		}

		// Apply filters
		if fb.Rating < input.MinRating || fb.Rating > input.MaxRating {
			continue
		}
		if input.Outcome != "" && fb.Outcome != input.Outcome {
			continue
		}
		if !sinceTime.IsZero() && fb.Timestamp.Before(sinceTime) {
			continue
		}

		feedbacks = append(feedbacks, fb)
	}

	// Analyze patterns
	output := analyzeFeedback(feedbacks)

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit optimize/feedback result")
}

func analyzeFeedback(feedbacks []SessionFeedback) Output {
	output := Output{
		FeedbackCount:       len(feedbacks),
		OutcomeDistribution: make(map[string]int),
		ToolUsageStats:      make(map[string]int),
		Recommendations:     make([]Recommendation, 0),
	}

	if len(feedbacks) == 0 {
		return output
	}

	// Calculate averages and distributions
	var totalRating int
	var totalDuration int
	var durationCount int

	successCounts := make(map[string]int)
	failureCounts := make(map[string]int)
	blockerCounts := make(map[string]int)
	suggestionCounts := make(map[string]int)

	for _, fb := range feedbacks {
		totalRating += fb.Rating
		output.OutcomeDistribution[fb.Outcome]++

		if fb.DurationMinutes > 0 {
			totalDuration += fb.DurationMinutes
			durationCount++
		}

		for _, s := range fb.WhatWorked {
			successCounts[s]++
		}
		for _, s := range fb.WhatDidntWork {
			failureCounts[s]++
		}
		for _, s := range fb.Blockers {
			blockerCounts[s]++
		}
		for _, s := range fb.Suggestions {
			suggestionCounts[s]++
		}
		for _, t := range fb.ToolsUsed {
			output.ToolUsageStats[t]++
		}
	}

	output.AvgRating = float64(totalRating) / float64(len(feedbacks))
	if durationCount > 0 {
		output.AvgDurationMinutes = float64(totalDuration) / float64(durationCount)
	}

	// Sort patterns by frequency
	output.TopSuccesses = topPatterns(successCounts, 10)
	output.TopFailures = topPatterns(failureCounts, 10)
	output.TopBlockers = topPatterns(blockerCounts, 10)
	output.TopSuggestions = topPatterns(suggestionCounts, 10)

	// Generate recommendations based on patterns
	output.Recommendations = generateRecommendations(output)

	return output
}

func topPatterns(counts map[string]int, limit int) []PatternCount {
	patterns := make([]PatternCount, 0, len(counts))
	for pattern, count := range counts {
		patterns = append(patterns, PatternCount{Pattern: pattern, Count: count})
	}

	sort.Slice(patterns, func(i, j int) bool {
		return patterns[i].Count > patterns[j].Count
	})

	if len(patterns) > limit {
		patterns = patterns[:limit]
	}

	return patterns
}

func generateRecommendations(output Output) []Recommendation {
	recommendations := make([]Recommendation, 0)

	// Low average rating
	if output.AvgRating < 3.0 && output.FeedbackCount >= 3 {
		recommendations = append(recommendations, Recommendation{
			Priority:    "high",
			Category:    "process",
			Description: "Session quality is below average. Review top failures and blockers.",
			Evidence:    fmt.Sprintf("Average rating: %.1f/5 across %d sessions", output.AvgRating, output.FeedbackCount),
		})
	}

	// High failure rate
	failureCount := output.OutcomeDistribution["failure"] + output.OutcomeDistribution["abandoned"]
	successCount := output.OutcomeDistribution["success"] + output.OutcomeDistribution["partial"]
	if failureCount > successCount && output.FeedbackCount >= 3 {
		recommendations = append(recommendations, Recommendation{
			Priority:    "high",
			Category:    "process",
			Description: "More sessions ending in failure than success. Investigate root causes.",
			Evidence:    fmt.Sprintf("%d failures vs %d successes", failureCount, successCount),
		})
	}

	// Common blockers
	if len(output.TopBlockers) > 0 && output.TopBlockers[0].Count >= 2 {
		recommendations = append(recommendations, Recommendation{
			Priority:    "medium",
			Category:    "workflow",
			Description: fmt.Sprintf("Recurring blocker: %q. Consider automating or documenting workarounds.", output.TopBlockers[0].Pattern),
			Evidence:    fmt.Sprintf("Occurred in %d sessions", output.TopBlockers[0].Count),
		})
	}

	// User suggestions
	if len(output.TopSuggestions) > 0 && output.TopSuggestions[0].Count >= 2 {
		recommendations = append(recommendations, Recommendation{
			Priority:    "medium",
			Category:    "tooling",
			Description: fmt.Sprintf("User-suggested improvement: %q", output.TopSuggestions[0].Pattern),
			Evidence:    fmt.Sprintf("Suggested %d times", output.TopSuggestions[0].Count),
		})
	}

	// Long average duration
	if output.AvgDurationMinutes > 60 && output.FeedbackCount >= 3 {
		recommendations = append(recommendations, Recommendation{
			Priority:    "low",
			Category:    "workflow",
			Description: "Sessions averaging over an hour. Consider breaking tasks into smaller chunks.",
			Evidence:    fmt.Sprintf("Average duration: %.0f minutes", output.AvgDurationMinutes),
		})
	}

	// Successful patterns to reinforce
	if len(output.TopSuccesses) > 0 && output.TopSuccesses[0].Count >= 3 {
		recommendations = append(recommendations, Recommendation{
			Priority:    "low",
			Category:    "process",
			Description: fmt.Sprintf("Reinforce successful pattern: %q", output.TopSuccesses[0].Pattern),
			Evidence:    fmt.Sprintf("Worked well in %d sessions", output.TopSuccesses[0].Count),
		})
	}

	return recommendations
}

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit optimize/feedback failure")
	os.Exit(1)
}
