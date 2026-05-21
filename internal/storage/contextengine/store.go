package contextengine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/context/contextengine"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

// Clock provides the current time. Inject for deterministic tests.
type Clock interface {
	Now() time.Time
}

// RealClock returns the actual system time.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// EventFilter is an alias for the domain-level filter type.
type EventFilter = contextengine.EventFilter

// ClaimFilter is an alias for the domain-level filter type.
type ClaimFilter = contextengine.ClaimFilter

// StalenessFilter is an alias for the domain-level filter type.
type StalenessFilter = contextengine.StalenessFilter

// ImpactFilter is an alias for the domain-level filter type.
type ImpactFilter = contextengine.ImpactFilter

// ProjectionFilter is an alias for the domain-level filter type.
type ProjectionFilter = contextengine.ProjectionFilter

// RetrievalEpisodeFilter is an alias for the domain-level filter type.
type RetrievalEpisodeFilter = contextengine.RetrievalEpisodeFilter

// RetrievalFeedbackFilter is an alias for the domain-level filter type.
type RetrievalFeedbackFilter = contextengine.RetrievalFeedbackFilter

// Store defines the abstract persistence interface for the context engine.
type Store interface {
	// Close releases all resources. Safe to call multiple times.
	Close() error

	// Events (append-only).
	AppendEvent(ctx context.Context, event contextengine.ContextEvent) (contextengine.ContextEvent, error)
	ListEvents(ctx context.Context, filter EventFilter) ([]contextengine.ContextEvent, error)

	// Evidence packs.
	PutEvidencePack(ctx context.Context, pack contextengine.EvidencePack) (contextengine.EvidencePack, error)
	GetEvidencePack(ctx context.Context, id string) (contextengine.EvidencePack, error)

	// Evidence nodes.
	PutEvidenceNode(ctx context.Context, node contextengine.EvidenceNode) (contextengine.EvidenceNode, error)
	GetEvidenceNode(ctx context.Context, id string) (contextengine.EvidenceNode, error)
	ListEvidenceNodes(ctx context.Context, workspaceID string, ref contextengine.EvidenceRef, limit int) ([]contextengine.EvidenceNode, error)

	// Memory claims.
	UpsertClaim(ctx context.Context, claim contextengine.MemoryClaim) (contextengine.MemoryClaim, error)
	GetClaim(ctx context.Context, id string) (contextengine.MemoryClaim, error)
	ListClaims(ctx context.Context, filter ClaimFilter) ([]contextengine.MemoryClaim, error)

	// Impact edges.
	PutImpactEdge(ctx context.Context, edge contextengine.ImpactEdge) (contextengine.ImpactEdge, error)
	ListImpactEdges(ctx context.Context, filter ImpactFilter) ([]contextengine.ImpactEdge, error)
	// ReverseImpact returns all edges pointing to the target ref.
	ReverseImpact(ctx context.Context, workspaceID string, target contextengine.EvidenceRef) ([]contextengine.ImpactEdge, error)

	// Staleness markers.
	UpsertStaleness(ctx context.Context, marker contextengine.StalenessMarker) (contextengine.StalenessMarker, error)
	GetStaleness(ctx context.Context, id string) (contextengine.StalenessMarker, error)
	ListStaleness(ctx context.Context, filter StalenessFilter) ([]contextengine.StalenessMarker, error)

	// Projections.
	PutProjection(ctx context.Context, id, workspaceID, projectionType string, version int, taskID string, generatedFromEvents []string, payload any, generatedAt, expiresAt time.Time) error
	GetProjection(ctx context.Context, workspaceID, id string) (projectionType string, version int, taskID string, generatedFromEvents []string, payload json.RawMessage, generatedAt, expiresAt time.Time, err error)
	ListProjections(ctx context.Context, filter ProjectionFilter) ([]ProjectionRow, error)

	// Retrieval episodes (append-only).
	RecordRetrievalEpisode(ctx context.Context, episode contextengine.RetrievalEpisode) (contextengine.RetrievalEpisode, error)
	GetRetrievalEpisode(ctx context.Context, id string) (contextengine.RetrievalEpisode, error)
	ListRetrievalEpisodes(ctx context.Context, filter RetrievalEpisodeFilter) ([]contextengine.RetrievalEpisode, error)

	// Retrieval feedback (append-only).
	RecordRetrievalFeedback(ctx context.Context, feedback contextengine.RetrievalFeedback) (contextengine.RetrievalFeedback, error)
	GetRetrievalFeedback(ctx context.Context, id string) (contextengine.RetrievalFeedback, error)
	ListRetrievalFeedback(ctx context.Context, filter RetrievalFeedbackFilter) ([]contextengine.RetrievalFeedback, error)

	// Index verification (for testing).
	ExplainQueryPlan(ctx context.Context, query string, args ...any) (string, error)
}

// ProjectionRow is an alias for the domain-level type.
type ProjectionRow = contextengine.ProjectionRow

// sqliteStore implements Store using SQLite.
type sqliteStore struct {
	db    *sql.DB
	close func() error
	clock Clock
	cas   CASBackend
	// writeMu serializes write paths in-process. SQLite WAL mode allows
	// concurrent readers but only one writer; concurrent writers from lane
	// goroutines (e.g. retrieve_mixed) hit SQLITE_BUSY before busy_timeout
	// helps. Holding this mutex around every write keeps writers serialized
	// in-process so the SQLite lock is never contended.
	writeMu sync.Mutex
}

// CASBackend stores and retrieves large payloads outside the database.
type CASBackend interface {
	Put(ctx context.Context, data []byte) (digest string, err error)
	Get(ctx context.Context, digest string) ([]byte, error)
}

