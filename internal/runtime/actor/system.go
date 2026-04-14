package actor

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/joshka0/foxctl/internal/storage/mailbox"
	"github.com/joshka0/foxctl/internal/storage/trajectory"
)

// System is the top-level container for the actor system.
//
// It wires together:
// - MailboxStore: SQLite-backed message persistence
// - MailboxAdapter: Bridges mailbox.Store to supervisor's interface
// - Watcher: Polls for new messages and sends WakeUp signals
// - Supervisor: Manages actor lifecycles and message routing
// - EventBus: Cross-actor event distribution
// - TrajectoryPersister: Persists important events to trajectory.db
type System struct {
	// Store is the underlying mailbox store.
	Store mailbox.Store

	// Adapter bridges Store to the supervisor's MailboxStore interface.
	Adapter *MailboxAdapter

	// Watcher provides reactive notifications from SQLite triggers.
	Watcher *Watcher

	// Supervisor manages actors and routes messages.
	Supervisor *Supervisor

	// EventBus distributes events across actors.
	EventBus *EventBus

	// TrajectoryPersister persists events to trajectory.db (optional).
	TrajectoryPersister *TrajectoryPersister
}

// SystemOption configures the actor system.
type SystemOption func(*systemConfig)

type systemConfig struct {
	supervisionStrategy SupervisionStrategy
	watcherOpts         []WatcherOption
	adapterOpts         []MailboxAdapterOption
	trajectoryStore     trajectory.Store
}

// WithSystemSupervisionStrategy sets the supervision strategy for the system.
func WithSystemSupervisionStrategy(s SupervisionStrategy) SystemOption {
	return func(c *systemConfig) {
		c.supervisionStrategy = s
	}
}

// WithWatcherOptions sets options for the watcher.
func WithWatcherOptions(opts ...WatcherOption) SystemOption {
	return func(c *systemConfig) {
		c.watcherOpts = append(c.watcherOpts, opts...)
	}
}

// WithAdapterOptions sets options for the mailbox adapter.
func WithAdapterOptions(opts ...MailboxAdapterOption) SystemOption {
	return func(c *systemConfig) {
		c.adapterOpts = append(c.adapterOpts, opts...)
	}
}

// WithTrajectoryStore sets the trajectory store for event persistence.
// If provided, important events will be persisted to trajectory.db.
func WithTrajectoryStore(store trajectory.Store) SystemOption {
	return func(c *systemConfig) {
		c.trajectoryStore = store
	}
}

// NewSystem creates a fully wired actor system from a mailbox store.
//
// The system creates and connects:
// 1. MailboxAdapter wrapping the store
// 2. Watcher using the store's DB connection
// 3. EventBus for cross-actor events
// 4. Supervisor with adapter, watcher, and event bus
//
// The caller is responsible for starting and stopping the system.
func NewSystem(store mailbox.Store, opts ...SystemOption) (*System, error) {
	cfg := &systemConfig{
		supervisionStrategy: DefaultSupervisionStrategy(),
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Get DB connection from store for watcher
	db := store.DB()
	if db == nil {
		return nil, fmt.Errorf("mailbox store does not expose DB connection")
	}

	// Create components
	adapter := NewMailboxAdapter(store, cfg.adapterOpts...)
	watcher := NewWatcher(db, cfg.watcherOpts...)

	// Create persister if trajectory store is provided
	var persister *TrajectoryPersister
	var eventBusOpts []EventBusOption
	if cfg.trajectoryStore != nil {
		persister = NewTrajectoryPersister(cfg.trajectoryStore)
		eventBusOpts = append(eventBusOpts, WithPersister(persister))
	}
	eventBus := NewEventBus(eventBusOpts...)

	// Create supervisor with all components
	supervisor := NewSupervisor(
		adapter,
		WithStrategy(cfg.supervisionStrategy),
		WithEventBus(eventBus),
	)
	if err := supervisor.SetWatcher(watcher); err != nil {
		return nil, fmt.Errorf("set watcher: %w", err)
	}

	return &System{
		Store:               store,
		Adapter:             adapter,
		Watcher:             watcher,
		Supervisor:          supervisor,
		EventBus:            eventBus,
		TrajectoryPersister: persister,
	}, nil
}

// Start starts the actor system (watcher and supervisor).
func (s *System) Start(ctx context.Context) error {
	// Start watcher first (it creates the notify table/trigger)
	if err := s.Watcher.Start(ctx); err != nil {
		return fmt.Errorf("start watcher: %w", err)
	}

	// Start supervisor (begins processing wake-ups)
	if err := s.Supervisor.Start(ctx); err != nil {
		_ = s.Watcher.Stop()
		return fmt.Errorf("start supervisor: %w", err)
	}

	return nil
}

// Stop gracefully stops the actor system.
// It attempts to stop both supervisor and watcher, combining any errors.
func (s *System) Stop(ctx context.Context) error {
	var errs []error

	// Stop supervisor first (stops processing)
	if err := s.Supervisor.Stop(ctx); err != nil {
		errs = append(errs, fmt.Errorf("stop supervisor: %w", err))
	}

	// Always attempt to stop watcher, even if supervisor failed
	if err := s.Watcher.Stop(); err != nil {
		errs = append(errs, fmt.Errorf("stop watcher: %w", err))
	}

	return errors.Join(errs...)
}

// Register registers an actor with the system.
func (s *System) Register(ctx context.Context, actor Actor) error {
	return s.Supervisor.Register(ctx, actor)
}

// Unregister removes an actor from the system.
func (s *System) Unregister(ctx context.Context, namespace string) error {
	return s.Supervisor.Unregister(ctx, namespace)
}

// DBWithMailbox is a helper interface for stores that expose their DB.
type DBWithMailbox interface {
	mailbox.Store
	DB() *sql.DB
}
