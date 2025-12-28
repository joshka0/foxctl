package actor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// MessageHandler processes a specific message type.
// Returns an optional reply message and an error.
type MessageHandler func(ctx context.Context, msg *Message) (*Message, error)

// BaseActor provides common functionality for all actors.
//
// Embed this in concrete actor implementations to get:
// - State management
// - Message handler registration
// - Error handling with configurable directives
// - Lifecycle management
//
// Example usage:
//
//	type MyActor struct {
//	    *actor.BaseActor
//	    // custom fields
//	}
//
//	func NewMyActor(cfg actor.Config) *MyActor {
//	    a := &MyActor{
//	        BaseActor: actor.NewBaseActor(cfg),
//	    }
//	    a.RegisterHandler("agent.ask", a.handleAsk)
//	    return a
//	}
type BaseActor struct {
	mu sync.RWMutex

	config   Config
	state    atomic.Value // State
	handlers map[string]MessageHandler
	timers   map[string]*time.Timer

	// Hooks for customization
	onStart   func(ctx context.Context) error
	onStop    func(ctx context.Context) error
	onError   func(ctx context.Context, err error) Directive
	onTimeout func(ctx context.Context, event TimerEvent) error

	// Reply sender (set by system when registered)
	replySender func(ctx context.Context, msg *Message) error
}

// BaseActorOption configures a BaseActor.
type BaseActorOption func(*BaseActor)

// WithOnStart sets the start hook.
func WithOnStart(fn func(ctx context.Context) error) BaseActorOption {
	return func(a *BaseActor) {
		a.onStart = fn
	}
}

// WithOnStop sets the stop hook.
func WithOnStop(fn func(ctx context.Context) error) BaseActorOption {
	return func(a *BaseActor) {
		a.onStop = fn
	}
}

// WithOnError sets the error handler.
func WithOnError(fn func(ctx context.Context, err error) Directive) BaseActorOption {
	return func(a *BaseActor) {
		a.onError = fn
	}
}

// WithOnTimeout sets the timeout handler.
func WithOnTimeout(fn func(ctx context.Context, event TimerEvent) error) BaseActorOption {
	return func(a *BaseActor) {
		a.onTimeout = fn
	}
}

// WithReplySender sets the function used to send reply messages.
func WithReplySender(fn func(ctx context.Context, msg *Message) error) BaseActorOption {
	return func(a *BaseActor) {
		a.replySender = fn
	}
}

// NewBaseActor creates a new BaseActor with the given configuration.
func NewBaseActor(cfg Config, opts ...BaseActorOption) *BaseActor {
	a := &BaseActor{
		config:   cfg,
		handlers: make(map[string]MessageHandler),
		timers:   make(map[string]*time.Timer),
	}
	a.state.Store(StateStopped)

	for _, opt := range opts {
		opt(a)
	}

	return a
}

// ID returns the actor's unique identifier.
func (a *BaseActor) ID() string {
	return a.config.ID
}

// Namespace returns the actor's mailbox namespace.
func (a *BaseActor) Namespace() string {
	return a.config.Namespace
}

// Config returns the actor's configuration.
func (a *BaseActor) Config() Config {
	return a.config
}

// State returns the current actor state.
func (a *BaseActor) State() State {
	return a.state.Load().(State)
}

// SetState sets the actor state.
func (a *BaseActor) SetState(s State) {
	a.state.Store(s)
}

// Start starts the actor.
func (a *BaseActor) Start(ctx context.Context) error {
	a.SetState(StateStarting)

	if a.onStart != nil {
		if err := a.onStart(ctx); err != nil {
			a.SetState(StateError)
			return fmt.Errorf("start hook: %w", err)
		}
	}

	a.SetState(StateIdle)
	return nil
}

// Stop stops the actor and cleans up resources.
func (a *BaseActor) Stop(ctx context.Context) error {
	a.SetState(StateStopped)

	// Cancel all timers
	a.mu.Lock()
	for name, timer := range a.timers {
		timer.Stop()
		delete(a.timers, name)
	}
	a.mu.Unlock()

	if a.onStop != nil {
		if err := a.onStop(ctx); err != nil {
			return fmt.Errorf("stop hook: %w", err)
		}
	}

	return nil
}

// RegisterHandler registers a handler for a specific message subject.
func (a *BaseActor) RegisterHandler(subject string, handler MessageHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers[subject] = handler
}

// UnregisterHandler removes a handler for a message subject.
func (a *BaseActor) UnregisterHandler(subject string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.handlers, subject)
}

// OnMailReceived processes an incoming message.
// Routes to registered handlers based on message subject.
func (a *BaseActor) OnMailReceived(ctx context.Context, msg *Message) error {
	a.mu.RLock()
	handler, ok := a.handlers[msg.Subject]
	a.mu.RUnlock()

	if !ok {
		// No handler registered for this subject
		return fmt.Errorf("no handler for subject %q", msg.Subject)
	}

	reply, err := handler(ctx, msg)
	if err != nil {
		return err
	}

	// Send reply if handler returned one
	if reply != nil {
		if err := a.Reply(ctx, msg, reply); err != nil {
			return fmt.Errorf("send reply: %w", err)
		}
	}

	return nil
}

// OnTimeout handles a timer event.
func (a *BaseActor) OnTimeout(ctx context.Context, event TimerEvent) error {
	if a.onTimeout != nil {
		return a.onTimeout(ctx, event)
	}
	return nil
}

// OnError handles an error and returns a supervision directive.
func (a *BaseActor) OnError(ctx context.Context, err error) Directive {
	if a.onError != nil {
		return a.onError(ctx, err)
	}
	// Default: try to resume
	return DirectiveResume
}

// Reply sends a reply to the original message sender.
func (a *BaseActor) Reply(ctx context.Context, original *Message, reply *Message) error {
	if a.replySender == nil {
		return fmt.Errorf("no reply sender configured")
	}

	// Set up reply routing
	reply.FromNS = a.Namespace()
	reply.ToNS = original.FromNS
	if reply.SessionID == "" {
		reply.SessionID = original.SessionID
	}
	if reply.Workspace == "" {
		reply.Workspace = original.Workspace
	}
	if reply.Headers == nil && len(original.Headers) > 0 {
		reply.Headers = make(map[string]string, len(original.Headers))
		for k, v := range original.Headers {
			reply.Headers[k] = v
		}
	}
	if reply.CreatedAt.IsZero() {
		reply.CreatedAt = time.Now()
	}

	return a.replySender(ctx, reply)
}

// SetTimer sets a named timer that will fire OnTimeout after the duration.
func (a *BaseActor) SetTimer(name string, duration time.Duration, data any) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Cancel existing timer with same name
	if existing, ok := a.timers[name]; ok {
		existing.Stop()
	}

	timer := time.AfterFunc(duration, func() {
		// Timer callback - would need to be routed through supervisor
		// For now, just clean up the timer
		a.mu.Lock()
		delete(a.timers, name)
		a.mu.Unlock()
	})

	a.timers[name] = timer
}

// CancelTimer cancels a named timer.
func (a *BaseActor) CancelTimer(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	timer, ok := a.timers[name]
	if !ok {
		return false
	}

	timer.Stop()
	delete(a.timers, name)
	return true
}

// Ensure BaseActor implements Actor interface.
var _ Actor = (*BaseActor)(nil)
