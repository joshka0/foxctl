package contextbuilder_test

import (
	"context"
	"errors"
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

func TestContextBuilder_BuildLayered_SemanticArtifactsDeterministic(t *testing.T) {
	t.Parallel()

	reader := &layeredTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-l1",
				SessionID: "run-layered",
				CreatedAt: time.Date(2026, time.February, 18, 10, 0, 0, 0, time.UTC),
				Command:   "ask",
				FinalOutput: run.MessageRef{
					ID:   "msg-final-1",
					Role: "assistant",
					Text: "final one",
				},
			},
			{
				ID:        "turn-l2",
				SessionID: "run-layered",
				CreatedAt: time.Date(2026, time.February, 18, 11, 0, 0, 0, time.UTC),
				Command:   "run",
				FinalOutput: run.MessageRef{
					ID:   "msg-final-2",
					Role: "assistant",
					Text: "final two",
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	builder.SetCompanionProvider(fakeCompanionProvider{})
	builder.SetArtifactRetriever(&fakeArtifactRetriever{
		result: run.ArtifactSearchResult{
			SearchPath:       run.ArtifactSearchPathFallback,
			VectorCapability: run.ArtifactVectorCapabilityDisabled,
			Hits: []run.ScoredArtifact{
				{
					Ref:             "turn/turn-l1/artifact/annotation/v1",
					TurnID:          "turn-l1",
					ArtifactType:    "annotation",
					ArtifactVersion: "v1",
					Similarity:      0.73,
				},
				{
					Ref:             "turn/turn-l2/artifact/embedding/v1",
					TurnID:          "turn-l2",
					ArtifactType:    "embedding",
					ArtifactVersion: "v1",
					Similarity:      0.91,
				},
				{
					Ref:             "turn/turn-l1/artifact/annotation/v1", // duplicate ref
					TurnID:          "turn-l1",
					ArtifactType:    "annotation",
					ArtifactVersion: "v1",
					Similarity:      0.70,
				},
				{
					Ref:             "turn/turn-l2/artifact/classification/v1", // filtered by min similarity
					TurnID:          "turn-l2",
					ArtifactType:    "classification",
					ArtifactVersion: "v1",
					Similarity:      0.40,
				},
			},
		},
	})

	got, err := builder.BuildLayered(context.Background(), contextbuilder.LayeredRequest{
		SessionID: "run-layered",
		MaxChars:  6000,
		Semantic: &contextbuilder.ArtifactSemanticQuery{
			QueryEmbedding: []float32{0.1, 0.2, 0.3},
			Limit:          5,
			MinSimilarity:  0.5,
		},
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}

	wantRefs := []string{
		"turn/turn-l2/artifact/embedding/v1",
		"turn/turn-l1/artifact/annotation/v1",
	}
	if !reflect.DeepEqual(got.ArtifactRefs, wantRefs) {
		t.Fatalf("artifact refs=%v want %v", got.ArtifactRefs, wantRefs)
	}
	if got.Meta["artifact_search_path"] != string(run.ArtifactSearchPathFallback) {
		t.Fatalf("artifact_search_path=%v want %q", got.Meta["artifact_search_path"], run.ArtifactSearchPathFallback)
	}
	if got.Meta["artifact_vector_capability"] != string(run.ArtifactVectorCapabilityDisabled) {
		t.Fatalf("artifact_vector_capability=%v want %q", got.Meta["artifact_vector_capability"], run.ArtifactVectorCapabilityDisabled)
	}
	if got.Meta["artifact_hit_count"] != len(wantRefs) {
		t.Fatalf("artifact_hit_count=%v want %d", got.Meta["artifact_hit_count"], len(wantRefs))
	}
	if !strings.Contains(got.Content, "## Semantic Artifacts") {
		t.Fatalf("content missing semantic section: %q", got.Content)
	}
	firstPos := strings.Index(got.Content, wantRefs[0])
	secondPos := strings.Index(got.Content, wantRefs[1])
	if firstPos < 0 || secondPos < 0 || firstPos > secondPos {
		t.Fatalf("semantic refs not rendered in deterministic order: %q", got.Content)
	}

	stats := builder.ArtifactStats()
	if stats.TotalCalls != 1 {
		t.Fatalf("stats.TotalCalls=%d want 1", stats.TotalCalls)
	}
	if stats.FallbackCalls != 1 {
		t.Fatalf("stats.FallbackCalls=%d want 1", stats.FallbackCalls)
	}
	if stats.VectorCalls != 0 || stats.ErrorCalls != 0 || stats.DisabledCalls != 0 {
		t.Fatalf("unexpected stats call distribution: %+v", stats)
	}
	if stats.TotalHits != 2 || stats.FallbackHits != 2 || stats.VectorHits != 0 {
		t.Fatalf("unexpected stats hit distribution: %+v", stats)
	}
	if stats.VectorCapabilityDisabledCalls != 1 || stats.VectorCapabilityEnabledCalls != 0 || stats.VectorCapabilityUnknownCalls != 0 {
		t.Fatalf("unexpected vector capability distribution: %+v", stats)
	}
	if stats.FallbackHitBucketOneTo3 != 1 || stats.FallbackHitBucketZero != 0 || stats.FallbackHitBucketFourTo10 != 0 || stats.FallbackHitBucketGT10 != 0 {
		t.Fatalf("unexpected fallback hit buckets: %+v", stats)
	}
	latencyBuckets := stats.FallbackLatencyLE10MS + stats.FallbackLatencyLE50MS + stats.FallbackLatencyLE100MS + stats.FallbackLatencyGT100MS
	if latencyBuckets != 1 {
		t.Fatalf("fallback latency bucket total=%d want 1", latencyBuckets)
	}
}

func TestContextBuilder_BuildLayered_SemanticErrorNonFatal(t *testing.T) {
	t.Parallel()

	reader := &layeredTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-l1",
				SessionID: "run-layered",
				CreatedAt: time.Date(2026, time.February, 18, 10, 0, 0, 0, time.UTC),
				Command:   "ask",
				FinalOutput: run.MessageRef{
					ID:   "msg-final-1",
					Role: "assistant",
					Text: "final one",
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	builder.SetCompanionProvider(fakeCompanionProvider{})
	builder.SetArtifactRetriever(&fakeArtifactRetriever{
		err: errors.New("semantic backend unavailable"),
	})

	got, err := builder.BuildLayered(context.Background(), contextbuilder.LayeredRequest{
		SessionID: "run-layered",
		Semantic: &contextbuilder.ArtifactSemanticQuery{
			QueryEmbedding: []float32{0.1, 0.2, 0.3},
		},
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}

	if got.Meta["artifact_search_path"] != string(run.ArtifactSearchPathError) {
		t.Fatalf("artifact_search_path=%v want %q", got.Meta["artifact_search_path"], run.ArtifactSearchPathError)
	}
	if got.Meta["artifact_vector_capability"] != string(run.ArtifactVectorCapabilityUnknown) {
		t.Fatalf("artifact_vector_capability=%v want %q", got.Meta["artifact_vector_capability"], run.ArtifactVectorCapabilityUnknown)
	}
	if got.Meta["artifact_hit_count"] != 0 {
		t.Fatalf("artifact_hit_count=%v want 0", got.Meta["artifact_hit_count"])
	}
	if strings.TrimSpace(got.Meta["artifact_search_error"].(string)) == "" {
		t.Fatalf("artifact_search_error should be populated: %#v", got.Meta)
	}
	if strings.Contains(got.Content, "## Semantic Artifacts") {
		t.Fatalf("semantic section should be omitted on error: %q", got.Content)
	}

	stats := builder.ArtifactStats()
	if stats.TotalCalls != 1 {
		t.Fatalf("stats.TotalCalls=%d want 1", stats.TotalCalls)
	}
	if stats.ErrorCalls != 1 {
		t.Fatalf("stats.ErrorCalls=%d want 1", stats.ErrorCalls)
	}
	if stats.TotalHits != 0 || stats.VectorHits != 0 || stats.FallbackHits != 0 {
		t.Fatalf("unexpected stats hits on error path: %+v", stats)
	}
	if stats.VectorCapabilityUnknownCalls != 1 || stats.VectorCapabilityEnabledCalls != 0 || stats.VectorCapabilityDisabledCalls != 0 {
		t.Fatalf("unexpected vector capability stats on error path: %+v", stats)
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

type fakeArtifactRetriever struct {
	result run.ArtifactSearchResult
	err    error
}

func (f *fakeArtifactRetriever) SearchArtifactsByEmbedding(_ context.Context, _ []float32, _ run.ArtifactSearchOptions) (run.ArtifactSearchResult, error) {
	if f.err != nil {
		return run.ArtifactSearchResult{}, f.err
	}
	out := run.ArtifactSearchResult{
		SearchPath:       f.result.SearchPath,
		VectorCapability: f.result.VectorCapability,
		Hits:             make([]run.ScoredArtifact, 0, len(f.result.Hits)),
	}
	for _, hit := range f.result.Hits {
		out.Hits = append(out.Hits, hit.Clone())
	}
	return out, nil
}
