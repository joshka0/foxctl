package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultEpicsDir = ".foxctl/epics"

type epicMetaDocument struct {
	Epic epicMirror `json:"epic"`
}

type epicMirror struct {
	ID                  string         `json:"id"`
	Title               string         `json:"title"`
	Status              string         `json:"status"`
	Finalized           bool           `json:"finalized"`
	Closed              bool           `json:"closed"`
	CloseReason         string         `json:"close_reason"`
	Root                epicMessage    `json:"root"`
	FinalBrief          epicMessage    `json:"final_brief"`
	Meta                epicMirrorMeta `json:"meta"`
	Logs                []epicLog      `json:"logs"`
	MilestoneCount      int            `json:"milestone_count"`
	StoryCount          int            `json:"story_count"`
	QuestionCount       int            `json:"question_count"`
	OpenQuestions       int            `json:"open_questions"`
	QuietMilestoneCount int            `json:"quiet_milestone_count"`
	QuietStoryCount     int            `json:"quiet_story_count"`
	LogCount            int            `json:"log_count"`
}

type epicMirrorMeta struct {
	Goal       string `json:"goal"`
	Outcome    string `json:"outcome"`
	SourcePath string `json:"source_path"`
	Horizon    string `json:"horizon"`
}

type epicMessage struct {
	Subject     string `json:"subject"`
	Body        string `json:"body"`
	WorkspaceID string `json:"workspace_id"`
	CreatedAt   string `json:"created_at"`
}

type epicLog struct {
	Label string      `json:"label"`
	Meta  epicLogMeta `json:"meta"`
	Root  epicMessage `json:"root"`
}

type epicLogMeta struct {
	Label     string     `json:"label"`
	Completed stringList `json:"completed"`
	NextFocus stringList `json:"next_focus"`
	Notes     string     `json:"notes"`
}

type stringList []string

func (s *stringList) UnmarshalJSON(data []byte) error {
	raw := strings.TrimSpace(string(data))
	switch raw {
	case "", "null":
		*s = nil
		return nil
	}

	var many []json.RawMessage
	if err := json.Unmarshal(data, &many); err == nil {
		out := make([]string, 0, len(many))
		for _, item := range many {
			value, ok := parseScalarString(item)
			if !ok {
				continue
			}
			out = append(out, value)
		}
		*s = normalizeList(out)
		return nil
	}

	value, ok := parseScalarString(data)
	if !ok {
		*s = nil
		return nil
	}
	value = strings.TrimSpace(value)
	if value == "" {
		*s = nil
		return nil
	}
	*s = []string{value}
	return nil
}

// LoadShellState returns the default mock state unless opts.EpicID is set.
func LoadShellState(opts Options) (ShellState, error) {
	epicID := strings.TrimSpace(opts.EpicID)
	if epicID == "" {
		return DefaultShellState(opts), nil
	}

	metaPath, err := epicMetaPath(opts)
	if err != nil {
		return ShellState{}, err
	}

	payload, err := os.ReadFile(metaPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ShellState{}, fmt.Errorf("epic %q was not found at %s; pass --epics-dir to point to your foxctl epics store", epicID, metaPath)
		}
		return ShellState{}, fmt.Errorf("read epic %q mirror from %s: %w", epicID, metaPath, err)
	}

	var doc epicMetaDocument
	if err := json.Unmarshal(payload, &doc); err != nil {
		return ShellState{}, fmt.Errorf("parse epic %q mirror from %s: %w", epicID, metaPath, err)
	}
	if strings.TrimSpace(doc.Epic.ID) == "" {
		doc.Epic.ID = epicID
	}
	if strings.TrimSpace(doc.Epic.ID) == "" {
		return ShellState{}, fmt.Errorf("epic mirror at %s is missing epic.id", metaPath)
	}

	return mapEpicToShellState(opts, doc.Epic, metaPath), nil
}

