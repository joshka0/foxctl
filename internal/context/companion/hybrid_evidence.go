package companion

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/rs/zerolog"
)

const evidenceTimeFormat = "2006-01-02 15:04:05.000000"

// InsertEvidenceSnippet stores an extractive evidence snippet with dedup.
//
// Computes content_hash = hash(sourceEventID || normalizedText) to prevent
// cross-event collapse. If the redact step drops the snippet as too noisy, it returns 0.
func (m *ConversationMemory) InsertEvidenceSnippet(ctx context.Context, tx *sql.Tx, snippet *EvidenceSnippet) (int64, error) {
	if snippet == nil {
		return 0, fmt.Errorf("snippet is required")
	}
	if strings.TrimSpace(snippet.ConversationID) == "" {
		return 0, fmt.Errorf("conversation_id is required")
	}
	if snippet.SourceEventID <= 0 {
		return 0, fmt.Errorf("source_event_id must be positive")
	}

	redactedText, keep := redactEvidence(snippet.FactText)
	if !keep {
		zerolog.Ctx(ctx).Warn().
			Str("conversation_id", snippet.ConversationID).
			Int64("source_event_id", snippet.SourceEventID).
			Msg("dropping evidence snippet after redaction gate")
		return 0, nil
	}
	snippet.FactText = redactedText

	text := strings.TrimSpace(snippet.FactText)
	if text == "" {
		return 0, fmt.Errorf("fact_text is required")
	}
	snippet.FactText = text
	snippet.ContentHash = computeEvidenceContentHash(snippet.SourceEventID, snippet.FactText)
	if snippet.Confidence <= 0 {
		snippet.Confidence = 0.5
	}
	if snippet.Bucket == "" {
		snippet.Bucket = "default"
	}

	now := m.nowUTC()
	createdAt := now.Format(evidenceTimeFormat)

	var ttl any
	var expiresAt any
	if snippet.TTLDays != nil && *snippet.TTLDays > 0 {
		ttl = *snippet.TTLDays
		expiresAt = now.AddDate(0, 0, *snippet.TTLDays).Format(evidenceTimeFormat)
	}

	row := m.queryRowWithTx(
		ctx,
		tx,
		`
		INSERT OR IGNORE INTO companion_evidence_snippets (
			conversation_id, source_event_id, event_type, fact_text,
			content_hash, confidence, bucket, ttl_days, created_at, expires_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
		RETURNING id
		`,
		snippet.ConversationID,
		snippet.SourceEventID,
		snippet.EventType,
		snippet.FactText,
		snippet.ContentHash,
		snippet.Confidence,
		snippet.Bucket,
		ttl,
		createdAt,
		expiresAt,
	)
	var id int64
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("insert evidence snippet: %w", err)
	}

	snippet.ID = id
	return id, nil
}

// redactEvidence strips sensitive content from evidence text before storage.
// Returns (redacted text, keep) where keep=false when redaction exceeds 50%.
func redactEvidence(text string) (string, bool) {
	if text == "" {
		return "", false
	}

	redacted := redactPatterns(text)
	redacted = redactHighEntropy(redacted)

	if redactionRatio(text, redacted) > 0.5 {
		return "", false
	}

	return redacted, true
}

// redactPatterns applies regex-based redactors for known sensitive patterns.
func redactPatterns(text string) string {
	redacted := reAPIKeySk.ReplaceAllString(text, "[REDACTED:api_key]")
	redacted = reAPIKeyAKIA.ReplaceAllString(redacted, "[REDACTED:api_key]")
	redacted = reAPIKeyXox.ReplaceAllString(redacted, "[REDACTED:api_key]")
	redacted = reBearerToken.ReplaceAllString(redacted, "[REDACTED:bearer_token]")
	redacted = reEmail.ReplaceAllString(redacted, "[REDACTED:email]")
	redacted = reURLCredentials.ReplaceAllString(redacted, "[REDACTED:url_with_credentials]")
	return redacted
}

