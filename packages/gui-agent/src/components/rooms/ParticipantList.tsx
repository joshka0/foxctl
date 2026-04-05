import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { RoomMember } from '@/api/types'
import { cn } from '@/lib/utils'
import { formatRelativeTime } from '@/lib/time'
import { Monitor, Cpu, ShieldAlert, Link2Off, Power, PowerOff } from 'lucide-react'

interface ParticipantListProps {
  participants: RoomMember[]
  coordinatorId?: string
}

export function ParticipantList({ participants, coordinatorId }: ParticipantListProps) {
  return (
    <div className="flex flex-col h-full bg-muted/5">
      <div className="px-4 py-2 border-b bg-muted/10">
        <h2 className="text-[10px] font-bold uppercase tracking-wider text-muted-foreground">Participants</h2>
      </div>
      <ScrollArea className="flex-1">
        <div className="p-2 space-y-1">
          {participants.map((p) => (
            <ParticipantRow key={p.actor_id} member={p} isCoordinator={p.actor_id === coordinatorId} />
          ))}
        </div>
      </ScrollArea>
    </div>
  )
}

function ParticipantRow({ member, isCoordinator }: { member: RoomMember, isCoordinator?: boolean }) {
  const isStale = member.status === 'stale'
  const isOffline = member.status === 'offline'
  
  const transportIcon = member.transport === 'zellij' 
    ? <Cpu className="w-3 h-3" /> 
    : <Monitor className="w-3 h-3" />

  return (
    <div className={cn(
      "px-3 py-2 rounded-md flex items-center justify-between group transition-colors",
      isCoordinator ? "bg-primary/5 border border-primary/10" : "hover:bg-muted/50"
    )}>
      <div className="flex items-center gap-2.5 min-w-0">
        <div className="relative shrink-0">
          <div className={cn(
            "w-2 h-2 rounded-full border border-background",
            isOffline ? "bg-slate-400" : (isStale ? "bg-orange-500 animate-pulse" : "bg-green-500")
          )} />
        </div>
        <div className="flex flex-col min-w-0">
          <div className="flex items-center gap-1.5">
            <span className={cn(
              "text-[11px] font-bold truncate",
              isCoordinator ? "text-primary" : "text-foreground/80"
            )} title={member.actor_id}>
              {member.actor_id}
            </span>
            {isCoordinator && (
              <Badge variant="outline" className="h-3.5 px-1 text-[8px] border-primary/30 text-primary bg-primary/5 uppercase font-black">Lead</Badge>
            )}
          </div>
          <div className="flex items-center gap-2 text-[9px] text-muted-foreground mt-0.5">
            <span className="capitalize">{member.role || 'member'}</span>
            <span className="opacity-30">•</span>
            <div className="flex items-center gap-1">
              {transportIcon}
              <span className="uppercase font-mono">{member.transport || 'unknown'}</span>
            </div>
          </div>
        </div>
      </div>

      <div className="flex items-center gap-1.5 shrink-0">
        {member.unbound && (
          <Tooltip>
            <TooltipTrigger>
              <Link2Off className="w-3.5 h-3.5 text-red-500 animate-bounce" />
            </TooltipTrigger>
            <TooltipContent side="left" className="text-[10px]">
              Transport Unbound: Messages may not reach this actor
            </TooltipContent>
          </Tooltip>
        )}
        {!member.unbound && member.session_id && (
          <Tooltip>
            <TooltipTrigger>
              <div className="text-[9px] font-mono bg-muted px-1 rounded text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity">
                {member.session_id.slice(0, 6)}
              </div>
            </TooltipTrigger>
            <TooltipContent side="left" className="text-[10px]">
              Session: {member.session_id} {member.pane_id ? `(Pane ${member.pane_id})` : ''}
            </TooltipContent>
          </Tooltip>
        )}
      </div>
    </div>
  )
}
