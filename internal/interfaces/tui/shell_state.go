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
