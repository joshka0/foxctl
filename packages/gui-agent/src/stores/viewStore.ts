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

const validViews: ViewType[] = [
  'runtime',
  'rooms',
  'orchestration',
  'turns',
  'context',
  'artifacts',
  'events',
  'companion',
]

function normalizeView(raw: string): ViewType | null {
  if (validViews.includes(raw as ViewType)) return raw as ViewType
  return null
}

// Get initial view from URL hash
function getInitialView(): ViewType {
  const hash = window.location.hash.slice(1) // Remove '#'
  const normalized = normalizeView(hash)
  if (normalized) {
    return normalized
  }
  return 'runtime'
}

// Update URL hash when view changes
function updateUrlHash(view: ViewType) {
  const nextHash = `#${view}`
  if (window.location.hash !== nextHash) {
    window.location.hash = view
  }
}

export interface ViewState {
  activeView: ViewType
  setActiveView: (view: ViewType) => void
  // Selected agent for right panel HUD
  selectedAgent: Agent | null
  setSelectedAgent: (agent: Agent | null) => void
  // Selected room for room/runtime cross-linking
  selectedRoomID: string | null
  selectedRoomWorkspaceID: string | null
  setSelectedRoom: (roomID: string | null, workspaceID?: string | null) => void
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

export const useViewStore = create<ViewState>((set) => ({
  activeView: getInitialView(),
  setActiveView: (activeView) => {
    updateUrlHash(activeView)
    set({ activeView })
  },
  selectedAgent: null,
  setSelectedAgent: (selectedAgent) => set({ selectedAgent }),
  selectedRoomID: null,
  selectedRoomWorkspaceID: null,
  setSelectedRoom: (selectedRoomID, selectedRoomWorkspaceID = null) =>
    set({ selectedRoomID, selectedRoomWorkspaceID }),
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
    const hash = window.location.hash.slice(1)
    const normalized = normalizeView(hash)
    if (!normalized) {
      useViewStore.setState({ activeView: 'runtime' })
      window.history.replaceState(null, '', '#runtime')
      return
    }
    useViewStore.setState({ activeView: normalized })
  }
  
  window.addEventListener('hashchange', hashChangeHandler)
}
