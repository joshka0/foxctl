package actor

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MailboxStore defines the interface for mailbox operations.
// This is a local definition; the full implementation lives in internal/mailbox.
type MailboxStore interface {
	// Poll atomically claims the next available message for a namespace.
	// Returns nil if no messages are available.
	Poll(ctx context.Context, namespace string, leaseTimeout time.Duration) (*Message, error)

	// Ack acknowledges successful processing of a message.
	Ack(ctx context.Context, messageID string) error

	// Nack returns a message to the queue for retry.
	Nack(ctx context.Context, messageID string) error

	// Send enqueues a new message.
	Send(ctx context.Context, msg *Message) error
}

// SupervisionStrategy configures how the supervisor handles actor failures.
type SupervisionStrategy struct {
	// MaxRestarts is the maximum number of restarts within RestartWindow.
	MaxRestarts int

	// RestartWindow is the time window for counting restarts.
	RestartWindow time.Duration

	// BackoffInitial is the initial backoff delay after a restart.
	BackoffInitial time.Duration

	// BackoffMax is the maximum backoff delay.
	BackoffMax time.Duration

	// BackoffMultiplier is the multiplier for exponential backoff.
	BackoffMultiplier float64
}

// DefaultSupervisionStrategy returns a strategy with sensible defaults.
func DefaultSupervisionStrategy() SupervisionStrategy {
	return SupervisionStrategy{
		MaxRestarts:       3,
		RestartWindow:     time.Minute,
		BackoffInitial:    time.Second,
		BackoffMax:        time.Minute,
		BackoffMultiplier: 2.0,
	}
}

// Supervisor manages actor lifecycles and message routing.
//
// The supervisor is the central control plane for the actor system:
// - Starts/stops actors on demand
// - Routes messages from the MailboxWatcher to actors
// - Handles actor failures with the configured supervision strategy
// - Provides health monitoring and metrics
//
// Contract: Sequential processing (MVP)
// - Per-actor concurrency = 1
// - Supervisor only claims next message when actor is idle
// - Queue depth stays in SQLite, no in-memory buffering
type Supervisor struct {
	mu sync.RWMutex

	// actors maps namespace -> actor
	actors map[string]Actor

	// stats maps namespace -> stats
	stats map[string]*Stats

	// restarts tracks restart times for backoff calculation
	restarts map[string][]time.Time

	// mailbox is the shared mailbox store
	mailbox MailboxStore

	// watcher provides reactive notifications
	watcher *Watcher

	// eventBus is for cross-actor events
	eventBus *EventBus

	// strategy is the supervision strategy
	strategy SupervisionStrategy

	// leaseTimeout is the default lease timeout for message processing
	leaseTimeout time.Duration

	// ctx is the supervisor's context
	ctx    context.Context
	cancel context.CancelFunc

	// wg tracks running goroutines
	wg sync.WaitGroup
}

// SupervisorOption configures a Supervisor.
type SupervisorOption func(*Supervisor)

// WithStrategy sets the supervision strategy.
func WithStrategy(s SupervisionStrategy) SupervisorOption {
	return func(sup *Supervisor) {
		sup.strategy = s
	}
}

// WithLeaseTimeout sets the default lease timeout.
func WithLeaseTimeout(d time.Duration) SupervisorOption {
	return func(sup *Supervisor) {
		sup.leaseTimeout = d
	}
}

// WithEventBus sets the event bus.
func WithEventBus(eb *EventBus) SupervisorOption {
	return func(sup *Supervisor) {
		sup.eventBus = eb
	}
}

// NewSupervisor creates a new Supervisor.
func NewSupervisor(mb MailboxStore, opts ...SupervisorOption) *Supervisor {
	sup := &Supervisor{
		actors:       make(map[string]Actor),
		stats:        make(map[string]*Stats),
		restarts:     make(map[string][]time.Time),
		mailbox:      mb,
		strategy:     DefaultSupervisionStrategy(),
		leaseTimeout: 5 * time.Minute,
	}

	for _, opt := range opts {
		opt(sup)
	}

	return sup
}

// Start starts the supervisor and begins processing messages.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.ctx != nil {
		s.mu.Unlock()
		return fmt.Errorf("supervisor already started")
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()

	// Start the watcher if configured
	if s.watcher != nil {
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.runWatcher()
		}()
	}

	// Emit started event
	if s.eventBus != nil {
		s.eventBus.Publish(Event{
			Type:      EventAgentStarted,
			Source:    "supervisor",
			Timestamp: time.Now(),
		})
	}

	return nil
}

