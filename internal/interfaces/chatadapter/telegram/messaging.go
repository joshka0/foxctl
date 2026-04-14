package telegram

import (
	consolepkg "github.com/joshka0/foxctl/internal/console"
	"github.com/joshka0/foxctl/internal/context/companion"
	"github.com/joshka0/foxctl/internal/interfaces/chatadapter"
	"github.com/joshka0/foxctl/internal/platform/config"
)

// NewSessionBridge returns a shared chatadapter.SessionBridge configured for Telegram.
func NewSessionBridge(hub consolepkg.SessionManager, adapter *Adapter, cfg config.TelegramSettings, turnLock companion.Locker) *chatadapter.SessionBridge {
	return chatadapter.NewSessionBridge(hub, adapter, chatadapter.SessionBridgeConfig{
		PlatformName:     "telegram",
		MaxMessageLen:    telegramMaxMessageLen,
		EditIntervalMS:   cfg.EditIntervalMS,
		ChatProfile:      cfg.ChatProfile,
		ChatSystemPrompt: cfg.ChatSystemPrompt,
	}, turnLock)
}
