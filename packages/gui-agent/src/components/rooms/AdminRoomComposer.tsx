import { useEffect, useMemo, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { SendHorizonal, ShieldAlert, ZapOff } from 'lucide-react'
import type { RoomStatusParticipant } from '@/api/types'
import { cn } from '@/lib/utils'

interface AdminRoomComposerProps {
  sender: string
  participants: RoomStatusParticipant[]
  onSend: (params: {
    recipient?: string
    subject?: string
    body: string
    kind: string
    ack_required: boolean
    reply_expected: boolean
    interrupt: boolean
  }) => void
  disabled?: boolean
}

export function AdminRoomComposer({ sender, participants, onSend, disabled }: AdminRoomComposerProps) {
  const [recipient, setRecipient] = useState('*')
  const [subject, setSubject] = useState('')
  const [body, setBody] = useState('')
  const [kind, setKind] = useState('info')
  const [ackRequired, setAckRequired] = useState(false)
  const [replyExpected, setReplyExpected] = useState(false)
  const [interrupt, setInterrupt] = useState(false)

  const recipientOptions = useMemo(() => {
    const ids = participants.map((p) => p.actor_id).filter(Boolean).sort()
    return ['*', ...ids]
  }, [participants])

  const isBroadcast = recipient === '*'
  const selectedParticipant = useMemo(() => {
    return participants.find(p => p.actor_id === recipient)
  }, [participants, recipient])

  const transportUnavailable = selectedParticipant?.transport?.transport === 'unavailable'
  const transportReady = selectedParticipant?.transport?.transport === 'available'

  useEffect(() => {
    if (!isBroadcast) return
    if (ackRequired) setAckRequired(false)
    if (replyExpected) setReplyExpected(false)
    if (interrupt) setInterrupt(false)
  }, [ackRequired, interrupt, isBroadcast, replyExpected])

  const canSend = body.trim().length > 0

  return (
    <div className="border-b bg-primary/5 px-4 py-3 space-y-3">
      <div className="flex items-center gap-2 text-[10px] font-black uppercase tracking-widest text-primary">
        <ShieldAlert className="w-3.5 h-3.5" />
        <span>Admin Dispatch</span>
        <span className="text-muted-foreground normal-case font-medium tracking-normal">as {sender}</span>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-[minmax(0,1fr)_180px_140px] gap-2">
        <Input
          value={subject}
          onChange={(e) => setSubject(e.target.value)}
          placeholder="Optional subject"
          className="h-8 text-xs"
        />
        <div className="relative group">
          <select
            value={recipient}
            onChange={(e) => setRecipient(e.target.value)}
            className={cn(
              "w-full h-8 rounded-md border border-input bg-background px-3 text-xs font-mono transition-colors appearance-none pr-8",
              transportUnavailable ? "border-red-500 text-red-600 focus:ring-red-500" : (transportReady ? "border-green-500/50" : "")
            )}
          >
            {recipientOptions.map((value) => (
              <option key={value} value={value}>
                {value === '*' ? 'broadcast (*)' : value}
              </option>
            ))}
          </select>
          <div className="absolute right-2 top-1.5 pointer-events-none text-muted-foreground">
            {transportUnavailable ? <ZapOff className="w-3.5 h-3.5 text-red-500" /> : (transportReady ? <div className="w-2 h-2 rounded-full bg-green-500 mt-1" /> : null)}
          </div>
        </div>
        <select
          value={kind}
          onChange={(e) => setKind(e.target.value)}
          className="h-8 rounded-md border border-input bg-background px-3 text-xs font-mono"
        >
          <option value="info">info</option>
          <option value="instruction">instruction</option>
          <option value="alert">alert</option>
          <option value="review_request">review_request</option>
        </select>
      </div>

      <Textarea
        value={body}
        onChange={(e) => setBody(e.target.value)}
        placeholder={isBroadcast ? 'Broadcast to the room…' : `Message for ${recipient}…`}
        className="min-h-[88px] text-xs resize-none bg-background"
      />

      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-4 text-[10px] text-muted-foreground font-mono">
          <label className="inline-flex items-center gap-1.5 cursor-pointer">
            <input
              type="checkbox"
              checked={ackRequired}
              disabled={isBroadcast}
              onChange={(e) => setAckRequired(e.target.checked)}
            />
            <span className={isBroadcast ? 'opacity-50' : ''}>ack</span>
          </label>
          <label className="inline-flex items-center gap-1.5 cursor-pointer">
            <input
              type="checkbox"
              checked={replyExpected}
              disabled={isBroadcast}
              onChange={(e) => setReplyExpected(e.target.checked)}
            />
            <span className={isBroadcast ? 'opacity-50' : ''}>reply</span>
          </label>
          <label className="inline-flex items-center gap-1.5 cursor-pointer">
            <input
              type="checkbox"
              checked={interrupt}
              disabled={isBroadcast}
              onChange={(e) => setInterrupt(e.target.checked)}
            />
            <span className={isBroadcast ? 'opacity-50' : ''}>interrupt</span>
          </label>
          
          <div className="h-4 w-px bg-muted mx-1" />

          {transportUnavailable ? (
            <span className="text-red-600 font-bold uppercase tracking-tighter animate-pulse flex items-center gap-1">
              <ZapOff className="w-3 h-3" /> Transport Unavailable
            </span>
          ) : isBroadcast ? (
            <span className="text-amber-600/70 italic">informational only</span>
          ) : replyExpected ? (
            <span className="text-amber-600">reply obligation</span>
          ) : ackRequired ? (
            <span className="text-amber-600">ack obligation</span>
          ) : interrupt ? (
            <span className="text-amber-600">relay escape first</span>
          ) : null}
        </div>
        <Button
          size="sm"
          disabled={disabled || !canSend}
          onClick={() => {
            onSend({
              recipient: isBroadcast ? undefined : recipient,
              subject: subject.trim() || undefined,
              body: body.trim(),
              kind,
              ack_required: isBroadcast ? false : ackRequired,
              reply_expected: isBroadcast ? false : replyExpected,
              interrupt: isBroadcast ? false : interrupt,
            })
            setBody('')
            setSubject('')
            setAckRequired(false)
            setReplyExpected(false)
            setInterrupt(false)
          }}
          className="h-8 text-[10px] font-black uppercase tracking-tight"
        >
          <SendHorizonal className="w-3 h-3 mr-1.5" />
          Send
        </Button>
      </div>
    </div>
  )
}