func epicMetaPath(opts Options) (string, error) {
	epicID := strings.TrimSpace(opts.EpicID)
	if epicID == "" {
		return "", errors.New("epic id is required")
	}
	if strings.ContainsAny(epicID, `/\`) || epicID == "." || epicID == ".." {
		return "", fmt.Errorf("epic id %q must be a single directory name, not a path", epicID)
	}

	epicsDir, err := resolveEpicsDir(opts.EpicsDir)
	if err != nil {
		return "", err
	}

	return filepath.Join(epicsDir, epicID, "meta.json"), nil
}

func resolveEpicsDir(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home for default epics dir: %w", err)
		}
		return filepath.Join(homeDir, defaultEpicsDir), nil
	}

	expanded, err := expandUserPath(trimmed)
	if err != nil {
		return "", err
	}
	return filepath.Clean(expanded), nil
}

func expandUserPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve user home for %q: %w", path, err)
		}
		if path == "~" {
			return homeDir, nil
		}
		return filepath.Join(homeDir, strings.TrimPrefix(path, "~/")), nil
	}
	return path, nil
}

func mapEpicToShellState(opts Options, epic epicMirror, metaPath string) ShellState {
	state := DefaultShellState(opts)

	if title := deriveTitle(epic); title != "" {
		state.EpicTitle = title
	}
	if status := deriveStatus(epic); status != "" {
		state.EpicStatus = status
	}
	if strings.TrimSpace(opts.Workspace) == "" {
		if workspace := firstNonEmpty(epic.Root.WorkspaceID, epic.FinalBrief.WorkspaceID); workspace != "" {
			state.Workspace = workspace
		}
	}

	continuityStatus := firstNonEmpty(epic.Status, state.EpicStatus)
	state.Continuity = ContinuitySummary{
		EpicID:   firstNonEmpty(epic.ID, opts.EpicID),
		Status:   continuityStatus,
		Boundary: summarize(deriveBoundary(epic)),
		Next:     summarize(deriveNext(epic)),
	}
	state.Memory = deriveMemory(epic)
	state.Transcript = deriveTranscript(epic, metaPath)
	state.Workers = deriveWorkers(epic)

	return state
}

func deriveTitle(epic epicMirror) string {
	title := firstNonEmpty(
		epic.Title,
		strings.TrimPrefix(epic.FinalBrief.Subject, "Epic Finalized:"),
		strings.TrimPrefix(epic.Root.Subject, "Epic:"),
		epic.Meta.SourcePath,
		epic.Meta.Goal,
	)
	return strings.TrimSpace(title)
}

func deriveStatus(epic epicMirror) string {
	if status := strings.TrimSpace(epic.Status); status != "" {
		return status
	}
	if epic.Closed {
		if reason := strings.TrimSpace(epic.CloseReason); reason != "" {
			return "closed:" + reason
		}
		return "closed"
	}
	if epic.Finalized {
		return "finalized"
	}
	if epic.OpenQuestions > 0 {
		return "intake"
	}
	return ""
}

func deriveBoundary(epic epicMirror) string {
	return firstNonEmpty(
		epic.Meta.Outcome,
		epic.Meta.Goal,
		epic.FinalBrief.Body,
		epic.Root.Subject,
		epic.Root.Body,
	)
}

func deriveNext(epic epicMirror) string {
	for _, log := range epic.Logs {
		if next := joinList(log.Meta.NextFocus, ", "); next != "" {
			return "Next focus: " + next
		}
	}
	for _, log := range epic.Logs {
		if notes := strings.TrimSpace(log.Meta.Notes); notes != "" {
			return notes
		}
	}
	if summary := summarizeMessage(epic.FinalBrief); summary != "" {
		return summary
	}
	return "No next focus recorded in epic logs."
}

func deriveMemory(epic epicMirror) []MemorySummary {
	memory := make([]MemorySummary, 0, 4)

	if summary := summarizeMessage(epic.FinalBrief); summary != "" {
		memory = append(memory, MemorySummary{
			Title:   "Final Brief",
			Summary: summarize(summary),
		})
	}

	for i := 0; i < len(epic.Logs) && len(memory) < 4; i++ {
		log := epic.Logs[i]
		summary := firstNonEmpty(
			withPrefix(joinList(log.Meta.NextFocus, ", "), "Next: "),
			joinList(log.Meta.Completed, " "),
			strings.TrimSpace(log.Meta.Notes),
			summarizeMessage(log.Root),
		)
		if summary == "" {
			continue
		}
		title := strings.TrimSpace(log.Label)
		if title == "" {
			title = "Delivery Log"
		}
		memory = append(memory, MemorySummary{
			Title:   title,
			Summary: summarize(summary),
		})
	}

	if len(memory) == 0 {
		memory = append(memory, MemorySummary{
			Title:   "Epic Mirror",
			Summary: "No final brief or delivery logs were found in this epic metadata.",
		})
	}

	return memory
}

func deriveTranscript(epic epicMirror, metaPath string) []TranscriptEntry {
	entries := []TranscriptEntry{
		{
			Speaker: "system",
			Kind:    "epic",
			Text: summarize(
				fmt.Sprintf("Loaded epic mirror %s from %s.", firstNonEmpty(epic.ID, "unknown"), metaPath),
			),
		},
		{
			Speaker: "assistant",
			Kind:    "counts",
			Text: summarize(
				fmt.Sprintf(
					"Milestones=%d, Stories=%d, OpenQuestions=%d, DeliveryLogs=%d.",
					epic.MilestoneCount,
					epic.StoryCount,
					epic.OpenQuestions,
					maxInt(epic.LogCount, len(epic.Logs)),
				),
			),
		},
	}

	if summary := summarizeMessage(epic.FinalBrief); summary != "" {
		entries = append(entries, TranscriptEntry{
			Speaker: "assistant",
			Kind:    "brief",
			Text:    summary,
		})
	}
	if next := deriveNext(epic); next != "" {
		entries = append(entries, TranscriptEntry{
			Speaker: "worker",
			Kind:    "next",
			Text:    summarize(next),
		})
	}

	return entries
}

func deriveWorkers(epic epicMirror) []WorkerSummary {
	return []WorkerSummary{
		{
			Name:   "milestones",
			Status: fmt.Sprintf("%d total", epic.MilestoneCount),
			Task:   fmt.Sprintf("%d quiet", epic.QuietMilestoneCount),
		},
		{
			Name:   "stories",
			Status: fmt.Sprintf("%d total", epic.StoryCount),
			Task:   fmt.Sprintf("%d quiet", epic.QuietStoryCount),
		},
		{
			Name:   "questions",
			Status: fmt.Sprintf("%d open", epic.OpenQuestions),
			Task:   fmt.Sprintf("%d total", epic.QuestionCount),
		},
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func withPrefix(value, prefix string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return prefix + trimmed
}

func summarizeMessage(message epicMessage) string {
	return firstNonEmpty(message.Body, message.Subject)
}

func parseScalarString(data []byte) (string, bool) {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		return one, true
	}

	var number json.Number
	if err := json.Unmarshal(data, &number); err == nil {
		return number.String(), true
	}

	var boolValue bool
	if err := json.Unmarshal(data, &boolValue); err == nil {
		return strconv.FormatBool(boolValue), true
	}

	return "", false
}

func normalizeList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func joinList(values []string, sep string) string {
	return strings.Join(normalizeList(values), sep)
}

func summarize(value string) string {
	normalized := strings.Join(strings.Fields(value), " ")
	runes := []rune(normalized)
	if len(runes) <= 220 {
		return normalized
	}
	return string(runes[:217]) + "..."
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