// Stop gracefully stops the supervisor and all actors.
func (s *Supervisor) Stop(ctx context.Context) error {
	s.mu.Lock()
	if s.cancel == nil {
		s.mu.Unlock()
		return nil
	}
	s.cancel()
	s.mu.Unlock()

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-ctx.Done():
		return ctx.Err()
	}

	// Stop all actors
	s.mu.RLock()
	actors := make([]Actor, 0, len(s.actors))
	for _, a := range s.actors {
		actors = append(actors, a)
	}
	s.mu.RUnlock()

	for _, a := range actors {
		if err := a.Stop(ctx); err != nil {
			// Log but continue stopping others
			continue
		}
	}

	// Emit stopped event
	if s.eventBus != nil {
		s.eventBus.Publish(Event{
			Type:      EventAgentStopped,
			Source:    "supervisor",
			Timestamp: time.Now(),
		})
	}

	return nil
}

// Register registers an actor with the supervisor.
func (s *Supervisor) Register(ctx context.Context, actor Actor) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ns := actor.Namespace()
	if _, exists := s.actors[ns]; exists {
		return fmt.Errorf("actor already registered for namespace %s", ns)
	}

	s.actors[ns] = actor
	s.stats[ns] = &Stats{State: StateIdle}

	// Start the actor
	if err := actor.Start(ctx); err != nil {
		delete(s.actors, ns)
		delete(s.stats, ns)
		return fmt.Errorf("start actor: %w", err)
	}

	// Wire reply sender if actor supports it
	if setter, ok := actor.(interface {
		SetReplySender(func(ctx context.Context, msg *Message) error)
	}); ok {
		setter.SetReplySender(func(ctx context.Context, msg *Message) error {
			if msg == nil {
				return fmt.Errorf("reply message is nil")
			}
			if msg.ID == "" {
				msg.ID = fmt.Sprintf("reply-%d", time.Now().UnixNano())
			}
			if msg.CreatedAt.IsZero() {
				msg.CreatedAt = time.Now()
			}
			if msg.ExpiresAt.IsZero() {
				msg.ExpiresAt = msg.CreatedAt.Add(5 * time.Minute)
			}
			if msg.FromNS == "" {
				msg.FromNS = actor.Namespace()
			}
			if err := s.mailbox.Send(ctx, msg); err != nil {
				return err
			}
			if s.eventBus != nil {
				s.eventBus.Publish(Event{
					Type:      EventMailSent,
					Source:    msg.FromNS,
					Target:    msg.ToNS,
					Timestamp: time.Now(),
					SessionID: msg.SessionID,
					Workspace: msg.Workspace,
				})
			}
			return nil
		})
	}

	return nil
}

// Unregister removes an actor from the supervisor.
func (s *Supervisor) Unregister(ctx context.Context, namespace string) error {
	s.mu.Lock()
	actor, exists := s.actors[namespace]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("no actor registered for namespace %s", namespace)
	}
	delete(s.actors, namespace)
	delete(s.stats, namespace)
	delete(s.restarts, namespace)
	s.mu.Unlock()

	return actor.Stop(ctx)
}

// GetActor returns the actor for a namespace.
func (s *Supervisor) GetActor(namespace string) (Actor, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.actors[namespace]
	return a, ok
}

// GetStats returns stats for an actor.
func (s *Supervisor) GetStats(namespace string) (Stats, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.stats[namespace]
	if !ok {
		return Stats{}, false
	}
	return *st, true
}

// ListActors returns all registered actor namespaces.
func (s *Supervisor) ListActors() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]string, 0, len(s.actors))
	for ns := range s.actors {
		result = append(result, ns)
	}
	return result
}

// HandleWakeUp processes a wake-up signal from the watcher.
//
// Contract: Only claims message if actor is idle (sequential processing).
func (s *Supervisor) HandleWakeUp(ctx context.Context, wake WakeUp) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.handleWakeUp(ctx, wake)
	}()
}

func (s *Supervisor) handleWakeUp(ctx context.Context, wake WakeUp) {
	s.mu.Lock()
	actor, ok := s.actors[wake.Namespace]
	stats := s.stats[wake.Namespace]
	if !ok {
		s.mu.Unlock()
		return // Actor not registered
	}

	// Check if actor is idle (sequential processing contract)
	if actor.State() != StateIdle {
		s.mu.Unlock()
		return // Actor busy, will poll when done
	}

	// Mark as processing to prevent concurrent claims
	actor.SetState(StateProcessing)
	s.mu.Unlock()

	// Atomically claim message with lease
	msg, err := s.mailbox.Poll(ctx, wake.Namespace, s.leaseTimeout)
	if err != nil || msg == nil {
		actor.SetState(StateIdle)
		return // No message or already claimed
	}

	// Process message
	s.processMessage(ctx, actor, stats, msg)
}

