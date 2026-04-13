package chatadapter

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	consolepkg "github.com/jkatigb/agentctl/internal/console"
	"github.com/jkatigb/agentctl/internal/context/companion"
	"github.com/jkatigb/agentctl/internal/domain/identity"
	"github.com/jkatigb/agentctl/internal/observability"
)

// Clock provides time operations for testability.
type Clock interface {
	Now() time.Time
}

// realClock implements Clock using the standard time package.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// TypingIndicator is implemented by platform adapters that can show a typing indicator.
type TypingIndicator interface {
	ShowTyping(ctx context.Context, channelID string)
}

// SessionBridgeConfig configures the generic SessionBridge.
type SessionBridgeConfig struct {
	PlatformName     string // "discord" | "telegram" | "teams" (observability + metadata)
	MaxMessageLen    int    // Discord: 2000, Telegram: 4096, Teams: 4000
	EditIntervalMS   int    // streaming edit cadence (default 1500ms)
	ChatProfile      string // console session profile (default "explorer")
	ChatSystemPrompt string // optional override
	Clock            Clock  // optional; defaults to realClock{}
}

// SessionBridge maps chat channels/conversations to console sessions for natural language chat.
type SessionBridge struct {
	consoleSessions consolepkg.SessionManager
	typing          TypingIndicator
	cfg             SessionBridgeConfig
	clock           Clock

	turnLock companion.Locker

	channelSessions sync.Map // channelKey -> sessionID (string)
}

// NewSessionBridge creates a SessionBridge wired to the given console session
// manager, typing adapter, and turn locker.
func NewSessionBridge(hub consolepkg.SessionManager, typing TypingIndicator, cfg SessionBridgeConfig, turnLock companion.Locker) *SessionBridge {
	clk := cfg.Clock
	if clk == nil {
		clk = realClock{}
	}
	if turnLock == nil {
		turnLock = companion.NewTurnLock()
	}
	return &SessionBridge{
		consoleSessions: hub,
		typing:          typing,
		cfg:             cfg,
		clock:           clk,
		turnLock:        turnLock,
	}
}

// HandleMessage processes a natural language message by routing it through a
// managed console session.
func (sb *SessionBridge) HandleMessage(ctx context.Context, evt MessageEvent) error {
	if ctx == nil {
		ctx = context.Background()
	}

	principal := evt.Principal
	if strings.TrimSpace(principal.Platform) == "" {
		// Fallback for tests or callers that didn't populate Principal yet.
		platform := strings.TrimSpace(sb.cfg.PlatformName)
		if platform == "" {
			platform = "chat"
		}
		principal.Platform = platform
	}
	ctx = identity.WithPrincipal(ctx, principal)
	channelKey := principal.ConversationKey(evt.ChannelID)

	unlock, err := sb.turnLock.Lock(ctx, channelKey)
	if err != nil {
		return err
	}
	defer unlock()

	if sb.typing != nil {
		sb.typing.ShowTyping(ctx, evt.ChannelID)
	}

	session, err := sb.GetOrCreateSession(ctx, channelKey)
	if err != nil {
		_, _ = evt.Respond("Failed to create chat session.", nil)
		return err
	}

	// Create cancellable context for this request.
	reqCtx, reqCancel := context.WithCancel(ctx)
	defer reqCancel()

	// Post initial "Thinking..." message.
	ref, err := evt.Respond("Thinking...", nil)
	if err != nil {
		return err
	}

	// Subscribe to session events.
	ch, unsub := session.Subscribe(64)
	defer unsub()

	platform := strings.TrimSpace(sb.cfg.PlatformName)
	if platform == "" {
		platform = "chat"
	}
	session.HandleAsk(reqCtx, consolepkg.AskRequest{
		Content: evt.Content,
		Metadata: map[string]any{
			platform + "_user":    evt.User.Username,
			platform + "_channel": evt.ChannelID,
		},
	})

	return sb.collectAndUpdate(reqCtx, ch, ref, evt)
}

