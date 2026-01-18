package consolews

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/jkatigb/agentctl/internal/storage"
	"github.com/jkatigb/agentctl/internal/storage/sessions"
)

// persistAsync runs a persistence function asynchronously with a timeout.
// The goroutine is tracked by wg for graceful shutdown, and derives its
// context from parentCtx (FC/IS compliant - no context.Background()).
func persistAsync(parentCtx context.Context, wg *sync.WaitGroup, log zerolog.Logger, name string, fn func(ctx context.Context) error) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			log.Error().Err(err).Str("op", name).Msg("persistence failed")
		}
	}()
}

// PersistenceAdapter handles saving console sessions to the sessions store.
type PersistenceAdapter struct {
	storageRoot string
	log         zerolog.Logger
}

// NewPersistenceAdapter creates a new persistence adapter.
func NewPersistenceAdapter(storageRoot string, log zerolog.Logger) *PersistenceAdapter {
	return &PersistenceAdapter{
		storageRoot: storageRoot,
		log:         log.With().Str("component", "persistence").Logger(),
	}
}

// CreateSession creates a persistent session record for a console session.
func (p *PersistenceAdapter) CreateSession(ctx context.Context, s *Session) error {
	store, err := sessions.Open(ctx, p.storageRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	// Create storage session
	session := storage.Session{
		ID:            s.ID(),
		WorkspacePath: s.Workspace(),
		AgentID:       "console",
		Status:        storage.SessionStatusRunning,
		StartedAt:     s.created,
		CreatedAt:     s.created,
	}

	_, err = store.Save(ctx, session)
	if err != nil {
		p.log.Error().Err(err).Str("session_id", s.ID()).Msg("failed to create persistent session")
		return err
	}

	p.log.Info().Str("session_id", s.ID()).Msg("created persistent session")
	return nil
}

// SaveTurn saves a conversation turn to the persistent session.
func (p *PersistenceAdapter) SaveTurn(ctx context.Context, sessionID string, msg Message, turnIndex int) error {
	store, err := sessions.Open(ctx, p.storageRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	// Build tool calls from metadata if present
	var toolCalls []storage.ToolCall
	if msg.Metadata != nil {
		if tc, ok := msg.Metadata["tool_calls"].([]any); ok {
			for _, t := range tc {
				if tcMap, ok := t.(map[string]any); ok {
					toolCall := storage.ToolCall{
						Name: getString(tcMap, "name"),
					}
					toolCalls = append(toolCalls, toolCall)
				}
			}
		}
	}

	// Build files touched from metadata
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
		ContentPreview: truncate(msg.Content, 500),
		Timestamp:      time.UnixMilli(msg.Timestamp),
		ToolCalls:      toolCalls,
		FilesTouched:   filesTouched,
	}

	_, err = store.SaveTurn(ctx, turn)
	if err != nil {
		p.log.Error().Err(err).
			Str("session_id", sessionID).
			Int("turn_index", turnIndex).
			Msg("failed to save turn")
		return err
	}

	return nil
}

// UpdateSessionStats updates the session message count and other stats.
func (p *PersistenceAdapter) UpdateSessionStats(ctx context.Context, sessionID string, messageCount, userTurns, toolInvocations int) error {
	store, err := sessions.Open(ctx, p.storageRoot)
	if err != nil {
		return err
	}
	defer store.Close()

	// Get existing session
	session, err := store.Get(ctx, sessionID)
	if err != nil {
		return err
	}

	// Update stats
	session.MessageCount = messageCount
	session.UserTurns = userTurns
	session.ToolInvocations = toolInvocations

	_, err = store.Save(ctx, session)
	return err
}

// EndSession marks a session as ended.
func (p *PersistenceAdapter) EndSession(ctx context.Context, sessionID string, status string) error {
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
		p.log.Error().Err(err).Str("session_id", sessionID).Str("status", status).Msg("failed to end session")
		return err
	}

	p.log.Info().Str("session_id", sessionID).Str("status", status).Msg("ended session")
	return nil
}

// getString safely gets a string from a map.
func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// truncate truncates a string to maxLen characters.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
