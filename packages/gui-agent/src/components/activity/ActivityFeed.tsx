import { useActivityStore } from '@/stores/activityStore'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Badge } from '@/components/ui/badge'
import { cn } from '@/lib/utils'
import { formatRelativeTime } from '@/lib/utils'
import {
  Bot,
  Zap,
  Webhook,
  AlertCircle,
} from 'lucide-react'
import type { ActivityEvent } from '@/api/types'

/**
 * Renders the Activity feed UI showing connection status, event count, and a scrollable list of activity events.
 *
 * The header displays "Activity" with a live/disconnected indicator and a secondary badge with the total number of events.
 * The body shows an empty-state message when there are no events, or a list of ActivityEventCard entries when events exist.
 *
 * @returns The rendered activity feed container (header and scrollable event list)
 */
export function ActivityFeed() {
  const events = useActivityStore((s) => s.events)
  const connected = useActivityStore((s) => s.connected)

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="p-4 border-b border-border flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h2 className="text-lg font-semibold text-foreground">Activity</h2>
          <div className="flex items-center gap-1">
            <span
              className={cn(
                'h-2 w-2 rounded-full',
                connected ? 'bg-green-500' : 'bg-red-500'
              )}
            />
            <span className="text-xs text-muted-foreground">
              {connected ? 'Live' : 'Disconnected'}
            </span>
          </div>
        </div>
        <Badge variant="secondary">{events.length} events</Badge>
      </div>

      {/* Feed */}
      <ScrollArea className="flex-1">
        <div className="p-4 space-y-2">
          {events.length === 0 ? (
            <div className="text-center py-12 text-muted-foreground">
              <AlertCircle className="h-8 w-8 mx-auto mb-2 opacity-50" />
              <p>No activity yet</p>
              <p className="text-sm">Events will appear here as agents work</p>
            </div>
          ) : (
            events.map((event, index) => (
              <ActivityEventCard key={`${event.ts}-${index}`} event={event} />
            ))
          )}
        </div>
      </ScrollArea>
    </div>
  )
}

/**
 * Renders a single activity event card showing an icon, operation label, status badge, timestamp, optional session and duration, and a compact preview of event data.
 *
 * @param event - The activity event to render. Expected fields include `operation`, `status`, `ts` (timestamp), optional `session_id`, optional `duration_ms`, and optional `data` (key/value pairs).
 * @returns A JSX element representing the activity event card.
 */
function ActivityEventCard({ event }: { event: ActivityEvent }) {
  const icon = getEventIcon(event.operation)
  const statusVariant = getStatusVariant(event.status)

  return (
    <div className="flex gap-3 p-3 rounded-lg bg-card border border-border hover:bg-accent/30 transition-colors">
      <div className="flex-shrink-0 mt-0.5">{icon}</div>
      <div className="flex-1 min-w-0">
        <div className="flex items-start justify-between gap-2">
          <div>
            <span className="font-medium text-foreground text-sm">
              {formatOperation(event.operation)}
            </span>
            <Badge
              variant={statusVariant}
              className="ml-2 text-xs"
            >
              {event.status}
            </Badge>
          </div>
          <span className="text-xs text-muted-foreground whitespace-nowrap">
            {formatRelativeTime(event.ts)}
          </span>
        </div>
        <div className="flex items-center gap-2 mt-1">
          {event.session_id && (
            <span className="text-xs text-muted-foreground font-mono">
              {event.session_id.slice(0, 8)}
            </span>
          )}
          {event.duration_ms !== undefined && event.duration_ms > 0 && (
            <span className="text-xs text-muted-foreground">
              {event.duration_ms}ms
            </span>
          )}
        </div>
        {event.data && Object.keys(event.data).length > 0 && (
          <div className="mt-2 text-xs text-muted-foreground">
            {renderEventData(event.data)}
          </div>
        )}
      </div>
    </div>
  )
}

/**
 * Selects a compact JSX icon that represents an event type based on the operation's leading segment.
 *
 * @param operation - Dot-separated operation name whose first segment determines the icon (for example, `"agent.execute"`).
 * @returns A JSX icon element: `Bot` for `"agent"`, `Webhook` for `"hook"`, `Zap` for `"skill"`, and `AlertCircle` for any other type.
 */
function getEventIcon(operation: string) {
  const type = operation.split('.')[0]

  switch (type) {
    case 'agent':
      return <Bot className="h-4 w-4 text-blue-500" />
    case 'hook':
      return <Webhook className="h-4 w-4 text-purple-500" />
    case 'skill':
      return <Zap className="h-4 w-4 text-yellow-500" />
    default:
      return <AlertCircle className="h-4 w-4 text-gray-500" />
  }
}

/**
 * Map an activity event status string to a UI badge variant.
 *
 * @param status - The status value from an activity event (e.g., "ok", "error")
 * @returns `success` for "ok", `destructive` for "error", `secondary` for any other value
 */
function getStatusVariant(status: string): 'default' | 'destructive' | 'success' | 'secondary' {
  switch (status) {
    case 'ok':
      return 'success'
    case 'error':
      return 'destructive'
    default:
      return 'secondary'
  }
}

/**
 * Format a dot-delimited operation identifier into a human-friendly label.
 *
 * @param operation - The operation identifier using dots as separators (e.g., `agent.start.session`)
 * @returns The label with each segment capitalized and joined by ` → ` (e.g., `Agent → Start → Session`)
 */
function formatOperation(operation: string): string {
  return operation
    .split('.')
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' → ')
}

/**
 * Renders a compact, inline preview of an event's data fields.
 *
 * Displays up to three key-value pairs as inline labeled spans and, if more fields exist,
 * appends a trailing indicator like `+N more` showing how many additional fields are omitted.
 *
 * @param data - The event data object whose top-level keys and values will be shown
 * @returns A React node containing the rendered key/value snippets and an optional overflow indicator
 */
function renderEventData(data: Record<string, unknown>): React.ReactNode {
  const entries = Object.entries(data).slice(0, 3) // Limit displayed fields

  return (
    <div className="flex flex-wrap gap-x-3 gap-y-1">
      {entries.map(([key, value]) => (
        <span key={key}>
          <span className="text-muted-foreground">{key}:</span>{' '}
          <span className="text-foreground">{formatValue(value)}</span>
        </span>
      ))}
      {Object.keys(data).length > 3 && (
        <span className="text-muted-foreground">
          +{Object.keys(data).length - 3} more
        </span>
      )}
    </div>
  )
}

/**
 * Formats a primitive or array value for concise display in the activity feed.
 *
 * @param value - The value to format; commonly a string, number, boolean, or array
 * @returns A string representation:
 * - for strings: the first 50 characters of the string
 * - for numbers: the number converted to a string
 * - for booleans: `"yes"` for `true`, `"no"` for `false`
 * - for arrays: `"[N items]"` where `N` is the array length
 * - for all other types: `"..."`
 */
function formatValue(value: unknown): string {
  if (typeof value === 'string') return value.slice(0, 50)
  if (typeof value === 'number') return String(value)
  if (typeof value === 'boolean') return value ? 'yes' : 'no'
  if (Array.isArray(value)) return `[${value.length} items]`
  return '...'
}