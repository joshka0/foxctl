import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Textarea } from '@/components/ui/textarea'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { addRoomReminder, cancelRoomReminder, listRoomReminders, readMuxPane } from '@/api/client'
import type { MuxPane, RoomDeliveryBinding, RoomReminder, RoomSendMessageResult, RoomStatusParticipant } from '@foxctl/data/types'
import { participantTransportKind } from '@/lib/room-utils'
import { Monitor, RefreshCw, SendHorizonal, TerminalSquare } from 'lucide-react'

interface RoomTerminalViewProps {
  roomId: string
  workspaceId: string
  panes: MuxPane[]
  participants: RoomStatusParticipant[]
  sender: string
  onSend: (params: {
    recipients?: string[]
    subject?: string
    body: string
    kind: string
    ack_required: boolean
    reply_expected: boolean
    interrupt: boolean
  }) => Promise<RoomSendMessageResult>
  onRebind: (params: {
    actorId: string
    delivery_binding?: RoomDeliveryBinding
  }) => Promise<void>
  sending?: boolean
  rebinding?: boolean
  loading?: boolean
}

interface MessagePreset {
  id: string
  label: string
  audience?: 'broadcast' | 'selected'
  kind: string
  subject?: string
  body: string
  ack_required: boolean
  reply_expected: boolean
  interrupt: boolean
}

