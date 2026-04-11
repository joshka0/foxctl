package cmd

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jkatigb/agentctl/internal/agentpane"
	domainagent "github.com/jkatigb/agentctl/internal/domain/agent"
	agentstore "github.com/jkatigb/agentctl/internal/storage/agents"
)

type zellijBoundPane struct {
	Session             string            `json:"session"`
	PaneName            string            `json:"pane_name"`
	ParticipantID       string            `json:"participant_id,omitempty"`
	RoomID              string            `json:"room_id,omitempty"`
	SocketPath          string            `json:"socket_path,omitempty"`
	ReadyPath           string            `json:"ready_path,omitempty"`
	Wrapped             bool              `json:"wrapped,omitempty"`
	Source              string            `json:"source,omitempty"`
	AgentID             string            `json:"agent_id"`
	ParentID            string            `json:"parent_id,omitempty"`
	Role                string            `json:"role"`
	State               domainagent.State `json:"state"`
	CreatedAt           string            `json:"created_at"`
	ParentParticipantID string            `json:"parent_participant_id,omitempty"`
	ParentAgentID       string            `json:"parent_agent_id,omitempty"`
	RoomAccess          string            `json:"room_access,omitempty"`
}

func openAgentStore(ctx context.Context, storageRoot string) (agentstore.Store, error) {
	return agentstore.Open(ctx, storageRoot)
}

func listZellijBoundPanes(all []domainagent.Agent, session string) []zellijBoundPane {
	resolvedSession := strings.TrimSpace(session)
	type paneEntry struct {
		pane      zellijBoundPane
		createdAt time.Time
	}
	byPane := make(map[string]paneEntry)
	for _, item := range all {
		binding := domainagent.NormalizeTerminalBinding(item.TerminalBinding)
		if binding.Backend != "zellij" {
			continue
		}
		if resolvedSession != "" && binding.Session != resolvedSession {
			continue
		}
		paneName := strings.TrimSpace(binding.PaneID)
		if paneName == "" {
			continue
		}
		candidate := zellijBoundPane{
			Session:             binding.Session,
			PaneName:            paneName,
			ParticipantID:       binding.ParticipantID,
			RoomID:              binding.RoomID,
			AgentID:             item.ID,
			ParentID:            item.ParentID,
			Role:                item.Role,
			State:               item.State,
			CreatedAt:           item.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
			ParentParticipantID: binding.ParentParticipantID,
			ParentAgentID:       binding.ParentAgentID,
			RoomAccess:          binding.RoomAccess,
		}
		existing, ok := byPane[paneName]
		if !ok || item.CreatedAt.After(existing.createdAt) {
			byPane[paneName] = paneEntry{
				pane:      candidate,
				createdAt: item.CreatedAt,
			}
		}
	}
	panes := make([]zellijBoundPane, 0, len(byPane))
	for _, item := range byPane {
		panes = append(panes, item.pane)
	}
	if resolvedSession != "" {
		panes = mergeZellijSocketPanes(panes, scanZellijSocketPanes(resolvedSession))
	}
	sort.Slice(panes, func(i, j int) bool {
		if panes[i].Session != panes[j].Session {
			return panes[i].Session < panes[j].Session
		}
		return panes[i].PaneName < panes[j].PaneName
	})
	return panes
}

func scanZellijSocketPanes(session string) []zellijBoundPane {
	session = strings.TrimSpace(session)
	if session == "" {
		return nil
	}
	dir := filepath.Dir(agentpane.DefaultSocketPath(session, "__scan__"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]zellijBoundPane, 0, len(entries))
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
		state := domainagent.StateStopped
		readyExists := false
		if _, err := os.Stat(readyPath); err == nil {
			readyExists = true
		}
		if agentpane.SocketReachable(socketPath) {
			if readyExists {
				state = domainagent.StateRunning
			} else {
				state = domainagent.StateStarting
			}
		}
		out = append(out, zellijBoundPane{
			Session:       session,
			PaneName:      participantID,
			ParticipantID: participantID,
			RoomID:        roomID,
			SocketPath:    socketPath,
			ReadyPath:     readyPath,
			Wrapped:       true,
			Source:        "pane_socket",
			State:         state,
		})
	}
	return out
}

func mergeZellijSocketPanes(existing, scanned []zellijBoundPane) []zellijBoundPane {
	if len(scanned) == 0 {
		return existing
	}
	byPane := make(map[string]int, len(existing))
	out := append([]zellijBoundPane(nil), existing...)
	for i := range out {
		key := strings.TrimSpace(out[i].Session) + "\x00" + strings.TrimSpace(out[i].PaneName)
		byPane[key] = i
	}
	for _, pane := range scanned {
		key := strings.TrimSpace(pane.Session) + "\x00" + strings.TrimSpace(pane.PaneName)
		if idx, ok := byPane[key]; ok {
			if strings.TrimSpace(out[idx].SocketPath) == "" {
				out[idx].SocketPath = pane.SocketPath
			}
			if strings.TrimSpace(out[idx].ReadyPath) == "" {
				out[idx].ReadyPath = pane.ReadyPath
			}
			out[idx].Wrapped = out[idx].Wrapped || pane.Wrapped
			if strings.TrimSpace(out[idx].Source) == "" {
				out[idx].Source = pane.Source
			}
			if out[idx].State == "" {
				out[idx].State = pane.State
			}
			continue
		}
		out = append(out, pane)
	}
	return out
}
