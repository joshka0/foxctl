package sourceimport

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/context/sessionkit/claudejsonl"
	"github.com/joshka0/foxctl/internal/context/sessionkit/codexjsonl"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

const (
	maxPromptLen       = 4000
	maxAssistantLen    = 6000
	maxToolResultLen   = 4000
	maxMessageSliceLen = 3000
)

// DetectProviderFromFile inspects the first parseable line and returns the source provider.
func DetectProviderFromFile(path string) (Provider, error) {
	f, err := os.Open(path)
	if err != nil {
		return ProviderAuto, fmt.Errorf("detect provider: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*32), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var probe struct {
			Type    string          `json:"type"`
			Message json.RawMessage `json:"message"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue
		}
		if len(probe.Payload) > 0 {
			return ProviderCodex, nil
		}
		if len(probe.Message) > 0 || probe.Type == "assistant" || probe.Type == "user" || probe.Type == "system" {
			return ProviderClaude, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return ProviderAuto, fmt.Errorf("detect provider: scan %s: %w", path, err)
	}
	return ProviderAuto, fmt.Errorf("detect provider: could not infer source format for %s", path)
}

// ParseClaudeFile converts a Claude JSONL conversation into canonical v2 turns.
func ParseClaudeFile(path, sessionID, workspacePath, actorID string) (ParsedSession, error) {
	return parseClaudeFileWithClock(path, sessionID, workspacePath, actorID, func() time.Time { return time.Now().UTC() })
}

func parseClaudeFileWithClock(path, sessionID, workspacePath, actorID string, now func() time.Time) (ParsedSession, error) {
	reader, err := claudejsonl.OpenReader(path)
	if err != nil {
		return ParsedSession{}, fmt.Errorf("parse claude file: %w", err)
	}
	defer func() { _ = reader.Close() }()

	sessionID = sanitizeRefSegment(normalizeSessionID(sessionID, path))
	if sessionID == "" {
		return ParsedSession{}, fmt.Errorf("parse claude file: missing session_id")
	}

	asm := newTurnAssembler(ProviderClaude, sessionID, actorID, now)
	for {
		rm, err := reader.Next()
		if err != nil {
			return ParsedSession{}, fmt.Errorf("parse claude file: read next: %w", err)
		}
		if rm == nil {
			break
		}
		if rm.Message == nil {
			continue
		}
		msg := rm.Message
		ts := rm.Timestamp
		switch claudejsonl.Classify(msg) {
		case claudejsonl.ChunkTypeUserRequest:
			prompt := truncate(claudejsonl.ExtractPreview(msg, maxPromptLen), maxPromptLen)
			if prompt != "" {
				asm.startTurn(prompt, ts)
			}
		case claudejsonl.ChunkTypeAssistantResponse:
			text := truncate(claudejsonl.ExtractPreview(msg, maxAssistantLen), maxAssistantLen)
			if text != "" {
				asm.addAssistant(text, ts)
			}
			if summary, ok := claudejsonl.MaybeCompactSummary(msg); ok {
				summary = truncate(summary, maxAssistantLen)
				if summary != "" {
					asm.addAssistant(summary, ts)
				}
			}
		case claudejsonl.ChunkTypeToolUse:
			asm.addToolUses(extractClaudeToolUses(msg), ts)
		case claudejsonl.ChunkTypeToolOutput:
			asm.addToolResults(extractClaudeToolResults(msg), ts)
		case claudejsonl.ChunkTypeError:
			results := extractClaudeToolResults(msg)
			if len(results) == 0 {
				asm.markLastToolError("tool error", ts)
			} else {
				asm.addToolResults(results, ts)
			}
		}
	}

	turns := asm.finish(time.Time{})
	return ParsedSession{
		Provider:      ProviderClaude,
		SessionID:     sessionID,
		SourcePath:    path,
		WorkspacePath: strings.TrimSpace(workspacePath),
		Turns:         turns,
	}, nil
}

// ParseCodexFile converts a Codex JSONL conversation into canonical v2 turns.
func ParseCodexFile(path, sessionID, workspacePath, actorID string) (ParsedSession, error) {
	return parseCodexFileWithClock(path, sessionID, workspacePath, actorID, func() time.Time { return time.Now().UTC() })
}

func parseCodexFileWithClock(path, sessionID, workspacePath, actorID string, now func() time.Time) (ParsedSession, error) {
	reader, err := codexjsonl.OpenReader(path)
	if err != nil {
		return ParsedSession{}, fmt.Errorf("parse codex file: %w", err)
	}
	defer func() { _ = reader.Close() }()

	if strings.TrimSpace(sessionID) == "" {
		sessionID = codexjsonl.SessionIDFromFilename(path)
	}
	sessionID = sanitizeRefSegment(sessionID)
	if sessionID == "" {
		return ParsedSession{}, fmt.Errorf("parse codex file: missing session_id")
	}

	asm := newTurnAssembler(ProviderCodex, sessionID, actorID, now)
	for {
		rm, err := reader.Next()
		if err != nil {
			return ParsedSession{}, fmt.Errorf("parse codex file: read next: %w", err)
		}
		if rm == nil {
			break
		}
		if rm.Message == nil {
			continue
		}
		msg := rm.Message
		ts := rm.Timestamp
		class := codexjsonl.Classify(msg)
		switch class {
		case codexjsonl.ChunkTypeUserRequest:
			prompt := truncate(codexjsonl.ExtractPreview(msg, maxPromptLen), maxPromptLen)
			if prompt != "" {
				asm.startTurn(prompt, ts)
			}
		case codexjsonl.ChunkTypeAssistantResponse:
			text := truncate(codexjsonl.ExtractPreview(msg, maxAssistantLen), maxAssistantLen)
			if text != "" {
				asm.addAssistant(text, ts)
			}
		case codexjsonl.ChunkTypeToolUse:
			if item, ok := parseCodexResponseItem(msg); ok {
				asm.addToolUses([]ToolUseInput{{
					CallID: strings.TrimSpace(item.CallID),
					Name:   strings.TrimSpace(item.Name),
					Args:   normalizeJSON(item.Arguments),
				}}, ts)
			}
		case codexjsonl.ChunkTypeToolOutput, codexjsonl.ChunkTypeError:
			if item, ok := parseCodexResponseItem(msg); ok {
				asm.addToolResults([]ToolResultInput{{
					CallID:  strings.TrimSpace(item.CallID),
					IsError: class == codexjsonl.ChunkTypeError,
					Content: truncate(codexToolResultText(item), maxToolResultLen),
				}}, ts)
			}
		}
	}

	turns := asm.finish(time.Time{})
	return ParsedSession{
		Provider:      ProviderCodex,
		SessionID:     sessionID,
		SourcePath:    path,
		WorkspacePath: strings.TrimSpace(workspacePath),
		Turns:         turns,
	}, nil
}

type toolLocation struct {
	iterPos int
	callPos int
}

type turnAssembler struct {
	provider  Provider
	sessionID string
	actorID   string

	now         func() time.Time
	turns       []run.TurnRecord
	current     run.TurnRecord
	hasCurrent  bool
	nextTurnIdx int

	toolByID map[string]toolLocation
	lastTool toolLocation
	hasTool  bool
}

func newTurnAssembler(provider Provider, sessionID, actorID string, now func() time.Time) *turnAssembler {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &turnAssembler{
		provider:    provider,
		sessionID:   sanitizeRefSegment(sessionID),
		actorID:     strings.TrimSpace(actorID),
		now:         now,
		nextTurnIdx: 1,
		toolByID:    make(map[string]toolLocation),
	}
}

func (a *turnAssembler) startTurn(prompt string, ts time.Time) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return
	}
	if a.hasCurrent {
		a.finalizeCurrent(ts)
	}
	a.beginCurrent(prompt, ts)
}

func (a *turnAssembler) beginCurrent(prompt string, ts time.Time) {
	if a.actorID == "" {
		a.actorID = "actor:system:source-import"
	}
	ts = a.normalizeTime(ts)
	seg := sanitizeRefSegment(a.sessionID)
	runID := fmt.Sprintf("source:%s:%s", a.provider, seg)
	traceID := fmt.Sprintf("trace:%s:%s:%04d", a.provider, seg, a.nextTurnIdx)
	turnID := fmt.Sprintf("turn:%s:%s:%04d", a.provider, seg, a.nextTurnIdx)
	rootSpanID := fmt.Sprintf("span:%s:%s:root", runID, turnID)
	requestID := fmt.Sprintf("req:%s", turnID)

	a.current = run.TurnRecord{
		ID:            turnID,
		SessionID:     runID,
		TurnIndex:     a.nextTurnIdx,
		TraceID:       traceID,
		RootSpanID:    rootSpanID,
		CorrelationID: traceID,
		CausationID:   requestID,
		RequestID:     requestID,
		ActorID:       a.actorID,
		Command:       fmt.Sprintf("source.import.%s", a.provider),
		Prompt:        strings.TrimSpace(prompt),
		CreatedAt:     ts,
		UpdatedAt:     ts,
	}
	a.nextTurnIdx++
	a.hasCurrent = true
	a.toolByID = make(map[string]toolLocation)
	a.hasTool = false
}

func (a *turnAssembler) ensureTurn(ts time.Time) {
	if a.hasCurrent {
		return
	}
	a.beginCurrent("", ts)
}

func (a *turnAssembler) addAssistant(text string, ts time.Time) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.ensureTurn(ts)
	iterPos := a.appendIteration("assistant", text)
	a.current.Iterations[iterPos].Message.Text = truncate(a.current.Iterations[iterPos].Message.Text, maxAssistantLen)
	a.current.UpdatedAt = a.normalizeTime(ts)
}

func (a *turnAssembler) appendIteration(role, text string) int {
	iterIdx := len(a.current.Iterations) + 1
	iter := run.IterationRecord{
		TurnID:         a.current.ID,
		IterationIndex: iterIdx,
		TraceID:        a.current.TraceID,
		SpanID:         fmt.Sprintf("span:%s:%s:iter:%d", a.current.SessionID, a.current.ID, iterIdx),
		ParentSpanID:   a.current.RootSpanID,
		Message: run.MessageRef{
			ID:   fmt.Sprintf("msg-iter-%d", iterIdx),
			Role: strings.TrimSpace(role),
			Text: strings.TrimSpace(text),
		},
	}
	a.current.Iterations = append(a.current.Iterations, iter)
	return len(a.current.Iterations) - 1
}

func (a *turnAssembler) ensureIteration(ts time.Time) int {
	a.ensureTurn(ts)
	if len(a.current.Iterations) == 0 {
		return a.appendIteration("assistant", "")
	}
	return len(a.current.Iterations) - 1
}

func (a *turnAssembler) addToolUses(uses []ToolUseInput, ts time.Time) {
	if len(uses) == 0 {
		return
	}
	iterPos := a.ensureIteration(ts)
	iter := &a.current.Iterations[iterPos]
	for i, in := range uses {
		callID := strings.TrimSpace(in.CallID)
		if callID == "" {
			callID = fmt.Sprintf("call-%d-%d", iter.IterationIndex, len(iter.ToolCalls)+1)
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = "unknown"
		}
		call := run.ToolCallRecord{
			CallID:         callID,
			IterationIndex: iter.IterationIndex,
			TraceID:        a.current.TraceID,
			SpanID:         fmt.Sprintf("span:%s:%s:iter:%d:tool:%d", a.current.SessionID, a.current.ID, iter.IterationIndex, len(iter.ToolCalls)+1),
			ParentSpanID:   iter.SpanID,
			Name:           name,
			ArgsJSON:       normalizeJSON(in.Args),
			Status:         "called",
		}
		iter.ToolCalls = append(iter.ToolCalls, call)
		loc := toolLocation{iterPos: iterPos, callPos: len(iter.ToolCalls) - 1}
		a.toolByID[callID] = loc
		a.lastTool = loc
		a.hasTool = true
		_ = i
	}
	a.current.UpdatedAt = a.normalizeTime(ts)
}

func (a *turnAssembler) addToolResults(results []ToolResultInput, ts time.Time) {
	if len(results) == 0 {
		return
	}
	a.ensureTurn(ts)
	for _, result := range results {
		loc, ok := a.locateTool(strings.TrimSpace(result.CallID), ts)
		if !ok {
			continue
		}
		call := &a.current.Iterations[loc.iterPos].ToolCalls[loc.callPos]
		if result.IsError {
			call.Status = "error"
		} else {
			call.Status = "ok"
		}
		text := truncate(strings.TrimSpace(result.Content), maxToolResultLen)
		if text == "" {
			text = "<empty>"
		}
		call.ResultRef = run.ArtifactRef{
			ID:   fmt.Sprintf("msg-tool-%d-%d", call.IterationIndex, loc.callPos+1),
			Kind: "tool_result",
			Text: text,
		}
		a.lastTool = loc
		a.hasTool = true
	}
	a.current.UpdatedAt = a.normalizeTime(ts)
}

func (a *turnAssembler) markLastToolError(content string, ts time.Time) {
	if !a.hasCurrent || !a.hasTool {
		return
	}
	call := &a.current.Iterations[a.lastTool.iterPos].ToolCalls[a.lastTool.callPos]
	if strings.TrimSpace(call.Status) == "" || strings.EqualFold(call.Status, "called") {
		call.Status = "error"
	}
	if strings.TrimSpace(call.ResultRef.Text) == "" {
		call.ResultRef = run.ArtifactRef{
			ID:   fmt.Sprintf("msg-tool-%d-%d", call.IterationIndex, a.lastTool.callPos+1),
			Kind: "tool_result",
			Text: truncate(strings.TrimSpace(content), maxToolResultLen),
		}
	}
	a.current.UpdatedAt = a.normalizeTime(ts)
}

func (a *turnAssembler) locateTool(callID string, ts time.Time) (toolLocation, bool) {
	if callID != "" {
		if loc, ok := a.toolByID[callID]; ok {
			return loc, true
		}
	}
	if a.hasTool {
		return a.lastTool, true
	}
	// No prior tool call; create a synthetic placeholder so result data is not lost.
	iterPos := a.ensureIteration(ts)
	iter := &a.current.Iterations[iterPos]
	call := run.ToolCallRecord{
		CallID:         fmt.Sprintf("call-%d-%d", iter.IterationIndex, len(iter.ToolCalls)+1),
		IterationIndex: iter.IterationIndex,
		TraceID:        a.current.TraceID,
		SpanID:         fmt.Sprintf("span:%s:%s:iter:%d:tool:%d", a.current.SessionID, a.current.ID, iter.IterationIndex, len(iter.ToolCalls)+1),
		ParentSpanID:   iter.SpanID,
		Name:           "unknown",
		Status:         "called",
	}
	iter.ToolCalls = append(iter.ToolCalls, call)
	loc := toolLocation{iterPos: iterPos, callPos: len(iter.ToolCalls) - 1}
	a.lastTool = loc
	a.hasTool = true
	return loc, true
}

func (a *turnAssembler) finish(ts time.Time) []run.TurnRecord {
	if a.hasCurrent {
		a.finalizeCurrent(ts)
	}
	out := make([]run.TurnRecord, len(a.turns))
	for i := range a.turns {
		out[i] = a.turns[i].Clone()
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].TurnIndex == out[j].TurnIndex {
			return out[i].ID < out[j].ID
		}
		return out[i].TurnIndex < out[j].TurnIndex
	})
	return out
}

func (a *turnAssembler) finalizeCurrent(ts time.Time) {
	if !a.hasCurrent {
		return
	}
	if !ts.IsZero() {
		a.current.UpdatedAt = a.normalizeTime(ts)
	}
	if a.current.CreatedAt.IsZero() {
		a.current.CreatedAt = a.now()
	}
	if a.current.UpdatedAt.IsZero() {
		a.current.UpdatedAt = a.current.CreatedAt
	}
	if a.current.UpdatedAt.Before(a.current.CreatedAt) {
		a.current.UpdatedAt = a.current.CreatedAt
	}

	a.current.FinalOutput = run.MessageRef{
		ID:   fmt.Sprintf("msg-final-%d", a.current.TurnIndex),
		Role: "assistant",
		Text: a.pickFinalText(),
	}

	// Skip empty synthetic turns.
	if strings.TrimSpace(a.current.Prompt) != "" || len(a.current.Iterations) > 0 || strings.TrimSpace(a.current.FinalOutput.Text) != "" {
		a.turns = append(a.turns, a.current.Clone())
	}
	a.current = run.TurnRecord{}
	a.hasCurrent = false
	a.toolByID = make(map[string]toolLocation)
	a.hasTool = false
}

func (a *turnAssembler) pickFinalText() string {
	for i := len(a.current.Iterations) - 1; i >= 0; i-- {
		text := strings.TrimSpace(a.current.Iterations[i].Message.Text)
		if text != "" {
			return truncate(text, maxAssistantLen)
		}
	}
	for i := len(a.current.Iterations) - 1; i >= 0; i-- {
		iter := a.current.Iterations[i]
		for j := len(iter.ToolCalls) - 1; j >= 0; j-- {
			text := strings.TrimSpace(iter.ToolCalls[j].ResultRef.Text)
			if text != "" {
				return truncate(text, maxToolResultLen)
			}
		}
	}
	return truncate(strings.TrimSpace(a.current.Prompt), maxPromptLen)
}

func (a *turnAssembler) normalizeTime(ts time.Time) time.Time {
	if ts.IsZero() {
		return a.now().UTC()
	}
	return ts.UTC()
}

func parseCodexResponseItem(msg *codexjsonl.Message) (codexjsonl.ResponseItem, bool) {
	if msg == nil || msg.Type != "response_item" || len(msg.Payload) == 0 {
		return codexjsonl.ResponseItem{}, false
	}
	var item codexjsonl.ResponseItem
	if err := json.Unmarshal(msg.Payload, &item); err != nil {
		return codexjsonl.ResponseItem{}, false
	}
	return item, true
}

func codexToolResultText(item codexjsonl.ResponseItem) string {
	if strings.TrimSpace(item.Summary) != "" {
		return strings.TrimSpace(item.Summary)
	}
	raw := strings.TrimSpace(string(item.Output))
	if raw == "" {
		return ""
	}
	var asString string
	if json.Unmarshal(item.Output, &asString) == nil {
		return strings.TrimSpace(asString)
	}
	return raw
}

func extractClaudeToolUses(msg *claudejsonl.Message) []ToolUseInput {
	if msg == nil {
		return nil
	}
	var out []ToolUseInput

	if msg.ToolUse != nil {
		out = append(out, ToolUseInput{
			CallID: strings.TrimSpace(msg.ToolUse.ID),
			Name:   strings.TrimSpace(msg.ToolUse.Name),
			Args:   normalizeJSON(msg.ToolUse.Input),
		})
	}

	if len(msg.Message) == 0 {
		return dedupeToolUses(out)
	}
	var nested claudejsonl.NestedMessage
	if err := json.Unmarshal(msg.Message, &nested); err != nil || len(nested.Content) == 0 {
		return dedupeToolUses(out)
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(nested.Content, &blocks); err != nil {
		return dedupeToolUses(out)
	}
	for _, block := range blocks {
		var typ string
		_ = json.Unmarshal(block["type"], &typ)
		if typ != "tool_use" {
			continue
		}
		var name, callID string
		_ = json.Unmarshal(block["name"], &name)
		_ = json.Unmarshal(block["id"], &callID)
		out = append(out, ToolUseInput{
			CallID: strings.TrimSpace(callID),
			Name:   strings.TrimSpace(name),
			Args:   normalizeJSON(block["input"]),
		})
	}
	return dedupeToolUses(out)
}

func dedupeToolUses(in []ToolUseInput) []ToolUseInput {
	if len(in) == 0 {
		return nil
	}
	out := make([]ToolUseInput, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, u := range in {
		key := strings.TrimSpace(u.CallID) + "|" + strings.TrimSpace(u.Name) + "|" + strings.TrimSpace(string(normalizeJSON(u.Args)))
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, u)
	}
	return out
}

func extractClaudeToolResults(msg *claudejsonl.Message) []ToolResultInput {
	if msg == nil {
		return nil
	}
	var out []ToolResultInput

	if msg.ToolResult != nil {
		out = append(out, ToolResultInput{
			CallID:  strings.TrimSpace(msg.ToolResult.ToolUseID),
			IsError: msg.ToolResult.IsError,
			Content: truncate(strings.TrimSpace(msg.ToolResult.Content), maxToolResultLen),
		})
	}

	if len(msg.Message) == 0 {
		return out
	}
	var nested claudejsonl.NestedMessage
	if err := json.Unmarshal(msg.Message, &nested); err != nil || len(nested.Content) == 0 {
		return out
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(nested.Content, &blocks); err != nil {
		return out
	}
	for _, block := range blocks {
		var typ string
		_ = json.Unmarshal(block["type"], &typ)
		if typ != "tool_result" {
			continue
		}
		var callID string
		_ = json.Unmarshal(block["tool_use_id"], &callID)
		var isError bool
		_ = json.Unmarshal(block["is_error"], &isError)
		content := rawContentToText(block["content"])
		out = append(out, ToolResultInput{
			CallID:  strings.TrimSpace(callID),
			IsError: isError,
			Content: truncate(strings.TrimSpace(content), maxToolResultLen),
		})
	}
	return out
}

func rawContentToText(raw json.RawMessage) string {
	raw = normalizeJSON(raw)
	if len(raw) == 0 || string(raw) == "{}" {
		return ""
	}
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return strings.TrimSpace(asString)
	}
	var asMap map[string]any
	if json.Unmarshal(raw, &asMap) == nil {
		if text, ok := asMap["text"].(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return strings.TrimSpace(string(raw))
}

func normalizeSessionID(sessionID, path string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID != "" {
		return sessionID
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func sanitizeRefSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, "#", "-")
	value = strings.ReplaceAll(value, " ", "-")
	return value
}

func normalizeJSON(raw json.RawMessage) json.RawMessage {
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return json.RawMessage("{}")
	}
	if !json.Valid([]byte(value)) {
		return json.RawMessage("{}")
	}
	return json.RawMessage(value)
}

func truncate(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "..."
}
