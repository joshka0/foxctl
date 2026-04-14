package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/rs/zerolog"

	"github.com/joshka0/foxctl/internal/domain/agent"
	"github.com/joshka0/foxctl/internal/platform/config"
	"github.com/joshka0/foxctl/internal/runtime/terminal/agentpane"
	"github.com/joshka0/foxctl/internal/runtime/terminal/tmuxbridge"
)

type muxPaneResponse struct {
	Backend        string `json:"backend"`
	ID             string `json:"id,omitempty"`
	Session        string `json:"session"`
	SessionPane    string `json:"session_pane,omitempty"`
	PaneName       string `json:"pane_name,omitempty"`
	Label          string `json:"label,omitempty"`
	ParticipantID  string `json:"participant_id,omitempty"`
	Provider       string `json:"provider,omitempty"`
	RoomID         string `json:"room_id,omitempty"`
	CurrentCommand string `json:"current_command,omitempty"`
	DisplayCommand string `json:"display_command,omitempty"`
	Wrapped        bool   `json:"wrapped,omitempty"`
	SocketPath     string `json:"socket_path,omitempty"`
	ReadyPath      string `json:"ready_path,omitempty"`
	State          string `json:"state,omitempty"`
	Active         bool   `json:"active,omitempty"`
}

var newMuxTMUXClient = func() *tmuxbridge.Client {
	return tmuxbridge.New()
}

func MuxReadHandler(_ config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		backend := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("backend")))
		if backend == "" {
			backend = "tmux"
		}
		target := strings.TrimSpace(r.URL.Query().Get("target"))
		if target == "" {
			httpError(w, http.StatusBadRequest, "target required")
			return
		}
		lines := 80
		if raw := strings.TrimSpace(r.URL.Query().Get("lines")); raw != "" {
			if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
				lines = parsed
			}
		}
		switch backend {
		case "tmux":
			client := newMuxTMUXClient()
			capture, err := client.Read(r.Context(), target, lines)
			if err != nil {
				log.Error().Err(err).Str("target", target).Msg("failed to read tmux pane")
				httpError(w, http.StatusInternalServerError, "failed to read mux pane")
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"backend": "tmux",
				"capture": capture,
			})
		default:
			httpError(w, http.StatusBadRequest, "mux read currently supports backend=tmux only")
		}
	}
}

func MuxPanesHandler(_ config.Config, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			httpError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		backend := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("backend")))
		if backend == "" {
			backend = "tmux"
		}
		switch backend {
		case "tmux":
			client := newMuxTMUXClient()
			panes, err := client.List(r.Context())
			if err != nil {
				log.Error().Err(err).Msg("failed to list tmux panes")
				httpError(w, http.StatusInternalServerError, "failed to list tmux panes")
				return
			}
			resp := make([]muxPaneResponse, 0, len(panes))
			for _, pane := range panes {
				socketPath := ""
				readyPath := ""
				state := ""
				if pane.Wrapped && strings.TrimSpace(pane.Session) != "" && strings.TrimSpace(pane.ParticipantID) != "" {
					socketPath = agentpane.DefaultSocketPath(pane.Session, pane.ParticipantID)
					readyPath = agentpane.DefaultReadyPath(pane.Session, pane.ParticipantID)
					readyExists := false
					if _, err := os.Stat(readyPath); err == nil {
						readyExists = true
					}
					state = string(agent.StateStopped)
					if agentpane.SocketReachable(socketPath) {
						if readyExists {
							state = string(agent.StateRunning)
						} else {
							state = string(agent.StateStarting)
						}
					}
				}
				resp = append(resp, muxPaneResponse{
					Backend:        "tmux",
					ID:             pane.ID,
					Session:        pane.Session,
					SessionPane:    pane.SessionPane,
					Label:          pane.Label,
					ParticipantID:  pane.ParticipantID,
					Provider:       pane.Provider,
					RoomID:         pane.RoomID,
					CurrentCommand: pane.CurrentCommand,
					DisplayCommand: pane.DisplayCommand,
					Wrapped:        pane.Wrapped,
					SocketPath:     socketPath,
					ReadyPath:      readyPath,
					State:          state,
					Active:         pane.Active,
				})
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"backend": "tmux",
				"count":   len(resp),
				"panes":   resp,
			})
		case "zellij":
			session := strings.TrimSpace(r.URL.Query().Get("session"))
			if session == "" {
				httpError(w, http.StatusBadRequest, "session required for zellij")
				return
			}
			resp := scanZellijPaneSockets(session)
			writeJSON(w, http.StatusOK, map[string]any{
				"backend": "zellij",
				"count":   len(resp),
				"panes":   resp,
			})
		default:
			httpError(w, http.StatusBadRequest, "unsupported backend")
		}
	}
}

func scanZellijPaneSockets(session string) []muxPaneResponse {
	session = strings.TrimSpace(session)
	if session == "" {
		return nil
	}
	dir := filepath.Dir(agentpane.DefaultSocketPath(session, "__scan__"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]muxPaneResponse, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSpace(entry.Name())
		if !strings.HasSuffix(name, ".sock") {
			continue
		}
		socketPath := filepath.Join(dir, name)
		participantID := strings.TrimSuffix(name, ".sock")
		roomID := ""
		readyPath := agentpane.DefaultReadyPath(session, participantID)
		if meta, err := agentpane.ReadMetadata(agentpane.MetadataPathForSocket(socketPath)); err == nil {
			if id := strings.TrimSpace(meta.ParticipantID); id != "" {
				participantID = id
			}
			if rid := strings.TrimSpace(meta.RoomID); rid != "" {
				roomID = rid
			}
			if rp := strings.TrimSpace(meta.ReadyPath); rp != "" {
				readyPath = rp
			}
		}
		state := string(agent.StateStopped)
		readyExists := false
		if _, err := os.Stat(readyPath); err == nil {
			readyExists = true
		}
		if agentpane.SocketReachable(socketPath) {
			if readyExists {
				state = string(agent.StateRunning)
			} else {
				state = string(agent.StateStarting)
			}
		}
		out = append(out, muxPaneResponse{
			Backend:        "zellij",
			Session:        session,
			PaneName:       participantID,
			ParticipantID:  participantID,
			RoomID:         roomID,
			DisplayCommand: participantID,
			Wrapped:        true,
			SocketPath:     socketPath,
			ReadyPath:      readyPath,
			State:          state,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].PaneName < out[j].PaneName
	})
	return out
}
