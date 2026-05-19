import { create } from 'zustand'
import type { Agent } from '@/api/types'

export type ViewType =
  | 'runtime'
  | 'rooms'
  | 'orchestration'
  | 'turns'
  | 'context'
  | 'artifacts'
  | 'events'
  | 'companion'
  | 'canvas'

const validViews: ViewType[] = [
  'runtime',
  'rooms',
  'orchestration',
  'turns',
  'context',
  'artifacts',
  'events',
  'companion',
  'canvas',
]

function normalizeView(raw: string): ViewType | null {
  if (validViews.includes(raw as ViewType)) return raw as ViewType
  return null
}

interface RouteState {
  activeView: ViewType
  selectedAgentID: string | null
  selectedRoomID: string | null
  selectedRoomWorkspaceID: string | null
  selectedConversationID: string | null
}

function defaultRouteState(): RouteState {
  return {
    activeView: 'runtime',
    selectedAgentID: null,
    selectedRoomID: null,
    selectedRoomWorkspaceID: null,
    selectedConversationID: null,
  }
}

function parseRouteState(): RouteState {
  if (typeof window === 'undefined') {
    return defaultRouteState()
  }
  const raw = window.location.hash.slice(1)
  const [rawView, rawQuery = ''] = raw.split('?')
  const normalizedView = normalizeView(rawView) ?? 'runtime'
  const query = new URLSearchParams(rawQuery)
  return {
    activeView: normalizedView,
    selectedAgentID: query.get('agent'),
    selectedRoomID: query.get('room'),
    selectedRoomWorkspaceID: query.get('workspace'),
    selectedConversationID: query.get('conversation'),
  }
}

function buildHash(state: RouteState): string {
  const query = new URLSearchParams()
  if (state.selectedAgentID) query.set('agent', state.selectedAgentID)
  if (state.selectedRoomID) query.set('room', state.selectedRoomID)
  if (state.selectedRoomWorkspaceID) {
    query.set('workspace', state.selectedRoomWorkspaceID)
  }
  if (state.selectedConversationID) {
    query.set('conversation', state.selectedConversationID)
  }
  const suffix = query.size > 0 ? `?${query.toString()}` : ''
  return `#${state.activeView}${suffix}`
}

function pushRouteHash(state: RouteState) {
  if (typeof window === 'undefined') return
  const nextHash = buildHash(state)
  if (window.location.hash !== nextHash) {
    window.location.hash = nextHash
  }
}

function replaceRouteHash(state: RouteState) {
  if (typeof window === 'undefined') return
  const nextHash = buildHash(state)
  if (window.location.hash !== nextHash) {
    window.history.replaceState(null, '', nextHash)
  }
}

export interface ViewState {
  activeView: ViewType
  setActiveView: (view: ViewType) => void
  // Selected agent for right panel HUD
  selectedAgentID: string | null
  selectedAgent: Agent | null
  setSelectedAgent: (agent: Agent | null) => void
  // Selected room for room/runtime cross-linking
  selectedRoomID: string | null
  selectedRoomWorkspaceID: string | null
  setSelectedRoom: (roomID: string | null, workspaceID?: string | null) => void
  // Selected companion conversation for cross-surface handoff
  selectedConversationID: string | null
  setSelectedConversationID: (conversationID: string | null) => void
  // Explicit ContextWiki workspace override for Context surface; null means follow current agent/current workspace
  selectedContextWorkspace: string | null
  setSelectedContextWorkspace: (workspacePath: string | null) => void
  // Spawn-room defaults for "spawn into room" flows
  spawnRoomID: string | null
  spawnRoomWorkspaceID: string | null
  spawnRoomRole: string | null
  setSpawnRoomDraft: (roomID: string | null, workspaceID?: string | null, roomRole?: string | null) => void
  clearSpawnRoomDraft: () => void
  // Spawn agent panel state (shared across views)
  spawnAgentOpen: boolean
  setSpawnAgentOpen: (open: boolean) => void
}

