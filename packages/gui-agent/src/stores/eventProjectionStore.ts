import { create } from 'zustand'

const STORAGE_KEY = 'gui-agent:event-projection:v1'

const DEFAULT_HIDDEN_COMMANDS = [
  'hooks/dispatch',
  'hooks/overseer_inbox',
  'session/restore',
  'code/incremental_index',
  'lsp/gopls',
]

interface PersistedState {
  errorsOnly: boolean
  hiddenCommands: string[]
  componentFilter: string
  workspaceFilter: string
  searchQuery: string
  showRawEvents: boolean
}

interface EventProjectionState extends PersistedState {
  setErrorsOnly: (value: boolean) => void
  toggleHiddenCommand: (command: string) => void
  setComponentFilter: (value: string) => void
  setWorkspaceFilter: (value: string) => void
  setSearchQuery: (value: string) => void
  setShowRawEvents: (value: boolean) => void
  clearHiddenCommands: () => void
  resetToDefaults: () => void
}

const defaultState: PersistedState = {
  errorsOnly: false,
  hiddenCommands: DEFAULT_HIDDEN_COMMANDS,
  componentFilter: '',
  workspaceFilter: '',
  searchQuery: '',
  showRawEvents: false,
}

function loadPersistedState(): Partial<PersistedState> {
  if (typeof window === 'undefined') return {}
  try {
    const raw = window.sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw) as Partial<PersistedState>
    return {
      errorsOnly: Boolean(parsed.errorsOnly),
      hiddenCommands: Array.isArray(parsed.hiddenCommands)
        ? parsed.hiddenCommands.filter((value): value is string => typeof value === 'string')
        : defaultState.hiddenCommands,
      componentFilter: typeof parsed.componentFilter === 'string' ? parsed.componentFilter : '',
      workspaceFilter: typeof parsed.workspaceFilter === 'string' ? parsed.workspaceFilter : '',
      searchQuery: typeof parsed.searchQuery === 'string' ? parsed.searchQuery : '',
      showRawEvents: Boolean(parsed.showRawEvents),
    }
  } catch {
    return {}
  }
}

function persistState(state: PersistedState) {
  if (typeof window === 'undefined') return
  try {
    window.sessionStorage.setItem(STORAGE_KEY, JSON.stringify(state))
  } catch {
    // Ignore storage write failures (private mode/storage restrictions).
  }
}

function pickPersistedState(state: EventProjectionState): PersistedState {
  return {
    errorsOnly: state.errorsOnly,
    hiddenCommands: state.hiddenCommands,
    componentFilter: state.componentFilter,
    workspaceFilter: state.workspaceFilter,
    searchQuery: state.searchQuery,
    showRawEvents: state.showRawEvents,
  }
}

const initial = { ...defaultState, ...loadPersistedState() }

export const useEventProjectionStore = create<EventProjectionState>((set, get) => ({
  ...initial,
  setErrorsOnly: (value) => {
    set({ errorsOnly: value })
    persistState(pickPersistedState(get()))
  },
  toggleHiddenCommand: (command) => {
    set((state) => {
      const exists = state.hiddenCommands.includes(command)
      return {
        hiddenCommands: exists
          ? state.hiddenCommands.filter((value) => value !== command)
          : [...state.hiddenCommands, command],
      }
    })
    persistState(pickPersistedState(get()))
  },
  setComponentFilter: (value) => {
    set({ componentFilter: value })
    persistState(pickPersistedState(get()))
  },
  setWorkspaceFilter: (value) => {
    set({ workspaceFilter: value })
    persistState(pickPersistedState(get()))
  },
  setSearchQuery: (value) => {
    set({ searchQuery: value })
    persistState(pickPersistedState(get()))
  },
  setShowRawEvents: (value) => {
    set({ showRawEvents: value })
    persistState(pickPersistedState(get()))
  },
  clearHiddenCommands: () => {
    set({ hiddenCommands: [] })
    persistState(pickPersistedState(get()))
  },
  resetToDefaults: () => {
    set({ ...defaultState })
    persistState(defaultState)
  },
}))