// Open initializes a new SQLite-backed Store.
func Open(ctx context.Context, root string, opts ...Option) (Store, error) {
	o := defaultOptions()
	for _, opt := range opts {
		opt(o)
	}

	db, closeFn, err := dbutil.OpenStoreDB(ctx, root, "CONTEXTENGINE", "contextengine.db", Migrate)
	if err != nil {
		return nil, fmt.Errorf("contextengine: open db: %w", err)
	}

	return &sqliteStore{db: db, close: closeFn, clock: o.clock, cas: o.cas}, nil
}

// OpenDB creates a Store from an existing *sql.DB (for testing).
func OpenDB(db *sql.DB, clock Clock, cas CASBackend) (Store, error) {
	return &sqliteStore{db: db, close: func() error { return nil }, clock: clock, cas: cas}, nil
}

// Option configures Store behavior.
type Option func(*options)

type options struct {
	clock Clock
	cas   CASBackend
}

func defaultOptions() *options {
	return &options{
		clock: RealClock{},
		cas:   nil, // no CAS by default
	}
}

// WithClock sets the clock for deterministic timestamps.
func WithClock(c Clock) Option {
	return func(o *options) { o.clock = c }
}

// WithCAS sets the CAS backend for large payloads.
func WithCAS(c CASBackend) Option {
	return func(o *options) { o.cas = c }
}

func (s *sqliteStore) Close() error {
	if s == nil || s.close == nil {
		return nil
	}
	return s.close()
}

// now returns the current time from the injected clock.
func (s *sqliteStore) now() time.Time {
	return s.clock.Now().UTC()
}

// --- Events (append-only) ---

