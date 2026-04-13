package telegram

import (
	"github.com/jkatigb/agentctl/internal/chatadapter"
	consolepkg "github.com/jkatigb/agentctl/internal/console"
	"github.com/jkatigb/agentctl/internal/context/companion"
	"github.com/jkatigb/agentctl/internal/platform/config"
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
