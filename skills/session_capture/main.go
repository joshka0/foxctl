// Package main implements the session/capture skill for extracting Claude Code conversations.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/domain/envelope"
	errs "github.com/jkatigb/agentctl/internal/platform/errors"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
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

// ClaudeMessage represents a message from Claude Code's JSONL format.
type ClaudeMessage struct {
	Type       string          `json:"type"`
	UUID       string          `json:"uuid,omitempty"`
	ParentUUID string          `json:"parentUuid,omitempty"`
	SessionID  string          `json:"sessionId,omitempty"`
	Timestamp  string          `json:"timestamp,omitempty"`
	CWD        string          `json:"cwd,omitempty"`
	GitBranch  string          `json:"gitBranch,omitempty"`
	Version    string          `json:"version,omitempty"`
	Message    *MessageContent `json:"message,omitempty"`
	Summary    string          `json:"summary,omitempty"`
	LeafUUID   string          `json:"leafUuid,omitempty"`
}

// MessageContent represents the content of a message.
type MessageContent struct {
	Role    string          `json:"role,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	Model   string          `json:"model,omitempty"`
	Usage   *TokenUsage     `json:"usage,omitempty"`
}

// TokenUsage represents token usage stats.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
}

// ContentBlock represents a block in assistant message content.
type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"id,omitempty"`
}

const command = "session/capture"

func main() {
	ctx := context.Background()

	// Read input from stdin
	var input Input
	if err := json.NewDecoder(os.Stdin).Decode(&input); err != nil {
		fail("EPARSE", fmt.Errorf("decode input: %w", err), "Ensure valid JSON on stdin")
	}

	// Default workspace to current directory
	if input.Workspace == "" {
		if wd, err := os.Getwd(); err == nil {
			input.Workspace = wd
		}
	}

	// Default Claude home
	if input.ClaudeHome == "" {
		homeDir, _ := os.UserHomeDir()
		input.ClaudeHome = filepath.Join(homeDir, ".claude")
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
		fail("EIO", fmt.Errorf("open sessions store: %w", err), "Check that storage directory exists and is accessible")
	}
	defer func() { errs.Ignore(sessionStore.Close(), "close sessions store") }()

	// Find the project directory in Claude's storage
	projectDir := findProjectDir(input.ClaudeHome, input.Workspace)
	if projectDir == "" {
		fail("ENOTFOUND", fmt.Errorf("no Claude Code project found for workspace: %s", input.Workspace), "Ensure Claude Code has been used in this workspace")
	}

	// Find session file(s)
	sessionFile, sessionID := findSessionFile(projectDir, input.SessionID)
	if sessionFile == "" {
		fail("ENOTFOUND", fmt.Errorf("no session file found in project directory"), "Check that session_id is correct or omit to use most recent session")
	}

	// Check if session already exists
	if !input.Force {
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
			env := envelope.OK(command, output)
			errs.Ignore(envelope.Write(os.Stdout, env), "emit session/capture result")
			return
		}
	}

	// Parse the JSONL file
	messages, err := parseJSONL(sessionFile)
	if err != nil {
		fail("EPARSE", fmt.Errorf("parse session file: %w", err), "Ensure JSONL file is valid and not corrupted")
	}

	// Extract session metadata and stats
	session := extractSession(sessionID, sessionFile, input.Workspace, messages)

	// Extract high-signal content for preview
	highSignal := extractHighSignal(messages, 5)

	// Save to store
	saved, err := sessionStore.Save(ctx, session)
	if err != nil {
		fail("EIO", fmt.Errorf("save session: %w", err), "Check database connectivity and permissions")
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

	env := envelope.OK(command, output)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/capture result")
}