func (s *sqliteStore) AppendEvent(ctx context.Context, event contextengine.ContextEvent) (contextengine.ContextEvent, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if event.CreatedAt.IsZero() {
		event.CreatedAt = s.now()
	}
	if err := event.Validate(); err != nil {
		return contextengine.ContextEvent{}, fmt.Errorf("contextengine: append event: %w", err)
	}

	refsJSON, err := json.Marshal(event.Refs)
	if err != nil {
		return contextengine.ContextEvent{}, fmt.Errorf("contextengine: marshal refs: %w", err)
	}
	dataJSON, err := json.Marshal(event.Data)
	if err != nil {
		return contextengine.ContextEvent{}, fmt.Errorf("contextengine: marshal data: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO context_events (id, workspace_id, kind, source, task_id, session_id, refs, data, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.WorkspaceID, string(event.Kind), event.Source,
		event.TaskID, event.SessionID, string(refsJSON), string(dataJSON),
		event.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return contextengine.ContextEvent{}, fmt.Errorf("contextengine: append event: %w", err)
	}
	return event, nil
}

func (s *sqliteStore) ListEvents(ctx context.Context, filter EventFilter) ([]contextengine.ContextEvent, error) {
	query := `SELECT id, workspace_id, kind, source, task_id, session_id, refs, data, created_at
		FROM context_events WHERE 1=1`
	var args []any

	if filter.WorkspaceID != "" {
		query += " AND workspace_id = ?"
		args = append(args, filter.WorkspaceID)
	}
	if filter.Kind != "" {
		query += " AND kind = ?"
		args = append(args, string(filter.Kind))
	}
	if filter.TaskID != "" {
		query += " AND task_id = ?"
		args = append(args, filter.TaskID)
	}
	if filter.SessionID != "" {
		query += " AND session_id = ?"
		args = append(args, filter.SessionID)
	}

	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("contextengine: list events: %w", err)
	}
	defer rows.Close()

	var events []contextengine.ContextEvent
	for rows.Next() {
		var e contextengine.ContextEvent
		var kind, refsJSON, dataJSON, createdAt string
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &kind, &e.Source, &e.TaskID, &e.SessionID, &refsJSON, &dataJSON, &createdAt); err != nil {
			return nil, fmt.Errorf("contextengine: scan event: %w", err)
		}
		e.Kind = contextengine.ContextEventKind(kind)
		if err := json.Unmarshal([]byte(refsJSON), &e.Refs); err != nil {
			return nil, fmt.Errorf("contextengine: unmarshal refs: %w", err)
		}
		if err := json.Unmarshal([]byte(dataJSON), &e.Data); err != nil {
			return nil, fmt.Errorf("contextengine: unmarshal data: %w", err)
		}
		e.CreatedAt, err = time.Parse(time.RFC3339Nano, createdAt)
		if err != nil {
			e.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// --- Evidence Packs ---

const casThreshold = 64 * 1024 // 64KB

func (s *sqliteStore) PutEvidencePack(ctx context.Context, pack contextengine.EvidencePack) (contextengine.EvidencePack, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := pack.Validate(); err != nil {
		return contextengine.EvidencePack{}, fmt.Errorf("contextengine: put pack: %w", err)
	}

	nodesJSON, err := json.Marshal(pack.Nodes)
	if err != nil {
		return contextengine.EvidencePack{}, fmt.Errorf("contextengine: marshal nodes: %w", err)
	}
	telemetryJSON, err := json.Marshal(pack.Telemetry)
	if err != nil {
		return contextengine.EvidencePack{}, fmt.Errorf("contextengine: marshal telemetry: %w", err)
	}
	metadataJSON, err := json.Marshal(pack.Metadata)
	if err != nil {
		return contextengine.EvidencePack{}, fmt.Errorf("contextengine: marshal metadata: %w", err)
	}

	var casDigest string
	if s.cas != nil && len(nodesJSON) > casThreshold {
		digest, cerr := s.cas.Put(ctx, nodesJSON)
		if cerr != nil {
			return contextengine.EvidencePack{}, fmt.Errorf("contextengine: cas put: %w", cerr)
		}
		casDigest = digest
		nodesJSON = []byte("[]") // clear inline nodes
	}

	createdAt := s.now()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO evidence_packs (id, workspace_id, query, lane, nodes, telemetry, metadata, cas_digest, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			query = excluded.query, lane = excluded.lane, nodes = excluded.nodes,
			telemetry = excluded.telemetry, metadata = excluded.metadata,
			cas_digest = excluded.cas_digest`,
		pack.ID, pack.WorkspaceID, pack.Query, string(pack.Lane),
		string(nodesJSON), string(telemetryJSON), string(metadataJSON),
		casDigest, createdAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return contextengine.EvidencePack{}, fmt.Errorf("contextengine: put pack: %w", err)
	}
	return pack, nil
}

func (s *sqliteStore) GetEvidencePack(ctx context.Context, id string) (contextengine.EvidencePack, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, query, lane, nodes, telemetry, metadata, cas_digest, created_at
		FROM evidence_packs WHERE id = ?`, id)

	var pack contextengine.EvidencePack
	var lane, nodesJSON, telemetryJSON, metadataJSON, casDigest, createdAt string
	if err := row.Scan(&pack.ID, &pack.WorkspaceID, &pack.Query, &lane,
		&nodesJSON, &telemetryJSON, &metadataJSON, &casDigest, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return contextengine.EvidencePack{}, ErrNotFound
		}
		return contextengine.EvidencePack{}, fmt.Errorf("contextengine: get pack: %w", err)
	}
	pack.Lane = contextengine.EvidenceLane(lane)

	if casDigest != "" && s.cas != nil {
		data, err := s.cas.Get(ctx, casDigest)
		if err != nil {
			return contextengine.EvidencePack{}, fmt.Errorf("contextengine: cas get: %w", err)
		}
		if err := json.Unmarshal(data, &pack.Nodes); err != nil {
			return contextengine.EvidencePack{}, fmt.Errorf("contextengine: unmarshal cas nodes: %w", err)
		}
	} else {
		if err := json.Unmarshal([]byte(nodesJSON), &pack.Nodes); err != nil {
			return contextengine.EvidencePack{}, fmt.Errorf("contextengine: unmarshal nodes: %w", err)
		}
	}

	if err := json.Unmarshal([]byte(telemetryJSON), &pack.Telemetry); err != nil {
		return contextengine.EvidencePack{}, fmt.Errorf("contextengine: unmarshal telemetry: %w", err)
	}
	if err := json.Unmarshal([]byte(metadataJSON), &pack.Metadata); err != nil {
		return contextengine.EvidencePack{}, fmt.Errorf("contextengine: unmarshal metadata: %w", err)
	}
	// Pack doesn't have CreatedAt in the domain type; skip returning it
	return pack, nil
}

// --- Evidence Nodes ---

func (s *sqliteStore) PutEvidenceNode(ctx context.Context, node contextengine.EvidenceNode) (contextengine.EvidenceNode, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := node.Validate(); err != nil {
		return contextengine.EvidenceNode{}, fmt.Errorf("contextengine: put node: %w", err)
	}

	metadataJSON, err := json.Marshal(node.Metadata)
	if err != nil {
		return contextengine.EvidenceNode{}, fmt.Errorf("contextengine: marshal metadata: %w", err)
	}

	var casDigest string
	statement := node.Statement
	if s.cas != nil && len(node.Statement) > casThreshold {
		digest, cerr := s.cas.Put(ctx, []byte(node.Statement))
		if cerr != nil {
			return contextengine.EvidenceNode{}, fmt.Errorf("contextengine: cas put: %w", cerr)
		}
		casDigest = digest
		statement = ""
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO evidence_nodes (id, workspace_id, node_type, ref_type, ref_value, statement,
			confidence, grounding, count, first_seen, last_seen, metadata, cas_digest)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			node_type = excluded.node_type, statement = excluded.statement,
			confidence = excluded.confidence, grounding = excluded.grounding,
			count = excluded.count, last_seen = excluded.last_seen,
			metadata = excluded.metadata, cas_digest = excluded.cas_digest`,
		node.ID, node.WorkspaceID, string(node.NodeType),
		string(node.Ref.Type), node.Ref.Ref, statement,
		node.Confidence, string(node.Grounding), node.Count,
		node.FirstSeen.UTC().Format(time.RFC3339Nano),
		node.LastSeen.UTC().Format(time.RFC3339Nano),
		string(metadataJSON), casDigest)
	if err != nil {
		return contextengine.EvidenceNode{}, fmt.Errorf("contextengine: put node: %w", err)
	}
	return node, nil
}

func (s *sqliteStore) GetEvidenceNode(ctx context.Context, id string) (contextengine.EvidenceNode, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, node_type, ref_type, ref_value, statement,
			confidence, grounding, count, first_seen, last_seen, metadata, cas_digest
		FROM evidence_nodes WHERE id = ?`, id)

	return s.scanNode(row)
}

func (s *sqliteStore) ListEvidenceNodes(ctx context.Context, workspaceID string, ref contextengine.EvidenceRef, limit int) ([]contextengine.EvidenceNode, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_id, node_type, ref_type, ref_value, statement,
			confidence, grounding, count, first_seen, last_seen, metadata, cas_digest
		FROM evidence_nodes
		WHERE workspace_id = ? AND ref_type = ? AND ref_value = ?
		ORDER BY last_seen DESC LIMIT ?`,
		workspaceID, string(ref.Type), ref.Ref, limit)
	if err != nil {
		return nil, fmt.Errorf("contextengine: list nodes: %w", err)
	}
	defer rows.Close()

	var nodes []contextengine.EvidenceNode
	for rows.Next() {
		var node contextengine.EvidenceNode
		var nodeType, refType, refValue, statement, grounding, firstSeen, lastSeen, metadataJSON, casDigest string
		if err := rows.Scan(&node.ID, &node.WorkspaceID, &nodeType, &refType, &refValue, &statement,
			&node.Confidence, &grounding, &node.Count, &firstSeen, &lastSeen, &metadataJSON, &casDigest); err != nil {
			return nil, fmt.Errorf("contextengine: scan node: %w", err)
		}
		node.NodeType = contextengine.EvidenceNodeType(nodeType)
		node.Ref = contextengine.EvidenceRef{Type: contextengine.RefType(refType), Ref: refValue}
		node.Grounding = contextengine.Grounding(grounding)
		node.FirstSeen, _ = time.Parse(time.RFC3339Nano, firstSeen)
		if node.FirstSeen.IsZero() {
			node.FirstSeen, _ = time.Parse(time.RFC3339, firstSeen)
		}
		node.LastSeen, _ = time.Parse(time.RFC3339Nano, lastSeen)
		if node.LastSeen.IsZero() {
			node.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
		}
		_ = json.Unmarshal([]byte(metadataJSON), &node.Metadata)

		if casDigest != "" && s.cas != nil {
			data, cerr := s.cas.Get(ctx, casDigest)
			if cerr == nil {
				node.Statement = string(data)
			}
		} else {
			node.Statement = statement
		}
		nodes = append(nodes, node)
	}
	return nodes, rows.Err()
}

func (s *sqliteStore) scanNode(row *sql.Row) (contextengine.EvidenceNode, error) {
	var node contextengine.EvidenceNode
	var nodeType, refType, refValue, statement, grounding, firstSeen, lastSeen, metadataJSON, casDigest string
	if err := row.Scan(&node.ID, &node.WorkspaceID, &nodeType, &refType, &refValue, &statement,
		&node.Confidence, &grounding, &node.Count, &firstSeen, &lastSeen, &metadataJSON, &casDigest); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return contextengine.EvidenceNode{}, ErrNotFound
		}
		return contextengine.EvidenceNode{}, fmt.Errorf("contextengine: scan node: %w", err)
	}
	node.NodeType = contextengine.EvidenceNodeType(nodeType)
	node.Ref = contextengine.EvidenceRef{Type: contextengine.RefType(refType), Ref: refValue}
	node.Grounding = contextengine.Grounding(grounding)
	node.FirstSeen, _ = time.Parse(time.RFC3339Nano, firstSeen)
	if node.FirstSeen.IsZero() {
		node.FirstSeen, _ = time.Parse(time.RFC3339, firstSeen)
	}
	node.LastSeen, _ = time.Parse(time.RFC3339Nano, lastSeen)
	if node.LastSeen.IsZero() {
		node.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
	}
	_ = json.Unmarshal([]byte(metadataJSON), &node.Metadata)

	if casDigest != "" && s.cas != nil {
		data, err := s.cas.Get(context.Background(), casDigest)
		if err == nil {
			node.Statement = string(data)
		}
	} else {
		node.Statement = statement
	}
	return node, nil
}

// --- Memory Claims ---

func (s *sqliteStore) UpsertClaim(ctx context.Context, claim contextengine.MemoryClaim) (contextengine.MemoryClaim, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if claim.CreatedAt.IsZero() {
		claim.CreatedAt = s.now()
	}
	claim.UpdatedAt = s.now()
	if err := claim.Validate(); err != nil {
		return contextengine.MemoryClaim{}, fmt.Errorf("contextengine: upsert claim: %w", err)
	}

	refsJSON, err := json.Marshal(claim.SourceRefs)
	if err != nil {
		return contextengine.MemoryClaim{}, fmt.Errorf("contextengine: marshal source_refs: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO memory_claims (id, workspace_id, claim_type, status,
			scope_path, scope_task_id, scope_session_id,
			summary, confidence, blast_radius,
			source_refs, source_event_id, superseded_by, reason,
			created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			status = excluded.status, summary = excluded.summary,
			confidence = excluded.confidence, blast_radius = excluded.blast_radius,
			source_refs = excluded.source_refs, source_event_id = excluded.source_event_id,
			superseded_by = excluded.superseded_by, reason = excluded.reason,
			updated_at = excluded.updated_at`,
		claim.ID, claim.WorkspaceID, claim.ClaimType, string(claim.Status),
		claim.Scope.Path, claim.Scope.TaskID, claim.Scope.SessionID,
		claim.Summary, claim.Confidence, claim.BlastRadius,
		string(refsJSON), claim.SourceEventID, claim.SupersededBy, claim.Reason,
		claim.CreatedAt.UTC().Format(time.RFC3339Nano),
		claim.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return contextengine.MemoryClaim{}, fmt.Errorf("contextengine: upsert claim: %w", err)
	}
	return claim, nil
}

