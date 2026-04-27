package goruntime

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	agentdomain "github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/storage/agents"
	"github.com/joshka0/foxctl/internal/v2/core/spawn"
	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
)

// EventPublisher accepts normalized worker lifecycle events.
type EventPublisher interface {
	Publish(ctx context.Context, evt coreworker.LifecycleEvent) error
}

// CommandSpec describes the child process to launch.
type CommandSpec struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

// CommandBuilder resolves one subprocess launch spec for a spawn request.
type CommandBuilder func(req spawn.Request) (CommandSpec, error)

// ChildSpawnerConfig configures the Go-native subprocess spawner.
type ChildSpawnerConfig struct {
	Publisher         EventPublisher
	BuildCommand      CommandBuilder
	Now               func() time.Time
	HeartbeatInterval time.Duration
}

// ChildSpawner launches one subprocess worker and reports its lifecycle.
type ChildSpawner struct {
	publisher         EventPublisher
	buildCommand      CommandBuilder
	now               func() time.Time
	heartbeatInterval time.Duration
}

// NewChildSpawner builds a Go-native subprocess child spawner.
func NewChildSpawner(cfg ChildSpawnerConfig) (*ChildSpawner, error) {
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("goruntime child spawner requires publisher")
	}
	if cfg.BuildCommand == nil {
		return nil, fmt.Errorf("goruntime child spawner requires command builder")
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = 30 * time.Second
	}
	return &ChildSpawner{
		publisher:         cfg.Publisher,
		buildCommand:      cfg.BuildCommand,
		now:               cfg.Now,
		heartbeatInterval: cfg.HeartbeatInterval,
	}, nil
}

