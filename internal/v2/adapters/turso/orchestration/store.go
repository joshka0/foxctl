package orchestration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/joshka0/foxctl/internal/storage/sqlutil"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	coreorchestration "github.com/joshka0/foxctl/internal/v2/core/orchestration"
)

const (
	defaultBoardLimit = 50
	maxBoardLimit     = 200
)

// ErrNotFound indicates missing orchestration projection rows.
var ErrNotFound = errors.New("v2 orchestration: not found")

// StoreOptions configures orchestration projection behavior.
type StoreOptions struct {
	LaneOptions coreorchestration.LaneOptions
}

// Store materializes orchestration cards and serves board/card reads.
type Store struct {
	db          *sql.DB
	now         func() time.Time
	laneOptions coreorchestration.LaneOptions
}

// NewStore creates a new orchestration projection store.
func NewStore(db *sql.DB, opts StoreOptions) *Store {
	return &Store{
		db:          db,
		now:         func() time.Time { return time.Now().UTC() },
		laneOptions: opts.LaneOptions,
	}
}

// SetNowForTest overrides clock for deterministic tests.
func (s *Store) SetNowForTest(now func() time.Time) {
	if s == nil || now == nil {
		return
	}
	s.now = now
}

// Apply materializes one event into orchestration card projections.
func (s *Store) Apply(ctx context.Context, evt coreevents.Event) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("v2 orchestration apply: nil store")
	}
	issueID, payload := extractIssueID(evt)
	if issueID == "" {
		return nil
	}
	scopeID := scopeIDForEvent(evt, payload, issueID)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin orchestration tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	appliedAt := sqlutil.FormatTimestamp(s.now().UTC())
	res, err := tx.ExecContext(
		ctx, `
		INSERT OR IGNORE INTO v2_orchestration_applied_events (
			event_id, command, scope_id, request_id, applied_at
		) VALUES ($1, $2, $3, $4, $5)
	`,
		strings.TrimSpace(evt.ID),
		strings.TrimSpace(evt.Command),
		scopeID,
		strings.TrimSpace(evt.RequestID),
		appliedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert applied event: %w", err)
	}
	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return nil
	}

	card := s.cardFromEvent(evt, payload)
	if card.Lane == "" {
		card.Lane = coreorchestration.DeriveLane(card, s.laneOptions)
	}

	var retryDueAt string
	if card.RetryDueAt != nil {
		retryDueAt = sqlutil.FormatTimestamp(card.RetryDueAt.UTC())
	}
	var lastEventAt string
	if card.LastEventAt != nil {
		lastEventAt = sqlutil.FormatTimestamp(card.LastEventAt.UTC())
	}

	_, err = tx.ExecContext(
		ctx, `
		INSERT INTO v2_orchestration_cards (
			issue_id, workspace_id, issue_identifier, title, state, lane, tracker_state, policy_status,
			last_outcome, eligibility, denial_reason, suggestion, run_id, agent_id, actor_id, attempt,
			retry_due_at, last_event_type, last_event_at, last_request_id, last_event_id,
			last_stream_version, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21,
			$22, $23
		)
		ON CONFLICT(issue_id) DO UPDATE SET
			workspace_id = CASE WHEN excluded.workspace_id <> '' THEN excluded.workspace_id ELSE v2_orchestration_cards.workspace_id END,
			issue_identifier = CASE WHEN excluded.issue_identifier <> '' THEN excluded.issue_identifier ELSE v2_orchestration_cards.issue_identifier END,
			title = CASE WHEN excluded.title <> '' THEN excluded.title ELSE v2_orchestration_cards.title END,
			state = excluded.state,
			lane = excluded.lane,
			tracker_state = CASE WHEN excluded.tracker_state <> '' THEN excluded.tracker_state ELSE v2_orchestration_cards.tracker_state END,
			policy_status = CASE WHEN excluded.policy_status <> '' THEN excluded.policy_status ELSE v2_orchestration_cards.policy_status END,
			last_outcome = CASE WHEN excluded.last_outcome <> '' THEN excluded.last_outcome ELSE v2_orchestration_cards.last_outcome END,
			eligibility = CASE WHEN excluded.eligibility <> '' THEN excluded.eligibility ELSE v2_orchestration_cards.eligibility END,
			denial_reason = CASE WHEN excluded.denial_reason <> '' THEN excluded.denial_reason ELSE v2_orchestration_cards.denial_reason END,
			suggestion = CASE WHEN excluded.suggestion <> '' THEN excluded.suggestion ELSE v2_orchestration_cards.suggestion END,
			run_id = CASE WHEN excluded.run_id <> '' THEN excluded.run_id ELSE v2_orchestration_cards.run_id END,
			agent_id = CASE WHEN excluded.agent_id <> '' THEN excluded.agent_id ELSE v2_orchestration_cards.agent_id END,
			actor_id = CASE WHEN excluded.actor_id <> '' THEN excluded.actor_id ELSE v2_orchestration_cards.actor_id END,
			attempt = CASE WHEN excluded.attempt > 0 THEN excluded.attempt ELSE v2_orchestration_cards.attempt END,
			retry_due_at = CASE WHEN excluded.retry_due_at <> '' THEN excluded.retry_due_at ELSE v2_orchestration_cards.retry_due_at END,
			last_event_type = excluded.last_event_type,
			last_event_at = excluded.last_event_at,
			last_request_id = CASE WHEN excluded.last_request_id <> '' THEN excluded.last_request_id ELSE v2_orchestration_cards.last_request_id END,
			last_event_id = excluded.last_event_id,
			last_stream_version = excluded.last_stream_version,
			updated_at = excluded.updated_at
	`,
		card.IssueID,
		stringFromMap(payload, "workspace_id"),
		card.IssueIdentifier,
		card.Title,
		card.State,
		card.Lane,
		card.TrackerState,
		card.PolicyStatus,
		card.LastOutcome,
		card.Eligibility,
		card.DenialReason,
		card.Suggestion,
		card.RunID,
		card.AgentID,
		card.ActorID,
		card.Attempt,
		retryDueAt,
		card.LastEvent,
		lastEventAt,
		strings.TrimSpace(evt.RequestID),
		strings.TrimSpace(evt.ID),
		evt.StreamVersion,
		appliedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert orchestration card: %w", err)
	}
	if err := clearExplicitCardFields(ctx, tx, card.IssueID, payload); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit orchestration tx: %w", err)
	}
	return nil
}

