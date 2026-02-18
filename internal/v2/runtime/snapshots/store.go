package snapshots

import (
	"sync/atomic"
	"time"

	coreevents "github.com/jkatigb/agentctl/internal/v2/core/events"
)

// RuntimeSnapshot is an immutable point-in-time runtime state view.
type RuntimeSnapshot struct {
	Version   int64          `json:"version"`
	UpdatedAt time.Time      `json:"updated_at"`
	Digest    DigestSnapshot `json:"digest"`
}

// DigestSnapshot is the first maintenance read model built from runtime events.
type DigestSnapshot struct {
	LastEventID   string               `json:"last_event_id,omitempty"`
	LastEventType coreevents.EventType `json:"last_event_type,omitempty"`
	TotalEvents   int64                `json:"total_events"`
	RunsStarted   int64                `json:"runs_started"`
	RunsCompleted int64                `json:"runs_completed"`
	RunsFailed    int64                `json:"runs_failed"`
	TurnsRecorded int64                `json:"turns_recorded"`
	RunStatus     map[string]string    `json:"run_status,omitempty"`
}

// Store provides atomic load/store semantics for runtime snapshots.
type Store struct {
	state atomic.Pointer[RuntimeSnapshot]
}

// NewStore creates an empty snapshot store.
func NewStore() *Store {
	return &Store{}
}

// Load returns a deep-cloned snapshot value.
func (s *Store) Load() RuntimeSnapshot {
	if s == nil {
		return RuntimeSnapshot{}
	}
	ptr := s.state.Load()
	if ptr == nil {
		return RuntimeSnapshot{}
	}
	return cloneSnapshot(*ptr)
}

// Store replaces the current snapshot atomically with a deep clone of input.
func (s *Store) Store(snapshot RuntimeSnapshot) {
	if s == nil {
		return
	}
	cloned := cloneSnapshot(snapshot)
	s.state.Store(&cloned)
}

func cloneSnapshot(in RuntimeSnapshot) RuntimeSnapshot {
	out := in
	out.Digest.RunStatus = cloneStringMap(in.Digest.RunStatus)
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
