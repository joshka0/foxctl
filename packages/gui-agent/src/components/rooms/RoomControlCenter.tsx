import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { ScrollArea } from '@/components/ui/scroll-area'
import { TooltipProvider, Tooltip, TooltipTrigger, TooltipContent } from '@/components/ui/tooltip'
import { 
  getRoomStatus, 
  getRoomInbox, 
  getRoomTasks, 
  getRoomLoop,
  listRoomMessages,
  ackRoomMessage,
  resolveRoomMessage,
  bulkResolveRoomMessages,
  claimRoomTask,
  touchRoomTask,
  completeRoomTask,
  sendRoomMessage,
  unblockRoomTask,
  abandonRoomTask
} from '@/api/client'
import { useRoomControlStore } from '@/stores/roomControlStore'
import { useAuthSession } from '@/hooks/useAuthSession'
import { useViewStore } from '@/stores/viewStore'
import { cn } from '@/lib/utils'
import { TaskCard } from './TaskCard'
import { InboxRow } from './InboxRow'
import { RoomDialogs } from './RoomDialogs'
import { TimelineEvent } from './TimelineEvent'
import { ParticipantList } from './ParticipantList'
import { ReplyComposer } from './ReplyComposer'
import { LoopPolicyEditor } from './LoopPolicyEditor'
import { Hash, MessageSquare, ShieldAlert, Zap, X, CheckSquare, Bell, RefreshCw, CheckCircle2, UserCircle, Users } from 'lucide-react'
import type { MailboxMessage } from '@/api/types'