export function RoomTerminalView({ roomId, workspaceId, panes, participants, sender, onSend, onRebind, sending, rebinding, loading }: RoomTerminalViewProps) {
  const queryClient = useQueryClient()
  const tmuxPanes = useMemo(() => panes.filter((pane) => pane.backend === 'tmux'), [panes])
  const terminalParticipants = useMemo(
    () =>
      participants
        .filter((participant) => {
          const actorId = participant.actor_id
          if (!actorId) return false
          if (actorId.startsWith('actor:system:')) return false
          return true
        })
        .sort((a, b) => a.actor_id.localeCompare(b.actor_id)),
    [participants],
  )
  const participantPaneByActor = useMemo(() => {
    const map = new Map<string, MuxPane>()
    for (const pane of tmuxPanes) {
      const actorId = pane.participant_id || pane.label || ''
      if (!actorId || map.has(actorId)) continue
      map.set(actorId, pane)
    }
    return map
  }, [tmuxPanes])
  const dispatchableParticipants = useMemo(
    () =>
      participants
        .filter((participant) => {
          const actorId = participant.actor_id
          if (!actorId || actorId === sender) return false
          if (actorId.startsWith('actor:system:')) return false
          if (actorId.startsWith('tmux:') || actorId.startsWith('zellij:')) return false
          return true
        })
        .sort((a, b) => a.actor_id.localeCompare(b.actor_id)),
    [participants, sender],
  )
  const reminderEligibleParticipants = useMemo(
    () =>
      participants
        .filter((participant) => {
          const actorId = participant.actor_id
          if (!actorId) return false
          if (actorId.startsWith('actor:system:')) return false
          if (actorId.startsWith('tmux:') || actorId.startsWith('zellij:')) return false
          return true
        })
        .sort((a, b) => a.actor_id.localeCompare(b.actor_id)),
    [participants],
  )

  const [selectedKey, setSelectedKey] = useState('')
  const [audienceMode, setAudienceMode] = useState<'selected' | 'broadcast'>('selected')
  const [selectedRecipients, setSelectedRecipients] = useState<string[]>([])
  const [subject, setSubject] = useState('')
  const [draft, setDraft] = useState('')
  const [kind, setKind] = useState('instruction')
  const [ackRequired, setAckRequired] = useState(false)
  const [replyExpected, setReplyExpected] = useState(false)
  const [interrupt, setInterrupt] = useState(false)
  const [selectedPreset, setSelectedPreset] = useState('')
  const [lastSendResult, setLastSendResult] = useState<RoomSendMessageResult | null>(null)
  const [sendError, setSendError] = useState('')
  const [showAudiencePanel, setShowAudiencePanel] = useState(false)
  const [showReminderEditor, setShowReminderEditor] = useState(false)
  const [showReminderList, setShowReminderList] = useState(false)
  const [showRebindPanel, setShowRebindPanel] = useState(false)
  const [reminderRecipient, setReminderRecipient] = useState('')
  const [reminderEvery, setReminderEvery] = useState('15m')
  const [reminderMaxIterations, setReminderMaxIterations] = useState('3')
  const [allowPassiveReminder, setAllowPassiveReminder] = useState(false)
  const [lastReminderResult, setLastReminderResult] = useState<RoomReminder | null>(null)
  const [reminderError, setReminderError] = useState('')
  const [bindActor, setBindActor] = useState('')
  const [rebindNotice, setRebindNotice] = useState('')

  useEffect(() => {
    if (selectedKey && !terminalParticipants.some((participant) => participant.actor_id === selectedKey)) {
      setSelectedKey('')
    }
  }, [terminalParticipants, selectedKey])

  const selectedPane = selectedKey ? participantPaneByActor.get(selectedKey) : undefined
  const target = selectedPane ? paneTarget(selectedPane) : ''
  const paneRecipient = selectedKey || (selectedPane ? paneDispatchActor(selectedPane) : '')
  const isBroadcast = audienceMode === 'broadcast'
  const selectedParticipant = useMemo(() => {
    if (audienceMode !== 'selected' || selectedRecipients.length !== 1) return undefined
    return dispatchableParticipants.find((participant) => participant.actor_id === selectedRecipients[0])
  }, [audienceMode, dispatchableParticipants, selectedRecipients])
  const transportUnavailable = selectedParticipant?.transport?.transport === 'unavailable'
  const transportReady = selectedParticipant?.transport?.transport === 'available'
  const bindOptions = useMemo(() => participants.map((participant) => participant.actor_id).filter(Boolean).sort(), [participants])

  useEffect(() => {
    const preferred = paneRecipient && bindOptions.includes(paneRecipient) ? paneRecipient : bindOptions[0] || ''
    if (!bindActor || !bindOptions.includes(bindActor)) {
      setBindActor(preferred)
    }
  }, [bindActor, bindOptions, paneRecipient])

  useEffect(() => {
    const recipients = reminderEligibleParticipants.map((participant) => participant.actor_id)
    if (selectedRecipients.length === 1 && recipients.includes(selectedRecipients[0])) {
      setReminderRecipient(selectedRecipients[0])
      return
    }
    if (paneRecipient && recipients.includes(paneRecipient)) {
      setReminderRecipient(paneRecipient)
      return
    }
    if (reminderRecipient && recipients.includes(reminderRecipient)) return
    if (sender && recipients.includes(sender)) {
      setReminderRecipient(sender)
      return
    }
    setReminderRecipient(recipients[0] || '')
  }, [paneRecipient, reminderEligibleParticipants, reminderRecipient, selectedRecipients, sender])

  useEffect(() => {
    if (!isBroadcast) return
    if (ackRequired) setAckRequired(false)
    if (replyExpected) setReplyExpected(false)
    if (interrupt) setInterrupt(false)
  }, [ackRequired, interrupt, isBroadcast, replyExpected])

  const captureQuery = useQuery({
    queryKey: ['room', roomId, 'terminal-capture', target],
    enabled: !!target,
    queryFn: () => readMuxPane(target, { backend: 'tmux', lines: 120 }),
    refetchInterval: 2000,
  })

  const remindersQuery = useQuery({
    queryKey: ['room', roomId, 'reminders'],
    queryFn: () => listRoomReminders(roomId, { workspace_id: workspaceId }),
    enabled: !!roomId && !!workspaceId,
    refetchInterval: 5000,
  })

  const addReminderMutation = useMutation({
    mutationFn: (params: {
      recipient: string
      subject?: string
      body: string
      every: string
      max_iterations: number
      ack_required: boolean
      reply_expected: boolean
      interrupt: boolean
      allow_passive: boolean
    }) =>
      addRoomReminder(roomId, {
        workspace_id: workspaceId,
        sender,
        recipient: params.recipient,
        subject: params.subject,
        body: params.body,
        every: params.every,
        max_iterations: params.max_iterations,
        ack_required: params.ack_required,
        reply_expected: params.reply_expected,
        interrupt: params.interrupt,
        allow_passive: params.allow_passive,
      }),
    onSuccess: async (result) => {
      setLastReminderResult(result.reminder)
      setReminderError('')
      setSelectedPreset('')
      setDraft('')
      setSubject('')
      setAckRequired(false)
      setReplyExpected(false)
      setInterrupt(false)
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: ['room', roomId] }),
        queryClient.invalidateQueries({ queryKey: ['room', roomId, 'reminders'] }),
      ])
    },
  })

  const cancelReminderMutation = useMutation({
    mutationFn: (reminderId: string) => cancelRoomReminder(roomId, reminderId, { workspace_id: workspaceId, actor: sender }),
    onSuccess: async () => {
      setReminderError('')
      await queryClient.invalidateQueries({ queryKey: ['room', roomId, 'reminders'] })
    },
  })

  const audienceSummary = isBroadcast
    ? 'broadcast (*)'
    : selectedRecipients.length === 0
      ? 'no recipients'
      : selectedRecipients.length === 1
        ? selectedRecipients[0]
        : `${selectedRecipients.length} selected`
  const effectiveReminderRecipient =
    selectedRecipients.length === 1 && reminderEligibleParticipants.some((participant) => participant.actor_id === selectedRecipients[0])
      ? selectedRecipients[0]
      : reminderRecipient

  const statusLine = reminderError
    ? `ERR · ${reminderError}`
    : sendError
      ? `ERR · ${sendError}`
      : rebindNotice
        ? rebindNotice
        : lastReminderResult
          ? `last reminder · ${lastReminderResult.recipient} · every ${lastReminderResult.interval} · ${lastReminderResult.sent_count}/${lastReminderResult.max_iterations}`
          : lastSendResult
            ? `last send · ${lastSendResult.status} · ${summarizeSendDelivery(lastSendResult)}`
            : 'No recent dispatch activity.'

  return (
    <div className="flex-1 min-h-0 flex flex-col bg-muted/5">
      <div className="px-4 py-2 border-b bg-muted/20 flex items-center justify-between gap-3">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <TerminalSquare className="w-4 h-4 text-primary" />
            <h2 className="text-xs font-black uppercase tracking-widest">Participant PTYs</h2>
            <Badge variant="outline" className="text-[9px] h-4 px-1.5 font-mono">
              tmux
            </Badge>
          </div>
          <p className="text-[11px] text-muted-foreground mt-1">
            Read-only room-member PTY preview. Delivery still goes through participant transport, not this panel.
          </p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="outline"
                size="xs"
                className="font-bold"
                onClick={() => captureQuery.refetch()}
                disabled={!target || captureQuery.isFetching}
              >
                <RefreshCw className={cn('w-3.5 h-3.5 mr-1.5', captureQuery.isFetching && 'animate-spin')} />
                Refresh
              </Button>
            </TooltipTrigger>
            <TooltipContent side="bottom" className="text-[10px]">
              Reload the selected participant PTY preview.
            </TooltipContent>
          </Tooltip>
        </div>
      </div>

      <div className="px-4 py-2 border-b bg-background/70 flex items-center gap-2 overflow-x-auto no-scrollbar">
        {loading ? (
          <span className="text-[11px] text-muted-foreground animate-pulse">Loading room viewer panes…</span>
        ) : terminalParticipants.length === 0 ? (
          <span className="text-[11px] text-muted-foreground">No room participants available for PTY preview.</span>
        ) : (
          terminalParticipants.map((participant) => {
            const pane = participantPaneByActor.get(participant.actor_id)
            const active = participant.actor_id === selectedKey
            return (
              <Button
                key={participant.actor_id}
                variant={active ? 'secondary' : 'outline'}
                size="xs"
                className="font-mono text-[10px] shrink-0"
                onClick={() => setSelectedKey(active ? '' : participant.actor_id)}
              >
                <Monitor className="w-3 h-3 mr-1.5" />
                {participant.actor_id}
                {!pane ? <span className="ml-1 text-muted-foreground">· no pty</span> : null}
              </Button>
            )
          })
        )}
      </div>

      <div className="flex-1 min-h-0 p-4">
        <div className="h-full min-h-[420px] rounded-lg border bg-[#101010] text-[#e6e6e6] overflow-hidden">
          {target ? (
            <ScrollArea className="h-full">
              <div className="px-4 py-3 border-b border-white/10 bg-black/20 flex items-center justify-between gap-2 text-[11px]">
                <div className="min-w-0">
                  <span className="font-bold truncate">
                    {selectedPane?.participant_id || selectedPane?.label || selectedPane?.id || target}
                  </span>
                  <span className="text-white/50 ml-2 font-mono truncate">
                    {selectedPane?.session_pane || selectedPane?.id || selectedPane?.session}
                  </span>
                </div>
                <div className="shrink-0 text-white/50 font-mono">
                  {captureQuery.data?.lines_requested ? `${captureQuery.data.lines_requested} lines` : 'live preview'}
                </div>
              </div>
              <pre className="px-4 py-3 text-[12px] leading-5 whitespace-pre-wrap break-words font-mono">
                {captureQuery.isLoading
                  ? 'Loading PTY capture…'
                  : captureQuery.error
                    ? String((captureQuery.error as Error).message || 'Failed to load PTY capture')
                    : captureQuery.data?.content || 'No PTY output available yet.'}
              </pre>
            </ScrollArea>
          ) : selectedKey ? (
            <div className="h-full flex items-center justify-center text-sm text-white/50 px-6 text-center">
              No tmux PTY is currently attached for <span className="mx-1 font-mono text-white/80">{selectedKey}</span>. Delivery can still work through participant transport if transport is available.
            </div>
          ) : (
            <div className="h-full flex items-center justify-center text-sm text-white/50">
              Select a room participant to preview its PTY.
            </div>
          )}
        </div>
      </div>

      <div className="border-t bg-primary/5 px-4 py-3 space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2 text-[10px] font-mono">
          <div className="text-muted-foreground">
            Dispatch via room transport as <span className="text-primary font-bold">{sender}</span>.
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <Badge variant="outline" className="h-5 px-2 text-[10px] font-mono">
              {audienceSummary}
            </Badge>
            <Button
              variant={showAudiencePanel ? 'secondary' : 'outline'}
              size="xs"
              onClick={() => setShowAudiencePanel((current) => !current)}
              disabled={isBroadcast}
            >
              audience
            </Button>
            <Button
              variant={showReminderEditor ? 'secondary' : 'outline'}
              size="xs"
              onClick={() => setShowReminderEditor((current) => !current)}
              disabled={isBroadcast}
            >
              remind
            </Button>
            <Button
              variant={showReminderList ? 'secondary' : 'outline'}
              size="xs"
              onClick={() => setShowReminderList((current) => !current)}
            >
              reminders ({remindersQuery.data?.count ?? 0})
            </Button>
            <Button
              variant={showRebindPanel ? 'secondary' : 'outline'}
              size="xs"
              onClick={() => setShowRebindPanel((current) => !current)}
              disabled={!selectedPane}
            >
              advanced
            </Button>
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-[140px_minmax(0,1fr)_140px_220px] gap-2">
          <select
            value={audienceMode}
            onChange={(e) => setAudienceMode(e.target.value === 'broadcast' ? 'broadcast' : 'selected')}
            className="h-8 rounded-md border border-input bg-background px-3 text-xs font-mono"
            disabled={!!sending}
          >
            <option value="selected">selected</option>
            <option value="broadcast">broadcast (*)</option>
          </select>
          <Input
            value={subject}
            onChange={(e) => setSubject(e.target.value)}
            placeholder="Optional subject"
            className="h-8 text-xs"
            disabled={!!sending}
          />
          <div className="relative group">
            <select
              value={kind}
              onChange={(e) => setKind(e.target.value)}
              className={cn(
                'w-full h-8 rounded-md border border-input bg-background px-3 text-xs font-mono transition-colors appearance-none pr-8',
                transportUnavailable ? 'border-red-500 text-red-600 focus:ring-red-500' : (transportReady ? 'border-green-500/50' : ''),
              )}
              disabled={!!sending}
            >
              <option value="info">info</option>
              <option value="instruction">instruction</option>
              <option value="alert">alert</option>
              <option value="review_request">review_request</option>
            </select>
            <div className="absolute right-2 top-1.5 pointer-events-none text-muted-foreground">
              {transportUnavailable ? <div className="w-2 h-2 rounded-full bg-red-500 mt-1" /> : (transportReady ? <div className="w-2 h-2 rounded-full bg-green-500 mt-1" /> : null)}
            </div>
          </div>
          <select
            value={selectedPreset}
            onChange={(e) => {
              const presetId = e.target.value
              setSelectedPreset(presetId)
              const preset = MESSAGE_PRESETS.find((entry) => entry.id === presetId)
              if (!preset) return
              setSubject(preset.subject ?? '')
              setDraft(preset.body)
              setKind(preset.kind)
              setAckRequired(preset.ack_required)
              setReplyExpected(preset.reply_expected)
              setInterrupt(preset.interrupt)
              if (preset.audience === 'broadcast') {
                setAudienceMode('broadcast')
              } else {
                setAudienceMode('selected')
                if (paneRecipient && dispatchableParticipants.some((participant) => participant.actor_id === paneRecipient)) {
                  setSelectedRecipients([paneRecipient])
                }
              }
            }}
            className="h-8 rounded-md border border-input bg-background px-3 text-xs font-mono"
            disabled={!!sending}
          >
            <option value="">template / preset</option>
            {MESSAGE_PRESETS.map((preset) => (
              <option key={preset.id} value={preset.id}>
                {preset.label}
              </option>
            ))}
          </select>
        </div>

        {showAudiencePanel && !isBroadcast ? (
          <div className="rounded-md border bg-background/60 px-3 py-2 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <Button variant="outline" size="xs" disabled={!!sending} onClick={() => paneRecipient && setSelectedRecipients([paneRecipient])}>
                Selected Pane
              </Button>
              <Button variant="outline" size="xs" disabled={!!sending} onClick={() => setSelectedRecipients(dispatchableParticipants.map((participant) => participant.actor_id))}>
                All Agents
              </Button>
              <Button variant="outline" size="xs" disabled={!!sending} onClick={() => setSelectedRecipients(dispatchableParticipants.filter((participant) => participant.role !== 'coordinator').map((participant) => participant.actor_id))}>
                Omit Coordinators
              </Button>
              <Button variant="ghost" size="xs" disabled={!!sending} onClick={() => setSelectedRecipients([])}>
                Clear
              </Button>
            </div>
            <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-2">
              {dispatchableParticipants.map((participant) => {
                const checked = selectedRecipients.includes(participant.actor_id)
                const transportState = participant.transport?.transport
                return (
                  <label
                    key={participant.actor_id}
                    className={cn(
                      'flex items-center gap-2 rounded-md border px-2 py-1.5 text-[11px] font-mono cursor-pointer',
                      checked ? 'border-primary bg-primary/5' : 'border-border bg-background',
                    )}
                  >
                    <Checkbox
                      checked={checked}
                      onCheckedChange={(next) => {
                        setSelectedRecipients((current) => {
                          if (next) return current.includes(participant.actor_id) ? current : [...current, participant.actor_id]
                          return current.filter((actorId) => actorId !== participant.actor_id)
                        })
                      }}
                    />
                    <span className="font-semibold">{participant.actor_id}</span>
                    {participant.role ? <span className="text-muted-foreground">[{participant.role}]</span> : null}
                    {transportState === 'unavailable' ? <span className="text-red-600">offline</span> : null}
                  </label>
                )
              })}
            </div>
          </div>
        ) : null}

        <Textarea
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder={isBroadcast ? 'Broadcast to the room…' : (selectedRecipients.length > 0 ? `Message for ${selectedRecipients.join(', ')}…` : 'Select one or more participants')}
          className="min-h-[76px] text-xs resize-none bg-background"
          disabled={(!isBroadcast && selectedRecipients.length === 0) || !!sending}
        />

        {showReminderEditor && !isBroadcast ? (
          <div className="rounded-md border bg-background/60 px-3 py-2 space-y-2">
            <div className="rounded-md border border-dashed bg-muted/20 px-3 py-2 text-[11px] font-mono text-muted-foreground">
              <span className="font-bold text-foreground">request being followed up:</span>{' '}
              {draft.trim() ? draft.trim() : 'use the main message box above to write the request first'}
            </div>
            <div className="grid grid-cols-1 md:grid-cols-[180px_110px_110px_1fr] gap-2">
              <select
                value={effectiveReminderRecipient}
                onChange={(e) => setReminderRecipient(e.target.value)}
                className="h-8 rounded-md border border-input bg-background px-3 text-xs font-mono"
                disabled={addReminderMutation.isPending || selectedRecipients.length === 1}
              >
                <option value="">recipient</option>
                {reminderEligibleParticipants.map((participant) => (
                  <option key={participant.actor_id} value={participant.actor_id}>
                    {participant.actor_id}
                  </option>
                ))}
              </select>
              <Input value={reminderEvery} onChange={(e) => setReminderEvery(e.target.value)} placeholder="15m" className="h-8 text-xs font-mono" disabled={addReminderMutation.isPending} />
              <Input value={reminderMaxIterations} onChange={(e) => setReminderMaxIterations(e.target.value)} placeholder="3" className="h-8 text-xs font-mono" disabled={addReminderMutation.isPending} />
              <label className="inline-flex items-center gap-2 rounded-md border px-3 text-[11px] font-mono">
                <input type="checkbox" checked={allowPassiveReminder} disabled={addReminderMutation.isPending} onChange={(e) => setAllowPassiveReminder(e.target.checked)} />
                <span>allow passive if loop is inactive</span>
              </label>
            </div>
          </div>
        ) : null}

        {showReminderList ? (
          <div className="rounded-md border bg-background/60 px-3 py-2 space-y-2">
            <div className="flex items-center justify-between gap-2">
              <div className="text-[10px] font-mono text-muted-foreground">Active room reminders.</div>
              <Button variant="outline" size="xs" className="font-bold" onClick={() => remindersQuery.refetch()} disabled={remindersQuery.isFetching}>
                <RefreshCw className={cn('w-3.5 h-3.5 mr-1.5', remindersQuery.isFetching && 'animate-spin')} />
                Refresh
              </Button>
            </div>
            {remindersQuery.isLoading ? (
              <div className="text-[11px] font-mono text-muted-foreground">Loading reminders…</div>
            ) : remindersQuery.error ? (
              <div className="text-[11px] font-mono text-red-600">{String((remindersQuery.error as Error).message || 'Failed to load reminders')}</div>
            ) : (remindersQuery.data?.reminders?.length ?? 0) === 0 ? (
              <div className="text-[11px] font-mono text-muted-foreground">No active reminders.</div>
            ) : (
              <div className="space-y-2">
                {remindersQuery.data?.reminders.map((reminder) => (
                  <div key={reminder.id} className="flex items-start justify-between gap-3 rounded-md border bg-background px-3 py-2">
                    <div className="min-w-0 space-y-1 text-[11px] font-mono">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-bold text-foreground">{reminder.recipient}</span>
                        <Badge variant="outline" className="h-5 px-2 text-[10px] font-mono">{reminder.interval}</Badge>
                        <span className="text-muted-foreground">{reminder.sent_count}/{reminder.max_iterations}</span>
                        {!reminder.active ? <span className="text-muted-foreground">inactive</span> : null}
                      </div>
                      <div className="text-foreground">{reminder.subject || reminder.body}</div>
                      {reminder.subject && reminder.body ? <div className="text-muted-foreground whitespace-pre-wrap break-words">{reminder.body}</div> : null}
                    </div>
                    <Button
                      variant="outline"
                      size="xs"
                      className="shrink-0"
                      disabled={cancelReminderMutation.isPending || !reminder.active}
                      onClick={async () => {
                        try {
                          await cancelReminderMutation.mutateAsync(reminder.id)
                        } catch (error) {
                          setReminderError(error instanceof Error ? error.message : 'Failed to cancel reminder')
                        }
                      }}
                    >
                      Cancel
                    </Button>
                  </div>
                ))}
              </div>
            )}
          </div>
        ) : null}

        {showRebindPanel ? (
          <div className="rounded-md border bg-background/60 px-3 py-2 space-y-2">
            <div className="text-[10px] font-mono text-muted-foreground">
              Optional override: rebind the selected pane to a canonical room participant after a provider restart.
            </div>
            <div className="flex items-center gap-2">
              <select
                value={bindActor}
                onChange={(e) => setBindActor(e.target.value)}
                className="h-8 rounded-md border border-input bg-background px-3 text-xs font-mono"
                disabled={!selectedPane || !!rebinding}
              >
                {bindOptions.map((actorId) => (
                  <option key={actorId} value={actorId}>{actorId}</option>
                ))}
              </select>
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-[10px] font-black uppercase tracking-tight"
                disabled={!selectedPane || !bindActor || !!rebinding}
                onClick={async () => {
                  if (!selectedPane || !bindActor) return
                  const participant = participants.find((entry) => entry.actor_id === bindActor)
                  try {
                    await onRebind({
                      actorId: bindActor,
                      delivery_binding: {
                        mux_backend: selectedPane.backend,
                        mux_session: selectedPane.session,
                        mux_pane_id: selectedPane.id,
                        transport_endpoint: selectedPane.socket_path || participant?.transport?.transport_endpoint,
                        transport_kind: selectedPane.socket_path ? 'pane_socket' : participantTransportKind(participant?.transport),
                      },
                    })
                    setRebindNotice(`rebound ${bindActor} to ${selectedPane.session_pane || selectedPane.id || selectedPane.session}`)
                  } catch (error) {
                    setRebindNotice(error instanceof Error ? error.message : 'failed to rebind room member')
                  }
                }}
              >
                Rebind Selected Pane
              </Button>
            </div>
          </div>
        ) : null}

        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-4 text-[10px] text-muted-foreground font-mono">
            <label className="inline-flex items-center gap-1.5 cursor-pointer">
              <input type="checkbox" checked={ackRequired} disabled={isBroadcast || selectedRecipients.length > 1 || !!sending} onChange={(e) => setAckRequired(e.target.checked)} />
              <span className={isBroadcast || selectedRecipients.length > 1 ? 'opacity-50' : ''}>ack</span>
            </label>
            <label className="inline-flex items-center gap-1.5 cursor-pointer">
              <input type="checkbox" checked={replyExpected} disabled={isBroadcast || selectedRecipients.length > 1 || !!sending} onChange={(e) => setReplyExpected(e.target.checked)} />
              <span className={isBroadcast || selectedRecipients.length > 1 ? 'opacity-50' : ''}>reply</span>
            </label>
            <label className="inline-flex items-center gap-1.5 cursor-pointer">
              <input type="checkbox" checked={interrupt} disabled={isBroadcast || !!sending} onChange={(e) => setInterrupt(e.target.checked)} />
              <span className={isBroadcast ? 'opacity-50' : ''}>interrupt</span>
            </label>
          </div>
          <div className="flex items-center gap-2">
            {showReminderEditor && !isBroadcast ? (
              <Button
                variant="outline"
                size="sm"
                className="h-8 text-[10px] font-black uppercase tracking-tight"
                disabled={!effectiveReminderRecipient || !draft.trim() || (!ackRequired && !replyExpected) || addReminderMutation.isPending}
                onClick={async () => {
                  const maxIterations = Number.parseInt(reminderMaxIterations, 10)
                  try {
                    const result = await addReminderMutation.mutateAsync({
                      recipient: effectiveReminderRecipient,
                      subject: subject.trim() || undefined,
                      body: draft.trim(),
                      every: reminderEvery.trim(),
                      max_iterations: Number.isFinite(maxIterations) && maxIterations > 0 ? maxIterations : 3,
                      ack_required: ackRequired,
                      reply_expected: replyExpected,
                      interrupt,
                      allow_passive: allowPassiveReminder,
                    })
                    setLastSendResult({
                      id: result.message.id,
                      room_id: roomId,
                      stream: result.message.stream || '',
                      status: 'queued',
                      delivery_owner: 'room_loop',
                      delivery_pending: true,
                    })
                  } catch (error) {
                    setReminderError(error instanceof Error ? error.message : 'Failed to schedule reminder')
                  }
                }}
              >
                <SendHorizonal className="w-3 h-3 mr-1.5" />
                Send + Remind
              </Button>
            ) : null}
            <Button
              size="sm"
              disabled={(!isBroadcast && selectedRecipients.length === 0) || !draft.trim() || !!sending}
              onClick={async () => {
                const body = draft.trim()
                if ((!isBroadcast && selectedRecipients.length === 0) || !body) return
                try {
                  const result = await onSend({
                    recipients: isBroadcast ? undefined : selectedRecipients,
                    subject: subject.trim() || undefined,
                    body,
                    kind,
                    ack_required: isBroadcast || selectedRecipients.length > 1 ? false : ackRequired,
                    reply_expected: isBroadcast || selectedRecipients.length > 1 ? false : replyExpected,
                    interrupt: isBroadcast ? false : interrupt,
                  })
                  setLastSendResult(result)
                  setSendError('')
                  setReminderError('')
                  setSelectedPreset('')
                  setDraft('')
                  setSubject('')
                  setAckRequired(false)
                  setReplyExpected(false)
                  setInterrupt(false)
                } catch (error) {
                  setSendError(error instanceof Error ? error.message : 'Failed to send message')
                }
              }}
              className="h-8 text-[10px] font-black uppercase tracking-tight"
            >
              <SendHorizonal className="w-3 h-3 mr-1.5" />
              Send
            </Button>
          </div>
        </div>

        <div className={cn(
          'rounded-md border px-3 py-2 text-[11px] font-mono',
          statusLine.startsWith('ERR') ? 'border-red-500/40 bg-red-500/5 text-red-600' : 'bg-background/80 text-muted-foreground',
        )}>
          {statusLine}
        </div>
      </div>
    </div>
  )
}

