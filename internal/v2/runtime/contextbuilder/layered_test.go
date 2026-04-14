package contextbuilder_test

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/v2/core/run"
	"github.com/joshka0/foxctl/internal/v2/runtime/contextbuilder"
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

func TestContextBuilder_BuildLayered_IncludesEpisodeRefs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 18, 12, 0, 0, 0, time.UTC)
	reader := &layeredTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-l1",
				SessionID: "run-layered-ep",
				CreatedAt: now.Add(-2 * time.Hour),
				Command:   "ask",
				FinalOutput: run.MessageRef{
					ID:   "msg-final-1",
					Role: "assistant",
					Text: "First output",
				},
			},
			{
				ID:        "turn-l2",
				SessionID: "run-layered-ep",
				CreatedAt: now.Add(-1 * time.Hour),
				Command:   "run",
				FinalOutput: run.MessageRef{
					ID:   "msg-final-2",
					Role: "assistant",
					Text: "Second output",
				},
			},
		},
		sessionEps: []run.EpisodeRecord{
			{
				ID:             "ep-layered-1",
				SessionID:      "run-layered-ep",
				EpisodeVersion: "v1",
				BoundaryKey:    "chunk:0001-0001",
				StartTurnID:    "turn-l1",
				EndTurnID:      "turn-l1",
				StartTurnIndex: 1,
				EndTurnIndex:   1,
				Topic:          "Initial investigation",
				Summary:        "Captured initial investigation details.",
				IsLandmark:     false,
				AnchorRefs: []string{
					"turn/turn-l1",
					"turn/turn-l1/artifact/annotation/v1",
				},
				CreatedAt: now.Add(-2 * time.Hour),
			},
			{
				ID:             "ep-layered-2",
				SessionID:      "run-layered-ep",
				EpisodeVersion: "v1",
				BoundaryKey:    "chunk:0002-0002",
				StartTurnID:    "turn-l2",
				EndTurnID:      "turn-l2",
				StartTurnIndex: 2,
				EndTurnIndex:   2,
				Topic:          "Decision milestone",
				Summary:        "Marked a decision checkpoint for rollout.",
				IsLandmark:     true,
				AnchorRefs: []string{
					"turn/turn-l2",
					"turn/turn-l2/artifact/learning/v1",
				},
				CreatedAt: now.Add(-1 * time.Hour),
			},
		},
	}

	builder := contextbuilder.New(reader)
	builder.SetNow(func() time.Time { return now })
	builder.SetCompanionProvider(fakeCompanionProvider{})

	got, err := builder.BuildLayered(context.Background(), contextbuilder.LayeredRequest{
		SessionID: "run-layered-ep",
		MaxChars:  6000,
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}

	if !containsString(got.EpisodeRefs, "episode/ep-layered-1") || !containsString(got.EpisodeRefs, "episode/ep-layered-2") {
		t.Fatalf("episode refs=%v missing episode refs", got.EpisodeRefs)
	}
	if !containsString(got.Refs, "turn/turn-l1/artifact/annotation/v1") {
		t.Fatalf("refs=%v missing episode anchor ref turn/turn-l1/artifact/annotation/v1", got.Refs)
	}
	if !containsString(got.Refs, "turn/turn-l2/artifact/learning/v1") {
		t.Fatalf("refs=%v missing episode anchor ref turn/turn-l2/artifact/learning/v1", got.Refs)
	}
	if got.Meta["episode_count"] != 2 {
		t.Fatalf("meta.episode_count=%v want 2", got.Meta["episode_count"])
	}
	if got.Meta["episode_landmark_count"] != 1 {
		t.Fatalf("meta.episode_landmark_count=%v want 1", got.Meta["episode_landmark_count"])
	}
	if !strings.Contains(got.Layers["L2"], "[landmark]") {
		t.Fatalf("L2 layer missing landmark annotation: %q", got.Layers["L2"])
	}
}

