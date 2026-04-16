package tui

import gotui "github.com/grindlemire/go-tui"

func focusPaneForIndex(index int) FocusPane {
	switch index {
	case 0:
		return FocusTranscript
	case 1:
		return FocusComposer
	case 2:
		return FocusRail
	case 3:
		return FocusWorkers
	default:
		return FocusTranscript
	}
}

func nextRail(tab RailTab, delta int) RailTab {
	tabs := railTabs()
	if len(tabs) == 0 {
		return RailMemory
	}
	index := 0
	for i, candidate := range tabs {
		if candidate == tab {
			index = i
			break
		}
	}
	index = (index + delta + len(tabs)) % len(tabs)
	return tabs[index]
}

func stopBindings() gotui.KeyMap {
	return gotui.KeyMap{
		gotui.On(gotui.KeyEscape, func(ke gotui.KeyEvent) { ke.App().Stop() }),
		gotui.On(gotui.Rune('q'), func(ke gotui.KeyEvent) { ke.App().Stop() }),
		gotui.On(gotui.Rune('c').Ctrl(), func(ke gotui.KeyEvent) { ke.App().Stop() }),
	}
}
