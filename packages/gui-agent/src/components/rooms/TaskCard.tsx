import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { RoomTask } from '@/api/types'
import { cn } from '@/lib/utils'
import { formatRelativeTime } from '@/lib/time'
import { Clock, Ban, User, MoreHorizontal, HandMetal, RotateCcw, UserPlus } from 'lucide-react'

interface TaskCardProps {
  task: RoomTask
  laneLabel?: string
  onClaim?: () => void
  onTouch?: () => void
  onBlock?: () => void
  onComplete?: () => void
  onReassign?: () => void
  onReclaim?: () => void
  onAbandon?: () => void
  isCoordinator?: boolean
}

export function TaskCard({ 
  task, 
  laneLabel,
  onClaim, 
  onTouch, 
  onBlock, 
  onComplete,
  onReassign,
  onReclaim,
  onAbandon,
  isCoordinator 
}: TaskCardProps) {
  const statusColors = {
    pending: 'border-l-blue-500',
    in_progress: 'border-l-slate-500',
    blocked: 'border-l-red-500',
    completed: 'border-l-green-500',
    abandoned: 'border-l-muted-foreground'
  }

  const priorityColors = {
    1: 'bg-red-500/10 text-red-600 border-red-500/20',
    2: 'bg-orange-500/10 text-orange-600 border-orange-500/20',
    3: 'bg-blue-500/10 text-blue-600 border-blue-500/20',
    4: 'bg-slate-500/10 text-slate-600 border-slate-500/20',
    5: 'bg-muted text-muted-foreground border-transparent'
  }

  return (
    <Card className={cn(
      "p-3 text-xs shadow-sm hover:shadow-md transition-all cursor-default group border-l-4",
      statusColors[task.status] || 'border-l-transparent',
      task.stale && "ring-2 ring-orange-500/30 animate-pulse bg-orange-500/[0.02]"
    )}>
      <div className="flex justify-between items-start mb-2">
        <div className="flex flex-col min-w-0 mr-2 text-left">
          <span className="font-bold truncate text-[13px] leading-tight" title={task.title}>{task.title}</span>
          <span className="text-[10px] text-muted-foreground mt-0.5">{formatRelativeTime(task.created_at)}</span>
        </div>
        <Badge variant="outline" className={cn("text-[9px] px-1 py-0 h-4 uppercase shrink-0 font-bold", priorityColors[task.priority as keyof typeof priorityColors])}>
          P{task.priority}
        </Badge>
      </div>

      <div className="space-y-1.5 mb-3">
        {laneLabel && (
          <div className="flex items-center gap-1.5">
            <Badge variant="outline" className="h-4 px-1.5 text-[9px] font-black uppercase tracking-tight border-primary/20 bg-primary/5 text-primary">
              {laneLabel}
            </Badge>
          </div>
        )}
        <div className="flex items-center gap-1.5 text-muted-foreground text-[10px]">
          <User className="w-3 h-3 text-foreground/50" />
          <span className={cn("truncate font-medium", !task.owner_actor_id && "italic")}>
            {task.owner_actor_id || task.assigned_actor_id || 'Unassigned'}
          </span>
        </div>
        
        {task.status === 'blocked' && (
          <div className="flex items-start gap-1.5 text-red-600 bg-red-500/5 p-1.5 rounded border border-red-500/10">
            <Ban className="w-3 h-3 mt-0.5 shrink-0" />
            <span className="text-[10px] italic line-clamp-2 leading-relaxed">{task.blocked_reason || 'Blocked'}</span>
          </div>
        )}

        {task.stale && (
          <div className="flex items-center gap-1.5 text-orange-600 bg-orange-500/5 p-1.5 rounded border border-orange-500/10">
            <Clock className="w-3 h-3 shrink-0" />
            <span className="text-[10px] font-medium uppercase tracking-tight">Stale: {formatDuration(task.stale_duration_ms || 0)}</span>
          </div>
        )}

        {/* Audit Metadata */}
        {task.reclaim_audit && (
          <div className="flex items-start gap-1.5 text-muted-foreground bg-muted/30 p-1.5 rounded border border-border/50 text-[9px] leading-tight">
            <RotateCcw className="w-2.5 h-2.5 mt-0.5 shrink-0" />
            <div>
              <span className="font-bold uppercase tracking-tighter mr-1 text-[8px]">Reclaimed</span>
              by <span className="text-foreground/70 font-bold">{task.reclaim_audit.reclaimed_by}</span> {formatRelativeTime(task.reclaim_audit.reclaimed_at)}
              <div className="mt-0.5 italic opacity-80 line-clamp-1">"{task.reclaim_audit.reclaim_reason}"</div>
            </div>
          </div>
        )}

        {task.reassign_audit && (
          <div className="flex items-start gap-1.5 text-muted-foreground bg-muted/30 p-1.5 rounded border border-border/50 text-[9px] leading-tight">
            <UserPlus className="w-2.5 h-2.5 mt-0.5 shrink-0" />
            <div>
              <span className="font-bold uppercase tracking-tighter mr-1 text-[8px]">Reassigned</span>
              by <span className="text-foreground/70 font-bold">{task.reassign_audit.reassigned_by}</span> {formatRelativeTime(task.reassign_audit.reassigned_at)}
            </div>
          </div>
        )}
      </div>

      <div className="flex items-center justify-between mt-auto pt-2 border-t border-muted/50">
        <div className="flex items-center gap-2">
          {task.nudge_count > 0 && (
            <Tooltip>
              <TooltipTrigger>
                <div className="flex items-center gap-1 text-[9px] text-muted-foreground bg-muted px-1 rounded hover:text-foreground transition-colors">
                  <HandMetal className="w-2.5 h-2.5" /> {task.nudge_count}
                </div>
              </TooltipTrigger>
              <TooltipContent side="bottom" className="text-[10px]">
                Nudged {task.nudge_count} times. Last: {task.last_nudged_at ? formatRelativeTime(task.last_nudged_at) : 'never'}
              </TooltipContent>
            </Tooltip>
          )}
        </div>

        <div className="flex items-center gap-1">
          {task.status === 'pending' && (
            <>
              {isCoordinator && <Button variant="outline" size="xs" onClick={onReassign} className="h-6">Assign</Button>}
              <Button size="xs" onClick={onClaim} className="h-6 font-bold">Claim</Button>
            </>
          )}
          {task.status === 'in_progress' && (
            <>
              <Button variant="ghost" size="xs" onClick={onTouch} className="h-6 hover:bg-slate-100">Touch</Button>
              <Button variant="outline" size="xs" onClick={onBlock} className="h-6 text-red-600 hover:text-red-700 hover:bg-red-50">Block</Button>
              <Button size="xs" onClick={onComplete} className="h-6 bg-green-600 hover:bg-green-700">Done</Button>
            </>
          )}
          {task.status === 'blocked' && (
            <>
              <Button variant="ghost" size="xs" onClick={onTouch} className="h-6">Unblock</Button>
              <Button variant="outline" size="xs" onClick={onAbandon} className="h-6">Abandon</Button>
            </>
          )}
          {isCoordinator && (task.stale || task.status === 'in_progress' || task.status === 'blocked') && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button variant="ghost" size="xs" onClick={onReclaim} className="h-6 w-6 p-0 text-red-600 hover:text-red-700 hover:bg-red-50">
                  <RotateCcw className="w-3 h-3" />
                </Button>
              </TooltipTrigger>
              <TooltipContent side="top" className="text-[10px]">Force Reclaim</TooltipContent>
            </Tooltip>
          )}
          {isCoordinator && (
            <Button variant="ghost" size="xs" onClick={onReassign} className="h-6 w-6 p-0">
              <MoreHorizontal className="w-3 h-3" />
            </Button>
          )}
        </div>
      </div>
    </Card>
  )
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m`
  const hours = Math.floor(minutes / 60)
  return `${hours}h`
}
