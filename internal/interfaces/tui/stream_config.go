package tui

import "strings"

const defaultTranscriptLimit = 200

func shouldAttachConsoleStream(opts Options) bool {
	return strings.TrimSpace(opts.APIBaseURL) != "" && strings.TrimSpace(opts.ConsoleSessionID) != ""
}

func shouldAttachAgentCompanion(opts Options) bool {
	return strings.TrimSpace(opts.APIBaseURL) != "" && strings.TrimSpace(opts.AgentID) != ""
}
