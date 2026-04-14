package runner

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	v2errors "github.com/joshka0/foxctl/internal/v2/core/errors"
	"github.com/joshka0/foxctl/internal/v2/core/events"
	"github.com/joshka0/foxctl/internal/v2/core/run"
)

type stageFunc func(ctx context.Context, st *executionState) *v2errors.V2Error

type stageDef struct {
	name string
	fn   stageFunc
}

type executionState struct {
	in            run.TurnInput
	out           run.TurnOutput
	turn          run.TurnRecord
	streamVersion int64
	sequence      int64
}

// Pipeline executes the canonical v2 runner stage sequence.
type Pipeline struct {
	cfg Config

	turnSeqMu sync.Mutex
	turnSeq   map[string]int
	turnSeen  map[string]uint64
	turnSeed  map[string]chan struct{}
	turnTick  uint64
}

var fallbackIDCounter atomic.Uint64

const turnSeqMaxEntries = 2048

// New builds a pipeline with deterministic-friendly defaults.
func New(cfg Config) *Pipeline {
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.NewID == nil {
		cfg.NewID = func() string {
			n := fallbackIDCounter.Add(1)
			return fmt.Sprintf("evt-%06d", n)
		}
	}
	if cfg.OnEventError == nil {
		cfg.OnEventError = func(error) {}
	}
	return &Pipeline{
		cfg:      cfg,
		turnSeq:  make(map[string]int),
		turnSeen: make(map[string]uint64),
		turnSeed: make(map[string]chan struct{}),
	}
}

// RunTurn executes one turn through the canonical stage sequence.
func (p *Pipeline) RunTurn(ctx context.Context, in run.TurnInput) (run.TurnOutput, error) {
	st := &executionState{in: in, out: run.TurnOutput{TurnID: strings.TrimSpace(in.TurnID)}}

	stages := []stageDef{
		{name: StageInitContext, fn: p.stageInitContext},
		{name: StageResolveDependencies, fn: p.stageResolveDependencies},
		{name: StageApplyPreHooks, fn: p.stageApplyPreHooks},
		{name: StageBuildToolset, fn: p.stageBuildToolset},
		{name: StageModelCall, fn: p.stageModelCall},
		{name: StageApplyPostHooks, fn: p.stageApplyPostHooks},
		{name: StagePersistTurn, fn: p.stagePersistTurn},
		{name: StageEmitEvents, fn: p.stageEmitEvents},
	}

	for _, stage := range stages {
		p.observe(stage.name)
		if err := ctx.Err(); err != nil {
			verr := contextError(stage.name, err)
			_ = p.emitRunFailed(ctx, st, stage.name, verr)
			return st.out, verr
		}

		verr := stage.fn(ctx, st)
		if verr == nil {
			continue
		}
		if verr.Kind == "" {
			verr.Kind = v2errors.ErrStageFailed
		}

		// Only stage_failed/non-fatal errors degrade and continue.
		if verr.Kind == v2errors.ErrStageFailed && !verr.IsFatal() {
			p.recordStageFailure(st, stage.name, verr)
			if emitErr := p.emitStageFailed(ctx, st, stage.name, verr); emitErr != nil {
				return st.out, emitErr
			}
			continue
		}

		if emitErr := p.emitRunFailed(ctx, st, stage.name, verr); emitErr != nil {
			return st.out, emitErr
		}
		return st.out, verr
	}

	return st.out, nil
}

func (p *Pipeline) observe(stageName string) {
	if p.cfg.ObserveStage != nil {
		p.cfg.ObserveStage(stageName)
	}
}

func (p *Pipeline) recordStageFailure(st *executionState, stageName string, verr *v2errors.V2Error) {
	st.out.Degraded = true
	st.out.StageFailures = append(st.out.StageFailures, run.StageFailure{
		Stage:     stageName,
		Kind:      string(verr.Kind),
		Message:   verr.Error(),
		Fatal:     verr.IsFatal(),
		Retryable: verr.Retryable,
	})
}

func (p *Pipeline) appendEvent(ctx context.Context, st *executionState, stageName string, evtType events.EventType, payload any) *v2errors.V2Error {
	if err := ctx.Err(); err != nil {
		return contextError(stageName, err)
	}
	if p.cfg.EventStore == nil {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrDependency,
			Message: "missing event appender",
			Fatal:   true,
		}
	}

	raw, err := events.MarshalPayload(payload)
	if err != nil {
		return &v2errors.V2Error{
			Kind:    v2errors.ErrInternal,
			Message: "marshal event payload",
			Cause:   err,
			Fatal:   true,
		}
	}

	st.sequence++
	st.streamVersion++
	evt := events.Event{
		ID:            p.cfg.NewID(),
		StreamID:      st.in.RunID,
		StreamType:    events.StreamTypeRun,
		StreamVersion: st.streamVersion,
		Sequence:      st.sequence,
		EventType:     evtType,
		OccurredAt:    p.cfg.Now().UTC(),
		CorrelationID: st.in.CorrelationID,
		CausationID:   st.in.CausationID,
		ActorID:       st.in.ActorID,
		RequestID:     st.in.RequestID,
		Command:       st.in.Command,
		Payload:       raw,
	}

	if err := p.cfg.EventStore.Append(ctx, evt); err != nil {
		return &v2errors.V2Error{
			Kind:      v2errors.ErrDependency,
			Message:   "append event",
			Cause:     err,
			Fatal:     true,
			Retryable: true,
		}
	}
	if p.cfg.EventBus != nil {
		// Runtime bus fanout is best-effort background wiring; event store append
		// remains the authoritative path and must not fail when subscribers lag.
		if err := p.cfg.EventBus.Publish(ctx, evt.Clone()); err != nil {
			p.cfg.OnEventError(err)
		}
	}
	return nil
}