// SpawnChild launches one subprocess and emits normalized worker lifecycle events.
func (s *ChildSpawner) SpawnChild(ctx context.Context, req spawn.Request) (spawn.Response, error) {
	if s == nil || s.publisher == nil || s.buildCommand == nil {
		return spawn.Response{}, fmt.Errorf("goruntime child spawner is not configured")
	}
	parentAgentID := strings.TrimSpace(req.ParentAgentID)
	if parentAgentID == "" {
		return spawn.Response{}, fmt.Errorf("parent_agent_id is required")
	}

	workerID := workerIDForRequest(req)
	now := s.now().UTC()
	if err := s.publish(ctx, coreworker.LifecycleEvent{
		EventKind:     coreworker.EventWorkerSpawnRequested,
		ObservedAt:    now,
		WorkerID:      workerID,
		BackendKind:   coreworker.BackendSubprocess,
		AgentID:       strings.TrimSpace(req.AgentID),
		RunID:         strings.TrimSpace(req.RunID),
		ParentAgentID: parentAgentID,
		WorkspaceID:   stringMeta(req.Metadata, "workspace_id"),
		RequestID:     strings.TrimSpace(req.RequestID),
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		CausationID:   strings.TrimSpace(req.CausationID),
		Status:        coreworker.StatusPending,
		Role:          strings.TrimSpace(req.Role),
		Metadata:      cloneMeta(req.Metadata),
	}); err != nil {
		return spawn.Response{}, err
	}

	spec, err := s.buildCommand(req)
	if err != nil {
		_ = s.publish(context.Background(), lifecycleFailure(req, workerID, s.now().UTC(), fmt.Errorf("build command: %w", err), 0))
		return spawn.Response{}, err
	}
	if strings.TrimSpace(spec.Path) == "" {
		err := fmt.Errorf("command path is required")
		_ = s.publish(context.Background(), lifecycleFailure(req, workerID, s.now().UTC(), err, 0))
		return spawn.Response{}, err
	}

	cmd := exec.Command(spec.Path, spec.Args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if strings.TrimSpace(spec.Dir) != "" {
		cmd.Dir = spec.Dir
	}
	if len(spec.Env) > 0 {
		cmd.Env = append([]string(nil), spec.Env...)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = s.publish(context.Background(), lifecycleFailure(req, workerID, s.now().UTC(), fmt.Errorf("stdout pipe: %w", err), 0))
		return spawn.Response{}, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = s.publish(context.Background(), lifecycleFailure(req, workerID, s.now().UTC(), fmt.Errorf("stderr pipe: %w", err), 0))
		return spawn.Response{}, err
	}
	if err := cmd.Start(); err != nil {
		_ = s.publish(context.Background(), lifecycleFailure(req, workerID, s.now().UTC(), err, 0))
		return spawn.Response{}, err
	}

	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}
	processGroupID := lookupProcessGroupID(pid)
	startedAt := s.now().UTC()
	entry := &processEntry{
		workerID:       workerID,
		agentID:        strings.TrimSpace(req.AgentID),
		process:        cmd.Process,
		processGroupID: processGroupID,
		done:           make(chan struct{}),
		publisher:      s.publisher,
		now:            s.now,
		baseEvent: coreworker.LifecycleEvent{
			WorkerID:      workerID,
			BackendKind:   coreworker.BackendSubprocess,
			AgentID:       strings.TrimSpace(req.AgentID),
			RunID:         strings.TrimSpace(req.RunID),
			ParentAgentID: parentAgentID,
			WorkspaceID:   stringMeta(req.Metadata, "workspace_id"),
			RequestID:     strings.TrimSpace(req.RequestID),
			CorrelationID: strings.TrimSpace(req.CorrelationID),
			CausationID:   strings.TrimSpace(req.CausationID),
			Role:          strings.TrimSpace(req.Role),
			PID:           intToString(pid),
			Metadata: mergeSpawnMetadata(cloneMeta(req.Metadata), map[string]any{
				"process_group_id": processGroupID,
			}),
		},
	}
	globalProcessRegistry.register(entry)
	var logWG sync.WaitGroup
	logWG.Add(2)
	go s.streamLogs(req, workerID, "stdout", stdoutPipe, &logWG)
	go s.streamLogs(req, workerID, "stderr", stderrPipe, &logWG)

	if err := s.publish(ctx, coreworker.LifecycleEvent{
		EventKind:     coreworker.EventWorkerSpawned,
		ObservedAt:    startedAt,
		WorkerID:      workerID,
		BackendKind:   coreworker.BackendSubprocess,
		AgentID:       strings.TrimSpace(req.AgentID),
		RunID:         strings.TrimSpace(req.RunID),
		ParentAgentID: parentAgentID,
		WorkspaceID:   stringMeta(req.Metadata, "workspace_id"),
		RequestID:     strings.TrimSpace(req.RequestID),
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		CausationID:   strings.TrimSpace(req.CausationID),
		Status:        coreworker.StatusStarting,
		Role:          strings.TrimSpace(req.Role),
		PID:           intToString(pid),
		Metadata: mergeSpawnMetadata(cloneMeta(req.Metadata), map[string]any{
			"process_group_id": processGroupID,
		}),
	}); err != nil {
		return spawn.Response{}, err
	}
	if err := s.publish(ctx, coreworker.LifecycleEvent{
		EventKind:     coreworker.EventWorkerStarted,
		ObservedAt:    startedAt,
		WorkerID:      workerID,
		BackendKind:   coreworker.BackendSubprocess,
		AgentID:       strings.TrimSpace(req.AgentID),
		RunID:         strings.TrimSpace(req.RunID),
		ParentAgentID: parentAgentID,
		WorkspaceID:   stringMeta(req.Metadata, "workspace_id"),
		RequestID:     strings.TrimSpace(req.RequestID),
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		CausationID:   strings.TrimSpace(req.CausationID),
		Status:        coreworker.StatusRunning,
		Role:          strings.TrimSpace(req.Role),
		PID:           intToString(pid),
		Metadata: mergeSpawnMetadata(cloneMeta(req.Metadata), map[string]any{
			"process_group_id": processGroupID,
		}),
	}); err != nil {
		return spawn.Response{}, err
	}

	if s.heartbeatInterval > 0 {
		go s.publishHeartbeats(req, entry)
	}

	go s.waitAndPublish(req, workerID, cmd, entry, &logWG)

	return spawn.Response{
		RunID:     strings.TrimSpace(req.RunID),
		AgentID:   strings.TrimSpace(req.AgentID),
		ActorID:   strings.TrimSpace(req.ActorID),
		RequestID: strings.TrimSpace(req.RequestID),
		Status:    "spawned",
		Summary:   "spawned subprocess worker",
		CreatedAt: startedAt,
	}, nil
}

