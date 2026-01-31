import { create } from 'zustand'
import type { ActivityEvent } from '../api/types'

interface ActivityState {
  events: ActivityEvent[]
  connected: boolean
  error: string | null
  initialLoaded: boolean
  addEvent: (event: ActivityEvent) => void
  setEvents: (events: ActivityEvent[]) => void
  setConnected: (connected: boolean) => void
  setError: (error: string | null) => void
  clearEvents: () => void
  setInitialLoaded: (loaded: boolean) => void
}

const MAX_EVENTS = 500

export const useActivityStore = create<ActivityState>((set) => ({
  events: [],
  connected: false,
  error: null,
  initialLoaded: false,
  addEvent: (event) =>
    set((state) => {
      // Dedupe by timestamp + operation (SSE might send duplicates of API-loaded events)
      const isDupe = state.events.some(
        (e) => e.ts === event.ts && e.operation === event.operation
      )
      if (isDupe) return state
      return { events: [event, ...state.events].slice(0, MAX_EVENTS) }
    }),
  setEvents: (events) =>
    set({ events: events.slice(0, MAX_EVENTS), initialLoaded: true }),
  setConnected: (connected) => set({ connected }),
  setError: (error) => set({ error }),
  clearEvents: () => set({ events: [] }), // Keep initialLoaded true to prevent auto-refetch
  setInitialLoaded: (loaded) => set({ initialLoaded: loaded }),
}))