// redactHighEntropy redacts base64/hex token-like values longer than 32 chars.
func redactHighEntropy(text string) string {
	redactFn := func(token string) string {
		clean := strings.Trim(token, ".,;:!?()[]{}<>\"'")
		if len(clean) <= 32 {
			return token
		}
		if isHexString(clean) && shannonEntropy(clean) > 3.8 {
			return "[REDACTED:high_entropy_token]"
		}
		if isBase64String(clean) && shannonEntropy(clean) > 3.8 {
			return "[REDACTED:high_entropy_token]"
		}
		return token
	}

	redacted := reHexLong.ReplaceAllStringFunc(text, redactFn)
	return reBase64Long.ReplaceAllStringFunc(redacted, redactFn)
}

// redactionRatio computes what fraction of the original text was replaced.
func redactionRatio(original, redacted string) float64 {
	orig := []rune(original)
	red := []rune(redacted)
	if len(orig) == 0 {
		return 0
	}

	lcs := lcsLen(orig, red)
	if lcs >= len(orig) {
		return 0
	}

	ratio := 1 - float64(lcs)/float64(len(orig))
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}

// SearchEvidenceFTS searches evidence snippets using FTS5 bm25() ranking.
func (m *ConversationMemory) SearchEvidenceFTS(ctx context.Context, convID, query string, limit int) ([]EvidenceSnippet, error) {
	if strings.TrimSpace(convID) == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}
	if strings.TrimSpace(query) == "" || limit <= 0 {
		return []EvidenceSnippet{}, nil
	}

	rows, err := m.db.QueryContext(ctx, `
		SELECT es.id, es.conversation_id, es.source_event_id, es.event_type, es.fact_text,
		       es.confidence, es.bucket, es.created_at,
		       bm25(companion_evidence_fts) AS rank
		FROM companion_evidence_fts fts
		JOIN companion_evidence_snippets es ON es.id = fts.rowid
		WHERE companion_evidence_fts MATCH $1 AND fts.conversation_id = $2
		ORDER BY rank
		LIMIT $3
	`, query, convID, limit)
	if err != nil {
		return nil, fmt.Errorf("search evidence fts: %w", err)
	}
	defer rows.Close()

	var snippets []EvidenceSnippet
	for rows.Next() {
		var snippet EvidenceSnippet
		var rank float64
		if err := rows.Scan(
			&snippet.ID,
			&snippet.ConversationID,
			&snippet.SourceEventID,
			&snippet.EventType,
			&snippet.FactText,
			&snippet.Confidence,
			&snippet.Bucket,
			&snippet.CreatedAt,
			&rank,
		); err != nil {
			continue
		}
		snippets = append(snippets, snippet)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan evidence snippets: %w", err)
	}

	return snippets, nil
}

// InsertAssumption adds a new assumption to the ledger.
func (m *ConversationMemory) InsertAssumption(ctx context.Context, tx *sql.Tx, assumption *Assumption) (int64, error) {
	if assumption == nil {
		return 0, fmt.Errorf("assumption is required")
	}
	if strings.TrimSpace(assumption.ConversationID) == "" {
		return 0, fmt.Errorf("conversation_id is required")
	}
	if strings.TrimSpace(assumption.Assumption) == "" {
		return 0, fmt.Errorf("assumption text is required")
	}
	if assumption.SourceEventID <= 0 {
		return 0, fmt.Errorf("source_event_id must be positive")
	}
	if assumption.Status == "" {
		assumption.Status = "active"
	}
	if assumption.Confidence <= 0 {
		assumption.Confidence = 0.5
	}

	row := m.queryRowWithTx(ctx, tx, `
		INSERT INTO companion_assumptions_ledger (
			conversation_id, assumption, status, reason, source_event_id, confidence, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, CURRENT_TIMESTAMP
		)
		RETURNING id
	`, assumption.ConversationID, assumption.Assumption, assumption.Status, assumption.Reason, assumption.SourceEventID, assumption.Confidence)
	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, fmt.Errorf("insert assumption: %w", err)
	}

	assumption.ID = id
	return id, nil
}