func (s *ChildSpawner) waitAndPublish(req spawn.Request, workerID string, cmd *exec.Cmd, entry *processEntry, logWG *sync.WaitGroup) {
	defer globalProcessRegistry.unregister(workerID, strings.TrimSpace(req.AgentID))
	if entry != nil {
		defer entry.closeDone()
	}
	err := cmd.Wait()
	if logWG != nil {
		logWG.Wait()
	}
	now := s.now().UTC()
	if err == nil {
		cancelled, signalName, reason := false, "", ""
		if entry != nil {
			cancelled, signalName, reason = entry.cancelState()
		}
		if cancelled {
			_ = s.publish(context.Background(), coreworker.LifecycleEvent{
				EventKind:     coreworker.EventWorkerCancelled,
				ObservedAt:    now,
				WorkerID:      workerID,
				BackendKind:   coreworker.BackendSubprocess,
				AgentID:       strings.TrimSpace(req.AgentID),
				RunID:         strings.TrimSpace(req.RunID),
				ParentAgentID: strings.TrimSpace(req.ParentAgentID),
				WorkspaceID:   stringMeta(req.Metadata, "workspace_id"),
				RequestID:     strings.TrimSpace(req.RequestID),
				CorrelationID: strings.TrimSpace(req.CorrelationID),
				CausationID:   strings.TrimSpace(req.CausationID),
				Status:        coreworker.StatusCancelled,
				Role:          strings.TrimSpace(req.Role),
				PID:           pidString(cmd),
				StopReason:    chooseNonEmpty(reason, signalName, "cancelled"),
				Metadata: mergeSpawnMetadata(cloneMeta(req.Metadata), map[string]any{
					"signal": signalName,
					"reason": reason,
				}),
			})
			return
		}
		_ = s.publish(context.Background(), coreworker.LifecycleEvent{
			EventKind:     coreworker.EventWorkerCompleted,
			ObservedAt:    now,
			WorkerID:      workerID,
			BackendKind:   coreworker.BackendSubprocess,
			AgentID:       strings.TrimSpace(req.AgentID),
			RunID:         strings.TrimSpace(req.RunID),
			ParentAgentID: strings.TrimSpace(req.ParentAgentID),
			WorkspaceID:   stringMeta(req.Metadata, "workspace_id"),
			RequestID:     strings.TrimSpace(req.RequestID),
			CorrelationID: strings.TrimSpace(req.CorrelationID),
			CausationID:   strings.TrimSpace(req.CausationID),
			Status:        coreworker.StatusCompleted,
			Role:          strings.TrimSpace(req.Role),
			PID:           pidString(cmd),
			StopReason:    "exited",
			Metadata:      cloneMeta(req.Metadata),
		})
		return
	}
	exitCode := extractExitCode(err)
	if entry != nil {
		if cancelled, signalName, reason := entry.cancelState(); cancelled {
			_ = s.publish(context.Background(), coreworker.LifecycleEvent{
				EventKind:     coreworker.EventWorkerCancelled,
				ObservedAt:    now,
				WorkerID:      workerID,
				BackendKind:   coreworker.BackendSubprocess,
				AgentID:       strings.TrimSpace(req.AgentID),
				RunID:         strings.TrimSpace(req.RunID),
				ParentAgentID: strings.TrimSpace(req.ParentAgentID),
				WorkspaceID:   stringMeta(req.Metadata, "workspace_id"),
				RequestID:     strings.TrimSpace(req.RequestID),
				CorrelationID: strings.TrimSpace(req.CorrelationID),
				CausationID:   strings.TrimSpace(req.CausationID),
				Status:        coreworker.StatusCancelled,
				Role:          strings.TrimSpace(req.Role),
				PID:           pidString(cmd),
				ExitCode:      exitCode,
				StopReason:    chooseNonEmpty(reason, signalName, strings.TrimSpace(err.Error())),
				Metadata: mergeSpawnMetadata(cloneMeta(req.Metadata), map[string]any{
					"signal": signalName,
					"reason": reason,
				}),
			})
			return
		}
	}
	_ = s.publish(context.Background(), lifecycleFailure(req, workerID, now, err, exitCode))
}

func (s *ChildSpawner) publish(ctx context.Context, evt coreworker.LifecycleEvent) error {
	return s.publisher.Publish(ctx, evt)
}

