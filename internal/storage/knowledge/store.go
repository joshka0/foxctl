// Package knowledge implements SQLite-backed persistence for the knowledge registry.
// This stores Claude-facing knowledge packs, agents, and commands with their triggers.
package knowledge

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/storage/dbutil"
	"github.com/jkatigb/agentctl/internal/storage/sqliteutil"
	"github.com/oklog/ulid/v2"
)

// ItemKind represents the type of knowledge item.
type ItemKind string

// ItemKind constants.
// These constants represent the different types of knowledge items.
const (
	KindPack    ItemKind = "pack"
	KindAgent   ItemKind = "agent"
	KindCommand ItemKind = "command"
)

// TriggerKind represents the type of trigger rule.
type TriggerKind string

// TriggerKind constants.
// These constants represent the different types of trigger rules.
const (
	TriggerKeyword TriggerKind = "keyword"
	TriggerIntent  TriggerKind = "intent"
	TriggerPath    TriggerKind = "path"
	TriggerContent TriggerKind = "content"
)

// Item represents a knowledge item (pack, agent, or command).
type Item struct {
	ID          string
	Name        string
	Kind        ItemKind
	Description string
	SourcePath  string
	Priority    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Trigger represents a rule that activates a knowledge item.
type Trigger struct {
	ID          string
	ItemID      string
	TriggerKind TriggerKind
	Pattern     string
}

// Document represents a text chunk associated with a knowledge item.
type Document struct {
	ID         string
	ItemID     string
	Title      string
	SourcePath string
	Body       string
	BodyDigest string // sha256:<hex> if stored in CAS
	CreatedAt  time.Time
}

// Store defines the persistence interface for the knowledge registry.
type Store interface {
	Close() error

	// Item operations
	UpsertItem(ctx context.Context, item Item) (Item, error)
	GetItem(ctx context.Context, id string) (Item, error)
	GetItemByName(ctx context.Context, name string) (Item, bool, error)
	ListItems(ctx context.Context, kind ItemKind) ([]Item, error)
	ListAllItems(ctx context.Context) ([]Item, error)
	DeleteItem(ctx context.Context, id string) error

	// Trigger operations
	AddTrigger(ctx context.Context, t Trigger) (Trigger, error)
	ListTriggers(ctx context.Context, itemID string) ([]Trigger, error)
	ListAllTriggers(ctx context.Context) ([]Trigger, error)
	DeleteTriggersForItem(ctx context.Context, itemID string) error

	// Document operations
	UpsertDocument(ctx context.Context, doc Document) (Document, error)
	ListDocuments(ctx context.Context, itemID string) ([]Document, error)
	DeleteDocumentsForItem(ctx context.Context, itemID string) error

	// Search operations
	MatchByKeyword(ctx context.Context, keywords []string) ([]Item, error)
	MatchByPath(ctx context.Context, path string) ([]Item, error)
}

type sqlStore struct {
	db *sql.DB
}

// Open initializes the knowledge store rooted at the provided path.
func Open(ctx context.Context, root string) (Store, error) {
	dbPath := filepath.Join(root, "knowledge.db")
	db, err := sqliteutil.OpenDB(ctx, dbPath, migrate)
	if err != nil {
		return nil, fmt.Errorf("knowledge: open db: %w", err)
	}
	return &sqlStore{db: db}, nil
}

// Close releases database resources.
func (s *sqlStore) Close() error {
	return s.db.Close()
}

func migrate(ctx context.Context, db *sql.DB) error {
	ddl := `
CREATE TABLE IF NOT EXISTS knowledge_items (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL UNIQUE,
	kind TEXT NOT NULL,
	description TEXT,
	source_path TEXT NOT NULL,
	priority TEXT DEFAULT 'medium',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_items_kind ON knowledge_items(kind);
CREATE INDEX IF NOT EXISTS idx_items_name ON knowledge_items(name);

CREATE TABLE IF NOT EXISTS knowledge_triggers (
	id TEXT PRIMARY KEY,
	item_id TEXT NOT NULL,
	trigger_kind TEXT NOT NULL,
	pattern TEXT NOT NULL,
	FOREIGN KEY(item_id) REFERENCES knowledge_items(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_triggers_item ON knowledge_triggers(item_id);
CREATE INDEX IF NOT EXISTS idx_triggers_kind ON knowledge_triggers(trigger_kind);

CREATE TABLE IF NOT EXISTS knowledge_documents (
	id TEXT PRIMARY KEY,
	item_id TEXT NOT NULL,
	title TEXT,
	source_path TEXT,
	body TEXT,
	body_digest TEXT,
	created_at TEXT NOT NULL,
	FOREIGN KEY(item_id) REFERENCES knowledge_items(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_documents_item ON knowledge_documents(item_id);
`
	if _, err := db.ExecContext(ctx, ddl); err != nil {
		return fmt.Errorf("knowledge: migrate: %w", err)
	}
	return nil
}

// UpsertItem inserts or updates a knowledge item.
func (s *sqlStore) UpsertItem(ctx context.Context, item Item) (Item, error) {
	now := timeutil.NowUTC()
	if item.ID == "" {
		item.ID = ulid.Make().String()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.Priority == "" {
		item.Priority = "medium"
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO knowledge_items (id, name, kind, description, source_path, priority, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(name) DO UPDATE SET
	kind = excluded.kind,
	description = excluded.description,
	source_path = excluded.source_path,
	priority = excluded.priority,
	updated_at = excluded.updated_at`,
		item.ID, item.Name, string(item.Kind), item.Description, item.SourcePath, item.Priority,
		timeutil.FormatRFC3339Nano(item.CreatedAt), timeutil.FormatRFC3339Nano(item.UpdatedAt))
	if err != nil {
		return Item{}, fmt.Errorf("knowledge: upsert item: %w", err)
	}

	// Fetch the actual item (in case of conflict, ID may differ)
	return s.getItemByName(ctx, item.Name)
}

func (s *sqlStore) getItemByName(ctx context.Context, name string) (Item, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, kind, description, source_path, priority, created_at, updated_at
FROM knowledge_items WHERE name = ?`, name)
	return scanItem(row)
}

// GetItem returns a knowledge item by ID.
func (s *sqlStore) GetItem(ctx context.Context, id string) (Item, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, kind, description, source_path, priority, created_at, updated_at
FROM knowledge_items WHERE id = ?`, id)
	return scanItem(row)
}

// GetItemByName returns a knowledge item by name.
func (s *sqlStore) GetItemByName(ctx context.Context, name string) (Item, bool, error) {
	item, err := s.getItemByName(ctx, name)
	if dbutil.IsNoRows(err) {
		return Item{}, false, nil
	}
	if err != nil {
		return Item{}, false, err
	}
	return item, true, nil
}

// ListItems returns all items of a given kind.
func (s *sqlStore) ListItems(ctx context.Context, kind ItemKind) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, kind, description, source_path, priority, created_at, updated_at
FROM knowledge_items WHERE kind = ? ORDER BY name`, string(kind))
	if err != nil {
		return nil, fmt.Errorf("knowledge: list items: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()
	return scanItems(rows)
}

// ListAllItems returns all knowledge items.
func (s *sqlStore) ListAllItems(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, kind, description, source_path, priority, created_at, updated_at
FROM knowledge_items ORDER BY kind, name`)
	if err != nil {
		return nil, fmt.Errorf("knowledge: list all items: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()
	return scanItems(rows)
}

// DeleteItem removes a knowledge item and its associated triggers/documents.
func (s *sqlStore) DeleteItem(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("knowledge: delete item: %w", err)
	}
	return nil
}

// AddTrigger adds a trigger for a knowledge item.
func (s *sqlStore) AddTrigger(ctx context.Context, t Trigger) (Trigger, error) {
	if t.ID == "" {
		t.ID = ulid.Make().String()
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO knowledge_triggers (id, item_id, trigger_kind, pattern)
VALUES (?, ?, ?, ?)`, t.ID, t.ItemID, string(t.TriggerKind), t.Pattern)
	if err != nil {
		return Trigger{}, fmt.Errorf("knowledge: add trigger: %w", err)
	}
	return t, nil
}

// ListTriggers returns all triggers for a knowledge item.
func (s *sqlStore) ListTriggers(ctx context.Context, itemID string) ([]Trigger, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, item_id, trigger_kind, pattern
FROM knowledge_triggers WHERE item_id = ?`, itemID)
	if err != nil {
		return nil, fmt.Errorf("knowledge: list triggers: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()
	return scanTriggers(rows)
}

// ListAllTriggers returns all triggers.
func (s *sqlStore) ListAllTriggers(ctx context.Context) ([]Trigger, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, item_id, trigger_kind, pattern
FROM knowledge_triggers`)
	if err != nil {
		return nil, fmt.Errorf("knowledge: list all triggers: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()
	return scanTriggers(rows)
}

// DeleteTriggersForItem removes all triggers for a knowledge item.
func (s *sqlStore) DeleteTriggersForItem(ctx context.Context, itemID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_triggers WHERE item_id = ?`, itemID)
	if err != nil {
		return fmt.Errorf("knowledge: delete triggers: %w", err)
	}
	return nil
}

// UpsertDocument inserts or updates a document.
func (s *sqlStore) UpsertDocument(ctx context.Context, doc Document) (Document, error) {
	if doc.ID == "" {
		doc.ID = ulid.Make().String()
	}
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = timeutil.NowUTC()
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO knowledge_documents (id, item_id, title, source_path, body, body_digest, created_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
	title = excluded.title,
	source_path = excluded.source_path,
	body = excluded.body,
	body_digest = excluded.body_digest`,
		doc.ID, doc.ItemID, doc.Title, doc.SourcePath, doc.Body, doc.BodyDigest,
		timeutil.FormatRFC3339Nano(doc.CreatedAt))
	if err != nil {
		return Document{}, fmt.Errorf("knowledge: upsert document: %w", err)
	}
	return doc, nil
}

// ListDocuments returns all documents for a knowledge item.
func (s *sqlStore) ListDocuments(ctx context.Context, itemID string) ([]Document, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, item_id, title, source_path, body, body_digest, created_at
FROM knowledge_documents WHERE item_id = ?`, itemID)
	if err != nil {
		return nil, fmt.Errorf("knowledge: list documents: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()
	return scanDocuments(rows)
}

// DeleteDocumentsForItem removes all documents for a knowledge item.
func (s *sqlStore) DeleteDocumentsForItem(ctx context.Context, itemID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM knowledge_documents WHERE item_id = ?`, itemID)
	if err != nil {
		return fmt.Errorf("knowledge: delete documents: %w", err)
	}
	return nil
}

// MatchByKeyword returns items that have keyword triggers matching any of the given keywords.
func (s *sqlStore) MatchByKeyword(ctx context.Context, keywords []string) ([]Item, error) {
	if len(keywords) == 0 {
		return nil, nil
	}

	// Build query with placeholders
	query := `
SELECT DISTINCT i.id, i.name, i.kind, i.description, i.source_path, i.priority, i.created_at, i.updated_at
FROM knowledge_items i
JOIN knowledge_triggers t ON i.id = t.item_id
WHERE t.trigger_kind = 'keyword' AND LOWER(t.pattern) IN (`
	args := make([]any, len(keywords))
	for i, kw := range keywords {
		if i > 0 {
			query += ", "
		}
		query += "?"
		args[i] = strings.ToLower(kw)
	}
	query += `) ORDER BY i.priority DESC, i.name`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("knowledge: match by keyword: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()
	return scanItems(rows)
}

// MatchByPath returns items that have path triggers matching the given path.
func (s *sqlStore) MatchByPath(ctx context.Context, path string) ([]Item, error) {
	// For now, do simple LIKE matching. Could use glob in the future.
	rows, err := s.db.QueryContext(ctx, `
SELECT DISTINCT i.id, i.name, i.kind, i.description, i.source_path, i.priority, i.created_at, i.updated_at
FROM knowledge_items i
JOIN knowledge_triggers t ON i.id = t.item_id
WHERE t.trigger_kind = 'path' AND ? LIKE REPLACE(REPLACE(t.pattern, '**', '%'), '*', '%')
ORDER BY i.priority DESC, i.name`, path)
	if err != nil {
		return nil, fmt.Errorf("knowledge: match by path: %w", err)
	}
	defer func() {
		// Rows cleanup in defer; error is not actionable after iteration.
		_ = rows.Close() //nolint:errcheck
	}()
	return scanItems(rows)
}

// Scan helpers

func scanItem(row *sql.Row) (Item, error) {
	var item Item
	var createdAt, updatedAt string
	err := row.Scan(&item.ID, &item.Name, &item.Kind, &item.Description, &item.SourcePath,
		&item.Priority, &createdAt, &updatedAt)
	if err != nil {
		return Item{}, fmt.Errorf("knowledge: scan item: %w", err)
	}
	times := dbutil.ScanTimestampsMust(createdAt, updatedAt)
	item.CreatedAt = times[0]
	item.UpdatedAt = times[1]
	return item, nil
}

func scanItems(rows *sql.Rows) ([]Item, error) {
	var items []Item
	for rows.Next() {
		var item Item
		var createdAt, updatedAt string
		err := rows.Scan(&item.ID, &item.Name, &item.Kind, &item.Description, &item.SourcePath,
			&item.Priority, &createdAt, &updatedAt)
		if err != nil {
			return nil, fmt.Errorf("knowledge: scan items: %w", err)
		}
		times := dbutil.ScanTimestampsMust(createdAt, updatedAt)
		item.CreatedAt = times[0]
		item.UpdatedAt = times[1]
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanTriggers(rows *sql.Rows) ([]Trigger, error) {
	var triggers []Trigger
	for rows.Next() {
		var t Trigger
		err := rows.Scan(&t.ID, &t.ItemID, &t.TriggerKind, &t.Pattern)
		if err != nil {
			return nil, fmt.Errorf("knowledge: scan triggers: %w", err)
		}
		triggers = append(triggers, t)
	}
	return triggers, rows.Err()
}

func scanDocuments(rows *sql.Rows) ([]Document, error) {
	var docs []Document
	for rows.Next() {
		var doc Document
		var createdAt string
		var title, sourcePath, body, bodyDigest sql.NullString
		err := rows.Scan(&doc.ID, &doc.ItemID, &title, &sourcePath, &body, &bodyDigest, &createdAt)
		if err != nil {
			return nil, fmt.Errorf("knowledge: scan documents: %w", err)
		}
		doc.Title = title.String
		doc.SourcePath = sourcePath.String
		doc.Body = body.String
		doc.BodyDigest = bodyDigest.String
		times := dbutil.ScanTimestampsMust(createdAt)
		doc.CreatedAt = times[0]
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}
