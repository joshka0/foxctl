package actor

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/joshka0/foxctl/internal/storage/trajectory"
	"github.com/oklog/ulid/v2"
)

// TrajectoryPersister persists actor events to trajectory.db.
// It implements the Persister interface for EventBus integration.
//
// Design: Each workspace gets a "system" trajectory that captures
// actor infrastructure events. Events are mapped from actor.Event
// to trajectory.Event format.
type TrajectoryPersister struct {
	store trajectory.Store

	// Cache of workspace → trajectory ID mappings.
	mu            sync.RWMutex
	trajectoryIDs map[string]string // workspace → trajectory_id
}

// NewTrajectoryPersister creates a new TrajectoryPersister.
func NewTrajectoryPersister(store trajectory.Store) *TrajectoryPersister {
	return &TrajectoryPersister{
		store:         store,
		trajectoryIDs: make(map[string]string),
	}
}

// Persist implements the Persister interface.
// It converts actor.Event to trajectory.Event and stores it.
func (p *TrajectoryPersister) Persist(ctx context.Context, event Event) error {
	// Skip events that shouldn't be persisted
	if !event.Type.ShouldPersist() {
		return nil
	}

	// Determine workspace - use default if not set
	workspace := event.Workspace
	if workspace == "" {
		workspace = "default"
	}

	// Get or create trajectory for this workspace
	trajectoryID, err := p.getOrCreateTrajectory(ctx, workspace, event.SessionID)
	if err != nil {
		return fmt.Errorf("get or create trajectory: %w", err)
	}

	// Map actor event to trajectory event
	trajEvent := mapActorEventToTrajectory(event, trajectoryID)

	// Insert event using the provided context
	if _, err := p.store.InsertEvent(ctx, trajEvent); err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return nil
}

// getOrCreateTrajectory returns a trajectory ID for the workspace,
// creating one if it doesn't exist.
func (p *TrajectoryPersister) getOrCreateTrajectory(ctx context.Context, workspace, sessionID string) (string, error) {
	// Check cache first
	p.mu.RLock()
	if id, ok := p.trajectoryIDs[workspace]; ok {
		p.mu.RUnlock()
		return id, nil
	}
	p.mu.RUnlock()

	// Create new trajectory
	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if id, ok := p.trajectoryIDs[workspace]; ok {
		return id, nil
	}

	traj := trajectory.Trajectory{
		ID:          ulid.Make().String(),
		WorkspaceID: workspace,
		AgentRole:   "actor-system",
		Status:      trajectory.StatusOK,
		Summary:     "Actor system infrastructure events",
		SessionID:   sessionID,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	created, err := p.store.InsertTrajectory(ctx, traj)
	if err != nil {
		return "", fmt.Errorf("create trajectory: %w", err)
	}

	p.trajectoryIDs[workspace] = created.ID
	return created.ID, nil
}

// mapActorEventToTrajectory converts an actor.Event to a trajectory.Event.
func mapActorEventToTrajectory(event Event, trajectoryID string) trajectory.Event {
	kind := mapEventTypeToKind(event.Type)

	// Build inline data from event
	dataInline := make(map[string]any)
	if event.Source != "" {
		dataInline["source"] = event.Source
	}
	if event.Target != "" {
		dataInline["target"] = event.Target
	}
	if len(event.Data) > 0 {
		var data any
		if err := json.Unmarshal(event.Data, &data); err == nil {
			dataInline["payload"] = data
		}
	}

	// Build metadata
	meta := &trajectory.EventMeta{}
	if event.SessionID != "" {
		meta.TraceID = event.SessionID // Use session as trace for correlation
	}

	return trajectory.Event{
		ID:           event.ID,
		TrajectoryID: trajectoryID,
		TS:           event.Timestamp,
		Kind:         kind,
		Actor:        event.Source,
		Status:       string(event.Type),
		DataInline:   dataInline,
		Meta:         meta,
	}
}

// mapEventTypeToKind maps actor.EventType to trajectory.EventKind.
func mapEventTypeToKind(eventType EventType) trajectory.EventKind {
	switch eventType {
	case EventMailReceived:
		return trajectory.EventKindToolCall
	case EventMailSent:
		return trajectory.EventKindToolResult
	case EventMailAcked, EventMailExpired:
		return trajectory.EventKindTaskTransition
	case EventAgentStarted, EventAgentStopped:
		return trajectory.EventKindTaskTransition
	case EventAgentError:
		return trajectory.EventKindAgentThought
	case EventTaskCompleted:
		return trajectory.EventKindTaskTransition
	default:
		return trajectory.EventKindAgentThought
	}
}

// Close releases resources. Currently a no-op as the store
// is owned by the caller.
func (p *TrajectoryPersister) Close() error {
	return nil
}

// Ensure TrajectoryPersister implements Persister.
var _ Persister = (*TrajectoryPersister)(nil)
