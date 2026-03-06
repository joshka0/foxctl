package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	v2jido "github.com/jkatigb/agentctl/internal/v2/adapters/jido"
	libsqlevents "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/events"
	libsqlprojections "github.com/jkatigb/agentctl/internal/v2/adapters/libsql/projections"
	v2events "github.com/jkatigb/agentctl/internal/v2/core/events"
	v2services "github.com/jkatigb/agentctl/internal/v2/services"
)

const (
	askDispatchModeMailbox = "mailbox"
	askDispatchModeJido    = "jido"
)

const (
	envAskDispatcherMode = "AGENTCTL_V2_ASK_DISPATCHER"
	envJidoSocketPath    = "AGENTCTL_JIDO_SOCKET"
	envJidoRPCPath       = "AGENTCTL_JIDO_RPC_PATH"
	envJidoRPCTimeoutMS  = "AGENTCTL_JIDO_RPC_TIMEOUT_MS"
	envJidoSignalSource  = "AGENTCTL_JIDO_SIGNAL_SOURCE"
)

func resolvedAskDispatcherMode(override string) string {
	mode := strings.ToLower(strings.TrimSpace(override))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(os.Getenv(envAskDispatcherMode)))
	}
	if mode == "" {
		mode = askDispatchModeMailbox
	}
	return mode
}

func newJidoAskRuntime(
	ctx context.Context,
	storageRoot string,
	nowFn func() time.Time,
	newID func() string,
) (v2services.AskDispatcher, v2events.Appender, v2services.AskProjectionApplier, func(), error) {
	eventStore, err := libsqlevents.Open(ctx, storageRoot)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open v2 events store for jido ask: %w", err)
	}
	projectionStore, closeProjections, err := libsqlprojections.Open(ctx, storageRoot)
	if err != nil {
		_ = eventStore.Close()
		return nil, nil, nil, nil, fmt.Errorf("open v2 projection store for jido ask: %w", err)
	}
	reconciler, err := v2jido.NewReconciler(v2jido.ReconcilerConfig{
		Events:      eventStore,
		Projections: projectionStore,
		Now:         nowFn,
		NewID:       newID,
	})
	if err != nil {
		_ = closeProjections()
		_ = eventStore.Close()
		return nil, nil, nil, nil, fmt.Errorf("configure jido reconciler: %w", err)
	}

	cleanup := func() {
		if closeProjections != nil {
			_ = closeProjections()
		}
		_ = eventStore.Close()
	}

	socket := strings.TrimSpace(os.Getenv(envJidoSocketPath))
	rpcPath := strings.TrimSpace(os.Getenv(envJidoRPCPath))
	timeout := parseDurationMillisEnv(envJidoRPCTimeoutMS, 10*time.Second)

	client, err := v2jido.NewJSONRPCClient(v2jido.JSONRPCClientConfig{
		SocketPath: socket,
		RPCPath:    rpcPath,
		Timeout:    timeout,
	})
	if err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("configure jido json-rpc client: %w", err)
	}

	signalSource := strings.TrimSpace(os.Getenv(envJidoSignalSource))
	adapter, err := v2jido.NewRuntimeAdapter(v2jido.RuntimeAdapterConfig{
		Client:       client,
		SignalSource: signalSource,
		OnSignalAck: func(ctx context.Context, req v2jido.SignalRequest, resp v2jido.SignalResponse) error {
			cb := v2jido.SignalAckToCallback(req, resp)
			_, reconcileErr := reconciler.ReconcileSignalCallback(ctx, cb)
			return reconcileErr
		},
	})
	if err != nil {
		cleanup()
		return nil, nil, nil, nil, fmt.Errorf("configure jido runtime adapter: %w", err)
	}

	return adapter, eventStore, projectionStore, cleanup, nil
}

func parseDurationMillisEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || ms <= 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}
