package discord

import (
	consolepkg "github.com/joshka0/foxctl/internal/console"
	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/platform/config"
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