export function RoomControlCenter({ roomId }: { roomId: string }) {
  const queryClient = useQueryClient()
  const authSession = useAuthSession()
  const currentActorID = authSession.data?.user.id || 'unknown'
  const { selectedRoomWorkspaceID } = useViewStore()
  const workspaceId = selectedRoomWorkspaceID || '.'

  const { 
    activeLane, 
    setActiveLane, 
    isTimelineOpen, 
    setTimelineOpen,
    taskFilters,
    inboxFilters,
    setInboxFilters,
    openTransferLead,
    openReclaimTask,
    openBlockTask,
    openAbandonTask,
    openReassignTask,
    openConfirm,
    setTaskFilters
  } = useRoomControlStore()

  const [timelineFilter, setTimelineFilter] = useState<'all' | 'messages' | 'reclaims' | 'handoffs' | 'reminders' | 'reassignments'>('all')
  const [isParticipantsOpen, setIsParticipantsOpen] = useState(false)
  const [isLoopEditorOpen, setIsLoopEditorOpen] = useState(false)
  const [replyTarget, setReplyTarget] = useState<MailboxMessage | null>(null)

  // Queries
  const { data: status } = useQuery({
    queryKey: ['room', roomId, 'status'],
    queryFn: () => getRoomStatus(roomId, { workspace_id: workspaceId, actor_id: currentActorID }),
    refetchInterval: 10000,
  })

  const { data: inbox } = useQuery({
    queryKey: ['room', roomId, 'inbox', inboxFilters],
    queryFn: () => getRoomInbox(roomId, { 
      workspace_id: workspaceId, 
      actor_id: currentActorID, 
      only: inboxFilters.only 
    }),
  })

  const { data: tasks } = useQuery({
    queryKey: ['room', roomId, 'tasks', taskFilters],
    queryFn: () => getRoomTasks(roomId, { 
      workspace_id: workspaceId,
      include_completed: taskFilters.includeCompleted
    }),
  })

  const { data: messages } = useQuery({
    queryKey: ['room', roomId, 'messages'],
    queryFn: () => listRoomMessages(roomId, { workspace_id: workspaceId, limit: 50 }),
    enabled: isTimelineOpen,
  })

  const { data: loop } = useQuery({
    queryKey: ['room', roomId, 'loop'],
    queryFn: () => getRoomLoop(roomId, workspaceId, currentActorID),
  })

  // Mutations
  const ackMutation = useMutation({
    mutationFn: (messageId: string) => ackRoomMessage(roomId, messageId, { workspace_id: workspaceId, actor: currentActorID }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['room', roomId] }),
  })

  const resolveMutation = useMutation({
    mutationFn: (messageId: string) => resolveRoomMessage(roomId, messageId, { workspace_id: workspaceId, actor: currentActorID }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['room', roomId] }),
  })

  const bulkResolveMutation = useMutation({
    mutationFn: (kind?: string) => bulkResolveRoomMessages(roomId, { 
      workspace_id: workspaceId, 
      actor: currentActorID,
      filter: kind ? { kind } : undefined
    }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['room', roomId] }),
  })

  const claimMutation = useMutation({
    mutationFn: (taskId: string) => claimRoomTask(roomId, taskId, { workspace_id: workspaceId, actor: currentActorID }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['room', roomId] }),
  })

  const touchMutation = useMutation({
    mutationFn: (taskId: string) => touchRoomTask(roomId, taskId, { workspace_id: workspaceId, actor: currentActorID }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['room', roomId] }),
  })

  const unblockMutation = useMutation({
    mutationFn: (taskId: string) => unblockRoomTask(roomId, taskId, { workspace_id: workspaceId, actor: currentActorID }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['room', roomId] }),
  })

  const sendReplyMutation = useMutation({
    mutationFn: (params: { body: string, relatedId: string, recipient: string }) => sendRoomMessage(roomId, {
      workspace_id: workspaceId,
      sender: currentActorID,
      body: params.body,
      recipient: params.recipient,
      related_message_id: params.relatedId,
      kind: 'reply'
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['room', roomId] })
      setReplyTarget(null)
    }
  })

  const nudgeMutation = useMutation({
    mutationFn: (target: string) => sendRoomMessage(roomId, {
      workspace_id: workspaceId,
      sender: currentActorID,
      body: `Reminder for @${target}: obligation pending.`,
      recipient: target,
      kind: 'alert',
      priority: 2
    }),
  })

  const isCoordinator = status?.coordinator_actor_id === currentActorID
  const userRole = status?.participants.find(p => p.actor_id === currentActorID)?.role || 'member'

  if (!status) return <div className="p-8 text-center text-muted-foreground animate-pulse text-sm font-mono tracking-tighter">LOADING ROOM_CONTROL_PLANE...</div>

  // Lane filtering logic
  const filteredTasks = tasks?.filter(t => {
    if (activeLane === 'ready to claim') return t.status === 'pending'
    if (activeLane === 'assigned') return t.status === 'in_progress'
    if (activeLane === 'blocked') return t.status === 'blocked'
    if (activeLane === 'stale') return t.stale
    return true
  })

  const filteredInboxEntries = inbox?.entries.filter(msg => {
    if (inboxFilters.only === 'all' && msg.kind === 'info' && !msg.ack_required && !msg.reply_expected) return false
    if (activeLane === 'awaiting ack') return msg.ack_required
    if (activeLane === 'awaiting reply') return msg.reply_expected
    if (inboxFilters.only === 'alerts') return msg.kind === 'alert' || msg.kind === 'error'
    return true
  })

  // Timeline filtering
  const filteredTimeline = messages?.messages.filter(msg => {
    if (timelineFilter === 'all') return true
    if (timelineFilter === 'messages') return msg.kind === 'chat' || msg.kind === 'info'
    if (timelineFilter === 'reclaims') return msg.kind === 'task_update' && msg.body.includes('reclaimed')
    if (timelineFilter === 'handoffs') return msg.kind === 'lead_change'
    if (timelineFilter === 'reminders') return msg.kind === 'alert' && msg.subject.includes('Reminder')
    if (timelineFilter === 'reassignments') return msg.kind === 'task_update' && msg.body.includes('reassigned')
    return true
  })

  return (
    <TooltipProvider>
      <div className="flex flex-col h-full bg-background text-foreground overflow-hidden">
        {/* 1. Room Header */}
        <header className="flex items-center justify-between px-4 py-2 border-b shrink-0 bg-muted/30 text-xs font-mono">
          <div className="flex items-center gap-4 text-nowrap overflow-hidden min-w-0">
            <div className="flex items-center gap-2 shrink-0">
              <Hash className="w-4 h-4 text-primary" />
              <h1 className="text-sm font-black tracking-tighter truncate max-w-[200px]">{status.room.title || roomId}</h1>
            </div>
            <div className="flex items-center gap-1 shrink-0">
              <Badge variant="outline" className="text-[9px] py-0 px-1.5 h-4 opacity-70 border-muted">WS: {workspaceId}</Badge>
              <Badge variant="outline" className="text-[9px] py-0 px-1.5 h-4 opacity-70 border-muted">POLICY: {status.room.dispatch_policy}</Badge>
            <Badge variant="secondary" className="text-[9px] py-0 px-1.5 h-4 bg-primary/10 text-primary border-primary/20 font-black uppercase">{userRole}</Badge>
            {loop ? (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Badge 
                    variant="secondary" 
                    className={cn(
                      "text-[9px] py-0 h-4 flex items-center gap-1 shrink-0 transition-colors",
                      loop.enabled 
                        ? "bg-green-500/10 text-green-600 border-green-500/20" 
                        : "bg-muted text-muted-foreground border-muted",
                      isCoordinator && "cursor-pointer hover:opacity-80"
                    )}
                    onClick={() => isCoordinator && setIsLoopEditorOpen(true)}
                  >
                    <Zap className={cn("w-2 h-2", loop.enabled ? "fill-current" : "opacity-50")} /> 
                    LOOP: {loop.enabled ? loop.pulse_interval : 'OFF'}
                  </Badge>
                </TooltipTrigger>
                <TooltipContent side="bottom" className="text-[10px] space-y-1">
                  <p>Managed by: <span className="font-bold">{loop.managed_by || 'none'}</span></p>
                  {loop.enabled && <p>Last tick: <span className="font-mono">{loop.last_tick_at ? new Date(loop.last_tick_at).toLocaleTimeString() : 'never'}</span></p>}
                  <p>Stale thresholds: task={loop.task_stale_after}, reply={loop.reply_stale_after}</p>
                  {isCoordinator && <p className="text-[9px] text-primary/70 italic pt-1 font-bold">Click to edit loop policy</p>}
                </TooltipContent>
              </Tooltip>
            ) : isCoordinator && (
              <Button 
                variant="ghost" 
                size="xs" 
                className="h-4 px-1.5 text-[9px] font-black text-muted-foreground hover:text-primary"
                onClick={() => setIsLoopEditorOpen(true)}
              >
                + CONFIGURE LOOP
              </Button>
            )}
            </div>
          </div>
          <div className="flex items-center gap-2 shrink-0 ml-4">
            <div className="text-[10px] text-muted-foreground flex items-center gap-1.5 mr-2">
              <span className="opacity-70">Coordinator:</span>
              <span className={cn(
                "font-bold px-1.5 py-0.5 rounded flex items-center gap-1 border",
                isCoordinator ? "bg-primary text-primary-foreground border-primary" : "bg-muted text-foreground border-border"
              )}>
                {isCoordinator ? <ShieldAlert className="w-2.5 h-2.5" /> : <UserCircle className="w-2.5 h-2.5 opacity-50" />}
                {status.coordinator_actor_id || 'none'}
              </span>
            </div>
            {isCoordinator && (
              <Button variant="outline" size="xs" className="text-[10px] h-7 font-black border-primary/20 hover:bg-primary/5 uppercase tracking-tighter" onClick={() => openTransferLead()}>
                Transfer Lead
              </Button>
            )}
            <Button variant="outline" size="xs" className="text-[10px] h-7 font-bold px-2" onClick={() => setIsParticipantsOpen(!isParticipantsOpen)}>
              <Users className="w-3.5 h-3.5 mr-1.5" /> {status.participants.length}
            </Button>
            <Button 
              variant={isTimelineOpen ? 'secondary' : 'outline'} 
              size="xs"
              className="text-[10px] h-7 font-bold px-2"
              onClick={() => setTimelineOpen(!isTimelineOpen)}
            >
              {isTimelineOpen ? 'Close Timeline' : 'Timeline'}
            </Button>
          </div>
        </header>

        {/* 2. Action Ribbon */}
        <div className="flex items-center gap-2 px-4 py-2 border-b bg-muted/10 shrink-0 overflow-x-auto no-scrollbar">
          <RibbonChip 
            label="ready to claim" 
            count={status.task_pulse.pending} 
            color="bg-blue-500" 
            active={activeLane === 'ready to claim'}
            onClick={() => setActiveLane('ready to claim')}
          />
          <RibbonChip 
            label="awaiting ack" 
            count={status.actionable_backlog.pending_acks} 
            color="bg-amber-500" 
            active={activeLane === 'awaiting ack'}
            onClick={() => setActiveLane('awaiting ack')}
          />
          <RibbonChip 
            label="awaiting reply" 
            count={status.actionable_backlog.pending_replies} 
            color="bg-purple-500" 
            active={activeLane === 'awaiting reply'}
            onClick={() => setActiveLane('awaiting reply')}
          />
          <RibbonChip 
            label="assigned" 
            count={status.task_pulse.in_progress} 
            color="bg-slate-500" 
            active={activeLane === 'assigned'}
            onClick={() => setActiveLane('assigned')}
          />
          <RibbonChip 
            label="blocked" 
            count={status.task_pulse.blocked} 
            color="bg-red-500" 
            active={activeLane === 'blocked'}
            onClick={() => setActiveLane('blocked')}
          />
          <RibbonChip 
            label="stale" 
            count={status.task_pulse.stale} 
            color="bg-orange-600" 
            animatePulse={status.task_pulse.stale > 0}
            active={activeLane === 'stale'}
            onClick={() => setActiveLane('stale')}
          />
          {activeLane !== 'all' && (
            <Button 
              variant="ghost" 
              size="xs" 
              onClick={() => setActiveLane('all')} 
              className="text-[10px] uppercase font-black h-7 hover:bg-red-50 hover:text-red-600 text-muted-foreground ml-2"
            >
              <X className="w-3 h-3 mr-1" /> Clear Filter
            </Button>
          )}
        </div>

        <div className="flex flex-1 min-h-0 relative">
          {/* Left: Participant List (Transport UX) */}
          {isParticipantsOpen && (
            <aside className="w-64 border-r bg-muted/5 flex flex-col shrink-0">
              <ParticipantList participants={status.participants} coordinatorId={status.coordinator_actor_id} />
            </aside>
          )}

          {/* Main Content: Task Board & Inbox */}
          <div className="flex-1 flex flex-col min-w-0">
            {/* 3. Task Board */}
            <div className="flex-1 min-h-0 flex flex-col border-b">
              <div className="px-4 py-1.5 bg-muted/5 border-b flex items-center justify-between">
                <div className="flex items-center gap-2 text-muted-foreground text-left">
                  <ShieldAlert className="w-3.5 h-3.5" />
                  <h2 className="text-[10px] font-black uppercase tracking-widest">Task Board</h2>
                </div>
                <div className="flex items-center gap-3">
                  <div className="flex items-center gap-1.5 mr-2">
                    <Switch 
                      id="show-completed" 
                      checked={taskFilters.includeCompleted} 
                      onCheckedChange={(checked) => setTaskFilters({ includeCompleted: checked })} 
                    />
                    <Label htmlFor="show-completed" className="text-[9px] font-bold uppercase text-muted-foreground cursor-pointer">Show Done</Label>
                  </div>
                  <Badge variant="outline" className="text-[9px] h-4 py-0 cursor-pointer hover:bg-muted font-bold" onClick={() => queryClient.invalidateQueries({ queryKey: ['room', roomId, 'tasks'] })}>
                    <RefreshCw className="w-2.5 h-2.5 mr-1" /> REFRESH
                  </Badge>
                </div>
              </div>
              <div className="flex-1 flex gap-4 p-4 overflow-x-auto min-h-0 bg-muted/5">
                <TaskColumn title="Pending" count={filteredTasks?.filter(t => t.status === 'pending').length || 0}>
                  {filteredTasks?.filter(t => t.status === 'pending').map(task => (
                    <TaskCard 
                      key={task.id} 
                      task={task} 
                      onClaim={() => claimMutation.mutate(task.id)}
                      onReassign={() => openReassignTask(task)}
                      isCoordinator={isCoordinator}
                    />
                  ))}
                </TaskColumn>
                <TaskColumn title="Claimed" count={filteredTasks?.filter(t => t.status === 'in_progress' && !t.stale).length || 0}>
                  {filteredTasks?.filter(t => t.status === 'in_progress' && !t.stale).map(task => (
                    <TaskCard 
                      key={task.id} 
                      task={task} 
                      onTouch={() => touchMutation.mutate(task.id)}
                      onBlock={() => openBlockTask(task)}
                      onComplete={() => openConfirm("Complete Task", `Mark "${task.title}" as done?`, () => completeMutation.mutate(task.id))}
                      onReclaim={() => openReclaimTask(task)}
                      onReassign={() => openReassignTask(task)}
                      isCoordinator={isCoordinator}
                    />
                  ))}
                </TaskColumn>
                <TaskColumn title="Blocked" count={filteredTasks?.filter(t => t.status === 'blocked' && !t.stale).length || 0}>
                  {filteredTasks?.filter(t => t.status === 'blocked' && !t.stale).map(task => (
                    <TaskCard 
                      key={task.id} 
                      task={task} 
                      onTouch={() => unblockMutation.mutate(task.id)}
                      onAbandon={() => openAbandonTask(task)}
                      onReclaim={() => openReclaimTask(task)}
                      onReassign={() => openReassignTask(task)}
                      isCoordinator={isCoordinator}
                    />
                  ))}
                </TaskColumn>
                <TaskColumn title="Stale / Done" count={(filteredTasks?.filter(t => t.stale).length || 0) + (filteredTasks?.filter(t => t.status === 'completed').length || 0)}>
                  {filteredTasks?.filter(t => t.stale || t.status === 'completed').map(task => (
                    <TaskCard 
                      key={task.id} 
                      task={task} 
                      isCoordinator={isCoordinator}
                      onReclaim={() => openReclaimTask(task)}
                      onReassign={() => openReassignTask(task)}
                    />
                  ))}
                </TaskColumn>
              </div>
            </div>

            {/* 4. Inbox */}
            <div className="h-1/3 min-h-[240px] flex flex-col shrink-0">
              <div className="px-4 py-1.5 bg-muted/5 border-b flex items-center justify-between">
                <div className="flex items-center gap-2 text-muted-foreground text-left">
                  <MessageSquare className="w-3.5 h-3.5" />
                  <h2 className="text-[10px] font-bold uppercase tracking-widest">Inbox</h2>
                </div>
                <div className="flex gap-1.5 items-center">
                  <InboxFilterButton active={inboxFilters.only === 'all'} label="All" onClick={() => setInboxFilters({ only: 'all' })} />
                  <InboxFilterButton active={inboxFilters.only === 'direct'} label="Direct" onClick={() => setInboxFilters({ only: 'direct' })} />
                  <InboxFilterButton active={inboxFilters.only === 'ack'} label="Ack" onClick={() => setInboxFilters({ only: 'ack' })} />
                  <InboxFilterButton active={inboxFilters.only === 'reply'} label="Reply" onClick={() => setInboxFilters({ only: 'reply' })} />
                  <InboxFilterButton active={inboxFilters.only === 'alerts'} label="Alerts" onClick={() => setInboxFilters({ only: 'alerts' })} />
                  {isCoordinator && (
                    <div className="ml-4 pl-4 border-l border-muted flex items-center gap-1.5">
                      <span className="text-[9px] font-black text-muted-foreground uppercase mr-1">Bulk Resolve:</span>
                      <BulkActionButton label="INFO" onClick={() => openConfirm("Bulk Resolve: INFO", "Resolve all informational broadcasts?", () => bulkResolveMutation.mutate('info'))} />
                      <BulkActionButton label="ACKS" onClick={() => openConfirm("Bulk Resolve: ACKS", "Force-resolve all pending acknowledgements?", () => bulkResolveMutation.mutate('ack'))} />
                      <BulkActionButton label="ALL" onClick={() => openConfirm("Bulk Resolve: ALL", "Danger: Resolve every actionable item in the inbox?", () => bulkResolveMutation.mutate())} variant="destructive" />
                    </div>
                  )}
                </div>
              </div>
              <ScrollArea className="flex-1">
                <div className="divide-y divide-muted/30">
                  {replyTarget && (
                    <ReplyComposer 
                      recipient={replyTarget.sender}
                      subject={replyTarget.subject}
                      relatedMessageId={replyTarget.id}
                      onSend={(body) => sendReplyMutation.mutate({ body, relatedId: replyTarget.id, recipient: replyTarget.sender })}
                      onCancel={() => setReplyTarget(null)}
                    />
                  )}
                  {filteredInboxEntries?.map(msg => (
                    <InboxRow 
                      key={msg.id}
                      message={msg}
                      onAck={() => ackMutation.mutate(msg.id)}
                      onResolve={() => openConfirm("Resolve Message", "Clear this message from the actionable inbox?", () => resolveMutation.mutate(msg.id))}
                      onReply={() => setReplyTarget(msg)}
                      onNudge={() => nudgeMutation.mutate(msg.sender)}
                      isCoordinator={isCoordinator}
                    />
                  ))}
                  {(!filteredInboxEntries || filteredInboxEntries.length === 0) && (
                    <div className="p-12 flex flex-col items-center justify-center text-muted-foreground opacity-50">
                      <CheckCircle2 className="w-8 h-8 mb-2 text-primary/40" />
                      <p className="text-xs italic font-medium">Inbox zero. All obligations cleared.</p>
                    </div>
                  )}
                </div>
              </ScrollArea>
            </div>
          </div>

          {/* 5. Timeline Drawer (Right) */}
          {isTimelineOpen && (
            <aside className="w-80 border-l bg-muted/5 flex flex-col shrink-0 absolute right-0 top-0 bottom-0 z-10 shadow-xl lg:relative lg:shadow-none animate-in slide-in-from-right duration-200">
              <div className="px-4 py-2 bg-muted/10 border-b flex items-center justify-between shrink-0">
                <h2 className="text-[10px] font-black uppercase tracking-widest text-muted-foreground">Timeline</h2>
                <Button variant="ghost" size="xs" onClick={() => setTimelineOpen(false)} className="h-6 w-6 p-0">
                  <X className="w-3 h-3" />
                </Button>
              </div>
              <div className="px-2 py-2 border-b flex flex-wrap gap-1 bg-background/50 shrink-0">
                <TimelineFilter label="messages" active={timelineFilter === 'messages'} onClick={() => setTimelineFilter('messages')} />
                <TimelineFilter label="reclaims" active={timelineFilter === 'reclaims'} onClick={() => setTimelineFilter('reclaims')} />
                <TimelineFilter label="reassigns" active={timelineFilter === 'reassignments'} onClick={() => setTimelineFilter('reassignments')} />
                <TimelineFilter label="handoffs" active={timelineFilter === 'handoffs'} onClick={() => setTimelineFilter('handoffs')} />
                <TimelineFilter label="reminders" active={timelineFilter === 'reminders'} onClick={() => setTimelineFilter('reminders')} />
                <TimelineFilter label="all" active={timelineFilter === 'all'} onClick={() => setTimelineFilter('all')} />
              </div>
              <ScrollArea className="flex-1">
                <div className="p-3 space-y-3">
                  {filteredTimeline?.map(msg => (
                    <TimelineEvent key={msg.id} event={msg} type={msg.kind === 'lead_change' ? 'lead' : (msg.kind === 'task_update' ? 'task' : 'message')} />
                  ))}
                  {(!filteredTimeline || filteredTimeline.length === 0) && (
                    <TimelinePlaceholder />
                  )}
                </div>
              </ScrollArea>
            </aside>
          )}
        </div>
        
        {/* Dialogs */}
        <RoomDialogs roomId={roomId} />
        {loop && (
          <LoopPolicyEditor 
            roomId={roomId} 
            workspaceId={workspaceId} 
            actorId={currentActorID}
            loop={loop} 
            isOpen={isLoopEditorOpen} 
            onClose={() => setIsLoopEditorOpen(false)} 
          />
        )}
      </div>
    </TooltipProvider>
  )
}

