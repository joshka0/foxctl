import { useState, useEffect } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Modal } from '@/components/ui/modal'
import { patchRoomLoop } from '@/api/client'
import type { RoomLoop } from '@/api/types'
import { Zap, Clock, ShieldAlert } from 'lucide-react'
import { cn } from '@/lib/utils'

interface LoopPolicyEditorProps {
  roomId: string
  workspaceId: string
  actorId: string
  loop: RoomLoop
  isOpen: boolean
  onClose: () => void
}

export function LoopPolicyEditor({ roomId, workspaceId, actorId, loop, isOpen, onClose }: LoopPolicyEditorProps) {
  const queryClient = useQueryClient()
  const [enabled, setEnabled] = useState(loop.enabled)
  const [pulseInterval, setPulseInterval] = useState(loop.pulse_interval)
  const [taskStaleAfter, setTaskStaleAfter] = useState(loop.task_stale_after)
  const [replyStaleAfter, setReplyStaleAfter] = useState(loop.reply_stale_after)
  const [minPulseFloor, setMinPulseFloor] = useState(loop.min_pulse_floor)
  const [interruptAttemptLimit, setInterruptAttemptLimit] = useState(String(loop.interrupt_attempt_limit))
  const [reminderBackoffCap, setReminderBackoffCap] = useState(String(loop.reminder_backoff_cap))
  const [coordinatorPulse, setCoordinatorPulse] = useState(loop.coordinator_pulse_enabled)
  const [coordinatorEscalation, setCoordinatorEscalation] = useState(loop.coordinator_escalation_enabled)

  useEffect(() => {
    setEnabled(loop.enabled)
    setPulseInterval(loop.pulse_interval)
    setTaskStaleAfter(loop.task_stale_after)
    setReplyStaleAfter(loop.reply_stale_after)
    setMinPulseFloor(loop.min_pulse_floor)
    setInterruptAttemptLimit(String(loop.interrupt_attempt_limit))
    setReminderBackoffCap(String(loop.reminder_backoff_cap))
    setCoordinatorPulse(loop.coordinator_pulse_enabled)
    setCoordinatorEscalation(loop.coordinator_escalation_enabled)
  }, [loop])

  const patchMutation = useMutation({
    mutationFn: () => patchRoomLoop(roomId, {
      workspace_id: workspaceId,
      actor_id: actorId,
      enabled,
      pulse_interval: pulseInterval,
      task_stale_after: taskStaleAfter,
      reply_stale_after: replyStaleAfter,
      min_pulse_floor: minPulseFloor,
      interrupt_attempt_limit: Number(interruptAttemptLimit),
      reminder_backoff_cap: Number(reminderBackoffCap),
      coordinator_pulse_enabled: coordinatorPulse,
      coordinator_escalation_enabled: coordinatorEscalation,
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['room', roomId, 'loop'] })
      onClose()
    }
  })

  return (
    <Modal isOpen={isOpen} onClose={onClose} title="Room Loop Policy" className="max-w-lg">
      <div className="space-y-6 py-2">
        <div className="flex items-center justify-between p-3 bg-muted/30 rounded-lg border border-border/50">
          <div className="space-y-0.5">
            <Label className="text-sm font-bold flex items-center gap-2">
              <Zap className={cn("w-4 h-4", enabled ? "text-green-500 fill-current" : "text-muted-foreground")} />
              Loop Enabled
            </Label>
            <p className="text-[11px] text-muted-foreground">Server-managed background coordination ticks</p>
          </div>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div className="space-y-2">
            <Label htmlFor="pulse-interval" className="text-[10px] font-black uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
              <Clock className="w-3 h-3" /> Pulse Interval
            </Label>
            <Input 
              id="pulse-interval"
              value={pulseInterval}
              onChange={(e) => setPulseInterval(e.target.value)}
              placeholder="e.g. 30m, 1h"
              className="h-8 text-xs font-mono"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="task-stale" className="text-[10px] font-black uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
              <ShieldAlert className="w-3 h-3 text-orange-500" /> Task Stale After
            </Label>
            <Input 
              id="task-stale"
              value={taskStaleAfter}
              onChange={(e) => setTaskStaleAfter(e.target.value)}
              placeholder="e.g. 5m, 15m"
              className="h-8 text-xs font-mono"
            />
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div className="space-y-2 text-left">
            <Label htmlFor="reply-stale" className="text-[10px] font-black uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
              <ShieldAlert className="w-3 h-3 text-purple-500" /> Reply Stale After
            </Label>
            <Input 
              id="reply-stale"
              value={replyStaleAfter}
              onChange={(e) => setReplyStaleAfter(e.target.value)}
              placeholder="e.g. 2m, 5m"
              className="h-8 text-xs font-mono"
            />
          </div>
          <div className="space-y-2 text-left">
            <Label htmlFor="min-pulse-floor" className="text-[10px] font-black uppercase tracking-wider text-muted-foreground">
              Minimum Pulse Floor
            </Label>
            <Input 
              id="min-pulse-floor"
              value={minPulseFloor}
              onChange={(e) => setMinPulseFloor(e.target.value)}
              placeholder="e.g. 30s, 5m"
              className="h-8 text-xs font-mono"
            />
          </div>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div className="space-y-2 text-left">
            <Label htmlFor="interrupt-limit" className="text-[10px] font-black uppercase tracking-wider text-muted-foreground">
              Interrupt Attempt Limit
            </Label>
            <Input
              id="interrupt-limit"
              type="number"
              min={1}
              value={interruptAttemptLimit}
              onChange={(e) => setInterruptAttemptLimit(e.target.value)}
              className="h-8 text-xs font-mono"
            />
          </div>
          <div className="space-y-2 text-left">
            <Label htmlFor="backoff-cap" className="text-[10px] font-black uppercase tracking-wider text-muted-foreground">
              Backoff Cap
            </Label>
            <Input
              id="backoff-cap"
              type="number"
              min={1}
              value={reminderBackoffCap}
              onChange={(e) => setReminderBackoffCap(e.target.value)}
              className="h-8 text-xs font-mono"
            />
          </div>
        </div>

        <div className="space-y-3">
          <div className="flex items-center justify-between p-3 border rounded-lg bg-background shadow-sm">
            <div className="space-y-0.5">
              <Label className="text-xs font-bold">Coordinator Pulse</Label>
              <p className="text-[10px] text-muted-foreground">Auto-deliver heartbeat pings to the coordinator</p>
            </div>
            <Switch checked={coordinatorPulse} onCheckedChange={setCoordinatorPulse} />
          </div>
          <div className="flex items-center justify-between p-3 border rounded-lg bg-background shadow-sm">
            <div className="space-y-0.5">
              <Label className="text-xs font-bold">Coordinator Escalation</Label>
              <p className="text-[10px] text-muted-foreground">After the interrupt budget is exhausted, escalate stuck work to the coordinator</p>
            </div>
            <Switch checked={coordinatorEscalation} onCheckedChange={setCoordinatorEscalation} />
          </div>
        </div>

        <div className="bg-muted/50 p-3 rounded-md border border-dashed border-muted text-[10px] text-muted-foreground leading-relaxed">
          <span className="font-bold text-foreground mr-1 uppercase">Note:</span> 
          Interrupt nudges now use a bounded retry budget with exponential backoff. Once that budget is exhausted, the loop can escalate to the coordinator instead of endlessly interrupting a long-running pane.
        </div>

        <div className="flex justify-end gap-2 pt-2 border-t">
          <Button variant="ghost" size="sm" onClick={onClose}>Cancel</Button>
          <Button 
            size="sm" 
            className="font-bold px-4"
            onClick={() => patchMutation.mutate()}
            disabled={patchMutation.isPending}
          >
            {patchMutation.isPending ? 'Saving...' : 'Apply Changes'}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