// RetractAssumption retracts an active assumption with a reason.
func (m *ConversationMemory) RetractAssumption(ctx context.Context, tx *sql.Tx, id int64, retractedByEventID int64, reason string) error {
	if id <= 0 {
		return fmt.Errorf("assumption id must be positive")
	}

	result, err := m.execWithTx(ctx, tx, `
		UPDATE companion_assumptions_ledger
		SET status = 'retracted',
			retracted_at = CURRENT_TIMESTAMP,
			retracted_by_event_id = $2,
			retraction_reason = $3
		WHERE id = $1
	`, id, retractedByEventID, reason)
	if err != nil {
		return fmt.Errorf("retract assumption: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("retract assumption rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("assumption not found: %d", id)
	}

	return nil
}

// GetActiveAssumptions returns all active assumptions for a conversation.
func (m *ConversationMemory) GetActiveAssumptions(ctx context.Context, convID string) ([]Assumption, error) {
	if strings.TrimSpace(convID) == "" {
		return nil, fmt.Errorf("conversation_id is required")
	}

	rows, err := m.db.QueryContext(ctx, `
		SELECT id, conversation_id, assumption, status, reason, source_event_id, confidence
		FROM companion_assumptions_ledger
		WHERE conversation_id = $1 AND status = 'active'
		ORDER BY id DESC
	`, convID)
	if err != nil {
		return nil, fmt.Errorf("get active assumptions: %w", err)
	}
	defer rows.Close()

	var assumptions []Assumption
	for rows.Next() {
		var a Assumption
		if err := rows.Scan(&a.ID, &a.ConversationID, &a.Assumption, &a.Status, &a.Reason, &a.SourceEventID, &a.Confidence); err != nil {
			continue
		}
		assumptions = append(assumptions, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan assumptions: %w", err)
	}
	return assumptions, nil
}

// PromoteAssumption promotes an assumption to hard state with audit metadata.
func (m *ConversationMemory) PromoteAssumption(ctx context.Context, tx *sql.Tx, assumptionID int64, promotedBy string, sourceEventID int64) error {
	if assumptionID <= 0 {
		return fmt.Errorf("assumption id must be positive")
	}
	if sourceEventID <= 0 {
		return fmt.Errorf("source_event_id must be positive")
	}

	var convID, assumptionText string
	var confidence float64
	if err := m.queryRowWithTx(ctx, tx, `
		SELECT conversation_id, assumption, confidence
		FROM companion_assumptions_ledger
		WHERE id = $1 AND status = 'active'
	`, assumptionID).Scan(&convID, &assumptionText, &confidence); err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("assumption not found or not active: %d", assumptionID)
		}
		return fmt.Errorf("load assumption: %w", err)
	}

	result, err := m.execWithTx(ctx, tx, `
		UPDATE companion_assumptions_ledger
		SET status = 'promoted'
		WHERE id = $1 AND status = 'active'
	`, assumptionID)
	if err != nil {
		return fmt.Errorf("promote assumption: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("promote assumption rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("assumption not found or already promoted: %d", assumptionID)
	}

	metadata, err := json.Marshal(map[string]any{
		"promoted_by":            promotedBy,
		"original_assumption_id": assumptionID,
		"original_confidence":    confidence,
		"promotion_event_id":     sourceEventID,
	})
	if err != nil {
		return fmt.Errorf("marshal promotion metadata: %w", err)
	}

	valueJSON, err := json.Marshal(map[string]any{
		"assumption": assumptionText,
	})
	if err != nil {
		return fmt.Errorf("marshal assumption value: %w", err)
	}

	_, err = m.AppendHardStateEntry(ctx, tx, &HardStateEntry{
		ConversationID: convID,
		EntryType:      "assumption",
		Key:            fmt.Sprintf("assumption:%d", assumptionID),
		ValueJSON:      string(valueJSON),
		Status:         "active",
		SourceEventID:  sourceEventID,
		Confidence:     confidence,
		MetadataJSON:   strPtr(string(metadata)),
	})
	if err != nil {
		return fmt.Errorf("insert promoted hard state entry: %w", err)
	}

	return nil
}

// AppendHardStateEntry inserts a new immutable hard state entry.
func (m *ConversationMemory) AppendHardStateEntry(ctx context.Context, tx *sql.Tx, entry *HardStateEntry) (int64, error) {
	if entry == nil {
		return 0, fmt.Errorf("hard state entry is required")
	}
	if strings.TrimSpace(entry.ConversationID) == "" {
		return 0, fmt.Errorf("conversation_id is required")
	}
	if strings.TrimSpace(entry.EntryType) == "" {
		return 0, fmt.Errorf("entry_type is required")
	}
	if strings.TrimSpace(entry.Key) == "" {
		return 0, fmt.Errorf("key is required")
	}
	if strings.TrimSpace(entry.ValueJSON) == "" {
		return 0, fmt.Errorf("value_json is required")
	}
	if entry.SourceEventID <= 0 {
		return 0, fmt.Errorf("source_event_id must be positive")
	}
	if entry.Confidence <= 0 {
		entry.Confidence = 0.8
	}
	if entry.Status == "" {
		entry.Status = "active"
	}

	var supersedes any
	if entry.Supersedes != nil && *entry.Supersedes > 0 {
		supersedes = *entry.Supersedes
	}

	row := m.queryRowWithTx(ctx, tx, `
		INSERT INTO companion_hard_state_entries (
			conversation_id, entry_type, key, value_json, status, source_event_id,
			confidence, metadata_json, supersedes, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, CURRENT_TIMESTAMP
		)
		RETURNING id
	`, entry.ConversationID, entry.EntryType, entry.Key, entry.ValueJSON, entry.Status,
		entry.SourceEventID, entry.Confidence, entry.MetadataJSON, supersedes)

	var id int64
	if err := row.Scan(&id); err != nil {
		return 0, fmt.Errorf("insert hard state entry: %w", err)
	}

	entry.ID = id
	return id, nil
}

// SupersedeHardStateEntry creates a new entry that supersedes an existing one.
func (m *ConversationMemory) SupersedeHardStateEntry(ctx context.Context, tx *sql.Tx, oldEntryID int64, newEntry *HardStateEntry) (int64, error) {
	if oldEntryID <= 0 {
		return 0, fmt.Errorf("old entry id must be positive")
	}
	if newEntry == nil {
		return 0, fmt.Errorf("hard state entry is required")
	}
	newEntry.Supersedes = int64Ptr(oldEntryID)
	return m.AppendHardStateEntry(ctx, tx, newEntry)
}

// RetractHardStateEntry creates a retraction entry for an existing key.
func (m *ConversationMemory) RetractHardStateEntry(ctx context.Context, tx *sql.Tx, oldEntryID int64, sourceEventID int64, reason string) (int64, error) {
	if oldEntryID <= 0 {
		return 0, fmt.Errorf("old entry id must be positive")
	}
	if sourceEventID <= 0 {
		return 0, fmt.Errorf("source_event_id must be positive")
	}

	var convID, entryType, key string
	var confidence float64
	var oldMetadata sql.NullString
	if err := m.queryRowWithTx(ctx, tx, `
		SELECT conversation_id, entry_type, key, confidence, metadata_json
		FROM companion_hard_state_entries
		WHERE id = $1
	`, oldEntryID).Scan(&convID, &entryType, &key, &confidence, &oldMetadata); err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("hard state entry not found: %d", oldEntryID)
		}
		return 0, fmt.Errorf("load hard state entry: %w", err)
	}

	metadata := map[string]any{}
	if oldMetadata.Valid && oldMetadata.String != "" {
		_ = json.Unmarshal([]byte(oldMetadata.String), &metadata)
	}
	metadata["retracted"] = true
	metadata["retracted_by_event_id"] = sourceEventID
	metadata["retraction_reason"] = reason
	metadata["superseded_entry_id"] = oldEntryID

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return 0, fmt.Errorf("marshal retraction metadata: %w", err)
	}

	retractedEntry := &HardStateEntry{
		ConversationID: convID,
		EntryType:      entryType,
		Key:            key,
		ValueJSON:      "null",
		Status:         "retracted",
		SourceEventID:  sourceEventID,
		Confidence:     confidence,
		MetadataJSON:   strPtr(string(metadataJSON)),
		Supersedes:     int64Ptr(oldEntryID),
	}

	return m.AppendHardStateEntry(ctx, tx, retractedEntry)
}

