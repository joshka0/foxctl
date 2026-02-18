package ports_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	v2errors "github.com/jkatigb/agentctl/internal/v2/core/errors"
	v2ports "github.com/jkatigb/agentctl/internal/v2/ports"
	portconfig "github.com/jkatigb/agentctl/internal/v2/ports/config"
)

func TestDispatchWithShadow_V1PrimaryRunsV2Shadow(t *testing.T) {
	t.Parallel()

	shadowFlags, err := portconfig.ParseV2ShadowCommands("ask")
	if err != nil {
		t.Fatalf("ParseV2ShadowCommands() error = %v", err)
	}

	shadowDone := make(chan v2ports.ShadowReport, 1)
	var v1Called atomic.Int32
	var v2Called atomic.Int32

	out, decision, dispatchErr := v2ports.DispatchWithShadow(context.Background(), v2ports.DispatchOptions[string]{
		Command:       "ask",
		CorrelationID: "corr-ask-1",
		ShadowFlags:   shadowFlags,
		V1: func(context.Context) (string, error) {
			v1Called.Add(1)
			return "ok", nil
		},
		V2: func(context.Context) (string, error) {
			v2Called.Add(1)
			return "ok", nil
		},
		ShadowObserve: func(report v2ports.ShadowReport) {
			shadowDone <- report
		},
		ShadowTimeout: time.Second,
	})
	if dispatchErr != nil {
		t.Fatalf("DispatchWithShadow() error = %v", dispatchErr)
	}
	if out != "ok" || decision != v2ports.DecisionV1 {
		t.Fatalf("out/decision = %q/%q want ok/v1", out, decision)
	}

	select {
	case report := <-shadowDone:
		if report.PrimaryDecision != v2ports.DecisionV1 || report.ShadowDecision != v2ports.DecisionV2 {
			t.Fatalf("decisions primary/shadow = %q/%q want v1/v2", report.PrimaryDecision, report.ShadowDecision)
		}
		if !report.Match {
			t.Fatalf("expected shadow match=true, got false reason=%q", report.Reason)
		}
		if report.Command != "ask" || report.CorrelationID != "corr-ask-1" {
			t.Fatalf("report command/correlation = %q/%q", report.Command, report.CorrelationID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shadow report")
	}

	if v1Called.Load() != 1 || v2Called.Load() != 1 {
		t.Fatalf("calls v1/v2 = %d/%d want 1/1", v1Called.Load(), v2Called.Load())
	}
}

func TestDispatchWithShadow_DoesNotRunWhenCommandNotEnabled(t *testing.T) {
	t.Parallel()

	shadowFlags, err := portconfig.ParseV2ShadowCommands("spawn")
	if err != nil {
		t.Fatalf("ParseV2ShadowCommands() error = %v", err)
	}

	shadowDone := make(chan v2ports.ShadowReport, 1)
	var v2Called atomic.Int32

	_, decision, dispatchErr := v2ports.DispatchWithShadow(context.Background(), v2ports.DispatchOptions[string]{
		Command:       "ask",
		CorrelationID: "corr-ask-2",
		ShadowFlags:   shadowFlags,
		V1: func(context.Context) (string, error) {
			return "ok", nil
		},
		V2: func(context.Context) (string, error) {
			v2Called.Add(1)
			return "shadow", nil
		},
		ShadowObserve: func(report v2ports.ShadowReport) {
			shadowDone <- report
		},
		ShadowTimeout: time.Second,
	})
	if dispatchErr != nil {
		t.Fatalf("DispatchWithShadow() error = %v", dispatchErr)
	}
	if decision != v2ports.DecisionV1 {
		t.Fatalf("decision=%q want v1", decision)
	}

	select {
	case report := <-shadowDone:
		t.Fatalf("unexpected shadow report: %+v", report)
	case <-time.After(200 * time.Millisecond):
	}
	if v2Called.Load() != 0 {
		t.Fatalf("v2 calls=%d want 0", v2Called.Load())
	}
}

func TestDispatchWithShadow_PrimaryV2SkipsShadow(t *testing.T) {
	t.Parallel()

	flags, err := portconfig.ParseV2Commands("ask")
	if err != nil {
		t.Fatalf("ParseV2Commands() error = %v", err)
	}
	shadowFlags, err := portconfig.ParseV2ShadowCommands("ask")
	if err != nil {
		t.Fatalf("ParseV2ShadowCommands() error = %v", err)
	}

	var v1Called atomic.Int32
	var v2Called atomic.Int32
	shadowDone := make(chan v2ports.ShadowReport, 1)

	out, decision, dispatchErr := v2ports.DispatchWithShadow(context.Background(), v2ports.DispatchOptions[string]{
		Command:       "ask",
		CorrelationID: "corr-ask-3",
		Flags:         flags,
		ShadowFlags:   shadowFlags,
		V1: func(context.Context) (string, error) {
			v1Called.Add(1)
			return "v1", nil
		},
		V2: func(context.Context) (string, error) {
			v2Called.Add(1)
			return "v2", nil
		},
		ShadowObserve: func(report v2ports.ShadowReport) {
			shadowDone <- report
		},
		ShadowTimeout: time.Second,
	})
	if dispatchErr != nil {
		t.Fatalf("DispatchWithShadow() error = %v", dispatchErr)
	}
	if out != "v2" || decision != v2ports.DecisionV2 {
		t.Fatalf("out/decision = %q/%q want v2/v2", out, decision)
	}
	if v1Called.Load() != 0 || v2Called.Load() != 1 {
		t.Fatalf("calls v1/v2 = %d/%d want 0/1", v1Called.Load(), v2Called.Load())
	}
	select {
	case report := <-shadowDone:
		t.Fatalf("unexpected shadow report: %+v", report)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestDispatchWithShadow_ShadowMismatchReported(t *testing.T) {
	t.Parallel()

	shadowFlags, err := portconfig.ParseV2ShadowCommands("ask")
	if err != nil {
		t.Fatalf("ParseV2ShadowCommands() error = %v", err)
	}

	shadowDone := make(chan v2ports.ShadowReport, 1)

	_, _, dispatchErr := v2ports.DispatchWithShadow(context.Background(), v2ports.DispatchOptions[string]{
		Command:       "ask",
		CorrelationID: "corr-ask-4",
		ShadowFlags:   shadowFlags,
		V1: func(context.Context) (string, error) {
			return "ok", nil
		},
		V2: func(context.Context) (string, error) {
			return "", errors.New("shadow failure")
		},
		ShadowObserve: func(report v2ports.ShadowReport) {
			shadowDone <- report
		},
		ShadowTimeout: time.Second,
	})
	if dispatchErr != nil {
		t.Fatalf("DispatchWithShadow() error = %v", dispatchErr)
	}

	select {
	case report := <-shadowDone:
		if report.Match {
			t.Fatalf("expected shadow mismatch, got match=true")
		}
		if report.ShadowError == "" {
			t.Fatalf("expected shadow_error populated, got empty")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shadow report")
	}
}

func TestDispatchWithShadow_FreezeFlagsBlockV1Path(t *testing.T) {
	t.Parallel()

	freezeFlags, err := portconfig.ParseV2FreezeCommands("list")
	if err != nil {
		t.Fatalf("ParseV2FreezeCommands() error = %v", err)
	}

	var v1Called atomic.Int32
	var v2Called atomic.Int32
	_, decision, dispatchErr := v2ports.DispatchWithShadow(context.Background(), v2ports.DispatchOptions[string]{
		Command:     "list",
		FreezeFlags: freezeFlags,
		V1: func(context.Context) (string, error) {
			v1Called.Add(1)
			return "v1", nil
		},
		V2: func(context.Context) (string, error) {
			v2Called.Add(1)
			return "v2", nil
		},
	})
	if dispatchErr == nil {
		t.Fatal("DispatchWithShadow() error = nil, want policy violation")
	}
	var v2Err *v2errors.V2Error
	if !errors.As(dispatchErr, &v2Err) {
		t.Fatalf("DispatchWithShadow() error type = %T, want *V2Error", dispatchErr)
	}
	if v2Err.Kind != v2errors.ErrPolicyViolation {
		t.Fatalf("error kind=%q want %q", v2Err.Kind, v2errors.ErrPolicyViolation)
	}
	if decision != v2ports.DecisionV1 {
		t.Fatalf("decision=%q want v1", decision)
	}
	if v1Called.Load() != 0 || v2Called.Load() != 0 {
		t.Fatalf("calls v1/v2 = %d/%d want 0/0", v1Called.Load(), v2Called.Load())
	}
}
