package goruntime

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	coreworker "github.com/jkatigb/agentctl/internal/v2/core/worker"
)

type processEntry struct {
	workerID       string
	agentID        string
	process        *os.Process
	processGroupID int
	done           chan struct{}

	publisher EventPublisher
	now       func() time.Time
	baseEvent coreworker.LifecycleEvent

	mu              sync.Mutex
	cancelRequested bool
	cancelReason    string
	cancelSignal    string
}

func (e *processEntry) markCancelRequested(signal, reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cancelRequested = true
	e.cancelReason = strings.TrimSpace(reason)
	e.cancelSignal = strings.TrimSpace(signal)
}

func (e *processEntry) cancelState() (bool, string, string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cancelRequested, e.cancelSignal, e.cancelReason
}

func (e *processEntry) closeDone() {
	if e == nil || e.done == nil {
		return
	}
	select {
	case <-e.done:
		return
	default:
		close(e.done)
	}
}

func (e *processEntry) emit(ctx context.Context, evt coreworker.LifecycleEvent) error {
	if e == nil || e.publisher == nil {
		return nil
	}
	return e.publisher.Publish(ctx, evt)
}

func (e *processEntry) emitSignalEvents(ctx context.Context, signal, reason string) error {
	now := time.Now().UTC()
	if e.now != nil {
		now = e.now().UTC()
	}
	cancelEvt := e.baseEvent
	cancelEvt.EventKind = coreworker.EventWorkerCancelRequested
	cancelEvt.ObservedAt = now
	cancelEvt.Status = coreworker.StatusStopping
	cancelEvt.StopReason = strings.TrimSpace(reason)
	cancelEvt.Metadata = mergeSpawnMetadata(cancelEvt.Metadata, map[string]any{
		"signal": signal,
		"reason": strings.TrimSpace(reason),
	})
	if err := e.emit(ctx, cancelEvt); err != nil {
		return err
	}

	sentEvt := e.baseEvent
	sentEvt.EventKind = coreworker.EventWorkerSignalSent
	sentEvt.ObservedAt = now
	sentEvt.Status = coreworker.StatusStopping
	sentEvt.StopReason = strings.TrimSpace(reason)
	sentEvt.Metadata = mergeSpawnMetadata(sentEvt.Metadata, map[string]any{
		"signal": signal,
		"reason": strings.TrimSpace(reason),
	})
	return e.emit(ctx, sentEvt)
}

type processRegistry struct {
	mu       sync.Mutex
	byWorker map[string]*processEntry
	byAgent  map[string]*processEntry
}

func newProcessRegistry() *processRegistry {
	return &processRegistry{
		byWorker: map[string]*processEntry{},
		byAgent:  map[string]*processEntry{},
	}
}

func (r *processRegistry) register(entry *processEntry) {
	if r == nil || entry == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry.workerID != "" {
		r.byWorker[entry.workerID] = entry
	}
	if entry.agentID != "" {
		r.byAgent[entry.agentID] = entry
	}
}

func (r *processRegistry) unregister(workerID, agentID string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if workerID != "" {
		delete(r.byWorker, workerID)
	}
	if agentID != "" {
		delete(r.byAgent, agentID)
	}
}

func (r *processRegistry) find(workerID, agentID string) (*processEntry, error) {
	if r == nil {
		return nil, fmt.Errorf("process registry is not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if trimmed := strings.TrimSpace(workerID); trimmed != "" {
		if entry := r.byWorker[trimmed]; entry != nil {
			return entry, nil
		}
	}
	if trimmed := strings.TrimSpace(agentID); trimmed != "" {
		if entry := r.byAgent[trimmed]; entry != nil {
			return entry, nil
		}
	}
	return nil, os.ErrNotExist
}

var globalProcessRegistry = newProcessRegistry()

type ReattachProcessConfig struct {
	Record    coreworker.Record
	Process   *os.Process
	Publisher EventPublisher
	Now       func() time.Time
}

func RegisterReattachedProcess(cfg ReattachProcessConfig) {
	if cfg.Process == nil {
		return
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	record := cfg.Record
	globalProcessRegistry.register(&processEntry{
		workerID:       strings.TrimSpace(record.WorkerID),
		agentID:        strings.TrimSpace(record.AgentID),
		process:        cfg.Process,
		processGroupID: runtimeProcessGroupID(record),
		publisher:      cfg.Publisher,
		now:            now,
		baseEvent: coreworker.LifecycleEvent{
			WorkerID:       strings.TrimSpace(record.WorkerID),
			BackendKind:    record.BackendKind,
			AgentID:        strings.TrimSpace(record.AgentID),
			RunID:          strings.TrimSpace(record.RunID),
			SessionID:      strings.TrimSpace(record.SessionID),
			ParentAgentID:  strings.TrimSpace(record.ParentAgentID),
			ParentWorkerID: strings.TrimSpace(record.ParentWorkerID),
			WorkspaceID:    strings.TrimSpace(record.WorkspaceID),
			Status:         record.Status,
			Role:           strings.TrimSpace(record.Role),
			Tag:            strings.TrimSpace(record.Tag),
			PID:            strings.TrimSpace(record.PID),
			StopReason:     strings.TrimSpace(record.StopReason),
			ExitCode:       record.ExitCode,
			Metadata:       cloneMap(record.Metadata),
			RawState:       append([]byte(nil), record.RawState...),
		},
	})
}

func runtimeProcessGroupID(record coreworker.Record) int {
	if record.Metadata != nil {
		if raw, ok := record.Metadata["process_group_id"]; ok {
			if pgid, ok := toInt(raw); ok && pgid > 0 {
				return pgid
			}
		}
	}
	if pid, ok := toInt(record.PID); ok && pid > 0 {
		return pid
	}
	return 0
}

func toInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil && parsed > 0 {
			return parsed, true
		}
	}
	return 0, false
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
