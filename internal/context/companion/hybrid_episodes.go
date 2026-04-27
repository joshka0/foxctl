package companion

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/rs/zerolog/log"
)

const (
	episodeMaxTurns        = 20
	toolRunOrphanWindow    = int64(50)
	topicDriftThreshold    = 0.15
	episodeTopicTokenLimit = 12
)

type explicitFactPattern struct {
	Key       string
	EntryType string
	Regexps   []*regexp.Regexp
}

var explicitFactPatterns = []explicitFactPattern{
	{
		Key:       "owner",
		EntryType: EntryTypeTechnicalContext,
		Regexps: []*regexp.Regexp{
			regexp.MustCompile(`(?i)\bupdate (?:the )?owner from [^.\n]+? to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\bchange (?:the )?owner to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\bset (?:the )?owner to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\bowner(?:\s+is|:)?\s+([^,.;\n]+)`),
		},
	},
	{
		Key:       "codename",
		EntryType: EntryTypeTechnicalContext,
		Regexps: []*regexp.Regexp{
			regexp.MustCompile(`(?i)\bupdate (?:the )?codename from [^.\n]+? to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\bchange (?:the )?codename to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\bset (?:the )?codename to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\bcodename(?:\s+is|:)?\s+([^,.;\n]+)`),
		},
	},
	{
		Key:       "deploy_window",
		EntryType: EntryTypeTechnicalContext,
		Regexps: []*regexp.Regexp{
			regexp.MustCompile(`(?i)\bupdate (?:the )?deploy window from [^.\n]+? to ([^.\n]+)`),
			regexp.MustCompile(`(?i)\bchange (?:the )?deploy window to ([^.\n]+)`),
			regexp.MustCompile(`(?i)\bset (?:the )?deploy window to ([^.\n]+)`),
			regexp.MustCompile(`(?i)\bdeploy window(?:\s+is|:)?\s+([^,.;\n]+(?:\s+[^,.;\n]+){0,4})`),
		},
	},
	{
		Key:       "rollback_color",
		EntryType: EntryTypeTechnicalContext,
		Regexps: []*regexp.Regexp{
			regexp.MustCompile(`(?i)\bupdate (?:the )?rollback color from [^.\n]+? to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\bchange (?:the )?rollback color to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\bset (?:the )?rollback color to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\brollback color(?:\s+is|:)?\s+([^,.;\n]+)`),
		},
	},
	{
		Key:       "code_word",
		EntryType: EntryTypeTechnicalContext,
		Regexps: []*regexp.Regexp{
			regexp.MustCompile(`(?i)\bupdate (?:the )?code word from [^.\n]+? to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\bchange (?:the )?code word to ([^,.;\n]+)`),
			regexp.MustCompile(`(?i)\bcode word(?:\s+is|:)?\s+([^,.;\n]+)`),
		},
	},
}

