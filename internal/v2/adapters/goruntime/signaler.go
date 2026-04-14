package goruntime

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
)

type SignalerConfig struct {
	Publisher EventPublisher
	Now       func() time.Time
}

type Signaler struct {
	publisher EventPublisher
	now       func() time.Time
}

func NewSignaler(cfg SignalerConfig) *Signaler {
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Signaler{
		publisher: cfg.Publisher,
		now:       now,
	}
}

func (s *Signaler) SignalWorker(ctx context.Context, req coreworker.SignalRequest) (coreworker.SignalResponse, error) {
	entry, err := globalProcessRegistry.find(req.WorkerID, req.AgentID)
	if err != nil {
		if err == os.ErrNotExist {
			return coreworker.SignalResponse{}, fmt.Errorf("runtime worker not found")
		}
		return coreworker.SignalResponse{}, err
	}
	signalName := normalizeSignalName(req.Signal)
	sig, err := runtimeSignal(signalName)
	if err != nil {
		return coreworker.SignalResponse{}, err
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = "runtime signal requested"
	}

	entry.publisher = choosePublisher(s.publisher, entry.publisher)
	entry.now = chooseNow(s.now, entry.now)
	entry.markCancelRequested(signalName, reason)
	if err := entry.emitSignalEvents(ctx, signalName, reason); err != nil {
		return coreworker.SignalResponse{}, err
	}
	if err := signalProcessEntry(entry, sig); err != nil {
		return coreworker.SignalResponse{}, err
	}
	return coreworker.SignalResponse{
		WorkerID:  entry.workerID,
		AgentID:   entry.agentID,
		Status:    coreworker.StatusStopping,
		MessageID: strings.TrimSpace(req.RequestID),
	}, nil
}

func normalizeSignalName(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "cancel", "terminate", "term", "sigterm":
		return "terminate"
	case "interrupt", "sigint", "int":
		return "interrupt"
	case "kill", "sigkill":
		return "kill"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func runtimeSignal(name string) (os.Signal, error) {
	switch name {
	case "terminate":
		return syscall.SIGTERM, nil
	case "interrupt":
		return syscall.SIGINT, nil
	case "kill":
		return syscall.SIGKILL, nil
	default:
		return nil, fmt.Errorf("unsupported runtime signal %q", name)
	}
}

func choosePublisher(primary, fallback EventPublisher) EventPublisher {
	if primary != nil {
		return primary
	}
	return fallback
}

func signalProcessEntry(entry *processEntry, sig os.Signal) error {
	if entry == nil || entry.process == nil {
		return fmt.Errorf("runtime worker process is not available")
	}
	if signal, ok := sig.(syscall.Signal); ok && entry.processGroupID > 0 {
		if err := syscall.Kill(-entry.processGroupID, signal); err == nil {
			return nil
		} else if err != syscall.ESRCH {
			return err
		}
	}
	return entry.process.Signal(sig)
}

func chooseNow(primary, fallback func() time.Time) func() time.Time {
	if primary != nil {
		return primary
	}
	if fallback != nil {
		return fallback
	}
	return func() time.Time { return time.Now().UTC() }
}
