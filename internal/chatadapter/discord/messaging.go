package discord

import (
	"github.com/jkatigb/agentctl/internal/chatadapter"
	"github.com/jkatigb/agentctl/internal/companion"
	consolepkg "github.com/jkatigb/agentctl/internal/console"
	"github.com/jkatigb/agentctl/internal/platform/config"
)

// NewSessionBridge returns a shared chatadapter.SessionBridge configured for Discord.
func NewSessionBridge(hub consolepkg.SessionManager, adapter *Adapter, cfg config.DiscordSettings, turnLock companion.Locker) *chatadapter.SessionBridge {
	return chatadapter.NewSessionBridge(hub, adapter, chatadapter.SessionBridgeConfig{
		PlatformName:     "discord",
		MaxMessageLen:    2000,
		EditIntervalMS:   1500,
		ChatProfile:      cfg.ChatProfile,
		ChatSystemPrompt: cfg.ChatSystemPrompt,
	}, turnLock)
}