// updateOpenEpisodeState updates the open episode's event count, topic signature,
// and manages tool runs. This runs on EVERY event (Tier 0).
func (m *ConversationMemory) updateOpenEpisodeState(ctx context.Context, tx *sql.Tx, convID string, event ConversationEvent) error {
	var startEventID int64
	var episodeType string
	eventCount := 0
	var topicSig sql.NullString

	if err := tx.QueryRowContext(ctx, `
		SELECT start_event_id, episode_type, event_count, topic_sig
		FROM companion_open_episode
		WHERE conversation_id = $1
	`, convID).Scan(&startEventID, &episodeType, &eventCount, &topicSig); err != nil {
		if err != sql.ErrNoRows {
			return fmt.Errorf("read open episode state: %w", err)
		}

		startEventID = event.ID
		episodeType = "exploration"
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_open_episode
				(conversation_id, start_event_id, episode_type, event_count, topic_sig, pending_seal_reason, updated_at)
			VALUES ($1, $2, $3, 0, NULL, NULL, CURRENT_TIMESTAMP)
		`, convID, startEventID, episodeType); err != nil {
			return fmt.Errorf("initialize open episode state: %w", err)
		}
	}

	eventCount++
	newTopicSig := topicSignature(topicSig.String, event.Content)

	if _, err := tx.ExecContext(ctx, `
		UPDATE companion_open_episode
		SET event_count = $1,
			topic_sig = $2,
			updated_at = CURRENT_TIMESTAMP
		WHERE conversation_id = $3
	`, eventCount, newTopicSig, convID); err != nil {
		return fmt.Errorf("update open episode state: %w", err)
	}

	switch event.EventType {
	case "tool_call":
		toolRunID := strings.TrimSpace(event.ToolRunID)
		if toolRunID == "" {
			log.Warn().
				Str("conversation_id", convID).
				Int64("event_id", event.ID).
				Msg("tool_call missing tool_run_id")
			break
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO companion_open_tool_runs
				(conversation_id, tool_run_id, start_event_id, parent_call_event_id, created_at)
			VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP)
			ON CONFLICT (conversation_id, tool_run_id) DO NOTHING
		`, convID, toolRunID, event.ID, nullableInt64(event.ParentToolCallID)); err != nil {
			return fmt.Errorf("insert open tool run: %w", err)
		}

	case "tool_result":
		toolRunID := strings.TrimSpace(event.ToolRunID)
		if toolRunID == "" && event.ParentToolCallID > 0 {
			if err := tx.QueryRowContext(ctx, `
				SELECT tool_run_id
				FROM companion_open_tool_runs
				WHERE conversation_id = $1 AND parent_call_event_id = $2
				LIMIT 1
			`, convID, event.ParentToolCallID).Scan(&toolRunID); err != nil {
				if err == sql.ErrNoRows {
					log.Warn().
						Str("conversation_id", convID).
						Int64("event_id", event.ID).
						Msg("tool_result without matching tool run")
					toolRunID = ""
				} else {
					return fmt.Errorf("lookup tool run by parent tool call id: %w", err)
				}
			}
		}

		if toolRunID == "" {
			log.Warn().
				Str("conversation_id", convID).
				Int64("event_id", event.ID).
				Msg("tool_result missing tool_run_id; skipping tool run cleanup")
			return nil
		}

		res, err := tx.ExecContext(ctx, `
			DELETE FROM companion_open_tool_runs
			WHERE conversation_id = $1 AND tool_run_id = $2
		`, convID, toolRunID)
		if err != nil {
			return fmt.Errorf("delete open tool run: %w", err)
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			log.Warn().
				Str("conversation_id", convID).
				Int64("event_id", event.ID).
				Str("tool_run_id", toolRunID).
				Msg("tool_result had no matching open tool run")
		}

		// Check for deferred seal: if no tool runs remain and a pending_seal_reason
		// was set, trigger the deferred episode seal now.
		var remainingRuns int
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM companion_open_tool_runs
			WHERE conversation_id = $1
		`, convID).Scan(&remainingRuns); err != nil {
			return fmt.Errorf("count remaining tool runs after drain: %w", err)
		}
		if remainingRuns == 0 {
			var pendingSeal sql.NullString
			if err := tx.QueryRowContext(ctx, `
				SELECT pending_seal_reason
				FROM companion_open_episode
				WHERE conversation_id = $1
			`, convID).Scan(&pendingSeal); err != nil && err != sql.ErrNoRows {
				return fmt.Errorf("check pending seal reason: %w", err)
			}
			if pendingSeal.Valid && strings.TrimSpace(pendingSeal.String) != "" {
				if err := m.handleEpisodeSeal(ctx, tx, convID, ""); err != nil {
					return fmt.Errorf("execute deferred seal on tool drain: %w", err)
				}
			}
		}
	}

	orphanThreshold := event.ID - toolRunOrphanWindow
	if orphanThreshold < 0 {
		orphanThreshold = 0
	}
	res, err := tx.ExecContext(ctx, `
		DELETE FROM companion_open_tool_runs
		WHERE conversation_id = $1
			AND start_event_id < $2
	`, convID, orphanThreshold)
	if err != nil {
		return fmt.Errorf("delete orphaned open tool runs: %w", err)
	}
	if rows, _ := res.RowsAffected(); rows > 0 {
		log.Warn().
			Str("conversation_id", convID).
			Int64("event_id", event.ID).
			Int64("threshold", orphanThreshold).
			Int64("removed", rows).
			Msg("force-removed orphaned open tool runs")
	}

	return nil
}

// checkEpisodeBoundary determines if the current open episode should be sealed.
// Returns the seal reason or "" if no boundary triggered.
func (m *ConversationMemory) checkEpisodeBoundary(ctx context.Context, tx *sql.Tx, convID string, event ConversationEvent) (string, error) {
	var startEventID int64
	var eventCount int
	var topicSig sql.NullString

	if err := tx.QueryRowContext(ctx, `
		SELECT start_event_id, event_count, topic_sig
		FROM companion_open_episode
		WHERE conversation_id = $1
	`, convID).Scan(&startEventID, &eventCount, &topicSig); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("load open episode for boundary check: %w", err)
	}

	var openToolRuns int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM companion_open_tool_runs
		WHERE conversation_id = $1
	`, convID).Scan(&openToolRuns); err != nil {
		return "", fmt.Errorf("count open tool runs: %w", err)
	}

	if eventCount >= episodeMaxTurns {
		return "max_turns", nil
	}

	if isToolResultEvent(event) && openToolRuns == 0 {
		toolCalls := 0
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM companion_events
			WHERE conversation_id = $1
				AND id > $2
				AND id <= $3
				AND event_type = 'tool_call'
		`, convID, startEventID, event.ID).Scan(&toolCalls); err != nil {
			return "", fmt.Errorf("check tool calls for boundary: %w", err)
		}
		if toolCalls > 0 {
			return "tool_chain_complete", nil
		}
	}

	if topicSig.Valid && topicSig.String != "" {
		if detectTopicDrift(extractTopicTokenSet(event.Content), parseTopicSig(topicSig.String)) {
			return "topic_drift", nil
		}
	}

	if isAssistantEvent(event) && openToolRuns == 0 {
		toolCalls := 0
		var lastToolEventType sql.NullString
		if err := tx.QueryRowContext(ctx, `
			SELECT event_type
			FROM companion_events
			WHERE conversation_id = $1
				AND id > $2
				AND id <= $3
				AND event_type IN ('tool_call', 'tool_result')
			ORDER BY id DESC
			LIMIT 1
		`, convID, startEventID, event.ID).Scan(&lastToolEventType); err != nil && err != sql.ErrNoRows {
			return "", fmt.Errorf("check latest tool event for boundary: %w", err)
		}
		if err := tx.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM companion_events
			WHERE conversation_id = $1
				AND id >= $2
				AND id < $3
				AND event_type = 'tool_call'
		`, convID, startEventID, event.ID).Scan(&toolCalls); err != nil {
			return "", fmt.Errorf("check tool-call history for final answer boundary: %w", err)
		}
		if lastToolEventType.Valid && lastToolEventType.String == "tool_result" && toolCalls > 0 {
			return "final_answer", nil
		}
	}

	if m.signalExtractor.DetectBoundarySignal(event.Content) == SignalAssumptionInvalidated {
		return "assumption_invalidated", nil
	}

	if m.signalExtractor.DetectBoundarySignal(event.Content) == SignalUserRedirect {
		return "user_redirect", nil
	}

	return "", nil
}

