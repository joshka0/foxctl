package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
	libsqlevents "github.com/joshka0/foxctl/internal/v2/adapters/libsql/events"
	libsqlprojections "github.com/joshka0/foxctl/internal/v2/adapters/libsql/projections"
	v2llm "github.com/joshka0/foxctl/internal/v2/adapters/llm"
	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	corekill "github.com/joshka0/foxctl/internal/v2/core/kill"
	corelist "github.com/joshka0/foxctl/internal/v2/core/list"
	corerun "github.com/joshka0/foxctl/internal/v2/core/run"
	coretool "github.com/joshka0/foxctl/internal/v2/core/tool"
	runtimeevents "github.com/joshka0/foxctl/internal/v2/runtime/events"
	"github.com/joshka0/foxctl/internal/v2/runtime/profiles"
	"github.com/joshka0/foxctl/internal/v2/runtime/runner"
	v2services "github.com/joshka0/foxctl/internal/v2/services"
)

const (
	commandV2RunsList   = "v2/runs/list"
	commandV2RunsCreate = "v2/runs/create"
	commandV2RunGet     = "v2/runs/get"
	commandV2RunEvents  = "v2/runs/events"
	commandV2Transcript = "v2/runs/transcript"
	commandV2RunKill    = "v2/runs/kill"
	commandV2Events     = "v2/events"
	commandV2EventsLive = "v2/events/stream"
	commandV2Model      = "v2/model"
)

var newV2RunModel = func(cfg config.Config, workspaceRoot string) (runner.Model, error) {
	return v2llm.NewChatModel(v2llm.ChatModelConfig{
		LLM:       cfg.LLM,
		Workspace: workspaceRoot,
	})
}

var (
	activeV2Runs      = newV2RunRegistry()
	v2RuntimeEventBus = runtimeevents.NewBus(runtimeevents.Config{
		SubscriberBuffer: 128,
		OverflowPolicy:   runtimeevents.OverflowDropOldest,
	})
)

type v2RunRegistry struct {
	mu      sync.Mutex
	cancels map[string]v2RunCancel
}

type v2RunCancel struct {
	token  string
	cancel context.CancelFunc
}

func newV2RunRegistry() *v2RunRegistry {
	return &v2RunRegistry{cancels: map[string]v2RunCancel{}}
}

func (r *v2RunRegistry) Register(runID string, cancel context.CancelFunc) func() {
	runID = strings.TrimSpace(runID)
	if r == nil || runID == "" || cancel == nil {
		return func() {}
	}
	token := ulid.Make().String()
	r.mu.Lock()
	r.cancels[runID] = v2RunCancel{token: token, cancel: cancel}
	r.mu.Unlock()
	return func() {
		r.mu.Lock()
		if current := r.cancels[runID]; current.token == token {
			delete(r.cancels, runID)
		}
		r.mu.Unlock()
	}
}

func (r *v2RunRegistry) Cancel(runID string) bool {
	runID = strings.TrimSpace(runID)
	if r == nil || runID == "" {
		return false
	}
	r.mu.Lock()
	entry := r.cancels[runID]
	r.mu.Unlock()
	if entry.cancel == nil {
		return false
	}
	entry.cancel()
	return true
}

// V2RunsHandler handles GET/POST /api/v2/runs.
func V2RunsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			handleV2RunsList(w, r, cfg)
		case http.MethodPost:
			handleV2RunsCreate(w, r, cfg, log)
		default:
			writeCommandError(w, http.StatusMethodNotAllowed, commandV2RunsList, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
		}
	}
}

// V2ModelHandler reports the model endpoint used by v2 run creation.
func V2ModelHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeCommandError(w, http.StatusMethodNotAllowed, commandV2Model, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}
		provider := strings.TrimSpace(r.URL.Query().Get("provider"))
		if provider == "" {
			provider = cfg.LLM.Provider
		}
		provider = strings.TrimSpace(provider)
		model := strings.TrimSpace(cfg.LLM.Model)
		baseURL := strings.TrimSpace(cfg.LLM.BaseURL)
		authMode := strings.TrimSpace(cfg.LLM.AuthMode)
		if provider != "" {
			model = cfg.LLM.ResolveModel(provider)
			baseURL = cfg.LLM.ResolveBaseURL(provider)
			authMode = cfg.LLM.ResolveAuthMode(provider)
		}
		writeCommandOK(w, http.StatusOK, commandV2Model, map[string]any{
			"provider":  provider,
			"model":     model,
			"base_url":  baseURL,
			"auth_mode": authMode,
		})
	}
}

