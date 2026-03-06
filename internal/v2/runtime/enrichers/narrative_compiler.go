package enrichers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jkatigb/agentctl/internal/observability"
	coreevents "github.com/jkatigb/agentctl/internal/v2/core/events"
	"github.com/jkatigb/agentctl/internal/v2/core/run"
)

var (
	// ErrMissingNarrativeReader indicates narrative reader dependency is nil.
	ErrMissingNarrativeReader = errors.New("v2 enrichers: missing narrative reader")
	// ErrMissingNarrativeWriter indicates narrative writer dependency is nil.
	ErrMissingNarrativeWriter = errors.New("v2 enrichers: missing narrative writer")
	// ErrMissingNarrativeCompiler indicates narrative compiler dependency is nil.
	ErrMissingNarrativeCompiler = errors.New("v2 enrichers: missing narrative compiler")
	// ErrMissingTurnTimelineReader indicates timeline reader dependency is nil.
	ErrMissingTurnTimelineReader = errors.New("v2 enrichers: missing turn timeline reader")
)

const (
	defaultNarrativeArtifactVersion = "v1"
	defaultNarrativeTurnsTrigger    = 12
	defaultNarrativeMaxAge          = 30 * time.Minute
	defaultNarrativeWindow          = 96
)

// NarrativeRefreshTrigger identifies why a narrative refresh was performed.
type NarrativeRefreshTrigger string

const (
	NarrativeRefreshInitial NarrativeRefreshTrigger = "initial"
	NarrativeRefreshEvent   NarrativeRefreshTrigger = "event"
	NarrativeRefreshTime    NarrativeRefreshTrigger = "time"
	NarrativeRefreshManual  NarrativeRefreshTrigger = "manual"
)

// NarrativeCompileInput is one narrative synthesis request.
type NarrativeCompileInput struct {
	SessionID string
	Turns     []run.TurnRecord
	Previous  *run.NarrativeRecord
	Trigger   NarrativeRefreshTrigger
	Now       time.Time
}

// NarrativeCompiler derives one session-scoped narrative artifact snapshot.
type NarrativeCompiler interface {
	Compile(ctx context.Context, input NarrativeCompileInput) (run.NarrativeRecord, error)
}

// DeterministicNarrativeCompiler is a cheap, deterministic compiler for local/runtime use.
type DeterministicNarrativeCompiler struct {
	MaxClaims int
}

// Compile emits claims anchored to recent turns with no model dependency.
func (c DeterministicNarrativeCompiler) Compile(_ context.Context, input NarrativeCompileInput) (run.NarrativeRecord, error) {
	turns := cloneTurns(input.Turns)
	if len(turns) == 0 {
		return run.NarrativeRecord{}, nil
	}

	maxClaims := c.MaxClaims
	if maxClaims <= 0 {
		maxClaims = 6
	}
	if maxClaims > 16 {
		maxClaims = 16
	}

	now := input.Now.UTC()
	if now.IsZero() {
		return run.NarrativeRecord{}, fmt.Errorf("narrative compile: now timestamp is required")
	}

	start := 0
	if len(turns) > maxClaims {
		start = len(turns) - maxClaims
	}
	recent := turns[start:]
	latest := recent[len(recent)-1]

	claims := make([]run.NarrativeClaim, 0, len(recent))
	for _, turn := range recent {
		turnID := strings.TrimSpace(turn.ID)
		if turnID == "" {
			continue
		}
		text := strings.TrimSpace(turn.FinalOutput.Text)
		if text == "" {
			text = strings.TrimSpace(turn.Prompt)
		}
		if text == "" {
			continue
		}
		claims = append(claims, run.NarrativeClaim{
			Text: truncateRunes(text, 180),
			AnchorRefs: []string{
				fmt.Sprintf("turn/%s", turnID),
			},
		})
	}
	if len(claims) == 0 {
		return run.NarrativeRecord{}, nil
	}

	summaryParts := make([]string, 0, 2)
	for _, claim := range claims {
		part := strings.TrimSpace(claim.Text)
		if part == "" {
			continue
		}
		summaryParts = append(summaryParts, truncateRunes(part, 120))
		if len(summaryParts) >= 2 {
			break
		}
	}

	anchorRefs := make([]string, 0, len(claims))
	for _, claim := range claims {
		anchorRefs = append(anchorRefs, claim.AnchorRefs...)
	}

	return run.NarrativeRecord{
		SessionID:       strings.TrimSpace(input.SessionID),
		ArtifactVersion: defaultNarrativeArtifactVersion,
		Summary:         strings.Join(summaryParts, " "),
		Claims:          claims,
		AnchorRefs:      uniqueStrings(anchorRefs),
		SourceTurnID:    strings.TrimSpace(latest.ID),
		SourceTurnIndex: latest.TurnIndex,
		SourceTurnCount: len(turns),
		UpdatedAt:       now,
	}, nil
}