// handleEpisodeSeal either seals the episode immediately or defers it.
// If tool runs are open, sets pending_seal_reason instead of sealing.
// If tool runs become empty AND pending_seal_reason is set, executes deferred seal.
func (m *ConversationMemory) handleEpisodeSeal(ctx context.Context, tx *sql.Tx, convID string, reason string) error {
	var openToolRuns int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM companion_open_tool_runs
		WHERE conversation_id = $1
	`, convID).Scan(&openToolRuns); err != nil {
		return fmt.Errorf("count open tool runs: %w", err)
	}

	var startEventID int64
	var episodeType string
	var pendingReason sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT start_event_id, episode_type, pending_seal_reason
		FROM companion_open_episode
		WHERE conversation_id = $1
	`, convID).Scan(&startEventID, &episodeType, &pendingReason); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return fmt.Errorf("load open episode for seal: %w", err)
	}

	if openToolRuns > 0 {
		if strings.TrimSpace(reason) != "" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE companion_open_episode
				SET pending_seal_reason = $1,
					updated_at = CURRENT_TIMESTAMP
				WHERE conversation_id = $2
			`, reason, convID); err != nil {
				return fmt.Errorf("defer episode seal: %w", err)
			}
		}
		return nil
	}

	sealReason := strings.TrimSpace(reason)
	if sealReason == "" && pendingReason.Valid {
		sealReason = pendingReason.String
	}
	if sealReason == "" {
		return nil
	}

	if episodeType == "" {
		episodeType = "exploration"
	}

	var endEventID int64
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(id), 0)
		FROM companion_events
		WHERE conversation_id = $1
	`, convID).Scan(&endEventID); err != nil {
		return fmt.Errorf("lookup end event for episode seal: %w", err)
	}
	if endEventID == 0 || endEventID < startEventID {
		return nil
	}

	if _, err := m.sealEpisodeInTx(ctx, tx, convID, startEventID, endEventID, episodeType); err != nil {
		return err
	}

	if err := m.runGateMissBackstop(ctx, tx, convID, startEventID, endEventID); err != nil {
		log.Warn().
			Str("conversation_id", convID).
			Err(err).
			Msg("gate-miss backstop failed")
	}

	return nil
}

