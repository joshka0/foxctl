package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/context/contextplane/taskhistory"
	"github.com/joshka0/foxctl/internal/domain/envelope"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/observability"
	"github.com/joshka0/foxctl/internal/storage/cas"
	"github.com/joshka0/foxctl/internal/storage/dbdriver"
	v2jido "github.com/joshka0/foxctl/internal/v2/adapters/jido"
	libsqlevents "github.com/joshka0/foxctl/internal/v2/adapters/libsql/events"
	libsqlorchestration "github.com/joshka0/foxctl/internal/v2/adapters/libsql/orchestration"
	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	coreevents "github.com/joshka0/foxctl/internal/v2/core/events"
	coreorchestration "github.com/joshka0/foxctl/internal/v2/core/orchestration"
	corespawn "github.com/joshka0/foxctl/internal/v2/core/spawn"
	coreworker "github.com/joshka0/foxctl/internal/v2/core/worker"
	v2services "github.com/joshka0/foxctl/internal/v2/services"
)

const (
	commandOrchestrationDispatchIssue       = "orchestration/dispatch-issue"
	commandOrchestrationCardAction          = "orchestration/card-action"
	commandOrchestrationBoardGet            = "orchestration/board-get"
	commandOrchestrationBoardCardGet        = "orchestration/board-card-get"
	commandOrchestrationBoardCardRuntimeGet = "orchestration/board-card-runtime-get"
	commandOrchestrationRefresh             = "orchestration/refresh"
	commandOrchestrationSeedCards           = "orchestration/seed-cards"
	commandOrchestrationCleanupCards        = "orchestration/cleanup-cards"
	commandOrchestrationArchiveCards        = "orchestration/archive-cards"
	commandOrchestrationRestoreCards        = "orchestration/restore-cards"
	opWebOrchestrationDispatchIssue         = "web.orchestration.dispatch_issue"
	opWebOrchestrationCardAction            = "web.orchestration.card_action"
	opWebOrchestrationBoardGet              = "web.orchestration.board_get"
	opWebOrchestrationBoardCardGet          = "web.orchestration.board_card_get"
	opWebOrchestrationBoardCardRuntimeGet   = "web.orchestration.board_card_runtime_get"
	opWebOrchestrationRefresh               = "web.orchestration.refresh"
	opWebOrchestrationSeedCards             = "web.orchestration.seed_cards"
	opWebOrchestrationCleanupCards          = "web.orchestration.cleanup_cards"
	opWebOrchestrationArchiveCards          = "web.orchestration.archive_cards"
	opWebOrchestrationRestoreCards          = "web.orchestration.restore_cards"

	orchestrationLargePayloadThreshold = 64 * 1024
	refreshQueueCoalesceWindow         = 5 * time.Second
	defaultRuntimeTreeDepth            = 2
	maxRuntimeTreeDepth                = 5
)

const (
	orchestrationActionRetryNow = "retry-now"
	orchestrationActionRelease  = "release"
	orchestrationActionMarkDone = "mark-done"
)

type orchestrationSeedCard struct {
	IssueID         string `json:"issue_id,omitempty"`
	IssueIdentifier string `json:"issue_identifier,omitempty"`
	Title           string `json:"title"`
	State           string `json:"state,omitempty"`
	TrackerState    string `json:"tracker_state,omitempty"`
	PolicyStatus    string `json:"policy_status,omitempty"`
	LastOutcome     string `json:"last_outcome,omitempty"`
	Eligibility     string `json:"eligibility,omitempty"`
}

type orchestrationSeedCardsRequest struct {
	RequestID   string                  `json:"request_id"`
	WorkspaceID string                  `json:"workspace_id,omitempty"`
	Cards       []orchestrationSeedCard `json:"cards"`
}

type orchestrationSeedCardsResponse struct {
	RequestID string    `json:"request_id"`
	Created   int       `json:"created"`
	Skipped   int       `json:"skipped,omitempty"`
	Timestamp time.Time `json:"ts"`
}

type orchestrationBoardCardData struct {
	Card    coreorchestration.Card        `json:"card"`
	Runtime *orchestrationCardRuntimeData `json:"runtime,omitempty"`
}

type orchestrationCardRuntimeData struct {
	Enabled  bool                       `json:"enabled"`
	AgentID  string                     `json:"agent_id,omitempty"`
	Status   string                     `json:"status,omitempty"`
	State    any                        `json:"state,omitempty"`
	Children map[string]v2jido.ChildRef `json:"children,omitempty"`
	Error    string                     `json:"error,omitempty"`
}

