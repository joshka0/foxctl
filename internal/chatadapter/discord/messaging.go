package discord

import (
	"github.com/jkatigb/agentctl/internal/chatadapter"
	"github.com/jkatigb/agentctl/internal/companion"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/consolews"
)

// NewSessionBridge returns a shared chatadapter.SessionBridge configured for Discord.
func NewSessionBridge(hub *consolews.Hub, adapter *Adapter, cfg config.DiscordSettings, turnLock companion.Locker) *chatadapter.SessionBridge {
	return chatadapter.NewSessionBridge(hub, adapter, chatadapter.SessionBridgeConfig{
		PlatformName:     "discord",
		MaxMessageLen:    2000,
		EditIntervalMS:   1500,
		ChatProfile:      cfg.ChatProfile,
		ChatSystemPrompt: cfg.ChatSystemPrompt,
	}, turnLock)
}