// sealEpisodeInTx inserts the episode row with needs_summary=1 inside the transaction.
// Returns the episode ID. LLM summary generation happens OUTSIDE the transaction.
func (m *ConversationMemory) sealEpisodeInTx(ctx context.Context, tx *sql.Tx, convID string, startEventID, endEventID int64, episodeType string) (int64, error) {
	if episodeType == "" {
		episodeType = "exploration"
	}

	boundaryHash := episodeBoundaryHash(startEventID, endEventID, episodeType)
	var episodeID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO companion_soft_episodes
			(conversation_id, episode_type, start_event_id, end_event_id, summary, needs_summary, assumption_ids, token_count, boundary_hash, created_at)
		VALUES ($1, $2, $3, $4, '', 1, '[]', 0, $5, CURRENT_TIMESTAMP)
		ON CONFLICT (conversation_id, boundary_hash)
		DO UPDATE SET
			end_event_id = EXCLUDED.end_event_id,
			summary = '',
			needs_summary = 1,
			episode_type = EXCLUDED.episode_type
		RETURNING id
	`, convID, episodeType, startEventID, endEventID, boundaryHash).Scan(&episodeID); err != nil {
		return 0, fmt.Errorf("insert sealed episode: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM companion_open_tool_runs
		WHERE conversation_id = $1
	`, convID); err != nil {
		return 0, fmt.Errorf("delete open tool runs after seal: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		UPDATE companion_open_episode
		SET start_event_id = $2,
			event_count = 0,
			topic_sig = NULL,
			pending_seal_reason = NULL,
			updated_at = CURRENT_TIMESTAMP
		WHERE conversation_id = $1
	`, convID, endEventID+1); err != nil {
		return 0, fmt.Errorf("reset open episode after seal: %w", err)
	}

	return episodeID, nil
}

// normalizeEntryKey normalizes a raw text extraction into a stable HardState key.
// Returns (key, isAmbiguous). If ambiguous, the entry should go to staging.
// For monotonic entry types (decision, open_question), tx and convID are used
// to derive the next counter from the database rather than process memory.
func normalizeEntryKey(ctx context.Context, tx *sql.Tx, convID, entryType, rawText string) (string, bool) {
	rawText = strings.TrimSpace(rawText)
	if rawText == "" {
		return "", true
	}

	switch entryType {
	case "preference":
		topic := normalizePreferenceTopic(rawText)
		if topic == "" {
			return "", true
		}
		return "pref:" + toSnakeCase(topic), false

	case "glossary":
		term := normalizeGlossaryTerm(rawText)
		if term == "" {
			return "", true
		}
		return "term:" + term, false

	case "decision":
		idx := nextMonotonicIndex(ctx, tx, convID, "decision", "dec:")
		return fmt.Sprintf("dec:%03d", idx), false

	case "open_question":
		idx := nextMonotonicIndex(ctx, tx, convID, "open_question", "q:")
		return fmt.Sprintf("q:%03d", idx), false

	case "goal":
		return "goal:current", false

	case "policy":
		policy := normalizePolicyName(rawText)
		if policy == "" {
			return "", true
		}
		return "policy:" + policy, false

	case EntryTypeTechnicalContext:
		key := toSnakeCase(rawText)
		if key == "" {
			return "", true
		}
		return "tech:" + key, false

	case EntryTypeIdentity:
		key := toSnakeCase(rawText)
		if key == "" {
			return "", true
		}
		return "identity:" + key, false
	}

	return "", true
}

func extractExplicitFacts(text string) []ExtractedEntry {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	seen := make(map[string]struct{}, len(explicitFactPatterns))
	out := make([]ExtractedEntry, 0, len(explicitFactPatterns))
	for _, pattern := range explicitFactPatterns {
		for _, re := range pattern.Regexps {
			matches := re.FindAllStringSubmatch(text, -1)
			if len(matches) == 0 {
				continue
			}
			for _, match := range matches {
				if len(match) < 2 {
					continue
				}
				value := cleanExplicitFactValue(match[1])
				if value == "" {
					continue
				}
				dedupeKey := pattern.EntryType + ":" + pattern.Key + ":" + strings.ToLower(value)
				if _, ok := seen[dedupeKey]; ok {
					continue
				}
				seen[dedupeKey] = struct{}{}
				out = append(out, ExtractedEntry{
					EntryType:  pattern.EntryType,
					Key:        pattern.Key,
					RawText:    strings.TrimSpace(match[0]),
					Value:      value,
					Confidence: 0.94,
				})
			}
			break
		}
	}
	return out
}

// stageAmbiguousEntry queues an entry that couldn't be normalized deterministically.
func (m *ConversationMemory) stageAmbiguousEntry(ctx context.Context, tx *sql.Tx, convID string, sourceEventID int64, entry ExtractedEntry, reason string) error {
	if strings.TrimSpace(reason) == "" {
		reason = "deterministic_ambiguity"
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO companion_extraction_staging
			(conversation_id, source_event_id, proposed_entry_type, raw_text, reason, attempt_count, created_at)
		VALUES ($1, $2, $3, $4, $5, 0, CURRENT_TIMESTAMP)
	`, convID, sourceEventID, entry.EntryType, entry.RawText, reason); err != nil {
		return fmt.Errorf("stage ambiguous extraction: %w", err)
	}

	return nil
}

