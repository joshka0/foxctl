// Package main implements the session/feedback skill for capturing session quality feedback.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
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
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("EPARSE", fmt.Errorf("decode input: %w", err))
	}

	// Validate required fields
	if input.Rating < 1 || input.Rating > 5 {
		fail("EINVALID", fmt.Errorf("rating must be between 1 and 5, got %d", input.Rating))
	}

	validOutcomes := map[string]bool{
		"success":   true,
		"partial":   true,
		"failure":   true,
		"abandoned": true,
	}
	if !validOutcomes[input.Outcome] {
		fail("EINVALID", fmt.Errorf("outcome must be one of: success, partial, failure, abandoned; got %q", input.Outcome))
	}

	// Default workspace to current directory
	if input.Workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			input.Workspace = wd
		}
	}

	// Resolve session ID from input or environment
	sessionID := input.SessionID
	if sessionID == "" {
		sessionID = resolveSessionID()
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

	// Generate feedback ID
	feedbackID := ulid.Make().String()

	// Build feedback record
	feedback := SessionFeedback{
		FeedbackID:      feedbackID,
		SessionID:       sessionID,
		Workspace:       input.Workspace,
		Rating:          input.Rating,
		Outcome:         input.Outcome,
		WhatWorked:      input.WhatWorked,
		WhatDidntWork:   input.WhatDidntWork,
		Blockers:        input.Blockers,
		Suggestions:     input.Suggestions,
		TaskID:          input.TaskID,
		ToolsUsed:       input.ToolsUsed,
		DurationMinutes: input.DurationMinutes,
		Notes:           input.Notes,
		Timestamp:       timeutil.NowUTC(),
	}

	// Serialize feedback
	feedbackJSON, err := json.Marshal(feedback)
	if err != nil {
		fail("ERUNTIME", fmt.Errorf("marshal feedback: %w", err))
	}

	// Build summary
	summaryText := fmt.Sprintf("Session feedback: %s (%d/5)", input.Outcome, input.Rating)
	if input.TaskID != "" {
		summaryText = fmt.Sprintf("Session feedback for task %s: %s (%d/5)", input.TaskID, input.Outcome, input.Rating)
	}

	// Store in memory with type "session_feedback"
	memoryName := fmt.Sprintf("session-feedback-%s", feedbackID)
	_, err = memStore.SaveResult(ctx, memory.SaveOptions{
		Name:      memoryName,
		Type:      "session_feedback",
		Workspace: input.Workspace,
		Summary:   summaryText,
		Result:    feedbackJSON,
		SessionID: sessionID,
	})
	if err != nil {
		fail("EIO", fmt.Errorf("save feedback: %w", err))
	}

	// Output result
	output := Output{
		FeedbackID: feedbackID,
		Message:    fmt.Sprintf("Feedback recorded: %s (%s, %d/5)", feedbackID, input.Outcome, input.Rating),
	}

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/feedback result")
}

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/feedback failure")
	os.Exit(1)
}

// resolveSessionID returns the session ID from environment variables.
// Priority: AGENTCTL_SESSION_ID > CLAUDE_SESSION_ID > OPENCODE_SESSION_ID >
// CURSOR_SESSION_ID > TERM_SESSION_ID. Returns empty string if none set.
func resolveSessionID() string {
	for _, key := range []string{
		"AGENTCTL_SESSION_ID",
		"CLAUDE_SESSION_ID",
		"OPENCODE_SESSION_ID",
		"CURSOR_SESSION_ID",
		"TERM_SESSION_ID",
	} {
		if sid := os.Getenv(key); sid != "" {
			return sid
		}
	}
	return ""
}
