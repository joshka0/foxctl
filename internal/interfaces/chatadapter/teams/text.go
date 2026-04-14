package teams

import "github.com/joshka0/foxctl/internal/interfaces/chatadapter"

const teamsMaxMessageLen = 4000

func truncateForTeams(s string) string {
	return chatadapter.TruncateRunesWithEllipsis(s, teamsMaxMessageLen)
}