// runGateMissBackstop runs a light extraction pass over all turns in [startEvent, endEvent]
// that were not individually processed by Tier 1. Called at episode seal time.
func (m *ConversationMemory) runGateMissBackstop(ctx context.Context, tx *sql.Tx, convID string, startEventID, endEventID int64) error {
	if startEventID <= 0 || endEventID <= 0 || endEventID < startEventID {
		return nil
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT e.id, t.content, e.event_type, e.payload_json
		FROM companion_events e
		LEFT JOIN companion_turns t ON t.id = e.turn_id
		WHERE e.conversation_id = $1
			AND e.id >= $2
			AND e.id <= $3
		ORDER BY e.id
	`, convID, startEventID, endEventID)
	if err != nil {
		return fmt.Errorf("select gate-miss events: %w", err)
	}
	defer rows.Close()

	type unprocessedEvent struct {
		id   int64
		text string
	}
	var events []unprocessedEvent
	for rows.Next() {
		var eventID int64
		var content sql.NullString
		var eventType sql.NullString
		var payload sql.NullString
		if err := rows.Scan(&eventID, &content, &eventType, &payload); err != nil {
			return fmt.Errorf("scan event for gate miss: %w", err)
		}

		processed, err := m.wasEventProcessedByTier1(ctx, tx, convID, eventID)
		if err != nil {
			return fmt.Errorf("lookup tier1 status for event %d: %w", eventID, err)
		}
		if processed {
			continue
		}

		text := ""
		if content.Valid {
			text = content.String
		} else if eventType.Valid && strings.HasPrefix(eventType.String, "tool") && payload.Valid {
			text = payload.String
		}
		if strings.TrimSpace(text) != "" {
			events = append(events, unprocessedEvent{id: eventID, text: text})
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate gate miss events: %w", err)
	}

	for _, ev := range events {
		text := strings.TrimSpace(ev.text)
		if text == "" {
			continue
		}

		// Use all categories for backstop extraction
		allCategories := []string{
			ExtractionCategoryPreference, ExtractionCategoryDecision,
			ExtractionCategoryQuestion, ExtractionCategoryGoalChange,
			ExtractionCategoryRetraction,
		}
		extractions := m.extractionPolicy.ExtractEntries(text, allCategories)

		for _, entry := range extractions {
			if err := m.persistDeterministicExtraction(ctx, tx, convID, ev.id, entry); err != nil {
				return err
			}
		}
	}

	return nil
}

// detectTopicDrift checks if the current event's content has drifted from the episode topic.
// Uses Jaccard similarity as the primary method (no embedding dependency).
func detectTopicDrift(currentTokens, episodeTokens map[string]struct{}) bool {
	if len(currentTokens) == 0 || len(episodeTokens) == 0 {
		return false
	}

	intersection := 0
	union := len(episodeTokens)
	for token := range currentTokens {
		if _, ok := episodeTokens[token]; ok {
			intersection++
		} else {
			union++
		}
	}

	return float64(intersection)/float64(union) < topicDriftThreshold
}

func (m *ConversationMemory) wasEventProcessedByTier1(ctx context.Context, tx *sql.Tx, convID string, eventID int64) (bool, error) {
	var exists int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM (
			SELECT 1 FROM companion_hard_state_entries WHERE conversation_id = $1 AND source_event_id = $2
			UNION
			SELECT 1 FROM companion_extraction_staging WHERE conversation_id = $1 AND source_event_id = $2
		) q
	`, convID, eventID).Scan(&exists); err != nil {
		return false, err
	}
	return exists > 0, nil
}

