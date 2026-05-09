package workers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
)

const (
	DefaultBuffer         = 64
	DefaultPublishTimeout = 25 * time.Millisecond
)

var (
	ErrClosed       = errors.New("v2 runtime workers: state component closed")
	ErrBackpressure = errors.New("v2 runtime workers: backpressure")
)

type OverflowPolicy string

const (
	OverflowDropNewest OverflowPolicy = "drop_newest"
	OverflowDropOldest OverflowPolicy = "drop_oldest"
	OverflowBlock      OverflowPolicy = "block"
)

type Config struct {
	Buffer         int
	OverflowPolicy OverflowPolicy
	PublishTimeout time.Duration
	Registry       coreworker.Registry
}

type Stats struct {
	Published    int64
	Applied      int64
	Dropped      int64
	Overflow     int64
	Backpressure int64
	QueueDepth   int
	Policy       OverflowPolicy
}

// Snapshot is an immutable point-in-time runtime worker view.
type Snapshot struct {
	Version          int64                        `json:"version"`
	UpdatedAt        time.Time                    `json:"updated_at"`
	Workers          map[string]coreworker.Record `json:"workers,omitempty"`
	ChildrenByParent map[string][]string          `json:"children_by_parent,omitempty"`
}

// StateComponent owns worker lifecycle state updates on a single goroutine.
type StateComponent struct {
	cfg   Config
	queue chan coreworker.LifecycleEvent

	mu     sync.Mutex
	closed bool

	snapshot atomic.Pointer[Snapshot]

	published    atomic.Int64
	applied      atomic.Int64
	dropped      atomic.Int64
	overflow     atomic.Int64
	backpressure atomic.Int64
}

func NewStateComponent(cfg Config) *StateComponent {
	if cfg.Buffer <= 0 {
		cfg.Buffer = DefaultBuffer
	}
	if cfg.OverflowPolicy == "" {
		cfg.OverflowPolicy = OverflowDropNewest
	}
	if cfg.PublishTimeout <= 0 {
		cfg.PublishTimeout = DefaultPublishTimeout
	}
	s := &StateComponent{
		cfg:   cfg,
		queue: make(chan coreworker.LifecycleEvent, cfg.Buffer),
	}
	snapshot := Snapshot{
		Workers:          map[string]coreworker.Record{},
		ChildrenByParent: map[string][]string{},
	}
	s.snapshot.Store(&snapshot)
	return s
}

func (s *StateComponent) Run(ctx context.Context) error {
	if s == nil {
		return nil
	}

	workers := map[string]coreworker.Record{}
	children := map[string][]string{}
	version := int64(0)

	for {
		select {
		case <-ctx.Done():
			s.closeQueue()
			return nil
		case evt, ok := <-s.queue:
			if !ok {
				return nil
			}
			applyEvent(workers, children, evt)
			version++
			s.applied.Add(1)
			if s.cfg.Registry != nil {
				record := workers[evt.WorkerID]
				if err := s.cfg.Registry.Upsert(ctx, record); err != nil {
					return err
				}
			}
			snapshot := Snapshot{
				Version:          version,
				UpdatedAt:        chooseTime(evt.ObservedAt, time.Now().UTC()),
				Workers:          cloneWorkers(workers),
				ChildrenByParent: cloneChildren(children),
			}
			s.snapshot.Store(&snapshot)
		}
	}
}

func (s *StateComponent) Publish(ctx context.Context, evt coreworker.LifecycleEvent) error {
	if s == nil {
		return ErrClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(evt.WorkerID) == "" && strings.TrimSpace(evt.AgentID) == "" {
		return errors.New("runtime worker event requires worker_id or agent_id")
	}
	if evt.WorkerID == "" {
		evt.WorkerID = strings.TrimSpace(evt.AgentID)
	}
	s.published.Add(1)

	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrClosed
	}

	select {
	case s.queue <- evt:
		return nil
	default:
		s.overflow.Add(1)
	}

	switch s.cfg.OverflowPolicy {
	case OverflowDropOldest:
		select {
		case <-s.queue:
			s.dropped.Add(1)
		default:
		}
		select {
		case s.queue <- evt:
			return nil
		default:
			s.dropped.Add(1)
			return nil
		}
	case OverflowBlock:
		timeout := s.cfg.PublishTimeout
		var cancel context.CancelFunc
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		select {
		case s.queue <- evt:
			return nil
		case <-ctx.Done():
			s.backpressure.Add(1)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return ErrBackpressure
			}
			return ctx.Err()
		}
	default:
		s.dropped.Add(1)
		return nil
	}
}

