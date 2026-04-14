package companion

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/runtime/actor"
	"github.com/jkatigb/agentctl/internal/storage/blackboard"
	"github.com/jkatigb/agentctl/internal/storage/contextvar"
	"github.com/rs/zerolog"
)

// Executor manages companion actors and their lifecycle.
// It provides a simple API for spawning companion agents that listen to mailboxes.
type Executor struct {
	contextStore  contextvar.Store
	boardStore    blackboard.BoardStore
	serviceConfig ServiceConfig
	logger        zerolog.Logger

	mu       sync.RWMutex
	actors   map[string]*CompanionActor
	services map[string]*Service
}

// ExecutorConfig configures the companion executor.
type ExecutorConfig struct {
	// ContextStore for RLM context variables.
	ContextStore contextvar.Store

	// BoardStore for message responses.
	BoardStore blackboard.BoardStore

	// ServiceConfig is the base configuration for companion services.
	ServiceConfig ServiceConfig

	// Logger for structured logging.
	Logger zerolog.Logger
}

// NewExecutor creates a new companion executor.
// NewExecutor initializes a companion executor with shared stores.
//
// Index:
// - Purpose: Create a companion executor for managing companion actors
// - Flow: validate stores → allocate maps → return executor
// - FailureModes: missing context store, missing board store
// - Related: Executor.Spawn, Executor.Stop
// - Keywords: companion_executor, context_store, board_store, actors
func NewExecutor(cfg ExecutorConfig) (*Executor, error) {
	if cfg.ContextStore == nil {
		return nil, fmt.Errorf("context store is required")
	}
	if cfg.BoardStore == nil {
		return nil, fmt.Errorf("board store is required")
	}

	return &Executor{
		contextStore:  cfg.ContextStore,
		boardStore:    cfg.BoardStore,
		serviceConfig: cfg.ServiceConfig,
		logger:        cfg.Logger,
		actors:        make(map[string]*CompanionActor),
		services:      make(map[string]*Service),
	}, nil
}

// SpawnConfig configures a companion agent spawn.
type SpawnConfig struct {
	// Name is the agent name, used as part of the namespace.
	// Namespace will be "companion:<name>".
	Name string

	// WorkspaceID scopes board messages (optional).
	WorkspaceID string

	// Personality overrides the default personality.
	Personality string

	// Provider overrides the default LLM provider.
	Provider string

	// Model overrides the default model.
	Model string

	// APIKey overrides the default API key.
	APIKey string
}

// Spawn creates and starts a new companion actor.
// The actor will listen on namespace "companion:<name>".
// Spawn creates and starts a companion actor for the given config.
//
// Index:
// - Purpose: Create and start a companion actor and service
// - Flow: validate name → build service config → create service → create actor → start actor → store
// - SideEffects: starts actor; allocates service
// - FailureModes: invalid name, actor creation errors, start errors
// - Related: NewCompanionActor, NewService, CompanionActor.Start
// - Keywords: companion_spawn, namespace, actor, service, workspace_id
func (e *Executor) Spawn(ctx context.Context, cfg SpawnConfig) (*CompanionActor, error) {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	namespace := "companion:" + strings.ToLower(name)
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if already exists
	if existing, ok := e.actors[namespace]; ok {
		return existing, nil // Return existing actor
	}

	// Create service config for this actor
	svcConfig := e.serviceConfig
	if cfg.Personality != "" {
		svcConfig.DefaultPersonality = cfg.Personality
	}
	if cfg.Provider != "" {
		svcConfig.LLMProvider = cfg.Provider
	}
	if cfg.Model != "" {
		svcConfig.LLMModel = cfg.Model
	}
	if cfg.APIKey != "" {
		svcConfig.LLMAPIKey = cfg.APIKey
	}
	svcConfig.Logger = e.logger.With().Str("companion", namespace).Logger()

	// Create companion service (nil TurnLock: each executor manages its own conversations)
	svc := NewService(e.contextStore, svcConfig, nil)

	// Create companion actor
	actorCfg := CompanionActorConfig{
		Namespace:   namespace,
		Service:     svc,
		BoardStore:  e.boardStore,
		WorkspaceID: cfg.WorkspaceID,
		Logger:      e.logger.With().Str("actor", namespace).Logger(),
	}

	companionActor, err := NewCompanionActor(actorCfg)
	if err != nil {
		return nil, fmt.Errorf("create actor: %w", err)
	}

	// Start the actor
	if err := companionActor.Start(ctx); err != nil {
		return nil, fmt.Errorf("start actor: %w", err)
	}

	// Track the actor and service
	e.actors[namespace] = companionActor
	e.services[namespace] = svc

	e.logger.Info().
		Str("namespace", namespace).
		Str("name", cfg.Name).
		Msg("Spawned companion actor")

	return companionActor, nil
}