func (m *ConversationMemory) persistDeterministicExtraction(ctx context.Context, tx *sql.Tx, convID string, sourceEventID int64, entry ExtractedEntry) error {
	if strings.TrimSpace(entry.EntryType) == "" {
		return nil
	}

	value := strings.TrimSpace(entry.Value)
	if value == "" {
		value = strings.TrimSpace(entry.RawText)
	}
	if value == "" {
		return nil
	}

	keySource := value
	if strings.TrimSpace(entry.Key) != "" {
		keySource = strings.TrimSpace(entry.Key)
	}
	key, ambiguous := normalizeEntryKey(ctx, tx, convID, entry.EntryType, keySource)
	if ambiguous {
		return m.stageAmbiguousEntry(ctx, tx, convID, sourceEventID, entry, "deterministic_ambiguity")
	}

	valueJSON, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal extracted value: %w", err)
	}

	var alreadySeen int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM companion_hard_state_entries
		WHERE conversation_id = $1
			AND entry_type = $2
			AND key = $3
			AND source_event_id = $4
			AND value_json = $5
	`, convID, entry.EntryType, key, sourceEventID, string(valueJSON)).Scan(&alreadySeen); err != nil {
		return fmt.Errorf("check existing deterministic extraction: %w", err)
	}
	if alreadySeen > 0 {
		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO companion_hard_state_entries
			(conversation_id, entry_type, key, value_json, status, source_event_id, confidence, metadata_json, supersedes, created_at)
		VALUES ($1, $2, $3, $4, 'active', $5, $6, NULL, NULL, CURRENT_TIMESTAMP)
	`, convID, entry.EntryType, key, string(valueJSON), sourceEventID, defaultConfidence(entry.Confidence)); err != nil {
		return fmt.Errorf("insert hard state entry: %w", err)
	}

	return nil
}

