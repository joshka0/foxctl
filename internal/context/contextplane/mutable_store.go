package contextplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/joshka0/foxctl/internal/platform/timeutil"
	"github.com/joshka0/foxctl/internal/storage/dbutil"
)

const acaDBFile = "contextplane.db"

func (s *WorkspaceStore) openMutableDB(ctx context.Context) (*sql.DB, func() error, error) {
	if _, err := s.EnsureLayout(); err != nil {
		return nil, nil, err
	}
	db, closeFn, err := dbutil.OpenStoreDB(ctx, s.layout.RuntimeDir, "ACA", acaDBFile, migrateMutableStore)
	if err != nil {
		return nil, nil, fmt.Errorf("contextplane: open mutable db: %w", err)
	}
	if err := s.importLegacyMutableState(ctx, db); err != nil {
		_ = closeFn()
		return nil, nil, err
	}
	return db, closeFn, nil
}

func migrateMutableStore(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS aca_observations (
	id TEXT PRIMARY KEY,
	merge_key TEXT NOT NULL UNIQUE,
	statement TEXT NOT NULL,
	confidence REAL NOT NULL DEFAULT 0,
	count INTEGER NOT NULL DEFAULT 1,
	project TEXT,
	area TEXT,
	evidence_refs TEXT NOT NULL DEFAULT '[]',
	first_seen TEXT NOT NULL,
	last_seen TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aca_observations_last_seen ON aca_observations(last_seen DESC);

CREATE TABLE IF NOT EXISTS aca_tensions (
	id TEXT PRIMARY KEY,
	merge_key TEXT NOT NULL UNIQUE,
	kind TEXT NOT NULL,
	statement TEXT NOT NULL,
	impact TEXT NOT NULL,
	related_refs TEXT NOT NULL DEFAULT '[]',
	status TEXT NOT NULL,
	count INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL,
	last_seen TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aca_tensions_last_seen ON aca_tensions(last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_aca_tensions_status ON aca_tensions(status);

CREATE TABLE IF NOT EXISTS aca_promotion_jobs (
	id TEXT PRIMARY KEY,
	source_ref TEXT NOT NULL,
	source_kind TEXT NOT NULL,
	note_type TEXT NOT NULL,
	title TEXT NOT NULL,
	draft_path TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aca_promotion_jobs_created_at ON aca_promotion_jobs(created_at DESC);

CREATE TABLE IF NOT EXISTS aca_maintenance_tasks (
	id TEXT PRIMARY KEY,
	title TEXT NOT NULL,
	kind TEXT NOT NULL,
	priority INTEGER NOT NULL,
	reason TEXT NOT NULL,
	source_refs TEXT NOT NULL DEFAULT '[]',
	work_packet_json TEXT NOT NULL DEFAULT '{}',
	status TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aca_maintenance_tasks_priority ON aca_maintenance_tasks(priority DESC, created_at DESC);

CREATE TABLE IF NOT EXISTS aca_retrieval_correction_runs (
	id TEXT PRIMARY KEY,
	suite TEXT NOT NULL,
	control_suite TEXT,
	artifact_digest TEXT NOT NULL,
	summary_json TEXT NOT NULL,
	policy_candidate INTEGER NOT NULL DEFAULT 0,
	policy_applied INTEGER NOT NULL DEFAULT 0,
	policy_accepted INTEGER NOT NULL DEFAULT 0,
	policy_reverted INTEGER NOT NULL DEFAULT 0,
	draft_count INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aca_retrieval_correction_runs_created_at ON aca_retrieval_correction_runs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_aca_retrieval_correction_runs_suite ON aca_retrieval_correction_runs(suite, created_at DESC);

CREATE TABLE IF NOT EXISTS aca_graph_correction_runs (
	id TEXT PRIMARY KEY,
	method TEXT NOT NULL,
	suite TEXT NOT NULL,
	artifact_digest TEXT NOT NULL,
	queries INTEGER NOT NULL DEFAULT 0,
	matched INTEGER NOT NULL DEFAULT 0,
	misses INTEGER NOT NULL DEFAULT 0,
	classification TEXT,
	recommended_fix TEXT,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aca_graph_correction_runs_created_at ON aca_graph_correction_runs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_aca_graph_correction_runs_method ON aca_graph_correction_runs(method, created_at DESC);

CREATE TABLE IF NOT EXISTS aca_memory_proposals (
	id TEXT PRIMARY KEY,
	dedupe_key TEXT NOT NULL UNIQUE,
	kind TEXT NOT NULL,
	classification TEXT,
	status TEXT NOT NULL,
	review_required INTEGER NOT NULL DEFAULT 0,
	confidence REAL NOT NULL DEFAULT 0,
	blast_radius TEXT NOT NULL DEFAULT 'medium',
	summary TEXT NOT NULL,
	source_refs TEXT NOT NULL DEFAULT '[]',
	proposed_change_json TEXT NOT NULL DEFAULT '{}',
	evaluation_status TEXT NOT NULL DEFAULT 'not_evaluated',
	apply_status TEXT NOT NULL DEFAULT 'pending',
	count INTEGER NOT NULL DEFAULT 1,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aca_memory_proposals_updated_at ON aca_memory_proposals(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_aca_memory_proposals_status ON aca_memory_proposals(status, updated_at DESC);

CREATE TABLE IF NOT EXISTS aca_evidence_import_runs (
	id TEXT PRIMARY KEY,
	source_kind TEXT NOT NULL,
	source_ref TEXT NOT NULL,
	title TEXT NOT NULL,
	draft_path TEXT NOT NULL,
	artifact_digest TEXT,
	processor_kind TEXT,
	processor_model TEXT,
	summary TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aca_evidence_import_runs_created_at ON aca_evidence_import_runs(created_at DESC);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("contextplane: migrate mutable store: %w", err)
	}
	if err := dbutil.AddColumnIfNotExists(ctx, db, "aca_maintenance_tasks", "work_packet_json", "TEXT NOT NULL", "'{}'"); err != nil {
		return fmt.Errorf("contextplane: add work_packet_json column: %w", err)
	}
	return nil
}

func (s *WorkspaceStore) importLegacyMutableState(ctx context.Context, db *sql.DB) error {
	if err := importLegacyObservations(ctx, db, s.layout.ObservationsPath); err != nil {
		return err
	}
	if err := importLegacyTensions(ctx, db, s.layout.TensionsPath); err != nil {
		return err
	}
	if err := importLegacyPromotionJobs(ctx, db, s.layout.PromotionJobsPath); err != nil {
		return err
	}
	if err := importLegacyMaintenanceTasks(ctx, db, s.layout.MaintenanceQueuePath); err != nil {
		return err
	}
	return nil
}

func importLegacyObservations(ctx context.Context, db *sql.DB, path string) error {
	empty, err := tableEmpty(ctx, db, "aca_observations")
	if err != nil || !empty {
		return err
	}
	items, err := readNDJSONFile[Observation](path, 0, nil)
	if err != nil {
		if strings.Contains(err.Error(), "no such file") {
			return nil
		}
		return err
	}
	for _, item := range items {
		if err := upsertObservationRow(ctx, db, item); err != nil {
			return err
		}
	}
	return nil
}

func importLegacyTensions(ctx context.Context, db *sql.DB, path string) error {
	empty, err := tableEmpty(ctx, db, "aca_tensions")
	if err != nil || !empty {
		return err
	}
	items, err := readNDJSONFile[Tension](path, 0, nil)
	if err != nil {
		if strings.Contains(err.Error(), "no such file") {
			return nil
		}
		return err
	}
	for _, item := range items {
		if err := upsertTensionRow(ctx, db, item); err != nil {
			return err
		}
	}
	return nil
}

func importLegacyPromotionJobs(ctx context.Context, db *sql.DB, path string) error {
	empty, err := tableEmpty(ctx, db, "aca_promotion_jobs")
	if err != nil || !empty {
		return err
	}
	items, err := readNDJSONFile[PromotionJob](path, 0, nil)
	if err != nil {
		if strings.Contains(err.Error(), "no such file") {
			return nil
		}
		return err
	}
	for _, item := range items {
		if err := insertPromotionJobRow(ctx, db, item); err != nil {
			return err
		}
	}
	return nil
}

func importLegacyMaintenanceTasks(ctx context.Context, db *sql.DB, path string) error {
	empty, err := tableEmpty(ctx, db, "aca_maintenance_tasks")
	if err != nil || !empty {
		return err
	}
	items, err := readNDJSONFile[MaintenanceTask](path, 0, nil)
	if err != nil {
		if strings.Contains(err.Error(), "no such file") {
			return nil
		}
		return err
	}
	for _, item := range items {
		if err := insertMaintenanceTaskRow(ctx, db, item); err != nil {
			return err
		}
	}
	return nil
}

func tableEmpty(ctx context.Context, db *sql.DB, table string) (bool, error) {
	switch table {
	case "aca_observations", "aca_tensions", "aca_promotion_jobs", "aca_maintenance_tasks", "aca_retrieval_correction_runs", "aca_graph_correction_runs", "aca_memory_proposals", "aca_evidence_import_runs":
	default:
		return false, fmt.Errorf("contextplane: unsupported table %q", table)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func upsertObservationRow(ctx context.Context, db *sql.DB, obs Observation) error {
	now := timeutil.NowUTC()
	if obs.ID == "" {
		obs.ID = buildRecordID("O", now)
	}
	if obs.Count <= 0 {
		obs.Count = 1
	}
	if obs.FirstSeen.IsZero() {
		obs.FirstSeen = now
	}
	if obs.LastSeen.IsZero() {
		obs.LastSeen = obs.FirstSeen
	}
	evidenceJSON, err := json.Marshal(uniqueStrings(obs.EvidenceRefs))
	if err != nil {
		return fmt.Errorf("marshal observation refs: %w", err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO aca_observations (id, merge_key, statement, confidence, count, project, area, evidence_refs, first_seen, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(merge_key) DO UPDATE SET
	confidence = CASE WHEN excluded.confidence > aca_observations.confidence THEN excluded.confidence ELSE aca_observations.confidence END,
	count = aca_observations.count + excluded.count,
	evidence_refs = CASE
		WHEN aca_observations.evidence_refs = '[]' THEN excluded.evidence_refs
		WHEN excluded.evidence_refs = '[]' THEN aca_observations.evidence_refs
		ELSE aca_observations.evidence_refs
	END,
	first_seen = CASE
		WHEN aca_observations.first_seen = '' OR aca_observations.first_seen > excluded.first_seen THEN excluded.first_seen
		ELSE aca_observations.first_seen
	END,
	last_seen = CASE
		WHEN aca_observations.last_seen < excluded.last_seen THEN excluded.last_seen
		ELSE aca_observations.last_seen
	END
`,
		obs.ID, observationKey(obs), obs.Statement, obs.Confidence, obs.Count, nullableString(obs.Project), nullableString(obs.Area), string(evidenceJSON),
		timeutil.FormatRFC3339Nano(obs.FirstSeen), timeutil.FormatRFC3339Nano(obs.LastSeen),
	)
	return err
}

func upsertTensionRow(ctx context.Context, db *sql.DB, tension Tension) error {
	now := timeutil.NowUTC()
	if tension.ID == "" {
		tension.ID = buildRecordID("X", now)
	}
	if tension.CreatedAt.IsZero() {
		tension.CreatedAt = now
	}
	if tension.LastSeen.IsZero() {
		tension.LastSeen = tension.CreatedAt
	}
	if tension.Count <= 0 {
		tension.Count = 1
	}
	relatedJSON, err := json.Marshal(uniqueStrings(tension.RelatedRefs))
	if err != nil {
		return fmt.Errorf("marshal tension refs: %w", err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO aca_tensions (id, merge_key, kind, statement, impact, related_refs, status, count, created_at, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(merge_key) DO UPDATE SET
	count = aca_tensions.count + excluded.count,
	impact = CASE
		WHEN aca_tensions.impact = 'high' THEN aca_tensions.impact
		WHEN excluded.impact = 'high' THEN excluded.impact
		WHEN aca_tensions.impact = 'medium' THEN aca_tensions.impact
		WHEN excluded.impact = 'medium' THEN excluded.impact
		ELSE aca_tensions.impact
	END,
	created_at = CASE
		WHEN aca_tensions.created_at = '' OR aca_tensions.created_at > excluded.created_at THEN excluded.created_at
		ELSE aca_tensions.created_at
	END,
	last_seen = CASE
		WHEN aca_tensions.last_seen < excluded.last_seen THEN excluded.last_seen
		ELSE aca_tensions.last_seen
	END
`,
		tension.ID, tensionKey(tension), tension.Kind, tension.Statement, tension.Impact, string(relatedJSON), tension.Status, tension.Count,
		timeutil.FormatRFC3339Nano(tension.CreatedAt), timeutil.FormatRFC3339Nano(tension.LastSeen),
	)
	return err
}

func insertPromotionJobRow(ctx context.Context, db *sql.DB, job PromotionJob) error {
	if job.CreatedAt.IsZero() {
		job.CreatedAt = timeutil.NowUTC()
	}
	if job.ID == "" {
		job.ID = buildRecordID("P", job.CreatedAt)
	}
	_, err := db.ExecContext(ctx, `
INSERT OR IGNORE INTO aca_promotion_jobs (id, source_ref, source_kind, note_type, title, draft_path, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, job.ID, job.SourceRef, job.SourceKind, job.NoteType, job.Title, job.DraftPath, job.Status, timeutil.FormatRFC3339Nano(job.CreatedAt))
	return err
}

func ensurePromotionJobRow(ctx context.Context, db *sql.DB, sourceRef, sourceKind, noteType, title, draftPath string) (PromotionJob, error) {
	existing, err := findPromotionJobRow(ctx, db, draftPath)
	if err != nil {
		return PromotionJob{}, err
	}
	if existing != nil {
		return *existing, nil
	}
	now := timeutil.NowUTC()
	job := PromotionJob{
		ID:         buildRecordID("P", now),
		SourceRef:  strings.TrimSpace(sourceRef),
		SourceKind: strings.TrimSpace(sourceKind),
		NoteType:   strings.TrimSpace(noteType),
		Title:      strings.TrimSpace(title),
		DraftPath:  strings.TrimSpace(draftPath),
		Status:     "drafted",
		CreatedAt:  now,
	}
	if job.SourceRef == "" {
		job.SourceRef = "draft:" + job.DraftPath
	}
	if job.SourceKind == "" {
		job.SourceKind = "draft"
	}
	if job.NoteType == "" {
		job.NoteType = "evidence"
	}
	if job.Title == "" {
		job.Title = filepath.Base(job.DraftPath)
	}
	if err := insertPromotionJobRow(ctx, db, job); err != nil {
		return PromotionJob{}, err
	}
	return job, nil
}

func insertMaintenanceTaskRow(ctx context.Context, db *sql.DB, task MaintenanceTask) error {
	if task.CreatedAt.IsZero() {
		task.CreatedAt = timeutil.NowUTC()
	}
	if task.ID == "" {
		task.ID = buildRecordID("M", task.CreatedAt)
	}
	sourceJSON, err := json.Marshal(uniqueStrings(task.SourceRefs))
	if err != nil {
		return fmt.Errorf("marshal maintenance refs: %w", err)
	}
	workPacketJSON, err := json.Marshal(task.WorkPacket)
	if err != nil {
		return fmt.Errorf("marshal maintenance work packet: %w", err)
	}
	_, err = db.ExecContext(ctx, `
INSERT OR REPLACE INTO aca_maintenance_tasks (id, title, kind, priority, reason, source_refs, work_packet_json, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, task.ID, task.Title, task.Kind, task.Priority, task.Reason, string(sourceJSON), string(workPacketJSON), task.Status, timeutil.FormatRFC3339Nano(task.CreatedAt))
	return err
}

func nullableString(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return s
}

func listObservationRows(ctx context.Context, db *sql.DB, limit int) ([]Observation, error) {
	query := `
SELECT id, statement, confidence, count, project, area, evidence_refs, first_seen, last_seen
FROM aca_observations
ORDER BY last_seen DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.QueryContext(ctx, query+` LIMIT ?`, limit)
	} else {
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("query observations: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Observation
	for rows.Next() {
		item, err := scanObservationRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func listTensionRows(ctx context.Context, db *sql.DB, limit int) ([]Tension, error) {
	query := `
SELECT id, kind, statement, impact, related_refs, status, count, created_at, last_seen
FROM aca_tensions
ORDER BY created_at DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.QueryContext(ctx, query+` LIMIT ?`, limit)
	} else {
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("query tensions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Tension
	for rows.Next() {
		item, err := scanTensionRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func findPromotableObservationRow(ctx context.Context, db *sql.DB, id string) (*Observation, error) {
	base := `
SELECT id, statement, confidence, count, project, area, evidence_refs, first_seen, last_seen
FROM aca_observations`
	var row *sql.Row
	if strings.TrimSpace(id) != "" {
		row = db.QueryRowContext(ctx, base+` WHERE id = ? LIMIT 1`, id)
	} else {
		row = db.QueryRowContext(ctx, base+` WHERE count >= 2 ORDER BY count DESC, last_seen DESC LIMIT 1`)
	}
	item, err := scanObservationRow(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func insertPromotionJobAndDraft(ctx context.Context, db *sql.DB, job PromotionJob) error {
	return insertPromotionJobRow(ctx, db, job)
}

func listPromotionJobRows(ctx context.Context, db *sql.DB, limit int) ([]PromotionJob, error) {
	query := `
SELECT id, source_ref, source_kind, note_type, title, draft_path, status, created_at
FROM aca_promotion_jobs
ORDER BY created_at DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.QueryContext(ctx, query+` LIMIT ?`, limit)
	} else {
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("query promotion jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []PromotionJob
	for rows.Next() {
		item, err := scanPromotionJobRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func findPromotionJobRow(ctx context.Context, db *sql.DB, draftPath string) (*PromotionJob, error) {
	draftPath = strings.TrimSpace(draftPath)
	if draftPath != "" {
		row := db.QueryRowContext(ctx, `
SELECT id, source_ref, source_kind, note_type, title, draft_path, status, created_at
FROM aca_promotion_jobs
WHERE draft_path = ?
LIMIT 1
`, draftPath)
		item, err := scanPromotionJobRow(row)
		if err != nil {
			if dbutil.IsNoRows(err) {
				return nil, nil
			}
			return nil, err
		}
		return &item, nil
	}
	row := db.QueryRowContext(ctx, `
SELECT id, source_ref, source_kind, note_type, title, draft_path, status, created_at
FROM aca_promotion_jobs
WHERE status = 'drafted'
ORDER BY created_at DESC
LIMIT 1
`)
	item, err := scanPromotionJobRow(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func updatePromotionJobStatusByDraftPath(ctx context.Context, db *sql.DB, draftPath, status string) error {
	_, err := db.ExecContext(ctx, `
UPDATE aca_promotion_jobs
SET status = ?
WHERE draft_path = ?
`, status, draftPath)
	return err
}

func replaceMaintenanceTaskRows(ctx context.Context, db *sql.DB, tasks []MaintenanceTask) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin maintenance tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM aca_maintenance_tasks`); err != nil {
		return fmt.Errorf("clear maintenance tasks: %w", err)
	}
	for _, task := range tasks {
		sourceJSON, err := json.Marshal(uniqueStrings(task.SourceRefs))
		if err != nil {
			return fmt.Errorf("marshal maintenance refs: %w", err)
		}
		workPacketJSON, err := json.Marshal(task.WorkPacket)
		if err != nil {
			return fmt.Errorf("marshal maintenance work packet: %w", err)
		}
		if task.CreatedAt.IsZero() {
			task.CreatedAt = timeutil.NowUTC()
		}
		if task.ID == "" {
			task.ID = buildRecordID("M", task.CreatedAt)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO aca_maintenance_tasks (id, title, kind, priority, reason, source_refs, work_packet_json, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, task.ID, task.Title, task.Kind, task.Priority, task.Reason, string(sourceJSON), string(workPacketJSON), task.Status, timeutil.FormatRFC3339Nano(task.CreatedAt)); err != nil {
			return fmt.Errorf("insert maintenance task: %w", err)
		}
	}
	return tx.Commit()
}

func listMaintenanceTaskRows(ctx context.Context, db *sql.DB, limit int) ([]MaintenanceTask, error) {
	query := `
SELECT id, title, kind, priority, reason, source_refs, work_packet_json, status, created_at
FROM aca_maintenance_tasks
ORDER BY priority DESC, created_at DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.QueryContext(ctx, query+` LIMIT ?`, limit)
	} else {
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("query maintenance tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []MaintenanceTask
	for rows.Next() {
		item, err := scanMaintenanceTaskRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func insertRetrievalCorrectionRunRow(ctx context.Context, db *sql.DB, run RetrievalCorrectionRun) error {
	summaryJSON, err := json.Marshal(run.Summary)
	if err != nil {
		return fmt.Errorf("marshal retrieval correction summary: %w", err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO aca_retrieval_correction_runs (
	id, suite, control_suite, artifact_digest, summary_json,
	policy_candidate, policy_applied, policy_accepted, policy_reverted, draft_count, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		run.ID,
		run.Suite,
		nullableString(run.ControlSuite),
		run.ArtifactDigest,
		string(summaryJSON),
		boolToInt(run.PolicyCandidate),
		boolToInt(run.PolicyApplied),
		boolToInt(run.PolicyAccepted),
		boolToInt(run.PolicyReverted),
		run.DraftCount,
		timeutil.FormatRFC3339Nano(run.CreatedAt),
	)
	return err
}

func listRetrievalCorrectionRunRows(ctx context.Context, db *sql.DB, limit int) ([]RetrievalCorrectionRun, error) {
	query := `
SELECT id, suite, control_suite, artifact_digest, summary_json,
       policy_candidate, policy_applied, policy_accepted, policy_reverted, draft_count, created_at
FROM aca_retrieval_correction_runs
ORDER BY created_at DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.QueryContext(ctx, query+` LIMIT ?`, limit)
	} else {
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("query retrieval correction runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RetrievalCorrectionRun
	for rows.Next() {
		item, err := scanRetrievalCorrectionRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func findRetrievalCorrectionRunRow(ctx context.Context, db *sql.DB, id string) (*RetrievalCorrectionRun, error) {
	row := db.QueryRowContext(ctx, `
SELECT id, suite, control_suite, artifact_digest, summary_json,
       policy_candidate, policy_applied, policy_accepted, policy_reverted, draft_count, created_at
FROM aca_retrieval_correction_runs
WHERE id = ?
LIMIT 1
`, strings.TrimSpace(id))
	item, err := scanRetrievalCorrectionRunRow(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func insertGraphCorrectionRunRow(ctx context.Context, db *sql.DB, run GraphCorrectionRun) error {
	_, err := db.ExecContext(ctx, `
INSERT INTO aca_graph_correction_runs (
	id, method, suite, artifact_digest, queries, matched, misses, classification, recommended_fix, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		run.ID,
		run.Method,
		run.Suite,
		run.ArtifactDigest,
		run.Queries,
		run.Matched,
		run.Misses,
		nullableString(run.Classification),
		nullableString(run.RecommendedFix),
		timeutil.FormatRFC3339Nano(run.CreatedAt),
	)
	return err
}

func listGraphCorrectionRunRows(ctx context.Context, db *sql.DB, limit int) ([]GraphCorrectionRun, error) {
	query := `
SELECT id, method, suite, artifact_digest, queries, matched, misses, classification, recommended_fix, created_at
FROM aca_graph_correction_runs
ORDER BY created_at DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.QueryContext(ctx, query+` LIMIT ?`, limit)
	} else {
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("query graph correction runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []GraphCorrectionRun
	for rows.Next() {
		item, err := scanGraphCorrectionRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func findGraphCorrectionRunRow(ctx context.Context, db *sql.DB, id string) (*GraphCorrectionRun, error) {
	row := db.QueryRowContext(ctx, `
SELECT id, method, suite, artifact_digest, queries, matched, misses, classification, recommended_fix, created_at
FROM aca_graph_correction_runs
WHERE id = ?
LIMIT 1
`, strings.TrimSpace(id))
	item, err := scanGraphCorrectionRunRow(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func upsertMemoryProposalRow(ctx context.Context, db *sql.DB, proposal MemoryProposal) error {
	now := timeutil.NowUTC()
	if proposal.CreatedAt.IsZero() {
		proposal.CreatedAt = now
	}
	if proposal.UpdatedAt.IsZero() {
		proposal.UpdatedAt = proposal.CreatedAt
	}
	if proposal.ID == "" {
		proposal.ID = buildRecordID("Q", proposal.CreatedAt)
	}
	if proposal.Count <= 0 {
		proposal.Count = 1
	}
	sourceJSON, err := json.Marshal(uniqueStrings(proposal.SourceRefs))
	if err != nil {
		return fmt.Errorf("marshal proposal source refs: %w", err)
	}
	changeJSON, err := json.Marshal(proposal.ProposedChange)
	if err != nil {
		return fmt.Errorf("marshal proposal change: %w", err)
	}
	_, err = db.ExecContext(ctx, `
INSERT INTO aca_memory_proposals (
	id, dedupe_key, kind, classification, status, review_required, confidence,
	blast_radius, summary, source_refs, proposed_change_json, evaluation_status,
	apply_status, count, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(dedupe_key) DO UPDATE SET
	status = 'open',
	review_required = CASE
		WHEN excluded.review_required != 0 THEN excluded.review_required
		ELSE aca_memory_proposals.review_required
	END,
	confidence = CASE
		WHEN excluded.confidence > aca_memory_proposals.confidence THEN excluded.confidence
		ELSE aca_memory_proposals.confidence
	END,
	blast_radius = excluded.blast_radius,
	summary = excluded.summary,
	source_refs = CASE
		WHEN aca_memory_proposals.source_refs = '[]' THEN excluded.source_refs
		ELSE aca_memory_proposals.source_refs
	END,
	proposed_change_json = excluded.proposed_change_json,
	evaluation_status = excluded.evaluation_status,
	apply_status = 'pending',
	count = aca_memory_proposals.count + excluded.count,
	updated_at = excluded.updated_at
`,
		proposal.ID,
		effectiveMemoryProposalKey(proposal),
		proposal.Kind,
		nullableString(proposal.Classification),
		proposal.Status,
		boolToInt(proposal.ReviewRequired),
		proposal.Confidence,
		firstNonEmpty(strings.TrimSpace(proposal.BlastRadius), "medium"),
		proposal.Summary,
		string(sourceJSON),
		string(changeJSON),
		firstNonEmpty(strings.TrimSpace(proposal.EvaluationStatus), "not_evaluated"),
		firstNonEmpty(strings.TrimSpace(proposal.ApplyStatus), "pending"),
		proposal.Count,
		timeutil.FormatRFC3339Nano(proposal.CreatedAt),
		timeutil.FormatRFC3339Nano(proposal.UpdatedAt),
	)
	return err
}

func listMemoryProposalRows(ctx context.Context, db *sql.DB, limit int) ([]MemoryProposal, error) {
	query := `
SELECT id, kind, classification, status, review_required, confidence, blast_radius,
       dedupe_key, summary, source_refs, proposed_change_json, evaluation_status, apply_status,
       count, created_at, updated_at
FROM aca_memory_proposals
ORDER BY updated_at DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.QueryContext(ctx, query+` LIMIT ?`, limit)
	} else {
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("query memory proposals: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []MemoryProposal
	for rows.Next() {
		item, err := scanMemoryProposalRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func findMemoryProposalRow(ctx context.Context, db *sql.DB, id string) (*MemoryProposal, error) {
	row := db.QueryRowContext(ctx, `
SELECT id, kind, classification, status, review_required, confidence, blast_radius,
       dedupe_key, summary, source_refs, proposed_change_json, evaluation_status, apply_status,
       count, created_at, updated_at
FROM aca_memory_proposals
WHERE id = ?
LIMIT 1
`, strings.TrimSpace(id))
	item, err := scanMemoryProposalRow(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func findMemoryProposalRowByKey(ctx context.Context, db *sql.DB, key string) (*MemoryProposal, error) {
	row := db.QueryRowContext(ctx, `
SELECT id, kind, classification, status, review_required, confidence, blast_radius,
       dedupe_key, summary, source_refs, proposed_change_json, evaluation_status, apply_status,
       count, created_at, updated_at
FROM aca_memory_proposals
WHERE dedupe_key = ?
LIMIT 1
`, strings.TrimSpace(key))
	item, err := scanMemoryProposalRow(row)
	if err != nil {
		if dbutil.IsNoRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func updateMemoryProposalRowStatus(ctx context.Context, db *sql.DB, id, status, evaluationStatus, applyStatus string) error {
	_, err := db.ExecContext(ctx, `
UPDATE aca_memory_proposals
SET status = ?, evaluation_status = ?, apply_status = ?, updated_at = ?
WHERE id = ?
`,
		status,
		firstNonEmpty(strings.TrimSpace(evaluationStatus), "not_evaluated"),
		firstNonEmpty(strings.TrimSpace(applyStatus), "pending"),
		timeutil.FormatRFC3339Nano(timeutil.NowUTC()),
		strings.TrimSpace(id),
	)
	return err
}

func updateMaintenanceTaskStatus(ctx context.Context, db *sql.DB, id, status string) error {
	_, err := db.ExecContext(ctx, `
UPDATE aca_maintenance_tasks
SET status = ?
WHERE id = ?
`,
		strings.TrimSpace(status),
		strings.TrimSpace(id),
	)
	return err
}

func insertEvidenceImportRunRow(ctx context.Context, db *sql.DB, run EvidenceImportRun) error {
	if run.CreatedAt.IsZero() {
		run.CreatedAt = timeutil.NowUTC()
	}
	if run.ID == "" {
		run.ID = buildRecordID("E", run.CreatedAt)
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO aca_evidence_import_runs (
	id, source_kind, source_ref, title, draft_path, artifact_digest,
	processor_kind, processor_model, summary, status, created_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		run.ID,
		run.SourceKind,
		run.SourceRef,
		run.Title,
		run.DraftPath,
		nullableString(run.ArtifactDigest),
		nullableString(run.ProcessorKind),
		nullableString(run.ProcessorModel),
		run.Summary,
		run.Status,
		timeutil.FormatRFC3339Nano(run.CreatedAt),
	)
	return err
}

func listEvidenceImportRunRows(ctx context.Context, db *sql.DB, limit int) ([]EvidenceImportRun, error) {
	query := `
SELECT id, source_kind, source_ref, title, draft_path, artifact_digest,
	processor_kind, processor_model, summary, status, created_at
FROM aca_evidence_import_runs
ORDER BY created_at DESC`
	var rows *sql.Rows
	var err error
	if limit > 0 {
		rows, err = db.QueryContext(ctx, query+` LIMIT ?`, limit)
	} else {
		rows, err = db.QueryContext(ctx, query)
	}
	if err != nil {
		return nil, fmt.Errorf("query evidence import runs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []EvidenceImportRun
	for rows.Next() {
		item, err := scanEvidenceImportRunRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanObservationRow(scanner interface{ Scan(dest ...any) error }) (Observation, error) {
	var item Observation
	var project, area sql.NullString
	var evidenceJSON string
	var firstSeen, lastSeen string
	if err := scanner.Scan(&item.ID, &item.Statement, &item.Confidence, &item.Count, &project, &area, &evidenceJSON, &firstSeen, &lastSeen); err != nil {
		return Observation{}, fmt.Errorf("scan observation: %w", err)
	}
	if project.Valid {
		item.Project = project.String
	}
	if area.Valid {
		item.Area = area.String
	}
	if evidenceJSON != "" {
		if err := json.Unmarshal([]byte(evidenceJSON), &item.EvidenceRefs); err != nil {
			return Observation{}, fmt.Errorf("decode observation refs: %w", err)
		}
	}
	item.FirstSeen = timeutil.MustParseRFC3339Nano(firstSeen)
	item.LastSeen = timeutil.MustParseRFC3339Nano(lastSeen)
	return item, nil
}

func scanTensionRow(scanner interface{ Scan(dest ...any) error }) (Tension, error) {
	var item Tension
	var refsJSON string
	var createdAt, lastSeen string
	if err := scanner.Scan(&item.ID, &item.Kind, &item.Statement, &item.Impact, &refsJSON, &item.Status, &item.Count, &createdAt, &lastSeen); err != nil {
		return Tension{}, fmt.Errorf("scan tension: %w", err)
	}
	if refsJSON != "" {
		if err := json.Unmarshal([]byte(refsJSON), &item.RelatedRefs); err != nil {
			return Tension{}, fmt.Errorf("decode tension refs: %w", err)
		}
	}
	item.CreatedAt = timeutil.MustParseRFC3339Nano(createdAt)
	item.LastSeen = timeutil.MustParseRFC3339Nano(lastSeen)
	return item, nil
}

func scanMaintenanceTaskRow(scanner interface{ Scan(dest ...any) error }) (MaintenanceTask, error) {
	var item MaintenanceTask
	var refsJSON string
	var workPacketJSON string
	var createdAt string
	if err := scanner.Scan(&item.ID, &item.Title, &item.Kind, &item.Priority, &item.Reason, &refsJSON, &workPacketJSON, &item.Status, &createdAt); err != nil {
		return MaintenanceTask{}, fmt.Errorf("scan maintenance task: %w", err)
	}
	if refsJSON != "" {
		if err := json.Unmarshal([]byte(refsJSON), &item.SourceRefs); err != nil {
			return MaintenanceTask{}, fmt.Errorf("decode maintenance refs: %w", err)
		}
	}
	if strings.TrimSpace(workPacketJSON) != "" && strings.TrimSpace(workPacketJSON) != "null" && strings.TrimSpace(workPacketJSON) != "{}" {
		var packet ProposalWorkPacket
		if err := json.Unmarshal([]byte(workPacketJSON), &packet); err != nil {
			return MaintenanceTask{}, fmt.Errorf("decode maintenance work packet: %w", err)
		}
		item.WorkPacket = &packet
	}
	item.CreatedAt = timeutil.MustParseRFC3339Nano(createdAt)
	return item, nil
}

func scanRetrievalCorrectionRunRow(scanner interface{ Scan(dest ...any) error }) (RetrievalCorrectionRun, error) {
	var item RetrievalCorrectionRun
	var controlSuite sql.NullString
	var summaryJSON string
	var policyCandidate, policyApplied, policyAccepted, policyReverted int
	var createdAt string
	if err := scanner.Scan(
		&item.ID,
		&item.Suite,
		&controlSuite,
		&item.ArtifactDigest,
		&summaryJSON,
		&policyCandidate,
		&policyApplied,
		&policyAccepted,
		&policyReverted,
		&item.DraftCount,
		&createdAt,
	); err != nil {
		return RetrievalCorrectionRun{}, fmt.Errorf("scan retrieval correction run: %w", err)
	}
	if controlSuite.Valid {
		item.ControlSuite = controlSuite.String
	}
	if summaryJSON != "" {
		if err := json.Unmarshal([]byte(summaryJSON), &item.Summary); err != nil {
			return RetrievalCorrectionRun{}, fmt.Errorf("decode retrieval correction summary: %w", err)
		}
	}
	item.PolicyCandidate = policyCandidate != 0
	item.PolicyApplied = policyApplied != 0
	item.PolicyAccepted = policyAccepted != 0
	item.PolicyReverted = policyReverted != 0
	item.CreatedAt = timeutil.MustParseRFC3339Nano(createdAt)
	return item, nil
}

func scanGraphCorrectionRunRow(scanner interface{ Scan(dest ...any) error }) (GraphCorrectionRun, error) {
	var item GraphCorrectionRun
	var classification, recommendedFix sql.NullString
	var createdAt string
	if err := scanner.Scan(
		&item.ID,
		&item.Method,
		&item.Suite,
		&item.ArtifactDigest,
		&item.Queries,
		&item.Matched,
		&item.Misses,
		&classification,
		&recommendedFix,
		&createdAt,
	); err != nil {
		return GraphCorrectionRun{}, fmt.Errorf("scan graph correction run: %w", err)
	}
	if classification.Valid {
		item.Classification = classification.String
	}
	if recommendedFix.Valid {
		item.RecommendedFix = recommendedFix.String
	}
	item.CreatedAt = timeutil.MustParseRFC3339Nano(createdAt)
	return item, nil
}

func scanPromotionJobRow(scanner interface{ Scan(dest ...any) error }) (PromotionJob, error) {
	var item PromotionJob
	var createdAt string
	if err := scanner.Scan(&item.ID, &item.SourceRef, &item.SourceKind, &item.NoteType, &item.Title, &item.DraftPath, &item.Status, &createdAt); err != nil {
		return PromotionJob{}, fmt.Errorf("scan promotion job: %w", err)
	}
	item.CreatedAt = timeutil.MustParseRFC3339Nano(createdAt)
	return item, nil
}

func scanMemoryProposalRow(scanner interface{ Scan(dest ...any) error }) (MemoryProposal, error) {
	var item MemoryProposal
	var classification sql.NullString
	var sourceJSON string
	var changeJSON string
	var createdAt string
	var updatedAt string
	var reviewRequired int
	if err := scanner.Scan(
		&item.ID,
		&item.Kind,
		&classification,
		&item.Status,
		&reviewRequired,
		&item.Confidence,
		&item.BlastRadius,
		&item.DedupeKey,
		&item.Summary,
		&sourceJSON,
		&changeJSON,
		&item.EvaluationStatus,
		&item.ApplyStatus,
		&item.Count,
		&createdAt,
		&updatedAt,
	); err != nil {
		return MemoryProposal{}, fmt.Errorf("scan memory proposal: %w", err)
	}
	if classification.Valid {
		item.Classification = classification.String
	}
	if sourceJSON != "" {
		if err := json.Unmarshal([]byte(sourceJSON), &item.SourceRefs); err != nil {
			return MemoryProposal{}, fmt.Errorf("decode memory proposal refs: %w", err)
		}
	}
	if changeJSON != "" {
		if err := json.Unmarshal([]byte(changeJSON), &item.ProposedChange); err != nil {
			return MemoryProposal{}, fmt.Errorf("decode memory proposal change: %w", err)
		}
	}
	item.ReviewRequired = reviewRequired != 0
	item.CreatedAt = timeutil.MustParseRFC3339Nano(createdAt)
	item.UpdatedAt = timeutil.MustParseRFC3339Nano(updatedAt)
	return item, nil
}

func scanEvidenceImportRunRow(scanner interface{ Scan(dest ...any) error }) (EvidenceImportRun, error) {
	var item EvidenceImportRun
	var artifactDigest sql.NullString
	var processorKind sql.NullString
	var processorModel sql.NullString
	var createdAt string
	if err := scanner.Scan(
		&item.ID,
		&item.SourceKind,
		&item.SourceRef,
		&item.Title,
		&item.DraftPath,
		&artifactDigest,
		&processorKind,
		&processorModel,
		&item.Summary,
		&item.Status,
		&createdAt,
	); err != nil {
		return EvidenceImportRun{}, fmt.Errorf("scan evidence import run: %w", err)
	}
	if artifactDigest.Valid {
		item.ArtifactDigest = artifactDigest.String
	}
	if processorKind.Valid {
		item.ProcessorKind = processorKind.String
	}
	if processorModel.Valid {
		item.ProcessorModel = processorModel.String
	}
	item.CreatedAt = timeutil.MustParseRFC3339Nano(createdAt)
	return item, nil
}