// Board returns bounded board columns and counts.
func (s *Store) Board(ctx context.Context, req coreorchestration.BoardRequest) (coreorchestration.BoardResponse, error) {
	if s == nil || s.db == nil {
		return coreorchestration.BoardResponse{}, fmt.Errorf("v2 orchestration board: nil store")
	}
	limit := normalizeLimit(req.Limit)

	args := []any{}
	where := []string{"1=1"}
	if ws := strings.TrimSpace(req.WorkspaceID); ws != "" {
		where = append(where, "workspace_id = ?")
		args = append(args, ws)
	}
	if req.ArchivedOnly {
		where = append(where, "archived_at != ''")
	} else {
		where = append(where, "archived_at = ''")
	}
	if lane := strings.TrimSpace(string(req.Lane)); lane != "" {
		where = append(where, "lane = ?")
		args = append(args, lane)
	}
	if cursor := strings.TrimSpace(req.Cursor); cursor != "" {
		where = append(where, "issue_id > ?")
		args = append(args, cursor)
	}

	query := fmt.Sprintf(`
		SELECT
			COALESCE(workspace_id, ''), issue_id, COALESCE(issue_identifier, ''), COALESCE(title, ''), state, COALESCE(lane, ''),
			COALESCE(tracker_state, ''), COALESCE(policy_status, ''), COALESCE(last_outcome, ''),
			COALESCE(eligibility, ''), COALESCE(denial_reason, ''), COALESCE(suggestion, ''),
			COALESCE(run_id, ''), COALESCE(agent_id, ''), COALESCE(actor_id, ''), COALESCE(attempt, 0),
			COALESCE(retry_due_at, ''), COALESCE(last_event_type, ''), COALESCE(last_event_at, ''), COALESCE(archived_at, '')
		FROM v2_orchestration_cards
		WHERE %s
		ORDER BY issue_id ASC
		LIMIT ?
	`, strings.Join(where, " AND "))
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return coreorchestration.BoardResponse{}, fmt.Errorf("query orchestration board: %w", err)
	}
	defer rows.Close()

	cards := make([]coreorchestration.Card, 0, limit+1)
	for rows.Next() {
		card, scanErr := scanCardRow(rows)
		if scanErr != nil {
			return coreorchestration.BoardResponse{}, scanErr
		}
		if card.Lane == "" {
			card.Lane = coreorchestration.DeriveLane(card, s.laneOptions)
		}
		cards = append(cards, card)
	}
	if err := rows.Err(); err != nil {
		return coreorchestration.BoardResponse{}, fmt.Errorf("iterate orchestration board rows: %w", err)
	}

	nextCursor := ""
	if len(cards) > limit {
		nextCursor = cards[limit-1].IssueID
		cards = cards[:limit]
	}

	counts, err := s.loadCounts(ctx, strings.TrimSpace(req.WorkspaceID), req.ArchivedOnly)
	if err != nil {
		return coreorchestration.BoardResponse{}, err
	}

	grouped := make(map[coreorchestration.Lane][]coreorchestration.Card)
	for _, card := range cards {
		grouped[card.Lane] = append(grouped[card.Lane], card)
	}

	orderedLanes := coreorchestration.LaneOrder()
	if req.Lane != "" {
		orderedLanes = []coreorchestration.Lane{req.Lane}
	}
	columns := make([]coreorchestration.LaneColumn, 0, len(orderedLanes))
	for _, lane := range orderedLanes {
		columns = append(columns, coreorchestration.LaneColumn{
			ID:    lane,
			Title: string(lane),
			Cards: grouped[lane],
		})
	}

	return coreorchestration.BoardResponse{
		GeneratedAt: s.now().UTC(),
		Counts:      counts,
		Lanes:       columns,
		NextCursor:  nextCursor,
	}, nil
}