func isAssistantEvent(event ConversationEvent) bool {
	return event.EventType == "assistant_message" || event.EventType == "assistant"
}

func isToolResultEvent(event ConversationEvent) bool {
	return event.EventType == "tool_result"
}

func defaultConfidence(v float64) float64 {
	if v > 0 {
		return v
	}
	return 0.75
}

func episodeBoundaryHash(startEventID, endEventID int64, episodeType string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d|%d|%s", startEventID, endEventID, episodeType)))
	return hex.EncodeToString(h[:])
}

func nullableInt64(v int64) sql.NullInt64 {
	if v <= 0 {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: v, Valid: true}
}

func extractByPatterns(text string, patterns []string, entryType string, confidence float64) []ExtractedEntry {
	lower := strings.ToLower(text)
	seen := map[string]struct{}{}
	var out []ExtractedEntry
	for _, pattern := range patterns {
		idx := strings.Index(lower, pattern)
		if idx < 0 {
			continue
		}
		value := firstSentence(strings.TrimSpace(text[idx+len(pattern):]))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, ExtractedEntry{
			EntryType:  entryType,
			RawText:    value,
			Value:      value,
			Confidence: confidence,
		})
	}
	return out
}

func cleanExplicitFactValue(value string) string {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	if value == "" {
		return ""
	}

	lower := strings.ToLower(value)
	for _, marker := range []string{
		" reply only",
		" reply with",
		" respond only",
		" respond with",
		" answer only",
		" answer with",
		" and change",
		" and update",
		" and set",
		" and add",
		" and reply",
		" and respond",
		" and answer",
	} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			value = value[:idx]
			lower = lower[:idx]
		}
	}

	value = strings.Trim(strings.TrimSpace(value), `"'`)
	value = strings.TrimSuffix(value, ".")
	return strings.TrimSpace(value)
}

func firstMatchAfterPattern(text string, patterns []string) string {
	lower := strings.ToLower(text)
	for _, pattern := range patterns {
		idx := strings.Index(lower, pattern)
		if idx < 0 {
			continue
		}
		return firstSentence(strings.TrimSpace(text[idx+len(pattern):]))
	}
	return ""
}

func splitQuestionParts(sentence string) []string {
	parts := strings.Split(sentence, "?")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
		if parts[i] != "" {
			parts[i] = parts[i] + "?"
		}
	}
	return parts
}

func splitSentences(text string) []string {
	parts := strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '.', '?', ';', '\n', '\r':
			return true
		default:
			return false
		}
	})
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func firstSentence(text string) string {
	for _, sep := range []string{". ", ";", "?", "!", ","} {
		if idx := strings.Index(text, sep); idx >= 0 {
			text = text[:idx]
			break
		}
	}
	return strings.Trim(strings.TrimSpace(text), `"'`)
}

