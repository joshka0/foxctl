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
			Role:     "foreground assistant",
			Provider: "native",
			Model:    "gpt-5.4",
		},
		Transcript: []TranscriptEntry{
			{
				Speaker: "system",
				Kind:    "epic",
				Text:    "READY epic loaded as an implementation plan. Epic creation is complete; this shell is the implementation surface.",
			},
			{
				Speaker: "assistant",
				Kind:    "plan",
				Text:    "Phase 0 creates the standalone go-tui shell, mocked panes, focus cycling, and generated .gsx path.",
			},
			{
				Speaker: "worker",
				Kind:    "frontier",
				Text:    "Next dispatchable frontier: foreground transcript, composer, context rail, and worker/task visibility.",
			},
		},
		Composer:   "",
		ActiveRail: RailMemory,
		Memory: []MemorySummary{
			{
				Title:   "Boundary",
				Summary: "TUI implementation is separate from epic creation and does not mutate room-agile state in Phase 0.",
			},
			{
				Title:   "Reference",
				Summary: "go-tui reference repo is local under ~/repos/githubs/go-tui and drives .gsx generation.",
			},
			{
				Title:   "Placement",
				Summary: "Terminal interface code belongs under internal/interfaces, not internal/v2.",
			},
		},
		Continuity: ContinuitySummary{
			EpicID:   "go-tui-agent-shell",
			Status:   "READY",
			Boundary: "Implementation plan only; live agent/API integration comes after shell skeleton.",
			Next:     "Replace mock adapters with typed foxctl API and event stream clients.",
		},
		Workers: []WorkerSummary{
			{Name: "foreground", Status: "active", Task: "own the transcript and composer loop"},
			{Name: "reviewer", Status: "idle", Task: "review generated UI shell slices"},
			{Name: "builder", Status: "blocked", Task: "waiting on live adapter contracts"},
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
