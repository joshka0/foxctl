package chatadapter

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/web/consolews"
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
	ChatProfile      string // consolews session profile (default "explorer")
	ChatSystemPrompt string // optional override
	Clock            Clock  // optional; defaults to realClock{}
}

// SessionBridge maps chat channels/conversations to consolews sessions for natural language chat.
type SessionBridge struct {
	consoleHub *consolews.Hub
	typing     TypingIndicator
	cfg        SessionBridgeConfig
	clock      Clock

	channelSessions sync.Map // channelKey -> sessionID (string)
	activeRequests  sync.Map // channelKey -> context.CancelFunc
}

// NewSessionBridge creates a SessionBridge wired to the given console hub and typing adapter.
func NewSessionBridge(hub *consolews.Hub, typing TypingIndicator, cfg SessionBridgeConfig) *SessionBridge {
	clk := cfg.Clock
	if clk == nil {
		clk = realClock{}
	}
	return &SessionBridge{
		consoleHub: hub,
		typing:     typing,
		cfg:        cfg,
		clock:      clk,
	}
}

// HandleMessage processes a natural language message by routing it through a consolews session.
func (sb *SessionBridge) HandleMessage(ctx context.Context, evt MessageEvent) error {
	if ctx == nil {
		ctx = context.Background()
	}

	if sb.typing != nil {
		sb.typing.ShowTyping(ctx, evt.ChannelID)
	}

	session, err := sb.GetOrCreateSession(ctx, evt.ChannelID)
	if err != nil {
		_, _ = evt.Respond("Failed to create chat session.", nil)
		return err
	}

	// Cancel any previous in-flight request for this channel key.
	if v, ok := sb.activeRequests.LoadAndDelete(evt.ChannelID); ok {
		if cancelFn, ok := v.(context.CancelFunc); ok && cancelFn != nil {
			cancelFn()
		}
	}

	// Create cancellable context for this request.
	reqCtx, reqCancel := context.WithCancel(ctx)
	sb.activeRequests.Store(evt.ChannelID, reqCancel)
	defer func() {
		sb.activeRequests.Delete(evt.ChannelID)
		reqCancel()
	}()

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
	payload := consolews.Payload{
		Type:    consolews.PayloadTypeAsk,
		Content: evt.Content,
		Metadata: map[string]any{
			platform + "_user":    evt.User.Username,
			platform + "_channel": evt.ChannelID,
		},
	}
	session.HandlePayload(reqCtx, nil, payload)

	return sb.collectAndUpdate(reqCtx, ch, ref, evt)
}

// GetOrCreateSession returns the consolews session for the channel key, creating one if needed.
func (sb *SessionBridge) GetOrCreateSession(ctx context.Context, channelKey string) (*consolews.Session, error) {
	if strings.TrimSpace(channelKey) == "" {
		return nil, errors.New("missing channel key")
	}
	if sb.consoleHub == nil {
		return nil, errors.New("console hub not configured")
	}

	// Check existing mapping.
	if sidVal, ok := sb.channelSessions.Load(channelKey); ok {
		if sid, ok := sidVal.(string); ok && sid != "" {
			if session := sb.consoleHub.GetSession(sid); session != nil {
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

	session := sb.consoleHub.CreateSession(consolews.SessionConfig{
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
func (sb *SessionBridge) collectAndUpdate(ctx context.Context, ch <-chan consolews.Payload, ref MessageRef, evt MessageEvent) error {
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
			case consolews.PayloadTypeEvent:
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

			case consolews.PayloadTypeReply:
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