func (s *sqliteStore) GetClaim(ctx context.Context, id string) (contextengine.MemoryClaim, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, claim_type, status,
			scope_path, scope_task_id, scope_session_id,
			summary, confidence, blast_radius,
			source_refs, source_event_id, superseded_by, reason,
			created_at, updated_at
		FROM memory_claims WHERE id = ?`, id)

	return s.scanClaim(row)
}

func (s *sqliteStore) ListClaims(ctx context.Context, filter ClaimFilter) ([]contextengine.MemoryClaim, error) {
	query := `SELECT id, workspace_id, claim_type, status,
		scope_path, scope_task_id, scope_session_id,
		summary, confidence, blast_radius,
		source_refs, source_event_id, superseded_by, reason,
		created_at, updated_at
		FROM memory_claims WHERE 1=1`
	var args []any

	if filter.WorkspaceID != "" {
		query += " AND workspace_id = ?"
		args = append(args, filter.WorkspaceID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, string(filter.Status))
	}
	query += " ORDER BY updated_at DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}
	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("contextengine: list claims: %w", err)
	}
	defer rows.Close()

	var claims []contextengine.MemoryClaim
	for rows.Next() {
		var claim contextengine.MemoryClaim
		var status, refsJSON, createdAt, updatedAt string
		if err := rows.Scan(&claim.ID, &claim.WorkspaceID, &claim.ClaimType, &status,
			&claim.Scope.Path, &claim.Scope.TaskID, &claim.Scope.SessionID,
			&claim.Summary, &claim.Confidence, &claim.BlastRadius,
			&refsJSON, &claim.SourceEventID, &claim.SupersededBy, &claim.Reason,
			&createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("contextengine: scan claim: %w", err)
		}
		claim.Status = contextengine.ClaimStatus(status)
		_ = json.Unmarshal([]byte(refsJSON), &claim.SourceRefs)
		claim.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if claim.CreatedAt.IsZero() {
			claim.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}
		claim.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		if claim.UpdatedAt.IsZero() {
			claim.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		}
		claims = append(claims, claim)
	}
	return claims, rows.Err()
}

func (s *sqliteStore) scanClaim(row *sql.Row) (contextengine.MemoryClaim, error) {
	var claim contextengine.MemoryClaim
	var status, refsJSON, createdAt, updatedAt string
	if err := row.Scan(&claim.ID, &claim.WorkspaceID, &claim.ClaimType, &status,
		&claim.Scope.Path, &claim.Scope.TaskID, &claim.Scope.SessionID,
		&claim.Summary, &claim.Confidence, &claim.BlastRadius,
		&refsJSON, &claim.SourceEventID, &claim.SupersededBy, &claim.Reason,
		&createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return contextengine.MemoryClaim{}, ErrNotFound
		}
		return contextengine.MemoryClaim{}, fmt.Errorf("contextengine: scan claim: %w", err)
	}
	claim.Status = contextengine.ClaimStatus(status)
	_ = json.Unmarshal([]byte(refsJSON), &claim.SourceRefs)
	claim.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if claim.CreatedAt.IsZero() {
		claim.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}
	claim.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if claim.UpdatedAt.IsZero() {
		claim.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	}
	return claim, nil
}

// --- Impact Edges ---

func (s *sqliteStore) PutImpactEdge(ctx context.Context, edge contextengine.ImpactEdge) (contextengine.ImpactEdge, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if edge.CreatedAt.IsZero() {
		edge.CreatedAt = s.now()
	}
	if err := edge.Validate(); err != nil {
		return contextengine.ImpactEdge{}, fmt.Errorf("contextengine: put impact edge: %w", err)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO impact_edges (id, workspace_id, from_type, from_ref, to_type, to_ref, kind, source_event_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, from_type, from_ref, to_type, to_ref, kind) DO UPDATE SET
			source_event_id = excluded.source_event_id, created_at = excluded.created_at`,
		edge.ID, edge.WorkspaceID,
		string(edge.From.Type), edge.From.Ref,
		string(edge.To.Type), edge.To.Ref,
		string(edge.Kind), edge.SourceEventID,
		edge.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return contextengine.ImpactEdge{}, fmt.Errorf("contextengine: put impact edge: %w", err)
	}
	return edge, nil
}

