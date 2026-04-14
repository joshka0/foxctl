package contextbuilder_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/v2/core/run"
	"github.com/joshka0/foxctl/internal/v2/runtime/contextbuilder"
)

func TestContextBuilder_BuildLayered_SemanticEmitsWideEvent(t *testing.T) {
	filePath := setupWideEvents(t)

	builder := newObsBuilder("run-layered-obs")
	builder.SetArtifactRetriever(&fakeArtifactRetriever{
		result: run.ArtifactSearchResult{
			SearchPath:       run.ArtifactSearchPathVector,
			VectorCapability: run.ArtifactVectorCapabilityEnabled,
			WorkingApplied:   true,
			FallbackLevel:    2,
			EligibleCount:    5,
			Hits: []run.ScoredArtifact{
				{
					Ref:             "turn/turn-obs-1/artifact/embedding/v1",
					TurnID:          "turn-obs-1",
					ArtifactType:    "embedding",
					ArtifactVersion: "v1",
					Similarity:      0.91,
				},
			},
		},
	})

	ctx, doneParent, parentSpan := observability.StartSpan(
		context.Background(),
		"test.parent",
		observability.WithSpanTraceID("trace-semantic-001"),
		observability.WithSpanComponent(observability.ComponentCLI),
		observability.WithSpanCommand("agent.ask"),
	)
	defer doneParent(nil)
	_, err := builder.BuildLayered(ctx, contextbuilder.LayeredRequest{
		SessionID: "run-layered-obs",
		Semantic: &contextbuilder.ArtifactSemanticQuery{
			QueryEmbedding: []float32{0.2, 0.4, 0.6},
			ArtifactTypes:  []string{"embedding"},
			Limit:          3,
			MinSimilarity:  0.5,
		},
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}

	evt := mustFindSemanticEvent(t, filePath, "run-layered-obs")
	if evt.Component != observability.ComponentContextBuilder {
		t.Fatalf("component=%q want %q", evt.Component, observability.ComponentContextBuilder)
	}
	if evt.TraceID != "trace-semantic-001" {
		t.Fatalf("trace_id=%q want trace-semantic-001", evt.TraceID)
	}
	if evt.ParentID != parentSpan.Build().SpanID {
		t.Fatalf("parent_id=%q want %q", evt.ParentID, parentSpan.Build().SpanID)
	}
	if evt.Status != observability.StatusOK {
		t.Fatalf("status=%q want %q", evt.Status, observability.StatusOK)
	}
	if evt.Data["search_path"] != string(run.ArtifactSearchPathVector) {
		t.Fatalf("data.search_path=%v want %q", evt.Data["search_path"], run.ArtifactSearchPathVector)
	}
	if evt.Data["vector_capability"] != string(run.ArtifactVectorCapabilityEnabled) {
		t.Fatalf("data.vector_capability=%v want %q", evt.Data["vector_capability"], run.ArtifactVectorCapabilityEnabled)
	}
	if got, ok := evt.Data["hit_count"].(float64); !ok || int(got) != 1 {
		t.Fatalf("data.hit_count=%v want 1", evt.Data["hit_count"])
	}
	if evt.Data["hit_bucket"] != "one_to_three" {
		t.Fatalf("data.hit_bucket=%v want one_to_three", evt.Data["hit_bucket"])
	}
	if _, ok := evt.Data["latency_bucket"].(string); !ok {
		t.Fatalf("data.latency_bucket missing/invalid: %v", evt.Data["latency_bucket"])
	}
	if evt.Data["session_id"] != "run-layered-obs" {
		t.Fatalf("data.session_id=%v want run-layered-obs", evt.Data["session_id"])
	}
	if got, ok := evt.Data["query_dims"].(float64); !ok || int(got) != 3 {
		t.Fatalf("data.query_dims=%v want 3", evt.Data["query_dims"])
	}
	if got, ok := evt.Data["working_context_applied"].(bool); !ok || !got {
		t.Fatalf("data.working_context_applied=%v want true", evt.Data["working_context_applied"])
	}
	if got, ok := evt.Data["working_context_fallback_level"].(float64); !ok || int(got) != 2 {
		t.Fatalf("data.working_context_fallback_level=%v want 2", evt.Data["working_context_fallback_level"])
	}
	if got, ok := evt.Data["working_context_eligible_count"].(float64); !ok || int(got) != 5 {
		t.Fatalf("data.working_context_eligible_count=%v want 5", evt.Data["working_context_eligible_count"])
	}
}

func TestContextBuilder_BuildLayered_SemanticErrorEmitsWideEvent(t *testing.T) {
	filePath := setupWideEvents(t)

	builder := newObsBuilder("run-layered-obs-error")
	builder.SetArtifactRetriever(&fakeArtifactRetriever{
		err: errors.New("semantic backend unavailable"),
	})

	_, err := builder.BuildLayered(context.Background(), contextbuilder.LayeredRequest{
		SessionID: "run-layered-obs-error",
		Semantic: &contextbuilder.ArtifactSemanticQuery{
			QueryEmbedding: []float32{0.1, 0.3, 0.5},
		},
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}

	evt := mustFindSemanticEvent(t, filePath, "run-layered-obs-error")
	if evt.Status != observability.StatusError {
		t.Fatalf("status=%q want %q", evt.Status, observability.StatusError)
	}
	if evt.Data["search_path"] != string(run.ArtifactSearchPathError) {
		t.Fatalf("data.search_path=%v want %q", evt.Data["search_path"], run.ArtifactSearchPathError)
	}
	if evt.Data["vector_capability"] != string(run.ArtifactVectorCapabilityUnknown) {
		t.Fatalf("data.vector_capability=%v want %q", evt.Data["vector_capability"], run.ArtifactVectorCapabilityUnknown)
	}
	if got, ok := evt.Data["hit_count"].(float64); !ok || int(got) != 0 {
		t.Fatalf("data.hit_count=%v want 0", evt.Data["hit_count"])
	}
	if evt.ErrorType != "semantic_retrieval" {
		t.Fatalf("error_type=%q want semantic_retrieval", evt.ErrorType)
	}
	if evt.ErrorCode != "ESEMANTIC_RETRIEVAL" {
		t.Fatalf("error_code=%q want ESEMANTIC_RETRIEVAL", evt.ErrorCode)
	}
	if evt.ErrorMessage == "" {
		t.Fatalf("error_message should be non-empty")
	}
}

func TestContextBuilder_BuildLayered_SemanticDisabledEmitsWideEvent(t *testing.T) {
	filePath := setupWideEvents(t)

	builder := newObsBuilder("run-layered-obs-disabled")
	// No artifact retriever configured: path must be disabled and still observable.

	_, err := builder.BuildLayered(context.Background(), contextbuilder.LayeredRequest{
		SessionID: "run-layered-obs-disabled",
		Semantic: &contextbuilder.ArtifactSemanticQuery{
			QueryEmbedding: []float32{0.8, 0.1},
		},
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}

	evt := mustFindSemanticEvent(t, filePath, "run-layered-obs-disabled")
	if evt.Status != observability.StatusOK {
		t.Fatalf("status=%q want %q", evt.Status, observability.StatusOK)
	}
	if evt.Data["search_path"] != string(run.ArtifactSearchPathDisabled) {
		t.Fatalf("data.search_path=%v want %q", evt.Data["search_path"], run.ArtifactSearchPathDisabled)
	}
	if evt.Data["vector_capability"] != string(run.ArtifactVectorCapabilityDisabled) {
		t.Fatalf("data.vector_capability=%v want %q", evt.Data["vector_capability"], run.ArtifactVectorCapabilityDisabled)
	}
	if got, ok := evt.Data["hit_count"].(float64); !ok || int(got) != 0 {
		t.Fatalf("data.hit_count=%v want 0", evt.Data["hit_count"])
	}
	if evt.Data["hit_bucket"] != "zero" {
		t.Fatalf("data.hit_bucket=%v want zero", evt.Data["hit_bucket"])
	}
}

func TestContextBuilder_BuildLayered_EmitsLayeredBundleEventWithRefs(t *testing.T) {
	filePath := setupWideEvents(t)

	builder := newObsBuilder("run-layered-obs-refs")
	got, err := builder.BuildLayered(context.Background(), contextbuilder.LayeredRequest{
		SessionID: "run-layered-obs-refs",
	})
	if err != nil {
		t.Fatalf("BuildLayered() error = %v", err)
	}
	if len(got.Refs) == 0 {
		t.Fatalf("expected layered refs, got none: %+v", got)
	}

	evt := mustFindOperationEvent(t, filePath, observability.OpContextLayeredBundle, "run-layered-obs-refs")
	if evt.Status != observability.StatusOK {
		t.Fatalf("status=%q want %q", evt.Status, observability.StatusOK)
	}
	if evt.Data["session_id"] != "run-layered-obs-refs" {
		t.Fatalf("data.session_id=%v want run-layered-obs-refs", evt.Data["session_id"])
	}
	if gotCount, ok := evt.Data["ref_count"].(float64); !ok || int(gotCount) <= 0 {
		t.Fatalf("data.ref_count=%v want >0", evt.Data["ref_count"])
	}
	if _, ok := evt.Data["refs"].([]any); !ok {
		t.Fatalf("data.refs type=%T want []any", evt.Data["refs"])
	}
	if _, ok := evt.Data["turn_refs"].([]any); !ok {
		t.Fatalf("data.turn_refs type=%T want []any", evt.Data["turn_refs"])
	}
}

func setupWideEvents(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	observability.SetObsDirForTesting(tmpDir)
	observability.SetSamplerForTesting(observability.SampleAll{})
	t.Cleanup(func() {
		observability.SetObsDirForTesting("")
		observability.SetSamplerForTesting(nil)
	})
	return filepath.Join(tmpDir, "events", observability.WideEventFileName+".ndjson")
}

func newObsBuilder(sessionID string) *contextbuilder.Builder {
	reader := &layeredTurnReader{
		sessionTurns: []run.TurnRecord{
			{
				ID:        "turn-obs-1",
				SessionID: sessionID,
				CreatedAt: time.Date(2026, time.February, 19, 12, 0, 0, 0, time.UTC),
				Command:   "ask",
				FinalOutput: run.MessageRef{
					ID:   "msg-obs-1",
					Role: "assistant",
					Text: "semantic retrieval for observability",
				},
			},
		},
	}

	builder := contextbuilder.New(reader)
	builder.SetCompanionProvider(fakeCompanionProvider{})
	return builder
}

func mustFindSemanticEvent(t *testing.T, filePath, sessionID string) observability.WideEvent {
	t.Helper()
	return mustFindOperationEvent(t, filePath, observability.OpContextSemanticArtifactSearch, sessionID)
}

func mustFindOperationEvent(t *testing.T, filePath, operation, sessionID string) observability.WideEvent {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		evt, found, err := findOperationEvent(filePath, operation, sessionID)
		if err == nil && found {
			return evt
		}
		if err != nil && !os.IsNotExist(err) {
			lastErr = err
			break
		}
		lastErr = err
		time.Sleep(15 * time.Millisecond)
	}

	if lastErr != nil {
		t.Fatalf("missing %q event for session %s (last err: %v)", operation, sessionID, lastErr)
	}
	t.Fatalf("missing %q event for session %s", operation, sessionID)
	return observability.WideEvent{}
}

func findOperationEvent(filePath, operation, sessionID string) (observability.WideEvent, bool, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return observability.WideEvent{}, false, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var evt observability.WideEvent
		if err := json.Unmarshal(scanner.Bytes(), &evt); err != nil {
			return observability.WideEvent{}, false, err
		}
		if evt.Operation != operation {
			continue
		}
		if evt.SessionID != sessionID {
			continue
		}
		return evt, true, nil
	}
	if err := scanner.Err(); err != nil {
		return observability.WideEvent{}, false, err
	}
	return observability.WideEvent{}, false, nil
}