// DeleteExpiredEvidence deletes evidence snippets past their TTL.
func (m *ConversationMemory) DeleteExpiredEvidence(ctx context.Context) (int64, error) {
	result, err := m.db.ExecContext(ctx, `
		DELETE FROM companion_evidence_snippets
		WHERE expires_at IS NOT NULL AND expires_at < CURRENT_TIMESTAMP
	`)
	if err != nil {
		return 0, fmt.Errorf("delete expired evidence: %w", err)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return removed, nil
}

func computeEvidenceContentHash(sourceEventID int64, normalizedText string) string {
	payload := fmt.Sprintf("%d|%s", sourceEventID, normalizeEvidenceText(normalizedText))
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

func normalizeEvidenceText(text string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(text)))
	return strings.Join(fields, " ")
}

func lcsLen(a, b []rune) int {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	dp := make([]int, len(b)+1)
	for i := 0; i < len(a); i++ {
		prevDiag := 0
		for j := 0; j < len(b); j++ {
			tmp := dp[j+1]
			if a[i] == b[j] {
				dp[j+1] = prevDiag + 1
			} else if dp[j] > dp[j+1] {
				dp[j+1] = dp[j]
			}
			prevDiag = tmp
		}
	}
	return dp[len(b)]
}

func isHexString(token string) bool {
	return reHexLong.MatchString(token)
}

