package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

type Input struct {
	Operation  string   `json:"operation"`
	Workspace  string   `json:"workspace"`
	SessionID  string   `json:"session_id,omitempty"`
	MainPrompt string   `json:"main_prompt,omitempty"`
	Trigger    string   `json:"trigger,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	Decisions  []string `json:"decisions,omitempty"`
	Gotchas    []string `json:"gotchas,omitempty"`
	Progress   []string `json:"progress,omitempty"`
	Question   string   `json:"question,omitempty"`
}

type Anchor struct {
	AnchorID        string     `json:"anchor_id"`
	Workspace       string     `json:"workspace"`
	MainPrompt      string     `json:"main_prompt"`
	Requirements    []string   `json:"requirements"`
	CompactionCount int        `json:"compaction_count"`
	RecentLearnings []Learning `json:"recent_learnings"`
	PendingQuestion string     `json:"pending_question,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	LastSessionID   string     `json:"last_session_id,omitempty"`
}

type Learning struct {
	At           time.Time `json:"at"`
	Trigger      string    `json:"trigger,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	Decisions    []string  `json:"decisions"`
	Gotchas      []string  `json:"gotchas"`
	Progress     []string  `json:"progress"`
	CompactionNo int       `json:"compaction_no,omitempty"`
}

type Output struct {
	Found   bool    `json:"found"`
	Anchor  *Anchor `json:"anchor,omitempty"`
	Message string  `json:"message"`
}

const (
	command    = "session/anchor"
	anchorName = "session-anchor"
	anchorType = "session_anchor"
)

func main() {
	skillmain.Main(command, run)
}

func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Default workspace
	in.Workspace = sessionkit.WorkspaceOrDefault(in.Workspace, rc.Workspace)

	// Default operation
	in.Operation = strings.TrimSpace(in.Operation)
	if in.Operation == "" {
		in.Operation = "get"
	}

	// Resolve session ID
	sessionID := sessionkit.ResolveSessionID(in.Workspace, in.SessionID)

	// Build anchor key
	key := anchorName
	if sessionID != "" {
		key = anchorName + ":" + sessionID
	}

	// Open memory store in cache
	store, cleanup, err := sessionkit.OpenMemoryInCache(ctx, rc.Config)
	if err != nil {
		return skillerr.IO("open memory store", skillerr.WithCause(err))
	}
	defer cleanup()

	switch in.Operation {
	case "get":
		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return skillerr.IO("load anchor", skillerr.WithCause(err))
		}
		return skillout.Emit(rc, command, Output{Found: found, Anchor: anchor, Message: messageForGet(found)})

	case "set":
		mainPrompt := strings.TrimSpace(in.MainPrompt)
		if mainPrompt == "" {
			return skillerr.Arg("main_prompt is required for operation=set")
		}

		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return skillerr.IO("load anchor", skillerr.WithCause(err))
		}
		now := timeutil.NowUTC()
		if !found || anchor == nil {
			anchor = &Anchor{
				AnchorID:        ulid.Make().String(),
				Workspace:       in.Workspace,
				Requirements:    []string{},
				RecentLearnings: []Learning{},
				CreatedAt:       now,
			}
		}

		anchor.MainPrompt = mainPrompt
		anchor.UpdatedAt = now
		anchor.LastSessionID = sessionID

		if err := saveAnchor(ctx, store, key, in.Workspace, anchor, sessionID); err != nil {
			return skillerr.IO("save anchor", skillerr.WithCause(err))
		}

		return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "anchor set"})

	case "append_learnings":
		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return skillerr.IO("load anchor", skillerr.WithCause(err))
		}
		if !found || anchor == nil {
			return skillout.Emit(rc, command, Output{Found: false, Anchor: nil, Message: "no anchor set"})
		}

		if strings.TrimSpace(in.Summary) == "" && len(in.Decisions) == 0 && len(in.Gotchas) == 0 && len(in.Progress) == 0 {
			return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "no learnings to append"})
		}

		now := timeutil.NowUTC()
		entry := Learning{
			At:           now,
			Trigger:      strings.TrimSpace(in.Trigger),
			Summary:      strings.TrimSpace(in.Summary),
			Decisions:    normalizeStrings(in.Decisions),
			Gotchas:      normalizeStrings(in.Gotchas),
			Progress:     normalizeStrings(in.Progress),
			CompactionNo: anchor.CompactionCount,
		}
		anchor.RecentLearnings = append(anchor.RecentLearnings, entry)
		anchor.RecentLearnings = capRecentLearnings(anchor.RecentLearnings, 10)
		anchor.UpdatedAt = now
		anchor.LastSessionID = sessionID

		if err := saveAnchor(ctx, store, key, in.Workspace, anchor, sessionID); err != nil {
			return skillerr.IO("save anchor", skillerr.WithCause(err))
		}

		return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "learnings appended"})

	case "bump_compaction":
		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return skillerr.IO("load anchor", skillerr.WithCause(err))
		}
		if !found || anchor == nil {
			return skillout.Emit(rc, command, Output{Found: false, Anchor: nil, Message: "no anchor set"})
		}

		now := timeutil.NowUTC()
		anchor.CompactionCount++
		anchor.UpdatedAt = now
		anchor.LastSessionID = sessionID

		if err := saveAnchor(ctx, store, key, in.Workspace, anchor, sessionID); err != nil {
			return skillerr.IO("save anchor", skillerr.WithCause(err))
		}

		return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "compaction count incremented"})

	case "set_question":
		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return skillerr.IO("load anchor", skillerr.WithCause(err))
		}
		if !found || anchor == nil {
			return skillout.Emit(rc, command, Output{Found: false, Anchor: nil, Message: "no anchor set"})
		}
		q := strings.TrimSpace(in.Question)
		if q == "" {
			return skillerr.Arg("question is required for operation=set_question")
		}

		now := timeutil.NowUTC()
		anchor.PendingQuestion = q
		anchor.UpdatedAt = now
		anchor.LastSessionID = sessionID

		if err := saveAnchor(ctx, store, key, in.Workspace, anchor, sessionID); err != nil {
			return skillerr.IO("save anchor", skillerr.WithCause(err))
		}
		return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "question set"})

	case "clear_question":
		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return skillerr.IO("load anchor", skillerr.WithCause(err))
		}
		if !found || anchor == nil {
			return skillout.Emit(rc, command, Output{Found: false, Anchor: nil, Message: "no anchor set"})
		}

		now := timeutil.NowUTC()
		anchor.PendingQuestion = ""
		anchor.UpdatedAt = now
		anchor.LastSessionID = sessionID

		if err := saveAnchor(ctx, store, key, in.Workspace, anchor, sessionID); err != nil {
			return skillerr.IO("save anchor", skillerr.WithCause(err))
		}
		return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "question cleared"})

	case "clear":
		err := store.Delete(ctx, key, in.Workspace)
		if err != nil && err != memory.ErrNotFound {
			return skillerr.IO("delete anchor", skillerr.WithCause(err))
		}
		return skillout.Emit(rc, command, Output{Found: false, Anchor: nil, Message: "anchor cleared"})

	default:
		return skillerr.Arg(fmt.Sprintf("unknown operation: %q", in.Operation))
	}
}

func messageForGet(found bool) string {
	if found {
		return "anchor loaded"
	}
	return "no anchor set"
}

func loadAnchor(ctx context.Context, store *memory.Store, name, workspace string) (*Anchor, bool, error) {
	entry, err := store.Get(ctx, name, workspace)
	if err != nil {
		if err == memory.ErrNotFound {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("get anchor: %w", err)
	}
	var a Anchor
	if err := json.Unmarshal(entry.Result, &a); err != nil {
		return nil, false, fmt.Errorf("parse anchor: %w", err)
	}
	a.Requirements = normalizeStrings(a.Requirements)
	a.RecentLearnings = normalizeLearnings(a.RecentLearnings)
	return &a, true, nil
}

func saveAnchor(ctx context.Context, store *memory.Store, name, workspace string, anchor *Anchor, sessionID string) error {
	if anchor == nil {
		return fmt.Errorf("save anchor: nil anchor")
	}

	anchor.Workspace = workspace
	anchor.Requirements = normalizeStrings(anchor.Requirements)
	anchor.RecentLearnings = normalizeLearnings(anchor.RecentLearnings)

	result, err := json.Marshal(anchor)
	if err != nil {
		return fmt.Errorf("marshal anchor: %w", err)
	}

	summary := "session anchor"
	if anchor.MainPrompt != "" {
		summary = fmt.Sprintf("session anchor: %s", truncateOneLine(anchor.MainPrompt, 96))
	}

	_, err = store.SaveResult(ctx, memory.SaveOptions{
		Name:      name,
		Type:      anchorType,
		Workspace: workspace,
		Summary:   summary,
		Result:    result,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("save anchor: %w", err)
	}
	return nil
}

func truncateOneLine(s string, max int) string {
	line := strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	line = strings.Join(strings.Fields(line), " ")
	if max <= 0 {
		return ""
	}
	if len(line) <= max {
		return line
	}
	return line[:max]
}

func capRecentLearnings(in []Learning, max int) []Learning {
	if max <= 0 {
		return []Learning{}
	}
	if len(in) <= max {
		return in
	}
	return append([]Learning{}, in[len(in)-max:]...)
}

func normalizeLearnings(in []Learning) []Learning {
	if in == nil {
		return []Learning{}
	}
	out := make([]Learning, 0, len(in))
	for _, l := range in {
		l.Decisions = normalizeStrings(l.Decisions)
		l.Gotchas = normalizeStrings(l.Gotchas)
		l.Progress = normalizeStrings(l.Progress)
		out = append(out, l)
	}
	return out
}

func normalizeStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}
