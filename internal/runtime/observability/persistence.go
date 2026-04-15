//nolint:forbidigo // This IS the logging infrastructure - zerolog/stderr usage is intentional
package observability

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	_ "modernc.org/sqlite" // CGO-free SQLite driver
)

// PersistenceMode determines how an event is persisted.
type PersistenceMode int

const (
	// PersistDefault uses the global default (NDJSON file).
	PersistDefault PersistenceMode = iota
	// PersistNDJSON writes to the default NDJSON file.
	PersistNDJSON
	// PersistSQL writes directly to SQLite (blocking).
	PersistSQL
	// PersistHybrid writes to NDJSON immediately, queues for SQLite sync.
	PersistHybrid
	// PersistNone disables persistence (still sampled/logged).
	PersistNone
)

const defaultPersistTimeout = 250 * time.Millisecond

// String returns the string representation of a PersistenceMode.
func (m PersistenceMode) String() string {
	switch m {
	case PersistDefault:
		return "default"
	case PersistNDJSON:
		return "ndjson"
	case PersistSQL:
		return "sql"
	case PersistHybrid:
		return "hybrid"
	case PersistNone:
		return "none"
	default:
		return "unknown"
	}
}

// persistConfig holds the persistence configuration for an event.
type persistConfig struct {
	mode     PersistenceMode
	fileName string // Custom NDJSON filename (without extension)
}

// WithPersistence sets the persistence mode for the event.
// Use PersistSQL for high-value events that need queryability.
// Use PersistHybrid for events that need both fast writes and queryability.
func (b *EventBuilder) WithPersistence(mode PersistenceMode) *EventBuilder {
	if b.persist == nil {
		b.persist = &persistConfig{}
	}
	b.persist.mode = mode
	return b
}

// WithPersistenceFile sets a custom NDJSON filename for the event.
// The file will be created at $FOXCTL_OBS_DIR/events/<name>.ndjson.
func (b *EventBuilder) WithPersistenceFile(name string) *EventBuilder {
	if b.persist == nil {
		b.persist = &persistConfig{}
	}
	b.persist.fileName = name
	return b
}

// PersistConfig returns the builder's persistence configuration.
// Returns nil if no persistence options were set.
func (b *EventBuilder) PersistConfig() *persistConfig {
	if b == nil {
		return nil
	}
	return b.persist
}

// ---------------------------------------------------------------------------
// SQLite Persistence
// ---------------------------------------------------------------------------

// EventStore provides SQLite-backed event persistence.
type EventStore struct {
	db     *sql.DB
	mu     sync.Mutex
	closed bool
}