function RibbonChip({ label, count, color, animatePulse, active, onClick }: { 
  label: string, 
  count: number, 
  color: string, 
  animatePulse?: boolean,
  active?: boolean,
  onClick?: () => void
}) {
  return (
    <div 
      onClick={onClick}
      className={cn(
        "flex items-center gap-2 px-3 py-1 rounded-full border bg-background hover:bg-muted/50 cursor-pointer transition-all shrink-0 h-7",
        animatePulse && "ring-2 ring-orange-500/50 animate-pulse border-orange-500/50",
        active ? "border-primary bg-primary/5 ring-1 ring-primary/20 shadow-inner" : "border-muted shadow-sm"
      )}
    >
      <span className={cn("w-2 h-2 rounded-full", color)} />
      <span className="text-[10px] font-black uppercase tracking-tight text-foreground/70">{label}</span>
      <span className={cn(
        "text-[10px] font-black px-1.5 py-0.5 rounded min-w-[1.25rem] text-center transition-colors font-mono",
        active ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground"
      )}>{count}</span>
    </div>
  )
}

function TaskColumn({ title, count, children }: { title: string, count: number, children: React.ReactNode }) {
  return (
    <div className="w-72 shrink-0 flex flex-col gap-2.5">
      <div className="flex items-center justify-between px-1.5 text-left font-mono">
        <h3 className="text-[10px] font-black uppercase tracking-widest text-muted-foreground/60">{title}</h3>
        <Badge variant="secondary" className="text-[9px] h-4 px-1.5 font-bold tabular-nums">{count}</Badge>
      </div>
      <ScrollArea className="flex-1 rounded-xl border border-muted/40 bg-muted/5 p-2.5 shadow-inner">
        <div className="space-y-2.5">
          {children}
        </div>
      </ScrollArea>
    </div>
  )
}

