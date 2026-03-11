package contextplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
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
	status TEXT NOT NULL,
	created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aca_maintenance_tasks_priority ON aca_maintenance_tasks(priority DESC, created_at DESC);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("contextplane: migrate mutable store: %w", err)
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
	case "aca_observations", "aca_tensions", "aca_promotion_jobs", "aca_maintenance_tasks":
	default:
		return false, fmt.Errorf("contextplane: unsupported table %q", table)
	}
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil {
		return false, err
	}
	return count == 0, nil
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
	_, err = db.ExecContext(ctx, `
INSERT OR REPLACE INTO aca_maintenance_tasks (id, title, kind, priority, reason, source_refs, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, task.ID, task.Title, task.Kind, task.Priority, task.Reason, string(sourceJSON), task.Status, timeutil.FormatRFC3339Nano(task.CreatedAt))
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
		if task.CreatedAt.IsZero() {
			task.CreatedAt = timeutil.NowUTC()
		}
		if task.ID == "" {
			task.ID = buildRecordID("M", task.CreatedAt)
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO aca_maintenance_tasks (id, title, kind, priority, reason, source_refs, status, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, task.ID, task.Title, task.Kind, task.Priority, task.Reason, string(sourceJSON), task.Status, timeutil.FormatRFC3339Nano(task.CreatedAt)); err != nil {
			return fmt.Errorf("insert maintenance task: %w", err)
		}
	}
	return tx.Commit()
}

func listMaintenanceTaskRows(ctx context.Context, db *sql.DB, limit int) ([]MaintenanceTask, error) {
	query := `
SELECT id, title, kind, priority, reason, source_refs, status, created_at
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
	var createdAt string
	if err := scanner.Scan(&item.ID, &item.Title, &item.Kind, &item.Priority, &item.Reason, &refsJSON, &item.Status, &createdAt); err != nil {
		return MaintenanceTask{}, fmt.Errorf("scan maintenance task: %w", err)
	}
	if refsJSON != "" {
		if err := json.Unmarshal([]byte(refsJSON), &item.SourceRefs); err != nil {
			return MaintenanceTask{}, fmt.Errorf("decode maintenance refs: %w", err)
		}
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