// OpenEventStore opens or creates the events SQLite database.
//
// Index:
// - Purpose: Initialize SQLite-backed persistence for wide events
// - Flow: resolve db path → open database → ensure schema → return store
// - SideEffects: opens database connection; creates tables/indexes
// - FailureModes: database open errors, schema creation errors
// - Related: createEventSchema, EventStore.Insert
// - Keywords: events_db, sqlite, wide_events, schema, persistence
func OpenEventStore(ctx context.Context, obsDir string) (*EventStore, error) {
	dbPath := filepath.Join(obsDir, "events.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// Create schema
	if err := createEventSchema(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &EventStore{db: db}, nil
}

func createEventSchema(ctx context.Context, db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS wide_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		span_id TEXT NOT NULL UNIQUE,
		trace_id TEXT NOT NULL,
		parent_id TEXT,
		ts TEXT NOT NULL,
		service TEXT NOT NULL,
		version TEXT,
		component TEXT,
		operation TEXT NOT NULL,
		command TEXT,
		subtype TEXT,
		session_id TEXT,
		agent_id TEXT,
		workspace_id TEXT,
		job_id TEXT,
		status TEXT NOT NULL,
		duration_ms INTEGER,
		error_type TEXT,
		error_code TEXT,
		error_message TEXT,
		retriable INTEGER,
		data TEXT,
		created_at TEXT DEFAULT (datetime('now'))
	);

	CREATE INDEX IF NOT EXISTS idx_wide_events_trace_id ON wide_events(trace_id);
	CREATE INDEX IF NOT EXISTS idx_wide_events_ts ON wide_events(ts);
	CREATE INDEX IF NOT EXISTS idx_wide_events_operation ON wide_events(operation);
	CREATE INDEX IF NOT EXISTS idx_wide_events_command ON wide_events(command);
	CREATE INDEX IF NOT EXISTS idx_wide_events_status ON wide_events(status);
	CREATE INDEX IF NOT EXISTS idx_wide_events_session_id ON wide_events(session_id);
	CREATE INDEX IF NOT EXISTS idx_wide_events_workspace_id ON wide_events(workspace_id);
	`
	_, err := db.ExecContext(ctx, schema)
	return err
}

// Insert writes a WideEvent to the database.
//
// Index:
// - Purpose: Persist a WideEvent row to SQLite
// - Flow: marshal data → lock store → validate state → insert/replace row
// - SideEffects: database writes
// - FailureModes: marshal errors, database execution errors
// - Related: EventStore.Close, OpenEventStore
// - Keywords: wide_events, insert, sqlite, span_id, trace_id
func (s *EventStore) Insert(ctx context.Context, event *WideEvent) error {
	if s == nil || event == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	dataJSON, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal event.Data: %w", err)
	}
	var retriable *int
	if event.Retriable != nil {
		v := 0
		if *event.Retriable {
			v = 1
		}
		retriable = &v
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO wide_events (
			span_id, trace_id, parent_id, ts, service, version, component,
			operation, command, subtype, session_id, agent_id, workspace_id, job_id,
			status, duration_ms, error_type, error_code, error_message, retriable, data
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.SpanID, event.TraceID, event.ParentID,
		event.Ts.Format(time.RFC3339Nano), event.Service, event.Version, event.Component,
		event.Operation, event.Command, event.Subtype,
		event.SessionID, event.AgentID, event.WorkspaceID, event.JobID,
		string(event.Status), event.DurationMS,
		event.ErrorType, event.ErrorCode, event.ErrorMessage, retriable,
		string(dataJSON),
	)
	return err
}

// Close closes the database connection.
func (s *EventStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.db.Close()
}

// ---------------------------------------------------------------------------
// Hybrid Sync (NDJSON → SQLite)
// ---------------------------------------------------------------------------

// SyncConfig configures the background NDJSON to SQLite sync.
type SyncConfig struct {
	// Interval between sync runs.
	Interval time.Duration
	// BatchSize is the maximum number of events to sync per run.
	BatchSize int
}

// DefaultSyncConfig returns the default sync configuration.
func DefaultSyncConfig() SyncConfig {
	return SyncConfig{
		Interval:  30 * time.Second,
		BatchSize: 100,
	}
}

// Syncer synchronizes NDJSON events to SQLite in the background.
type Syncer struct {
	store  *EventStore
	config SyncConfig
	logger zerolog.Logger

	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	doneCh   chan struct{}
	lastSync time.Time
	offset   int64 // File offset for incremental sync
}

// NewSyncer creates a new background syncer.
func NewSyncer(store *EventStore, config SyncConfig) *Syncer {
	return &Syncer{
		store:  store,
		config: config,
		logger: zerolog.New(os.Stderr).With().Str("component", "obs-syncer").Timestamp().Logger(),
	}
}

// Start begins background synchronization.
//
// Index:
// - Purpose: Start the NDJSON-to-SQLite sync worker
// - Flow: acquire lock → guard running → init channels → launch goroutine
// - SideEffects: spawns goroutine
// - FailureModes: no-op when already running
// - Related: Syncer.Stop, Syncer.run
// - Keywords: syncer, ndjson, sqlite, background, goroutine
func (s *Syncer) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})

	go s.run()
}

// Stop stops the background syncer and waits for it to finish.
//
// Index:
// - Purpose: Stop the NDJSON-to-SQLite sync worker gracefully
// - Flow: acquire lock → close stop channel → wait for done → reset running flag
// - SideEffects: stops goroutine; blocks until completion
// - FailureModes: no-op when not running
// - Related: Syncer.Start, Syncer.run
// - Keywords: syncer, stop, ndjson, sqlite, goroutine
func (s *Syncer) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	close(s.stopCh)
	s.mu.Unlock()

	<-s.doneCh

	// Reset running flag so Start() can be called again
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

// run drives periodic synchronization until stopped.
//
// Index:
// - Purpose: Periodically sync NDJSON events into SQLite
// - Flow: start ticker → on tick syncOnce → on stop syncOnce and exit
// - SideEffects: reads NDJSON files; writes to SQLite; logs warnings
// - Concurrency: runs in background goroutine
// - FailureModes: sync failures logged; stop signal triggers final sync
// - Observability: emits zerolog warnings for sync failures
// - Related: Syncer.syncOnce, Syncer.syncFile
// - Keywords: syncer, ticker, ndjson, sqlite, sync_once
func (s *Syncer) run() {
	defer close(s.doneCh)

	ticker := time.NewTicker(s.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			// Final sync before exit
			s.syncOnce()
			return
		case <-ticker.C:
			s.syncOnce()
		}
	}
}

// syncOnce performs a single NDJSON-to-SQLite sync cycle.
//
// Index:
// - Purpose: Sync a batch of wide events from NDJSON to SQLite
// - Flow: create timeout ctx → resolve obs dir → sync file → log results
// - SideEffects: reads NDJSON file; writes SQLite; logs warnings
// - FailureModes: missing obs dir, sync file errors
// - Observability: emits zerolog warnings and debug logs
// - Related: Syncer.syncFile, getObsDir
// - Keywords: sync_once, ndjson, sqlite, batch_size, timeout
func (s *Syncer) syncOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dir := getObsDir()
	if dir == "" {
		return
	}

	ndjsonPath := filepath.Join(dir, "events", WideEventFileName+".ndjson")
	synced, err := s.syncFile(ctx, ndjsonPath)
	if err != nil {
		s.logger.Warn().Err(err).Msg("sync failed")
		return
	}

	if synced > 0 {
		s.logger.Debug().Int("synced", synced).Msg("synced events to SQLite")
	}
	s.lastSync = time.Now()
}

// syncFile streams NDJSON events from a file into SQLite.
//
// Index:
// - Purpose: Read NDJSON events incrementally and persist them to SQLite
// - Flow: open file → seek offset → decode entries → insert rows → update offset
// - SideEffects: reads file; writes SQLite; logs warnings
// - FailureModes: file open/seek errors, decode errors, insert errors
// - Observability: emits zerolog warnings on decode/insert failures
// - Related: EventStore.Insert, Syncer.syncOnce
// - Keywords: ndjson, sqlite, offset, decode, insert
func (s *Syncer) syncFile(ctx context.Context, path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	// Seek to last known offset
	if s.offset > 0 {
		if _, err := f.Seek(s.offset, 0); err != nil {
			// Reset offset if seek fails
			s.offset = 0
		}
	}

	reader := bufio.NewReader(f)
	synced := 0
	offset := s.offset

	for synced < s.config.BatchSize {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				if len(line) == 0 {
					break
				}
				// Partial line without newline; retry on next sync.
				break
			}
			s.logger.Warn().Err(err).Msg("read error")
			break
		}

		lineLen := int64(len(line))
		line = strings.TrimSpace(line)
		if line == "" {
			offset += lineLen
			continue
		}

		var event WideEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			s.logger.Warn().Err(err).Msg("decode error")
			offset += lineLen
			continue
		}

		if err := s.store.Insert(ctx, &event); err != nil {
			s.logger.Warn().Err(err).Str("span_id", event.SpanID).Msg("insert failed")
			break
		}

		offset += lineLen
		synced++
	}

	s.offset = offset

	return synced, nil
}

// ---------------------------------------------------------------------------
// Global Persistence Manager
// ---------------------------------------------------------------------------

var (
	globalStore  *EventStore
	globalSyncer *Syncer
	globalMu     sync.Mutex
	globalInit   sync.Once
)

// InitPersistence initializes the global persistence layer.
// Call this once at startup if you want SQLite persistence.
//
// Index:
// - Purpose: Initialize global SQLite persistence and background sync
// - Flow: resolve obs dir → open event store → start syncer → cache globals
// - SideEffects: opens database; starts goroutine
// - FailureModes: missing obs dir (no-op), database open errors
// - Related: OpenEventStore, NewSyncer, ClosePersistence
// - Keywords: init_persistence, sqlite, syncer, observability_dir, wide_events
func InitPersistence(ctx context.Context) error {
	var initErr error
	globalInit.Do(func() {
		dir := getObsDir()
		if dir == "" {
			return
		}

		store, err := OpenEventStore(ctx, dir)
		if err != nil {
			initErr = err
			return
		}
		globalStore = store

		syncer := NewSyncer(store, DefaultSyncConfig())
		syncer.Start()
		globalSyncer = syncer
	})
	return initErr
}

// ClosePersistence shuts down the global persistence layer.
// After calling this, InitPersistence can be called again to re-initialize.
//
// Index:
// - Purpose: Stop global persistence and release resources
// - Flow: lock globals → stop syncer → close store → reset init guard
// - SideEffects: stops goroutine; closes database
// - FailureModes: close errors returned
// - Related: InitPersistence, EventStore.Close
// - Keywords: close_persistence, syncer, sqlite, observability_dir
func ClosePersistence() error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if globalSyncer != nil {
		globalSyncer.Stop()
		globalSyncer = nil
	}
	if globalStore != nil {
		err := globalStore.Close()
		globalStore = nil
		// Reset sync.Once to allow re-initialization
		globalInit = sync.Once{}
		return err
	}
	// Reset sync.Once even if store was nil (in case init was called but failed)
	globalInit = sync.Once{}
	return nil
}

// persistEvent handles the persistence based on event configuration.
//
// Index:
// - Purpose: Persist a WideEvent according to configured mode
// - Flow: resolve mode → select destination → write NDJSON and/or SQLite
// - SideEffects: NDJSON writes; SQLite inserts; logs warnings
// - FailureModes: insert/write errors logged; nil event returns early
// - Observability: emits zerolog warnings for persistence failures
// - Related: WriteEvent, EventStore.Insert
// - Keywords: wide_events, persist_mode, ndjson, sqlite, hybrid
func persistEvent(ctx context.Context, event *WideEvent, config *persistConfig) {
	if event == nil {
		return
	}

	persistCtx, cancel := context.WithTimeout(context.Background(), defaultPersistTimeout)
	defer cancel()

	mode := PersistDefault
	fileName := WideEventFileName
	if config != nil {
		mode = config.mode
		if config.fileName != "" {
			fileName = config.fileName
		}
	}

	switch mode {
	case PersistNone:
		return

	case PersistSQL:
		if globalStore != nil {
			if err := globalStore.Insert(persistCtx, event); err != nil {
				logPersistError("sql", event.Operation, err)
			}
		}

	case PersistHybrid:
		// Write to NDJSON first (fast path)
		if err := WriteEvent(persistCtx, fileName, event); err != nil {
			logPersistError("ndjson", event.Operation, err)
		}
		// SQLite sync happens in background via Syncer

	case PersistDefault, PersistNDJSON:
		if err := WriteEvent(persistCtx, fileName, event); err != nil {
			logPersistError("ndjson", event.Operation, err)
		}
	}
}

func logPersistError(mode, operation string, err error) {
	log := zerolog.New(os.Stderr).With().Timestamp().Logger()
	log.Warn().
		Str("component", "observability").
		Str("mode", mode).
		Str("operation", operation).
		Err(err).
		Msg("persistence failed")
}
