// Package main implements the session/expand skill for retrieving session turns.
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
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// Input defines the skill input parameters.
type Input struct {
	SessionID  string `json:"session_id"`
	ErrorsOnly bool   `json:"errors_only,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

// Output defines the skill output.
type Output struct {
	SessionID  string       `json:"session_id"`
	Session    *SessionInfo `json:"session,omitempty"`
	Turns      []TurnInfo   `json:"turns"`
	TotalTurns int          `json:"total_turns"`
	ErrorCount int          `json:"error_count"`
	Status     string       `json:"status"`
	Message    string       `json:"message"`
}

// SessionInfo provides summary info about the session.
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

// TurnInfo represents a turn in the conversation.
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

// ToolCall represents a tool invocation.
type ToolCall struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
}

const (
	command      = "session/expand"
	defaultLimit = 100
)

func main() {
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("DECODE_ERROR", fmt.Errorf("decode input: %w", err))
	}

	if input.SessionID == "" {
		fail("INVALID_INPUT", fmt.Errorf("session_id is required"))
	}

	if input.Limit <= 0 {
		input.Limit = defaultLimit
	}

	// Get agentctl home
	agentctlHome := os.Getenv("AGENTCTL_HOME")
	if agentctlHome == "" {
		homeDir, _ := os.UserHomeDir()
		agentctlHome = filepath.Join(homeDir, ".agentctl")
	}

	// Open sessions store
	storageRoot := filepath.Join(agentctlHome, "storage")
	sessionStore, err := sessions.Open(ctx, storageRoot)
	if err != nil {
		fail("STORE_ERROR", fmt.Errorf("open sessions store: %w", err))
	}
	defer func() { errs.Ignore(sessionStore.Close(), "close sessions store") }()

	// Get session metadata
	session, err := sessionStore.Get(ctx, input.SessionID)
	if err != nil {
		fail("NOT_FOUND", fmt.Errorf("session not found: %w", err))
	}

	// Get turns
	var turns []storage.SessionTurn
	if input.ErrorsOnly {
		turns, err = sessionStore.GetTurnsWithErrors(ctx, input.SessionID)
	} else {
		opts := storage.SessionTurnListOptions{
			SessionID: input.SessionID,
			Limit:     input.Limit,
		}
		turns, err = sessionStore.GetTurns(ctx, input.SessionID, opts)
	}
	if err != nil {
		fail("QUERY_ERROR", fmt.Errorf("get turns: %w", err))
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
		SessionID:  input.SessionID,
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

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/expand result")
}

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/expand failure")
	os.Exit(1)
}
