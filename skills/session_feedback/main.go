// Package main implements the session/feedback skill for capturing session quality feedback.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/memory"
	"github.com/oklog/ulid/v2"
)

// Input defines the skill input parameters.
type Input struct {
	SessionID       string   `json:"session_id,omitempty"`
	Workspace       string   `json:"workspace"`
	Rating          int      `json:"rating"`                     // 1-5
	Outcome         string   `json:"outcome"`                    // success, partial, failure, abandoned
	WhatWorked      []string `json:"what_worked,omitempty"`      // Things that worked well
	WhatDidntWork   []string `json:"what_didnt_work,omitempty"`  // Things that didn't work
	Blockers        []string `json:"blockers,omitempty"`         // Blockers encountered
	Suggestions     []string `json:"suggestions,omitempty"`      // Improvement suggestions
	TaskID          string   `json:"task_id,omitempty"`          // Associated task
	ToolsUsed       []string `json:"tools_used,omitempty"`       // Tools/skills used
	DurationMinutes int      `json:"duration_minutes,omitempty"` // Session duration
	Notes           string   `json:"notes,omitempty"`            // Additional notes
}

// SessionFeedback represents the stored feedback structure.
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

// Output defines the skill output.
type Output struct {
	FeedbackID string `json:"feedback_id"`
	Message    string `json:"message"`
}

const command = "session/feedback"

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Validate required fields
	if in.Rating < 1 || in.Rating > 5 {
		return skillerr.Arg(fmt.Sprintf("rating must be between 1 and 5, got %d", in.Rating))
	}

	validOutcomes := map[string]bool{
		"success":   true,
		"partial":   true,
		"failure":   true,
		"abandoned": true,
	}
	if !validOutcomes[in.Outcome] {
		return skillerr.Arg(fmt.Sprintf("outcome must be one of: success, partial, failure, abandoned; got %q", in.Outcome))
	}

	// Default workspace
	in.Workspace = sessionkit.WorkspaceOrDefault(in.Workspace, rc.Workspace)

	// Resolve session ID
	sessionID := sessionkit.ResolveSessionID(in.Workspace, in.SessionID)

	// Open memory store in cache
	memStore, cleanup, err := sessionkit.OpenMemoryInCache(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open memory store", skillerr.WithCause(err))
	}
	defer cleanup()

	// Generate feedback ID
	feedbackID := ulid.Make().String()

	// Build feedback record
	feedback := SessionFeedback{
		FeedbackID:      feedbackID,
		SessionID:       sessionID,
		Workspace:       in.Workspace,
		Rating:          in.Rating,
		Outcome:         in.Outcome,
		WhatWorked:      in.WhatWorked,
		WhatDidntWork:   in.WhatDidntWork,
		Blockers:        in.Blockers,
		Suggestions:     in.Suggestions,
		TaskID:          in.TaskID,
		ToolsUsed:       in.ToolsUsed,
		DurationMinutes: in.DurationMinutes,
		Notes:           in.Notes,
		Timestamp:       timeutil.NowUTC(),
	}

	// Serialize feedback
	feedbackJSON, err := json.Marshal(feedback)
	if err != nil {
		return skillerr.Runtime("marshal feedback", skillerr.WithCause(err))
	}

	// Build summary
	summaryText := fmt.Sprintf("Session feedback: %s (%d/5)", in.Outcome, in.Rating)
	if in.TaskID != "" {
		summaryText = fmt.Sprintf("Session feedback for task %s: %s (%d/5)", in.TaskID, in.Outcome, in.Rating)
	}

	// Store in memory with type "session_feedback"
	memoryName := fmt.Sprintf("session-feedback-%s", feedbackID)
	_, err = memStore.SaveResult(ctx, memory.SaveOptions{
		Name:      memoryName,
		Type:      "session_feedback",
		Workspace: in.Workspace,
		Summary:   summaryText,
		Result:    feedbackJSON,
		SessionID: sessionID,
	})
	if err != nil {
		return skillerr.IO("save feedback", skillerr.WithCause(err))
	}

	// Output result
	output := Output{
		FeedbackID: feedbackID,
		Message:    fmt.Sprintf("Feedback recorded: %s (%s, %d/5)", feedbackID, in.Outcome, in.Rating),
	}

	return skillout.Emit(rc, command, output)
}
