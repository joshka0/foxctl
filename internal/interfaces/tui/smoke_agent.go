package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const defaultSmokeAgentTimeout = 10 * time.Second

type SmokeAgentOptions struct {
	Options Options
	Ask     string
	Timeout time.Duration
}

type SmokeAgentSummary struct {
	InitialTranscriptRows int
	AskAccepted           int
	AskErrors             int
	AskStatus             string
	Reply                 string
	TimedOut              bool
}

func (s SmokeAgentSummary) String() string {
	return fmt.Sprintf(
		"smoke_agent initial_transcript_rows=%d ask_accepted=%d ask_errors=%d ask_status=%s reply=%q timed_out=%t",
		s.InitialTranscriptRows,
		s.AskAccepted,
		s.AskErrors,
		s.AskStatus,
		s.Reply,
		s.TimedOut,
	)
}

func RunAgentSmoke(parent context.Context, raw SmokeAgentOptions) (SmokeAgentSummary, error) {
	opts, err := normalizeSmokeAgentOptions(raw)
	if err != nil {
		return SmokeAgentSummary{}, err
	}
	if parent == nil {
		parent = context.Background()
	}

	ctx, cancel := context.WithTimeout(parent, opts.Timeout)
	defer cancel()

	initial, err := LoadInitialShellState(ctx, opts.Options)
	if err != nil {
		return SmokeAgentSummary{}, err
	}
	summary := SmokeAgentSummary{
		InitialTranscriptRows: len(initial.Transcript),
		AskStatus:             smokeStatusPending,
	}

	client, err := NewAPIClient(opts.Options.APIBaseURL, nil)
	if err != nil {
		return summary, fmt.Errorf("configure smoke api client: %w", err)
	}
	agentAdapter, err := NewAgentAdapter(client)
	if err != nil {
		return summary, fmt.Errorf("configure smoke agent adapter: %w", err)
	}
	submitter, err := NewHTTPAgentAskSubmitter(agentAdapter, opts.Options.AgentID)
	if err != nil {
		return summary, fmt.Errorf("configure smoke agent submitter: %w", err)
	}
	askRuntime, err := NewConsoleAskRuntime(ctx, submitter, 0, 0)
	if err != nil {
		return summary, fmt.Errorf("start smoke agent ask runtime: %w", err)
	}
	defer askRuntime.Close()

	if err := askRuntime.Enqueue(ctx, AskConsoleSessionRequest{Content: opts.Ask}); err != nil {
		return summary, fmt.Errorf("enqueue smoke agent ask: %w", err)
	}

	for {
		select {
		case update, ok := <-askRuntime.Updates():
			if !ok {
				if summary.AskStatus == smokeStatusPending {
					summary.AskStatus = smokeStatusError
					summary.AskErrors++
				}
				return summary, nil
			}
			switch update.Type {
			case ConsoleAskUpdateAccepted:
				summary.AskAccepted++
				summary.AskStatus = smokeStatusAccepted
				if update.Accepted != nil {
					summary.Reply = strings.TrimSpace(update.Accepted.Message)
				}
			case ConsoleAskUpdateError:
				summary.AskErrors++
				summary.AskStatus = smokeStatusError
				if update.Failed != nil && update.Failed.Err != nil {
					summary.Reply = update.Failed.Err.Error()
				}
			default:
				summary.AskErrors++
				summary.AskStatus = smokeStatusError
			}
			return summary, nil
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				summary.TimedOut = true
				if summary.AskStatus == smokeStatusPending {
					summary.AskStatus = smokeStatusTimeout
				}
				return summary, nil
			}
			return summary, ctx.Err()
		}
	}
}

func normalizeSmokeAgentOptions(raw SmokeAgentOptions) (SmokeAgentOptions, error) {
	opts := raw
	opts.Options.APIBaseURL = strings.TrimSpace(opts.Options.APIBaseURL)
	opts.Options.AgentID = strings.TrimSpace(opts.Options.AgentID)
	opts.Ask = strings.TrimSpace(opts.Ask)

	if opts.Options.APIBaseURL == "" {
		return SmokeAgentOptions{}, errors.New("--smoke-agent requires --api-base-url")
	}
	if opts.Options.AgentID == "" {
		return SmokeAgentOptions{}, errors.New("--smoke-agent requires --agent-id")
	}
	if opts.Ask == "" {
		return SmokeAgentOptions{}, errors.New("--smoke-agent requires --smoke-ask")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultSmokeAgentTimeout
	}
	return opts, nil
}