func (s *sqliteStore) ListImpactEdges(ctx context.Context, filter ImpactFilter) ([]contextengine.ImpactEdge, error) {
	query := `SELECT id, workspace_id, from_type, from_ref, to_type, to_ref, kind, source_event_id, created_at
		FROM impact_edges WHERE 1=1`
	var args []any

	if filter.WorkspaceID != "" {
		query += " AND workspace_id = ?"
		args = append(args, filter.WorkspaceID)
	}
	if filter.FromRef != nil {
		query += " AND from_type = ? AND from_ref = ?"
		args = append(args, string(filter.FromRef.Type), filter.FromRef.Ref)
	}
	if filter.ToRef != nil {
		query += " AND to_type = ? AND to_ref = ?"
		args = append(args, string(filter.ToRef.Type), filter.ToRef.Ref)
	}
	if filter.Kind != "" {
		query += " AND kind = ?"
		args = append(args, string(filter.Kind))
	}
	query += " ORDER BY created_at DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("contextengine: list impact edges: %w", err)
	}
	defer rows.Close()

	return s.scanEdges(rows)
}

func (s *sqliteStore) ReverseImpact(ctx context.Context, workspaceID string, target contextengine.EvidenceRef) ([]contextengine.ImpactEdge, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, workspace_id, from_type, from_ref, to_type, to_ref, kind, source_event_id, created_at
		FROM impact_edges
		WHERE workspace_id = ? AND to_type = ? AND to_ref = ?
		ORDER BY created_at DESC`,
		workspaceID, string(target.Type), target.Ref)
	if err != nil {
		return nil, fmt.Errorf("contextengine: reverse impact: %w", err)
	}
	defer rows.Close()
	return s.scanEdges(rows)
}

func (s *sqliteStore) scanEdges(rows *sql.Rows) ([]contextengine.ImpactEdge, error) {
	var edges []contextengine.ImpactEdge
	for rows.Next() {
		var edge contextengine.ImpactEdge
		var fromType, fromRef, toType, toRef, kind, createdAt string
		if err := rows.Scan(&edge.ID, &edge.WorkspaceID,
			&fromType, &fromRef, &toType, &toRef,
			&kind, &edge.SourceEventID, &createdAt); err != nil {
			return nil, fmt.Errorf("contextengine: scan edge: %w", err)
		}
		edge.From = contextengine.EvidenceRef{Type: contextengine.RefType(fromType), Ref: fromRef}
		edge.To = contextengine.EvidenceRef{Type: contextengine.RefType(toType), Ref: toRef}
		edge.Kind = contextengine.ImpactEdgeKind(kind)
		edge.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if edge.CreatedAt.IsZero() {
			edge.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}
		edges = append(edges, edge)
	}
	return edges, rows.Err()
}

// --- Staleness Markers ---

func (s *sqliteStore) UpsertStaleness(ctx context.Context, marker contextengine.StalenessMarker) (contextengine.StalenessMarker, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if marker.CreatedAt.IsZero() {
		marker.CreatedAt = s.now()
	}
	marker.UpdatedAt = s.now()
	if err := marker.Validate(); err != nil {
		return contextengine.StalenessMarker{}, fmt.Errorf("contextengine: upsert staleness: %w", err)
	}

	causedJSON, err := json.Marshal(marker.CausedByEvents)
	if err != nil {
		return contextengine.StalenessMarker{}, fmt.Errorf("contextengine: marshal caused_by_events: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO staleness_markers (id, workspace_id, target_ref_type, target_ref_value,
			status, caused_by_events, resolved_by_event, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, target_ref_type, target_ref_value) DO UPDATE SET
			status = excluded.status, caused_by_events = excluded.caused_by_events,
			resolved_by_event = excluded.resolved_by_event, updated_at = excluded.updated_at`,
		marker.ID, marker.WorkspaceID,
		string(marker.TargetRef.Type), marker.TargetRef.Ref,
		string(marker.Status), string(causedJSON), marker.ResolvedByEvent,
		marker.CreatedAt.UTC().Format(time.RFC3339Nano),
		marker.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return contextengine.StalenessMarker{}, fmt.Errorf("contextengine: upsert staleness: %w", err)
	}
	return marker, nil
}

