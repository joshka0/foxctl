package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/platform/config"
	flowmodel "github.com/joshka0/foxctl/internal/runtime/flow"
	"github.com/joshka0/foxctl/internal/runtime/daemon"
	flowstore "github.com/joshka0/foxctl/internal/storage/flow"

	foxproxclient "github.com/joshka/foxprox/foxprox/client"
	"github.com/joshka/foxprox/foxprox/transport/unixsocket"
)

// ---------------------------------------------------------------------------
// DTOs
// ---------------------------------------------------------------------------

type flowCreateRequest struct {
	Name        string `json:"name"`
	Workspace   string `json:"workspace,omitempty"`
	Description string `json:"description,omitempty"`
	RoomID      string `json:"room_id,omitempty"`
}

type flowPatchRequest struct {
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	RoomID      string `json:"room_id,omitempty"`
}

type flowResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Workspace   string    `json:"workspace"`
	State       string    `json:"state"`
	Description string    `json:"description,omitempty"`
	RoomID      string    `json:"room_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type flowDetailResponse struct {
	flowResponse
	Nodes []flowNodeResponse `json:"nodes"`
	Edges []flowEdgeResponse `json:"edges"`
}

type flowNodeResponse struct {
	ID       string           `json:"id"`
	FlowID   string           `json:"flow_id"`
	Kind     string           `json:"kind"`
	Label    string           `json:"label"`
	Config   json.RawMessage  `json:"config"`
	Position *flowmodel.Position `json:"position,omitempty"`
}

type flowEdgeResponse struct {
	ID              string `json:"id"`
	FlowID          string `json:"flow_id"`
	FromNodeID      string `json:"from_node_id"`
	ToNodeID        string `json:"to_node_id"`
	Transform       string `json:"transform"`
	TransformConfig string `json:"transform_config,omitempty"`
	Trigger         string `json:"trigger"`
	TriggerConfig   string `json:"trigger_config,omitempty"`
	Condition       string `json:"condition,omitempty"`
}

type flowNodeCreateRequest struct {
	Kind     string          `json:"kind"`
	Label    string          `json:"label"`
	Config   json.RawMessage `json:"config,omitempty"`
	Position *flowmodel.Position `json:"position,omitempty"`
}

type flowEdgeCreateRequest struct {
	FromNodeID      string `json:"from_node_id"`
	ToNodeID        string `json:"to_node_id"`
	Transform       string `json:"transform,omitempty"`
	TransformConfig string `json:"transform_config,omitempty"`
	Trigger         string `json:"trigger,omitempty"`
	TriggerConfig   string `json:"trigger_config,omitempty"`
	Condition       string `json:"condition,omitempty"`
}

type flowRunLogsResponse struct {
	Logs  []runLogResponse `json:"logs"`
	Count int              `json:"count"`
}

type runLogResponse struct {
	ID        string          `json:"id"`
	RunID     string          `json:"run_id"`
	NodeID    string          `json:"node_id"`
	Seq       int             `json:"seq"`
	Envelope  json.RawMessage `json:"envelope"`
	CreatedAt time.Time       `json:"created_at"`
}

type flowStartResponse struct {
	FlowID string `json:"flow_id"`
	RunID  string `json:"run_id"`
	State  string `json:"state"`
}

type flowStatusResponse struct {
	FlowID    string                  `json:"flow_id"`
	State     string                  `json:"state"`
	RunID     string                  `json:"run_id,omitempty"`
	Nodes     []flowmodel.NodeExecState `json:"nodes"`
	Edges     []flowmodel.EdgeExecState `json:"edges"`
}

// ---------------------------------------------------------------------------
// Handler factory
// ---------------------------------------------------------------------------

// FlowHandler returns an http.HandlerFunc that routes flow CRUD and execution
// operations. Paths:
//
//	GET    /api/flows?workspace=...
//	POST   /api/flows
//	GET    /api/flows/:id
//	DELETE /api/flows/:id
//	POST   /api/flows/:id/nodes
//	DELETE /api/flows/:id/nodes/:nid
//	POST   /api/flows/:id/edges
//	DELETE /api/flows/:id/edges/:eid
//	POST   /api/flows/:id/start
//	POST   /api/flows/:id/stop
//	POST   /api/flows/:id/pause
//	GET    /api/flows/:id/status
//	GET    /api/flows/:id/runs/:rid/logs
func FlowHandler(cfg config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/flows")
		path = strings.TrimPrefix(path, "/")

		// No ID — collection routes.
		if path == "" || path == "/" {
			switch r.Method {
			case http.MethodGet:
				handleFlowList(w, r, cfg, log)
				return
			case http.MethodPost:
				handleFlowCreate(w, r, cfg, log)
				return
			default:
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
		}

		parts := strings.Split(path, "/")
		if len(parts) == 0 || parts[0] == "" {
			httpError(w, http.StatusNotFound, "not found")
			return
		}

		flowID := parts[0]

		// Sub-resource routing.
		if len(parts) >= 2 {
			switch parts[1] {
			case "nodes":
				if len(parts) == 2 {
					if r.Method == http.MethodPost {
						handleFlowAddNode(w, r, cfg, log, flowID)
						return
					}
					httpError(w, http.StatusMethodNotAllowed, "method not allowed")
					return
				}
				if len(parts) == 3 && r.Method == http.MethodDelete {
					handleFlowRemoveNode(w, r, cfg, log, flowID, parts[2])
					return
				}
				if len(parts) == 4 && parts[3] == "terminal" && r.Method == http.MethodGet {
					handleFlowNodeTerminal(w, r, cfg, log, flowID, parts[2])
					return
				}
			case "edges":
				if len(parts) == 2 {
					if r.Method == http.MethodPost {
						handleFlowAddEdge(w, r, cfg, log, flowID)
						return
					}
					httpError(w, http.StatusMethodNotAllowed, "method not allowed")
					return
				}
				if len(parts) == 3 && r.Method == http.MethodDelete {
					handleFlowRemoveEdge(w, r, cfg, log, flowID, parts[2])
					return
				}
			case "start":
				if len(parts) == 2 && r.Method == http.MethodPost {
					handleFlowStart(w, r, cfg, log, flowID)
					return
				}
			case "stop":
				if len(parts) == 2 && r.Method == http.MethodPost {
					handleFlowStop(w, r, cfg, log, flowID)
					return
				}
			case "pause":
				if len(parts) == 2 && r.Method == http.MethodPost {
					handleFlowPause(w, r, cfg, log, flowID)
					return
				}
			case "status":
				if len(parts) == 2 && r.Method == http.MethodGet {
					handleFlowStatus(w, r, cfg, log, flowID)
					return
				}
			case "runs":
				if len(parts) == 4 && parts[2] != "" && parts[3] == "logs" && r.Method == http.MethodGet {
					handleFlowRunLogs(w, r, cfg, log, flowID, parts[2])
					return
				}
			}
		}

		// Single flow routes.
		if len(parts) == 1 {
			switch r.Method {
			case http.MethodGet:
				handleFlowShow(w, r, cfg, log, flowID)
				return
			case http.MethodPatch:
				handleFlowPatch(w, r, cfg, log, flowID)
				return
			case http.MethodDelete:
				handleFlowDelete(w, r, cfg, log, flowID)
				return
			default:
				httpError(w, http.StatusMethodNotAllowed, "method not allowed")
				return
			}
		}

		httpError(w, http.StatusNotFound, "not found")
	}
}

// ---------------------------------------------------------------------------
// Store helper
// ---------------------------------------------------------------------------

func openFlowStore(ctx context.Context, cfg config.Config) (flowmodel.Store, error) {
	return flowstore.Open(ctx, cfg.Storage.Root)
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

func handleFlowList(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	workspace := r.URL.Query().Get("workspace")
	if workspace == "" {
		workspace = "."
	}

	store, err := openFlowStore(r.Context(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("flow: open store")
		httpError(w, http.StatusInternalServerError, "flow store unavailable")
		return
	}
	defer store.Close()

	flows, err := store.ListFlows(r.Context(), workspace)
	if err != nil {
		log.Error().Err(err).Msg("flow: list")
		httpError(w, http.StatusInternalServerError, "failed to list flows")
		return
	}

	resp := make([]flowResponse, len(flows))
	for i, f := range flows {
		resp[i] = toFlowResponse(f)
	}
	writeJSON(w, http.StatusOK, map[string]any{"flows": resp, "count": len(resp)})
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

func handleFlowCreate(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger) {
	var req flowCreateRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Name) == "" {
		httpError(w, http.StatusBadRequest, "name is required")
		return
	}

	workspace := strings.TrimSpace(req.Workspace)
	if workspace == "" {
		workspace = "."
	}

	now := time.Now().UTC()
	fl := flowmodel.Flow{
		ID:        ulid.Make().String(),
		Name:      strings.TrimSpace(req.Name),
		Workspace: workspace,
		State:     flowmodel.FlowDraft,
		Description: strings.TrimSpace(req.Description),
		RoomID:    strings.TrimSpace(req.RoomID),
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := fl.Validate(); err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}

	store, err := openFlowStore(r.Context(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("flow: open store")
		httpError(w, http.StatusInternalServerError, "flow store unavailable")
		return
	}
	defer store.Close()

	created, err := store.CreateFlow(r.Context(), fl)
	if err != nil {
		log.Error().Err(err).Msg("flow: create")
		httpError(w, http.StatusInternalServerError, "failed to create flow")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"flow": toFlowResponse(created)})
}

// ---------------------------------------------------------------------------
// Show
// ---------------------------------------------------------------------------

func handleFlowShow(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID string) {
	store, err := openFlowStore(r.Context(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("flow: open store")
		httpError(w, http.StatusInternalServerError, "flow store unavailable")
		return
	}
	defer store.Close()

	fl, err := store.GetFlow(r.Context(), flowID)
	if err != nil {
		if errors.Is(err, flowmodel.ErrNotFound) {
			httpError(w, http.StatusNotFound, "flow not found")
			return
		}
		log.Error().Err(err).Msg("flow: get")
		httpError(w, http.StatusInternalServerError, "failed to get flow")
		return
	}

	nodes, err := store.ListNodesByFlow(r.Context(), flowID)
	if err != nil {
		log.Error().Err(err).Msg("flow: list nodes")
		httpError(w, http.StatusInternalServerError, "failed to list nodes")
		return
	}

	edges, err := store.ListEdgesByFlow(r.Context(), flowID)
	if err != nil {
		log.Error().Err(err).Msg("flow: list edges")
		httpError(w, http.StatusInternalServerError, "failed to list edges")
		return
	}

	resp := flowDetailResponse{
		flowResponse: toFlowResponse(fl),
		Nodes:        make([]flowNodeResponse, len(nodes)),
		Edges:        make([]flowEdgeResponse, len(edges)),
	}
	for i, n := range nodes {
		resp.Nodes[i] = toFlowNodeResponse(n)
	}
	for i, e := range edges {
		resp.Edges[i] = toFlowEdgeResponse(e)
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Patch
// ---------------------------------------------------------------------------

func handleFlowPatch(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID string) {
	var req flowPatchRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	store, err := openFlowStore(r.Context(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("flow: open store")
		httpError(w, http.StatusInternalServerError, "flow store unavailable")
		return
	}
	defer store.Close()

	fl, err := store.GetFlow(r.Context(), flowID)
	if err != nil {
		if errors.Is(err, flowmodel.ErrNotFound) {
			httpError(w, http.StatusNotFound, "flow not found")
			return
		}
		log.Error().Err(err).Msg("flow: get")
		httpError(w, http.StatusInternalServerError, "failed to get flow")
		return
	}

	if req.Name != "" {
		fl.Name = strings.TrimSpace(req.Name)
	}
	if req.Description != "" {
		fl.Description = strings.TrimSpace(req.Description)
	}
	fl.RoomID = strings.TrimSpace(req.RoomID) // allow clearing

	updated, err := store.UpdateFlow(r.Context(), fl)
	if err != nil {
		log.Error().Err(err).Msg("flow: update")
		httpError(w, http.StatusInternalServerError, "failed to update flow")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"flow": toFlowResponse(updated)})
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func handleFlowDelete(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID string) {
	store, err := openFlowStore(r.Context(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("flow: open store")
		httpError(w, http.StatusInternalServerError, "flow store unavailable")
		return
	}
	defer store.Close()

	if err := store.DeleteFlow(r.Context(), flowID); err != nil {
		if errors.Is(err, flowmodel.ErrNotFound) {
			httpError(w, http.StatusNotFound, "flow not found")
			return
		}
		log.Error().Err(err).Msg("flow: delete")
		httpError(w, http.StatusInternalServerError, "failed to delete flow")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": flowID})
}

// ---------------------------------------------------------------------------
// Add Node
// ---------------------------------------------------------------------------

func handleFlowAddNode(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID string) {
	var req flowNodeCreateRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.Label) == "" {
		httpError(w, http.StatusBadRequest, "label is required")
		return
	}
	if !flowmodel.NodeKind(req.Kind).IsValid() {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("invalid node kind: %s", req.Kind))
		return
	}

	node := flowmodel.FlowNode{
		ID:       ulid.Make().String(),
		FlowID:   flowID,
		Kind:     flowmodel.NodeKind(req.Kind),
		Label:    strings.TrimSpace(req.Label),
		Config:   req.Config,
		Position: req.Position,
	}

	store, err := openFlowStore(r.Context(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("flow: open store")
		httpError(w, http.StatusInternalServerError, "flow store unavailable")
		return
	}
	defer store.Close()

	// Verify flow exists.
	if _, err := store.GetFlow(r.Context(), flowID); err != nil {
		if errors.Is(err, flowmodel.ErrNotFound) {
			httpError(w, http.StatusNotFound, "flow not found")
			return
		}
		log.Error().Err(err).Msg("flow: get")
		httpError(w, http.StatusInternalServerError, "failed to verify flow")
		return
	}

	created, err := store.AddNode(r.Context(), node)
	if err != nil {
		log.Error().Err(err).Msg("flow: add node")
		httpError(w, http.StatusInternalServerError, "failed to add node")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"node": toFlowNodeResponse(created)})
}

// ---------------------------------------------------------------------------
// Remove Node
// ---------------------------------------------------------------------------

func handleFlowRemoveNode(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID, nodeID string) {
	store, err := openFlowStore(r.Context(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("flow: open store")
		httpError(w, http.StatusInternalServerError, "flow store unavailable")
		return
	}
	defer store.Close()

	if err := store.RemoveNode(r.Context(), nodeID); err != nil {
		if errors.Is(err, flowmodel.ErrNotFound) {
			httpError(w, http.StatusNotFound, "node not found")
			return
		}
		log.Error().Err(err).Msg("flow: remove node")
		httpError(w, http.StatusInternalServerError, "failed to remove node")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": nodeID})
}

// ---------------------------------------------------------------------------
// Add Edge
// ---------------------------------------------------------------------------

func handleFlowAddEdge(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID string) {
	var req flowEdgeCreateRequest
	if err := readJSON(w, r, &req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if strings.TrimSpace(req.FromNodeID) == "" || strings.TrimSpace(req.ToNodeID) == "" {
		httpError(w, http.StatusBadRequest, "from_node_id and to_node_id are required")
		return
	}

	transform := flowmodel.TransformKind(req.Transform)
	if transform == "" {
		transform = flowmodel.TransformPassthrough
	}
	if !transform.IsValid() {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("invalid transform: %s", req.Transform))
		return
	}

	trigger := flowmodel.TriggerKind(req.Trigger)
	if trigger == "" {
		trigger = flowmodel.TriggerOutputReady
	}
	if !trigger.IsValid() {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("invalid trigger: %s", req.Trigger))
		return
	}

	edge := flowmodel.FlowEdge{
		ID:              ulid.Make().String(),
		FlowID:          flowID,
		FromNodeID:      strings.TrimSpace(req.FromNodeID),
		ToNodeID:        strings.TrimSpace(req.ToNodeID),
		Transform:       transform,
		TransformConfig: req.TransformConfig,
		Trigger:         trigger,
		TriggerConfig:   req.TriggerConfig,
		Condition:       req.Condition,
	}

	store, err := openFlowStore(r.Context(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("flow: open store")
		httpError(w, http.StatusInternalServerError, "flow store unavailable")
		return
	}
	defer store.Close()

	// Verify flow exists.
	if _, err := store.GetFlow(r.Context(), flowID); err != nil {
		if errors.Is(err, flowmodel.ErrNotFound) {
			httpError(w, http.StatusNotFound, "flow not found")
			return
		}
		log.Error().Err(err).Msg("flow: get")
		httpError(w, http.StatusInternalServerError, "failed to verify flow")
		return
	}

	created, err := store.AddEdge(r.Context(), edge)
	if err != nil {
		log.Error().Err(err).Msg("flow: add edge")
		httpError(w, http.StatusInternalServerError, "failed to add edge")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"edge": toFlowEdgeResponse(created)})
}

// ---------------------------------------------------------------------------
// Remove Edge
// ---------------------------------------------------------------------------

func handleFlowRemoveEdge(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID, edgeID string) {
	store, err := openFlowStore(r.Context(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("flow: open store")
		httpError(w, http.StatusInternalServerError, "flow store unavailable")
		return
	}
	defer store.Close()

	if err := store.RemoveEdge(r.Context(), edgeID); err != nil {
		if errors.Is(err, flowmodel.ErrNotFound) {
			httpError(w, http.StatusNotFound, "edge not found")
			return
		}
		log.Error().Err(err).Msg("flow: remove edge")
		httpError(w, http.StatusInternalServerError, "failed to remove edge")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "id": edgeID})
}

// ---------------------------------------------------------------------------
// Start / Stop / Pause / Status (daemon-backed)
// ---------------------------------------------------------------------------

func handleFlowStart(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID string) {
	workspace := r.URL.Query().Get("workspace")
	if workspace == "" {
		workspace = "."
	}

	client := daemon.NewClient()
	result, err := client.FlowStart(flowID, workspace)
	if err != nil {
		log.Error().Err(err).Str("flow_id", flowID).Msg("flow: start")
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, flowStartResponse{
		FlowID: flowID,
		RunID:  result.RunID,
		State:  string(flowmodel.FlowRunning),
	})
}

func handleFlowStop(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID string) {
	workspace := r.URL.Query().Get("workspace")
	if workspace == "" {
		workspace = "."
	}

	client := daemon.NewClient()
	_, err := client.FlowStop(flowID, workspace)
	if err != nil {
		log.Error().Err(err).Str("flow_id", flowID).Msg("flow: stop")
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"flow_id": flowID,
		"state":   string(flowmodel.FlowStopped),
	})
}

func handleFlowPause(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID string) {
	workspace := r.URL.Query().Get("workspace")
	if workspace == "" {
		workspace = "."
	}

	client := daemon.NewClient()
	_, err := client.FlowPause(flowID, workspace)
	if err != nil {
		log.Error().Err(err).Str("flow_id", flowID).Msg("flow: pause")
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"flow_id": flowID,
		"state":   string(flowmodel.FlowPaused),
	})
}

func handleFlowStatus(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID string) {
	workspace := r.URL.Query().Get("workspace")
	if workspace == "" {
		workspace = "."
	}

	client := daemon.NewClient()
	result, err := client.FlowStatus(flowID, workspace)
	if err != nil {
		log.Error().Err(err).Str("flow_id", flowID).Msg("flow: status")
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Also load the persisted flow state as fallback.
	store, storeErr := openFlowStore(r.Context(), cfg)
	if storeErr == nil {
		defer store.Close()
		if fl, err := store.GetFlow(r.Context(), flowID); err == nil {
			if result.State == "" {
				result.State = string(fl.State)
			}
		}
	}

	resp := flowStatusResponse{
		FlowID: flowID,
		State:  result.State,
		RunID:  result.RunID,
	}
	if result.Nodes != nil {
		resp.Nodes = result.Nodes
	} else {
		resp.Nodes = []flowmodel.NodeExecState{}
	}
	if result.Edges != nil {
		resp.Edges = result.Edges
	} else {
		resp.Edges = []flowmodel.EdgeExecState{}
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Node Terminal (foxprox screen snapshot)
// ---------------------------------------------------------------------------

func handleFlowNodeTerminal(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID, nodeID string) {
	workspace := r.URL.Query().Get("workspace")
	if workspace == "" {
		workspace = "."
	}

	// Get flow status from daemon to resolve the node's session ID.
	client := daemon.NewClient()
	status, err := client.FlowStatus(flowID, workspace)
	if err != nil {
		log.Error().Err(err).Str("flow_id", flowID).Str("node_id", nodeID).Msg("flow: terminal: status")
		httpError(w, http.StatusInternalServerError, "failed to get flow status")
		return
	}

	var sessionID string
	for _, n := range status.Nodes {
		if n.ID == nodeID {
			sessionID = n.SessionID
			break
		}
	}
	if sessionID == "" {
		httpError(w, http.StatusNotFound, "node has no active session")
		return
	}

	// Fetch screen snapshot from foxprox.
	fpClient := foxproxclient.ForSocket(unixsocket.DefaultSocketPath())
	snap, err := fpClient.SessionScreen(r.Context(), sessionID)
	if err != nil {
		log.Error().Err(err).Str("session_id", sessionID).Msg("flow: terminal: session screen")
		httpError(w, http.StatusInternalServerError, "failed to fetch terminal screen")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": sessionID,
		"rows":       snap.Rows,
		"cols":       snap.Cols,
		"lines":      snap.Lines,
		"cursor":     snap.Cursor,
		"alt_screen": snap.AltScreen,
	})
}

// ---------------------------------------------------------------------------
// Run Logs
// ---------------------------------------------------------------------------

func handleFlowRunLogs(w http.ResponseWriter, r *http.Request, cfg config.Config, log zerolog.Logger, flowID, runID string) {
	nodeID := r.URL.Query().Get("node_id")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	opts := []flowmodel.RunLogOption{}
	if nodeID != "" {
		opts = append(opts, flowmodel.WithNodeID(nodeID))
	}
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			opts = append(opts, flowmodel.WithLimit(n))
		}
	}
	if offsetStr != "" {
		if n, err := strconv.Atoi(offsetStr); err == nil && n >= 0 {
			opts = append(opts, flowmodel.WithOffset(n))
		}
	}

	store, err := openFlowStore(r.Context(), cfg)
	if err != nil {
		log.Error().Err(err).Msg("flow: open store")
		httpError(w, http.StatusInternalServerError, "flow store unavailable")
		return
	}
	defer store.Close()

	logs, err := store.ListRunLogs(r.Context(), runID, opts...)
	if err != nil {
		log.Error().Err(err).Msg("flow: list run logs")
		httpError(w, http.StatusInternalServerError, "failed to list run logs")
		return
	}

	resp := make([]runLogResponse, len(logs))
	for i, l := range logs {
		resp[i] = runLogResponse{
			ID:        l.ID,
			RunID:     l.RunID,
			NodeID:    l.NodeID,
			Seq:       l.Seq,
			Envelope:  l.Envelope,
			CreatedAt: l.CreatedAt,
		}
	}

	writeJSON(w, http.StatusOK, flowRunLogsResponse{Logs: resp, Count: len(resp)})
}

// ---------------------------------------------------------------------------
// Mappers
// ---------------------------------------------------------------------------

func toFlowResponse(f flowmodel.Flow) flowResponse {
	return flowResponse{
		ID:          f.ID,
		Name:        f.Name,
		Workspace:   f.Workspace,
		State:       string(f.State),
		Description: f.Description,
		RoomID:      f.RoomID,
		CreatedAt:   f.CreatedAt,
		UpdatedAt:   f.UpdatedAt,
	}
}

func toFlowNodeResponse(n flowmodel.FlowNode) flowNodeResponse {
	return flowNodeResponse{
		ID:       n.ID,
		FlowID:   n.FlowID,
		Kind:     string(n.Kind),
		Label:    n.Label,
		Config:   n.Config,
		Position: n.Position,
	}
}

func toFlowEdgeResponse(e flowmodel.FlowEdge) flowEdgeResponse {
	return flowEdgeResponse{
		ID:              e.ID,
		FlowID:          e.FlowID,
		FromNodeID:      e.FromNodeID,
		ToNodeID:        e.ToNodeID,
		Transform:       string(e.Transform),
		TransformConfig: e.TransformConfig,
		Trigger:         string(e.Trigger),
		TriggerConfig:   e.TriggerConfig,
		Condition:       e.Condition,
	}
}

