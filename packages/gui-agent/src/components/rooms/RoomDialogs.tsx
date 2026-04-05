import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Modal, ConfirmModal } from '@/components/ui/modal'
import { useRoomControlStore } from '@/stores/roomControlStore'
import { transferRoomCoordinator, reclaimRoomTask, blockRoomTask, abandonRoomTask, reassignRoomTask } from '@/api/client'

export function RoomDialogs({ roomId }: { roomId: string }) {
  const queryClient = useQueryClient()
  const { 
    dialogs, 
    closeTransferLead, 
    closeReclaimTask, 
    closeConfirm,
    closeBlockTask,
    closeAbandonTask,
    closeReassignTask
  } = useRoomControlStore()
  
  const [leadTarget, setLeadTarget] = useState('')
  const [leadNote, setLeadNote] = useState('')
  const [reclaimReason, setReclaimReason] = useState('')
  const [blockReason, setBlockReason] = useState('')
  const [reassignTarget, setReassignTarget] = useState('')
  const [reassignReason, setReassignReason] = useState('')

  const transferMutation = useMutation({
    mutationFn: () => transferRoomCoordinator(roomId, { 
      workspace_id: '.', 
      to: leadTarget || dialogs.transferLead.targetActorID || '', 
      note: leadNote 
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['room', roomId] })
      closeTransferLead()
      setLeadTarget('')
      setLeadNote('')
    }
  })

  const reclaimMutation = useMutation({
    mutationFn: () => reclaimRoomTask(roomId, dialogs.reclaimTask.task?.id || '', {
      workspace_id: '.',
      reason: reclaimReason
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['room', roomId] })
      closeReclaimTask()
      setReclaimReason('')
    }
  })

  const blockMutation = useMutation({
    mutationFn: () => blockRoomTask(roomId, dialogs.blockTask.task?.id || '', {
      workspace_id: '.',
      reason: blockReason
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['room', roomId] })
      closeBlockTask()
      setBlockReason('')
    }
  })

  const abandonMutation = useMutation({
    mutationFn: () => abandonRoomTask(roomId, dialogs.abandonTask.task?.id || '', {
      workspace_id: '.'
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['room', roomId] })
      closeAbandonTask()
    }
  })

  const reassignMutation = useMutation({
    mutationFn: () => reassignRoomTask(roomId, dialogs.reassignTask.task?.id || '', {
      workspace_id: '.',
      to: reassignTarget || dialogs.reassignTask.targetActorID || '',
      reason: reassignReason
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['room', roomId] })
      closeReassignTask()
      setReassignTarget('')
      setReassignReason('')
    }
  })

  return (
    <>
      {/* Transfer Lead Dialog */}
      <Modal 
        isOpen={dialogs.transferLead.isOpen} 
        onClose={closeTransferLead}
        title="Transfer Coordinator Lead"
      >
        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="target-actor" className="text-xs uppercase font-bold text-muted-foreground">Target Actor ID</Label>
            <Input 
              id="target-actor"
              value={leadTarget || dialogs.transferLead.targetActorID || ''} 
              onChange={(e) => setLeadTarget(e.target.value)}
              placeholder="e.g. claude-a"
              className="text-sm"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="lead-note" className="text-xs uppercase font-bold text-muted-foreground">Transition Note (Optional)</Label>
            <Textarea 
              id="lead-note"
              value={leadNote} 
              onChange={(e) => setLeadNote(e.target.value)}
              placeholder="Reason for handoff..."
              className="text-sm min-h-[80px]"
            />
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="ghost" size="sm" onClick={closeTransferLead}>Cancel</Button>
            <Button 
              size="sm" 
              disabled={!leadTarget && !dialogs.transferLead.targetActorID}
              onClick={() => transferMutation.mutate()}
            >
              Transfer Lead
            </Button>
          </div>
        </div>
      </Modal>

      {/* Force Reclaim Dialog */}
      <Modal
        isOpen={dialogs.reclaimTask.isOpen}
        onClose={closeReclaimTask}
        title="Force Reclaim Task"
      >
        <div className="space-y-4">
          <div className="p-3 bg-destructive/5 border border-destructive/10 rounded-md text-[13px]">
            <p className="font-bold text-destructive mb-1">Warning: Disruptive Action</p>
            <p className="text-muted-foreground">
              Reclaiming task <span className="font-mono bg-muted px-1 rounded">{dialogs.reclaimTask.task?.title}</span> will strip the current owner and return it to pending.
            </p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="reclaim-reason" className="text-xs uppercase font-bold text-muted-foreground">Reason for Reclaim</Label>
            <Textarea 
              id="reclaim-reason"
              value={reclaimReason} 
              onChange={(e) => setReclaimReason(e.target.value)}
              placeholder="e.g. Owner heartbeat timed out, priority shift..."
              className="text-sm min-h-[80px]"
            />
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="ghost" size="sm" onClick={closeReclaimTask}>Cancel</Button>
            <Button 
              variant="destructive"
              size="sm" 
              disabled={!reclaimReason.trim()}
              onClick={() => reclaimMutation.mutate()}
            >
              Force Reclaim
            </Button>
          </div>
        </div>
      </Modal>

      {/* Block Task Dialog */}
      <Modal
        isOpen={dialogs.blockTask.isOpen}
        onClose={closeBlockTask}
        title="Block Task"
      >
        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="block-reason" className="text-xs uppercase font-bold text-muted-foreground">Reason for Blocking</Label>
            <Textarea 
              id="block-reason"
              value={blockReason} 
              onChange={(e) => setBlockReason(e.target.value)}
              placeholder="e.g. Waiting for upstream dependency..."
              className="text-sm min-h-[80px]"
            />
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="ghost" size="sm" onClick={closeBlockTask}>Cancel</Button>
            <Button 
              variant="destructive"
              size="sm" 
              disabled={!blockReason.trim()}
              onClick={() => blockMutation.mutate()}
            >
              Block Task
            </Button>
          </div>
        </div>
      </Modal>

      {/* Reassign Task Dialog */}
      <Modal
        isOpen={dialogs.reassignTask.isOpen}
        onClose={closeReassignTask}
        title="Reassign Task"
      >
        <div className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="reassign-target" className="text-xs uppercase font-bold text-muted-foreground">Target Actor ID</Label>
            <Input 
              id="reassign-target"
              value={reassignTarget || dialogs.reassignTask.targetActorID || ''} 
              onChange={(e) => setReassignTarget(e.target.value)}
              placeholder="e.g. gemini-a"
              className="text-sm"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="reassign-reason" className="text-xs uppercase font-bold text-muted-foreground">Reason (Optional)</Label>
            <Textarea 
              id="reassign-reason"
              value={reassignReason} 
              onChange={(e) => setReassignReason(e.target.value)}
              placeholder="Reason for reassignment..."
              className="text-sm min-h-[80px]"
            />
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="ghost" size="sm" onClick={closeReassignTask}>Cancel</Button>
            <Button 
              size="sm" 
              disabled={!reassignTarget && !dialogs.reassignTask.targetActorID}
              onClick={() => reassignMutation.mutate()}
            >
              Reassign Task
            </Button>
          </div>
        </div>
      </Modal>

      {/* Abandon Task Dialog */}
      <ConfirmModal
        isOpen={dialogs.abandonTask.isOpen}
        onClose={closeAbandonTask}
        onConfirm={() => abandonMutation.mutate()}
        title="Abandon Task"
        message={`Are you sure you want to abandon "${dialogs.abandonTask.task?.title}"? It will return to pending.`}
        confirmText="Abandon"
        variant="destructive"
      />

      {/* Generic Confirm Dialog */}
      <ConfirmModal
        isOpen={dialogs.confirmAction.isOpen}
        onClose={closeConfirm}
        onConfirm={dialogs.confirmAction.onConfirm || (() => {})}
        title={dialogs.confirmAction.title}
        message={dialogs.confirmAction.message}
      />
    </>
  )
}
