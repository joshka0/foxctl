package longmemeval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/joshka0/foxctl/internal/intelligence/indexing/embedding"
	"github.com/joshka0/foxctl/internal/storage"
)

const (
	MemoryType        = "longmem_session"
	defaultMaxSummary = 500
	defaultMaxText    = 16000
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Case struct {
	QuestionID         string      `json:"question_id"`
	QuestionType       string      `json:"question_type"`
	Question           string      `json:"question"`
	QuestionDate       string      `json:"question_date"`
	Answer             string      `json:"answer"`
	AnswerSessionIDs   []string    `json:"answer_session_ids"`
	HaystackDates      []string    `json:"haystack_dates"`
	HaystackSessionIDs []string    `json:"haystack_session_ids"`
	HaystackSessions   [][]Message `json:"haystack_sessions"`
}

func (c *Case) UnmarshalJSON(data []byte) error {
	var payload struct {
		QuestionID         string          `json:"question_id"`
		QuestionType       string          `json:"question_type"`
		Question           string          `json:"question"`
		QuestionDate       string          `json:"question_date"`
		Answer             json.RawMessage `json:"answer"`
		AnswerSessionIDs   []string        `json:"answer_session_ids"`
		HaystackDates      []string        `json:"haystack_dates"`
		HaystackSessionIDs []string        `json:"haystack_session_ids"`
		HaystackSessions   [][]Message     `json:"haystack_sessions"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	answer, err := decodeAnswerText(payload.Answer)
	if err != nil {
		return err
	}
	*c = Case{
		QuestionID:         payload.QuestionID,
		QuestionType:       payload.QuestionType,
		Question:           payload.Question,
		QuestionDate:       payload.QuestionDate,
		Answer:             answer,
		AnswerSessionIDs:   payload.AnswerSessionIDs,
		HaystackDates:      payload.HaystackDates,
		HaystackSessionIDs: payload.HaystackSessionIDs,
		HaystackSessions:   payload.HaystackSessions,
	}
	return nil
}

func decodeAnswerText(raw json.RawMessage) (string, error) {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return "", fmt.Errorf("decode answer: %w", err)
	}
	switch v := value.(type) {
	case json.Number:
		return v.String(), nil
	case bool:
		return strconv.FormatBool(v), nil
	default:
		return "", fmt.Errorf("answer must be a scalar string, number, bool, or null")
	}
}

type IngestOptions struct {
	WorkspaceID     string
	EmbeddingModel  string
	MaxSummaryChars int
	MaxTextChars    int
}

type Plan struct {
	WorkspaceID    string
	EmbeddingModel string
	Records        []Record
	Leakage        []LeakageFinding
}

type Record struct {
	CaseID             string
	SessionID          string
	Name               string
	Type               string
	Summary            string
	AtomicText         string
	Entities           []string
	Keywords           []string
	Result             []byte
	EmbeddingInput     embedding.MemoryInput
	IsExpectedEvidence bool
	// SessionName is the memory name for the parent session (without chunk
	// suffix). Used by the eval to match chunked memories back to expected
	// session-level evidence names.
	SessionName string
}

type LeakageFinding struct {
	CaseID    string `json:"case_id"`
	SessionID string `json:"session_id,omitempty"`
	Field     string `json:"field"`
	Token     string `json:"token"`
	Reason    string `json:"reason"`
}

type ApplyResult struct {
	Saved   int
	Queued  int
	Skipped int
}

var ErrLeakage = errors.New("longmem ingest leakage detected")

func LoadCases(path string) ([]Case, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []Case
	if err := json.Unmarshal(body, &cases); err != nil {
		return nil, fmt.Errorf("decode longmem cases: %w", err)
	}
	return cases, nil
}

func BuildPlan(cases []Case, opts IngestOptions) (Plan, error) {
	workspaceID := strings.TrimSpace(opts.WorkspaceID)
	if workspaceID == "" {
		return Plan{}, fmt.Errorf("workspace_id is required")
	}
	maxSummary := opts.MaxSummaryChars
	if maxSummary <= 0 {
		maxSummary = defaultMaxSummary
	}
	maxText := opts.MaxTextChars
	if maxText <= 0 {
		maxText = defaultMaxText
	}

	plan := Plan{WorkspaceID: workspaceID, EmbeddingModel: strings.TrimSpace(opts.EmbeddingModel)}
	for _, c := range cases {
		caseID := strings.TrimSpace(c.QuestionID)
		if caseID == "" {
			return Plan{}, fmt.Errorf("longmem case missing question_id")
		}
		expected := stringSet(c.AnswerSessionIDs)
		for i, session := range c.HaystackSessions {
			sessionID := listValue(c.HaystackSessionIDs, i)
			if sessionID == "" {
				sessionID = fmt.Sprintf("session-%03d", i)
			}
			sessionDate := listValue(c.HaystackDates, i)
			isExpected := expected[sessionID]
			sessionName := memoryName(plan.WorkspaceID, caseID, sessionID)

			// Split session into turn-pair chunks for finer-grained retrieval.
			// Each user+assistant turn-pair becomes its own memory record so
			// that a critical fact buried in msg 4 of 12 is independently
			// retrievable instead of drowned in a 15K-char session blob.
			chunks := chunkSessionByTurnPairs(session)
			if len(chunks) == 0 {
				// Fallback: no messages — skip.
				continue
			}
			for ci, chunk := range chunks {
				rendered := renderChunk(chunk)
				if strings.TrimSpace(rendered) == "" {
					continue
				}
				chunkID := fmt.Sprintf("%s/chunk-%03d", sessionID, ci)
				semanticText := truncateText(renderSessionWithMetadata(rendered, sessionDate, chunkID), maxText)
				if strings.TrimSpace(semanticText) == "" {
					continue
				}
				rec := Record{
					CaseID:             caseID,
					SessionID:          sessionID,
					Name:               memoryName(plan.WorkspaceID, caseID, chunkID),
					Type:               MemoryType,
					Summary:            truncateText(semanticText, maxSummary),
					AtomicText:         semanticText,
					Entities:           extractEntities(semanticText, 12),
					Keywords:           extractKeywords(semanticText, 16),
					IsExpectedEvidence: isExpected,
					SessionName:        sessionName,
				}
				rec.Result = resultEnvelope()
				rec.EmbeddingInput = embedding.MemoryInput{
					Name:          rec.Name,
					Type:          rec.Type,
					Content:       rec.AtomicText,
					ContentDigest: digestString(rec.AtomicText),
				}
				// Leakage check: compare against the session source text,
				// not just the chunk, to avoid false negatives when the
				// answer spans multiple turns.
				plan.Leakage = append(plan.Leakage, CheckLeakage(c, rec)...)
				plan.Records = append(plan.Records, rec)
			}
		}
	}
	return plan, nil
}

func ApplyPlan(ctx context.Context, memoryStore storage.MemoryStore, queueStore *embedding.Store, plan Plan) (ApplyResult, error) {
	if memoryStore == nil {
		return ApplyResult{}, fmt.Errorf("memory store is required")
	}
	if queueStore == nil {
		return ApplyResult{}, fmt.Errorf("embedding queue store is required")
	}
	if strings.TrimSpace(plan.WorkspaceID) == "" {
		return ApplyResult{}, fmt.Errorf("workspace_id is required")
	}
	if len(plan.Leakage) > 0 {
		return ApplyResult{}, fmt.Errorf("%w: %d finding(s)", ErrLeakage, len(plan.Leakage))
	}
	var result ApplyResult
	inputs := make([]embedding.MemoryInput, 0, len(plan.Records))
	unchangedRecords := 0
	for _, rec := range plan.Records {
		inputs = append(inputs, rec.EmbeddingInput)
		if existing, err := memoryStore.Get(ctx, rec.Name, plan.WorkspaceID); err == nil && recordUnchanged(existing, rec) {
			unchangedRecords++
			result.Skipped++
			continue
		}
		if _, err := memoryStore.SaveFromResult(ctx, rec.Name, rec.Type, plan.WorkspaceID, rec.Summary, rec.Result); err != nil {
			return result, fmt.Errorf("save longmem memory %s: %w", rec.Name, err)
		}
		if err := memoryStore.UpdateAtomic(ctx, rec.Name, plan.WorkspaceID, rec.AtomicText, rec.Entities, rec.Keywords); err != nil {
			return result, fmt.Errorf("update longmem atomic %s: %w", rec.Name, err)
		}
		result.Saved++
	}
	queued, err := queueStore.EnqueueMemories(ctx, embedding.MemoryEnqueueRequest{
		WorkspaceID: plan.WorkspaceID,
		Memories:    inputs,
		Model:       plan.EmbeddingModel,
	})
	if err != nil {
		return result, fmt.Errorf("enqueue longmem memories: %w", err)
	}
	result.Queued = queued.Queued
	if queueSkipped := queued.Skipped - unchangedRecords; queueSkipped > 0 {
		result.Skipped += queueSkipped
	}
	return result, nil
}

func recordUnchanged(existing storage.NamedEntry, rec Record) bool {
	return strings.TrimSpace(existing.Type) == strings.TrimSpace(rec.Type) &&
		strings.TrimSpace(existing.Summary) == strings.TrimSpace(rec.Summary) &&
		string(existing.Result) == string(rec.Result) &&
		strings.TrimSpace(existing.AtomicText) == strings.TrimSpace(rec.AtomicText) &&
		stringSlicesEqual(existing.Entities, rec.Entities) &&
		stringSlicesEqual(existing.Keywords, rec.Keywords)
}

func CheckLeakage(c Case, rec Record) []LeakageFinding {
	source := renderSession(sessionForCase(c, rec.SessionID))
	// For leakage checking, strip the metadata header from atomic_text and
	// embedding_content. The [Session date: ...] header is structural metadata
	// from the dataset (haystack_dates), not conversation content. Numeric
	// answers like "3" can collide with year digits in the date header.
	atomicTextStripped := stripSessionMetadata(rec.AtomicText)
	embeddingContentStripped := stripSessionMetadata(rec.EmbeddingInput.Content)
	fields := map[string]string{
		"name":              rec.Name,
		"summary":           stripSessionMetadata(rec.Summary),
		"atomic_text":       atomicTextStripped,
		"embedding_content": embeddingContentStripped,
		"entities":          strings.Join(rec.Entities, " "),
		"keywords":          strings.Join(rec.Keywords, " "),
	}
	var findings []LeakageFinding
	for field, text := range fields {
		findings = append(findings, leakageForToken(c, rec, field, text, c.QuestionID, "question_id", false)...)
		findings = append(findings, leakageForToken(c, rec, field, text, c.Question, "question", false)...)
		findings = append(findings, leakageForToken(c, rec, field, text, c.QuestionDate, "question_date", false)...)
		findings = append(findings, leakageForToken(c, rec, field, text, c.QuestionType, "question_type", false)...)
		for _, id := range c.AnswerSessionIDs {
			findings = append(findings, leakageForToken(c, rec, field, text, id, "answer_session_id", false)...)
		}
		findings = append(findings, leakageForToken(c, rec, field, text, c.Answer, "answer", containsFold(source, c.Answer))...)
	}
	return findings
}

func leakageForToken(c Case, rec Record, field, text, token, reason string, allowedBySource bool) []LeakageFinding {
	token = strings.TrimSpace(token)
	if token == "" || allowedBySource || !containsFold(text, token) {
		return nil
	}
	if field == "name" && reason == "answer" && lowInformationNameAnswerToken(token) {
		return nil
	}
	return []LeakageFinding{{
		CaseID:    strings.TrimSpace(c.QuestionID),
		SessionID: rec.SessionID,
		Field:     field,
		Token:     token,
		Reason:    reason,
	}}
}

func lowInformationNameAnswerToken(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" || len(token) >= 8 {
		return false
	}
	for _, ch := range token {
		if !strings.ContainsRune("0123456789abcdef", ch) {
			return false
		}
	}
	return true
}

func resultEnvelope() []byte {
	body := map[string]any{
		"version": "1",
		"status":  "ok",
		"command": "longmem/ingest",
		"data":    map[string]any{},
		"meta": map[string]any{
			"source":          "longmemeval",
			"semantic_fields": []string{"summary", "atomic_text", "entities", "keywords"},
		},
		"error": map[string]any{},
	}
	out, _ := json.Marshal(body)
	return out
}

func renderSession(messages []Message) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		if role == "" {
			parts = append(parts, content)
			continue
		}
		parts = append(parts, role+": "+content)
	}
	return strings.Join(parts, "\n")
}

// chunkSessionByTurnPairs splits a session into chunks of user+assistant
// turn-pairs. Each chunk contains one user message followed by its assistant
// response (if present). This creates fine-grained memory records where a
// single critical fact is independently retrievable instead of buried inside
// a 15K-character monolithic session blob.
//
// Chunking strategy:
//   - Pair each user message with the immediately following assistant message.
//   - Unpaired trailing messages form their own chunk.
//   - Empty messages are skipped.
func chunkSessionByTurnPairs(messages []Message) [][]Message {
	if len(messages) == 0 {
		return nil
	}
	var chunks [][]Message
	i := 0
	for i < len(messages) {
		msg := messages[i]
		if strings.TrimSpace(msg.Content) == "" {
			i++
			continue
		}
		chunk := []Message{msg}
		// If the next message is from the assistant, pair it.
		if i+1 < len(messages) {
			next := messages[i+1]
			if strings.TrimSpace(next.Content) != "" {
				chunk = append(chunk, next)
				i += 2
				chunks = append(chunks, chunk)
				continue
			}
		}
		chunks = append(chunks, chunk)
		i++
	}
	return chunks
}

// renderChunk renders a chunk of messages using the same format as
// renderSession. Kept separate to allow future per-chunk formatting.
func renderChunk(messages []Message) string {
	return renderSession(messages)
}

// renderSessionWithMetadata prepends a metadata header to the rendered session
// text. The header includes the session date (when available) and session ID,
// giving the model temporal context for temporal-reasoning questions. The
// metadata is separated from conversation content by a blank line and a
// "Conversation:" label so the model can distinguish metadata from content.
//
// Example output:
//
//	[Session date: 2023/01/08 (Sun) 12:49]
//
//	Conversation:
//	user: I just got back from a guided tour at the Museum of Modern Art...
//	assistant: What a great experience! ...
func renderSessionWithMetadata(rendered, sessionDate, sessionID string) string {
	rendered = strings.TrimSpace(rendered)
	if rendered == "" {
		return ""
	}
	var b strings.Builder
	// Session date header.
	date := strings.TrimSpace(sessionDate)
	if date != "" {
		b.WriteString("[Session date: ")
		b.WriteString(date)
		b.WriteString("]\n\n")
	}
	b.WriteString("Conversation:\n")
	b.WriteString(rendered)
	return b.String()
}

// stripSessionMetadata removes the [Session date: ...] header and
// "Conversation:" label added by renderSessionWithMetadata, returning only
// the conversation content. Used by CheckLeakage to avoid false leakage
// from date digits colliding with short numeric answers.
func stripSessionMetadata(text string) string {
	text = strings.TrimSpace(text)
	// Remove the leading [Session date: ...] block.
	if strings.HasPrefix(text, "[Session date:") {
		if idx := strings.Index(text, "]\n"); idx >= 0 {
			text = strings.TrimSpace(text[idx+2:])
		}
	}
	// Remove the "Conversation:" label.
	text = strings.TrimPrefix(text, "Conversation:\n")
	return strings.TrimSpace(text)
}

func sessionForCase(c Case, sessionID string) []Message {
	for i, candidate := range c.HaystackSessionIDs {
		if strings.TrimSpace(candidate) == strings.TrimSpace(sessionID) && i < len(c.HaystackSessions) {
			return c.HaystackSessions[i]
		}
	}
	return nil
}

func memoryName(workspaceID, caseID, sessionID string) string {
	return "longmem://" + digestString(strings.Join([]string{
		strings.TrimSpace(workspaceID),
		strings.TrimSpace(caseID),
		strings.TrimSpace(sessionID),
	}, "\x00"))[:24]
}

func digestString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func listValue(values []string, index int) string {
	if index < 0 || index >= len(values) {
		return ""
	}
	return strings.TrimSpace(values[index])
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}

func truncateText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return strings.TrimSpace(string(runes[:max]))
}

var wordRE = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_'-]*`)

func extractKeywords(text string, limit int) []string {
	counts := map[string]int{}
	for _, raw := range wordRE.FindAllString(text, -1) {
		word := strings.ToLower(strings.Trim(raw, "'-_"))
		if len(word) < 4 || stopWords[word] {
			continue
		}
		counts[word]++
	}
	words := make([]string, 0, len(counts))
	for word := range counts {
		words = append(words, word)
	}
	sort.Slice(words, func(i, j int) bool {
		if counts[words[i]] == counts[words[j]] {
			return words[i] < words[j]
		}
		return counts[words[i]] > counts[words[j]]
	})
	if limit > 0 && len(words) > limit {
		words = words[:limit]
	}
	return words
}

func extractEntities(text string, limit int) []string {
	seen := map[string]struct{}{}
	entities := make([]string, 0, limit)
	for _, word := range wordRE.FindAllString(text, -1) {
		if len(word) < 3 || strings.ToLower(word) == word {
			continue
		}
		if _, ok := seen[word]; ok {
			continue
		}
		seen[word] = struct{}{}
		entities = append(entities, word)
		if limit > 0 && len(entities) >= limit {
			break
		}
	}
	return entities
}

func containsFold(text, token string) bool {
	text = strings.TrimSpace(text)
	token = strings.TrimSpace(token)
	if text == "" || token == "" {
		return false
	}
	return strings.Contains(strings.ToLower(text), strings.ToLower(token))
}

var stopWords = map[string]bool{
	"about": true, "also": true, "from": true, "have": true, "that": true,
	"this": true, "with": true, "your": true, "their": true, "there": true,
	"what": true, "when": true, "where": true, "which": true, "will": true,
	"would": true, "could": true, "should": true, "assistant": true, "user": true,
}