func (p *Pipeline) emitStageFailed(ctx context.Context, st *executionState, stageName string, verr *v2errors.V2Error) *v2errors.V2Error {
	return p.appendEvent(ctx, st, stageName, events.EventStageFailed, events.ErrorPayload{
		Kind:       string(verr.Kind),
		Message:    fmt.Sprintf("%s: %s", stageName, verr.Error()),
		Fatal:      false,
		Retryable:  verr.Retryable,
		HTTPStatus: verr.HTTPStatus(),
		Details: map[string]any{
			"stage": stageName,
		},
		EnvelopeCode: verr.EnvelopeCode(),
	})
}

func (p *Pipeline) emitRunFailed(ctx context.Context, st *executionState, stageName string, verr *v2errors.V2Error) *v2errors.V2Error {
	payload := events.ErrorPayload{
		Kind:         string(verr.Kind),
		Message:      fmt.Sprintf("%s: %s", stageName, verr.Error()),
		Fatal:        verr.IsFatal(),
		Retryable:    verr.Retryable,
		HTTPStatus:   verr.HTTPStatus(),
		EnvelopeCode: verr.EnvelopeCode(),
		Details: map[string]any{
			"stage": stageName,
		},
	}
	if verr.Cause != nil {
		payload.Cause = verr.Cause.Error()
	}
	return p.appendEvent(ctx, st, stageName, events.EventRunFailed, payload)
}

func asStageError(stageName string, err error, fatal bool) *v2errors.V2Error {
	if err == nil {
		return nil
	}
	var verr *v2errors.V2Error
	if stderrors.As(err, &verr) {
		if verr.Kind == "" {
			verr.Kind = v2errors.ErrStageFailed
		}
		return verr
	}
	if stderrors.Is(err, context.Canceled) || stderrors.Is(err, context.DeadlineExceeded) {
		return contextError(stageName, err)
	}
	return &v2errors.V2Error{
		Kind:    v2errors.ErrStageFailed,
		Message: fmt.Sprintf("%s failed", stageName),
		Cause:   err,
		Fatal:   fatal,
	}
}

func contextError(stageName string, err error) *v2errors.V2Error {
	return &v2errors.V2Error{
		Kind:      v2errors.ErrTimeout,
		Message:   fmt.Sprintf("%s canceled", stageName),
		Cause:     err,
		Fatal:     true,
		Retryable: true,
	}
}

func (p *Pipeline) nextTurnIndex(ctx context.Context, runID string) int {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return 1
	}

	for {
		p.turnSeqMu.Lock()
		if next, ok := p.turnSeq[runID]; ok {
			next++
			p.turnSeq[runID] = next
			p.touchTurnSeqLocked(runID)
			p.turnSeqMu.Unlock()
			return next
		}
		if waitCh, ok := p.turnSeed[runID]; ok {
			p.turnSeqMu.Unlock()
			select {
			case <-ctx.Done():
				p.turnSeqMu.Lock()
				if ch, ok := p.turnSeed[runID]; ok && ch == waitCh {
					delete(p.turnSeed, runID)
					close(waitCh)
				}
				p.turnSeqMu.Unlock()
				return 1
			case <-waitCh:
				continue
			}
		}

		waitCh := make(chan struct{})
		p.turnSeed[runID] = waitCh
		p.turnSeqMu.Unlock()

		last := p.lookupLastTurnIndex(ctx, runID)
		next := last + 1
		if next <= 0 {
			next = 1
		}

		p.turnSeqMu.Lock()
		if ch, ok := p.turnSeed[runID]; ok && ch == waitCh {
			delete(p.turnSeed, runID)
			close(waitCh)
		}
		if current, ok := p.turnSeq[runID]; ok {
			current++
			p.turnSeq[runID] = current
			p.touchTurnSeqLocked(runID)
			p.turnSeqMu.Unlock()
			return current
		}

		p.turnSeq[runID] = next
		p.touchTurnSeqLocked(runID)
		p.evictTurnSeqLocked()
		p.turnSeqMu.Unlock()
		return next
	}
}

func (p *Pipeline) lookupLastTurnIndex(ctx context.Context, runID string) int {
	timelineReader, ok := p.cfg.TurnRecorder.(run.TurnTimelineReader)
	if !ok || timelineReader == nil {
		return 0
	}

	turns, err := timelineReader.ListTurns(ctx, runID, run.TurnListOptions{
		Limit: 1,
		Asc:   false,
	})
	if err != nil {
		if p.cfg.OnEventError != nil {
			p.cfg.OnEventError(fmt.Errorf("v2 runner: resolve turn index from timeline: %w", err))
		}
		return 0
	}
	if len(turns) == 0 {
		return 0
	}
	if turns[0].TurnIndex <= 0 {
		return 0
	}
	return turns[0].TurnIndex
}

func (p *Pipeline) touchTurnSeqLocked(runID string) {
	if p.turnSeen == nil {
		p.turnSeen = make(map[string]uint64)
	}
	p.turnTick++
	p.turnSeen[runID] = p.turnTick
}

func (p *Pipeline) evictTurnSeqLocked() {
	if len(p.turnSeq) <= turnSeqMaxEntries {
		return
	}

	oldestID := ""
	oldestTS := uint64(0)
	for runID := range p.turnSeq {
		ts, ok := p.turnSeen[runID]
		if !ok {
			oldestID = runID
			break
		}
		if oldestID == "" || ts < oldestTS {
			oldestID = runID
			oldestTS = ts
		}
	}
	if oldestID == "" {
		return
	}
	delete(p.turnSeq, oldestID)
	delete(p.turnSeen, oldestID)
}
