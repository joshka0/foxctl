import { useEffect, useRef, useCallback } from 'react'
import { useActivityStore } from '../stores/activityStore'
import type { ActivityEvent } from '@/types/activity'
import { getLogs } from '../api/client'

const SSE_URL = '/api/events'
const MAX_EVENTS = 500

function dedupKey(event: ActivityEvent): string {
  return `${event.ts}:${event.operation}:${event.session_id || ''}`
}

function parseTS(ts: string): number {
  const parsed = Date.parse(ts)
  return Number.isFinite(parsed) ? parsed : 0
}

function mergeActivityEvents(snapshot: ActivityEvent[], existing: ActivityEvent[]): ActivityEvent[] {
  const merged = [...existing, ...snapshot]
  if (merged.length === 0) return []
  const deduped = new Map<string, ActivityEvent>()
  for (const event of merged) {
    const key = dedupKey(event)
    const prev = deduped.get(key)
    if (!prev || parseTS(event.ts) > parseTS(prev.ts)) {
      deduped.set(key, event)
    }
  }
  return Array.from(deduped.values())
    .sort((a, b) => parseTS(b.ts) - parseTS(a.ts))
    .slice(0, MAX_EVENTS)
}

export function useActivityStream() {
  const eventSourceRef = useRef<EventSource | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const reconnectAttemptsRef = useRef(0)
  const connectRef = useRef<(() => void) | null>(null)
  const maxReconnectAttempts = 10
  const { addEvent, setConnected, setError, setEvents, initialLoaded } = useActivityStore()

  const connect = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = null
    }
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
    }

    const eventSource = new EventSource(SSE_URL)
    eventSourceRef.current = eventSource

    eventSource.onopen = () => {
      setConnected(true)
      setError(null)
      reconnectAttemptsRef.current = 0
    }

    eventSource.onerror = () => {
      setConnected(false)
      eventSource.close()
      eventSourceRef.current = null

      if (reconnectAttemptsRef.current < maxReconnectAttempts) {
        const delay = Math.min(1000 * Math.pow(2, reconnectAttemptsRef.current), 30000)
        reconnectAttemptsRef.current++
        setError(`Connection lost. Reconnecting in ${Math.round(delay / 1000)}s...`)
        reconnectTimerRef.current = setTimeout(() => {
          connectRef.current?.()
        }, delay)
      } else {
        setError('Connection lost. Max retries reached. Click to retry.')
      }
    }

    eventSource.onmessage = (e) => {
      try {
        const message = JSON.parse(e.data)
        if (message.type === 'activity' && message.data) {
          const event: ActivityEvent = message.data
          if (event.operation) {
            addEvent(event)
          }
        } else if (message.type === 'connected') {
          // Connection confirmation
        } else if (message.operation) {
          addEvent(message as ActivityEvent)
        }
      } catch {
        // Ignore non-JSON messages
      }
    }

    return eventSource
  }, [addEvent, setConnected, setError])

  // Avoid self-referential useCallback initializers (lint) while keeping stable reconnect behavior.
  useEffect(() => {
    connectRef.current = connect
  }, [connect])

  const disconnect = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = null
    }
    if (eventSourceRef.current) {
      eventSourceRef.current.close()
      eventSourceRef.current = null
      setConnected(false)
    }
  }, [setConnected])

  useEffect(() => {
    connect()
    return disconnect
  }, [connect, disconnect])

  useEffect(() => {
    if (initialLoaded) return
    let cancelled = false
    getLogs({ limit: MAX_EVENTS })
      .then((response) => {
        if (cancelled) return
        const snapshot = response.entries.map((entry) => ({ ...entry })) as ActivityEvent[]
        const existing = useActivityStore.getState().events
        setEvents(mergeActivityEvents(snapshot, existing))
      })
      .catch(() => {
        // Keep stream-only behavior if snapshot bootstrap fails.
      })
    return () => {
      cancelled = true
    }
  }, [initialLoaded, setEvents])

  return {
    connect,
    disconnect,
    isConnected: useActivityStore((s) => s.connected),
    error: useActivityStore((s) => s.error),
  }
}

// Convenience hooks for filtered events
export function useActivityEvents(filter?: {
  operation?: string
  component?: string
  sessionId?: string
}) {
  return useActivityStore((state) => {
    if (!filter) return state.events
    return state.events.filter((event) => {
      if (filter.operation && !event.operation.startsWith(filter.operation)) return false
      if (filter.sessionId && event.session_id !== filter.sessionId) return false
      if (filter.component && event.component !== filter.component) return false
      return true
    })
  })
}