func TestContextBuilder_BuildLayered_IncludesNarrativeSectionAndRefs(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 18, 12, 0, 0, 0, time.UTC)
	reader := &layeredTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-n1",
				SessionID: "run-layered-narrative",
				CreatedAt: now.Add(-30 * time.Minute),
				Command:   "ask",
				FinalOutput: run.MessageRef{
					ID:   "msg-final-n1",
					Role: "assistant",
					Text: "first narrative seed",
				},
			},
		},
		narrative: &run.NarrativeRecord{
			SessionID:       "run-layered-narrative",
			TurnID:          "turn-n1",
			Ref:             "turn/turn-n1/artifact/narrative/v1",
			ArtifactVersion: "v1",
			Summary:         "We stabilized the retrieval path and agreed on strict citation.",
			Claims: []run.NarrativeClaim{
				{
					Text:       "Team agreed to require anchor refs on narrative claims.",
					AnchorRefs: []string{"turn/turn-n1", "turn/turn-n1/artifact/annotation/v1"},
				},
			},
			AnchorRefs: []string{"turn/turn-n1", "turn/turn-n1/artifact/annotation/v1"},
			UpdatedAt:  now,
		},
	}

	builder := contextbuilder.New(reader)
	builder.SetNow(func() time.Time { return now })
	builder.SetCompanionProvider(fakeCompanionProvider{})

	got, err := builder.BuildLayered(context.Background(), contextbuilder.LayeredRequest{
		SessionID: "run-layered-narrative",
		MaxChars:  6000,
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}

	if !strings.Contains(got.Content, "## Narrative") {
		t.Fatalf("content missing narrative section: %q", got.Content)
	}
	if !containsString(got.NarrativeRefs, "turn/turn-n1/artifact/narrative/v1") {
		t.Fatalf("narrative refs=%v missing narrative artifact ref", got.NarrativeRefs)
	}
	if !containsString(got.Refs, "turn/turn-n1/artifact/annotation/v1") {
		t.Fatalf("refs=%v missing narrative anchor ref", got.Refs)
	}
	if got.Meta["narrative_present"] != true {
		t.Fatalf("meta.narrative_present=%v want true", got.Meta["narrative_present"])
	}
	if got.Meta["narrative_claim_count"] != 1 {
		t.Fatalf("meta.narrative_claim_count=%v want 1", got.Meta["narrative_claim_count"])
	}
	if got.Meta["narrative_version"] != "v1" {
		t.Fatalf("meta.narrative_version=%v want v1", got.Meta["narrative_version"])
	}
	if got.Meta["narrative_stale"] != false {
		t.Fatalf("meta.narrative_stale=%v want false", got.Meta["narrative_stale"])
	}
	if got.Meta["narrative_age_seconds"] != int64(0) {
		t.Fatalf("meta.narrative_age_seconds=%v want 0", got.Meta["narrative_age_seconds"])
	}
	if got.Meta["narrative_max_age_seconds"] != int64(1800) {
		t.Fatalf("meta.narrative_max_age_seconds=%v want 1800", got.Meta["narrative_max_age_seconds"])
	}
}

func TestContextBuilder_BuildLayered_NarrativeStalenessMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.February, 23, 12, 0, 0, 0, time.UTC)
	reader := &layeredTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-n-stale",
				SessionID: "run-layered-narrative-stale",
				CreatedAt: now.Add(-2 * time.Hour),
				Command:   "ask",
				FinalOutput: run.MessageRef{
					ID:   "msg-final-n-stale",
					Role: "assistant",
					Text: "stale narrative test",
				},
			},
		},
		narrative: &run.NarrativeRecord{
			SessionID:       "run-layered-narrative-stale",
			TurnID:          "turn-n-stale",
			ArtifactVersion: "v1",
			Summary:         "Old narrative snapshot.",
			Claims: []run.NarrativeClaim{
				{
					Text:       "Prior summary exists for stale metadata assertion.",
					AnchorRefs: []string{"turn/turn-n-stale"},
				},
			},
			AnchorRefs: []string{"turn/turn-n-stale"},
			UpdatedAt:  now.Add(-2 * time.Hour),
		},
	}

	builder := contextbuilder.New(reader)
	builder.SetNow(func() time.Time { return now })
	builder.SetCompanionProvider(fakeCompanionProvider{})

	got, err := builder.BuildLayered(context.Background(), contextbuilder.LayeredRequest{
		SessionID: "run-layered-narrative-stale",
		MaxChars:  6000,
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}

	if got.Meta["narrative_stale"] != true {
		t.Fatalf("meta.narrative_stale=%v want true", got.Meta["narrative_stale"])
	}
	age, ok := got.Meta["narrative_age_seconds"].(int64)
	if !ok {
		t.Fatalf("meta.narrative_age_seconds=%T want int64", got.Meta["narrative_age_seconds"])
	}
	if age < int64((2 * time.Hour).Seconds()) {
		t.Fatalf("meta.narrative_age_seconds=%d want >= %d", age, int64((2 * time.Hour).Seconds()))
	}
	if got.Meta["narrative_max_age_seconds"] != int64(1800) {
		t.Fatalf("meta.narrative_max_age_seconds=%v want 1800", got.Meta["narrative_max_age_seconds"])
	}
}