func (s *sqliteStore) GetStaleness(ctx context.Context, id string) (contextengine.StalenessMarker, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, target_ref_type, target_ref_value,
			status, caused_by_events, resolved_by_event, created_at, updated_at
		FROM staleness_markers WHERE id = ?`, id)

	return s.scanStaleness(row)
}

func (s *sqliteStore) ListStaleness(ctx context.Context, filter StalenessFilter) ([]contextengine.StalenessMarker, error) {
	query := `SELECT id, workspace_id, target_ref_type, target_ref_value,
		status, caused_by_events, resolved_by_event, created_at, updated_at
		FROM staleness_markers WHERE 1=1`
	var args []any

	if filter.WorkspaceID != "" {
		query += " AND workspace_id = ?"
		args = append(args, filter.WorkspaceID)
	}
	if filter.TargetRef != nil {
		query += " AND target_ref_type = ? AND target_ref_value = ?"
		args = append(args, string(filter.TargetRef.Type), filter.TargetRef.Ref)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, string(filter.Status))
	}
	query += " ORDER BY updated_at DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("contextengine: list staleness: %w", err)
	}
	defer rows.Close()

	var markers []contextengine.StalenessMarker
	for rows.Next() {
		var marker contextengine.StalenessMarker
		var status, causedJSON, createdAt, updatedAt string
		var targetType, targetValue string
		if err := rows.Scan(&marker.ID, &marker.WorkspaceID, &targetType, &targetValue,
			&status, &causedJSON, &marker.ResolvedByEvent, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("contextengine: scan staleness: %w", err)
		}
		marker.TargetRef = contextengine.EvidenceRef{Type: contextengine.RefType(targetType), Ref: targetValue}
		marker.Status = contextengine.StalenessStatus(status)
		_ = json.Unmarshal([]byte(causedJSON), &marker.CausedByEvents)
		marker.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if marker.CreatedAt.IsZero() {
			marker.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}
		marker.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		if marker.UpdatedAt.IsZero() {
			marker.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		}
		markers = append(markers, marker)
	}
	return markers, rows.Err()
}

func (s *sqliteStore) scanStaleness(row *sql.Row) (contextengine.StalenessMarker, error) {
	var marker contextengine.StalenessMarker
	var status, causedJSON, createdAt, updatedAt string
	var targetType, targetValue string
	if err := row.Scan(&marker.ID, &marker.WorkspaceID, &targetType, &targetValue,
		&status, &causedJSON, &marker.ResolvedByEvent, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return contextengine.StalenessMarker{}, ErrNotFound
		}
		return contextengine.StalenessMarker{}, fmt.Errorf("contextengine: scan staleness: %w", err)
	}
	marker.TargetRef = contextengine.EvidenceRef{Type: contextengine.RefType(targetType), Ref: targetValue}
	marker.Status = contextengine.StalenessStatus(status)
	_ = json.Unmarshal([]byte(causedJSON), &marker.CausedByEvents)
	marker.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if marker.CreatedAt.IsZero() {
		marker.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}
	marker.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if marker.UpdatedAt.IsZero() {
		marker.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	}
	return marker, nil
}

// --- Projections ---

func (s *sqliteStore) PutProjection(ctx context.Context, id, workspaceID, projectionType string, version int, taskID string, generatedFromEvents []string, payload any, generatedAt, expiresAt time.Time) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	eventsJSON, err := json.Marshal(generatedFromEvents)
	if err != nil {
		return fmt.Errorf("contextengine: marshal generated_from_events: %w", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("contextengine: marshal payload: %w", err)
	}
	now := s.now()

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO projections (id, workspace_id, projection_type, projection_version, task_id,
			generated_from_events, payload, generated_at, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, id) DO UPDATE SET
			projection_version = excluded.projection_version,
			task_id = excluded.task_id,
			generated_from_events = excluded.generated_from_events,
			payload = excluded.payload,
			generated_at = excluded.generated_at,
			expires_at = excluded.expires_at`,
		id, workspaceID, projectionType, version, taskID,
		string(eventsJSON), string(payloadJSON),
		generatedAt.UTC().Format(time.RFC3339Nano),
		expiresAt.UTC().Format(time.RFC3339Nano),
		now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("contextengine: put projection: %w", err)
	}
	return nil
}

