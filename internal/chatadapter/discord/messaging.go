// Package discord implements the ChatAdapter interface for Discord using discordgo.
package discord

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/chatadapter"
	"github.com/jkatigb/agentctl/internal/observability"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/consolews"
)

// SessionBridge maps Discord channels to consolews sessions for natural language chat.
type SessionBridge struct {
	consoleHub      *consolews.Hub
	adapter         *Adapter
	cfg             config.DiscordSettings
	channelSessions sync.Map // channelID -> sessionID (string)
	activeRequests  sync.Map // channelID -> context.CancelFunc
}

// NewSessionBridge creates a SessionBridge wired to the given console hub and adapter.
func NewSessionBridge(hub *consolews.Hub, adapter *Adapter, cfg config.DiscordSettings) *SessionBridge {
	return &SessionBridge{
		consoleHub: hub,
		adapter:    adapter,
		cfg:        cfg,
	}
}

// HandleMessage processes a natural language message by routing it through a consolews session.
func (sb *SessionBridge) HandleMessage(ctx context.Context, evt chatadapter.MessageEvent) error {
	// Show typing indicator
	sb.adapter.ShowTyping(evt.ChannelID)

	// Get or create session for this channel
	session, err := sb.getOrCreateSession(evt.ChannelID)
	if err != nil {
		_, _ = evt.Respond("Failed to create chat session.", nil)
		return err
	}

	// Cancel any previous in-flight request for this channel
	if cancelFn, ok := sb.activeRequests.LoadAndDelete(evt.ChannelID); ok {
		if fn, ok := cancelFn.(context.CancelFunc); ok {
			fn()
		}
	}

	// Create cancellable context for this request
	reqCtx, reqCancel := context.WithCancel(ctx)
	sb.activeRequests.Store(evt.ChannelID, reqCancel)
	defer func() {
		sb.activeRequests.Delete(evt.ChannelID)
		reqCancel()
	}()

	// Post initial \"Thinking...\" message
	ref, err := evt.Respond("Thinking...", nil)
	if err != nil {
		return err
	}

	// Subscribe to session events
	ch, unsub := session.Subscribe(64)
	defer unsub()

	// Submit user message to the session
	payload := consolews.Payload{
		Type:    consolews.PayloadTypeAsk,
		Content: evt.Content,
		Metadata: map[string]any{
			"discord_user":    evt.User.Username,
			"discord_channel": evt.ChannelID,
		},
	}
	session.HandlePayload(reqCtx, nil, payload)

	// Collect streaming events and update Discord message
	return sb.collectAndUpdate(reqCtx, ch, ref, evt)
}

// collectAndUpdate loops on the subscriber channel, accumulating text and periodically
// editing the Discord message with progress.
func (sb *SessionBridge) collectAndUpdate(ctx context.Context, ch <-chan consolews.Payload, ref chatadapter.MessageRef, evt chatadapter.MessageEvent) error {
	var accumulated strings.Builder
	editInterval := 1500 * time.Millisecond // rate-limit safety for Discord
	// Allow the first partial to trigger an immediate edit; subsequent edits are
	// throttled by editInterval.
	lastEdit := time.Now().Add(-editInterval)
	lastContent := "Thinking..."

	for {
		select {
		case <-ctx.Done():
			// Context cancelled — update with partial text if we have any
			if accumulated.Len() > 0 {
				final := truncateForDiscord(accumulated.String())
				_ = evt.Edit(ref, final, nil)
			}
			return ctx.Err()

		case p, ok := <-ch:
			if !ok {
				// Channel closed (session closed) — finalize with what we have
				if accumulated.Len() > 0 {
					final := truncateForDiscord(accumulated.String())
					_ = evt.Edit(ref, final, nil)
				} else if lastContent == "Thinking..." {
					_ = evt.Edit(ref, "Session ended without a response.", nil)
				}
				return nil
			}

			switch p.Type {
			case consolews.PayloadTypeEvent:
				// Streaming partial — accumulate text
				if isPartial(p.Metadata) {
					accumulated.WriteString(p.Content)

					// Periodically update Discord message (rate-limit safe)
					if time.Since(lastEdit) >= editInterval {
						content := truncateForDiscordWithSuffix(accumulated.String(), "...")
						if content != lastContent {
							_ = evt.Edit(ref, content, nil)
							lastContent = content
							lastEdit = time.Now()
						}
					}
				} else if p.Content != "" {
					// Non-partial event with content (tool output, etc.) — append
					accumulated.WriteString(p.Content)
				}

			case consolews.PayloadTypeReply:
				// Final reply — use this as the complete response
				final := p.Content
				if final == "" && accumulated.Len() > 0 {
					final = accumulated.String()
				}
				if final == "" {
					final = "No response generated."
				}
				return evt.Edit(ref, truncateForDiscord(final), nil)
			}
		}
	}
}

// getOrCreateSession returns the consolews session for a channel, creating one if needed.
func (sb *SessionBridge) getOrCreateSession(channelID string) (*consolews.Session, error) {
	// Check existing mapping
	if sidVal, ok := sb.channelSessions.Load(channelID); ok {
		sid := sidVal.(string)
		if session := sb.consoleHub.GetSession(sid); session != nil {
			return session, nil
		}
		// Stale mapping — session was removed from hub
		sb.channelSessions.Delete(channelID)
	}

	// Create new session
	session := sb.consoleHub.CreateSession(consolews.SessionConfig{
		Profile:      sb.cfg.ChatProfile,
		SystemPrompt: sb.cfg.ChatSystemPrompt,
	})

	sb.channelSessions.Store(channelID, session.ID())

	observability.Emit(context.Background(), observability.NewEvent("discord.session_created").
		WithComponent("discord").
		WithData("channel_id", channelID).
		WithData("session_id", session.ID()).
		WithData("profile", sb.cfg.ChatProfile).
		Success(0))

	return session, nil
}

// truncateForDiscord ensures text fits within Discord's 2000 character message limit.
// Operates on runes to avoid splitting multi-byte UTF-8 characters.
func truncateForDiscord(s string) string {
	const maxLen = 2000
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-3]) + "..."
}

// truncateForDiscordWithSuffix appends suffix while ensuring the final message
// fits within Discord's 2000-character (rune) limit.
func truncateForDiscordWithSuffix(s string, suffix string) string {
	const maxLen = 2000
	sRunes := []rune(s)
	sufRunes := []rune(suffix)

	// Degenerate case: suffix is longer than the allowed message length.
	if len(sufRunes) >= maxLen {
		return string(sufRunes[:maxLen])
	}

	avail := maxLen - len(sufRunes)
	if len(sRunes) <= avail {
		return s + suffix
	}
	return string(sRunes[:avail]) + suffix
}

// isPartial returns true if the metadata indicates a streaming partial event.
func isPartial(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	if v, ok := meta["partial"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}