// ListRunningCards returns projected non-archived cards that are still marked Running.
func (s *Store) ListRunningCards(ctx context.Context, req coreorchestration.RunningCardsRequest) ([]coreorchestration.Card, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("v2 orchestration running cards: nil store")
	}
	limit := normalizeLimit(req.Limit)

	args := []any{string(coreorchestration.StateRunning)}
	where := []string{"state = ?", "archived_at = ''"}
	if ws := strings.TrimSpace(req.WorkspaceID); ws != "" {
		where = append(where, "workspace_id = ?")
		args = append(args, ws)
	}
	if cursor := strings.TrimSpace(req.Cursor); cursor != "" {
		where = append(where, "issue_id > ?")
		args = append(args, cursor)
	}

	query := fmt.Sprintf(`
		SELECT
			COALESCE(workspace_id, ''), issue_id, COALESCE(issue_identifier, ''), COALESCE(title, ''), state, COALESCE(lane, ''),
			COALESCE(tracker_state, ''), COALESCE(policy_status, ''), COALESCE(last_outcome, ''),
			COALESCE(eligibility, ''), COALESCE(denial_reason, ''), COALESCE(suggestion, ''),
			COALESCE(run_id, ''), COALESCE(agent_id, ''), COALESCE(actor_id, ''), COALESCE(attempt, 0),
			COALESCE(retry_due_at, ''), COALESCE(last_event_type, ''), COALESCE(last_event_at, ''), COALESCE(archived_at, '')
		FROM v2_orchestration_cards
		WHERE %s
		ORDER BY issue_id ASC
		LIMIT ?
	`, strings.Join(where, " AND "))
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query running orchestration cards: %w", err)
	}
	defer rows.Close()

	cards := make([]coreorchestration.Card, 0, limit)
	for rows.Next() {
		card, scanErr := scanCardRow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		if card.Lane == "" {
			card.Lane = coreorchestration.DeriveLane(card, s.laneOptions)
		}
		cards = append(cards, card)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate running orchestration cards: %w", err)
	}
	return cards, nil
}