// V2EventsHandler handles GET /api/v2/events.
func V2EventsHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeCommandError(w, http.StatusMethodNotAllowed, commandV2Events, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}
		streamID := strings.TrimSpace(r.URL.Query().Get("stream_id"))
		if streamID == "" {
			writeCommandError(w, http.StatusBadRequest, commandV2Events, "EARG", "stream_id is required", map[string]any{
				"field": "stream_id",
				"hint":  httpErrorHint(http.StatusBadRequest),
			})
			return
		}
		streamType := coreevents.StreamType(strings.TrimSpace(r.URL.Query().Get("stream_type")))
		if streamType == "" {
			streamType = coreevents.StreamTypeRun
		}
		switch streamType {
		case coreevents.StreamTypeRun, coreevents.StreamTypeAgent, coreevents.StreamTypeTurn:
		default:
			writeCommandError(w, http.StatusBadRequest, commandV2Events, "EARG", "unsupported stream_type", map[string]any{
				"field": "stream_type",
				"value": string(streamType),
				"hint":  httpErrorHint(http.StatusBadRequest),
			})
			return
		}
		afterVersion, ok := parseNonNegativeInt64Query(w, commandV2Events, r, "after_version")
		if !ok {
			return
		}
		limit, ok := parsePositiveIntQuery(w, commandV2Events, r, "limit")
		if !ok {
			return
		}

		events, err := listV2Events(r, cfg, streamID, streamType, afterVersion, limit)
		if err != nil {
			writeV2RuntimeServiceError(w, commandV2Events, err)
			return
		}
		writeCommandOK(w, http.StatusOK, commandV2Events, map[string]any{
			"stream_id":     streamID,
			"stream_type":   streamType,
			"after_version": afterVersion,
			"count":         len(events),
			"events":        events,
		})
	}
}

// V2EventsStreamHandler handles GET /api/v2/events/stream.
func V2EventsStreamHandler(cfg config.Config, _ zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeCommandError(w, http.StatusMethodNotAllowed, commandV2EventsLive, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}
		query, ok := parseV2EventStreamQuery(w, r)
		if !ok {
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			writeCommandError(w, http.StatusInternalServerError, commandV2EventsLive, "ERUNTIME", "streaming unsupported", map[string]any{
				"hint": httpErrorHint(http.StatusInternalServerError),
			})
			return
		}

		eventStore, err := libsqlevents.Open(r.Context(), cfg.Storage.Root)
		if err != nil {
			writeCommandError(w, http.StatusServiceUnavailable, commandV2EventsLive, "ERUNTIME", "v2 event store is unavailable", map[string]any{
				"hint": httpErrorHint(http.StatusServiceUnavailable),
			})
			return
		}
		defer func() { _ = eventStore.Close() }()

		liveEvents, unsubscribe := v2RuntimeEventBus.Subscribe(query.buffer)
		defer unsubscribe()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")

		lastVersion := query.afterVersion
		writeSSEJSON(w, "v2.connected", map[string]any{
			"stream_id":     query.streamID,
			"stream_type":   query.streamType,
			"after_version": query.afterVersion,
		})
		flusher.Flush()

		replayed, err := eventStore.ListStream(r.Context(), coreevents.StreamFilter{
			StreamID:     query.streamID,
			StreamType:   query.streamType,
			AfterVersion: query.afterVersion,
			Limit:        query.limit,
		})
		if err != nil {
			writeSSEJSON(w, "v2.error", map[string]any{
				"message": "list persisted stream events",
				"error":   err.Error(),
			})
			flusher.Flush()
			return
		}
		for _, evt := range replayed {
			if evt.StreamVersion > lastVersion {
				lastVersion = evt.StreamVersion
			}
			writeSSEJSON(w, "v2.event", evt)
		}
		writeSSEJSON(w, "v2.replay_complete", map[string]any{
			"stream_id":           query.streamID,
			"stream_type":         query.streamType,
			"last_stream_version": lastVersion,
			"replayed":            len(replayed),
		})
		flusher.Flush()

		heartbeat := time.NewTicker(query.heartbeat)
		defer heartbeat.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-heartbeat.C:
				writeSSEJSON(w, "heartbeat", map[string]any{
					"ts": time.Now().UTC().Format(time.RFC3339Nano),
				})
				flusher.Flush()
			case evt, ok := <-liveEvents:
				if !ok {
					return
				}
				if !matchesV2EventStream(evt, query.streamID, query.streamType) || evt.StreamVersion <= lastVersion {
					continue
				}
				lastVersion = evt.StreamVersion
				writeSSEJSON(w, "v2.event", evt)
				flusher.Flush()
			}
		}
	}
}

