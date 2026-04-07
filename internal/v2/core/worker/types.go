package worker

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/core/spawn"
)

// BackendKind identifies the runtime backend implementation.
type BackendKind string

const (
	BackendUnknown    BackendKind = "unknown"
	BackendJido       BackendKind = "jido"
	BackendSubprocess BackendKind = "subprocess"
)

// Status is the normalized lifecycle status for one runtime worker.
type Status string

const (
	StatusUnknown   Status = "unknown"
	StatusPending   Status = "pending"
	StatusStarting  Status = "starting"
	StatusRunning   Status = "running"
	StatusStopping  Status = "stopping"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// LifecycleEventKind is the normalized lifecycle event vocabulary for runtime backends.
type LifecycleEventKind string

const (
	EventWorkerSpawnRequested  LifecycleEventKind = "worker_spawn_requested"
	EventWorkerSpawned         LifecycleEventKind = "worker_spawned"
	EventWorkerStarted         LifecycleEventKind = "worker_started"
	EventWorkerHeartbeat       LifecycleEventKind = "worker_heartbeat"
	EventWorkerStateChanged    LifecycleEventKind = "worker_state_changed"
	EventWorkerCompleted       LifecycleEventKind = "worker_completed"
	EventWorkerFailed          LifecycleEventKind = "worker_failed"
	EventWorkerCancelRequested LifecycleEventKind = "worker_cancel_requested"
	EventWorkerCancelled       LifecycleEventKind = "worker_cancelled"
	EventWorkerSignalSent      LifecycleEventKind = "worker_signal_sent"
	EventWorkerLogChunk        LifecycleEventKind = "worker_log_chunk"
)

// LifecycleEvent is the normalized worker observation emitted by a runtime backend.
type LifecycleEvent struct {
	EventID        string             `json:"event_id,omitempty"`
	EventKind      LifecycleEventKind `json:"event_kind,omitempty"`
	ObservedAt     time.Time          `json:"observed_at,omitempty"`
	WorkerID       string             `json:"worker_id,omitempty"`
	BackendKind    BackendKind        `json:"backend_kind,omitempty"`
	AgentID        string             `json:"agent_id,omitempty"`
	RunID          string             `json:"run_id,omitempty"`
	SessionID      string             `json:"session_id,omitempty"`
	ParentAgentID  string             `json:"parent_agent_id,omitempty"`
	ParentWorkerID string             `json:"parent_worker_id,omitempty"`
	WorkspaceID    string             `json:"workspace_id,omitempty"`
	RequestID      string             `json:"request_id,omitempty"`
	CorrelationID  string             `json:"correlation_id,omitempty"`
	CausationID    string             `json:"causation_id,omitempty"`
	Status         Status             `json:"status,omitempty"`
	Role           string             `json:"role,omitempty"`
	Tag            string             `json:"tag,omitempty"`
	PID            string             `json:"pid,omitempty"`
	Attempt        int                `json:"attempt,omitempty"`
	StopReason     string             `json:"stop_reason,omitempty"`
	ExitCode       int                `json:"exit_code,omitempty"`
	Metadata       map[string]any     `json:"metadata,omitempty"`
	RawState       json.RawMessage    `json:"raw_state,omitempty"`
}

// Record is the Go-owned runtime view of one worker.
type Record struct {
	WorkerID         string          `json:"worker_id,omitempty"`
	BackendKind      BackendKind     `json:"backend_kind,omitempty"`
	BackendWorkerRef string          `json:"backend_worker_ref,omitempty"`
	AgentID          string          `json:"agent_id,omitempty"`
	RunID            string          `json:"run_id,omitempty"`
	SessionID        string          `json:"session_id,omitempty"`
	ParentAgentID    string          `json:"parent_agent_id,omitempty"`
	ParentWorkerID   string          `json:"parent_worker_id,omitempty"`
	WorkspaceID      string          `json:"workspace_id,omitempty"`
	Role             string          `json:"role,omitempty"`
	Status           Status          `json:"status,omitempty"`
	Tag              string          `json:"tag,omitempty"`
	PID              string          `json:"pid,omitempty"`
	StartedAt        time.Time       `json:"started_at,omitempty"`
	UpdatedAt        time.Time       `json:"updated_at,omitempty"`
	HeartbeatAt      time.Time       `json:"heartbeat_at,omitempty"`
	StopReason       string          `json:"stop_reason,omitempty"`
	ExitCode         int             `json:"exit_code,omitempty"`
	Metadata         map[string]any  `json:"metadata,omitempty"`
	RawState         json.RawMessage `json:"raw_state,omitempty"`
}

// LookupRequest requests one worker record.
type LookupRequest struct {
	WorkerID string `json:"worker_id,omitempty"`
	AgentID  string `json:"agent_id,omitempty"`
}

// ChildrenRequest requests the children of one parent.
type ChildrenRequest struct {
	ParentAgentID  string `json:"parent_agent_id,omitempty"`
	ParentWorkerID string `json:"parent_worker_id,omitempty"`
}

// SignalRequest requests a runtime signal against one worker.
type SignalRequest struct {
	WorkerID      string          `json:"worker_id,omitempty"`
	AgentID       string          `json:"agent_id,omitempty"`
	RequestID     string          `json:"request_id,omitempty"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	CausationID   string          `json:"causation_id,omitempty"`
	Signal        string          `json:"signal,omitempty"`
	Reason        string          `json:"reason,omitempty"`
	Payload       json.RawMessage `json:"payload,omitempty"`
}

// SignalResponse is the normalized result of signaling a worker.
type SignalResponse struct {
	WorkerID  string `json:"worker_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Status    Status `json:"status,omitempty"`
	MessageID string `json:"message_id,omitempty"`
}

// Spawner creates one child worker from a canonical spawn request.
type Spawner interface {
	SpawnChild(ctx context.Context, req spawn.Request) (spawn.Response, error)
}

// StateReader reads Go-owned worker state for runtime trees and status views.
type StateReader interface {
	Worker(ctx context.Context, req LookupRequest) (Record, error)
	Children(ctx context.Context, req ChildrenRequest) ([]Record, error)
}

// Registry persists and serves canonical worker records.
type Registry interface {
	StateReader
	Upsert(ctx context.Context, record Record) error
}

// Signaler delivers a runtime signal to one worker.
type Signaler interface {
	SignalWorker(ctx context.Context, req SignalRequest) (SignalResponse, error)
}

// NormalizeStatus maps backend-specific lifecycle labels into the shared status vocabulary.
func NormalizeStatus(raw string) Status {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "pending":
		return StatusPending
	case "starting":
		return StatusStarting
	case "running", "active", "processing":
		return StatusRunning
	case "stopping":
		return StatusStopping
	case "completed", "ok", "success", "done":
		return StatusCompleted
	case "failed", "error":
		return StatusFailed
	case "cancelled", "canceled":
		return StatusCancelled
	case "":
		return StatusUnknown
	default:
		return StatusUnknown
	}
}

// IsTerminal reports whether the status is terminal for reconcile/idempotency logic.
func IsTerminal(status Status) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}
