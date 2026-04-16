package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tui "github.com/grindlemire/go-tui"
)

const defaultComposerAskEnqueueTimeout = 250 * time.Millisecond

type Shell struct {
	state           *tui.State[ShellState]
	transcriptFocus *tui.State[bool]
	composerFocus   *tui.State[bool]
	railFocus       *tui.State[bool]
	workersFocus    *tui.State[bool]
	focus           *tui.FocusGroup
	streamUpdates   <-chan ConsoleStreamUpdate
	askUpdates      <-chan ConsoleAskUpdate
	enqueueAsk      func(context.Context, AskConsoleSessionRequest) error
	askTimeout      time.Duration
	transcriptLimit int
}

func NewShell(initial ShellState) *Shell {
	return NewShellWithRuntime(initial, nil, nil, nil, defaultTranscriptLimit, defaultComposerAskEnqueueTimeout)
}

func NewShellWithStream(initial ShellState, streamUpdates <-chan ConsoleStreamUpdate, transcriptLimit int) *Shell {
	return NewShellWithRuntime(
		initial,
		streamUpdates,
		nil,
		nil,
		transcriptLimit,
		defaultComposerAskEnqueueTimeout,
	)
}

func NewShellWithRuntime(
	initial ShellState,
	streamUpdates <-chan ConsoleStreamUpdate,
	askUpdates <-chan ConsoleAskUpdate,
	enqueueAsk func(context.Context, AskConsoleSessionRequest) error,
	transcriptLimit int,
	askTimeout time.Duration,
) *Shell {
	if askTimeout <= 0 {
		askTimeout = defaultComposerAskEnqueueTimeout
	}

	transcriptFocus := tui.NewState(true)
	composerFocus := tui.NewState(false)
	railFocus := tui.NewState(false)
	workersFocus := tui.NewState(false)

	return &Shell{
		state:           tui.NewState(initial),
		transcriptFocus: transcriptFocus,
		composerFocus:   composerFocus,
		railFocus:       railFocus,
		workersFocus:    workersFocus,
		focus:           tui.MustNewFocusGroup(transcriptFocus, composerFocus, railFocus, workersFocus),
		streamUpdates:   streamUpdates,
		askUpdates:      askUpdates,
		enqueueAsk:      enqueueAsk,
		askTimeout:      askTimeout,
		transcriptLimit: transcriptLimit,
	}
}

func (s *Shell) Watchers() []tui.Watcher {
	watchers := make([]tui.Watcher, 0, 2)
	if s.streamUpdates != nil {
		watchers = append(watchers, tui.Watch(s.streamUpdates, s.handleConsoleStreamUpdate))
	}
	if s.askUpdates != nil {
		watchers = append(watchers, tui.Watch(s.askUpdates, s.handleConsoleAskUpdate))
	}
	if len(watchers) == 0 {
		return nil
	}
	return watchers
}

func (s *Shell) handleConsoleStreamUpdate(update ConsoleStreamUpdate) {
	switch update.Type {
	case ConsoleStreamUpdateEvent:
		s.state.Update(func(state ShellState) ShellState {
			return state.ApplyConsoleStreamEvent(update.Event, s.transcriptLimit)
		})
	case ConsoleStreamUpdateError:
		msg := "console stream error"
		if update.Err != nil {
			msg = "console stream error: " + update.Err.Error()
		}
		s.appendTranscriptEntry(TranscriptEntry{
			Speaker: "system",
			Kind:    "error",
			Text:    msg,
		})
	case ConsoleStreamUpdateDone:
		s.appendTranscriptEntry(TranscriptEntry{
			Speaker: "system",
			Kind:    "status",
			Text:    "console stream closed",
		})
	}
}

func (s *Shell) handleConsoleAskUpdate(update ConsoleAskUpdate) {
	switch update.Type {
	case ConsoleAskUpdateAccepted:
		text := "ask queued"
		if update.Accepted != nil && strings.TrimSpace(update.Accepted.CorrelationID) != "" {
			text = "ask queued: " + strings.TrimSpace(update.Accepted.CorrelationID)
		}
		s.appendTranscriptEntry(TranscriptEntry{
			Speaker: "system",
			Kind:    "status",
			Text:    text,
		})
	case ConsoleAskUpdateError:
		text := "ask failed"
		if update.Failed != nil && update.Failed.Err != nil {
			text = "ask failed: " + update.Failed.Err.Error()
		}
		s.appendTranscriptEntry(TranscriptEntry{
			Speaker: "system",
			Kind:    "error",
			Text:    text,
		})
	}
}

func (s *Shell) appendTranscriptEntry(entry TranscriptEntry) {
	s.state.Update(func(state ShellState) ShellState {
		transcript := append([]TranscriptEntry(nil), state.Transcript...)
		transcript = append(transcript, entry)
		state.Transcript = capTranscriptEntries(transcript, s.transcriptLimit)
		return state
	})
}