func TestContextBuilder_BuildLayered_BudgetReallocatesWhenSemanticUnavailable(t *testing.T) {
	t.Parallel()

	reader := &layeredTurnReader{}
	builder := contextbuilder.New(reader)
	builder.SetCompanionProvider(fakeCompanionProvider{})

	got, err := builder.BuildLayered(context.Background(), contextbuilder.LayeredRequest{
		SessionID: "run-layered",
		MaxChars:  100,
		// Semantic request exists but cannot execute (empty embedding + no retriever).
		Semantic: &contextbuilder.ArtifactSemanticQuery{},
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}

	if got.Meta["budget_semantic_chars"] != 0 {
		t.Fatalf("budget_semantic_chars=%v want 0", got.Meta["budget_semantic_chars"])
	}
	if got.Meta["budget_l0_chars"] != 55 {
		t.Fatalf("budget_l0_chars=%v want 55", got.Meta["budget_l0_chars"])
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
			WorkingApplied:   true,
			FallbackLevel:    1,
			EligibleCount:    7,
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
	if got.Meta["working_context_applied"] != true {
		t.Fatalf("working_context_applied=%v want true", got.Meta["working_context_applied"])
	}
	if got.Meta["working_context_fallback_level"] != 1 {
		t.Fatalf("working_context_fallback_level=%v want 1", got.Meta["working_context_fallback_level"])
	}
	if got.Meta["working_context_eligible_count"] != 7 {
		t.Fatalf("working_context_eligible_count=%v want 7", got.Meta["working_context_eligible_count"])
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

func TestContextBuilder_BuildLayered_SemanticPassesWorkingContext(t *testing.T) {
	t.Parallel()

	reader := &layeredTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-l1",
				SessionID: "run-layered-working",
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

	retriever := &fakeArtifactRetriever{
		result: run.ArtifactSearchResult{
			SearchPath:       run.ArtifactSearchPathFallback,
			VectorCapability: run.ArtifactVectorCapabilityDisabled,
		},
	}

	builder := contextbuilder.New(reader)
	builder.SetCompanionProvider(fakeCompanionProvider{})
	builder.SetArtifactRetriever(retriever)

	_, err := builder.BuildLayered(context.Background(), contextbuilder.LayeredRequest{
		SessionID: "run-layered-working",
		Semantic: &contextbuilder.ArtifactSemanticQuery{
			QueryEmbedding: []float32{0.2, 0.3, 0.4},
			Working: run.WorkingContext{
				WorkspaceID:    "ws-1",
				ActiveFiles:    []string{"internal/v2/runtime/contextbuilder/layered.go"},
				RequiredLabels: []string{"auth", "decision"},
				MinSalience:    0.75,
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}

	if retriever.lastOpts.Working.WorkspaceID != "ws-1" {
		t.Fatalf("working.workspace_id=%q want ws-1", retriever.lastOpts.Working.WorkspaceID)
	}
	if !reflect.DeepEqual(retriever.lastOpts.Working.RequiredLabels, []string{"auth", "decision"}) {
		t.Fatalf("working.required_labels=%v want [auth decision]", retriever.lastOpts.Working.RequiredLabels)
	}
	if retriever.lastOpts.Working.MinSalience != 0.75 {
		t.Fatalf("working.min_salience=%v want 0.75", retriever.lastOpts.Working.MinSalience)
	}
}

type layeredTurnReader struct {
	sessionTurns []run.TurnRecord
	sessionEps   []run.EpisodeRecord
	narrative    *run.NarrativeRecord
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

func (r *layeredTurnReader) GetEpisode(_ context.Context, episodeID string) (run.EpisodeRecord, error) {
	for _, episode := range r.sessionEps {
		if episode.ID == episodeID {
			return episode.Clone(), nil
		}
	}
	return run.EpisodeRecord{}, run.ErrEpisodeNotFound
}

func (r *layeredTurnReader) ListEpisodes(_ context.Context, sessionID string, opts run.EpisodeListOptions) ([]run.EpisodeRecord, error) {
	sessionID = strings.TrimSpace(sessionID)
	out := make([]run.EpisodeRecord, 0, len(r.sessionEps))
	for _, episode := range r.sessionEps {
		if episode.SessionID != sessionID {
			continue
		}
		if opts.LandmarkOnly && !episode.IsLandmark {
			continue
		}
		if !opts.Since.IsZero() && episode.CreatedAt.Before(opts.Since) {
			continue
		}
		if !opts.Until.IsZero() && episode.CreatedAt.After(opts.Until) {
			continue
		}
		out = append(out, episode.Clone())
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

func (r *layeredTurnReader) GetNarrative(_ context.Context, sessionID, _ string) (run.NarrativeRecord, error) {
	if r.narrative == nil {
		return run.NarrativeRecord{}, run.ErrNarrativeNotFound
	}
	narrative := r.narrative.Clone()
	if strings.TrimSpace(narrative.SessionID) != strings.TrimSpace(sessionID) {
		return run.NarrativeRecord{}, run.ErrNarrativeNotFound
	}
	return narrative, nil
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
	result   run.ArtifactSearchResult
	err      error
	lastOpts run.ArtifactSearchOptions
}

func (f *fakeArtifactRetriever) SearchArtifactsByEmbedding(_ context.Context, _ []float32, opts run.ArtifactSearchOptions) (run.ArtifactSearchResult, error) {
	f.lastOpts = opts
	if f.err != nil {
		return run.ArtifactSearchResult{}, f.err
	}
	out := run.ArtifactSearchResult{
		SearchPath:       f.result.SearchPath,
		VectorCapability: f.result.VectorCapability,
		WorkingApplied:   f.result.WorkingApplied,
		FallbackLevel:    f.result.FallbackLevel,
		EligibleCount:    f.result.EligibleCount,
		Hits:             make([]run.ScoredArtifact, 0, len(f.result.Hits)),
	}
	for _, hit := range f.result.Hits {
		out.Hits = append(out.Hits, hit.Clone())
	}
	return out, nil
}
