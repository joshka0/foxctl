// Package main implements the session/capture skill for extracting Claude Code and Codex conversations with comprehensive metadata.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillerr"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillmain"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/skillout"
	"github.com/joshka0/foxctl/internal/adapters/skillslib/workspaceutil"
	"github.com/joshka0/foxctl/internal/context/sessionkit/claudejsonl"
	"github.com/joshka0/foxctl/internal/context/sessionkit/codexjsonl"
	platformpath "github.com/joshka0/foxctl/internal/platform/pathutil"
	"github.com/joshka0/foxctl/internal/storage"
	"github.com/joshka0/foxctl/internal/storage/sessions"
)

// Input defines the skill input parameters for session capture with source selection and scanning options.
type Input struct {
	Workspace  string `json:"workspace"`
	SessionID  string `json:"session_id,omitempty"`
	Source     string `json:"source,omitempty"` // "claude" (default) or "codex"
	ClaudeHome string `json:"claude_home,omitempty"`
	CodexHome  string `json:"codex_home,omitempty"`
	Scan       bool   `json:"scan,omitempty"`
	ScanLimit  int    `json:"scan_limit,omitempty"`
	Summarize  bool   `json:"summarize,omitempty"`
	Force      bool   `json:"force,omitempty"`
}

// Output defines the skill output with comprehensive session statistics and capture results.
type Output struct {
	SessionID        string   `json:"session_id"`
	WorkspacePath    string   `json:"workspace_path"`
	ProjectName      string   `json:"project_name"`
	GitBranch        string   `json:"git_branch"`
	MessageCount     int      `json:"message_count"`
	UserTurns        int      `json:"user_turns"`
	ToolInvocations  int      `json:"tool_invocations"`
	TotalTokens      int      `json:"total_tokens"`
	Status           string   `json:"status"`
	RawJSONLPath     string   `json:"raw_jsonl_path"`
	HighSignal       []string `json:"high_signal,omitempty"` // Preview of extracted content
	SessionsScanned  int      `json:"sessions_scanned,omitempty"`
	SessionsMatched  int      `json:"sessions_matched,omitempty"`
	SessionsCaptured int      `json:"sessions_captured,omitempty"`
	SessionsSkipped  int      `json:"sessions_skipped,omitempty"`
	Message          string   `json:"message"`
}

const command = "session/capture"

