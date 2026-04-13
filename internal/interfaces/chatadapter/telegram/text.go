package telegram

import "github.com/jkatigb/agentctl/internal/interfaces/chatadapter"

const telegramMaxMessageLen = 4096

func truncateForTelegram(s string) string {
	return chatadapter.TruncateRunesWithEllipsis(s, telegramMaxMessageLen)
}

func truncateForTelegramWithSuffix(s string, suffix string) string {
	return chatadapter.TruncateRunesWithSuffix(s, suffix, telegramMaxMessageLen)
}
