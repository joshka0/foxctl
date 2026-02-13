package teams

import "github.com/jkatigb/agentctl/internal/chatadapter"

const teamsMaxMessageLen = 4000

func truncateForTeams(s string) string {
	return chatadapter.TruncateRunesWithEllipsis(s, teamsMaxMessageLen)
}