function InboxFilterButton({ active, label, onClick }: { active: boolean, label: string, onClick: () => void }) {
  return (
    <button 
      onClick={onClick}
      className={cn(
        "text-[9px] font-black uppercase tracking-tighter px-2 py-0.5 rounded transition-all font-mono",
        active ? "bg-primary text-primary-foreground shadow-sm" : "text-muted-foreground hover:bg-muted"
      )}
    >
      {label}
    </button>
  )
}

function BulkActionButton({ label, onClick, variant = 'default' }: { label: string, onClick: () => void, variant?: 'default' | 'destructive' }) {
  return (
    <Button 
      variant="ghost" 
      size="xs" 
      onClick={onClick}
      className={cn(
        "h-5 px-1.5 text-[8px] font-black border font-mono tracking-tighter",
        variant === 'destructive' ? "text-red-600 border-red-500/20 hover:bg-red-50" : "text-muted-foreground border-muted hover:text-primary hover:border-primary/20"
      )}
    >
      {label}
    </Button>
  )
}

function TimelineFilter({ label, active, onClick }: { label: string, active?: boolean, onClick?: () => void }) {
  return (
    <Badge 
      variant="outline" 
      onClick={onClick}
      className={cn(
        "text-[9px] py-0 h-5 cursor-pointer hover:bg-muted transition-colors font-mono uppercase tracking-tighter",
        active ? "bg-primary/5 border-primary/20 text-primary font-black" : "text-muted-foreground border-muted/50"
      )}
    >
      {label}
    </Badge>
  )
}

function TimelinePlaceholder() {
  return (
    <div className="flex flex-col items-center justify-center p-8 space-y-3 opacity-40">
      <Bell className="w-8 h-8 text-muted-foreground" />
      <p className="text-[10px] text-muted-foreground italic text-center leading-relaxed">
        No events found for<br />the selected filter.
      </p>
    </div>
  )
}