func (s *ChildSpawner) publishHeartbeats(req spawn.Request, entry *processEntry) {
	if s == nil || entry == nil || entry.done == nil || s.heartbeatInterval <= 0 {
		return
	}
	ticker := time.NewTicker(s.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-entry.done:
			return
		case <-ticker.C:
			// Guard against publish racing with done closure:
			// if done is already closed, skip this heartbeat.
			select {
			case <-entry.done:
				return
			default:
			}
			_ = s.publish(context.Background(), coreworker.LifecycleEvent{
				EventKind:     coreworker.EventWorkerHeartbeat,
				ObservedAt:    s.now().UTC(),
				WorkerID:      entry.workerID,
				BackendKind:   coreworker.BackendSubprocess,
				AgentID:       strings.TrimSpace(req.AgentID),
				RunID:         strings.TrimSpace(req.RunID),
				ParentAgentID: strings.TrimSpace(req.ParentAgentID),
				WorkspaceID:   stringMeta(req.Metadata, "workspace_id"),
				RequestID:     strings.TrimSpace(req.RequestID),
				CorrelationID: strings.TrimSpace(req.CorrelationID),
				CausationID:   strings.TrimSpace(req.CausationID),
				Status:        coreworker.StatusRunning,
				Role:          strings.TrimSpace(req.Role),
				PID:           entry.baseEvent.PID,
				Metadata:      cloneMeta(req.Metadata),
			})
		}
	}
}

func (s *ChildSpawner) streamLogs(req spawn.Request, workerID, stream string, reader io.ReadCloser, wg *sync.WaitGroup) {
	if wg != nil {
		defer wg.Done()
	}
	defer func() { _ = reader.Close() }()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		chunk := strings.TrimRight(scanner.Text(), "\r\n")
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		_ = s.publish(context.Background(), coreworker.LifecycleEvent{
			EventKind:     coreworker.EventWorkerLogChunk,
			ObservedAt:    s.now().UTC(),
			WorkerID:      workerID,
			BackendKind:   coreworker.BackendSubprocess,
			AgentID:       strings.TrimSpace(req.AgentID),
			RunID:         strings.TrimSpace(req.RunID),
			ParentAgentID: strings.TrimSpace(req.ParentAgentID),
			WorkspaceID:   stringMeta(req.Metadata, "workspace_id"),
			RequestID:     strings.TrimSpace(req.RequestID),
			CorrelationID: strings.TrimSpace(req.CorrelationID),
			CausationID:   strings.TrimSpace(req.CausationID),
			Status:        coreworker.StatusRunning,
			Role:          strings.TrimSpace(req.Role),
			Metadata: mergeSpawnMetadata(cloneMeta(req.Metadata), map[string]any{
				"stream": stream,
				"chunk":  chunk,
			}),
		})
	}
}

func lifecycleFailure(req spawn.Request, workerID string, observedAt time.Time, err error, exitCode int) coreworker.LifecycleEvent {
	return coreworker.LifecycleEvent{
		EventKind:     coreworker.EventWorkerFailed,
		ObservedAt:    observedAt,
		WorkerID:      workerID,
		BackendKind:   coreworker.BackendSubprocess,
		AgentID:       strings.TrimSpace(req.AgentID),
		RunID:         strings.TrimSpace(req.RunID),
		ParentAgentID: strings.TrimSpace(req.ParentAgentID),
		WorkspaceID:   stringMeta(req.Metadata, "workspace_id"),
		RequestID:     strings.TrimSpace(req.RequestID),
		CorrelationID: strings.TrimSpace(req.CorrelationID),
		CausationID:   strings.TrimSpace(req.CausationID),
		Status:        coreworker.StatusFailed,
		Role:          strings.TrimSpace(req.Role),
		ExitCode:      exitCode,
		StopReason:    strings.TrimSpace(err.Error()),
		Metadata:      cloneMeta(req.Metadata),
	}
}

func workerIDForRequest(req spawn.Request) string {
	if agentID := strings.TrimSpace(req.AgentID); agentID != "" {
		return "subprocess:" + agentID
	}
	if runID := strings.TrimSpace(req.RunID); runID != "" {
		return "subprocess:" + runID
	}
	if requestID := strings.TrimSpace(req.RequestID); requestID != "" {
		return "subprocess:" + requestID
	}
	return "subprocess:unknown"
}