// V2RunDetailHandler handles /api/v2/runs/{run_id} subroutes.
func V2RunDetailHandler(cfg config.Config, _ zerolog.Logger, _ OrchestrationRuntimeHost) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v2/runs/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
			writeCommandError(w, http.StatusBadRequest, commandV2RunGet, "EARG", "run_id is required", map[string]any{
				"field": "run_id",
				"hint":  httpErrorHint(http.StatusBadRequest),
			})
			return
		}
		runID := strings.TrimSpace(parts[0])
		switch {
		case len(parts) == 1:
			handleV2RunGet(w, r, cfg, runID)
		case len(parts) == 2 && parts[1] == "events":
			handleV2RunEvents(w, r, cfg, runID)
		case len(parts) == 2 && parts[1] == "transcript":
			handleV2RunTranscript(w, r, cfg, runID)
		case len(parts) == 2 && parts[1] == "kill":
			handleV2RunKill(w, r, cfg, runID)
		default:
			writeCommandError(w, http.StatusNotFound, commandV2RunGet, "ENOTFOUND", "route not found", map[string]any{
				"hint": httpErrorHint(http.StatusNotFound),
			})
		}
	}
}

func handleV2RunsList(w http.ResponseWriter, r *http.Request, cfg config.Config) {
	limit, ok := parsePositiveIntQuery(w, commandV2RunsList, r, "limit")
	if !ok {
		return
	}

	store, closeFn, err := libsqlprojections.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		writeCommandError(w, http.StatusServiceUnavailable, commandV2RunsList, "ERUNTIME", "v2 run projection store is unavailable", map[string]any{
			"hint": httpErrorHint(http.StatusServiceUnavailable),
		})
		return
	}
	defer func() { _ = closeFn() }()

	svc := v2services.NewListService(libsqlprojections.NewServiceAdapter(store))
	resp, err := svc.List(r.Context(), corelist.Request{
		Limit:   limit,
		Status:  strings.TrimSpace(r.URL.Query().Get("status")),
		Command: strings.TrimSpace(r.URL.Query().Get("command")),
		ActorID: strings.TrimSpace(r.URL.Query().Get("actor_id")),
	})
	if err != nil {
		writeV2RuntimeServiceError(w, commandV2RunsList, err)
		return
	}
	writeCommandOK(w, http.StatusOK, commandV2RunsList, resp)
}

func handleV2RunsCreate(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	if r.Method != http.MethodPost {
		writeCommandError(w, http.StatusMethodNotAllowed, commandV2RunsCreate, "EARG", "method not allowed", map[string]any{
			"hint": httpErrorHint(http.StatusMethodNotAllowed),
		})
		return
	}
	var req corerun.TurnInput
	if err := readJSON(w, r, &req); err != nil {
		writeCommandError(w, http.StatusBadRequest, commandV2RunsCreate, "EARG", "invalid json", map[string]any{
			"hint": httpErrorHint(http.StatusBadRequest),
		})
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		writeCommandError(w, http.StatusBadRequest, commandV2RunsCreate, "EARG", "prompt is required", map[string]any{
			"field": "prompt",
			"hint":  httpErrorHint(http.StatusBadRequest),
		})
		return
	}

	profile, ok := resolveV2RunProfile(w, r)
	if !ok {
		return
	}
	workspaceRoot := resolveV2RunWorkspaceRoot()
	model, err := newV2RunModel(cfg, workspaceRoot)
	if err != nil {
		writeCommandError(w, http.StatusServiceUnavailable, commandV2RunsCreate, "ERUNTIME", "v2 run model is unavailable", map[string]any{
			"hint": httpErrorHint(http.StatusServiceUnavailable),
		})
		return
	}

	req = normalizeV2RunCreateInput(req)
	exec, err := newWebV2RunExecution(r.Context(), cfg, profile, workspaceRoot, model)
	if err != nil {
		writeV2RunExecutionSetupError(w, err)
		return
	}

	if wantsAsyncV2Run(r) {
		runCtx, cancel := context.WithCancel(context.Background())
		unregister := activeV2Runs.Register(req.RunID, cancel)
		go func() {
			defer unregister()
			defer cancel()
			defer exec.Close()
			if _, err := exec.svc.Run(runCtx, req); err != nil {
				log.Warn().Err(err).Str("run_id", req.RunID).Msg("async v2 run failed")
			}
		}()
		writeCommandOK(w, http.StatusAccepted, commandV2RunsCreate, map[string]any{
			"run_id":         req.RunID,
			"turn_id":        req.TurnID,
			"request_id":     req.RequestID,
			"correlation_id": req.CorrelationID,
			"profile":        profile,
			"status":         "started",
			"async":          true,
		})
		return
	}
	defer exec.Close()
	runCtx, cancel := context.WithCancel(r.Context())
	defer cancel()
	unregister := activeV2Runs.Register(req.RunID, cancel)
	defer unregister()

	out, err := exec.svc.Run(runCtx, req)
	if err != nil {
		writeV2RuntimeServiceError(w, commandV2RunsCreate, err)
		return
	}
	writeCommandOK(w, http.StatusAccepted, commandV2RunsCreate, map[string]any{
		"run_id":         req.RunID,
		"turn_id":        out.TurnID,
		"request_id":     req.RequestID,
		"correlation_id": req.CorrelationID,
		"profile":        profile,
		"output":         out,
	})
}

