package contextbuilder_test

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jkatigb/agentctl/internal/v2/core/run"
	"github.com/jkatigb/agentctl/internal/v2/runtime/contextbuilder"
)

func TestContextBuilder_BuildLayered_DeterministicMixAndRefs(t *testing.T) {
	t.Parallel()

	reader := &layeredTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-l1",
				SessionID: "run-layered",
				CreatedAt: time.Date(2026, time.February, 18, 10, 0, 0, 0, time.UTC),
				Command:   "ask",
				Prompt:    "what changed?",
				FinalOutput: run.MessageRef{
					ID:   "msg-final-1",
					Role: "assistant",
					Text: "We updated the temporal context pipeline and added refs.",
				},
			},
			{
				ID:        "turn-l2",
				SessionID: "run-layered",
				CreatedAt: time.Date(2026, time.February, 18, 11, 0, 0, 0, time.UTC),
				Command:   "run",
				Prompt:    "summarize",
				FinalOutput: run.MessageRef{
					ID:   "msg-final-2",
					Role: "assistant",
					Text: strings.Repeat("x", 300),
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	builder.SetCompanionProvider(fakeCompanionProvider{})

	req := contextbuilder.LayeredRequest{
		SessionID: "run-layered",
		MaxChars:  6000,
	}
	got1, err := builder.BuildLayered(context.Background(), req)
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}
	got2, err := builder.BuildLayered(context.Background(), req)
	if err != nil {
		t.Fatalf("BuildLayered() second call error = %v", err)
	}

	if !reflect.DeepEqual(got1, got2) {
		t.Fatalf("BuildLayered not deterministic:\nfirst=%+v\nsecond=%+v", got1, got2)
	}
	if !strings.Contains(got1.Content, "## L2 History") {
		t.Fatalf("content missing L2 section: %q", got1.Content)
	}
	if !strings.Contains(got1.Content, "## L1 Recent") {
		t.Fatalf("content missing L1 section: %q", got1.Content)
	}
	if !strings.Contains(got1.Content, "## L0 Vivid") {
		t.Fatalf("content missing L0 section: %q", got1.Content)
	}
	if !containsString(got1.Refs, "companion:summary:run-layered:2026-02-18") {
		t.Fatalf("refs=%v missing companion ref", got1.Refs)
	}
	if !containsString(got1.TurnRefs, "turn/turn-l2") {
		t.Fatalf("turn refs=%v missing turn ref", got1.TurnRefs)
	}
	if len(got1.SliceRefs) == 0 {
		t.Fatalf("slice refs should not be empty: %+v", got1)
	}
	for _, ref := range got1.SliceRefs {
		parsed, err := contextbuilder.ParseRef(ref)
		if err != nil {
			t.Fatalf("slice ref parse error for %q: %v", ref, err)
		}
		if parsed.Kind != contextbuilder.RefSlice {
			t.Fatalf("slice ref kind=%q want %q", parsed.Kind, contextbuilder.RefSlice)
		}
	}
}

type layeredTurnReader struct {
	sessionTurns []run.TurnRecord
}

func (r *layeredTurnReader) GetTurn(_ context.Context, turnID string) (run.TurnRecord, error) {
	for _, turn := range r.sessionTurns {
		if turn.ID == turnID {
			return turn.Clone(), nil
		}
	}
	return run.TurnRecord{}, run.ErrTurnNotFound
}

func (r *layeredTurnReader) ListTurns(_ context.Context, sessionID string, opts run.TurnListOptions) ([]run.TurnRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	out := make([]run.TurnRecord, 0, len(r.sessionTurns))
	for _, turn := range r.sessionTurns {
		if turn.SessionID != sessionID {
			continue
		}
		if !opts.Since.IsZero() && turn.CreatedAt.Before(opts.Since) {
			continue
		}
		if !opts.Until.IsZero() && turn.CreatedAt.After(opts.Until) {
			continue
		}
		out = append(out, turn.Clone())
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		if opts.Asc {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})

	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

type fakeCompanionProvider struct{}

func (fakeCompanionProvider) GetLayeredContext(_ context.Context, _ string, _ contextbuilder.CompanionRequest) (contextbuilder.CompanionLayeredContext, error) {
	return contextbuilder.CompanionLayeredContext{
		L2: "Companion durable context",
		L1: "Companion daily summary",
		L0: "Companion vivid recall",
		Refs: []string{
			"companion:summary:run-layered:2026-02-18",
			"companion:history:run-layered",
		},
	}, nil
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