func stringMeta(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	raw, ok := meta[key]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func cloneMeta(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func extractExitCode(err error) int {
	var exitErr *exec.ExitError
	if !errorAs(err, &exitErr) {
		return 0
	}
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
		return status.ExitStatus()
	}
	return exitErr.ExitCode()
}

func errorAs(err error, target any) bool {
	switch t := target.(type) {
	case **exec.ExitError:
		if exitErr, ok := err.(*exec.ExitError); ok {
			*t = exitErr
			return true
		}
	}
	return false
}

func pidString(cmd *exec.Cmd) string {
	if cmd == nil || cmd.Process == nil || cmd.Process.Pid <= 0 {
		return ""
	}
	return intToString(cmd.Process.Pid)
}

func lookupProcessGroupID(pid int) int {
	if pid <= 0 {
		return 0
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid <= 0 {
		return pid
	}
	return pgid
}

func intToString(v int) string {
	if v <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", v)
}

// AgentCommandBuilder resolves the subprocess command for a provisioned agent record.
type AgentCommandBuilder func(record agentdomain.Agent, req spawn.Request) (CommandSpec, error)

// ManagedAgentSpawnerConfig configures a subprocess spawner that provisions a real agent record first.
type ManagedAgentSpawnerConfig struct {
	StorageRoot       string
	WorkspaceRoot     string
	BinaryPath        string
	Publisher         EventPublisher
	BuildCommand      AgentCommandBuilder
	Now               func() time.Time
	HeartbeatInterval time.Duration
}

// ManagedAgentSpawner provisions a classic agent record, then runs it in a subprocess.
type ManagedAgentSpawner struct {
	storageRoot   string
	workspaceRoot string
	now           func() time.Time
	childSpawner  *ChildSpawner
}

// NewManagedAgentSpawner builds a subprocess spawner that launches `foxctl agent run <id>`.
func NewManagedAgentSpawner(cfg ManagedAgentSpawnerConfig) (*ManagedAgentSpawner, error) {
	if strings.TrimSpace(cfg.StorageRoot) == "" {
		return nil, fmt.Errorf("goruntime managed spawner requires storage root")
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("goruntime managed spawner requires publisher")
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	workspaceRoot := strings.TrimSpace(cfg.WorkspaceRoot)
	if workspaceRoot == "" {
		if cwd, err := os.Getwd(); err == nil {
			workspaceRoot = cwd
		}
	}
	binaryPath := strings.TrimSpace(cfg.BinaryPath)
	if binaryPath == "" {
		if exe, err := os.Executable(); err == nil && strings.TrimSpace(exe) != "" {
			binaryPath = exe
		} else {
			binaryPath = "foxctl"
		}
	}
	builder := cfg.BuildCommand
	if builder == nil {
		builder = defaultAgentRunCommandBuilder(binaryPath, workspaceRoot)
	}

	spawner, err := NewChildSpawner(ChildSpawnerConfig{
		Publisher:         cfg.Publisher,
		Now:               cfg.Now,
		HeartbeatInterval: cfg.HeartbeatInterval,
		BuildCommand: func(req spawn.Request) (CommandSpec, error) {
			record, err := ensureAgentRecord(context.Background(), cfg.StorageRoot, workspaceRoot, cfg.Now, req)
			if err != nil {
				return CommandSpec{}, err
			}
			return builder(record, req)
		},
	})
	if err != nil {
		return nil, err
	}

	return &ManagedAgentSpawner{
		storageRoot:   cfg.StorageRoot,
		workspaceRoot: workspaceRoot,
		now:           cfg.Now,
		childSpawner:  spawner,
	}, nil
}

// SpawnChild provisions a real agent record and launches it in a subprocess.
func (s *ManagedAgentSpawner) SpawnChild(ctx context.Context, req spawn.Request) (spawn.Response, error) {
	if s == nil || s.childSpawner == nil {
		return spawn.Response{}, fmt.Errorf("goruntime managed spawner is not configured")
	}
	record, err := ensureAgentRecord(ctx, s.storageRoot, s.workspaceRoot, s.now, req)
	if err != nil {
		return spawn.Response{}, err
	}
	resp, err := s.childSpawner.SpawnChild(ctx, withProvisionedAgent(req, record))
	if err != nil {
		_ = updateAgentState(ctx, s.storageRoot, record.ID, agentdomain.StateError)
		return spawn.Response{}, err
	}
	return resp, nil
}

func withProvisionedAgent(req spawn.Request, record agentdomain.Agent) spawn.Request {
	req.AgentID = record.ID
	req.Metadata = mergeSpawnMetadata(req.Metadata, map[string]any{
		"workspace_root": record.WorkspaceRoot,
		"workspace_id":   chooseNonEmpty(stringMeta(req.Metadata, "workspace_id"), record.Namespace),
	})
	return req
}

func ensureAgentRecord(ctx context.Context, storageRoot, workspaceRoot string, nowFn func() time.Time, req spawn.Request) (agentdomain.Agent, error) {
	store, err := agents.Open(ctx, storageRoot)
	if err != nil {
		return agentdomain.Agent{}, fmt.Errorf("open agent store: %w", err)
	}
	defer func() { _ = store.Close() }()

	agentID := strings.TrimSpace(req.AgentID)
	if agentID == "" {
		return agentdomain.Agent{}, fmt.Errorf("agent_id is required")
	}
	existing, err := store.Get(ctx, agentID)
	switch {
	case err == nil:
		return existing, nil
	case !errors.Is(err, agents.ErrNotFound):
		return agentdomain.Agent{}, fmt.Errorf("load existing agent: %w", err)
	}

	now := time.Now().UTC()
	if nowFn != nil {
		now = nowFn().UTC()
	}
	record := agentdomain.Agent{
		ID:              agentID,
		ParentID:        strings.TrimSpace(req.ParentAgentID),
		Namespace:       buildNamespace(strings.TrimSpace(req.ParentAgentID), agentID),
		WorkspaceRoot:   chooseNonEmpty(stringMeta(req.Metadata, "workspace_root"), strings.TrimSpace(workspaceRoot)),
		WorkspaceSource: "local",
		Role:            strings.TrimSpace(req.Role),
		Prompt:          strings.TrimSpace(req.Prompt),
		SkillsAllow:     nil,
		Policy:          agentdomain.Policy{},
		ShareBB:         "scoped",
		State:           agentdomain.StateStarting,
		CreatedAt:       now,
		HeartbeatAt:     now,
		ExecMode:        agentdomain.ExecutionMode(strings.TrimSpace(req.ExecMode)),
		ExecutionLayer:  agentdomain.ExecutionLayerClassic,
		MaxIterations:   req.MaxIterations,
		MaxAutoTurns:    req.MaxAutoTurns,
		ThinkInterval:   req.ThinkInterval,
		MemoryScope:     agentdomain.MemoryScopeAgent,
		MemoryRetention: agentdomain.DefaultMemoryRetentionForScope(agentdomain.MemoryScopeAgent),
	}
	if record.ExecMode == "" {
		record.ExecMode = agentdomain.ModeAutonomous
	}
	if record.MaxIterations <= 0 {
		record.MaxIterations = 10
	}
	if record.MaxAutoTurns <= 0 {
		record.MaxAutoTurns = 1
	}
	if record.ThinkInterval <= 0 {
		record.ThinkInterval = 60
	}

	if err := store.Create(ctx, record); err != nil {
		existing, getErr := store.Get(ctx, agentID)
		if getErr == nil {
			return existing, nil
		}
		return agentdomain.Agent{}, fmt.Errorf("create agent record: %w", err)
	}
	return record, nil
}

func updateAgentState(ctx context.Context, storageRoot, agentID string, state agentdomain.State) error {
	store, err := agents.Open(ctx, storageRoot)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return store.UpdateState(ctx, agentID, state)
}

func defaultAgentRunCommandBuilder(binaryPath, workspaceRoot string) AgentCommandBuilder {
	return func(record agentdomain.Agent, req spawn.Request) (CommandSpec, error) {
		dir := chooseNonEmpty(record.WorkspaceRoot, strings.TrimSpace(workspaceRoot))
		if dir != "" {
			if abs, err := filepath.Abs(dir); err == nil {
				dir = abs
			}
		}
		return CommandSpec{
			Path: binaryPath,
			Args: []string{"agent", "run", record.ID},
			Dir:  dir,
			Env:  filteredFoxctlEnv(os.Environ()),
		}, nil
	}
}

func filteredFoxctlEnv(in []string) []string {
	out := make([]string, 0, len(in))
	for _, kv := range in {
		switch {
		case strings.HasPrefix(kv, "FOXCTL_JIDO_"):
			continue
		case strings.HasPrefix(kv, "FOXCTL_V2_ASK_DISPATCHER="):
			continue
		case strings.HasPrefix(kv, "FOXCTL_JIDO_SOCKET="):
			continue
		case strings.HasPrefix(kv, "FOXCTL_JIDO_RPC_PATH="):
			continue
		case strings.HasPrefix(kv, "FOXCTL_JIDO_RPC_TIMEOUT_MS="):
			continue
		case strings.HasPrefix(kv, "FOXCTL_JIDO_SIGNAL_SOURCE="):
			continue
		default:
			out = append(out, kv)
		}
	}
	return out
}

func buildNamespace(parentNS, agentID string) string {
	if parentNS == "" {
		return agentID
	}
	return parentNS + "/child-" + agentID
}

func chooseNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func mergeSpawnMetadata(current, extra map[string]any) map[string]any {
	if len(current) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]any, len(current)+len(extra))
	for key, value := range current {
		out[key] = value
	}
	for key, value := range extra {
		out[key] = value
	}
	return out
}
