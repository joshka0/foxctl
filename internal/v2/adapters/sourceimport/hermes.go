package sourceimport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const hermesSessionGlob = "session_*.json"

type hermesSessionFile struct {
	SessionID    string          `json:"session_id"`
	Model        string          `json:"model,omitempty"`
	BaseURL      string          `json:"base_url,omitempty"`
	Platform     string          `json:"platform,omitempty"`
	SessionStart string          `json:"session_start,omitempty"`
	LastUpdated  string          `json:"last_updated,omitempty"`
	Messages     []hermesMessage `json:"messages"`
}

type hermesMessage struct {
	Role       string           `json:"role"`
	Content    json.RawMessage  `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []hermesToolCall `json:"tool_calls,omitempty"`
}

type hermesToolCall struct {
	ID       string             `json:"id,omitempty"`
	CallID   string             `json:"call_id,omitempty"`
	Function hermesToolFunction `json:"function,omitempty"`
}

type hermesToolFunction struct {
	Name      string          `json:"name,omitempty"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// LooksLikeHermesSessionJSON reports whether raw JSON has Hermes' session-file schema.
func LooksLikeHermesSessionJSON(raw []byte) bool {
	var probe struct {
		SessionID string            `json:"session_id"`
		Messages  []json.RawMessage `json:"messages"`
		Platform  string            `json:"platform"`
		Model     string            `json:"model"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return strings.TrimSpace(probe.SessionID) != "" && len(probe.Messages) > 0 &&
		(strings.TrimSpace(probe.Platform) != "" || strings.TrimSpace(probe.Model) != "")
}

// LocateHermesSessionJSON resolves a Hermes session file by session id.
func LocateHermesSessionJSON(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	dir := hermesSessionsDir()
	candidates := []string{
		filepath.Join(dir, sessionID),
		filepath.Join(dir, sessionID+".json"),
		filepath.Join(dir, "session_"+sessionID+".json"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "*"+sessionID+"*.json"))
	sort.Strings(matches)
	if len(matches) == 0 {
		return ""
	}
	return matches[len(matches)-1]
}

// LocateMostRecentHermesSessionJSON returns the most recently modified Hermes session file.
func LocateMostRecentHermesSessionJSON() (string, string) {
	matches, _ := filepath.Glob(filepath.Join(hermesSessionsDir(), hermesSessionGlob))
	if len(matches) == 0 {
		return "", ""
	}
	sort.SliceStable(matches, func(i, j int) bool {
		left, lerr := os.Stat(matches[i])
		right, rerr := os.Stat(matches[j])
		if lerr == nil && rerr == nil && !left.ModTime().Equal(right.ModTime()) {
			return left.ModTime().Before(right.ModTime())
		}
		return matches[i] < matches[j]
	})
	path := matches[len(matches)-1]
	return path, hermesSessionIDFromFilename(path)
}

// ParseHermesFile converts a Hermes Agent session JSON file into canonical v2 turns.
func ParseHermesFile(path, sessionID, workspacePath, actorID string) (ParsedSession, error) {
	return parseHermesFileWithClock(path, sessionID, workspacePath, actorID, func() time.Time { return time.Now().UTC() })
}

func parseHermesFileWithClock(path, sessionID, workspacePath, actorID string, now func() time.Time) (ParsedSession, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ParsedSession{}, fmt.Errorf("parse hermes file: %w", err)
	}
	var file hermesSessionFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return ParsedSession{}, fmt.Errorf("parse hermes file: decode %s: %w", path, err)
	}

	sessionID = firstNonEmpty(strings.TrimSpace(sessionID), strings.TrimSpace(file.SessionID), hermesSessionIDFromFilename(path))
	sessionID = sanitizeRefSegment(sessionID)
	if sessionID == "" {
		return ParsedSession{}, fmt.Errorf("parse hermes file: missing session_id")
	}

	asm := newTurnAssembler(ProviderHermes, sessionID, actorID, now)
	baseTime := firstNonZeroTime(parseHermesTime(file.SessionStart), parseHermesTime(file.LastUpdated), now().UTC())
	toolAliases := make(map[string]string)
	for idx, msg := range file.Messages {
		ts := baseTime.Add(time.Duration(idx) * time.Second)
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		switch role {
		case "user":
			prompt := truncate(hermesContentText(msg.Content), maxPromptLen)
			if prompt != "" {
				asm.startTurn(prompt, ts)
			}
		case "assistant":
			text := truncate(hermesContentText(msg.Content), maxAssistantLen)
			if text != "" {
				asm.addAssistant(text, ts)
			}
			uses := hermesToolUses(msg.ToolCalls)
			indexHermesToolAliases(toolAliases, msg.ToolCalls, uses)
			asm.addToolUses(uses, ts)
		case "tool":
			content := truncate(hermesContentText(msg.Content), maxToolResultLen)
			callID := strings.TrimSpace(msg.ToolCallID)
			if alias := strings.TrimSpace(toolAliases[callID]); alias != "" {
				callID = alias
			}
			asm.addToolResults([]ToolResultInput{{
				CallID:  callID,
				Content: content,
			}}, ts)
		}
	}

	turns := asm.finish(parseHermesTime(file.LastUpdated))
	return ParsedSession{
		Provider:      ProviderHermes,
		SessionID:     sessionID,
		SourcePath:    path,
		WorkspacePath: strings.TrimSpace(workspacePath),
		Turns:         turns,
	}, nil
}

func indexHermesToolAliases(dst map[string]string, calls []hermesToolCall, uses []ToolUseInput) {
	if len(calls) == 0 || len(uses) == 0 {
		return
	}
	for idx, call := range calls {
		if idx >= len(uses) {
			return
		}
		primary := strings.TrimSpace(uses[idx].CallID)
		if primary == "" {
			continue
		}
		for _, alias := range []string{call.CallID, call.ID} {
			alias = strings.TrimSpace(alias)
			if alias != "" {
				dst[alias] = primary
			}
		}
	}
}

func hermesToolUses(calls []hermesToolCall) []ToolUseInput {
	if len(calls) == 0 {
		return nil
	}
	out := make([]ToolUseInput, 0, len(calls))
	for idx, call := range calls {
		callID := firstNonEmpty(strings.TrimSpace(call.CallID), strings.TrimSpace(call.ID))
		if callID == "" {
			callID = fmt.Sprintf("call-%d", idx+1)
		}
		name := strings.TrimSpace(call.Function.Name)
		if name == "" {
			name = "unknown"
		}
		out = append(out, ToolUseInput{
			CallID: callID,
			Name:   name,
			Args:   normalizeHermesToolArgs(call.Function.Arguments),
		})
	}
	return out
}

func normalizeHermesToolArgs(raw json.RawMessage) []byte {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return []byte("{}")
	}
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		asString = strings.TrimSpace(asString)
		if json.Valid([]byte(asString)) {
			return []byte(asString)
		}
		wrapped, _ := json.Marshal(map[string]string{"text": asString})
		return wrapped
	}
	return normalizeJSON(raw)
}

func hermesContentText(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if text := rawContentToText(raw); strings.TrimSpace(text) != "" && !strings.HasPrefix(strings.TrimSpace(text), "[") {
		return strings.TrimSpace(text)
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) == nil {
		parts := make([]string, 0, len(blocks))
		for _, block := range blocks {
			part := rawContentToText(block["text"])
			if strings.TrimSpace(part) == "" {
				part = rawContentToText(block["content"])
			}
			parts = append(parts, part)
		}
		return strings.TrimSpace(strings.Join(nonEmptyStrings(parts), "\n"))
	}
	return strings.TrimSpace(string(raw))
}

func hermesSessionsDir() string {
	home := strings.TrimSpace(os.Getenv("HERMES_HOME"))
	if home == "" {
		if userHome, err := os.UserHomeDir(); err == nil {
			home = filepath.Join(userHome, ".hermes")
		}
	}
	return filepath.Join(home, "sessions")
}

func hermesSessionIDFromFilename(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return strings.TrimPrefix(base, "session_")
}

func parseHermesTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	formats := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05.999999",
		"2006-01-02T15:04:05",
	}
	for _, format := range formats {
		if ts, err := time.Parse(format, raw); err == nil {
			return ts.UTC()
		}
	}
	return time.Time{}
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func nonEmptyStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