func handleV2RunGet(w http.ResponseWriter, r *http.Request, cfg config.Config, runID string) {
	if r.Method != http.MethodGet {
		writeCommandError(w, http.StatusMethodNotAllowed, commandV2RunGet, "EARG", "method not allowed", map[string]any{
			"hint": httpErrorHint(http.StatusMethodNotAllowed),
		})
		return
	}

	store, closeFn, err := libsqlprojections.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		writeCommandError(w, http.StatusServiceUnavailable, commandV2RunGet, "ERUNTIME", "v2 run projection store is unavailable", map[string]any{
			"hint": httpErrorHint(http.StatusServiceUnavailable),
		})
		return
	}
	defer func() { _ = closeFn() }()

	state, err := libsqlprojections.NewServiceAdapter(store).GetRunState(r.Context(), runID)
	if err != nil {
		if errors.Is(err, coreevents.ErrNotFound) {
			writeV2RuntimeServiceError(w, commandV2RunGet, &v2errors.V2Error{
				Kind:    v2errors.ErrNotFound,
				Message: "run not found",
				Fatal:   true,
				Details: map[string]any{"run_id": runID},
			})
			return
		}
		writeV2RuntimeServiceError(w, commandV2RunGet, err)
		return
	}
	writeCommandOK(w, http.StatusOK, commandV2RunGet, map[string]any{
		"run_id":     state.RunID,
		"status":     state.Status,
		"command":    state.Command,
		"request_id": state.RequestID,
		"actor_id":   state.ActorID,
		"updated_at": state.UpdatedAt,
	})
}

func handleV2RunEvents(w http.ResponseWriter, r *http.Request, cfg config.Config, runID string) {
	if r.Method != http.MethodGet {
		writeCommandError(w, http.StatusMethodNotAllowed, commandV2RunEvents, "EARG", "method not allowed", map[string]any{
			"hint": httpErrorHint(http.StatusMethodNotAllowed),
		})
		return
	}

	afterVersion, ok := parseNonNegativeInt64Query(w, commandV2RunEvents, r, "after_version")
	if !ok {
		return
	}
	limit, ok := parsePositiveIntQuery(w, commandV2RunEvents, r, "limit")
	if !ok {
		return
	}

	events, err := listV2Events(r, cfg, runID, coreevents.StreamTypeRun, afterVersion, limit)
	if err != nil {
		writeV2RuntimeServiceError(w, commandV2RunEvents, err)
		return
	}

	writeCommandOK(w, http.StatusOK, commandV2RunEvents, map[string]any{
		"run_id":        runID,
		"after_version": afterVersion,
		"count":         len(events),
		"events":        events,
	})
}