// DeleteCards removes projected orchestration cards for the provided issue ids
// in a workspace. When issueIDs is empty, all cards in the workspace are
// removed. If appliedEventIDs are provided, corresponding replay guards are
// removed as well so the cleanup fully clears projection state.
func (s *Store) DeleteCards(ctx context.Context, workspaceID string, issueIDs []string, appliedEventIDs []string) (deleted int, err error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("v2 orchestration delete cards: nil store")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return 0, fmt.Errorf("v2 orchestration delete cards: workspace_id is required")
	}

	issueSet := make(map[string]struct{}, len(issueIDs))
	for _, issueID := range issueIDs {
		if trimmed := strings.TrimSpace(issueID); trimmed != "" {
			issueSet[trimmed] = struct{}{}
		}
	}

	returnedDeleted := 0
	err = sqlutil.WithTransaction(ctx, s.db, func(tx *sql.Tx) error {
		var (
			res     sql.Result
			execErr error
		)
		if len(issueSet) == 0 {
			res, execErr = tx.ExecContext(ctx, `DELETE FROM v2_orchestration_cards WHERE workspace_id = $1`, workspaceID)
		} else {
			for issueID := range issueSet {
				result, deleteErr := tx.ExecContext(ctx, `DELETE FROM v2_orchestration_cards WHERE workspace_id = $1 AND issue_id = $2`, workspaceID, issueID)
				if deleteErr != nil {
					return fmt.Errorf("delete orchestration card %s: %w", issueID, deleteErr)
				}
				rows, rowsErr := result.RowsAffected()
				if rowsErr == nil {
					returnedDeleted += int(rows)
				}
			}
		}
		if execErr != nil {
			return fmt.Errorf("delete orchestration cards: %w", execErr)
		}
		if res != nil {
			rows, rowsErr := res.RowsAffected()
			if rowsErr == nil {
				returnedDeleted = int(rows)
			}
		}
		for _, eventID := range appliedEventIDs {
			eventID = strings.TrimSpace(eventID)
			if eventID == "" {
				continue
			}
			if _, deleteErr := tx.ExecContext(ctx, `DELETE FROM v2_orchestration_applied_events WHERE event_id = $1`, eventID); deleteErr != nil {
				return fmt.Errorf("delete applied orchestration event %s: %w", eventID, deleteErr)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return returnedDeleted, nil
}

func (s *Store) ArchiveCards(ctx context.Context, workspaceID string, issueIDs []string) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("v2 orchestration archive cards: nil store")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return 0, fmt.Errorf("v2 orchestration archive cards: workspace_id is required")
	}
	now := sqlutil.FormatTimestamp(s.now().UTC())
	return updateArchivedCards(ctx, s.db, workspaceID, issueIDs, now, "archive")
}

func (s *Store) RestoreCards(ctx context.Context, workspaceID string, issueIDs []string) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("v2 orchestration restore cards: nil store")
	}
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return 0, fmt.Errorf("v2 orchestration restore cards: workspace_id is required")
	}
	return updateArchivedCards(ctx, s.db, workspaceID, issueIDs, "", "restore")
}

