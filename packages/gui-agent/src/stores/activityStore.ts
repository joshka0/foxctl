import { create } from 'zustand'
import type { ActivityEvent } from '@/types/activity'

interface ActivityState {
  events: ActivityEvent[]
  dedupKeys: Set<string>
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

function makeDedupKey(event: ActivityEvent): string {
  return `${event.ts}:${event.operation}:${event.session_id || ''}`
}

export const useActivityStore = create<ActivityState>((set) => ({
  events: [],
  dedupKeys: new Set<string>(),
  connected: false,
  error: null,
  initialLoaded: false,
  addEvent: (event) =>
    set((state) => {
      const key = makeDedupKey(event)
      if (state.dedupKeys.has(key)) return state
      const newEvents = [event, ...state.events].slice(0, MAX_EVENTS)
      const newKeys = new Set(state.dedupKeys)
      newKeys.add(key)
      // Trim keys set to match events
      if (newKeys.size > MAX_EVENTS) {
        const keysToKeep = new Set(newEvents.map(makeDedupKey))
        return { events: newEvents, dedupKeys: keysToKeep }
      }
      return { events: newEvents, dedupKeys: newKeys }
    }),
  setEvents: (events) =>
    set({
      events: events.slice(0, MAX_EVENTS),
      dedupKeys: new Set(events.slice(0, MAX_EVENTS).map(makeDedupKey)),
      initialLoaded: true,
    }),
  setConnected: (connected) => set({ connected }),
  setError: (error) => set({ error }),
  clearEvents: () => set({ events: [], dedupKeys: new Set() }),
  setInitialLoaded: (loaded) => set({ initialLoaded: loaded }),
}))