// main is the skill entry point for session/capture with comprehensive conversation extraction capabilities.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates session capture from Claude Code and Codex with metadata extraction and storage.
//
// Index:
//
//	Purpose: Extract and store conversation sessions from Claude Code and Codex with metadata, statistics, and high-signal content
//	Keywords: session/capture, conversation_extraction, claude_code, codex, metadata_extraction, session_storage
//	Related: findSessionFile, extractSession, extractHighSignal, scanCodexSessions, claudejsonl.OpenReader
//	Flow: resolve workspace → detect source → locate session files → parse messages → extract metadata → save to store → emit results
//	Resources: session store, Claude/Codex JSONL files
//	Events: session capture events
//	OutputFields: session_id, workspace_path, message_count, user_turns, tool_invocations, high_signal
//
// [[domain:session-capture-from-provider]]
// [[protocol:claude-codex-jsonl-parsing]]
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Default workspace
	in.Workspace = workspaceutil.Resolve(in.Workspace, "", rc.Workspace)
	in.Workspace = normalizeWorkspacePath(in.Workspace)

	source := strings.ToLower(strings.TrimSpace(in.Source))
	if source == "" {
		source = "claude"
	}
	if source != "claude" && source != "codex" {
		return skillerr.Arg("source must be \"claude\" or \"codex\"")
	}

	// Default Claude home
	if source == "claude" && in.ClaudeHome == "" {
		homeDir, _ := os.UserHomeDir()
		in.ClaudeHome = filepath.Join(homeDir, ".claude")
	}

	// Default Codex home
	if source == "codex" && in.CodexHome == "" {
		homeDir, _ := os.UserHomeDir()
		in.CodexHome = filepath.Join(homeDir, ".codex")
	}

	// Open sessions store
	sessionStore, err := rc.Stores.Sessions(ctx)
	if err != nil {
		return skillerr.IO("open sessions store", skillerr.WithCause(err))
	}

	if source == "codex" && in.Scan {
		if strings.TrimSpace(in.SessionID) != "" {
			return skillerr.Arg("session_id must be empty when scan=true")
		}
		return scanCodexSessions(ctx, rc, in, sessionStore)
	}

	var sessionFile string
	sessionID := in.SessionID

	switch source {
	case "claude":
		// Find the project directory in Claude's storage
		projectDir := claudejsonl.ClaudeProjectDir(in.Workspace)
		if projectDir == "" {
			return skillerr.Arg(fmt.Sprintf("no Claude Code project found for workspace: %s", in.Workspace),
				skillerr.WithHint("Ensure Claude Code has been used in this workspace"))
		}

		// Debug: log the project dir
		fmt.Fprintf(os.Stderr, "DEBUG: workspace=%s projectDir=%s\n", in.Workspace, projectDir)

		// Find session file(s)
		sessionFile, sessionID = findSessionFile(projectDir, in.SessionID)
		if sessionFile == "" {
			return skillerr.Arg(fmt.Sprintf("no session file found in project directory: %s (workspace: %s)", projectDir, in.Workspace),
				skillerr.WithHint("Check that session_id is correct or omit to use most recent session"))
		}
	case "codex":
		if in.SessionID != "" {
			sessionFile = codexjsonl.LocateSessionJSONL(in.SessionID)
			sessionID = in.SessionID
		} else {
			sessionFile, sessionID = codexjsonl.LocateMostRecentSessionJSONL()
		}
		if sessionFile == "" {
			return skillerr.Arg("no Codex session file found",
				skillerr.WithHint("Check that session_id is correct or omit to use most recent session"))
		}
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

	var session storage.Session
	var highSignal []string

	switch source {
	case "claude":
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
		session = extractSession(sessionID, sessionFile, in.Workspace, messages)

		// Extract high-signal content for preview
		highSignal = extractHighSignal(messages, 5)
	case "codex":
		messages, err := readCodexMessages(sessionFile)
		if err != nil {
			return skillerr.IO("parse session file", skillerr.WithCause(err),
				skillerr.WithHint("Ensure JSONL file is valid and not corrupted"))
		}

		session = extractCodexSession(sessionID, sessionFile, in.Workspace, messages)
		highSignal = extractCodexHighSignal(messages, 5)
	}

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

// findSessionFile finds the session JSONL file to capture with session ID matching and modification time sorting.
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

// extractSession creates a Session from parsed Claude Code messages with comprehensive metadata extraction.
func extractSession(sessionID, rawPath, workspace string, messages []*claudejsonl.ReadMessage) storage.Session {
	session := storage.Session{
		ID:            sessionID,
		WorkspacePath: workspace,
		RawJSONLPath:  rawPath,
		AgentType:     "claude",
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

// extractHighSignal extracts high-signal content preview from Claude Code messages with user requests and summaries.
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
			signals = append(signals, fmt.Sprintf("[Summary] %s", skillout.TruncateSingleLine(summary, 100)))
		}
	}

	return signals
}

// readCodexMessages reads and parses Codex session messages from JSONL files with error handling.
func readCodexMessages(path string) ([]*codexjsonl.ReadMessage, error) {
	reader, err := codexjsonl.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return reader.ReadAll()
}