func (s *Shell) KeyMap() tui.KeyMap {
	keyMap := append(tui.KeyMap{}, s.focus.KeyMap()...)
	keyMap = append(keyMap, stopBindings()...)
	keyMap = append(keyMap,
		tui.On(tui.Rune('m').Ctrl(), func(ke tui.KeyEvent) { s.setRail(RailMemory) }),
		tui.On(tui.Rune('y').Ctrl(), func(ke tui.KeyEvent) { s.setRail(RailContinuity) }),
		tui.On(tui.Rune('w').Ctrl(), func(ke tui.KeyEvent) { s.setRail(RailWorkers) }),
		tui.On(tui.Rune('b').Ctrl(), func(ke tui.KeyEvent) { s.setRail(RailTask) }),
		tui.On(tui.KeyRight, func(ke tui.KeyEvent) { s.cycleRail(1) }),
		tui.On(tui.KeyLeft, func(ke tui.KeyEvent) { s.cycleRail(-1) }),
		tui.On(tui.AnyRune, func(ke tui.KeyEvent) {
			if s.activeFocus() != FocusComposer {
				return
			}
			s.updateComposer(string(ke.Rune))
		}),
		tui.On(tui.KeyBackspace, func(ke tui.KeyEvent) {
			if s.activeFocus() != FocusComposer {
				return
			}
			s.backspaceComposer()
		}),
		tui.On(tui.KeyEnter, func(ke tui.KeyEvent) {
			if s.activeFocus() != FocusComposer {
				return
			}
			s.submitComposer()
		}),
	)
	return keyMap
}

func (s *Shell) activeFocus() FocusPane {
	return focusPaneForIndex(s.focus.Current())
}

func (s *Shell) isFocused(pane FocusPane) bool {
	return s.activeFocus() == pane
}

func (s *Shell) setRail(tab RailTab) {
	s.state.Update(func(state ShellState) ShellState {
		state.ActiveRail = tab
		return state
	})
}

func (s *Shell) cycleRail(delta int) {
	s.state.Update(func(state ShellState) ShellState {
		state.ActiveRail = nextRail(state.ActiveRail, delta)
		return state
	})
}

func (s *Shell) updateComposer(text string) {
	s.state.Update(func(state ShellState) ShellState {
		state.Composer += text
		return state
	})
}

func (s *Shell) backspaceComposer() {
	s.state.Update(func(state ShellState) ShellState {
		if state.Composer == "" {
			return state
		}
		runes := []rune(state.Composer)
		state.Composer = string(runes[:len(runes)-1])
		return state
	})
}

func (s *Shell) submitComposer() {
	if s.enqueueAsk == nil {
		s.state.Update(func(state ShellState) ShellState {
			text := strings.TrimSpace(state.Composer)
			if text == "" {
				return state
			}
			state.Transcript = append(state.Transcript, TranscriptEntry{
				Speaker: "you",
				Kind:    "draft",
				Text:    text,
			})
			state.Composer = ""
			return state
		})
		return
	}

	current := s.state.Get()
	content := strings.TrimSpace(current.Composer)
	if content == "" {
		return
	}

	s.state.Update(func(state ShellState) ShellState {
		state.Composer = ""
		transcript := append([]TranscriptEntry(nil), state.Transcript...)
		transcript = append(transcript, TranscriptEntry{
			Speaker: "you",
			Kind:    "pending",
			Text:    content,
		})
		state.Transcript = capTranscriptEntries(
			transcript,
			s.transcriptLimit,
		)
		return state
	})

	enqueueCtx, cancel := context.WithTimeout(context.Background(), s.askTimeout)
	defer cancel()
	if err := s.enqueueAsk(enqueueCtx, AskConsoleSessionRequest{Content: content}); err != nil {
		s.appendTranscriptEntry(TranscriptEntry{
			Speaker: "system",
			Kind:    "error",
			Text:    "ask enqueue failed: " + err.Error(),
		})
	}
}

func focusClass(active bool) string {
	if active {
		return "border-cyan"
	}
	return "border-black"
}

func focusLabel(active bool) string {
	if active {
		return "focused"
	}
	return "idle"
}

func railTabClass(active bool) string {
	if active {
		return "text-cyan font-bold"
	}
	return "font-dim"
}

func composerText(text string) string {
	if text == "" {
		return "Type a draft instruction, Enter records it locally"
	}
	return text
}

func paneNames() string {
	return "Tab: focus | Shift+Tab: reverse | Ctrl+M/Y/W/B: rails | q/Esc/Ctrl+C: quit"
}

