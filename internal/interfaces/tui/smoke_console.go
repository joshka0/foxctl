package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const defaultSmokeConsoleTimeout = 3 * time.Second

const (
	smokeStatusNotRequested = "not_requested"
	smokeStatusPending      = "pending"
	smokeStatusObserved     = "observed"
	smokeStatusDone         = "done"
	smokeStatusAccepted     = "accepted"
	smokeStatusError        = "error"
	smokeStatusTimeout      = "timeout"
)

// SmokeConsoleOptions configures the non-interactive attached-console smoke path.
type SmokeConsoleOptions struct {
	Options Options
	Ask     string
	Cancel  bool
	Timeout time.Duration
}

// SmokeConsoleSummary is a deterministic smoke report suitable for CLI output.
type SmokeConsoleSummary struct {
	InitialTranscriptRows int
	StreamEventsObserved  int
	StreamErrors          int
	StreamStatus          string
	AskAccepted           int
	AskErrors             int
	AskStatus             string
	CancelAccepted        int
	CancelErrors          int
	CancelStatus          string
	TimedOut              bool
}

func (s SmokeConsoleSummary) String() string {
	return fmt.Sprintf(
		"smoke_console initial_transcript_rows=%d stream_events=%d stream_errors=%d stream_status=%s ask_accepted=%d ask_errors=%d ask_status=%s cancel_accepted=%d cancel_errors=%d cancel_status=%s timed_out=%t",
		s.InitialTranscriptRows,
		s.StreamEventsObserved,
		s.StreamErrors,
		s.StreamStatus,
		s.AskAccepted,
		s.AskErrors,
		s.AskStatus,
		s.CancelAccepted,
		s.CancelErrors,
		s.CancelStatus,
		s.TimedOut,
	)
}