// extractCodexSession creates a Session from parsed Codex messages with metadata and tool usage tracking.
func extractCodexSession(sessionID, rawPath, workspace string, messages []*codexjsonl.ReadMessage) storage.Session {
	session := storage.Session{
		ID:            sessionID,
		WorkspacePath: workspace,
		RawJSONLPath:  rawPath,
		AgentType:     "codex",
	}

	var minTime, maxTime time.Time
	toolSet := make(map[string]bool)

	for _, rm := range messages {
		if rm == nil || rm.Message == nil {
			continue
		}
		msg := rm.Message
		if msg.Type != "response_item" {
			continue
		}

		session.MessageCount++

		if !rm.Timestamp.IsZero() {
			if minTime.IsZero() || rm.Timestamp.Before(minTime) {
				minTime = rm.Timestamp
			}
			if maxTime.IsZero() || rm.Timestamp.After(maxTime) {
				maxTime = rm.Timestamp
			}
		}

		switch codexjsonl.Classify(msg) {
		case codexjsonl.ChunkTypeUserRequest:
			session.UserTurns++
		case codexjsonl.ChunkTypeToolUse:
			session.ToolInvocations++
			tools := codexjsonl.ExtractTools(msg)
			for _, tool := range tools {
				toolSet[tool] = true
			}
		}
	}

	if !minTime.IsZero() {
		session.StartedAt = minTime
	}
	if !maxTime.IsZero() {
		session.EndedAt = maxTime
	}

	if session.WorkspacePath != "" {
		session.ProjectName = filepath.Base(session.WorkspacePath)
	}

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

// extractCodexHighSignal extracts high-signal content preview from Codex messages with user request focus.
func extractCodexHighSignal(messages []*codexjsonl.ReadMessage, limit int) []string {
	var signals []string

	for _, rm := range messages {
		if len(signals) >= limit {
			break
		}
		if rm == nil || rm.Message == nil {
			continue
		}
		msg := rm.Message
		if msg.Type != "response_item" {
			continue
		}

		if codexjsonl.Classify(msg) == codexjsonl.ChunkTypeUserRequest {
			preview := codexjsonl.ExtractPreview(msg, 100)
			if preview != "" {
				signals = append(signals, fmt.Sprintf("[User] %s", preview))
			}
		}
	}

	return signals
}

// scanCodexSessions scans and captures multiple Codex sessions with workspace matching and batch processing.
func scanCodexSessions(ctx context.Context, rc *skillmain.RunContext, in Input, sessionStore *sessions.Store) error {
	files, err := codexjsonl.ListSessionFiles(in.CodexHome)
	if err != nil {
		return skillerr.IO("list codex sessions", skillerr.WithCause(err))
	}
	if len(files) == 0 {
		return skillout.Emit(rc, command, Output{
			Status:          "scanned",
			SessionsScanned: 0,
			Message:         "No Codex session files found",
		})
	}

	workspacePaths := resolveWorkspaceCandidates(in.Workspace)
	repoURL := normalizeGitURL(gitRemoteURL(ctx, in.Workspace))

	var scanned, matched, captured, skipped int
	for _, file := range files {
		if in.ScanLimit > 0 && scanned >= in.ScanLimit {
			break
		}
		scanned++
		if strings.TrimSpace(file.ID) == "" {
			skipped++
			continue
		}

		if !in.Force {
			existing, err := sessionStore.Get(ctx, file.ID)
			if err == nil && existing.ID != "" {
				skipped++
				continue
			}
		}

		meta, err := codexjsonl.ExtractMetadata(file.Path)
		if err != nil {
			skipped++
			continue
		}
		if !matchesCodexWorkspace(meta, workspacePaths, repoURL) {
			continue
		}
		matched++

		messages, err := readCodexMessages(file.Path)
		if err != nil {
			skipped++
			continue
		}
		session := extractCodexSession(file.ID, file.Path, in.Workspace, messages)

		if _, err := sessionStore.Save(ctx, session); err != nil {
			skipped++
			continue
		}
		captured++
	}

	output := Output{
		Status:           "scanned",
		SessionsScanned:  scanned,
		SessionsMatched:  matched,
		SessionsCaptured: captured,
		SessionsSkipped:  skipped,
		Message:          fmt.Sprintf("Scanned %d Codex sessions, matched %d, captured %d, skipped %d", scanned, matched, captured, skipped),
	}

	return skillout.Emit(rc, command, output)
}

// resolveWorkspaceCandidates generates workspace path candidates with symlink resolution and normalization.
func resolveWorkspaceCandidates(path string) []string {
	cleaned := filepath.Clean(path)
	if cleaned == "" {
		return nil
	}

	candidates := []string{cleaned}
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil && resolved != "" && resolved != cleaned {
		candidates = append(candidates, resolved)
	}
	return candidates
}

func normalizeWorkspacePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if abs, err := filepath.Abs(path); err == nil && abs != "" {
		path = abs
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && resolved != "" {
		path = resolved
	}
	return filepath.Clean(path)
}

// matchesCodexWorkspace determines if a Codex session matches the target workspace using path and repository URL comparison.
func matchesCodexWorkspace(meta codexjsonl.SessionMetadata, workspacePaths []string, repoURL string) bool {
	if meta.CWD != "" {
		cleaned := filepath.Clean(meta.CWD)
		for _, ws := range workspacePaths {
			if ws == "" {
				continue
			}
			if platformpath.IsUnderWorkspace(cleaned, ws) || platformpath.IsUnderWorkspace(ws, cleaned) {
				return true
			}
		}
	}

	if repoURL != "" && meta.RepositoryURL != "" {
		return normalizeGitURL(meta.RepositoryURL) == repoURL
	}

	return false
}

// gitRemoteURL retrieves the git remote URL for a workspace with timeout and error handling.
func gitRemoteURL(ctx context.Context, workspace string) string {
	if strings.TrimSpace(workspace) == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = workspace
	out, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(out))
}

// normalizeGitURL normalizes git URLs by removing protocols, .git suffix, and converting SSH to HTTPS format.
func normalizeGitURL(url string) string {
	url = strings.TrimSpace(url)
	if url == "" {
		return ""
	}
	url = strings.TrimSuffix(url, ".git")

	if strings.HasPrefix(url, "git@") {
		url = strings.TrimPrefix(url, "git@")
		url = strings.Replace(url, ":", "/", 1)
		return url
	}

	if strings.HasPrefix(url, "https://") {
		return strings.TrimPrefix(url, "https://")
	}
	if strings.HasPrefix(url, "http://") {
		return strings.TrimPrefix(url, "http://")
	}

	return url
}