func updateArchivedCards(ctx context.Context, db *sql.DB, workspaceID string, issueIDs []string, archivedAt, verb string) (int, error) {
	issueSet := make(map[string]struct{}, len(issueIDs))
	for _, issueID := range issueIDs {
		if trimmed := strings.TrimSpace(issueID); trimmed != "" {
			issueSet[trimmed] = struct{}{}
		}
	}
	updated := 0
	err := sqlutil.WithTransaction(ctx, db, func(tx *sql.Tx) error {
		if len(issueSet) == 0 {
			query := `UPDATE v2_orchestration_cards SET archived_at = $1 WHERE workspace_id = $2`
			if archivedAt == "" {
				query += ` AND archived_at != ''`
			} else {
				query += ` AND archived_at = ''`
			}
			res, err := tx.ExecContext(ctx, query, archivedAt, workspaceID)
			if err != nil {
				return fmt.Errorf("%s orchestration cards: %w", verb, err)
			}
			rows, _ := res.RowsAffected()
			updated = int(rows)
			return nil
		}
		for issueID := range issueSet {
			query := `UPDATE v2_orchestration_cards SET archived_at = $1 WHERE workspace_id = $2 AND issue_id = $3`
			if archivedAt == "" {
				query += ` AND archived_at != ''`
			} else {
				query += ` AND archived_at = ''`
			}
			res, err := tx.ExecContext(ctx, query, archivedAt, workspaceID, issueID)
			if err != nil {
				return fmt.Errorf("%s orchestration card %s: %w", verb, issueID, err)
			}
			rows, _ := res.RowsAffected()
			updated += int(rows)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return updated, nil
}

// Card returns one projected card by issue id.
func (s *Store) Card(ctx context.Context, req coreorchestration.CardRequest) (coreorchestration.CardResponse, error) {
	if s == nil || s.db == nil {
		return coreorchestration.CardResponse{}, fmt.Errorf("v2 orchestration card: nil store")
	}
	issueID := strings.TrimSpace(req.IssueID)
	if issueID == "" {
		return coreorchestration.CardResponse{}, ErrNotFound
	}

	args := []any{issueID}
	query := `
		SELECT
			COALESCE(workspace_id, ''), issue_id, COALESCE(issue_identifier, ''), COALESCE(title, ''), state, COALESCE(lane, ''),
			COALESCE(tracker_state, ''), COALESCE(policy_status, ''), COALESCE(last_outcome, ''),
			COALESCE(eligibility, ''), COALESCE(denial_reason, ''), COALESCE(suggestion, ''),
			COALESCE(run_id, ''), COALESCE(agent_id, ''), COALESCE(actor_id, ''), COALESCE(attempt, 0),
			COALESCE(retry_due_at, ''), COALESCE(last_event_type, ''), COALESCE(last_event_at, ''), COALESCE(archived_at, '')
		FROM v2_orchestration_cards
		WHERE issue_id = ?
	`
	if ws := strings.TrimSpace(req.WorkspaceID); ws != "" {
		query += " AND workspace_id = ?"
		args = append(args, ws)
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	card, err := scanCardSingle(row)
	if errors.Is(err, sql.ErrNoRows) {
		return coreorchestration.CardResponse{}, ErrNotFound
	}
	if err != nil {
		return coreorchestration.CardResponse{}, err
	}
	if card.Lane == "" {
		card.Lane = coreorchestration.DeriveLane(card, s.laneOptions)
	}
	return coreorchestration.CardResponse{Card: card}, nil
}

func (s *Store) loadCounts(ctx context.Context, workspaceID string, archivedOnly bool) (map[coreorchestration.Lane]int, error) {
	counts := coreorchestration.EnsureLaneCounts(nil)

	where := "1=1"
	args := []any{}
	if workspaceID != "" {
		where = "workspace_id = ?"
		args = append(args, workspaceID)
	}
	if archivedOnly {
		where += " AND archived_at != ''"
	} else {
		where += " AND archived_at = ''"
	}
	query := fmt.Sprintf(`
		SELECT COALESCE(lane, ''), COUNT(*)
		FROM v2_orchestration_cards
		WHERE %s
		GROUP BY lane
	`, where)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query orchestration lane counts: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var lane string
		var count int
		if err := rows.Scan(&lane, &count); err != nil {
			return nil, fmt.Errorf("scan orchestration lane count: %w", err)
		}
		counts[coreorchestration.Lane(strings.TrimSpace(lane))] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orchestration lane counts: %w", err)
	}
	return coreorchestration.EnsureLaneCounts(counts), nil
}

func (s *Store) cardFromEvent(evt coreevents.Event, payload map[string]any) coreorchestration.Card {
	now := s.now().UTC()
	card := coreorchestration.Card{
		WorkspaceID:     strings.TrimSpace(stringFromMap(payload, "workspace_id")),
		IssueID:         strings.TrimSpace(stringFromMap(payload, "issue_id")),
		IssueIdentifier: strings.TrimSpace(stringFromMap(payload, "issue_identifier")),
		Title:           strings.TrimSpace(stringFromMap(payload, "title")),
		State:           parseState(stringFromMap(payload, "state"), evt),
		Lane:            "",
		TrackerState:    strings.TrimSpace(stringFromMap(payload, "tracker_state")),
		PolicyStatus:    coreorchestration.PolicyStatus(strings.TrimSpace(stringFromMap(payload, "policy_status"))),
		LastOutcome:     coreorchestration.Outcome(strings.TrimSpace(stringFromMap(payload, "last_outcome"))),
		Eligibility:     coreorchestration.Eligibility(strings.TrimSpace(stringFromMap(payload, "eligibility"))),
		DenialReason:    strings.TrimSpace(stringFromMap(payload, "denial_reason")),
		Suggestion:      strings.TrimSpace(stringFromMap(payload, "suggestion")),
		RunID:           strings.TrimSpace(stringFromMap(payload, "run_id")),
		AgentID:         strings.TrimSpace(stringFromMap(payload, "agent_id")),
		ActorID:         strings.TrimSpace(stringFromMap(payload, "actor_id")),
		Attempt:         intFromMap(payload, "attempt"),
		LastEvent:       string(evt.EventType),
		LastEventAt:     eventTimestamp(evt, now),
	}
	if card.RunID == "" && evt.StreamType == coreevents.StreamTypeRun {
		card.RunID = strings.TrimSpace(evt.StreamID)
	}
	if card.ActorID == "" {
		card.ActorID = strings.TrimSpace(evt.ActorID)
	}
	if due := timeFromMap(payload, "retry_due_at"); due != nil {
		card.RetryDueAt = due
	}
	return card
}

func parseState(raw string, evt coreevents.Event) coreorchestration.State {
	normalized := strings.TrimSpace(raw)
	if normalized != "" {
		return coreorchestration.State(normalized)
	}
	switch evt.EventType {
	case coreevents.EventRunStarted:
		return coreorchestration.StateRunning
	case coreevents.EventRunCompleted, coreevents.EventRunFailed:
		return coreorchestration.StateReleased
	default:
		return coreorchestration.StateUnclaimed
	}
}

func extractIssueID(evt coreevents.Event) (string, map[string]any) {
	payload := map[string]any{}
	if len(evt.Payload) > 0 {
		_ = json.Unmarshal(evt.Payload, &payload)
	}
	issueID := stringFromMap(payload, "issue_id")
	return strings.TrimSpace(issueID), payload
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return defaultBoardLimit
	}
	if limit > maxBoardLimit {
		return maxBoardLimit
	}
	return limit
}

type cardScanner interface {
	Scan(dest ...any) error
}

func scanCardSingle(row cardScanner) (coreorchestration.Card, error) {
	return scanCardFrom(row)
}

func scanCardRow(rows *sql.Rows) (coreorchestration.Card, error) {
	return scanCardFrom(rows)
}

func scanCardFrom(scanner cardScanner) (coreorchestration.Card, error) {
	var (
		card        coreorchestration.Card
		state       string
		lane        string
		retryDueAt  string
		lastEventAt string
		archivedAt  string
	)
	err := scanner.Scan(
		&card.WorkspaceID,
		&card.IssueID,
		&card.IssueIdentifier,
		&card.Title,
		&state,
		&lane,
		&card.TrackerState,
		&card.PolicyStatus,
		&card.LastOutcome,
		&card.Eligibility,
		&card.DenialReason,
		&card.Suggestion,
		&card.RunID,
		&card.AgentID,
		&card.ActorID,
		&card.Attempt,
		&retryDueAt,
		&card.LastEvent,
		&lastEventAt,
		&archivedAt,
	)
	if err != nil {
		return coreorchestration.Card{}, err
	}
	card.State = coreorchestration.State(strings.TrimSpace(state))
	card.Lane = coreorchestration.Lane(strings.TrimSpace(lane))
	if strings.TrimSpace(retryDueAt) != "" {
		parsed, err := sqlutil.ScanTimestamp(retryDueAt)
		if err != nil {
			return coreorchestration.Card{}, fmt.Errorf("parse orchestration retry_due_at: %w", err)
		}
		card.RetryDueAt = &parsed
	}
	if strings.TrimSpace(lastEventAt) != "" {
		parsed, err := sqlutil.ScanTimestamp(lastEventAt)
		if err != nil {
			return coreorchestration.Card{}, fmt.Errorf("parse orchestration last_event_at: %w", err)
		}
		card.LastEventAt = &parsed
	}
	if strings.TrimSpace(archivedAt) != "" {
		parsed, err := sqlutil.ScanTimestamp(archivedAt)
		if err != nil {
			return coreorchestration.Card{}, fmt.Errorf("parse orchestration archived_at: %w", err)
		}
		card.ArchivedAt = &parsed
	}
	return card, nil
}

func stringFromMap(m map[string]any, key string) string {
	if len(m) == 0 {
		return ""
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func intFromMap(m map[string]any, key string) int {
	if len(m) == 0 {
		return 0
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return parsed
		}
	}
	return 0
}

func timeFromMap(m map[string]any, key string) *time.Time {
	if len(m) == 0 {
		return nil
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, trimmed)
		if err != nil {
			return nil
		}
		utc := parsed.UTC()
		return &utc
	default:
		return nil
	}
}

func clearExplicitCardFields(ctx context.Context, tx *sql.Tx, issueID string, payload map[string]any) error {
	if tx == nil || strings.TrimSpace(issueID) == "" || len(payload) == 0 {
		return nil
	}

	updates := make([]string, 0, 5)
	if shouldClearStringField(payload, "tracker_state") {
		updates = append(updates, "tracker_state = ''")
	}
	if shouldClearStringField(payload, "last_outcome") {
		updates = append(updates, "last_outcome = ''")
	}
	if shouldClearStringField(payload, "denial_reason") {
		updates = append(updates, "denial_reason = ''")
	}
	if shouldClearStringField(payload, "suggestion") {
		updates = append(updates, "suggestion = ''")
	}
	if shouldClearTimeField(payload, "retry_due_at") {
		updates = append(updates, "retry_due_at = ''")
	}
	if len(updates) == 0 {
		return nil
	}

	query := `UPDATE v2_orchestration_cards SET ` + strings.Join(updates, ", ") + ` WHERE issue_id = ?`
	if _, err := tx.ExecContext(ctx, query, issueID); err != nil {
		return fmt.Errorf("clear orchestration card fields: %w", err)
	}
	return nil
}

func shouldClearStringField(m map[string]any, key string) bool {
	if len(m) == 0 {
		return false
	}
	raw, ok := m[key]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	default:
		return strings.TrimSpace(fmt.Sprint(v)) == ""
	}
}

func shouldClearTimeField(m map[string]any, key string) bool {
	if len(m) == 0 {
		return false
	}
	raw, ok := m[key]
	if !ok {
		return false
	}
	if raw == nil {
		return true
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v) == ""
	default:
		return timeFromMap(m, key) == nil
	}
}

func eventTimestamp(evt coreevents.Event, fallback time.Time) *time.Time {
	if !evt.OccurredAt.IsZero() {
		ts := evt.OccurredAt.UTC()
		return &ts
	}
	ts := fallback.UTC()
	return &ts
}

func scopeIDForEvent(evt coreevents.Event, payload map[string]any, issueID string) string {
	command := strings.TrimSpace(evt.Command)
	workspaceID := strings.TrimSpace(stringFromMap(payload, "workspace_id"))
	switch command {
	case "orchestration/refresh":
		if workspaceID != "" {
			return workspaceID
		}
		return "_workspace"
	default:
		if strings.TrimSpace(issueID) != "" {
			return strings.TrimSpace(issueID)
		}
		if workspaceID != "" {
			return workspaceID
		}
		if strings.TrimSpace(evt.StreamID) != "" {
			return strings.TrimSpace(evt.StreamID)
		}
		return "_global"
	}
}
