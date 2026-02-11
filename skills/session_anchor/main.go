package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/jkatigb/agentctl/internal/adapters/skillslib/oputil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillerr"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillmain"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/skillout"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/stringutil"
	"github.com/jkatigb/agentctl/internal/adapters/skillslib/workspaceutil"
	"github.com/jkatigb/agentctl/internal/platform/timeutil"
	"github.com/jkatigb/agentctl/internal/sessionkit"
	"github.com/jkatigb/agentctl/internal/storage/memory"
)

// Input defines the parameters for session anchor operations.
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

// Anchor represents a session anchor with main prompt and learning history.
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

// Learning represents a learning entry within an anchor.
type Learning struct {
	At           time.Time `json:"at"`
	Trigger      string    `json:"trigger,omitempty"`
	Summary      string    `json:"summary,omitempty"`
	Decisions    []string  `json:"decisions"`
	Gotchas      []string  `json:"gotchas"`
	Progress     []string  `json:"progress"`
	CompactionNo int       `json:"compaction_no,omitempty"`
}

// Output represents the result of session anchor operations.
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

var allowedOps = []string{
	"get",
	"set",
	"append_learnings",
	"bump_compaction",
	"set_question",
	"clear_question",
	"clear",
}

// main is the skill entry point for session/anchor.
func main() {
	skillmain.Main(command, run)
}

// run orchestrates session anchor operations including get, set, learning management, and question handling.
//
// Index:
// - Purpose: Manage session anchors with main prompts, learnings, compaction tracking, and pending questions
// - Flow: validate operation → resolve session ID → build anchor key → open memory store → execute operation
// - SideEffects: anchor storage/retrieval; learning accumulation; compaction counting; question management
// - FailureModes: memory store access failures, invalid operations, missing required fields
// - Observability: emits anchor status, operation results, learning counts, and compaction numbers
// - Related: loadAnchor, saveAnchor, messageForGet, truncateOneLine, capRecentLearnings, normalizeLearnings
// - Keywords: session/anchor, session_management, learning_tracking, compaction, question_management
func run(ctx context.Context, rc *skillmain.RunContext, in Input) error {
	// Default workspace
	in.Workspace = workspaceutil.Resolve(in.Workspace, "", rc.Workspace)

	op := oputil.Op(oputil.DefaultOp(in.Operation, "get"))
	opHint := fmt.Sprintf("Use one of: %s.", strings.Join(allowedOps, ", "))
	if err := oputil.Validate(op, allowedOps...); err != nil {
		return skillerr.Arg(err.Error(), skillerr.WithHint(opHint))
	}

	// Resolve session ID
	sessionID := sessionkit.ResolveSessionID(in.Workspace, in.SessionID)

	// Build anchor key
	key := anchorName
	if sessionID != "" {
		key = anchorName + ":" + sessionID
	}

	// Open memory store in cache
	store, err := rc.Stores.MemoryInCache(ctx)
	if err != nil {
		return skillerr.IO("open memory store", skillerr.WithCause(err))
	}

	switch op {
	case "get":
		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return err
		}
		return skillout.Emit(rc, command, Output{Found: found, Anchor: anchor, Message: messageForGet(found)})

	case "set":
		mainPrompt := strings.TrimSpace(in.MainPrompt)
		if mainPrompt == "" {
			return skillerr.Arg(
				"main_prompt is required for operation=set",
				skillerr.WithHint("Provide main_prompt when operation is set."),
			)
		}

		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return err
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
			return err
		}

		return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "anchor set"})

	case "append_learnings":
		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return err
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
			Decisions:    stringutil.NormalizeStrings(in.Decisions),
			Gotchas:      stringutil.NormalizeStrings(in.Gotchas),
			Progress:     stringutil.NormalizeStrings(in.Progress),
			CompactionNo: anchor.CompactionCount,
		}
		anchor.RecentLearnings = append(anchor.RecentLearnings, entry)
		anchor.RecentLearnings = capRecentLearnings(anchor.RecentLearnings, 10)
		anchor.UpdatedAt = now
		anchor.LastSessionID = sessionID

		if err := saveAnchor(ctx, store, key, in.Workspace, anchor, sessionID); err != nil {
			return err
		}

		return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "learnings appended"})

	case "bump_compaction":
		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return err
		}
		if !found || anchor == nil {
			return skillout.Emit(rc, command, Output{Found: false, Anchor: nil, Message: "no anchor set"})
		}

		now := timeutil.NowUTC()
		anchor.CompactionCount++
		anchor.UpdatedAt = now
		anchor.LastSessionID = sessionID

		if err := saveAnchor(ctx, store, key, in.Workspace, anchor, sessionID); err != nil {
			return err
		}

		return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "compaction count incremented"})

	case "set_question":
		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return err
		}
		if !found || anchor == nil {
			return skillout.Emit(rc, command, Output{Found: false, Anchor: nil, Message: "no anchor set"})
		}
		q := strings.TrimSpace(in.Question)
		if q == "" {
			return skillerr.Arg(
				"question is required for operation=set_question",
				skillerr.WithHint("Provide question when operation is set_question."),
			)
		}

		now := timeutil.NowUTC()
		anchor.PendingQuestion = q
		anchor.UpdatedAt = now
		anchor.LastSessionID = sessionID

		if err := saveAnchor(ctx, store, key, in.Workspace, anchor, sessionID); err != nil {
			return err
		}
		return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "question set"})

	case "clear_question":
		anchor, found, err := loadAnchor(ctx, store, key, in.Workspace)
		if err != nil {
			return err
		}
		if !found || anchor == nil {
			return skillout.Emit(rc, command, Output{Found: false, Anchor: nil, Message: "no anchor set"})
		}

		now := timeutil.NowUTC()
		anchor.PendingQuestion = ""
		anchor.UpdatedAt = now
		anchor.LastSessionID = sessionID

		if err := saveAnchor(ctx, store, key, in.Workspace, anchor, sessionID); err != nil {
			return err
		}
		return skillout.Emit(rc, command, Output{Found: true, Anchor: anchor, Message: "question cleared"})

	case "clear":
		err := store.Delete(ctx, key, in.Workspace)
		if err != nil && err != memory.ErrNotFound {
			return skillerr.IO("delete anchor", skillerr.WithCause(err))
		}
		return skillout.Emit(rc, command, Output{Found: false, Anchor: nil, Message: "anchor cleared"})

	default:
		return skillerr.Arg("invalid operation", skillerr.WithHint(opHint))
	}
}