// NarrativeCompilerConfig wires turn.recorded -> narrative compile refreshes.
type NarrativeCompilerConfig struct {
	Bus                EventSubscriber
	TurnReader         run.TurnReader
	TurnTimelineReader run.TurnTimelineReader
	NarrativeReader    run.NarrativeReader
	NarrativeWriter    run.NarrativeWriter
	Compiler           NarrativeCompiler
	Buffer             int
	ArtifactVersion    string
	TurnsTrigger       int
	MaxAge             time.Duration
	MaxTurns           int
	Now                func() time.Time
	OnError            func(error)
}

// NarrativeCompilerComponent asynchronously compiles session-scoped narratives.
type NarrativeCompilerComponent struct {
	bus             EventSubscriber
	turnReader      run.TurnReader
	turnTimeline    run.TurnTimelineReader
	narrativeReader run.NarrativeReader
	narrativeWriter run.NarrativeWriter
	compiler        NarrativeCompiler
	buffer          int
	artifactVersion string
	turnsTrigger    int
	maxAge          time.Duration
	maxTurns        int
	now             func() time.Time
	onError         func(error)
}

// NewNarrativeCompilerComponent creates a non-blocking narrative compiler component.
func NewNarrativeCompilerComponent(cfg NarrativeCompilerConfig) *NarrativeCompilerComponent {
	if cfg.Buffer <= 0 {
		cfg.Buffer = defaultProducerBuffer
	}
	if cfg.Compiler == nil {
		cfg.Compiler = DeterministicNarrativeCompiler{}
	}
	if cfg.TurnsTrigger <= 0 {
		cfg.TurnsTrigger = defaultNarrativeTurnsTrigger
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = defaultNarrativeMaxAge
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = defaultNarrativeWindow
	}
	if cfg.ArtifactVersion == "" {
		cfg.ArtifactVersion = defaultNarrativeArtifactVersion
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.OnError == nil {
		cfg.OnError = func(err error) {
			if err == nil {
				return
			}
			observability.Emit(context.Background(), observability.NewEvent("v2.runtime.enricher.narrative.error").
				WithComponent(observability.ComponentAgent).
				Error(err, 0))
		}
	}

	return &NarrativeCompilerComponent{
		bus:             cfg.Bus,
		turnReader:      cfg.TurnReader,
		turnTimeline:    cfg.TurnTimelineReader,
		narrativeReader: cfg.NarrativeReader,
		narrativeWriter: cfg.NarrativeWriter,
		compiler:        cfg.Compiler,
		buffer:          cfg.Buffer,
		artifactVersion: strings.TrimSpace(cfg.ArtifactVersion),
		turnsTrigger:    cfg.TurnsTrigger,
		maxAge:          cfg.MaxAge,
		maxTurns:        cfg.MaxTurns,
		now:             cfg.Now,
		onError:         cfg.OnError,
	}
}

// Run subscribes to runtime events and refreshes narratives without blocking turns.
func (c *NarrativeCompilerComponent) Run(ctx context.Context) error {
	if c == nil || c.bus == nil {
		return ErrMissingSubscriber
	}
	if c.turnReader == nil {
		return ErrMissingTurnReader
	}
	if c.turnTimeline == nil {
		return ErrMissingTurnTimelineReader
	}
	if c.narrativeReader == nil {
		return ErrMissingNarrativeReader
	}
	if c.narrativeWriter == nil {
		return ErrMissingNarrativeWriter
	}
	if c.compiler == nil {
		return ErrMissingNarrativeCompiler
	}

	eventsCh, unsubscribe := c.bus.Subscribe(c.buffer)
	defer unsubscribe()
	jobs := make(chan coreevents.Event, c.buffer)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-jobs:
				if !ok {
					return
				}
				c.handleEvent(ctx, evt)
			}
		}
	}()
	defer func() {
		close(jobs)
		wg.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			return nil
		case evt, ok := <-eventsCh:
			if !ok {
				return nil
			}
			select {
			case jobs <- evt:
			default:
				c.onError(fmt.Errorf("v2 enrichers: dropped narrative compile event type=%s stream_id=%s", evt.EventType, evt.StreamID))
			}
		}
	}
}