// GetOrCreateSession returns the console session for the channel key, creating one if needed.
func (sb *SessionBridge) GetOrCreateSession(ctx context.Context, channelKey string) (consolepkg.Session, error) {
	if strings.TrimSpace(channelKey) == "" {
		return nil, errors.New("missing channel key")
	}
	if sb.consoleSessions == nil {
		return nil, errors.New("console sessions not configured")
	}

	// Check existing mapping.
	if sidVal, ok := sb.channelSessions.Load(channelKey); ok {
		if sid, ok := sidVal.(string); ok && sid != "" {
			if session := sb.consoleSessions.GetSession(sid); session != nil {
				return session, nil
			}
		}
		// Stale mapping — session was removed from hub.
		sb.channelSessions.Delete(channelKey)
	}

	profile := strings.TrimSpace(sb.cfg.ChatProfile)
	if profile == "" {
		profile = "explorer"
	}

	session := sb.consoleSessions.CreateSession(consolepkg.SessionConfig{
		Profile:      profile,
		SystemPrompt: sb.cfg.ChatSystemPrompt,
	})
	sb.channelSessions.Store(channelKey, session.ID())

	platform := strings.TrimSpace(sb.cfg.PlatformName)
	if platform == "" {
		platform = "chat"
	}
	observability.Emit(ctx, observability.NewEvent(platform+".session_created").
		WithComponent(platform).
		WithData("channel_id", channelKey).
		WithData("session_id", session.ID()).
		WithData("profile", profile).
		Success(0))

	return session, nil
}

func (sb *SessionBridge) maxMessageLen() int {
	if sb.cfg.MaxMessageLen > 0 {
		return sb.cfg.MaxMessageLen
	}
	// Conservative fallback.
	return 2000
}

func (sb *SessionBridge) editInterval() time.Duration {
	d := time.Duration(sb.cfg.EditIntervalMS) * time.Millisecond
	if d <= 0 {
		return 1500 * time.Millisecond
	}
	return d
}

// collectAndUpdate loops on the subscriber channel, accumulating text and periodically
// editing the platform message with progress.
func (sb *SessionBridge) collectAndUpdate(ctx context.Context, ch <-chan consolepkg.Event, ref MessageRef, evt MessageEvent) error {
	var accumulated strings.Builder

	editInterval := sb.editInterval()
	lastEdit := sb.clock.Now().Add(-editInterval)
	lastContent := "Thinking..."

	for {
		select {
		case <-ctx.Done():
			if accumulated.Len() > 0 {
				final := TruncateRunesWithEllipsis(accumulated.String(), sb.maxMessageLen())
				_ = evt.Edit(ref, final, nil)
			}
			return ctx.Err()

		case p, ok := <-ch:
			if !ok {
				if accumulated.Len() > 0 {
					final := TruncateRunesWithEllipsis(accumulated.String(), sb.maxMessageLen())
					_ = evt.Edit(ref, final, nil)
				} else if lastContent == "Thinking..." {
					_ = evt.Edit(ref, "Session ended without a response.", nil)
				}
				return nil
			}

			switch p.Type {
			case consolepkg.EventTypeEvent:
				if IsPartial(p.Metadata) {
					accumulated.WriteString(p.Content)
					if sb.clock.Now().Sub(lastEdit) >= editInterval {
						content := TruncateRunesWithSuffix(accumulated.String(), "...", sb.maxMessageLen())
						if content != lastContent {
							_ = evt.Edit(ref, content, nil)
							lastContent = content
							lastEdit = sb.clock.Now()
						}
					}
				} else if p.Content != "" {
					accumulated.WriteString(p.Content)
				}

			case consolepkg.EventTypeReply:
				final := p.Content
				if final == "" && accumulated.Len() > 0 {
					final = accumulated.String()
				}
				if final == "" {
					final = "No response generated."
				}
				final = TruncateRunesWithEllipsis(final, sb.maxMessageLen())
				return evt.Edit(ref, final, nil)

			default:
				if p.Content != "" {
					accumulated.WriteString(p.Content)
				}
			}
		}
	}
}