func (s *sqliteStore) GetProjection(ctx context.Context, workspaceID, id string) (string, int, string, []string, json.RawMessage, time.Time, time.Time, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT projection_type, projection_version, task_id, generated_from_events,
			payload, generated_at, expires_at
		FROM projections WHERE workspace_id = ? AND id = ?`, workspaceID, id)

	var projectionType, eventsJSON string
	var version int
	var taskID string
	var payloadStr string
	var generatedAt, expiresAt string

	if err := row.Scan(&projectionType, &version, &taskID, &eventsJSON,
		&payloadStr, &generatedAt, &expiresAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, "", nil, nil, time.Time{}, time.Time{}, ErrNotFound
		}
		return "", 0, "", nil, nil, time.Time{}, time.Time{}, fmt.Errorf("contextengine: get projection: %w", err)
	}

	var events []string
	_ = json.Unmarshal([]byte(eventsJSON), &events)
	payload := json.RawMessage(payloadStr)

	ga, _ := time.Parse(time.RFC3339Nano, generatedAt)
	if ga.IsZero() {
		ga, _ = time.Parse(time.RFC3339, generatedAt)
	}
	ea, _ := time.Parse(time.RFC3339Nano, expiresAt)
	if ea.IsZero() {
		ea, _ = time.Parse(time.RFC3339, expiresAt)
	}

	return projectionType, version, taskID, events, payload, ga, ea, nil
}

func (s *sqliteStore) ListProjections(ctx context.Context, filter ProjectionFilter) ([]ProjectionRow, error) {
	query := `SELECT id, workspace_id, projection_type, projection_version, task_id,
		generated_from_events, payload, generated_at, expires_at, created_at
		FROM projections WHERE 1=1`
	var args []any

	if filter.WorkspaceID != "" {
		query += " AND workspace_id = ?"
		args = append(args, filter.WorkspaceID)
	}
	if filter.ProjectionType != "" {
		query += " AND projection_type = ?"
		args = append(args, filter.ProjectionType)
	}
	if filter.TaskID != "" {
		query += " AND task_id = ?"
		args = append(args, filter.TaskID)
	}
	query += " ORDER BY projection_version DESC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("contextengine: list projections: %w", err)
	}
	defer rows.Close()

	var results []ProjectionRow
	for rows.Next() {
		var r ProjectionRow
		var eventsJSON, payloadStr string
		var generatedAt, expiresAt, createdAt string
		if err := rows.Scan(&r.ID, &r.WorkspaceID, &r.ProjectionType, &r.ProjectionVersion, &r.TaskID,
			&eventsJSON, &payloadStr, &generatedAt, &expiresAt, &createdAt); err != nil {
			return nil, fmt.Errorf("contextengine: scan projection: %w", err)
		}
		_ = json.Unmarshal([]byte(eventsJSON), &r.GeneratedFromEvents)
		r.Payload = json.RawMessage(payloadStr)
		r.GeneratedAt, _ = time.Parse(time.RFC3339Nano, generatedAt)
		if r.GeneratedAt.IsZero() {
			r.GeneratedAt, _ = time.Parse(time.RFC3339, generatedAt)
		}
		r.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)
		if r.ExpiresAt.IsZero() {
			r.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		if r.CreatedAt.IsZero() {
			r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// --- Retrieval Episodes (append-only) ---

func (s *sqliteStore) RecordRetrievalEpisode(ctx context.Context, episode contextengine.RetrievalEpisode) (contextengine.RetrievalEpisode, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if episode.CreatedAt.IsZero() {
		episode.CreatedAt = s.now()
	}
	if err := episode.Validate(); err != nil {
		return contextengine.RetrievalEpisode{}, fmt.Errorf("contextengine: record episode: %w", err)
	}

	subsJSON, err := json.Marshal(episode.SubEpisodeIDs)
	if err != nil {
		return contextengine.RetrievalEpisode{}, fmt.Errorf("contextengine: marshal sub_episode_ids: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO retrieval_episodes (id, workspace_id, query, lane, pack_id,
			duration_ms, tokens_used, hit_count, sub_episode_ids, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		episode.ID, episode.WorkspaceID, episode.Query, string(episode.Lane),
		episode.PackID, episode.DurationMs, episode.TokensUsed, episode.HitCount,
		string(subsJSON), episode.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return contextengine.RetrievalEpisode{}, fmt.Errorf("contextengine: record episode: %w", err)
	}
	return episode, nil
}

func (s *sqliteStore) GetRetrievalEpisode(ctx context.Context, id string) (contextengine.RetrievalEpisode, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, query, lane, pack_id,
			duration_ms, tokens_used, hit_count, sub_episode_ids, created_at
		FROM retrieval_episodes WHERE id = ?`, id)

	episode, err := scanRetrievalEpisode(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return contextengine.RetrievalEpisode{}, ErrNotFound
		}
		return contextengine.RetrievalEpisode{}, fmt.Errorf("contextengine: get episode: %w", err)
	}
	return episode, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRetrievalEpisode(row rowScanner) (contextengine.RetrievalEpisode, error) {
	var episode contextengine.RetrievalEpisode
	var lane, subsJSON, createdAt string
	if err := row.Scan(&episode.ID, &episode.WorkspaceID, &episode.Query, &lane,
		&episode.PackID, &episode.DurationMs, &episode.TokensUsed, &episode.HitCount,
		&subsJSON, &createdAt); err != nil {
		return contextengine.RetrievalEpisode{}, err
	}
	episode.Lane = contextengine.EvidenceLane(lane)
	_ = json.Unmarshal([]byte(subsJSON), &episode.SubEpisodeIDs)
	episode.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if episode.CreatedAt.IsZero() {
		episode.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}
	return episode, nil
}

