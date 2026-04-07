package agentpane

import (
	"context"
	"fmt"
	"strings"

	"github.com/jkatigb/agentctl/internal/domain/agent"
	"github.com/jkatigb/agentctl/internal/tmuxbridge"
	"github.com/jkatigb/agentctl/internal/zellijbridge"
)

var (
	newTmuxClient   = func() *tmuxbridge.Client { return tmuxbridge.New() }
	newZellijClient = func() *zellijbridge.Client { return zellijbridge.New() }
)

// CreateWatchPane allocates a terminal pane for a spawned agent watch stream and
// returns the normalized terminal binding plus backend-specific metadata.
func CreateWatchPane(ctx context.Context, binding agent.TerminalBinding, workspaceRoot, label, watchCommand string) (agent.TerminalBinding, map[string]any, error) {
	if strings.TrimSpace(binding.Backend) == "" || strings.TrimSpace(binding.Session) == "" {
		return agent.TerminalBinding{}, nil, nil
	}
	binding = agent.NormalizeTerminalBinding(binding)
	switch binding.Backend {
	case "tmux":
		result, err := newTmuxClient().CreatePane(ctx, tmuxbridge.CreatePaneOptions{
			Session:           binding.Session,
			CWD:               strings.TrimSpace(workspaceRoot),
			Label:             strings.TrimSpace(label),
			Command:           strings.TrimSpace(watchCommand),
			ParticipantID:     binding.ParticipantID,
			ParentParticipant: binding.ParentParticipantID,
			ParentAgentID:     binding.ParentAgentID,
			RoomID:            binding.RoomID,
			RoomAccess:        binding.RoomAccess,
		})
		if err != nil {
			return binding, nil, err
		}
		binding.Session = result.Session
		binding.PaneID = result.Pane.ID
		if strings.TrimSpace(binding.ParticipantID) == "" {
			binding.ParticipantID = "tmux:" + result.Session + ":" + result.Pane.ID
		}
		binding = agent.NormalizeTerminalBinding(binding)
		return binding, map[string]any{
			"backend":        "tmux",
			"pane":           result.Pane,
			"attach_command": result.AttachCommand,
			"socket_mode":    result.SocketMode,
		}, nil
	case "zellij":
		name := strings.TrimSpace(binding.PaneID)
		if name == "" {
			name = strings.TrimSpace(label)
		}
		if name == "" {
			return binding, nil, fmt.Errorf("zellij watch pane requires pane name")
		}
		result, err := newZellijClient().CreatePane(ctx, zellijbridge.CreatePaneOptions{
			Session:           binding.Session,
			CWD:               strings.TrimSpace(workspaceRoot),
			Name:              name,
			Command:           strings.TrimSpace(watchCommand),
			ParticipantID:     binding.ParticipantID,
			ParentParticipant: binding.ParentParticipantID,
			ParentAgentID:     binding.ParentAgentID,
			RoomID:            binding.RoomID,
			RoomAccess:        binding.RoomAccess,
		})
		if err != nil {
			return binding, nil, err
		}
		binding.Session = result.Session
		binding.PaneID = result.PaneName
		if strings.TrimSpace(binding.ParticipantID) == "" {
			binding.ParticipantID = result.ParticipantID
		}
		binding = agent.NormalizeTerminalBinding(binding)
		return binding, map[string]any{
			"backend":        "zellij",
			"session":        result.Session,
			"pane_name":      result.PaneName,
			"participant_id": result.ParticipantID,
		}, nil
	default:
		return binding, nil, fmt.Errorf("unsupported mux backend %q", binding.Backend)
	}
}

// InheritChildBinding derives a child-private binding from a parent binding.
func InheritChildBinding(parent agent.TerminalBinding, parentParticipant, parentAgentID string) agent.TerminalBinding {
	parent = agent.NormalizeTerminalBinding(parent)
	if parent == (agent.TerminalBinding{}) {
		return agent.TerminalBinding{}
	}
	return agent.NormalizeTerminalBinding(agent.TerminalBinding{
		Backend:             parent.Backend,
		Session:             parent.Session,
		ParentParticipantID: firstNonEmpty(strings.TrimSpace(parentParticipant), strings.TrimSpace(parent.ParticipantID)),
		ParentAgentID:       strings.TrimSpace(parentAgentID),
		RoomAccess:          "none",
	})
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
