import { useMemo } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
import { ScrollArea } from '@/components/ui/scroll-area'
import { formatRelativeTime } from '@/lib/utils'
import { X } from 'lucide-react'

interface TraceDrawerEvent {
  ts: string
  operation: string
  status: string
  command?: string
  component?: string
  trace_id?: string
  span_id?: string
  session_id?: string
}

interface EventTraceDrawerProps {
  traceID: string | null
  events: TraceDrawerEvent[]
  onClose: () => void
}

function parseTS(ts: string): number {
  const parsed = Date.parse(ts)
  return Number.isFinite(parsed) ? parsed : 0
}

export function EventTraceDrawer({
  traceID,
  events,
  onClose,
}: EventTraceDrawerProps) {
  const traceEvents = useMemo(() => {
    if (!traceID) return []
    return events
      .filter((event) => event.trace_id === traceID)
      .sort((a, b) => parseTS(b.ts) - parseTS(a.ts))
      .slice(0, 50)
  }, [events, traceID])

  if (!traceID) return null

  return (
    <Card className="border-border bg-card/60">
      <CardHeader className="py-3">
        <div className="flex items-center justify-between gap-2">
          <div>
            <div className="text-xs uppercase tracking-wide text-muted-foreground">
              Trace
            </div>
            <div className="font-mono text-sm text-foreground">{traceID.slice(0, 16)}</div>
          </div>
          <Button
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-[11px]"
            onClick={onClose}
          >
            <X className="h-3 w-3 mr-1" />
            Close
          </Button>
        </div>
      </CardHeader>
      <CardContent className="pt-0 pb-3">
        {traceEvents.length === 0 ? (
          <div className="text-xs text-muted-foreground">No events for this trace in current view.</div>
        ) : (
          <ScrollArea className="max-h-64">
            <div className="space-y-1.5">
              {traceEvents.map((event) => (
                <div
                  key={`${event.ts}-${event.operation}-${event.span_id || event.session_id || ''}`}
                  className="rounded border border-border/70 bg-muted/20 px-2 py-1.5"
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="text-[11px] font-medium text-foreground truncate">
                      {event.operation}
                    </div>
                    <Badge
                      variant={event.status === 'error' ? 'destructive' : 'secondary'}
                      className="text-[10px]"
                    >
                      {event.status}
                    </Badge>
                  </div>
                  <div className="text-[10px] text-muted-foreground flex items-center justify-between mt-0.5 gap-2">
                    <span className="truncate">{event.command || event.component || 'event'}</span>
                    <span>{formatRelativeTime(event.ts)}</span>
                  </div>
                </div>
              ))}
            </div>
          </ScrollArea>
        )}
      </CardContent>
    </Card>
  )
}