// processMessage handles message delivery to an actor.
func (s *Supervisor) processMessage(ctx context.Context, actor Actor, stats *Stats, msg *Message) {
	start := time.Now()
	defer func() {
		actor.SetState(StateIdle)
	}()

	// Emit received event
	if s.eventBus != nil {
		s.eventBus.Publish(Event{
			Type:      EventMailReceived,
			Source:    actor.Namespace(),
			Target:    msg.ToNS,
			Timestamp: time.Now(),
			SessionID: msg.SessionID,
			Workspace: msg.Workspace,
		})
	}

	// Process message
	err := actor.OnMailReceived(ctx, msg)

	// Update stats
	s.mu.Lock()
	stats.MessagesProcessed++
	stats.TotalProcessingNs += time.Since(start).Nanoseconds()
	stats.LastMessageAt = time.Now()
	if err != nil {
		stats.MessagesErrored++
	}
	s.mu.Unlock()

	if err != nil {
		s.handleError(ctx, actor, msg, err)
		return
	}

	// Ack the message
	if err := s.mailbox.Ack(ctx, msg.ID); err != nil {
		// Log but don't fail - message will be reprocessed after lease expires
		return
	}

	// Emit acked event
	if s.eventBus != nil {
		s.eventBus.Publish(Event{
			Type:      EventMailAcked,
			Source:    actor.Namespace(),
			Target:    msg.ToNS,
			Timestamp: time.Now(),
			SessionID: msg.SessionID,
			Workspace: msg.Workspace,
		})
	}
}

// handleError processes an actor error according to the directive.
func (s *Supervisor) handleError(ctx context.Context, actor Actor, msg *Message, err error) {
	directive := actor.OnError(ctx, err)

	// Emit error event
	if s.eventBus != nil {
		s.eventBus.Publish(Event{
			Type:      EventAgentError,
			Source:    actor.Namespace(),
			Timestamp: time.Now(),
		})
	}

	switch directive {
	case DirectiveResume:
		// Nack with backoff for retry
		_ = s.mailbox.Nack(ctx, msg.ID)

	case DirectiveRestart:
		s.restartActor(ctx, actor)
		_ = s.mailbox.Nack(ctx, msg.ID)

	case DirectiveStop:
		actor.SetState(StateStopped)
		_ = s.mailbox.Nack(ctx, msg.ID)

	case DirectiveEscalate:
		// For now, treat escalate as stop + log
		actor.SetState(StateError)
		_ = s.mailbox.Nack(ctx, msg.ID)
	}
}

// restartActor restarts an actor with backoff.
func (s *Supervisor) restartActor(ctx context.Context, actor Actor) {
	ns := actor.Namespace()

	s.mu.Lock()
	// Track restart time
	now := time.Now()
	s.restarts[ns] = append(s.restarts[ns], now)

	// Prune old restarts outside window
	cutoff := now.Add(-s.strategy.RestartWindow)
	var recent []time.Time
	for _, t := range s.restarts[ns] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	s.restarts[ns] = recent

	// Check if we've exceeded max restarts
	if len(recent) > s.strategy.MaxRestarts {
		s.mu.Unlock()
		actor.SetState(StateError)
		return
	}

	// Update stats
	if st, ok := s.stats[ns]; ok {
		st.RestartCount++
	}
	s.mu.Unlock()

	// Calculate backoff
	backoff := s.calculateBackoff(len(recent))
	timer := time.NewTimer(backoff)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		actor.SetState(StateError)
		return
	case <-timer.C:
	}

	// Restart
	if err := actor.Stop(ctx); err != nil {
		actor.SetState(StateError)
		if s.eventBus != nil {
			s.eventBus.Publish(Event{
				Type:      EventAgentError,
				Source:    ns,
				Timestamp: time.Now(),
			})
		}
		return
	}
	if err := actor.Start(ctx); err != nil {
		actor.SetState(StateError)
		if s.eventBus != nil {
			s.eventBus.Publish(Event{
				Type:      EventAgentError,
				Source:    ns,
				Timestamp: time.Now(),
			})
		}
		return
	}
}

// calculateBackoff calculates exponential backoff duration.
func (s *Supervisor) calculateBackoff(attempt int) time.Duration {
	backoff := s.strategy.BackoffInitial
	for i := 1; i < attempt; i++ {
		backoff = time.Duration(float64(backoff) * s.strategy.BackoffMultiplier)
		if backoff > s.strategy.BackoffMax {
			backoff = s.strategy.BackoffMax
			break
		}
	}
	return backoff
}

// runWatcher runs the watcher loop.
func (s *Supervisor) runWatcher() {
	for {
		select {
		case <-s.ctx.Done():
			return
		case wake := <-s.watcher.WakeUps():
			s.HandleWakeUp(s.ctx, wake)
		}
	}
}

// SetWatcher sets the mailbox watcher.
// Must be called before Start() for the watcher to be active.
func (s *Supervisor) SetWatcher(w *Watcher) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx != nil {
		return fmt.Errorf("cannot set watcher after supervisor started")
	}
	s.watcher = w
	return nil
}