function paneTarget(pane: MuxPane): string {
  return pane.participant_id || pane.label || pane.id || ''
}

function paneDispatchActor(pane: MuxPane): string {
  return pane.participant_id || pane.label || ''
}

function summarizeLiveRelay(results?: RoomSendMessageResult['live_relay']): string {
  if (!results || results.length === 0) return 'not reported'
  const totalDelivered = results.reduce((sum, result) => sum + (result.delivered_count ?? 0), 0)
  const totalFailed = results.reduce((sum, result) => sum + (result.failed_count ?? 0), 0)
  const backends = Array.from(new Set(results.map((result) => result.backend).filter(Boolean))).join(', ') || 'unknown'
  const errors = results.map((result) => result.error).filter(Boolean)
  if (errors.length > 0) {
    return `${backends}; error: ${errors[0]}`
  }
  return `${backends}; delivered ${totalDelivered}, failed ${totalFailed}`
}

function summarizeSendDelivery(result?: RoomSendMessageResult): string {
  if (!result) return 'not reported'
  if (result.live_relay && result.live_relay.length > 0) {
    return summarizeLiveRelay(result.live_relay)
  }
  if (result.delivery_owner === 'room_loop' || result.delivery_pending || result.status === 'queued') {
    return 'queued for room loop'
  }
  return 'not reported'
}