// RefreshSession performs explicit/manual narrative refresh for one session.
func (c *NarrativeCompilerComponent) RefreshSession(ctx context.Context, sessionID string) error {
	if c == nil {
		return ErrMissingNarrativeCompiler
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	latestTurn, ok, err := c.latestTurn(ctx, sessionID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return c.refreshSession(ctx, latestTurn, NarrativeRefreshManual)
}

func (c *NarrativeCompilerComponent) handleEvent(ctx context.Context, evt coreevents.Event) {
	if evt.EventType != coreevents.EventTurnRecorded {
		return
	}
	turnID, err := turnIDFromEvent(evt)
	if err != nil {
		c.onError(err)
		return
	}
	turn, err := c.turnReader.GetTurn(ctx, turnID)
	if err != nil {
		c.onError(err)
		return
	}
	if err := c.refreshSession(ctx, turn, NarrativeRefreshEvent); err != nil {
		c.onError(err)
	}
}

func (c *NarrativeCompilerComponent) refreshSession(ctx context.Context, latestTurn run.TurnRecord, trigger NarrativeRefreshTrigger) error {
	sessionID := strings.TrimSpace(latestTurn.SessionID)
	if sessionID == "" {
		return nil
	}

	previous, hasPrevious, err := c.loadPrevious(ctx, sessionID)
	if err != nil {
		return err
	}

	now := c.now().UTC()
	shouldRefresh, resolvedTrigger := c.shouldRefresh(previous, hasPrevious, latestTurn, trigger, now)
	if !shouldRefresh {
		return nil
	}

	turns, err := c.turnTimeline.ListTurns(ctx, sessionID, run.TurnListOptions{
		Limit: c.maxTurns,
		Asc:   true,
	})
	if err != nil {
		return fmt.Errorf("list turns for narrative: %w", err)
	}
	if len(turns) == 0 {
		return nil
	}

	input := NarrativeCompileInput{
		SessionID: sessionID,
		Turns:     cloneTurns(turns),
		Trigger:   resolvedTrigger,
		Now:       now,
	}
	if hasPrevious {
		prev := previous.Clone()
		input.Previous = &prev
	}
	out, err := c.compiler.Compile(ctx, input)
	if err != nil {
		return err
	}
	out = out.Clone()
	if len(out.Claims) == 0 {
		return nil
	}
	if strings.TrimSpace(out.SessionID) == "" {
		out.SessionID = sessionID
	}
	if strings.TrimSpace(out.ArtifactVersion) == "" {
		out.ArtifactVersion = c.artifactVersion
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = now
	}
	if strings.TrimSpace(out.SourceTurnID) == "" {
		out.SourceTurnID = strings.TrimSpace(latestTurn.ID)
	}
	if out.SourceTurnIndex <= 0 {
		out.SourceTurnIndex = latestTurn.TurnIndex
	}
	if out.SourceTurnCount <= 0 {
		out.SourceTurnCount = len(turns)
	}
	if len(out.AnchorRefs) == 0 {
		refs := make([]string, 0, len(out.Claims))
		for _, claim := range out.Claims {
			refs = append(refs, claim.AnchorRefs...)
		}
		out.AnchorRefs = uniqueStrings(refs)
	}
	return c.narrativeWriter.SaveNarrative(ctx, out)
}

func (c *NarrativeCompilerComponent) loadPrevious(ctx context.Context, sessionID string) (run.NarrativeRecord, bool, error) {
	previous, err := c.narrativeReader.GetNarrative(ctx, sessionID, c.artifactVersion)
	if errors.Is(err, run.ErrNarrativeNotFound) {
		return run.NarrativeRecord{}, false, nil
	}
	if err != nil {
		return run.NarrativeRecord{}, false, fmt.Errorf("load previous narrative: %w", err)
	}
	return previous.Clone(), true, nil
}

func (c *NarrativeCompilerComponent) shouldRefresh(
	previous run.NarrativeRecord,
	hasPrevious bool,
	latestTurn run.TurnRecord,
	trigger NarrativeRefreshTrigger,
	now time.Time,
) (bool, NarrativeRefreshTrigger) {
	if trigger == NarrativeRefreshManual {
		return true, NarrativeRefreshManual
	}
	if !hasPrevious {
		return true, NarrativeRefreshInitial
	}

	if !previous.UpdatedAt.IsZero() && c.maxAge > 0 {
		age := now.Sub(previous.UpdatedAt.UTC())
		if age < 0 {
			age = 0
		}
		if age >= c.maxAge {
			return true, NarrativeRefreshTime
		}
	}

	if c.turnsTrigger > 0 && latestTurn.TurnIndex > 0 {
		if previous.SourceTurnIndex > 0 {
			if latestTurn.TurnIndex-previous.SourceTurnIndex >= c.turnsTrigger {
				return true, NarrativeRefreshEvent
			}
			return false, NarrativeRefreshEvent
		}
		if latestTurn.TurnIndex%c.turnsTrigger == 0 {
			return true, NarrativeRefreshEvent
		}
	}

	return false, trigger
}

func (c *NarrativeCompilerComponent) latestTurn(ctx context.Context, sessionID string) (run.TurnRecord, bool, error) {
	turns, err := c.turnTimeline.ListTurns(ctx, sessionID, run.TurnListOptions{
		Limit: 1,
		Asc:   false,
	})
	if err != nil {
		return run.TurnRecord{}, false, fmt.Errorf("load latest turn: %w", err)
	}
	if len(turns) == 0 {
		return run.TurnRecord{}, false, nil
	}
	return turns[0].Clone(), true, nil
}

func cloneTurns(turns []run.TurnRecord) []run.TurnRecord {
	if len(turns) == 0 {
		return nil
	}
	out := make([]run.TurnRecord, len(turns))
	for i := range turns {
		out[i] = turns[i].Clone()
	}
	return out
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func truncateRunes(text string, max int) string {
	text = strings.TrimSpace(text)
	if max <= 0 || text == "" {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "..."
}
