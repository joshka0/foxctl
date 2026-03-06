package jido

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvJidoSocketPath                         = "AGENTCTL_JIDO_SOCKET"
	EnvJidoRPCPath                            = "AGENTCTL_JIDO_RPC_PATH"
	EnvJidoRPCTimeoutMS                       = "AGENTCTL_JIDO_RPC_TIMEOUT_MS"
	EnvJidoSignalSource                       = "AGENTCTL_JIDO_SIGNAL_SOURCE"
	EnvJidoOrchestrationParentAgentIDs        = "AGENTCTL_JIDO_ORCHESTRATION_PARENT_AGENT_IDS"
	EnvJidoOrchestrationDispatchParentAgentID = "AGENTCTL_JIDO_ORCHESTRATION_DISPATCH_PARENT_AGENT_ID"
	EnvJidoOrchestrationSuccessTrackerState   = "AGENTCTL_JIDO_ORCHESTRATION_SUCCESS_TRACKER_STATE"
	EnvJidoRetryPolicy                        = "AGENTCTL_JIDO_RETRY_POLICY"
)

// OrchestrationRuntimeConfig wires a reusable Jido orchestration bridge.
type OrchestrationRuntimeConfig struct {
	Client              Client
	Events              EventAppender
	Projections         ProjectionApplier
	Reader              OrchestrationCardReader
	SignalSource        string
	SocketPath          string
	RPCPath             string
	Timeout             time.Duration
	ParentAgentIDs      []string
	SuccessTrackerState string
	RetryPolicy         RetryPolicy
	Now                 func() time.Time
	NewID               func() string
}

// OrchestrationRuntime bundles the runtime-backed reconciler and child spawner.
type OrchestrationRuntime struct {
	Client       Client
	Reconciler   *OrchestrationReconciler
	ChildSpawner *ChildSpawner
}

// NewEnvJSONRPCClient builds a JSON-RPC client from the current Jido env settings.
func NewEnvJSONRPCClient() (*JSONRPCClient, error) {
	return NewJSONRPCClient(JSONRPCClientConfig{
		SocketPath: resolveJidoSocketPath(""),
		RPCPath:    resolveJidoRPCPath(""),
		Timeout:    resolveJidoTimeout(0),
	})
}

// OrchestrationRuntimeEnabled reports whether the env-backed runtime should be used.
func OrchestrationRuntimeEnabled(cfg OrchestrationRuntimeConfig) bool {
	return len(resolveOrchestrationParentAgentIDs(cfg.ParentAgentIDs)) > 0
}

// NewOrchestrationRuntime builds a reusable Jido orchestration runtime bridge.
func NewOrchestrationRuntime(cfg OrchestrationRuntimeConfig) (*OrchestrationRuntime, error) {
	parentIDs := resolveOrchestrationParentAgentIDs(cfg.ParentAgentIDs)
	if len(parentIDs) == 0 {
		return nil, fmt.Errorf("jido orchestration runtime requires parent_agent_ids")
	}
	if cfg.Events == nil {
		return nil, fmt.Errorf("jido orchestration runtime requires event appender")
	}
	if cfg.Reader == nil {
		return nil, fmt.Errorf("jido orchestration runtime requires orchestration card reader")
	}

	client := cfg.Client
	if client == nil {
		var err error
		client, err = NewJSONRPCClient(JSONRPCClientConfig{
			SocketPath: resolveJidoSocketPath(cfg.SocketPath),
			RPCPath:    resolveJidoRPCPath(cfg.RPCPath),
			Timeout:    resolveJidoTimeout(cfg.Timeout),
		})
		if err != nil {
			return nil, fmt.Errorf("configure jido json-rpc client: %w", err)
		}
	}

	signalSource := resolveSignalSource(cfg.SignalSource)
	reconciler, err := NewOrchestrationReconciler(OrchestrationReconcilerConfig{
		Events:              cfg.Events,
		Projections:         cfg.Projections,
		Reader:              cfg.Reader,
		Client:              client,
		ParentAgentIDs:      parentIDs,
		SuccessTrackerState: resolveSuccessTrackerStateConfig(cfg.SuccessTrackerState),
		RetryPolicy:         resolveRetryPolicyConfig(cfg.RetryPolicy),
		Now:                 cfg.Now,
		NewID:               cfg.NewID,
	})
	if err != nil {
		return nil, fmt.Errorf("configure jido orchestration reconciler: %w", err)
	}

	childSpawner, err := NewChildSpawner(ChildSpawnerConfig{
		Client:        client,
		SignalSource:  signalSource,
		Timeout:       resolveJidoTimeout(cfg.Timeout),
		Now:           cfg.Now,
		OnSpawnResult: reconciler.SpawnResultCallback(),
	})
	if err != nil {
		return nil, fmt.Errorf("configure jido child spawner: %w", err)
	}

	return &OrchestrationRuntime{
		Client:       client,
		Reconciler:   reconciler,
		ChildSpawner: childSpawner,
	}, nil
}

func resolveOrchestrationParentAgentIDs(values []string) []string {
	if len(values) > 0 {
		return normalizeParentAgentIDs(values)
	}
	raw := strings.TrimSpace(os.Getenv(EnvJidoOrchestrationParentAgentIDs))
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	return normalizeParentAgentIDs(parts)
}

func resolveSignalSource(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(os.Getenv(EnvJidoSignalSource)); trimmed != "" {
		return trimmed
	}
	return DefaultSignalSource
}

func resolveJidoSocketPath(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(os.Getenv(EnvJidoSocketPath))
}

func resolveJidoRPCPath(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(os.Getenv(EnvJidoRPCPath))
}

func resolveJidoTimeout(value time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	raw := strings.TrimSpace(os.Getenv(EnvJidoRPCTimeoutMS))
	if raw == "" {
		return defaultTimeout
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || ms <= 0 {
		return defaultTimeout
	}
	return time.Duration(ms) * time.Millisecond
}

func resolveSuccessTrackerStateConfig(value string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(os.Getenv(EnvJidoOrchestrationSuccessTrackerState))
}
