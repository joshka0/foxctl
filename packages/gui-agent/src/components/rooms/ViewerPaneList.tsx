import type { MuxPane } from '@/api/types'
import { Badge } from '@/components/ui/badge'
import { ScrollArea } from '@/components/ui/scroll-area'
import { cn } from '@/lib/utils'
import { Cpu, Monitor, TerminalSquare, Zap } from 'lucide-react'

interface ViewerPaneListProps {
  panes: MuxPane[]
  loading?: boolean
}

export function ViewerPaneList({ panes, loading }: ViewerPaneListProps) {
  return (
    <div className="flex flex-col h-full border-t bg-background/40">
      <div className="px-4 py-2 border-b bg-muted/10 flex items-center justify-between">
        <h3 className="text-[10px] font-bold uppercase tracking-wider text-muted-foreground">Viewer Panes</h3>
        <Badge variant="outline" className="h-4 px-1.5 text-[9px] font-mono">
          {panes.length}
        </Badge>
      </div>
      <ScrollArea className="flex-1">
        <div className="p-2 space-y-1">
          {loading ? (
            <div className="px-3 py-2 text-[10px] text-muted-foreground animate-pulse">Loading viewer panes…</div>
          ) : panes.length === 0 ? (
            <div className="px-3 py-2 text-[10px] text-muted-foreground">
              No tmux/zellij viewer panes detected for this room.
            </div>
          ) : (
            panes.map((pane) => <ViewerPaneRow key={viewerPaneKey(pane)} pane={pane} />)
          )}
        </div>
      </ScrollArea>
    </div>
  )
}

function ViewerPaneRow({ pane }: { pane: MuxPane }) {
  const backendIcon = pane.backend === 'zellij'
    ? <Cpu className="w-3 h-3 text-muted-foreground/70" />
    : <Monitor className="w-3 h-3 text-muted-foreground/70" />
  const provider = pane.provider || pane.display_command || pane.current_command || 'viewer'
  const active = pane.active ?? false
  const wrapped = pane.wrapped ?? false
  const running = pane.state === 'running'

  return (
    <div className={cn(
      'px-3 py-2 rounded-md border bg-background/70 flex flex-col gap-1.5',
      active ? 'border-primary/30 bg-primary/5' : 'border-border/50',
    )}>
      <div className="flex items-center justify-between gap-2 min-w-0">
        <div className="flex items-center gap-2 min-w-0">
          {backendIcon}
          <span className="text-[11px] font-bold truncate" title={pane.participant_id || pane.label || pane.pane_name}>
            {pane.participant_id || pane.label || pane.pane_name || pane.id || 'unknown-pane'}
          </span>
        </div>
        <div className="flex items-center gap-1.5 shrink-0">
          {wrapped && <Zap className="w-3 h-3 text-primary" />}
          {active && <div className="w-2 h-2 rounded-full bg-green-500" />}
        </div>
      </div>

      <div className="flex items-center justify-between gap-3 text-[8px] uppercase tracking-wide text-muted-foreground">
        <div className="flex items-center gap-1.5 min-w-0">
          <TerminalSquare className="w-2.5 h-2.5" />
          <span className="font-mono truncate">{provider}</span>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <span className="font-mono">{pane.backend}</span>
          {running && <span className="text-green-600 font-bold">live</span>}
        </div>
      </div>

      <div className="flex items-center justify-between gap-3 text-[8px] text-muted-foreground/80">
        <span className="font-mono truncate" title={pane.session}>
          {pane.backend === 'zellij' ? pane.pane_name || pane.session : pane.session_pane || pane.id || pane.session}
        </span>
        {pane.room_id && (
          <span className="truncate" title={pane.room_id}>
            room {pane.room_id}
          </span>
        )}
      </div>
    </div>
  )
}

function viewerPaneKey(pane: MuxPane): string {
  return [
    pane.backend,
    pane.session,
    pane.id || pane.pane_name || pane.participant_id || pane.label || '',
  ].join(':')
}
