package tui

// Options configures the standalone foxctl_tui shell.
type Options struct {
	Workspace  string
	EpicID     string
	EpicsDir   string
	APIBaseURL string
	AgentLimit int
}

// FocusPane identifies the major interactive regions in the shell.
type FocusPane string

const (
	FocusTranscript FocusPane = "transcript"
	FocusComposer   FocusPane = "composer"
	FocusRail       FocusPane = "rail"
	FocusWorkers    FocusPane = "workers"
)

// RailTab identifies the right-side planning context rail.
type RailTab int

const (
	RailMemory RailTab = iota
	RailContinuity
	RailWorkers
	RailTask
)

func railTabs() []RailTab {
	return []RailTab{RailMemory, RailContinuity, RailWorkers, RailTask}
}

func (r RailTab) Label() string {
	switch r {
	case RailMemory:
		return "Memory"
	case RailContinuity:
		return "Continuity"
	case RailWorkers:
		return "Workers"
	case RailTask:
		return "Task"
	default:
		return "Unknown"
	}
}

type AssistantSummary struct {
	Name     string
	Role     string
	Provider string
	Model    string
}

type TranscriptEntry struct {
	Speaker string
	Kind    string
	Text    string
}

type WorkerSummary struct {
	Name   string
	Status string
	Task   string
}

type MemorySummary struct {
	Title   string
	Summary string
}

type ContinuitySummary struct {
	EpicID   string
	Status   string
	Boundary string
	Next     string
}