func isBase64String(token string) bool {
	return reBase64Long.MatchString(token)
}

func shannonEntropy(value string) float64 {
	if value == "" {
		return 0
	}
	freq := make(map[rune]int, len(value))
	for _, r := range value {
		freq[unicode.ToLower(r)]++
	}

	total := float64(len([]rune(value)))
	if total == 0 {
		return 0
	}

	var entropy float64
	for _, count := range freq {
		prob := float64(count) / total
		if prob <= 0 {
			continue
		}
		entropy -= prob * (math.Log(prob) / math.Log(2))
	}
	return entropy
}

func (m *ConversationMemory) nowUTC() time.Time {
	return m.clock.Now().UTC()
}

func (m *ConversationMemory) queryRowWithTx(ctx context.Context, tx *sql.Tx, query string, args ...any) *sql.Row {
	if tx != nil {
		return tx.QueryRowContext(ctx, query, args...)
	}
	return m.db.QueryRowContext(ctx, query, args...)
}

func (m *ConversationMemory) execWithTx(ctx context.Context, tx *sql.Tx, query string, args ...any) (sql.Result, error) {
	if tx != nil {
		return tx.ExecContext(ctx, query, args...)
	}
	return m.db.ExecContext(ctx, query, args...)
}

var (
	reAPIKeySk       = regexp.MustCompile(`(?i)\bsk-[a-z0-9]{20,}\b`)
	reAPIKeyAKIA     = regexp.MustCompile(`\bAKIA[0-9A-Z]{16,}\b`)
	reAPIKeyXox      = regexp.MustCompile(`\bxox-[a-z0-9_-]{10,}\b`)
	reBearerToken    = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9\-._~+/=]{16,}\b`)
	reEmail          = regexp.MustCompile(`(?i)\b[0-9a-z._%+\-]+@[0-9a-z.\-]+\.[a-z]{2,}\b`)
	reURLCredentials = regexp.MustCompile(`(?i)\b(?:[a-z][a-z0-9+\-.]*://[^/\s:@]+:[^/\s@]+@[^/\s]+)`)
	reHexLong        = regexp.MustCompile(`(?i)\b[0-9a-f]{33,}\b`)
	reBase64Long     = regexp.MustCompile(`(?i)\b[a-z0-9+\-_]{33,}={0,2}\b`)
)
