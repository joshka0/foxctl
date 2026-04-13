package teams

import "github.com/jkatigb/agentctl/internal/interfaces/chatadapter"

const teamsMaxMessageLen = 4000

func truncateForTeams(s string) string {
	return chatadapter.TruncateRunesWithEllipsis(s, teamsMaxMessageLen)
}
