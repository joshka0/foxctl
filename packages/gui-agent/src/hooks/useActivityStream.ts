import { useEffect, useRef, useCallback } from 'react'
import { useActivityStore } from '../stores/activityStore'
import type { ActivityEvent } from '../api/types'

const SSE_URL = '/api/events'

export function useActivityStream() {
  const eventSourceRef = useRef<EventSource | null>(null)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const reconnectAttemptsRef = useRef(0)
  const maxReconnectAttempts = 10
  const { addEvent, setConnected, setError } = useActivityStore()

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
        reconnectTimerRef.current = setTimeout(connect, delay)
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