type orchestrationCardActionRequest struct {
	RequestID   string `json:"request_id"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	IssueID     string `json:"issue_id"`
	Action      string `json:"action"`
}

type orchestrationCardActionResponse struct {
	RequestID  string                 `json:"request_id"`
	Action     string                 `json:"action"`
	Card       coreorchestration.Card `json:"card"`
	Idempotent bool                   `json:"idempotent,omitempty"`
	Timestamp  time.Time              `json:"ts"`
}

type orchestrationCleanupCardsRequest struct {
	RequestID   string   `json:"request_id"`
	WorkspaceID string   `json:"workspace_id,omitempty"`
	IssueIDs    []string `json:"issue_ids,omitempty"`
}

type orchestrationCleanupCardsResponse struct {
	RequestID     string    `json:"request_id"`
	DeletedCards  int       `json:"deleted_cards"`
	DeletedEvents int       `json:"deleted_events"`
	Timestamp     time.Time `json:"ts"`
}

type orchestrationArchiveCardsRequest struct {
	RequestID   string   `json:"request_id"`
	WorkspaceID string   `json:"workspace_id,omitempty"`
	IssueIDs    []string `json:"issue_ids,omitempty"`
}

type orchestrationArchiveCardsResponse struct {
	RequestID string    `json:"request_id"`
	Updated   int       `json:"updated"`
	Action    string    `json:"action"`
	Timestamp time.Time `json:"ts"`
}

type orchestrationRuntimeTreeData struct {
	Enabled bool                          `json:"enabled"`
	AgentID string                        `json:"agent_id,omitempty"`
	Depth   int                           `json:"depth"`
	Root    *orchestrationRuntimeTreeNode `json:"root,omitempty"`
	Error   string                        `json:"error,omitempty"`
}

type orchestrationRuntimeTreeNode struct {
	Tag      string                          `json:"tag,omitempty"`
	AgentID  string                          `json:"agent_id,omitempty"`
	PID      string                          `json:"pid,omitempty"`
	Metadata map[string]any                  `json:"metadata,omitempty"`
	Status   string                          `json:"status,omitempty"`
	State    any                             `json:"state,omitempty"`
	Error    string                          `json:"error,omitempty"`
	Children []*orchestrationRuntimeTreeNode `json:"children,omitempty"`
}

type orchestrationBoardCardRuntimeData struct {
	Card    coreorchestration.Card        `json:"card"`
	Runtime *orchestrationRuntimeTreeData `json:"runtime,omitempty"`
}

// OrchestrationBoardGetHandler handles GET /api/orchestration/board-get.
func OrchestrationBoardGetHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	svc := newOrchestrationCommandService(cfg, log, nil)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeCommandError(w, http.StatusMethodNotAllowed, commandOrchestrationBoardGet, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}

		limit := 0
		if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
			parsed, err := strconv.Atoi(rawLimit)
			if err != nil {
				writeCommandError(w, http.StatusBadRequest, commandOrchestrationBoardGet, "EARG", "limit must be an integer", map[string]any{
					"field": "limit",
					"hint":  httpErrorHint(http.StatusBadRequest),
				})
				return
			}
			limit = parsed
		}

		started := time.Now()
		requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
		workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
		laneFilter := strings.TrimSpace(r.URL.Query().Get("lane"))
		event := observability.NewEvent(opWebOrchestrationBoardGet).
			WithComponent(observability.ComponentWeb).
			WithCommand(commandOrchestrationBoardGet).
			WithWorkspace(workspaceID).
			WithData("limit", limit).
			WithData("request_id", requestID).
			WithData("lane_filter", laneFilter).
			EnrichFromContext(r.Context())

		resp, err := svc.Board(r.Context(), coreorchestration.BoardRequest{
			RequestID:    requestID,
			WorkspaceID:  workspaceID,
			Limit:        limit,
			Cursor:       strings.TrimSpace(r.URL.Query().Get("cursor")),
			Lane:         coreorchestration.Lane(laneFilter),
			ArchivedOnly: parseBool(r.URL.Query().Get("archived_only")),
		})
		if err != nil {
			observability.Emit(r.Context(), event.Error(err, time.Since(started)))
			writeOrchestrationServiceError(w, commandOrchestrationBoardGet, err)
			return
		}

		data, artifactized, err := maybeArtifactizeBoard(r.Context(), cfg, resp)
		if err != nil {
			observability.Emit(r.Context(), event.Error(err, time.Since(started)))
			log.Error().Err(err).Msg("failed to artifactize orchestration board payload")
			writeCommandError(w, http.StatusInternalServerError, commandOrchestrationBoardGet, "ERUNTIME", "failed to persist board artifact", map[string]any{
				"hint": httpErrorHint(http.StatusInternalServerError),
			})
			return
		}
		event.WithData("card_count", countBoardCards(resp.Lanes)).
			WithData("artifactized", artifactized)
		observability.Emit(r.Context(), event.Success(time.Since(started)))

		if artifactized {
			log.Info().
				Str("command", commandOrchestrationBoardGet).
				Int("cards", countBoardCards(resp.Lanes)).
				Msg("orchestration board payload stored as artifact")
		}
		writeCommandOK(w, http.StatusOK, commandOrchestrationBoardGet, data)
	}
}

// OrchestrationBoardCardGetHandler handles GET /api/orchestration/board-card-get.
func OrchestrationBoardCardGetHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	svc := newOrchestrationCommandService(cfg, log, nil)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeCommandError(w, http.StatusMethodNotAllowed, commandOrchestrationBoardCardGet, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}

		started := time.Now()
		requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
		workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
		issueID := strings.TrimSpace(r.URL.Query().Get("issue_id"))
		includeRuntime := parseBool(r.URL.Query().Get("include_runtime"))
		event := observability.NewEvent(opWebOrchestrationBoardCardGet).
			WithComponent(observability.ComponentWeb).
			WithCommand(commandOrchestrationBoardCardGet).
			WithWorkspace(workspaceID).
			WithData("request_id", requestID).
			WithData("issue_id", issueID).
			WithData("include_runtime", includeRuntime).
			EnrichFromContext(r.Context())

		resp, err := svc.Card(r.Context(), coreorchestration.CardRequest{
			RequestID:   requestID,
			WorkspaceID: workspaceID,
			IssueID:     issueID,
		})
		if err != nil {
			observability.Emit(r.Context(), event.Error(err, time.Since(started)))
			writeOrchestrationServiceError(w, commandOrchestrationBoardCardGet, err)
			return
		}
		data := orchestrationBoardCardData{Card: resp.Card}
		if includeRuntime {
			data.Runtime = loadOrchestrationCardRuntime(r.Context(), cfg, log, resp.Card)
			if data.Runtime != nil {
				event.WithData("runtime_enabled", data.Runtime.Enabled).
					WithData("runtime_agent_id", data.Runtime.AgentID).
					WithData("runtime_status", data.Runtime.Status)
				if strings.TrimSpace(data.Runtime.Error) != "" {
					event.WithData("runtime_error", data.Runtime.Error)
				}
			}
		}
		event.WithData("issue_identifier", resp.Card.IssueIdentifier).
			WithData("lane", strings.TrimSpace(string(resp.Card.Lane))).
			WithData("last_outcome", strings.TrimSpace(string(resp.Card.LastOutcome))).
			WithData("policy_status", strings.TrimSpace(string(resp.Card.PolicyStatus))).
			WithData("eligibility", strings.TrimSpace(string(resp.Card.Eligibility)))
		observability.Emit(r.Context(), event.Success(time.Since(started)))

		writeCommandOK(w, http.StatusOK, commandOrchestrationBoardCardGet, data)
	}
}

// OrchestrationBoardCardRuntimeGetHandler handles GET /api/orchestration/board-card-runtime-get.
func OrchestrationBoardCardRuntimeGetHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	svc := newOrchestrationCommandService(cfg, log, nil)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeCommandError(w, http.StatusMethodNotAllowed, commandOrchestrationBoardCardRuntimeGet, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}

		depth := defaultRuntimeTreeDepth
		if rawDepth := strings.TrimSpace(r.URL.Query().Get("depth")); rawDepth != "" {
			parsed, err := strconv.Atoi(rawDepth)
			if err != nil || parsed < 0 {
				writeCommandError(w, http.StatusBadRequest, commandOrchestrationBoardCardRuntimeGet, "EARG", "depth must be a non-negative integer", map[string]any{
					"field": "depth",
					"hint":  httpErrorHint(http.StatusBadRequest),
				})
				return
			}
			depth = parsed
		}
		if depth > maxRuntimeTreeDepth {
			depth = maxRuntimeTreeDepth
		}

		started := time.Now()
		requestID := strings.TrimSpace(r.URL.Query().Get("request_id"))
		workspaceID := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
		issueID := strings.TrimSpace(r.URL.Query().Get("issue_id"))
		event := observability.NewEvent(opWebOrchestrationBoardCardRuntimeGet).
			WithComponent(observability.ComponentWeb).
			WithCommand(commandOrchestrationBoardCardRuntimeGet).
			WithWorkspace(workspaceID).
			WithData("request_id", requestID).
			WithData("issue_id", issueID).
			WithData("depth", depth).
			EnrichFromContext(r.Context())

		resp, err := svc.Card(r.Context(), coreorchestration.CardRequest{
			RequestID:   requestID,
			WorkspaceID: workspaceID,
			IssueID:     issueID,
		})
		if err != nil {
			observability.Emit(r.Context(), event.Error(err, time.Since(started)))
			writeOrchestrationServiceError(w, commandOrchestrationBoardCardRuntimeGet, err)
			return
		}

		data := orchestrationBoardCardRuntimeData{
			Card:    resp.Card,
			Runtime: loadOrchestrationCardRuntimeTree(r.Context(), cfg, log, resp.Card, depth),
		}
		if data.Runtime != nil {
			event.WithData("runtime_enabled", data.Runtime.Enabled).
				WithData("runtime_agent_id", data.Runtime.AgentID)
			if strings.TrimSpace(data.Runtime.Error) != "" {
				event.WithData("runtime_error", data.Runtime.Error)
			}
		}
		observability.Emit(r.Context(), event.Success(time.Since(started)))
		writeCommandOK(w, http.StatusOK, commandOrchestrationBoardCardRuntimeGet, data)
	}
}

// OrchestrationDispatchIssueHandler handles POST /api/orchestration/dispatch-issue.
func OrchestrationDispatchIssueHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return OrchestrationDispatchIssueHandlerWithRuntime(cfg, log, nil)
}

func OrchestrationDispatchIssueHandlerWithRuntime(cfg config.Config, log zerolog.Logger, runtimeHost OrchestrationRuntimeHost) http.HandlerFunc {
	svc := newOrchestrationCommandService(cfg, log, runtimeHost)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeCommandError(w, http.StatusMethodNotAllowed, commandOrchestrationDispatchIssue, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}

		started := time.Now()
		var req coreorchestration.DispatchRequest
		if err := readJSON(w, r, &req); err != nil {
			evt := observability.NewEvent(opWebOrchestrationDispatchIssue).
				WithComponent(observability.ComponentWeb).
				WithCommand(commandOrchestrationDispatchIssue).
				EnrichFromContext(r.Context())
			observability.Emit(r.Context(), evt.Error(err, time.Since(started)))
			writeCommandError(w, http.StatusBadRequest, commandOrchestrationDispatchIssue, "EARG", "invalid json", map[string]any{
				"hint": httpErrorHint(http.StatusBadRequest),
			})
			return
		}
		req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
		req.IssueID = strings.TrimSpace(req.IssueID)
		req.IssueIdentifier = strings.TrimSpace(req.IssueIdentifier)
		req.Title = strings.TrimSpace(req.Title)
		req.ParentAgentID = chooseNonEmpty(strings.TrimSpace(req.ParentAgentID), resolveOrchestrationDispatchParentAgentID())
		req.Prompt = chooseNonEmpty(strings.TrimSpace(req.Prompt), defaultOrchestrationDispatchPrompt(req))

		event := observability.NewEvent(opWebOrchestrationDispatchIssue).
			WithComponent(observability.ComponentWeb).
			WithCommand(commandOrchestrationDispatchIssue).
			WithWorkspace(req.WorkspaceID).
			WithData("request_id", strings.TrimSpace(req.RequestID)).
			WithData("issue_id", req.IssueID).
			WithData("parent_agent_id", req.ParentAgentID).
			EnrichFromContext(r.Context())

		resp, err := svc.DispatchIssue(r.Context(), req)
		if err != nil {
			observability.Emit(r.Context(), event.Error(err, time.Since(started)))
			writeOrchestrationServiceError(w, commandOrchestrationDispatchIssue, err)
			return
		}

		event.WithData("status", resp.Status).
			WithData("policy_status", strings.TrimSpace(string(resp.PolicyStatus))).
			WithData("run_id", strings.TrimSpace(resp.RunID)).
			WithData("agent_id", strings.TrimSpace(resp.AgentID)).
			WithData("idempotent", resp.Idempotent)
		observability.Emit(r.Context(), event.Success(time.Since(started)))
		writeCommandOK(w, http.StatusOK, commandOrchestrationDispatchIssue, resp)
	}
}

// OrchestrationCardActionHandler handles POST /api/orchestration/card-action.
func OrchestrationCardActionHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeCommandError(w, http.StatusMethodNotAllowed, commandOrchestrationCardAction, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}

		started := time.Now()
		var req orchestrationCardActionRequest
		if err := readJSON(w, r, &req); err != nil {
			evt := observability.NewEvent(opWebOrchestrationCardAction).
				WithComponent(observability.ComponentWeb).
				WithCommand(commandOrchestrationCardAction).
				EnrichFromContext(r.Context())
			observability.Emit(r.Context(), evt.Error(err, time.Since(started)))
			writeCommandError(w, http.StatusBadRequest, commandOrchestrationCardAction, "EARG", "invalid json", map[string]any{
				"hint": httpErrorHint(http.StatusBadRequest),
			})
			return
		}

		req.RequestID = strings.TrimSpace(req.RequestID)
		req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
		req.IssueID = strings.TrimSpace(req.IssueID)
		req.Action = strings.TrimSpace(req.Action)

		event := observability.NewEvent(opWebOrchestrationCardAction).
			WithComponent(observability.ComponentWeb).
			WithCommand(commandOrchestrationCardAction).
			WithWorkspace(req.WorkspaceID).
			WithData("request_id", req.RequestID).
			WithData("issue_id", req.IssueID).
			WithData("action", req.Action).
			EnrichFromContext(r.Context())

		resp, err := applyOrchestrationCardAction(r.Context(), cfg, log, req)
		if err != nil {
			observability.Emit(r.Context(), event.Error(err, time.Since(started)))
			writeOrchestrationServiceError(w, commandOrchestrationCardAction, err)
			return
		}

		event.WithData("lane", strings.TrimSpace(string(resp.Card.Lane))).
			WithData("state", strings.TrimSpace(string(resp.Card.State))).
			WithData("idempotent", resp.Idempotent)
		observability.Emit(r.Context(), event.Success(time.Since(started)))
		writeCommandOK(w, http.StatusOK, commandOrchestrationCardAction, resp)
	}
}

// OrchestrationRefreshHandler handles POST /api/orchestration/refresh.
func OrchestrationRefreshHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return OrchestrationRefreshHandlerWithRuntime(cfg, log, nil)
}

func OrchestrationRefreshHandlerWithRuntime(cfg config.Config, log zerolog.Logger, runtimeHost OrchestrationRuntimeHost) http.HandlerFunc {
	svc := newOrchestrationCommandService(cfg, log, runtimeHost)
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeCommandError(w, http.StatusMethodNotAllowed, commandOrchestrationRefresh, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}

		started := time.Now()
		var req coreorchestration.RefreshRequest
		if err := readJSON(w, r, &req); err != nil {
			evt := observability.NewEvent(opWebOrchestrationRefresh).
				WithComponent(observability.ComponentWeb).
				WithCommand(commandOrchestrationRefresh).
				EnrichFromContext(r.Context())
			observability.Emit(r.Context(), evt.Error(err, time.Since(started)))
			writeCommandError(w, http.StatusBadRequest, commandOrchestrationRefresh, "EARG", "invalid json", map[string]any{
				"hint": httpErrorHint(http.StatusBadRequest),
			})
			return
		}
		event := observability.NewEvent(opWebOrchestrationRefresh).
			WithComponent(observability.ComponentWeb).
			WithCommand(commandOrchestrationRefresh).
			WithWorkspace(strings.TrimSpace(req.WorkspaceID)).
			WithData("request_id", strings.TrimSpace(req.RequestID)).
			EnrichFromContext(r.Context())

		resp, err := svc.Refresh(r.Context(), req)
		if err != nil {
			observability.Emit(r.Context(), event.Error(err, time.Since(started)))
			writeOrchestrationServiceError(w, commandOrchestrationRefresh, err)
			return
		}
		event.WithData("queued", resp.Queued).
			WithData("coalesced", resp.Coalesced).
			WithData("idempotent", resp.Idempotent)
		observability.Emit(r.Context(), event.Success(time.Since(started)))

		writeCommandOK(w, http.StatusOK, commandOrchestrationRefresh, resp)
	}
}

// OrchestrationSeedCardsHandler handles POST /api/orchestration/seed-cards.
func OrchestrationSeedCardsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeCommandError(w, http.StatusMethodNotAllowed, commandOrchestrationSeedCards, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}

		started := time.Now()
		var req orchestrationSeedCardsRequest
		if err := readJSON(w, r, &req); err != nil {
			evt := observability.NewEvent(opWebOrchestrationSeedCards).
				WithComponent(observability.ComponentWeb).
				WithCommand(commandOrchestrationSeedCards).
				EnrichFromContext(r.Context())
			observability.Emit(r.Context(), evt.Error(err, time.Since(started)))
			writeCommandError(w, http.StatusBadRequest, commandOrchestrationSeedCards, "EARG", "invalid json", map[string]any{
				"hint": httpErrorHint(http.StatusBadRequest),
			})
			return
		}

		req.RequestID = strings.TrimSpace(req.RequestID)
		if req.RequestID == "" {
			writeCommandError(w, http.StatusBadRequest, commandOrchestrationSeedCards, "EARG", "request_id is required", map[string]any{
				"field": "request_id",
				"hint":  httpErrorHint(http.StatusBadRequest),
			})
			return
		}
		if len(req.Cards) == 0 {
			writeCommandError(w, http.StatusBadRequest, commandOrchestrationSeedCards, "EARG", "cards must not be empty", map[string]any{
				"field": "cards",
				"hint":  httpErrorHint(http.StatusBadRequest),
			})
			return
		}

		event := observability.NewEvent(opWebOrchestrationSeedCards).
			WithComponent(observability.ComponentWeb).
			WithCommand(commandOrchestrationSeedCards).
			WithWorkspace(strings.TrimSpace(req.WorkspaceID)).
			WithData("request_id", req.RequestID).
			WithData("cards", len(req.Cards)).
			EnrichFromContext(r.Context())

		resp, err := seedOrchestrationProjectionCards(r.Context(), cfg, log, req)
		if err != nil {
			observability.Emit(r.Context(), event.Error(err, time.Since(started)))
			writeCommandError(w, http.StatusInternalServerError, commandOrchestrationSeedCards, "ERUNTIME", "failed to seed cards", map[string]any{
				"hint": httpErrorHint(http.StatusInternalServerError),
			})
			return
		}

		event.WithData("created", resp.Created).
			WithData("skipped", resp.Skipped)
		observability.Emit(r.Context(), event.Success(time.Since(started)))
		writeCommandOK(w, http.StatusOK, commandOrchestrationSeedCards, resp)
	}
}

// OrchestrationCleanupCardsHandler handles POST /api/orchestration/cleanup-cards.
func OrchestrationCleanupCardsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeCommandError(w, http.StatusMethodNotAllowed, commandOrchestrationCleanupCards, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}

		started := time.Now()
		var req orchestrationCleanupCardsRequest
		if err := readJSON(w, r, &req); err != nil {
			evt := observability.NewEvent(opWebOrchestrationCleanupCards).
				WithComponent(observability.ComponentWeb).
				WithCommand(commandOrchestrationCleanupCards).
				EnrichFromContext(r.Context())
			observability.Emit(r.Context(), evt.Error(err, time.Since(started)))
			writeCommandError(w, http.StatusBadRequest, commandOrchestrationCleanupCards, "EARG", "invalid json", map[string]any{
				"hint": httpErrorHint(http.StatusBadRequest),
			})
			return
		}

		req.RequestID = strings.TrimSpace(req.RequestID)
		req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
		if req.RequestID == "" {
			writeCommandError(w, http.StatusBadRequest, commandOrchestrationCleanupCards, "EARG", "request_id is required", map[string]any{
				"field": "request_id",
				"hint":  httpErrorHint(http.StatusBadRequest),
			})
			return
		}
		if req.WorkspaceID == "" {
			writeCommandError(w, http.StatusBadRequest, commandOrchestrationCleanupCards, "EARG", "workspace_id is required", map[string]any{
				"field": "workspace_id",
				"hint":  httpErrorHint(http.StatusBadRequest),
			})
			return
		}

		event := observability.NewEvent(opWebOrchestrationCleanupCards).
			WithComponent(observability.ComponentWeb).
			WithCommand(commandOrchestrationCleanupCards).
			WithWorkspace(req.WorkspaceID).
			WithData("request_id", req.RequestID).
			WithData("issue_count", len(req.IssueIDs)).
			EnrichFromContext(r.Context())

		resp, err := cleanupOrchestrationCards(r.Context(), cfg, log, req)
		if err != nil {
			observability.Emit(r.Context(), event.Error(err, time.Since(started)))
			writeCommandError(w, http.StatusInternalServerError, commandOrchestrationCleanupCards, "ERUNTIME", "failed to clean up cards", map[string]any{
				"hint": httpErrorHint(http.StatusInternalServerError),
			})
			return
		}

		event.WithData("deleted_cards", resp.DeletedCards).
			WithData("deleted_events", resp.DeletedEvents)
		observability.Emit(r.Context(), event.Success(time.Since(started)))
		writeCommandOK(w, http.StatusOK, commandOrchestrationCleanupCards, resp)
	}
}

func OrchestrationArchiveCardsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return orchestrationArchiveToggleHandler(cfg, log, true)
}

func OrchestrationRestoreCardsHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return orchestrationArchiveToggleHandler(cfg, log, false)
}

func orchestrationArchiveToggleHandler(cfg config.Config, log zerolog.Logger, archive bool) http.HandlerFunc {
	command := commandOrchestrationArchiveCards
	op := opWebOrchestrationArchiveCards
	action := "archived"
	if !archive {
		command = commandOrchestrationRestoreCards
		op = opWebOrchestrationRestoreCards
		action = "restored"
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeCommandError(w, http.StatusMethodNotAllowed, command, "EARG", "method not allowed", map[string]any{
				"hint": httpErrorHint(http.StatusMethodNotAllowed),
			})
			return
		}
		started := time.Now()
		var req orchestrationArchiveCardsRequest
		if err := readJSON(w, r, &req); err != nil {
			evt := observability.NewEvent(op).
				WithComponent(observability.ComponentWeb).
				WithCommand(command).
				EnrichFromContext(r.Context())
			observability.Emit(r.Context(), evt.Error(err, time.Since(started)))
			writeCommandError(w, http.StatusBadRequest, command, "EARG", "invalid json", map[string]any{
				"hint": httpErrorHint(http.StatusBadRequest),
			})
			return
		}
		req.RequestID = strings.TrimSpace(req.RequestID)
		req.WorkspaceID = strings.TrimSpace(req.WorkspaceID)
		if req.RequestID == "" || req.WorkspaceID == "" {
			writeCommandError(w, http.StatusBadRequest, command, "EARG", "request_id and workspace_id are required", map[string]any{
				"hint": httpErrorHint(http.StatusBadRequest),
			})
			return
		}
		event := observability.NewEvent(op).
			WithComponent(observability.ComponentWeb).
			WithCommand(command).
			WithWorkspace(req.WorkspaceID).
			WithData("request_id", req.RequestID).
			WithData("issue_count", len(req.IssueIDs)).
			EnrichFromContext(r.Context())
		resp, err := archiveOrRestoreOrchestrationCards(r.Context(), cfg, log, req, archive)
		if err != nil {
			observability.Emit(r.Context(), event.Error(err, time.Since(started)))
			writeCommandError(w, http.StatusInternalServerError, command, "ERUNTIME", "failed to update archive state", map[string]any{
				"hint": httpErrorHint(http.StatusInternalServerError),
			})
			return
		}
		event.WithData("updated", resp.Updated).
			WithData("action", action)
		observability.Emit(r.Context(), event.Success(time.Since(started)))
		writeCommandOK(w, http.StatusOK, command, resp)
	}
}

type orchestrationProjectionReader struct {
	cfg config.Config
	log zerolog.Logger
}

func (r orchestrationProjectionReader) Board(ctx context.Context, req coreorchestration.BoardRequest) (coreorchestration.BoardResponse, error) {
	store, closeFn, err := openOrchestrationStore(ctx, r.cfg)
	if err != nil {
		return coreorchestration.BoardResponse{}, err
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			r.log.Warn().Err(closeErr).Msg("failed to close orchestration store")
		}
	}()
	return store.Board(ctx, req)
}

func (r orchestrationProjectionReader) Card(ctx context.Context, req coreorchestration.CardRequest) (coreorchestration.CardResponse, error) {
	store, closeFn, err := openOrchestrationStore(ctx, r.cfg)
	if err != nil {
		return coreorchestration.CardResponse{}, err
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			r.log.Warn().Err(closeErr).Msg("failed to close orchestration store")
		}
	}()
	return store.Card(ctx, req)
}

type inMemoryRefreshQueue struct {
	mu         sync.Mutex
	window     time.Duration
	inflight   map[string]*refreshInflight
	lastResult map[string]refreshResult
	onEnqueue  func(ctx context.Context, workspaceID, requestID string) error
}

type refreshInflight struct {
	done chan struct{}
	err  error
}

type refreshResult struct {
	completedAt time.Time
	err         error
}

func newInMemoryRefreshQueue(
	window time.Duration,
	onEnqueue func(ctx context.Context, workspaceID, requestID string) error,
) *inMemoryRefreshQueue {
	if window <= 0 {
		window = refreshQueueCoalesceWindow
	}
	return &inMemoryRefreshQueue{
		window:     window,
		inflight:   map[string]*refreshInflight{},
		lastResult: map[string]refreshResult{},
		onEnqueue:  onEnqueue,
	}
}

func (q *inMemoryRefreshQueue) Enqueue(ctx context.Context, workspaceID, requestID string) (queued bool, coalesced bool, err error) {
	if q == nil {
		return false, false, fmt.Errorf("refresh queue is not configured")
	}

	now := time.Now().UTC()
	scope := strings.TrimSpace(workspaceID)
	if scope == "" {
		scope = "_workspace"
	}

	q.mu.Lock()
	for key, result := range q.lastResult {
		if !result.completedAt.Add(q.window).After(now) {
			delete(q.lastResult, key)
		}
	}

	if inflight := q.inflight[scope]; inflight != nil {
		q.mu.Unlock()
		select {
		case <-ctx.Done():
			return false, false, ctx.Err()
		case <-inflight.done:
			if inflight.err != nil {
				return false, false, inflight.err
			}
			return true, true, nil
		}
	}

	if result, exists := q.lastResult[scope]; exists && result.completedAt.Add(q.window).After(now) {
		q.mu.Unlock()
		if result.err != nil {
			return false, false, result.err
		}
		return true, true, nil
	}

	inflight := &refreshInflight{done: make(chan struct{})}
	q.inflight[scope] = inflight
	onEnqueue := q.onEnqueue
	q.mu.Unlock()

	if onEnqueue == nil {
		q.mu.Lock()
		delete(q.inflight, scope)
		q.mu.Unlock()
		return false, false, fmt.Errorf("refresh queue has no runtime enqueue callback")
	}
	runErr := onEnqueue(ctx, workspaceID, requestID)

	q.mu.Lock()
	delete(q.inflight, scope)
	inflight.err = runErr
	close(inflight.done)
	q.lastResult[scope] = refreshResult{
		completedAt: time.Now().UTC(),
		err:         runErr,
	}
	q.mu.Unlock()

	if runErr != nil {
		return false, false, runErr
	}
	return true, false, nil
}

func newOrchestrationCommandService(cfg config.Config, log zerolog.Logger, runtimeHost OrchestrationRuntimeHost) *v2services.OrchestrationService {
	reader := orchestrationProjectionReader{cfg: cfg, log: log}
	dispatchSpawner := newOrchestrationDispatchSpawner(cfg, log, runtimeHost)
	return v2services.NewOrchestrationService(v2services.OrchestrationDependencies{
		Spawn:  dispatchSpawner,
		Reader: reader,
		RefreshQueue: newInMemoryRefreshQueue(refreshQueueCoalesceWindow, func(ctx context.Context, workspaceID, requestID string) error {
			if runtimeHost != nil {
				return runtimeHost.Refresh(ctx, workspaceID, requestID)
			}
			return runOrchestrationRefresh(ctx, cfg, log, workspaceID, requestID)
		}),
		Now: func() time.Time {
			return time.Now().UTC()
		},
		LaneOptions: defaultOrchestrationLaneOptions(),
	})
}

type orchestrationRuntimeSpawner struct {
	cfg         config.Config
	log         zerolog.Logger
	runtimeHost OrchestrationRuntimeHost
}

func newOrchestrationDispatchSpawner(cfg config.Config, log zerolog.Logger, runtimeHost OrchestrationRuntimeHost) v2services.OrchestrationSpawner {
	return orchestrationRuntimeSpawner{cfg: cfg, log: log, runtimeHost: runtimeHost}
}

func (s orchestrationRuntimeSpawner) Spawn(ctx context.Context, req corespawn.Request) (corespawn.Response, error) {
	if s.runtimeHost != nil {
		return s.runtimeHost.Spawn(ctx, req)
	}
	req.ParentAgentID = chooseNonEmpty(strings.TrimSpace(req.ParentAgentID), resolveOrchestrationDispatchParentAgentID())
	if strings.TrimSpace(req.ParentAgentID) == "" {
		return corespawn.Response{}, &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "orchestration dispatch parent_agent_id is not configured",
			Fatal:   true,
		}
	}

	eventStore, err := openOrchestrationEventStore(ctx, s.cfg)
	if err != nil {
		return corespawn.Response{}, fmt.Errorf("open event store for dispatch: %w", err)
	}
	defer func() {
		if closeErr := eventStore.Close(); closeErr != nil {
			s.log.Warn().Err(closeErr).Msg("failed to close orchestration event store during dispatch")
		}
	}()

	orchestrationStore, closeFn, err := openOrchestrationStore(ctx, s.cfg)
	if err != nil {
		return corespawn.Response{}, fmt.Errorf("open orchestration store for dispatch: %w", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			s.log.Warn().Err(closeErr).Msg("failed to close orchestration store during dispatch")
		}
	}()

	runtime, err := v2jido.NewOrchestrationRuntime(v2jido.OrchestrationRuntimeConfig{
		Events:         eventStore,
		Projections:    orchestrationStore,
		Reader:         orchestrationStore,
		ParentAgentIDs: []string{req.ParentAgentID},
	})
	if err != nil {
		return corespawn.Response{}, fmt.Errorf("configure jido orchestration runtime: %w", err)
	}

	spawnService := v2services.NewSpawnService(v2services.SpawnDependencies{
		RuntimeSpawner: runtime.ChildSpawner,
	})
	return spawnService.Spawn(ctx, req)
}

func openOrchestrationStore(ctx context.Context, cfg config.Config) (*libsqlorchestration.Store, func() error, error) {
	storageRoot := strings.TrimSpace(cfg.Storage.Root)
	if storageRoot == "" {
		return nil, nil, fmt.Errorf("orchestration store open: storage root is required")
	}

	dbCfg, err := orchestrationDBConfig(cfg)
	if err != nil {
		return nil, nil, err
	}

	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, dbCfg, libsqlorchestration.MigrateSchema)
	if err != nil {
		return nil, nil, fmt.Errorf("orchestration store open: %w", err)
	}

	store := libsqlorchestration.NewStore(db, libsqlorchestration.StoreOptions{
		LaneOptions: defaultOrchestrationLaneOptions(),
	})
	return store, closeFn, nil
}

func defaultOrchestrationLaneOptions() coreorchestration.LaneOptions {
	return coreorchestration.DefaultLaneOptions()
}

func orchestrationDBConfig(cfg config.Config) (dbdriver.Config, error) {
	storageRoot := strings.TrimSpace(cfg.Storage.Root)
	if storageRoot == "" {
		return dbdriver.Config{}, fmt.Errorf("orchestration db config: storage root is required")
	}

	if hasV2EventsDriverOverride() {
		loader := dbdriver.NewConfigLoader(storageRoot)
		cfg := loader.LoadConfig("V2_EVENTS", "v2_events.db")
		switch cfg.Driver {
		case dbdriver.DriverSQLite, dbdriver.DriverTurso:
			return cfg, nil
		case dbdriver.DriverPostgres:
			return dbdriver.Config{}, fmt.Errorf("orchestration db config: postgres is not supported by v2 orchestration projections")
		default:
			return dbdriver.Config{}, fmt.Errorf("orchestration db config: unsupported database driver override %q", cfg.Driver)
		}
	}

	switch strings.ToLower(strings.TrimSpace(cfg.Database.Driver)) {
	case "sqlite":
		return dbdriver.DefaultSQLiteConfig(v2EventsDBPath(filepath.Join(storageRoot, "v2_events.db"))), nil
	case "", "turso":
		dims := cfg.Database.Vector.Dimensions
		if dims <= 0 {
			dims = dbdriver.GetDefaultVectorDimensions()
		}
		return dbdriver.Config{
			Driver: dbdriver.DriverTurso,
			Turso: dbdriver.TursoConfig{
				Path:               v2EventsDBPath(filepath.Join(storageRoot, "v2_events.turso")),
				DatabaseName:       "v2_events",
				ReplicaPath:        v2EventsDBPath(filepath.Join(storageRoot, "v2_events.turso")),
				EnableVectorSearch: false,
				VectorDimensions:   dims,
			},
		}, nil
	case "postgres":
		// Orchestration projections are always stored in SQLite-compatible tables and
		// intentionally decoupled from the primary runtime DB driver.
		return dbdriver.DefaultSQLiteConfig(v2EventsDBPath(filepath.Join(storageRoot, "v2_events.db"))), nil
	default:
		return dbdriver.Config{}, fmt.Errorf("orchestration db config: unsupported database driver %q", cfg.Database.Driver)
	}
}

func hasV2EventsDriverOverride() bool {
	return strings.TrimSpace(os.Getenv("FOXCTL_V2_EVENTS_DB_DRIVER")) != ""
}

func v2EventsDBPath(defaultPath string) string {
	if override := strings.TrimSpace(os.Getenv("FOXCTL_V2_EVENTS_DB_PATH")); override != "" {
		return override
	}
	return defaultPath
}

func runOrchestrationRefresh(
	ctx context.Context,
	cfg config.Config,
	log zerolog.Logger,
	workspaceID string,
	requestID string,
) error {
	eventStore, err := openOrchestrationEventStore(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open event store for refresh: %w", err)
	}
	defer func() {
		if closeErr := eventStore.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close event store during refresh")
		}
	}()

	orchestrationStore, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		return fmt.Errorf("open orchestration store for refresh: %w", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close orchestration store during refresh")
		}
	}()

	if err := orchestrationStore.ReplayFrom(ctx, eventStore, coreevents.ReplayFilter{}); err != nil {
		return fmt.Errorf("refresh orchestration projection replay: %w", err)
	}
	log.Info().
		Str("workspace_id", strings.TrimSpace(workspaceID)).
		Str("request_id", strings.TrimSpace(requestID)).
		Msg("orchestration refresh replay completed")

	if !v2jido.OrchestrationRuntimeEnabled(v2jido.OrchestrationRuntimeConfig{}) {
		return nil
	}

	runtime, err := v2jido.NewOrchestrationRuntime(v2jido.OrchestrationRuntimeConfig{
		Events:      eventStore,
		Projections: orchestrationStore,
		Reader:      orchestrationStore,
	})
	if err != nil {
		return fmt.Errorf("configure jido orchestration runtime: %w", err)
	}
	if runtime.Reconciler == nil {
		return nil
	}
	if err := runtime.Reconciler.Reconcile(ctx); err != nil {
		return fmt.Errorf("reconcile jido orchestration runtime: %w", err)
	}

	log.Info().
		Str("workspace_id", strings.TrimSpace(workspaceID)).
		Str("request_id", strings.TrimSpace(requestID)).
		Msg("orchestration refresh jido reconcile completed")
	return nil
}

func openOrchestrationEventStore(ctx context.Context, cfg config.Config) (*libsqlevents.Store, error) {
	dbCfg, err := orchestrationDBConfig(cfg)
	if err != nil {
		return nil, err
	}
	db, closeFn, err := dbdriver.OpenDBCompatWithCloser(ctx, dbCfg, libsqlevents.MigrateSchema)
	if err != nil {
		return nil, fmt.Errorf("orchestration event store open: %w", err)
	}
	return libsqlevents.NewStore(db, closeFn), nil
}

func seedOrchestrationProjectionCards(
	ctx context.Context,
	cfg config.Config,
	log zerolog.Logger,
	req orchestrationSeedCardsRequest,
) (orchestrationSeedCardsResponse, error) {
	eventStore, err := openOrchestrationEventStore(ctx, cfg)
	if err != nil {
		return orchestrationSeedCardsResponse{}, fmt.Errorf("open event store for seed: %w", err)
	}
	defer func() {
		if closeErr := eventStore.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close event store during card seed")
		}
	}()

	orchestrationStore, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		return orchestrationSeedCardsResponse{}, fmt.Errorf("open orchestration store for seed: %w", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close orchestration store during card seed")
		}
	}()

	now := time.Now().UTC()
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	created := 0
	skipped := 0

	for i, card := range req.Cards {
		title := strings.TrimSpace(card.Title)
		if title == "" {
			skipped++
			continue
		}

		issueID := strings.TrimSpace(card.IssueID)
		if issueID == "" {
			issueID = fmt.Sprintf("seed-%s-%03d", stableToken(req.RequestID), i+1)
		}
		issueIdentifier := strings.TrimSpace(card.IssueIdentifier)
		if issueIdentifier == "" {
			issueIdentifier = strings.ToUpper(issueID)
		}

		state := strings.TrimSpace(card.State)
		if state == "" {
			state = string(coreorchestration.StateReleased)
		}
		eligibility := strings.TrimSpace(card.Eligibility)
		if eligibility == "" {
			eligibility = string(coreorchestration.EligibilityEligible)
		}

		eventID := fmt.Sprintf("evt-seed-%s-%s-%03d", stableToken(req.RequestID), stableToken(issueID), i+1)
		evt := coreevents.Event{
			ID:            eventID,
			StreamID:      fmt.Sprintf("seed:%s:%03d", stableToken(req.RequestID), i+1),
			StreamType:    coreevents.StreamTypeRun,
			StreamVersion: 1,
			Sequence:      1,
			EventType:     coreevents.EventRunStarted,
			OccurredAt:    now,
			ActorID:       "actor:web:orchestration",
			RequestID:     req.RequestID,
			Command:       "orchestration/dispatch-issue",
			Payload: coreevents.MustMarshalPayload(map[string]any{
				"workspace_id":     workspaceID,
				"issue_id":         issueID,
				"issue_identifier": issueIdentifier,
				"title":            title,
				"state":            state,
				"tracker_state":    strings.TrimSpace(card.TrackerState),
				"policy_status":    strings.TrimSpace(card.PolicyStatus),
				"last_outcome":     strings.TrimSpace(card.LastOutcome),
				"eligibility":      eligibility,
			}),
		}

		if appendErr := eventStore.Append(ctx, evt); appendErr != nil {
			if errors.Is(appendErr, coreevents.ErrVersionConflict) {
				skipped++
				continue
			}
			return orchestrationSeedCardsResponse{}, fmt.Errorf("append seed card event: %w", appendErr)
		}
		if applyErr := orchestrationStore.Apply(ctx, evt); applyErr != nil {
			return orchestrationSeedCardsResponse{}, fmt.Errorf("apply seed card projection: %w", applyErr)
		}
		created++
	}

	return orchestrationSeedCardsResponse{
		RequestID: req.RequestID,
		Created:   created,
		Skipped:   skipped,
		Timestamp: now,
	}, nil
}

func cleanupOrchestrationCards(
	ctx context.Context,
	cfg config.Config,
	log zerolog.Logger,
	req orchestrationCleanupCardsRequest,
) (orchestrationCleanupCardsResponse, error) {
	eventStore, err := openOrchestrationEventStore(ctx, cfg)
	if err != nil {
		return orchestrationCleanupCardsResponse{}, fmt.Errorf("open event store for cleanup: %w", err)
	}
	defer func() {
		if closeErr := eventStore.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close event store during card cleanup")
		}
	}()

	orchestrationStore, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		return orchestrationCleanupCardsResponse{}, fmt.Errorf("open orchestration store for cleanup: %w", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close orchestration store during card cleanup")
		}
	}()

	deletedEvents, eventIDs, err := eventStore.DeleteOrchestrationIssueHistory(ctx, req.WorkspaceID, req.IssueIDs)
	if err != nil {
		return orchestrationCleanupCardsResponse{}, fmt.Errorf("delete orchestration event history: %w", err)
	}
	deletedCards, err := orchestrationStore.DeleteCards(ctx, req.WorkspaceID, req.IssueIDs, eventIDs)
	if err != nil {
		return orchestrationCleanupCardsResponse{}, fmt.Errorf("delete orchestration cards: %w", err)
	}

	return orchestrationCleanupCardsResponse{
		RequestID:     req.RequestID,
		DeletedCards:  deletedCards,
		DeletedEvents: deletedEvents,
		Timestamp:     time.Now().UTC(),
	}, nil
}

func archiveOrRestoreOrchestrationCards(
	ctx context.Context,
	cfg config.Config,
	log zerolog.Logger,
	req orchestrationArchiveCardsRequest,
	archive bool,
) (orchestrationArchiveCardsResponse, error) {
	orchestrationStore, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		return orchestrationArchiveCardsResponse{}, fmt.Errorf("open orchestration store for archive toggle: %w", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close orchestration store during archive toggle")
		}
	}()

	var updated int
	if archive {
		updated, err = orchestrationStore.ArchiveCards(ctx, req.WorkspaceID, req.IssueIDs)
	} else {
		updated, err = orchestrationStore.RestoreCards(ctx, req.WorkspaceID, req.IssueIDs)
	}
	if err != nil {
		return orchestrationArchiveCardsResponse{}, err
	}

	action := "restored"
	if archive {
		action = "archived"
	}
	return orchestrationArchiveCardsResponse{
		RequestID: req.RequestID,
		Updated:   updated,
		Action:    action,
		Timestamp: time.Now().UTC(),
	}, nil
}

func applyOrchestrationCardAction(
	ctx context.Context,
	cfg config.Config,
	log zerolog.Logger,
	req orchestrationCardActionRequest,
) (orchestrationCardActionResponse, error) {
	if req.RequestID == "" {
		return orchestrationCardActionResponse{}, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "request_id is required",
			Details: map[string]any{"field": "request_id"},
		}
	}
	if req.IssueID == "" {
		return orchestrationCardActionResponse{}, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "issue_id is required",
			Details: map[string]any{"field": "issue_id"},
		}
	}

	action := normalizeOrchestrationCardAction(req.Action)
	if action == "" {
		return orchestrationCardActionResponse{}, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "action must be one of retry-now, release, or mark-done",
			Details: map[string]any{"field": "action"},
		}
	}

	eventStore, err := openOrchestrationEventStore(ctx, cfg)
	if err != nil {
		return orchestrationCardActionResponse{}, fmt.Errorf("open event store for card action: %w", err)
	}
	defer func() {
		if closeErr := eventStore.Close(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close event store during card action")
		}
	}()

	orchestrationStore, closeFn, err := openOrchestrationStore(ctx, cfg)
	if err != nil {
		return orchestrationCardActionResponse{}, fmt.Errorf("open orchestration store for card action: %w", err)
	}
	defer func() {
		if closeErr := closeFn(); closeErr != nil {
			log.Warn().Err(closeErr).Msg("failed to close orchestration store during card action")
		}
	}()

	current, err := orchestrationStore.Card(ctx, coreorchestration.CardRequest{
		WorkspaceID: req.WorkspaceID,
		IssueID:     req.IssueID,
	})
	if err != nil {
		if errors.Is(err, libsqlorchestration.ErrNotFound) {
			return orchestrationCardActionResponse{}, &v2errors.V2Error{
				Kind:    v2errors.ErrNotFound,
				Message: "orchestration card not found",
				Details: map[string]any{
					"workspace_id": req.WorkspaceID,
					"issue_id":     req.IssueID,
				},
			}
		}
		return orchestrationCardActionResponse{}, fmt.Errorf("read orchestration card for action: %w", err)
	}

	now := time.Now().UTC()
	payload, err := orchestrationCardActionPayload(current.Card, action, now)
	if err != nil {
		return orchestrationCardActionResponse{}, err
	}

	evt := coreevents.Event{
		ID:            fmt.Sprintf("evt-orch-action-%s-%s-%s", stableToken(req.RequestID), stableToken(req.IssueID), stableToken(action)),
		StreamID:      chooseNonEmpty(strings.TrimSpace(current.Card.RunID), "orch:"+strings.TrimSpace(current.Card.IssueID)),
		StreamType:    coreevents.StreamTypeRun,
		EventType:     coreevents.EventOrchestrationUpdated,
		OccurredAt:    now,
		CorrelationID: req.RequestID,
		CausationID:   req.RequestID,
		ActorID:       "actor:web:orchestration",
		RequestID:     req.RequestID,
		Command:       commandOrchestrationCardAction,
		Payload:       coreevents.MustMarshalPayload(payload),
	}

	idempotent := false
	if err := eventStore.Append(ctx, evt); err != nil {
		if !errors.Is(err, coreevents.ErrVersionConflict) {
			return orchestrationCardActionResponse{}, fmt.Errorf("append orchestration card action: %w", err)
		}
		idempotent = true
	} else {
		if err := orchestrationStore.Apply(ctx, evt); err != nil {
			return orchestrationCardActionResponse{}, fmt.Errorf("apply orchestration card action: %w", err)
		}
	}

	updated, err := orchestrationStore.Card(ctx, coreorchestration.CardRequest{
		WorkspaceID: chooseNonEmpty(req.WorkspaceID, current.Card.WorkspaceID),
		IssueID:     req.IssueID,
	})
	if err != nil {
		return orchestrationCardActionResponse{}, fmt.Errorf("read orchestration card after action: %w", err)
	}

	return orchestrationCardActionResponse{
		RequestID:  req.RequestID,
		Action:     action,
		Card:       updated.Card,
		Idempotent: idempotent,
		Timestamp:  now,
	}, nil
}

func orchestrationCardActionPayload(card coreorchestration.Card, action string, now time.Time) (map[string]any, error) {
	action = normalizeOrchestrationCardAction(action)
	if action == "" {
		return nil, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "unsupported orchestration card action",
		}
	}
	if card.IssueID == "" {
		return nil, &v2errors.V2Error{
			Kind:    v2errors.ErrNotFound,
			Message: "orchestration card not found",
		}
	}
	if card.State == coreorchestration.StateRunning || card.State == coreorchestration.StateClaimed {
		return nil, &v2errors.V2Error{
			Kind:    v2errors.ErrPolicyViolation,
			Message: "card action is not allowed while the card is actively running",
			Details: map[string]any{
				"state":  card.State,
				"action": action,
			},
		}
	}

	payload := map[string]any{
		"workspace_id":     strings.TrimSpace(card.WorkspaceID),
		"issue_id":         strings.TrimSpace(card.IssueID),
		"issue_identifier": strings.TrimSpace(card.IssueIdentifier),
		"title":            strings.TrimSpace(card.Title),
		"run_id":           strings.TrimSpace(card.RunID),
		"agent_id":         strings.TrimSpace(card.AgentID),
		"actor_id":         strings.TrimSpace(card.ActorID),
		"attempt":          card.Attempt,
	}

	switch action {
	case orchestrationActionRetryNow:
		if card.Lane != coreorchestration.LaneRetryQueue && card.Lane != coreorchestration.LaneBlocked {
			return nil, &v2errors.V2Error{
				Kind:    v2errors.ErrPolicyViolation,
				Message: "retry-now is only allowed for blocked or retry-queued cards",
				Details: map[string]any{
					"lane": card.Lane,
				},
			}
		}
		payload["state"] = string(coreorchestration.StateRetryQueue)
		payload["eligibility"] = string(coreorchestration.EligibilityEligible)
		payload["policy_status"] = string(coreorchestration.PolicyStatusOK)
		payload["retry_due_at"] = now.Format(time.RFC3339Nano)
		payload["tracker_state"] = ""
		payload["last_outcome"] = ""
		payload["denial_reason"] = ""
		payload["suggestion"] = ""
	case orchestrationActionRelease:
		payload["state"] = string(coreorchestration.StateReleased)
		payload["eligibility"] = string(coreorchestration.EligibilityEligible)
		payload["policy_status"] = string(coreorchestration.PolicyStatusOK)
		payload["retry_due_at"] = ""
		payload["tracker_state"] = ""
		payload["last_outcome"] = ""
		payload["denial_reason"] = ""
		payload["suggestion"] = ""
	case orchestrationActionMarkDone:
		payload["state"] = string(coreorchestration.StateReleased)
		payload["eligibility"] = string(coreorchestration.EligibilityEligible)
		payload["policy_status"] = string(coreorchestration.PolicyStatusOK)
		payload["tracker_state"] = "Done"
		payload["retry_due_at"] = ""
		payload["last_outcome"] = ""
		payload["denial_reason"] = ""
		payload["suggestion"] = ""
	default:
		return nil, &v2errors.V2Error{
			Kind:    v2errors.ErrValidation,
			Message: "unsupported orchestration card action",
		}
	}
	return payload, nil
}

func normalizeOrchestrationCardAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case orchestrationActionRetryNow:
		return orchestrationActionRetryNow
	case orchestrationActionRelease:
		return orchestrationActionRelease
	case orchestrationActionMarkDone:
		return orchestrationActionMarkDone
	default:
		return ""
	}
}

func loadOrchestrationCardRuntime(ctx context.Context, cfg config.Config, log zerolog.Logger, card coreorchestration.Card) *orchestrationCardRuntimeData {
	runtime := &orchestrationCardRuntimeData{
		Enabled: strings.TrimSpace(card.AgentID) != "",
		AgentID: strings.TrimSpace(card.AgentID),
	}
	if runtime.AgentID == "" {
		runtime.Error = "card has no agent_id"
		return runtime
	}

	if strings.EqualFold(ResolveOrchestrationRuntimeBackend(), orchestrationRuntimeBackendGoruntimeAPI) {
		reader, closeFn, available, err := loadOptionalRuntimeStateReader(ctx, cfg)
		if err != nil {
			runtime.Error = err.Error()
			return runtime
		}
		if !available {
			return runtime
		}
		defer func() {
			if closeFn != nil {
				_ = closeFn()
			}
		}()

		record, err := reader.Worker(ctx, coreworker.LookupRequest{AgentID: runtime.AgentID})
		if err != nil {
			runtime.Error = err.Error()
			return runtime
		}
		runtime.Status = string(record.Status)
		runtime.State = decodeRuntimeWorkerState(ctx, cfg, log, runtime.AgentID, record.RawState, "failed to decode orchestration runtime state; returning raw payload")
		children, err := reader.Children(ctx, coreworker.ChildrenRequest{ParentAgentID: runtime.AgentID})
		if err != nil {
			runtime.Error = chooseNonEmpty(runtime.Error, err.Error())
			return runtime
		}
		runtime.Children = workerChildRefs(children)
		return runtime
	}

	client, available, err := loadOptionalJidoClient()
	if err != nil {
		runtime.Error = err.Error()
		return runtime
	}
	if !available {
		return runtime
	}

	stateResp, err := client.State(ctx, v2jido.StateRequest{AgentID: runtime.AgentID})
	if err != nil {
		runtime.Error = err.Error()
		return runtime
	}
	runtime.Status = strings.TrimSpace(stateResp.Status)
	if len(stateResp.State) > 0 && string(stateResp.State) != "null" {
		var state any
		if err := json.Unmarshal(stateResp.State, &state); err != nil {
			runtime.State = string(stateResp.State)
			log.Debug().Err(err).Str("agent_id", runtime.AgentID).Msg("failed to decode orchestration runtime state; returning raw payload")
		} else {
			if stateMap, ok := state.(map[string]any); ok {
				state = taskhistory.RefreshJidoRuntimeState(ctx, cfg.Storage.Root, cfg.Paths.CAS, stateMap)
			}
			runtime.State = state
		}
	}

	childrenResp, err := client.GetChildren(ctx, v2jido.GetChildrenRequest{AgentID: runtime.AgentID})
	if err != nil {
		runtime.Error = chooseNonEmpty(runtime.Error, err.Error())
		return runtime
	}
	if len(childrenResp.Children) > 0 {
		runtime.Children = childrenResp.Children
	}
	return runtime
}

func loadOrchestrationCardRuntimeTree(ctx context.Context, cfg config.Config, log zerolog.Logger, card coreorchestration.Card, depth int) *orchestrationRuntimeTreeData {
	runtime := &orchestrationRuntimeTreeData{
		Enabled: strings.TrimSpace(card.AgentID) != "",
		AgentID: strings.TrimSpace(card.AgentID),
		Depth:   depth,
	}
	if runtime.AgentID == "" {
		runtime.Error = "card has no agent_id"
		return runtime
	}

	reader, closeFn, available, err := loadOptionalRuntimeStateReader(ctx, cfg)
	if err != nil {
		runtime.Error = err.Error()
		return runtime
	}
	if !available {
		return runtime
	}
	defer func() {
		if closeFn != nil {
			_ = closeFn()
		}
	}()

	visited := map[string]struct{}{}
	root := loadOrchestrationRuntimeTreeNode(ctx, cfg, log, reader, coreworker.Record{
		Tag:      runtime.AgentID,
		AgentID:  runtime.AgentID,
		Metadata: map[string]any{"workspace_id": card.WorkspaceID, "issue_id": card.IssueID},
	}, depth, visited)
	runtime.Root = root
	if root != nil && strings.TrimSpace(root.Error) != "" {
		runtime.Error = root.Error
	}
	return runtime
}

func loadOrchestrationRuntimeTreeNode(
	ctx context.Context,
	cfg config.Config,
	log zerolog.Logger,
	reader coreworker.StateReader,
	seed coreworker.Record,
	depth int,
	visited map[string]struct{},
) *orchestrationRuntimeTreeNode {
	agentID := strings.TrimSpace(seed.AgentID)
	node := &orchestrationRuntimeTreeNode{
		Tag:      strings.TrimSpace(seed.Tag),
		AgentID:  agentID,
		PID:      strings.TrimSpace(seed.PID),
		Metadata: seed.Metadata,
	}
	if agentID == "" {
		node.Error = "runtime node has no agent_id"
		return node
	}
	if _, ok := visited[agentID]; ok {
		node.Error = "runtime subtree cycle detected"
		return node
	}
	visited[agentID] = struct{}{}
	defer delete(visited, agentID)

	record, err := reader.Worker(ctx, coreworker.LookupRequest{AgentID: agentID})
	if err != nil {
		node.Error = err.Error()
		return node
	}
	node.Tag = chooseNonEmpty(strings.TrimSpace(record.Tag), node.Tag)
	node.PID = chooseNonEmpty(strings.TrimSpace(record.PID), node.PID)
	node.Metadata = mergeRuntimeMetadata(node.Metadata, record.Metadata)
	node.Status = string(record.Status)
	node.State = decodeRuntimeWorkerState(ctx, cfg, log, agentID, record.RawState, "failed to decode orchestration runtime node state; returning raw payload")
	if depth <= 0 {
		return node
	}

	children, err := reader.Children(ctx, coreworker.ChildrenRequest{ParentAgentID: agentID})
	if err != nil {
		node.Error = chooseNonEmpty(node.Error, err.Error())
		return node
	}
	for _, child := range children {
		node.Children = append(node.Children, loadOrchestrationRuntimeTreeNode(ctx, cfg, log, reader, child, depth-1, visited))
	}
	return node
}

func resolveOrchestrationDispatchParentAgentID() string {
	if value := strings.TrimSpace(os.Getenv(EnvOrchestrationDispatchParentAgentID)); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv(v2jido.EnvJidoOrchestrationDispatchParentAgentID)); value != "" {
		return value
	}
	raw := strings.TrimSpace(os.Getenv(EnvOrchestrationParentAgentIDs))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv(v2jido.EnvJidoOrchestrationParentAgentIDs))
	}
	if raw == "" {
		return ""
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func defaultOrchestrationDispatchPrompt(req coreorchestration.DispatchRequest) string {
	switch {
	case strings.TrimSpace(req.IssueIdentifier) != "" && strings.TrimSpace(req.Title) != "":
		return fmt.Sprintf("Work on issue %s: %s", strings.TrimSpace(req.IssueIdentifier), strings.TrimSpace(req.Title))
	case strings.TrimSpace(req.Title) != "":
		return "Work on issue: " + strings.TrimSpace(req.Title)
	case strings.TrimSpace(req.IssueIdentifier) != "":
		return "Work on issue " + strings.TrimSpace(req.IssueIdentifier)
	case strings.TrimSpace(req.IssueID) != "":
		return "Work on issue " + strings.TrimSpace(req.IssueID)
	default:
		return ""
	}
}

func stableToken(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return "x"
	}
	var b strings.Builder
	b.Grow(len(trimmed))
	prevDash := false
	for _, r := range trimmed {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "x"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

func chooseNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func maybeArtifactizeBoard(ctx context.Context, cfg config.Config, board coreorchestration.BoardResponse) (data any, artifactized bool, err error) {
	raw, err := json.Marshal(board)
	if err != nil {
		return nil, false, fmt.Errorf("marshal board payload: %w", err)
	}
	if len(raw) <= orchestrationLargePayloadThreshold {
		return board, false, nil
	}

	store, err := cas.OpenDefault(ctx, cfg.Storage.Root)
	if err != nil {
		return nil, false, fmt.Errorf("open cas store: %w", err)
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}()

	obj, err := store.Put(ctx, bytes.NewReader(raw), "application/json", []string{"orchestration", "board-get"})
	if err != nil {
		return nil, false, fmt.Errorf("store board artifact: %w", err)
	}

	return map[string]any{
		"summary":      fmt.Sprintf("orchestration board payload moved to CAS (%d bytes, %d cards)", len(raw), countBoardCards(board.Lanes)),
		"artifact":     obj.Digest,
		"hint":         fmt.Sprintf("Use GET /api/cas/%s to read the full payload", obj.Digest),
		"generated_at": board.GeneratedAt,
		"counts":       board.Counts,
	}, true, nil
}

func countBoardCards(lanes []coreorchestration.LaneColumn) int {
	total := 0
	for _, lane := range lanes {
		total += len(lane.Cards)
	}
	return total
}

func writeOrchestrationServiceError(w http.ResponseWriter, command string, err error) {
	status := http.StatusInternalServerError
	code := "ERUNTIME"
	msg := "request failed"
	data := map[string]any{
		"hint": httpErrorHint(status),
	}

	var verr *v2errors.V2Error
	if errors.As(err, &verr) {
		status = verr.HTTPStatus()
		code = verr.EnvelopeCode()
		if strings.TrimSpace(verr.Message) != "" {
			msg = strings.TrimSpace(verr.Message)
		} else if strings.TrimSpace(verr.Error()) != "" {
			msg = strings.TrimSpace(verr.Error())
		}
		data["kind"] = string(verr.Kind)
		if len(verr.Details) > 0 {
			data["details"] = verr.Details
		}
		data["hint"] = httpErrorHint(status)
	} else if strings.TrimSpace(err.Error()) != "" {
		msg = strings.TrimSpace(err.Error())
	}

	writeCommandError(w, status, command, code, msg, data)
}

func writeCommandOK(w http.ResponseWriter, status int, command string, data any) {
	writeJSON(w, status, map[string]any{
		"version": envelope.Version,
		"status":  envelope.StatusOK,
		"command": command,
		"data":    data,
		"meta": map[string]any{
			"ts": time.Now().UTC().Format(time.RFC3339),
		},
		"error": map[string]any{
			"code":    nil,
			"message": nil,
		},
	})
}

func writeCommandError(w http.ResponseWriter, status int, command, code, message string, data any) {
	writeJSON(w, status, map[string]any{
		"version": envelope.Version,
		"status":  envelope.StatusError,
		"command": command,
		"data":    data,
		"meta": map[string]any{
			"ts": time.Now().UTC().Format(time.RFC3339),
		},
		"error": map[string]any{
			"code":    strings.TrimSpace(code),
			"message": strings.TrimSpace(message),
		},
	})
}
