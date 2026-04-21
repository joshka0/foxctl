package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
	libsqlevents "github.com/joshka0/foxctl/internal/v2/adapters/libsql/events"
	libsqlprojections "github.com/joshka0/foxctl/internal/v2/adapters/libsql/projections"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/runtime/runner"
)

func TestV2RunsListHandlerReturnsEnvelope(t *testing.T) {
	t.Setenv("FOXCTL_DB_DRIVER", "sqlite")
	t.Setenv("FOXCTL_V2_EVENTS_DB_DRIVER", "sqlite")
	t.Setenv("FOXCTL_V2_PROJECTIONS_DB_DRIVER", "sqlite")

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()

	projectionStore, closeFn, err := libsqlprojections.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open projection store: %v", err)
	}
	defer func() { _ = closeFn() }()

	projectionStore.SetNowForTest(func() time.Time {
		return time.Date(2026, time.April, 21, 11, 0, 0, 0, time.UTC)
	})
	if err := projectionStore.Apply(ctx, coreevents.Event{
		ID:            "evt-run-1",
		StreamID:      "run-1",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		Command:       "spawn",
		RequestID:     "req-1",
		ActorID:       "actor:a",
	}); err != nil {
		t.Fatalf("apply run-1: %v", err)
	}
	if err := projectionStore.Apply(ctx, coreevents.Event{
		ID:            "evt-run-2",
		StreamID:      "run-2",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunCompleted,
		Command:       "ask",
		RequestID:     "req-2",
		ActorID:       "actor:b",
	}); err != nil {
		t.Fatalf("apply run-2: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v2/runs?status=completed&limit=10", nil)
	rr := httptest.NewRecorder()
	V2RunsHandler(cfg, zerolog.Nop()).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if got := strings.TrimSpace(body["status"].(string)); got != "ok" {
		t.Fatalf("envelope status=%q want ok", got)
	}
	if got := strings.TrimSpace(body["command"].(string)); got != commandV2RunsList {
		t.Fatalf("command=%q want %q", got, commandV2RunsList)
	}
	data, _ := body["data"].(map[string]any)
	if count := int(data["count"].(float64)); count != 1 {
		t.Fatalf("count=%d want 1", count)
	}
	items, _ := data["items"].([]any)
	item, _ := items[0].(map[string]any)
	if got := strings.TrimSpace(item["run_id"].(string)); got != "run-2" {
		t.Fatalf("run_id=%q want run-2", got)
	}
}

