package api

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/executil"
	"github.com/jkatigb/agentctl/internal/platform/config"
	v2goruntime "github.com/jkatigb/agentctl/internal/v2/adapters/goruntime"
	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
	libsqlevents "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/events"
	libsqlorchestration "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/orchestration"
	libsqlworkers "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/workers"
	coreevents "github.com/jkatigb/agentctl/internal/v2/core/events"
	corespawn "github.com/jkatigb/agentctl/internal/v2/core/spawn"
	coreworker "github.com/jkatigb/agentctl/internal/v2/core/worker"
	runtimeworkers "github.com/jkatigb/agentctl/internal/v2/runtime/workers"
	v2services "github.com/jkatigb/agentctl/internal/v2/services"
)

const (
	EnvOrchestrationRuntimeBackend          = "AGENTCTL_V2_ORCHESTRATION_RUNTIME_BACKEND"
	orchestrationRuntimeBackendJidoAPI      = "jido"
	orchestrationRuntimeBackendGoruntimeAPI = "goruntime"
)

// OrchestrationRuntimeHost is the long-lived runtime host used by web dispatch/refresh flows.
type OrchestrationRuntimeHost interface {
	Run(ctx context.Context) error
	Close() error
	Spawn(ctx context.Context, req corespawn.Request) (corespawn.Response, error)
	Refresh(ctx context.Context, workspaceID, requestID string) error
	Signal(ctx context.Context, req coreworker.SignalRequest) (coreworker.SignalResponse, error)
}

// ResolveOrchestrationRuntimeBackend returns the active orchestration backend for web/runtime hosts.
func ResolveOrchestrationRuntimeBackend() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvOrchestrationRuntimeBackend))) {
	case orchestrationRuntimeBackendGoruntimeAPI:
		return orchestrationRuntimeBackendGoruntimeAPI
	case orchestrationRuntimeBackendJidoAPI:
		return orchestrationRuntimeBackendJidoAPI
	default:
		return orchestrationRuntimeBackendJidoAPI
	}
}

// NewOrchestrationRuntimeHost creates a long-lived subprocess-backed host when enabled.
func NewOrchestrationRuntimeHost(ctx context.Context, cfg config.Config, log zerolog.Logger) (OrchestrationRuntimeHost, error) {
	if ResolveOrchestrationRuntimeBackend() != orchestrationRuntimeBackendGoruntimeAPI {
		return nil, nil
	}

	eventStore, err := openOrchestrationEventStore(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open event store: %w", err)
	}
	orchestrationStore, closeOrchestration, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		_ = eventStore.Close()
		return nil, fmt.Errorf("open orchestration store: %w", err)
	}
	workerStore, closeWorkers, err := libsqlworkers.Open(ctx, cfg.Storage.Root)
	if err != nil {
		if closeOrchestration != nil {
			_ = closeOrchestration()
		}
		_ = eventStore.Close()
		return nil, fmt.Errorf("open worker store: %w", err)
	}

	workspaceRoot, err := os.Getwd()
	if err != nil {
		workspaceRoot = "."
	}
	workerState := runtimeworkers.NewStateComponent(runtimeworkers.Config{
		Buffer:         128,
		OverflowPolicy: runtimeworkers.OverflowBlock,
		Registry:       workerStore,
	})
	spawner, err := v2goruntime.NewManagedAgentSpawner(v2goruntime.ManagedAgentSpawnerConfig{
		StorageRoot:   cfg.Storage.Root,
		WorkspaceRoot: workspaceRoot,
		BinaryPath:    executil.AgentctlBin(),
		Publisher:     workerState,
	})
	if err != nil {
		_ = closeWorkers()
		if closeOrchestration != nil {
			_ = closeOrchestration()
		}
		_ = eventStore.Close()
		return nil, fmt.Errorf("configure managed spawner: %w", err)
	}
	signaler := v2goruntime.NewSignaler(v2goruntime.SignalerConfig{
		Publisher: workerState,
	})

	return &persistentOrchestrationRuntimeHost{
		cfg:                cfg,
		log:                log,
		eventStore:         eventStore,
		orchestrationStore: orchestrationStore,
		closeOrchestration: closeOrchestration,
		workerStore:        workerStore,
		closeWorkers:       closeWorkers,
		workerState:        workerState,
		spawner:            spawner,
		signaler:           signaler,
		parentAgentIDs:     map[string]struct{}{},
	}, nil
}

