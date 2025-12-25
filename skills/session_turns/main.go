// Package main implements the session/turns skill for querying turn patterns across sessions.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// Input defines the skill input parameters.
type Input struct {
	Query       string `json:"query,omitempty"`
	ErrorType   string `json:"error_type,omitempty"`
	ToolPattern string `json:"tool_pattern,omitempty"`
	Role        string `json:"role,omitempty"`
	ErrorsOnly  bool   `json:"errors_only,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// Output defines the skill output.
type Output struct {
	Query      string       `json:"query,omitempty"`
	Turns      []TurnResult `json:"turns"`
	TotalFound int          `json:"total_found"`
	Status     string       `json:"status"`
	Message    string       `json:"message"`
}

// TurnResult represents a turn found across sessions.
type TurnResult struct {
	SessionID      string     `json:"session_id"`
	ProjectName    string     `json:"project_name,omitempty"`
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
	command      = "session/turns"
	defaultLimit = 50
)

func main() {
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("DECODE_ERROR", fmt.Errorf("decode input: %w", err))
	}

	// At least one filter must be provided
	if input.Query == "" && input.ErrorType == "" && input.ToolPattern == "" && !input.ErrorsOnly {
		fail("INVALID_INPUT", fmt.Errorf("at least one filter required: query, error_type, tool_pattern, or errors_only"))
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

	results := []TurnResult{}

	// Search by query text
	if input.Query != "" {
		turns, err := sessionStore.SearchTurns(ctx, input.Query, input.Limit*2)
		if err != nil {
			fail("SEARCH_ERROR", fmt.Errorf("search turns: %w", err))
		}

		// Get session info for context and apply filters
		sessionCache := make(map[string]*sessions.Session)

		for _, t := range turns {
			// Apply additional filters
			if input.ErrorsOnly && !t.HasError {
				continue
			}
			if input.ErrorType != "" && !strings.EqualFold(t.ErrorType, input.ErrorType) {
				continue
			}
			if input.Role != "" && !strings.EqualFold(t.Role, input.Role) {
				continue
			}
			if input.ToolPattern != "" && !matchesToolPattern(t.ToolCalls, input.ToolPattern) {
				continue
			}

			// Get session for project name
			sess, ok := sessionCache[t.SessionID]
			if !ok {
				s, err := sessionStore.Get(ctx, t.SessionID)
				if err == nil {
					sess = &s
					sessionCache[t.SessionID] = sess
				}
			}

			result := TurnResult{
				SessionID:      t.SessionID,
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

			if sess != nil {
				result.ProjectName = sess.ProjectName
			}

			// Convert tool calls
			if len(t.ToolCalls) > 0 {
				result.ToolCalls = make([]ToolCall, len(t.ToolCalls))
				for i, tc := range t.ToolCalls {
					result.ToolCalls[i] = ToolCall{
						Name:    tc.Name,
						Success: tc.Success,
					}
				}
			}

			if !t.Timestamp.IsZero() {
				result.Timestamp = t.Timestamp.Format(time.RFC3339)
			}

			results = append(results, result)
			if len(results) >= input.Limit {
				break
			}
		}
	} else {
		// No text query - scan sessions and filter
		sessionList, err := sessionStore.List(ctx, sessions.ListOptions{Limit: 100})
		if err != nil {
			fail("LIST_ERROR", fmt.Errorf("list sessions: %w", err))
		}

		for _, sess := range sessionList {
			var turns []sessions.SessionTurn

			if input.ErrorsOnly {
				turns, err = sessionStore.GetTurnsWithErrors(ctx, sess.ID)
			} else {
				opts := sessions.TurnListOptions{
					SessionID: sess.ID,
					Role:      input.Role,
					Limit:     input.Limit,
				}
				turns, err = sessionStore.GetTurns(ctx, sess.ID, opts)
			}

			if err != nil {
				continue // Skip sessions with errors
			}

			for _, t := range turns {
				// Apply filters
				if input.ErrorType != "" && !strings.EqualFold(t.ErrorType, input.ErrorType) {
					continue
				}
				if input.ToolPattern != "" && !matchesToolPattern(t.ToolCalls, input.ToolPattern) {
					continue
				}

				result := TurnResult{
					SessionID:      sess.ID,
					ProjectName:    sess.ProjectName,
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
					result.ToolCalls = make([]ToolCall, len(t.ToolCalls))
					for i, tc := range t.ToolCalls {
						result.ToolCalls[i] = ToolCall{
							Name:    tc.Name,
							Success: tc.Success,
						}
					}
				}

				if !t.Timestamp.IsZero() {
					result.Timestamp = t.Timestamp.Format(time.RFC3339)
				}

				results = append(results, result)
				if len(results) >= input.Limit {
					break
				}
			}

			if len(results) >= input.Limit {
				break
			}
		}
	}

	output := Output{
		Query:      input.Query,
		Turns:      results,
		TotalFound: len(results),
		Status:     "ok",
		Message:    fmt.Sprintf("Found %d matching turns", len(results)),
	}

	if len(results) == 0 {
		output.Status = "no_matches"
		output.Message = "No turns matched the specified filters"
	}

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/turns result")
}

// matchesToolPattern checks if any tool call matches the pattern.
func matchesToolPattern(toolCalls []sessions.ToolCall, pattern string) bool {
	pattern = strings.ToLower(pattern)
	for _, tc := range toolCalls {
		if strings.Contains(strings.ToLower(tc.Name), pattern) {
			return true
		}
	}
	return false
}

func fail(code string, err error) {
	env := envelope.Error(command, code, err.Error(), nil)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/turns failure")
	os.Exit(1)
}
