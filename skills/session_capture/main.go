// Package main implements the session/capture skill for extracting Claude Code conversations.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/sessionkit/claudejsonl"
	"github.com/jkatigb/agentctl/internal/storage"
)

// Input defines the skill input parameters.
type Input struct {
	Workspace  string `json:"workspace"`
	SessionID  string `json:"session_id,omitempty"`
	ClaudeHome string `json:"claude_home,omitempty"`
	Summarize  bool   `json:"summarize,omitempty"`
	Force      bool   `json:"force,omitempty"`
}

// Output defines the skill output.
type Output struct {
	SessionID       string   `json:"session_id"`
	WorkspacePath   string   `json:"workspace_path"`
	ProjectName     string   `json:"project_name"`
	GitBranch       string   `json:"git_branch"`
	MessageCount    int      `json:"message_count"`
	UserTurns       int      `json:"user_turns"`
	ToolInvocations int      `json:"tool_invocations"`
	TotalTokens     int      `json:"total_tokens"`
	Status          string   `json:"status"`
	RawJSONLPath    string   `json:"raw_jsonl_path"`
	HighSignal      []string `json:"high_signal,omitempty"` // Preview of extracted content
	Message         string   `json:"message"`
}

const command = "session/capture"

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Default workspace
	in.Workspace = sessionkit.WorkspaceOrDefault(in.Workspace, rc.Workspace)

	// Default Claude home
	if in.ClaudeHome == "" {
		homeDir, _ := os.UserHomeDir()
		in.ClaudeHome = filepath.Join(homeDir, ".claude")
	}

	// Open sessions store
	sessionStore, cleanup, err := sessionkit.OpenSessions(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}
	defer cleanup()

	// Find the project directory in Claude's storage
	projectDir := claudejsonl.ClaudeProjectDir(in.Workspace)
	if projectDir == "" {
		return skillerr.Arg(fmt.Sprintf("no Claude Code project found for workspace: %s", in.Workspace),
			skillerr.WithHint("Ensure Claude Code has been used in this workspace"))
	}

	// Find session file(s)
	sessionFile, sessionID := findSessionFile(projectDir, in.SessionID)
	if sessionFile == "" {
		return skillerr.Arg("no session file found in project directory",
			skillerr.WithHint("Check that session_id is correct or omit to use most recent session"))
	}

	// Check if session already exists
	if !in.Force {
		existing, err := sessionStore.Get(ctx, sessionID)
		if err == nil && existing.ID != "" {
			output := Output{
				SessionID:       sessionID,
				WorkspacePath:   existing.WorkspacePath,
				ProjectName:     existing.ProjectName,
				GitBranch:       existing.GitBranch,
				MessageCount:    existing.MessageCount,
				UserTurns:       existing.UserTurns,
				ToolInvocations: existing.ToolInvocations,
				TotalTokens:     existing.TotalTokens,
				Status:          "exists",
				RawJSONLPath:    existing.RawJSONLPath,
				Message:         fmt.Sprintf("Session %s already captured (use force=true to re-capture)", sessionID),
			}
			return skillout.Emit(rc, command, output)
		}
	}

	// Parse the JSONL file using claudejsonl Reader
	reader, err := claudejsonl.OpenReader(sessionFile)
	if err != nil {
		return skillerr.IO("open session file", skillerr.WithCause(err))
	}
	defer reader.Close()

	messages, err := reader.ReadAll()
	if err != nil {
		return skillerr.IO("parse session file", skillerr.WithCause(err),
			skillerr.WithHint("Ensure JSONL file is valid and not corrupted"))
	}

	// Extract session metadata and stats
	session := extractSession(sessionID, sessionFile, in.Workspace, messages)

	// Extract high-signal content for preview
	highSignal := extractHighSignal(messages, 5)

	// Save to store
	saved, err := sessionStore.Save(ctx, session)
	if err != nil {
		return skillerr.IO("save session", skillerr.WithCause(err))
	}

	output := Output{
		SessionID:       saved.ID,
		WorkspacePath:   saved.WorkspacePath,
		ProjectName:     saved.ProjectName,
		GitBranch:       saved.GitBranch,
		MessageCount:    saved.MessageCount,
		UserTurns:       saved.UserTurns,
		ToolInvocations: saved.ToolInvocations,
		TotalTokens:     saved.TotalTokens,
		Status:          "captured",
		RawJSONLPath:    saved.RawJSONLPath,
		HighSignal:      highSignal,
		Message: fmt.Sprintf("Captured session %s: %d messages, %d user turns, %d tool calls",
			sessionID, saved.MessageCount, saved.UserTurns, saved.ToolInvocations),
	}

	return skillout.Emit(rc, command, output)
}