func (s *StateComponent) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	ptr := s.snapshot.Load()
	if ptr == nil {
		return Snapshot{}
	}
	return Snapshot{
		Version:          ptr.Version,
		UpdatedAt:        ptr.UpdatedAt,
		Workers:          cloneWorkers(ptr.Workers),
		ChildrenByParent: cloneChildren(ptr.ChildrenByParent),
	}
}

func (s *StateComponent) Stats() Stats {
	if s == nil {
		return Stats{}
	}
	return Stats{
		Published:    s.published.Load(),
		Applied:      s.applied.Load(),
		Dropped:      s.dropped.Load(),
		Overflow:     s.overflow.Load(),
		Backpressure: s.backpressure.Load(),
		QueueDepth:   len(s.queue),
		Policy:       s.cfg.OverflowPolicy,
	}
}

func (s *StateComponent) closeQueue() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	// Publishers may still be racing with shutdown; mark closed without closing
	// the channel so in-flight sends cannot panic or trip the race detector.
	s.closed = true
}

func applyEvent(workers map[string]coreworker.Record, children map[string][]string, evt coreworker.LifecycleEvent) {
	workerID := strings.TrimSpace(evt.WorkerID)
	if workerID == "" {
		workerID = strings.TrimSpace(evt.AgentID)
	}
	current := workers[workerID]
	next := mergeRecord(current, evt)

	// Preserve monotonic terminal state unless the new event is also terminal.
	if coreworker.IsTerminal(current.Status) && !coreworker.IsTerminal(next.Status) {
		next.Status = current.Status
		next.StopReason = chooseNonEmpty(current.StopReason, next.StopReason)
		if current.ExitCode != 0 && next.ExitCode == 0 {
			next.ExitCode = current.ExitCode
		}
	}

	workers[workerID] = next
	rebuildParentIndex(children, workers)
}

func mergeRecord(current coreworker.Record, evt coreworker.LifecycleEvent) coreworker.Record {
	next := current
	next.WorkerID = chooseNonEmpty(strings.TrimSpace(evt.WorkerID), current.WorkerID, strings.TrimSpace(evt.AgentID))
	if evt.BackendKind != "" {
		next.BackendKind = evt.BackendKind
	}
	next.AgentID = chooseNonEmpty(strings.TrimSpace(evt.AgentID), current.AgentID)
	next.RunID = chooseNonEmpty(strings.TrimSpace(evt.RunID), current.RunID)
	next.SessionID = chooseNonEmpty(strings.TrimSpace(evt.SessionID), current.SessionID)
	next.ParentAgentID = chooseNonEmpty(strings.TrimSpace(evt.ParentAgentID), current.ParentAgentID)
	next.ParentWorkerID = chooseNonEmpty(strings.TrimSpace(evt.ParentWorkerID), current.ParentWorkerID)
	next.WorkspaceID = chooseNonEmpty(strings.TrimSpace(evt.WorkspaceID), current.WorkspaceID)
	next.Role = chooseNonEmpty(strings.TrimSpace(evt.Role), current.Role)
	next.Tag = chooseNonEmpty(strings.TrimSpace(evt.Tag), current.Tag)
	next.PID = chooseNonEmpty(strings.TrimSpace(evt.PID), current.PID)
	next.StopReason = chooseNonEmpty(strings.TrimSpace(evt.StopReason), current.StopReason)
	if evt.Status != "" {
		next.Status = evt.Status
	}
	if evt.ExitCode != 0 || current.ExitCode == 0 {
		next.ExitCode = evt.ExitCode
	}
	if evt.Metadata != nil {
		next.Metadata = mergeMetadata(current.Metadata, evt.Metadata)
	}
	if len(evt.RawState) > 0 {
		next.RawState = append(next.RawState[:0], evt.RawState...)
	}
	if !evt.ObservedAt.IsZero() {
		next.UpdatedAt = evt.ObservedAt.UTC()
		if next.StartedAt.IsZero() && (evt.EventKind == coreworker.EventWorkerSpawned || evt.EventKind == coreworker.EventWorkerStarted || evt.EventKind == coreworker.EventWorkerStateChanged) {
			next.StartedAt = evt.ObservedAt.UTC()
		}
		if evt.EventKind == coreworker.EventWorkerHeartbeat {
			next.HeartbeatAt = evt.ObservedAt.UTC()
		}
	}
	next.RawState = mergeSyntheticState(next.RawState, next, evt)
	return next
}