type v2RunTranscriptItem struct {
	ID         string         `json:"id"`
	Role       string         `json:"role"`
	Kind       string         `json:"kind"`
	Title      string         `json:"title,omitempty"`
	Text       string         `json:"text,omitempty"`
	EventID    string         `json:"event_id"`
	EventType  string         `json:"event_type"`
	OccurredAt time.Time      `json:"occurred_at"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

func handleV2RunTranscript(w http.ResponseWriter, r *http.Request, cfg config.Config, runID string) {
	if r.Method != http.MethodGet {
		writeCommandError(w, http.StatusMethodNotAllowed, commandV2Transcript, "EARG", "method not allowed", map[string]any{
			"hint": httpErrorHint(http.StatusMethodNotAllowed),
		})
		return
	}
	events, err := listV2Events(r, cfg, runID, coreevents.StreamTypeRun, 0, 0)
	if err != nil {
		writeV2RuntimeServiceError(w, commandV2Transcript, err)
		return
	}
	items := make([]v2RunTranscriptItem, 0, len(events))
	for _, evt := range events {
		items = append(items, transcriptItemsForEvent(evt)...)
	}
	writeCommandOK(w, http.StatusOK, commandV2Transcript, map[string]any{
		"run_id": runID,
		"count":  len(items),
		"items":  items,
	})
}

func transcriptItemsForEvent(evt coreevents.Event) []v2RunTranscriptItem {
	payload := eventPayloadMap(evt.Payload)
	base := v2RunTranscriptItem{
		EventID:    evt.ID,
		EventType:  string(evt.EventType),
		OccurredAt: evt.OccurredAt,
	}
	withID := func(item v2RunTranscriptItem, suffix string) v2RunTranscriptItem {
		item.ID = fmt.Sprintf("%s:%s", evt.ID, suffix)
		item.EventID = base.EventID
		item.EventType = base.EventType
		item.OccurredAt = base.OccurredAt
		return item
	}

	switch evt.EventType {
	case coreevents.EventRunStarted:
		prompt := stringPayloadField(payload, "prompt")
		if prompt != "" {
			return []v2RunTranscriptItem{withID(v2RunTranscriptItem{
				Role:  "user",
				Kind:  "prompt",
				Title: "prompt",
				Text:  prompt,
				Metadata: map[string]any{
					"mode": stringPayloadField(payload, "mode"),
				},
			}, "prompt")}
		}
		return []v2RunTranscriptItem{withID(v2RunTranscriptItem{
			Role:  "system",
			Kind:  "status",
			Title: "run started",
			Text:  stringPayloadField(payload, "mode"),
		}, "started")}
	case coreevents.EventToolInvoked:
		return []v2RunTranscriptItem{withID(v2RunTranscriptItem{
			Role:  "tool",
			Kind:  "tool_call",
			Title: stringPayloadField(payload, "name"),
			Text:  "invoked",
			Metadata: map[string]any{
				"iteration_index": payload["iteration_index"],
			},
		}, "tool-call")}
	case coreevents.EventToolResponded:
		return []v2RunTranscriptItem{withID(v2RunTranscriptItem{
			Role:  "tool",
			Kind:  "tool_result",
			Title: stringPayloadField(payload, "name"),
			Text:  stringPayloadField(payload, "status"),
		}, "tool-result")}
	case coreevents.EventTurnRecorded:
		return []v2RunTranscriptItem{withID(v2RunTranscriptItem{
			Role:  "system",
			Kind:  "turn",
			Title: stringPayloadField(payload, "turn_id"),
			Text:  "turn recorded",
			Metadata: map[string]any{
				"iterations": payload["iterations"],
				"tool_calls": payload["tool_calls"],
			},
		}, "turn")}
	case coreevents.EventRunCompleted:
		return []v2RunTranscriptItem{withID(v2RunTranscriptItem{
			Role:  "assistant",
			Kind:  "message",
			Title: "assistant",
			Text:  stringPayloadField(payload, "summary"),
		}, "assistant")}
	case coreevents.EventRunFailed, coreevents.EventStageFailed, coreevents.EventArtifactFailed:
		return []v2RunTranscriptItem{withID(v2RunTranscriptItem{
			Role:  "system",
			Kind:  "error",
			Title: stringPayloadField(payload, "kind"),
			Text:  stringPayloadField(payload, "message"),
			Metadata: map[string]any{
				"fatal":     payload["fatal"],
				"retryable": payload["retryable"],
				"stage":     nestedPayloadField(payload, "details", "stage"),
			},
		}, "error")}
	default:
		return nil
	}
}

func eventPayloadMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func stringPayloadField(payload map[string]any, field string) string {
	value, ok := payload[field]
	if !ok {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func nestedPayloadField(payload map[string]any, objectField string, field string) any {
	value, ok := payload[objectField].(map[string]any)
	if !ok {
		return nil
	}
	return value[field]
}

type projectingV2EventStore struct {
	eventStore      coreevents.Appender
	projectionStore interface {
		Apply(context.Context, coreevents.Event) error
	}
	eventBus runner.EventPublisher
}

func (s projectingV2EventStore) Append(ctx context.Context, evt coreevents.Event) error {
	if s.eventStore == nil {
		return fmt.Errorf("v2 projecting event store: nil event store")
	}
	evt = s.withNextVersion(ctx, evt)
	if err := s.eventStore.Append(ctx, evt); err != nil {
		return err
	}
	if s.projectionStore == nil {
		return fmt.Errorf("v2 projecting event store: nil projection store")
	}
	if err := s.projectionStore.Apply(ctx, evt); err != nil {
		return fmt.Errorf("apply v2 projection: %w", err)
	}
	if s.eventBus != nil {
		_ = s.eventBus.Publish(ctx, evt.Clone())
	}
	return nil
}

func (s projectingV2EventStore) withNextVersion(ctx context.Context, evt coreevents.Event) coreevents.Event {
	if evt.StreamVersion != 0 && evt.Sequence != 0 {
		return evt
	}
	reader, ok := s.eventStore.(interface {
		ListStream(context.Context, coreevents.StreamFilter) ([]coreevents.Event, error)
	})
	if !ok || strings.TrimSpace(evt.StreamID) == "" || strings.TrimSpace(string(evt.StreamType)) == "" {
		return evt
	}
	existing, err := reader.ListStream(ctx, coreevents.StreamFilter{
		StreamID:   evt.StreamID,
		StreamType: evt.StreamType,
		Limit:      1_000_000,
	})
	if err != nil || len(existing) == 0 {
		if evt.StreamVersion == 0 {
			evt.StreamVersion = 1
		}
		if evt.Sequence == 0 {
			evt.Sequence = 1
		}
		return evt
	}
	last := existing[len(existing)-1]
	if evt.StreamVersion == 0 {
		evt.StreamVersion = last.StreamVersion + 1
	}
	if evt.Sequence == 0 {
		evt.Sequence = last.Sequence + 1
	}
	return evt
}

type webV2RunKiller struct {
	registry   *v2RunRegistry
	eventStore coreevents.Appender
	requestID  string
	actorID    string
}

func (k webV2RunKiller) Kill(ctx context.Context, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return coreevents.ErrNotFound
	}
	if k.registry != nil && k.registry.Cancel(runID) {
		return nil
	}
	if k.eventStore == nil {
		return fmt.Errorf("v2 run killer: nil event store")
	}
	payload, err := coreevents.MarshalPayload(coreevents.ErrorPayload{
		Kind:         "killed",
		Message:      "run killed",
		Fatal:        true,
		HTTPStatus:   http.StatusOK,
		EnvelopeCode: "EKILLED",
		Details: map[string]any{
			"run_id": runID,
		},
	})
	if err != nil {
		return err
	}
	return k.eventStore.Append(ctx, coreevents.Event{
		ID:         "evt-" + ulid.Make().String(),
		StreamID:   runID,
		StreamType: coreevents.StreamTypeRun,
		EventType:  coreevents.EventRunFailed,
		OccurredAt: time.Now().UTC(),
		RequestID:  strings.TrimSpace(k.requestID),
		ActorID:    strings.TrimSpace(k.actorID),
		Command:    "kill",
		Payload:    payload,
	})
}

func normalizeV2RunCreateInput(req corerun.TurnInput) corerun.TurnInput {
	req.RunID = normalizePrefixedULID(req.RunID, "run")
	req.TurnID = normalizePrefixedULID(req.TurnID, "turn")
	req.RequestID = normalizePrefixedULID(req.RequestID, "req")
	if strings.TrimSpace(req.CorrelationID) == "" {
		req.CorrelationID = req.RequestID
	}
	if strings.TrimSpace(req.CausationID) == "" {
		req.CausationID = req.RequestID
	}
	if strings.TrimSpace(req.Command) == "" {
		req.Command = "run"
	}
	if strings.TrimSpace(req.Mode) == "" {
		req.Mode = "reactive"
	}
	if strings.TrimSpace(req.ActorID) == "" {
		req.ActorID = "actor:web"
	}
	return req
}

type webV2RunExecution struct {
	svc             *v2services.RunService
	eventStore      *libsqlevents.Store
	closeProjection func() error
}

func newWebV2RunExecution(ctx context.Context, cfg config.Config, profile coretool.ProcessProfile, workspaceRoot string, model runner.Model) (*webV2RunExecution, error) {
	eventStore, err := libsqlevents.Open(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, &v2errors.V2Error{
			Kind:      v2errors.ErrDependency,
			Message:   "v2 event store is unavailable",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
		}
	}

	projectionStore, closeProjection, err := libsqlprojections.Open(ctx, cfg.Storage.Root)
	if err != nil {
		_ = eventStore.Close()
		return nil, &v2errors.V2Error{
			Kind:      v2errors.ErrDependency,
			Message:   "v2 run projection store is unavailable",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
		}
	}

	svc, err := v2services.NewDefaultRunService(v2services.DefaultRuntimeDependencies{
		Profile:           profile,
		AppConfig:         cfg,
		WorkspaceRoot:     workspaceRoot,
		IncludeExtensions: true,
		EventStore: projectingV2EventStore{
			eventStore:      eventStore,
			projectionStore: projectionStore,
			eventBus:        v2RuntimeEventBus,
		},
		Model: model,
		NewID: func() string {
			return "evt-" + ulid.Make().String()
		},
	})
	if err != nil {
		_ = closeProjection()
		_ = eventStore.Close()
		return nil, &v2errors.V2Error{
			Kind:      v2errors.ErrDependency,
			Message:   "v2 run service is unavailable",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
		}
	}

	return &webV2RunExecution{
		svc:             svc,
		eventStore:      eventStore,
		closeProjection: closeProjection,
	}, nil
}

func (e *webV2RunExecution) Close() {
	if e == nil {
		return
	}
	if e.closeProjection != nil {
		_ = e.closeProjection()
	}
	if e.eventStore != nil {
		_ = e.eventStore.Close()
	}
}

func writeV2RunExecutionSetupError(w http.ResponseWriter, err error) {
	var verr *v2errors.V2Error
	if errors.As(err, &verr) {
		writeV2RuntimeServiceError(w, commandV2RunsCreate, verr)
		return
	}
	writeV2RuntimeServiceError(w, commandV2RunsCreate, &v2errors.V2Error{
		Kind:      v2errors.ErrDependency,
		Message:   "v2 run service is unavailable",
		Cause:     err,
		Fatal:     true,
		Retryable: true,
	})
}

func wantsAsyncV2Run(r *http.Request) bool {
	return truthyQueryValue(r.URL.Query().Get("async"))
}

func truthyQueryValue(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func normalizePrefixedULID(value, prefix string) string {
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		return trimmed
	}
	return prefix + "-" + ulid.Make().String()
}

type v2EventStreamQuery struct {
	streamID     string
	streamType   coreevents.StreamType
	afterVersion int64
	limit        int
	buffer       int
	heartbeat    time.Duration
}

func parseV2EventStreamQuery(w http.ResponseWriter, r *http.Request) (v2EventStreamQuery, bool) {
	streamID := strings.TrimSpace(r.URL.Query().Get("stream_id"))
	if streamID == "" {
		writeCommandError(w, http.StatusBadRequest, commandV2EventsLive, "EARG", "stream_id is required", map[string]any{
			"field": "stream_id",
			"hint":  httpErrorHint(http.StatusBadRequest),
		})
		return v2EventStreamQuery{}, false
	}
	streamType := coreevents.StreamType(strings.TrimSpace(r.URL.Query().Get("stream_type")))
	if streamType == "" {
		streamType = coreevents.StreamTypeRun
	}
	if !isSupportedV2StreamType(streamType) {
		writeCommandError(w, http.StatusBadRequest, commandV2EventsLive, "EARG", "unsupported stream_type", map[string]any{
			"field": "stream_type",
			"value": string(streamType),
			"hint":  httpErrorHint(http.StatusBadRequest),
		})
		return v2EventStreamQuery{}, false
	}
	afterVersion, ok := parseNonNegativeInt64Query(w, commandV2EventsLive, r, "after_version")
	if !ok {
		return v2EventStreamQuery{}, false
	}
	limit, ok := parsePositiveIntQuery(w, commandV2EventsLive, r, "limit")
	if !ok {
		return v2EventStreamQuery{}, false
	}
	buffer, ok := parsePositiveIntQuery(w, commandV2EventsLive, r, "buffer")
	if !ok {
		return v2EventStreamQuery{}, false
	}
	if buffer <= 0 {
		buffer = runtimeevents.DefaultSubscriberBuffer
	}
	heartbeatMS, ok := parsePositiveIntQuery(w, commandV2EventsLive, r, "heartbeat_ms")
	if !ok {
		return v2EventStreamQuery{}, false
	}
	heartbeat := 15 * time.Second
	if heartbeatMS > 0 {
		heartbeat = time.Duration(heartbeatMS) * time.Millisecond
	}
	return v2EventStreamQuery{
		streamID:     streamID,
		streamType:   streamType,
		afterVersion: afterVersion,
		limit:        limit,
		buffer:       buffer,
		heartbeat:    heartbeat,
	}, true
}

func isSupportedV2StreamType(streamType coreevents.StreamType) bool {
	switch streamType {
	case coreevents.StreamTypeRun, coreevents.StreamTypeAgent, coreevents.StreamTypeTurn:
		return true
	default:
		return false
	}
}

func matchesV2EventStream(evt coreevents.Event, streamID string, streamType coreevents.StreamType) bool {
	return strings.TrimSpace(evt.StreamID) == strings.TrimSpace(streamID) && evt.StreamType == streamType
}

func resolveV2RunProfile(w http.ResponseWriter, r *http.Request) (coretool.ProcessProfile, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get("profile"))
	if raw == "" {
		return coretool.ProfileCompanion, true
	}
	profile, err := profiles.Resolve(raw)
	if err != nil {
		writeCommandError(w, http.StatusBadRequest, commandV2RunsCreate, "EARG", "unsupported profile", map[string]any{
			"field": "profile",
			"value": raw,
			"hint":  httpErrorHint(http.StatusBadRequest),
		})
		return "", false
	}
	return profile, true
}

func resolveV2RunWorkspaceRoot() string {
	wd, err := os.Getwd()
	if err != nil || strings.TrimSpace(wd) == "" {
		return "."
	}
	return wd
}

func listV2Events(r *http.Request, cfg config.Config, streamID string, streamType coreevents.StreamType, afterVersion int64, limit int) ([]coreevents.Event, error) {
	eventStore, err := libsqlevents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		return nil, &v2errors.V2Error{
			Kind:      v2errors.ErrDependency,
			Message:   "v2 event store is unavailable",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
		}
	}
	defer func() { _ = eventStore.Close() }()

	return eventStore.ListStream(r.Context(), coreevents.StreamFilter{
		StreamID:     streamID,
		StreamType:   streamType,
		AfterVersion: afterVersion,
		Limit:        limit,
	})
}

func handleV2RunKill(w http.ResponseWriter, r *http.Request, cfg config.Config, runID string) {
	if r.Method != http.MethodPost {
		writeCommandError(w, http.StatusMethodNotAllowed, commandV2RunKill, "EARG", "method not allowed", map[string]any{
			"hint": httpErrorHint(http.StatusMethodNotAllowed),
		})
		return
	}

	var req corekill.Request
	if r.ContentLength > 0 {
		if err := readJSON(w, r, &req); err != nil {
			writeCommandError(w, http.StatusBadRequest, commandV2RunKill, "EARG", "invalid json", map[string]any{
				"hint": httpErrorHint(http.StatusBadRequest),
			})
			return
		}
	}
	req.RunID = runID
	req.RequestID = normalizePrefixedULID(req.RequestID, "req")

	eventStore, err := libsqlevents.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		writeCommandError(w, http.StatusServiceUnavailable, commandV2RunKill, "ERUNTIME", "v2 event store is unavailable", map[string]any{
			"hint": httpErrorHint(http.StatusServiceUnavailable),
		})
		return
	}
	defer func() { _ = eventStore.Close() }()

	projectionStore, closeProjection, err := libsqlprojections.Open(r.Context(), cfg.Storage.Root)
	if err != nil {
		writeCommandError(w, http.StatusServiceUnavailable, commandV2RunKill, "ERUNTIME", "v2 run projection store is unavailable", map[string]any{
			"hint": httpErrorHint(http.StatusServiceUnavailable),
		})
		return
	}
	defer func() { _ = closeProjection() }()

	projectingStore := projectingV2EventStore{
		eventStore:      eventStore,
		projectionStore: projectionStore,
		eventBus:        v2RuntimeEventBus,
	}
	svc := v2services.NewKillService(v2services.KillDependencies{
		Killer: webV2RunKiller{
			registry:   activeV2Runs,
			eventStore: projectingStore,
			requestID:  req.RequestID,
			actorID:    "actor:web",
		},
		Projections: libsqlprojections.NewServiceAdapter(projectionStore),
	})
	resp, err := svc.Kill(r.Context(), req)
	if err != nil {
		writeV2RuntimeServiceError(w, commandV2RunKill, err)
		return
	}
	writeCommandOK(w, http.StatusOK, commandV2RunKill, resp)
}

func parsePositiveIntQuery(w http.ResponseWriter, command string, r *http.Request, key string) (int, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed < 0 {
		writeCommandError(w, http.StatusBadRequest, command, "EARG", key+" must be a non-negative integer", map[string]any{
			"field": key,
			"hint":  httpErrorHint(http.StatusBadRequest),
		})
		return 0, false
	}
	return parsed, true
}

func parseNonNegativeInt64Query(w http.ResponseWriter, command string, r *http.Request, key string) (int64, bool) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, true
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed < 0 {
		writeCommandError(w, http.StatusBadRequest, command, "EARG", key+" must be a non-negative integer", map[string]any{
			"field": key,
			"hint":  httpErrorHint(http.StatusBadRequest),
		})
		return 0, false
	}
	return parsed, true
}

func writeV2RuntimeServiceError(w http.ResponseWriter, command string, err error) {
	writeOrchestrationServiceError(w, command, err)
}