// RunConsoleSmoke validates attached-console API behavior without launching the interactive TUI.
func RunConsoleSmoke(parent context.Context, raw SmokeConsoleOptions) (SmokeConsoleSummary, error) {
	opts, err := normalizeSmokeConsoleOptions(raw)
	if err != nil {
		return SmokeConsoleSummary{}, err
	}

	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithTimeout(parent, opts.Timeout)
	defer cancel()

	initial, err := LoadInitialShellState(ctx, opts.Options)
	if err != nil {
		return SmokeConsoleSummary{}, err
	}

	summary := SmokeConsoleSummary{
		InitialTranscriptRows: len(initial.Transcript),
		StreamStatus:          smokeStatusPending,
		AskStatus:             smokeStatusNotRequested,
		CancelStatus:          smokeStatusNotRequested,
	}

	client, err := NewAPIClient(opts.Options.APIBaseURL, nil)
	if err != nil {
		return summary, fmt.Errorf("configure smoke api client: %w", err)
	}
	adapter, err := NewConsoleAdapter(client)
	if err != nil {
		return summary, fmt.Errorf("configure smoke console adapter: %w", err)
	}

	streamSource := NewHTTPConsoleStreamSource(
		client,
		opts.Options.ConsoleSessionID,
		ConsoleEventStreamOptions{PayloadFormat: true},
	)
	streamPump, err := NewConsoleStreamPump(ctx, streamSource, opts.Options.ConsoleStreamBuffer)
	if err != nil {
		return summary, fmt.Errorf("start smoke stream pump: %w", err)
	}
	defer streamPump.Close()

	var askRuntime *ConsoleAskRuntime
	if opts.Ask != "" {
		summary.AskStatus = smokeStatusPending
		submitter, submitterErr := NewHTTPConsoleAskSubmitter(adapter, opts.Options.ConsoleSessionID)
		if submitterErr != nil {
			return summary, fmt.Errorf("configure smoke ask submitter: %w", submitterErr)
		}
		askRuntime, err = NewConsoleAskRuntime(ctx, submitter, 0, 0)
		if err != nil {
			return summary, fmt.Errorf("start smoke ask runtime: %w", err)
		}
		defer askRuntime.Close()
		if err := askRuntime.Enqueue(ctx, AskConsoleSessionRequest{Content: opts.Ask}); err != nil {
			return summary, fmt.Errorf("enqueue smoke ask: %w", err)
		}
	}

	var cancelRuntime *ConsoleCancelRuntime
	if opts.Cancel {
		summary.CancelStatus = smokeStatusPending
		canceler, cancelerErr := NewHTTPConsoleCanceler(adapter, opts.Options.ConsoleSessionID)
		if cancelerErr != nil {
			return summary, fmt.Errorf("configure smoke canceler: %w", cancelerErr)
		}
		cancelRuntime, err = NewConsoleCancelRuntime(ctx, canceler, 0, 0)
		if err != nil {
			return summary, fmt.Errorf("start smoke cancel runtime: %w", err)
		}
		defer cancelRuntime.Close()
	}

	var askUpdates <-chan ConsoleAskUpdate
	if askRuntime != nil {
		askUpdates = askRuntime.Updates()
	}

	var cancelUpdates <-chan ConsoleCancelUpdate
	if cancelRuntime != nil {
		cancelUpdates = cancelRuntime.Updates()
	}

	streamReady := false
	askDone := askRuntime == nil
	cancelDone := cancelRuntime == nil
	cancelQueued := cancelRuntime == nil

	if cancelRuntime != nil && askRuntime == nil {
		if err := cancelRuntime.Enqueue(ctx, CancelConsoleSessionRequest{}); err != nil {
			return summary, fmt.Errorf("enqueue smoke cancel: %w", err)
		}
		cancelQueued = true
	}

	for {
		if err := ctx.Err(); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				summary.TimedOut = true
				if !streamReady {
					summary.StreamStatus = smokeStatusTimeout
				}
				if !askDone && summary.AskStatus == smokeStatusPending {
					summary.AskStatus = smokeStatusTimeout
				}
				if !cancelDone && summary.CancelStatus == smokeStatusPending {
					summary.CancelStatus = smokeStatusTimeout
				}
				return summary, nil
			}
			return summary, err
		}

		if streamReady && askDone && cancelDone {
			if summary.StreamStatus == smokeStatusPending {
				summary.StreamStatus = smokeStatusObserved
			}
			return summary, nil
		}

		select {
		case update, ok := <-streamPump.Updates():
			if !ok {
				streamReady = true
				if summary.StreamStatus == smokeStatusPending || summary.StreamStatus == smokeStatusObserved {
					summary.StreamStatus = smokeStatusDone
				}
				continue
			}
			switch update.Type {
			case ConsoleStreamUpdateEvent:
				streamReady = true
				summary.StreamEventsObserved++
				if summary.StreamStatus == smokeStatusPending {
					summary.StreamStatus = smokeStatusObserved
				}
			case ConsoleStreamUpdateError:
				streamReady = true
				summary.StreamErrors++
				summary.StreamStatus = smokeStatusError
			case ConsoleStreamUpdateDone:
				streamReady = true
				if summary.StreamStatus != smokeStatusError {
					summary.StreamStatus = smokeStatusDone
				}
			}
		case update, ok := <-askUpdates:
			if !ok {
				if !askDone && summary.AskStatus == smokeStatusPending {
					summary.AskStatus = smokeStatusError
					summary.AskErrors++
				}
				askDone = true
				askUpdates = nil
				continue
			}

			correlationID := ""
			switch update.Type {
			case ConsoleAskUpdateAccepted:
				summary.AskAccepted++
				summary.AskStatus = smokeStatusAccepted
				if update.Accepted != nil {
					correlationID = strings.TrimSpace(update.Accepted.CorrelationID)
				}
			case ConsoleAskUpdateError:
				summary.AskErrors++
				summary.AskStatus = smokeStatusError
				if update.Failed != nil {
					correlationID = strings.TrimSpace(update.Failed.CorrelationID)
				}
			default:
				summary.AskErrors++
				summary.AskStatus = smokeStatusError
			}
			askDone = true
			askUpdates = nil

			if cancelRuntime != nil && !cancelQueued {
				if enqueueErr := cancelRuntime.Enqueue(ctx, CancelConsoleSessionRequest{
					CorrelationID: correlationID,
				}); enqueueErr != nil {
					return summary, fmt.Errorf("enqueue smoke cancel: %w", enqueueErr)
				}
				cancelQueued = true
			}
		case update, ok := <-cancelUpdates:
			if !ok {
				if !cancelDone && summary.CancelStatus == smokeStatusPending {
					summary.CancelStatus = smokeStatusError
					summary.CancelErrors++
				}
				cancelDone = true
				cancelUpdates = nil
				continue
			}
			switch update.Type {
			case ConsoleCancelUpdateAccepted:
				summary.CancelAccepted++
				summary.CancelStatus = smokeStatusAccepted
			case ConsoleCancelUpdateError:
				summary.CancelErrors++
				summary.CancelStatus = smokeStatusError
			default:
				summary.CancelErrors++
				summary.CancelStatus = smokeStatusError
			}
			cancelDone = true
			cancelUpdates = nil
		case <-ctx.Done():
			continue
		}
	}
}

func normalizeSmokeConsoleOptions(raw SmokeConsoleOptions) (SmokeConsoleOptions, error) {
	opts := raw
	opts.Options.APIBaseURL = strings.TrimSpace(opts.Options.APIBaseURL)
	opts.Options.ConsoleSessionID = strings.TrimSpace(opts.Options.ConsoleSessionID)
	opts.Ask = strings.TrimSpace(opts.Ask)

	if opts.Options.APIBaseURL == "" {
		return SmokeConsoleOptions{}, errors.New("--smoke-console requires --api-base-url")
	}
	if opts.Options.ConsoleSessionID == "" {
		return SmokeConsoleOptions{}, errors.New("--smoke-console requires --console-session-id")
	}

	if opts.Timeout <= 0 {
		opts.Timeout = defaultSmokeConsoleTimeout
	}
	return opts, nil
}
