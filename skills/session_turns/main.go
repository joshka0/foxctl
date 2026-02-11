// Package main implements the session/turns skill for querying turn patterns across sessions with comprehensive filtering.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// Input defines the skill input parameters for turn pattern querying with multiple filter options.
type Input struct {
	Query       string `json:"query,omitempty"`
	ErrorType   string `json:"error_type,omitempty"`
	ToolPattern string `json:"tool_pattern,omitempty"`
	Role        string `json:"role,omitempty"`
	ErrorsOnly  bool   `json:"errors_only,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

// Output defines the skill output with turn results and comprehensive metadata.
type Output struct {
	Query      string       `json:"query,omitempty"`
	Turns      []TurnResult `json:"turns"`
	TotalFound int          `json:"total_found"`
	Status     string       `json:"status"`
	Message    string       `json:"message"`
}

// TurnResult represents a turn found across sessions with detailed context and metadata.
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

// ToolCall represents a tool invocation with success status tracking.
type ToolCall struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
}

const (
	command      = "session/turns"
	defaultLimit = 50
)

// main is the skill entry point for session/turns with comprehensive turn pattern querying capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates turn pattern querying across sessions with multiple filter strategies and comprehensive result formatting.
//
// Index:
// - Purpose: Query turn patterns across sessions using text search, error filtering, tool pattern matching, and role filtering
// - Flow: validate filters → open sessions store → search or scan sessions → apply filters → collect results → format output → emit results
// - SideEffects: reads session metadata; accesses turn records; performs text searches; filters by multiple criteria; manages session cache
// - FailureModes: missing filters, store access errors, search failures, session retrieval errors, invalid filter combinations
// - Observability: emits turn results with session context, tool call information, error details, and comprehensive match statistics
// - Related: sessionkit.OpenSessions, sessions.SessionStore, matchesToolPattern
// - Keywords: session/turns, turn_pattern_query, session_search, error_filtering, tool_pattern_matching
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// At least one filter must be provided
	if in.Query == "" && in.ErrorType == "" && in.ToolPattern == "" && !in.ErrorsOnly {
		return skillerr.Arg("at least one filter required: query, error_type, tool_pattern, or errors_only")
	}

	if in.Limit <= 0 {
		in.Limit = defaultLimit
	}

	// Open sessions store
	store, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}

	results := []TurnResult{}

	// Search by query text
	if in.Query != "" {
		turns, err := store.SearchTurns(ctx, in.Query, in.Limit*2)
		if err != nil {
			return skillerr.Runtime("search turns", skillerr.WithCause(err))
		}

		// Get session info for context and apply filters
		sessionCache := make(map[string]*sessions.Session)

		for _, t := range turns {
			// Apply additional filters
			if in.ErrorsOnly && !t.HasError {
				continue
			}
			if in.ErrorType != "" && !strings.EqualFold(t.ErrorType, in.ErrorType) {
				continue
			}
			if in.Role != "" && !strings.EqualFold(t.Role, in.Role) {
				continue
			}
			if in.ToolPattern != "" && !matchesToolPattern(t.ToolCalls, in.ToolPattern) {
				continue
			}

			// Get session for project name
			sess, ok := sessionCache[t.SessionID]
			if !ok {
				s, err := store.Get(ctx, t.SessionID)
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
			if len(results) >= in.Limit {
				break
			}
		}
	} else {
		// No text query - scan sessions and filter
		sessionList, err := store.List(ctx, sessions.ListOptions{Limit: 100})
		if err != nil {
			return skillerr.Runtime("list sessions", skillerr.WithCause(err))
		}

		for _, sess := range sessionList {
			var turns []sessions.SessionTurn

			if in.ErrorsOnly {
				turns, err = store.GetTurnsWithErrors(ctx, sess.ID)
			} else {
				opts := sessions.TurnListOptions{
					SessionID: sess.ID,
					Role:      in.Role,
					Limit:     in.Limit,
				}
				turns, err = store.GetTurns(ctx, sess.ID, opts)
			}

			if err != nil {
				continue // Skip sessions with errors
			}

			for _, t := range turns {
				// Apply filters
				if in.ErrorType != "" && !strings.EqualFold(t.ErrorType, in.ErrorType) {
					continue
				}
				if in.ToolPattern != "" && !matchesToolPattern(t.ToolCalls, in.ToolPattern) {
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
				if len(results) >= in.Limit {
					break
				}
			}

			if len(results) >= in.Limit {
				break
			}
		}
	}

	output := Output{
		Query:      in.Query,
		Turns:      results,
		TotalFound: len(results),
		Status:     "ok",
		Message:    fmt.Sprintf("Found %d matching turns", len(results)),
	}

	if len(results) == 0 {
		output.Status = "no_matches"
		output.Message = "No turns matched the specified filters"
	}

	return skillout.Emit(rc, command, output)
}

// matchesToolPattern checks if any tool call matches the pattern with case-insensitive matching.
func matchesToolPattern(toolCalls []sessions.ToolCall, pattern string) bool {
	pattern = strings.ToLower(pattern)
	for _, tc := range toolCalls {
		if strings.Contains(strings.ToLower(tc.Name), pattern) {
			return true
		}
	}
	return false
}