// Get returns an existing companion actor by namespace.
func (e *Executor) Get(namespace string) (*CompanionActor, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	actor, ok := e.actors[namespace]
	return actor, ok
}

// GetService returns the companion service for a namespace.
// Used to access memory stats and other service-level operations.
func (e *Executor) GetService(namespace string) *Service {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.services[namespace]
}

// GetByName returns a companion actor by name.
func (e *Executor) GetByName(name string) (*CompanionActor, bool) {
	namespace := "companion:" + strings.ToLower(name)
	return e.Get(namespace)
}

// List returns all active companion actors.
func (e *Executor) List() []*CompanionActor {
	e.mu.RLock()
	defer e.mu.RUnlock()

	actors := make([]*CompanionActor, 0, len(e.actors))
	for _, a := range e.actors {
		actors = append(actors, a)
	}
	return actors
}

// Stop stops a companion actor by namespace.
// Stop terminates a companion actor and removes it from the registry.
//
// Index:
// - Purpose: Stop a running companion actor by namespace
// - Flow: lookup actor → stop actor → remove actor/service → return
// - SideEffects: stops actor; logs event
// - FailureModes: actor not found, stop errors
// - Related: Executor.Spawn, CompanionActor.Stop
// - Keywords: companion_stop, namespace, actor, service, remove
func (e *Executor) Stop(ctx context.Context, namespace string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	actor, ok := e.actors[namespace]
	if !ok {
		return fmt.Errorf("actor not found: %s", namespace)
	}

	if err := actor.Stop(ctx); err != nil {
		return fmt.Errorf("stop actor: %w", err)
	}

	delete(e.actors, namespace)
	delete(e.services, namespace)

	e.logger.Info().
		Str("namespace", namespace).
		Msg("Stopped companion actor")

	return nil
}

// StopAll stops all companion actors.
func (e *Executor) StopAll(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var lastErr error
	for namespace, actor := range e.actors {
		if err := actor.Stop(ctx); err != nil {
			e.logger.Error().
				Err(err).
				Str("namespace", namespace).
				Msg("Failed to stop actor")
			lastErr = err
		}
	}

	e.actors = make(map[string]*CompanionActor)
	e.services = make(map[string]*Service)

	return lastErr
}

// DirectMessage sends a message directly to a companion actor and waits for response.
// This bypasses the mailbox system for synchronous interactions.
func (e *Executor) DirectMessage(ctx context.Context, namespace, message string) (*ChatResponse, error) {
	e.mu.RLock()
	svc, ok := e.services[namespace]
	e.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("companion not found: %s", namespace)
	}

	return svc.Chat(ctx, ChatRequest{
		ConversationID: namespace,
		Message:        message,
	})
}

// ActorInfo contains information about a companion actor.
type ActorInfo struct {
	Namespace   string      `json:"namespace"`
	Name        string      `json:"name"`
	State       actor.State `json:"state"`
	WorkspaceID string      `json:"workspace_id,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

// Info returns information about all companion actors.
func (e *Executor) Info() []ActorInfo {
	e.mu.RLock()
	defer e.mu.RUnlock()

	info := make([]ActorInfo, 0, len(e.actors))
	for namespace, a := range e.actors {
		name := strings.TrimPrefix(namespace, "companion:")
		wsID, _ := a.config.Metadata["workspace_id"].(string)
		info = append(info, ActorInfo{
			Namespace:   namespace,
			Name:        name,
			State:       a.State(),
			WorkspaceID: wsID,
			CreatedAt:   a.CreatedAt,
		})
	}
	return info
}

// RegisterWithSupervisor registers companion actor factories with an actor supervisor.
// This allows the supervisor to spawn companion actors on demand when messages arrive.
func (e *Executor) RegisterWithSupervisor(supervisor ActorSupervisor, namespaces ...string) error {
	for _, namespace := range namespaces {
		e.mu.RLock()
		svc, ok := e.services[namespace]
		wsID := ""
		if a, exists := e.actors[namespace]; exists {
			wsID, _ = a.config.Metadata["workspace_id"].(string)
		}
		e.mu.RUnlock()

		if !ok {
			return fmt.Errorf("service not found: %s (spawn it first)", namespace)
		}

		factory := CompanionActorFactory(svc, e.boardStore, wsID, e.logger)
		reg := actor.Registration{
			Config:    actor.DefaultConfig(namespace),
			Factory:   factory,
			CreatedAt: time.Now(),
		}
		reg.Config.Role = "companion"

		if err := supervisor.Register(namespace, reg); err != nil {
			return fmt.Errorf("register %s: %w", namespace, err)
		}
	}
	return nil
}

// ActorSupervisor is the interface for registering actors with the supervisor.
type ActorSupervisor interface {
	Register(namespace string, reg actor.Registration) error
}