function pickRouteState(state: Pick<
  ViewState,
  | 'activeView'
  | 'selectedAgentID'
  | 'selectedRoomID'
  | 'selectedRoomWorkspaceID'
  | 'selectedConversationID'
>): RouteState {
  return {
    activeView: state.activeView,
    selectedAgentID: state.selectedAgentID,
    selectedRoomID: state.selectedRoomID,
    selectedRoomWorkspaceID: state.selectedRoomWorkspaceID,
    selectedConversationID: state.selectedConversationID,
  }
}

const initialRoute = parseRouteState()

export const useViewStore = create<ViewState>((set, get) => ({
  activeView: initialRoute.activeView,
  setActiveView: (activeView) => {
    pushRouteHash({
      ...pickRouteState(get()),
      activeView,
    })
    set({ activeView })
  },
  selectedAgentID: initialRoute.selectedAgentID,
  selectedAgent: null,
  setSelectedAgent: (selectedAgent) => {
    replaceRouteHash({
      ...pickRouteState(get()),
      selectedAgentID: selectedAgent?.id ?? null,
    })
    set({
      selectedAgentID: selectedAgent?.id ?? null,
      selectedAgent,
    })
  },
  selectedRoomID: initialRoute.selectedRoomID,
  selectedRoomWorkspaceID: initialRoute.selectedRoomWorkspaceID,
  setSelectedRoom: (selectedRoomID, selectedRoomWorkspaceID = null) => {
    replaceRouteHash({
      ...pickRouteState(get()),
      selectedRoomID,
      selectedRoomWorkspaceID,
    })
    set({ selectedRoomID, selectedRoomWorkspaceID })
  },
  selectedConversationID: initialRoute.selectedConversationID,
  setSelectedConversationID: (selectedConversationID) => {
    replaceRouteHash({
      ...pickRouteState(get()),
      selectedConversationID,
    })
    set({ selectedConversationID })
  },
  selectedContextWorkspace: null,
  setSelectedContextWorkspace: (selectedContextWorkspace) =>
    set({ selectedContextWorkspace: selectedContextWorkspace?.trim() || null }),
  spawnRoomID: null,
  spawnRoomWorkspaceID: null,
  spawnRoomRole: null,
  setSpawnRoomDraft: (spawnRoomID, spawnRoomWorkspaceID = null, spawnRoomRole = null) =>
    set({ spawnRoomID, spawnRoomWorkspaceID, spawnRoomRole }),
  clearSpawnRoomDraft: () =>
    set({ spawnRoomID: null, spawnRoomWorkspaceID: null, spawnRoomRole: null }),
  spawnAgentOpen: false,
  setSpawnAgentOpen: (spawnAgentOpen) => set({ spawnAgentOpen }),
}))

// Listen for hash changes (back/forward navigation)
// Use named handler for HMR safety - prevents duplicate listeners
let hashChangeHandler: (() => void) | null = null

if (typeof window !== 'undefined') {
  // Remove existing handler on HMR
  if (hashChangeHandler) {
    window.removeEventListener('hashchange', hashChangeHandler)
  }
  
  hashChangeHandler = () => {
    const route = parseRouteState()
    const current = useViewStore.getState()
    useViewStore.setState({
      activeView: route.activeView,
      selectedAgentID: route.selectedAgentID,
      selectedAgent:
        current.selectedAgent?.id === route.selectedAgentID
          ? current.selectedAgent
          : null,
      selectedRoomID: route.selectedRoomID,
      selectedRoomWorkspaceID: route.selectedRoomWorkspaceID,
      selectedConversationID: route.selectedConversationID,
    })

    if (!window.location.hash.startsWith(`#${route.activeView}`)) {
      window.history.replaceState(null, '', buildHash(route))
    }
  }
  
  window.addEventListener('hashchange', hashChangeHandler)
}