func toSnakeCase(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range raw {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastUnderscore = false
			continue
		}
		if r == ' ' || r == '-' || r == '_' {
			if !lastUnderscore && b.Len() > 0 {
				b.WriteRune('_')
				lastUnderscore = true
			}
			continue
		}
	}

	return strings.Trim(b.String(), "_")
}

func normalizePreferenceTopic(rawText string) string {
	matched := firstMatchAfterPattern(rawText, []string{
		"i prefer",
		"i'd prefer",
		"i always",
		"i never",
		"i don't like",
		"i dislike",
		"i like",
	})
	if strings.TrimSpace(matched) != "" {
		return matched
	}
	// Deterministic extractors may already pass the preference value without
	// the leading phrase ("short responses" instead of "I prefer short responses").
	return strings.Trim(strings.TrimSpace(rawText), `"'`)
}

func normalizeGlossaryTerm(rawText string) string {
	term := firstMatchAfterPattern(rawText, []string{
		"term:",
		"define ",
		"i mean ",
		"we call it ",
		"let's call ",
	})
	if term == "" {
		return ""
	}
	return toSnakeCase(term)
}

func normalizePolicyName(rawText string) string {
	policy := firstMatchAfterPattern(rawText, []string{
		"policy:",
		"the policy is",
		"policy is",
	})
	if policy == "" {
		return ""
	}
	return toSnakeCase(policy)
}

// nextMonotonicIndex returns the next counter value for monotonic entry types
// (decision, open_question) by querying the database for the current max.
// keyPrefix is the prefix used in the key column (e.g., "dec:" or "q:").
func nextMonotonicIndex(ctx context.Context, tx *sql.Tx, convID, entryType, keyPrefix string) int {
	var maxKey sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT MAX(key)
		FROM companion_hard_state_entries
		WHERE conversation_id = $1 AND entry_type = $2 AND key LIKE $3
	`, convID, entryType, keyPrefix+"%").Scan(&maxKey); err != nil || !maxKey.Valid {
		return 1
	}
	suffix := strings.TrimPrefix(maxKey.String, keyPrefix)
	var num int
	if _, err := fmt.Sscanf(suffix, "%d", &num); err != nil {
		return 1
	}
	return num + 1
}

func topicSignature(topicSig string, content string) string {
	set := parseTopicSig(topicSig)
	for _, token := range extractTopicTokens(content, episodeTopicTokenLimit) {
		set[token] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for token := range set {
		keys = append(keys, token)
	}
	sort.Strings(keys)
	if len(keys) > episodeTopicTokenLimit {
		keys = keys[:episodeTopicTokenLimit]
	}
	return strings.Join(keys, ",")
}

func parseTopicSig(raw string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, tok := range strings.Split(raw, ",") {
		t := strings.ToLower(strings.TrimSpace(tok))
		if t != "" {
			set[t] = struct{}{}
		}
	}
	return set
}

func extractTopicTokenSet(text string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, tok := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-')
	}) {
		t := strings.Trim(tok, "_-")
		if t == "" {
			continue
		}
		if len(t) < 3 {
			continue
		}
		if isStopWord(t) {
			continue
		}
		set[t] = struct{}{}
	}
	return set
}

func extractTopicTokens(text string, max int) []string {
	set := extractTopicTokenSet(text)
	out := make([]string, 0, len(set))
	for tok := range set {
		out = append(out, tok)
	}
	sort.Strings(out)
	if max > 0 && len(out) > max {
		out = out[:max]
	}
	return out
}

func isStopWord(token string) bool {
	switch token {
	case "the", "and", "for", "you", "are", "but", "that", "with", "this", "from", "have", "has", "had", "was", "were", "will", "just", "about", "into", "they", "there", "their", "them", "then", "than", "what", "when", "where", "which", "would", "could", "should", "did", "does", "your", "youre", "we", "im", "it", "its", "or", "if", "at", "by", "be", "to", "in", "on", "as", "not", "been", "an", "a", "of":
		return true
	default:
		return false
	}
}