func (s *sqliteStore) ListRetrievalEpisodes(ctx context.Context, filter RetrievalEpisodeFilter) ([]contextengine.RetrievalEpisode, error) {
	query := `SELECT id, workspace_id, query, lane, pack_id,
			duration_ms, tokens_used, hit_count, sub_episode_ids, created_at
		FROM retrieval_episodes WHERE 1=1`
	var args []any

	if filter.WorkspaceID != "" {
		query += " AND workspace_id = ?"
		args = append(args, filter.WorkspaceID)
	}
	if !filter.Since.IsZero() {
		query += " AND created_at >= ?"
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}

	query += " ORDER BY created_at ASC, id ASC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("contextengine: list episodes: %w", err)
	}
	defer rows.Close()

	var episodes []contextengine.RetrievalEpisode
	for rows.Next() {
		episode, err := scanRetrievalEpisode(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, episode)
	}
	return episodes, rows.Err()
}

// --- Retrieval Feedback (append-only) ---

func (s *sqliteStore) RecordRetrievalFeedback(ctx context.Context, feedback contextengine.RetrievalFeedback) (contextengine.RetrievalFeedback, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if feedback.CreatedAt.IsZero() {
		feedback.CreatedAt = s.now()
	}
	if err := feedback.Validate(); err != nil {
		return contextengine.RetrievalFeedback{}, fmt.Errorf("contextengine: record feedback: %w", err)
	}

	refsJSON, err := json.Marshal(feedback.UsedRefs)
	if err != nil {
		return contextengine.RetrievalFeedback{}, fmt.Errorf("contextengine: marshal used_refs: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO retrieval_feedback (id, workspace_id, episode_id, kind, query,
			used_refs, gap_stmt, correction_stmt, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		feedback.ID, feedback.WorkspaceID, feedback.EpisodeID,
		string(feedback.Kind), feedback.Query,
		string(refsJSON), feedback.GapStmt, feedback.CorrectionStmt,
		feedback.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return contextengine.RetrievalFeedback{}, fmt.Errorf("contextengine: record feedback: %w", err)
	}
	return feedback, nil
}

func (s *sqliteStore) GetRetrievalFeedback(ctx context.Context, id string) (contextengine.RetrievalFeedback, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, workspace_id, episode_id, kind, query,
			used_refs, gap_stmt, correction_stmt, created_at
		FROM retrieval_feedback WHERE id = ?`, id)

	feedback, err := scanRetrievalFeedback(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return contextengine.RetrievalFeedback{}, ErrNotFound
		}
		return contextengine.RetrievalFeedback{}, fmt.Errorf("contextengine: get feedback: %w", err)
	}
	return feedback, nil
}

func scanRetrievalFeedback(row rowScanner) (contextengine.RetrievalFeedback, error) {
	var feedback contextengine.RetrievalFeedback
	var kind, refsJSON, createdAt string
	if err := row.Scan(&feedback.ID, &feedback.WorkspaceID, &feedback.EpisodeID,
		&kind, &feedback.Query,
		&refsJSON, &feedback.GapStmt, &feedback.CorrectionStmt, &createdAt); err != nil {
		return contextengine.RetrievalFeedback{}, err
	}
	feedback.Kind = contextengine.RetrievalFeedbackKind(kind)
	_ = json.Unmarshal([]byte(refsJSON), &feedback.UsedRefs)
	feedback.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	if feedback.CreatedAt.IsZero() {
		feedback.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}
	return feedback, nil
}

func (s *sqliteStore) ListRetrievalFeedback(ctx context.Context, filter RetrievalFeedbackFilter) ([]contextengine.RetrievalFeedback, error) {
	query := `SELECT id, workspace_id, episode_id, kind, query,
			used_refs, gap_stmt, correction_stmt, created_at
		FROM retrieval_feedback WHERE 1=1`
	var args []any

	if filter.WorkspaceID != "" {
		query += " AND workspace_id = ?"
		args = append(args, filter.WorkspaceID)
	}
	if filter.EpisodeID != "" {
		query += " AND episode_id = ?"
		args = append(args, filter.EpisodeID)
	}
	if !filter.Since.IsZero() {
		query += " AND created_at >= ?"
		args = append(args, filter.Since.UTC().Format(time.RFC3339Nano))
	}
	if len(filter.Kinds) > 0 {
		query += " AND kind IN ("
		for i, kind := range filter.Kinds {
			if !kind.IsValid() {
				return nil, fmt.Errorf("contextengine: list feedback: unknown kind %q", kind)
			}
			if i > 0 {
				query += ", "
			}
			query += "?"
			args = append(args, string(kind))
		}
		query += ")"
	}

	query += " ORDER BY created_at ASC, id ASC"
	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("contextengine: list feedback: %w", err)
	}
	defer rows.Close()

	var out []contextengine.RetrievalFeedback
	for rows.Next() {
		feedback, err := scanRetrievalFeedback(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, feedback)
	}
	return out, rows.Err()
}

// --- ExplainQueryPlan (testing utility) ---

func (s *sqliteStore) ExplainQueryPlan(ctx context.Context, query string, args ...any) (string, error) {
	row := s.db.QueryRowContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
	var plan string
	var unused1, unused2 any
	if err := row.Scan(&unused1, &unused2, &unused1, &plan); err != nil {
		// Try alternate scan for different sqlite driver versions
		row2 := s.db.QueryRowContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
		if err2 := row2.Scan(&plan); err2 != nil {
			return "", fmt.Errorf("contextengine: explain: %w (original: %v)", err2, err)
		}
	}
	return plan, nil
}

// Errors.
var (
	ErrNotFound = errors.New("contextengine: not found")
)
