package telegram

import (
	"github.com/jkatigb/agentctl/internal/chatadapter"
	"github.com/jkatigb/agentctl/internal/platform/config"
	"github.com/jkatigb/agentctl/internal/web/consolews"
)

// NewSessionBridge returns a shared chatadapter.SessionBridge configured for Telegram.
func NewSessionBridge(hub *consolews.Hub, adapter *Adapter, cfg config.TelegramSettings) *chatadapter.SessionBridge {
	return chatadapter.NewSessionBridge(hub, adapter, chatadapter.SessionBridgeConfig{
		PlatformName:     "telegram",
		MaxMessageLen:    telegramMaxMessageLen,
		EditIntervalMS:   cfg.EditIntervalMS,
		ChatProfile:      cfg.ChatProfile,
		ChatSystemPrompt: cfg.ChatSystemPrompt,
	})
}
