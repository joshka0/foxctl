import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { RoomStatusParticipant } from '@foxctl/data/types'
import { participantTransportKind } from '@/lib/room-utils'
import { cn } from '@/lib/utils'
import { Monitor, Cpu, Link2Off, Zap, Activity, Power, ShieldCheck, HelpCircle } from 'lucide-react'

interface ParticipantListProps {
  participants: RoomStatusParticipant[]
  coordinatorId?: string
}

export function ParticipantList({ participants, coordinatorId }: ParticipantListProps) {
  return (
    <div className="flex flex-col h-full bg-muted/5">
      <div className="px-4 py-2 border-b bg-muted/10 flex items-center justify-between">
        <h2 className="text-[10px] font-bold uppercase tracking-wider text-muted-foreground">Participants</h2>
        <Tooltip>
          <TooltipTrigger>
            <HelpCircle className="w-3 h-3 text-muted-foreground/50" />
          </TooltipTrigger>
          <TooltipContent side="bottom" className="max-w-xs text-[10px] space-y-2">
            <p><span className="font-bold text-primary">Transport-First:</span> Delivery depends on the canonical endpoint (pane_socket), not the viewer (tmux/zellij).</p>
            <p><span className="font-bold text-green-500">Green Dot:</span> Ready. <span className="font-bold text-red-500">Red Dot:</span> Endpoint missing. <span className="font-bold text-slate-400">Gray:</span> Unknown/Membership-only.</p>
          </TooltipContent>
        </Tooltip>
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

function ParticipantRow({ member, isCoordinator }: { member: RoomStatusParticipant, isCoordinator?: boolean }) {
  const t = member.transport
  const transportReady = t?.transport === 'available'
  const transportUnavailable = t?.transport === 'unavailable'
  const isUnbound = t?.membership === 'unbound'
  const isExplicitMember = t?.membership === 'active' || isUnbound

  const isStale = member.status === 'stale'
  // Status dot: green = available transport, red = unavailable, orange = stale
  const statusColor = transportReady ? "bg-green-500" :
                     transportUnavailable ? "bg-red-500" :
                     (isStale ? "bg-orange-500 animate-pulse" : "bg-slate-400")

  // Runtime icon: live/stopped/unknown
  const runtimeIcon = t?.runtime === 'live'
    ? <Activity className="w-2.5 h-2.5 text-green-500" />
    : t?.runtime === 'stopped'
    ? <Power className="w-2.5 h-2.5 text-red-500" />
    : <Activity className="w-2.5 h-2.5 text-muted-foreground/30" />

  // Transport kind derived from endpoint string
  const transportKind = participantTransportKind(t)
  const transportIcon = transportKind === 'pane_socket'
    ? <Zap className="w-3 h-3 text-primary" />
    : <Cpu className="w-3 h-3 text-muted-foreground/50" />

  // Presentation: mux_backend indicates which viewer is attached
  const presentationIcon = t?.mux_backend === 'zellij'
    ? <Cpu className="w-3 h-3" />
    : t?.mux_backend === 'tmux'
    ? <Monitor className="w-3 h-3" />
    : null

  return (
    <div className={cn(
      "px-3 py-2 rounded-md flex flex-col gap-1.5 group transition-colors",
      isCoordinator ? "bg-primary/5 border border-primary/10" : "hover:bg-muted/50"
    )}>
      <div className="flex items-center justify-between min-w-0">
        <div className="flex items-center gap-2.5 min-w-0">
          <div className="relative shrink-0">
            <div className={cn(
              "w-2 h-2 rounded-full border border-background",
              statusColor
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
              {isExplicitMember && (
                <ShieldCheck className="w-2.5 h-2.5 text-primary/40" />
              )}
            </div>
          </div>
        </div>

        <div className="flex items-center gap-1.5 shrink-0">
          {runtimeIcon}
          {isUnbound && (
            <Tooltip>
              <TooltipTrigger>
                <Link2Off className="w-3.5 h-3.5 text-red-500 animate-bounce" />
              </TooltipTrigger>
              <TooltipContent side="left" className="text-[10px]">
                Transport Unbound: Messages may not reach this actor
              </TooltipContent>
            </Tooltip>
          )}
          {presentationIcon && (
            <Tooltip>
              <TooltipTrigger>
                <div className="text-muted-foreground/60 hover:text-primary transition-colors">
                  {presentationIcon}
                </div>
              </TooltipTrigger>
              <TooltipContent side="left" className="text-[10px]">
                Viewer: {t?.mux_backend} {t?.presentation !== 'none' ? '(attached)' : '(detached)'}
              </TooltipContent>
            </Tooltip>
          )}
        </div>
      </div>

      <div className="flex items-center justify-between px-4">
        <div className="flex items-center gap-2 text-[8px] text-muted-foreground/60 uppercase font-bold tracking-tighter">
          <span className="truncate max-w-[60px]">{member.role || 'member'}</span>
          <span className="opacity-30">•</span>
          <div className="flex items-center gap-1">
            {transportIcon}
            <span className="font-mono">{transportKind}</span>
          </div>
        </div>

        {t?.transport_endpoint && (
          <Tooltip>
            <TooltipTrigger>
              <div className="text-[7px] font-mono text-muted-foreground/40 bg-muted/30 px-1 rounded truncate max-w-[80px]">
                {t.transport_endpoint.split('/').pop()}
              </div>
            </TooltipTrigger>
            <TooltipContent side="left" className="text-[9px] font-mono">
              Endpoint: {t.transport_endpoint}
            </TooltipContent>
          </Tooltip>
        )}
      </div>
    </div>
  )
}
