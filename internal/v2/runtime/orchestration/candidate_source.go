package orchestration

import (
	"context"
	"fmt"
	"strings"
	"time"

	coreorchestration "github.com/joshka0/foxctl/internal/v2/core/orchestration"
)

const (
	defaultCandidateRole     = "coder"
	defaultCandidateExecMode = "autonomous"
	maxCandidateScanLimit    = 200
)

// BoardReader reads the projection-backed board view used for candidate selection.
type BoardReader interface {
	Board(ctx context.Context, req coreorchestration.BoardRequest) (coreorchestration.BoardResponse, error)
}

// BoardCandidateSourceConfig configures candidate selection from projected kanban cards.
type BoardCandidateSourceConfig struct {
	Reader        BoardReader
	WorkspaceID   string
	ParentAgentID string
	Role          string
	ExecMode      string
	ThinkInterval int
	MaxIterations int
	PromptBuilder func(card coreorchestration.Card) string
	Now           func() time.Time
}

// BoardCandidateSource converts `Todo` board cards into scheduler candidates.
type BoardCandidateSource struct {
	reader        BoardReader
	workspaceID   string
	parentAgentID string
	role          string
	execMode      string
	thinkInterval int
	maxIterations int
	promptBuilder func(card coreorchestration.Card) string
	now           func() time.Time
}

// NewBoardCandidateSource builds a projection-backed candidate source.
func NewBoardCandidateSource(cfg BoardCandidateSourceConfig) (*BoardCandidateSource, error) {
	if cfg.Reader == nil {
		return nil, fmt.Errorf("orchestration board candidate source requires reader")
	}
	role := strings.TrimSpace(cfg.Role)
	if role == "" {
		role = defaultCandidateRole
	}
	execMode := strings.TrimSpace(cfg.ExecMode)
	if execMode == "" {
		execMode = defaultCandidateExecMode
	}
	builder := cfg.PromptBuilder
	if builder == nil {
		builder = defaultCandidatePrompt
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &BoardCandidateSource{
		reader:        cfg.Reader,
		workspaceID:   strings.TrimSpace(cfg.WorkspaceID),
		parentAgentID: strings.TrimSpace(cfg.ParentAgentID),
		role:          role,
		execMode:      execMode,
		thinkInterval: cfg.ThinkInterval,
		maxIterations: cfg.MaxIterations,
		promptBuilder: builder,
		now:           now,
	}, nil
}

// ListCandidates returns deterministic due RetryQueued candidates first, then Todo-lane candidates.
func (s *BoardCandidateSource) ListCandidates(ctx context.Context, limit int) ([]Candidate, error) {
	if s == nil || s.reader == nil {
		return nil, fmt.Errorf("orchestration board candidate source is not configured")
	}
	if limit <= 0 {
		return nil, nil
	}

	out := make([]Candidate, 0, limit)
	retryCandidates, err := s.listLaneCandidates(ctx, coreorchestration.LaneRetryQueue, max(limit, maxCandidateScanLimit), true)
	if err != nil {
		return nil, err
	}
	out = append(out, retryCandidates...)
	if len(out) >= limit {
		return out[:limit], nil
	}

	todoCandidates, err := s.listLaneCandidates(ctx, coreorchestration.LaneTodo, limit-len(out), false)
	if err != nil {
		return nil, err
	}
	out = append(out, todoCandidates...)
	return out, nil
}

func (s *BoardCandidateSource) workspaceIDFor(card coreorchestration.Card) string {
	if trimmed := strings.TrimSpace(card.WorkspaceID); trimmed != "" {
		return trimmed
	}
	if s.workspaceID != "" {
		return s.workspaceID
	}
	return ""
}

func defaultCandidatePrompt(card coreorchestration.Card) string {
	identifier := strings.TrimSpace(card.IssueIdentifier)
	title := strings.TrimSpace(card.Title)
	switch {
	case identifier != "" && title != "":
		return fmt.Sprintf("Work on issue %s: %s", identifier, title)
	case title != "":
		return "Work on issue: " + title
	case identifier != "":
		return "Work on issue " + identifier
	default:
		return ""
	}
}

func (s *BoardCandidateSource) listLaneCandidates(ctx context.Context, lane coreorchestration.Lane, limit int, onlyDue bool) ([]Candidate, error) {
	if limit <= 0 {
		return nil, nil
	}
	board, err := s.reader.Board(ctx, coreorchestration.BoardRequest{
		WorkspaceID: s.workspaceID,
		Limit:       limit,
		Lane:        lane,
	})
	if err != nil {
		return nil, err
	}

	now := s.now().UTC()
	out := make([]Candidate, 0, limit)
	for _, column := range board.Lanes {
		for _, card := range column.Cards {
			if onlyDue && !retryDue(card, now) {
				continue
			}
			issueID := strings.TrimSpace(card.IssueID)
			if issueID == "" {
				continue
			}
			out = append(out, Candidate{
				WorkspaceID:     strings.TrimSpace(s.workspaceIDFor(card)),
				IssueID:         issueID,
				IssueIdentifier: strings.TrimSpace(card.IssueIdentifier),
				Title:           strings.TrimSpace(card.Title),
				ParentAgentID:   s.parentAgentID,
				Role:            s.role,
				Prompt:          strings.TrimSpace(s.promptBuilder(card)),
				ExecMode:        s.execMode,
				ThinkInterval:   s.thinkInterval,
				MaxIterations:   s.maxIterations,
				Attempt:         card.Attempt,
			})
			if len(out) == limit {
				return out, nil
			}
		}
	}
	return out, nil
}

func retryDue(card coreorchestration.Card, now time.Time) bool {
	if card.RetryDueAt == nil {
		return true
	}
	return !card.RetryDueAt.UTC().After(now.UTC())
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