const MESSAGE_PRESETS: MessagePreset[] = [
  {
    id: 'status-check',
    label: 'coordination / status check',
    audience: 'selected',
    kind: 'instruction',
    subject: 'Status update requested',
    body: 'Please post a durable status update with changed, verified, and remaining.',
    ack_required: false,
    reply_expected: true,
    interrupt: false,
  },
  {
    id: 'review-request',
    label: 'review / request findings',
    audience: 'selected',
    kind: 'review_request',
    subject: 'Review request',
    body: 'Please review the current slice for regressions, missing tests, and interface risks. Reply with prioritized findings first.',
    ack_required: false,
    reply_expected: true,
    interrupt: false,
  },
  {
    id: 'ack-ping',
    label: 'coordination / ack ping',
    audience: 'selected',
    kind: 'alert',
    subject: 'Acknowledge receipt',
    body: 'Please acknowledge this message in the room.',
    ack_required: true,
    reply_expected: false,
    interrupt: false,
  },
  {
    id: 'interrupt-nudge',
    label: 'recovery / interrupt nudge',
    audience: 'selected',
    kind: 'alert',
    subject: 'Interrupt and reply',
    body: 'Interrupt current work if needed and reply with your current state, blocker, or next step.',
    ack_required: false,
    reply_expected: true,
    interrupt: true,
  },
  {
    id: 'broadcast-sync',
    label: 'broadcast / room sync',
    audience: 'broadcast',
    kind: 'info',
    subject: 'Room sync',
    body: 'Room update: post your current state, notable risks, or immediate next step if needed.',
    ack_required: false,
    reply_expected: false,
    interrupt: false,
  },
]
