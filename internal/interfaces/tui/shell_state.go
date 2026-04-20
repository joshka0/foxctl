package tui

import "strings"

// ShellState is the presentation snapshot rendered by the terminal shell.
type ShellState struct {
	Workspace  string
	EpicTitle  string
	EpicStatus string

	Assistant AssistantSummary

	Transcript []TranscriptEntry
	Composer   string
	ActiveRail RailTab

	Memory     []MemorySummary
	Continuity ContinuitySummary
	Workers    []WorkerSummary
}

func DefaultShellState(opts Options) ShellState {
	workspace := strings.TrimSpace(opts.Workspace)
	if workspace == "" {
		workspace = "."
	}

	return ShellState{
		Workspace:  workspace,
		EpicTitle:  "Go TUI Agent Shell",
		EpicStatus: "READY",
		Assistant: AssistantSummary{
			Name:     "Codex",
			Role:     "plan preview",
			Provider: "native",
			Model:    "gpt-5.4",
		},
		Transcript: []TranscriptEntry{
			{
				Speaker: "system",
				Kind:    "epic",
				Text:    "Plan preview loaded. No companion agent is attached yet; start with make go-tui-agent to chat with a foxctl agent.",
			},
			{
				Speaker: "assistant",
				Kind:    "plan",
				Text:    "This screen is the TUI shell: transcript on the left, composer below, operational context on the right.",
			},
			{
				Speaker: "system",
				Kind:    "next",
				Text:    "Attach an agent with --agent-id or run make go-tui-agent; then Enter sends composer text to the companion.",
			},
		},
		Composer:   "",
		ActiveRail: RailMemory,
		Memory: []MemorySummary{
			{
				Title:   "Mode",
				Summary: "Offline plan preview. Composer text is local unless an agent or console session is attached.",
			},
			{
				Title:   "Run",
				Summary: "Use make go-tui-agent to spawn a foxctl companion and open this shell in live chat mode.",
			},
			{
				Title:   "Layout",
				Summary: "Transcript is conversation history; context rail is orientation; footer lists navigation keys.",
			},
		},
		Continuity: ContinuitySummary{
			EpicID:   "go-tui-agent-shell",
			Status:   "READY",
			Boundary: "Epic creation is done; this shell is for implementation and companion interaction.",
			Next:     "Run make go-tui-agent or pass --agent-id to continue with a live foxctl companion.",
		},
		Workers: []WorkerSummary{
			{Name: "companion", Status: "not attached", Task: "run make go-tui-agent to spawn a local LMStudio-backed foxctl agent"},
			{Name: "composer", Status: "draft-only", Task: "without an attached agent, Enter records local notes only"},
			{Name: "runtime", Status: "ready", Task: "binary is built; API/agent attachment is optional per launch"},
		},
	}
}

// ApplyConsoleStreamEvent maps one console stream event into transcript state.
func (state ShellState) ApplyConsoleStreamEvent(event ConsoleStreamEvent, transcriptLimit int) ShellState {
	return state.ApplyConsoleStreamEvents([]ConsoleStreamEvent{event}, transcriptLimit)
}

// ApplyConsoleStreamEvents maps a batch of console stream events into transcript state.
func (state ShellState) ApplyConsoleStreamEvents(events []ConsoleStreamEvent, transcriptLimit int) ShellState {
	transcript := append([]TranscriptEntry(nil), state.Transcript...)
	appended := false
	for _, event := range events {
		entry, ok := MapConsoleStreamEventToTranscriptEntry(event)
		if !ok {
			continue
		}
		if shouldSuppressAskEcho(transcript, entry) {
			continue
		}
		transcript = append(transcript, entry)
		appended = true
	}
	if !appended {
		return state
	}

	next := state
	next.Transcript = capTranscriptEntries(transcript, transcriptLimit)
	return next
}

// AttachAskCorrelation updates the oldest pending ask row without a correlation id.
// It prefers an exact content match when acceptedContent is provided.
func (state ShellState) AttachAskCorrelation(acceptedContent string, correlationID string) (ShellState, bool) {
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return state, false
	}

	transcript := append([]TranscriptEntry(nil), state.Transcript...)
	target := findPendingTranscriptIndex(transcript, acceptedContent)
	if target < 0 {
		return state, false
	}

	transcript[target].CorrelationID = correlationID
	next := state
	next.Transcript = transcript
	return next, true
}

func shouldSuppressAskEcho(transcript []TranscriptEntry, entry TranscriptEntry) bool {
	if normalizeTranscriptKind(entry.Kind) != "ask" {
		return false
	}

	correlationID := strings.TrimSpace(entry.CorrelationID)
	if correlationID == "" {
		return false
	}

	for i := range transcript {
		existing := transcript[i]
		if strings.TrimSpace(existing.CorrelationID) != correlationID {
			continue
		}
		switch normalizeTranscriptKind(existing.Kind) {
		case "pending", "ask":
			return true
		}
	}
	return false
}

func findPendingTranscriptIndex(transcript []TranscriptEntry, acceptedContent string) int {
	acceptedContent = strings.TrimSpace(acceptedContent)
	if acceptedContent != "" {
		for i := range transcript {
			entry := transcript[i]
			if normalizeTranscriptKind(entry.Kind) != "pending" || strings.TrimSpace(entry.CorrelationID) != "" {
				continue
			}
			if strings.TrimSpace(entry.Text) == acceptedContent {
				return i
			}
		}
	}

	for i := range transcript {
		entry := transcript[i]
		if normalizeTranscriptKind(entry.Kind) != "pending" {
			continue
		}
		if strings.TrimSpace(entry.CorrelationID) == "" {
			return i
		}
	}

	return -1
}

func normalizeTranscriptKind(kind string) string {
	return strings.ToLower(strings.TrimSpace(kind))
}

func capTranscriptEntries(entries []TranscriptEntry, limit int) []TranscriptEntry {
	if limit <= 0 || len(entries) <= limit {
		return entries
	}

	start := len(entries) - limit
	capped := make([]TranscriptEntry, len(entries[start:]))
	copy(capped, entries[start:])
	return capped
}