templ TopBar(state ShellState) {
	<div class="flex justify-between border-single p-1 shrink-0">
		<div class="flex gap-1">
			<span class="text-cyan font-bold">foxctl_tui</span>
			<span>{state.Workspace}</span>
			<span class="font-dim">{"|"}</span>
			<span>{state.EpicTitle}</span>
			<span class="text-green font-bold">{state.EpicStatus}</span>
		</div>
		<div class="flex gap-1">
			<span class="font-dim">{state.Assistant.Role}</span>
			<span>{state.Assistant.Name}</span>
			<span class="font-dim">{fmt.Sprintf("%s/%s", state.Assistant.Provider, state.Assistant.Model)}</span>
		</div>
	</div>
}

templ TranscriptPane(state ShellState, active bool) {
	<div class={"flex-col grow border-rounded p-1 gap-1 " + focusClass(active)}>
		<div class="flex justify-between shrink-0">
			<span class="font-bold text-cyan">Transcript</span>
			<span class="font-dim">{focusLabel(active)}</span>
		</div>
		<hr />
		<div class="flex-col gap-1 grow">
			for _, entry := range state.Transcript {
				<div class="flex-col border-single p-1">
					<span class="font-bold">{fmt.Sprintf("%s [%s]", entry.Speaker, entry.Kind)}</span>
					<span>{entry.Text}</span>
				</div>
			}
		</div>
	</div>
}

templ ComposerPane(state ShellState, active bool) {
	<div class={"flex-col border-rounded p-1 gap-1 shrink-0 " + focusClass(active)} height={5}>
		<div class="flex justify-between">
			<span class="font-bold text-cyan">Composer</span>
			<span class="font-dim">{focusLabel(active)}</span>
		</div>
		<span>{composerText(state.Composer)}</span>
	</div>
}

templ RailTabs(active RailTab) {
	<div class="flex gap-1 shrink-0">
		for _, tab := range railTabs() {
			<span class={railTabClass(tab == active)}>{tab.Label()}</span>
		}
	</div>
}

templ MemoryRail(items []MemorySummary) {
	<div class="flex-col gap-1 grow">
		for _, item := range items {
			<div class="flex-col border-single p-1">
				<span class="font-bold">{item.Title}</span>
				<span>{item.Summary}</span>
			</div>
		}
	</div>
}

templ ContinuityRail(item ContinuitySummary) {
	<div class="flex-col gap-1 grow">
		<span class="font-bold">{item.EpicID}</span>
		<span>{fmt.Sprintf("status: %s", item.Status)}</span>
		<span>{item.Boundary}</span>
		<hr />
		<span class="font-bold">Next</span>
		<span>{item.Next}</span>
	</div>
}

templ WorkersRail(workers []WorkerSummary, active bool) {
	<div class={"flex-col gap-1 grow border-rounded p-1 " + focusClass(active)}>
		<div class="flex justify-between">
			<span class="font-bold">Workers</span>
			<span class="font-dim">{focusLabel(active)}</span>
		</div>
		for _, worker := range workers {
			<div class="flex-col border-single p-1">
				<span class="font-bold">{fmt.Sprintf("%s: %s", worker.Name, worker.Status)}</span>
				<span>{worker.Task}</span>
			</div>
		}
	</div>
}

templ TaskRail() {
	<div class="flex-col gap-1 grow">
		<span class="font-bold">Current task</span>
		<span>{"Build Phase 0 shell and prove go-tui generation/build path."}</span>
		<hr />
		<span class="font-bold">Definition of done</span>
		<span>{"binary builds, generated .gsx committed, focus works, mocked panes visible"}</span>
	</div>
}

templ RightRail(state ShellState, railActive bool, workersActive bool) {
	<div class={"flex-col border-rounded p-1 gap-1 shrink-0 " + focusClass(railActive)} width={44}>
		<div class="flex justify-between">
			<span class="font-bold text-cyan">Context Rail</span>
			<span class="font-dim">{focusLabel(railActive)}</span>
		</div>
		@RailTabs(state.ActiveRail)
		<hr />
		if state.ActiveRail == RailMemory {
			@MemoryRail(state.Memory)
		} else if state.ActiveRail == RailContinuity {
			@ContinuityRail(state.Continuity)
		} else if state.ActiveRail == RailWorkers {
			@WorkersRail(state.Workers, workersActive)
		} else {
			@TaskRail()
		}
	</div>
}

templ Footer() {
	<div class="border-single p-1 shrink-0">
		<span class="font-dim">{paneNames()}</span>
	</div>
}

templ (s *Shell) Render() {
	<div class="flex-col h-full w-full" deps={s.state, s.transcriptFocus, s.composerFocus, s.railFocus, s.workersFocus}>
		@TopBar(s.state.Get())
		<div class="flex gap-1 grow p-1">
			<div class="flex-col gap-1 grow">
				@TranscriptPane(s.state.Get(), s.isFocused(FocusTranscript))
				@ComposerPane(s.state.Get(), s.isFocused(FocusComposer))
			</div>
			@RightRail(s.state.Get(), s.isFocused(FocusRail), s.isFocused(FocusWorkers))
		</div>
		@Footer()
	</div>
}
