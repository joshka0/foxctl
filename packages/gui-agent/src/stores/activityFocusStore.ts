import { create } from 'zustand'

export interface ActivityFocus {
  traceIDs: string[]
  sessionID?: string
  sourceSurface?:
    | 'runtime'
    | 'orchestration'
    | 'turns'
    | 'context'
    | 'artifacts'
    | 'companion'
    | 'events'
  label?: string
  at: number
}

interface ActivityFocusState {
  focus: ActivityFocus | null
  setFocus: (focus: Omit<ActivityFocus, 'at'>) => void
  clearFocus: () => void
}

export const useActivityFocusStore = create<ActivityFocusState>((set) => ({
  focus: null,
  setFocus: (focus) =>
    set({
      focus: {
        ...focus,
        at: Date.now(),
      },
    }),
  clearFocus: () => set({ focus: null }),
}))
