package console

import (
	"context"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// SessionPersistence is the canonical persistence contract for console
// session lifecycle and turns, independent of any specific transport.
type SessionPersistence interface {
	CreateSession(ctx context.Context, s Session) error
	SaveTurn(ctx context.Context, sessionID string, msg Message, turnIndex int) error
	EndSession(ctx context.Context, sessionID string, status string) error
}

// SessionsStorePersistence persists console sessions into the shared sessions
// store.
type SessionsStorePersistence struct {
	storageRoot string
}

// NewSessionPersistence creates the canonical sessions-store persistence
// adapter for console session lifecycle and turn storage.
func NewSessionPersistence(storageRoot string) *SessionsStorePersistence {
	return &SessionsStorePersistence{storageRoot: storageRoot}
}

// CreateSession creates a persistent session record for a console session.
func (p *SessionsStorePersistence) CreateSession(ctx context.Context, s Session) error {
	store, err := sessions.Open(ctx, p.storageRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	info := s.Info()
	session := storage.Session{
		ID:            info.ID,
		WorkspacePath: info.Workspace,
		AgentID:       "console",
		Status:        storage.SessionStatusRunning,
		StartedAt:     info.Created,
		CreatedAt:     info.Created,
	}

	_, err = store.Save(ctx, session)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("console.create_session_failed").
			WithComponent("console").
			WithSession(info.ID, "").
			Error(err, 0))
		return err
	}

	observability.Emit(ctx, observability.NewEvent("console.session_persisted").
		WithComponent("console").
		WithSession(info.ID, "").
		Success(0))
	return nil
}

// SaveTurn saves a conversation turn to the persistent session.
func (p *SessionsStorePersistence) SaveTurn(ctx context.Context, sessionID string, msg Message, turnIndex int) error {
	store, err := sessions.Open(ctx, p.storageRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	var toolCalls []storage.ToolCall
	if msg.Metadata != nil {
		if tc, ok := msg.Metadata["tool_calls"].([]any); ok {
			for _, t := range tc {
				if tcMap, ok := t.(map[string]any); ok {
					toolCall := storage.ToolCall{
						Name: metadataString(tcMap, "name"),
					}
					toolCalls = append(toolCalls, toolCall)
				}
			}
		}
	}

	var filesTouched []string
	if msg.Metadata != nil {
		if files, ok := msg.Metadata["files_touched"].([]any); ok {
			for _, f := range files {
				if s, ok := f.(string); ok {
					filesTouched = append(filesTouched, s)
				}
			}
		}
	}

	turn := storage.SessionTurn{
		SessionID:      sessionID,
		TurnIndex:      turnIndex,
		Role:           msg.Role,
		ContentPreview: truncateContent(msg.Content, 500),
		Timestamp:      time.UnixMilli(msg.Timestamp),
		ToolCalls:      toolCalls,
		FilesTouched:   filesTouched,
	}

	_, err = store.SaveTurn(ctx, turn)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("console.save_turn_failed").
			WithComponent("console").
			WithSession(sessionID, "").
			WithData("turn_index", turnIndex).
			Error(err, 0))
		return err
	}

	return nil
}

// EndSession marks a session as ended.
func (p *SessionsStorePersistence) EndSession(ctx context.Context, sessionID string, status string) error {
	store, err := sessions.Open(ctx, p.storageRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	if status == "" {
		status = storage.SessionStatusOK
	}

	err = store.SetStatus(ctx, sessionID, status)
	if err != nil {
		observability.Emit(ctx, observability.NewEvent("console.end_session_failed").
			WithComponent("console").
			WithSession(sessionID, "").
			WithData("status", status).
			Error(err, 0))
		return err
	}

	observability.Emit(ctx, observability.NewEvent("console.session_ended").
		WithComponent("console").
		WithSession(sessionID, "").
		WithData("status", status).
		Success(0))
	return nil
}

func metadataString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func truncateContent(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
