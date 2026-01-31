import { create } from 'zustand'
import type { Agent } from '@/api/types'

export type ViewType = 'activity' | 'search' | 'logs' | 'skills' | 'mailbox' | 'blackboard' | 'settings' | 'agents' | 'conversations'

const validViews: ViewType[] = ['activity', 'search', 'logs', 'skills', 'mailbox', 'blackboard', 'settings', 'agents', 'conversations']

// Get initial view from URL hash
function getInitialView(): ViewType {
  const hash = window.location.hash.slice(1) // Remove '#'
  if (validViews.includes(hash as ViewType)) {
    return hash as ViewType
  }
  return 'conversations'
}

// Update URL hash when view changes
function updateUrlHash(view: ViewType) {
  window.history.replaceState(null, '', `#${view}`)
}

export interface ViewState {
  activeView: ViewType
  setActiveView: (view: ViewType) => void
  // Selected agent for right panel HUD
  selectedAgent: Agent | null
  setSelectedAgent: (agent: Agent | null) => void
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
    if (validViews.includes(hash as ViewType)) {
      useViewStore.setState({ activeView: hash as ViewType })
    }
  }
  
  window.addEventListener('hashchange', hashChangeHandler)
}