// findProjectDir finds the Claude Code project directory for a workspace.
func findProjectDir(claudeHome, workspace string) string {
	projectsDir := filepath.Join(claudeHome, "projects")

	// Claude Code encodes workspace paths by replacing / with -
	// e.g., /Users/jkatigbak/repos/personal/agentctl -> -Users-jkatigbak-repos-personal-agentctl
	encodedPath := strings.ReplaceAll(workspace, "/", "-")
	if !strings.HasPrefix(encodedPath, "-") {
		encodedPath = "-" + encodedPath
	}

	projectDir := filepath.Join(projectsDir, encodedPath)
	if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
		return projectDir
	}

	// Try without leading dash (Windows-style paths)
	encodedPath = strings.ReplaceAll(workspace, string(filepath.Separator), "-")
	projectDir = filepath.Join(projectsDir, encodedPath)
	if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
		return projectDir
	}

	return ""
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

// parseJSONL reads and parses a Claude Code JSONL file.
func parseJSONL(path string) ([]ClaudeMessage, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { errs.Ignore(file.Close(), "close session file") }()

	var messages []ClaudeMessage
	scanner := bufio.NewScanner(file)

	// Increase buffer size for large lines
	const maxCapacity = 10 * 1024 * 1024 // 10MB
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg ClaudeMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			// Skip malformed lines but continue parsing
			continue
		}
		messages = append(messages, msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan error: %w", err)
	}

	return messages, nil
}

// extractSession creates a Session from parsed messages.
func extractSession(sessionID, rawPath, workspace string, messages []ClaudeMessage) storage.Session {
	session := storage.Session{
		ID:            sessionID,
		WorkspacePath: workspace,
		RawJSONLPath:  rawPath,
	}

	var minTime, maxTime time.Time
	toolSet := make(map[string]bool)

	for _, msg := range messages {
		session.MessageCount++

		// Parse timestamp
		if msg.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, msg.Timestamp); err == nil {
				if minTime.IsZero() || t.Before(minTime) {
					minTime = t
				}
				if maxTime.IsZero() || t.After(maxTime) {
					maxTime = t
				}
			}
		}

		// Extract metadata
		if msg.CWD != "" && session.WorkspacePath == "" {
			session.WorkspacePath = msg.CWD
		}
		if msg.GitBranch != "" {
			session.GitBranch = msg.GitBranch
		}
		if msg.Version != "" {
			session.ClaudeVersion = msg.Version
		}

		// Count by type
		switch msg.Type {
		case "user":
			session.UserTurns++
		case "assistant":
			if msg.Message != nil {
				// Count tokens
				if msg.Message.Usage != nil {
					session.TotalTokens += msg.Message.Usage.InputTokens
					session.TotalTokens += msg.Message.Usage.OutputTokens
				}

				// Count tool uses
				if msg.Message.Content != nil {
					var blocks []ContentBlock
					if err := json.Unmarshal(msg.Message.Content, &blocks); err == nil {
						for _, block := range blocks {
							if block.Type == "tool_use" {
								session.ToolInvocations++
								toolSet[block.Name] = true
							}
						}
					}
				}
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
func extractHighSignal(messages []ClaudeMessage, limit int) []string {
	var signals []string

	for _, msg := range messages {
		if len(signals) >= limit {
			break
		}

		switch msg.Type {
		case "user":
			if msg.Message != nil {
				var content string
				if err := json.Unmarshal(msg.Message.Content, &content); err == nil {
					content = truncate(content, 100)
					if content != "" {
						signals = append(signals, fmt.Sprintf("[User] %s", content))
					}
				}
			}
		case "summary":
			if msg.Summary != "" {
				signals = append(signals, fmt.Sprintf("[Summary] %s", truncate(msg.Summary, 100)))
			}
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

func fail(code string, err error, hint string) {
	var data map[string]any
	if hint != "" {
		data = map[string]any{"hint": hint}
	}
	env := envelope.Error(command, code, err.Error(), data)
	errs.Ignore(envelope.Write(os.Stdout, env), "emit session/capture failure")
	os.Exit(1)
}