func mergeSyntheticState(raw json.RawMessage, record coreworker.Record, evt coreworker.LifecycleEvent) json.RawMessage {
	if record.BackendKind != coreworker.BackendSubprocess && evt.EventKind != coreworker.EventWorkerLogChunk {
		return raw
	}

	root := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		_ = json.Unmarshal(raw, &root)
	}
	foxctl, _ := root["foxctl"].(map[string]any)
	if foxctl == nil {
		foxctl = map[string]any{}
	}
	foxctl["status"] = string(record.Status)
	if record.AgentID != "" {
		foxctl["agent"] = record.AgentID
	}
	if record.RunID != "" {
		foxctl["run_id"] = record.RunID
	}
	if record.WorkspaceID != "" {
		foxctl["workspace_id"] = record.WorkspaceID
	}
	if record.PID != "" {
		foxctl["pid"] = record.PID
	}
	if record.StopReason != "" {
		foxctl["stop_reason"] = record.StopReason
	}
	if record.ExitCode != 0 {
		foxctl["exit_code"] = record.ExitCode
	}
	if record.Role != "" {
		foxctl["role"] = record.Role
	}
	if evt.EventKind == coreworker.EventWorkerLogChunk {
		stream := strings.TrimSpace(fmt.Sprint(evt.Metadata["stream"]))
		chunk := strings.TrimSpace(fmt.Sprint(evt.Metadata["chunk"]))
		if chunk != "" {
			recentLogs, _ := foxctl["recent_logs"].([]any)
			entry := map[string]any{
				"stream": stream,
				"text":   chunk,
			}
			if !evt.ObservedAt.IsZero() {
				entry["ts"] = evt.ObservedAt.UTC().Format(time.RFC3339Nano)
			}
			recentLogs = append(recentLogs, entry)
			if len(recentLogs) > 50 {
				recentLogs = recentLogs[len(recentLogs)-50:]
			}
			foxctl["recent_logs"] = recentLogs
		}
	}
	root["foxctl"] = foxctl
	encoded, err := json.Marshal(root)
	if err != nil {
		return raw
	}
	return encoded
}

func rebuildParentIndex(children map[string][]string, workers map[string]coreworker.Record) {
	for key := range children {
		delete(children, key)
	}
	for workerID, record := range workers {
		parentKey := parentKey(record)
		if parentKey == "" {
			continue
		}
		children[parentKey] = append(children[parentKey], workerID)
	}
	for key, ids := range children {
		children[key] = append([]string(nil), ids...)
	}
}

func parentKey(record coreworker.Record) string {
	if record.ParentWorkerID != "" {
		return "worker:" + strings.TrimSpace(record.ParentWorkerID)
	}
	if record.ParentAgentID != "" {
		return "agent:" + strings.TrimSpace(record.ParentAgentID)
	}
	return ""
}

func cloneWorkers(in map[string]coreworker.Record) map[string]coreworker.Record {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]coreworker.Record, len(in))
	for key, value := range in {
		cloned := value
		if len(value.Metadata) > 0 {
			meta := make(map[string]any, len(value.Metadata))
			for mk, mv := range value.Metadata {
				meta[mk] = mv
			}
			cloned.Metadata = meta
		}
		if len(value.RawState) > 0 {
			cloned.RawState = append(jsonClone(nil), value.RawState...)
		}
		out[key] = cloned
	}
	return out
}

func cloneChildren(in map[string][]string) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for key, ids := range in {
		out[key] = append([]string(nil), ids...)
	}
	return out
}

func mergeMetadata(current, incoming map[string]any) map[string]any {
	if len(current) == 0 && len(incoming) == 0 {
		return nil
	}
	out := make(map[string]any, len(current)+len(incoming))
	for key, value := range current {
		out[key] = value
	}
	for key, value := range incoming {
		out[key] = value
	}
	return out
}

func chooseNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func chooseTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value.UTC()
		}
	}
	return time.Time{}
}

func jsonClone(dst []byte) []byte {
	if dst == nil {
		return make([]byte, 0, 128)
	}
	return dst[:0]
}