// findSessionFile finds the session JSONL file to capture.
func findSessionFile(projectDir, sessionID string) (string, string) {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return "", ""
	}

	type sessionFile struct {
		path    string
		id      string
		modTime time.Time
		size    int64
	}

	var sessions []sessionFile

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		// Skip agent subfiles (agent-*.jsonl)
		if strings.HasPrefix(name, "agent-") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Extract session ID from filename (remove .jsonl extension)
		id := strings.TrimSuffix(name, ".jsonl")

		// If specific session requested, check for match
		if sessionID != "" && id != sessionID {
			continue
		}

		sessions = append(sessions, sessionFile{
			path:    filepath.Join(projectDir, name),
			id:      id,
			modTime: info.ModTime(),
			size:    info.Size(),
		})
	}

	if len(sessions) == 0 {
		return "", ""
	}

	// Sort by modification time (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].modTime.After(sessions[j].modTime)
	})

	// Return most recent (or the requested one if found)
	return sessions[0].path, sessions[0].id
}

// extractSession creates a Session from parsed messages.
func extractSession(sessionID, rawPath, workspace string, messages []*claudejsonl.ReadMessage) storage.Session {
	session := storage.Session{
		ID:            sessionID,
		WorkspacePath: workspace,
		RawJSONLPath:  rawPath,
	}

	var minTime, maxTime time.Time
	toolSet := make(map[string]bool)

	for _, rm := range messages {
		msg := rm.Message
		session.MessageCount++

		// Use parsed timestamp
		if !rm.Timestamp.IsZero() {
			if minTime.IsZero() || rm.Timestamp.Before(minTime) {
				minTime = rm.Timestamp
			}
			if maxTime.IsZero() || rm.Timestamp.After(maxTime) {
				maxTime = rm.Timestamp
			}
		}

		// Count by type
		msgType := claudejsonl.Classify(msg)
		switch msgType {
		case claudejsonl.ChunkTypeUserRequest:
			session.UserTurns++
		case claudejsonl.ChunkTypeToolUse:
			session.ToolInvocations++
		case claudejsonl.ChunkTypeAssistantResponse:
			// Count tools from assistant messages
			tools := claudejsonl.ExtractTools(msg)
			for _, tool := range tools {
				toolSet[tool] = true
			}
			session.ToolInvocations += len(tools)
		}

		// Count tokens from message usage
		if msg.Message != nil {
			var nested struct {
				Usage *struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal(msg.Message, &nested) == nil && nested.Usage != nil {
				session.TotalTokens += nested.Usage.InputTokens
				session.TotalTokens += nested.Usage.OutputTokens
			}
		}
	}

	// Set times
	if !minTime.IsZero() {
		session.StartedAt = minTime
	}
	if !maxTime.IsZero() {
		session.EndedAt = maxTime
	}

	// Extract project name from workspace path
	if session.WorkspacePath != "" {
		session.ProjectName = filepath.Base(session.WorkspacePath)
	}

	// Build tools pattern from tool set
	if len(toolSet) > 0 {
		tools := make([]string, 0, len(toolSet))
		for tool := range toolSet {
			tools = append(tools, tool)
		}
		sort.Strings(tools)
		session.ToolsPattern = strings.Join(tools, ", ")
	}

	return session
}

// extractHighSignal extracts high-signal content preview.
func extractHighSignal(messages []*claudejsonl.ReadMessage, limit int) []string {
	var signals []string

	for _, rm := range messages {
		if len(signals) >= limit {
			break
		}

		msg := rm.Message
		msgType := claudejsonl.Classify(msg)

		switch msgType {
		case claudejsonl.ChunkTypeUserRequest:
			preview := claudejsonl.ExtractPreview(msg, 100)
			if preview != "" {
				signals = append(signals, fmt.Sprintf("[User] %s", preview))
			}
		}

		// Check for compact summary messages
		if summary, ok := claudejsonl.MaybeCompactSummary(msg); ok && summary != "" {
			signals = append(signals, fmt.Sprintf("[Summary] %s", truncate(summary, 100)))
		}
	}

	return signals
}

func truncate(s string, maxLen int) string {
	// Remove newlines for cleaner output
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
