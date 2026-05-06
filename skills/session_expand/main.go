// Package main implements the session/expand skill for retrieving and analyzing session turns with comprehensive metadata.
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/storage"
)

// Input defines the skill input parameters for session expansion with filtering and limiting options.
type Input struct {
	SessionID  string `json:"session_id" validate:"required"`
	ErrorsOnly bool   `json:"errors_only,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

// Output defines the skill output with session metadata, turn details, and error statistics.
type Output struct {
	SessionID  string       `json:"session_id"`
	Session    *SessionInfo `json:"session,omitempty"`
	Turns      []TurnInfo   `json:"turns"`
	TotalTurns int          `json:"total_turns"`
	ErrorCount int          `json:"error_count"`
	Status     string       `json:"status"`
	Message    string       `json:"message"`
}

// SessionInfo provides summary info about the session with project details and accomplishments.
type SessionInfo struct {
	ProjectName  string   `json:"project_name"`
	GitBranch    string   `json:"git_branch,omitempty"`
	Summary      string   `json:"summary,omitempty"`
	Accomplished []string `json:"accomplished,omitempty"`
	Decisions    []string `json:"decisions,omitempty"`
	Gotchas      []string `json:"gotchas,omitempty"`
	StartedAt    string   `json:"started_at,omitempty"`
	EndedAt      string   `json:"ended_at,omitempty"`
	MessageCount int      `json:"message_count"`
	UserTurns    int      `json:"user_turns"`
}

// TurnInfo represents a turn in the conversation with tool calls, errors, and metadata.
type TurnInfo struct {
	TurnIndex      int        `json:"turn_index"`
	Role           string     `json:"role"`
	ContentPreview string     `json:"content_preview,omitempty"`
	ToolCalls      []ToolCall `json:"tool_calls,omitempty"`
	FilesTouched   []string   `json:"files_touched,omitempty"`
	HasError       bool       `json:"has_error"`
	ErrorType      string     `json:"error_type,omitempty"`
	ErrorMessage   string     `json:"error_message,omitempty"`
	Resolution     string     `json:"resolution,omitempty"`
	TokensUsed     int        `json:"tokens_used"`
	Timestamp      string     `json:"timestamp,omitempty"`
}

// ToolCall represents a tool invocation with success status tracking.
type ToolCall struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
}

const (
	command      = "session/expand"
	defaultLimit = 100
)

// main is the skill entry point for session/expand with comprehensive turn retrieval capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates session expansion with metadata retrieval, turn filtering, and comprehensive output formatting.
//
// Index:
//   Purpose: Retrieve and analyze session turns with optional error filtering, tool call tracking, and session metadata
//   Keywords: session/expand, turn_retrieval, session_analysis, error_tracking, tool_call_analysis
//   Related: sessionkit.OpenSessions, storage.SessionStore, storage.SessionTurn
//   Flow: validate input → open sessions store → get session metadata → retrieve turns (filtered or all) → format output → emit results
//   Resources: session store
//   Events: session expansion events
//   OutputFields: session_id, session, turns, total_turns, error_count
// [[domain:session-turn-expansion]]
// [[protocol:turn-filtering-by-errors]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	if in.Limit <= 0 {
		in.Limit = defaultLimit
	}

	// Open sessions store
	store, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}

	// Get session metadata
	session, err := store.Get(ctx, in.SessionID)
	if err != nil {
		return skillerr.Runtime("session not found",
			skillerr.WithCause(err),
			skillerr.WithData("session_id", in.SessionID))
	}

	// Get turns
	var turns []storage.SessionTurn
	if in.ErrorsOnly {
		turns, err = store.GetTurnsWithErrors(ctx, in.SessionID)
	} else {
		opts := storage.SessionTurnListOptions{
			SessionID: in.SessionID,
			Limit:     in.Limit,
		}
		turns, err = store.GetTurns(ctx, in.SessionID, opts)
	}
	if err != nil {
		return skillerr.Runtime("get turns", skillerr.WithCause(err))
	}

	// Convert to output format
	turnInfos := make([]TurnInfo, 0, len(turns))
	errorCount := 0
	for _, t := range turns {
		info := TurnInfo{
			TurnIndex:      t.TurnIndex,
			Role:           t.Role,
			ContentPreview: t.ContentPreview,
			FilesTouched:   t.FilesTouched,
			HasError:       t.HasError,
			ErrorType:      t.ErrorType,
			ErrorMessage:   t.ErrorMessage,
			Resolution:     t.Resolution,
			TokensUsed:     t.TokensUsed,
		}

		// Convert tool calls
		if len(t.ToolCalls) > 0 {
			info.ToolCalls = make([]ToolCall, len(t.ToolCalls))
			for i, tc := range t.ToolCalls {
				info.ToolCalls[i] = ToolCall{
					Name:    tc.Name,
					Success: tc.Success,
				}
			}
		}

		if !t.Timestamp.IsZero() {
			info.Timestamp = t.Timestamp.Format(time.RFC3339)
		}

		if t.HasError {
			errorCount++
		}

		turnInfos = append(turnInfos, info)
	}

	// Build session info
	sessionInfo := &SessionInfo{
		ProjectName:  session.ProjectName,
		GitBranch:    session.GitBranch,
		Summary:      session.Summary,
		Accomplished: session.Accomplished,
		Decisions:    session.Decisions,
		Gotchas:      session.Gotchas,
		MessageCount: session.MessageCount,
		UserTurns:    session.UserTurns,
	}
	if !session.StartedAt.IsZero() {
		sessionInfo.StartedAt = session.StartedAt.Format(time.RFC3339)
	}
	if !session.EndedAt.IsZero() {
		sessionInfo.EndedAt = session.EndedAt.Format(time.RFC3339)
	}

	output := Output{
		SessionID:  in.SessionID,
		Session:    sessionInfo,
		Turns:      turnInfos,
		TotalTurns: len(turnInfos),
		ErrorCount: errorCount,
		Status:     "ok",
		Message:    fmt.Sprintf("Retrieved %d turns for session", len(turnInfos)),
	}

	if len(turnInfos) == 0 {
		output.Status = "no_turns"
		output.Message = "No turns found for this session (turns may not have been extracted yet)"
	}

	return skillout.Emit(rc, command, output)
}