type persistentOrchestrationRuntimeHost struct {
	cfg                config.Config
	log                zerolog.Logger
	eventStore         *libsqlevents.Store
	orchestrationStore *libsqlorchestration.Store
	closeOrchestration func() error
	workerStore        *libsqlworkers.Store
	closeWorkers       func() error
	workerState        *runtimeworkers.StateComponent
	spawner            *v2goruntime.ManagedAgentSpawner
	signaler           *v2goruntime.Signaler

	mu             sync.Mutex
	parentAgentIDs map[string]struct{}
}

func (h *persistentOrchestrationRuntimeHost) Run(ctx context.Context) error {
	if h == nil || h.workerState == nil {
		return nil
	}
	if err := h.reattachActiveWorkers(ctx); err != nil {
		return err
	}
	return h.workerState.Run(ctx)
}

func (h *persistentOrchestrationRuntimeHost) Close() error {
	if h == nil {
		return nil
	}
	var firstErr error
	if h.closeWorkers != nil {
		if err := h.closeWorkers(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if h.closeOrchestration != nil {
		if err := h.closeOrchestration(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if h.eventStore != nil {
		if err := h.eventStore.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h *persistentOrchestrationRuntimeHost) Spawn(ctx context.Context, req corespawn.Request) (corespawn.Response, error) {
	if h == nil || h.spawner == nil {
		return corespawn.Response{}, fmt.Errorf("orchestration runtime host is not configured")
	}
	req.ParentAgentID = chooseNonEmpty(strings.TrimSpace(req.ParentAgentID), resolveOrchestrationDispatchParentAgentID())
	if strings.TrimSpace(req.ParentAgentID) == "" {
		return corespawn.Response{}, fmt.Errorf("parent_agent_id is required")
	}
	h.addParentAgentID(req.ParentAgentID)
	spawnService := v2services.NewSpawnService(v2services.SpawnDependencies{
		RuntimeSpawner: h.spawner,
	})
	return spawnService.Spawn(ctx, req)
}

func (h *persistentOrchestrationRuntimeHost) Refresh(ctx context.Context, workspaceID, requestID string) error {
	if h == nil || h.eventStore == nil || h.orchestrationStore == nil || h.workerStore == nil {
		return fmt.Errorf("orchestration runtime host is not configured")
	}
	if err := h.orchestrationStore.ReplayFrom(ctx, h.eventStore, coreevents.ReplayFilter{}); err != nil {
		return fmt.Errorf("refresh orchestration projection replay: %w", err)
	}
	parentIDs := h.snapshotParentAgentIDs()
	if len(parentIDs) == 0 {
		if parentID := strings.TrimSpace(resolveOrchestrationDispatchParentAgentID()); parentID != "" {
			parentIDs = append(parentIDs, parentID)
		}
	}
	reconciler, err := v2goruntime.NewOrchestrationReconciler(v2goruntime.OrchestrationReconcilerConfig{
		Events:              h.eventStore,
		Projections:         h.orchestrationStore,
		Reader:              h.orchestrationStore,
		Workers:             h.workerStore,
		ParentAgentIDs:      parentIDs,
		SuccessTrackerState: strings.TrimSpace(os.Getenv(v2jido.EnvJidoOrchestrationSuccessTrackerState)),
	})
	if err != nil {
		return fmt.Errorf("configure go orchestration reconcile: %w", err)
	}
	if err := reconciler.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile go orchestration runtime: %w", err)
	}
	h.log.Info().
		Str("workspace_id", strings.TrimSpace(workspaceID)).
		Str("request_id", strings.TrimSpace(requestID)).
		Msg("orchestration refresh go reconcile completed")
	return nil
}

func (h *persistentOrchestrationRuntimeHost) Signal(ctx context.Context, req coreworker.SignalRequest) (coreworker.SignalResponse, error) {
	if h == nil || h.signaler == nil {
		return coreworker.SignalResponse{}, fmt.Errorf("orchestration runtime host is not configured")
	}
	return h.signaler.SignalWorker(ctx, req)
}

func (h *persistentOrchestrationRuntimeHost) addParentAgentID(parentAgentID string) {
	trimmed := strings.TrimSpace(parentAgentID)
	if trimmed == "" {
		return
	}
	h.mu.Lock()
	h.parentAgentIDs[trimmed] = struct{}{}
	h.mu.Unlock()
}

func (h *persistentOrchestrationRuntimeHost) snapshotParentAgentIDs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.parentAgentIDs) == 0 {
		return nil
	}
	out := make([]string, 0, len(h.parentAgentIDs))
	for parentID := range h.parentAgentIDs {
		out = append(out, parentID)
	}
	sort.Strings(out)
	return out
}

func (h *persistentOrchestrationRuntimeHost) reattachActiveWorkers(ctx context.Context) error {
	if h == nil || h.workerStore == nil || h.workerState == nil {
		return nil
	}
	records, err := h.workerStore.Active(ctx, coreworker.BackendSubprocess)
	if err != nil {
		return fmt.Errorf("list active subprocess workers: %w", err)
	}
	for _, record := range records {
		if err := h.workerState.Publish(ctx, coreworker.LifecycleEvent{
			EventKind:      coreworker.EventWorkerStateChanged,
			ObservedAt:     chooseObservedAt(record.UpdatedAt, timeNowUTC()),
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
			RawState:       append(json.RawMessage(nil), record.RawState...),
		}); err != nil {
			return err
		}

		pid, ok := parseRuntimePID(record.PID)
		if !ok {
			if err := h.publishMissingProcess(ctx, record, "subprocess missing pid during runtime host reattach"); err != nil {
				return err
			}
			continue
		}
		if !runtimeProcessAlive(pid) {
			if err := h.publishMissingProcess(ctx, record, "subprocess missing during runtime host reattach"); err != nil {
				return err
			}
			continue
		}
		process, findErr := os.FindProcess(pid)
		if findErr == nil {
			v2goruntime.RegisterReattachedProcess(v2goruntime.ReattachProcessConfig{
				Record:    record,
				Process:   process,
				Publisher: h.workerState,
				Now:       timeNowUTC,
			})
		}
		if err := h.workerState.Publish(ctx, coreworker.LifecycleEvent{
			EventKind:      coreworker.EventWorkerHeartbeat,
			ObservedAt:     timeNowUTC(),
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
			Metadata:       cloneMap(record.Metadata),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (h *persistentOrchestrationRuntimeHost) publishMissingProcess(ctx context.Context, record coreworker.Record, reason string) error {
	return h.workerState.Publish(ctx, coreworker.LifecycleEvent{
		EventKind:      coreworker.EventWorkerFailed,
		ObservedAt:     timeNowUTC(),
		WorkerID:       strings.TrimSpace(record.WorkerID),
		BackendKind:    record.BackendKind,
		AgentID:        strings.TrimSpace(record.AgentID),
		RunID:          strings.TrimSpace(record.RunID),
		SessionID:      strings.TrimSpace(record.SessionID),
		ParentAgentID:  strings.TrimSpace(record.ParentAgentID),
		ParentWorkerID: strings.TrimSpace(record.ParentWorkerID),
		WorkspaceID:    strings.TrimSpace(record.WorkspaceID),
		Status:         coreworker.StatusFailed,
		Role:           strings.TrimSpace(record.Role),
		Tag:            strings.TrimSpace(record.Tag),
		PID:            strings.TrimSpace(record.PID),
		StopReason:     reason,
		ExitCode:       record.ExitCode,
		Metadata:       cloneMap(record.Metadata),
		RawState:       append(json.RawMessage(nil), record.RawState...),
	})
}

func parseRuntimePID(raw string) (int, bool) {
	pid, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func runtimeProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
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

func timeNowUTC() time.Time {
	return time.Now().UTC()
}

func chooseObservedAt(primary, fallback time.Time) time.Time {
	if !primary.IsZero() {
		return primary.UTC()
	}
	return fallback.UTC()
}
