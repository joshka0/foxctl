import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import type { MailboxMessage } from '@/api/types'
import { cn } from '@/lib/utils'
import { formatRelativeTime } from '@/lib/time'
import { Check, CornerDownRight, HandMetal, MessageSquare, AlertTriangle, CheckCircle2 } from 'lucide-react'

interface InboxRowProps {
  message: MailboxMessage
  onAck?: () => void
  onResolve?: () => void
  onReply?: () => void
  onNudge?: () => void
  isCoordinator?: boolean
}

export function InboxRow({ 
  message, 
  onAck, 
  onResolve, 
  onReply, 
  onNudge,
  isCoordinator 
}: InboxRowProps) {
  const flags = []
  if (message.ack_required) flags.push('ACK-REQUIRED')
  if (message.reply_expected) flags.push('REPLY-EXPECTED')

  const isAlert = message.kind === 'alert' || message.kind === 'error'

  return (
    <div className="px-4 py-3 hover:bg-muted/30 cursor-pointer transition-colors group border-b border-muted/50 last:border-0">
      <div className="flex items-center justify-between mb-1.5 text-left">
        <div className="flex items-center gap-2 overflow-hidden">
          {isAlert ? (
            <AlertTriangle className="w-3.5 h-3.5 text-red-500 shrink-0" />
          ) : (
            <MessageSquare className="w-3.5 h-3.5 text-muted-foreground shrink-0" />
          )}
          <span className="text-[11px] font-bold shrink-0">{message.sender}</span>
          <span className="text-[11px] font-medium text-foreground/90 truncate">{message.subject}</span>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <span className="text-[10px] text-muted-foreground font-mono">{formatRelativeTime(message.created_at)}</span>
        </div>
      </div>

      <div className="flex items-start justify-between gap-4">
        <div className="flex-1 min-w-0 text-left">
          <p className="text-[11px] text-muted-foreground line-clamp-2 leading-relaxed italic">
            {message.body}
          </p>
          <div className="mt-2 flex flex-wrap gap-1.5">
            {flags.map(f => (
              <Badge 
                key={f} 
                variant="outline" 
                className={cn(
                  "text-[9px] px-1.5 py-0 font-bold",
                  f === 'ACK-REQUIRED' ? "border-amber-500/50 text-amber-600 bg-amber-500/5" :
                  "border-purple-500/50 text-purple-600 bg-purple-500/5"
                )}
              >
                {f}
              </Badge>
            ))}
            {message.kind === 'task_update' && (
              <Badge variant="outline" className="text-[9px] px-1.5 py-0 border-blue-500/50 text-blue-600 bg-blue-500/5">
                TASK-UPDATE
              </Badge>
            )}
          </div>
        </div>

        <div className="flex items-center gap-1 opacity-0 group-hover:opacity-100 transition-opacity shrink-0">
          {message.ack_required && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button size="xs" onClick={onAck} className="h-7 px-2 text-[10px] bg-amber-600 hover:bg-amber-700">
                  <Check className="w-3 h-3 mr-1" /> Ack
                </Button>
              </TooltipTrigger>
              <TooltipContent side="top">Mark as acknowledged</TooltipContent>
            </Tooltip>
          )}
          {message.reply_expected && (
            <Button size="xs" variant="secondary" onClick={onReply} className="h-7 px-2 text-[10px]">
              <CornerDownRight className="w-3 h-3 mr-1" /> Reply
            </Button>
          )}
          {isCoordinator && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button size="xs" variant="outline" onClick={onResolve} className="h-7 px-2 text-[10px] hover:text-green-600 hover:bg-green-50">
                  <CheckCircle2 className="w-3.5 h-3.5 mr-1" /> Resolve
                </Button>
              </TooltipTrigger>
              <TooltipContent side="top">Close obligation from inbox</TooltipContent>
            </Tooltip>
          )}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button size="xs" variant="ghost" onClick={onNudge} className="h-7 w-7 p-0 hover:bg-primary/5 hover:text-primary transition-colors">
                <HandMetal className="w-3.5 h-3.5" />
              </Button>
            </TooltipTrigger>
            <TooltipContent side="top">Send reminder ping</TooltipContent>
          </Tooltip>
        </div>
      </div>
    </div>
  )
}
