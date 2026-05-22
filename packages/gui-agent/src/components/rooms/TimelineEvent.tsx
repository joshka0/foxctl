import { cn } from '@/lib/utils'
import { formatRelativeTime } from '@/lib/time'
import { MessageSquare, ClipboardList, Crown, AlertTriangle, ShieldCheck } from 'lucide-react'
import type { LeadChangeEvent } from '@foxctl/data/types'

interface TimelineEventProps {
  event: any // Can be MailboxMessage or LeadChangeEvent
  type: 'message' | 'task' | 'lead' | 'alert' | 'system'
}

export function TimelineEvent({ event, type }: TimelineEventProps) {
  const isLeadChange = type === 'lead' || event.kind === 'lead_change'
  
  const iconMap = {
    message: <MessageSquare className="w-3 h-3" />,
    task: <ClipboardList className="w-3 h-3" />,
    lead: <Crown className="w-3 h-3 text-primary" />,
    alert: <AlertTriangle className="w-3 h-3 text-red-500" />,
    system: <ShieldCheck className="w-3 h-3 text-blue-500" />
  }

  const actor = event.sender || event.changed_by || 'system'
  const time = event.created_at || event.changed_at

  if (isLeadChange) {
    const leadEvent = event as LeadChangeEvent
    return (
      <div className="flex gap-3 text-[11px] group relative py-1 px-2 rounded hover:bg-primary/5 transition-colors border border-transparent hover:border-primary/10">
        <div className="shrink-0 mt-0.5">
          <div className="w-5 h-5 rounded-full bg-primary/10 flex items-center justify-center">
            <Crown className="w-3 h-3 text-primary" />
          </div>
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center justify-between gap-2 mb-0.5">
            <span className="font-black uppercase tracking-widest text-[9px] text-primary">Coordinator Handoff</span>
            <span className="text-[9px] text-muted-foreground font-mono">{formatRelativeTime(time)}</span>
          </div>
          <p className="text-foreground/90 leading-normal">
            <span className="font-bold">{leadEvent.previous_lead || 'none'}</span>
            <span className="mx-1.5 opacity-50">→</span>
            <span className="font-bold text-primary px-1 bg-primary/5 rounded">{leadEvent.new_lead}</span>
          </p>
          {leadEvent.note && (
            <p className="mt-1.5 text-[10px] text-muted-foreground italic border-l-2 border-primary/20 pl-2 py-0.5 bg-muted/30 rounded-r">
              "{leadEvent.note}"
            </p>
          )}
        </div>
      </div>
    )
  }

  // Task Update Event
  if (event.kind === 'task_update') {
    return (
      <div className="flex gap-3 text-[11px] group relative py-1 px-2 rounded hover:bg-blue-500/5 transition-colors border border-transparent hover:border-blue-500/10">
        <div className="shrink-0 mt-0.5">
          <div className="w-5 h-5 rounded-full bg-blue-500/10 flex items-center justify-center">
            <ClipboardList className="w-3 h-3 text-blue-500" />
          </div>
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center justify-between gap-2 mb-0.5">
            <span className="font-bold text-blue-600/80 uppercase tracking-tighter text-[9px]">{event.subject}</span>
            <span className="text-[9px] text-muted-foreground font-mono">{formatRelativeTime(time)}</span>
          </div>
          <p className="text-muted-foreground leading-relaxed italic line-clamp-2">
            {event.body}
          </p>
        </div>
      </div>
    )
  }

  // Default Message Event
  return (
    <div className="flex gap-3 text-[11px] group relative py-1 px-2 rounded hover:bg-muted/50 transition-colors border border-transparent hover:border-border/50">
      <div className="shrink-0 mt-0.5 opacity-50 group-hover:opacity-100 transition-opacity">
        {iconMap[type] || iconMap.message}
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center justify-between gap-2 mb-0.5">
          <span className="font-bold text-foreground/80">{actor}</span>
          <span className="text-[9px] text-muted-foreground font-mono">{formatRelativeTime(time)}</span>
        </div>
        <p className={cn(
          "text-muted-foreground leading-relaxed",
          event.kind === 'alert' && "text-red-600 font-medium"
        )}>
          {event.body}
        </p>
      </div>
    </div>
  )
}