// messageForGet generates appropriate messages for anchor retrieval operations.
func messageForGet(found bool) string {
	if found {
		return "anchor loaded"
	}
	return "no anchor set"
}

// loadAnchor retrieves and deserializes an anchor from the memory store.
func loadAnchor(ctx context.Context, store *memory.Store, name, workspace string) (*Anchor, bool, error) {
	entry, err := store.Get(ctx, name, workspace)
	if err != nil {
		if err == memory.ErrNotFound {
			return nil, false, nil
		}
		return nil, false, skillerr.WrapIO("get anchor", err)
	}
	var a Anchor
	if err := json.Unmarshal(entry.Result, &a); err != nil {
		return nil, false, skillerr.WrapParse("parse anchor", err)
	}
	a.Requirements = stringutil.NormalizeStrings(a.Requirements)
	a.RecentLearnings = normalizeLearnings(a.RecentLearnings)
	return &a, true, nil
}

// saveAnchor serializes and stores an anchor with normalized data and metadata.
func saveAnchor(ctx context.Context, store *memory.Store, name, workspace string, anchor *Anchor, sessionID string) error {
	if anchor == nil {
		return skillerr.Validation("save anchor: nil anchor")
	}

	anchor.Workspace = workspace
	anchor.Requirements = stringutil.NormalizeStrings(anchor.Requirements)
	anchor.RecentLearnings = normalizeLearnings(anchor.RecentLearnings)

	result, err := json.Marshal(anchor)
	if err != nil {
		return skillerr.WrapRuntime("marshal anchor", err)
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
		return skillerr.WrapIO("save anchor", err)
	}
	return nil
}

// truncateOneLine converts multi-line text to a single line with length limit.
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

// capRecentLearnings limits the number of recent learnings to prevent unbounded growth.
func capRecentLearnings(in []Learning, max int) []Learning {
	if max <= 0 {
		return []Learning{}
	}
	if len(in) <= max {
		return in
	}
	return append([]Learning{}, in[len(in)-max:]...)
}

// normalizeLearnings normalizes string arrays within learning entries.
func normalizeLearnings(in []Learning) []Learning {
	if in == nil {
		return []Learning{}
	}
	out := make([]Learning, 0, len(in))
	for _, l := range in {
		l.Decisions = stringutil.NormalizeStrings(l.Decisions)
		l.Gotchas = stringutil.NormalizeStrings(l.Gotchas)
		l.Progress = stringutil.NormalizeStrings(l.Progress)
		out = append(out, l)
	}
	return out
}