func TestV2RunDetailEventsHandlerAfterVersion(t *testing.T) {
	t.Setenv("FOXCTL_DB_DRIVER", "sqlite")
	t.Setenv("FOXCTL_V2_EVENTS_DB_DRIVER", "sqlite")
	t.Setenv("FOXCTL_V2_PROJECTIONS_DB_DRIVER", "sqlite")

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	eventStore, err := libsqlevents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open event store: %v", err)
	}
	defer func() { _ = eventStore.Close() }()

	eventStore.SetNowForTest(func() time.Time {
		return time.Date(2026, time.April, 21, 12, 0, 0, 0, time.UTC)
	})
	for i, evtType := range []coreevents.EventType{coreevents.EventRunStarted, coreevents.EventTurnRecorded} {
		if err := eventStore.Append(ctx, coreevents.Event{
			ID:            "evt-e-" + string(rune('1'+i)),
			StreamID:      "run-events-1",
			StreamType:    coreevents.StreamTypeRun,
			StreamVersion: int64(i + 1),
			Sequence:      int64(i + 1),
			EventType:     evtType,
			OccurredAt:    time.Date(2026, time.April, 21, 12, 0, i, 0, time.UTC),
			RequestID:     "req-events-1",
			ActorID:       "actor:events",
			Command:       "run",
		}); err != nil {
			t.Fatalf("append event %d: %v", i+1, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v2/runs/run-events-1/events?after_version=1", nil)
	rr := httptest.NewRecorder()
	V2RunDetailHandler(cfg, zerolog.Nop(), nil).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if got := strings.TrimSpace(body["command"].(string)); got != commandV2RunEvents {
		t.Fatalf("command=%q want %q", got, commandV2RunEvents)
	}
	data, _ := body["data"].(map[string]any)
	if count := int(data["count"].(float64)); count != 1 {
		t.Fatalf("count=%d want 1", count)
	}
	events, _ := data["events"].([]any)
	event, _ := events[0].(map[string]any)
	if version := int(event["stream_version"].(float64)); version != 2 {
		t.Fatalf("stream_version=%d want 2", version)
	}
}

func TestV2EventsHandlerReplaysCanonicalStream(t *testing.T) {
	t.Setenv("FOXCTL_DB_DRIVER", "sqlite")
	t.Setenv("FOXCTL_V2_EVENTS_DB_DRIVER", "sqlite")
	t.Setenv("FOXCTL_V2_PROJECTIONS_DB_DRIVER", "sqlite")

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	eventStore, err := libsqlevents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open event store: %v", err)
	}
	defer func() { _ = eventStore.Close() }()

	for i, evtType := range []coreevents.EventType{coreevents.EventRunStarted, coreevents.EventRunCompleted} {
		if err := eventStore.Append(ctx, coreevents.Event{
			ID:            "evt-v2-global-" + string(rune('1'+i)),
			StreamID:      "run-global-events-1",
			StreamType:    coreevents.StreamTypeRun,
			StreamVersion: int64(i + 1),
			Sequence:      int64(i + 1),
			EventType:     evtType,
			OccurredAt:    time.Date(2026, time.April, 21, 13, 0, i, 0, time.UTC),
			RequestID:     "req-global-events-1",
			Command:       "run",
		}); err != nil {
			t.Fatalf("append event %d: %v", i+1, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v2/events?stream_id=run-global-events-1&stream_type=run&after_version=1", nil)
	rr := httptest.NewRecorder()
	V2EventsHandler(cfg, zerolog.Nop()).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if got := strings.TrimSpace(body["command"].(string)); got != commandV2Events {
		t.Fatalf("command=%q want %q", got, commandV2Events)
	}
	data, _ := body["data"].(map[string]any)
	if got := strings.TrimSpace(data["stream_id"].(string)); got != "run-global-events-1" {
		t.Fatalf("stream_id=%q want run-global-events-1", got)
	}
	if count := int(data["count"].(float64)); count != 1 {
		t.Fatalf("count=%d want 1", count)
	}
	events, _ := data["events"].([]any)
	event, _ := events[0].(map[string]any)
	if version := int(event["stream_version"].(float64)); version != 2 {
		t.Fatalf("stream_version=%d want 2", version)
	}
}

func TestV2RunsCreateExecutesRunAndProjectsState(t *testing.T) {
	t.Setenv("FOXCTL_DB_DRIVER", "sqlite")
	t.Setenv("FOXCTL_V2_EVENTS_DB_DRIVER", "sqlite")
	t.Setenv("FOXCTL_V2_PROJECTIONS_DB_DRIVER", "sqlite")

	cfg := orchestrationTestConfig(t.TempDir())
	oldNewModel := newV2RunModel
	newV2RunModel = func(config.Config, string) (runner.Model, error) {
		return testV2RunModel{}, nil
	}
	t.Cleanup(func() {
		newV2RunModel = oldNewModel
	})

	createReq := httptest.NewRequest(http.MethodPost, "/api/v2/runs?profile=worker", strings.NewReader(`{
		"run_id":"run-new-1",
		"turn_id":"turn-new-1",
		"request_id":"req-new-1",
		"prompt":"summarize this run",
		"max_iterations":1
	}`))
	createRR := httptest.NewRecorder()
	V2RunsHandler(cfg, zerolog.Nop()).ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", createRR.Code, createRR.Body.String())
	}
	createBody := decodeResponseBody(t, createRR)
	if got := strings.TrimSpace(createBody["command"].(string)); got != commandV2RunsCreate {
		t.Fatalf("create command=%q want %q", got, commandV2RunsCreate)
	}
	data, _ := createBody["data"].(map[string]any)
	if got := strings.TrimSpace(data["run_id"].(string)); got != "run-new-1" {
		t.Fatalf("run_id=%q want run-new-1", got)
	}
	output, _ := data["output"].(map[string]any)
	if got := strings.TrimSpace(output["summary"].(string)); got != "model response for summarize this run" {
		t.Fatalf("summary=%q", got)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v2/runs/run-new-1", nil)
	getRR := httptest.NewRecorder()
	V2RunDetailHandler(cfg, zerolog.Nop(), nil).ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	getBody := decodeResponseBody(t, getRR)
	state, _ := getBody["data"].(map[string]any)
	if got := strings.TrimSpace(state["status"].(string)); got != "completed" {
		t.Fatalf("projected status=%q want completed", got)
	}
	if got := strings.TrimSpace(state["request_id"].(string)); got != "req-new-1" {
		t.Fatalf("projected request_id=%q want req-new-1", got)
	}

	eventsReq := httptest.NewRequest(http.MethodGet, "/api/v2/runs/run-new-1/events", nil)
	eventsRR := httptest.NewRecorder()
	V2RunDetailHandler(cfg, zerolog.Nop(), nil).ServeHTTP(eventsRR, eventsReq)
	if eventsRR.Code != http.StatusOK {
		t.Fatalf("events status=%d body=%s", eventsRR.Code, eventsRR.Body.String())
	}
	eventsBody := decodeResponseBody(t, eventsRR)
	eventsData, _ := eventsBody["data"].(map[string]any)
	if count := int(eventsData["count"].(float64)); count < 3 {
		t.Fatalf("event count=%d want at least 3", count)
	}
}

func TestV2RunsCreateRequiresPrompt(t *testing.T) {
	cfg := orchestrationTestConfig(t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/v2/runs", strings.NewReader(`{"run_id":"run-new-1"}`))
	rr := httptest.NewRecorder()
	V2RunsHandler(cfg, zerolog.Nop()).ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	body := decodeResponseBody(t, rr)
	if got := strings.TrimSpace(body["command"].(string)); got != commandV2RunsCreate {
		t.Fatalf("command=%q want %q", got, commandV2RunsCreate)
	}
}

func TestV2RunKillAppendsFailureForRunningRun(t *testing.T) {
	t.Setenv("FOXCTL_DB_DRIVER", "sqlite")
	t.Setenv("FOXCTL_V2_EVENTS_DB_DRIVER", "sqlite")
	t.Setenv("FOXCTL_V2_PROJECTIONS_DB_DRIVER", "sqlite")

	cfg := orchestrationTestConfig(t.TempDir())
	ctx := context.Background()
	projectionStore, closeFn, err := libsqlprojections.Open(ctx, cfg.Storage.Root)
	if err != nil {
		t.Fatalf("open projection store: %v", err)
	}
	defer func() { _ = closeFn() }()
	if err := projectionStore.Apply(ctx, coreevents.Event{
		ID:            "evt-kill-started",
		StreamID:      "run-kill-1",
		StreamType:    coreevents.StreamTypeRun,
		StreamVersion: 1,
		EventType:     coreevents.EventRunStarted,
		Command:       "run",
		RequestID:     "req-kill-start",
		ActorID:       "actor:killer",
	}); err != nil {
		t.Fatalf("apply started projection: %v", err)
	}

	killReq := httptest.NewRequest(http.MethodPost, "/api/v2/runs/run-kill-1/kill", strings.NewReader(`{"request_id":"req-kill-1"}`))
	killRR := httptest.NewRecorder()
	V2RunDetailHandler(cfg, zerolog.Nop(), nil).ServeHTTP(killRR, killReq)
	if killRR.Code != http.StatusOK {
		t.Fatalf("kill status=%d body=%s", killRR.Code, killRR.Body.String())
	}
	killBody := decodeResponseBody(t, killRR)
	if got := strings.TrimSpace(killBody["command"].(string)); got != commandV2RunKill {
		t.Fatalf("kill command=%q want %q", got, commandV2RunKill)
	}
	data, _ := killBody["data"].(map[string]any)
	if got := strings.TrimSpace(data["status"].(string)); got != "killed" {
		t.Fatalf("kill status=%q want killed", got)
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v2/runs/run-kill-1", nil)
	getRR := httptest.NewRecorder()
	V2RunDetailHandler(cfg, zerolog.Nop(), nil).ServeHTTP(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRR.Code, getRR.Body.String())
	}
	getBody := decodeResponseBody(t, getRR)
	state, _ := getBody["data"].(map[string]any)
	if got := strings.TrimSpace(state["status"].(string)); got != "failed" {
		t.Fatalf("projected status=%q want failed", got)
	}
	if got := strings.TrimSpace(state["request_id"].(string)); got != "req-kill-1" {
		t.Fatalf("projected request_id=%q want req-kill-1", got)
	}
}

type testV2RunModel struct{}

func (testV2RunModel) Complete(_ context.Context, in runner.ModelInput) (runner.ModelResponse, error) {
	return runner.ModelResponse{
		Message: "model response for " + strings.TrimSpace(in.Prompt),
		Done:    true,
	}, nil
}
